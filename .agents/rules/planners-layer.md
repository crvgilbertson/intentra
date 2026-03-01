---
description: Planner layer constraints
globs: "engine/planners/**/*.go"
alwaysApply: false
---

# Planner Layer Rules

- **NO git calls** — planners must never execute git commands directly.
- Planner works on hunk granularity (not file-only); do not lose hunk mapping.
- Each hunk includes: hunk_id, file_path, header, patch, summary.
- Commit planning is one `Planner` implementation; keep the interface generic for future planners (PRPlanner, RiskPlanner, ReleasePlanner).
