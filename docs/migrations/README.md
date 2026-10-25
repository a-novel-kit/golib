# Migration guides

A golib release that asks something of its consumers ships a guide here, named after its version.

| Version                 | What changes                                                                                         |
| ----------------------- | ---------------------------------------------------------------------------------------------------- |
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
