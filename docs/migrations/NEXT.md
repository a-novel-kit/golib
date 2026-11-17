# Upgrading to NEXT

golib takes over the boilerplate every service copied: the service token gate, the ping and health
handlers, the telemetry and Postgres presets read from the environment, the gRPC client
configuration, the job root span and the SQLSTATE reader. It also removes the exports no repository
uses, and drops lipgloss and samber/lo from its dependencies.

## Do I need to act?

**No, to take the bump.** Every service compiles and runs on it as it is: nothing removed has a
consumer in either organization, and every changed signature keeps its old form. The old forms carry
no `Deprecated:` paragraph, so staticcheck stays green on the bump. Each section below is a
recommended move that deletes a service's own copy of the same code; a later release removes an old
form once no consumer uses it. Check a
repository from its root for a removed name:

```bash
grep -rnE 'GetTraceProvider|GetLogger|GetPropagators|HTTPHandler\(\)|RPCInterceptor\(\)|httpftest|smtptest|BoundedSender|ServiceTokenTransport|RunIsolatedTransactionalTest|NewContextTest|NewContextSchema|DBSchema|DropSchema|MethodCallLimit|MaxConcurrentCalls\(|config\.(Int(8|16|32)?|Uint(8|16|32|64)?|Float(32|64)|Bool|Duration|Time|Enum)Parser|httpf\.(NewServer|Serve|NewPoolTransport)\(|postgres\.(PingTimeout|PingRetryInterval|HealthTimeout)|smtp\.DefaultTimeout|loggingpresets\.LogLevel' --include='*.go' .
```

No output means nothing to do. Run `go mod tidy` after the bump: it drops lipgloss and the 13
modules it brought, unless the service imports them.

## Build the router with `NewMux` and the gRPC server with `NewGRPCServer`

`otel.Config` shrinks to `Init` and `Flush`. The presets install their providers in `Init`, and
`httpf` and `grpcf` trace through otelhttp and otelgrpc under the global providers. The router
leaves its liveness path untraced under every preset. `httpf.NewMux` and `grpcf.NewGRPCServer` take
no tracing argument. Prefer them to `httpf.NewRouter` and `grpcf.NewServer`, which ignore their
tracing argument; a later release removes the old two once no consumer calls them. Leave
`otelpresets.OTLP.UntracedPaths` unset: it does nothing.

```go
// before
router := httpf.NewRouter(cfg.Rest, cfg.Otel, cfg.RestLogger, cfg.App.DowntimeStart)
server := grpcf.NewServer(ctx, cfg.Otel, cfg.GrpcLogger, cfg.App.DowntimeStart, ctxInterceptor, serviceToken...)
&otelpresets.OTLP{UntracedPaths: []string{env.Rest.Liveness}, FlushTimeout: flushTimeout}

// after
router := httpf.NewMux(cfg.Rest, cfg.RestLogger, cfg.App.DowntimeStart)
server := grpcf.NewGRPCServer(ctx, cfg.GrpcLogger, cfg.App.DowntimeStart, nil, serviceToken...)
&otelpresets.OTLP{FlushTimeout: flushTimeout}
```

`otel.Init(cfg.Otel)` and `defer cfg.Otel.Flush()` stay as they are. A nil `baseContext` now gives
each gRPC call the values of the process context, so the closure that called
`postgres.TransferContext` goes; prefer that to `TransferContext`. A value only one service
transfers, such as json-keys' master key, rides the process context the same way.

## Pick the telemetry backend with `otelpresets.LoadTelemetry`

`LoadTelemetry` holds the switch each `app.config.default.go` wrote: `OTEL` off is `Disabled`, no
`OTEL_EXPORTER_OTLP_ENDPOINT` is `Local`, both is `OTLP`, with the loggers that match. Delete
`newOtelPreset`, `exportsOTLP`, `LoggerDev`, `LoggerDevGrpc`, `LoggerProd`, `OtelFlushTimeout`,
`OtelJobFlushTimeout`, the `Otel` and `OTLPEndpoint` variables of `env.go`, and the
`if env.OTLPEndpoint == "" { log.SetFlags(...) }` line of each main: `Disabled` and `Local` drop the
time from log lines themselves.

```go
// env.go
Telemetry = otelpresets.LoadTelemetry(getEnv, otelpresets.ServerFlushTimeout)

// app.config.default.go
Otel:       env.Telemetry.Otel,
Logger:     env.Telemetry.Logger,
RestLogger: env.Telemetry.HTTPLogger,
GrpcLogger: env.Telemetry.RPCLogger,
```

