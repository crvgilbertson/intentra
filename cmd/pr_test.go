package cmd

import (
	"strings"
	"testing"

	"github.com/crvgilbertson/intentra/engine/artifacts"
	"github.com/crvgilbertson/intentra/engine/models"
)

func TestBuildPRFromPlan_SingleCommit(t *testing.T) {
	scope := "auth"
	cp := &models.CommitPlan{
		ToolVersion: "0.3.0",
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Scope: &scope, Subject: "add login flow", Hunks: []string{"h1", "h2"}},
		},
	}

	title, body := buildPRFromPlan(cp, nil)

	if title != "feat(auth): add login flow" {
		t.Errorf("unexpected title: %q", title)
	}
	if !strings.Contains(body, "## Changes") {
		t.Error("body should contain Changes header")
	}
	if !strings.Contains(body, "feat(auth)") {
		t.Error("body should reference the commit type and scope")
	}
}

func TestBuildPRFromPlan_MultipleCommits(t *testing.T) {
	scope1 := "auth"
	scope2 := "ui"
	cp := &models.CommitPlan{
		ToolVersion: "0.3.0",
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Scope: &scope1, Subject: "add login", Hunks: []string{"h1"}},
			{ID: "c2", Type: "fix", Scope: &scope2, Subject: "fix button", Hunks: []string{"h2"}},
			{ID: "c3", Type: "refactor", Subject: "clean up utils", Hunks: []string{"h3"}},
		},
	}

	title, body := buildPRFromPlan(cp, nil)

	if !strings.HasPrefix(title, "3 changes:") {
		t.Errorf("unexpected title: %q", title)
	}
	if !strings.Contains(body, "1.") && !strings.Contains(body, "2.") {
		t.Error("body should have numbered items")
	}
}

func TestSummarizeCommitTypes(t *testing.T) {
	scope := "auth"
	commits := []models.CommitUnit{
		{Type: "feat", Scope: &scope},
		{Type: "feat", Scope: &scope},
		{Type: "fix"},
	}

	result := summarizeCommitTypes(commits)
	if !strings.Contains(result, "feat(auth)") {
		t.Errorf("expected feat(auth) in result: %q", result)
	}
	if !strings.Contains(result, "fix") {
		t.Errorf("expected fix in result: %q", result)
	}
}

func TestBuildPRFromPlan_IncludesTicket(t *testing.T) {
	cp := &models.CommitPlan{
		ToolVersion: "0.6.0",
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Subject: "add changelog command", Hunks: []string{"h1"}},
		},
	}

	_, body := buildPRFromPlan(cp, &artifacts.TicketRef{ID: "PROJ-123", Source: "flag"})

	if !strings.Contains(body, "Ticket: PROJ-123") {
		t.Fatalf("expected ticket in PR body, got %q", body)
	}
}
