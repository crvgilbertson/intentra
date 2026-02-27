package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/planners"
	"github.com/crvgilbertson/intentra/internal"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Print a diagnostic report for bug reports and debugging",
	Long:  "Outputs a sanitized bundle of configuration, diff metadata, cache status, and trust surface information. Safe to share — no code content or secrets included.",
	RunE:  runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(doctorCmd)
}

type diagnosticReport struct {
	Version           string        `json:"version"`
	SchemaVersion     string        `json:"schema_version"`
	PromptFingerprint string        `json:"prompt_fingerprint"`
	ConfigPath    string            `json:"config_path"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	BaseURL       string            `json:"base_url,omitempty"`
	Temperature   float64           `json:"temperature"`
	MaxDiffKB     int               `json:"max_diff_kb"`
	MaxHunkLines  int               `json:"max_hunk_lines"`
	Timeout       int               `json:"timeout_seconds"`
	MaxRetries    int               `json:"max_retries"`
	StrictMode    bool              `json:"strict_mode"`
	MaxCommits    int               `json:"max_commits"`
	BatchThreshold int              `json:"batch_threshold"`
	Protected     []string          `json:"protected_branches"`
	SignCommits   bool              `json:"sign_commits"`
	SkipHooks     bool              `json:"skip_hooks"`
	AutoPush      bool              `json:"auto_push"`
	IgnorePatterns []string         `json:"ignore_patterns"`
	Style         models.CommitStyle `json:"style"`
	APIKeyStatus  string            `json:"api_key_status"`
	Diff          *diffInfo         `json:"diff,omitempty"`
	Cache         *cacheInfo        `json:"cache,omitempty"`
	TrustSurface  trustInfo         `json:"trust_surface"`
}

type diffInfo struct {
	HunkCount   int               `json:"hunk_count"`
	FileCount   int               `json:"file_count"`
	SizeKB      float64           `json:"size_kb"`
	Fingerprint string            `json:"fingerprint"`
	Files       map[string]int    `json:"files"`
}

type cacheInfo struct {
	Path        string `json:"path"`
	Schema      string `json:"schema_version"`
	CommitCount int    `json:"commit_count"`
	Fingerprint string `json:"fingerprint"`
	Status      string `json:"status"`
}

type trustInfo struct {
	Sent    []string `json:"sent_to_provider"`
	NotSent []string `json:"not_sent"`
	Caching []string `json:"caching_rules"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	report := diagnosticReport{
		Version:           internal.Version,
		SchemaVersion:     models.CurrentSchemaVersion,
		PromptFingerprint: planners.PromptFingerprint(),
		ConfigPath:     cfgPath,
		Provider:       cfg.AI.Provider,
		Model:          cfg.AI.Model,
		BaseURL:        cfg.AI.BaseURL,
		Temperature:    cfg.AI.Temperature,
		MaxDiffKB:      cfg.AI.MaxDiffKB,
		MaxHunkLines:   cfg.AI.MaxHunkLines,
		Timeout:        cfg.AI.Timeout,
		MaxRetries:     cfg.AI.MaxRetries,
		StrictMode:     cfg.Engine.StrictMode,
		MaxCommits:     cfg.Engine.MaxCommits,
		BatchThreshold: cfg.Engine.BatchThreshold,
		Protected:      cfg.Engine.ProtectedBranches,
		SignCommits:    cfg.Engine.SignCommits,
		SkipHooks:      cfg.Engine.SkipHooks,
		AutoPush:       cfg.Engine.AutoPush,
		IgnorePatterns: cfg.Engine.IgnorePatterns,
		Style:          cfg.Style,
	}

	report.APIKeyStatus = resolveAPIKeyStatus(cfg.AI.Provider)
	report.TrustSurface = buildTrustSurface(cfg.AI.Provider, cfg.AI.MaxHunkLines)

	ec, diffErr := enginectx.BuildContext(ctx, cfg)
	if diffErr == nil && len(ec.Hunks) > 0 {
		report.Diff = buildDiffInfo(ec)
	}

	cached, cacheErr := loadCachedPlan()
	if cacheErr == nil {
		ci := &cacheInfo{
			Path:        defaultPlanFile,
			Schema:      cached.SchemaVersion,
			CommitCount: len(cached.Commits),
			Fingerprint: cached.DiffFingerprint,
		}
		if report.Diff != nil && cached.DiffFingerprint == report.Diff.Fingerprint {
			ci.Status = "fresh"
		} else if report.Diff != nil {
			ci.Status = "stale"
		} else {
			ci.Status = "unknown"
		}
		report.Cache = ci
	}

	if doctorJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	printDiagnosticText(report, diffErr)
	return nil
}

