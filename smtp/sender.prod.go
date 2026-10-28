package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/textproto"
	"slices"
	"strings"
	"text/template"
	"time"
)

// DefaultTimeout is the network budget of [ProdSender.Ping] when [ProdSender.Timeout] is non-positive.
const DefaultTimeout = 30 * time.Second

// DefaultSendTimeouts holds the per-command timeouts of RFC 5321 §4.5.3.2. The RFC gives Command no
// value, so it takes the one MAIL and RCPT share, as Postfix does.
var DefaultSendTimeouts = SendTimeouts{
	Greeting:        5 * time.Minute,
	Command:         5 * time.Minute,
	Mail:            5 * time.Minute,
	Rcpt:            5 * time.Minute,
	DataInit:        2 * time.Minute,
	DataBlock:       3 * time.Minute,
	DataTermination: 10 * time.Minute,
}

// dataBlockSize is the length of the message written under one [SendTimeouts.DataBlock], the size of
// the client's write buffer.
const dataBlockSize = 4096

// ErrNoAuthSupport is returned when a [ProdSender] with a Password meets a server that does not
// advertise the AUTH extension. Delivery stops there: a configured credential is always used.
var ErrNoAuthSupport = errors.New("SMTP server does not support AUTH")

// ErrNoStartTLSSupport is returned when a [ProdSender] without a Password meets a server that does
// not advertise the STARTTLS extension. Delivery stops there: mail sent without a credential still
// crosses an encrypted connection.
var ErrNoStartTLSSupport = errors.New("SMTP server does not support STARTTLS")

// Every error [ProdSender.SendMail] returns matches exactly one of these classes through errors.Is.
var (
	// ErrTransient marks a send the server did not accept: a 4yz reply, or a network failure before the
	// final "." of the message. A later attempt may deliver the mail.
	ErrTransient = errors.New("transient SMTP failure")
	// ErrPermanent marks a send that fails the same way on every attempt: a 5yz reply, a server that
	// lacks a required extension, or a template that does not render.
	ErrPermanent = errors.New("permanent SMTP failure")
	// ErrUncertain marks a send that got no reply after the final ".". The server may have accepted
	// the mail, so another attempt may deliver it twice.
	ErrUncertain = errors.New("uncertain SMTP delivery")
)

// SendTimeouts bounds each step of a send, as RFC 5321 asks instead of one budget for the whole
// exchange. A slow server then holds a send only while it stays silent within a step. A
// non-positive field takes its value from [DefaultSendTimeouts].
type SendTimeouts struct {
	// Greeting bounds the DNS lookup, the connection and the server's 220 greeting.
	Greeting time.Duration `json:"greeting" yaml:"greeting"`
	// Command bounds each command without a timeout of its own: EHLO, STARTTLS with its TLS
	// handshake, AUTH and QUIT.
	Command time.Duration `json:"command" yaml:"command"`
	// Mail bounds the MAIL command.
	Mail time.Duration `json:"mail" yaml:"mail"`
	// Rcpt bounds each RCPT command.
	Rcpt time.Duration `json:"rcpt" yaml:"rcpt"`
	// DataInit bounds the DATA command, until the server invites the message.
	DataInit time.Duration `json:"dataInit" yaml:"dataInit"`
	// DataBlock bounds each 4 KiB block of the message.
	DataBlock time.Duration `json:"dataBlock" yaml:"dataBlock"`
	// DataTermination bounds the wait for the reply to the final ".", which accepts or refuses the
	// mail.
	DataTermination time.Duration `json:"dataTermination" yaml:"dataTermination"`
}

