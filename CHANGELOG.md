# Changelog

All notable changes to Intentra are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] -- Deterministic Replay & Confidence System

### Added

**Deterministic Replay**
- `intentra plan --snapshot <file>`: export reproducible plan bundle (engine version, schema version, prompt fingerprint, provider/model, config, diff fingerprint, hunk metadata, normalized plan, confidence breakdown, trace, timestamp)
- `intentra replay <snapshot>`: re-run planner from snapshot, compare structurally against stored plan, report IDENTICAL / STRUCTURALLY_EQUIVALENT / DIVERGENT with specific divergences
- Snapshot format is self-contained — no git dependency required for replay
- Schema version mismatch on replay is a hard failure
- Prompt fingerprint mismatch is reported clearly

**Structured Confidence System**
- Confidence decomposed into components: `coverage`, `entanglement`, `repair_activity`, `overlap`, `reorder_penalty`
- Components included in plan JSON, snapshot, and `doctor` output
- Configurable confidence profiles: `strict` (block < 90%), `balanced` (block < 75%), `permissive` (warn only)
- Profile configurable via `engine.confidence.profile` in config

**Cache Invalidation Policy**
- Cached plans automatically invalidated when prompt fingerprint changes
- Cached plans automatically invalidated when schema version changes
- `--allow-stale-prompts` flag on apply for power-user override
- No silent reuse of behaviorally stale plans

**Explainability**
- `intentra explain`: pure engine trace showing clustering rationale, repair heuristic activity (dedup count, orphan count, rescue success/failure, repair count), dependency reorder status, and confidence breakdown
- `intentra explain --json` for programmatic access
- Pipeline trace stored in plan JSON (`trace` field)

**Pipeline Instrumentation**
- Pipeline trace records: clustering strategy used, dedup count, orphan count, rescue attempted/succeeded, repair count, reorder applied, commit count before/after reorder
- Trace data flows into confidence scoring (`repair_activity`, `reorder_penalty` components)

**CI Regression**
- Replay regression tests added to CI (snapshot round-trip, structural comparison, explain report)
- 8 new tests covering plan comparison logic (identical, structurally equivalent, divergent grouping, divergent count, divergent type, prompt mismatch, snapshot serialization, explain report)

### Changed
- Prompt fingerprint now stores full 64-char SHA256 hash in plan JSON (truncated to 16 chars for display)
- `PromptFingerprintShort()` added for display contexts
- Confidence scoring accepts optional trace data for `repair_activity` and `reorder_penalty` computation
- Apply command uses configurable confidence profile instead of hardcoded "low" threshold

## [0.3.0] -- Hardening, Explainability & Scaling

### Added

**Engine Guarantees**
- Full rollback coverage: hook failure, orphan HEAD, working tree drift
- Patch pre-check with `git apply --check` before staging
- Empty patch guard (rejects zero-byte patches)
- Working tree drift detection via OS-level file fingerprinting (size + mtime)
- Graceful interrupt handling (Ctrl+C triggers clean rollback)
- Cached plan structural validation on load
- Command timeouts for all git/gh calls (30s local, 120s network)

**Determinism & Drift Control**
- Prompt fingerprinting (SHA256 hash of all prompt templates), stored in plan JSON
- Cache invalidation when prompt fingerprint changes (prevents silent behavioral regression)
- Schema versioning in CommitPlan (`schema_version: "v1"`)
- Commit ID normalization (`c1`, `c2`, `c3`) independent of LLM output ordering
- CI import boundary checker (`scripts/check-imports.go`)
- CI schema discipline checks (version defined, set on save, validated on load)
- Snapshot regression test covering deduplication, orphan repair, dependency ordering, ID normalization, prompt fingerprint determinism, and rationale propagation

**Explainability**
- Per-cluster rationale in schema and plan output
- Confidence scoring (high/medium/low) displayed after plan summary
- Low-confidence gating on `apply --yes` (requires `--force` to override)
- `intentra doctor` command: engine version, schema version, prompt fingerprint, diff fingerprint, config snapshot, API key status, trust surface disclosure, caching rules (`--json` for automation)
- Sentinel error types (`ValidationError`, `GitError`, `ReasoningError`) with typed exit codes
- `--verbose` global flag for debug output
- `--force` flag on apply to override low-confidence gate

**Scaling**
- Three-tier clustering scaling: compact prompt IDs, file-level pre-grouping, batched clustering with LLM merge pass
- `batch_threshold` config option (default 40) for controlling clustering strategy
- Hunk summarization (configurable `max_hunk_lines`, default 50) to reduce token usage
- Compact prompt IDs: 64-char SHA256 hunk IDs replaced with short tokens (h1, h2, ...) in LLM prompts
- Single-file group consolidation (prevents over-splitting same-file hunks)
- Phased progress indicator with contextual stages and elapsed time
- Fuzz tests for diff parser
- CRLF/mixed line-ending tests for diff parser

**GitHub Integration (Adapter Layer)**
- `intentra push` command with smart upstream detection and configurable remote
- `intentra pr` command with plan-derived PR title/body, `--title`, `--base`, `--draft` flags
- `gh` CLI integration for authentication, repo info, and branch protection checks
- Remote branch protection awareness before push

### Changed
- Clustering prompt requires rationale per group and discourages same-file splitting
- Messaging prompt enforces concise bodies (1-2 sentences, no line-by-line enumeration)
- Duplicate hunk assignments auto-deduplicated instead of hard error
- Refactored git helpers to shared module for reuse across commands
- Architectural laws documented in README

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
