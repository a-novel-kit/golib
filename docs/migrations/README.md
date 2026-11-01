# Migration guides

A golib release that asks something of its consumers ships a guide here, named after its version.

| Version                 | What changes                                                                                         |
| ----------------------- | ---------------------------------------------------------------------------------------------------- |
| [v0.61.0](./v0.61.0.md) | `sentryf` and `grpcf.GcloudCredentialsProvider` are removed.                                         |
| [v0.59.0](./v0.59.0.md) | `httpf.ServiceTokenCaller` returns a request's verified caller, which `Verifier.Verify` now returns. |
| [v0.57.0](./v0.57.0.md) | In-cluster gRPC calls balance and retry, and `NewCallLimit` replaces `MaxConcurrentCalls`.           |
| [v0.56.0](./v0.56.0.md) | The Google Cloud telemetry presets are removed.                                                      |
| [v0.54.0](./v0.54.0.md) | Telemetry exports over OTLP/HTTP, and the Google Cloud presets are deprecated.                       |
| [v0.52.0](./v0.52.0.md) | `sentryf` gives each event 2 minutes to reach Sentry, so a job's flush outlasts it.                  |
| [v0.51.0](./v0.51.0.md) | `sentryf` reports server faults to Sentry, and the unused `otelpresets.Sentry` is removed.           |
| [v0.50.0](./v0.50.0.md) | `otel.ReportSuccess` and `otel.ReportSuccessNoContent` are removed.                                  |
| [v0.49.0](./v0.49.0.md) | `otel/utils` is removed. Capture responses with httpsnoop.                                           |
| [v0.47.0](./v0.47.0.md) | Unused exports are dropped, initialisms take one case, and `httpf.SendJSONStatus` loses its context. |
| [v0.45.2](./v0.45.2.md) | `postgres.RunMigrations` records only migrations that succeed, and runs one runner per schema.       |
| [v0.45.0](./v0.45.0.md) | Servers drain requests in flight on shutdown and end gRPC streams at once.                           |
| [v0.44.0](./v0.44.0.md) | A service's `main` builds and runs each server in one line.                                          |
| [v0.43.0](./v0.43.0.md) | `httpf.Limits.By` takes an `httpf.Rate`.                                                             |
| [v0.38.0](./v0.38.0.md) | Telemetry follows OpenTelemetry's error-recording guidance.                                          |
| [v0.36.0](./v0.36.0.md) | PostgreSQL test helpers move to `postgres/postgrestest`.                                             |
| [v0.28.0](./v0.28.0.md) | `httpf.SendJSON` is removed.                                                                         |
| [v0.26.0](./v0.26.0.md) | `config.LoadEnv` panics on an unparsable value, and `httpf.SendJSON` is deprecated.                  |
| [v0.25.1](./v0.25.1.md) | `postgrespresets.Default` gains `MaxOpenConns`.                                                      |
| [v0.25.0](./v0.25.0.md) | Two preset defaults change.                                                                          |
| [v0.24.0](./v0.24.0.md) | `postgres.RunInTx` is removed.                                                                       |
