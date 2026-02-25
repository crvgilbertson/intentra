package reasoning

import (
	"context"
	"encoding/json"
)

// ReasoningEngine abstracts structured LLM calls for testability.
// Implementations exist for OpenAI and Anthropic; any OpenAI-compatible
// endpoint (Ollama, vLLM, LM Studio, Azure OpenAI) can use the OpenAI
// engine with a custom base URL.
type ReasoningEngine interface {
	CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, userInput string) (json.RawMessage, error)
}
