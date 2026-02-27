package validators

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/crvgilbertson/intentra/engine/models"
)

// PlanConfidence describes the overall confidence level of a commit plan.
type PlanConfidence struct {
	Score      float64          // 0.0 (no confidence) to 1.0 (high confidence)
	Level      string           // "high", "medium", "low"
	Warnings   []string         // human-readable risk signals
	Risks      []CommitRisk     // per-commit risk details
	Components models.ConfidenceComponents
}

// CommitRisk describes risk factors for a single commit.
type CommitRisk struct {
	CommitID string
	Factors  []string
}

// AssessPlanConfidence evaluates a commit plan and returns a confidence
// assessment based on structural heuristics. This does NOT call the LLM;
// it is a pure function over the plan and hunk data.
func AssessPlanConfidence(plan models.CommitPlan, hunks []models.Hunk) PlanConfidence {
	return AssessPlanConfidenceWithTrace(plan, hunks, nil)
}

// AssessPlanConfidenceWithTrace is AssessPlanConfidence with optional trace
// data used to compute repair_activity and reorder_penalty components.
func AssessPlanConfidenceWithTrace(plan models.CommitPlan, hunks []models.Hunk, trace *models.PipelineTrace) PlanConfidence {
	pc := PlanConfidence{Score: 1.0}

	hunkByID := make(map[string]models.Hunk, len(hunks))
	for _, h := range hunks {
		hunkByID[h.HunkID] = h
	}

	overlapPenalty := assessFileOverlap(plan, hunkByID, &pc)
	entanglePenalty := assessEntanglement(plan, hunkByID, &pc)
	spreadPenalty := assessCommitSpread(plan, hunkByID, &pc)
	singleHunkBonus := assessSingleHunkCommits(plan, &pc)

	pc.Score -= overlapPenalty + entanglePenalty + spreadPenalty
	pc.Score += singleHunkBonus

	if pc.Score > 1.0 {
		pc.Score = 1.0
	}
	if pc.Score < 0.0 {
		pc.Score = 0.0
	}

	coverageScore := 1.0
	repairActivity := 1.0
	reorderPenalty := 1.0

	if trace != nil {
		totalHunks := len(hunks)
		if totalHunks > 0 && trace.OrphanCount > 0 {
			repairActivity = 1.0 - float64(trace.OrphanCount)/float64(totalHunks)*0.5
			if repairActivity < 0 {
				repairActivity = 0
			}
		}
		if trace.ReorderApplied {
			reorderPenalty = 0.95
		}
	}

	pc.Components = models.ConfidenceComponents{
		Coverage:       coverageScore,
		Entanglement:   clampScore(1.0 - entanglePenalty),
		RepairActivity: repairActivity,
		Overlap:        clampScore(1.0 - overlapPenalty),
		ReorderPenalty: reorderPenalty,
	}

	switch {
	case pc.Score >= 0.8:
		pc.Level = "high"
	case pc.Score >= 0.5:
		pc.Level = "medium"
	default:
		pc.Level = "low"
	}

	return pc
}

func clampScore(v float64) float64 {
	if v > 1.0 {
		return 1.0
	}
	if v < 0.0 {
		return 0.0
	}
	return v
}

// assessFileOverlap penalizes plans where the same file is modified in
// multiple commits, since later patches may fail due to shifted line numbers.
func assessFileOverlap(plan models.CommitPlan, hunkByID map[string]models.Hunk, pc *PlanConfidence) float64 {
	fileCommits := make(map[string][]string)
	for _, c := range plan.Commits {
		seen := make(map[string]bool)
		for _, hid := range c.Hunks {
			if h, ok := hunkByID[hid]; ok && !seen[h.FilePath] {
				fileCommits[h.FilePath] = append(fileCommits[h.FilePath], c.ID)
				seen[h.FilePath] = true
			}
		}
	}

	var penalty float64
	for file, commits := range fileCommits {
		if len(commits) > 1 {
			penalty += 0.10 * float64(len(commits)-1)
			pc.Warnings = append(pc.Warnings,
				fmt.Sprintf("%q split across %d commits (%s) — later patches may fail",
					file, len(commits), strings.Join(commits, ", ")))
		}
	}
	return penalty
}

