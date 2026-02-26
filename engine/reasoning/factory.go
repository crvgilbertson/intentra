package reasoning

import (
	"fmt"

	"github.com/crvgilbertson/intentra/config"
)

// NewEngineFromConfig creates the appropriate ReasoningEngine based on
// the provider setting in the config.
//
// Supported providers:
//   - "openai"    — OpenAI API (default). Set base_url for Azure or compatible endpoints.
//   - "anthropic" — Anthropic Claude API. Set base_url for custom endpoints.
//   - "gemini"    — Google Gemini via its OpenAI-compatible endpoint. Uses GEMINI_API_KEY.
//   - "ollama"    — Shorthand for OpenAI-compatible with base URL http://localhost:11434/v1.
func NewEngineFromConfig(cfg config.AIConfig) (ReasoningEngine, error) {
	switch cfg.Provider {
	case "openai", "":
		if cfg.BaseURL != "" {
			return NewOpenAIEngineWithBaseURL(cfg.Model, cfg.Temperature, cfg.BaseURL), nil
		}
		return NewOpenAIEngine(cfg.Model, cfg.Temperature), nil

	case "anthropic":
		if cfg.BaseURL != "" {
			return NewAnthropicEngineWithBaseURL(cfg.Model, cfg.Temperature, cfg.BaseURL), nil
		}
		return NewAnthropicEngine(cfg.Model, cfg.Temperature), nil

	case "gemini":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "https://generativelanguage.googleapis.com/v1beta/openai/"
		}
		return NewOpenAIEngineWithAPIKey(cfg.Model, cfg.Temperature, baseURL, "GEMINI_API_KEY"), nil

	case "ollama":
		baseURL := cfg.BaseURL
		if baseURL == "" {
			baseURL = "http://localhost:11434/v1"
		}
		return NewOpenAIEngineWithBaseURL(cfg.Model, cfg.Temperature, baseURL), nil

	default:
		return nil, fmt.Errorf("unknown AI provider %q (supported: openai, anthropic, gemini, ollama)", cfg.Provider)
	}
}
