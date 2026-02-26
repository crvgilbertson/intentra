package planners

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/config"
)

// mockEngine is a test double for reasoning.ReasoningEngine.
type mockEngine struct {
	calls    int
	responses []json.RawMessage
	errors   []error
}

func (m *mockEngine) CallStructured(_ context.Context, _ string, _ interface{}, _ string, _ string) (json.RawMessage, error) {
	idx := m.calls
	m.calls++
	if idx >= len(m.responses) {
		return nil, fmt.Errorf("unexpected call %d", idx)
	}
	return m.responses[idx], m.errors[idx]
}

func testHunks() []models.Hunk {
	return []models.Hunk{
		{HunkID: "aaa", FilePath: "auth.go", Header: "@@ -1 +1 @@", Patch: "+login"},
		{HunkID: "bbb", FilePath: "auth.go", Header: "@@ -10 +10 @@", Patch: "+logout"},
		{HunkID: "ccc", FilePath: "ui.go", Header: "@@ -1 +1 @@", Patch: "+button"},
	}
}

func testConfig() config.EngineConfig {
	cfg := config.DefaultConfig()
	cfg.Style.Scopes = []string{"auth", "ui"}
	return cfg
}

func testEngineContext() enginectx.EngineContext {
	return enginectx.EngineContext{
		BaseRef:       "abc123",
		Hunks:         testHunks(),
		RecentCommits: []string{"feat(auth): add login"},
		Config:        testConfig(),
	}
}

func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// After sortHunks: aaa (auth.go @@-1), bbb (auth.go @@-10), ccc (ui.go @@-1)
// Compact IDs: h1=aaa, h2=bbb, h3=ccc

func TestCommitPlanner_BuildPlan_Success(t *testing.T) {
	clusterResp := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1", "h2"}},
			{ID: "g2", HunkIDs: []string{"h3"}},
		},
	}
	scope1 := "auth"
	scope2 := "ui"
	messagingResp := MessagingResponse{
		Commits: []CommitMessageWithGroup{
			{
				GroupID: "g1",
				CommitMessage: CommitMessage{
					Type: "feat", Scope: &scope1, Subject: "add authentication flow",
					Breaking: false, Footers: []FooterMessage{},
				},
			},
			{
				GroupID: "g2",
				CommitMessage: CommitMessage{
					Type: "feat", Scope: &scope2, Subject: "add button component",
					Breaking: false, Footers: []FooterMessage{},
				},
			},
		},
	}

	engine := &mockEngine{
		responses: []json.RawMessage{mustJSON(clusterResp), mustJSON(messagingResp)},
		errors:    []error{nil, nil},
	}

	planner := NewCommitPlanner(engine)
	plan, err := planner.BuildPlan(context.Background(), testEngineContext())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		t.Fatal("plan is not *CommitPlan")
	}

	if len(cp.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(cp.Commits))
	}

	if cp.Commits[0].ID != "c1" || cp.Commits[1].ID != "c2" {
		t.Errorf("unexpected commit IDs: %s, %s", cp.Commits[0].ID, cp.Commits[1].ID)
	}

	if len(cp.Commits[0].Hunks) != 2 {
		t.Errorf("expected 2 hunks in c1, got %d", len(cp.Commits[0].Hunks))
	}
	if len(cp.Commits[1].Hunks) != 1 {
		t.Errorf("expected 1 hunk in c2, got %d", len(cp.Commits[1].Hunks))
	}

	if cp.SchemaVersion != models.CurrentSchemaVersion {
		t.Errorf("expected schema version %s, got %s", models.CurrentSchemaVersion, cp.SchemaVersion)
	}
	if cp.ToolVersion != "0.3.0" {
		t.Errorf("expected tool version 0.3.0, got %s", cp.ToolVersion)
	}
	if cp.BaseRef != "abc123" {
		t.Errorf("expected base ref abc123, got %s", cp.BaseRef)
	}
}

func TestCommitPlanner_BuildPlan_NoHunks(t *testing.T) {
	engine := &mockEngine{}
	planner := NewCommitPlanner(engine)

	ec := testEngineContext()
	ec.Hunks = nil

	_, err := planner.BuildPlan(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error for empty hunks")
	}
}

func TestCommitPlanner_Name(t *testing.T) {
	planner := NewCommitPlanner(nil)
	if planner.Name() != "commit" {
		t.Errorf("expected name 'commit', got %s", planner.Name())
	}
}

