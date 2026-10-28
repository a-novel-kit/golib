package smtp_test

import (
	"bufio"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/textproto"
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

// fakeMode is how a fakeServer departs from a complete session.
type fakeMode int

const (
	fakeComplete fakeMode = iota
	// fakeStall accepts the connection and then never writes a greeting. The dial succeeds, so only a
	// deadline on the connection ends the wait.
	fakeStall
	// fakeDropQuit ends the session at QUIT without a reply.
	fakeDropQuit
)

// fakeServer is a minimal SMTP server for one connection.
type fakeServer struct {
	listener net.Listener
	mode     fakeMode
	// extensions are the keywords the server advertises in its EHLO reply.
	extensions []string
	// transcript records the commands the client sent, in order.
	transcript []string
	// stop releases a stalled handler at cleanup; done reports that serve has returned. They are
	// separate channels: the handler waits on stop and the cleanup waits on done.
	stop chan struct{}
	done chan struct{}
}

// newFakeServer starts a fakeServer advertising extensions, stopped with the test.
func newFakeServer(t *testing.T, mode fakeMode, extensions ...string) *fakeServer {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &fakeServer{
		listener:   listener,
		mode:       mode,
		extensions: extensions,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}

	go server.serve()

	t.Cleanup(server.shutdown)

	return server
}

// shutdown releases a stalled handler, stops accepting, and waits for serve to return.
func (s *fakeServer) shutdown() {
	close(s.stop)

	_ = s.listener.Close()

	<-s.done
}

// addr returns the address the server listens on.
func (s *fakeServer) addr() string { return s.listener.Addr().String() }

// wait returns once the server has handled its connection.
func (s *fakeServer) wait() { <-s.done }

// serve handles one connection, recording every command outside the message body.
func (s *fakeServer) serve() {
	defer close(s.done)

	conn, err := s.listener.Accept()
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	if s.mode == fakeStall {
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
			write("250-fake")

			for _, extension := range s.extensions {
				write("250-" + extension)
			}

			write("250 HELP")
		case strings.HasPrefix(line, "STARTTLS"):
			// The server ends the session instead of handshaking, so the client's handshake reads EOF.
			// A half-close drains the ClientHello, which a full close would answer with a reset.
			write("220 2.0.0 Ready to start TLS")

			_ = conn.(*net.TCPConn).CloseWrite()
			_, _ = io.Copy(io.Discard, reader)

			return
		case strings.HasPrefix(line, "AUTH"):
			write("235 2.7.0 Accepted")
		case strings.HasPrefix(line, "MAIL FROM"), strings.HasPrefix(line, "RCPT TO"):
			write("250 2.0.0 Ok")
		case strings.HasPrefix(line, "DATA"):
			inData = true

			write("354 End data with <CR><LF>.<CR><LF>")
		case strings.HasPrefix(line, "QUIT"):
			if s.mode != fakeDropQuit {
				write("221 2.0.0 Bye")
			}

			return
		default:
			write("250 2.0.0 Ok")
		}
	}
}

// testTemplate returns the template every test mail renders.
func testTemplate(t *testing.T) *template.Template {
	t.Helper()

	tmpl, err := template.New("mail").Parse("hello {{ . }}")
	require.NoError(t, err)

	return tmpl
}

// runOperation sends a test mail through sender when operation is "send", and pings it otherwise.
func runOperation(t *testing.T, sender *smtp.ProdSender, operation string) error {
	t.Helper()

	if operation == "send" {
		return sender.SendMail(smtp.MailUsers{{Email: "to@example.com"}}, testTemplate(t), "mail", "world")
	}

	return sender.Ping()
}

// plainAuthCredentials decodes the identity, username and password of the AUTH PLAIN command in
// transcript.
func plainAuthCredentials(t *testing.T, transcript []string) (string, string, string) {
	t.Helper()

	for _, line := range transcript {
		payload, ok := strings.CutPrefix(line, "AUTH PLAIN ")
		if !ok {
			continue
		}

		decoded, err := base64.StdEncoding.DecodeString(payload)
		require.NoError(t, err)

		credentials := strings.Split(string(decoded), "\x00")
		require.Len(t, credentials, 3)

		return credentials[0], credentials[1], credentials[2]
	}

	require.FailNow(t, "AUTH PLAIN command not found")

	return "", "", ""
}

