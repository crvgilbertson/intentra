package planners

import (
	"context"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
)

// Planner produces a Plan from repository context.
type Planner interface {
	Name() string
	BuildPlan(ctx context.Context, ec enginectx.EngineContext) (models.Plan, error)
}
