# Contributing to golib

For platform-wide setup (Go, Node, Podman, the `a-novel` CLI) and the day-to-day commands, see the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md). This file documents what is specific to `golib`.

## The bar for additions

`golib` is intentionally minimal — it holds only the cross-cutting glue that the backend services would otherwise copy between repos. Before adding to it, weigh the addition against this bar:

- **A well-maintained library already does it?** Use that library directly; do not wrap it here.
- **Only one service needs it?** Keep it in that service until a second one does.
- **It has grown a broad public API of its own?** It should graduate into its own repo rather than live as a `golib` sub-package — the [`jwt`](https://github.com/a-novel-kit/jwt) package is the precedent.

Good additions are small, dependency-light helpers that at least two services share and that no upstream library covers cleanly.

## Questions?

- Open an issue at https://github.com/a-novel-kit/golib/issues
- Check existing issues for similar problems
- Include relevant logs and environment details
