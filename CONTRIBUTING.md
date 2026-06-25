# Contributing to golib

For platform-wide setup (Go, Node, Podman, the `a-novel` CLI) and the day-to-day commands, see the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md). This file documents what is specific to `golib`.

## The bar for additions

`golib` stays small on purpose — it holds only the glue two or more services would otherwise copy between repos. Weigh any addition against three questions:

- Does a well-maintained library already do it? Use that library; don't wrap it here.
- Does only one service need it? Keep it there until a second one does.
- Has it grown a broad public API of its own? Graduate it into its own repo, like [`jwt`](https://github.com/a-novel-kit/jwt) did.

The sweet spot is a small, dependency-light helper that several services share and nothing upstream covers cleanly.

## Questions?

- Open an issue at https://github.com/a-novel-kit/golib/issues
- Check existing issues for similar problems
- Include relevant logs and environment details
