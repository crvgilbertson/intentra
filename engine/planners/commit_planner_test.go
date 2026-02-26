package planners

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	enginectx "intentra/engine/context"
	"intentra/engine/models"
	"intentra/config"
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

func TestCommitPlanner_BuildPlan_Success(t *testing.T) {
	clusterResp := ClusteringResponse{
		Groups: []ClusterGroup{
			{ID: "g1", HunkIDs: []string{"aaa", "bbb"}},
			{ID: "g2", HunkIDs: []string{"ccc"}},
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

	if cp.ToolVersion != "0.1.0" {
		t.Errorf("expected tool version 0.1.0, got %s", cp.ToolVersion)
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
	err := validateMessagingResponse(mr, clustering)
	if err == nil {
		t.Fatal("expected error for missing group g2")
	}
}
