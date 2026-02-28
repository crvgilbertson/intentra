package cmd

import (
	"encoding/json"
	"testing"

	"github.com/crvgilbertson/intentra/engine/models"
)

func TestDoctorReport_JSONStability(t *testing.T) {
	// Ensure atomicity_profile and effective_max_commits are always present in JSON.
	// Risk thresholds use omitempty and must be present only when risk is enabled.
	report := diagnosticReport{
		Version:             "0.5.0",
		SchemaVersion:       models.CurrentSchemaVersion,
		PromptFingerprint:   "fp",
		ConfigPath:          "",
		Provider:            "openai",
		Model:               "gpt-4",
		Temperature:         0.2,
		MaxDiffKB:           0,
		MaxHunkLines:        0,
		Timeout:             0,
		MaxRetries:          0,
		StrictMode:          false,
		MaxCommits:          20,
		AtomicityProfile:    "balanced",
		EffectiveMaxCommits:  20,
		BatchThreshold:      0,
		RiskEnabled:         false,
		Protected:           nil,
		SignCommits:         false,
		SkipHooks:           false,
		AutoPush:            false,
		IgnorePatterns:      nil,
		Style:               models.CommitStyle{},
		APIKeyStatus:        "unset",
		TrustSurface: trustInfo{
			Sent:    nil,
			NotSent: nil,
			Caching: nil,
		},
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := out["atomicity_profile"]; !ok {
		t.Error("JSON must include atomicity_profile")
	}
	if _, ok := out["effective_max_commits"]; !ok {
		t.Error("JSON must include effective_max_commits")
	}
	if v, _ := out["atomicity_profile"].(string); v != "balanced" {
		t.Errorf("atomicity_profile want balanced, got %q", v)
	}
	if v, ok := out["effective_max_commits"].(float64); !ok || int(v) != 20 {
		t.Errorf("effective_max_commits want 20, got %v", out["effective_max_commits"])
	}

	// Risk thresholds should be omitted when risk is disabled (omitempty for zero values).
	if _, ok := out["risk_threshold_medium"]; ok {
		t.Error("risk_threshold_medium should be omitted when risk disabled")
	}
	if _, ok := out["risk_threshold_high"]; ok {
		t.Error("risk_threshold_high should be omitted when risk disabled")
	}
}

func TestDoctorReport_JSONRiskThresholdsPresentWhenEnabled(t *testing.T) {
	report := diagnosticReport{
		Version:                "0.5.0",
		SchemaVersion:          models.CurrentSchemaVersion,
		PromptFingerprint:      "fp",
		ConfigPath:             "",
		Provider:               "openai",
		Model:                  "gpt-4",
		Temperature:            0.2,
		MaxDiffKB:              0,
		MaxHunkLines:           0,
		Timeout:                0,
		MaxRetries:             0,
		StrictMode:             false,
		MaxCommits:             20,
		AtomicityProfile:       "strict",
		EffectiveMaxCommits:    40,
		BatchThreshold:        0,
		RiskEnabled:            true,
		RiskThresholdMedium:    25.0,
		RiskThresholdHigh:      75.0,
		Protected:              nil,
		SignCommits:            false,
		SkipHooks:              false,
		AutoPush:               false,
		IgnorePatterns:         nil,
		Style:                  models.CommitStyle{},
		APIKeyStatus:           "unset",
		TrustSurface:           trustInfo{},
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if _, ok := out["atomicity_profile"]; !ok {
		t.Error("JSON must include atomicity_profile")
	}
	if _, ok := out["effective_max_commits"]; !ok {
		t.Error("JSON must include effective_max_commits")
	}
	if v, _ := out["atomicity_profile"].(string); v != "strict" {
		t.Errorf("atomicity_profile want strict, got %q", v)
	}
	if v, ok := out["effective_max_commits"].(float64); !ok || int(v) != 40 {
		t.Errorf("effective_max_commits want 40, got %v", out["effective_max_commits"])
	}
	if _, ok := out["risk_threshold_medium"]; !ok {
		t.Error("risk_threshold_medium should be present when risk enabled and set")
	}
	if _, ok := out["risk_threshold_high"]; !ok {
		t.Error("risk_threshold_high should be present when risk enabled and set")
	}
}
