// Package smtp sends templated transactional email through a pluggable Sender.
// A Sender renders a text/template into the message body and delivers it; the
// package ships a production sender backed by net/smtp and a debug sender that
// writes the rendered body to a writer instead of dialing a server.
package smtp

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/samber/lo"
)

// A MailUser is a single mail participant, pairing a display name with its
// email address.
type MailUser struct {
	Name  string
	Email string
}

// String renders the participant as an RFC 5322 address, "Name <email>".
func (mailUser MailUser) String() string {
	return fmt.Sprintf("%s <%s>", mailUser.Name, mailUser.Email)
}

// MailUsers is a list of recipients.
type MailUsers []MailUser

// String renders every recipient as a comma-separated address list, suitable
// for a message header.
func (mailUsers MailUsers) String() string {
	return strings.Join(lo.Map(mailUsers, func(item MailUser, _ int) string {
		return item.String()
	}), ", ")
}

// Emails returns just the email addresses, dropping the display names.
func (mailUsers MailUsers) Emails() []string {
	return lo.Map(mailUsers, func(item MailUser, _ int) string {
		return item.Email
	})
}

// Sender delivers a rendered mail template to a set of recipients. Callers
// depend on this interface rather than a concrete type so the delivery backend
// can be swapped per environment: a real SMTP server in production, a writer in
// local development, an in-memory recorder in tests.
type Sender interface {
	// SendMail renders template tName from t with data and delivers the result
	// to the given recipients.
	SendMail(to MailUsers, t *template.Template, tName string, data any) error
	// Ping reports whether the sender can reach its backend, for use as a
	// readiness check.
	Ping() error
}
