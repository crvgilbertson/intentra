package cmd

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func ghAvailable() bool {
	ctx, cancel := localCtx()
	defer cancel()
	return exec.CommandContext(ctx, "gh", "--version").Run() == nil
}

func ghAuthenticated() bool {
	ctx, cancel := netCtx()
	defer cancel()
	return exec.CommandContext(ctx, "gh", "auth", "status").Run() == nil
}

type ghRepoView struct {
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

func ghRepoInfo() (owner, repo string, err error) {
	ctx, cancel := netCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "owner,name").Output()
	if err != nil {
		return "", "", fmt.Errorf("gh repo view: %w", err)
	}
	var rv ghRepoView
	if err := json.Unmarshal(out, &rv); err != nil {
		return "", "", fmt.Errorf("parsing gh repo view: %w", err)
	}
	return rv.Owner.Login, rv.Name, nil
}

func requireGH() error {
	if !ghAvailable() {
		return fmt.Errorf(
			"gh CLI not found\n\n" +
				"  Install it: https://cli.github.com\n" +
				"  Or via package manager:\n" +
				"    winget install GitHub.cli\n" +
				"    brew install gh\n" +
				"    sudo apt install gh",
		)
	}
	if !ghAuthenticated() {
		return fmt.Errorf(
			"gh CLI not authenticated\n\n" +
				"  Run: gh auth login",
		)
	}
	return nil
}

func ghBranchProtection(owner, repo, branch string) (requiresPR bool, err error) {
	ctx, cancel := netCtx()
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "api",
		fmt.Sprintf("repos/%s/%s/branches/%s/protection", owner, repo, branch),
		"--jq", ".required_pull_request_reviews",
	).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "404") || strings.Contains(string(out), "Not Found") {
			return false, nil
		}
		return false, fmt.Errorf("gh api: %s", strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	return trimmed != "" && trimmed != "null", nil
}
