package validators

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/crvgilbertson/intentra/config"
	"github.com/crvgilbertson/intentra/engine/models"
)

// ScoreCommitRisk computes deterministic risk for a commit based on file paths.
// Returns nil if risk config is disabled or empty.
func ScoreCommitRisk(c models.CommitUnit, hunkToFile map[string]string, cfg config.RiskConfig) *models.CommitRisk {
	if !cfg.Enabled || len(cfg.Areas) == 0 {
		return nil
	}

	var score float64
	var areas []string
	var signals []string

	for areaName, rule := range cfg.Areas {
		for _, pattern := range rule.Patterns {
			for _, hid := range c.Hunks {
				fp := hunkToFile[hid]
				if fp == "" {
					continue
				}
				slash := filepath.ToSlash(fp)
				matched, _ := pathMatch(pattern, slash)
				if matched {
					score += rule.Weight
					areas = appendUnique(areas, areaName)
					signals = appendUnique(signals, "risk."+areaName+".pattern:"+pattern)
				}
			}
		}
	}

	sort.Strings(areas)
	sort.Strings(signals)

	if score == 0 {
		return nil
	}

	// Normalize score to 0..1 (cap at 1.0)
	if score > 1.0 {
		score = 1.0
	}

	level := "low"
	if score >= cfg.HighThreshold() {
		level = "high"
	} else if score >= cfg.MediumThreshold() {
		level = "medium"
	}

	return &models.CommitRisk{
		Score:   score,
		Level:   level,
		Areas:   areas,
		Signals: signals,
	}
}

func pathMatch(pattern, path string) (bool, error) {
	slash := filepath.ToSlash(path)
	if matched, err := filepath.Match(pattern, slash); err == nil && matched {
		return true, nil
	}
	// Directory prefix: "auth/" or "auth" matches "auth/foo.go"
	p := filepath.ToSlash(pattern)
	if strings.HasSuffix(p, "/") {
		return strings.HasPrefix(slash, p), nil
	}
	if !strings.Contains(p, "*") && !strings.Contains(p, "?") {
		if slash == p || strings.HasPrefix(slash, p+"/") {
			return true, nil
		}
		// Segment match: "auth" matches "pkg/auth/handler.go"
		return strings.Contains(slash, "/"+p+"/") || strings.HasSuffix(slash, "/"+p), nil
	}
	return false, nil
}

func appendUnique(s []string, x string) []string {
	for _, v := range s {
		if v == x {
			return s
		}
	}
	return append(s, x)
}
