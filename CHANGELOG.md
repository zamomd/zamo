# Changelog

All notable changes to Zamo are recorded here. Entries are generated from
[Conventional Commits](https://www.conventionalcommits.org) by
[git-cliff](https://git-cliff.org); the layout follows
[Keep a Changelog](https://keepachangelog.com).

## [0.0.14] - 2026-10-15

### Breaking Changes

- Rename the project from comind to zamo

### Features

- Enlarge the rail glyphs and drop the sidebar title
- Word-level history diff, d3-force topic map
- Lay the settings screen out in sections
- Scheduled block refreshes with a cron scheduler ([#176](https://github.com/zamomd/zamo/pull/176))
- AI progress in the command box, resizable sidebar ([#177](https://github.com/zamomd/zamo/pull/177))
- Page properties, a first block by hand, and no block metadata
- An assistant panel on one agent loop, replacing one-shot generation ([#178](https://github.com/zamomd/zamo/pull/178))
- A kept export destination, scheduled and optionally committed to git ([#173](https://github.com/zamomd/zamo/pull/173))
- Tabs for several topics open at once ([#181](https://github.com/zamomd/zamo/pull/181))

### Bug Fixes

- Name a document topic by its first line
- An edit that changes nothing writes no revision ([#171](https://github.com/zamomd/zamo/pull/171))
- Keep Windows device names out of exported filenames
- Report the repository root with native separators

### Internal

- Publish one SHA256SUMS instead of a checksum per artifact
- Drop the CLAUDE.md pointer to AGENTS.md
- Move from encoding/json to encoding/json/v2
- Adopt testify as the assertion style
- Assert with testify
- Assert with testify
- Assert with testify
- Assert with testify in the remaining packages and drop go-cmp
- Lint with golangci-lint v2.13.2, built with Go 1.27
- Fence entry points and feature modules with depguard
- Use Go 1.27 idioms
- Ship deb, rpm and tar.gz for Linux amd64 and arm64
- Test on Windows and ship NSIS installers for amd64 and arm64
- Lint with golangci-lint 2.14 and hold bun updates for five days
- Run the CI gate before any release build

## [0.0.13] - 2026-09-18

### Features

- Import Obsidian vaults and Logseq graphs ([#134](https://github.com/zamomd/zamo/pull/134)) ([#161](https://github.com/zamomd/zamo/pull/161))
- Bring a Comind export back in ([#162](https://github.com/zamomd/zamo/pull/162))

### Internal

- Exempt tests and fixtures from the english-only guard
- Ship a DMG next to the zip and prepare Developer ID signing ([#168](https://github.com/zamomd/zamo/pull/168))
- Group runs on main by commit so quick merges keep their results

## [0.0.12] - 2026-09-08

### Features

- Cancel an in-flight AI generation ([#155](https://github.com/zamomd/zamo/pull/155))
- Back up and restore the complete Comind base ([#131](https://github.com/zamomd/zamo/pull/131)) ([#144](https://github.com/zamomd/zamo/pull/144))
- Export the base as Markdown from Settings ([#158](https://github.com/zamomd/zamo/pull/158))

### Bug Fixes

- Restore Svelte type checking and enforce it in the gate ([#124](https://github.com/zamomd/zamo/pull/124)) ([#143](https://github.com/zamomd/zamo/pull/143))

### Performance

- Load topic subtrees concurrently on reload ([#154](https://github.com/zamomd/zamo/pull/154))

### Internal

- Run mutation testing over the whole backend by default
- Move markdown export into the first feature module ([#156](https://github.com/zamomd/zamo/pull/156))
- Bind base backup as its own module ([#157](https://github.com/zamomd/zamo/pull/157))

## [0.0.11] - 2026-08-08

### Breaking Changes

- Replace semantic links with inline references ([#115](https://github.com/zamomd/zamo/pull/115))
- Databases created by comind v0.0.10 or earlier cannot be opened. Inline references replaced the edges table by rewriting the first migration in place, so there is no upgrade path: delete the database file (and re-export beforehand if you want to keep its contents).
- A topic is either an outline or a document ([#128](https://github.com/zamomd/zamo/pull/128))
- The MCP search tool returns {"hits": [...]} with a snippet per match, where it previously returned {"blocks": [...]}. MCP clients are configured outside this repo and need their expectations updated.

### Features

- A global switch to disable all AI features ([#109](https://github.com/zamomd/zamo/pull/109)) ([#116](https://github.com/zamomd/zamo/pull/116))

### Bug Fixes

- Carry unknown keys in settings.toml instead of refusing ([#125](https://github.com/zamomd/zamo/pull/125)) ([#126](https://github.com/zamomd/zamo/pull/126))
- Carry the error marshaler on the bound service ([#118](https://github.com/zamomd/zamo/pull/118)) ([#121](https://github.com/zamomd/zamo/pull/121))

### Internal

- Component testing infrastructure ([#119](https://github.com/zamomd/zamo/pull/119)) ([#122](https://github.com/zamomd/zamo/pull/122))

## [0.0.10] - 2026-07-31

### Features

- Outline operations — sibling anchors, strided positions, seven keyboard ops ([#105](https://github.com/zamomd/zamo/pull/105))
- The block row — anchor dot, zoom-in, one title, a still scrollbar ([#106](https://github.com/zamomd/zamo/pull/106))
- The block editing model — click for a caret, and an outliner's keyboard ([#66](https://github.com/zamomd/zamo/pull/66)) ([#108](https://github.com/zamomd/zamo/pull/108))
- Undo by write batch — one Ctrl+Z for every outline gesture

### Bug Fixes

- Keep revision positions aligned on renumber ([#112](https://github.com/zamomd/zamo/pull/112))

### Internal

- Preserve dist gitkeep after frontend build
- Trim implementation commentary
- Remove dead code and collapse genuine duplication ([#113](https://github.com/zamomd/zamo/pull/113))
- Reduce minimum package release age

## [0.0.9] - 2026-07-29

### Features

- Include typed links in the markdown export ([#55](https://github.com/zamomd/zamo/pull/55)) ([#90](https://github.com/zamomd/zamo/pull/90))
- Unlink op with reviewable removal ([#53](https://github.com/zamomd/zamo/pull/53)) ([#96](https://github.com/zamomd/zamo/pull/96))
- Render block content as markdown ([#59](https://github.com/zamomd/zamo/pull/59)) ([#98](https://github.com/zamomd/zamo/pull/98))
- Live generation progress channel ([#76](https://github.com/zamomd/zamo/pull/76)) ([#97](https://github.com/zamomd/zamo/pull/97))
- Visual foundation — design tokens, embedded mono, tree rails ([#101](https://github.com/zamomd/zamo/pull/101))
- Shell layout — mode rail, topic sidebar, one topic in the main area ([#102](https://github.com/zamomd/zamo/pull/102))
- Topic graph — read path, deterministic layout, click to open ([#103](https://github.com/zamomd/zamo/pull/103))

## [0.0.8] - 2026-07-18

### Breaking Changes

- Block revisions replace proposals — direct agent writes with post-hoc review ([#95](https://github.com/zamomd/zamo/pull/95))
- Proposals are gone from the frontend. The AI flows now write directly and every touched block lands flagged unreviewed; the tree renders unreviewed rows and pending-deletion ghosts in place with inline Accept/Reject (and page-level Accept all/Reject all), and a new Changes screen replaces the Review tab for a flat pass over everything awaiting review, including typed-link edges.

### Features

- Implement GenerateEdit (kill the last NotImplemented stub) ([#50](https://github.com/zamomd/zamo/pull/50))
- Structured errors across the Wails boundary ([#58](https://github.com/zamomd/zamo/pull/58)) ([#62](https://github.com/zamomd/zamo/pull/62))
- Search in the Base view with jump-to-block ([#51](https://github.com/zamomd/zamo/pull/51))
- Edges get a read path and manual links ([#52](https://github.com/zamomd/zamo/pull/52)) ([#89](https://github.com/zamomd/zamo/pull/89))
- Show readable messages for binding errors ([#83](https://github.com/zamomd/zamo/pull/83)) ([#94](https://github.com/zamomd/zamo/pull/94))

### Bug Fixes

- Sync error map to the post-#95 code taxonomy

### Internal

- Frontend into lib/ components and stores/ view-models ([#57](https://github.com/zamomd/zamo/pull/57)) ([#60](https://github.com/zamomd/zamo/pull/60))
- Drop the now-dead ErrNotImplemented ([#61](https://github.com/zamomd/zamo/pull/61))
- Add placeholder README with logo
- Move file writing out of the domain ([#56](https://github.com/zamomd/zamo/pull/56)) ([#64](https://github.com/zamomd/zamo/pull/64))

## [0.0.7] - 2026-07-16

### Features

- Browse the tree — collapse/expand nodes ([#10](https://github.com/zamomd/zamo/pull/10)) ([#44](https://github.com/zamomd/zamo/pull/44))
- One "ask AI" command box for both AI flows ([#46](https://github.com/zamomd/zamo/pull/46))
- Live "new proposals" badge from MCP ([#45](https://github.com/zamomd/zamo/pull/45))
- Provider settings in-app ([#19](https://github.com/zamomd/zamo/pull/19)) ([#49](https://github.com/zamomd/zamo/pull/49))

## [0.0.6] - 2026-07-16

### Features

- Review screen — accept / reject by keyboard ([#42](https://github.com/zamomd/zamo/pull/42))
- Expose Search to the frontend ([#43](https://github.com/zamomd/zamo/pull/43))

## [0.0.5] - 2026-07-15

### Features

- Serve the base over MCP (stdio) ([#34](https://github.com/zamomd/zamo/pull/34))
- Export the base as portable markdown ([#37](https://github.com/zamomd/zamo/pull/37))
- Bring-your-own-AI via OpenAI-compatible providers ([#38](https://github.com/zamomd/zamo/pull/38))
- Restructure the base by an AI command ([#39](https://github.com/zamomd/zamo/pull/39))
- Edit block text by hand ([#40](https://github.com/zamomd/zamo/pull/40))

## [0.0.4] - 2026-07-12

### Features

- Assemble a reviewed proposal into ready ops ([#32](https://github.com/zamomd/zamo/pull/32))
- Expand a topic into a queued proposal (the full flow) ([#33](https://github.com/zamomd/zamo/pull/33))
- Raw slice screen — topic in, tree out ([#8](https://github.com/zamomd/zamo/pull/8)) ([#36](https://github.com/zamomd/zamo/pull/36))

## [0.0.3] - 2026-07-11

### Features

- Expand a topic into a tree via the Anthropic API ([#31](https://github.com/zamomd/zamo/pull/31))
- Implement the SQLite proposal queue (save, list, reject) ([#29](https://github.com/zamomd/zamo/pull/29))
- Implement the SQLite block read methods (topics, subtree, search) ([#35](https://github.com/zamomd/zamo/pull/35))
- Apply a proposal atomically into the base ([#30](https://github.com/zamomd/zamo/pull/30))

## [0.0.2] - 2026-07-09

### Features

- Implement ParseOps for topic-expansion replies ([#22](https://github.com/zamomd/zamo/pull/22))

### Bug Fixes

- Regenerate bundle metadata from config.yml
- Install Bun instead of Node in the release workflow

### Internal

- Sync nix devShell to GTK4/WebKit6
- Add Biome frontend linting to the check gate
- Migrate frontend tooling from npm to bun
- Bump Go to 1.26.5 to fix GO-2026-5856 (crypto/tls) ([#24](https://github.com/zamomd/zamo/pull/24))
- Add gitleaks, english-only check, and Dependabot ([#23](https://github.com/zamomd/zamo/pull/23))
- Derive wails3 CLI version from go.mod in release workflow ([#28](https://github.com/zamomd/zamo/pull/28))
- Add gremlins mutation-testing config and recipe ([#26](https://github.com/zamomd/zamo/pull/26))
- Auto-generate CHANGELOG.md from Conventional Commits (git-cliff)
- Add .editorconfig and enforce 120-col line width

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

### Internal

- Set up linting, CI, and task runner
- Initialize Go module
- Add Wails v3 + Svelte shell scaffolding
- Add nix dev shell (flake + direnv)
- Add agent guide (AGENTS.md, CLAUDE.md)
- Stamp version from git tag via ldflags
- Add tag-driven macOS release workflow
- Install GTK4/WebKit6 dev libraries to match wails v3 default


