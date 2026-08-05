package memory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestStrictOwnerOnlyAdmitsOwnAndArchivedExperience(t *testing.T) {
	owner := "desktop-user:D:/workprj/isolated"
	entries := []Entry{
		{ID: "own", OwnerID: owner, Content: "own session memory"},
		{ID: "shared", Content: "legacy shared memory"},
		{ID: "other", OwnerID: "desktop-user:D:/workprj/other", Content: "other session memory"},
		{ID: "archive", OwnerID: "desktop-user:D:/workprj/other", Content: "distilled final experience", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, SourceType: "archived_experience", Tags: []string{"archived_experience"}},
	}
	filtered := filterEntriesForProactiveOwner(entries, ProactiveRecallOptions{OwnerID: owner, StrictOwner: true, AllowArchivedExperience: true})
	if len(filtered) != 2 || filtered[0].ID != "own" || filtered[1].ID != "archive" {
		t.Fatalf("strict owner filtering = %+v", filtered)
	}
}

func TestStrictOwnerProactiveRecallRetainsOnlyFinalArchivedExperience(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	owner := "desktop-user:D:/workprj/isolated"
	for _, entry := range []Entry{
		{ID: "shared", Content: "legacy shared memory database migration", Category: CategoryProjectKnowledge},
		{ID: "other", OwnerID: "desktop-user:D:/workprj/other", Content: "other session database migration", Category: CategoryProjectKnowledge},
		{ID: "archive", OwnerID: "desktop-user:D:/workprj/other", Content: "final archived database migration experience", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, SourceType: "archived_experience", Tags: []string{"archived_experience"}},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	got := store.RecallProactiveWithDecision(lifecycle.RetrievalDecision{
		ShouldRetrieve: true,
		Query:          "database migration",
		Budget:         lifecycle.RetrievalBudget{MaxEntries: 5},
	}, ProactiveRecallOptions{OwnerID: owner, StrictOwner: true, AllowArchivedExperience: true, MaxEntries: 5})
	if len(got) != 1 || got[0].ID != "archive" {
		t.Fatalf("strict proactive recall admitted raw cross-session memory: %+v", got)
	}
}

func TestStrictOwnerPartialPromptRecallRetainsOnlyFinalArchivedExperience(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	owner := "desktop-user:D:/workprj/isolated"
	for _, entry := range []Entry{
		{ID: "shared", Content: "legacy shared release checklist", Category: CategoryProjectKnowledge},
		{ID: "other", OwnerID: "desktop-user:D:/workprj/other", Content: "other session release checklist", Category: CategoryProjectKnowledge},
		{ID: "archive", OwnerID: "desktop-user:D:/workprj/other", Content: "final archived release checklist", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, SourceType: "archived_experience", Tags: []string{"archived_experience"}},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	_, recalled := store.ProactiveContextForPrompt("release checklist", ProactivePromptOptions{
		PartialResultsEnabled: true,
		Recall:                ProactiveRecallOptions{OwnerID: owner, StrictOwner: true, AllowArchivedExperience: true, MaxEntries: 5},
		RecallEntries:         RecallEntriesPromptOptions{Header: "Recall"},
	})
	if len(recalled) != 1 || recalled[0].ID != "archive" {
		t.Fatalf("strict partial recall admitted raw cross-session memory: %+v", recalled)
	}
}

func TestStrictOwnerPartialPromptRecallUsesDefaultBudgetForArchivedExperience(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	owner := "desktop-user:D:/workprj/isolated"
	if err := store.Save(Entry{
		ID:         "archive-default-budget",
		OwnerID:    "desktop-user:D:/workprj/other",
		Content:    "final archived deployment checklist",
		Category:   CategoryProjectKnowledge,
		Scope:      ScopeGlobal,
		SourceType: "archived_experience",
		Tags:       []string{"archived_experience"},
	}); err != nil {
		t.Fatal(err)
	}

	_, recalled := store.ProactiveContextForPrompt("deployment checklist", ProactivePromptOptions{
		PartialResultsEnabled: true,
		Recall: ProactiveRecallOptions{
			OwnerID:                 owner,
			StrictOwner:             true,
			AllowArchivedExperience: true,
		},
		RecallEntries: RecallEntriesPromptOptions{Header: "Recall"},
	})
	if len(recalled) != 1 || recalled[0].ID != "archive-default-budget" {
		t.Fatalf("strict partial recall lost archived experience with default budget: %+v", recalled)
	}
}

func TestStrictOwnerPromptRejectsUnverifiedProviderCandidate(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	owner := "desktop-user:D:/workprj/isolated"
	section, recalled := store.ProactiveContextForPrompt("release checklist", ProactivePromptOptions{
		Recall: ProactiveRecallOptions{
			OwnerID:                 owner,
			StrictOwner:             true,
			AllowArchivedExperience: true,
			Provider: promptTestProvider{candidates: []lifecycle.Candidate{{
				Entry:     lifecycle.Entry{ID: "external-other-session", EntryType: lifecycle.EntryTypeFailureSkill, Content: "other session raw provider content"},
				Relevance: 1,
			}}},
		},
		RecallEntries: RecallEntriesPromptOptions{Header: "Recall"},
	})
	if len(recalled) != 0 {
		t.Fatalf("strict prompt retained unverified provider entry: %+v", recalled)
	}
	if strings.Contains(section, "other session raw provider content") {
		t.Fatalf("strict prompt leaked unverified provider content: %q", section)
	}
}

func TestMemoryToolStrictOwnerPreventsListAndDeleteLeaks(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	owner := "desktop-user:D:/workprj/isolated"
	for _, entry := range []Entry{
		{ID: "own", OwnerID: owner, Content: "own session memory", Category: CategoryProjectKnowledge},
		{ID: "shared", Content: "legacy shared memory", Category: CategoryProjectKnowledge},
		{ID: "other", OwnerID: "desktop-user:D:/workprj/other", Content: "other session memory", Category: CategoryProjectKnowledge},
		{ID: "archive", OwnerID: "desktop-user:D:/workprj/other", Content: "distilled final experience", Category: CategoryProjectKnowledge, Scope: ScopeGlobal, SourceType: "archived_experience", Tags: []string{"archived_experience"}},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	opts := ToolOptions{OwnerID: owner, StrictOwner: true, AllowArchivedExperience: true}
	listed := HandleTool(store, map[string]interface{}{"action": "list"}, opts)
	if !strings.Contains(listed, "own session memory") || !strings.Contains(listed, "distilled final experience") || strings.Contains(listed, "legacy shared memory") || strings.Contains(listed, "other session memory") {
		t.Fatalf("strict list leaked memory: %q", listed)
	}
	deleted := HandleTool(store, map[string]interface{}{"action": "delete", "id": "other"}, opts)
	if deleted != "memory not found in this isolated conversation" {
		t.Fatalf("strict delete = %q", deleted)
	}
	if got := HandleTool(store, map[string]interface{}{"action": "delete", "id": "archive"}, opts); got != "archived experience is read-only in this isolated conversation" {
		t.Fatalf("strict delete of shared archive = %q", got)
	}
	if !store.entryIsArchivedExperience("archive") {
		t.Fatal("shared archive was deleted or no longer recognized")
	}
	if got := HandleTool(store, map[string]interface{}{"action": "delete", "id": "own"}, opts); !strings.Contains(got, "Memory deleted") {
		t.Fatalf("own delete = %q", got)
	}
}
