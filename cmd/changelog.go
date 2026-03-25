package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/engine/artifacts"
)

var (
	changelogJSON    bool
	changelogTicket  string
	changelogSince   string
	changelogVersion string
)

var changelogCmd = &cobra.Command{
	Use:   "changelog",
	Short: "Generate a structured changelog entry from the cached plan",
	Long:  "Builds a changelog-ready entry from the cached plan, grouped by change type and enriched with deterministic risk metadata. No extra AI call is made.",
	RunE:  runChangelog,
}

func init() {
	changelogCmd.Flags().BoolVar(&changelogJSON, "json", false, "output as JSON")
	changelogCmd.Flags().StringVar(&changelogTicket, "ticket", "", "ticket reference to include (for example PROJ-123)")
	changelogCmd.Flags().StringVar(&changelogSince, "since", "", "base version or reference for the changelog entry")
	changelogCmd.Flags().StringVar(&changelogVersion, "version", "", "release version label for the changelog entry")
	rootCmd.AddCommand(changelogCmd)
}

func runChangelog(cmd *cobra.Command, args []string) error {
	cp, err := loadPlanForArtifacts()
	if err != nil {
		return err
	}

	report := artifacts.BuildChangelog(*cp, artifactOptions(changelogTicket, changelogVersion, changelogSince, cp))

	if changelogJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling changelog: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printChangelogText(report)
	return nil
}

func printChangelogText(report artifacts.Changelog) {
	title := "## Unreleased"
	if report.Version != "" {
		title = fmt.Sprintf("## %s", report.Version)
	}
	if report.Since != "" {
		title += fmt.Sprintf(" (since %s)", report.Since)
	}

	fmt.Println(title)
	fmt.Println()
	if report.Ticket != nil {
		fmt.Printf("Ticket: %s\n\n", report.Ticket.ID)
	}

	for _, section := range report.Sections {
		fmt.Printf("### %s\n\n", section.Title)
		for _, c := range section.Commits {
			fmt.Printf("- %s\n", c.Title)
			if c.Body != nil && *c.Body != "" {
				fmt.Printf("  %s\n", *c.Body)
			}
		}
		fmt.Println()
	}

	if len(report.Breaking) > 0 {
		fmt.Println("### Breaking Changes")
		fmt.Println()
		for _, c := range report.Breaking {
			fmt.Printf("- %s\n", c.Title)
		}
		fmt.Println()
	}

	fmt.Println("### Risk")
	fmt.Println()
	fmt.Printf("- Aggregate risk: %.2f (%s)\n", report.Risk.AggregateScore, report.Risk.AggregateLevel)
	if len(report.Risk.SensitiveAreas) > 0 {
		fmt.Printf("- Sensitive areas: %s\n", strings.Join(report.Risk.SensitiveAreas, ", "))
	}
	if report.Risk.HighRiskCommits > 0 {
		fmt.Printf("- High-risk commits: %d\n", report.Risk.HighRiskCommits)
	}
	if report.Risk.MediumRiskCommits > 0 {
		fmt.Printf("- Medium-risk commits: %d\n", report.Risk.MediumRiskCommits)
	}
	fmt.Println()
}
