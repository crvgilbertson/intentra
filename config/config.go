package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/crvgilbertson/intentra/engine/models"
)

type AIConfig struct {
	Provider    string  `yaml:"provider"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxDiffKB   int     `yaml:"max_diff_kb"`
	BaseURL     string  `yaml:"base_url,omitempty"`
}

type EngineSettings struct {
	StrictMode bool `yaml:"strict_mode"`
}

type EngineConfig struct {
	Style  models.CommitStyle `yaml:"style"`
	AI     AIConfig           `yaml:"ai"`
	Engine EngineSettings     `yaml:"engine"`
}

func DefaultConfig() EngineConfig {
	return EngineConfig{
		Style: models.CommitStyle{
			Convention:    "conventional_commits",
			MaxSubjectLen: 72,
			AllowedTypes:  []string{"feat", "fix", "refactor", "perf", "docs", "test", "chore"},
			Scopes:        []string{},
		},
		AI: AIConfig{
			Provider:    "openai",
			Model:       "gpt-4.1",
			Temperature: 0.2,
			MaxDiffKB:   500,
		},
		Engine: EngineSettings{
			StrictMode: true,
		},
	}
}

func Load(path string) (EngineConfig, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("reading config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return cfg, nil
}

func WriteDefault(path string) error {
	cfg := DefaultConfig()
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshalling default config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
