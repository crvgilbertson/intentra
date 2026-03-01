---
description: LLM reasoning layer constraints and prompting rules
globs: "engine/reasoning/**/*.go"
alwaysApply: false
---

# Reasoning Layer Rules

This package owns the LLM SDK clients (OpenAI, Anthropic), structured calls, and retries with message history. **NO git calls allowed.**

## LLM Prompting & Structured Output

- Use a two-pass approach if needed:
  1. Clustering — assign hunk_ids into groups.
  2. Messaging — produce commit metadata for each group.
- Output must match CommitPlan schema exactly.
- No extra keys. No markdown. No commentary.
- Enforce Conventional Commits style:
  - `type(scope): subject`
  - Imperative subject, no trailing period.
  - Subject length <= config `max_subject_len`.
  - Breaking changes require footers (e.g., `BREAKING CHANGE: ...`).
