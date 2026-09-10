package smtp_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/a-novel-kit/golib/smtp"
)

func testProdSenderDeadline(t *testing.T) {
	for _, operation := range []string{"send", "ping"} {
		for _, greeting := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s greeting=%t", operation, greeting), func(t *testing.T) {
				const timeout = 2 * time.Second

				useDelayedResolver(t, timeout/2)
				listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				require.NoError(t, err)
				t.Cleanup(func() { _ = listener.Close() })

				closed := make(chan error, 1)

				go func() {
					closed <- stallSMTP(listener, greeting)
				}()

				_, port, err := net.SplitHostPort(listener.Addr().String())
				require.NoError(t, err)

				sender := &smtp.ProdSender{
					Addr:    net.JoinHostPort("smtp-deadline.invalid.", port),
					Email:   "noreply@example.com",
					Timeout: timeout,
				}
				tmpl := testTemplate(t)
				start := time.Now()

				if operation == "send" {
					err = sender.SendMail(smtp.MailUsers{{Email: "to@example.com"}}, tmpl, "mail", "world")
				} else {
					err = sender.Ping()
				}

				elapsed := time.Since(start)

				var timeoutErr net.Error
				require.ErrorAs(t, err, &timeoutErr)
				require.True(t, timeoutErr.Timeout())

				if greeting {
					require.ErrorContains(t, err, "greet SMTP server")
				} else {
					require.ErrorContains(t, err, "open SMTP client")
				}

				require.GreaterOrEqual(t, elapsed, timeout)
				require.Less(t, elapsed, timeout+timeout/4, "DNS and SMTP must share one timeout")

				select {
				case err := <-closed:
					require.NoError(t, err, "the client must close the connection on timeout")
				case <-time.After(time.Second):
					t.Fatal("SMTP connection remained open after timeout")
				}
			})
		}
	}
}

// stallSMTP waits for the client to close, either before the greeting or during EHLO.
func stallSMTP(listener net.Listener, greeting bool) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	err = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err != nil {
		return err
	}

	if greeting {
		_, err = io.WriteString(conn, "220 fake ESMTP\r\n")
		if err != nil {
			return err
		}
	}

	_, err = io.Copy(io.Discard, conn)

	return err
}

// useDelayedResolver spends part of the dial budget in DNS and resolves only to loopback.
// Its caller must be sequential because the production dialer uses net.DefaultResolver.
func useDelayedResolver(t *testing.T, delay time.Duration) {
	t.Helper()

	server, err := (&net.ListenConfig{}).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- serveLoopbackDNS(server) }()

	original := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			timer := time.NewTimer(delay)
			defer timer.Stop()

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return (&net.Dialer{}).DialContext(ctx, "udp", server.LocalAddr().String())
			}
		},
	}

	t.Cleanup(func() {
		net.DefaultResolver = original
		_ = server.Close()

		require.ErrorIs(t, <-done, net.ErrClosed)
	})
}

func serveLoopbackDNS(server net.PacketConn) error {
	buffer := make([]byte, 4096)
	for {
		size, addr, err := server.ReadFrom(buffer)
		if err != nil {
			return err
		}

		var query dnsmessage.Message

		err = query.Unpack(buffer[:size])
		if err != nil {
			return err
		}

		reply := dnsmessage.Message{
			Header: dnsmessage.Header{
				ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true,
			},
			Questions: query.Questions,
		}
		for _, question := range query.Questions {
			if question.Type == dnsmessage.TypeA {
				reply.Answers = append(reply.Answers, dnsmessage.Resource{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
					Body:   &dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}},
				})
			}
		}

		packet, err := reply.Pack()
		if err != nil {
			return err
		}

		_, err = server.WriteTo(packet, addr)
		if err != nil {
			return err
		}
	}
}