// ProdSender delivers mail through a real SMTP server using net/smtp.
type ProdSender struct {
	// Addr is the host:port of the SMTP server.
	Addr string `json:"addr" yaml:"addr"`
	// Name is the display name of the From header.
	Name string `json:"name" yaml:"name"`
	// Email is the envelope sender and the address of the From header. Its domain names the client
	// in the greeting, which falls back to localhost for a domainless address.
	Email string `json:"email" yaml:"email"`
	// Username is the SMTP login. Empty falls back to Email. It is unused without a Password.
	Username string `json:"username" yaml:"username"`
	// Domain is the server name authentication is bound to. It must match the host of Addr, since
	// PlainAuth sends credentials to that server only. It is unused without a Password.
	Domain string `json:"domain" yaml:"domain"`

	// Password is the SMTP credential. It is never serialized.
	//
	// An empty Password sends without AUTH, for a relay that admits the client by its IP address.
	// The connection must then upgrade with STARTTLS.
	Password string `json:"-" yaml:"-"`

	// ForceUnencryptedTLS allows a session over a connection the server has not secured with TLS.
	// With a Password, it sends the credentials in the clear. Use it only against a local test SMTP
	// server, never in production.
	ForceUnencryptedTLS bool `json:"forceUnencryptedTLS" yaml:"forceUnencryptedTLS"`

	// Timeout bounds a Ping from DNS resolution through QUIT, so a readiness check answers within it.
	// Non-positive selects [DefaultTimeout]. SendMail ignores it.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
	// Timeouts bounds each step of SendMail. Template rendering happens before the first step.
	Timeouts SendTimeouts `json:"timeouts" yaml:"timeouts"`
}

