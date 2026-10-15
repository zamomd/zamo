<!-- Title: a Conventional Commit, e.g. `feat(editor): split a block at the cursor`.
     It becomes the squash commit and the changelog entry; mark a breaking change with `!`. -->

## What and why

<!-- What changes and the problem it solves. Link the issue: Closes #123 -->

## How it was tested

<!-- `just check` runs in CI. Describe what it cannot cover: manual steps,
     and a screenshot or recording for any UI change. -->

## Checklist

- [ ] A feature lives in `internal/modules/<name>` and uses only `internal/host`
- [ ] A model writes to the base only through the agent loop and its `WritePolicy`
- [ ] No `confirm()`, `alert()` or `prompt()` in the frontend (they do nothing in the app)
- [ ] A change to the database schema or an export format is marked breaking (`!`)

## AI assistance

<!-- None, or which tool and how much of the change it wrote.
     You must understand and be able to explain every line you submit. -->
