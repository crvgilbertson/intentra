---
description: Determinism requirements, git safety, and failure handling
alwaysApply: true
---

# Determinism & Safety

## Determinism Requirements

- Use low temperature (0.1–0.2) for planning.
- Use strict JSON schema outputs (no free-form text).
- Stable ordering:
  - Hunks sorted by file path then hunk header position.
  - Commits returned by planner must have stable IDs (c1, c2, …) and consistent order.
- HunkIDs must be stable: `sha256(filePath + header + patch)`.

## Git Safety

- Never modify working tree in `plan`.
- `apply` must:
  - Take a snapshot of current index (or otherwise be able to restore).
  - Fail fast if working tree/index changes mid-run (detect via `git diff` / `status`).
  - Abort cleanly on any error and restore index state.
- No history rewriting. No rebase/amend in v0.1.

## Failure Handling

AI failures:
- Invalid JSON → retry with correction prompt (max 1 retry).
- Missing hunks → regenerate plan.
- Overlapping hunks → regenerate plan.
- If still invalid after retry → abort with clear error; never apply.

Git failures:
- Any git command failure → abort immediately.
- Restore index to pre-apply snapshot.
- Report which commit failed and why.
- Never leave partially applied state.
