// Package smtptest provides an in-memory smtp.Sender for tests. A test substitutes
// *smtptest.Sender wherever an smtp.Sender is expected, then asserts on what was sent.
package smtptest