func TestValidateClusteringResponse_MissingHunk(t *testing.T) {
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa", "bbb"}},
		},
	}
	err := validateClusteringResponse(cr, testHunks())
	if err == nil {
		t.Fatal("expected error for missing hunk ccc")
	}
}

func TestValidateClusteringResponse_DuplicateHunk(t *testing.T) {
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa", "bbb"}},
			{ID: "g2", HunkIDs: []string{"ccc", "aaa"}},
		},
	}
	err := validateClusteringResponse(cr, testHunks())
	if err == nil {
		t.Fatal("expected error for duplicate hunk aaa")
	}
}

func TestValidateClusteringResponse_UnknownHunk(t *testing.T) {
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa", "bbb", "ccc", "zzz"}},
		},
	}
	err := validateClusteringResponse(cr, testHunks())
	if err == nil {
		t.Fatal("expected error for unknown hunk zzz")
	}
}

func TestReorderCommitsByDependency(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "cmd/plan.go"},
		{HunkID: "h2", FilePath: "cmd/apply.go"},
		{HunkID: "h3", FilePath: "engine/models/commit_plan.go"},
		{HunkID: "h4", FilePath: "engine/planners/commit_planner.go"},
	}

	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Subject: "cli changes", Hunks: []string{"h1", "h2"}},
			{ID: "c2", Subject: "model changes", Hunks: []string{"h3"}},
			{ID: "c3", Subject: "planner changes", Hunks: []string{"h4"}},
		},
	}

	reorderCommitsByDependency(&plan, hunks)

	if plan.Commits[0].Subject != "model changes" {
		t.Errorf("expected models commit first, got %q", plan.Commits[0].Subject)
	}
	if plan.Commits[1].Subject != "planner changes" {
		t.Errorf("expected planner commit second, got %q", plan.Commits[1].Subject)
	}
	if plan.Commits[2].Subject != "cli changes" {
		t.Errorf("expected cli commit last, got %q", plan.Commits[2].Subject)
	}

	if plan.Commits[0].ID != "c1" || plan.Commits[1].ID != "c2" || plan.Commits[2].ID != "c3" {
		t.Errorf("IDs not renumbered: %s, %s, %s",
			plan.Commits[0].ID, plan.Commits[1].ID, plan.Commits[2].ID)
	}
}

func TestPackageLayer(t *testing.T) {
	tests := []struct {
		dir      string
		expected int
	}{
		{"engine/models", 0},
		{"engine/context", 1},
		{"engine/reasoning", 2},
		{"engine/planners", 3},
		{"engine/validators", 4},
		{"engine/executors", 5},
		{"cmd", 6},
		{".", 7},
	}
	for _, tt := range tests {
		got := packageLayer(tt.dir)
		if got != tt.expected {
			t.Errorf("packageLayer(%q) = %d, want %d", tt.dir, got, tt.expected)
		}
	}
}

func TestSummarizePatch_NoTruncation(t *testing.T) {
	patch := "+line1\n+line2\n+line3"
	got := summarizePatch(patch, 50)
	if got != patch {
		t.Errorf("expected no truncation, got %q", got)
	}
}

func TestSummarizePatch_Disabled(t *testing.T) {
	patch := "+line1\n+line2\n+line3"
	got := summarizePatch(patch, 0)
	if got != patch {
		t.Errorf("expected no truncation when disabled, got %q", got)
	}
}

func TestSummarizePatch_Truncated(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("+line%d", i))
	}
	patch := ""
	for i, l := range lines {
		if i > 0 {
			patch += "\n"
		}
		patch += l
	}

	got := summarizePatch(patch, 50)
	if got == patch {
		t.Error("expected truncation for 100-line patch with maxLines=50")
	}
	if !contains(got, "lines omitted") {
		t.Errorf("expected omission marker, got %q", got)
	}
	if !contains(got, "+line0") {
		t.Error("expected first lines to be preserved")
	}
	if !contains(got, "+line99") {
		t.Error("expected last lines to be preserved")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestValidateMessagingResponse_MissingGroup(t *testing.T) {
	clustering := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa"}},
			{ID: "g2", HunkIDs: []string{"bbb"}},
		},
	}
	mr := MessagingResponse{
		Commits: []CommitMessageWithGroup{
			{GroupID: "g1", CommitMessage: CommitMessage{Type: "feat", Subject: "x"}},
		},
	}
	err := validateMessagingResponse(mr, clustering, 72)
	if err == nil {
		t.Fatal("expected error for missing group g2")
	}
}

