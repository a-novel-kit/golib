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
	"strings"
	"text/template"
	"time"
)

// DefaultTimeout is the network budget used when [ProdSender.Timeout] is non-positive.
const DefaultTimeout = 30 * time.Second

// ErrNoAuthSupport is returned when a [ProdSender] with a Password meets a server that does not
// advertise the AUTH extension. Delivery stops there: a configured credential is always used.
var ErrNoAuthSupport = errors.New("SMTP server does not support AUTH")

// ErrNoStartTLSSupport is returned when a [ProdSender] without a Password meets a server that does
// not advertise the STARTTLS extension. Delivery stops there: mail sent without a credential still
// crosses an encrypted connection.
var ErrNoStartTLSSupport = errors.New("SMTP server does not support STARTTLS")

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

	// Timeout is the shared network budget from DNS resolution through SMTP QUIT, for both
	// SendMail and Ping. Template rendering happens before this budget starts.
	// Non-positive selects [DefaultTimeout].
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
}

// SendMail renders tName from t with data and delivers it, driving the SMTP exchange directly.
//
// This transcribes [smtp.SendMail], which dials internally with no deadline. Driving the exchange
// here puts the delivery deadline on the connection.
func (sender *ProdSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	body := bytes.NewBuffer(nil)

	err := t.ExecuteTemplate(body, tName, data)
	if err != nil {
		return fmt.Errorf("execute template err: %w", err)
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

// deliver runs one SMTP exchange that sends msg to recipients.
func (sender *ProdSender) deliver(recipients []string, msg string) error {
	client, host, err := sender.dial()
	if err != nil {
		return err
	}

	defer func() { _ = client.Close() }()

	err = sender.negotiate(client, host)
	if err != nil {
		return err
	}

	err = client.Mail(sender.Email)
	if err != nil {
		return err
	}

	for _, addr := range recipients {
		err = client.Rcpt(addr)
		if err != nil {
			return err
		}
	}

	data, err := client.Data()
	if err != nil {
		return err
	}

	_, err = io.WriteString(data, msg)
	if err != nil {
		return err
	}

	// Closing the data writer commits the message, so its error is the server's verdict on the
	// delivery.
	err = data.Close()
	if err != nil {
		return err
	}

	// The server has accepted the message, so a failed QUIT changes nothing.
	_ = client.Quit()

	return nil
}

// timeout returns the network budget of one exchange.
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

// dial connects to the SMTP server with the delivery's deadline already set on the connection.
//
// net/smtp offers no context and no cancellation, so the connection's own deadline reaches every
// subsequent read and write. The dialer takes a background context because [Sender] carries none,
// and the deadline bounds the dial instead.
func (sender *ProdSender) dial() (*smtp.Client, string, error) {
	deadline := time.Now().Add(sender.timeout())

	host, _, err := net.SplitHostPort(sender.Addr)
	if err != nil {
		return nil, "", fmt.Errorf("parse SMTP address: %w", err)
	}

	dialer := &net.Dialer{Deadline: deadline}

	conn, err := dialer.DialContext(context.Background(), "tcp", sender.Addr)
	if err != nil {
		return nil, "", fmt.Errorf("dial SMTP server: %w", err)
	}

	err = conn.SetDeadline(deadline)
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

// negotiate performs the handshake: upgrade to TLS whenever the server offers it, then
// authenticate when a Password is configured.
//
// The ordering is security-relevant. STARTTLS is attempted before any credential is offered, so
// credentials only cross an upgraded connection. PlainAuth refuses an unencrypted connection to a
// remote server on its own. Without a Password nothing else refuses one, so negotiate requires the
// upgrade itself.
func (sender *ProdSender) negotiate(client *smtp.Client, host string) error {
	// Relays may reject localhost, so the greeting names the domain of the sender address.
	localName := "localhost"
	if at := strings.LastIndexByte(sender.Email, '@'); at >= 0 && at < len(sender.Email)-1 {
		localName = sender.Email[at+1:]
	}

	// Extension hides greeting errors as absent capabilities, so greet explicitly first.
	err := client.Hello(localName)
	if err != nil {
		return fmt.Errorf("greet SMTP server: %w", err)
	}

	encrypted, _ := client.Extension("STARTTLS")
	if encrypted {
		config := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

		err := client.StartTLS(config)
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

	ok, _ := client.Extension("AUTH")
	if !ok {
		return ErrNoAuthSupport
	}

	err = client.Auth(sender.auth())
	if err != nil {
		return fmt.Errorf("authenticate with SMTP server: %w", err)
	}

	return nil
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
