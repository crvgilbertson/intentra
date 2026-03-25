package context

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/crvgilbertson/intentra/config"
)

func TestBuildContext_InitialCommitIncludesStagedFiles(t *testing.T) {
	repo := initTempGitRepo(t)
	writeFile(t, filepath.Join(repo, "staged.txt"), "hello\n")
	runGit(t, repo, "add", "staged.txt")

	restore := chdir(t, repo)
	defer restore()

	ec, err := BuildContext(context.Background(), config.DefaultConfig())
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if !ec.InitialCommit {
		t.Fatal("expected InitialCommit=true")
	}
	if len(ec.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(ec.Hunks))
	}
	if ec.Hunks[0].FilePath != "staged.txt" {
		t.Fatalf("expected staged.txt, got %q", ec.Hunks[0].FilePath)
	}
}

func TestBuildContext_InitialCommitIncludesEmptyUntrackedFile(t *testing.T) {
	repo := initTempGitRepo(t)
	writeFile(t, filepath.Join(repo, "empty.txt"), "")

	restore := chdir(t, repo)
	defer restore()

	ec, err := BuildContext(context.Background(), config.DefaultConfig())
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if !ec.InitialCommit {
		t.Fatal("expected InitialCommit=true")
	}
	if len(ec.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(ec.Hunks))
	}
	if ec.Hunks[0].FilePath != "empty.txt" {
		t.Fatalf("expected empty.txt, got %q", ec.Hunks[0].FilePath)
	}
	if !ec.Hunks[0].NewFile {
		t.Fatal("expected empty untracked file to be marked as new")
	}
}

func initTempGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	return repo
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
