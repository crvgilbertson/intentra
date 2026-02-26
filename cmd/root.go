package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/cmd/ui"
	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/internal"
)

var (
	cfgPath string
	cfg     config.EngineConfig
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:     "intentra",
	Short:   "AI-powered code change reasoning engine",
	Long:    "Intentra is a deterministic, extensible workflow engine that understands code changes and produces structured, machine-actionable plans.",
	Version: internal.Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" || cmd.Name() == "init" {
			return nil
		}

		// Legacy config fallback: if the new path doesn't exist, check the old one.
		if cfgPath == config.DefaultPath {
			if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
				if _, err := os.Stat(config.LegacyPath); err == nil {
					cfgPath = config.LegacyPath
					ui.Warn("Using legacy %s. Run 'intentra init' to migrate to %s\n", config.LegacyPath, config.DefaultPath)
				}
			}
		}

		var err error
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		ui.VerboseMode = verbose
		return nil
	},
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", config.DefaultPath, "path to config file")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose debug output")
}

func Execute() error {
	return rootCmd.Execute()
}
