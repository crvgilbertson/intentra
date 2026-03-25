package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/cmd/ui"
	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/engine/atomicity"
	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/engine/validators"
	"github.com/crvgilbertson/intentra/internal"
)

var defaultPlanFile = config.PlanPath

var (
	jsonOutput    bool
	snapshotFile  string
	analyzeOutput bool
	planTicket    string
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Generate a commit plan from uncommitted changes",
	Long:  "Analyzes the current git diff and produces a structured commit plan using AI reasoning. The plan is saved to .intentra/plan.json for use by apply.",
	RunE:  runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&jsonOutput, "json", false, "output raw CommitPlan JSON")
	planCmd.Flags().BoolVar(&analyzeOutput, "analyze", false, "output detailed per-commit diagnostics")
	planCmd.Flags().StringVar(&snapshotFile, "snapshot", "", "export reproducible plan snapshot to file")
	planCmd.Flags().StringVar(&planTicket, "ticket", "", "ticket reference to attach to generated commits (for example PROJ-123)")
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

	hunkToFile := make(map[string]string, len(ec.Hunks))
	for _, h := range ec.Hunks {
		hunkToFile[h.HunkID] = h.FilePath
	}
	for i := range cp.Commits {
		if r := validators.ScoreCommitRisk(cp.Commits[i], hunkToFile, cfg.Engine.Risk); r != nil {
			cp.Commits[i].Risk = r
		}
	}

	if ticket := resolveTicketRef(planTicket, cp); ticket != nil {
		addTicketFooter(cp, ticket.ID)
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
		if analyzeOutput {
			data, err := json.MarshalIndent(buildAnalyzeReport(cp, ec.Hunks), "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling analyze report: %w", err)
			}
			fmt.Println(string(data))
		} else {
			data, err := json.MarshalIndent(cp, "", "  ")
			if err != nil {
				return fmt.Errorf("marshalling plan: %w", err)
			}
			fmt.Println(string(data))
		}
		return nil
	}

	if analyzeOutput {
		printAnalyzeReport(cp, ec.Hunks)
	} else {
		printPlanSummary(cp, ec.Hunks)
	}
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

// analyzeCommitEntry holds per-commit diagnostic data.
type analyzeCommitEntry struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Subject   string   `json:"subject"`
	HunkCount int      `json:"hunk_count"`
	Files     []string `json:"files"`
	Rationale string   `json:"rationale,omitempty"`
	Risk      *struct {
		Score float64  `json:"score"`
		Level string   `json:"level"`
		Areas []string `json:"areas,omitempty"`
	} `json:"risk,omitempty"`
}

// analyzeReport is the structured output for plan --analyze.
type analyzeReport struct {
	Commits []analyzeCommitEntry `json:"commits"`
	Plan    *models.CommitPlan   `json:"plan,omitempty"`
}

func buildAnalyzeReport(cp *models.CommitPlan, hunks []models.Hunk) analyzeReport {
	hunkFileMap := make(map[string]string, len(hunks))
	for _, h := range hunks {
		hunkFileMap[h.HunkID] = h.FilePath
	}

	var entries []analyzeCommitEntry
	for _, c := range cp.Commits {
		files := uniqueFiles(c.Hunks, hunkFileMap)
		e := analyzeCommitEntry{
			ID:        c.ID,
			Type:      c.Type,
			Subject:   c.Subject,
			HunkCount: len(c.Hunks),
			Files:     files,
			Rationale: c.Rationale,
		}
		if c.Risk != nil {
			e.Risk = &struct {
				Score float64  `json:"score"`
				Level string   `json:"level"`
				Areas []string `json:"areas,omitempty"`
			}{
				Score: c.Risk.Score,
				Level: c.Risk.Level,
				Areas: c.Risk.Areas,
			}
		}
		entries = append(entries, e)
	}
	return analyzeReport{Commits: entries}
}

func printAnalyzeReport(cp *models.CommitPlan, hunks []models.Hunk) {
	hunkFileMap := make(map[string]string, len(hunks))
	for _, h := range hunks {
		hunkFileMap[h.HunkID] = h.FilePath
	}

	printPlanSummary(cp, hunks)
	fmt.Println()

	for i, c := range cp.Commits {
		files := uniqueFiles(c.Hunks, hunkFileMap)
		fmt.Printf("── %s %s ──\n", c.ID, c.FullSubject())
		fmt.Printf("  hunks:  %d\n", len(c.Hunks))
		fmt.Printf("  files:  %s\n", strings.Join(files, ", "))
		if c.Rationale != "" {
			fmt.Printf("  rationale: %s\n", c.Rationale)
		}
		if c.Risk != nil {
			fmt.Printf("  risk: %.2f (%s)", c.Risk.Score, c.Risk.Level)
			if len(c.Risk.Areas) > 0 {
				fmt.Printf(" areas=%s", strings.Join(c.Risk.Areas, ","))
			}
			fmt.Println()
		}
		if i < len(cp.Commits)-1 {
			fmt.Println()
		}
	}
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
			Provider:         cfg.AI.Provider,
			Model:            cfg.AI.Model,
			Temperature:      cfg.AI.Temperature,
			MaxCommits:       cfg.Engine.MaxCommits,
			MaxHunkLines:     cfg.AI.MaxHunkLines,
			BatchThreshold:   cfg.Engine.BatchThreshold,
			AtomicityProfile: atomicity.NormalizeProfile(cfg.Engine.Atomicity.Profile),
			Style:            cfg.Style,
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
