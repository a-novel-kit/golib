package smtp

import (
	"net/mail"
	"strings"
	"text/template"

	"github.com/samber/lo"
)

// MailUser is a single mail participant, pairing a display name with its
// email address.
type MailUser struct {
	Name  string
	Email string
}

// String renders the participant as an RFC 5322 §3.4 mailbox, such as `"Name" <email>`, or
// `<email>` without a name. The display name is quoted, or encoded as RFC 2047 prescribes when it
// holds a character outside printable ASCII, so no name can split or end the header it sits in.
func (mailUser MailUser) String() string {
	return (&mail.Address{Name: mailUser.Name, Address: mailUser.Email}).String()
}

// MailUsers is a list of recipients.
type MailUsers []MailUser

// String renders every recipient as a comma-separated address list, suitable
// for a message header.
func (mailUsers MailUsers) String() string {
	var list strings.Builder

	for i, mailUser := range mailUsers {
		if i > 0 {
			list.WriteString(", ")
		}

		list.WriteString(mailUser.String())
	}

	return list.String()
}

// Emails returns just the email addresses, dropping the display names.
func (mailUsers MailUsers) Emails() []string {
	return lo.Map(mailUsers, func(item MailUser, _ int) string {
		return item.Email
	})
}

// Sender delivers a rendered mail template to a set of recipients. The delivery
// backend swaps per environment: a real SMTP server in production, a writer in
// local development, an in-memory recorder in tests.
type Sender interface {
	// SendMail renders template tName from t with data and delivers the result
	// to the given recipients.
	SendMail(to MailUsers, t *template.Template, tName string, data any) error
	// Ping reports whether the sender can reach its backend, for use as a
	// readiness check.
	Ping() error
}
