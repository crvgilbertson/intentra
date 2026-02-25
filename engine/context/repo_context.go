package context

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"intentra/config"
	"intentra/engine/models"
)

// EngineContext holds all repository state needed by planners.
type EngineContext struct {
	BaseRef       string
	Hunks         []models.Hunk
	RecentCommits []string
	Config        config.EngineConfig
}

// BuildContext collects git state and returns an EngineContext.
// It shells out to git for diff and log; this is the only place in the
// context package that has side effects.
func BuildContext(ctx context.Context, cfg config.EngineConfig) (EngineContext, error) {
	diff, err := gitCommand(ctx, "diff")
	if err != nil {
		return EngineContext{}, fmt.Errorf("collecting git diff: %w", err)
	}

	hunks := ParseDiff(diff)

	if cfg.AI.MaxDiffKB > 0 && len(diff)/1024 > cfg.AI.MaxDiffKB {
		return EngineContext{}, fmt.Errorf("diff size %d KB exceeds max_diff_kb (%d KB)", len(diff)/1024, cfg.AI.MaxDiffKB)
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
		Hunks:         hunks,
		RecentCommits: commits,
		Config:        cfg,
	}, nil
}

func gitCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
