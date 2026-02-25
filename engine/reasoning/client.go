package reasoning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3"
)

// ReasoningEngine abstracts structured LLM calls for testability.
type ReasoningEngine interface {
	CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, userInput string) (json.RawMessage, error)
}

// OpenAIEngine implements ReasoningEngine using the official openai-go SDK.
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

func (e *OpenAIEngine) CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, userInput string) (json.RawMessage, error) {
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:   schemaName,
		Schema: schema,
		Strict: openai.Bool(true),
	}

	chat, err := e.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: e.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userInput),
		},
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
