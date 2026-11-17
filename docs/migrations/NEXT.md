# Upgrading to NEXT

golib removes the exports no repository in either organization uses, and drops lipgloss and
samber/lo from its dependencies.

## Do I need to act?

**No, in every service.** Nothing removed has a consumer in either organization. Check a repository
from its root:

```bash
grep -rnE 'httpftest|smtptest|BoundedSender|ServiceTokenTransport|RunIsolatedTransactionalTest|NewContextTest|NewContextSchema|DBSchema|DropSchema|MethodCallLimit|MaxConcurrentCalls\(|config\.(Int(8|16|32)?|Uint(8|16|32|64)?|Float(32|64)|Bool|Duration|Time|Enum)Parser|httpf\.(NewServer|Serve|NewPoolTransport)\(|postgres\.(PingTimeout|PingRetryInterval|HealthTimeout)|smtp\.DefaultTimeout|loggingpresets\.LogLevel' --include='*.go' .
```

No output means nothing to do. Run `go mod tidy` after the bump: it drops lipgloss and the 13
modules it brought, unless the service imports them.

## Remove what golib no longer exports

No repository in either organization uses these, so the release removes them outright:

| Removed                                                                                   | Use instead                                                   |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `httpf/httpftest`, `smtp/smtptest`                                                        | `httptest.Server`; the service's own mockery mock of a sender |
| `smtp.BoundedSender`, `httpf.ServiceTokenTransport`                                       | Nothing: no caller                                            |
| `postgrestest.RunIsolatedTransactionalTest`, `NewContextTest`                             | `postgrestest.RunDBTest`                                      |
| `postgres.Config.DBSchema`, `postgres.NewContextSchema`, `Default.DBSchema`, `DropSchema` | `postgres.Config.DB`                                          |
| `grpcf.MaxConcurrentCalls`, `grpcf.MethodCallLimit`, `grpcf.CallLimitOption`              | `grpcf.NewCallLimit(limit)`                                   |
| `config.Int8Parser` to `config.TimeParser`, `config.EnumParser`                           | `strconv.Atoi`, `strconv.ParseBool`, `time.ParseDuration`     |

`httpf.NewServer`, `httpf.Serve`, `httpf.NewPoolTransport`, the `postgres` ping and health timeouts,
`smtp.DefaultTimeout` and `loggingpresets.LogLevel` become unexported. `httpf.ListenAndServe`
serves a handler; `NewPoolClient` builds the pooled client.

`postgrestest.RunDBTest` and `RunMigrationRoundtripTest` take a `*postgrespresets.Default`, which
every caller already passes. A variable typed `postgres.Config` stops compiling there instead of
failing at test time.

## Replace `config.Must`

`config.Must` is deprecated. `lo.Must` from `github.com/samber/lo` does the same:

```go
// before
RestRateLimitStrictDefault = config.Must(httpf.ParseRate("3/5m"))

// after
RestRateLimitStrictDefault = lo.Must(httpf.ParseRate("3/5m"))
```

## Expect plain ANSI colors in local logs

The Local presets paint with ANSI escape sequences instead of lipgloss. A terminal shows the same
colors. Output redirected to a file now keeps the escape sequences.
