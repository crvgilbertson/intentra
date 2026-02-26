package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/crvgilbertson/intentra/engine/models"
)

var (
	Dir        = ".intentra"
	ConfigFile = "config.yaml"
	PlanFile   = "plan.json"

	DefaultPath = filepath.Join(Dir, ConfigFile)
	PlanPath    = filepath.Join(Dir, PlanFile)
	LegacyPath  = ".engine.yaml"
)

type AIConfig struct {
	Provider    string  `yaml:"provider"`
	Model       string  `yaml:"model"`
	Temperature float64 `yaml:"temperature"`
	MaxDiffKB   int     `yaml:"max_diff_kb"`
	BaseURL     string  `yaml:"base_url,omitempty"`
	MaxRetries  int     `yaml:"max_retries"`
	Timeout     int     `yaml:"timeout"`
}

type EngineSettings struct {
	StrictMode        bool     `yaml:"strict_mode"`
	ProtectedBranches []string `yaml:"protected_branches"`
	MaxCommits        int      `yaml:"max_commits"`
	IgnorePatterns    []string `yaml:"ignore_patterns"`
	SignCommits       bool     `yaml:"sign_commits"`
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
			ScopeRequired: false,
			BodyRequired:  false,
		},
		AI: AIConfig{
			Provider:    "openai",
			Model:       "gpt-4.1",
			Temperature: 0.2,
			MaxDiffKB:   500,
			MaxRetries:  1,
			Timeout:     120,
		},
		Engine: EngineSettings{
			StrictMode:        true,
			ProtectedBranches: []string{"main", "master"},
			MaxCommits:        20,
			IgnorePatterns:    []string{},
			SignCommits:       false,
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

// EnsureDir creates the .intentra directory if it doesn't exist.
func EnsureDir() error {
	return os.MkdirAll(Dir, 0755)
}

func WriteDefault(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	cfg := DefaultConfig()
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshalling default config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// WriteGitignore writes a .gitignore inside the .intentra directory
// that ignores ephemeral files but allows config to be committed.
func WriteGitignore() error {
	path := filepath.Join(Dir, ".gitignore")
	content := "# Ephemeral files — do not commit\nplan.json\n"
	return os.WriteFile(path, []byte(content), 0644)
}
