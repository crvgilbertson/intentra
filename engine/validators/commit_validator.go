package validators

import (
	"fmt"
	"strings"
	"unicode"

	enginectx "intentra/engine/context"
	"intentra/engine/models"
)

// ValidationError collects multiple validation violations.
type ValidationError struct {
	Errors []string
}

func (ve *ValidationError) Error() string {
	return fmt.Sprintf("validation failed with %d error(s):\n- %s", len(ve.Errors), strings.Join(ve.Errors, "\n- "))
}

func (ve *ValidationError) add(msg string) {
	ve.Errors = append(ve.Errors, msg)
}

// ValidateCommitPlan checks a CommitPlan against business rules and the
// EngineContext it was derived from.
func ValidateCommitPlan(plan models.CommitPlan, ec enginectx.EngineContext) error {
	ve := &ValidationError{}

	validateHunkCoverage(plan, ec, ve)
	validateCommitTypes(plan, ve)
	validateScopes(plan, ec, ve)
	validateSubjects(plan, ec, ve)
	validateBreaking(plan, ve)

	if len(ve.Errors) > 0 {
		return ve
	}
	return nil
}

func validateHunkCoverage(plan models.CommitPlan, ec enginectx.EngineContext, ve *ValidationError) {
	expected := make(map[string]bool)
	for _, h := range ec.Hunks {
		expected[h.HunkID] = true
	}

	seen := make(map[string]bool)
	for _, c := range plan.Commits {
		for _, hid := range c.Hunks {
			if !expected[hid] {
				ve.add(fmt.Sprintf("commit %s references unknown hunk_id %q", c.ID, hid))
			}
			if seen[hid] {
				ve.add(fmt.Sprintf("hunk_id %q appears in multiple commits", hid))
			}
			seen[hid] = true
		}
	}

	for hid := range expected {
		if !seen[hid] {
			ve.add(fmt.Sprintf("hunk_id %q not assigned to any commit", hid))
		}
	}
}

func validateCommitTypes(plan models.CommitPlan, ve *ValidationError) {
	allowed := make(map[string]bool)
	for _, t := range plan.Style.AllowedTypes {
		allowed[t] = true
	}

	for _, c := range plan.Commits {
		if !allowed[c.Type] {
			ve.add(fmt.Sprintf("commit %s has disallowed type %q", c.ID, c.Type))
		}
	}
}

func validateScopes(plan models.CommitPlan, ec enginectx.EngineContext, ve *ValidationError) {
	if len(ec.Config.Style.Scopes) == 0 {
		return
	}

	allowed := make(map[string]bool)
	for _, s := range ec.Config.Style.Scopes {
		allowed[s] = true
	}

	for _, c := range plan.Commits {
		if c.Scope != nil && *c.Scope != "" && !allowed[*c.Scope] {
			ve.add(fmt.Sprintf("commit %s has disallowed scope %q", c.ID, *c.Scope))
		}
	}
}

func validateSubjects(plan models.CommitPlan, ec enginectx.EngineContext, ve *ValidationError) {
	maxLen := ec.Config.Style.MaxSubjectLen
	for _, c := range plan.Commits {
		if c.Subject == "" {
			ve.add(fmt.Sprintf("commit %s has empty subject", c.ID))
			continue
		}
		if maxLen > 0 && len(c.Subject) > maxLen {
			ve.add(fmt.Sprintf("commit %s subject exceeds %d chars (%d)", c.ID, maxLen, len(c.Subject)))
		}
		if strings.HasSuffix(c.Subject, ".") {
			ve.add(fmt.Sprintf("commit %s subject has trailing period", c.ID))
		}
		if len(c.Subject) > 0 && unicode.IsUpper(rune(c.Subject[0])) {
			ve.add(fmt.Sprintf("commit %s subject starts with uppercase", c.ID))
		}
	}
}

func validateBreaking(plan models.CommitPlan, ve *ValidationError) {
	for _, c := range plan.Commits {
		if !c.Breaking {
			continue
		}
		hasFooter := false
		for _, f := range c.Footers {
			if strings.EqualFold(f.Token, "BREAKING CHANGE") || strings.EqualFold(f.Token, "BREAKING-CHANGE") {
				hasFooter = true
				break
			}
		}
		if !hasFooter {
			ve.add(fmt.Sprintf("commit %s is breaking but has no BREAKING CHANGE footer", c.ID))
		}
	}
}
