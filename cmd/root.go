package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"intentra/config"
)

var (
	cfgPath string
	cfg     config.EngineConfig
)

var rootCmd = &cobra.Command{
	Use:   "intentra",
	Short: "AI-powered code change reasoning engine",
	Long:  "Intentra is a deterministic, extensible workflow engine that understands code changes and produces structured, machine-actionable plans.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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
