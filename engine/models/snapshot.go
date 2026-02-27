package models

import "time"

// HunkMeta is the redacted hunk representation stored in snapshots.
// It contains enough information to reconstruct the planning context
// without including full file contents.
// HunkMeta is the hunk representation stored in snapshots.
// It contains enough information to reconstruct the planning context
// and replay the planner. Patch is included because the planner sends
// patch content to the LLM for clustering — without it, replay would
// always diverge. Patches are diff hunks, not full file contents.
type HunkMeta struct {
	HunkID      string `json:"hunk_id"`
	FilePath    string `json:"file_path"`
	Header      string `json:"header"`
	Patch       string `json:"patch"`
	Summary     string `json:"summary,omitempty"`
	NewFile     bool   `json:"new_file,omitempty"`
	DeletedFile bool   `json:"deleted_file,omitempty"`
	RenamedFrom string `json:"renamed_from,omitempty"`
}

func HunkMetaFromHunk(h Hunk) HunkMeta {
	return HunkMeta{
		HunkID:      h.HunkID,
		FilePath:    h.FilePath,
		Header:      h.Header,
		Patch:       h.Patch,
		Summary:     h.Summary,
		NewFile:     h.NewFile,
		DeletedFile: h.DeletedFile,
		RenamedFrom: h.RenamedFrom,
	}
}

type SnapshotConfig struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model"`
	Temperature  float64 `json:"temperature"`
	MaxCommits   int     `json:"max_commits"`
	MaxHunkLines int     `json:"max_hunk_lines"`
	BatchThreshold int   `json:"batch_threshold"`
	Style        CommitStyle `json:"style"`
}

type PlanSnapshot struct {
	EngineVersion     string           `json:"engine_version"`
	SchemaVersion     string           `json:"schema_version"`
	PromptFingerprint string           `json:"prompt_fingerprint"`
	Provider          string           `json:"provider"`
	Model             string           `json:"model"`
	Config            SnapshotConfig   `json:"config"`
	DiffFingerprint   string           `json:"diff_fingerprint"`
	HunkCount         int              `json:"hunk_count"`
	Hunks             []HunkMeta       `json:"hunks"`
	Plan              CommitPlan       `json:"plan"`
	Confidence        *PlanConfidence  `json:"confidence,omitempty"`
	Trace             *PipelineTrace   `json:"trace,omitempty"`
	Timestamp         time.Time        `json:"timestamp"`
}
