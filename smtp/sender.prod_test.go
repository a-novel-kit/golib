package smtp_test

import (
	"bufio"
	"cmp"
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

// These tests cover ProdSender's own SMTP exchange. The handshake happens in the right order, a
// server that goes quiet at any step releases the caller on that step's timeout, and every failure
// carries its class.
//
// The fake server is a real listener speaking enough SMTP to complete a transaction, so the client
// under test is the real net/smtp client on a real socket.

// Scripted replies of a fakeServer.
const (
	// replyStall makes the server stop reading and writing, so only a deadline on the connection ends
	// the client's wait.
	replyStall = "stall"
	// replyClose makes the server end the session without a reply.
	replyClose = "close"
)

// fakeServer is a minimal SMTP server for one connection.
type fakeServer struct {
	listener net.Listener
	// replies overrides the server's reply at a step: "greeting", a command verb such as "MAIL",
	// "body" once the message starts, or "." for the end of the message. A reply is a raw SMTP line,
	// replyStall or replyClose.
	replies map[string]string
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
func newFakeServer(t *testing.T, replies map[string]string, extensions ...string) *fakeServer {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &fakeServer{
		listener:   listener,
		replies:    replies,
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

	reader := bufio.NewReader(conn)

	if !s.reply(conn, "greeting", "220 fake ESMTP") {
		return
	}

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

				if !s.reply(conn, ".", "250 2.0.0 Ok") {
					return
				}
			}

			continue
		}

		s.transcript = append(s.transcript, line)

		verb, _, _ := strings.Cut(line, " ")

		var ok bool

		switch verb {
		case "EHLO":
			ok = s.reply(conn, verb, s.ehlo()...)
		case "STARTTLS":
			// The server ends the session instead of handshaking, so the client's handshake reads EOF.
			// A half-close drains the ClientHello, which a full close would answer with a reset.
			s.reply(conn, verb, "220 2.0.0 Ready to start TLS")

			_ = conn.(*net.TCPConn).CloseWrite()
			_, _ = io.Copy(io.Discard, reader)

			return
		case "AUTH":
			ok = s.reply(conn, verb, "235 2.7.0 Accepted")
		case "DATA":
			inData = true
			ok = s.reply(conn, verb, "354 End data with <CR><LF>.<CR><LF>") && s.reply(conn, "body")
		case "QUIT":
			s.reply(conn, verb, "221 2.0.0 Bye")

			return
		default:
			ok = s.reply(conn, verb, "250 2.0.0 Ok")
		}

		if !ok {
			return
		}
	}
}

// reply writes the scripted reply of step, or lines when the script has none. It reports whether the
// session goes on.
func (s *fakeServer) reply(conn net.Conn, step string, lines ...string) bool {
	switch scripted := s.replies[step]; scripted {
	case replyStall:
		<-s.stop

		return false
	case replyClose:
		return false
	case "":
	default:
		lines = []string{scripted}
	}

	for _, line := range lines {
		_, _ = conn.Write([]byte(line + "\r\n"))
	}

	return true
}