// SendMail renders tName from t with data and delivers it, driving the SMTP exchange directly. The
// error it returns matches [ErrTransient], [ErrPermanent] or [ErrUncertain].
//
// This transcribes [smtp.SendMail], which dials internally with no deadline. Driving the exchange
// here puts the deadline of each step on the connection.
func (sender *ProdSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	body := bytes.NewBuffer(nil)

	err := t.ExecuteTemplate(body, tName, data)
	if err != nil {
		return classify(fmt.Errorf("execute template err: %w", err), false)
	}

	msg := fmt.Sprintf("From: %s <%s>\r\nTo: %s\r\n%s", sender.Name, sender.Email, to.String(), body.String())

	err = sender.deliver(to.Emails(), msg)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

// Ping reports whether the SMTP server is reachable and completes the handshake of a delivery, TLS
// upgrade and credentials included, within [ProdSender.Timeout].
func (sender *ProdSender) Ping() error {
	timeouts := sender.Timeouts.withDefaults()

	session, err := sender.open(timeouts.Greeting, time.Now().Add(sender.timeout()))
	if err != nil {
		return err
	}

	defer session.close()

	err = sender.negotiate(session, timeouts.Command)
	if err != nil {
		return err
	}

	session.arm(timeouts.Command)

	err = session.client.Quit()
	if err != nil {
		return fmt.Errorf("quit SMTP connection: %w", err)
	}

	return nil
}

// deliver runs one SMTP exchange that sends msg to recipients, and classifies its failure.
func (sender *ProdSender) deliver(recipients []string, msg string) error {
	timeouts := sender.Timeouts.withDefaults()

	session, err := sender.open(timeouts.Greeting, time.Time{})
	if err != nil {
		return classify(err, false)
	}

	defer session.close()

	data, err := sender.transmit(session, timeouts, recipients, msg)
	if err != nil {
		return classify(err, false)
	}

	// The reply to the final "." accepts or refuses the mail. Without one, the server may hold it.
	session.arm(timeouts.DataTermination)

	err = data.Close()
	if err != nil {
		return classify(fmt.Errorf("end message: %w", err), true)
	}

	// The server has accepted the mail, so a failed QUIT changes nothing.
	session.arm(timeouts.Command)

	_ = session.client.Quit()

	return nil
}

// transmit runs every step of the exchange before the final ".": the handshake, the envelope, and
// the message, which leaves the client's buffer before transmit returns.
func (sender *ProdSender) transmit(
	session *session, timeouts SendTimeouts, recipients []string, msg string,
) (io.WriteCloser, error) {
	err := sender.negotiate(session, timeouts.Command)
	if err != nil {
		return nil, err
	}

	session.arm(timeouts.Mail)

	err = session.client.Mail(sender.Email)
	if err != nil {
		return nil, fmt.Errorf("send MAIL: %w", err)
	}

	for _, addr := range recipients {
		session.arm(timeouts.Rcpt)

		err = session.client.Rcpt(addr)
		if err != nil {
			return nil, fmt.Errorf("send RCPT: %w", err)
		}
	}

	session.arm(timeouts.DataInit)

	data, err := session.client.Data()
	if err != nil {
		return nil, fmt.Errorf("send DATA: %w", err)
	}

	for block := range slices.Chunk([]byte(msg), dataBlockSize) {
		session.arm(timeouts.DataBlock)

		_, err = data.Write(block)
		if err != nil {
			return nil, fmt.Errorf("write message: %w", err)
		}
	}

	// A failure while the message is still buffered happens before the final ".", so it is transient.
	session.arm(timeouts.DataBlock)

	err = session.client.Text.W.Flush()
	if err != nil {
		return nil, fmt.Errorf("write message: %w", err)
	}

	return data, nil
}

// timeout returns the network budget of a Ping.
func (sender *ProdSender) timeout() time.Duration {
	if sender.Timeout <= 0 {
		return DefaultTimeout
	}

	return sender.Timeout
}

// auth returns the PLAIN authentication of the configured login.
func (sender *ProdSender) auth() smtp.Auth {
	username := sender.Username
	if username == "" {
		username = sender.Email
	}

	auth := smtp.PlainAuth("", username, sender.Password, sender.Domain)
	if sender.ForceUnencryptedTLS {
		return unencryptedAuth{auth}
	}

	return auth
}

// open connects to the SMTP server and reads its greeting within greeting, and within limit when it
// is set.
//
// net/smtp offers no context and no cancellation, so the connection's own deadline reaches every
// subsequent read and write. The dialer takes a background context because [Sender] carries none,
// and the deadline bounds the dial instead.
func (sender *ProdSender) open(greeting time.Duration, limit time.Time) (*session, error) {
	host, _, err := net.SplitHostPort(sender.Addr)
	if err != nil {
		return nil, fmt.Errorf("parse SMTP address: %w", err)
	}

	session := &session{host: host, limit: limit}
	// The lookup, the connection and the greeting share one deadline.
	deadline := session.deadline(greeting)
	dialer := &net.Dialer{Deadline: deadline}

	session.conn, err = dialer.DialContext(context.Background(), "tcp", sender.Addr)
	if err != nil {
		return nil, fmt.Errorf("dial SMTP server: %w", err)
	}

	// SetDeadline fails only on a closed connection, which NewClient then reports.
	_ = session.conn.SetDeadline(deadline)

	session.client, err = smtp.NewClient(session.conn, host)
	if err != nil {
		_ = session.conn.Close()

		return nil, fmt.Errorf("open SMTP client: %w", err)
	}

	return session, nil
}

// negotiate performs the handshake, each command within timeout: upgrade to TLS whenever the server
// offers it, then authenticate when a Password is configured.
//
// The ordering is security-relevant. STARTTLS is attempted before any credential is offered, so
// credentials only cross an upgraded connection. PlainAuth refuses an unencrypted connection to a
// remote server on its own. Without a Password nothing else refuses one, so negotiate requires the
// upgrade itself.
func (sender *ProdSender) negotiate(session *session, timeout time.Duration) error {
	// Relays may reject localhost, so the greeting names the domain of the sender address.
	localName := "localhost"
	if at := strings.LastIndexByte(sender.Email, '@'); at >= 0 && at < len(sender.Email)-1 {
		localName = sender.Email[at+1:]
	}

	session.arm(timeout)

	// Extension hides greeting errors as absent capabilities, so greet explicitly first.
	err := session.client.Hello(localName)
	if err != nil {
		return fmt.Errorf("greet SMTP server: %w", err)
	}

	encrypted, _ := session.client.Extension("STARTTLS")
	if encrypted {
		config := &tls.Config{ServerName: session.host, MinVersion: tls.VersionTLS12}

		session.arm(timeout)

		err := session.client.StartTLS(config)
		if err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}

	if sender.Password == "" {
		if !encrypted && !sender.ForceUnencryptedTLS {
			return ErrNoStartTLSSupport
		}

		return nil
	}

	ok, _ := session.client.Extension("AUTH")
	if !ok {
		return ErrNoAuthSupport
	}

	session.arm(timeout)

	err = session.client.Auth(sender.auth())
	if err != nil {
		return fmt.Errorf("authenticate with SMTP server: %w", err)
	}

	return nil
}

// withDefaults replaces each non-positive timeout with its default.
func (timeouts SendTimeouts) withDefaults() SendTimeouts {
	return SendTimeouts{
		Greeting:        orDefault(timeouts.Greeting, DefaultSendTimeouts.Greeting),
		Command:         orDefault(timeouts.Command, DefaultSendTimeouts.Command),
		Mail:            orDefault(timeouts.Mail, DefaultSendTimeouts.Mail),
		Rcpt:            orDefault(timeouts.Rcpt, DefaultSendTimeouts.Rcpt),
		DataInit:        orDefault(timeouts.DataInit, DefaultSendTimeouts.DataInit),
		DataBlock:       orDefault(timeouts.DataBlock, DefaultSendTimeouts.DataBlock),
		DataTermination: orDefault(timeouts.DataTermination, DefaultSendTimeouts.DataTermination),
	}
}

func orDefault(timeout, fallback time.Duration) time.Duration {
	if timeout <= 0 {
		return fallback
	}

	return timeout
}

// classify joins err with its failure class. A reply carries its own class. Without a reply, a
// network failure is transient until the final "." is sent and uncertain after it.
func classify(err error, sent bool) error {
	var reply *textproto.Error

	switch {
	case errors.As(err, &reply) && reply.Code/100 == 4:
		return errors.Join(err, ErrTransient)
	case errors.As(err, &reply):
		return errors.Join(err, ErrPermanent)
	case sent:
		return errors.Join(err, ErrUncertain)
	case isNetworkFailure(err):
		return errors.Join(err, ErrTransient)
	default:
		return errors.Join(err, ErrPermanent)
	}
}

// isNetworkFailure reports whether err comes from the network rather than from the configuration or
// the server's capabilities.
func isNetworkFailure(err error) bool {
	var (
		opErr  *net.OpError
		dnsErr *net.DNSError
	)

	return errors.As(err, &opErr) || errors.As(err, &dnsErr) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// session is one SMTP connection, whose deadline each step of the exchange renews.
type session struct {
	client *smtp.Client
	conn   net.Conn
	host   string
	// limit caps every deadline of a Ping. It is zero for a send, whose steps keep their own timeouts.
	limit time.Time
}

// deadline returns the deadline of a step given timeout to complete.
func (session *session) deadline(timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if !session.limit.IsZero() && session.limit.Before(deadline) {
		return session.limit
	}

	return deadline
}

// arm gives the next step timeout to complete. SetDeadline fails only on a closed connection, which
// the step then reports itself.
func (session *session) arm(timeout time.Duration) {
	_ = session.conn.SetDeadline(session.deadline(timeout))
}

func (session *session) close() {
	_ = session.client.Close()
}

// unencryptedAuth wraps an smtp.Auth to report the connection as TLS-secured,
// bypassing net/smtp's refusal to send plaintext credentials over an
// unencrypted link. Reached only through ProdSender.ForceUnencryptedTLS.
type unencryptedAuth struct {
	smtp.Auth
}

// Start reports the connection as TLS-secured to the wrapped Auth.
func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true

	return a.Auth.Start(&s)
}
