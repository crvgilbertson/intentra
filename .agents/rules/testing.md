---
description: Testing requirements and strategy
globs: "**/*_test.go"
alwaysApply: false
---

# Testing Requirements

- Unit test pure logic:
  - Diff parsing + hunk hashing.
  - Schema validation + business validation.
  - Patch generation correctness.
- For git executor:
  - Integration tests using a temp git repo.
  - Ensure abort restores index on failure.
- Mock LLM client in tests; never hit network.
