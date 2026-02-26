# Intentra

A deterministic, AI-powered code change reasoning engine. Intentra analyzes your uncommitted diffs, understands the intent behind each change, and produces structured, atomic commit plans that follow the [Conventional Commits](https://www.conventionalcommits.org/) specification.

---

## Table of Contents

- [How It Works](#how-it-works)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
  - [plan](#intentra-plan)
  - [apply](#intentra-apply)
  - [init](#intentra-init)
- [Configuration](#configuration)
- [Architecture](#architecture)
  - [Data Flow](#data-flow)
  - [Two-Pass Planning Pipeline](#two-pass-planning-pipeline)
  - [Safety Model](#safety-model)
  - [Project Structure](#project-structure)
- [Development](#development)
  - [Running Tests](#running-tests)
  - [Architecture Boundaries](#architecture-boundaries)
- [Roadmap](#roadmap)
- [License](#license)

---

## How It Works

1. **Context** -- Intentra runs `git diff HEAD` on your working tree and parses the output into individual hunks, capturing both staged and unstaged changes. Each hunk receives a stable, deterministic ID via `sha256(filePath + header + patch)`. Untracked files are detected separately and included as synthetic diffs. Files matching `ignore_patterns` in your config are excluded.

2. **Clustering** -- The hunks are sent to an LLM with strict JSON schema enforcement. The model groups related hunks by intent: which changes belong together in a single atomic commit. Supports OpenAI, Anthropic Claude, and any OpenAI-compatible endpoint (Ollama, vLLM, LM Studio). A live spinner shows elapsed time during LLM calls. If the model drops any hunks, a targeted rescue call recovers them (see [Orphan Hunk Recovery](#two-pass-planning-pipeline)).

3. **Messaging** -- For each cluster, the LLM generates Conventional Commit metadata: type, scope, subject, body, breaking change flags, and footers. All output is schema-validated. Both passes support configurable retries with correction prompts.

4. **Ordering** -- Commits are reordered by dependency: foundational changes (models, types, interfaces) are applied before higher-level consumers (planners, validators, CLI). This ensures the repository compiles at every commit boundary.

5. **Validation** -- The resulting `CommitPlan` is validated against business rules: every hunk is assigned exactly once, commit types and scopes are from the allowed set, subject length is within limits, breaking changes have proper footers, and more. Warnings are printed when multiple commits touch the same file.

6. **Caching** -- The validated plan is saved to `.intentra/plan.json` with a diff fingerprint (SHA256 of all hunk IDs). If you run `apply` without changing your working tree, the cached plan is reused instantly -- no second LLM call. If the diff changes, the stale plan is detected and a fresh one is generated.

7. **Execution** -- Only when you explicitly pass `--yes` does Intentra touch git. It snapshots the current HEAD and index, resets to a clean state, then stages each commit's hunks via `git apply --cached` and commits them. If anything fails, all commits are rolled back and the index is restored to its pre-apply state. No partial applies. No data corruption.

---

## Prerequisites

- **Go 1.22+**
- **Git** installed and available on `PATH`
- An API key for your chosen provider:
  - **OpenAI**: set `OPENAI_API_KEY`
  - **Anthropic**: set `ANTHROPIC_API_KEY`
  - **Gemini**: set `GEMINI_API_KEY`
  - **Ollama**: no key needed (runs locally)

---

## Installation

```bash
go install github.com/crvgilbertson/intentra@latest
```

Or build from source:

```bash
git clone https://github.com/crvgilbertson/intentra.git && cd intentra
go build -o intentra .
```

---

## Quick Start

```bash
# 1. Initialize configuration (creates .intentra/ directory)
intentra init

# 2. Make some code changes in your repo...

# 3. Preview the commit plan (saved to .intentra/plan.json)
intentra plan

# 4. See the raw JSON plan
intentra plan --json

# 5. Apply the cached plan (dry-run by default -- no LLM call if diff unchanged)
intentra apply

# 6. Actually apply the commits
intentra apply --yes
```

---

## Examples

### Splitting a messy diff into clean commits

You've been working for a while and have a mix of unrelated changes in your working tree -- a bug fix, a new feature, and some refactoring:

```bash
$ git diff --stat
 src/auth/jwt.go       | 42 +++++++++++++++++++++++++++
 src/auth/jwt_test.go  | 18 ++++++++++++
 src/api/handler.go    |  8 ++----
 src/api/middleware.go  | 15 ++++++++--
 src/core/utils.go     | 23 ++++++---------
 5 files changed, 81 insertions(+), 25 deletions(-)
```

Run `intentra plan` to see how it would split these into atomic commits:

```
$ intentra plan

Found 7 hunk(s) across the diff.
  ⠹ Generating commit plan... 12s

  ┌─────────────────────────────────────────────────────┐
  │ Commit Plan  3 commit(s)
  │ base: e4a91bc3d1f2  •  engine v0.2.0
  └─────────────────────────────────────────────────────┘

  1 feat(auth): add JWT token validation and refresh logic
    3 hunk(s)  →  src/auth/jwt.go, src/auth/jwt_test.go

  2 fix(api): handle nil user in request middleware
    2 hunk(s)  →  src/api/handler.go, src/api/middleware.go

  3 refactor(core): simplify utility string helpers
    2 hunk(s)  →  src/core/utils.go

  ─────────────────────────────────────────────────────
Plan saved to .intentra/plan.json
```

Three clean, atomic commits -- each with a single concern, ordered by dependency. When you're happy with the plan:

```
$ intentra apply --yes

Using cached plan from .intentra/plan.json (diff unchanged).
Applying 3 commit(s)...
✓ Successfully applied 3 commit(s).
```

No second LLM call -- the cached plan is reused since the diff hasn't changed.

```
$ git log --oneline -3

a7f2d1c refactor(core): simplify utility string helpers
b3e8f4a fix(api): handle nil user in request middleware
c9d1a2e feat(auth): add JWT token validation and refresh logic
```

### Previewing the JSON plan

Use `--json` to get the full structured plan, useful for CI pipelines or custom tooling:

```
$ intentra plan --json
```

```json
{
  "tool_version": "0.2.0",
  "base_ref": "e4a91bc",
  "diff_fingerprint": "3a7f2b1c9d4e8f0a...",
  "style": {
    "convention": "conventional_commits",
    "max_subject_len": 72,
    "allowed_types": ["feat", "fix", "refactor", "perf", "docs", "test", "chore"],
    "scopes": ["auth", "api", "core"]
  },
  "commits": [
    {
      "id": "c1",
      "type": "feat",
      "scope": "auth",
      "subject": "add JWT token validation and refresh logic",
      "body": "Implement token validation middleware and automatic refresh\nfor expired tokens using the configured secret key.",
      "breaking": false,
      "hunks": ["a1b2c3...", "d4e5f6...", "71g8h9..."]
    }
  ]
}
```

### Safe dry-run by default

`intentra apply` without `--yes` shows the plan but makes no changes:

```
$ intentra apply

Using cached plan from .intentra/plan.json (diff unchanged).
...
Dry-run mode. Pass --yes to apply.
```

Your working tree is untouched. The cached plan is loaded instantly. Review the plan, then run with `--yes` when ready.

### Using with a local model

Run Intentra completely offline with Ollama:

```bash
# Pull a model
ollama pull qwen3-coder:32b

# Configure in .intentra/config.yaml:
# ai:
#     provider: ollama
#     model: qwen3-coder:32b

# Plan as usual -- no API key needed, all local
intentra plan
```

---

## Commands

### `intentra plan`

Analyzes the current `git diff HEAD` (staged + unstaged changes) and generates a structured commit plan using AI reasoning. The plan is saved to `.intentra/plan.json` so that a subsequent `apply` can reuse it without calling the LLM again.

```
Usage:
  intentra plan [flags]

Flags:
      --json   Output raw CommitPlan JSON instead of human-readable summary
```

### `intentra apply`

Applies a commit plan to the repository. If a cached plan exists from a previous `plan` run and the diff hasn't changed, it is reused instantly (no LLM call). Otherwise, a new plan is generated. **Defaults to dry-run** -- you must pass `--yes` to actually create commits.

```
Usage:
  intentra apply [flags]

Flags:
      --yes    Actually apply commits (default is dry-run)
```

**Plan caching behavior:**

- If `.intentra/plan.json` exists and the diff fingerprint matches: `Using cached plan (diff unchanged).`
- If the file exists but the diff has changed: `Cached plan is stale (diff changed). Re-planning...`
- If no cached plan exists: `No cached plan found.`

After a successful apply, the cache file is automatically deleted.

**With `--yes`**, Intentra:

1. Checks if the current branch is protected (configurable via `protected_branches`)
2. Snapshots the current HEAD and index state
3. Resets the index to HEAD for clean patch application
4. For each commit: writes a patch file, stages with `git apply --cached`, then `git commit` (optionally with `-S` for GPG signing)
5. If any step fails: rolls back all commits and restores the index to the snapshot
6. On success: deletes the cached plan and reports which commits were created

### `intentra init`

Creates the `.intentra/` directory with a default `config.yaml` and a `.gitignore` that ignores ephemeral files.

```
Usage:
  intentra init
```

Output:
```
Created .intentra/ with default configuration.
  .intentra/config.yaml  — project config (commit to repo)
  .intentra/.gitignore — ignores ephemeral files
```

The config file is meant to be committed to your repo so team members share the same settings. The plan cache is automatically gitignored.

### Global Flags

```
      --config string   Path to config file (default ".intentra/config.yaml")
      --version         Print version
```

If `.intentra/config.yaml` is not found, Intentra checks for a legacy `.engine.yaml` and prints a migration notice.

---

## Configuration

Intentra is configured via `.intentra/config.yaml` in your project root. Run `intentra init` to generate the default:

```yaml
style:
    convention: conventional_commits
    max_subject_len: 72
    allowed_types:
        - feat
        - fix
        - refactor
        - perf
        - docs
        - test
        - chore
    scopes: []
    scope_required: false
    body_required: false

ai:
    provider: openai
    model: gpt-4.1
    temperature: 0.2
    max_diff_kb: 500
    max_retries: 1
    timeout: 120

engine:
    strict_mode: true
    protected_branches:
        - main
        - master
    max_commits: 20
    ignore_patterns: []
    sign_commits: false
```

### Configuration Reference

| Section | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| `style` | `convention` | string | `conventional_commits` | Commit convention to enforce |
| `style` | `max_subject_len` | int | `72` | Maximum subject line length |
| `style` | `allowed_types` | []string | `[feat, fix, ...]` | Permitted commit types |
| `style` | `scopes` | []string | `[]` | Permitted scopes (empty = any) |
| `style` | `scope_required` | bool | `false` | Require a scope on every commit |
| `style` | `body_required` | bool | `false` | Require a body on every commit |
| `ai` | `provider` | string | `openai` | LLM provider: `openai`, `anthropic`, `gemini`, or `ollama` |
| `ai` | `model` | string | `gpt-4.1` | Model name (provider-specific) |
| `ai` | `temperature` | float | `0.2` | LLM temperature (0.1--0.2 recommended) |
| `ai` | `max_diff_kb` | int | `500` | Maximum diff size in KB before aborting |
| `ai` | `base_url` | string | *(empty)* | Custom API base URL (for Azure, proxies, or self-hosted endpoints) |
| `ai` | `max_retries` | int | `1` | Number of LLM retry attempts on validation failure |
| `ai` | `timeout` | int | `120` | Timeout in seconds for the entire planning phase |
| `engine` | `strict_mode` | bool | `true` | Enable strict validation |
| `engine` | `protected_branches` | []string | `[main, master]` | Branches that `apply --yes` refuses to commit to |
| `engine` | `max_commits` | int | `20` | Maximum number of commits per plan |
| `engine` | `ignore_patterns` | []string | `[]` | File glob patterns to exclude from the diff |
| `engine` | `sign_commits` | bool | `false` | GPG-sign commits with `git commit -S` |
| `engine` | `auto_push` | bool | `false` | Automatically push to remote after successful apply (handles `--set-upstream` for new branches) |

If no config file is found, Intentra uses these defaults automatically.

### Provider Setup

**OpenAI** (default):

```yaml
ai:
    provider: openai
    model: gpt-4.1          # or any OpenAI model
```

**Anthropic Claude**:

```yaml
ai:
    provider: anthropic
    model: claude-sonnet-4-5-20250929   # or any Claude model
```

**Google Gemini**:

```yaml
ai:
    provider: gemini
    model: gemini-3.1-pro    # or any Gemini model
```

**Ollama** (local, no API key needed):

```yaml
ai:
    provider: ollama
    model: llama3            # or any model you've pulled
```

**Azure OpenAI** or any OpenAI-compatible endpoint:

```yaml
ai:
    provider: openai
    model: my-deployment-name
    base_url: https://my-instance.openai.azure.com/openai/deployments/my-deployment
```

**vLLM / LM Studio** (local OpenAI-compatible server):

```yaml
ai:
    provider: openai
    model: my-local-model
    base_url: http://localhost:8000/v1
```

### Model Reference

The `model` field is passed directly to the provider API -- any model your account has access to will work. Here are some common choices:

**OpenAI**

| Model | Notes |
|-------|-------|
| `gpt-5.2` | Latest flagship. Best quality for complex changesets. |
| `gpt-5-mini` | Smaller, faster GPT-5 variant. Good cost/quality balance. |
| `gpt-4.1` | Default. Proven, reliable, still available. |
| `gpt-4.1-mini` | Cheaper and faster. Good for smaller diffs. |
| `gpt-4.1-nano` | Cheapest OpenAI option. |

**Anthropic Claude**

| Model | Notes |
|-------|-------|
| `claude-opus-4-6-latest` | Most intelligent. Best for large, complex changesets. |
| `claude-sonnet-4-6-latest` | Best speed/intelligence balance. Recommended starting point. |
| `claude-haiku-4-5-latest` | Fastest and cheapest Claude model. |

**Google Gemini**

| Model | Notes |
|-------|-------|
| `gemini-3.1-pro` | Latest. Strong reasoning, 1M token context. |
| `gemini-2.5-pro` | Proven, excellent for code and STEM tasks. |
| `gemini-2.5-flash` | Fast and cheap. Good for smaller diffs. |

**Ollama (local)**

| Model | Notes |
|-------|-------|
| `qwen3-coder:32b` | Top pick for coding. Stable tool calling, strong reasoning. |
| `glm4.7-flash` | Strong 30B-class model. Precise and reliable. |
| `deepseek-r1` | Excellent reasoning and code understanding. |
| `llama3.3:70b` | Meta's latest. Needs 48GB+ VRAM. |
| `qwen3-coder:14b` | Lighter option for 8-16GB VRAM. |

New models work immediately -- no Intentra update required. Just change the `model` string in your config.

---

## Architecture

### Data Flow

```
CLI Command (plan / apply)
    |
    v
Context Builder ---- git diff HEAD, git ls-files --others ---> EngineContext { Hunks, RecentCommits, Config }
    |                 (staged + unstaged + untracked)
    |                 filter by ignore_patterns
    v
Plan Cache Check ---- .intentra/plan.json + diff fingerprint
    |                  match? --> reuse cached plan (skip LLM)
    |                  stale/missing? --> continue to reasoning
    v
Reasoning Engine --- LLM structured output (OpenAI / Anthropic / Ollama) ---> JSON (schema-validated)
    |                 with configurable retries and timeout
    v
Commit Planner ---- two-pass (cluster + message) + dependency reorder ---> CommitPlan
    |                 max_commits enforced
    v
Validator ---- business rules + file overlap warnings ---> pass / error
    |
    v
Plan Cache Save ---- .intentra/plan.json (for reuse by apply)
    |
    v
Executor ---- snapshot HEAD + index, reset index, git apply --cached + git commit ---> commits created
              on failure: git reset --soft + read-tree (full rollback, no orphaned commits)
```

### Two-Pass Planning Pipeline

Intentra uses a two-pass approach for deterministic commit planning:

**Pass 1 -- Intent Clustering**

The LLM receives all hunks (file path, header, patch content) and groups them by logical intent. The output is a strict JSON schema: an array of groups, each containing a stable group ID and a list of hunk IDs. Validation ensures no duplicates and no unknown IDs. The number of groups is capped by `max_commits`.

**Orphan Hunk Recovery** -- With large diffs (20+ hunks), LLMs occasionally drop one or more hunk IDs from their response. Intentra handles this with a three-tier recovery strategy:

1. **Prevention** -- The prompt includes an explicit hunk count and a numbered checklist of all IDs at the end of the input, instructing the model to cross-reference before responding.
2. **Targeted rescue call** -- If any hunks are still missing after the main clustering call, a small, focused LLM call is made with *only* the orphaned hunks and the existing group descriptions. The model assigns each orphan to the most semantically appropriate group. This is cheap (tiny context) and accurate (the LLM decides, not a heuristic).
3. **File-path fallback** -- If the rescue call also fails (e.g., timeout or API error), orphans are assigned deterministically to the group that already contains the most hunks from the same file. If no file match exists, the largest group is used.

This means Intentra will produce a valid plan even when the LLM is imperfect. The trade-off: tier 3 uses file proximity rather than semantic understanding, so an orphan may land in a sub-optimal commit. You can always re-run `plan` to get a fresh clustering.

**Pass 2 -- Message Generation**

For each cluster, the LLM generates Conventional Commit metadata: type, scope, subject, body, breaking flag, and footers. The output is again schema-validated. Recent commit history from the repo is provided as style reference.

Both passes use the generic `CallWithRetry[T]` mechanism: if the LLM output fails validation, it retries up to `max_retries` times with a correction prompt. If all retries fail, the operation aborts cleanly.

**Post-Processing -- Dependency Ordering**

After the LLM produces the plan, commits are deterministically reordered by package dependency. Each commit's hunks are inspected to determine which layer of the codebase they touch (models → context → reasoning → planners → validators → executors → cmd). Commits touching foundational layers are applied first, ensuring the repository compiles at every commit boundary. This is a pure, deterministic step -- no LLM involved.

### Safety Model

Intentra enforces a strict trust model:

- **Plan never mutates** -- `intentra plan` is read-only. It collects the diff and reasons about it but never changes any files or git state.
- **Dry-run by default** -- `intentra apply` without `--yes` shows the plan and exits.
- **Protected branch check** -- `apply --yes` refuses to commit to branches listed in `protected_branches` (default: `main`, `master`).
- **Atomic apply with full rollback** -- Each commit is staged via `git apply --cached` and committed individually. If any step fails, the entire operation is rolled back: all commits are undone with `git reset --soft`, and the index is restored to its pre-apply state. No partial applies. No orphaned commits.
- **Clean index isolation** -- Before applying, the index is reset to HEAD. Pre-existing staged changes cannot leak into commits.
- **No history rewriting** -- No rebase, no amend, no force-push. Intentra only creates new commits.
- **Plan caching** -- `plan` saves the result to `.intentra/plan.json` with a diff fingerprint. `apply` reuses it if the diff is unchanged, avoiding redundant LLM calls and ensuring the same plan is applied that was reviewed. If the diff changes, the stale plan is automatically discarded.
- **Strict layer separation** -- The reasoning layer (LLM) cannot execute git commands. The executor layer cannot call the LLM. This is enforced architecturally, not by convention.

### Project Structure

```
intentra/
├── main.go                          Entry point
├── go.mod
│
├── .intentra/                       Runtime directory (created by intentra init)
│   ├── config.yaml                  Project config (commit to repo)
│   ├── plan.json                    Cached plan (gitignored)
│   └── .gitignore                   Ignores ephemeral files
│
├── cmd/                             CLI layer (Cobra commands, no business logic)
│   ├── root.go                      Root command, --config flag, legacy config fallback
│   ├── plan.go                      intentra plan [--json], plan caching
│   ├── apply.go                     intentra apply [--yes], cache-aware plan resolution
│   ├── init.go                      intentra init (creates .intentra/ directory)
│   └── ui/
│       └── styles.go                Colored output, spinner, plan summary (fatih/color)
│
├── config/                          Configuration loading and defaults
│   └── config.go                    EngineConfig, YAML load/write, directory helpers
│
├── internal/
│   └── version.go                   Version constant
│
└── engine/
    ├── context/                     Git state collection, pure diff parsing
    │   ├── diff_parser.go           Unified diff -> []Hunk (new/deleted/renamed/mode-change aware)
    │   ├── diff_parser_test.go
    │   ├── hunk_hasher.go           sha256-based stable hunk IDs
    │   ├── hunk_hasher_test.go
    │   └── repo_context.go          BuildContext(): git diff HEAD + untracked + ignore filtering
    │
    ├── models/                      Shared domain types (no logic)
    │   ├── hunk.go                  Hunk { HunkID, FilePath, Header, Patch, NewFile, DeletedFile, ... }
    │   ├── commit_plan.go           CommitPlan, CommitUnit, CommitStyle, DiffFingerprint, Plan interface
    │   └── change_intent.go         Reserved for future capabilities
    │
    ├── reasoning/                   LLM abstraction (NO git calls)
    │   ├── client.go                ReasoningEngine interface
    │   ├── openai.go                OpenAI + OpenAI-compatible implementation
    │   ├── anthropic.go             Anthropic Claude implementation (tool_use)
    │   ├── factory.go               Provider selection from config
    │   └── retry.go                 Generic CallWithRetry[T] with configurable retries
    │
    ├── planners/                    Plan generation (NO git calls)
    │   ├── planner.go               Planner interface
    │   ├── commit_planner.go        Two-pass CommitPlanner with timeout and max_commits
    │   ├── commit_schema.go         JSON schemas for clustering + messaging
    │   └── commit_planner_test.go
    │
    ├── validators/                  Business rule validation (NO git calls)
    │   ├── commit_validator.go      ValidateCommitPlan(), WarnFileOverlap()
    │   └── commit_validator_test.go
    │
    └── executors/                   Git operations (NO LLM calls)
        ├── executor.go              Executor interface
        ├── git_executor.go          Snapshot, apply, commit, full rollback, GPG signing
        └── git_executor_test.go     Integration tests: apply, dry-run, rollback, delete, staged
```

---

## Development

### Building

```bash
go build -o intentra .
```

### Running Tests

```bash
# All tests
go test ./...

# Verbose output
go test ./... -v

# Specific package
go test ./engine/context/ -v
go test ./engine/validators/ -v
go test ./engine/executors/ -v
```

The test suite includes:

| Package | Tests | Coverage |
|---------|-------|----------|
| `engine/context` | 12 | Diff parsing (single file, multi-file, binary skip, renames, deleted files, mode-only changes, mode+content changes, empty), hunk hashing (stability, uniqueness, format) |
| `engine/planners` | 9 | Full planner flow with mocked LLM, empty hunks, clustering validation (missing/duplicate/unknown hunks), messaging validation (missing groups), commit dependency reordering, package layer scoring |
| `engine/validators` | 13 | Valid plan, missing hunk, duplicate hunk, unknown hunk, bad type, bad scope, subject too long, trailing period, uppercase subject, breaking without footer, breaking with footer, empty scopes, multiple errors |
| `engine/executors` | 6 | Apply single commit, dry-run, fail-and-restore, partial apply rollback, deleted file commit, staged changes included |

All LLM calls are mocked in tests -- no network access required.

### Architecture Boundaries

These boundaries are enforced by design and must not be violated:

| Layer | Can call git? | Can call LLM? |
|-------|:---:|:---:|
| `cmd/` | No | No (delegates to engine) |
| `engine/context/` | Yes (read-only) | No |
| `engine/reasoning/` | No | Yes |
| `engine/planners/` | No | No (uses ReasoningEngine) |
| `engine/validators/` | No | No |
| `engine/executors/` | Yes (read + write) | No |
| `config/` | No | No |

---

## Roadmap

Intentra is designed as an extensible platform. The `Planner` interface is generic -- commit planning is the first implementation. Future capabilities will follow the same pattern: context -> reasoning -> plan -> validate -> execute.

The individual features (v0.1--v0.4) drive developer adoption. The team features (v0.5--v0.7) drive enterprise value. v1.0.0 is the stability promise.

### v0.1.0 -- Commit Planning (Released)

- Atomic commit planning from uncommitted diffs using AI reasoning
- Two-pass pipeline: intent clustering + message generation
- Plan caching with diff fingerprint for instant reuse
- Dependency-aware commit ordering (foundational packages first)
- Multi-provider LLM support: OpenAI, Anthropic, Gemini, Ollama
- Colored terminal output with type-coded commit summaries
- Protected branch detection, dry-run by default, index restore on failure

### v0.2.0 -- Robustness & Configuration (Released)

- **Staged change capture**: `git diff HEAD` now includes both staged and unstaged changes
- **Full rollback on partial failure**: if commit N fails, all prior commits are undone — no orphaned commits
- **Clean index isolation**: index is reset to HEAD before apply, preventing staged change leakage
- **Deleted file handling**: parser and executor correctly detect and apply file deletions
- **Rename detection**: `rename from`/`rename to` headers are parsed and preserved in patches
- **File mode changes**: `old mode`/`new mode` detected; synthetic hunks for mode-only changes
- **File overlap warnings**: validator warns when multiple commits touch the same file
- **`.intentra/` directory**: all runtime files consolidated under `.intentra/` with per-directory `.gitignore`
- **Legacy config fallback**: automatic detection and migration notice for old `.engine.yaml`
- **New config options**: `max_retries`, `timeout`, `max_commits`, `ignore_patterns`, `sign_commits`, `scope_required`, `body_required`
- **Live spinner**: elapsed-time indicator during LLM calls
- **Deterministic patch output**: files sorted alphabetically in generated patches

### v0.3.0 -- Streaming & GitHub Integration

- **LLM response streaming**: real-time token-by-token output with live progress, proving the connection is alive and enabling early abort on bad output
- **Hunk summarization**: send concise summaries instead of full patches during clustering to reduce token usage on large diffs
- `intentra pr` -- create a feature branch, push, and open a GitHub PR with AI-generated title and description
- `intentra push` -- push the current branch with smart remote detection
- Remote branch protection awareness via GitHub API
- `gh` CLI integration for authentication and API access

### v0.4.0 -- Commit Intelligence

- Risk scoring per commit (sensitive areas: auth, database, payments, config)
- Entanglement detection (warning when a commit touches unrelated subsystems)
- Confidence score for clustering quality (how sure is the model about the grouping?)
- `intentra plan --analyze` flag for detailed per-commit breakdown

### v0.5.0 -- PR Intelligence

- Auto-generate PR descriptions from the structured commit plan
- Suggest PR splits when a branch has too many unrelated changes
- Review checklist generation based on what files and subsystems changed
- `intentra review` command for self-review before submitting

### v0.6.0 -- CI/CD Integration

- Official GitHub Actions action (`uses: crvgilbertson/intentra-action`)
- Run `intentra plan --json` in CI to validate commit hygiene on every PR
- Block merges if commits don't follow conventions or fail risk thresholds
- Post plan summary as a PR comment for reviewer context

### v0.7.0 -- Team Configuration

- Shareable config presets for common setups (monorepo, library, microservice)
- Scope auto-detection from directory structure and module boundaries
- Team-wide config in repo root with personal overrides
- `intentra config check` for config validation and drift detection

### v1.0.0 -- Stable Platform

- Stability promise: CLI flags, config format, and JSON output schema will not break
- Plugin interface for custom planners and validators
- Parallel hunk analysis for large diffs
- `intentra upgrade` for self-updating to latest release
- Comprehensive documentation site

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
