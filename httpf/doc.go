// Package httpf holds the HTTP glue a service would otherwise write twice, on both sides of the
// wire.
//
// Inbound, it writes handler responses that stay consistent with the service's OpenTelemetry
// tracing and structured logging: every outcome is recorded on the request span, so handlers report
// success and failure the same way.
//
// Outbound, it builds the client a service calls other systems through — pooled for the
// concurrency it will actually see, traced end to end, and deliberately without the timeouts that
// break long-running responses. [httpftest] supplies the scripted server those calls are tested
// against.
//
// [httpftest]: https://pkg.go.dev/github.com/a-novel-kit/golib/httpf/httpftest
package httpf
