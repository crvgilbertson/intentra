package cmd

import (
	"regexp"
	"strings"

	"github.com/crvgilbertson/intentra/engine/artifacts"
	"github.com/crvgilbertson/intentra/engine/models"
)

var ticketPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

func resolveTicketRef(explicit string, cp *models.CommitPlan) *artifacts.TicketRef {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return &artifacts.TicketRef{ID: explicit, Source: "flag"}
	}
	if cp != nil {
		if ticket := ticketFromPlan(cp); ticket != "" {
			return &artifacts.TicketRef{ID: ticket, Source: "plan"}
		}
	}
	branch, err := currentBranch()
	if err != nil {
		return nil
	}
	if ticket := detectTicketFromBranch(branch); ticket != "" {
		return &artifacts.TicketRef{ID: ticket, Source: "branch"}
	}
	return nil
}

func detectTicketFromBranch(branch string) string {
	return ticketPattern.FindString(branch)
}

func ticketFromPlan(cp *models.CommitPlan) string {
	for _, c := range cp.Commits {
		for _, f := range c.Footers {
			if ticket := ticketPattern.FindString(f.Value); ticket != "" {
				return ticket
			}
		}
	}
	return ""
}

func addTicketFooter(cp *models.CommitPlan, ticket string) bool {
	ticket = strings.TrimSpace(ticket)
	if cp == nil || ticket == "" {
		return false
	}

	changed := false
	for i := range cp.Commits {
		if commitHasTicket(cp.Commits[i], ticket) {
			continue
		}
		cp.Commits[i].Footers = append(cp.Commits[i].Footers, models.Footer{
			Token: "Refs",
			Value: ticket,
		})
		changed = true
	}
	return changed
}

func commitHasTicket(c models.CommitUnit, ticket string) bool {
	for _, f := range c.Footers {
		if strings.EqualFold(f.Token, "Refs") || strings.EqualFold(f.Token, "Ticket") {
			if strings.Contains(f.Value, ticket) {
				return true
			}
		}
	}
	return false
}
