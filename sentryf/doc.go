// Package sentryf reports a service's server faults to Sentry, one error event per fault. golib's
// servers and its panic recovery capture each fault where it surfaces, so a service only installs
// the reporter at boot and flushes it at exit.
//
// An event names the failed operation, the type of its innermost error and the trace that holds the
// rest. It carries no error message and nothing about the visitor, since either can hold caller
// input.
//
// The reporter also starts a sampler, which sends a fixed set of metrics on the process every minute:
// the memory and CPU use of its container, its Go runtime, and its database connection pools. Tracing
// and logs stay with OpenTelemetry.
package sentryf
