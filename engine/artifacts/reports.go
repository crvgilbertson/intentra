package artifacts

import (
	"fmt"
	"sort"

	"github.com/crvgilbertson/intentra/engine/models"
)

type TicketRef struct {
	ID     string `json:"id"`
	Source string `json:"source,omitempty"`
}

type RiskThresholds struct {
	Medium float64
	High   float64
}

type Options struct {
	Ticket         *TicketRef
	Version        string
	Since          string
	RiskThresholds RiskThresholds
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type CommitEntry struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Scope     *string            `json:"scope,omitempty"`
	Subject   string             `json:"subject"`
	Title     string             `json:"title"`
	Body      *string            `json:"body,omitempty"`
	Breaking  bool               `json:"breaking"`
	Rationale string             `json:"rationale,omitempty"`
	Risk      *models.CommitRisk `json:"risk,omitempty"`
}

type Section struct {
	Key     string        `json:"key"`
	Title   string        `json:"title"`
	Commits []CommitEntry `json:"commits"`
}

type RiskAreaSummary struct {
	Area     string  `json:"area"`
	Count    int     `json:"count"`
	MaxScore float64 `json:"max_score"`
}

type RiskCommitEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Breaking bool     `json:"breaking"`
	Score    float64  `json:"score"`
	Level    string   `json:"level"`
	Areas    []string `json:"areas,omitempty"`
	Signals  []string `json:"signals,omitempty"`
}

type RiskSummary struct {
	AggregateScore    float64           `json:"aggregate_score"`
	AggregateLevel    string            `json:"aggregate_level"`
	RiskyCommits      int               `json:"risky_commits"`
	HighRiskCommits   int               `json:"high_risk_commits"`
	MediumRiskCommits int               `json:"medium_risk_commits"`
	SensitiveAreas    []string          `json:"sensitive_areas,omitempty"`
	Areas             []RiskAreaSummary `json:"areas,omitempty"`
}

type ReleaseNotes struct {
	Ticket   *TicketRef    `json:"ticket,omitempty"`
	Summary  ReleaseStats  `json:"summary"`
	Sections []Section     `json:"sections,omitempty"`
	Breaking []CommitEntry `json:"breaking_changes,omitempty"`
	Risk     RiskSummary   `json:"risk"`
}

type ReleaseStats struct {
	TotalCommits    int         `json:"total_commits"`
	BreakingChanges int         `json:"breaking_changes"`
	TypeCounts      []TypeCount `json:"type_counts,omitempty"`
}

type Changelog struct {
	Version  string        `json:"version,omitempty"`
	Since    string        `json:"since,omitempty"`
	Ticket   *TicketRef    `json:"ticket,omitempty"`
	Sections []Section     `json:"sections,omitempty"`
	Breaking []CommitEntry `json:"breaking_changes,omitempty"`
	Risk     RiskSummary   `json:"risk"`
}

type RiskReport struct {
	Ticket       *TicketRef        `json:"ticket,omitempty"`
	TotalCommits int               `json:"total_commits"`
	Summary      RiskSummary       `json:"summary"`
	Commits      []RiskCommitEntry `json:"commits,omitempty"`
}

func BuildReleaseNotes(plan models.CommitPlan, opts Options) ReleaseNotes {
	sections, breaking := buildSections(plan.Commits)
	return ReleaseNotes{
		Ticket: opts.Ticket,
		Summary: ReleaseStats{
			TotalCommits:    len(plan.Commits),
			BreakingChanges: len(breaking),
			TypeCounts:      buildTypeCounts(plan.Commits),
		},
		Sections: sections,
		Breaking: breaking,
		Risk:     summarizeRisk(plan.Commits, normalizedThresholds(opts.RiskThresholds)),
	}
}

func BuildChangelog(plan models.CommitPlan, opts Options) Changelog {
	sections, breaking := buildSections(plan.Commits)
	return Changelog{
		Version:  opts.Version,
		Since:    opts.Since,
		Ticket:   opts.Ticket,
		Sections: sections,
		Breaking: breaking,
		Risk:     summarizeRisk(plan.Commits, normalizedThresholds(opts.RiskThresholds)),
	}
}

func BuildRiskReport(plan models.CommitPlan, opts Options) RiskReport {
	summary := summarizeRisk(plan.Commits, normalizedThresholds(opts.RiskThresholds))
	commits := make([]RiskCommitEntry, 0)
	for _, c := range plan.Commits {
		if c.Risk == nil {
			continue
		}
		commits = append(commits, RiskCommitEntry{
			ID:       c.ID,
			Title:    c.FullSubject(),
			Breaking: c.Breaking,
			Score:    c.Risk.Score,
			Level:    c.Risk.Level,
			Areas:    append([]string(nil), c.Risk.Areas...),
			Signals:  append([]string(nil), c.Risk.Signals...),
		})
	}

	sort.SliceStable(commits, func(i, j int) bool {
		if commits[i].Score != commits[j].Score {
			return commits[i].Score > commits[j].Score
		}
		return commits[i].Title < commits[j].Title
	})

	return RiskReport{
		Ticket:       opts.Ticket,
		TotalCommits: len(plan.Commits),
		Summary:      summary,
		Commits:      commits,
	}
}

