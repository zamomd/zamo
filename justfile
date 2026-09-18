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
# Tests and fixtures are exempt: the base holds knowledge in any script, and that is
# checked with real characters, not escapes.
english-only:
    #!/usr/bin/env bash
    set -euo pipefail
    if git grep -I -n -P '(?=\p{L})[^\p{Latin}\x00-\x7F]' -- ':!*_test.go' ':!*.test.ts' ':!**/testdata/**'; then
        echo "error: non-Latin letters in tracked files (repo is English-only; see above)" >&2
        exit 1
    fi

# lint + format-check the frontend
web-lint:
    cd frontend && bun run lint

# type-check the frontend, .svelte files included
web-check:
    cd frontend && bun run check

# run the frontend unit tests
web-test:
    cd frontend && bun run test

# production-build the frontend into a throwaway directory, so a broken bundle
# surfaces before a release; frontend/dist and its .gitkeep stay untouched
web-build:
    cd frontend && bun run build --outDir "$(mktemp -d)"

# apply frontend formatting and safe fixes
web-fmt:
    cd frontend && bun run lint:fix

# regenerate the frontend bindings after changing a bound Go service
bindings:
    wails3 generate bindings -f '' -clean=true -ts -i

# fail when the tracked bindings differ from what the Go services generate.
# Generates into a throwaway directory, so uncommitted regenerated bindings pass.
bindings-check:
    #!/usr/bin/env bash
    set -euo pipefail
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    wails3 generate bindings -f '' -clean=true -ts -i -silent -d "$tmp"
    if ! diff -r "$tmp" frontend/bindings; then
        echo "error: frontend/bindings is stale; run 'just bindings'" >&2
        exit 1
    fi

# regenerate CHANGELOG.md from Conventional Commits (git-cliff comes from nix develop)
changelog:
    git-cliff -o CHANGELOG.md

# everything CI checks; CI runs the three parts below as parallel jobs
check: check-static check-web test

# checks that read the repo without running it
check-static: lint english-only bindings-check
    go mod tidy -diff

# every frontend check
check-web: web-lint web-check web-test web-build

# per-package coverage (atomic mode: the only one correct under -race)
cover:
    go test -race -covermode=atomic -coverprofile=cover.out ./...
    go tool cover -func=cover.out | tail -1

# line-by-line coverage in the browser
cover-html: cover
    go tool cover -html=cover.out

# scan dependencies for known vulnerabilities
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

# mutation testing (config in .gremlins.yaml); local only, not part of `check`.
# install: go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0
# The test cache is cleared first: gremlins derives the per-mutant timeout from
# the coverage run, and a cached run makes nearly every mutant time out.
# The whole backend takes ~5 min; pass a package dir to narrow the run.
mutation package='.':
    go clean -testcache
    gremlins unleash --timeout-coefficient 8 {{package}}
