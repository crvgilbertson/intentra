package context

import (
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
}

func TestParseDiff_Empty(t *testing.T) {
	hunks := ParseDiff("")
	if len(hunks) != 0 {
		t.Fatalf("expected 0 hunks for empty diff, got %d", len(hunks))
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
