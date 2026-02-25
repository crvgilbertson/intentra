package executors

import (
	"context"

	"intentra/engine/models"
)

// Executor applies a validated Plan to the repository.
type Executor interface {
	Execute(ctx context.Context, plan models.Plan, dryRun bool) error
}
