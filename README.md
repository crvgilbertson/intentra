# Intentra

Intentra is a deterministic change-intelligence CLI for local Git repositories.
It analyzes your uncommitted diff, proposes atomic commits, and turns the cached
plan into release-facing artifacts such as release notes, changelog entries, and
risk reports.

## What v0.6 adds

- `intentra release-notes` from the cached plan
- `intentra changelog` from the cached plan
- `intentra risk-report` from the cached plan
- Lightweight ticket linking via `--ticket` or branch-name detection
- Initial-commit and empty-file handling improvements
- Stronger import-graph ordering and planner fallback reliability

## Install

### Homebrew

```bash
brew tap crvgilbertson/intentra
brew install intentra
```

### Go

```bash
go install github.com/crvgilbertson/intentra@v0.6.0
```

### Build from source

```bash
git clone https://github.com/crvgilbertson/intentra.git
cd intentra
go build -o intentra .
```

## Requirements

- Go 1.25+
- Git on `PATH`
- Optional: `gh` CLI for `intentra pr`
- One provider configured through environment variables

| Provider | Env var | Notes |
| --- | --- | --- |
| OpenAI | `OPENAI_API_KEY` | Default |
| Anthropic | `ANTHROPIC_API_KEY` | Claude |
| Gemini | `GEMINI_API_KEY` | OpenAI-compatible endpoint |
| Ollama | none | Local models |

## Quick start

```bash
# 1. Create local config
intentra init

# 2. Review the current diff and cache a plan
intentra plan

# 3. Inspect the plan
intentra explain
intentra plan --analyze

# 4. Turn the plan into release-facing artifacts
intentra release-notes
intentra risk-report
intentra changelog --since v0.5.0 --version v0.6.0

# 5. Apply the plan when ready
intentra apply --yes

# 6. Push or open a PR
intentra push
intentra pr
```

## How it works

1. Intentra reads the current diff, including staged, unstaged, and untracked files.
2. It parses the diff into stable hunk IDs.
3. An LLM groups hunks into logical commits and generates Conventional Commit metadata.
4. The result is validated, confidence-scored, risk-scored, ordered, and cached to `.intentra/plan.json`.
5. That cached plan becomes the source of truth for `apply`, `pr`, `release-notes`, `changelog`, and `risk-report`.

The design goal is a deterministic shell around nondeterministic AI: planning uses a model, but validation, confidence, caching, risk scoring, replay, and execution safety are all deterministic.

## Commands

### Core

| Command | Purpose |
| --- | --- |
| `intentra plan` | Generate and cache a commit plan |
| `intentra apply` | Reuse or regenerate the plan, then dry-run or apply it |
| `intentra explain` | Show deterministic planning trace from the cached plan |
| `intentra replay <snapshot>` | Re-run a snapshot to check for drift |
| `intentra doctor` | Show config, cache, trust-surface, and diff diagnostics |

### Artifacts

| Command | Purpose |
| --- | --- |
| `intentra release-notes` | Group cached commits into release notes |
| `intentra changelog` | Build a changelog-ready entry from the cached plan |
| `intentra risk-report` | Summarize risky commits and sensitive areas touched |

### Git / GitHub helpers

| Command | Purpose |
| --- | --- |
| `intentra push` | Push current branch with upstream detection |
| `intentra pr` | Create a GitHub PR from the cached plan or git log |
| `intentra init` | Create `.intentra/config.yaml` and `.intentra/.gitignore` |

## Common usage

### Generate a plan

```bash
intentra plan
intentra plan --json
intentra plan --analyze
intentra plan --snapshot .intentra/snapshot.json
intentra plan --ticket PROJ-123
```

Notes:

- `plan` caches to `.intentra/plan.json`
- `--analyze` shows per-commit files, rationale, and risk
- `--snapshot` exports a replayable snapshot
- `--ticket` adds a `Refs: PROJ-123` footer to generated commits

### Apply safely

```bash
intentra apply
intentra apply --yes
intentra apply --yes --force
intentra apply --yes --ticket PROJ-123
```

Notes:

- Dry-run is the default
- Cached plans are reused when the diff is unchanged
- Apply is blocked on protected branches by default
- Low-confidence plans are blocked unless `--force` is passed
- Failed applies are rolled back

