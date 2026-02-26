package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/crvgilbertson/intentra/cmd/ui"
)

const (
	localCmdTimeout = 30 * time.Second
	netCmdTimeout   = 120 * time.Second
)

func localCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), localCmdTimeout)
}

func netCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), netCmdTimeout)
}

func currentBranch() (string, error) {
	ctx, cancel := localCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hasUpstream(branch string) bool {
	ctx, cancel := localCtx()
	defer cancel()
	return exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", branch+"@{upstream}").Run() == nil
}

func remoteExists(remote string) bool {
	ctx, cancel := localCtx()
	defer cancel()
	return exec.CommandContext(ctx, "git", "remote", "get-url", remote).Run() == nil
}

func pushBranch(remote string) error {
	ctx, cancel := netCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "push", remote).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func pushBranchWithUpstream(remote, branch string) error {
	ctx, cancel := netCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "push", "--set-upstream", remote, branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func findGitDir() string {
	ctx, cancel := localCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func preflightCheck() error {
	gitDir := findGitDir()
	if gitDir == "" {
		return nil
	}

	checks := []struct {
		path    string
		isDir   bool
		message string
		hint    string
	}{
		{filepath.Join(gitDir, "MERGE_HEAD"), false,
			"Repository is in the middle of a merge.",
			"Resolve the merge first: git merge --continue  OR  git merge --abort"},
		{filepath.Join(gitDir, "CHERRY_PICK_HEAD"), false,
			"Repository is in the middle of a cherry-pick.",
			"Resolve it first: git cherry-pick --continue  OR  git cherry-pick --abort"},
		{filepath.Join(gitDir, "BISECT_LOG"), false,
			"Repository is in the middle of a bisect.",
			"Finish the bisect first: git bisect reset"},
		{filepath.Join(gitDir, "rebase-merge"), true,
			"Repository is in the middle of a rebase.",
			"Resolve the rebase first: git rebase --continue  OR  git rebase --abort"},
		{filepath.Join(gitDir, "rebase-apply"), true,
			"Repository is in the middle of a rebase or am.",
			"Resolve it first: git rebase --continue  OR  git rebase --abort"},
	}

	for _, c := range checks {
		var exists bool
		if c.isDir {
			info, err := os.Stat(c.path)
			exists = err == nil && info.IsDir()
		} else {
			_, err := os.Stat(c.path)
			exists = err == nil
		}
		if exists {
			ui.Error("%s\n", c.message)
			ui.Dim("  %s\n", c.hint)
			return fmt.Errorf("unsafe repo state: %s", c.message)
		}
	}

	ctx, cancel := localCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "diff", "--name-only", "--diff-filter=U").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		ui.Error("Repository has unmerged paths (merge conflicts).\n")
		ui.Dim("  Resolve all conflicts, then run: git add <files> && git commit\n")
		return fmt.Errorf("unsafe repo state: unmerged paths")
	}

	return nil
}

func warnIfHooksDetected() {
	var sources []string

	if info, err := os.Stat(".husky"); err == nil && info.IsDir() {
		sources = append(sources, "husky")
	}
	if _, err := os.Stat(".pre-commit-config.yaml"); err == nil {
		sources = append(sources, "pre-commit")
	}
	if data, err := os.ReadFile("package.json"); err == nil {
		if strings.Contains(string(data), "\"husky\"") {
			if len(sources) == 0 || sources[0] != "husky" {
				sources = append(sources, "husky (package.json)")
			}
		}
	}
	gitDir := findGitDir()
	if gitDir != "" {
		if info, err := os.Stat(filepath.Join(gitDir, "hooks", "pre-commit")); err == nil && !info.IsDir() {
			sources = append(sources, "git hooks")
		}
	}

	if len(sources) > 0 {
		ui.Dim("Detected commit hooks (%s). If commits are rejected, set skip_hooks: true in config.\n",
			strings.Join(sources, ", "))
	}
}

// warnIfBranchProtected checks GitHub branch protection via gh CLI and
// warns the user if the branch requires PRs. This is advisory only.
func warnIfBranchProtected(branch string) {
	if !ghAvailable() || !ghAuthenticated() {
		return
	}
	owner, repo, err := ghRepoInfo()
	if err != nil {
		return
	}
	requiresPR, err := ghBranchProtection(owner, repo, branch)
	if err != nil {
		return
	}
	if requiresPR {
		ui.Warn("Branch %q is protected on GitHub (requires PR).\n", branch)
		ui.Dim("  Push may be rejected. Consider: intentra pr\n")
	}
}

// branchUpToDate returns true if the local branch has no unpushed commits
// relative to the given remote. It verifies the tracking upstream actually
// points to the expected remote before comparing.
func branchUpToDate(remote, branch string) bool {
	if !hasUpstream(branch) {
		return false
	}
	ctx, cancel := localCtx()
	defer cancel()
	upstreamRemote, err := exec.CommandContext(ctx, "git", "config", "--get", fmt.Sprintf("branch.%s.remote", branch)).Output()
	if err != nil || strings.TrimSpace(string(upstreamRemote)) != remote {
		return false
	}
	out, err := exec.CommandContext(ctx, "git", "rev-list", "--count", branch+"@{upstream}.."+branch).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "0"
}

// smartPush handles the push logic shared between apply and push commands.
// It returns true if the branch is pushed (or was already up-to-date).
func smartPush(remote, branch string) bool {
	if !remoteExists(remote) {
		ui.Warn("Remote %q not found. Skipping push.\n", remote)
		ui.Dim("  Add a remote: git remote add %s <url>\n", remote)
		return false
	}

	warnIfBranchProtected(branch)

	if hasUpstream(branch) {
		if branchUpToDate(remote, branch) {
			ui.Dim("Branch %s is already up-to-date with remote.\n", branch)
			return true
		}
		ui.Info("Pushing to %s...\n", remote)
		if err := pushBranch(remote); err != nil {
			ui.Warn("Push failed: %v\n", err)
			ui.Dim("  You can push manually: git push\n")
			return false
		}
		ui.Success("Pushed to %s.\n", remote)
	} else {
		ui.Info("Pushing and setting upstream...\n")
		if err := pushBranchWithUpstream(remote, branch); err != nil {
			ui.Warn("Push failed: %v\n", err)
			ui.Dim("  You can push manually: git push --set-upstream %s %s\n", remote, branch)
			return false
		}
		ui.Success("Pushed to %s/%s.\n", remote, branch)
	}
	return true
}
