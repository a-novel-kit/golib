# Contributing to golib

The library taxonomy — what a library is, the aggregated-vs-graduated split, and the public-package obligations a graduated package takes on — lives in the [libraries, tooling & platform concepts](https://github.com/a-novel-kit/.github/blob/master/CONTRIBUTING.md); this file covers what's specific to `golib`. Platform setup and day-to-day commands are in the [developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md).

## The bar for additions

`golib` stays small on purpose. Weigh any addition against three questions:

- Does a well-maintained library already do it? Use that library; don't wrap it here.
- Does only one service need it? Keep it there until a second one does.
- Has it grown a broad API of its own? Then it [graduates](https://github.com/a-novel-kit/.github/blob/master/CONTRIBUTING.md#libraries) to its own repo, like [`jwt`](https://github.com/a-novel-kit/jwt) did — taking on the public-package obligations that come with standing alone: its own README/CONTRIBUTING/SECURITY/CODE_OF_CONDUCT, Codecov, and a semver policy it holds to.

The sweet spot: a small, dependency-light helper several services share that nothing upstream covers cleanly.

## Questions?

[Open an issue](https://github.com/a-novel-kit/golib/issues) — include logs and environment details.
