package context

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/engine/models"
)

// EngineContext holds all repository state needed by planners.
type EngineContext struct {
	BaseRef       string
	RootPath      string
	Hunks         []models.Hunk
	RecentCommits []string
	Config        config.EngineConfig
}

// BuildContext collects git state and returns an EngineContext.
// It shells out to git for diff and log; this is the only place in the
// context package that has side effects.
func BuildContext(ctx context.Context, cfg config.EngineConfig) (EngineContext, error) {
	root, _ := repoRoot(ctx)

	// Use "diff HEAD" to capture both staged and unstaged changes.
	// Run from repo root so paths are consistent regardless of CWD.
	trackedDiff, err := gitCommandInDir(ctx, root, "diff", "HEAD")
	if err != nil {
		trackedDiff = ""
	}

	untrackedDiff, err := collectUntrackedDiff(ctx, root)
	if err != nil {
		return EngineContext{}, fmt.Errorf("collecting untracked files: %w", err)
	}

	fullDiff := trackedDiff + untrackedDiff
	hunks := ParseDiff(fullDiff)

	if len(cfg.Engine.IgnorePatterns) > 0 {
		hunks = filterHunks(hunks, cfg.Engine.IgnorePatterns)
	}

	if cfg.AI.MaxDiffKB > 0 && len(fullDiff)/1024 > cfg.AI.MaxDiffKB {
		return EngineContext{}, fmt.Errorf("diff size %d KB exceeds max_diff_kb (%d KB)", len(fullDiff)/1024, cfg.AI.MaxDiffKB)
	}

	logOut, err := gitCommand(ctx, "log", "--oneline", "-n", "10")
	if err != nil {
		logOut = ""
	}

	var commits []string
	for _, line := range strings.Split(strings.TrimSpace(logOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			commits = append(commits, line)
		}
	}

	baseRef, err := gitCommand(ctx, "rev-parse", "HEAD")
	if err != nil {
		baseRef = "WORKING_TREE"
	}
	baseRef = strings.TrimSpace(baseRef)

	return EngineContext{
		BaseRef:       baseRef,
		RootPath:      root,
		Hunks:         hunks,
		RecentCommits: commits,
		Config:        cfg,
	}, nil
}

// collectUntrackedDiff finds untracked files (respecting .gitignore) and
// generates unified diff output for each, treating them as new file additions.
// root is the repo top-level directory; all paths are resolved relative to it
// so the function works correctly regardless of the caller's CWD.
func collectUntrackedDiff(ctx context.Context, root string) (string, error) {
	out, err := gitCommandInDir(ctx, root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", nil
	}

	var sb strings.Builder
	for _, file := range strings.Split(strings.TrimSpace(out), "\n") {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}

		filePath := filepath.ToSlash(file)

		absPath := file
		if root != "" {
			absPath = filepath.Join(root, file)
		}

		if isBinaryFile(absPath) {
			continue
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if len(content) == 0 {
			continue
		}

		text := strings.ReplaceAll(string(content), "\r\n", "\n")
		hasTrailingNewline := strings.HasSuffix(text, "\n")
		text = strings.TrimRight(text, "\n")
		lines := strings.Split(text, "\n")
		fmt.Fprintf(&sb, "diff --git a/%s b/%s\n", filePath, filePath)
		fmt.Fprintf(&sb, "new file mode 100644\n")
		fmt.Fprintf(&sb, "--- /dev/null\n")
		fmt.Fprintf(&sb, "+++ b/%s\n", filePath)
		fmt.Fprintf(&sb, "@@ -0,0 +1,%d @@\n", len(lines))
		for _, line := range lines {
			fmt.Fprintf(&sb, "+%s\n", line)
		}
		if !hasTrailingNewline {
			sb.WriteString("\\ No newline at end of file\n")
		}
	}

	return sb.String(), nil
}

func isBinaryFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	binaryExts := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true,
		".bin": true, ".o": true, ".a": true, ".lib": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
		".ico": true, ".webp": true, ".bmp": true, ".tiff": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true,
		".7z": true, ".rar": true, ".xz": true,
		".pdf": true, ".wasm": true, ".pyc": true,
	}
	if binaryExts[ext] {
		return true
	}
	return hasBinaryContent(path)
}

// hasBinaryContent reads the first 512 bytes and checks for null bytes,
// which is the same heuristic git uses to detect binary files.
func hasBinaryContent(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return false
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// filterHunks removes hunks whose file path matches any ignore pattern.
func filterHunks(hunks []models.Hunk, patterns []string) []models.Hunk {
	var filtered []models.Hunk
	for _, h := range hunks {
		if !shouldIgnore(h.FilePath, patterns) {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

func shouldIgnore(filePath string, patterns []string) bool {
	base := filepath.Base(filePath)
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, filePath); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}
	}
	return false
}

// RepoRoot returns the repository root directory for the current working directory,
// or "" if not in a git repo. Used by replay to enable import-graph ordering.
func RepoRoot(ctx context.Context) string {
	root, err := repoRoot(ctx)
	if err != nil {
		return ""
	}
	return root
}

// repoRoot returns the absolute path of the repository's top-level directory.
func repoRoot(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(string(out), "\r\n", "\n")), nil
}

func gitCommandInDir(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	result := strings.ReplaceAll(string(out), "\r\n", "\n")
	return result, nil
}

func gitCommand(ctx context.Context, args ...string) (string, error) {
	return gitCommandInDir(ctx, "", args...)
}
