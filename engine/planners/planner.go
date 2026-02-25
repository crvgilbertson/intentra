package planners

import (
	"context"

	enginectx "intentra/engine/context"
	"intentra/engine/models"
)

// Planner produces a Plan from repository context.
type Planner interface {
	Name() string
	BuildPlan(ctx context.Context, ec enginectx.EngineContext) (models.Plan, error)
}
