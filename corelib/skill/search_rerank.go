package skill

import (
	"math"
	"sort"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RerankByLocalHistory reorders search results based on local execution
// history AND global quality signals (AvgRating, Downloads). Skills with
// low local success rates are demoted; skills with high global quality
// signals are promoted.
//
// This closes the signal gap between execution results and discovery:
// a skill that consistently fails locally should not appear at the top
// of search results just because it has high Hub downloads. Conversely,
// a skill with high community rating and download count should rank above
// a zero-history skill with the same text relevance.
//
// Sorting key: globalBoost (descending) - localPenalty (ascending).
// Within equal composite scores, original order is preserved (stable sort).
//
// The function is pure — it does not modify the input slice but returns
// a new sorted slice.
func RerankByLocalHistory(results []HubSearchResult, localSkills []corelib.NLSkillEntry) []HubSearchResult {
	if len(results) == 0 {
		return results
	}

	// Build lookup by name.
	var skillMap map[string]*corelib.NLSkillEntry
	if len(localSkills) > 0 {
		skillMap = make(map[string]*corelib.NLSkillEntry, len(localSkills))
		for i := range localSkills {
			skillMap[localSkills[i].Name] = &localSkills[i]
		}
	}

	// Copy results to avoid mutating the input.
	ranked := make([]HubSearchResult, len(results))
	copy(ranked, results)

	// Stable sort preserves original order for results with equal scores.
	sort.SliceStable(ranked, func(i, j int) bool {
		// Composite score = globalBoost - localPenalty.
		// Higher composite = better ranking (sort ascending by negated score).
		si := globalBoost(ranked[i]) - float64(localPenaltyByName(ranked[i].Name, skillMap))
		sj := globalBoost(ranked[j]) - float64(localPenaltyByName(ranked[j].Name, skillMap))
		return si > sj // descending: higher composite score first
	})

	return ranked
}

// globalBoost computes a [0, 2] boost score from global Hub signals:
// - AvgRating (1-5 scale, normalized to [0, 1], weight 0.6)
// - Downloads (log-scale normalized to [0, 1], weight 0.4)
//
// A skill with AvgRating=4.5 and 1000+ downloads gets boost ≈ 1.7.
// A skill with AvgRating=0 and 0 downloads gets boost = 0.
func globalBoost(r HubSearchResult) float64 {
	// Normalize rating: (rating - 1) / 4 maps [1,5] → [0,1]. Rating=0 means unrated → 0.
	ratingNorm := 0.0
	if r.AvgRating > 0 {
		ratingNorm = (r.AvgRating - 1.0) / 4.0
		if ratingNorm < 0 {
			ratingNorm = 0
		}
		if ratingNorm > 1 {
			ratingNorm = 1
		}
	}

	// Normalize downloads: log10(downloads+1) / log10(10001) maps [0,10000] → [0,1].
	// log10(10001) ≈ 4.0, so 10000 downloads → 1.0, 100 downloads → 0.5, 1 download → 0.075.
	downloadsNorm := 0.0
	if r.Downloads > 0 {
		dl := math.Log10(float64(r.Downloads) + 1)
		downloadsNorm = dl / 4.0
		if downloadsNorm > 1 {
			downloadsNorm = 1
		}
	}

	return ratingNorm*1.2 + downloadsNorm*0.8
}

// LocalPenalty returns a penalty score [0, 3] for a skill based on
// local execution history. 0 = no penalty (no local data or good history),
// higher = worse local experience. Exported for use by GUI search reranking.
//
// Penalty levels:
//
//	0 — not enough data, or success rate >= 50%
//	1 — success rate < 50% (skill has been tried and mostly fails)
//	2 — success rate == 0% with >= 3 uses (consistently broken)
//	3 — skill is disabled / needs_review / needs_setup
func LocalPenalty(s *corelib.NLSkillEntry) int {
	if s == nil {
		return 0
	}
	switch s.Status {
	case "disabled", "needs_review", "needs_setup":
		return 3
	}
	if s.UsageCount < 2 {
		return 0
	}
	successRate := float64(s.SuccessCount) / float64(s.UsageCount)
	if successRate == 0 && s.UsageCount >= 3 {
		return 2
	}
	if successRate < 0.5 {
		return 1
	}
	return 0
}

// localPenaltyByName looks up a skill by name and returns its penalty.
func localPenaltyByName(name string, skillMap map[string]*corelib.NLSkillEntry) int {
	if s, ok := skillMap[name]; ok {
		return LocalPenalty(s)
	}
	return 0
}
