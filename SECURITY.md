# Security policy

## Reporting a vulnerability

Please report security issues **privately**, not as a public issue.

Use GitHub's private vulnerability reporting: on the repository's **Security**
tab, choose **Report a vulnerability**. This opens a private advisory visible
only to you and the maintainers.

Include, as far as you can: what the issue is, how to reproduce it, the impact
you see, and the affected version (`comind version`). You'll get an
acknowledgement, and we'll keep you posted as we work on a fix. Please give us a
reasonable window to release a fix before disclosing publicly.

Comind is pre-1.0; only the latest release is supported. There is no bug-bounty
program.

## Security model

Comind is **local-first, bring-your-own-key, and serverless by design**. The
trust model follows from that.

- **No Comind servers, no telemetry.** There is no backend we operate and
  nothing we collect. Your knowledge base is a SQLite file in your XDG data
  directory; it never leaves your machine unless you export or sync it
  yourself.

- **Bring your own key (BYOK).** AI features talk directly from your machine to
  the provider *you* configure — Anthropic, any OpenAI-compatible endpoint, or a
  fully local runtime such as Ollama or LM Studio (no network at all). Comind is
  never in that path. With no provider configured, AI features are simply off.

- **Secrets stay out of the repo and off the tracker.** The API key lives in
  `settings.toml` (written `0600` in the XDG data dir), or is supplied via
  `api_key_file` / an environment variable. The declarative `config.toml` is
  read-only to the app and never holds a key it wrote. No secret is ever
  committed; `declarative = true` locks the mutable layer so the GUI cannot
  unlock itself.

- **The AI proposes, you review.** Every AI-originated change — from the GUI or
  from an MCP client — enters a review queue as a *proposal*. Nothing reaches
  the base until you approve it. The review boundary guards the base against the
  AI, not against its owner: your own manual edits apply directly.

- **MCP is a local stdio server.** `comind mcp` speaks JSON-RPC over stdio to a
  client you launch (Claude Code, Claude Desktop, Cursor, …). It exposes read
  tools and a single write path (`propose_blocks`) that only queues a proposal —
  it cannot write to the base directly. It listens on no network port.

### What is in scope

Anything that lets code or a proposal reach the base without review, exfiltrate
the local database or a configured key, escape the proposal/review boundary, or
that mishandles a secret (logging it, writing it world-readable, committing it).

### What is out of scope

The security of a third-party AI provider you configure, the MCP client you
choose to run, or the machine Comind runs on. A provider key you paste into a
public place is your disclosure, not Comind's.
