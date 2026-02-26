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
  - [push](#intentra-push)
  - [pr](#intentra-pr)
  - [init](#intentra-init)
- [Configuration](#configuration)
- [Architecture](#architecture)
  - [Data Flow](#data-flow)
  - [Two-Pass Planning Pipeline](#two-pass-planning-pipeline)
  - [Scaling for Large Diffs](#scaling-for-large-diffs)
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

2. **Clustering** -- The hunks are sent to an LLM with strict JSON schema enforcement. The model groups related hunks by intent: which changes belong together in a single atomic commit. Supports OpenAI, Anthropic Claude, and any OpenAI-compatible endpoint (Ollama, vLLM, LM Studio). Large patches are automatically summarized (first/last N lines) to reduce token usage -- configurable via `max_hunk_lines`. A phased progress indicator shows the current stage ("Clustering N hunks...", "Generating commit messages...") with elapsed time. If the model drops any hunks, a targeted rescue call recovers them (see [Orphan Hunk Recovery](#two-pass-planning-pipeline)). Intentra automatically scales its clustering strategy based on diff size (see [Scaling for Large Diffs](#scaling-for-large-diffs)).

3. **Messaging** -- For each cluster, the LLM generates Conventional Commit metadata: type, scope, subject, body, breaking change flags, and footers. All output is schema-validated. Both passes support configurable retries with correction prompts.

4. **Ordering** -- Commits are reordered by dependency: foundational changes (models, types, interfaces) are applied before higher-level consumers (planners, validators, CLI). This ensures the repository compiles at every commit boundary.

5. **Validation** -- The resulting `CommitPlan` is validated against business rules: every hunk is assigned exactly once, commit types and scopes are from the allowed set, subject length is within limits, breaking changes have proper footers, and more. A confidence score assesses overall plan quality based on file overlap, hunk entanglement, and commit spread -- displayed after the plan summary.

6. **Caching** -- The validated plan is saved to `.intentra/plan.json` with a diff fingerprint (SHA256 of all hunk IDs). If you run `apply` without changing your working tree, the cached plan is reused instantly -- no second LLM call. If the diff changes, the stale plan is detected and a fresh one is generated.

7. **Execution** -- Only when you explicitly pass `--yes` does Intentra touch git. It snapshots the current HEAD and index, resets to a clean state, then validates each patch with `git apply --check` before staging with `git apply --cached` and committing. Between commits, working tree files are fingerprinted (OS-level mtime/size) to detect external modifications. If anything fails -- or if you press Ctrl+C -- all commits are rolled back and the index is restored to its pre-apply state. No partial applies. No data corruption.

---

## Prerequisites

- **Go 1.22+**
- **Git** installed and available on `PATH`
- **`gh` CLI** (optional) -- required for `intentra pr` and remote branch protection checks. Install: https://cli.github.com
- An API key for your chosen provider (Intentra validates this before making any LLM calls and will tell you exactly which variable is missing):

  | Provider | Environment Variable | Example |
  |----------|---------------------|---------|
  | OpenAI (default) | `OPENAI_API_KEY` | `export OPENAI_API_KEY=sk-...` |
  | Anthropic | `ANTHROPIC_API_KEY` | `export ANTHROPIC_API_KEY=sk-ant-...` |
  | Gemini | `GEMINI_API_KEY` | `export GEMINI_API_KEY=AI...` |
  | Ollama | (none) | Runs locally -- no key needed |

  API keys are read from environment variables only -- they are never stored in config files, so there is no risk of committing secrets.

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

# 7. Push the branch (auto-detects upstream)
intentra push

# 8. Open a PR (title/body derived from the commit plan)
intentra pr
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
  │ base: e4a91bc3d1f2  •  engine v0.3.0
  └─────────────────────────────────────────────────────┘

  1 feat(auth): add JWT token validation and refresh logic
    3 hunk(s)  →  src/auth/jwt.go, src/auth/jwt_test.go

  2 fix(api): handle nil user in request middleware
    2 hunk(s)  →  src/api/handler.go, src/api/middleware.go

  3 refactor(core): simplify utility string helpers
    2 hunk(s)  →  src/core/utils.go

  ─────────────────────────────────────────────────────

  Confidence: high (100%)
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
  "tool_version": "0.3.0",
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

1. **Pre-flight check**: aborts if the repo is mid-merge, mid-rebase, mid-cherry-pick, mid-bisect, or has unmerged paths
2. Checks if the current branch is protected (configurable via `protected_branches`)
3. **Hook detection**: prints an informational warning if husky, pre-commit, or git hooks are detected (suggests `skip_hooks: true` if they cause issues)
4. Snapshots the current HEAD and index state
5. Resets the index to HEAD for clean patch application
6. For each commit: writes a patch file, stages with `git apply --cached`, then `git commit` (optionally with `-S` for GPG signing, `--no-verify` if `skip_hooks`, `--author` if `commit_author` is set)
7. If any step fails: rolls back all commits and restores the index to the snapshot
8. On success: deletes the cached plan. If `auto_push` is enabled, pushes to the configured remote (validates the remote exists first, handles `--set-upstream` for new branches)

### `intentra push`

Pushes the current branch to the configured remote, automatically setting the upstream if needed. Checks for remote branch protection via the GitHub API before pushing (requires `gh` CLI).

```
Usage:
  intentra push [flags]

Flags:
      --remote string   Override remote name (default: config remote_name)
```

**Behavior:**

1. Detect the current branch
2. Validate the remote exists
3. If `gh` is available, check GitHub branch protection and warn if PRs are required
4. If the branch has an upstream: `git push`
5. If no upstream: `git push --set-upstream <remote> <branch>`

### `intentra pr`

Creates a GitHub pull request from the current branch. Pushes the branch first, then generates a PR with title and body derived from the cached commit plan. Requires the `gh` CLI to be installed and authenticated.

```
Usage:
  intentra pr [flags]

Flags:
      --title string   PR title (auto-generated if omitted)
      --base string    Base branch (default: first protected branch from config, or main)
      --draft          Create as draft PR
```

**PR content generation:**

- **Single commit**: PR title is the commit's full subject (`type(scope): subject`)
- **Multiple commits**: title is `N changes: feat(scope), fix(scope), ...`
- **Body**: lists each commit with its type, scope, subject, and hunk count
- **Fallback**: if no cached plan exists, uses `git log --oneline` from the branch

No LLM call is needed -- the structured commit plan already contains everything.

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
    max_hunk_lines: 50

engine:
    strict_mode: true
    protected_branches:
        - main
        - master
    max_commits: 20
    ignore_patterns: []
    sign_commits: false
    auto_push: false
    remote_name: origin
    commit_author: ""
    skip_hooks: false
    batch_threshold: 40
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
| `ai` | `max_hunk_lines` | int | `50` | Truncate patches longer than this in the clustering prompt (0 = no truncation). Reduces token usage on large diffs. |
| `engine` | `strict_mode` | bool | `true` | Enable strict validation |
| `engine` | `protected_branches` | []string | `[main, master]` | Branches that `apply --yes` refuses to commit to |
| `engine` | `max_commits` | int | `20` | Maximum number of commits per plan |
| `engine` | `ignore_patterns` | []string | `[]` | File glob patterns to exclude from the diff |
| `engine` | `sign_commits` | bool | `false` | GPG-sign commits with `git commit -S` |
| `engine` | `auto_push` | bool | `false` | Automatically push to remote after successful apply (handles `--set-upstream` for new branches) |
| `engine` | `remote_name` | string | `"origin"` | Git remote to push to when `auto_push` is enabled |
| `engine` | `commit_author` | string | `""` | Override commit author (e.g., `"Name <email>"`) — empty uses git default |
| `engine` | `skip_hooks` | bool | `false` | Skip pre-commit hooks with `--no-verify` |
| `engine` | `batch_threshold` | int | `40` | Hunk/file-unit count above which clustering switches strategy (file-level grouping, then batched clustering). See [Scaling for Large Diffs](#scaling-for-large-diffs). |

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

### Scaling for Large Diffs

Intentra automatically selects a clustering strategy based on diff size, controlled by `batch_threshold` (default: 40):

| Diff size | Strategy | What happens |
|-----------|----------|-------------|
| ≤ threshold hunks | **Direct** | Each hunk is sent individually with compact IDs (h1, h2, ...) |
| > threshold hunks, ≤ threshold files | **File-level** | Hunks are grouped by file into units (f1, f2, ...). The LLM clusters file units, then they're expanded back to hunk-level assignments. |
| > threshold files | **Batched** | File units are split into directory-proximate batches. Each batch is clustered independently, then an LLM merge pass reconciles groups across batches. Falls back to concatenation if merge fails. |

**Compact prompt IDs** -- In all strategies, 64-character SHA256 hunk IDs are replaced with short tokens (h1, h2, f1, f2, ...) in the LLM prompt. This saves ~4,500 tokens at 100 hunks and dramatically reduces duplicate/omission errors. Real IDs are mapped back after the LLM responds.

**Duplicate repair** -- If the LLM assigns the same hunk to multiple groups (common at high hunk counts), duplicates are silently deduplicated (first assignment wins) rather than treated as a hard error. Missing hunks are then recovered via the standard [orphan recovery](#two-pass-planning-pipeline) pipeline.

### Safety Model

Intentra enforces a strict trust model:

- **Pre-flight checks** -- Before apply, Intentra verifies the repo is in a safe state: not mid-merge, mid-rebase, mid-cherry-pick, or mid-bisect, and has no unmerged paths. If any unsafe state is detected, apply aborts with a clear message and resolution hint.
- **Plan never mutates** -- `intentra plan` is read-only. It collects the diff and reasons about it but never changes any files or git state.
- **Dry-run by default** -- `intentra apply` without `--yes` shows the plan and exits.
- **Protected branch check** -- `apply --yes` refuses to commit to branches listed in `protected_branches` (default: `main`, `master`).
- **Patch pre-check** -- Each commit's patch is validated with `git apply --check` before staging, catching malformed patches before they can cause a partial apply. Empty patches are rejected immediately.
- **Atomic apply with full rollback** -- Each commit is staged via `git apply --cached` and committed individually. If any step fails, the entire operation is rolled back: all commits are undone with `git reset --soft`, and the index is restored to its pre-apply state. No partial applies. No orphaned commits.
- **Graceful interrupt handling** -- Pressing Ctrl+C (SIGINT/SIGTERM) during `apply` triggers a clean rollback of all commits applied so far, just like a step failure. The process exits with an error, not a corrupt state.
- **Working tree drift detection** -- Between each commit, Intentra fingerprints the files touched by the plan using OS-level metadata (size + mtime). If any file is modified externally mid-apply, the operation aborts with rollback. This detection is immune to git index changes caused by Intentra's own operations.
- **Clean index isolation** -- Before applying, the index is reset to HEAD. Pre-existing staged changes cannot leak into commits.
- **Cached plan structural validation** -- When loading a cached plan from `.intentra/plan.json`, the plan is validated against business rules before use. A stale or corrupted plan file is rejected with a clear error.
- **Hook awareness** -- Intentra detects common hook managers (husky, pre-commit framework, git hooks) and warns before applying. If a hook rejects a commit, the error message suggests `skip_hooks: true`. When `skip_hooks` is enabled, `--no-verify` is passed to `git commit`.
- **Remote validation** -- When `auto_push` is enabled, Intentra verifies the configured remote exists before attempting to push, avoiding cryptic git errors.
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
│   ├── push.go                      intentra push [--remote], smart upstream detection
│   ├── pr.go                        intentra pr [--title, --base, --draft], plan-derived PR
│   ├── gh.go                        gh CLI helpers (available, authenticated, repo info, protection)
│   ├── githelpers.go                Shared git operations (push, branch, remote, preflight)
│   ├── init.go                      intentra init (creates .intentra/ directory)
│   └── ui/
│       └── styles.go                Colored output, phased spinner, plan summary (fatih/color)
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
    │   ├── diff_parser_fuzz_test.go  Fuzz tests for diff parsing edge cases
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
    │   ├── commit_validator_test.go
    │   ├── confidence.go            PlanConfidence scoring (overlap, entanglement, spread)
    │   └── confidence_test.go
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

The test suite includes 74 tests across 5 packages:

| Package | Tests | Coverage |
|---------|-------|----------|
| `engine/context` | 16 | Diff parsing (single file, multi-file, binary skip, renames, deleted files, mode-only changes, mode+content changes, empty, hunk ID uniqueness), hunk hashing (stability, uniqueness, format), fuzz tests (diff parsing, file path extraction, hunk splitting) |
| `engine/planners` | 27 | Full planner flow with mocked LLM, empty hunks, clustering validation (missing/duplicate/unknown hunks), messaging validation (missing groups), commit dependency reordering, package layer scoring, hunk summarization (no-op, disabled, truncated), compact ID mapping (build, remap, validation, unknowns, too many groups), file-level pre-grouping, file unit expansion (pure and mixed groups), batch splitting (count-based and directory-proximate), batch concatenation, duplicate deduplication, clustering input builders |
| `engine/validators` | 20 | Valid plan, missing hunk, duplicate hunk, unknown hunk, bad type, bad scope, subject too long, trailing period, uppercase subject, breaking without footer, breaking with footer, empty scopes, multiple errors, confidence scoring (high for clean plans, file overlap penalty, entanglement penalty, wide-spread penalty, range overlap, hunk range parsing, same-commit entanglement) |
| `engine/executors` | 8 | Apply single commit, dry-run, fail-and-restore, partial apply rollback, deleted file commit, staged changes included, commit author override, skip hooks bypass |
| `cmd` | 3 | PR title/body generation from commit plan (single commit, multiple commits, type summarization) |

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

The individual features (v0.1--v0.4) drive developer adoption. The team features (v0.5--v0.8) drive enterprise value. v1.0.0 is the stability promise.

**Design note:** v0.1 and v0.2 are intentionally stateless — no database, no persistent index, no learning across sessions. Every `plan` call reads the live diff and starts fresh. This keeps onboarding frictionless (works on any repo with zero setup) and eliminates an entire class of stale-state bugs. Stateful capabilities — commit style learning, cross-session memory, and repo-level intelligence — are introduced incrementally from v0.4 onward, always as opt-in features so that Intentra remains fully functional without them.

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

### v0.3.0 -- GitHub Integration, Scaling & Hardening (Released)

- **Phased progress indicator**: spinner shows contextual stages ("Clustering N hunks...", "Generating commit messages...") with elapsed time, instead of a generic message
- **Hunk summarization**: large patches are automatically truncated (first/last N lines) in the clustering prompt, reducing token usage. Configurable via `max_hunk_lines` (default: 50, 0 = no truncation). Full patches are preserved for apply.
- **`intentra push`**: push the current branch with smart upstream detection (`--set-upstream` for new branches), configurable remote, and remote validation
- **`intentra pr`**: create a GitHub PR with title and body derived from the cached commit plan -- no LLM call needed. Supports `--title`, `--base`, and `--draft` flags. Falls back to `git log` if no cached plan exists.
- **Remote branch protection awareness**: before pushing, Intentra checks GitHub branch protection via `gh api` and warns if the branch requires PRs. Graceful degradation if `gh` is not installed.
- **`gh` CLI integration**: shared helpers for authentication validation, repo info, and branch protection checks. Used by `push`, `pr`, and `apply`.
- **Refactored git helpers**: push, branch, remote, and preflight logic extracted to shared module for reuse across commands
- **Scaling for large diffs (100+ hunks)**: three-tier clustering strategy -- compact prompt IDs, file-level pre-grouping, and batched clustering with LLM merge pass. Configurable via `batch_threshold` (default: 40). See [Scaling for Large Diffs](#scaling-for-large-diffs).
- **Compact prompt IDs**: 64-char SHA256 hunk IDs replaced with short tokens (h1, h2, ...) in LLM prompts, saving thousands of tokens and reducing LLM bookkeeping errors
- **Duplicate hunk repair**: duplicate hunk assignments across groups are auto-deduplicated instead of causing a hard error
- **Confidence scoring**: each plan is scored for quality based on file overlap, hunk entanglement (adjacent-line edits in different commits), and commit spread. Displayed after the plan summary as high/medium/low with specific warnings.
- **Patch pre-check**: each commit's patch is validated with `git apply --check` before staging, catching malformed patches early
- **Empty patch guard**: commits that produce zero-byte patches are rejected immediately instead of creating empty commits
- **Working tree drift detection**: OS-level file fingerprinting (size + mtime) detects external modifications between commits during apply, immune to git index changes from Intentra's own operations
- **Graceful interrupt handling**: Ctrl+C (SIGINT/SIGTERM) during `apply` triggers clean rollback of all commits applied so far
- **Cached plan validation**: plans loaded from `.intentra/plan.json` are structurally validated before use
- **Command timeouts**: all git and `gh` CLI calls use `exec.CommandContext` with timeouts (30s local, 120s network) to prevent indefinite hangs
- **Fuzz tests**: diff parser now includes fuzz tests for edge cases in unified diff parsing

### v0.4.0 -- Commit Intelligence

- Risk scoring per commit (sensitive areas: auth, database, payments, config)
- **Import-graph analysis**: replace directory-name heuristics with actual import graph for commit dependency ordering
- **Cross-session plan memory**: optionally remember rejected plans so subsequent runs can avoid similar groupings
- `intentra plan --analyze` flag for detailed per-commit breakdown
- **Streaming LLM output**: stream clustering and messaging results as they arrive, reducing perceived latency for large diffs

### v0.5.0 -- Ship & PR Intelligence

- **`intentra ship`** -- single-command workflow: create branch, apply commits, push, and open PR(s)
  - AI groups commits into logical PRs: if your diff contains an auth feature, a bug fix, and a refactor, `ship` creates separate branches and PRs for each concern -- not one monolithic PR
  - **Configurable branch naming**: template-based convention in config (e.g., `branch_template: "{type}/{ticket}/{summary}"`), producing branches like `feat/PROJ-123/add-jwt-auth`. Falls back to `{type}/{short-subject}` when no ticket is present.
  - Detects if you're already on a feature branch and skips branch creation (just applies, pushes, and opens the PR)
  - Rolls back branch creation if apply fails mid-way
- **PR split suggestions**: when a single diff has unrelated concerns, the LLM identifies logical boundaries and proposes N separate PRs rather than one. `intentra ship --split` executes the split automatically; without `--split`, it warns and asks for confirmation.
- **AI-generated PR descriptions**: structured body with change summary, commit list, affected files, and review hints -- all derived from the commit plan, no extra LLM call
- **Review checklist generation**: auto-generated checklist based on what files and subsystems changed (e.g., "database migration included -- verify rollback", "auth changes -- check token expiry")
- `intentra review` command for self-review before submitting

### v0.6.0 -- CI/CD Integration

- Official GitHub Actions action (`uses: crvgilbertson/intentra-action`)
- Run `intentra plan --json` in CI to validate commit hygiene on every PR
- Block merges if commits don't follow conventions or fail risk thresholds
- Post plan summary as a PR comment for reviewer context

### v0.7.0 -- Team Configuration

- Shareable config presets for common setups (monorepo, library, microservice)
- Scope auto-detection from directory structure and module boundaries
- **Commit style learning**: analyze recent repo history to infer preferred commit types, scopes, and message patterns — reducing reliance on the LLM's default style
- Team-wide config in repo root with personal overrides
- `intentra config check` for config validation and drift detection

### v0.8.0 -- Issue Tracker Integration

- **Jira integration**: link commits and PRs to Jira tickets automatically
  - Parse ticket IDs from branch names (e.g., `feat/PROJ-123/add-auth`) and include them in commit footers
  - `intentra plan --ticket PROJ-123` to associate all commits with a specific ticket
  - Auto-add `Refs: PROJ-123` footers to commit messages when a ticket is detected
  - Works with the `branch_template` from v0.5.0 -- `{ticket}` placeholder is populated from the active ticket
- **PR-to-ticket linking**: when creating PRs with `intentra pr` or `intentra ship`, auto-populate Jira ticket references in the description
- **Ticket-aware validation**: warn if commits on a ticket branch don't reference the expected ticket ID
- **Linear / GitHub Issues support**: same linking model for Linear, GitHub Issues, and other trackers via configurable patterns
- Configurable ticket ID pattern in `.intentra/config.yaml` (e.g., `ticket_pattern: "[A-Z]+-\\d+"`)

### v0.9.0 -- Repository Intelligence

- **Optional local index**: persistent store of repo structure, file ownership, and change frequency for smarter clustering (opt-in — Intentra remains fully functional without it)
- **Hot-path detection**: identify files that frequently change together and bias clustering toward co-locating them in the same commit
- **Style fingerprinting**: learn the team's commit voice from history (preferred verbs, scope conventions, subject length norms) and use it to guide LLM output

### v1.0.0 -- Stable Platform

- Stability promise: CLI flags, config format, and JSON output schema will not break
- Plugin interface for custom planners and validators
- Parallel hunk analysis for large diffs
- `intentra upgrade` for self-updating to latest release
- Comprehensive documentation site

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
