package guiapp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
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
	if !strings.Contains(got, "this session fact") {
		t.Fatalf("isolated session should include its own user facts: %q", got)
	}
	for _, forbidden := range []string{"shared desktop fact", "local desktop fact", "other project fact"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("isolated static prompt included another owner's fact %q: %q", forbidden, got)
		}
	}
	if !strings.Contains(got, memory.PromptSectionUserMemory) {
		t.Fatalf("isolated session should still receive the memory section header: %q", got)
	}
}

func TestPromptSelfIdentityFollowsSessionOwner(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	projectOwner := projectSessionOwnerID(`D:\workprj\isolated`)
	for _, entry := range []memory.Entry{
		{Content: "desktop identity", Category: memory.CategorySelfIdentity, OwnerID: desktopUserID},
		{Content: "shared identity", Category: memory.CategorySelfIdentity},
		{Content: "session identity", Category: memory.CategorySelfIdentity, OwnerID: projectOwner},
		{Content: "other identity", Category: memory.CategorySelfIdentity, OwnerID: projectSessionOwnerID(`D:\workprj\other`)},
	} {
		if err := store.Save(entry); err != nil {
			t.Fatal(err)
		}
	}
	h := &IMMessageHandler{memoryStore: store}
	desktop := h.buildSystemPromptBase(false, "hello")
	for _, want := range []string{"desktop identity", "shared identity"} {
		if !strings.Contains(desktop, want) {
			t.Fatalf("desktop prompt missing %q", want)
		}
	}
	for _, forbidden := range []string{"session identity", "other identity"} {
		if strings.Contains(desktop, forbidden) {
			t.Fatalf("desktop prompt included %q", forbidden)
		}
	}
	isolated := h.buildSystemPromptBaseWithExperienceContext(false, lifecycle.EventContext{}, &LoopContext{UserID: projectOwner}, "hello")
	if !strings.Contains(isolated, "session identity") {
		t.Fatal("isolated prompt missing its own identity")
	}
	for _, forbidden := range []string{"desktop identity", "shared identity", "other identity"} {
		if strings.Contains(isolated, forbidden) {
			t.Fatalf("isolated prompt included %q", forbidden)
		}
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

func TestDesktopMemoryListKeepsProjectSessionsAndDropsInactive(t *testing.T) {
	projectOwner := projectSessionOwnerID(`D:\workprj\isolated`)
	visible := filterDesktopManageableMemories([]memory.Entry{
		{Content: "desktop", OwnerID: desktopUserID},
		{Content: "shared"},
		{Content: "project", OwnerID: projectOwner},
		{Content: "other", OwnerID: "someone-else"},
		{Content: "old desktop", OwnerID: desktopUserID, Status: memory.StatusSuperseded},
		{Content: "dormant project", OwnerID: projectOwner, Status: memory.StatusDormant},
		{Content: "foreign boundary", Boundary: &memory.MemoryBoundary{OwnerID: "someone-else"}},
	})
	got := map[string]bool{}
	for _, entry := range visible {
		got[entry.Content] = true
	}
	for _, want := range []string{"desktop", "shared", "project"} {
		if !got[want] {
			t.Fatalf("missing %q in %+v", want, visible)
		}
	}
	for _, forbidden := range []string{"other", "old desktop", "dormant project", "foreign boundary"} {
		if got[forbidden] {
			t.Fatalf("included %q in %+v", forbidden, visible)
		}
	}
}
