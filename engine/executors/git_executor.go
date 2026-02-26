package executors

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crvgilbertson/intentra/engine/models"
)

// GitExecutor applies a CommitPlan by staging hunks and committing.
type GitExecutor struct {
	repoDir     string
	signCommits bool
}

func NewGitExecutor(repoDir string) *GitExecutor {
	return &GitExecutor{repoDir: repoDir}
}

// indexSnapshot captures the repo state before apply so we can roll back.
type indexSnapshot struct {
	originalHead string // empty if no commits yet
	indexTree    string // tree object of the index at snapshot time
}

func (e *GitExecutor) snapshotState(ctx context.Context) (*indexSnapshot, error) {
	head, err := e.gitOutput(ctx, "rev-parse", "HEAD")
	if err != nil {
		head = ""
	} else {
		head = strings.TrimSpace(head)
	}

	tree, err := e.gitOutput(ctx, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("write-tree: %w", err)
	}

	return &indexSnapshot{
		originalHead: head,
		indexTree:    strings.TrimSpace(tree),
	}, nil
}

// rollback undoes any commits made during apply and restores the index.
func (e *GitExecutor) rollback(ctx context.Context, snap *indexSnapshot, committedCount int) error {
	if committedCount > 0 && snap.originalHead != "" {
		if err := e.git(ctx, "reset", "--soft", snap.originalHead); err != nil {
			return fmt.Errorf("reset HEAD: %w", err)
		}
	}
	if err := e.git(ctx, "read-tree", snap.indexTree); err != nil {
		return fmt.Errorf("restore index: %w", err)
	}
	return nil
}

func (e *GitExecutor) Execute(ctx context.Context, plan models.Plan, dryRun bool) error {
	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		return fmt.Errorf("executor expects *CommitPlan, got %T", plan)
	}

	hunkMap := buildHunkMap(cp)

	snap, err := e.snapshotState(ctx)
	if err != nil {
		return fmt.Errorf("snapshot state: %w", err)
	}

	if snap.originalHead != "" {
		if err := e.git(ctx, "read-tree", "HEAD"); err != nil {
			return fmt.Errorf("resetting index: %w", err)
		}
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
			if rollbackErr := e.rollback(ctx, snap, i); rollbackErr != nil {
				return fmt.Errorf("commit %s failed: %w (additionally, rollback failed: %v)", commit.ID, err, rollbackErr)
			}
			if i > 0 {
				return fmt.Errorf("commit %s failed (rolled back %d prior commit(s)): %w", commit.ID, i, err)
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

	args := []string{"commit"}
	if e.signCommits {
		args = append(args, "-S")
	}
	args = append(args, "-m", commit.FullSubject())
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
// Returns empty when full hunk data isn't available (use GitExecutorWithHunks).
func buildHunkMap(cp *models.CommitPlan) map[string]models.Hunk {
	return make(map[string]models.Hunk)
}

// GitExecutorWithHunks carries full hunk data for patch generation.
type GitExecutorWithHunks struct {
	*GitExecutor
	hunks []models.Hunk
}

func NewGitExecutorWithHunks(repoDir string, hunks []models.Hunk, signCommits bool) *GitExecutorWithHunks {
	return &GitExecutorWithHunks{
		GitExecutor: &GitExecutor{repoDir: repoDir, signCommits: signCommits},
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

	snap, err := e.snapshotState(ctx)
	if err != nil {
		return fmt.Errorf("snapshot state: %w", err)
	}

	// Reset index to HEAD so patches (relative to HEAD) apply cleanly.
	// This also prevents pre-existing staged changes from leaking into commits.
	if snap.originalHead != "" {
		if err := e.git(ctx, "read-tree", "HEAD"); err != nil {
			return fmt.Errorf("resetting index: %w", err)
		}
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
			if rollbackErr := e.rollback(ctx, snap, i); rollbackErr != nil {
				return fmt.Errorf("commit %s failed: %w (additionally, rollback failed: %v)", commit.ID, err, rollbackErr)
			}
			if i > 0 {
				return fmt.Errorf("commit %s failed (rolled back %d prior commit(s)): %w", commit.ID, i, err)
			}
			return fmt.Errorf("commit %s failed (index restored): %w", commit.ID, err)
		}
	}

	return nil
}

// buildPatch constructs a unified diff patch for a single commit's hunks.
// Files are sorted for deterministic output.
func buildPatch(commit models.CommitUnit, hunkMap map[string]models.Hunk) string {
	fileHunks := make(map[string][]models.Hunk)
	for _, hid := range commit.Hunks {
		h, ok := hunkMap[hid]
		if !ok {
			continue
		}
		fileHunks[h.FilePath] = append(fileHunks[h.FilePath], h)
	}

	files := make([]string, 0, len(fileHunks))
	for f := range fileHunks {
		files = append(files, f)
	}
	sort.Strings(files)

	var sb strings.Builder
	for _, filePath := range files {
		hunks := fileHunks[filePath]
		pathSlash := filepath.ToSlash(filePath)
		absB := "b/" + pathSlash

		var renamedFrom string
		var isNew, isDeleted bool
		var oldMode, newMode string
		for _, h := range hunks {
			if h.NewFile {
				isNew = true
			}
			if h.DeletedFile {
				isDeleted = true
			}
			if h.RenamedFrom != "" {
				renamedFrom = h.RenamedFrom
			}
			if h.OldMode != "" {
				oldMode = h.OldMode
			}
			if h.NewMode != "" {
				newMode = h.NewMode
			}
		}

		absA := "a/" + pathSlash
		if renamedFrom != "" {
			absA = "a/" + filepath.ToSlash(renamedFrom)
		}

		hasContentHunks := false
		for _, h := range hunks {
			if h.Header != "" {
				hasContentHunks = true
				break
			}
		}

		fmt.Fprintf(&sb, "diff --git %s %s\n", absA, absB)

		if isDeleted {
			fmt.Fprintf(&sb, "deleted file mode 100644\n")
		} else if isNew {
			fmt.Fprintf(&sb, "new file mode 100644\n")
		}

		if oldMode != "" && newMode != "" {
			fmt.Fprintf(&sb, "old mode %s\n", oldMode)
			fmt.Fprintf(&sb, "new mode %s\n", newMode)
		}

		if renamedFrom != "" {
			fmt.Fprintf(&sb, "rename from %s\n", filepath.ToSlash(renamedFrom))
			fmt.Fprintf(&sb, "rename to %s\n", pathSlash)
		}

		if hasContentHunks {
			if isDeleted {
				fmt.Fprintf(&sb, "--- %s\n", absA)
				fmt.Fprintf(&sb, "+++ /dev/null\n")
			} else if isNew {
				fmt.Fprintf(&sb, "--- /dev/null\n")
				fmt.Fprintf(&sb, "+++ %s\n", absB)
			} else {
				fmt.Fprintf(&sb, "--- %s\n", absA)
				fmt.Fprintf(&sb, "+++ %s\n", absB)
			}

			for _, h := range hunks {
				header := strings.TrimRight(h.Header, "\r")
				if header == "" {
					continue
				}
				fmt.Fprintf(&sb, "%s\n", header)
				if h.Patch != "" {
					patch := strings.ReplaceAll(h.Patch, "\r\n", "\n")
					patch = strings.ReplaceAll(patch, "\r", "")
					fmt.Fprintf(&sb, "%s\n", patch)
				}
			}
		}
	}

	return sb.String()
}
