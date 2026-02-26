package planners

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	enginectx "intentra/engine/context"
	"intentra/engine/models"
	"intentra/engine/reasoning"
)

const clusteringSystemPrompt = `You are a code change analyzer. Your task is to group related code changes (hunks) into logical commits.

Rules:
- Each hunk_id must appear in exactly one group.
- Group changes that serve the same purpose together.
- Split unrelated concerns into separate groups.
- Keep inseparable changes together.
- Do not over-split: if changes are tightly coupled, keep them in one group.
- Use stable group IDs: g1, g2, g3, etc.
- Every hunk_id provided must be assigned to exactly one group. Do not omit any.
- Order groups by dependency: foundational changes (models, types, interfaces) first, then logic that depends on them, then CLI/entrypoint last. A later group may depend on an earlier group, but never the reverse.`

const messagingSystemPrompt = `You are a commit message generator following the Conventional Commits specification.

Rules:
- type must be one of the allowed types provided.
- subject must be imperative mood, lowercase first letter, no trailing period.
- subject length must be <= %d characters.
- scope is optional; if used, it must be from the allowed scopes or left null.
- body is optional; use it only for non-obvious changes.
- breaking must be true only for breaking changes, and must include a BREAKING CHANGE footer.
- Generate one commit per group_id provided.`

// CommitPlanner implements Planner for the commit planning use case.
type CommitPlanner struct {
	engine reasoning.ReasoningEngine
}

func NewCommitPlanner(engine reasoning.ReasoningEngine) *CommitPlanner {
	return &CommitPlanner{engine: engine}
}

func (p *CommitPlanner) Name() string {
	return "commit"
}

func (p *CommitPlanner) BuildPlan(ctx context.Context, ec enginectx.EngineContext) (models.Plan, error) {
	if len(ec.Hunks) == 0 {
		return nil, fmt.Errorf("no hunks to plan")
	}

	sortHunks(ec.Hunks)

	clustering, err := p.clusterHunks(ctx, ec)
	if err != nil {
		return nil, fmt.Errorf("clustering: %w", err)
	}

	messaging, err := p.generateMessages(ctx, ec, clustering)
	if err != nil {
		return nil, fmt.Errorf("messaging: %w", err)
	}

	plan := assemblePlan(ec, clustering, messaging)
	reorderCommitsByDependency(&plan, ec.Hunks)
	return &plan, nil
}

func (p *CommitPlanner) clusterHunks(ctx context.Context, ec enginectx.EngineContext) (ClusteringResponse, error) {
	input := buildClusteringInput(ec.Hunks)

	validateClustering := func(cr ClusteringResponse) error {
		return validateClusteringResponse(cr, ec.Hunks)
	}

	return reasoning.CallWithRetry[ClusteringResponse](
		ctx, p.engine,
		"clustering", ClusteringSchema,
		clusteringSystemPrompt, input,
		validateClustering,
	)
}

func (p *CommitPlanner) generateMessages(ctx context.Context, ec enginectx.EngineContext, clustering ClusteringResponse) (MessagingResponse, error) {
	input := buildMessagingInput(ec, clustering)
	sysPrompt := fmt.Sprintf(messagingSystemPrompt, ec.Config.Style.MaxSubjectLen)

	validateMessaging := func(mr MessagingResponse) error {
		return validateMessagingResponse(mr, clustering)
	}

	return reasoning.CallWithRetry[MessagingResponse](
		ctx, p.engine,
		"messaging", MessagingSchema,
		sysPrompt, input,
		validateMessaging,
	)
}

func buildClusteringInput(hunks []models.Hunk) string {
	var sb strings.Builder
	sb.WriteString("Hunks to cluster:\n\n")
	for _, h := range hunks {
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  header: %s\n  patch:\n%s\n\n",
			h.HunkID, h.FilePath, h.Header, h.Patch)
	}
	return sb.String()
}

