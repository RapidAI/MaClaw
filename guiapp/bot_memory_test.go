package guiapp

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestAppBotMemoryBindingsCRUD(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}

	saved, err := app.SaveBotMemory("bot_x", "likes dark mode", "preference", []string{"ui"})
	if err != nil {
		t.Fatal(err)
	}
	if saved == nil || saved.BotID != "bot_x" || saved.Content != "likes dark mode" {
		t.Fatalf("saved=%#v", saved)
	}
	listed, err := app.ListBotMemories("bot_x", "", "dark")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	if err := app.UpdateBotMemory("bot_x", saved.ID, "likes dark mode always", "preference", []string{"ui"}); err != nil {
		t.Fatal(err)
	}
	if err := app.DeleteBotMemory("bot_y", saved.ID); err == nil {
		t.Fatal("cross-bot delete should fail")
	}
	if err := app.DeleteBotMemory("bot_x", saved.ID); err != nil {
		t.Fatal(err)
	}
	left, err := app.ListBotMemories("bot_x", "", "")
	if err != nil || len(left) != 0 {
		t.Fatalf("left=%#v err=%v", left, err)
	}
}

func TestAppDeleteBotMemoriesBulk(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	a, err := app.SaveBotMemory("bot_x", "one", "preference", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := app.SaveBotMemory("bot_x", "two", "preference", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := app.DeleteBotMemories("bot_x", []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if out["deleted"] != 2 {
		t.Fatalf("out=%#v", out)
	}
}

func TestAppDeleteBotMemoriesBulkIsAtomic(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	a, err := app.SaveBotMemory("bot_x", "one", "preference", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := app.SaveBotMemory("bot_x", "two", "preference", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.DeleteBotMemories("bot_x", []string{a.ID, "missing"}); err == nil {
		t.Fatal("batch with a missing ID should fail")
	}
	entries, err := app.ListBotMemories("bot_x", "", "")
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries after rejected batch = %#v, err=%v", entries, err)
	}
	if _, err := app.DeleteBotMemories("bot_y", []string{b.ID}); err == nil {
		t.Fatal("cross-bot batch delete should fail")
	}
	if entries := store.List("", ""); len(entries) != 2 {
		t.Fatalf("store after rejected batches = %+v", entries)
	}
}

func TestDesktopMemoryMutationsRejectBotScope(t *testing.T) {
	store, err := memory.NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()
	app := &App{memoryStore: store, testHomeDir: t.TempDir()}
	saved, err := app.SaveBotMemory("bot_x", "bot-only preference", "preference", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.UpdateMemory(saved.ID, "changed through shared scope", "preference", nil); err == nil {
		t.Fatal("shared update accepted a bot memory")
	}
	if err := app.DeleteMemory(saved.ID); err == nil {
		t.Fatal("shared delete accepted a bot memory")
	}
	if _, err := app.DeleteMemories([]string{saved.ID}); err == nil {
		t.Fatal("shared batch delete accepted a bot memory")
	}
	entries, err := app.ListBotMemories("bot_x", "", "")
	if err != nil || len(entries) != 1 || entries[0].Content != "bot-only preference" {
		t.Fatalf("bot memory after rejected shared mutations = %#v, err=%v", entries, err)
	}
	if visible := app.ListMemories("", ""); len(visible) != 0 {
		t.Fatalf("bot memory leaked into shared list: %+v", visible)
	}
}
