package main

import (
	"testing"
	"time"
)

func TestPromptSkillIndexEntriesFiltersAndRanksActiveSkills(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	skills := []NLSkillDefinition{
		{Name: "inactive", Status: "disabled", UsageCount: 100, SuccessRate: 1},
		{Name: "beta", Status: "active", UsageCount: 2, SuccessRate: 1, LastUsedAt: &yesterday},
		{Name: "alpha", Status: "active", UsageCount: 2, SuccessRate: 1, LastUsedAt: &now},
		{Name: "gamma", Status: "active", UsageCount: 1, SuccessRate: 1},
		{Name: "", Status: "active", UsageCount: 99, SuccessRate: 1},
	}

	got := promptSkillIndexEntries(skills, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 skills, got %d: %+v", len(got), got)
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("unexpected prompt skill order: %+v", got)
	}
}

func TestPromptSkillIndexEntriesReturnsNilWhenLimitDisabled(t *testing.T) {
	got := promptSkillIndexEntries([]NLSkillDefinition{{Name: "alpha", Status: "active"}}, 0)
	if got != nil {
		t.Fatalf("expected nil when limit is disabled, got %+v", got)
	}
}
