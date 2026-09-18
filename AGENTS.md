# Comind — agent guide

Local-first block-based knowledge base: AI writes directly; every change is
flagged, reversible, and reviewed by the human. One Go binary, two modes:
`comind` (Wails v3 GUI) and `comind mcp` (stdio MCP server).
Stack: Go 1.26, Wails v3 beta, Svelte 5 + TS + Vite, modernc.org/sqlite.

## Commands

Always finish work with the full gate — CI runs exactly this, split into
three parallel jobs (`check-static`, `test`, `check-web`):

```
just check          # lint + bindings drift + go mod tidy -diff, race tests, frontend
```

- `just fmt` — format (gofumpt via golangci-lint); run after editing Go code
- `just test` / `just lint` / `just cover` — pieces of the gate
- `go test -race -run TestName ./internal/sqlite/` — a single test
- `just dev` — GUI with hot reload; `just app` — build the app into bin/
- Dev toolchain comes from `nix develop` (flake.nix); the `wails3` CLI is
  installed separately (`go install github.com/wailsapp/wails/v3/cmd/wails3@<v>`)
  and its version must match the wails/v3 version pinned in go.mod

## Conventions

- Keep comments in English and concise. Document exported Go APIs with godoc
  when the name and signature do not already make the contract clear. Other
  comments are only for non-obvious invariants, constraints, or safety reasons.
  Never record implementation history, rejected approaches, task references,
  debugging evidence, or the steps used to reach a solution. Do not narrate
  code or duplicate assertions and test names.
- Go 1.26 idioms: `errors.AsType`, `wg.Go`, `t.Context()`, `omitzero`,
  `for i := range n`, `new(expr)`.
- `context.Context` is the first argument of every method that crosses
  a boundary.
- Config: `config.toml` (`~/.config/comind/config.toml`) is declarative
  and read-only to the app — safe to be a nix-store symlink. The GUI
  writes a separate mutable layer, `settings.toml`, in the XDG data
  dir (0600; only fields the user changed in the GUI land there, may
  hold the key). Precedence: env > settings.toml > config.toml >
  defaults. `declarative = true` in config.toml (only) turns the
  mutable layer off entirely, so the GUI cannot unlock itself. The LLM
  provider has no default; empty means AI features are off. Secrets
  via `api_key_file` or env, never committed. The database lives in
  the XDG data dir, not the config dir.
- Prompts are embedded template files in `internal/llm/prompts/` —
  edit them there, not as string literals in Go.
- Features live in `internal/modules/<name>`, one package each, built
  only from the capability interfaces in `internal/host` (never
  `knowledge.Service` or `internal/sqlite`) and assembled in `main.go`,
  where each module is bound as its own Wails service. The core does
  not grow a method per feature; a module must be deletable without
  breaking the core build. Long-running work runs under
  `internal/jobs` so it can be cancelled and ends with the process.

## Gotchas

- `go build ./...` needs `frontend/dist` to exist; the committed
  `.gitkeep` guarantees it. Don't delete it.
- After changing a bound Go service — its methods, types, or godoc —
  regenerate the frontend bindings with `just bindings`; `just check`
  fails on stale ones. The recipe carries the flags `build/Taskfile.yml`
  uses; the bare `wails3 generate bindings` emits `.js` and deletes the
  tracked `.ts` bindings.
- `confirm()`, `alert()` and `prompt()` do nothing in the Wails webview:
  v3 implements no `runJavaScriptConfirmPanelWithMessage:`, so WKUIDelegate
  treats the call as cancelled and `confirm()` always returns false. Build
  confirmation into the component. This passes in a browser tab under
  `just dev` and only breaks in the packaged app.
- `Taskfile.yml` and `build/` belong to the Wails toolchain — put team
  commands in `justfile`, and keep template modifications minimal so
  diffs against a freshly generated template stay readable.
