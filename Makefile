# Run tests.
test:
	bash -c "set -m; bash '$(CURDIR)/scripts/test.sh'"

# Check code quality.
lint:
	go tool -modfile=golangci-lint.mod golangci-lint run
	go tool buf lint
	pnpm lint

# Generate Go code.
generate-go:
	go generate ./...

generate: generate-go

# Reformat code so it passes the code style lint checks.
format:
	go mod tidy
	go mod tidy -modfile=golangci-lint.mod
	go mod tidy -modfile=gotestsum.mod
	go tool -modfile=golangci-lint.mod golangci-lint run --fix
	go tool buf format -w
	go tool buf dep update
	pnpm format
