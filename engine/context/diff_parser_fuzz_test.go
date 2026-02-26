package context

import (
	"testing"
)

func FuzzParseDiff(f *testing.F) {
	f.Add(singleFileDiff)
	f.Add(multiFileDiff)
	f.Add(deletedFileDiff)
	f.Add(modeChangeDiff)
	f.Add(modeAndContentDiff)
	f.Add(binaryDiff)
	f.Add(renameDiff)
	f.Add("")
	f.Add("diff --git a/f b/f\n")
	f.Add("diff --git a/f b/f\n@@ -0,0 +1 @@\n+x\n")
	f.Add("diff --git a/f b/f\n--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n")
	f.Add("not a diff at all\nrandom text\n@@@@\n")
	f.Add("diff --git a/f b/f\nBinary files differ\n")
	f.Add("diff --git a/f b/f\nnew file mode 100644\n--- /dev/null\n+++ b/f\n@@ -0,0 +1,0 @@\n")
	f.Add("diff --git a/ b/f b/ b/f\n--- a/ b/f\n+++ b/ b/f\n@@ -1,3 +1,4 @@\n context\n+added\n context\n")
	f.Add("\r\ndiff --git a/f b/f\r\n--- a/f\r\n+++ b/f\r\n@@ -1,1 +1,1 @@\r\n-old\r\n+new\r\n")

	f.Fuzz(func(t *testing.T, raw string) {
		hunks := ParseDiff(raw)

		for _, h := range hunks {
			if h.HunkID == "" {
				t.Error("hunk has empty HunkID")
			}
			if h.FilePath == "" {
				t.Error("hunk has empty FilePath")
			}
		}

		ids := make(map[string]int)
		for _, h := range hunks {
			ids[h.HunkID]++
		}
		for id, count := range ids {
			if count > 1 {
				// Duplicate IDs are possible if identical hunks exist in input;
				// we just verify no panic occurred during dedup-safe operations.
				_ = id
			}
		}
	})
}

func FuzzExtractFilePath(f *testing.F) {
	f.Add("diff --git a/main.go b/main.go\n--- a/main.go\n+++ b/main.go\n")
	f.Add("diff --git a/f b/f\n")
	f.Add("diff --git a/ b/ b/ b/\n")
	f.Add("+++ b/fallback.go\n")
	f.Add("nothing useful\n")
	f.Add("")

	f.Fuzz(func(t *testing.T, section string) {
		_ = extractFilePath(section)
	})
}

func FuzzSplitHunks(f *testing.F) {
	f.Add("@@ -1,3 +1,4 @@\n context\n+added\n context\n")
	f.Add("@@ -1 +1 @@\n-old\n+new\n@@ -10 +10 @@\n-old2\n+new2\n")
	f.Add("no hunks here\n")
	f.Add("")
	f.Add("@@\n")
	f.Add("@@ -0,0 +0,0 @@\n")

	f.Fuzz(func(t *testing.T, section string) {
		_ = splitHunks(section)
	})
}

func FuzzSplitHunkHeaderAndPatch(f *testing.F) {
	f.Add("@@ -1,3 +1,4 @@\n context\n+added\n context")
	f.Add("@@ -1 +1 @@")
	f.Add("")
	f.Add("single line no newline")
	f.Add("\n\n\n")

	f.Fuzz(func(t *testing.T, hunk string) {
		header, patch := splitHunkHeaderAndPatch(hunk)
		_ = header
		_ = patch
	})
}
