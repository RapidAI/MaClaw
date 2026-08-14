package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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

func TestPromptSkillIndexEntriesFiltersShellBrowserAutomationSkills(t *testing.T) {
	skills := []NLSkillDefinition{
		{
			Name:        "zhihu-poster",
			Status:      "active",
			RequiresGUI: true,
			Triggers:    []string{"zhihu", "知乎"},
			Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
				"command": `python post.py article --title "{{title}}" --file "{{file}}" --screenshot`,
			}}},
		},
		{Name: "normal", Status: "active", UsageCount: 1},
	}

	got := promptSkillIndexEntries(skills, 10)
	if len(got) != 1 || got[0].Name != "normal" {
		t.Fatalf("unexpected skill index entries: %+v", got)
	}
}

func TestPromptSkillIndexEntriesFiltersBrowserToolsetSkills(t *testing.T) {
	skills := []NLSkillDefinition{
		{Name: "browser-wrapper", Status: "active", RequiresToolsets: []string{"browser"}},
		{Name: "normal", Status: "active"},
	}

	got := promptSkillIndexEntries(skills, 10)
	if len(got) != 1 || got[0].Name != "normal" {
		t.Fatalf("unexpected skill index entries: %+v", got)
	}
}

func TestPromptSkillIndexEntriesSkipsAgentGuidedWorkflowEvenWhenStaleActive(t *testing.T) {
	skills := []NLSkillDefinition{
		{Name: "Book-PDF", Status: "active", ExecutionClass: "agent_guided_workflow"},
		{Name: "pdf-word", Status: "active", ExecutionClass: "native_skill"},
	}
	got := promptSkillIndexEntries(skills, 10)
	if len(got) != 1 || got[0].Name != "pdf-word" {
		t.Fatalf("promptSkillIndexEntries() = %#v, want only runnable native skill", got)
	}
}
