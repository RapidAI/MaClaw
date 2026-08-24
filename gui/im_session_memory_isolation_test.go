package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestIsolatedAssistantSessionStaticMemoryExcludesSharedAndOtherOwners(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	projectOwner := projectSessionOwnerID(`D:\workprj\isolated`)
	for _, entry := range []memory.Entry{
		{Content: "shared desktop fact", Category: memory.CategoryUserFact},
		{Content: "local desktop fact", Category: memory.CategoryUserFact, OwnerID: desktopUserID},
		{Content: "other project fact", Category: memory.CategoryUserFact, OwnerID: projectSessionOwnerID(`D:\workprj\other`)},
		{Content: "this session fact", Category: memory.CategoryUserFact, OwnerID: projectOwner},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	var out strings.Builder
	(&IMMessageHandler{memoryStore: store}).generateStaticMemorySection(&out, true, projectOwner)
	got := out.String()
	for _, forbidden := range []string{"this session fact", "shared desktop fact", "local desktop fact", "other project fact"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("catalog-only static prompt must not dump warehouse text %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, memory.PromptSectionUserMemory) {
		t.Fatalf("isolated session should still receive the memory section header: %q", got)
	}
}

func TestArchiveCollectionRequiresProjectSessionOwner(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	projectPath := `D:\workprj\isolated`
	owner := projectSessionOwnerID(projectPath)
	for _, entry := range []memory.Entry{
		{Content: "owned project result", Category: memory.CategoryTaskArtifact, Scope: memory.ScopeProject, OwnerID: owner, Tags: []string{projectPath}},
		{Content: "foreign project result", Category: memory.CategoryTaskArtifact, Scope: memory.ScopeProject, OwnerID: projectSessionOwnerID(`D:\workprj\other`), Tags: []string{projectPath}},
		{Content: "legacy shared result", Category: memory.CategoryTaskArtifact, Scope: memory.ScopeProject, Tags: []string{projectPath}},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}

	entries := NewArchiveService(store, nil, nil).collectProjectEntries(projectPath, owner)
	if len(entries) != 1 || entries[0].Content != "owned project result" {
		t.Fatalf("archive collection crossed an owner boundary: %+v", entries)
	}
}
