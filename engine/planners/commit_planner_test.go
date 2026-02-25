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
