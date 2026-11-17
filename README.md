# Go lib

Shared Go library of the A-Novel backend services, holding the glue each would otherwise copy.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/golib)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/golib)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/golib)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/golib/main.yaml)

## What this is

golib collects the boilerplate the A-Novel backend services share, from server wiring to test
harnesses, so each service imports it instead of keeping its own copy. It holds no business logic.
Each sub-package covers one concern and imports on its own.

Services depend on a released version. The godoc on
[pkg.go.dev](https://pkg.go.dev/github.com/a-novel-kit/golib) is the full reference.

## Installation

```bash
go get github.com/a-novel-kit/golib
```

## Sub-packages

| Path                            | What it's for                                                                                                                                                                                              |
| ------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/service-token-issuer`      | Serves the `servicetoken/servicetokentest` issuer in a container, so a local stack's services trust each other's tokens.                                                                                   |
| `config`                        | Parses environment variables into typed values. An unset one keeps its default and a malformed one panics at startup, so a service never boots half-configured.                                            |
| `downtime`                      | Makes a service refuse every request but liveness from the start of a planned downtime until operators remove it. A marker in each refusal tells it apart from an outage.                                  |
| `grpcf`                         | The gRPC counterpart of `httpf`: the server with its standard interceptors and bounded shutdown, and the dial credentials of a local target or a service in the same Kubernetes cluster.                   |
| `httpf`                         | The HTTP glue on both sides of the wire: the REST server with its standard middleware and rate limits, the responses a handler writes, and the pooled, traced client a service calls others with.          |
| `logging`                       | Defines the interfaces a service logs through, so it can swap its log backend without touching call sites. Its presets cover a terminal, for both HTTP and gRPC, and any OpenTelemetry backend.            |
| `otel`                          | Wires OpenTelemetry tracing and logging under the service's name, and reports each operation's outcome on its span. Its presets export locally or to a hosted backend.                                     |
| `picture`                       | Checks a picture a user uploads, such as a profile picture or a logo: a still WebP or a JPEG, or a PNG where the contract allows one, of the contract's aspect ratio and size bounds, which decodes whole. |
| `postgres`                      | Carries the database handle on the request context, runs work inside transactions installed on it, and applies migrations. Its presets open the pool from a URL or discrete settings.                      |
| `postgres/postgrestest`         | Isolates the database work of each test, in a migrated clone or a rolled-back transaction, and proves every down migration reverses its up.                                                                |
| `servertls`                     | Serves TLS from a key pair renewed on disk, such as a certificate cert-manager keeps in a mounted Secret. `httpf` and `grpcf` serve TLS through it once their configuration names the files.               |
| `servicetoken`                  | Authenticates calls between services with Kubernetes service-account tokens: a caller reads its projected token, and a callee verifies it against the cluster issuer's cached keys.                        |
| `servicetoken/servicetokentest` | Runs a service-account issuer that signs tokens on request, for tests and local stacks.                                                                                                                    |
| `smtp`                          | Sends transactional mail behind one `Sender` interface, with a real SMTP sender for production and a debug sender for local runs.                                                                          |
| `transaction`                   | Declares a unit of work in one interface that names no database, so business code can require atomicity without reaching a driver. `postgres` implements it.                                               |
| `transaction/transactiontest`   | Runs a unit of work in memory, for tests to assert that a caller opened one.                                                                                                                               |
| `worker`                        | Runs the background loops that live beside the request path, each run traced on its own and a failing or panicking run recorded on its span and logged.                                                    |

## Contributing

Set up your machine with the [developer guide](https://a-novel-kit.github.io/stack/). The rules
specific to golib are in [CONTRIBUTING.md](./CONTRIBUTING.md).
