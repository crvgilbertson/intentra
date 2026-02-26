package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/executors"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/engine/validators"

	"github.com/crvgilbertson/intentra/cmd/ui"
)

var yesFlag bool

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a commit plan to the repository",
	Long:  "Applies a commit plan to the repository. If a cached plan exists from a previous 'plan' run and the diff hasn't changed, it is reused. Otherwise, a new plan is generated. Defaults to dry-run unless --yes is passed.",
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&yesFlag, "yes", false, "actually apply commits (default is dry-run)")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	dryRun := !yesFlag

	if !dryRun {
		if err := preflightCheck(); err != nil {
			return err
		}
		if err := checkProtectedBranch(); err != nil {
			return err
		}
	}

	ec, err := enginectx.BuildContext(ctx, cfg)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	if len(ec.Hunks) == 0 {
		ui.Warn("No uncommitted changes found.\n")
		return nil
	}

	ui.Info("Found %d hunk(s) across the diff.\n", len(ec.Hunks))

	if !dryRun && !cfg.Engine.SkipHooks {
		warnIfHooksDetected()
	}

	cp, err := resolveCommitPlan(ctx, &ec)
	if err != nil {
		return err
	}

	if err := validators.ValidateCommitPlan(*cp, ec); err != nil {
		return fmt.Errorf("plan validation failed: %w", err)
	}

	printPlanSummary(cp, ec.Hunks)

	for _, w := range validators.WarnFileOverlap(*cp, ec.Hunks) {
		ui.Warn("  ⚠ %s\n", w)
	}

	if dryRun {
		ui.Warn("Dry-run mode. Pass --yes to apply.\n")
		return nil
	}

	ui.Info("Applying %d commit(s)...\n", len(cp.Commits))

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	executor := executors.NewGitExecutorWithHunks(cwd, ec.Hunks, executors.ExecutorOptions{
		SignCommits:  cfg.Engine.SignCommits,
		CommitAuthor: cfg.Engine.CommitAuthor,
		SkipHooks:    cfg.Engine.SkipHooks,
	})
	if err := executor.Execute(ctx, cp, false); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	_ = os.Remove(defaultPlanFile)

	ui.Success("\n✓ Successfully applied %d commit(s).\n", len(cp.Commits))

	remote := cfg.Engine.RemoteName
	if remote == "" {
		remote = "origin"
	}
	branch, _ := currentBranch()

	if cfg.Engine.AutoPush && branch != "" {
		if !remoteExists(remote) {
			ui.Warn("Remote %q not found. Skipping push.\n", remote)
			ui.Dim("  Add a remote: git remote add %s <url>\n", remote)
		} else if hasUpstream(branch) {
			ui.Info("Pushing to %s...\n", remote)
			if err := pushBranch(remote); err != nil {
				ui.Warn("Push failed: %v\n", err)
				ui.Dim("  You can push manually: git push\n")
			} else {
				ui.Success("✓ Pushed to %s.\n", remote)
			}
		} else {
			ui.Info("Pushing and setting upstream...\n")
			if err := pushBranchWithUpstream(remote, branch); err != nil {
				ui.Warn("Push failed: %v\n", err)
				ui.Dim("  You can push manually: git push --set-upstream %s %s\n", remote, branch)
			} else {
				ui.Success("✓ Pushed to %s/%s.\n", remote, branch)
			}
		}
	} else if branch != "" && !hasUpstream(branch) {
		ui.Dim("\nTo push this branch:\n")
		ui.Dim("  git push --set-upstream %s %s\n", remote, branch)
	}

	return nil
}

func resolveCommitPlan(ctx context.Context, ec *enginectx.EngineContext) (*models.CommitPlan, error) {
	currentFingerprint := models.DiffFingerprintFromHunks(ec.Hunks)

	cached, err := loadCachedPlan()
	if err == nil && cached.DiffFingerprint == currentFingerprint {
		ui.Success("Using cached plan from %s (diff unchanged).\n", defaultPlanFile)
		return cached, nil
	}
	if err == nil {
		ui.Warn("Cached plan is stale (diff changed). Re-planning...\n")
	} else {
		ui.Info("No cached plan found.\n")
	}

	engine, err := reasoning.NewEngineFromConfig(cfg.AI)
	if err != nil {
		return nil, fmt.Errorf("creating reasoning engine: %w", err)
	}
	planner := planners.NewCommitPlanner(engine)

	spin := ui.NewSpinner("Generating commit plan...")
	spin.Start()
	plan, err := planner.BuildPlan(ctx, *ec)
	spin.Stop()
	if err != nil {
		return nil, fmt.Errorf("building plan: %w", err)
	}

	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		return nil, fmt.Errorf("unexpected plan type %T", plan)
	}

	if saveErr := savePlan(cp); saveErr != nil {
		ui.Warn("Warning: could not save plan cache: %v\n", saveErr)
	}

	return cp, nil
}

