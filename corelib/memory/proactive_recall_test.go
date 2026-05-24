package memory

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestRecallProactiveFiltersProfileByDefault(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	entries := []Entry{
		{Content: "User prefers concise answers about React migration", Category: CategoryUserFact, Tags: []string{"react", "migration"}, Status: StatusActive},
		{Content: "React migration project uses pnpm test for validation", Category: CategoryProjectKnowledge, Tags: []string{"react", "migration", "pnpm"}, Status: StatusActive},
		{Content: "Conversation summary about React migration", Category: CategoryConversationSummary, Tags: []string{"react"}, Status: StatusActive},
	}
	for _, entry := range entries {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	got := store.RecallProactive("react migration pnpm", ProactiveRecallOptions{MaxEntries: 10})
	for _, entry := range got {
		if MapToCanonical(entry.Category) == CategoryUserFact || MapToCanonical(entry.Category) == CategoryConversationSummary {
			t.Fatalf("unexpected filtered category in proactive recall: %+v", entry)
		}
	}
	if len(got) == 0 {
		t.Fatal("expected project memory")
	}
}

func TestRecallProactiveCanIncludeProfile(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "User prefers concise answers about Kubernetes", Category: CategoryUserFact, Tags: []string{"kubernetes", "concise"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	got := store.RecallProactive("kubernetes concise preference", ProactiveRecallOptions{IncludeUserProfile: true, MaxEntries: 5})
	if len(got) == 0 {
		t.Fatal("expected profile memory when IncludeUserProfile is true")
	}
}

func TestRecallProactiveWithDecisionBalancesEntryTypes(t *testing.T) {
	entries := []Entry{
		{ID: "fact-1", Content: "API endpoint is https://api.example.com", Category: CategoryProjectKnowledge},
		{ID: "fact-2", Content: "API port is 8080", Category: CategoryProjectKnowledge},
		{ID: "fail-1", Content: "Avoid repeating deploy after timeout failure", Category: CategoryProjectKnowledge, Tags: []string{"failure"}},
		{ID: "skill-1", Content: "Use pnpm test before delivery", Category: CategoryInstruction},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "api deploy pnpm",
		Types:          []lifecycle.EntryType{lifecycle.EntryTypeFailureSkill, lifecycle.EntryTypeSuccessSkill, lifecycle.EntryTypeFactual},
		Budget: lifecycle.RetrievalBudget{
			MaxEntries: 3,
			Quotas: map[lifecycle.EntryType]int{
				lifecycle.EntryTypeFailureSkill: 1,
				lifecycle.EntryTypeSuccessSkill: 1,
				lifecycle.EntryTypeFactual:      1,
			},
		},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(got), got)
	}
	if got[0].ID != "fail-1" || got[1].ID != "skill-1" || got[2].ID != "fact-1" {
		t.Fatalf("expected balanced type selection, got ids %q %q %q", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestRecallProactiveWithDecisionBackfillsEmptyQuotas(t *testing.T) {
	entries := []Entry{
		{ID: "fact-1", Content: "first factual memory", Category: CategoryProjectKnowledge},
		{ID: "fact-2", Content: "second factual memory", Category: CategoryProjectKnowledge},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "facts",
		Types:          []lifecycle.EntryType{lifecycle.EntryTypeFailureSkill},
		Budget: lifecycle.RetrievalBudget{
			MaxEntries: 2,
			Quotas:     map[lifecycle.EntryType]int{lifecycle.EntryTypeFailureSkill: 1},
		},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 2 || got[0].ID != "fact-1" || got[1].ID != "fact-2" {
		t.Fatalf("expected original-order backfill, got %+v", got)
	}
}

func TestRecallProactiveWithDecisionReranksByUtility(t *testing.T) {
	entries := []Entry{
		{ID: "low", Content: "same factual memory", Category: CategoryProjectKnowledge, Strength: 0.1, AccessCount: 1},
		{ID: "high", Content: "same factual memory", Category: CategoryProjectKnowledge, Strength: 5, AccessCount: 10},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "same factual memory",
		Types:          []lifecycle.EntryType{lifecycle.EntryTypeFactual},
		Budget: lifecycle.RetrievalBudget{
			MaxEntries: 1,
			Quotas:     map[lifecycle.EntryType]int{lifecycle.EntryTypeFactual: 1},
		},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 1 || got[0].ID != "high" {
		t.Fatalf("expected utility-aware rerank to select high utility entry, got %+v", got)
	}
}

func TestRecallProactiveWithDecisionFiltersBoundary(t *testing.T) {
	entries := []Entry{
		{ID: "alpha", Content: "project scoped memory", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\work\alpha`}},
		{ID: "beta", Content: "project scoped memory", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\work\beta`}},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "project scoped memory",
		Boundary:       lifecycle.Boundary{ProjectPath: `D:\work\alpha`},
		Budget:         lifecycle.RetrievalBudget{MaxEntries: 2},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("expected boundary filter to keep alpha only, got %+v", got)
	}
}

func TestRecallProactiveWithDecisionSkipsRedundantCandidates(t *testing.T) {
	entries := []Entry{
		{ID: "api-low", Content: "API endpoint is https://api.example.com", Category: CategoryProjectKnowledge, Strength: 0.1},
		{ID: "api-high", Content: "API endpoint is https://api.example.com", Category: CategoryProjectKnowledge, Strength: 5},
		{ID: "backup", Content: "PostgreSQL backup window is Sunday", Category: CategoryProjectKnowledge},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "api backup",
		Types:          []lifecycle.EntryType{lifecycle.EntryTypeFactual},
		Budget: lifecycle.RetrievalBudget{
			MaxEntries: 2,
			Quotas:     map[lifecycle.EntryType]int{lifecycle.EntryTypeFactual: 2},
		},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %+v", got)
	}
	if got[0].ID != "api-high" || got[1].ID != "backup" {
		t.Fatalf("expected redundant API memory to be skipped, got %+v", got)
	}
}

func TestRecallProactiveWithDecisionAllowsRedundantBackfillWhenNeeded(t *testing.T) {
	entries := []Entry{
		{ID: "api-low", Content: "API endpoint is https://api.example.com", Category: CategoryProjectKnowledge, Strength: 0.1},
		{ID: "api-high", Content: "API endpoint is https://api.example.com", Category: CategoryProjectKnowledge, Strength: 5},
	}
	decision := lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "api",
		Types:          []lifecycle.EntryType{lifecycle.EntryTypeFactual},
		Budget: lifecycle.RetrievalBudget{
			MaxEntries: 2,
			Quotas:     map[lifecycle.EntryType]int{lifecycle.EntryTypeFactual: 2},
		},
	}

	got := selectBalancedProactiveEntries(entries, decision)
	if len(got) != 2 || got[0].ID != "api-high" || got[1].ID != "api-low" {
		t.Fatalf("expected redundant memory to backfill only when needed, got %+v", got)
	}
}
