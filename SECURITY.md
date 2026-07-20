# Security Policy

Comind is a **local-first, single-user desktop application** and an **MCP
server**. It has no hosted backend: your knowledge base is a SQLite file on your
own machine, and you bring your own AI provider and key. That shapes the threat
model below.

## Reporting a vulnerability

**Please do not report security issues in public GitHub issues, pull requests,
or discussions.**

Report privately through GitHub's [private vulnerability
reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability):
open the repository's **Security** tab and choose **Report a vulnerability**.
This opens a private advisory visible only to you and the maintainers.

Please include, as far as you can:

- what the issue is and the impact you think it has;
- the version or commit you tested (`comind version`);
- steps or a minimal proof of concept to reproduce it;
- your environment (OS, provider — local model or hosted).

You will get an acknowledgement as soon as we can triage it. This is an
early-stage project maintained on a best-effort basis, so please allow time for
a fix; we will keep you updated and coordinate disclosure with you.

## Supported versions

Comind is pre-1.0 (`v0.0.x`) and moving fast. Security fixes land on `main` and
in the **latest release** only — older `v0.0.x` tags are not backported. Please
reproduce against the latest release or `main` before reporting.

## Threat model

Because Comind runs entirely on your machine under your own user account, the
relevant attack surface is not a server but the untrusted inputs the app
processes and the credentials it handles.

### In scope

- **Rendering of untrusted, AI-authored content.** Block content is written by
  an LLM and is treated as untrusted: it is rendered so that it cannot introduce
  active content — script execution, embedded HTML, or a live hostile link —
  into the GUI webview. Any bypass that does is in scope.
- **Credential handling.** Any path that leaks the user's API key — writing it
  somewhere it should not go, logging it, or the app persisting it where it does
  not belong — is in scope. The app never writes your key to `config.toml`.
- **The MCP server (`comind mcp`).** A write that reaches the base without
  passing through the human review queue, or a read that returns data outside
  the requested scope, is in scope.
- **Local file handling.** Path traversal or arbitrary read/write through
  configurable paths (database, export directory, `api_key_file`) is in scope.
- **Dependencies.** Known-vulnerable dependencies (Go modules, GitHub Actions,
  frontend packages) — though these are largely handled by the automation below.

### Out of scope

- Attacks that require an **already-compromised machine or OS account**: Comind
  trusts the local user and anything running as that user.
- **The content you choose to send to a hosted provider.** When you point Comind
  at a hosted model, the prompts and block content you ask it to work on go to
  that provider under your own key — that boundary is the provider's, not
  Comind's. Use a local model if data must not leave the machine.
- The quality, safety, or behavior of the **AI model itself** (hallucinations,
  bad edits) — that is exactly what the human review queue exists to catch.
- Denial of service from feeding the app a pathologically large local database
  or input.
- Social engineering that convinces a user to run a malicious binary or paste a
  hostile `config.toml`.

## How Comind handles security

- **You review every AI change.** AI writes are never applied blindly; they land
  in a review queue as reversible block revisions for you to accept or reject.
- **Bring your own key, kept out of the repo.** `config.toml` is declarative and
  may be committed, so it *should not* hold secrets — prefer the environment
  (`ANTHROPIC_API_KEY` / `OPENAI_API_KEY`) or `api_key_file`. An inline `api_key`
  field exists as a fallback but is discouraged for exactly this reason, and the
  app never writes keys to `config.toml` itself.
- **Secret scanning.** CI runs [gitleaks](https://github.com/gitleaks/gitleaks)
  (pinned by version and checksum) over pull requests and history.
- **Dependency vulnerabilities.** CI runs
  [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) on every
  change, and Dependabot proposes dependency updates weekly.
- **Pure-Go SQLite.** The database driver (`modernc.org/sqlite`) is pure Go, so
  there is no bundled C library to keep patched.

## Disclosure

We prefer coordinated disclosure: report privately, give us a reasonable window
to ship a fix and cut a release, and we will credit you in the advisory unless
you prefer to stay anonymous.
