# Changelog

All notable changes to Intentra are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] -- GitHub Integration, Scaling & Hardening

### Added
- `intentra push` command with smart upstream detection and configurable remote
- `intentra pr` command with plan-derived PR title/body, `--title`, `--base`, `--draft` flags
- `gh` CLI integration for authentication, repo info, and branch protection checks
- Remote branch protection awareness before push
- Phased progress indicator with contextual stages and elapsed time
- Hunk summarization (configurable `max_hunk_lines`, default 50) to reduce token usage
- Three-tier clustering scaling: compact prompt IDs, file-level pre-grouping, batched clustering with LLM merge pass
- `batch_threshold` config option (default 40) for controlling clustering strategy
- Confidence scoring (high/medium/low) displayed after plan summary
- Low-confidence gating on `apply --yes` (requires `--force` to override)
- Patch pre-check with `git apply --check` before staging
- Empty patch guard (rejects zero-byte patches)
- Working tree drift detection via OS-level file fingerprinting (size + mtime)
- Graceful interrupt handling (Ctrl+C triggers clean rollback)
- Cached plan structural validation on load
- Command timeouts for all git/gh calls (30s local, 120s network)
- Schema versioning in CommitPlan (`schema_version: "v1"`)
- Sentinel error types (`ValidationError`, `GitError`, `ReasoningError`) with typed exit codes
- `--verbose` global flag for debug output
- `--force` flag on apply to override low-confidence gate
- Single-file group consolidation (prevents over-splitting same-file hunks)
- Fuzz tests for diff parser
- CRLF/mixed line-ending tests for diff parser
- CI import boundary checker (`scripts/check-imports.go`)
- Architectural laws documented in README

### Changed
- Clustering prompt discourages same-file splitting
- Messaging prompt enforces concise bodies (1-2 sentences, no line-by-line enumeration)
- Duplicate hunk assignments auto-deduplicated instead of hard error
- 64-char SHA256 hunk IDs replaced with compact tokens (h1, h2, ...) in LLM prompts
- Refactored git helpers to shared module for reuse across commands

## [0.2.0] -- Robustness & Configuration

### Added
- `git diff HEAD` captures both staged and unstaged changes
- Full rollback on partial failure (no orphaned commits)
- Clean index isolation (reset to HEAD before apply)
- Deleted file handling in parser and executor
- Rename detection (`rename from`/`rename to`)
- File mode change detection (old mode/new mode, synthetic hunks)
- File overlap warnings when multiple commits touch the same file
- `.intentra/` directory for all runtime files with per-directory `.gitignore`
- Legacy `.engine.yaml` config fallback with migration notice
- Config options: `max_retries`, `timeout`, `max_commits`, `ignore_patterns`, `sign_commits`, `scope_required`, `body_required`, `auto_push`, `remote_name`, `commit_author`, `skip_hooks`
- Live elapsed-time spinner during LLM calls
- Deterministic patch output (files sorted alphabetically)
- Untracked file detection and inclusion as synthetic diffs

### Changed
- All config consolidated under `.intentra/config.yaml`

## [0.1.0] -- Commit Planning

### Added
- Atomic commit planning from uncommitted diffs using AI reasoning
- Two-pass pipeline: intent clustering + message generation
- Plan caching with diff fingerprint for instant reuse
- Dependency-aware commit ordering (foundational packages first)
- Multi-provider LLM support: OpenAI, Anthropic, Gemini, Ollama
- Colored terminal output with type-coded commit summaries
- Protected branch detection
- Dry-run by default, `--yes` to apply
- Index restore on failure
- `plan`, `apply`, `init` commands
- `plan --json` for raw CommitPlan JSON output
- `.intentra/config.yaml` configuration
