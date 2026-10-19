// Package smtp sends templated transactional email through a [Sender]. A Sender renders a
// text/template into the message body and delivers it. The package ships a production sender
// speaking SMTP and a debug sender that writes the rendered body to a writer.
package smtp
