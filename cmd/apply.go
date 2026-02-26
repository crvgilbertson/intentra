package cmd

import (
	"context"
	"fmt"
	"os"

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

	ec, err := enginectx.BuildContext(ctx, cfg)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	if len(ec.Hunks) == 0 {
		ui.Warn("No uncommitted changes found.\n")
		return nil
	}

	ui.Info("Found %d hunk(s) across the diff.\n", len(ec.Hunks))

	cp, err := resolveCommitPlan(ctx, &ec)
	if err != nil {
		return err
	}

	if err := validators.ValidateCommitPlan(*cp, ec); err != nil {
		return fmt.Errorf("plan validation failed: %w", err)
	}

	printPlanSummary(cp, ec.Hunks)

	if dryRun {
		ui.Warn("Dry-run mode. Pass --yes to apply.\n")
		return nil
	}

	ui.Info("Applying %d commit(s)...\n", len(cp.Commits))

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	executor := executors.NewGitExecutorWithHunks(cwd, ec.Hunks)
	if err := executor.Execute(ctx, cp, false); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	_ = os.Remove(defaultPlanFile)

	ui.Success("\n✓ Successfully applied %d commit(s).\n", len(cp.Commits))
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
		ui.Info("No cached plan found. Generating commit plan...\n")
	}

	engine, err := reasoning.NewEngineFromConfig(cfg.AI)
	if err != nil {
		return nil, fmt.Errorf("creating reasoning engine: %w", err)
	}
	planner := planners.NewCommitPlanner(engine)

	plan, err := planner.BuildPlan(ctx, *ec)
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
