---
description: CommitPlan validation rules
globs: "engine/validators/**/*.go"
alwaysApply: false
---

# Validator Layer Rules

**NO git calls allowed.**

CommitPlan validation must check:
- Every hunk_id in context appears exactly once across all commits.
- No duplicate hunk_ids across commits.
- Commit type is in allowed types.
- Scope (if present) matches allowed scopes or is empty.
- Subject constraints: length, no trailing period, imperative tense (best-effort).
- Breaking commits must have a `BREAKING CHANGE` footer or body mention.

If invalid:
- Attempt correction retries (defined by max_retries config) by returning the validation error back to the reasoning layer.
- The reasoning layer will append the failed JSON and the error to the message history to allow the LLM to self-correct.
- If still invalid after all retries, return an error and do not apply anything.
