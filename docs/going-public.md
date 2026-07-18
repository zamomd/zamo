# Going public — pre-flip checklist

Flipping the repository from private to public is **irreversible**: once the
history is public, it must be assumed to be public forever (clones, forks, and
mirrors persist even after a later re-private). Work this list top to bottom;
do not flip until every **must** item is checked.

## 1. History is secret-free (verified 2026-07-18)

The whole history was scanned, not just the working tree.

- [x] **gitleaks over the full history** — `gitleaks detect --source . --log-opts="--all"` → 208 commits scanned, **no leaks**.
- [x] **No secret file was ever committed** — `git log --all --full-history -- '*settings.toml' '*api_key*' '*.env' '*secret*' '*.key' '*credentials*'` returns nothing.
- [x] **No hardcoded provider key in any revision** — `git grep -E 'sk-ant-…|sk-[A-Za-z0-9]{20}' $(git rev-list --all)` returns nothing.
- [x] **Secrets live outside the repo by design** — `settings.toml` is in the XDG data dir, `config.toml` in `~/.config`; neither is tracked. `.gitignore` covers `.env`, `*.key`, `*.keystore`.

Re-run the gitleaks scan immediately before flipping, in case new commits landed
after this date.

## 2. The default branch shows the product

- [x] **Default branch is `main`** (GitHub `default_branch` = `main`), not the docs-only `mvp` commit. Local `origin/HEAD` re-pointed to `main`.
- [ ] **README reflects the product** — currently a placeholder (see #84, still open). Land the real README (install, screenshots/GIF, MCP setup, AGPL/BYOK) **before** the flip so the first public impression is the product, not a stub.
- [x] **LICENSE present** — AGPL-3.0.

## 3. The flip

- [ ] Re-run `gitleaks detect --source . --log-opts="--all"` — must be clean on the tip being made public.
- [ ] Confirm no open PR or branch carries a secret in its diff (`gitleaks` runs per-PR in CI; check the branches you intend to keep).
- [ ] Settings → Danger Zone → **Change visibility → Public**. Type the confirmation. This is the irreversible step.

## 4. Immediately after the flip

Branch protection and other controls are **unavailable while the repo is
private** (GitHub Free requires the repo to be public to set them), so they can
only be configured once public — do it right away, before sharing the URL.

- [ ] **Branch protection on `main`**: require a PR before merge, require the `check`, `build + test (ubuntu-latest)`, `build + test (macos-latest)`, and `gitleaks secret scan` status checks to pass, require the branch to be up to date, and (recommended) require at least one approving review.
- [ ] **Delete the stale `mvp` branch** on the remote once nothing references it, so the history has a single trunk.
- [ ] **Verify CI is green** on `main` post-flip (the public Actions runs are the first thing visitors see via the README badge).
- [ ] Decide on **Discussions / Issues templates / SECURITY.md** (see #86) and a **CONTRIBUTING** note if external contributions are welcome.

## Notes

- This checklist is part of #87. The real README is #84; `SECURITY.md` is #86;
  signed release artifacts are #77/#78 — none block the flip, but the README
  (#84) should precede it.
- "Verified 2026-07-18" items reflect the state on that date; the history is
  append-only, so they stay valid unless a later commit reintroduces a secret —
  which the per-PR gitleaks check and the pre-flip re-scan guard against.