// ehlo returns the lines of the EHLO reply, advertising the server's extensions.
func (s *fakeServer) ehlo() []string {
	lines := []string{"250-fake"}
	for _, extension := range s.extensions {
		lines = append(lines, "250-"+extension)
	}

	return append(lines, "250 HELP")
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

			server := newFakeServer(t, nil, "AUTH PLAIN")
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
			server := newFakeServer(t, nil, "AUTH PLAIN")

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

			server := newFakeServer(t, nil, testCase.extensions...)

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

	// A server that goes quiet at one step releases the send on that step's timeout alone. Every other
	// timeout keeps its default of minutes, so a step timed by the wrong field fails the elapsed bound.
	// The connection is accepted, so only the deadline on the connection ends each wait.
	const stepTimeout = 150 * time.Millisecond

	stepCases := []struct {
		name string

		replies  map[string]string
		timeouts smtp.SendTimeouts
		message  string

		expectErr   string
		expectClass error
	}{
		{
			name:        "Greeting",
			replies:     map[string]string{"greeting": replyStall},
			timeouts:    smtp.SendTimeouts{Greeting: stepTimeout},
			expectErr:   "open SMTP client",
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Command",
			replies:     map[string]string{"EHLO": replyStall},
			timeouts:    smtp.SendTimeouts{Command: stepTimeout},
			expectErr:   "greet SMTP server",
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Mail",
			replies:     map[string]string{"MAIL": replyStall},
			timeouts:    smtp.SendTimeouts{Mail: stepTimeout},
			expectErr:   "send MAIL",
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Rcpt",
			replies:     map[string]string{"RCPT": replyStall},
			timeouts:    smtp.SendTimeouts{Rcpt: stepTimeout},
			expectErr:   "send RCPT",
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "DataInit",
			replies:     map[string]string{"DATA": replyStall},
			timeouts:    smtp.SendTimeouts{DataInit: stepTimeout},
			expectErr:   "send DATA",
			expectClass: smtp.ErrTransient,
		},
		{
			// The message outgrows the socket buffers, so the client blocks writing it.
			name:        "DataBlock",
			replies:     map[string]string{"body": replyStall},
			timeouts:    smtp.SendTimeouts{DataBlock: stepTimeout},
			message:     strings.Repeat("a", 32<<20),
			expectErr:   "write message",
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "DataTermination",
			replies:     map[string]string{".": replyStall},
			timeouts:    smtp.SendTimeouts{DataTermination: stepTimeout},
			expectErr:   "end message",
			expectClass: smtp.ErrUncertain,
		},
	}

	for _, testCase := range stepCases {
		t.Run("Error/StepTimeout/"+testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newFakeServer(t, testCase.replies)

			sender := &smtp.ProdSender{
				Addr:                server.addr(),
				Email:               "noreply@example.com",
				ForceUnencryptedTLS: true,
				Timeouts:            testCase.timeouts,
			}

			start := time.Now()

			err := sender.SendMail(smtp.MailUsers{{Email: "to@example.com"}}, testTemplate(t), "mail", testCase.message)
			elapsed := time.Since(start)

			var netErr net.Error

			require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "expected a timeout, got %v", err)
			require.ErrorContains(t, err, testCase.expectErr)
			requireFailureClass(t, err, testCase.expectClass)
			require.GreaterOrEqual(t, elapsed, stepTimeout)
			require.Less(t, elapsed, 5*time.Second, "the send must return on the step's own deadline")
		})
	}

	// The class tells a caller whether to try again: a transient failure may deliver on a later
	// attempt, a permanent one never will, and an uncertain one may already have delivered.
	classCases := []struct {
		name string

		replies    map[string]string
		extensions []string
		tName      string

		expectClass error
	}{
		{
			name:        "Transient/Reply",
			replies:     map[string]string{"MAIL": "451 4.3.0 Try again later"},
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Transient/Disconnect",
			replies:     map[string]string{"RCPT": replyClose},
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Transient/RefusedMessage",
			replies:     map[string]string{".": "452 4.3.1 Insufficient system storage"},
			expectClass: smtp.ErrTransient,
		},
		{
			name:        "Permanent/Reply",
			replies:     map[string]string{"RCPT": "550 5.1.1 No such user"},
			expectClass: smtp.ErrPermanent,
		},
		{
			name:        "Permanent/RefusedMessage",
			replies:     map[string]string{".": "554 5.7.1 Message rejected"},
			expectClass: smtp.ErrPermanent,
		},
		{
			name:        "Permanent/NoStartTLS",
			extensions:  []string{"AUTH PLAIN"},
			expectClass: smtp.ErrPermanent,
		},
		{
			name:        "Permanent/Template",
			tName:       "missing",
			expectClass: smtp.ErrPermanent,
		},
		{
			name:        "Uncertain/Disconnect",
			replies:     map[string]string{".": replyClose},
			expectClass: smtp.ErrUncertain,
		},
		{
			// The reply to the final "." accepted the mail, so the failed QUIT is no failure.
			name:    "Success/QuitDisconnect",
			replies: map[string]string{"QUIT": replyClose},
		},
	}

	for _, testCase := range classCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newFakeServer(t, testCase.replies, testCase.extensions...)

			sender := &smtp.ProdSender{
				Addr:  server.addr(),
				Email: "noreply@example.com",
				// Without extensions, the server offers no STARTTLS, which only this setting allows.
				ForceUnencryptedTLS: testCase.extensions == nil,
			}

			err := sender.SendMail(
				smtp.MailUsers{{Email: "to@example.com"}}, testTemplate(t), cmp.Or(testCase.tName, "mail"), "world",
			)
			if testCase.expectClass == nil {
				require.NoError(t, err)

				return
			}

			requireFailureClass(t, err, testCase.expectClass)
		})
	}

	t.Run("Error/PingTimesOutOnStalledServer", func(t *testing.T) {
		t.Parallel()

		server := newFakeServer(t, map[string]string{"greeting": replyStall})

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

	t.Run("Success/Defaults", func(t *testing.T) {
		t.Parallel()

		// An unset timeout selects its default. Proving it on a dial would cost minutes, so the test
		// holds the defaults to their contract: RFC 5321 §4.5.3.2 for a send.
		require.Positive(t, smtp.DefaultTimeout)
		require.Equal(t, smtp.SendTimeouts{
			Greeting:        5 * time.Minute,
			Command:         5 * time.Minute,
			Mail:            5 * time.Minute,
			Rcpt:            5 * time.Minute,
			DataInit:        2 * time.Minute,
			DataBlock:       3 * time.Minute,
			DataTermination: 10 * time.Minute,
		}, smtp.DefaultSendTimeouts)
	})
}

// requireFailureClass checks that err matches class and neither of the two other failure classes.
func requireFailureClass(t *testing.T, err, class error) {
	t.Helper()

	for _, candidate := range []error{smtp.ErrTransient, smtp.ErrPermanent, smtp.ErrUncertain} {
		require.Equal(t, errors.Is(candidate, class), errors.Is(err, candidate), "%v against %v", err, candidate)
	}
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
