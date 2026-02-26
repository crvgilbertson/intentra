package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/config"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a default .engine.yaml config file",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	const path = ".engine.yaml"

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; remove it first to reinitialize", path)
	}

	if err := config.WriteDefault(path); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	fmt.Printf("Created %s with default configuration.\n", path)
	return nil
}
