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

// ---------------------------------------------------------------------------
// System prompts
// ---------------------------------------------------------------------------

const clusteringSystemPrompt = `You are a code change analyzer. Your task is to group related code changes (hunks) into logical commits.

Rules:
- Each hunk_id must appear in exactly one group.
- Group changes that serve the same purpose together.
- Split unrelated concerns into separate groups.
- Keep inseparable changes together.
- Do not over-split: if changes are tightly coupled, keep them in one group.
- Hunks from the same file should stay in one group unless they serve clearly unrelated purposes (e.g., a bug fix and a new feature). Documentation or config changes in a single file should always be one group.
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
- body is optional; use it only when the subject alone is not enough to understand the change. When used, keep it to 1-2 concise sentences that explain WHY the change was made, not WHAT changed. Never enumerate individual changes line-by-line.
- breaking must be true only for breaking changes, and must include a BREAKING CHANGE footer.
- Generate one commit per group_id provided.`

const mergeSystemPrompt = `You are a code change analyzer. Groups from separate clustering batches need to be reconciled into final commit groups.

Rules:
- Every source group ID must appear in exactly one merged group.
- Merge groups from different batches that serve the same logical purpose.
- Do not over-merge: keep unrelated groups separate.
- Use stable group IDs: g1, g2, g3, etc.
- Create at most %d groups.`

// ---------------------------------------------------------------------------
// Compact ID mapping — replaces 64-char SHA256 hunk IDs with short tokens
// (h1, h2, …) in LLM prompts to save tokens and improve accuracy.
// ---------------------------------------------------------------------------

type idMapping struct {
	toReal    map[string]string
	toCompact map[string]string
	validIDs  map[string]bool
}

func buildIDMapping(hunks []models.Hunk) *idMapping {
	m := &idMapping{
		toReal:    make(map[string]string, len(hunks)),
		toCompact: make(map[string]string, len(hunks)),
		validIDs:  make(map[string]bool, len(hunks)),
	}
	for i, h := range hunks {
		compact := fmt.Sprintf("h%d", i+1)
		m.toReal[compact] = h.HunkID
		m.toCompact[h.HunkID] = compact
		m.validIDs[compact] = true
	}
	return m
}

func remapClusteringResponse(cr ClusteringResponse, m *idMapping) ClusteringResponse {
	for i, g := range cr.Groups {
		remapped := make([]string, 0, len(g.HunkIDs))
		for _, hid := range g.HunkIDs {
			if real, ok := m.toReal[hid]; ok {
				remapped = append(remapped, real)
			}
		}
		cr.Groups[i].HunkIDs = remapped
	}
	return cr
}

func validateCompactIDs(cr ClusteringResponse, validIDs map[string]bool, maxCommits int) error {
	for _, g := range cr.Groups {
		for _, hid := range g.HunkIDs {
			if !validIDs[hid] {
				return fmt.Errorf("unknown hunk_id %q in group %s", hid, g.ID)
			}
		}
	}
	if len(cr.Groups) > maxCommits {
		return fmt.Errorf("produced %d groups, max allowed is %d", len(cr.Groups), maxCommits)
	}
	return nil
}

// ---------------------------------------------------------------------------
// File-level pre-grouping — collapses per-file hunks into single units
// so the LLM clusters N files instead of M hunks (M >> N).
// ---------------------------------------------------------------------------

type fileUnit struct {
	id       string
	filePath string
	hunkIDs  []string
}

func preGroupByFile(hunks []models.Hunk) []fileUnit {
	fileMap := make(map[string][]models.Hunk)
	var fileOrder []string
	for _, h := range hunks {
		if _, exists := fileMap[h.FilePath]; !exists {
			fileOrder = append(fileOrder, h.FilePath)
		}
		fileMap[h.FilePath] = append(fileMap[h.FilePath], h)
	}

	units := make([]fileUnit, 0, len(fileOrder))
	for i, fp := range fileOrder {
		hunksForFile := fileMap[fp]
		hunkIDs := make([]string, 0, len(hunksForFile))
		for _, h := range hunksForFile {
			hunkIDs = append(hunkIDs, h.HunkID)
		}
		units = append(units, fileUnit{
			id:       fmt.Sprintf("f%d", i+1),
			filePath: fp,
			hunkIDs:  hunkIDs,
		})
	}
	return units
}

