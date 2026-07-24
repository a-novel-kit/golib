// Package httpf holds the HTTP glue a service needs on both sides of the wire.
//
// Inbound, it writes handler responses that stay consistent with the service's OpenTelemetry
// tracing and structured logging: every outcome is recorded on the request span. Outbound, it
// builds the pooled, traced client a service calls other systems through.
package httpf
