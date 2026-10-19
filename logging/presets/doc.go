// Package loggingpresets implements the logging contracts for the two environments a service runs
// in. The Gcloud presets write structured JSON that Cloud Logging parses and links to traces. The
// Local presets write colored lines for a developer's terminal. Each environment offers a base
// logger, plus HTTP and gRPC middleware built on it.
package loggingpresets
