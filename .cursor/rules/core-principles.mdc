---
description: Golden rules, architecture boundaries, and guiding principles for the AI Code Workflow Engine
alwaysApply: true
---

# Core Principles

You are an expert Go engineer building a CLI-first workflow engine.
Priorities in order: correctness, safety, determinism, extensibility, UX clarity.

## Golden Rules

- NEVER let the AI/LLM layer execute shell commands or mutate the repo.
- NEVER let the Executor call the LLM.
- NEVER let the Planner execute git commands directly.
- All side effects happen ONLY in Executors and only when user explicitly requests apply.
- Always support dry-run mode for anything that mutates state.
- All LLM outputs must be validated against strict JSON Schema and additional business validation.
- If validation fails, retry with a correction prompt; if still failing, abort with clear error.

## Architecture Boundaries (mandatory)

- `cmd/` — Cobra CLI commands only; no business logic.
- `engine/context/` — Git state collection + diff/hunk parsing; pure functions where possible.
- `engine/models/` — Shared domain types (Hunk, CommitPlan, etc.); no logic, no imports from other engine packages.
- `engine/reasoning/` — OpenAI client + structured calls + retries; NO git calls.
- `engine/reasoning/schemas/` — JSON schema definitions for structured LLM output.
- `engine/planners/` — Planner implementations; NO git calls.
- `engine/validators/` — Plan validation; NO git calls.
- `engine/executors/` — Apply logic; can call git; NO LLM calls.
- `config/` — Configuration loading/merging; no engine logic.

Do not introduce cross-layer imports that violate the above.

## When Unsure

- Choose safety and clarity over clever automation.
- Prefer returning a plan for user review rather than auto-applying.
