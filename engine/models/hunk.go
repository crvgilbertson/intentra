package models

type Hunk struct {
	HunkID      string `json:"hunk_id"`
	FilePath    string `json:"file_path"`
	Header      string `json:"header"`
	Patch       string `json:"patch"`
	Summary     string `json:"summary"`
	NewFile     bool   `json:"new_file,omitempty"`
	DeletedFile bool   `json:"deleted_file,omitempty"`
	RenamedFrom string `json:"renamed_from,omitempty"`
	OldMode     string `json:"old_mode,omitempty"`
	NewMode     string `json:"new_mode,omitempty"`
}
