package reasoning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/crvgilbertson/intentra/engine"
)

// CallWithRetry calls the engine, unmarshals into T, and validates.
// On failure it retries up to maxRetries times with a correction prompt.
func CallWithRetry[T any](
	ctx context.Context,
	eng ReasoningEngine,
	schemaName string,
	schema interface{},
	systemPrompt string,
	userInput string,
	validate func(T) error,
	maxRetries int,
) (T, error) {
	var zero T
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		input := userInput
		if attempt > 0 && lastErr != nil {
			input += fmt.Sprintf("\n\n[CORRECTION]: Your previous response failed: %v. Please fix the issues and respond again.", lastErr)
		}

		raw, err := eng.CallStructured(ctx, schemaName, schema, systemPrompt, input)
		if err != nil {
			return zero, engine.NewReasoningError(
				fmt.Sprintf("call failed (attempt %d/%d)", attempt+1, maxRetries+1), err)
		}

		var result T
		if err := json.Unmarshal(raw, &result); err != nil {
			lastErr = fmt.Errorf("invalid JSON: %w", err)
			continue
		}

		if err := validate(result); err != nil {
			lastErr = fmt.Errorf("validation: %w", err)
			continue
		}

		return result, nil
	}

	return zero, engine.NewReasoningError(
		fmt.Sprintf("all %d attempt(s) failed", maxRetries+1), lastErr)
}
