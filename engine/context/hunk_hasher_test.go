package context

import (
	"testing"

	"intentra/engine/models"
)

func TestHashHunk_Stability(t *testing.T) {
	h := models.Hunk{
		FilePath: "main.go",
		Header:   "@@ -1,3 +1,4 @@",
		Patch:    "+import \"fmt\"",
	}
	id1 := HashHunk(h)
	id2 := HashHunk(h)
	if id1 != id2 {
		t.Errorf("HashHunk not stable: %s != %s", id1, id2)
	}
}

func TestHashHunk_Uniqueness(t *testing.T) {
	h1 := models.Hunk{FilePath: "a.go", Header: "@@ -1 +1 @@", Patch: "+x"}
	h2 := models.Hunk{FilePath: "b.go", Header: "@@ -1 +1 @@", Patch: "+x"}
	h3 := models.Hunk{FilePath: "a.go", Header: "@@ -2 +2 @@", Patch: "+x"}
	h4 := models.Hunk{FilePath: "a.go", Header: "@@ -1 +1 @@", Patch: "+y"}

	ids := map[string]string{
		"h1": HashHunk(h1),
		"h2": HashHunk(h2),
		"h3": HashHunk(h3),
		"h4": HashHunk(h4),
	}

	seen := make(map[string]string)
	for name, id := range ids {
		if prev, ok := seen[id]; ok {
			t.Errorf("collision between %s and %s: %s", prev, name, id)
		}
		seen[id] = name
	}
}

func TestHashHunk_Length(t *testing.T) {
	h := models.Hunk{FilePath: "x.go", Header: "@@", Patch: "+"}
	id := HashHunk(h)
	if len(id) != 64 {
		t.Errorf("expected 64-char hex SHA-256, got len %d", len(id))
	}
}
