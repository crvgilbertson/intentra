package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	enginectx "intentra/engine/context"
	"intentra/engine/executors"
	"intentra/engine/models"
	"intentra/engine/planners"
	"intentra/engine/reasoning"
	"intentra/engine/validators"
)

var yesFlag bool

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a commit plan to the repository",
	Long:  "Generates a commit plan and applies it, creating git commits. Defaults to dry-run unless --yes is passed.",
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
		fmt.Println("No uncommitted changes found.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Found %d hunk(s) across the diff.\n", len(ec.Hunks))
	fmt.Fprintf(os.Stderr, "Generating commit plan...\n")

	engine := reasoning.NewOpenAIEngine(cfg.AI.Model, cfg.AI.Temperature)
	planner := planners.NewCommitPlanner(engine)

	plan, err := planner.BuildPlan(ctx, ec)
	if err != nil {
		return fmt.Errorf("building plan: %w", err)
	}

	cp, ok := plan.(*models.CommitPlan)
	if !ok {
		return fmt.Errorf("unexpected plan type %T", plan)
	}

	if err := validators.ValidateCommitPlan(*cp, ec); err != nil {
		return fmt.Errorf("plan validation failed: %w", err)
	}

	printPlanSummary(cp)

	if dryRun {
		fmt.Println("Dry-run mode. Pass --yes to apply.")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Applying %d commit(s)...\n", len(cp.Commits))

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	executor := executors.NewGitExecutorWithHunks(cwd, ec.Hunks)
	if err := executor.Execute(ctx, cp, false); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	fmt.Printf("Successfully applied %d commit(s).\n", len(cp.Commits))
	return nil
}
