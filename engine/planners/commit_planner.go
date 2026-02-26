package planners

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	enginectx "github.com/crvgilbertson/intentra/engine/context"
	"github.com/crvgilbertson/intentra/engine/models"
	"github.com/crvgilbertson/intentra/engine/reasoning"
	"github.com/crvgilbertson/intentra/internal"
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
- Order groups by dependency: foundational changes (models, types, interfaces) first, then logic that depends on them, then CLI/entrypoint last. A later group may depend on an earlier group, but never the reverse.
- Create at most %d groups. If changes are numerous, group less critical changes together rather than exceeding the limit.
- CRITICAL: Before responding, count the total hunk_ids across all your groups and verify it equals the number provided. Missing even one hunk is a failure. Cross-reference against the checklist at the end of the input.`

const rescueSystemPrompt = `You are a code change analyzer. During clustering, some hunks were not assigned to any group.
Assign each orphaned hunk to the single most appropriate existing group based on its file path, content, and purpose.

Rules:
- You must assign every orphaned hunk to exactly one existing group.
- Do not create new groups.
- Do not omit any hunk.`

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

	if ec.Config.AI.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(ec.Config.AI.Timeout)*time.Second)
		defer cancel()
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

	maxCommits := ec.Config.Engine.MaxCommits
	if maxCommits <= 0 {
		maxCommits = 20
	}
	sysPrompt := fmt.Sprintf(clusteringSystemPrompt, maxCommits)

	// Only fail on hard errors (duplicates, unknowns). Missing hunks are repaired after.
	validateHard := func(cr ClusteringResponse) error {
		if err := validateClusteringHardErrors(cr, ec.Hunks); err != nil {
			return err
		}
		if len(cr.Groups) > maxCommits {
			return fmt.Errorf("produced %d groups, max allowed is %d", len(cr.Groups), maxCommits)
		}
		return nil
	}

	cr, err := reasoning.CallWithRetry[ClusteringResponse](
		ctx, p.engine,
		"clustering", ClusteringSchema,
		sysPrompt, input,
		validateHard,
		ec.Config.AI.MaxRetries,
	)
	if err != nil {
		return cr, err
	}

	missing := findMissingHunks(cr, ec.Hunks)
	if len(missing) > 0 {
		rescued, err := p.rescueOrphanHunks(ctx, cr, missing, ec.Hunks)
		if err == nil {
			cr = rescued
		} else {
			cr = repairMissingHunks(cr, ec.Hunks)
		}
	}

	return cr, nil
}

func findMissingHunks(cr ClusteringResponse, hunks []models.Hunk) []models.Hunk {
	assigned := make(map[string]bool)
	for _, g := range cr.Groups {
		for _, hid := range g.HunkIDs {
			assigned[hid] = true
		}
	}

	var missing []models.Hunk
	for _, h := range hunks {
		if !assigned[h.HunkID] {
			missing = append(missing, h)
		}
	}
	return missing
}

func (p *CommitPlanner) rescueOrphanHunks(
	ctx context.Context,
	cr ClusteringResponse,
	orphans []models.Hunk,
	allHunks []models.Hunk,
) (ClusteringResponse, error) {
	hunkByID := make(map[string]models.Hunk, len(allHunks))
	for _, h := range allHunks {
		hunkByID[h.HunkID] = h
	}

	var sb strings.Builder
	sb.WriteString("Existing groups:\n")
	for _, g := range cr.Groups {
		var files []string
		seen := make(map[string]bool)
		for _, hid := range g.HunkIDs {
			if h, ok := hunkByID[hid]; ok && !seen[h.FilePath] {
				files = append(files, h.FilePath)
				seen[h.FilePath] = true
			}
		}
		fmt.Fprintf(&sb, "- %s: %s\n", g.ID, strings.Join(files, ", "))
	}

	fmt.Fprintf(&sb, "\nOrphaned hunks (%d):\n\n", len(orphans))
	for _, h := range orphans {
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  header: %s\n  patch:\n%s\n\n",
			h.HunkID, h.FilePath, h.Header, h.Patch)
	}

	groupIDs := make(map[string]bool, len(cr.Groups))
	for _, g := range cr.Groups {
		groupIDs[g.ID] = true
	}
	orphanIDs := make(map[string]bool, len(orphans))
	for _, h := range orphans {
		orphanIDs[h.HunkID] = true
	}

	validate := func(rr RescueResponse) error {
		if len(rr.Assignments) != len(orphans) {
			return fmt.Errorf("expected %d assignments, got %d", len(orphans), len(rr.Assignments))
		}
		seen := make(map[string]bool)
		for _, a := range rr.Assignments {
			if !orphanIDs[a.HunkID] {
				return fmt.Errorf("unexpected hunk_id %q", a.HunkID)
			}
			if !groupIDs[a.GroupID] {
				return fmt.Errorf("unknown group_id %q", a.GroupID)
			}
			if seen[a.HunkID] {
				return fmt.Errorf("duplicate hunk_id %q", a.HunkID)
			}
			seen[a.HunkID] = true
		}
		return nil
	}

	rr, err := reasoning.CallWithRetry[RescueResponse](
		ctx, p.engine,
		"rescue", RescueSchema,
		rescueSystemPrompt, sb.String(),
		validate,
		1,
	)
	if err != nil {
		return cr, fmt.Errorf("rescue call: %w", err)
	}

	groupIdx := make(map[string]int, len(cr.Groups))
	for i, g := range cr.Groups {
		groupIdx[g.ID] = i
	}
	for _, a := range rr.Assignments {
		idx := groupIdx[a.GroupID]
		cr.Groups[idx].HunkIDs = append(cr.Groups[idx].HunkIDs, a.HunkID)
	}

	return cr, nil
}

func (p *CommitPlanner) generateMessages(ctx context.Context, ec enginectx.EngineContext, clustering ClusteringResponse) (MessagingResponse, error) {
	input := buildMessagingInput(ec, clustering)
	maxSubjectLen := ec.Config.Style.MaxSubjectLen
	sysPrompt := fmt.Sprintf(messagingSystemPrompt, maxSubjectLen)

	validateMessaging := func(mr MessagingResponse) error {
		return validateMessagingResponse(mr, clustering, maxSubjectLen)
	}

	return reasoning.CallWithRetry[MessagingResponse](
		ctx, p.engine,
		"messaging", MessagingSchema,
		sysPrompt, input,
		validateMessaging,
		ec.Config.AI.MaxRetries,
	)
}

func buildClusteringInput(hunks []models.Hunk) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Hunks to cluster (%d total — you must assign all %d):\n\n", len(hunks), len(hunks))
	for _, h := range hunks {
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  header: %s\n  patch:\n%s\n\n",
			h.HunkID, h.FilePath, h.Header, h.Patch)
	}

	fmt.Fprintf(&sb, "\n--- CHECKLIST: all %d hunk IDs (every one must appear in exactly one group) ---\n", len(hunks))
	for _, h := range hunks {
		fmt.Fprintf(&sb, "  %s\n", h.HunkID)
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

// validateClusteringHardErrors checks for duplicates and unknown hunks only.
// Missing hunks are handled by repairMissingHunks rather than treated as errors.
func validateClusteringHardErrors(cr ClusteringResponse, hunks []models.Hunk) error {
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

	return nil
}

// repairMissingHunks assigns any unassigned hunks to the group with the most
// hunks from the same file. If no file match exists, the largest group is used.
func repairMissingHunks(cr ClusteringResponse, hunks []models.Hunk) ClusteringResponse {
	assigned := make(map[string]bool)
	for _, g := range cr.Groups {
		for _, hid := range g.HunkIDs {
			assigned[hid] = true
		}
	}

	hunkByID := make(map[string]models.Hunk, len(hunks))
	for _, h := range hunks {
		hunkByID[h.HunkID] = h
	}

	var missing []string
	for _, h := range hunks {
		if !assigned[h.HunkID] {
			missing = append(missing, h.HunkID)
		}
	}

	if len(missing) == 0 {
		return cr
	}

	// Build group -> file -> count index
	groupFiles := make([]map[string]int, len(cr.Groups))
	for i, g := range cr.Groups {
		groupFiles[i] = make(map[string]int)
		for _, hid := range g.HunkIDs {
			if h, ok := hunkByID[hid]; ok {
				groupFiles[i][h.FilePath]++
			}
		}
	}

	for _, hid := range missing {
		h := hunkByID[hid]
		bestGroup := 0
		bestScore := -1

		for i, fc := range groupFiles {
			if score := fc[h.FilePath]; score > bestScore {
				bestScore = score
				bestGroup = i
			}
		}

		// No file match — use the largest group
		if bestScore <= 0 {
			maxSize := 0
			for i, g := range cr.Groups {
				if len(g.HunkIDs) > maxSize {
					maxSize = len(g.HunkIDs)
					bestGroup = i
				}
			}
		}

		cr.Groups[bestGroup].HunkIDs = append(cr.Groups[bestGroup].HunkIDs, hid)
		groupFiles[bestGroup][h.FilePath]++
	}

	return cr
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

func validateMessagingResponse(mr MessagingResponse, clustering ClusteringResponse, maxSubjectLen int) error {
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

		if maxSubjectLen > 0 && len(c.Subject) > maxSubjectLen+10 {
			return fmt.Errorf("group %q subject far exceeds %d chars (%d): %q",
				c.GroupID, maxSubjectLen, len(c.Subject), c.Subject)
		}
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

		subject := msg.Subject
		maxLen := ec.Config.Style.MaxSubjectLen
		if maxLen > 0 && len(subject) > maxLen {
			subject = strings.TrimSpace(subject[:maxLen-3]) + "..."
		}

		commits = append(commits, models.CommitUnit{
			ID:       fmt.Sprintf("c%d", i+1),
			Type:     msg.Type,
			Scope:    msg.Scope,
			Subject:  subject,
			Body:     msg.Body,
			Breaking: msg.Breaking,
			Footers:  footers,
			Hunks:    groupHunks[g.ID],
		})
	}

	return models.CommitPlan{
		ToolVersion:     internal.Version,
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