// ---------------------------------------------------------------------------
// New tests for scaling layers
// ---------------------------------------------------------------------------

func TestBuildIDMapping(t *testing.T) {
	hunks := testHunks()
	sortHunks(hunks) // aaa, bbb, ccc
	m := buildIDMapping(hunks)

	if m.toCompact["aaa"] != "h1" {
		t.Errorf("expected aaa -> h1, got %s", m.toCompact["aaa"])
	}
	if m.toCompact["bbb"] != "h2" {
		t.Errorf("expected bbb -> h2, got %s", m.toCompact["bbb"])
	}
	if m.toCompact["ccc"] != "h3" {
		t.Errorf("expected ccc -> h3, got %s", m.toCompact["ccc"])
	}

	if m.toReal["h1"] != "aaa" {
		t.Errorf("expected h1 -> aaa, got %s", m.toReal["h1"])
	}

	if !m.validIDs["h1"] || !m.validIDs["h2"] || !m.validIDs["h3"] {
		t.Error("expected h1, h2, h3 to be valid")
	}
	if m.validIDs["h4"] {
		t.Error("h4 should not be valid")
	}
}

func TestRemapClusteringResponse(t *testing.T) {
	hunks := testHunks()
	sortHunks(hunks)
	m := buildIDMapping(hunks)

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1", "h2"}},
			{ID: "g2", HunkIDs: []string{"h3"}},
		},
	}

	remapped := remapClusteringResponse(cr, m)

	if remapped.Groups[0].HunkIDs[0] != "aaa" || remapped.Groups[0].HunkIDs[1] != "bbb" {
		t.Errorf("g1 remap failed: %v", remapped.Groups[0].HunkIDs)
	}
	if remapped.Groups[1].HunkIDs[0] != "ccc" {
		t.Errorf("g2 remap failed: %v", remapped.Groups[1].HunkIDs)
	}
}

func TestRemapClusteringResponse_UnknownDropped(t *testing.T) {
	hunks := testHunks()
	sortHunks(hunks)
	m := buildIDMapping(hunks)

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1", "h999", "h2"}},
		},
	}

	remapped := remapClusteringResponse(cr, m)
	if len(remapped.Groups[0].HunkIDs) != 2 {
		t.Errorf("expected 2 hunks after dropping unknown, got %d", len(remapped.Groups[0].HunkIDs))
	}
}

func TestValidateCompactIDs_Valid(t *testing.T) {
	valid := map[string]bool{"h1": true, "h2": true, "h3": true}
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1", "h2"}},
			{ID: "g2", HunkIDs: []string{"h3"}},
		},
	}
	if err := validateCompactIDs(cr, valid, 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCompactIDs_Unknown(t *testing.T) {
	valid := map[string]bool{"h1": true, "h2": true}
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1", "h99"}},
		},
	}
	if err := validateCompactIDs(cr, valid, 10); err == nil {
		t.Fatal("expected error for unknown h99")
	}
}

func TestValidateCompactIDs_TooManyGroups(t *testing.T) {
	valid := map[string]bool{"h1": true}
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"h1"}},
			{ID: "g2", HunkIDs: []string{}},
		},
	}
	if err := validateCompactIDs(cr, valid, 1); err == nil {
		t.Fatal("expected error for exceeding max groups")
	}
}

func TestPreGroupByFile(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "a1", FilePath: "auth.go"},
		{HunkID: "a2", FilePath: "auth.go"},
		{HunkID: "b1", FilePath: "ui.go"},
		{HunkID: "c1", FilePath: "config.go"},
	}

	units := preGroupByFile(hunks)

	if len(units) != 3 {
		t.Fatalf("expected 3 file units, got %d", len(units))
	}

	if units[0].filePath != "auth.go" || len(units[0].hunkIDs) != 2 {
		t.Errorf("first unit: %s with %d hunks", units[0].filePath, len(units[0].hunkIDs))
	}
	if units[0].id != "f1" {
		t.Errorf("expected f1, got %s", units[0].id)
	}
	if units[1].filePath != "ui.go" || units[1].id != "f2" {
		t.Errorf("second unit: %s %s", units[1].filePath, units[1].id)
	}
	if units[2].filePath != "config.go" || units[2].id != "f3" {
		t.Errorf("third unit: %s %s", units[2].filePath, units[2].id)
	}
}

