package smtptest

import (
	"errors"
	"slices"
	"sync"
	"text/template"

	"github.com/a-novel-kit/golib/smtp"
)

// ErrPing is returned by Sender.Ping. Reaching it means this test sender is
// wired where the real SMTP sender belongs.
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

// SendMail satisfies smtp.Sender by appending a captured Mail entry.
func (sender *Sender) SendMail(to smtp.MailUsers, _ *template.Template, _ string, data any) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()

	sender.mails = append(sender.mails, &Mail{To: to.Emails(), Data: data})

	return nil
}

// Ping always returns ErrPing.
func (sender *Sender) Ping() error {
	return ErrPing
}

// FindMail returns the first captured Mail for which cmp returns true, or (nil, false) when none
// matches.
//
// cmp runs on a snapshot of the captured mails, after the lock is released. A slow cmp then cannot
// block SendMail, and a cmp that calls the Sender cannot deadlock. The returned *Mail is the
// captured entry itself, so do not mutate it.
func (sender *Sender) FindMail(cmp func(*Mail) bool) (*Mail, bool) {
	sender.mu.RLock()
	snapshot := slices.Clone(sender.mails)
	sender.mu.RUnlock()

	for _, mail := range snapshot {
		if cmp(mail) {
			return mail, true
		}
	}

	return nil, false
}
