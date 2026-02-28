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

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
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

	executor := NewGitExecutorWithHunks(dir, nil, ExecutorOptions{})
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

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
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

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
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

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
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

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
	if err := executor.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute with staged changes failed: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(logs))
	}
}

func TestGitExecutor_CommitAuthor(t *testing.T) {
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
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: hunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{
		CommitAuthor: "Custom Author <custom@example.com>",
	})
	if err := executor.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	authorCmd := exec.Command("git", "log", "-1", "--format=%an <%ae>")
	authorCmd.Dir = dir
	authorOut, err := authorCmd.Output()
	if err != nil {
		t.Fatalf("git log author: %v", err)
	}
	got := strings.TrimSpace(string(authorOut))
	if got != "Custom Author <custom@example.com>" {
		t.Errorf("expected author 'Custom Author <custom@example.com>', got %q", got)
	}
}

func TestGitExecutor_SkipHooks(t *testing.T) {
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

	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}
	hookScript := "#!/bin/sh\necho 'pre-commit hook rejected' >&2\nexit 1\n"
	hookPath := filepath.Join(hooksDir, "pre-commit")
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		t.Fatalf("writing hook: %v", err)
	}
	_ = run

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
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: hunkIDs},
		},
	}

	// Without skip_hooks the rejecting hook should cause a failure
	noSkip := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
	err = noSkip.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error from pre-commit hook rejection")
	}
	if !strings.Contains(err.Error(), "hook") {
		t.Errorf("error should mention hook: %v", err)
	}

	// With skip_hooks the hook is bypassed
	withSkip := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{SkipHooks: true})
	if err := withSkip.Execute(context.Background(), plan, false); err != nil {
		t.Fatalf("execute with skip_hooks should succeed: %v", err)
	}

	logs := gitLog(t, dir)
	if len(logs) < 2 {
		t.Fatalf("expected at least 2 commits, got %d", len(logs))
	}
}

func TestGitExecutor_HookFailureRollback(t *testing.T) {
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

	writeFile(t, dir, "extra.go", "package main\n\nfunc extra() {}\n")
	run("add", ".")
	run("commit", "-m", "add extra.go")

	originalHead := gitHead(t, dir)

	writeFile(t, dir, "hello.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	writeFile(t, dir, "extra.go", "package main\n\nimport \"fmt\"\n\nfunc extra() {\n\tfmt.Println(\"extra\")\n}\n")

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

	var helloHunkIDs, extraHunkIDs []string
	for _, h := range hunks {
		if strings.Contains(h.FilePath, "extra.go") {
			extraHunkIDs = append(extraHunkIDs, h.HunkID)
		} else {
			helloHunkIDs = append(helloHunkIDs, h.HunkID)
		}
	}
	if len(helloHunkIDs) == 0 || len(extraHunkIDs) == 0 {
		t.Fatalf("expected hunks for both files, got hello=%d extra=%d", len(helloHunkIDs), len(extraHunkIDs))
	}

	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}
	hookScript := "#!/bin/sh\nif git diff --cached --name-only | grep -q extra.go; then\n  echo 'hook rejected: extra.go' >&2\n  exit 1\nfi\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte(hookScript), 0755); err != nil {
		t.Fatalf("writing hook: %v", err)
	}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: helloHunkIDs},
			{ID: "c2", Type: "refactor", Subject: "use fmt in extra", Hunks: extraHunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
	err = executor.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error from hook rejection on c2")
	}

	if !strings.Contains(err.Error(), "hook") {
		t.Errorf("error should mention hook: %v", err)
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention rollback of prior commit: %v", err)
	}

	currentHead := gitHead(t, dir)
	if currentHead != originalHead {
		t.Errorf("HEAD should be restored to %s, got %s", originalHead, currentHead)
	}

	logs := gitLog(t, dir)
	if len(logs) != 2 {
		t.Errorf("expected 2 commits after rollback (initial + add extra.go), got %d: %v", len(logs), logs)
	}
}

