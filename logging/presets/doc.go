// Package loggingpresets implements the logging contracts for the two environments a service runs
// in. The OTel presets log through the installed OpenTelemetry backend, where each entry links to its
// trace, and leave each request's record to its server span. The Local presets write colored lines
// for a developer's terminal, with a base logger plus HTTP and gRPC middleware built on it.
package loggingpresets