func expandFileUnits(cr ClusteringResponse, units []fileUnit) ClusteringResponse {
	unitMap := make(map[string]fileUnit, len(units))
	for _, u := range units {
		unitMap[u.id] = u
	}
	for i, g := range cr.Groups {
		expanded := make([]string, 0)
		for _, fid := range g.HunkIDs {
			if u, ok := unitMap[fid]; ok {
				expanded = append(expanded, u.hunkIDs...)
			}
		}
		cr.Groups[i].HunkIDs = expanded
	}
	return cr
}

// ---------------------------------------------------------------------------
// Batch splitting — groups file units by directory proximity.
// ---------------------------------------------------------------------------

type batchResult struct {
	cr    ClusteringResponse
	units []fileUnit
}

func splitIntoBatches(units []fileUnit, batchSize int) [][]fileUnit {
	sorted := make([]fileUnit, len(units))
	copy(sorted, units)
	sort.Slice(sorted, func(i, j int) bool {
		di := filepath.ToSlash(filepath.Dir(sorted[i].filePath))
		dj := filepath.ToSlash(filepath.Dir(sorted[j].filePath))
		return di < dj
	})

	var batches [][]fileUnit
	for i := 0; i < len(sorted); i += batchSize {
		end := i + batchSize
		if end > len(sorted) {
			end = len(sorted)
		}
		batches = append(batches, sorted[i:end])
	}
	return batches
}

func concatenateBatchGroups(results []batchResult) ClusteringResponse {
	var groups []ClusterGroup
	counter := 1
	for _, r := range results {
		for _, g := range r.cr.Groups {
			groups = append(groups, ClusterGroup{
				ID:      fmt.Sprintf("g%d", counter),
				HunkIDs: g.HunkIDs,
			})
			counter++
		}
	}
	return ClusteringResponse{Groups: groups}
}

// ---------------------------------------------------------------------------
// CommitPlanner
// ---------------------------------------------------------------------------

// CommitPlanner implements Planner for the commit planning use case.
type CommitPlanner struct {
	engine     reasoning.ReasoningEngine
	OnProgress func(stage string)
}

func NewCommitPlanner(engine reasoning.ReasoningEngine) *CommitPlanner {
	return &CommitPlanner{engine: engine}
}

func (p *CommitPlanner) progress(stage string) {
	if p.OnProgress != nil {
		p.OnProgress(stage)
	}
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

	p.progress(fmt.Sprintf("Clustering %d hunks...", len(ec.Hunks)))
	clustering, err := p.clusterHunks(ctx, ec)
	if err != nil {
		return nil, fmt.Errorf("clustering: %w", err)
	}

	p.progress(fmt.Sprintf("Generating commit messages for %d groups...", len(clustering.Groups)))
	messaging, err := p.generateMessages(ctx, ec, clustering)
	if err != nil {
		return nil, fmt.Errorf("messaging: %w", err)
	}

	plan := assemblePlan(ec, clustering, messaging)
	reorderCommitsByDependency(&plan, ec.Hunks)
	return &plan, nil
}

// ---------------------------------------------------------------------------
// Clustering — three-tier strategy based on diff size.
//
//	<= batchThreshold hunks  → clusterDirect  (hunk-level, compact IDs)
//	<= batchThreshold files  → clusterByFile  (file-unit level)
//	> batchThreshold files   → clusterBatched (split → parallel cluster → merge)
//
// All paths return a ClusteringResponse containing real hunk IDs.
// ---------------------------------------------------------------------------

