# Changelog

All notable changes to Intentra are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.6.0] -- Change Intelligence & Reliability

### Added
- `intentra release-notes`: derive release notes from the cached plan with grouped changes, breaking-change sections, and deterministic risk summaries.
- `intentra changelog`: generate changelog-ready output from the cached plan, including version/since metadata and linked ticket references.
- `intentra risk-report`: summarize risky commits, aggregate risk level, and sensitive areas touched from the cached plan.
- Lightweight ticket linking across `plan`, `apply`, `pr`, `release-notes`, `changelog`, and `risk-report` via `--ticket PROJ-123` and branch-name detection.
- New `engine/artifacts` package to keep release-facing report generation deterministic and reusable outside the CLI layer.
- Focused tests for artifact generation, ticket enrichment, and PR ticket propagation.

### Changed
- README rewritten to focus on the actual v0.6 command surface, shorter onboarding, and pointers to deeper docs instead of carrying the full architecture book inline.
- PR generation can now include ticket metadata from explicit flags, cached plan footers, or branch-name detection.
- Initial-commit plans now use the full prompt fingerprint, keeping cache invalidation behavior consistent with normal plans.
- CI and security workflows now use Go 1.25.8 to pick up standard-library security fixes flagged by `govulncheck`.

### Fixed
- `BuildContext` now captures staged files more reliably in brand-new repositories where `HEAD` does not exist yet.
- Synthetic diffs for untracked files now preserve empty new files instead of silently dropping them.
- Diff parsing now retains empty new files as synthetic hunks so they survive planning and replay.
- Import-graph commit ordering no longer relies on layer indexes that can drift during sort; positive ordering coverage was added.
- Batched planner fallback now respects `max_commits` when merge reconciliation fails, rather than concatenating every batch group unchecked.

## [0.5.7] -- Executor Resiliency

### Fixed
- **Executor Layer**: Hunks are now sorted by line number before patch assembly, preventing `git apply` failures when the LLM returns hunk IDs in arbitrary order.
- **Executor Layer**: File paths containing spaces are now properly quoted in generated patches using `strconv.Quote`, matching standard git escaping.
- **Executor Layer**: Mode-only hunks (no `@@` content) no longer emit empty `---`/`+++` blocks that could confuse `git apply`.

### Changed
- GoReleaser config updated to `version: 2` (required by `goreleaser-action@v7`).
- Replaced `if/else if` role checks with `switch` statements in `anthropic.go` and `openai.go` to satisfy `staticcheck` linter.

## [0.5.6] -- Bug Fixes

### Fixed
- **Context Layer**: Fixed diff parser to handle quoted file paths (files with spaces) and natively extract `OldMode` and `NewMode` from the diff header.
- **Planner Layer**: Fixed deduplication loop dropping duplicate hunks without checking if it left a group completely empty, causing slice indexing bugs later on.
- **Executor Layer**: Fixed git executor hardcoding executable permissions (`100644`). It now propagates `100755` permissions for new executable shell scripts correctly by using the parsed bits.
- **Reasoning Layer**: Fixed the "Retry Context Amnesia" issue where the fallback retry loop told the LLM it failed structurally, but didn't retain the Assistant's failed generation output over multiple attempts, breaking its ability to self-correct.

## [0.5.4] -- Release Infrastructure

### Added
- GoReleaser config for cross-platform binaries (Linux, macOS, Windows × amd64/arm64)
- Tag-triggered release workflow with smoke test
- Manual tag workflow (`workflow_dispatch`) with semver validation and branch/duplicate guards
- Homebrew tap distribution via `crvgilbertson/homebrew-intentra`
- Version injection via ldflags (no manual version bumps)
- `golangci-lint` in CI
- `govulncheck` security scanning (on push/PR + weekly schedule)
- Dependabot for Go modules and GitHub Actions
- Concurrency guard on release workflow

### Changed
- Consolidated CI from 4 jobs to 1 (same checks, lower minutes usage)
- Homebrew tap auto-skips pre-release tags (`skip_upload: auto`)

### Fixed
- `os.MkdirAll` unchecked return values in `git_executor_test.go` (errcheck)
- Removed unused `validateClusteringHardErrors` function

## [0.5.0] -- Import Graph, Risk, Atomicity & Plan --analyze

### Added

**Import-Graph Dependency Ordering**
- `BuildImportGraph` and `OrderCommitsByImportGraph` use `go list -json ./...` to derive package layers
- Planner tries import-graph ordering first; falls back to directory heuristics when not in a Go repo
- `PipelineTrace.OrderingStrategy` records `import_graph` or `fallback`
- `engine/context/import_graph.go` with `BuildImportGraph`, `OrderCommitsByImportGraph`, `computePackageLayers`

**Deterministic Risk Scoring**
- `engine.risk` config: `enabled`, `areas` (map of patterns + weight), `threshold_medium`, `threshold_high`
- `CommitUnit.Risk`: score, level, areas, signals
- `validators.ScoreCommitRisk` matches file paths to glob/prefix patterns for deterministic risk per commit

**Atomicity Profiles** (v0.5 = commit count policy; v0.6+ will add deterministic merge/split normalization)
- `engine.atomicity.profile`: `cohesive` (fewer commits), `balanced` (default), `strict` (more commits)
- `engine/atomicity/policy.go`: `EffectiveMaxCommits` adjusts max-commits cap per profile
- Cache invalidation when atomicity profile changes
- `explain` and snapshots include `atomicity_profile`

**Plan --analyze**
- `intentra plan --analyze`: detailed per-commit diagnostics (hunks, files, rationale, risk)
- `--analyze --json` for structured output

**Replay Fixture Corpus**
- Canonical fixture at `testdata/snapshots/v0.5/regression.json` (run `go run scripts/gen-fixture.go` to regenerate)
- `TestReplayFixtureV05` loads from repo root and verifies structural equivalence with mock replay

### Changed
- Snapshot config includes `atomicity_profile` for replay equivalence
- `apply` rejects cached plan when atomicity profile changes (no `--allow-stale-prompts` override)

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
