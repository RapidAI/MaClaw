package memory

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryIndexForPromptFormatsStats(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "Project uses pnpm test", Category: CategoryProjectKnowledge, Tags: []string{"pnpm"}, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	out := store.MemoryIndexForPrompt(false, "", "entries")
	if !strings.Contains(out, "1 entries") || !strings.Contains(out, "pnpm") {
		t.Fatalf("unexpected memory index: %q", out)
	}
}

func TestSceneIndexForPromptAppliesProjectFilter(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	alpha := filepath.Join(t.TempDir(), "alpha")
	beta := filepath.Join(t.TempDir(), "beta")
	if err := store.Save(Entry{Title: "Alpha plan", Content: "Alpha workflow artifact", Category: CategoryTaskArtifact, SourceURL: filepath.Join(alpha, "plan.md"), Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(Entry{Title: "Beta plan", Content: "Beta workflow artifact", Category: CategoryTaskArtifact, SourceURL: filepath.Join(beta, "plan.md"), Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	out := store.SceneIndexForPrompt(true, alpha, 10, 3, 2)
	if !strings.Contains(out, "Alpha") || strings.Contains(out, "Beta") {
		t.Fatalf("unexpected filtered scene index: %q", out)
	}
}

func TestUserFactSummaryForPrompt(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "User prefers concise Chinese updates", Category: CategoryUserFact, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	out := store.UserFactSummaryForPrompt(UserFactSummaryPromptOptions{
		Header: "## User Info",
		Prefix: "Owner: ",
	})
	if !strings.Contains(out, "## User Info\nOwner: User prefers concise Chinese updates\n") {
		t.Fatalf("unexpected user fact prompt: %q", out)
	}

	templated := store.UserFactSummaryForPrompt(UserFactSummaryPromptOptions{Template: "[facts] %s\n"})
	if !strings.HasPrefix(templated, "[facts] User prefers concise Chinese updates") {
		t.Fatalf("unexpected templated user fact prompt: %q", templated)
	}
}

func TestStaticMemorySectionForPrompt(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "User likes careful memory consolidation", Category: CategoryUserFact, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}

	out := store.StaticMemorySectionForPrompt(StaticMemoryPromptOptions{
		UserFacts:         UserFactSummaryPromptOptions{Header: "## User Memory", Prefix: "User info: "},
		IncludeRecallHint: true,
		IncludeGuide:      true,
		Guide:             "## Guide\nSave stable facts.",
		GuidePrefix:       "\n",
	})
	for _, want := range []string{"## User Memory", "User info: User likes careful memory consolidation", "memory(action: recall", "## Guide", "Save stable facts."} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in static memory prompt: %q", want, out)
		}
	}
}

func TestStaticMemorySectionForPromptStrictOwnerExcludesSharedFacts(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	if err := store.Save(Entry{Content: "shared desktop fact", Category: CategoryUserFact, Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveForUser(Entry{Content: "group-only fact", Category: CategoryUserFact, Status: StatusActive}, "group-owner"); err != nil {
		t.Fatal(err)
	}

	opts := StaticMemoryPromptOptions{UserFacts: UserFactSummaryPromptOptions{OwnerID: "group-owner", StrictOwner: true}}
	out := store.StaticMemorySectionForPrompt(opts)
	if strings.Contains(out, "shared desktop fact") || !strings.Contains(out, "group-only fact") {
		t.Fatalf("strict owner summary = %q", out)
	}
}

func TestMemoryPromptProfiles(t *testing.T) {
	static := StaticUserMemoryPromptOptions("## User Memory", true, "## Guide")
	if !static.IncludeRecallHint || !static.IncludeGuide || static.UserFacts.Prefix == "" {
		t.Fatalf("unexpected static profile: %+v", static)
	}

	core := CoreAgentProactivePromptOptions()
	if !core.CatalogOnly || !core.IncludeMemoryIndex || core.Recall.MaxEntries != 0 || core.Recall.EntityLimit != 3 {
		t.Fatalf("unexpected core proactive profile: %+v", core)
	}

	im := IMProactivePromptOptions("/tmp/project", true)
	if !im.IncludeMemoryIndex || !im.CatalogOnly || im.IncludeDerivedFacts || !im.Recall.StrictProject || im.Recall.ProjectPath != "/tmp/project" || im.Recall.MaxEntries != 0 {
		t.Fatalf("unexpected IM proactive profile: %+v", im)
	}

	ve := VEProactivePromptOptions()
	if !ve.CatalogOnly || !ve.IncludeMemoryIndex || !ve.Recall.IncludeUserProfile || ve.Recall.MaxEntries != 0 {
		t.Fatalf("unexpected VE proactive profile: %+v", ve)
	}

	btw := BtwProactivePromptOptions("/tmp/project", "## Recall")
	if !btw.CatalogOnly || btw.Recall.ProjectPath != "/tmp/project" || btw.RecallEntries.Header != "## Recall" || btw.Recall.MaxEntries != 0 {
		t.Fatalf("unexpected /btw proactive profile: %+v", btw)
	}

	footer := CatalogOnlyWorkingSetFooter()
	if strings.Contains(footer, "必须先") {
		t.Fatal("catalog footer must not mandate warehouse-first")
	}
	if !strings.Contains(footer, "仅当本轮工具列表里有") {
		t.Fatal("catalog footer should gate retrieval on the current tool list")
	}
}