func (p *CommitPlanner) clusterHunks(ctx context.Context, ec enginectx.EngineContext) (ClusteringResponse, error) {
	batchThreshold := ec.Config.Engine.BatchThreshold
	if batchThreshold <= 0 {
		batchThreshold = 40
	}
	maxCommits := ec.Config.Engine.MaxCommits
	if maxCommits <= 0 {
		maxCommits = 20
	}

	var cr ClusteringResponse
	var err error

	units := preGroupByFile(ec.Hunks)
	switch {
	case len(ec.Hunks) <= batchThreshold:
		cr, err = p.clusterDirect(ctx, ec, maxCommits)
	case len(units) <= batchThreshold:
		p.progress(fmt.Sprintf("File-level clustering (%d files from %d hunks)...", len(units), len(ec.Hunks)))
		cr, err = p.clusterByFile(ctx, ec, units, maxCommits)
	default:
		p.progress(fmt.Sprintf("Batched clustering (%d files)...", len(units)))
		cr, err = p.clusterBatched(ctx, ec, units, batchThreshold, maxCommits)
	}
	if err != nil {
		return cr, err
	}

	cr = deduplicateGroups(cr)
	cr = consolidateSingleFileGroups(cr, ec.Hunks)

	missing := findMissingHunks(cr, ec.Hunks)
	if len(missing) > 0 {
		rescued, rescueErr := p.rescueOrphanHunks(ctx, cr, missing, ec.Hunks)
		if rescueErr == nil {
			cr = rescued
		} else {
			cr = repairMissingHunks(cr, ec.Hunks)
		}
	}

	return cr, nil
}

