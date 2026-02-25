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

1. **Context** -- Intentra runs `git diff` on your working tree and parses the output into individual hunks. Each hunk receives a stable, deterministic ID via `sha256(filePath + header + patch)`.

2. **Clustering** -- The hunks are sent to an LLM with strict JSON schema enforcement. The model groups related hunks by intent: which changes belong together in a single atomic commit. Supports OpenAI, Anthropic Claude, and any OpenAI-compatible endpoint (Ollama, vLLM, LM Studio).

3. **Messaging** -- For each cluster, the LLM generates Conventional Commit metadata: type, scope, subject, body, breaking change flags, and footers. All output is schema-validated.

4. **Validation** -- The resulting `CommitPlan` is validated against business rules: every hunk is assigned exactly once, commit types and scopes are from the allowed set, subject length is within limits, breaking changes have proper footers, and more.

5. **Execution** -- Only when you explicitly pass `--yes` does Intentra touch git. It stages each commit's hunks via `git apply --cached` and commits them. If anything fails, the index is restored to its pre-apply state. No partial applies. No data corruption.

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
go install intentra@latest
```

Or build from source:

```bash
git clone <repo-url> && cd intentra
go build -o intentra .
```

---

## Quick Start

```bash
# 1. Initialize configuration
intentra init

# 2. Make some code changes in your repo...

# 3. Preview the commit plan
intentra plan

# 4. See the raw JSON plan
intentra plan --json

# 5. Apply the plan (dry-run by default)
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
Generating commit plan...

Commit Plan (3 commit(s)):
Base: e4a91bc

  1. feat(auth): add JWT token validation and refresh logic
     hunks: 3
  2. fix(api): handle nil user in request middleware
     hunks: 2
  3. refactor(core): simplify utility string helpers
     hunks: 2
```

Three clean, atomic commits -- each with a single concern. When you're happy with the plan:

```
$ intentra apply --yes

Applying 3 commit(s)...
Successfully applied 3 commit(s).
```

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
  "tool_version": "0.1.0",
  "base_ref": "e4a91bc",
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
    },
    {
      "id": "c2",
      "type": "fix",
      "scope": "api",
      "subject": "handle nil user in request middleware",
      "breaking": false,
      "hunks": ["j0k1l2...", "m3n4o5..."]
    },
    {
      "id": "c3",
      "type": "refactor",
      "scope": "core",
      "subject": "simplify utility string helpers",
      "breaking": false,
      "hunks": ["p6q7r8...", "s9t0u1..."]
    }
  ]
}
```

### Safe dry-run by default

`intentra apply` without `--yes` shows the plan but makes no changes:

```
$ intentra apply

Commit Plan (3 commit(s)):
Base: e4a91bc

  1. feat(auth): add JWT token validation and refresh logic
     hunks: 3
  2. fix(api): handle nil user in request middleware
     hunks: 2
  3. refactor(core): simplify utility string helpers
     hunks: 2

Dry-run mode. Pass --yes to apply.
```

Your working tree is untouched. Review the plan, then run with `--yes` when ready.

### Using with a local model

Run Intentra completely offline with Ollama:

```bash
# Pull a model
ollama pull qwen3-coder:32b

# Configure Intentra
cat .engine.yaml
# ai:
#     provider: ollama
#     model: qwen3-coder:32b

# Plan as usual -- no API key needed, all local
intentra plan
```

---

## Commands

### `intentra plan`

Analyzes the current `git diff` and generates a structured commit plan using AI reasoning.

```
Usage:
  intentra plan [flags]

Flags:
      --json   Output raw CommitPlan JSON instead of human-readable summary
```

**Default output** is a readable summary:

```
Found 5 hunk(s) across the diff.
Generating commit plan...

Commit Plan (2 commit(s)):
Base: a1b2c3d

  1. feat(auth): add JWT token validation
     hunks: 3
  2. fix(api): handle nil pointer in user lookup
     hunks: 2
```

**With `--json`**, outputs the full `CommitPlan` JSON -- useful for piping to other tools or inspection:

