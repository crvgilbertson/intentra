package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/internal"
)

var (
	cfgPath string
	cfg     config.EngineConfig
)

var rootCmd = &cobra.Command{
	Use:     "intentra",
	Short:   "AI-powered code change reasoning engine",
	Long:    "Intentra is a deterministic, extensible workflow engine that understands code changes and produces structured, machine-actionable plans.",
	Version: internal.Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" {
			return nil
		}
		var err error
		cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		return nil
	},
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", ".engine.yaml", "path to config file")
}

func Execute() error {
	return rootCmd.Execute()
}
