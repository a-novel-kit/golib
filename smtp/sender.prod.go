package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"text/template"
	"time"
)

// DefaultTimeout bounds a single delivery when ProdSender leaves Timeout unset.
//
// Bounded rather than unlimited on purpose. net/smtp dials with no deadline, so an SMTP host that
// accepts the connection and then goes quiet — a security-group change, a provider throttling by
// stalling — holds the calling goroutine forever. A caller who must remember to set a timeout is a
// caller who eventually forgets, and the resulting leak reports nothing: the request that spawned
// the send has already returned, so there is no latency signal, no error rate and no failing probe.
// Only a slow climb in memory.
const DefaultTimeout = 30 * time.Second

// ErrNoAuthSupport is returned when the server does not advertise the AUTH extension. Continuing
// would mean either skipping authentication or offering credentials the server never asked for.
var ErrNoAuthSupport = errors.New("SMTP server does not support AUTH")

// ProdSender delivers mail through a real SMTP server using net/smtp. Its
// fields are populated from configuration; the password is never serialized
// back out.
type ProdSender struct {
	Addr   string `json:"addr"   yaml:"addr"`
	Name   string `json:"name"   yaml:"name"`
	Email  string `json:"email"  yaml:"email"`
	Domain string `json:"domain" yaml:"domain"`

	Password string `json:"-" yaml:"-"`

	// ForceUnencryptedTls allows plaintext authentication over a connection the
	// server has not secured with TLS, sending credentials in the clear. Use it
	// only against a local test SMTP server; never enable it in production.
	ForceUnencryptedTls bool `json:"forceUnencryptedTLS" yaml:"forceUnencryptedTLS"`

	// Timeout bounds one delivery end to end. Connect, handshake, authentication and the message
	// body share a single budget, because a stalled server can hold any of those steps.
	// Non-positive selects DefaultTimeout.
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
}

// SendMail renders tName from t with data and delivers it, driving the SMTP exchange directly.
//
// [smtp.SendMail] would be the obvious call, and this is a transcription of what it does — but it
// dials internally with no deadline, so nothing a caller can do bounds the exchange once the
// connection is accepted.
func (sender *ProdSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	writer := bytes.NewBuffer(nil)

	err := t.ExecuteTemplate(writer, tName, data)
	if err != nil {
		return fmt.Errorf("execute template err: %w", err)
	}

	msg := fmt.Sprintf("From: %s <%s>\r\n", sender.Name, sender.Email)
	msg += fmt.Sprintf("To: %s\r\n", to.String())
	msg += writer.String()

	client, host, err := sender.dial()
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	defer func() { _ = client.Close() }()

	err = sender.negotiate(client, host)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	err = client.Mail(sender.Email)
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	for _, addr := range to.Emails() {
		err = client.Rcpt(addr)
		if err != nil {
			return fmt.Errorf("send email: %w", err)
		}
	}

	body, err := client.Data()
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	_, err = body.Write([]byte(msg))
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	// Closing the body is what commits the message, so its error is the server's verdict on the
	// delivery — not the droppable error of a cleanup close.
	err = body.Close()
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	err = client.Quit()
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

// Ping reports whether the SMTP server is reachable and accepts the configured credentials. It
// shares SendMail's timeout: a readiness probe that can hang is one that reports nothing about the
// very outage it exists to detect.
func (sender *ProdSender) Ping() error {
	client, host, err := sender.dial()
	if err != nil {
		return err
	}

	defer func() { _ = client.Close() }()

	err = sender.negotiate(client, host)
	if err != nil {
		return err
	}

	err = client.Quit()
	if err != nil {
		return fmt.Errorf("quit SMTP connection: %w", err)
	}

	return nil
}

func (sender *ProdSender) timeout() time.Duration {
	if sender.Timeout <= 0 {
		return DefaultTimeout
	}

	return sender.Timeout
}

func (sender *ProdSender) auth() smtp.Auth {
	auth := smtp.PlainAuth(sender.Name, sender.Email, sender.Password, sender.Domain)
	if sender.ForceUnencryptedTls {
		return unencryptedAuth{auth}
	}

	return auth
}

// dial connects to the SMTP server with the delivery's deadline already set on the connection.
//
// The deadline is the whole reason for dialing here rather than through [smtp.SendMail] or
// [smtp.Dial]: both dial internally with no deadline, so a caller cannot bound what happens once the
// connection is accepted. net/smtp offers no context and no cancellation, which leaves the
// connection's own deadline as the only mechanism reaching every subsequent read and write.
//
// The dialer takes a background context because [Sender] carries none; the timeout, not the context,
// is what bounds this.
func (sender *ProdSender) dial() (*smtp.Client, string, error) {
	timeout := sender.timeout()

	host, _, err := net.SplitHostPort(sender.Addr)
	if err != nil {
		return nil, "", fmt.Errorf("parse SMTP address: %w", err)
	}

	dialer := &net.Dialer{Timeout: timeout}

	conn, err := dialer.DialContext(context.Background(), "tcp", sender.Addr)
	if err != nil {
		return nil, "", fmt.Errorf("dial SMTP server: %w", err)
	}

	// Measured from here, so the budget covers the whole exchange rather than restarting per step.
	err = conn.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		_ = conn.Close()

		return nil, "", fmt.Errorf("set SMTP deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()

		return nil, "", fmt.Errorf("open SMTP client: %w", err)
	}

	return client, host, nil
}

// negotiate performs the handshake [smtp.SendMail] would have done: upgrade to TLS whenever the
// server offers it, then authenticate, refusing to continue when the server does not advertise AUTH.
//
// The ordering is the security-relevant part and matches the standard library's: STARTTLS is
// attempted before any credential is offered, so an eavesdropper sees the upgrade rather than the
// password.
func (sender *ProdSender) negotiate(client *smtp.Client, host string) error {
	ok, _ := client.Extension("STARTTLS")
	if ok {
		config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

		err := client.StartTLS(config)
		if err != nil {
			return fmt.Errorf("start TLS: %w", err)
		}
	}

	ok, _ = client.Extension("AUTH")
	if !ok {
		return ErrNoAuthSupport
	}

	err := client.Auth(sender.auth())
	if err != nil {
		return fmt.Errorf("authenticate with SMTP server: %w", err)
	}

	return nil
}

// unencryptedAuth wraps an smtp.Auth to report the connection as TLS-secured,
// bypassing net/smtp's refusal to send plaintext credentials over an
// unencrypted link. Reached only through ProdSender.ForceUnencryptedTls.
type unencryptedAuth struct {
	smtp.Auth
}

func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true

	return a.Auth.Start(&s)
}
