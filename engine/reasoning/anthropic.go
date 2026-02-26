package reasoning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/invopop/jsonschema"
)

// AnthropicEngine implements ReasoningEngine using the official Anthropic SDK.
// It uses tool_use with forced tool choice to achieve structured output,
// which is the recommended approach for Claude models.
type AnthropicEngine struct {
	client      *anthropic.Client
	model       anthropic.Model
	temperature float64
	maxTokens   int64
}

func NewAnthropicEngine(model string, temperature float64) *AnthropicEngine {
	client := anthropic.NewClient()
	return &AnthropicEngine{
		client:      &client,
		model:       anthropic.Model(model),
		temperature: temperature,
		maxTokens:   4096,
	}
}

func NewAnthropicEngineWithBaseURL(model string, temperature float64, baseURL string) *AnthropicEngine {
	client := anthropic.NewClient(anthropicopt.WithBaseURL(baseURL))
	return &AnthropicEngine{
		client:      &client,
		model:       anthropic.Model(model),
		temperature: temperature,
		maxTokens:   4096,
	}
}

func (e *AnthropicEngine) CallStructured(ctx context.Context, schemaName string, schema interface{}, systemPrompt string, userInput string) (json.RawMessage, error) {
	inputSchema := convertToToolInputSchema(schema)

	tool := anthropic.ToolParam{
		Name:        schemaName,
		Description: anthropic.String("Respond with structured data matching this schema."),
		InputSchema: inputSchema,
	}

	message, err := e.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     e.model,
		MaxTokens: e.maxTokens,
		System: []anthropic.TextBlockParam{
			{Text: systemPrompt},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userInput)),
		},
		Tools: []anthropic.ToolUnionParam{
			{OfTool: &tool},
		},
		ToolChoice: anthropic.ToolChoiceParamOfTool(schemaName),
		Temperature: anthropic.Float(e.temperature),
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic structured call: %w", err)
	}

	for _, block := range message.Content {
		toolUse, ok := block.AsAny().(anthropic.ToolUseBlock)
		if !ok {
			continue
		}
		if toolUse.Name != schemaName {
			continue
		}

		raw, err := json.Marshal(toolUse.Input)
		if err != nil {
			return nil, fmt.Errorf("marshalling tool_use input: %w", err)
		}

		if !json.Valid(raw) {
			return nil, fmt.Errorf("anthropic returned invalid JSON in tool_use")
		}

		return raw, nil
	}

	return nil, fmt.Errorf("anthropic response contained no tool_use block for %q", schemaName)
}

// convertToToolInputSchema converts a jsonschema-generated schema (used by
// OpenAI) into the Anthropic ToolInputSchemaParam format.
func convertToToolInputSchema(schema interface{}) anthropic.ToolInputSchemaParam {
	data, err := json.Marshal(schema)
	if err != nil {
		return anthropic.ToolInputSchemaParam{}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return anthropic.ToolInputSchemaParam{}
	}

	var properties interface{}
	if p, ok := raw["properties"]; ok {
		properties = p
	}

	return anthropic.ToolInputSchemaParam{
		Properties: properties,
	}
}

// GenerateAnthropicSchema generates a JSON schema from a Go type using
// the same reflector settings as the OpenAI schemas.
func GenerateAnthropicSchema[T any]() *jsonschema.Schema {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T
	return reflector.Reflect(v)
}