func buildSections(commits []models.CommitUnit) ([]Section, []CommitEntry) {
	grouped := make(map[string][]CommitEntry)
	var breaking []CommitEntry
	for _, c := range commits {
		entry := commitEntryFromUnit(c)
		key, title := sectionForType(c.Type)
		grouped[key] = append(grouped[key], entry)
		if c.Breaking {
			breaking = append(breaking, entry)
		}
		_ = title
	}

	order := []string{"feat", "fix", "perf", "refactor", "docs", "test", "chore", "other"}
	sections := make([]Section, 0, len(grouped))
	for _, key := range order {
		entries := grouped[key]
		if len(entries) == 0 {
			continue
		}
		_, title := sectionForType(key)
		sections = append(sections, Section{
			Key:     key,
			Title:   title,
			Commits: entries,
		})
	}
	return sections, breaking
}

func buildTypeCounts(commits []models.CommitUnit) []TypeCount {
	counts := make(map[string]int)
	for _, c := range commits {
		key, _ := sectionForType(c.Type)
		counts[key]++
	}

	order := []string{"feat", "fix", "perf", "refactor", "docs", "test", "chore", "other"}
	out := make([]TypeCount, 0, len(counts))
	for _, key := range order {
		if counts[key] == 0 {
			continue
		}
		out = append(out, TypeCount{Type: key, Count: counts[key]})
	}
	return out
}

func commitEntryFromUnit(c models.CommitUnit) CommitEntry {
	return CommitEntry{
		ID:        c.ID,
		Type:      c.Type,
		Scope:     c.Scope,
		Subject:   c.Subject,
		Title:     c.FullSubject(),
		Body:      c.Body,
		Breaking:  c.Breaking,
		Rationale: c.Rationale,
		Risk:      c.Risk,
	}
}

func summarizeRisk(commits []models.CommitUnit, thresholds RiskThresholds) RiskSummary {
	if len(commits) == 0 {
		return RiskSummary{AggregateLevel: "low"}
	}

	areaSummary := make(map[string]*RiskAreaSummary)
	var aggregate float64
	var riskyCommits int
	var highRisk int
	var mediumRisk int

	for _, c := range commits {
		if c.Risk != nil {
			aggregate += c.Risk.Score
			riskyCommits++
			switch classifyRisk(c.Risk.Score, thresholds) {
			case "high":
				highRisk++
			case "medium":
				mediumRisk++
			}
			for _, area := range c.Risk.Areas {
				s := areaSummary[area]
				if s == nil {
					s = &RiskAreaSummary{Area: area}
					areaSummary[area] = s
				}
				s.Count++
				if c.Risk.Score > s.MaxScore {
					s.MaxScore = c.Risk.Score
				}
			}
		}
	}

	aggregate /= float64(len(commits))
	areas := make([]RiskAreaSummary, 0, len(areaSummary))
	sensitiveAreas := make([]string, 0, len(areaSummary))
	for _, s := range areaSummary {
		areas = append(areas, *s)
		sensitiveAreas = append(sensitiveAreas, s.Area)
	}
	sort.SliceStable(areas, func(i, j int) bool {
		if areas[i].Count != areas[j].Count {
			return areas[i].Count > areas[j].Count
		}
		return areas[i].Area < areas[j].Area
	})
	sort.Strings(sensitiveAreas)

	return RiskSummary{
		AggregateScore:    aggregate,
		AggregateLevel:    classifyRisk(aggregate, thresholds),
		RiskyCommits:      riskyCommits,
		HighRiskCommits:   highRisk,
		MediumRiskCommits: mediumRisk,
		SensitiveAreas:    sensitiveAreas,
		Areas:             areas,
	}
}

func normalizedThresholds(th RiskThresholds) RiskThresholds {
	if th.Medium <= 0 {
		th.Medium = 0.3
	}
	if th.High <= 0 {
		th.High = 0.6
	}
	return th
}

func classifyRisk(score float64, thresholds RiskThresholds) string {
	switch {
	case score >= thresholds.High:
		return "high"
	case score >= thresholds.Medium:
		return "medium"
	default:
		return "low"
	}
}

func sectionForType(commitType string) (string, string) {
	switch commitType {
	case "feat":
		return "feat", "Features"
	case "fix":
		return "fix", "Fixes"
	case "perf":
		return "perf", "Performance"
	case "refactor":
		return "refactor", "Refactors"
	case "docs":
		return "docs", "Documentation"
	case "test":
		return "test", "Tests"
	case "chore":
		return "chore", "Maintenance"
	default:
		return "other", fmt.Sprintf("Other Changes")
	}
}