func buildMessagingInput(ec enginectx.EngineContext, clustering ClusteringResponse) string {
	hunkMap := make(map[string]models.Hunk)
	for _, h := range ec.Hunks {
		hunkMap[h.HunkID] = h
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Allowed types: %s\n", strings.Join(ec.Config.Style.AllowedTypes, ", "))
	if len(ec.Config.Style.Scopes) > 0 {
		fmt.Fprintf(&sb, "Allowed scopes: %s\n", strings.Join(ec.Config.Style.Scopes, ", "))
	}
	sb.WriteString("\nGroups:\n\n")

	for _, g := range clustering.Groups {
		fmt.Fprintf(&sb, "Group %s:\n", g.ID)
		for _, hid := range g.HunkIDs {
			if h, ok := hunkMap[hid]; ok {
				fmt.Fprintf(&sb, "  - file: %s, header: %s\n    patch:\n%s\n", h.FilePath, h.Header, h.Patch)
			}
		}
		sb.WriteString("\n")
	}

	if len(ec.RecentCommits) > 0 {
		sb.WriteString("Recent commit style for reference:\n")
		for _, c := range ec.RecentCommits {
			fmt.Fprintf(&sb, "  %s\n", c)
		}
	}

	return sb.String()
}

func validateClusteringResponse(cr ClusteringResponse, hunks []models.Hunk) error {
	expected := make(map[string]bool)
	for _, h := range hunks {
		expected[h.HunkID] = true
	}

	seen := make(map[string]bool)
	for _, g := range cr.Groups {
		for _, hid := range g.HunkIDs {
			if !expected[hid] {
				return fmt.Errorf("unknown hunk_id %q in group %s", hid, g.ID)
			}
			if seen[hid] {
				return fmt.Errorf("duplicate hunk_id %q across groups", hid)
			}
			seen[hid] = true
		}
	}

	for hid := range expected {
		if !seen[hid] {
			return fmt.Errorf("hunk_id %q not assigned to any group", hid)
		}
	}

	return nil
}

func validateMessagingResponse(mr MessagingResponse, clustering ClusteringResponse) error {
	expectedGroups := make(map[string]bool)
	for _, g := range clustering.Groups {
		expectedGroups[g.ID] = true
	}

	seen := make(map[string]bool)
	for _, c := range mr.Commits {
		if !expectedGroups[c.GroupID] {
			return fmt.Errorf("unknown group_id %q", c.GroupID)
		}
		if seen[c.GroupID] {
			return fmt.Errorf("duplicate group_id %q in messaging", c.GroupID)
		}
		seen[c.GroupID] = true
	}

	for gid := range expectedGroups {
		if !seen[gid] {
			return fmt.Errorf("group_id %q has no commit message", gid)
		}
	}

	return nil
}

func assemblePlan(ec enginectx.EngineContext, clustering ClusteringResponse, messaging MessagingResponse) models.CommitPlan {
	groupHunks := make(map[string][]string)
	for _, g := range clustering.Groups {
		groupHunks[g.ID] = g.HunkIDs
	}

	msgMap := make(map[string]CommitMessageWithGroup)
	for _, c := range messaging.Commits {
		msgMap[c.GroupID] = c
	}

	var commits []models.CommitUnit
	for i, g := range clustering.Groups {
		msg := msgMap[g.ID]
		var footers []models.Footer
		for _, f := range msg.Footers {
			footers = append(footers, models.Footer{Token: f.Token, Value: f.Value})
		}

		commits = append(commits, models.CommitUnit{
			ID:       fmt.Sprintf("c%d", i+1),
			Type:     msg.Type,
			Scope:    msg.Scope,
			Subject:  msg.Subject,
			Body:     msg.Body,
			Breaking: msg.Breaking,
			Footers:  footers,
			Hunks:    groupHunks[g.ID],
		})
	}

	return models.CommitPlan{
		ToolVersion:     "0.1.0",
		BaseRef:         ec.BaseRef,
		DiffFingerprint: models.DiffFingerprintFromHunks(ec.Hunks),
		Style:           ec.Config.Style,
		Commits:         commits,
	}
}

func sortHunks(hunks []models.Hunk) {
	sort.Slice(hunks, func(i, j int) bool {
		if hunks[i].FilePath != hunks[j].FilePath {
			return hunks[i].FilePath < hunks[j].FilePath
		}
		return hunks[i].Header < hunks[j].Header
	})
}

// reorderCommitsByDependency sorts commits so that foundational packages
// (models, types, interfaces) come before higher-level consumers (cmd, main).
// Each commit gets a priority score based on the minimum package layer of
// its hunks. Lower scores are applied first.
func reorderCommitsByDependency(plan *models.CommitPlan, hunks []models.Hunk) {
	hunkDir := make(map[string]string, len(hunks))
	for _, h := range hunks {
		hunkDir[h.HunkID] = filepath.ToSlash(filepath.Dir(h.FilePath))
	}

	commitScore := func(c models.CommitUnit) int {
		minScore := 999
		for _, hid := range c.Hunks {
			s := packageLayer(hunkDir[hid])
			if s < minScore {
				minScore = s
			}
		}
		return minScore
	}

	sort.SliceStable(plan.Commits, func(i, j int) bool {
		return commitScore(plan.Commits[i]) < commitScore(plan.Commits[j])
	})

	for i := range plan.Commits {
		plan.Commits[i].ID = fmt.Sprintf("c%d", i+1)
	}
}

// packageLayer assigns a numeric layer to a directory path. Lower layers
// are more foundational and should be committed first.
func packageLayer(dir string) int {
	dir = strings.ToLower(dir)

	for _, seg := range strings.Split(dir, "/") {
		switch seg {
		case "models", "types", "schema", "schemas":
			return 0
		}
	}

	layerMap := map[string]int{
		"engine/models":     0,
		"engine/context":    1,
		"engine/reasoning":  2,
		"engine/planners":   3,
		"engine/validators": 4,
		"engine/executors":  5,
		"cmd":               6,
	}

	for prefix, layer := range layerMap {
		if strings.HasPrefix(dir, prefix) {
			return layer
		}
	}

	if dir == "." || dir == "" {
		return 7
	}

	return 5
}

// MarshalPlan serializes a CommitPlan to JSON.
func MarshalPlan(plan models.CommitPlan) ([]byte, error) {
	return json.MarshalIndent(plan, "", "  ")
}
