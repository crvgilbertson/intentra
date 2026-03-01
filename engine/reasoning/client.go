package reasoning

import (
	"context"
	"encoding/json"
)

// Message represents a single chat message for LLM interactions.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// ReasoningEngine abstracts structured LLM calls for testability.
// Implementations exist for OpenAI and Anthropic; any OpenAI-compatible
// endpoint (Ollama, vLLM, LM Studio, Azure OpenAI) can use the OpenAI
// engine with a custom base URL.
type ReasoningEngine interface {
	CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, messages []Message) (json.RawMessage, error)
}
