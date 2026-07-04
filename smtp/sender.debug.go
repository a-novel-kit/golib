package smtp

import (
	"fmt"
	"io"
	"os"
	"text/template"
)

// DebugSender is a Sender that renders the mail template to an io.Writer
// instead of delivering it, for local development where no SMTP server is
// available. Recipients are ignored and Ping always succeeds.
type DebugSender struct {
	writer io.Writer
}

// NewDebugSender returns a DebugSender writing to writer, defaulting to
// os.Stdout when writer is nil.
func NewDebugSender(writer io.Writer) *DebugSender {
	if writer == nil {
		writer = os.Stdout
	}

	return &DebugSender{writer: writer}
}

func (sender *DebugSender) SendMail(_ MailUsers, t *template.Template, tName string, data any) error {
	err := t.ExecuteTemplate(sender.writer, tName, data)
	if err != nil {
		return fmt.Errorf("execute template err: %w", err)
	}

	return nil
}

func (sender *DebugSender) Ping() error {
	return nil
}