func TestGitExecutor_OrphanBranchRollback(t *testing.T) {
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

	writeFile(t, dir, "new1.go", "package main\n\nfunc new1() {}\n")

	goodHunk := models.Hunk{
		FilePath: "new1.go",
		Header:   "@@ -0,0 +1,3 @@",
		Patch:    "+package main\n+\n+func new1() {}",
		NewFile:  true,
	}
	goodHunk.HunkID = testHash(goodHunk)

	badHunk := models.Hunk{
		FilePath: "nonexistent.go",
		Header:   "@@ -1 +1 @@",
		Patch:    "+broken",
	}
	badHunk.HunkID = testHash(badHunk)

	hunks := []models.Hunk{goodHunk, badHunk}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "feat", Subject: "add new1.go", Hunks: []string{goodHunk.HunkID}},
			{ID: "c2", Type: "fix", Subject: "bad change", Hunks: []string{badHunk.HunkID}},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
	err := executor.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error for bad patch in c2")
	}

	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error should mention rollback: %v", err)
	}

	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = dir
	if out, headErr := headCmd.CombinedOutput(); headErr == nil {
		t.Errorf("expected HEAD to not exist after orphan rollback, but got: %s", strings.TrimSpace(string(out)))
	}

	logCmd := exec.Command("git", "log", "--oneline")
	logCmd.Dir = dir
	if out, logErr := logCmd.CombinedOutput(); logErr == nil {
		t.Errorf("expected git log to fail (no commits), but got: %s", strings.TrimSpace(string(out)))
	}
}

func TestGitExecutor_WorkingTreeDrift(t *testing.T) {
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

	writeFile(t, dir, "extra.go", "package main\n\nfunc extra() {}\n")
	run("add", ".")
	run("commit", "-m", "add extra.go")

	originalHead := gitHead(t, dir)

	writeFile(t, dir, "hello.go", "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n")
	writeFile(t, dir, "extra.go", "package main\n\nimport \"fmt\"\n\nfunc extra() {\n\tfmt.Println(\"extra\")\n}\n")

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

	var helloHunkIDs, extraHunkIDs []string
	for _, h := range hunks {
		if strings.Contains(h.FilePath, "extra.go") {
			extraHunkIDs = append(extraHunkIDs, h.HunkID)
		} else {
			helloHunkIDs = append(helloHunkIDs, h.HunkID)
		}
	}
	if len(helloHunkIDs) == 0 || len(extraHunkIDs) == 0 {
		t.Fatalf("expected hunks for both files, got hello=%d extra=%d", len(helloHunkIDs), len(extraHunkIDs))
	}

	hooksDir := filepath.Join(dir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		t.Fatalf("creating hooks dir: %v", err)
	}
	hookScript := "#!/bin/sh\necho '// external drift' >> extra.go\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "post-commit"), []byte(hookScript), 0755); err != nil {
		t.Fatalf("writing hook: %v", err)
	}

	plan := &models.CommitPlan{
		Commits: []models.CommitUnit{
			{ID: "c1", Type: "refactor", Subject: "use fmt for printing", Hunks: helloHunkIDs},
			{ID: "c2", Type: "refactor", Subject: "use fmt in extra", Hunks: extraHunkIDs},
		},
	}

	executor := NewGitExecutorWithHunks(dir, hunks, ExecutorOptions{})
	err = executor.Execute(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected error from working tree drift detection")
	}

	if !strings.Contains(err.Error(), "working tree changed externally") {
		t.Errorf("error should mention working tree drift: %v", err)
	}

	currentHead := gitHead(t, dir)
	if currentHead != originalHead {
		t.Errorf("HEAD should be restored to %s, got %s", originalHead, currentHead)
	}

	logs := gitLog(t, dir)
	if len(logs) != 2 {
		t.Errorf("expected 2 commits after drift rollback, got %d: %v", len(logs), logs)
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
