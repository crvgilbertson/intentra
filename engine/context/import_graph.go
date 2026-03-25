package context

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crvgilbertson/intentra/engine/models"
)

// ImportGraph holds package dependency data for ordering commits.
// Built via go list when the repo is a Go module.
type ImportGraph struct {
	FileToPackage    map[string]string   // file path (slash) -> import path
	PackageImports   map[string][]string // pkg -> direct imports
	PackageLayer     map[string]int      // pkg -> topological layer (0 = leaves)
	OrderingStrategy string              // "import_graph" or "fallback"
}

// goListPackage is the JSON shape from go list -json.
type goListPackage struct {
	ImportPath string   `json:"ImportPath"`
	Dir        string   `json:"Dir"`
	Imports    []string `json:"Imports"`
	GoFiles    []string `json:"GoFiles"`
}

// BuildImportGraph runs go list and builds the dependency graph.
// Returns nil, nil if this is not a Go repo or go list fails.
func BuildImportGraph(ctx context.Context, root string) (*ImportGraph, error) {
	if root == "" {
		return nil, fmt.Errorf("root required")
	}

	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	g := &ImportGraph{
		FileToPackage:    make(map[string]string),
		PackageImports:   make(map[string][]string),
		OrderingStrategy: "import_graph",
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var pkg goListPackage
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("parse go list: %w", err)
		}
		if pkg.ImportPath == "" || pkg.Dir == "" {
			continue
		}

		g.PackageImports[pkg.ImportPath] = pkg.Imports

		for _, f := range pkg.GoFiles {
			if !strings.HasSuffix(f, ".go") {
				continue
			}
			absPath := filepath.Join(pkg.Dir, f)
			rel, err := filepath.Rel(root, absPath)
			if err != nil {
				continue
			}
			slash := filepath.ToSlash(rel)
			g.FileToPackage[slash] = pkg.ImportPath
		}
	}

	g.PackageLayer = computePackageLayers(g.PackageImports)
	return g, nil
}

func computePackageLayers(imports map[string][]string) map[string]int {
	// Layer 0 = leaves (no in-repo deps), layer 1 = depends only on leaves, etc.
	// Foundational packages get lower layers and are committed first.
	layer := make(map[string]int)

	var layerOf func(string) int
	layerOf = func(pkg string) int {
		if l, ok := layer[pkg]; ok {
			return l
		}
		deps := imports[pkg]
		maxDep := -1
		for _, d := range deps {
			if d == "" {
				continue
			}
			if _, inMap := imports[d]; inMap {
				ld := layerOf(d)
				if ld > maxDep {
					maxDep = ld
				}
			}
		}
		l := maxDep + 1
		layer[pkg] = l
		return l
	}

	for pkg := range imports {
		_ = layerOf(pkg)
	}
	return layer
}

// OrderCommitsByImportGraph reorders plan commits by import dependency.
// Commits touching packages that others depend on come first.
// Uses stable sort when layer is equal (existing order or path sort).
func OrderCommitsByImportGraph(plan *models.CommitPlan, hunks []models.Hunk, g *ImportGraph, hunkToFile map[string]string) {
	if g == nil || len(g.FileToPackage) == 0 {
		return
	}
	_ = hunks

	commitLayers := make(map[string]int, len(plan.Commits))

	for _, c := range plan.Commits {
		minLayer := 999
		for _, hid := range c.Hunks {
			filePath := hunkToFile[hid]
			pkg := g.FileToPackage[filePath]
			if pkg != "" {
				if l, ok := g.PackageLayer[pkg]; ok && l < minLayer {
					minLayer = l
				}
			}
		}
		if minLayer == 999 {
			minLayer = 0
		}
		commitLayers[c.ID] = minLayer
	}

	sort.SliceStable(plan.Commits, func(i, j int) bool {
		li := commitLayers[plan.Commits[i].ID]
		lj := commitLayers[plan.Commits[j].ID]
		if li != lj {
			return li < lj
		}
		return false
	})

	for i := range plan.Commits {
		plan.Commits[i].ID = fmt.Sprintf("c%d", i+1)
	}
}
