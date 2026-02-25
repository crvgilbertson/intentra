package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	enginectx "intentra/engine/context"
	"intentra/engine/models"
	"intentra/engine/planners"
	"intentra/engine/reasoning"
	"intentra/engine/validators"

)

var jsonOutput bool

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate a commit plan from uncommitted changes",
	Long:  "Analyzes the current git diff and produces a structured commit plan using AI reasoning.",
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw CommitPlan JSON")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

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

	engine, err := reasoning.NewEngineFromConfig(cfg.AI)
	if err != nil {
		return fmt.Errorf("creating reasoning engine: %w", err)
	}
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

	if jsonOutput {
		data, err := json.MarshalIndent(cp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling plan: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printPlanSummary(cp)
	return nil
}

func printPlanSummary(cp *models.CommitPlan) {
	fmt.Printf("\nCommit Plan (%d commit(s)):\n", len(cp.Commits))
	fmt.Printf("Base: %s\n\n", cp.BaseRef)

	for i, c := range cp.Commits {
		fmt.Printf("  %d. %s\n", i+1, c.FullSubject())
		if c.Body != nil && *c.Body != "" {
			fmt.Printf("     %s\n", *c.Body)
		}
		fmt.Printf("     hunks: %d\n", len(c.Hunks))
		if c.Breaking {
			fmt.Printf("     ⚠ BREAKING CHANGE\n")
		}
	}
	fmt.Println()
}
