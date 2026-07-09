# show available recipes
default:
    @just --list --unsorted

# run the app in development mode (hot reload; needs the wails3 CLI)
dev:
    wails3 task dev

# build the desktop application into bin/
app:
    wails3 task build VERSION=$(git describe --tags --always --dirty)

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

# fail on non-Latin letters in tracked files (Cyrillic, CJK, Arabic, …), keeping the
# repo English-only. Typographic symbols (— → ↔ …) and accented Latin are allowed, so
# this is a language guard, not an ASCII guard; generated/template files pass naturally.
english-only:
    #!/usr/bin/env bash
    set -euo pipefail
    if git grep -I -n -P '(?=\p{L})[^\p{Latin}\x00-\x7F]' --; then
        echo "error: non-Latin letters in tracked files (repo is English-only; see above)" >&2
        exit 1
    fi

# lint + format-check the frontend
web-lint:
    cd frontend && bun run lint

# apply frontend formatting and safe fixes
web-fmt:
    cd frontend && bun run lint:fix

# everything CI checks
check: lint web-lint english-only test
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