var hunkRangeRe = regexp.MustCompile(`@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type lineRange struct {
	start, end int
}

func parseHunkRange(header string) (old, new_ lineRange, ok bool) {
	m := hunkRangeRe.FindStringSubmatch(header)
	if m == nil {
		return lineRange{}, lineRange{}, false
	}
	oldStart, _ := strconv.Atoi(m[1])
	oldCount := 1
	if m[2] != "" {
		oldCount, _ = strconv.Atoi(m[2])
	}
	newStart, _ := strconv.Atoi(m[3])
	newCount := 1
	if m[4] != "" {
		newCount, _ = strconv.Atoi(m[4])
	}
	return lineRange{oldStart, oldStart + oldCount},
		lineRange{newStart, newStart + newCount},
		true
}

func rangesOverlap(a, b lineRange, margin int) bool {
	return a.start-margin <= b.end && b.start-margin <= a.end
}

// assessEntanglement detects hunks in different commits that touch adjacent
// or overlapping line ranges in the same file. These are high-risk because
// the second commit's patch is generated against the original HEAD, but the
// first commit changes the file, shifting line numbers.
func assessEntanglement(plan models.CommitPlan, hunkByID map[string]models.Hunk, pc *PlanConfidence) float64 {
	type commitHunk struct {
		commitID string
		hunk     models.Hunk
		newRange lineRange
	}

	fileHunks := make(map[string][]commitHunk)
	for _, c := range plan.Commits {
		for _, hid := range c.Hunks {
			h, ok := hunkByID[hid]
			if !ok || h.Header == "" {
				continue
			}
			_, nr, ok := parseHunkRange(h.Header)
			if !ok {
				continue
			}
			fileHunks[h.FilePath] = append(fileHunks[h.FilePath], commitHunk{
				commitID: c.ID,
				hunk:     h,
				newRange: nr,
			})
		}
	}

	const adjacencyMargin = 5
	var penalty float64
	seen := make(map[string]bool)

	for file, chs := range fileHunks {
		sort.Slice(chs, func(i, j int) bool {
			return chs[i].newRange.start < chs[j].newRange.start
		})

		for i := 0; i < len(chs); i++ {
			for j := i + 1; j < len(chs); j++ {
				if chs[i].commitID == chs[j].commitID {
					continue
				}
				if rangesOverlap(chs[i].newRange, chs[j].newRange, adjacencyMargin) {
					key := chs[i].commitID + ":" + chs[j].commitID + ":" + file
					if seen[key] {
						continue
					}
					seen[key] = true
					penalty += 0.15
					pc.Warnings = append(pc.Warnings,
						fmt.Sprintf("entangled hunks in %q: %s (lines %d-%d) and %s (lines %d-%d) are within %d lines",
							file, chs[i].commitID, chs[i].newRange.start, chs[i].newRange.end,
							chs[j].commitID, chs[j].newRange.start, chs[j].newRange.end,
							adjacencyMargin))

					addCommitRisk(pc, chs[i].commitID,
						fmt.Sprintf("entangled with %s in %s", chs[j].commitID, file))
					addCommitRisk(pc, chs[j].commitID,
						fmt.Sprintf("entangled with %s in %s", chs[i].commitID, file))
				}
			}
		}
	}
	return penalty
}

// assessCommitSpread penalizes commits that touch many unrelated files,
// which often indicates the LLM lumped unrelated changes together.
func assessCommitSpread(plan models.CommitPlan, hunkByID map[string]models.Hunk, pc *PlanConfidence) float64 {
	const wideThreshold = 8
	var penalty float64
	for _, c := range plan.Commits {
		files := make(map[string]bool)
		for _, hid := range c.Hunks {
			if h, ok := hunkByID[hid]; ok {
				files[h.FilePath] = true
			}
		}
		if len(files) > wideThreshold {
			penalty += 0.05 * float64(len(files)-wideThreshold)
			pc.Warnings = append(pc.Warnings,
				fmt.Sprintf("commit %s touches %d files — may be over-grouped", c.ID, len(files)))
			addCommitRisk(pc, c.ID,
				fmt.Sprintf("wide spread: %d files", len(files)))
		}
	}
	return penalty
}

// assessSingleHunkCommits gives a small bonus when all commits are
// single-hunk (simplest, most reliable case).
func assessSingleHunkCommits(plan models.CommitPlan, pc *PlanConfidence) float64 {
	allSingle := true
	for _, c := range plan.Commits {
		if len(c.Hunks) > 1 {
			allSingle = false
			break
		}
	}
	if allSingle && len(plan.Commits) > 0 {
		return 0.05
	}
	return 0.0
}

func addCommitRisk(pc *PlanConfidence, commitID, factor string) {
	for i, r := range pc.Risks {
		if r.CommitID == commitID {
			pc.Risks[i].Factors = append(pc.Risks[i].Factors, factor)
			return
		}
	}
	pc.Risks = append(pc.Risks, CommitRisk{CommitID: commitID, Factors: []string{factor}})
}