func checkProtectedBranch() error {
	branch, err := currentBranch()
	if err != nil {
		return nil
	}

	for _, protected := range cfg.Engine.ProtectedBranches {
		if strings.EqualFold(branch, protected) {
			ui.Error("Cannot apply commits to protected branch %q.\n", branch)
			ui.Info("Create a feature branch first:\n")
			ui.Dim("  git checkout -b <branch-name>\n")
			ui.Dim("  intentra apply --yes\n")
			return fmt.Errorf("branch %q is protected", branch)
		}
	}
	return nil
}

func currentBranch() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hasUpstream(branch string) bool {
	err := exec.Command("git", "rev-parse", "--abbrev-ref", branch+"@{upstream}").Run()
	return err == nil
}

func preflightCheck() error {
	gitDir := findGitDir()
	if gitDir == "" {
		return nil
	}

	checks := []struct {
		path    string
		isDir   bool
		message string
		hint    string
	}{
		{filepath.Join(gitDir, "MERGE_HEAD"), false,
			"Repository is in the middle of a merge.",
			"Resolve the merge first: git merge --continue  OR  git merge --abort"},
		{filepath.Join(gitDir, "CHERRY_PICK_HEAD"), false,
			"Repository is in the middle of a cherry-pick.",
			"Resolve it first: git cherry-pick --continue  OR  git cherry-pick --abort"},
		{filepath.Join(gitDir, "BISECT_LOG"), false,
			"Repository is in the middle of a bisect.",
			"Finish the bisect first: git bisect reset"},
		{filepath.Join(gitDir, "rebase-merge"), true,
			"Repository is in the middle of a rebase.",
			"Resolve the rebase first: git rebase --continue  OR  git rebase --abort"},
		{filepath.Join(gitDir, "rebase-apply"), true,
			"Repository is in the middle of a rebase or am.",
			"Resolve it first: git rebase --continue  OR  git rebase --abort"},
	}

	for _, c := range checks {
		var exists bool
		if c.isDir {
			info, err := os.Stat(c.path)
			exists = err == nil && info.IsDir()
		} else {
			_, err := os.Stat(c.path)
			exists = err == nil
		}
		if exists {
			ui.Error("%s\n", c.message)
			ui.Dim("  %s\n", c.hint)
			return fmt.Errorf("unsafe repo state: %s", c.message)
		}
	}

	out, err := exec.Command("git", "diff", "--name-only", "--diff-filter=U").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		ui.Error("Repository has unmerged paths (merge conflicts).\n")
		ui.Dim("  Resolve all conflicts, then run: git add <files> && git commit\n")
		return fmt.Errorf("unsafe repo state: unmerged paths")
	}

	return nil
}

func findGitDir() string {
	out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func remoteExists(remote string) bool {
	return exec.Command("git", "remote", "get-url", remote).Run() == nil
}

func warnIfHooksDetected() {
	var sources []string

	if info, err := os.Stat(".husky"); err == nil && info.IsDir() {
		sources = append(sources, "husky")
	}
	if _, err := os.Stat(".pre-commit-config.yaml"); err == nil {
		sources = append(sources, "pre-commit")
	}
	if data, err := os.ReadFile("package.json"); err == nil {
		if strings.Contains(string(data), "\"husky\"") {
			if len(sources) == 0 || sources[0] != "husky" {
				sources = append(sources, "husky (package.json)")
			}
		}
	}
	gitDir := findGitDir()
	if gitDir != "" {
		if info, err := os.Stat(filepath.Join(gitDir, "hooks", "pre-commit")); err == nil && !info.IsDir() {
			sources = append(sources, "git hooks")
		}
	}

	if len(sources) > 0 {
		ui.Dim("Detected commit hooks (%s). If commits are rejected, set skip_hooks: true in config.\n",
			strings.Join(sources, ", "))
	}
}

func pushBranch(remote string) error {
	out, err := exec.Command("git", "push", remote).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func pushBranchWithUpstream(remote, branch string) error {
	out, err := exec.Command("git", "push", "--set-upstream", remote, branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}
