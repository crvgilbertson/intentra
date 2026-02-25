# AI Code Workflow Engine
## Agent Handoff Specification (Foundational Architecture)

---

## 1. Vision

Build a deterministic, extensible AI-powered workflow engine that understands code changes and produces structured, machine-actionable plans.

### Version 0.1 Capability
- Intelligent atomic commit planning

### Long-Term Direction
- PR planning
- Risk scoring
- Change intent detection
- Release intelligence
- Semantic versioning automation
- Changelog generation
- Architectural awareness

This is not a commit message tool.

This is a code change reasoning engine.

---

## 2. Design Principles

1. Deterministic output (schema-validated)
2. Strict separation of reasoning and execution
3. Extensible planner system
4. Git-safe (never corrupt state)
5. CLI-first
6. Stateless by default
7. Cross-platform
8. Trust-first UX
9. Minimal magic
10. Modular for future capabilities

---

## 3. Core Architecture Overview

CLI Layer  
↓  
Context Builder  
↓  
Reasoning Engine  
↓  
Planner  
↓  
Validator  
↓  
Executor  

---

## 4. System Layers

### 4.1 CLI Layer

Responsibilities:
- Parse arguments
- Load config
- Invoke planners
- Display plans
- Trigger execution

Commands (v1):

    engine plan
    engine apply
    engine init

Future commands:

    engine pr-plan
    engine risk
    engine release

---

### 4.2 Context Builder Layer

Purpose:
Create structured repository context for reasoning.

Components:
- Git state extractor
- Diff parser
- Hunk extractor
- Commit history sampler
- Repo metadata collector

Output structure:

    type EngineContext struct {
        BaseRef       string
        Hunks         []Hunk
        RecentCommits []string
        Config        EngineConfig
    }

---

### 4.3 Diff & Hunk Model

    type Hunk struct {
        HunkID   string
        FilePath string
        Header   string
        Patch    string
        Summary  string
    }

HunkID generation:

    sha256(file_path + header + patch)

Rules:
- Must be stable
- Must uniquely identify change fragment
- No duplicate HunkIDs

---

### 4.4 Reasoning Engine

Responsibilities:
- Structured OpenAI calls
- Schema enforcement
- Retry logic
- Deterministic settings
- Error correction

Interface:

    type ReasoningEngine interface {
        CallStructured(
            schema JSONSchema,
            input any,
        ) (any, error)
    }

Key properties:
- Strict JSON schema
- Temperature low (0.1–0.2)
- Validation required
- No side effects

---

### 4.5 Planner Abstraction

    type Plan interface {
        Validate() error
    }

    type Planner interface {
        Name() string
        BuildPlan(ctx EngineContext) (Plan, error)
    }

Commit planning is one implementation.

Future planners:
- PRPlanner
- RiskPlanner
- ReleasePlanner

---

## 5. Commit Planner (v0.1 Capability)

### 5.1 Purpose

Convert uncommitted diffs into atomic, Conventional Commit–compliant commits.

---

### 5.2 Commit Plan Model

    type CommitPlan struct {
        ToolVersion string
        BaseRef     string
        Style       CommitStyle
        Commits     []CommitUnit
    }

Commit unit:

    type CommitUnit struct {
        ID       string
        Type     string
        Scope    *string
        Subject  string
        Body     *string
        Breaking bool
        Footers  []Footer
        Hunks    []string
    }

Commit style:

    type CommitStyle struct {
        Convention    string
        MaxSubjectLen int
        AllowedTypes  []string
        Scopes        []string
    }

---

## 6. Commit Planning Pipeline

### Stage 1 — Intent Clustering

Input:
- Hunk summaries
- File paths
- Patch snippets

Output:
- Logical group assignments

Rules:
- Split unrelated concerns
- Keep inseparable changes together
- Avoid over-splitting
- Ensure every hunk assigned once

---

### Stage 2 — Message Generation

For each cluster:
- Determine type
- Determine scope
- Generate subject
- Generate body if needed
- Determine breaking
- Add footers if required

---

### Stage 3 — Validation

Validate:
- No overlapping hunks
- No missing hunks
- Valid commit types
- Subject length ≤ max
- No trailing period
- Imperative tense
- Breaking change must include footer

If invalid:
- Retry with correction prompt

---

## 7. Executor Layer

    type Executor interface {
        Execute(plan Plan) error
    }

---

### 7.1 Git Executor (v1)

For each commit:

1. Generate patch containing only its hunks
2. Run:

        git apply --cached

3. Run:

        git commit -m "<subject>"

Safety rules:
- If any failure → abort
- Restore index state
- Never partially apply

---

## 8. Configuration System

File: .engine.yaml

Example:

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
      scopes:
        - auth
        - api
        - ui
        - core

    ai:
      model: gpt-4.1
      temperature: 0.2
      max_diff_kb: 500

    engine:
      strict_mode: true

---

## 9. Determinism Requirements

- Strict schema
- Retry invalid outputs
- Low temperature
- No side effects from reasoning
- No overlapping hunks
- Stable plan ordering

---

## 10. Failure Handling

AI Failure:
- Invalid JSON → retry
- Missing hunks → regenerate
- Overlap → regenerate

Git Failure:
- Abort
- Restore previous index
- Clear error messaging

---

## 11. Trust Model

The system must never:
- Modify working tree without explicit apply
- Execute arbitrary AI commands
- Change Git configuration
- Rewrite history automatically

User must explicitly approve apply.

---

## 12. Extensibility Strategy

Future planners will reuse:
- EngineContext
- ReasoningEngine
- Structured schema enforcement
- Validation pattern
- Executor pattern

Examples:

PR Planner:
- Input: multiple commits, branch diff
- Output: PR title, PR description, change summary

Risk Planner:
- Input: change intent + file graph
- Output: risk score + blast radius estimation

Release Planner:
- Input: commit types since last tag
- Output: semantic version suggestion + changelog draft

---

## 13. Folder Structure

    /cmd
        root.go
        plan.go
        apply.go
        init.go

    /engine
        /context
            repo_context.go
            diff_parser.go
            hunk_hasher.go

        /models
            hunk.go
            commit_plan.go
            change_intent.go

        /reasoning
            client.go
            structured_call.go
            retry.go
            schemas/

        /planners
            planner.go
            commit_planner.go
            commit_schema.go

        /validators
            commit_validator.go

        /executors
            executor.go
            git_executor.go

    /config
        config.go

---

## 14. Phase Roadmap

Phase 1 — Commit Planner
- Diff parsing
- Hunk hashing
- AI structured plan
- Safe apply

Phase 2 — Commit Intelligence
- Risk score
- Entanglement detection
- Commit quality score
- Confidence metric

Phase 3 — PR Intelligence
- PR summarization
- PR split suggestions
- Review assistance

Phase 4 — Workflow Intelligence Platform
- Change impact graph
- Test selection
- Semantic release engine
- Policy enforcement

---

## 15. Strategic Positioning

Not:
AI commit writer

But:
Deterministic AI code change intelligence engine

Commit planning is capability 0.1.

---

## 16. Definition of Done (v0.1)

- Hunk parsing stable
- JSON schema enforced
- Deterministic commit plan
- Safe apply
- No data corruption
- Configurable
- Extensible planner interface

---

## 17. Long-Term Moat

The moat becomes:
- Deterministic structured reasoning
- Workflow-aware AI planning
- Safe execution layer
- Trust

Not:
- Writing commit messages

---

## 18. Final Architectural Constraints

Do not:
- Hardcode commit logic into CLI
- Let planner call git
- Let executor call AI
- Collapse reasoning and execution layers

Separation is mandatory.
