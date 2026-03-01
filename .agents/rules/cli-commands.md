---
description: CLI layer rules and UX requirements
globs: "cmd/**/*.go"
alwaysApply: false
---

# CLI Layer Rules

- No business logic in `cmd/`; delegate to engine packages.
- `plan` prints a readable summary by default.
- `--json` outputs exact CommitPlan JSON.
- `apply` defaults to `--dry-run` unless user passes `--yes` or `--apply`.
- Always print what will happen before doing it.
