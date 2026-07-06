# Comind — agent guide

Local-first block-based knowledge base: AI proposes changes, the human
reviews. One Go binary, two modes: `comind` (Wails v3 GUI) and
`comind mcp` (stdio MCP server).
Stack: Go 1.26, Wails v3 alpha, Svelte 5 + TS + Vite, modernc.org/sqlite.

## Commands

Always finish work with the full gate — CI runs exactly this:

```
just check          # lint + race tests + go mod tidy -diff
```

- `just fmt` — format (gofumpt via golangci-lint); run after editing Go code
- `just test` / `just lint` / `just cover` — pieces of the gate
- `go test -race -run TestName ./internal/sqlite/` — a single test
- `just dev` — GUI with hot reload; `just app` — build the app into bin/
- Dev toolchain comes from `nix develop` (flake.nix); the `wails3` CLI is
  installed separately (`go install github.com/wailsapp/wails/v3/cmd/wails3@<v>`)
  and its version must match the wails/v3 version pinned in go.mod

## Conventions

- Code comments in English, godoc style; comments state
  constraints the code cannot show, not what the next line does.
- Go 1.26 idioms: `errors.AsType`, `wg.Go`, `t.Context()`, `omitzero`,
  `for i := range n`, `new(expr)`.
- `context.Context` is the first argument of every method that crosses
  a boundary.
- Config: TOML at `~/.config/comind/config.toml`, precedence
  env > file > defaults; secrets via `api_key_file` or env, never
  committed. The database lives in the XDG data dir, not the config dir.
- Prompts are embedded template files in `internal/llm/prompts/` —
  edit them there, not as string literals in Go.

## Gotchas

- `go build ./...` needs `frontend/dist` to exist; the committed
  `.gitkeep` guarantees it. Don't delete it.
- After changing `internal/wailsbind` methods, regenerate the frontend
  bindings: `wails3 generate bindings`.
- `Taskfile.yml` and `build/` belong to the Wails toolchain — put team
  commands in `justfile`, and keep template modifications minimal so
  diffs against a freshly generated template stay readable.
