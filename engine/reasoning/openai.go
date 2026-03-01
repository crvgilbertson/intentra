package reasoning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// OpenAIEngine implements ReasoningEngine using the official openai-go SDK.
// It supports any OpenAI-compatible endpoint via the baseURL parameter,
// including Ollama (http://localhost:11434/v1), vLLM, LM Studio, and
// Azure OpenAI.
type OpenAIEngine struct {
	client      *openai.Client
	model       openai.ChatModel
	temperature float64
}

func NewOpenAIEngine(model string, temperature float64) *OpenAIEngine {
	client := openai.NewClient()
	return &OpenAIEngine{
		client:      &client,
		model:       openai.ChatModel(model),
		temperature: temperature,
	}
}

func NewOpenAIEngineWithBaseURL(model string, temperature float64, baseURL string) *OpenAIEngine {
	client := openai.NewClient(option.WithBaseURL(baseURL))
	return &OpenAIEngine{
		client:      &client,
		model:       openai.ChatModel(model),
		temperature: temperature,
	}
}

// NewOpenAIEngineWithAPIKey creates an OpenAI-compatible engine that reads its
// API key from a specific environment variable (e.g. "GEMINI_API_KEY") instead
// of the default OPENAI_API_KEY.
func NewOpenAIEngineWithAPIKey(model string, temperature float64, baseURL string, apiKeyEnv string) *OpenAIEngine {
	apiKey := os.Getenv(apiKeyEnv)
	client := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
	)
	return &OpenAIEngine{
		client:      &client,
		model:       openai.ChatModel(model),
		temperature: temperature,
	}
}

func (e *OpenAIEngine) CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, messages []Message) (json.RawMessage, error) {
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:   schemaName,
		Schema: schema,
		Strict: openai.Bool(true),
	}

	oaiMessages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}
	for _, m := range messages {
		if m.Role == "user" {
			oaiMessages = append(oaiMessages, openai.UserMessage(m.Content))
		} else if m.Role == "assistant" {
			oaiMessages = append(oaiMessages, openai.AssistantMessage(m.Content))
		}
	}

	chat, err := e.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    e.model,
		Messages: oaiMessages,
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{JSONSchema: schemaParam},
		},
		Temperature: openai.Float(e.temperature),
	})
	if err != nil {
		return nil, fmt.Errorf("openai structured call: %w", err)
	}

	if len(chat.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	raw := json.RawMessage(chat.Choices[0].Message.Content)
	if !json.Valid(raw) {
		return nil, fmt.Errorf("openai returned invalid JSON")
	}

	return raw, nil
}
