---
description: Context builder and diff/hunk handling rules
globs: "engine/context/**/*.go"
alwaysApply: false
---

# Context Layer Rules

Prefer pure functions where possible.

## Diff/Hunk Handling

- Parse unified diff into hunks.
- Each hunk includes: hunk_id, file_path, header, patch, summary.
- HunkID = `sha256(filePath + header + patch)` — must be stable and unique.
- Sort hunks by file path, then by hunk header position.
- Planner works on hunk granularity; do not collapse hunks to file-level.
