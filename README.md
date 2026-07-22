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

Full reference: godoc on [pkg.go.dev](https://pkg.go.dev/github.com/a-novel-kit/golib).

## Installation

```bash
go get github.com/a-novel-kit/golib
```

## Sub-packages

Each **sub-package** is a directory-scoped, independently importable helper — focused, dependency-light, and shared across services. One that grows a broad API of its own graduates to its own repo (see [What this is](#what-this-is)).

| Path          | What it's for                                                                                                                                                                               |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `config`      | Loads environment variables into typed config structs and fails fast at startup when one is missing or malformed, so a service never boots half-configured.                                 |
| `otel`        | Wires OpenTelemetry tracing and logging under a shared app identity and reports each operation's outcome on its span. Exporters ship for local development and hosted backends.             |
| `httpf`       | Holds the REST boundary logic a handler leans on — mapping domain error sentinels to HTTP status codes and writing JSON — so every service answers errors the same way.                     |
| `grpcf`       | The gRPC equivalent of `httpf`: it shapes per-call context, supplies client dial credentials (local or GCP), and bundles a health/echo service for tests.                                   |
| `logging`     | Defines the interfaces the platform logs through, so a service can swap its log backend without touching call sites. Presets cover local and Google Cloud, for both HTTP and gRPC.          |
| `postgres`    | Carries the database handle on the request context and runs work inside transactions installed on it, and ships a migration runner plus harnesses that give each test an isolated database. |
| `smtp`        | Sends transactional mail behind one `Sender` interface, with a real SMTP sender for production and a debug sender for local runs and tests.                                                 |
| `transaction` | Declares what a unit of work is, in one interface that names no database — so business code can require atomicity without gaining a way to reach a driver. `postgres` implements it.        |

## Contributing

Setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md); golib-specific notes are in [CONTRIBUTING.md](./CONTRIBUTING.md).
