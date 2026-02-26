package executors

import (
	"context"

	"github.com/crvgilbertson/intentra/engine/models"
)

// Executor applies a validated Plan to the repository.
type Executor interface {
	Execute(ctx context.Context, plan models.Plan, dryRun bool) error
}
