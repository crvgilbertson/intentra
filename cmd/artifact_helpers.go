package cmd

import (
	"fmt"
	"strings"

	"github.com/crvgilbertson/intentra/engine/artifacts"
	"github.com/crvgilbertson/intentra/engine/models"
)

func artifactOptions(explicitTicket, version, since string, cp *models.CommitPlan) artifacts.Options {
	return artifacts.Options{
		Ticket:  resolveTicketRef(explicitTicket, cp),
		Version: strings.TrimSpace(version),
		Since:   strings.TrimSpace(since),
		RiskThresholds: artifacts.RiskThresholds{
			Medium: cfg.Engine.Risk.MediumThreshold(),
			High:   cfg.Engine.Risk.HighThreshold(),
		},
	}
}

func loadPlanForArtifacts() (*models.CommitPlan, error) {
	cached, err := loadCachedPlan()
	if err != nil {
		return nil, fmt.Errorf("no cached plan found — run 'intentra plan' first: %w", err)
	}
	return cached, nil
}