### Generate release-facing output

```bash
intentra release-notes
intentra release-notes --json

intentra risk-report
intentra risk-report --json

intentra changelog --since v0.5.0 --version v0.6.0
intentra changelog --ticket PROJ-123
```

### Open a PR

```bash
intentra pr
intentra pr --draft
intentra pr --base main --ticket PROJ-123
```

Intentra pushes the branch first, then derives the PR title/body from the cached plan when available.

## Ticket linking

v0.6 adds lightweight ticket support:

- `--ticket PROJ-123` on `plan`, `apply`, `pr`, `release-notes`, `risk-report`, and `changelog`
- Automatic detection from branch names like `feature/PROJ-123-add-risk-report`
- Commit footer enrichment as `Refs: PROJ-123`
- Ticket inclusion in PR bodies and generated artifacts

## Configuration

`intentra init` creates `.intentra/config.yaml`.

Minimal example:

```yaml
style:
  convention: conventional_commits
  max_subject_len: 72
  allowed_types: [feat, fix, refactor, perf, docs, test, chore]

ai:
  provider: openai
  model: gpt-4.1
  temperature: 0.2
  max_diff_kb: 500
  max_retries: 1
  timeout: 120
  max_hunk_lines: 50

engine:
  protected_branches: [main, master]
  max_commits: 20
  batch_threshold: 40
  strict_mode: true
  auto_push: false
  remote_name: origin
  atomicity:
    profile: balanced
  confidence:
    profile: balanced
  risk:
    enabled: false
```

High-value settings:

- `engine.atomicity.profile`: `cohesive`, `balanced`, `strict`
- `engine.confidence.profile`: `strict`, `balanced`, `permissive`
- `engine.risk.enabled`: turn on deterministic per-commit risk scoring
- `engine.risk.areas`: file-pattern based risk weights
- `ai.disable_initial_commit_heuristic`: bypass the built-in initial-commit fast path

## Safety and caching

Intentra is conservative by default:

- `apply` is dry-run unless `--yes` is passed
- protected branches are blocked by config
- cached plans are invalidated when diff, prompt fingerprint, schema version, or atomicity profile changes
- plan execution uses patch validation and rollback on failure
- working-tree drift during apply aborts and rolls back

The plan cache lives at `.intentra/plan.json`.

## Reliability notes in v0.6

This release hardens a few important edges:

- initial-commit repos now include staged files more reliably
- empty new files are preserved instead of being silently dropped
- import-graph ordering has stronger positive test coverage
- batched planner fallback now respects `max_commits`

## Example outputs

### Plan summary

```text
$ intentra plan

Found 5 hunk(s) across the diff.
  Commit Plan  2 commit(s)

  1 feat(auth): add token refresh handling
    3 hunk(s)  ->  auth/service.go, auth/service_test.go

  2 fix(api): return 401 on invalid session
    2 hunk(s)  ->  api/middleware.go

  Confidence: high (92%)
Plan saved to .intentra/plan.json
```

### Risk report

```text
$ intentra risk-report

# Risk Report

- Commits analyzed: 2
- Aggregate risk: 0.45 (medium)
- Risky commits: 1
- Sensitive areas: auth
```

## Development

```bash
go test ./...
go run scripts/check-imports.go
go build -o intentra .
```

Project layout:

- `cmd/`: CLI commands and adapters
- `engine/context/`: diff parsing and repository context
- `engine/planners/`: LLM planning pipeline
- `engine/validators/`: validation, confidence, risk
- `engine/executors/`: transactional git apply/commit logic
- `engine/artifacts/`: release notes, changelog, and risk-report generation

## Deeper docs

- [CHANGELOG.md](CHANGELOG.md)
- [docs/v0.6-implementation-plan.md](docs/v0.6-implementation-plan.md)

## Roadmap

v0.6 is the first release where the cached plan becomes more than an apply artifact.
The next steps are team policy enforcement, richer PR/review outputs, and broader repository intelligence.

See [project.md](project.md) for the longer roadmap and architectural thesis.

## License

Apache License 2.0. See [LICENSE](LICENSE).
