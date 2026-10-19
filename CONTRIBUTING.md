# Contributing to golib

Set up your machine with the [developer guide](https://a-novel-kit.github.io/stack/), and read the
[README](./README.md) first. The
[taxonomy](https://github.com/a-novel-kit/.github/blob/master/docs/taxonomy.md#libraries) defines
what a library is and when one graduates. This file covers what is specific to `golib`.

## The bar for additions

`golib` stays small. Weigh any addition against three questions.

- Does a well-maintained library already do it? Use that library directly, without a wrapper here.
- Does only one service need it? Keep it in that service until a second one does.
- Has it grown a broad API of its own? It then
  [graduates](https://github.com/a-novel-kit/.github/blob/master/docs/taxonomy.md#libraries) to its
  own repository, as [`jwt`](https://github.com/a-novel-kit/jwt) did.

A good addition is a small, dependency-light helper that several services share and nothing
upstream covers.

## Breaking changes

Services pin a released version, so every exported identifier, signature, error value and default
is a contract. A release that asks anything of its consumers ships a migration guide in
[`docs/migrations`](./docs/migrations), named after the version.

## Run the PostgreSQL tests

The `postgres` packages wrap a real database, so their tests need one. They create and drop
databases and schemas on it, so point them at a throwaway server.

```bash
POSTGRES_PASSWORD="postgres"
podman run -d --name golib-postgres -e POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
  -p 5432:5432 docker.io/library/postgres:18
export POSTGRES_DSN="postgres://postgres:$POSTGRES_PASSWORD@localhost:5432/postgres?sslmode=disable"
a-novel test --type=go -y
```

Remove the server once done with `podman rm -f golib-postgres`.

## Questions?

[Open an issue](https://github.com/a-novel-kit/golib/issues) with logs and environment details.
