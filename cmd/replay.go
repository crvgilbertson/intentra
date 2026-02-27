package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/cmd/ui"
	cfgpkg "github.com/crvgilbertson/intentra/config"
	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/engine/validators"
)

var replayCmd = &cobra.Command{
	Use:   "replay <snapshot.json>",
	Short: "Replay planning from a snapshot and check for divergence",
	Long:  "Re-runs the planner using the context from a previously exported snapshot. Compares the new plan structurally against the stored plan and reports whether results are IDENTICAL, STRUCTURALLY_EQUIVALENT, or DIVERGENT.",
	Args:  cobra.ExactArgs(1),
	RunE:  runReplay,
}

func init() {
	rootCmd.AddCommand(replayCmd)
}

type replayResult struct {
	Status       string   `json:"status"`
	Divergences  []string `json:"divergences,omitempty"`
	PromptMatch  bool     `json:"prompt_match"`
	SchemaMatch  bool     `json:"schema_match"`
}

func runReplay(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading snapshot: %w", err)
	}

	var snap models.PlanSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("parsing snapshot: %w", err)
	}

	if snap.SchemaVersion != models.CurrentSchemaVersion {
		return fmt.Errorf("schema version mismatch: snapshot has %s, current engine expects %s",
			snap.SchemaVersion, models.CurrentSchemaVersion)
	}

	currentPrompts := planners.PromptFingerprint()
	promptMatch := snap.PromptFingerprint == currentPrompts
	if !promptMatch {
		ui.Warn("Prompt fingerprint mismatch:\n")
		ui.Warn("  snapshot: %s\n", shortHash(snap.PromptFingerprint))
		ui.Warn("  current:  %s\n", shortHash(currentPrompts))
	}

	hunks := make([]models.Hunk, len(snap.Hunks))
	for i, m := range snap.Hunks {
		hunks[i] = models.Hunk{
			HunkID:      m.HunkID,
			FilePath:    m.FilePath,
			Header:      m.Header,
			Patch:       m.Patch,
			Summary:     m.Summary,
			NewFile:     m.NewFile,
			DeletedFile: m.DeletedFile,
			RenamedFrom: m.RenamedFrom,
		}
	}

	ec := enginectx.EngineContext{
		BaseRef: snap.Plan.BaseRef,
		Hunks:   hunks,
		Config:  configFromSnapshot(snap.Config),
	}

	ctx := context.Background()
	engine, err := reasoning.NewEngineFromConfig(ec.Config.AI)
	if err != nil {
		return fmt.Errorf("creating reasoning engine: %w", err)
	}

	planner := planners.NewCommitPlanner(engine)
	spin := ui.NewSpinner("Replaying plan...")
	planner.OnProgress = func(stage string) {
		spin.UpdateMessage(stage)
	}
	spin.Start()
	defer spin.Stop()
	plan, err := planner.BuildPlan(ctx, ec)
	spin.Stop()
	if err != nil {
		return fmt.Errorf("replay planning failed: %w", err)
	}

	replayPlan, ok := plan.(*models.CommitPlan)
	if !ok {
		return fmt.Errorf("unexpected plan type %T", plan)
	}

	if err := validators.ValidateCommitPlan(*replayPlan, ec); err != nil {
		ui.Warn("Replayed plan has validation errors: %v\n", err)
	}

	result := comparePlans(&snap.Plan, replayPlan, promptMatch)

	fmt.Println()
	switch result.Status {
	case "IDENTICAL":
		ui.Success("Result: IDENTICAL\n")
		ui.Dim("  Plans are byte-for-byte equivalent after normalization.\n")
	case "STRUCTURALLY_EQUIVALENT":
		ui.Success("Result: STRUCTURALLY_EQUIVALENT\n")
		ui.Dim("  Same groupings and ordering, minor textual differences.\n")
	case "DIVERGENT":
		ui.Error("Result: DIVERGENT\n")
		for _, d := range result.Divergences {
			ui.Warn("  - %s\n", d)
		}
	}

	if !result.PromptMatch {
		ui.Warn("  Note: prompt fingerprints differ — divergence may be expected.\n")
	}

	return nil
}

func comparePlans(stored, replayed *models.CommitPlan, promptMatch bool) replayResult {
	result := replayResult{
		PromptMatch: promptMatch,
		SchemaMatch: stored.SchemaVersion == replayed.SchemaVersion,
	}

	if len(stored.Commits) != len(replayed.Commits) {
		result.Status = "DIVERGENT"
		result.Divergences = append(result.Divergences,
			fmt.Sprintf("commit count: stored=%d replayed=%d", len(stored.Commits), len(replayed.Commits)))
		return result
	}

	identicalText := true
	structurallyEquivalent := true

	for i := range stored.Commits {
		sc := stored.Commits[i]
		rc := replayed.Commits[i]

		storedHunks := sortedCopy(sc.Hunks)
		replayedHunks := sortedCopy(rc.Hunks)

		if !slicesEqual(storedHunks, replayedHunks) {
			structurallyEquivalent = false
			result.Divergences = append(result.Divergences,
				fmt.Sprintf("commit %d hunk grouping differs", i+1))
		}

		if sc.Type != rc.Type {
			identicalText = false
			result.Divergences = append(result.Divergences,
				fmt.Sprintf("commit %d type: stored=%q replayed=%q", i+1, sc.Type, rc.Type))
		}

		if sc.Subject != rc.Subject {
			identicalText = false
		}

		if !slicesEqual(sc.Hunks, rc.Hunks) {
			identicalText = false
		}
	}

	storedOrder := commitTypeOrder(stored)
	replayedOrder := commitTypeOrder(replayed)
	if storedOrder != replayedOrder {
		result.Divergences = append(result.Divergences,
			fmt.Sprintf("commit ordering differs: stored=%s replayed=%s", storedOrder, replayedOrder))
		structurallyEquivalent = false
	}

	if stored.Confidence != nil && replayed.Confidence != nil {
		scoreDelta := stored.Confidence.Overall - replayed.Confidence.Overall
		if scoreDelta > 0.1 || scoreDelta < -0.1 {
			result.Divergences = append(result.Divergences,
				fmt.Sprintf("confidence score: stored=%.2f replayed=%.2f", stored.Confidence.Overall, replayed.Confidence.Overall))
		}
	}

	switch {
	case identicalText && structurallyEquivalent && len(result.Divergences) == 0:
		result.Status = "IDENTICAL"
	case structurallyEquivalent && len(result.Divergences) == 0:
		result.Status = "STRUCTURALLY_EQUIVALENT"
	default:
		result.Status = "DIVERGENT"
	}

	return result
}

func commitTypeOrder(plan *models.CommitPlan) string {
	var types []string
	for _, c := range plan.Commits {
		types = append(types, c.Type)
	}
	return strings.Join(types, ",")
}

func sortedCopy(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	sort.Strings(c)
	return c
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func configFromSnapshot(sc models.SnapshotConfig) cfgpkg.EngineConfig {
	c := cfgpkg.DefaultConfig()
	c.AI.Provider = sc.Provider
	c.AI.Model = sc.Model
	c.AI.Temperature = sc.Temperature
	c.AI.MaxHunkLines = sc.MaxHunkLines
	c.Engine.MaxCommits = sc.MaxCommits
	c.Engine.BatchThreshold = sc.BatchThreshold
	c.Style = sc.Style
	return c
}
