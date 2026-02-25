package reasoning

import (
	"context"
	"encoding/json"
	"fmt"
)

// CallWithRetry calls the engine, unmarshals the result into target, and runs
// validate. If either unmarshal or validate fails, it retries once with a
// correction prompt. If the retry also fails, it returns the error.
func CallWithRetry[T any](
	ctx context.Context,
	engine ReasoningEngine,
	schemaName string,
	schema interface{},
	systemPrompt string,
	userInput string,
	validate func(T) error,
) (T, error) {
	var zero T

	raw, err := engine.CallStructured(ctx, schemaName, schema, systemPrompt, userInput)
	if err != nil {
		return zero, fmt.Errorf("reasoning call: %w", err)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		retried, retryErr := retryOnce(ctx, engine, schemaName, schema, systemPrompt, userInput,
			fmt.Sprintf("Your previous response was invalid JSON: %v. Please fix and respond with valid JSON only.", err),
			validate,
		)
		return retried, retryErr
	}

	if err := validate(result); err != nil {
		retried, retryErr := retryOnce(ctx, engine, schemaName, schema, systemPrompt, userInput,
			fmt.Sprintf("Your previous response failed validation: %v. Please fix the issues and respond again.", err),
			validate,
		)
		return retried, retryErr
	}

	return result, nil
}

func retryOnce[T any](
	ctx context.Context,
	engine ReasoningEngine,
	schemaName string,
	schema interface{},
	systemPrompt string,
	userInput string,
	correction string,
	validate func(T) error,
) (T, error) {
	var zero T

	correctedInput := userInput + "\n\n[CORRECTION]: " + correction
	raw, err := engine.CallStructured(ctx, schemaName, schema, systemPrompt, correctedInput)
	if err != nil {
		return zero, fmt.Errorf("reasoning retry call: %w", err)
	}

	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("retry unmarshal failed: %w", err)
	}

	if err := validate(result); err != nil {
		return zero, fmt.Errorf("retry validation failed: %w", err)
	}

	return result, nil
}
