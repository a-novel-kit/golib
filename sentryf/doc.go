// Package sentryf reports a service's server faults to Sentry, one error event per fault. golib's
// servers and its panic recovery capture each fault where it surfaces, so a service only installs
// the reporter at boot and flushes it at exit.
//
// An event names the failed operation, the type of its innermost error and the trace that holds the
// rest. It carries no error message and nothing about the visitor, since either can hold caller
// input. Tracing, logs and metrics stay with OpenTelemetry.
package sentryf
