package context

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func TestBuildImportGraph_IntentraRepo(t *testing.T) {
	ctx := context.Background()
	root, err := os.Getwd()
	if err != nil {
		t.Skip("cannot get cwd")
	}
	for root != "/" && root != "" {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		root = filepath.Dir(root)
	}
	if root == "" || root == "/" {
		t.Skip("not in a Go module")
	}

	g, err := BuildImportGraph(ctx, root)
	if err != nil {
		t.Fatalf("BuildImportGraph: %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if g.OrderingStrategy != "import_graph" {
		t.Errorf("OrderingStrategy: got %s, want import_graph", g.OrderingStrategy)
	}
	if len(g.FileToPackage) == 0 {
		t.Error("expected FileToPackage to be non-empty in intentra repo")
	}
	if len(g.PackageImports) == 0 {
		t.Error("expected PackageImports to be non-empty in intentra repo")
	}
	if len(g.PackageLayer) == 0 {
		t.Error("expected PackageLayer to be non-empty in intentra repo")
	}
}

func TestBuildImportGraph_NonGoDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	g, err := BuildImportGraph(ctx, dir)
	if err == nil && g != nil {
		t.Log("BuildImportGraph in empty dir: no error (go list may return empty)")
	}
}

func TestOrderCommitsByImportGraph_EmptyGraph(t *testing.T) {
	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: []string{"h1"}},
			{ID: "c2", Hunks: []string{"h2"}},
		},
	}
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "cmd/foo.go"},
		{HunkID: "h2", FilePath: "engine/models/bar.go"},
	}
	hunkToFile := map[string]string{"h1": "cmd/foo.go", "h2": "engine/models/bar.go"}

	OrderCommitsByImportGraph(plan, hunks, nil, hunkToFile)
	// nil graph should be a no-op
	if plan.Commits[0].ID != "c1" {
		t.Errorf("nil graph should not reorder: got %s", plan.Commits[0].ID)
	}
}

func TestComputePackageLayers_Deterministic(t *testing.T) {
	// Single leaf
	layer1 := computePackageLayers(map[string][]string{"a": {}})
	if layer1["a"] != 0 {
		t.Errorf("leaf: got %d, want 0", layer1["a"])
	}

	// A imports B (B leaf)
	layer2 := computePackageLayers(map[string][]string{
		"a": {"b"},
		"b": {},
	})
	if layer2["b"] != 0 {
		t.Errorf("b (leaf): got %d, want 0", layer2["b"])
	}
	if layer2["a"] != 1 {
		t.Errorf("a (depends on b): got %d, want 1", layer2["a"])
	}

	// Chain: cmd -> engine -> models
	imports := map[string][]string{
		"mypkg/cmd":    {"mypkg/engine"},
		"mypkg/engine": {"mypkg/models"},
		"mypkg/models": {},
	}
	layer := computePackageLayers(imports)
	if layer["mypkg/models"] != 0 {
		t.Errorf("models (leaf): got %d, want 0", layer["mypkg/models"])
	}
	if layer["mypkg/engine"] != 1 {
		t.Errorf("engine: got %d, want 1", layer["mypkg/engine"])
	}
	if layer["mypkg/cmd"] != 2 {
		t.Errorf("cmd: got %d, want 2", layer["mypkg/cmd"])
	}
}

func TestOrderCommitsByImportGraph_ReordersByLayer(t *testing.T) {
	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Subject: "cmd changes", Hunks: []string{"h1"}},
			{ID: "c2", Subject: "engine changes", Hunks: []string{"h2"}},
			{ID: "c3", Subject: "model changes", Hunks: []string{"h3"}},
		},
	}

	graph := &ImportGraph{
		FileToPackage: map[string]string{
			"cmd/root.go":            "example/cmd",
			"engine/service.go":      "example/engine",
			"engine/models/types.go": "example/models",
		},
		PackageLayer: map[string]int{
			"example/cmd":    2,
			"example/engine": 1,
			"example/models": 0,
		},
	}

	hunkToFile := map[string]string{
		"h1": "cmd/root.go",
		"h2": "engine/service.go",
		"h3": "engine/models/types.go",
	}

	OrderCommitsByImportGraph(plan, nil, graph, hunkToFile)

	if plan.Commits[0].Subject != "model changes" {
		t.Fatalf("first commit = %q, want model changes", plan.Commits[0].Subject)
	}
	if plan.Commits[1].Subject != "engine changes" {
		t.Fatalf("second commit = %q, want engine changes", plan.Commits[1].Subject)
	}
	if plan.Commits[2].Subject != "cmd changes" {
		t.Fatalf("third commit = %q, want cmd changes", plan.Commits[2].Subject)
	}
	if plan.Commits[0].ID != "c1" || plan.Commits[1].ID != "c2" || plan.Commits[2].ID != "c3" {
		t.Fatalf("expected IDs to be renumbered sequentially, got %s, %s, %s",
			plan.Commits[0].ID, plan.Commits[1].ID, plan.Commits[2].ID)
	}
}
