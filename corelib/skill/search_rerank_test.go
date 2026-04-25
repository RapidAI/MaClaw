package skill

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRerankByLocalHistory_EmptyInputs(t *testing.T) {
	// No results → no change.
	got := RerankByLocalHistory(nil, nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}

	// No local skills → original order preserved.
	results := []HubSearchResult{{Name: "a"}, {Name: "b"}}
	got = RerankByLocalHistory(results, nil)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("expected original order, got %v", names(got))
	}
}

func TestRerankByLocalHistory_DemotesLowSuccessRate(t *testing.T) {
	results := []HubSearchResult{
		{Name: "bad-skill", Source: "skillhub"},
		{Name: "good-skill", Source: "skillhub"},
		{Name: "unknown-skill", Source: "clawhub"},
	}
	localSkills := []corelib.NLSkillEntry{
		{Name: "bad-skill", UsageCount: 5, SuccessCount: 1, FailureCount: 4, Status: "active"},
		{Name: "good-skill", UsageCount: 5, SuccessCount: 4, FailureCount: 1, Status: "active"},
	}

	got := RerankByLocalHistory(results, localSkills)

	// good-skill (penalty=0) and unknown-skill (penalty=0) should come before bad-skill (penalty=1).
	if got[0].Name == "bad-skill" {
		t.Errorf("bad-skill should not be first, got order: %v", names(got))
	}
	// bad-skill should be last.
	if got[len(got)-1].Name != "bad-skill" {
		t.Errorf("bad-skill should be last, got order: %v", names(got))
	}
}

func TestRerankByLocalHistory_DemotesDisabledSkills(t *testing.T) {
	results := []HubSearchResult{
		{Name: "disabled-skill", Source: "skillhub"},
		{Name: "normal-skill", Source: "skillhub"},
	}
	localSkills := []corelib.NLSkillEntry{
		{Name: "disabled-skill", UsageCount: 3, SuccessCount: 3, Status: "disabled"},
		{Name: "normal-skill", UsageCount: 1, SuccessCount: 1, Status: "active"},
	}

	got := RerankByLocalHistory(results, localSkills)

	if got[0].Name != "normal-skill" {
		t.Errorf("normal-skill should be first, got order: %v", names(got))
	}
}

func TestRerankByLocalHistory_ConsistentlyBrokenGetsPenalty2(t *testing.T) {
	results := []HubSearchResult{
		{Name: "broken", Source: "skillhub"},
		{Name: "flaky", Source: "skillhub"},
		{Name: "fresh", Source: "clawhub"},
	}
	localSkills := []corelib.NLSkillEntry{
		{Name: "broken", UsageCount: 5, SuccessCount: 0, FailureCount: 5, Status: "active"},
		{Name: "flaky", UsageCount: 5, SuccessCount: 2, FailureCount: 3, Status: "active"},
	}

	got := RerankByLocalHistory(results, localSkills)

	// fresh (0) < flaky (1) < broken (2)
	if got[0].Name != "fresh" {
		t.Errorf("fresh should be first, got order: %v", names(got))
	}
	if got[2].Name != "broken" {
		t.Errorf("broken should be last, got order: %v", names(got))
	}
}

func TestRerankByLocalHistory_PreservesOriginalOrderForEqualPenalty(t *testing.T) {
	results := []HubSearchResult{
		{Name: "a", Source: "skillhub"},
		{Name: "b", Source: "clawhub"},
		{Name: "c", Source: "github"},
	}
	// No local data for any → all penalty=0 → original order preserved.
	got := RerankByLocalHistory(results, []corelib.NLSkillEntry{
		{Name: "other-skill", UsageCount: 10, SuccessCount: 0, Status: "active"},
	})

	if got[0].Name != "a" || got[1].Name != "b" || got[2].Name != "c" {
		t.Errorf("expected original order preserved, got: %v", names(got))
	}
}

func TestRerankByLocalHistory_DoesNotMutateInput(t *testing.T) {
	results := []HubSearchResult{
		{Name: "bad", Source: "skillhub"},
		{Name: "good", Source: "skillhub"},
	}
	localSkills := []corelib.NLSkillEntry{
		{Name: "bad", UsageCount: 5, SuccessCount: 0, FailureCount: 5, Status: "active"},
	}

	_ = RerankByLocalHistory(results, localSkills)

	// Original slice should be unchanged.
	if results[0].Name != "bad" {
		t.Error("input slice was mutated")
	}
}

func names(results []HubSearchResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Name
	}
	return out
}
