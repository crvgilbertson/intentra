package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/cmd/ui"
	"github.com/crvgilbertson/intentra/config"
	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/engine/validators"
	"github.com/crvgilbertson/intentra/internal"
)

var defaultPlanFile = config.PlanPath

var (
	jsonOutput   bool
	snapshotFile string
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate a commit plan from uncommitted changes",
	Long:  "Analyzes the current git diff and produces a structured commit plan using AI reasoning. The plan is saved to .intentra/plan.json for use by apply.",
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw CommitPlan JSON")
	planCmd.Flags().StringVar(&snapshotFile, "snapshot", "", "export reproducible plan snapshot to file")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	ec, err := enginectx.BuildContext(ctx, cfg)
	if err != nil {
		return fmt.Errorf("building context: %w", err)
	}

	if len(ec.Hunks) == 0 {
		ui.Warn("No uncommitted changes found.\n")
		return nil
	}

	ui.Info("Found %d hunk(s) across the diff.\n", len(ec.Hunks))
	ui.Verbose("Provider: %s, Model: %s, Temperature: %.1f\n", cfg.AI.Provider, cfg.AI.Model, cfg.AI.Temperature)
	ui.Verbose("Max commits: %d, Batch threshold: %d, Max hunk lines: %d\n", cfg.Engine.MaxCommits, cfg.Engine.BatchThreshold, cfg.AI.MaxHunkLines)

	engine, err := reasoning.NewEngineFromConfig(cfg.AI)
	if err != nil {
		return fmt.Errorf("creating reasoning engine: %w", err)
	}
	planner := planners.NewCommitPlanner(engine)

	spin := ui.NewSpinner("Generating commit plan...")
	planner.OnProgress = func(stage string) {
		spin.UpdateMessage(stage)
		ui.Verbose("%s\n", stage)
	}
	spin.Start()
	defer spin.Stop()
	plan, err := planner.BuildPlan(ctx, ec)
	spin.Stop()
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

	pc := validators.AssessPlanConfidenceWithTrace(*cp, ec.Hunks, cp.Trace)
	cp.Confidence = &models.PlanConfidence{
		Overall:    pc.Score,
		Level:      pc.Level,
		Components: pc.Components,
	}

	if err := savePlan(cp); err != nil {
		ui.Warn("Warning: could not save plan cache: %v\n", err)
	} else {
		ui.Dim("Plan saved to %s\n", defaultPlanFile)
	}

	if snapshotFile != "" {
		return writeSnapshot(cp, &ec)
	}

	if jsonOutput {
		data, err := json.MarshalIndent(cp, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling plan: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printPlanSummary(cp, ec.Hunks)
	ui.PrintConfidence(pc.Level, pc.Score, pc.Warnings)
	return nil
}

func printPlanSummary(cp *models.CommitPlan, hunks []models.Hunk) {
	hunkFileMap := make(map[string]string, len(hunks))
	for _, h := range hunks {
		hunkFileMap[h.HunkID] = h.FilePath
	}

	var commits []ui.CommitDisplay
	for _, c := range cp.Commits {
		files := uniqueFiles(c.Hunks, hunkFileMap)
		body := ""
		if c.Body != nil {
			body = *c.Body
		}
		scope := ""
		if c.Scope != nil {
			scope = *c.Scope
		}
		commits = append(commits, ui.CommitDisplay{
			Type:      c.Type,
			Scope:     scope,
			Subject:   c.Subject,
			Body:      body,
			HunkCount: len(c.Hunks),
			Files:     files,
			Breaking:  c.Breaking,
		})
	}

	ui.PrintPlanSummary(cp.ToolVersion, cp.BaseRef, commits)
}

func uniqueFiles(hunkIDs []string, hunkFileMap map[string]string) []string {
	seen := make(map[string]bool)
	var files []string
	for _, hid := range hunkIDs {
		f := hunkFileMap[hid]
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files
}

func writeSnapshot(cp *models.CommitPlan, ec *enginectx.EngineContext) error {
	hunkMetas := make([]models.HunkMeta, len(ec.Hunks))
	for i, h := range ec.Hunks {
		hunkMetas[i] = models.HunkMetaFromHunk(h)
	}

	snap := models.PlanSnapshot{
		EngineVersion:     internal.Version,
		SchemaVersion:     models.CurrentSchemaVersion,
		PromptFingerprint: planners.PromptFingerprint(),
		Provider:          cfg.AI.Provider,
		Model:             cfg.AI.Model,
		Config: models.SnapshotConfig{
			Provider:       cfg.AI.Provider,
			Model:          cfg.AI.Model,
			Temperature:    cfg.AI.Temperature,
			MaxCommits:     cfg.Engine.MaxCommits,
			MaxHunkLines:   cfg.AI.MaxHunkLines,
			BatchThreshold: cfg.Engine.BatchThreshold,
			Style:          cfg.Style,
		},
		DiffFingerprint: cp.DiffFingerprint,
		HunkCount:       len(ec.Hunks),
		Hunks:           hunkMetas,
		Plan:            *cp,
		Confidence:      cp.Confidence,
		Trace:           cp.Trace,
		Timestamp:       time.Now().UTC(),
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling snapshot: %w", err)
	}

	if err := os.WriteFile(snapshotFile, data, 0644); err != nil {
		return fmt.Errorf("writing snapshot to %s: %w", snapshotFile, err)
	}

	ui.Success("Snapshot written to %s\n", snapshotFile)
	return nil
}

func savePlan(cp *models.CommitPlan) error {
	if err := config.EnsureDir(); err != nil {
		return fmt.Errorf("creating %s: %w", config.Dir, err)
	}
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling plan: %w", err)
	}
	return os.WriteFile(defaultPlanFile, data, 0644)
}

func loadCachedPlan() (*models.CommitPlan, error) {
	data, err := os.ReadFile(defaultPlanFile)
	if err != nil {
		return nil, err
	}
	var cp models.CommitPlan
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("parsing cached plan: %w", err)
	}
	if err := cp.Validate(); err != nil {
		return nil, fmt.Errorf("cached plan is structurally invalid: %w", err)
	}
	return &cp, nil
}