A job loads its own with `otelpresets.JobFlushTimeout`.

## Gate service tokens with `ServiceTokenGate`

`servicetoken.LoadConfig` reads `SERVICE_TOKEN_ISSUER`, `SERVICE_TOKEN_AUDIENCE`,
`SERVICE_TOKEN_CALLERS` and `SERVICE_ISSUER_OPTIONAL` into one `servicetoken.Config`, whose new
`IssuerOptional` field replaces the separate `ServiceIssuerOptional` setting.
`httpf.ServiceTokenGate` and `grpcf.ServiceTokenGate` always fail closed: without an issuer they
return `servicetoken.ErrIssuerRequired`, unless `IssuerOptional` is set. Delete the local
`RestServiceToken` or `GrpcServiceToken` wrapper, its test, and the `log.Fatalln` guard of the main:

```go
// env.go
ServiceToken = servicetoken.LoadConfig(getEnv)

// main.go, REST
withServiceToken := lo.Must(httpf.ServiceTokenGate(
	cfg.ServiceToken, httpf.NewPoolClient(httpf.PoolOptions{}), cfg.Logger, cfg.Rest.Liveness,
))

// main.go, gRPC
serviceToken := lo.Must(grpcf.ServiceTokenGate(cfg.ServiceToken, httpf.NewPoolClient(httpf.PoolOptions{})))
```

A service whose gate failed open, as the template's did, now refuses to start without an issuer.
Set `SERVICE_ISSUER_OPTIONAL=true` in its local and test stacks. Prefer the gates to
`httpf.ServiceTokenMiddleware` and the two `grpcf` interceptors.

## Serve ping and health from `httpf`

`httpf.Ping` answers "pong". `httpf.Health` runs named probes at once and answers the same body as
the local handlers, `{"client:postgres": {"status": "up"}}`, with 503 when a probe fails:

```go
api.Get("/ping", httpf.Ping)
api.Get("/healthcheck", httpf.Health(map[string]func(context.Context) error{
	"client:postgres": postgres.Health,
	"api:jsonKeys":    jsonKeysStatus,
}))
```

Delete `rest.ping.go`, `rest.health.go` and their tests, and keep only the probe functions a service
owns. A probe sets attributes on the span of its context, as auth's SMTP probe does. Its error is
exported on that span, so it returns one safe to export: auth's SMTP probe keeps returning
`errSmtpUnhealthy`, never the error of `Ping`, whose reply text can carry a credential. Each probe's
span is named `rest.Health(<name>)`, such as `rest.Health(client:postgres)`, and the ping opens no
span of its own.

A gRPC main replaces `RegisterEchoServers`, the `SERVING` status and the goroutine that flips it with
one call, which reports `NOT_SERVING` once ctx is done:

```go
grpcf.RegisterHealth(ctx, server)
```

## Dial dependencies with `grpcf.LoadClientConfig`

`LoadClientConfig` reads `<PREFIX>_HOST`, `_PORT`, `_TLS` and `_TOKEN_PATH`. `Address` and
`DialOptions` replace `net.JoinHostPort`, the `newCredentials` selector and `lo.Must(....Options(ctx))`.
Prefer it to `CredentialsProvider` and its two implementations:

```go
// env.go
ServiceJsonKeys = grpcf.LoadClientConfig(getEnv, "SERVICE_JSON_KEYS", grpcf.ClientConfig{Host: "localhost", Port: 8080})

// main.go
jsonKeysClient := lo.Must(servicejsonkeys.NewClient(env.ServiceJsonKeys.Address(), env.ServiceJsonKeys.DialOptions()...))
```

The token path is now `SERVICE_JSON_KEYS_TOKEN_PATH` in every service. To read authentication's old
`SERVICE_TOKEN_PATH` for one more release, pass it as the default:
`grpcf.ClientConfig{Host: "localhost", Port: 8080, TokenPath: getEnv("SERVICE_TOKEN_PATH")}`.

## Delete the 6-second drain overrides

The REST and gRPC drains default to 6 seconds instead of 30, the value every service but
authentication set. Delete `TimeoutShutdownDefault`, `restDefaults` and `grpcDefaults` where they
only set it. Authentication keeps its 25-second override.

## Load Postgres with `postgrespresets.LoadConnection`

`LoadConnection` reads the discrete `POSTGRES_*` variables, pool bounds included, and
`ConnectionDefault` holds the fleet's values: port 5432, TLS on, 20 open and 20 idle connections.
`Connection.Preset` replaces each service's `NewPostgresPreset`. Delete the `POSTGRES_*` block and
its four default constants of `env.go`, `postgres.config.default.go`'s helper and its test:

