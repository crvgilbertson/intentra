package models

type Hunk struct {
	HunkID   string `json:"hunk_id"`
	FilePath string `json:"file_path"`
	Header   string `json:"header"`
	Patch    string `json:"patch"`
	Summary  string `json:"summary"`
	NewFile  bool   `json:"new_file,omitempty"`
}
