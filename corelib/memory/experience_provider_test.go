package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

func TestExperienceProviderSearchReturnsTypedCandidates(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "fact", Content: "Project API endpoint is https://api.example.com", Category: CategoryProjectKnowledge, Tags: []string{"api"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{ID: "failure", Content: "Avoid repeating deploy after timeout failure", Category: CategoryProjectKnowledge, Tags: []string{"deploy", "failure"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	provider := NewExperienceProvider(store)
	candidates, err := provider.SearchExperience(context.Background(), lifecycle.Query{Text: "api deploy timeout", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	foundFailure := false
	for _, candidate := range candidates {
		if candidate.Entry.ID == "failure" && candidate.Entry.EntryType == lifecycle.EntryTypeFailureSkill && candidate.TokenCost > 0 {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatalf("expected typed failure candidate, got %+v", candidates)
	}
}

func TestExperienceProviderListFiltersScopeAndType(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "alpha", Content: "Alpha project API", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\work\alpha`}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{ID: "beta", Content: "Beta project API", Category: CategoryProjectKnowledge, Scope: ScopeProject, Tags: []string{`D:\work\beta`}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{ID: "skill", Content: "Use pnpm test", Category: CategoryInstruction, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	provider := NewExperienceProvider(store)
	entries, err := provider.ListExperience(context.Background(), lifecycle.Scope{Boundary: lifecycle.Boundary{ProjectPath: `D:\work\alpha`}, Types: []lifecycle.EntryType{lifecycle.EntryTypeFactual}})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != "alpha" {
		t.Fatalf("expected only alpha factual entry, got %+v", entries)
	}
}

func TestExperienceProviderUpdateUtilityAdjustsMemory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(Entry{ID: "utility", Content: "Utility update target", Category: CategoryProjectKnowledge, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	provider := NewExperienceProvider(store)
	if err := provider.UpdateUtility(context.Background(), lifecycle.UtilityUpdate{EntryID: "utility", Helpful: true, Success: true}); err != nil {
		t.Fatal(err)
	}
	entries := store.SearchDirectByID("utility")
	if len(entries) != 1 || entries[0].AccessCount == 0 || entries[0].Strength == 0 {
		t.Fatalf("expected utility to update memory, got %+v", entries)
	}
}

func TestExperienceProviderUpdateUtilityPersistsThroughSQLiteBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	backend := newTestSQLiteBackend(t)
	store.SetBackend(backend, SyncConfig{Enabled: false})
	entry := Entry{ID: "utility-sqlite", Content: "  preserve utility text  ", Category: CategoryProjectKnowledge, Status: StatusActive, AccessCount: 1, Strength: 1}
	if err := store.UpsertEntriesByID([]Entry{entry}); err != nil {
		t.Fatalf("UpsertEntriesByID: %v", err)
	}

	provider := NewExperienceProvider(store)
	if err := provider.UpdateUtility(context.Background(), lifecycle.UtilityUpdate{EntryID: "utility-sqlite", Helpful: true, Success: true}); err != nil {
		t.Fatal(err)
	}
	loaded, err := backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one loaded entry, got %+v", loaded)
	}
	if loaded[0].AccessCount != 2 || loaded[0].Strength != 2 {
		t.Fatalf("expected persisted utility update, got %+v", loaded[0])
	}
	if loaded[0].Content != "preserve utility text" {
		t.Fatalf("expected stored content unchanged after metadata update, got %q", loaded[0].Content)
	}

	if err := provider.UpdateUtility(context.Background(), lifecycle.UtilityUpdate{EntryID: "utility-sqlite", Harmful: true}); err != nil {
		t.Fatal(err)
	}
	loaded, err = backend.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll harmful: %v", err)
	}
	if loaded[0].Strength != 1 {
		t.Fatalf("expected harmful utility decrement persisted, got %+v", loaded[0])
	}
}
