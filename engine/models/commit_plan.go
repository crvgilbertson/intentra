package models

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type CommitStyle struct {
	Convention    string   `json:"convention" yaml:"convention"`
	MaxSubjectLen int      `json:"max_subject_len" yaml:"max_subject_len"`
	AllowedTypes  []string `json:"allowed_types" yaml:"allowed_types"`
	Scopes        []string `json:"scopes" yaml:"scopes"`
	ScopeRequired bool     `json:"scope_required" yaml:"scope_required"`
	BodyRequired  bool     `json:"body_required" yaml:"body_required"`
}

type Footer struct {
	Token string `json:"token"`
	Value string `json:"value"`
}

type CommitRisk struct {
	Score   float64  `json:"score"`
	Level   string   `json:"level"`
	Areas   []string `json:"areas,omitempty"`
	Signals []string `json:"signals,omitempty"`
}

type CommitUnit struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Scope     *string  `json:"scope,omitempty"`
	Subject   string   `json:"subject"`
	Body      *string  `json:"body,omitempty"`
	Breaking  bool     `json:"breaking"`
	Footers   []Footer `json:"footers,omitempty"`
	Hunks     []string `json:"hunks"`
	Rationale string   `json:"rationale,omitempty"`
	Risk      *CommitRisk `json:"risk,omitempty"`
}

// FullSubject returns the formatted commit subject line: type(scope): subject
func (c CommitUnit) FullSubject() string {
	prefix := c.Type
	if c.Scope != nil && *c.Scope != "" {
		prefix += "(" + *c.Scope + ")"
	}
	return prefix + ": " + c.Subject
}

const CurrentSchemaVersion = "v1"

type ConfidenceComponents struct {
	Coverage       float64 `json:"coverage"`
	Entanglement   float64 `json:"entanglement"`
	RepairActivity float64 `json:"repair_activity"`
	Overlap        float64 `json:"overlap"`
	ReorderPenalty float64 `json:"reorder_penalty"`
}

type PlanConfidence struct {
	Overall    float64              `json:"overall"`
	Level      string               `json:"level"`
	Components ConfidenceComponents `json:"components"`
}

type PipelineTrace struct {
	Strategy          string `json:"strategy"`
	OrderingStrategy  string `json:"ordering_strategy,omitempty"`
	AtomicityProfile  string `json:"atomicity_profile,omitempty"`
	DedupCount        int    `json:"dedup_count"`
	OrphanCount       int    `json:"orphan_count"`
	RescueAttempted   bool   `json:"rescue_attempted"`
	RescueSucceeded   bool   `json:"rescue_succeeded"`
	RepairCount       int    `json:"repair_count"`
	ReorderApplied    bool   `json:"reorder_applied"`
	CommitsBefore     int    `json:"commits_before_reorder"`
	CommitsAfter      int    `json:"commits_after_reorder"`
}

type CommitPlan struct {
	SchemaVersion     string          `json:"schema_version"`
	ToolVersion       string          `json:"tool_version"`
	BaseRef           string          `json:"base_ref"`
	DiffFingerprint   string          `json:"diff_fingerprint"`
	PromptFingerprint string          `json:"prompt_fingerprint,omitempty"`
	Style             CommitStyle     `json:"style"`
	Commits           []CommitUnit    `json:"commits"`
	Confidence        *PlanConfidence `json:"confidence,omitempty"`
	Trace             *PipelineTrace  `json:"trace,omitempty"`
}

// DiffFingerprintFromHunks produces a stable hash of all hunk IDs. If the
// working tree changes, the hunks change, and this fingerprint won't match
// the cached plan — signaling that the plan is stale.
func DiffFingerprintFromHunks(hunks []Hunk) string {
	ids := make([]string, len(hunks))
	for i, h := range hunks {
		ids[i] = h.HunkID
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return fmt.Sprintf("%x", sum)
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
