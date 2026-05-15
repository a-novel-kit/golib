package smtp

import (
	"errors"
	"sync"
	"text/template"
)

// ErrPingTestSender is returned by TestSender.Ping.
//
// Deprecated: use smtptest.ErrPing in the github.com/a-novel-kit/golib/smtp/smtptest
// sub-package. The legacy symbols in this file ship in every consumer binary
// because the `.test.go` filename suffix is not recognised by `go build` —
// only `_test.go` is excluded from production builds. The smtptest package
// makes the test-only boundary explicit.
var ErrPingTestSender = errors.New("pinging test sender: make sure this is not a misconfiguration")

// TestMail is the captured form of a single SendMail call.
//
// Deprecated: use smtptest.Mail instead.
type TestMail struct {
	To   []string
	Data any
}

// TestSender is an in-memory smtp.Sender that records every SendMail call.
//
// Deprecated: use smtptest.Sender instead. See ErrPingTestSender for the
// rationale.
type TestSender struct {
	mails []*TestMail
	mu    sync.RWMutex
}

// NewTestSender returns an empty TestSender.
//
// Deprecated: use smtptest.NewSender instead.
func NewTestSender() *TestSender {
	return &TestSender{}
}

func (sender *TestSender) SendMail(to MailUsers, _ *template.Template, _ string, data any) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()

	sender.mails = append(sender.mails, &TestMail{
		To:   to.Emails(),
		Data: data,
	})

	return nil
}

func (sender *TestSender) Ping() error {
	return ErrPingTestSender
}

func (sender *TestSender) FindTestMail(cmp func(*TestMail) bool) (*TestMail, bool) {
	sender.mu.RLock()
	defer sender.mu.RUnlock()

	for _, mail := range sender.mails {
		if cmp(mail) {
			return mail, true
		}
	}

	return nil, false
}
