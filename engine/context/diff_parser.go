package context

import (
	"strings"

	"intentra/engine/models"
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
		fileHunks := splitHunks(section)
		for _, h := range fileHunks {
			header, patch := splitHunkHeaderAndPatch(h)
			if header == "" {
				continue
			}
			hunk := models.Hunk{
				FilePath: filePath,
				Header:   header,
				Patch:    patch,
				NewFile:  newFile,
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

// extractFilePath pulls the b/ path from the "diff --git a/... b/..." line,
// falling back to +++ header if needed.
func extractFilePath(section string) string {
	lines := strings.SplitN(section, "\n", 10)
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimSpace(strings.TrimPrefix(line, "+++ b/"))
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