// clusterDirect clusters hunks directly with compact prompt IDs.
func (p *CommitPlanner) clusterDirect(ctx context.Context, ec enginectx.EngineContext, maxCommits int) (ClusteringResponse, error) {
	input, idMap := buildClusteringInput(ec.Hunks, ec.Config.AI.MaxHunkLines)
	sysPrompt := fmt.Sprintf(clusteringSystemPrompt, maxCommits)

	validateHard := func(cr ClusteringResponse) error {
		return validateCompactIDs(cr, idMap.validIDs, maxCommits)
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

	return remapClusteringResponse(cr, idMap), nil
}

// clusterByFile groups hunks by file, then clusters file units.
func (p *CommitPlanner) clusterByFile(ctx context.Context, ec enginectx.EngineContext, units []fileUnit, maxCommits int) (ClusteringResponse, error) {
	input := buildFileUnitClusteringInput(units, ec.Hunks, ec.Config.AI.MaxHunkLines)
	sysPrompt := fmt.Sprintf(clusteringSystemPrompt, maxCommits)

	validIDs := make(map[string]bool, len(units))
	for _, u := range units {
		validIDs[u.id] = true
	}

	validateHard := func(cr ClusteringResponse) error {
		return validateCompactIDs(cr, validIDs, maxCommits)
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

	return expandFileUnits(cr, units), nil
}

// clusterBatched splits file units into directory-proximate batches,
// clusters each independently, then merges groups across batches.
func (p *CommitPlanner) clusterBatched(ctx context.Context, ec enginectx.EngineContext, units []fileUnit, batchSize int, maxCommits int) (ClusteringResponse, error) {
	batches := splitIntoBatches(units, batchSize)

	var results []batchResult
	for batchIdx, batch := range batches {
		p.progress(fmt.Sprintf("Clustering batch %d/%d (%d files)...", batchIdx+1, len(batches), len(batch)))
		input := buildFileUnitClusteringInput(batch, ec.Hunks, ec.Config.AI.MaxHunkLines)

		batchMaxCommits := (maxCommits*len(batch))/len(units) + 1
		if batchMaxCommits < 2 {
			batchMaxCommits = 2
		}
		if batchMaxCommits > maxCommits {
			batchMaxCommits = maxCommits
		}

		sysPrompt := fmt.Sprintf(clusteringSystemPrompt, batchMaxCommits)

		validIDs := make(map[string]bool, len(batch))
		for _, u := range batch {
			validIDs[u.id] = true
		}

		validateHard := func(cr ClusteringResponse) error {
			return validateCompactIDs(cr, validIDs, batchMaxCommits)
		}

		cr, err := reasoning.CallWithRetry[ClusteringResponse](
			ctx, p.engine,
			"clustering", ClusteringSchema,
			sysPrompt, input,
			validateHard,
			ec.Config.AI.MaxRetries,
		)
		if err != nil {
			return ClusteringResponse{}, fmt.Errorf("batch %d/%d: %w", batchIdx+1, len(batches), err)
		}

		prefix := fmt.Sprintf("b%d", batchIdx+1)
		for i, g := range cr.Groups {
			cr.Groups[i].ID = prefix + g.ID
		}

		results = append(results, batchResult{cr: cr, units: batch})
	}

	if len(results) == 1 {
		return expandFileUnits(results[0].cr, results[0].units), nil
	}

	p.progress(fmt.Sprintf("Merging %d batches...", len(results)))
	merged, err := p.mergeBatchGroups(ctx, ec, results, maxCommits)
	if err != nil {
		merged = concatenateBatchGroups(results)
	}

	allUnits := make([]fileUnit, 0, len(units))
	for _, r := range results {
		allUnits = append(allUnits, r.units...)
	}
	return expandFileUnits(merged, allUnits), nil
}

// mergeBatchGroups asks the LLM to reconcile groups across batches.
func (p *CommitPlanner) mergeBatchGroups(ctx context.Context, ec enginectx.EngineContext, results []batchResult, maxCommits int) (ClusteringResponse, error) {
	unitByID := make(map[string]fileUnit)
	for _, r := range results {
		for _, u := range r.units {
			unitByID[u.id] = u
		}
	}

	var sb strings.Builder
	var allGroupIDs []string

	sb.WriteString("Batch groups to reconcile:\n\n")
	for _, r := range results {
		for _, g := range r.cr.Groups {
			files := make(map[string]bool)
			for _, fid := range g.HunkIDs {
				if u, ok := unitByID[fid]; ok {
					files[u.filePath] = true
				}
			}
			var fileList []string
			for f := range files {
				fileList = append(fileList, f)
			}
			sort.Strings(fileList)
			fmt.Fprintf(&sb, "- %s: %s\n", g.ID, strings.Join(fileList, ", "))
			allGroupIDs = append(allGroupIDs, g.ID)
		}
	}

	sysPrompt := fmt.Sprintf(mergeSystemPrompt, maxCommits)

	validGroupIDs := make(map[string]bool, len(allGroupIDs))
	for _, gid := range allGroupIDs {
		validGroupIDs[gid] = true
	}

	validate := func(mr MergeResponse) error {
		seen := make(map[string]bool)
		for _, mg := range mr.Groups {
			for _, sg := range mg.SourceGroups {
				if !validGroupIDs[sg] {
					return fmt.Errorf("unknown source group %q", sg)
				}
				if seen[sg] {
					return fmt.Errorf("source group %q assigned to multiple merged groups", sg)
				}
				seen[sg] = true
			}
		}
		for _, gid := range allGroupIDs {
			if !seen[gid] {
				return fmt.Errorf("source group %q not assigned to any merged group", gid)
			}
		}
		if len(mr.Groups) > maxCommits {
			return fmt.Errorf("produced %d groups, max allowed is %d", len(mr.Groups), maxCommits)
		}
		return nil
	}

	mr, err := reasoning.CallWithRetry[MergeResponse](
		ctx, p.engine,
		"merge", MergeSchema,
		sysPrompt, sb.String(),
		validate,
		ec.Config.AI.MaxRetries,
	)
	if err != nil {
		return ClusteringResponse{}, err
	}

	sourceHunks := make(map[string][]string)
	for _, r := range results {
		for _, g := range r.cr.Groups {
			sourceHunks[g.ID] = g.HunkIDs
		}
	}

	var groups []ClusterGroup
	for _, mg := range mr.Groups {
		var combined []string
		for _, sg := range mg.SourceGroups {
			combined = append(combined, sourceHunks[sg]...)
		}
		groups = append(groups, ClusterGroup{
			ID:      mg.ID,
			HunkIDs: combined,
		})
	}

	return ClusteringResponse{Groups: groups}, nil
}

// ---------------------------------------------------------------------------
// Rescue — assigns orphaned hunks to existing groups.
// ---------------------------------------------------------------------------

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
	orphanIDMap := &idMapping{
		toReal:    make(map[string]string, len(orphans)),
		toCompact: make(map[string]string, len(orphans)),
		validIDs:  make(map[string]bool, len(orphans)),
	}
	for i, h := range orphans {
		compact := fmt.Sprintf("o%d", i+1)
		orphanIDMap.toReal[compact] = h.HunkID
		orphanIDMap.toCompact[h.HunkID] = compact
		orphanIDMap.validIDs[compact] = true
	}

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
		compact := orphanIDMap.toCompact[h.HunkID]
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  header: %s\n  patch:\n%s\n\n",
			compact, h.FilePath, h.Header, h.Patch)
	}

	groupIDs := make(map[string]bool, len(cr.Groups))
	for _, g := range cr.Groups {
		groupIDs[g.ID] = true
	}

	validate := func(rr RescueResponse) error {
		if len(rr.Assignments) != len(orphans) {
			return fmt.Errorf("expected %d assignments, got %d", len(orphans), len(rr.Assignments))
		}
		seen := make(map[string]bool)
		for _, a := range rr.Assignments {
			if !orphanIDMap.validIDs[a.HunkID] {
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
		realID := orphanIDMap.toReal[a.HunkID]
		idx := groupIdx[a.GroupID]
		cr.Groups[idx].HunkIDs = append(cr.Groups[idx].HunkIDs, realID)
	}

	return cr, nil
}

// ---------------------------------------------------------------------------
// Messaging
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Input builders
// ---------------------------------------------------------------------------

func buildClusteringInput(hunks []models.Hunk, maxHunkLines int) (string, *idMapping) {
	idMap := buildIDMapping(hunks)

	var sb strings.Builder
	fmt.Fprintf(&sb, "Hunks to cluster (%d total — you must assign all %d):\n\n", len(hunks), len(hunks))
	for _, h := range hunks {
		compact := idMap.toCompact[h.HunkID]
		patch := summarizePatch(h.Patch, maxHunkLines)
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  header: %s\n  patch:\n%s\n\n",
			compact, h.FilePath, h.Header, patch)
	}

	fmt.Fprintf(&sb, "\n--- CHECKLIST: all %d hunk IDs (every one must appear in exactly one group) ---\n", len(hunks))
	for _, h := range hunks {
		fmt.Fprintf(&sb, "  %s\n", idMap.toCompact[h.HunkID])
	}

	return sb.String(), idMap
}

