package executors

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	writeFile(t, dir, "hello.go", `package main

func main() {
	println("hello")
}
`)
	run("add", ".")
	run("commit", "-m", "initial commit")

	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func gitLog(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "log", "--oneline", "--format=%s")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func testHash(h models.Hunk) string {
	data := h.FilePath + h.Header + h.Patch
	sum := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", sum)
}

func TestGitExecutor_ApplySingleCommit(t *testing.T) {
	dir := setupTestRepo(t)

	writeFile(t, dir, "hello.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = dir
	diffOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff HEAD: %v", err)
	}

	hunks := parseDiffForTest(string(diffOut))
	if len(hunks) == 0 {
		t.Fatal("expected hunks from diff")
	}

	hunkIDs := make([]string, len(hunks))
	for i, h := range hunks {
		hunkIDs[i] = h.HunkID
	}

	plan := &models.CommitPlan{
		ToolVersion: "0.2.0",
		BaseRef:     "HEAD",
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: hunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, false)
	if err := executor.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(logs))
	}
	if logs[0] != "refactor: use fmt for printing" {
		t.Errorf("unexpected commit message: %s", logs[0])
	}
}

func TestGitExecutor_DryRun(t *testing.T) {
	dir := setupTestRepo(t)

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Subject: "test dry run", Hunks: []string{"fake"}},
		},
	}

	executor := NewGitExecutorWithHunks(dir, nil, false)
	if err := executor.Execute(context.Background(), plan, true); err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) != 1 {
		t.Errorf("expected 1 commit (initial only), got %d", len(logs))
	}
}

func TestGitExecutor_FailRestoresIndex(t *testing.T) {
	dir := setupTestRepo(t)

	hunks := []models.Hunk{
		{HunkID: "bad", FilePath: "nonexistent.go", Header: "@@ -1 +1 @@", Patch: "+broken"},
	}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "fix", Subject: "this should fail", Hunks: []string{"bad"}},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, false)
	err := executor.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error for bad patch")
	}
	if !strings.Contains(err.Error(), "index restored") {
		t.Errorf("error should mention index restored: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) != 1 {
		t.Errorf("expected only initial commit after abort, got %d", len(logs))
	}
}

func TestGitExecutor_PartialApplyRollsBack(t *testing.T) {
	dir := setupTestRepo(t)

	writeFile(t, dir, "hello.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = dir
	diffOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff HEAD: %v", err)
	}

	hunks := parseDiffForTest(string(diffOut))
	if len(hunks) == 0 {
		t.Fatal("expected hunks from diff")
	}

	hunkIDs := make([]string, len(hunks))
	for i, h := range hunks {
		hunkIDs[i] = h.HunkID
	}

	originalHead := gitHead(t, dir)

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: hunkIDs},
			{ID: "c2", Type: "fix", Subject: "this should fail", Hunks: []string{"nonexistent"}},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, false)
	err = executor.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error for c2")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention rolled back: %v", err)
	}

	currentHead := gitHead(t, dir)
	if currentHead != originalHead {
		t.Errorf("HEAD should be restored to %s, got %s", originalHead, currentHead)
	}

	logs := gitLog(t, dir)
	if len(logs) != 1 {
		t.Errorf("expected only initial commit after rollback, got %d: %v", len(logs), logs)
	}
}

func TestGitExecutor_DeletedFile(t *testing.T) {
	dir := setupTestRepo(t)

	os.Remove(filepath.Join(dir, "hello.go"))

	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = dir
	diffOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff HEAD: %v", err)
	}

	hunks := parseDiffForTest(string(diffOut))
	if len(hunks) == 0 {
		t.Fatal("expected hunks from diff for deleted file")
	}

	for i := range hunks {
		hunks[i].DeletedFile = true
	}

	hunkIDs := make([]string, len(hunks))
	for i, h := range hunks {
		hunkIDs[i] = h.HunkID
	}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "chore", Subject: "remove hello.go", Hunks: hunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, false)
	if err := executor.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute failed for deletion: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(logs))
	}
	if logs[0] != "chore: remove hello.go" {
		t.Errorf("unexpected commit message: %s", logs[0])
	}
}

func TestGitExecutor_StagedChangesIncluded(t *testing.T) {
	dir := setupTestRepo(t)

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	writeFile(t, dir, "hello.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	run("add", "hello.go")

	// git diff HEAD captures staged changes; git diff alone would not
	cmd := exec.Command("git", "diff", "HEAD")
	cmd.Dir = dir
	diffOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("git diff HEAD: %v", err)
	}
	if len(diffOut) == 0 {
		t.Fatal("git diff HEAD should show staged changes")
	}

	hunks := parseDiffForTest(string(diffOut))
	if len(hunks) == 0 {
		t.Fatal("expected hunks from staged diff")
	}

	hunkIDs := make([]string, len(hunks))
	for i, h := range hunks {
		hunkIDs[i] = h.HunkID
	}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: hunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, false)
	if err := executor.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute with staged changes failed: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(logs))
	}
}

func parseDiffForTest(raw string) []models.Hunk {
	var hunks []models.Hunk
	sections := strings.Split(raw, "diff --git ")
	for _, section := range sections {
		if !strings.Contains(section, "@@") {
			continue
		}
		section = "diff --git " + section

		var filePath string
		for _, line := range strings.SplitN(section, "\n", 10) {
			if strings.HasPrefix(line, "diff --git ") {
				parts := strings.SplitN(line, " b/", 2)
				if len(parts) == 2 {
					filePath = strings.TrimSpace(parts[1])
				}
			}
		}
		if filePath == "" {
			continue
		}

		lines := strings.Split(section, "\n")
		var current []string
		var header string
		inHunk := false
		for _, line := range lines {
			if strings.HasPrefix(line, "@@") {
				if inHunk && header != "" {
					patch := strings.TrimRight(strings.Join(current, "\n"), "\n")
					h := models.Hunk{FilePath: filePath, Header: header, Patch: patch}
					h.HunkID = testHash(h)
					hunks = append(hunks, h)
				}
				header = strings.TrimSpace(line)
				current = nil
				inHunk = true
				continue
			}
			if inHunk {
				current = append(current, line)
			}
		}
		if inHunk && header != "" {
			patch := strings.TrimRight(strings.Join(current, "\n"), "\n")
			h := models.Hunk{FilePath: filePath, Header: header, Patch: patch}
			h.HunkID = testHash(h)
			hunks = append(hunks, h)
		}
	}
	return hunks
}