func TestExpandFileUnits(t *testing.T) {
	units := []fileUnit{
		{id: "f1", filePath: "auth.go", hunkIDs: []string{"a1", "a2"}},
		{id: "f2", filePath: "ui.go", hunkIDs: []string{"b1"}},
	}

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"f1"}},
			{ID: "g2", HunkIDs: []string{"f2"}},
		},
	}

	expanded := expandFileUnits(cr, units)

	if len(expanded.Groups[0].HunkIDs) != 2 {
		t.Errorf("expected 2 hunks in g1, got %d", len(expanded.Groups[0].HunkIDs))
	}
	if expanded.Groups[0].HunkIDs[0] != "a1" || expanded.Groups[0].HunkIDs[1] != "a2" {
		t.Errorf("g1 expansion wrong: %v", expanded.Groups[0].HunkIDs)
	}
	if len(expanded.Groups[1].HunkIDs) != 1 || expanded.Groups[1].HunkIDs[0] != "b1" {
		t.Errorf("g2 expansion wrong: %v", expanded.Groups[1].HunkIDs)
	}
}

func TestExpandFileUnits_MixedGroups(t *testing.T) {
	units := []fileUnit{
		{id: "f1", filePath: "a.go", hunkIDs: []string{"h1"}},
		{id: "f2", filePath: "b.go", hunkIDs: []string{"h2", "h3"}},
		{id: "f3", filePath: "c.go", hunkIDs: []string{"h4"}},
	}

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"f1", "f3"}},
			{ID: "g2", HunkIDs: []string{"f2"}},
		},
	}

	expanded := expandFileUnits(cr, units)

	if len(expanded.Groups[0].HunkIDs) != 2 {
		t.Errorf("expected 2 hunks in g1 (f1+f3), got %d", len(expanded.Groups[0].HunkIDs))
	}
	if len(expanded.Groups[1].HunkIDs) != 2 {
		t.Errorf("expected 2 hunks in g2 (f2), got %d", len(expanded.Groups[1].HunkIDs))
	}
}

func TestSplitIntoBatches(t *testing.T) {
	units := []fileUnit{
		{id: "f1", filePath: "cmd/a.go"},
		{id: "f2", filePath: "cmd/b.go"},
		{id: "f3", filePath: "engine/c.go"},
		{id: "f4", filePath: "engine/d.go"},
		{id: "f5", filePath: "engine/e.go"},
	}

	batches := splitIntoBatches(units, 3)

	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}
	if len(batches[0]) != 3 {
		t.Errorf("first batch: expected 3, got %d", len(batches[0]))
	}
	if len(batches[1]) != 2 {
		t.Errorf("second batch: expected 2, got %d", len(batches[1]))
	}
}

func TestSplitIntoBatches_DirectoryProximity(t *testing.T) {
	units := []fileUnit{
		{id: "f1", filePath: "z/file.go"},
		{id: "f2", filePath: "a/file.go"},
		{id: "f3", filePath: "a/other.go"},
		{id: "f4", filePath: "m/file.go"},
	}

	batches := splitIntoBatches(units, 2)

	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}

	// After sorting by dir: a/, a/, m/, z/ → batch 1 = [a,a], batch 2 = [m,z]
	for _, u := range batches[0] {
		if u.filePath != "a/file.go" && u.filePath != "a/other.go" {
			t.Errorf("expected 'a/' directory files in first batch, got %s", u.filePath)
		}
	}
}

func TestConcatenateBatchGroups(t *testing.T) {
	results := []batchResult{
		{cr: ClusteringResponse{Groups: []ClusterGroup{
			{ID: "b1g1", HunkIDs: []string{"f1"}},
			{ID: "b1g2", HunkIDs: []string{"f2"}},
		}}},
		{cr: ClusteringResponse{Groups: []ClusterGroup{
			{ID: "b2g1", HunkIDs: []string{"f3"}},
		}}},
	}

	merged := concatenateBatchGroups(results)

	if len(merged.Groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(merged.Groups))
	}
	if merged.Groups[0].ID != "g1" || merged.Groups[1].ID != "g2" || merged.Groups[2].ID != "g3" {
		t.Errorf("unexpected IDs: %s, %s, %s",
			merged.Groups[0].ID, merged.Groups[1].ID, merged.Groups[2].ID)
	}
}

