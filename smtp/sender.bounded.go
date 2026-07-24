package smtp

import (
	"text/template"
)

// BoundedSender is a [Sender] that caps concurrent deliveries; excess sends wait for a slot.
// ProdSender holds one connection per delivery, so an unbounded burst piles up connections.
type BoundedSender struct {
	sender Sender
	slots  chan struct{}
}

// NewBoundedSender caps sender at limit concurrent deliveries; a limit below one becomes one.
func NewBoundedSender(sender Sender, limit int) *BoundedSender {
	return &BoundedSender{sender: sender, slots: make(chan struct{}, max(limit, 1))}
}

// SendMail waits for a slot, then delegates.
func (bounded *BoundedSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	bounded.slots <- struct{}{}
	defer func() { <-bounded.slots }()

	return bounded.sender.SendMail(to, t, tName, data)
}

// Ping delegates without taking a slot, so probes never queue behind sends.
func (bounded *BoundedSender) Ping() error {
	return bounded.sender.Ping()
}
