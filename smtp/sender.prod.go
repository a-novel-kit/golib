package smtp

import (
	"bytes"
	"errors"
	"fmt"
	"net/smtp"
	"text/template"
)

type ProdSender struct {
	Addr   string `json:"addr"   yaml:"addr"`
	Name   string `json:"name"   yaml:"name"`
	Email  string `json:"email"  yaml:"email"`
	Domain string `json:"domain" yaml:"domain"`

	Password string `json:"-" yaml:"-"`

	// Dangerous setting, only use it for local development.
	ForceUnencryptedTls bool `json:"forceUnencryptedTLS" yaml:"forceUnencryptedTLS"`
}

func (sender *ProdSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	writer := bytes.NewBuffer(nil)

	err := t.ExecuteTemplate(writer, tName, data)
	if err != nil {
		return fmt.Errorf("execute template err: %w", err)
	}

	auth := smtp.PlainAuth(sender.Name, sender.Email, sender.Password, sender.Domain)
	if sender.ForceUnencryptedTls {
		auth = unencryptedAuth{auth}
	}

	msg := fmt.Sprintf("From: %s <%s>\r\n", sender.Name, sender.Email)
	msg += fmt.Sprintf("To: %s\r\n", to.String())
	msg += writer.String()

	err = smtp.SendMail(sender.Addr, auth, sender.Email, to.Emails(), []byte(msg))
	if err != nil {
		return fmt.Errorf("send email: %w", err)
	}

	return nil
}

func (sender *ProdSender) Ping() error {
	auth := smtp.PlainAuth(sender.Name, sender.Email, sender.Password, sender.Domain)
	if sender.ForceUnencryptedTls {
		auth = unencryptedAuth{auth}
	}

	conn, err := smtp.Dial(sender.Addr)
	if err != nil {
		return fmt.Errorf("dial SMTP server: %w", err)
	}

	defer func() {
		_ = conn.Close()
	}()

	err = conn.Auth(auth)
	if err != nil {
		return errors.Join(fmt.Errorf("authenticate with SMTP server: %w", err), conn.Close())
	}

	err = conn.Quit()
	if err != nil {
		return errors.Join(fmt.Errorf("quit SMTP connection: %w", err), conn.Close())
	}

	return nil
}

type unencryptedAuth struct {
	smtp.Auth
}

func (a unencryptedAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	s := *server
	s.TLS = true

	return a.Auth.Start(&s)
}
