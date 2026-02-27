package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/internal"
)

func makeGoldenPlan() *models.CommitPlan {
	scope := "models"
	return &models.CommitPlan{
		SchemaVersion:     models.CurrentSchemaVersion,
		ToolVersion:       internal.Version,
		BaseRef:           "abc123",
		DiffFingerprint:   "deadbeef",
		PromptFingerprint: planners.PromptFingerprint(),
		Style: models.CommitStyle{
			Convention:    "conventional_commits",
			MaxSubjectLen: 72,
			AllowedTypes:  []string{"feat", "fix", "refactor"},
		},
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Scope: &scope, Subject: "add new types", Hunks: []string{"h1", "h2"}, Rationale: "foundational types"},
			{ID: "c2", Type: "fix", Subject: "correct handler", Hunks: []string{"h3"}, Rationale: "bug fix in handler"},
			{ID: "c3", Type: "refactor", Subject: "clean up imports", Hunks: []string{"h4", "h5"}, Rationale: "import cleanup"},
		},
		Confidence: &models.PlanConfidence{
			Overall: 0.85,
			Level:   "high",
			Components: models.ConfidenceComponents{
				Coverage:       1.0,
				Entanglement:   0.9,
				RepairActivity: 1.0,
				Overlap:        0.8,
				ReorderPenalty: 1.0,
			},
		},
		Trace: &models.PipelineTrace{
			Strategy:        "direct",
			DedupCount:      0,
			OrphanCount:     0,
			RescueAttempted: false,
			RescueSucceeded: false,
			RepairCount:     0,
			ReorderApplied:  false,
			CommitsBefore:   3,
			CommitsAfter:    3,
		},
	}
}

func TestComparePlans_Identical(t *testing.T) {
	plan := makeGoldenPlan()
	result := comparePlans(plan, plan, true)
	if result.Status != "IDENTICAL" {
		t.Errorf("expected IDENTICAL, got %s; divergences: %v", result.Status, result.Divergences)
	}
}

func TestComparePlans_StructurallyEquivalent(t *testing.T) {
	stored := makeGoldenPlan()
	replayed := makeGoldenPlan()
	replayed.Commits[0].Subject = "add new types (v2)"

	result := comparePlans(stored, replayed, true)
	if result.Status != "STRUCTURALLY_EQUIVALENT" {
		t.Errorf("expected STRUCTURALLY_EQUIVALENT, got %s; divergences: %v", result.Status, result.Divergences)
	}
}

func TestComparePlans_DivergentGrouping(t *testing.T) {
	stored := makeGoldenPlan()
	replayed := makeGoldenPlan()
	replayed.Commits[0].Hunks = []string{"h1"}
	replayed.Commits[1].Hunks = []string{"h2", "h3"}

	result := comparePlans(stored, replayed, true)
	if result.Status != "DIVERGENT" {
		t.Errorf("expected DIVERGENT, got %s", result.Status)
	}
	if len(result.Divergences) == 0 {
		t.Error("expected divergences to be populated")
	}
}

func TestComparePlans_DivergentCommitCount(t *testing.T) {
	stored := makeGoldenPlan()
	replayed := makeGoldenPlan()
	replayed.Commits = replayed.Commits[:2]

	result := comparePlans(stored, replayed, true)
	if result.Status != "DIVERGENT" {
		t.Errorf("expected DIVERGENT, got %s", result.Status)
	}
}

func TestComparePlans_DivergentType(t *testing.T) {
	stored := makeGoldenPlan()
	replayed := makeGoldenPlan()
	replayed.Commits[0].Type = "refactor"

	result := comparePlans(stored, replayed, true)
	if result.Status == "IDENTICAL" {
		t.Error("expected non-IDENTICAL when type changes")
	}
}

func TestComparePlans_PromptMismatch(t *testing.T) {
	plan := makeGoldenPlan()
	result := comparePlans(plan, plan, false)
	if result.PromptMatch {
		t.Error("expected PromptMatch=false")
	}
	if result.Status != "IDENTICAL" {
		t.Errorf("prompt mismatch alone should not change status, got %s", result.Status)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	plan := makeGoldenPlan()
	snap := models.PlanSnapshot{
		EngineVersion:     internal.Version,
		SchemaVersion:     models.CurrentSchemaVersion,
		PromptFingerprint: planners.PromptFingerprint(),
		Provider:          "openai",
		Model:             "gpt-4.1",
		Config: models.SnapshotConfig{
			Provider:       "openai",
			Model:          "gpt-4.1",
			Temperature:    0.2,
			MaxCommits:     20,
			MaxHunkLines:   50,
			BatchThreshold: 40,
			Style:          plan.Style,
		},
		DiffFingerprint: plan.DiffFingerprint,
		HunkCount:       5,
		Hunks: []models.HunkMeta{
			{HunkID: "h1", FilePath: "engine/models/types.go", Header: "@@ -1 +1 @@"},
			{HunkID: "h2", FilePath: "engine/models/hunk.go", Header: "@@ -1 +1 @@"},
			{HunkID: "h3", FilePath: "cmd/handler.go", Header: "@@ -10 +10 @@"},
			{HunkID: "h4", FilePath: "cmd/root.go", Header: "@@ -1 +1 @@"},
			{HunkID: "h5", FilePath: "cmd/root.go", Header: "@@ -20 +20 @@"},
		},
		Plan:       *plan,
		Confidence: plan.Confidence,
		Trace:      plan.Trace,
		Timestamp:  time.Now().UTC(),
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded models.PlanSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.EngineVersion != snap.EngineVersion {
		t.Errorf("engine version: %s != %s", decoded.EngineVersion, snap.EngineVersion)
	}
	if decoded.SchemaVersion != snap.SchemaVersion {
		t.Errorf("schema version: %s != %s", decoded.SchemaVersion, snap.SchemaVersion)
	}
	if decoded.PromptFingerprint != snap.PromptFingerprint {
		t.Errorf("prompt fingerprint: %s != %s", decoded.PromptFingerprint, snap.PromptFingerprint)
	}
	if len(decoded.Hunks) != len(snap.Hunks) {
		t.Errorf("hunk count: %d != %d", len(decoded.Hunks), len(snap.Hunks))
	}
	if len(decoded.Plan.Commits) != len(snap.Plan.Commits) {
		t.Errorf("commit count: %d != %d", len(decoded.Plan.Commits), len(snap.Plan.Commits))
	}
	if decoded.Confidence == nil {
		t.Fatal("confidence should not be nil after round-trip")
	}
	if decoded.Confidence.Components.Coverage != 1.0 {
		t.Errorf("coverage: %.2f != 1.0", decoded.Confidence.Components.Coverage)
	}
	if decoded.Trace == nil {
		t.Fatal("trace should not be nil after round-trip")
	}
	if decoded.Trace.Strategy != "direct" {
		t.Errorf("trace strategy: %s != direct", decoded.Trace.Strategy)
	}
}

func TestExplainReport(t *testing.T) {
	plan := makeGoldenPlan()
	report := buildExplainReport(plan)

	if report.Clustering.CommitCount != 3 {
		t.Errorf("commit count: %d != 3", report.Clustering.CommitCount)
	}
	if len(report.Clustering.Rationales) != 3 {
		t.Errorf("rationales: %d != 3", len(report.Clustering.Rationales))
	}
	if report.Clustering.Strategy != "direct" {
		t.Errorf("strategy: %s != direct", report.Clustering.Strategy)
	}
	if report.Confidence.Overall != 0.85 {
		t.Errorf("confidence: %.2f != 0.85", report.Confidence.Overall)
	}
}