func buildFileUnitClusteringInput(units []fileUnit, hunks []models.Hunk, maxHunkLines int) string {
	hunkMap := make(map[string]models.Hunk, len(hunks))
	for _, h := range hunks {
		hunkMap[h.HunkID] = h
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "File units to cluster (%d total — you must assign all %d):\n\n", len(units), len(units))
	for _, u := range units {
		fmt.Fprintf(&sb, "- hunk_id: %s\n  file: %s\n  changes: %d hunk(s)\n", u.id, u.filePath, len(u.hunkIDs))
		for _, hid := range u.hunkIDs {
			if h, ok := hunkMap[hid]; ok {
				patch := summarizePatch(h.Patch, maxHunkLines)
				fmt.Fprintf(&sb, "  header: %s\n  patch:\n%s\n", h.Header, patch)
			}
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "\n--- CHECKLIST: all %d IDs (every one must appear in exactly one group) ---\n", len(units))
	for _, u := range units {
		fmt.Fprintf(&sb, "  %s\n", u.id)
	}

	return sb.String()
}

// summarizePatch truncates a patch to the first and last keepLines lines
// if it exceeds maxLines, reducing token usage while preserving enough
// context for the LLM to cluster correctly. maxLines <= 0 disables truncation.
func summarizePatch(patch string, maxLines int) string {
	if maxLines <= 0 {
		return patch
	}
	lines := strings.Split(patch, "\n")
	if len(lines) <= maxLines {
		return patch
	}
	keepLines := maxLines / 5
	if keepLines < 5 {
		keepLines = 5
	}
	if keepLines*2 >= len(lines) {
		return patch
	}

	head := lines[:keepLines]
	tail := lines[len(lines)-keepLines:]
	omitted := len(lines) - keepLines*2
	return strings.Join(head, "\n") +
		fmt.Sprintf("\n... (%d lines omitted) ...\n", omitted) +
		strings.Join(tail, "\n")
}

func buildMessagingInput(ec enginectx.EngineContext, clustering ClusteringResponse) string {
	hunkMap := make(map[string]models.Hunk)
	for _, h := range ec.Hunks {
		hunkMap[h.HunkID] = h
	}

	maxLines := ec.Config.AI.MaxHunkLines

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
				patch := summarizePatch(h.Patch, maxLines)
				fmt.Fprintf(&sb, "  - file: %s, header: %s\n    patch:\n%s\n", h.FilePath, h.Header, patch)
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

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// validateClusteringHardErrors checks for unknown hunks only (kept for tests).
func validateClusteringHardErrors(cr ClusteringResponse, hunks []models.Hunk) error {
	expected := make(map[string]bool)
	for _, h := range hunks {
		expected[h.HunkID] = true
	}
	for _, g := range cr.Groups {
		for _, hid := range g.HunkIDs {
			if !expected[hid] {
				return fmt.Errorf("unknown hunk_id %q in group %s", hid, g.ID)
			}
		}
	}
	return nil
}

// consolidateSingleFileGroups merges groups that exclusively contain hunks
// from the same file. If g1 only touches README.md and g3 only touches
// README.md, they become one group. Groups touching multiple files are left
// alone.
func consolidateSingleFileGroups(cr ClusteringResponse, hunks []models.Hunk) ClusteringResponse {
	hunkByID := make(map[string]models.Hunk, len(hunks))
	for _, h := range hunks {
		hunkByID[h.HunkID] = h
	}

	type groupInfo struct {
		file     string
		isSingle bool
	}

	infos := make([]groupInfo, len(cr.Groups))
	for i, g := range cr.Groups {
		files := make(map[string]bool)
		for _, hid := range g.HunkIDs {
			if h, ok := hunkByID[hid]; ok {
				files[h.FilePath] = true
			}
		}
		if len(files) == 1 {
			for f := range files {
				infos[i] = groupInfo{file: f, isSingle: true}
			}
		}
	}

	fileToGroups := make(map[string][]int)
	for i, info := range infos {
		if info.isSingle {
			fileToGroups[info.file] = append(fileToGroups[info.file], i)
		}
	}

	merged := make(map[int]bool)
	for _, indices := range fileToGroups {
		if len(indices) <= 1 {
			continue
		}
		target := indices[0]
		for _, src := range indices[1:] {
			cr.Groups[target].HunkIDs = append(cr.Groups[target].HunkIDs, cr.Groups[src].HunkIDs...)
			merged[src] = true
		}
	}

	if len(merged) == 0 {
		return cr
	}

	var result []ClusterGroup
	for i, g := range cr.Groups {
		if !merged[i] {
			result = append(result, g)
		}
	}
	for i := range result {
		result[i].ID = fmt.Sprintf("g%d", i+1)
	}
	return ClusteringResponse{Groups: result}
}

func deduplicateGroups(cr ClusteringResponse) ClusteringResponse {
	seen := make(map[string]bool)
	for i, g := range cr.Groups {
		deduped := make([]string, 0, len(g.HunkIDs))
		for _, hid := range g.HunkIDs {
			if !seen[hid] {
				seen[hid] = true
				deduped = append(deduped, hid)
			}
		}
		cr.Groups[i].HunkIDs = deduped
	}
	return cr
}

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

// ---------------------------------------------------------------------------
// Plan assembly
// ---------------------------------------------------------------------------

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
		SchemaVersion:   models.CurrentSchemaVersion,
		ToolVersion:     internal.Version,
		BaseRef:         ec.BaseRef,
		DiffFingerprint: models.DiffFingerprintFromHunks(ec.Hunks),
		Style:           ec.Config.Style,
		Commits:         commits,
	}
}

// ---------------------------------------------------------------------------
// Sorting & ordering
// ---------------------------------------------------------------------------

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
