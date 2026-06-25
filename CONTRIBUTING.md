# Contributing to golib

Platform setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md). This file covers what's specific to `golib`.

## The bar for additions

`golib` stays small on purpose. Weigh any addition against three questions:

- Does a well-maintained library already do it? Use that library; don't wrap it here.
- Does only one service need it? Keep it there until a second one does.
- Has it grown a broad API of its own? Graduate it to its own repo, like [`jwt`](https://github.com/a-novel-kit/jwt) did.

The sweet spot: a small, dependency-light helper several services share that nothing upstream covers cleanly.

## Questions?

[Open an issue](https://github.com/a-novel-kit/golib/issues) — include logs and environment details.
