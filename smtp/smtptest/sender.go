// Package smtptest provides an in-memory implementation of smtp.Sender for use
// in unit and integration tests. Its types satisfy the smtp.Sender interface,
// so a test can substitute *smtptest.Sender wherever an smtp.Sender is
// expected.
//
// This package supersedes the legacy `smtp.TestSender` / `smtp.TestMail` /
// `smtp.NewTestSender` / `smtp.ErrPingTestSender` symbols, which were defined
// in `smtp/sender.test.go`. The dot-suffixed filename was misleading — Go's
// build tooling only excludes files ending in `_test.go` (underscore) from
// production builds, so the legacy symbols ship in every consumer binary
// regardless of whether they are used. Keeping the test helpers in a dedicated
// sub-package makes the boundary explicit.
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
// (nil, false) if no captured Mail matched.
func (sender *Sender) FindMail(cmp func(*Mail) bool) (*Mail, bool) {
	sender.mu.RLock()
	defer sender.mu.RUnlock()

	for _, mail := range sender.mails {
		if cmp(mail) {
			return mail, true
		}
	}

	return nil, false
}
