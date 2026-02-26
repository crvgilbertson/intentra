package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/config"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize .intentra/ directory with default config",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(config.DefaultPath); err == nil {
		return fmt.Errorf("%s already exists; remove it first to reinitialize", config.DefaultPath)
	}

	if err := config.WriteDefault(config.DefaultPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	if err := config.WriteGitignore(); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	fmt.Printf("Created %s/ with default configuration.\n", config.Dir)
	fmt.Printf("  %s  — project config (commit to repo)\n", config.DefaultPath)
	fmt.Printf("  %s/.gitignore — ignores ephemeral files\n", config.Dir)
	return nil
}
