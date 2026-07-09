# Changelog

All notable changes to Comind are recorded here. Entries are generated from
[Conventional Commits](https://www.conventionalcommits.org) by
[git-cliff](https://git-cliff.org); the layout follows
[Keep a Changelog](https://keepachangelog.com).

## [Unreleased]

### CI & Build

- Sync nix devShell to GTK4/WebKit6
- Add Biome frontend linting to the check gate
- Migrate frontend tooling from npm to bun

### Miscellaneous

- Bump Go to 1.26.5 to fix GO-2026-5856 (crypto/tls) (#24)

## [0.0.1] - 2026-07-06

### Features

- Add config loading (TOML file, env overrides, XDG paths)
- Add ULID identifier generator
- Add SQLite store with schema migrations
- Add Anthropic and OpenAI-compatible LLM clients
- Add knowledge domain (service, proposals, repository)
- Add stdio MCP server
- Add Wails bindings and regenerate frontend types
- Wire up main entrypoint (gui/mcp modes, --version)

### Bug Fixes

- Restore frontend/dist/.gitkeep so go:embed resolves on clean checkout

### Documentation

- Add agent guide (AGENTS.md, CLAUDE.md)

### CI & Build

- Stamp version from git tag via ldflags
- Add tag-driven macOS release workflow
- Install GTK4/WebKit6 dev libraries to match wails v3 default

### Miscellaneous

- Set up linting, CI, and task runner
- Initialize Go module
- Add Wails v3 + Svelte shell scaffolding
- Add nix dev shell (flake + direnv)


