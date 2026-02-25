package models

// ChangeIntent captures the detected intent behind a set of changes.
// Reserved for Phase 2+ capabilities (risk scoring, PR planning).
type ChangeIntent struct {
	Summary    string   `json:"summary"`
	Categories []string `json:"categories"`
}
