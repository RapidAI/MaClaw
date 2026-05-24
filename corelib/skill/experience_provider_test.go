package skill

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestExperienceProviderSearchesSkillCandidates(t *testing.T) {
	provider := NewExperienceProvider([]corelib.NLSkillEntry{
		{Name: "playwright-check", Description: "Verify browser UI with Playwright snapshots", Triggers: []string{"browser", "snapshot"}, Status: "active", SuccessCount: 3, UsageCount: 4},
		{Name: "pdf-tool", Description: "Render PDF documents", Triggers: []string{"pdf"}, Status: "active"},
	})

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:  "browser playwright snapshot",
		Types: []lifecycle.EntryType{lifecycle.EntryTypeSuccessSkill},
		Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one matching skill candidate, got %+v", candidates)
	}
	got := candidates[0]
	if got.Entry.ID == "" || got.Entry.EntryType != lifecycle.EntryTypeSuccessSkill || got.Reason != "skill_provider" {
		t.Fatalf("unexpected candidate metadata: %+v", got)
	}
	if !strings.Contains(got.Entry.Content, "playwright-check") || got.PriorityScore <= 0.2 {
		t.Fatalf("expected skill content and usage priority, got %+v", got)
	}
}

func TestExperienceProviderSearchHonorsLimitAfterRanking(t *testing.T) {
	provider := NewExperienceProvider([]corelib.NLSkillEntry{
		{Name: "weak-browser", Description: "browser workflow", Status: "active"},
		{Name: "strong-browser", Description: "browser workflow", Status: "active", SuccessCount: 8, UsageCount: 12},
	})

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{Text: "browser workflow", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Entry.ID != "skill:strong-browser" {
		t.Fatalf("expected top-ranked limited candidate, got %+v", candidates)
	}
}

func TestExperienceProviderClassifiesFailureSkill(t *testing.T) {
	provider := NewExperienceProvider([]corelib.NLSkillEntry{{
		Name:         "broken-export",
		Description:  "Export reports",
		Triggers:     []string{"export", "report"},
		Status:       "needs_review",
		FailureCount: 4,
		LastError:    "timeout while exporting report",
	}})

	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{
		Text:  "export report timeout",
		Types: []lifecycle.EntryType{lifecycle.EntryTypeFailureSkill},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected failure skill candidate, got %+v", candidates)
	}
	if candidates[0].Entry.Governance != lifecycle.GovernanceDraft || !strings.Contains(candidates[0].Entry.Content, "Recent failure") {
		t.Fatalf("expected governed failure evidence, got %+v", candidates[0].Entry)
	}
}

func TestExperienceProviderListHonorsTypesAndSkipsDisabled(t *testing.T) {
	provider := NewExperienceProvider([]corelib.NLSkillEntry{
		{Name: "active", Description: "Active workflow", Status: "active"},
		{Name: "disabled", Description: "Disabled workflow", Status: "disabled"},
		{Name: "failed", Description: "Failed workflow", Status: "needs_review", LastError: "boom"},
	})
	entries, err := provider.ListExperience(context.Background(), lifecycle.Scope{Types: []lifecycle.EntryType{lifecycle.EntryTypeFailureSkill}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "skill:failed" {
		t.Fatalf("expected only failure skill entry, got %+v", entries)
	}
}
