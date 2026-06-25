# Go lib

Shared Go library for the A-Novel backend services — the glue they'd otherwise copy between repos.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/golib)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/golib)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/golib)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/golib/main.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/a-novel-kit/golib)](https://goreportcard.com/report/github.com/a-novel-kit/golib)

## What this is

Not a framework, and small on purpose: if a well-maintained library already covers it, it doesn't live here. A sub-package that grows a broad API of its own graduates to its own repo, as [`jwt`](https://github.com/a-novel-kit/jwt) did.

godoc on [pkg.go.dev](https://pkg.go.dev/github.com/a-novel-kit/golib) is the full reference.

## Installation

```bash
go get github.com/a-novel-kit/golib
```

## Sub-packages

| Path       | What it covers                                                                          |
| ---------- | --------------------------------------------------------------------------------------- |
| `config`   | Typed env-var loading (`LoadEnv[T]`) and panic-on-error startup helpers.                |
| `otel`     | OpenTelemetry tracer/logger access and span error reporting; local and GCP presets.     |
| `httpf`    | REST boundary: map error sentinels to HTTP statuses, send JSON.                         |
| `grpcf`    | gRPC boundary: context interceptors, local/GCP credentials, a demo echo/health service. |
| `logging`  | Shared logging interfaces, with local and GCP presets for HTTP and gRPC.                |
| `postgres` | Context-scoped `bun` DB, transactions, a migrations runner, and test harnesses.         |
| `smtp`     | An SMTP `Sender` (prod and debug) and an in-memory test helper.                         |

## Contributing

Setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md); golib-specific notes are in [CONTRIBUTING.md](./CONTRIBUTING.md).
