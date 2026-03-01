package context

import (
	"strings"
	"testing"
)

const singleFileDiff = `diff --git a/main.go b/main.go
index abc1234..def5678 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
 func main() {
@@ -10,3 +11,5 @@
 	// existing code
+	fmt.Println("hello")
+	fmt.Println("world")
 }
`

const multiFileDiff = `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,3 +1,4 @@
 package foo
+// added comment
 
diff --git a/bar.go b/bar.go
index 3333333..4444444 100644
--- a/bar.go
+++ b/bar.go
@@ -5,2 +5,3 @@
 func bar() {
+	return
 }
`

const deletedFileDiff = `diff --git a/removed.go b/removed.go
deleted file mode 100644
--- a/removed.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package removed
-
-func Gone() {
-	return
-}
`

const modeChangeDiff = `diff --git a/script.sh b/script.sh
old mode 100644
new mode 100755
`

const modeAndContentDiff = `diff --git a/script.sh b/script.sh
old mode 100644
new mode 100755
--- a/script.sh
+++ b/script.sh
@@ -1,3 +1,4 @@
 #!/bin/bash
+set -e
 echo "hello"
`

const binaryDiff = `diff --git a/image.png b/image.png
Binary files /dev/null and b/image.png differ
`

const renameDiff = `diff --git a/old.go b/new.go
similarity index 95%
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1,3 +1,3 @@
-package old
+package new
 
 func Run() {}
`

const spaceFilenameDiff = `diff --git "a/my file.go" "b/my file.go"
index 1111111..2222222 100644
--- "a/my file.go"
+++ "b/my file.go"
@@ -1,3 +1,4 @@
 package main
 
+import "fmt"
`

const newFileModeDiff = `diff --git a/new_script.sh b/new_script.sh
new file mode 100755
index 0000000..def5678
--- /dev/null
+++ b/new_script.sh
@@ -0,0 +1,2 @@
+#!/bin/bash
+echo "hello"
`


func TestParseDiff_SingleFile_TwoHunks(t *testing.T) {
	hunks := ParseDiff(singleFileDiff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	for _, h := range hunks {
		if h.FilePath != "main.go" {
			t.Errorf("expected file path main.go, got %s", h.FilePath)
		}
		if h.HunkID == "" {
			t.Error("expected non-empty HunkID")
		}
	}
	if hunks[0].Header != "@@ -1,3 +1,4 @@" {
		t.Errorf("unexpected header: %s", hunks[0].Header)
	}
}

func TestParseDiff_MultiFile(t *testing.T) {
	hunks := ParseDiff(multiFileDiff)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}
	if hunks[0].FilePath != "foo.go" {
		t.Errorf("expected foo.go, got %s", hunks[0].FilePath)
	}
	if hunks[1].FilePath != "bar.go" {
		t.Errorf("expected bar.go, got %s", hunks[1].FilePath)
	}
}

func TestParseDiff_SkipsBinary(t *testing.T) {
	hunks := ParseDiff(binaryDiff)
	if len(hunks) != 0 {
		t.Fatalf("expected 0 hunks for binary diff, got %d", len(hunks))
	}
}

func TestParseDiff_Rename(t *testing.T) {
	hunks := ParseDiff(renameDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "new.go" {
		t.Errorf("expected new.go, got %s", hunks[0].FilePath)
	}
	if hunks[0].RenamedFrom != "old.go" {
		t.Errorf("expected renamed_from=old.go, got %q", hunks[0].RenamedFrom)
	}
}

func TestParseDiff_DeletedFile(t *testing.T) {
	hunks := ParseDiff(deletedFileDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "removed.go" {
		t.Errorf("expected removed.go, got %s", hunks[0].FilePath)
	}
	if !hunks[0].DeletedFile {
		t.Error("expected DeletedFile=true")
	}
}

func TestParseDiff_ModeOnlyChange(t *testing.T) {
	hunks := ParseDiff(modeChangeDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 synthetic hunk for mode change, got %d", len(hunks))
	}
	if hunks[0].OldMode != "100644" {
		t.Errorf("expected old_mode=100644, got %q", hunks[0].OldMode)
	}
	if hunks[0].NewMode != "100755" {
		t.Errorf("expected new_mode=100755, got %q", hunks[0].NewMode)
	}
	if hunks[0].Header != "" {
		t.Errorf("expected empty header for mode-only hunk, got %q", hunks[0].Header)
	}
}

func TestParseDiff_ModeAndContentChange(t *testing.T) {
	hunks := ParseDiff(modeAndContentDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].OldMode != "100644" {
		t.Errorf("expected old_mode=100644, got %q", hunks[0].OldMode)
	}
	if hunks[0].NewMode != "100755" {
		t.Errorf("expected new_mode=100755, got %q", hunks[0].NewMode)
	}
	if hunks[0].Header == "" {
		t.Error("expected non-empty header for content hunk")
	}
}

func TestParseDiff_Empty(t *testing.T) {
	hunks := ParseDiff("")
	if len(hunks) != 0 {
		t.Fatalf("expected 0 hunks for empty diff, got %d", len(hunks))
	}
}

func TestParseDiff_CRLFLineEndings(t *testing.T) {
	crlf := strings.ReplaceAll(singleFileDiff, "\n", "\r\n")
	hunks := ParseDiff(crlf)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks from CRLF diff, got %d", len(hunks))
	}
	for _, h := range hunks {
		if h.FilePath != "main.go" {
			t.Errorf("expected file path main.go, got %s", h.FilePath)
		}
		if strings.Contains(h.Header, "\r") {
			t.Errorf("header should not contain \\r: %q", h.Header)
		}
	}
}

func TestParseDiff_MixedLineEndings(t *testing.T) {
	mixed := "diff --git a/f.go b/f.go\r\nindex aaa..bbb 100644\n--- a/f.go\r\n+++ b/f.go\n@@ -1,2 +1,3 @@\r\n pkg\r\n+new\n end\n"
	hunks := ParseDiff(mixed)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk from mixed-ending diff, got %d", len(hunks))
	}
	if hunks[0].FilePath != "f.go" {
		t.Errorf("expected f.go, got %s", hunks[0].FilePath)
	}
}

func TestParseDiff_HunkIDsUnique(t *testing.T) {
	hunks := ParseDiff(singleFileDiff)
	seen := make(map[string]bool)
	for _, h := range hunks {
		if seen[h.HunkID] {
			t.Errorf("duplicate HunkID: %s", h.HunkID)
		}
		seen[h.HunkID] = true
	}
}

func TestParseDiff_SpacesInFilename(t *testing.T) {
	hunks := ParseDiff(spaceFilenameDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if hunks[0].FilePath != "my file.go" {
		t.Errorf("expected 'my file.go', got %q", hunks[0].FilePath)
	}
}

func TestParseDiff_NewFileMode(t *testing.T) {
	hunks := ParseDiff(newFileModeDiff)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if !hunks[0].NewFile {
		t.Error("expected NewFile=true")
	}
	if hunks[0].NewMode != "100755" {
		t.Errorf("expected new_mode=100755, got %q", hunks[0].NewMode)
	}
	if hunks[0].FilePath != "new_script.sh" {
		t.Errorf("expected 'new_script.sh', got %q", hunks[0].FilePath)
	}
}

