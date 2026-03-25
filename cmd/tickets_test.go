package cmd

import (
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func TestDetectTicketFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{branch: "feature/PROJ-123-add-release-notes", want: "PROJ-123"},
		{branch: "bugfix/API2-99-fix-timeout", want: "API2-99"},
		{branch: "chore/no-ticket-here", want: ""},
	}

	for _, tt := range tests {
		if got := detectTicketFromBranch(tt.branch); got != tt.want {
			t.Fatalf("detectTicketFromBranch(%q) = %q, want %q", tt.branch, got, tt.want)
		}
	}
}

func TestAddTicketFooter_AppendsOnce(t *testing.T) {
	cp := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Subject: "add release notes", Hunks: []string{"h1"}},
		},
	}

	if !addTicketFooter(cp, "PROJ-123") {
		t.Fatal("expected ticket footer to be added")
	}
	if got := len(cp.Commits[0].Footers); got != 1 {
		t.Fatalf("footer count = %d, want 1", got)
	}
	if addTicketFooter(cp, "PROJ-123") {
		t.Fatal("expected duplicate ticket footer to be ignored")
	}
	if got := len(cp.Commits[0].Footers); got != 1 {
		t.Fatalf("footer count after duplicate add = %d, want 1", got)
	}
}
