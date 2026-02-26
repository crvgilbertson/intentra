package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/cmd/ui"
)

var pushRemoteFlag string

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the current branch to the remote",
	Long:  "Pushes the current branch, automatically setting the upstream if needed. Uses the configured remote_name or --remote override.",
	RunE:  runPush,
}

func init() {
	pushCmd.Flags().StringVar(&pushRemoteFlag, "remote", "", "override remote name (default: config remote_name)")
	rootCmd.AddCommand(pushCmd)
}

func runPush(cmd *cobra.Command, args []string) error {
	remote := pushRemoteFlag
	if remote == "" {
		remote = cfg.Engine.RemoteName
	}
	if remote == "" {
		remote = "origin"
	}

	branch, err := currentBranch()
	if err != nil {
		return fmt.Errorf("could not determine current branch: %w", err)
	}
	if branch == "HEAD" {
		return fmt.Errorf("detached HEAD state — checkout a branch first")
	}

	ui.Info("Branch: %s  Remote: %s\n", branch, remote)

	smartPush(remote, branch)
	return nil
}
