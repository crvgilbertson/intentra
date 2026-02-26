package validators

import (
	"errors"
	"strings"
	"testing"

	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/engine"
	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
)

func makeEC(hunks []models.Hunk) enginectx.EngineContext {
	cfg := config.DefaultConfig()
	cfg.Style.Scopes = []string{"auth", "ui", "core"}
	return enginectx.EngineContext{
		BaseRef: "HEAD",
		Hunks:   hunks,
		Config:  cfg,
	}
}

func ptr(s string) *string { return &s }

func validPlan() (models.CommitPlan, enginectx.EngineContext) {
	hunks := []models.Hunk{
		{HunkID: "h1"}, {HunkID: "h2"}, {HunkID: "h3"},
	}
	ec := makeEC(hunks)
	plan := models.CommitPlan{
		Style: ec.Config.Style,
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Scope: ptr("auth"), Subject: "add login flow", Hunks: []string{"h1", "h2"}},
			{ID: "c2", Type: "fix", Subject: "fix button alignment", Hunks: []string{"h3"}},
		},
	}
	return plan, ec
}

func TestValidate_ValidPlan(t *testing.T) {
	plan, ec := validPlan()
	if err := ValidateCommitPlan(plan, ec); err != nil {
		t.Fatalf("expected valid plan, got: %v", err)
	}
}

func TestValidate_MissingHunk(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[1].Hunks = nil
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for missing hunk h3")
	}
	if !strings.Contains(err.Error(), "h3") {
		t.Errorf("error should mention h3: %v", err)
	}
}

func TestValidate_DuplicateHunk(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[1].Hunks = []string{"h3", "h1"}
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for duplicate hunk h1")
	}
	if !strings.Contains(err.Error(), "multiple commits") {
		t.Errorf("error should mention multiple commits: %v", err)
	}
}

func TestValidate_UnknownHunk(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Hunks = []string{"h1", "h2", "hZZZ"}
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for unknown hunk")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention unknown: %v", err)
	}
}

func TestValidate_BadType(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Type = "yolo"
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for disallowed type")
	}
	if !strings.Contains(err.Error(), "disallowed type") {
		t.Errorf("error should mention disallowed type: %v", err)
	}
}

func TestValidate_BadScope(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Scope = ptr("database")
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for disallowed scope")
	}
	if !strings.Contains(err.Error(), "disallowed scope") {
		t.Errorf("error should mention disallowed scope: %v", err)
	}
}

func TestValidate_SubjectTooLong(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Subject = strings.Repeat("x", 100)
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for long subject")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should mention exceeds: %v", err)
	}
}

func TestValidate_SubjectTrailingPeriod(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Subject = "add login flow."
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for trailing period")
	}
	if !strings.Contains(err.Error(), "trailing period") {
		t.Errorf("error should mention trailing period: %v", err)
	}
}

func TestValidate_SubjectUppercase(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Subject = "Add login flow"
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for uppercase subject")
	}
	if !strings.Contains(err.Error(), "uppercase") {
		t.Errorf("error should mention uppercase: %v", err)
	}
}

func TestValidate_BreakingNoFooter(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Breaking = true
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected error for breaking without footer")
	}
	if !strings.Contains(err.Error(), "BREAKING CHANGE footer") {
		t.Errorf("error should mention BREAKING CHANGE footer: %v", err)
	}
}

func TestValidate_BreakingWithFooter(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Breaking = true
	plan.Commits[0].Footers = []models.Footer{{Token: "BREAKING CHANGE", Value: "auth API changed"}}
	if err := ValidateCommitPlan(plan, ec); err != nil {
		t.Fatalf("expected valid plan with breaking footer, got: %v", err)
	}
}

func TestValidate_EmptyScopesAllowsAnything(t *testing.T) {
	plan, ec := validPlan()
	ec.Config.Style.Scopes = []string{}
	plan.Commits[0].Scope = ptr("anything")
	if err := ValidateCommitPlan(plan, ec); err != nil {
		t.Fatalf("expected valid plan with empty scopes config, got: %v", err)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	plan, ec := validPlan()
	plan.Commits[0].Type = "yolo"
	plan.Commits[0].Subject = "Bad subject."
	plan.Commits[0].Breaking = true
	err := ValidateCommitPlan(plan, ec)
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	var engineVE *engine.ValidationError
	if !errors.As(err, &engineVE) {
		t.Fatalf("expected *engine.ValidationError, got %T", err)
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected inner *ValidationError, got %T", engineVE.Err)
	}
	if len(ve.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(ve.Errors), ve.Errors)
	}
}
