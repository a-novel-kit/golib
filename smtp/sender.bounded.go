package smtp

import (
	"text/template"
)

// BoundedSender is a [Sender] that caps concurrent deliveries; sends past the cap wait for a
// free slot.
//
// ProdSender holds one connection per delivery, up to its full timeout, so concurrent callers
// pile up connections during a burst or a server stall. The cap bounds the connections; callers
// queue.
type BoundedSender struct {
	sender Sender
	slots  chan struct{}
}

// NewBoundedSender wraps sender so at most limit deliveries run concurrently. A limit below one
// is raised to one.
func NewBoundedSender(sender Sender, limit int) *BoundedSender {
	return &BoundedSender{sender: sender, slots: make(chan struct{}, max(limit, 1))}
}

// SendMail waits for a delivery slot, then delegates to the wrapped sender.
func (bounded *BoundedSender) SendMail(to MailUsers, t *template.Template, tName string, data any) error {
	bounded.slots <- struct{}{}
	defer func() { <-bounded.slots }()

	return bounded.sender.SendMail(to, t, tName, data)
}

// Ping delegates to the wrapped sender without taking a delivery slot, so readiness answers
// while every slot is busy.
func (bounded *BoundedSender) Ping() error {
	return bounded.sender.Ping()
}