//nolint:paralleltest,tparallel // Replaces net.DefaultResolver temporarily.
func TestProdSender(t *testing.T) {
	t.Run("SharedNetworkDeadline", testProdSenderDeadline)

	testCases := map[string]struct {
		email string
		hello string
	}{
		"Greeting/SenderDomain":       {email: "noreply@example.com", hello: "example.com"},
		"Greeting/QuotedLocalPart":    {email: `"no@reply"@example.org`, hello: "example.org"},
		"Greeting/DomainlessFallback": {email: "noreply", hello: "localhost"},
		"Greeting/EmptyFallback":      {hello: "localhost"},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := newFakeServer(t, fakeComplete, "AUTH PLAIN")
			host, _, err := net.SplitHostPort(server.addr())
			require.NoError(t, err)

			sender := &smtp.ProdSender{
				Addr:     server.addr(),
				Email:    testCase.email,
				Username: "relay-user@example.net",
				Password: "hunter2",
				Domain:   host,
			}

			require.NoError(t, sender.Ping())
			server.wait()
			require.Equal(t, "EHLO "+testCase.hello, server.transcript[0])
			require.Len(t, server.transcript, 3)
			require.Equal(t, "QUIT", server.transcript[2])
		})
	}

	rejectionCases := []struct {
		name string

		operation  string
		disconnect bool
	}{
		{name: "RejectedGreeting/Ping", operation: "ping"},
		{name: "RejectedGreeting/Ping/Disconnect", operation: "ping", disconnect: true},
		{name: "RejectedGreeting/Send", operation: "send"},
		{name: "RejectedGreeting/Send/Disconnect", operation: "send", disconnect: true},
	}

	for _, testCase := range rejectionCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })

			commands := make(chan []string, 1)

			go func() { commands <- rejectGreeting(listener, testCase.disconnect) }()

			sender := &smtp.ProdSender{
				Addr:    listener.Addr().String(),
				Email:   "noreply@example.com",
				Timeout: time.Second,
			}

			err = runOperation(t, sender, testCase.operation)
			require.Error(t, err)
			require.NotErrorIs(t, err, smtp.ErrNoAuthSupport)

			if testCase.disconnect {
				// net/smtp retries a failed EHLO as HELO, which sees the closed connection.
				require.ErrorIs(t, err, io.EOF)
			} else {
				var replyErr *textproto.Error
				require.ErrorAs(t, err, &replyErr)
				require.Equal(t, 421, replyErr.Code)
			}

			transcript := <-commands
			require.Equal(t, "EHLO example.com", transcript[0])

			for _, command := range transcript {
				require.True(t, strings.HasPrefix(command, "EHLO ") || strings.HasPrefix(command, "HELO "))
			}
		})
	}

	sendCases := map[string]struct {
		username         string
		expectedUsername string
	}{
		"Success/DistinctUsername": {
			username:         "relay-user@example.net",
			expectedUsername: "relay-user@example.net",
		},
		"Success/EmailFallback": {
			expectedUsername: "noreply@example.com",
		},
	}

	for name, testCase := range sendCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// The server offers no STARTTLS. PlainAuth permits plaintext credentials to a loopback server.
			server := newFakeServer(t, fakeComplete, "AUTH PLAIN")

			host, _, err := net.SplitHostPort(server.addr())
			require.NoError(t, err)

			sender := &smtp.ProdSender{
				Addr:     server.addr(),
				Name:     "A-Novel",
				Email:    "noreply@example.com",
				Username: testCase.username,
				Password: "hunter2",
				// PlainAuth checks this against the server it is talking to, so Domain is the SMTP host.
				Domain: host,
			}

			err = sender.SendMail(
				smtp.MailUsers{{Name: "Recipient", Email: "to@example.com"}},
				testTemplate(t), "mail", "world",
			)
			require.NoError(t, err)
			server.wait()

			joined := strings.Join(server.transcript, "\n")
			require.Equal(t, "EHLO example.com", server.transcript[0])
			require.Contains(t, joined, "MAIL FROM:<noreply@example.com>")
			require.Contains(t, joined, "RCPT TO:<to@example.com>")
			require.Contains(t, joined, "DATA")
			require.Contains(t, joined, "QUIT")

			identity, username, password := plainAuthCredentials(t, server.transcript)
			require.Empty(t, identity)
			require.Equal(t, testCase.expectedUsername, username)
			require.Equal(t, "hunter2", password)

			// AUTH before MAIL. The transaction starts only once the client is authenticated.
			authAt := strings.Index(joined, "AUTH")
			mailAt := strings.Index(joined, "MAIL FROM")
			require.Less(t, authAt, mailAt, "AUTH must precede MAIL FROM")
		})
	}

	// An IP relay admits the client without a Password. The client upgrades whenever the server offers
	// STARTTLS. Without a credential, a server that offers none stops the session unless
	// ForceUnencryptedTLS allows it.
	handshakeCases := []struct {
		name string

		operation  string
		extensions []string
		password   string
		force      bool

		expectErr      error
		expectCommands []string
	}{
		{
			name:       "Success/IPRelay/Send",
			operation:  "send",
			extensions: []string{"AUTH PLAIN"},
			force:      true,
			expectCommands: []string{
				"EHLO example.com", "MAIL FROM:<noreply@example.com>", "RCPT TO:<to@example.com>", "DATA", "QUIT",
			},
		},
		{
			name:           "Success/IPRelay/Ping",
			operation:      "ping",
			extensions:     []string{"AUTH PLAIN"},
			force:          true,
			expectCommands: []string{"EHLO example.com", "QUIT"},
		},
		{
			name:           "Error/IPRelay/NoStartTLS/Send",
			operation:      "send",
			extensions:     []string{"AUTH PLAIN"},
			expectErr:      smtp.ErrNoStartTLSSupport,
			expectCommands: []string{"EHLO example.com"},
		},
		{
			name:           "Error/IPRelay/NoStartTLS/Ping",
			operation:      "ping",
			extensions:     []string{"AUTH PLAIN"},
			expectErr:      smtp.ErrNoStartTLSSupport,
			expectCommands: []string{"EHLO example.com"},
		},
		{
			name:           "Error/IPRelay/StartTLSFirst",
			operation:      "send",
			extensions:     []string{"STARTTLS", "AUTH PLAIN"},
			force:          true,
			expectErr:      io.EOF,
			expectCommands: []string{"EHLO example.com", "STARTTLS"},
		},
		{
			name:           "Error/Credential/StartTLSFirst",
			operation:      "send",
			extensions:     []string{"STARTTLS", "AUTH PLAIN"},
			password:       "hunter2",
			expectErr:      io.EOF,
			expectCommands: []string{"EHLO example.com", "STARTTLS"},
		},
		{
			name:           "Error/Credential/NoAuthSupport",
			operation:      "send",
			password:       "hunter2",
			expectErr:      smtp.ErrNoAuthSupport,
			expectCommands: []string{"EHLO example.com"},
		},
	}

	for _, testCase := range handshakeCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newFakeServer(t, fakeComplete, testCase.extensions...)

			sender := &smtp.ProdSender{
				Addr:                server.addr(),
				Email:               "noreply@example.com",
				Password:            testCase.password,
				ForceUnencryptedTLS: testCase.force,
			}

			err := runOperation(t, sender, testCase.operation)
			require.ErrorIs(t, err, testCase.expectErr)
			server.wait()
			require.Equal(t, testCase.expectCommands, server.transcript)
		})
	}

	t.Run("Success/QuitDisconnect", func(t *testing.T) {
		t.Parallel()

		// The reply to the final "." accepted the message before QUIT, so a failed QUIT is no failed
		// send, and a caller never reports a delivered mail as lost.
		server := newFakeServer(t, fakeDropQuit)

		sender := &smtp.ProdSender{Addr: server.addr(), Email: "noreply@example.com", ForceUnencryptedTLS: true}

		require.NoError(t, runOperation(t, sender, "send"))
	})

	t.Run("Error/SendTimesOutOnStalledServer", func(t *testing.T) {
		t.Parallel()

		server := newFakeServer(t, fakeStall)

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
	})

	t.Run("Error/PingTimesOutOnStalledServer", func(t *testing.T) {
		t.Parallel()

		server := newFakeServer(t, fakeStall)

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
	})

	t.Run("Success/DefaultTimeout", func(t *testing.T) {
		t.Parallel()

		// An unset Timeout selects DefaultTimeout. Proving it on a dial would cost the whole
		// DefaultTimeout, so the test holds the constant to its contract.
		require.Positive(t, smtp.DefaultTimeout)
	})
}

// rejectGreeting answers every greeting of one connection with 421 and returns the commands it
// received. With disconnect, it ends the session after the first rejection.
func rejectGreeting(listener net.Listener, disconnect bool) []string {
	var transcript []string

	conn, err := listener.Accept()
	if err != nil {
		return transcript
	}
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	_, _ = conn.Write([]byte("220 fake ESMTP\r\n"))

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return transcript
		}

		transcript = append(transcript, strings.TrimSpace(line))
		_, _ = conn.Write([]byte("421 4.7.0 Greeting rejected\r\n"))

		// A half-close ends the session with FIN while the server keeps reading, so the client's
		// HELO retry reads EOF. Closing with that retry unread resets the connection instead.
		if disconnect {
			_ = conn.(*net.TCPConn).CloseWrite()
			_, _ = io.Copy(io.Discard, reader)

			return transcript
		}
	}
}
