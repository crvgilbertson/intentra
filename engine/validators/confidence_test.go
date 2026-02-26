package validators

import (
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func TestConfidence_HighForCleanPlan(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "a.go", Header: "@@ -1,3 +1,4 @@"},
		{HunkID: "h2", FilePath: "b.go", Header: "@@ -1,3 +1,4 @@"},
	}
	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: []string{"h1"}},
			{ID: "c2", Hunks: []string{"h2"}},
		},
	}

	pc := AssessPlanConfidence(plan, hunks)
	if pc.Level != "high" {
		t.Errorf("expected high confidence, got %s (%.2f)", pc.Level, pc.Score)
	}
	if len(pc.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", pc.Warnings)
	}
}

func TestConfidence_PenalizeFileOverlap(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "a.go", Header: "@@ -1,3 +1,4 @@"},
		{HunkID: "h2", FilePath: "a.go", Header: "@@ -50,3 +51,4 @@"},
	}
	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: []string{"h1"}},
			{ID: "c2", Hunks: []string{"h2"}},
		},
	}

	pc := AssessPlanConfidence(plan, hunks)
	if pc.Score >= 1.0 {
		t.Errorf("expected penalty for file overlap, got %.2f", pc.Score)
	}
	hasOverlapWarning := false
	for _, w := range pc.Warnings {
		if contains(w, "split across") {
			hasOverlapWarning = true
		}
	}
	if !hasOverlapWarning {
		t.Error("expected file overlap warning")
	}
}

func TestConfidence_PenalizeEntanglement(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "a.go", Header: "@@ -1,5 +1,6 @@"},
		{HunkID: "h2", FilePath: "a.go", Header: "@@ -4,5 +5,6 @@"},
	}
	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: []string{"h1"}},
			{ID: "c2", Hunks: []string{"h2"}},
		},
	}

	pc := AssessPlanConfidence(plan, hunks)
	hasEntangleWarning := false
	for _, w := range pc.Warnings {
		if contains(w, "entangled") {
			hasEntangleWarning = true
		}
	}
	if !hasEntangleWarning {
		t.Error("expected entanglement warning")
	}
	if len(pc.Risks) == 0 {
		t.Error("expected per-commit risks")
	}
}

func TestConfidence_NoEntanglementSameCommit(t *testing.T) {
	hunks := []models.Hunk{
		{HunkID: "h1", FilePath: "a.go", Header: "@@ -1,5 +1,6 @@"},
		{HunkID: "h2", FilePath: "a.go", Header: "@@ -4,5 +5,6 @@"},
	}
	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: []string{"h1", "h2"}},
		},
	}

	pc := AssessPlanConfidence(plan, hunks)
	for _, w := range pc.Warnings {
		if contains(w, "entangled") {
			t.Error("should not warn about entanglement within the same commit")
		}
	}
}

func TestConfidence_PenalizeWideSpread(t *testing.T) {
	var hunks []models.Hunk
	var ids []string
	for i := 0; i < 12; i++ {
		id := "h" + string(rune('a'+i))
		hunks = append(hunks, models.Hunk{
			HunkID:   id,
			FilePath: "file" + string(rune('a'+i)) + ".go",
			Header:   "@@ -1,3 +1,4 @@",
		})
		ids = append(ids, id)
	}
	plan := models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Hunks: ids},
		},
	}

	pc := AssessPlanConfidence(plan, hunks)
	hasSpreadWarning := false
	for _, w := range pc.Warnings {
		if contains(w, "over-grouped") {
			hasSpreadWarning = true
		}
	}
	if !hasSpreadWarning {
		t.Error("expected wide spread warning for 12 files in one commit")
	}
}

func TestConfidence_ParseHunkRange(t *testing.T) {
	tests := []struct {
		header   string
		wantOk   bool
		newStart int
		newEnd   int
	}{
		{"@@ -1,3 +1,4 @@", true, 1, 5},
		{"@@ -10,5 +11,6 @@ func main()", true, 11, 17},
		{"@@ -0,0 +1,25 @@", true, 1, 26},
		{"@@ -1 +1 @@", true, 1, 2},
		{"not a header", false, 0, 0},
	}

	for _, tt := range tests {
		_, nr, ok := parseHunkRange(tt.header)
		if ok != tt.wantOk {
			t.Errorf("parseHunkRange(%q): ok=%v, want %v", tt.header, ok, tt.wantOk)
			continue
		}
		if ok && (nr.start != tt.newStart || nr.end != tt.newEnd) {
			t.Errorf("parseHunkRange(%q): got %d-%d, want %d-%d",
				tt.header, nr.start, nr.end, tt.newStart, tt.newEnd)
		}
	}
}

func TestConfidence_RangesOverlap(t *testing.T) {
	tests := []struct {
		a, b   lineRange
		margin int
		want   bool
	}{
		{lineRange{1, 5}, lineRange{3, 8}, 0, true},
		{lineRange{1, 5}, lineRange{6, 10}, 0, false},
		{lineRange{1, 5}, lineRange{6, 10}, 1, true},
		{lineRange{1, 5}, lineRange{10, 15}, 5, true},
		{lineRange{1, 5}, lineRange{11, 15}, 5, false},
	}

	for _, tt := range tests {
		got := rangesOverlap(tt.a, tt.b, tt.margin)
		if got != tt.want {
			t.Errorf("rangesOverlap(%v, %v, %d) = %v, want %v",
				tt.a, tt.b, tt.margin, got, tt.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsHelper(s, sub)
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
