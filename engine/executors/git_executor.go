package executors

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"intentra/engine/models"
)

// GitExecutor applies a CommitPlan by staging hunks and committing.
type GitExecutor struct {
	repoDir string
}

func NewGitExecutor(repoDir string) *GitExecutor {
	return &GitExecutor{repoDir: repoDir}
}

func (e *GitExecutor) Execute(ctx context.Context, plan models.Plan, dryRun bool) error {
	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		return fmt.Errorf("executor expects *CommitPlan, got %T", plan)
	}

	hunkMap := buildHunkMap(cp)

	snapshotRef, err := e.snapshotIndex(ctx)
	if err != nil {
		return fmt.Errorf("snapshot index: %w", err)
	}

	for i, commit := range cp.Commits {
		if dryRun {
			fmt.Printf("[dry-run] commit %d/%d: %s\n", i+1, len(cp.Commits), commit.FullSubject())
			for _, hid := range commit.Hunks {
				if h, ok := hunkMap[hid]; ok {
					fmt.Printf("  hunk: %s %s\n", h.FilePath, h.Header)
				}
			}
			continue
		}

		if err := e.applyCommit(ctx, commit, hunkMap); err != nil {
			restoreErr := e.restoreIndex(ctx, snapshotRef)
			if restoreErr != nil {
				return fmt.Errorf("commit %s failed: %w (additionally, restore failed: %v)", commit.ID, err, restoreErr)
			}
			return fmt.Errorf("commit %s failed (index restored): %w", commit.ID, err)
		}
	}

	return nil
}

func (e *GitExecutor) applyCommit(ctx context.Context, commit models.CommitUnit, hunkMap map[string]models.Hunk) error {
	patch := buildPatch(commit, hunkMap)

	tmpFile, err := os.CreateTemp("", "intentra-patch-*.patch")
	if err != nil {
		return fmt.Errorf("creating temp patch: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(patch); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing patch: %w", err)
	}
	tmpFile.Close()

	if err := e.git(ctx, "apply", "--cached", tmpFile.Name()); err != nil {
		return fmt.Errorf("git apply --cached: %w", err)
	}

	args := []string{"commit", "-m", commit.FullSubject()}
	if commit.Body != nil && *commit.Body != "" {
		args = append(args, "-m", *commit.Body)
	}
	for _, f := range commit.Footers {
		args = append(args, "-m", f.Token+": "+f.Value)
	}

	if err := e.git(ctx, args...); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return nil
}

func (e *GitExecutor) snapshotIndex(ctx context.Context) (string, error) {
	out, err := e.gitOutput(ctx, "stash", "create")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(out)
	if ref == "" {
		ref, err = e.gitOutput(ctx, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		ref = strings.TrimSpace(ref)
	}
	return ref, nil
}

func (e *GitExecutor) restoreIndex(ctx context.Context, ref string) error {
	return e.git(ctx, "read-tree", ref)
}

func (e *GitExecutor) git(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (e *GitExecutor) gitOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = e.repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// buildHunkMap creates a lookup of hunk_id -> Hunk from the commit plan.
// It walks all commits to collect every hunk referenced.
func buildHunkMap(cp *models.CommitPlan) map[string]models.Hunk {
	// The CommitPlan only stores hunk IDs in commits; full Hunk data is
	// not embedded. For patch generation we need the full hunk, which the
	// caller must supply. We store them here when available.
	return make(map[string]models.Hunk)
}

// GitExecutorWithHunks is a variant that carries full hunk data for patch generation.
type GitExecutorWithHunks struct {
	*GitExecutor
	hunks []models.Hunk
}

func NewGitExecutorWithHunks(repoDir string, hunks []models.Hunk) *GitExecutorWithHunks {
	return &GitExecutorWithHunks{
		GitExecutor: NewGitExecutor(repoDir),
		hunks:       hunks,
	}
}

func (e *GitExecutorWithHunks) Execute(ctx context.Context, plan models.Plan, dryRun bool) error {
	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		return fmt.Errorf("executor expects *CommitPlan, got %T", plan)
	}

	hunkMap := make(map[string]models.Hunk)
	for _, h := range e.hunks {
		hunkMap[h.HunkID] = h
	}

	snapshotRef, err := e.snapshotIndex(ctx)
	if err != nil {
		return fmt.Errorf("snapshot index: %w", err)
	}

	for i, commit := range cp.Commits {
		if dryRun {
			fmt.Printf("[dry-run] commit %d/%d: %s\n", i+1, len(cp.Commits), commit.FullSubject())
			for _, hid := range commit.Hunks {
				if h, ok := hunkMap[hid]; ok {
					fmt.Printf("  hunk: %s %s\n", h.FilePath, h.Header)
				}
			}
			continue
		}

		if err := e.applyCommit(ctx, commit, hunkMap); err != nil {
			restoreErr := e.restoreIndex(ctx, snapshotRef)
			if restoreErr != nil {
				return fmt.Errorf("commit %s failed: %w (additionally, restore failed: %v)", commit.ID, err, restoreErr)
			}
			return fmt.Errorf("commit %s failed (index restored): %w", commit.ID, err)
		}
	}

	return nil
}

// buildPatch constructs a unified diff patch for a single commit's hunks.
func buildPatch(commit models.CommitUnit, hunkMap map[string]models.Hunk) string {
	fileHunks := make(map[string][]models.Hunk)
	for _, hid := range commit.Hunks {
		h, ok := hunkMap[hid]
		if !ok {
			continue
		}
		fileHunks[h.FilePath] = append(fileHunks[h.FilePath], h)
	}

	var sb strings.Builder
	for filePath, hunks := range fileHunks {
		absA := filepath.ToSlash("a/" + filePath)
		absB := filepath.ToSlash("b/" + filePath)
		fmt.Fprintf(&sb, "diff --git %s %s\n", absA, absB)
		fmt.Fprintf(&sb, "--- %s\n", absA)
		fmt.Fprintf(&sb, "+++ %s\n", absB)
		for _, h := range hunks {
			fmt.Fprintf(&sb, "%s\n", h.Header)
			if h.Patch != "" {
				fmt.Fprintf(&sb, "%s\n", h.Patch)
			}
		}
	}

	return sb.String()
}
