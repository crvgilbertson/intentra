package context

import (
	"strings"

	"intentra/engine/models"
)

// ParseDiff splits unified diff output into individual Hunks.
// It expects the output of `git diff` (unified format).
func ParseDiff(raw string) []models.Hunk {
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
			}
			hunk.HunkID = HashHunk(hunk)
			hunks = append(hunks, hunk)
		}
	}

	return hunks
}

// splitFileSections splits on "diff --git" boundaries.
func splitFileSections(raw string) []string {
	const marker = "diff --git "
	var sections []string
	remaining := raw

	for {
		idx := strings.Index(remaining, marker)
		if idx == -1 {
			if strings.TrimSpace(remaining) != "" {
				sections = append(sections, remaining)
			}
			break
		}
		if idx > 0 {
			before := remaining[:idx]
			if strings.TrimSpace(before) != "" {
				sections = append(sections, before)
			}
		}
		remaining = remaining[idx:]

		next := strings.Index(remaining[1:], marker)
		if next == -1 {
			sections = append(sections, remaining)
			break
		}
		sections = append(sections, remaining[:next+1])
		remaining = remaining[next+1:]
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

func isBinaryDiff(section string) bool {
	return strings.Contains(section, "Binary files") ||
		strings.Contains(section, "GIT binary patch")
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