```json
{
  "tool_version": "0.1.0",
  "base_ref": "a1b2c3d",
  "style": {
    "convention": "conventional_commits",
    "max_subject_len": 72,
    "allowed_types": ["feat", "fix", "refactor", "perf", "docs", "test", "chore"],
    "scopes": ["auth", "api"]
  },
  "commits": [
    {
      "id": "c1",
      "type": "feat",
      "scope": "auth",
      "subject": "add JWT token validation",
      "hunks": ["<sha256>", "<sha256>", "<sha256>"]
    }
  ]
}
```

### `intentra apply`

Generates a commit plan and applies it to the repository. **Defaults to dry-run** -- you must pass `--yes` to actually create commits.

```
Usage:
  intentra apply [flags]

Flags:
      --yes    Actually apply commits (default is dry-run)
```

**Without `--yes`**, the command shows the plan and exits:

```
Commit Plan (2 commit(s)):
...
Dry-run mode. Pass --yes to apply.
```

**With `--yes`**, Intentra:

1. Snapshots the current git index state
2. For each commit: writes a patch file, stages with `git apply --cached`, then `git commit`
3. If any step fails: immediately aborts and restores the index to the snapshot
4. Reports success or which commit failed and why

### `intentra init`

Creates a default `.engine.yaml` configuration file in the current directory.

```
Usage:
  intentra init
```

Fails if `.engine.yaml` already exists (to prevent accidental overwrites).

### Global Flags

```
      --config string   Path to config file (default ".engine.yaml")
```

---

## Configuration

Intentra is configured via `.engine.yaml` in your project root. Run `intentra init` to generate the default:

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

ai:
    provider: openai
    model: gpt-4.1
    temperature: 0.2
    max_diff_kb: 500

engine:
    strict_mode: true
```

### Configuration Reference

| Section | Key | Type | Default | Description |
|---------|-----|------|---------|-------------|
| `style` | `convention` | string | `conventional_commits` | Commit convention to enforce |
| `style` | `max_subject_len` | int | `72` | Maximum subject line length |
| `style` | `allowed_types` | []string | `[feat, fix, ...]` | Permitted commit types |
| `style` | `scopes` | []string | `[]` | Permitted scopes (empty = any) |
| `ai` | `provider` | string | `openai` | LLM provider: `openai`, `anthropic`, `gemini`, or `ollama` |
| `ai` | `model` | string | `gpt-4.1` | Model name (provider-specific) |
| `ai` | `temperature` | float | `0.2` | LLM temperature (0.1--0.2 recommended) |
| `ai` | `max_diff_kb` | int | `500` | Maximum diff size in KB before aborting |
| `ai` | `base_url` | string | *(empty)* | Custom API base URL (for Azure, proxies, or self-hosted endpoints) |
| `engine` | `strict_mode` | bool | `true` | Enable strict validation |

If no `.engine.yaml` is found, Intentra uses these defaults automatically.

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
CLI Command
    |
    v
Context Builder ---- git diff, git log ---> EngineContext { Hunks, RecentCommits, Config }
    |
    v
Reasoning Engine --- LLM structured output (OpenAI / Anthropic / Ollama) ---> JSON (schema-validated)
    |
    v
Commit Planner ---- two-pass (cluster + message) ---> CommitPlan
    |
    v
Validator ---- business rules ---> pass / error
    |
    v
Executor ---- git apply --cached + git commit ---> commits created
```

### Two-Pass Planning Pipeline

Intentra uses a two-pass approach for deterministic commit planning:

**Pass 1 -- Intent Clustering**

The LLM receives all hunks (file path, header, patch content) and groups them by logical intent. The output is a strict JSON schema: an array of groups, each containing a stable group ID and a list of hunk IDs. Validation ensures every hunk is assigned exactly once with no duplicates and no omissions.

**Pass 2 -- Message Generation**

For each cluster, the LLM generates Conventional Commit metadata: type, scope, subject, body, breaking flag, and footers. The output is again schema-validated. Recent commit history from the repo is provided as style reference.

Both passes use the generic `CallWithRetry[T]` mechanism: if the LLM output fails validation, it retries once with a correction prompt. If the retry also fails, the operation aborts cleanly.

