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
	out, err := app.DeleteBotMemories("bot_x", []string{a.ID, b.ID, "nope"})
	if err != nil {
		t.Fatal(err)
	}
	if out["deleted"] != 2 {
		t.Fatalf("out=%#v", out)
	}
}