func TestDeduplicateGroups(t *testing.T) {
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"a", "b", "a"}},
			{ID: "g2", HunkIDs: []string{"c", "b"}},
		},
	}

	deduped := deduplicateGroups(cr)

	if len(deduped.Groups[0].HunkIDs) != 2 {
		t.Errorf("g1: expected 2 after dedup, got %d", len(deduped.Groups[0].HunkIDs))
	}
	// "b" was already seen in g1, so g2 should only have "c"
	if len(deduped.Groups[1].HunkIDs) != 1 {
		t.Errorf("g2: expected 1 after dedup, got %d", len(deduped.Groups[1].HunkIDs))
	}
	if deduped.Groups[1].HunkIDs[0] != "c" {
		t.Errorf("g2: expected 'c', got %s", deduped.Groups[1].HunkIDs[0])
	}
}

func TestConsolidateSingleFileGroups_AllSameFile(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "a1", FilePath: "README.md"},
		{HunkID: "a2", FilePath: "README.md"},
		{HunkID: "a3", FilePath: "README.md"},
		{HunkID: "a4", FilePath: "README.md"},
	}

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"a1", "a2"}},
			{ID: "g2", HunkIDs: []string{"a3"}},
			{ID: "g3", HunkIDs: []string{"a4"}},
		},
	}

	result := consolidateSingleFileGroups(cr, hunks)

	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group after consolidation, got %d", len(result.Groups))
	}
	if len(result.Groups[0].HunkIDs) != 4 {
		t.Errorf("expected 4 hunks in merged group, got %d", len(result.Groups[0].HunkIDs))
	}
	if result.Groups[0].ID != "g1" {
		t.Errorf("expected renumbered ID g1, got %s", result.Groups[0].ID)
	}
}

func TestConsolidateSingleFileGroups_MixedFiles(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "a1", FilePath: "README.md"},
		{HunkID: "a2", FilePath: "README.md"},
		{HunkID: "b1", FilePath: "main.go"},
		{HunkID: "c1", FilePath: "config.go"},
		{HunkID: "c2", FilePath: "config.go"},
	}

	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"a1"}},
			{ID: "g2", HunkIDs: []string{"b1", "c1"}},
			{ID: "g3", HunkIDs: []string{"a2"}},
			{ID: "g4", HunkIDs: []string{"c2"}},
		},
	}

	result := consolidateSingleFileGroups(cr, hunks)

	// g1 and g3 both only touch README.md → merged.
	// g2 touches main.go + config.go → left alone.
	// g4 only touches config.go → left alone (only one single-file group for config.go).
	if len(result.Groups) != 3 {
		t.Fatalf("expected 3 groups after consolidation, got %d", len(result.Groups))
	}
}

func TestConsolidateSingleFileGroups_NoMergeNeeded(t *testing.T) {
	hunks := testHunks()
	cr := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa", "bbb"}},
			{ID: "g2", HunkIDs: []string{"ccc"}},
		},
	}

	result := consolidateSingleFileGroups(cr, hunks)

	if len(result.Groups) != 2 {
		t.Fatalf("expected 2 groups (no merge needed), got %d", len(result.Groups))
	}
}

func TestBuildClusteringInput_UsesCompactIDs(t *testing.T) {
	hunks := testHunks()
	sortHunks(hunks)

	input, idMap := buildClusteringInput(hunks, 50)

	if !contains(input, "h1") || !contains(input, "h2") || !contains(input, "h3") {
		t.Error("expected compact IDs h1, h2, h3 in input")
	}
	// Real hunk IDs should NOT appear in the prompt
	if contains(input, "aaa") || contains(input, "bbb") || contains(input, "ccc") {
		t.Error("real hunk IDs should not appear in clustering input")
	}

	if idMap.toReal["h1"] != "aaa" {
		t.Errorf("mapping h1 -> %s, expected aaa", idMap.toReal["h1"])
	}
}

func TestBuildFileUnitClusteringInput(t *testing.T) {
	hunks := testHunks()
	units := preGroupByFile(hunks)

	input := buildFileUnitClusteringInput(units, hunks, 50)

	if !contains(input, "f1") || !contains(input, "f2") {
		t.Error("expected file unit IDs f1, f2 in input")
	}
	if !contains(input, "auth.go") || !contains(input, "ui.go") {
		t.Error("expected file paths in input")
	}
}
