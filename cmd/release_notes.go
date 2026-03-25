package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/engine/artifacts"
)

var (
	releaseNotesJSON   bool
	releaseNotesTicket string
)

var releaseNotesCmd = &cobra.Command{
	Use:   "release-notes",
	Short: "Generate release notes from the cached plan",
	Long:  "Builds release notes from the cached plan, including grouped changes, breaking changes, and deterministic risk summaries. No extra AI call is made.",
	RunE:  runReleaseNotes,
}

func init() {
	releaseNotesCmd.Flags().BoolVar(&releaseNotesJSON, "json", false, "output as JSON")
	releaseNotesCmd.Flags().StringVar(&releaseNotesTicket, "ticket", "", "ticket reference to include (for example PROJ-123)")
	rootCmd.AddCommand(releaseNotesCmd)
}

func runReleaseNotes(cmd *cobra.Command, args []string) error {
	cp, err := loadPlanForArtifacts()
	if err != nil {
		return err
	}

	report := artifacts.BuildReleaseNotes(*cp, artifactOptions(releaseNotesTicket, "", "", cp))

	if releaseNotesJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling release notes: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printReleaseNotesText(report)
	return nil
}

func printReleaseNotesText(report artifacts.ReleaseNotes) {
	fmt.Println("# Release Notes")
	fmt.Println()
	fmt.Printf("- Commits: %d\n", report.Summary.TotalCommits)
	fmt.Printf("- Breaking changes: %d\n", report.Summary.BreakingChanges)
	fmt.Printf("- Aggregate risk: %.2f (%s)\n", report.Risk.AggregateScore, report.Risk.AggregateLevel)
	if report.Ticket != nil {
		fmt.Printf("- Ticket: %s\n", report.Ticket.ID)
	}
	if len(report.Risk.SensitiveAreas) > 0 {
		fmt.Printf("- Sensitive areas: %s\n", strings.Join(report.Risk.SensitiveAreas, ", "))
	}
	fmt.Println()

	for _, section := range report.Sections {
		fmt.Printf("## %s\n\n", section.Title)
		for _, c := range section.Commits {
			fmt.Printf("- %s\n", c.Title)
			if c.Body != nil && *c.Body != "" {
				fmt.Printf("  %s\n", *c.Body)
			}
		}
		fmt.Println()
	}

	if len(report.Breaking) > 0 {
		fmt.Println("## Breaking Changes")
		fmt.Println()
		for _, c := range report.Breaking {
			fmt.Printf("- %s\n", c.Title)
		}
		fmt.Println()
	}

	if report.Risk.RiskyCommits > 0 {
		fmt.Println("## Risk Summary")
		fmt.Println()
		fmt.Printf("- Risky commits: %d\n", report.Risk.RiskyCommits)
		fmt.Printf("- High-risk commits: %d\n", report.Risk.HighRiskCommits)
		fmt.Printf("- Medium-risk commits: %d\n", report.Risk.MediumRiskCommits)
		for _, area := range report.Risk.Areas {
			fmt.Printf("- %s: %d commit(s), max score %.2f\n", area.Area, area.Count, area.MaxScore)
		}
		fmt.Println()
	}
}
