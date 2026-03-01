---
description: Go code standards for the workflow engine
globs: "**/*.go"
alwaysApply: false
---

# Go Code Standards

- Go 1.22+.
- Prefer small packages, explicit interfaces, minimal globals.
- Use `context.Context` for any operation that can block (LLM calls, git exec).
- Structured errors:
  - Wrap with `fmt.Errorf("...: %w", err)`.
  - Sentinel errors for user-facing categories (validation, git, reasoning).
- No panics except truly unrecoverable programmer errors.
