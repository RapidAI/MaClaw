package skill

import (
	"sort"

	"github.com/RapidAI/CodeClaw/corelib"
)

// RerankByLocalHistory reorders search results based on local execution
// history. Skills with low local success rates are demoted; skills that
// are locally disabled or under review are pushed to the bottom.
//
// This closes the signal gap between execution results and discovery:
// a skill that consistently fails locally should not appear at the top
// of search results just because it has high Hub downloads.
//
// The function is pure — it does not modify the input slice but returns
// a new sorted slice. Results without local history are left in their
// original relative order.
func RerankByLocalHistory(results []HubSearchResult, localSkills []corelib.NLSkillEntry) []HubSearchResult {
	if len(results) == 0 || len(localSkills) == 0 {
		return results
	}

	// Build lookup by name.
	skillMap := make(map[string]*corelib.NLSkillEntry, len(localSkills))
	for i := range localSkills {
		skillMap[localSkills[i].Name] = &localSkills[i]
	}

	// Copy results to avoid mutating the input.
	ranked := make([]HubSearchResult, len(results))
	copy(ranked, results)

	// Stable sort preserves original order for results with equal scores.
	sort.SliceStable(ranked, func(i, j int) bool {
		si := localPenaltyByName(ranked[i].Name, skillMap)
		sj := localPenaltyByName(ranked[j].Name, skillMap)
		return si < sj
	})

	return ranked
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
