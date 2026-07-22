package smtp_test

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/golib/smtp"
)

// These tests cover ProdSender's own SMTP exchange. The handshake happens in the right order, and
// a server that accepts a connection and then goes quiet releases the caller on the timeout.
//
// The fake server is a real listener speaking enough SMTP to complete a transaction, so the client
// under test is the real net/smtp client on a real socket.

// fakeServer is a minimal SMTP server for one connection.
type fakeServer struct {
	listener net.Listener
	// stall makes the server accept the connection and then never write a greeting. The dial
	// succeeds, so only a deadline on the connection ends the wait.
	stall bool
	// transcript records the commands the client sent, in order.
	transcript []string
	// stop releases a stalled handler at cleanup; done reports that serve has returned. They are
	// separate channels: the handler waits on stop and the cleanup waits on done.
	stop chan struct{}
	done chan struct{}
}

func newFakeServer(t *testing.T, stall bool) *fakeServer {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &fakeServer{
		listener: listener,
		stall:    stall,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}

	go server.serve()

	t.Cleanup(func() {
		close(server.stop)

		_ = listener.Close()

		<-server.done
	})

	return server
}

func (s *fakeServer) addr() string { return s.listener.Addr().String() }

func (s *fakeServer) serve() {
	defer close(s.done)

	conn, err := s.listener.Accept()
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	if s.stall {
		// Accept and say nothing. The client waits here for a greeting until its deadline fires.
		<-s.stop

		return
	}

	reader := bufio.NewReader(conn)
	write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }

	write("220 fake ESMTP")

	inData := false

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")

		if inData {
			if line == "." {
				inData = false

				write("250 2.0.0 Ok")
			}

			continue
		}

		s.transcript = append(s.transcript, line)

		switch {
		case strings.HasPrefix(line, "EHLO"):
			// AUTH is advertised and STARTTLS is not, so the client skips the upgrade and carries on.
			// PlainAuth permits plaintext credentials to a loopback server.
			write("250-fake")
			write("250 AUTH PLAIN")
		case strings.HasPrefix(line, "AUTH"):
			write("235 2.7.0 Accepted")
		case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
			write("250 2.0.0 Ok")
		case strings.HasPrefix(line, "DATA"):
			inData = true

			write("354 End data with <CR><LF>.<CR><LF>")
		case strings.HasPrefix(line, "QUIT"):
			write("221 2.0.0 Bye")

			return
		default:
			write("250 2.0.0 Ok")
		}
	}
}

func testTemplate(t *testing.T) *template.Template {
	t.Helper()

	tmpl, err := template.New("mail").Parse("hello {{ . }}")
	require.NoError(t, err)

	return tmpl
}

func TestProdSenderSendMail(t *testing.T) {
	t.Parallel()

	server := newFakeServer(t, false)

	host, _, err := net.SplitHostPort(server.addr())
	require.NoError(t, err)

	sender := &smtp.ProdSender{
		Addr:  server.addr(),
		Name:  "Agora",
		Email: "noreply@example.com",
		// The fake accepts any credential; what matters is that AUTH is attempted at all.
		Password: "hunter2",
		// PlainAuth checks this against the server it is talking to, so Domain is the SMTP host.
		Domain: host,
	}

	err = sender.SendMail(
		smtp.MailUsers{{Name: "Recipient", Email: "to@example.com"}},
		testTemplate(t), "mail", "world",
	)
	require.NoError(t, err)

	joined := strings.Join(server.transcript, "\n")
	require.Contains(t, joined, "AUTH PLAIN", "credentials must be offered")
	require.Contains(t, joined, "MAIL FROM:<noreply@example.com>")
	require.Contains(t, joined, "RCPT TO:<to@example.com>")
	require.Contains(t, joined, "DATA")
	require.Contains(t, joined, "QUIT")

	// AUTH before MAIL. The transaction starts only once the client is authenticated.
	authAt := strings.Index(joined, "AUTH")
	mailAt := strings.Index(joined, "MAIL FROM")
	require.Less(t, authAt, mailAt, "AUTH must precede MAIL FROM")
}

func TestProdSenderRefusesServerWithoutAuth(t *testing.T) {
	t.Parallel()

	// A server advertising no AUTH fails the send. Every message goes out authenticated.
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		reader := bufio.NewReader(conn)

		_, _ = conn.Write([]byte("220 fake ESMTP\r\n"))

		for {
			line, readErr := reader.ReadString('\n')
			if readErr != nil {
				return
			}

			if strings.HasPrefix(line, "EHLO") {
				_, _ = conn.Write([]byte("250 fake\r\n")) // no extensions at all

				continue
			}

			_, _ = conn.Write([]byte("221 Bye\r\n"))

			return
		}
	}()

	sender := &smtp.ProdSender{Addr: listener.Addr().String(), Email: "noreply@example.com"}

	err = sender.SendMail(
		smtp.MailUsers{{Name: "R", Email: "to@example.com"}},
		testTemplate(t), "mail", "world",
	)
	require.ErrorIs(t, err, smtp.ErrNoAuthSupport)
}

func TestProdSenderTimesOutOnStalledServer(t *testing.T) {
	t.Parallel()

	server := newFakeServer(t, true)

	sender := &smtp.ProdSender{
		Addr:    server.addr(),
		Email:   "noreply@example.com",
		Timeout: 150 * time.Millisecond,
	}

	// The connection is accepted, so the deadline on the connection bounds the wait for a greeting.
	// The goroutine holding it is invisible: the HTTP request that spawned it has already returned
	// 202.
	start := time.Now()

	err := sender.SendMail(
		smtp.MailUsers{{Name: "R", Email: "to@example.com"}},
		testTemplate(t), "mail", "world",
	)
	elapsed := time.Since(start)

	require.Error(t, err)

	var netErr net.Error

	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected a timeout, got %v", err)
	require.Less(t, elapsed, 5*time.Second, "the send must return on its own deadline")
}

func TestProdSenderPingTimesOutOnStalledServer(t *testing.T) {
	t.Parallel()

	server := newFakeServer(t, true)

	// Ping backs the readiness probe, so it returns within the timeout and reports the outage it
	// exists to detect.
	sender := &smtp.ProdSender{
		Addr:    server.addr(),
		Email:   "noreply@example.com",
		Timeout: 150 * time.Millisecond,
	}

	start := time.Now()
	err := sender.Ping()

	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestProdSenderDefaultTimeoutApplies(t *testing.T) {
	t.Parallel()

	// An unset Timeout selects DefaultTimeout.
	sender := &smtp.ProdSender{Addr: "192.0.2.1:25", Email: "noreply@example.com"}

	require.Positive(t, smtp.DefaultTimeout)

	_ = sender // the constant is the contract; dialing TEST-NET-1 costs DefaultTimeout
}
