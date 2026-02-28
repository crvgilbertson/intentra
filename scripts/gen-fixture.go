// gen-fixture generates testdata/snapshots/v0.5/regression.json.
// Run: go run scripts/gen-fixture.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/engine/atomicity"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/internal"
)

func main() {
	hunks := []models.Hunk{
		{HunkID: "h_cmd", FilePath: "cmd/main.go", Header: "@@ -1 +1 @@", Patch: "+main"},
		{HunkID: "h_doc", FilePath: "docs/README.md", Header: "@@ -1 +1 @@", Patch: "+docs"},
		{HunkID: "h_exec", FilePath: "engine/executors/exec.go", Header: "@@ -1 +1 @@", Patch: "+exec"},
		{HunkID: "h_mod1", FilePath: "engine/models/types.go", Header: "@@ -1 +1 @@", Patch: "+type1"},
		{HunkID: "h_mod2", FilePath: "engine/models/types.go", Header: "@@ -10 +10 @@", Patch: "+type2"},
		{HunkID: "h_plan", FilePath: "engine/planners/planner.go", Header: "@@ -1 +1 @@", Patch: "+plan"},
	}

	scope := "models"
	plan := models.CommitPlan{
		SchemaVersion:     models.CurrentSchemaVersion,
		ToolVersion:       internal.Version,
		BaseRef:           "snapshot-base",
		DiffFingerprint:   models.DiffFingerprintFromHunks(hunks),
		PromptFingerprint: planners.PromptFingerprint(),
		Style: models.CommitStyle{
			Convention:    "conventional_commits",
			MaxSubjectLen: 72,
			AllowedTypes:  []string{"feat", "fix", "refactor", "chore"},
		},
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Scope: &scope, Subject: "update type definitions",
				Hunks: []string{"h_mod1", "h_mod2", "h_doc", "h_exec"}, Rationale: "type definitions"},
			{ID: "c2", Type: "refactor", Subject: "update planner logic",
				Hunks: []string{"h_plan"}, Rationale: "planner logic"},
			{ID: "c3", Type: "chore", Subject: "update CLI entrypoint",
				Hunks: []string{"h_cmd"}, Rationale: "cli entrypoint"},
		},
		Confidence: &models.PlanConfidence{
			Overall: 0.9,
			Level:   "high",
			Components: models.ConfidenceComponents{
				Coverage: 1.0, Entanglement: 0.9, RepairActivity: 1.0, Overlap: 0.8, ReorderPenalty: 1.0,
			},
		},
		Trace: &models.PipelineTrace{
			Strategy:          "direct",
			OrderingStrategy:  "fallback",
			AtomicityProfile:  "balanced",
			DedupCount:        1,
			OrphanCount:       2,
			RescueAttempted:   true,
			RescueSucceeded:   false,
			RepairCount:       2,
			ReorderApplied:    false,
			CommitsBefore:     3,
			CommitsAfter:      3,
		},
	}

	cfg := config.DefaultConfig()
	cfg.Style.Scopes = []string{"models", "auth", "ui"}

	hunkMetas := make([]models.HunkMeta, len(hunks))
	for i, h := range hunks {
		hunkMetas[i] = models.HunkMetaFromHunk(h)
	}

	snap := models.PlanSnapshot{
		EngineVersion:     internal.Version,
		SchemaVersion:     models.CurrentSchemaVersion,
		PromptFingerprint: planners.PromptFingerprint(),
		Provider:          "openai",
		Model:             "gpt-4.1",
		Config: models.SnapshotConfig{
			Provider:          "openai",
			Model:             "gpt-4.1",
			Temperature:       0.2,
			MaxCommits:        20,
			MaxHunkLines:      50,
			BatchThreshold:    40,
			AtomicityProfile:  atomicity.NormalizeProfile(cfg.Engine.Atomicity.Profile),
			Style:             plan.Style,
		},
		DiffFingerprint: plan.DiffFingerprint,
		HunkCount:       len(hunks),
		Hunks:           hunkMetas,
		Plan:            plan,
		Confidence:      plan.Confidence,
		Trace:           plan.Trace,
		Timestamp:       time.Now().UTC(),
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}

	out := filepath.Join("testdata", "snapshots", "v0.5", "regression.json")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", out)
}
