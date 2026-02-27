package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/engine/models"
)

var explainJSON bool

var explainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Explain the cached plan's clustering, repairs, and confidence",
	Long:  "Reads the cached plan and displays a pure engine trace: clustering rationale, repair heuristic activity, dependency reordering, and confidence breakdown. No AI prose — only deterministic engine data.",
	RunE:  runExplain,
}

func init() {
	explainCmd.Flags().BoolVar(&explainJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(explainCmd)
}

type explainReport struct {
	Clustering  clusteringExplain  `json:"clustering"`
	Repairs     repairExplain      `json:"repairs"`
	Ordering    orderingExplain    `json:"ordering"`
	Confidence  confidenceExplain  `json:"confidence"`
}

type clusteringExplain struct {
	Strategy   string           `json:"strategy"`
	CommitCount int             `json:"commit_count"`
	Rationales []rationaleEntry `json:"rationales,omitempty"`
}

type rationaleEntry struct {
	CommitID  string `json:"commit_id"`
	Type      string `json:"type"`
	Subject   string `json:"subject"`
	Rationale string `json:"rationale"`
}

type repairExplain struct {
	DedupCount      int  `json:"dedup_count"`
	OrphanCount     int  `json:"orphan_count"`
	RescueAttempted bool `json:"rescue_attempted"`
	RescueSucceeded bool `json:"rescue_succeeded"`
	RepairCount     int  `json:"repair_count"`
}

type orderingExplain struct {
	ReorderApplied bool `json:"reorder_applied"`
	CommitsBefore  int  `json:"commits_before_reorder"`
	CommitsAfter   int  `json:"commits_after_reorder"`
}

type confidenceExplain struct {
	Overall    float64                    `json:"overall"`
	Level      string                     `json:"level"`
	Components *models.ConfidenceComponents `json:"components,omitempty"`
}

func runExplain(cmd *cobra.Command, args []string) error {
	cached, err := loadCachedPlan()
	if err != nil {
		return fmt.Errorf("no cached plan found — run 'intentra plan' first: %w", err)
	}

	report := buildExplainReport(cached)

	if explainJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling explain report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printExplainText(report)
	return nil
}

func buildExplainReport(cp *models.CommitPlan) explainReport {
	report := explainReport{}

	report.Clustering.CommitCount = len(cp.Commits)
	for _, c := range cp.Commits {
		if c.Rationale != "" {
			report.Clustering.Rationales = append(report.Clustering.Rationales, rationaleEntry{
				CommitID:  c.ID,
				Type:      c.Type,
				Subject:   c.Subject,
				Rationale: c.Rationale,
			})
		}
	}

	if cp.Trace != nil {
		report.Clustering.Strategy = cp.Trace.Strategy
		report.Repairs = repairExplain{
			DedupCount:      cp.Trace.DedupCount,
			OrphanCount:     cp.Trace.OrphanCount,
			RescueAttempted: cp.Trace.RescueAttempted,
			RescueSucceeded: cp.Trace.RescueSucceeded,
			RepairCount:     cp.Trace.RepairCount,
		}
		report.Ordering = orderingExplain{
			ReorderApplied: cp.Trace.ReorderApplied,
			CommitsBefore:  cp.Trace.CommitsBefore,
			CommitsAfter:   cp.Trace.CommitsAfter,
		}
	}

	if cp.Confidence != nil {
		report.Confidence = confidenceExplain{
			Overall:    cp.Confidence.Overall,
			Level:      cp.Confidence.Level,
			Components: &cp.Confidence.Components,
		}
	}

	return report
}

func printExplainText(r explainReport) {
	section := func(title string) {
		fmt.Printf("\n  %s\n", title)
		fmt.Println("  " + repeatStr("─", len(title)))
	}

	section("Clustering")
	if r.Clustering.Strategy != "" {
		fmt.Printf("    strategy:     %s\n", r.Clustering.Strategy)
	}
	fmt.Printf("    commits:      %d\n", r.Clustering.CommitCount)

	if len(r.Clustering.Rationales) > 0 {
		fmt.Println()
		for _, rat := range r.Clustering.Rationales {
			fmt.Printf("    %s %s: %s\n", rat.CommitID, rat.Type, rat.Subject)
			if rat.Rationale != "" {
				fmt.Printf("      → %s\n", rat.Rationale)
			}
		}
	}

	section("Repair Heuristics")
	fmt.Printf("    dedup:        %d hunk(s) deduplicated\n", r.Repairs.DedupCount)
	fmt.Printf("    orphans:      %d hunk(s) unassigned after clustering\n", r.Repairs.OrphanCount)
	if r.Repairs.RescueAttempted {
		if r.Repairs.RescueSucceeded {
			fmt.Printf("    rescue:       succeeded (LLM assigned orphans to groups)\n")
		} else {
			fmt.Printf("    rescue:       failed (fell back to file-proximity repair)\n")
			fmt.Printf("    repaired:     %d hunk(s) assigned by file proximity\n", r.Repairs.RepairCount)
		}
	} else if r.Repairs.OrphanCount == 0 {
		fmt.Printf("    rescue:       not needed (full coverage)\n")
	}

	section("Dependency Ordering")
	if r.Ordering.ReorderApplied {
		fmt.Printf("    reorder:      applied (commit order changed to match dependency layers)\n")
	} else {
		fmt.Printf("    reorder:      not needed (LLM ordering was already correct)\n")
	}

	section("Confidence")
	fmt.Printf("    overall:      %.0f%% (%s)\n", r.Confidence.Overall*100, r.Confidence.Level)
	if r.Confidence.Components != nil {
		c := r.Confidence.Components
		fmt.Printf("    coverage:     %.2f\n", c.Coverage)
		fmt.Printf("    entanglement: %.2f\n", c.Entanglement)
		fmt.Printf("    repair:       %.2f\n", c.RepairActivity)
		fmt.Printf("    overlap:      %.2f\n", c.Overlap)
		fmt.Printf("    reorder:      %.2f\n", c.ReorderPenalty)
	}
	fmt.Println()
}

func repeatStr(s string, n int) string {
	return strings.Repeat(s, n)
}
