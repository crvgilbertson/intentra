package artifacts

import (
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func makePlan() models.CommitPlan {
	scope := "auth"
	body := "Tighten validation and logging around the login flow."
	return models.CommitPlan{
		Commits: []models.CommitUnit{
			{
				ID:       "c1",
				Type:     "feat",
				Scope:    &scope,
				Subject:  "add login flow",
				Body:     &body,
				Hunks:    []string{"h1"},
				Breaking: false,
				Risk: &models.CommitRisk{
					Score:   0.8,
					Level:   "high",
					Areas:   []string{"auth"},
					Signals: []string{"risk.auth.pattern:auth/"},
				},
			},
			{
				ID:      "c2",
				Type:    "fix",
				Subject: "correct redirect handling",
				Hunks:   []string{"h2"},
				Risk: &models.CommitRisk{
					Score: 0.4,
					Level: "medium",
					Areas: []string{"routing"},
				},
			},
			{
				ID:       "c3",
				Type:     "refactor",
				Subject:  "rename token interfaces",
				Hunks:    []string{"h3"},
				Breaking: true,
			},
		},
	}
}

func TestBuildReleaseNotes_GroupsChangesAndRisk(t *testing.T) {
	report := BuildReleaseNotes(makePlan(), Options{
		Ticket: &TicketRef{ID: "PROJ-123", Source: "flag"},
		RiskThresholds: RiskThresholds{
			Medium: 0.3,
			High:   0.6,
		},
	})

	if report.Summary.TotalCommits != 3 {
		t.Fatalf("total commits = %d, want 3", report.Summary.TotalCommits)
	}
	if report.Summary.BreakingChanges != 1 {
		t.Fatalf("breaking changes = %d, want 1", report.Summary.BreakingChanges)
	}
	if report.Ticket == nil || report.Ticket.ID != "PROJ-123" {
		t.Fatalf("ticket = %#v, want PROJ-123", report.Ticket)
	}
	if len(report.Sections) != 3 {
		t.Fatalf("section count = %d, want 3", len(report.Sections))
	}
	if report.Sections[0].Title != "Features" {
		t.Fatalf("first section = %q, want Features", report.Sections[0].Title)
	}
	if report.Risk.AggregateLevel != "medium" {
		t.Fatalf("aggregate level = %q, want medium", report.Risk.AggregateLevel)
	}
	if len(report.Risk.SensitiveAreas) != 2 {
		t.Fatalf("sensitive areas = %v, want 2 entries", report.Risk.SensitiveAreas)
	}
}

func TestBuildRiskReport_SortsCommitsByScore(t *testing.T) {
	report := BuildRiskReport(makePlan(), Options{
		RiskThresholds: RiskThresholds{
			Medium: 0.3,
			High:   0.6,
		},
	})

	if len(report.Commits) != 2 {
		t.Fatalf("risk commits = %d, want 2", len(report.Commits))
	}
	if report.Commits[0].ID != "c1" {
		t.Fatalf("first risk commit = %s, want c1", report.Commits[0].ID)
	}
	if report.Summary.HighRiskCommits != 1 {
		t.Fatalf("high risk commits = %d, want 1", report.Summary.HighRiskCommits)
	}
	if report.Summary.MediumRiskCommits != 1 {
		t.Fatalf("medium risk commits = %d, want 1", report.Summary.MediumRiskCommits)
	}
}

func TestBuildChangelog_PreservesVersionAndSince(t *testing.T) {
	report := BuildChangelog(makePlan(), Options{
		Version: "v0.6.0",
		Since:   "v0.5.0",
	})

	if report.Version != "v0.6.0" {
		t.Fatalf("version = %q, want v0.6.0", report.Version)
	}
	if report.Since != "v0.5.0" {
		t.Fatalf("since = %q, want v0.5.0", report.Since)
	}
	if len(report.Breaking) != 1 {
		t.Fatalf("breaking count = %d, want 1", len(report.Breaking))
	}
}
