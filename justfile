# show available recipes
default:
    @just --list --unsorted

# run the app in development mode (hot reload; needs the wails3 CLI)
dev:
    wails3 task dev

# build the desktop application into bin/
app:
    wails3 task build

# build all packages
build:
    go build ./...

# run all tests with the race detector
test:
    go test -race ./...

# static analysis + formatting check
lint:
    golangci-lint run ./...

# apply formatting
fmt:
    golangci-lint fmt

# everything CI checks
check: lint test
    go mod tidy -diff

# per-package coverage (atomic mode: the only one correct under -race)
cover:
    go test -race -covermode=atomic -coverprofile=cover.out ./...
    go tool cover -func=cover.out | tail -1

# line-by-line coverage in the browser
cover-html: cover
    go tool cover -html=cover.out

# scan dependencies for known vulnerabilities
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
