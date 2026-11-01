// Package httpf holds the HTTP glue a service needs on both sides of the wire.
//
// Inbound, it assembles a REST server from its REST_* configuration: the standard middleware
// chain, rate limits, and the server itself, which Serve runs under a bounded shutdown lifecycle.
// Handler responses stay consistent with the service's OpenTelemetry tracing and structured
// logging: a server fault marks the request span failed, and every error is logged once. Its
// service token middleware admits only the services allowed to call, and tells the handlers which
// one called. A route can stream its response as server-sent events, past the request timeout.
//
// Outbound, it builds the pooled, traced client a service calls other systems through, and the
// transport that sends a service token to another service.
package httpf
