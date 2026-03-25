package context

import (
	"strings"

	"github.com/crvgilbertson/intentra/engine/models"
)

// ParseDiff splits unified diff output into individual Hunks.
// It expects the output of `git diff` (unified format).
func ParseDiff(raw string) []models.Hunk {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	fileSections := splitFileSections(raw)
	var hunks []models.Hunk

	for _, section := range fileSections {
		filePath := extractFilePath(section)
		if filePath == "" {
			continue
		}
		if isBinaryDiff(section) {
			continue
		}

		newFile := isNewFileDiff(section)
		deletedFile := isDeletedFileDiff(section)
		renamedFrom := extractRenamedFrom(section)
		oldMode, newMode := extractModeChange(section)

		fileHunks := splitHunks(section)

		if len(fileHunks) == 0 {
			// Metadata-only or empty-file change.
			// Create a synthetic hunk so it isn't silently dropped.
			if newFile || deletedFile || (oldMode != "" && newMode != "") || renamedFrom != "" {
				hunk := models.Hunk{
					FilePath:    filePath,
					NewFile:     newFile,
					DeletedFile: deletedFile,
					RenamedFrom: renamedFrom,
					OldMode:     oldMode,
					NewMode:     newMode,
				}
				hunk.HunkID = HashHunk(hunk)
				hunks = append(hunks, hunk)
			}
			continue
		}

		for _, h := range fileHunks {
			header, patch := splitHunkHeaderAndPatch(h)
			if header == "" {
				continue
			}
			hunk := models.Hunk{
				FilePath:    filePath,
				Header:      header,
				Patch:       patch,
				NewFile:     newFile,
				DeletedFile: deletedFile,
				RenamedFrom: renamedFrom,
				OldMode:     oldMode,
				NewMode:     newMode,
			}
			hunk.HunkID = HashHunk(hunk)
			hunks = append(hunks, hunk)
		}
	}

	return hunks
}

// splitFileSections splits on "diff --git" boundaries that appear at the
// start of a line. Occurrences inside patch content (e.g. source code that
// literally contains "diff --git") are not treated as boundaries.
func splitFileSections(raw string) []string {
	const marker = "diff --git "
	lines := strings.Split(raw, "\n")

	var sections []string
	var current []string

	for _, line := range lines {
		if strings.HasPrefix(line, marker) {
			if len(current) > 0 {
				sec := strings.Join(current, "\n")
				if strings.TrimSpace(sec) != "" {
					sections = append(sections, sec)
				}
			}
			current = []string{line}
		} else {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		sec := strings.Join(current, "\n")
		if strings.TrimSpace(sec) != "" {
			sections = append(sections, sec)
		}
	}

	return sections
}

func extractFilePath(section string) string {
	lines := strings.SplitN(section, "\n", 10)
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			// e.g., diff --git a/foo b/foo
			// diff --git "a/foo bar" "b/foo bar"
			suffix := strings.TrimPrefix(line, "diff --git ")
			// Split by " b/" or ` "b/` to handle quotes
			idx := strings.Index(suffix, " b/")
			if idx == -1 {
				idx = strings.Index(suffix, ` "b/`)
			}
			if idx != -1 {
				path := strings.TrimSpace(suffix[idx+1:])
				path = strings.TrimPrefix(path, `"`)
				path = strings.TrimPrefix(path, "b/")
				path = strings.TrimSuffix(path, `"`)
				return path
			}
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimSpace(strings.TrimPrefix(line, "+++ b/"))
		}
		if strings.HasPrefix(line, `+++ "b/`) {
			path := strings.TrimSpace(strings.TrimPrefix(line, `+++ "b/`))
			return strings.TrimSuffix(path, `"`)
		}
	}
	return ""
}

// isBinaryDiff checks the diff header lines (not patch content) for binary markers.
func isBinaryDiff(section string) bool {
	for _, line := range headerLines(section) {
		if strings.HasPrefix(line, "Binary files") || strings.HasPrefix(line, "GIT binary patch") {
			return true
		}
	}
	return false
}

// isNewFileDiff checks the diff header lines for new-file markers.
func isNewFileDiff(section string) bool {
	for _, line := range headerLines(section) {
		if strings.HasPrefix(line, "new file mode") || line == "--- /dev/null" {
			return true
		}
	}
	return false
}

// isDeletedFileDiff checks the diff header lines for deleted-file markers.
func isDeletedFileDiff(section string) bool {
	for _, line := range headerLines(section) {
		if strings.HasPrefix(line, "deleted file mode") || line == "+++ /dev/null" {
			return true
		}
	}
	return false
}

// extractRenamedFrom returns the original file path for a rename diff.
func extractRenamedFrom(section string) string {
	for _, line := range headerLines(section) {
		if strings.HasPrefix(line, "rename from ") {
			return strings.TrimPrefix(line, "rename from ")
		}
	}
	return ""
}

// extractModeChange returns old/new mode strings if a mode change or creation/deletion is present.
func extractModeChange(section string) (oldMode, newMode string) {
	for _, line := range headerLines(section) {
		if strings.HasPrefix(line, "old mode ") {
			oldMode = strings.TrimSpace(strings.TrimPrefix(line, "old mode "))
		}
		if strings.HasPrefix(line, "new mode ") {
			newMode = strings.TrimSpace(strings.TrimPrefix(line, "new mode "))
		}
		if strings.HasPrefix(line, "new file mode ") {
			newMode = strings.TrimSpace(strings.TrimPrefix(line, "new file mode "))
		}
		if strings.HasPrefix(line, "deleted file mode ") {
			oldMode = strings.TrimSpace(strings.TrimPrefix(line, "deleted file mode "))
		}
	}
	return
}

// headerLines returns the lines before the first @@ hunk marker.
func headerLines(section string) []string {
	var headers []string
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, "@@") {
			break
		}
		headers = append(headers, line)
	}
	return headers
}

// splitHunks splits a file section into individual hunk strings starting at @@ markers.
func splitHunks(section string) []string {
	var hunks []string
	lines := strings.Split(section, "\n")
	var current []string
	inHunk := false

	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			if inHunk && len(current) > 0 {
				hunks = append(hunks, strings.Join(current, "\n"))
			}
			current = []string{line}
			inHunk = true
			continue
		}
		if inHunk {
			current = append(current, line)
		}
	}
	if inHunk && len(current) > 0 {
		hunks = append(hunks, strings.Join(current, "\n"))
	}

	return hunks
}

// splitHunkHeaderAndPatch separates the @@ header line from the patch body.
func splitHunkHeaderAndPatch(hunk string) (header, patch string) {
	idx := strings.Index(hunk, "\n")
	if idx == -1 {
		return strings.TrimSpace(hunk), ""
	}
	header = strings.TrimSpace(hunk[:idx])
	patch = strings.TrimRight(hunk[idx+1:], "\n")
	return header, patch
}
