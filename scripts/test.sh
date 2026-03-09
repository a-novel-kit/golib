#!/bin/bash

# shellcheck disable=SC2046
go tool -modfile=gotestsum.mod gotestsum --format pkgname -- -count=1 -cover $(go list ./... | grep -v /mocks)
