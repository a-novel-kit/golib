# Go lib

The minimal shared Go library for A-Novel backend services — the cross-cutting glue (env parsing, OpenTelemetry, Postgres, REST/gRPC helpers) that would otherwise be copied repo to repo.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/golib)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/golib)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/golib)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/golib/main.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/a-novel-kit/golib)](https://goreportcard.com/report/github.com/a-novel-kit/golib)

## What this is

`golib` collects the small amount of cross-cutting glue that the A-Novel backend services would otherwise copy from one repo to the next — env-variable parsing, OpenTelemetry plumbing, Postgres + bun helpers, REST and gRPC boundary utilities, shared logging interfaces, and an SMTP sender. It is **not** a framework and is deliberately kept as small as possible: anything a well-maintained library already covers belongs in that library, not here. When a sub-package outgrows the "boilerplate" bar and earns a broader public API, it graduates into its own repo — the [`jwt`](https://github.com/a-novel-kit/jwt) package is the precedent.

The full API reference lives on [**pkg.go.dev**](https://pkg.go.dev/github.com/a-novel-kit/golib); godoc is canonical and this README only points at it.

## Installation

```bash
go get github.com/a-novel-kit/golib
```

## Sub-packages

| Path       | What it covers                                                                                                                                                               |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `config`   | `LoadEnv[T]` + a set of `strconv`-shaped parsers for environment variables; `Must` / `MustUnmarshal` panic-on-error helpers for one-shot startup wiring.                     |
| `otel`     | `Tracer` / `Logger` accessors keyed on a shared `AppName`, the `ReportError` / `ReportSuccess` span helpers, and a `Config` interface implemented by `otel/presets/*`.       |
| `httpf`    | `HandleError(errMap)` for mapping sentinels onto HTTP status codes (and reporting them on the request span); `SendJSON` for the success path.                                |
| `grpcf`    | `BaseContext*Interceptor` for per-call context shaping, a `CredentialsProvider` interface with local / GCP implementations, and a built-in echo + health-check demo service. |
| `logging`  | The shared `Log` / `HTTPConfig` / `RPCConfig` interfaces; concrete implementations live in `logging/presets/*` (local and GCP variants for both HTTP and gRPC).              |
| `postgres` | `bun.IDB`-on-context plumbing (`NewContext`, `GetContext`, `RunInTx`), the migrations runner, the `PassthroughTx` test wrapper, and `RunTransactionalTest` / -`Isolated`.    |
| `smtp`     | `Sender` interface with `ProdSender` (real SMTP) and `DebugSender` (writes to an `io.Writer`); the in-memory test helper lives in `smtp/smtptest`.                           |

## Contributing

Platform setup and the day-to-day commands live in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md); `golib`-specific notes are in [CONTRIBUTING.md](./CONTRIBUTING.md). The bar for additions is deliberately high — convenience wrappers around well-maintained dependencies, and one-off helpers only one service needs, do not belong here.
