<div align="center">
  <img src=".github/assets/logo.svg" alt="Comind" width="160">
  <h1>Comind</h1>
  <p><b>Private. Local-first. AI writes, you review.</b></p>

  <p>
    <a href="https://github.com/cominddev/comind/actions/workflows/ci.yml"><img src="https://github.com/cominddev/comind/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
    <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-AGPL--3.0-blue" alt="License: AGPL-3.0"></a>
  </p>
</div>

---

**Comind** is a block-based knowledge base that stays on your machine. The AI
writes into it directly — expanding a topic, restructuring a branch, adding
links — but nothing is silently accepted: every change is flagged, reversible,
and waits in a review queue until *you* accept or reject it. One human keeps
authority over an AI-written base.

It runs two ways from a single binary:

- **`comind`** — a desktop app (Wails) to browse the tree, drive the AI, and
  review its changes.
- **`comind mcp`** — an [MCP](https://modelcontextprotocol.io) server, so the
  AI assistant you already use (Claude Code, or any MCP client) can read your
  base and propose writes straight from where you work. Its writes land in the
  same review queue.

> [!NOTE]
> Early work in progress. Interfaces and storage may still change.

## Screenshots

<!-- TODO(#84): drop a screenshot of the tree + review queue and a short GIF of
     "expand a topic -> review -> accept" into .github/assets/ and embed them here. -->

_Coming soon — the tree with pending AI changes, and the review queue._

## Why

Auto-memory tools let an AI write to a knowledge base on its own; the base
drifts, and you never quite trust it. Comind's bet is the opposite: the AI is a
prolific writer, but a **human reviews at the block level**. That review loop —
plus provenance on every block (*why was this proposed, where did it come
from*) — is the whole point.

- **Local-first & private.** Your base is a single SQLite file on your disk.
  With a local model (Ollama / LM Studio) the data never leaves the machine.
- **AI writes, you review.** Each AI change is a block revision you can accept,
  reject, or roll back — reviewing is retroactive, so the AI is never blocked
  waiting on you.
- **Reversible by design.** History is kept per block; rejecting restores the
  last reviewed version.
- **Portable.** Block content is plain Markdown; export to Markdown is built in,
  not "someday".
- **Bring your own AI.** Anthropic, any OpenAI-compatible endpoint, or a fully
  local model. No key is ever committed.

## Features

- Block tree with a retroactive **review queue** (accept / reject / accept page).
- Per-block **revision history** and rollback.
- Typed **links** between blocks, reviewed like any other change.
- Full-text **search** (SQLite FTS5).
- **Markdown export** (Logseq-style bullets).
- **MCP server** exposing the base to AI tools: `write_blocks`, `search`,
  `get_subtree`, `get_links`, `topic_map`.

## Install & run

Comind targets **macOS and Linux**. It builds into a single Go binary.

### Prerequisites

The dev toolchain is pinned in `flake.nix`. With [Nix](https://nixos.org):

```sh
nix develop          # Go 1.26, just, bun, golangci-lint, …
```

Without Nix, you need **Go 1.26+** and **[bun](https://bun.sh)**. The desktop
build also needs the Wails v3 CLI, whose version must match the one pinned in
`go.mod`:

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.117
```

### Build the desktop app

```sh
just app             # builds the app into bin/
# or, for hot reload while developing:
just dev
```

### Build just the binary (enough for the MCP server)

```sh
go build -o comind .
./comind mcp         # stdio MCP server
./comind             # GUI (default mode)
./comind version     # print the version
```

## Configure the AI (BYOK)

The GUI's AI features are off until you configure a provider. Configuration is
declarative and read-only to the app: `~/.config/comind/config.toml` (override
with `$COMIND_CONFIG`; respects `XDG_CONFIG_HOME`). Secrets are **never** put in
this file — use an environment variable or `api_key_file`.

**Anthropic:**

```toml
[llm]
provider = "anthropic"
model = "claude-sonnet-5"   # optional; empty uses the provider default
# key via env: export ANTHROPIC_API_KEY=sk-...
# or a file:  api_key_file = "/path/to/anthropic.key"
```

**Local model (Ollama) — nothing leaves your machine:**

```toml
[llm]
provider = "openai-compatible"   # covers OpenAI, OpenRouter, DeepSeek, Ollama, LM Studio
model = "qwen2.5:3b"
base_url = "http://localhost:11434/v1"
# no key needed for a local model
```

Key resolution order: environment variable (`ANTHROPIC_API_KEY` /
`OPENAI_API_KEY`) → `api_key_file` → inline `api_key`. Leaving `provider` empty
turns AI features off in the GUI — but `comind mcp` still works fully, because
there the AI is the *client*, not this setting.

The GUI writes changes you make in its settings screen to a separate mutable
layer (`settings.toml` in the data dir); set `declarative = true` in
`config.toml` to turn that off and keep the app strictly read-only to config.

## Use it as an MCP server

`comind mcp` speaks MCP over stdio, so an MCP client spawns it as a subprocess.
To register it with **Claude Code**:

```sh
claude mcp add comind -- /absolute/path/to/comind mcp
```

Now the assistant can read your base (`search`, `get_subtree`, `get_links`,
`topic_map`) and propose writes (`write_blocks`) while you work. Proposed writes
are **not** applied blindly — they land in Comind's review queue for you to
accept or reject, same as any AI change.

## Data & privacy

Everything is local:

- **Config:** `~/.config/comind/config.toml` (declarative, safe to commit —
  minus secrets).
- **Database & mutable settings:** the platform data directory, e.g.
  `~/Library/Application Support/comind/` (macOS) or `~/.local/share/comind/`
  (Linux). Override the DB path with `$COMIND_DB`.

With a local model, no part of your base is sent to a third party. With a hosted
provider, only the content you ask the AI to work on is sent to that provider,
under your own key.

## Development

```sh
just check    # the full CI gate: lint + race tests + tidy check
just test     # race tests only
just fmt      # format Go
```

Architecture, conventions and gotchas live in [`AGENTS.md`](AGENTS.md).

**Stack:** Go 1.26 · Wails v3 (alpha) · Svelte 5 + TypeScript + Vite ·
`modernc.org/sqlite` (pure-Go, FTS5).

## License

[AGPL-3.0](LICENSE). You bring your own AI provider and key; Comind ships no
credentials and routes nothing through anyone else's servers.
