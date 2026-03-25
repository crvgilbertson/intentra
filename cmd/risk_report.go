package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/engine/artifacts"
)

var (
	riskReportJSON   bool
	riskReportTicket string
)

var riskReportCmd = &cobra.Command{
	Use:   "risk-report",
	Short: "Summarize release risk from the cached plan",
	Long:  "Builds a deterministic risk report from the cached plan, highlighting high-risk commits, aggregate risk, and sensitive areas touched. No extra AI call is made.",
	RunE:  runRiskReport,
}

func init() {
	riskReportCmd.Flags().BoolVar(&riskReportJSON, "json", false, "output as JSON")
	riskReportCmd.Flags().StringVar(&riskReportTicket, "ticket", "", "ticket reference to include (for example PROJ-123)")
	rootCmd.AddCommand(riskReportCmd)
}

func runRiskReport(cmd *cobra.Command, args []string) error {
	cp, err := loadPlanForArtifacts()
	if err != nil {
		return err
	}

	report := artifacts.BuildRiskReport(*cp, artifactOptions(riskReportTicket, "", "", cp))

	if riskReportJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling risk report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printRiskReportText(report)
	return nil
}

func printRiskReportText(report artifacts.RiskReport) {
	fmt.Println("# Risk Report")
	fmt.Println()
	fmt.Printf("- Commits analyzed: %d\n", report.TotalCommits)
	fmt.Printf("- Aggregate risk: %.2f (%s)\n", report.Summary.AggregateScore, report.Summary.AggregateLevel)
	fmt.Printf("- Risky commits: %d\n", report.Summary.RiskyCommits)
	fmt.Printf("- High-risk commits: %d\n", report.Summary.HighRiskCommits)
	fmt.Printf("- Medium-risk commits: %d\n", report.Summary.MediumRiskCommits)
	if report.Ticket != nil {
		fmt.Printf("- Ticket: %s\n", report.Ticket.ID)
	}
	if len(report.Summary.SensitiveAreas) > 0 {
		fmt.Printf("- Sensitive areas: %s\n", strings.Join(report.Summary.SensitiveAreas, ", "))
	}
	fmt.Println()

	if len(report.Commits) == 0 {
		fmt.Println("No commit-level risk data is present in the cached plan.")
		return
	}

	fmt.Println("## Commits")
	fmt.Println()
	for _, c := range report.Commits {
		fmt.Printf("- %s  %.2f (%s)\n", c.Title, c.Score, c.Level)
		if len(c.Areas) > 0 {
			fmt.Printf("  areas: %s\n", strings.Join(c.Areas, ", "))
		}
	}
	fmt.Println()
}
