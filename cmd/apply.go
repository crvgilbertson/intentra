package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/executors"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/engine/validators"

	"github.com/crvgilbertson/intentra/cmd/ui"
)

var (
	yesFlag   bool
	forceFlag bool
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a commit plan to the repository",
	Long:  "Applies a commit plan to the repository. If a cached plan exists from a previous 'plan' run and the diff hasn't changed, it is reused. Otherwise, a new plan is generated. Defaults to dry-run unless --yes is passed.",
	RunE:  runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&yesFlag, "yes", false, "actually apply commits (default is dry-run)")
	applyCmd.Flags().BoolVar(&forceFlag, "force", false, "apply even when plan confidence is low")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	ui.Verbose("Provider: %s, Model: %s, Dry-run: %v\n", cfg.AI.Provider, cfg.AI.Model, dryRun)

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

	pc := validators.AssessPlanConfidence(*cp, ec.Hunks)
	ui.PrintConfidence(pc.Level, pc.Score, pc.Warnings)

	if dryRun {
		ui.Warn("Dry-run mode. Pass --yes to apply.\n")
		return nil
	}

	if pc.Level == "low" && !forceFlag {
		ui.Error("\nPlan confidence is low — refusing to apply.\n")
		ui.Info("Review the warnings above. To apply anyway, pass --force:\n")
		ui.Dim("  intentra apply --yes --force\n")
		return fmt.Errorf("plan confidence too low (%.0f%%); use --force to override", pc.Score*100)
	}

	ui.Info("Applying %d commit(s)...\n", len(cp.Commits))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		ui.Warn("\nInterrupt received — rolling back...\n")
		cancel()
	}()
	defer signal.Stop(sigCh)

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

	if branch == "" || branch == "HEAD" {
		// Detached HEAD — pushing doesn't make sense.
	} else if cfg.Engine.AutoPush {
		smartPush(remote, branch)
	} else if !hasUpstream(branch) {
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
	planner.OnProgress = func(stage string) {
		spin.UpdateMessage(stage)
		ui.Verbose("%s\n", stage)
	}
	spin.Start()
	defer spin.Stop()
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