### Safety Model

Intentra enforces a strict trust model:

- **Plan never mutates** -- `intentra plan` is read-only. It collects the diff and reasons about it but never changes any files or git state.
- **Dry-run by default** -- `intentra apply` without `--yes` shows the plan and exits.
- **Atomic apply** -- Each commit is staged via `git apply --cached` and committed individually. If any step fails, the entire operation aborts and the git index is restored to its pre-apply snapshot.
- **No history rewriting** -- No rebase, no amend, no force-push. Intentra only creates new commits.
- **Strict layer separation** -- The reasoning layer (LLM) cannot execute git commands. The executor layer cannot call the LLM. This is enforced architecturally, not by convention.

### Project Structure

```
intentra/
├── main.go                          Entry point
├── go.mod
│
├── cmd/                             CLI layer (Cobra commands, no business logic)
│   ├── root.go                      Root command, --config flag, config loading
│   ├── plan.go                      intentra plan [--json]
│   ├── apply.go                     intentra apply [--yes]
│   └── init.go                      intentra init
│
├── config/                          Configuration loading and defaults
│   └── config.go                    EngineConfig, YAML load/write
│
└── engine/
    ├── context/                     Git state collection, pure diff parsing
    │   ├── diff_parser.go           Unified diff -> []Hunk
    │   ├── diff_parser_test.go
    │   ├── hunk_hasher.go           sha256-based stable hunk IDs
    │   ├── hunk_hasher_test.go
    │   └── repo_context.go          BuildContext() -> EngineContext
    │
    ├── models/                      Shared domain types (no logic)
    │   ├── hunk.go                  Hunk { HunkID, FilePath, Header, Patch, Summary }
    │   ├── commit_plan.go           CommitPlan, CommitUnit, CommitStyle, Plan interface
    │   └── change_intent.go         Reserved for future capabilities
    │
    ├── reasoning/                   LLM abstraction (NO git calls)
    │   ├── client.go                ReasoningEngine interface
    │   ├── openai.go                OpenAI + OpenAI-compatible implementation
    │   ├── anthropic.go             Anthropic Claude implementation (tool_use)
    │   ├── factory.go               Provider selection from config
    │   └── retry.go                 Generic CallWithRetry[T] with correction prompts
    │
    ├── planners/                    Plan generation (NO git calls)
    │   ├── planner.go               Planner interface
    │   ├── commit_planner.go        Two-pass CommitPlanner implementation
    │   ├── commit_schema.go         JSON schemas for clustering + messaging
    │   └── commit_planner_test.go
    │
    ├── validators/                  Business rule validation (NO git calls)
    │   ├── commit_validator.go      ValidateCommitPlan()
    │   └── commit_validator_test.go
    │
    └── executors/                   Git operations (NO LLM calls)
        ├── executor.go              Executor interface
        ├── git_executor.go          git apply --cached + commit, abort/restore
        └── git_executor_test.go     Integration tests in temp repos
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
| `engine/context` | 9 | Diff parsing (single file, multi-file, binary skip, renames, empty), hunk hashing (stability, uniqueness, format) |
| `engine/planners` | 7 | Full planner flow with mocked LLM, empty hunks, clustering validation (missing/duplicate/unknown hunks), messaging validation (missing groups) |
| `engine/validators` | 13 | Valid plan, missing hunk, duplicate hunk, unknown hunk, bad type, bad scope, subject too long, trailing period, uppercase subject, breaking without footer, breaking with footer, empty scopes, multiple errors |
| `engine/executors` | 3 | Apply single commit in temp repo, dry-run (no mutation), fail-and-restore (index recovery after bad patch) |

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

| Phase | Capability | Status |
|-------|-----------|--------|
| **1** | Commit planning (atomic, Conventional Commits) | **v0.1 -- Done** |
| 2 | Commit intelligence (risk score, entanglement detection, confidence metrics) | Planned |
| 3 | PR intelligence (summarization, split suggestions, review assistance) | Planned |
| 4 | Workflow intelligence platform (change impact graph, test selection, semantic release, policy enforcement) | Planned |

---

## License

TBD
