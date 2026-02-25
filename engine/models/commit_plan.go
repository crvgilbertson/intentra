package models

import "fmt"

type CommitStyle struct {
	Convention    string   `json:"convention" yaml:"convention"`
	MaxSubjectLen int      `json:"max_subject_len" yaml:"max_subject_len"`
	AllowedTypes  []string `json:"allowed_types" yaml:"allowed_types"`
	Scopes        []string `json:"scopes" yaml:"scopes"`
}

type Footer struct {
	Token string `json:"token"`
	Value string `json:"value"`
}

type CommitUnit struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Scope    *string  `json:"scope,omitempty"`
	Subject  string   `json:"subject"`
	Body     *string  `json:"body,omitempty"`
	Breaking bool     `json:"breaking"`
	Footers  []Footer `json:"footers,omitempty"`
	Hunks    []string `json:"hunks"`
}

// FullSubject returns the formatted commit subject line: type(scope): subject
func (c CommitUnit) FullSubject() string {
	prefix := c.Type
	if c.Scope != nil && *c.Scope != "" {
		prefix += "(" + *c.Scope + ")"
	}
	return prefix + ": " + c.Subject
}

type CommitPlan struct {
	ToolVersion string      `json:"tool_version"`
	BaseRef     string      `json:"base_ref"`
	Style       CommitStyle `json:"style"`
	Commits     []CommitUnit `json:"commits"`
}

// Plan is the generic plan interface that all planner outputs must satisfy.
type Plan interface {
	Validate() error
}

// Validate satisfies the Plan interface. Full business validation is performed
// by the validators package; this ensures the struct is minimally well-formed.
func (cp *CommitPlan) Validate() error {
	if len(cp.Commits) == 0 {
		return fmt.Errorf("commit plan has no commits")
	}
	for _, c := range cp.Commits {
		if c.ID == "" {
			return fmt.Errorf("commit missing ID")
		}
		if len(c.Hunks) == 0 {
			return fmt.Errorf("commit %s has no hunks", c.ID)
		}
	}
	return nil
}