func resolveAPIKeyStatus(provider string) string {
	envVars := map[string]string{
		"openai":    "OPENAI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"gemini":    "GEMINI_API_KEY",
	}
	envVar, ok := envVars[provider]
	if !ok {
		if provider == "ollama" {
			return "not required (ollama)"
		}
		return "unknown provider"
	}
	val := os.Getenv(envVar)
	if val == "" {
		return envVar + " not set"
	}
	return envVar + " set (" + maskKey(val) + ")"
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func buildDiffInfo(ec enginectx.EngineContext) *diffInfo {
	fileHunks := make(map[string]int)
	var totalBytes int
	for _, h := range ec.Hunks {
		fileHunks[h.FilePath]++
		totalBytes += len(h.Patch)
	}
	return &diffInfo{
		HunkCount:   len(ec.Hunks),
		FileCount:   len(fileHunks),
		SizeKB:      float64(totalBytes) / 1024,
		Fingerprint: models.DiffFingerprintFromHunks(ec.Hunks),
		Files:       fileHunks,
	}
}

func buildTrustSurface(provider string, maxHunkLines int) trustInfo {
	patchDesc := "Patch content (full, no truncation)"
	if maxHunkLines > 0 {
		patchDesc = fmt.Sprintf("Patch content (truncated to %d lines per hunk)", maxHunkLines)
	}
	return trustInfo{
		Sent: []string{
			"File paths (full)",
			"Diff headers (@@ lines)",
			patchDesc,
			"Recent commit messages (10 most recent)",
			"Allowed types, scopes, and style rules",
		},
		NotSent: []string{
			"Full file contents (only unified diffs)",
			"API keys or credentials",
			"Environment variables or system info",
			"Git history beyond recent subjects",
		},
		Caching: []string{
			"Plan cached to .intentra/plan.json after generation",
			"Cache keyed by SHA256 of sorted hunk IDs (diff fingerprint)",
			"Cache reused when fingerprint matches; discarded when stale",
			"Cache deleted after successful apply",
		},
	}
}

func printDiagnosticText(r diagnosticReport, diffErr error) {
	fmt.Printf("Intentra v%s (schema %s, prompts %s)\n", r.Version, r.SchemaVersion, r.PromptFingerprint)
	fmt.Println()

	section("Config")
	kv("Path", r.ConfigPath)
	kv("Provider", r.Provider)
	kv("Model", r.Model)
	if r.BaseURL != "" {
		kv("Base URL", r.BaseURL)
	}
	kv("Temperature", fmt.Sprintf("%.1f", r.Temperature))
	kv("Max Diff KB", fmt.Sprintf("%d", r.MaxDiffKB))
	kv("Max Hunk Lines", fmt.Sprintf("%d", r.MaxHunkLines))
	kv("Timeout", fmt.Sprintf("%ds", r.Timeout))
	kv("Max Retries", fmt.Sprintf("%d", r.MaxRetries))
	kv("Strict Mode", fmt.Sprintf("%v", r.StrictMode))
	kv("Max Commits", fmt.Sprintf("%d", r.MaxCommits))
	kv("Batch Threshold", fmt.Sprintf("%d", r.BatchThreshold))
	kv("Protected", strings.Join(r.Protected, ", "))
	kv("Sign Commits", fmt.Sprintf("%v", r.SignCommits))
	kv("Skip Hooks", fmt.Sprintf("%v", r.SkipHooks))
	kv("Auto Push", fmt.Sprintf("%v", r.AutoPush))
	if len(r.IgnorePatterns) > 0 {
		kv("Ignore", strings.Join(r.IgnorePatterns, ", "))
	} else {
		kv("Ignore", "(none)")
	}
	fmt.Println()

	section("Style")
	kv("Convention", r.Style.Convention)
	kv("Max Subject", fmt.Sprintf("%d", r.Style.MaxSubjectLen))
	kv("Types", strings.Join(r.Style.AllowedTypes, ", "))
	if len(r.Style.Scopes) > 0 {
		kv("Scopes", strings.Join(r.Style.Scopes, ", "))
	} else {
		kv("Scopes", "(any)")
	}
	kv("Scope Required", fmt.Sprintf("%v", r.Style.ScopeRequired))
	kv("Body Required", fmt.Sprintf("%v", r.Style.BodyRequired))
	fmt.Println()

	section("API Key")
	fmt.Printf("  %s\n\n", r.APIKeyStatus)

	section("Diff")
	if diffErr != nil {
		fmt.Printf("  error: %v\n\n", diffErr)
	} else if r.Diff == nil {
		fmt.Printf("  (clean working tree)\n\n")
	} else {
		kv("Hunks", fmt.Sprintf("%d", r.Diff.HunkCount))
		kv("Files", fmt.Sprintf("%d", r.Diff.FileCount))
		kv("Size", fmt.Sprintf("%.1f KB", r.Diff.SizeKB))
		kv("Fingerprint", r.Diff.Fingerprint)
		fmt.Println()
		files := make([]string, 0, len(r.Diff.Files))
		for f := range r.Diff.Files {
			files = append(files, f)
		}
		sort.Strings(files)
		for _, f := range files {
			fmt.Printf("    %s (%d hunk(s))\n", f, r.Diff.Files[f])
		}
		fmt.Println()
	}

	section("Cache")
	if r.Cache == nil {
		fmt.Printf("  (no plan cached)\n\n")
	} else {
		kv("File", r.Cache.Path)
		kv("Schema", r.Cache.Schema)
		kv("Commits", fmt.Sprintf("%d", r.Cache.CommitCount))
		kv("Fingerprint", r.Cache.Fingerprint)
		kv("Status", r.Cache.Status)
		fmt.Println()
	}

	section("Trust Surface")
	fmt.Printf("  Sent to %s:\n", r.Provider)
	for _, s := range r.TrustSurface.Sent {
		fmt.Printf("    + %s\n", s)
	}
	fmt.Printf("  NOT sent:\n")
	for _, s := range r.TrustSurface.NotSent {
		fmt.Printf("    - %s\n", s)
	}
	fmt.Println()

	section("Caching Rules")
	for _, s := range r.TrustSurface.Caching {
		fmt.Printf("    %s\n", s)
	}
	fmt.Println()
}

func section(name string) {
	fmt.Printf("[%s]\n", name)
}

func kv(key, value string) {
	fmt.Printf("  %-18s %s\n", key+":", value)
}
