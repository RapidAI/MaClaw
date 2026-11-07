package guiapp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestMemoryFactsOverlayReflectsWritesWithoutRebuildingFrozenGuide(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	if err := store.SaveWithWriteContext(memory.Entry{Content: "User lives in Beijing and works on desktop software", Category: memory.CategoryUserFact}, memory.WriteContext{OwnerID: desktopUserID}, ""); err != nil {
		t.Fatal(err)
	}
	handler := &IMMessageHandler{memoryStore: store}
	handler.WarmFrozenMemorySnapshot(desktopUserID)
	frozen := handler.cachedStaticMemorySnapshot(desktopUserID)
	if frozen == "" {
		t.Fatal("missing frozen memory guide")
	}
	if err := store.SaveWithWriteContext(memory.Entry{Content: "User prefers concise Chinese answers with concrete examples", Category: memory.CategoryPreference}, memory.WriteContext{OwnerID: desktopUserID}, ""); err != nil {
		t.Fatal(err)
	}
	var current strings.Builder
	handler.appendMemorySection(&current, true, desktopUserID, lifecycle.EventContext{}, "hello")
	if !strings.Contains(current.String(), "concise Chinese answers") {
		t.Fatalf("new preference missing from current prompt: %q", current.String())
	}
	if got := handler.cachedStaticMemorySnapshot(desktopUserID); got != frozen {
		t.Fatal("dynamic fact write rebuilt the frozen guide")
	}

	old := store.List(memory.CategoryUserFact, "Beijing")[0]
	old.Content = "User lives in Shanghai and works on desktop software"
	if err := store.UpdateEntriesByID([]memory.Entry{old}); err != nil {
		t.Fatal(err)
	}
	current.Reset()
	handler.appendMemorySection(&current, false, desktopUserID, lifecycle.EventContext{}, "where do I live")
	if strings.Contains(current.String(), "Beijing") || !strings.Contains(current.String(), "Shanghai") {
		t.Fatalf("updated fact overlay retained stale content: %q", current.String())
	}
}
