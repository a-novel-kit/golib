// Package smtptest provides an in-memory smtp.Sender for unit and integration
// tests. Its Sender records every SendMail call instead of reaching a network,
// so a test can substitute *smtptest.Sender wherever an smtp.Sender is expected
// and later assert on what was sent.
package smtptest

import (
	"errors"
	"sync"
	"text/template"

	"github.com/a-novel-kit/golib/smtp"
)

// ErrPing is returned by Sender.Ping. Production code that pings a test
// sender almost always indicates a misconfiguration where the real SMTP
// sender was meant to be wired in.
var ErrPing = errors.New("pinging smtptest sender: make sure this is not a misconfiguration")

// Mail is the captured form of a single SendMail call.
type Mail struct {
	To   []string
	Data any
}

// Sender is an in-memory smtp.Sender that records every SendMail call into
// an internal slice queryable via FindMail. Safe for concurrent use.
type Sender struct {
	mails []*Mail
	mu    sync.RWMutex
}

// NewSender returns an empty Sender ready to capture mails.
func NewSender() *Sender {
	return &Sender{}
}

// SendMail satisfies smtp.Sender by appending a captured Mail entry rather
// than reaching any network.
func (sender *Sender) SendMail(to smtp.MailUsers, _ *template.Template, _ string, data any) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()

	sender.mails = append(sender.mails, &Mail{
		To:   to.Emails(),
		Data: data,
	})

	return nil
}

// Ping always returns ErrPing — production code should never end up calling
// it.
func (sender *Sender) Ping() error {
	return ErrPing
}

// FindMail returns the first captured Mail for which cmp returns true, or
// (nil, false) if none matched.
//
// cmp runs after the lock is released, against a snapshot of the captured
// pointers: this keeps a slow comparator from blocking concurrent SendMail
// callers, and a re-entrant one (that calls back into SendMail) from
// deadlocking. The returned *Mail aliases the captured entry — callers must
// not mutate it.
func (sender *Sender) FindMail(cmp func(*Mail) bool) (*Mail, bool) {
	sender.mu.RLock()
	snapshot := append([]*Mail(nil), sender.mails...)
	sender.mu.RUnlock()

	for _, mail := range snapshot {
		if cmp(mail) {
			return mail, true
		}
	}

	return nil, false
}
