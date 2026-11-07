package guiapp

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestStoreMutationInvalidatesRecallAndScopesRetraction(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	ownerA := "desktop-user:project-a"
	ownerB := "desktop-user:project-b"
	entry := memory.Entry{
		Content:    "Project A API endpoint is https://api.example.com with timeout 30 seconds",
		Category:   memory.CategoryProjectKnowledge,
		OwnerID:    ownerA,
		SourceType: "online_extraction",
	}
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	app.currentMemoryRuntime()
	entry = store.List(memory.CategoryProjectKnowledge, "")[0]

	result := memory.HandleTool(store, map[string]interface{}{"action": "recall", "query": "api.example.com"}, memory.ToolOptions{OwnerID: ownerA, LoopID: "loop-a"})
	if !strings.Contains(result, "Recalled") || store.Paginator().ActiveCursorsForUser(ownerA) != 1 {
		t.Fatalf("failed to establish recall cursor: %q", result)
	}
	entry.Status = memory.StatusSuperseded
	entry.Stale = true
	now := time.Now()
	entry.InvalidAt = &now
	if err := store.UpdateEntriesByID([]memory.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if got := store.Paginator().ActiveCursorsForUser(ownerA); got != 0 {
		t.Fatalf("automatic store mutation left %d stale cursors", got)
	}
	if got := agentRetractionText(handler, ownerA); !strings.Contains(got, entry.Content) {
		t.Fatalf("owning session missing retraction: %q", got)
	}
	if got := agentRetractionText(handler, ownerB); strings.Contains(got, entry.Content) {
		t.Fatalf("other owner received retraction: %q", got)
	}
}

func agentRetractionText(handler *IMMessageHandler, ownerID string) string {
	retraction := handler.LoadMemoryRetractionsForUser(ownerID)
	if retraction == nil {
		return ""
	}
	var b strings.Builder
	for _, item := range retraction.Items {
		b.WriteString(item.Content)
	}
	return b.String()
}

func TestStoreResetEventInvalidatesFrozenSnapshot(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.Save(memory.Entry{Content: "User prefers concise Chinese answers in technical discussions", Category: memory.CategoryUserFact}); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	app := &App{memoryStore: store, imHandler: handler, testHomeDir: t.TempDir()}
	app.currentMemoryRuntime()
	if handler.cachedStaticMemorySnapshot(desktopUserID) == "" {
		t.Fatal("missing prewarmed snapshot")
	}
	if err := store.RestoreEntriesSnapshot(nil); err != nil {
		t.Fatal(err)
	}
	if got := handler.cachedStaticMemorySnapshot(desktopUserID); got != "" {
		t.Fatalf("store reset retained frozen snapshot: %q", got)
	}
}

func TestRetiredRuntimeMutationTargetsOldHandlerWithoutInvalidatingNewStore(t *testing.T) {
	oldStore, err := memory.NewStore(filepath.Join(t.TempDir(), "old.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer oldStore.Stop()
	newStore, err := memory.NewStore(filepath.Join(t.TempDir(), "new.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer newStore.Stop()
	owner := "desktop-user:project-a"
	handler := &IMMessageHandler{memoryStore: oldStore}
	app := &App{memoryStore: oldStore, imHandler: handler, testHomeDir: t.TempDir()}
	app.installMemoryRuntime(oldStore, nil, "old")
	oldEntry := memory.Entry{Content: "Old runtime API endpoint is https://old.example.com with timeout 30 seconds", Category: memory.CategoryProjectKnowledge, OwnerID: owner}
	if err := oldStore.Save(oldEntry); err != nil {
		t.Fatal(err)
	}
	oldEntry = oldStore.List(memory.CategoryProjectKnowledge, "")[0]
	if err := newStore.Save(memory.Entry{Content: "New runtime API endpoint is https://new.example.com with timeout 30 seconds", Category: memory.CategoryProjectKnowledge, OwnerID: owner}); err != nil {
		t.Fatal(err)
	}
	app.memoryStore = newStore
	app.installMemoryRuntime(newStore, nil, "new")
	memory.HandleTool(newStore, map[string]interface{}{"action": "recall", "query": "new.example.com"}, memory.ToolOptions{OwnerID: owner, LoopID: "new-loop"})
	if got := newStore.Paginator().ActiveCursorsForUser(owner); got != 1 {
		t.Fatalf("new store cursor setup = %d", got)
	}

	oldEntry.Status = memory.StatusSuperseded
	if err := oldStore.UpdateEntriesByID([]memory.Entry{oldEntry}); err != nil {
		t.Fatal(err)
	}
	if got := agentRetractionText(handler, owner); !strings.Contains(got, oldEntry.Content) {
		t.Fatalf("old runtime handler missed retraction: %q", got)
	}
	if got := newStore.Paginator().ActiveCursorsForUser(owner); got != 1 {
		t.Fatalf("old runtime event invalidated new store cursors: %d", got)
	}
}