```go
// env.go
Postgres = postgrespresets.LoadConnection(getEnv, postgrespresets.ConnectionDefault)

// postgres.config.default.go
var PostgresPresetDefault = lo.Must(env.Postgres.Preset())

var PostgresPresetMigrations = lo.Must(env.Postgres.Preset(pgdriver.WithConnParams(map[string]any{"lock_timeout": "3s"})))
```

The loader reads no `POSTGRES_DSN`. Move a service's compose files and CI to the discrete variables
before it adopts the loader. `postgrespresets.MaxIdleConnsDefault` rises from 10 to 20.

## Close the pool with `Close`

`postgres.Config` gains `Close`, which closes the pool once opened and logs a failure. Replace the
local `closeDatabase` and the second `DB` call of each main:

```go
// before
ctx = lo.Must(postgres.NewContext(ctx, cfg.Postgres))
database := lo.Must(cfg.Postgres.DB(ctx))
defer closeDatabase(database)

// after
ctx = lo.Must(postgres.NewContext(ctx, cfg.Postgres))
defer cfg.Postgres.Close()
```

## Read SQLSTATE with `postgres.SQLState`

`SQLState(err)` returns the code and the constraint of the PostgreSQL error `err` wraps. It replaces
`isUniqueViolation`, `isViolation` and the `errors.As` on `pgdriver.Error` in the DAO packages:

```go
code, constraint := postgres.SQLState(err)
```

## Run migrations with `RunMigrations`

`RunMigrations` logs the group it applied, such as `migrations: applied group #4 (...)`, or that
nothing was pending. Delete a main's own listing of the embedded files. `RunMigrationsContext` is
deprecated:

```go
db := lo.Must(config.PostgresPresetMigrations.DB(ctx))
lo.Must0(postgres.RunMigrations(ctx, db, migrations.Migrations))
```

`postgres.ContextKey` is deprecated too. A test puts a transaction on its context with
`postgres.WithTx(ctx, tx)`.

## Open a job's root span with `worker.Run`

`worker.Run(ctx, "job.RotateKeys", fn)` runs `fn` under a root span, and reports a failure there
with its `error.type`, as a job's root span must. It replaces the hand-written `run` of
json-keys' `rotate-keys`, and serves authentication's maintenance operations.

## Remove what golib no longer exports

No repository in either organization uses these, so the release removes them outright:

| Removed                                                                                   | Use instead                                                   |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `httpf/httpftest`, `smtp/smtptest`                                                        | `httptest.Server`; the service's own mockery mock of a sender |
| `smtp.BoundedSender`, `httpf.ServiceTokenTransport`                                       | Nothing: no caller                                            |
| `postgrestest.RunIsolatedTransactionalTest`, `NewContextTest`                             | `postgrestest.RunDBTest`                                      |
| `postgres.Config.DBSchema`, `postgres.NewContextSchema`, `Default.DBSchema`, `DropSchema` | `postgres.Config.DB`                                          |
| `grpcf.MaxConcurrentCalls`                                                                | `grpcf.NewCallLimit(limit)`                                   |
| `grpcf.MethodCallLimit`, `grpcf.CallLimitOption`                                          | Nothing: no caller                                            |
| Every `config` parser except `StringParser`, `Int64Parser` and `SliceParser`              | A `strconv` or `time` function, or a one-line closure         |

`httpf.NewServer`, `httpf.Serve`, `httpf.NewPoolTransport`, the `postgres` ping and health timeouts,
`smtp.DefaultTimeout` and `loggingpresets.LogLevel` become unexported. `httpf.ListenAndServe`
serves a handler; `NewPoolClient` builds the pooled client.

`postgrestest.RunDBTest` and `RunMigrationRoundtripTest` take a `*postgrespresets.Default`, which
every caller already passes. A variable typed `postgres.Config` stops compiling there instead of
failing at test time.

## Prefer `lo.Must` to `config.Must`

`lo.Must` from `github.com/samber/lo` does what `config.Must` does. A later release removes
`config.Must` once no consumer calls it:

```go
// before
RestRateLimitStrictDefault = config.Must(httpf.ParseRate("3/5m"))

// after
RestRateLimitStrictDefault = lo.Must(httpf.ParseRate("3/5m"))
```

## Expect plain ANSI colors in local logs

The Local presets paint with ANSI escape sequences instead of lipgloss, in the same colors.
`LogLocal` lines written to a file or a pipe now keep their escape sequences, as the request lines
and banners already did, and every terminal gets 24-bit colors.
