package memory

import (
	"path/filepath"
	"testing"
)

func TestFormatParseBotMemoryOwner(t *testing.T) {
	owner, err := FormatBotMemoryOwner("desktop-user", "bot_1")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "desktop-user:bot:bot_1" {
		t.Fatalf("owner=%q", owner)
	}
	botID, ok := ParseBotMemoryOwner("desktop-user", owner)
	if !ok || botID != "bot_1" {
		t.Fatalf("parse=%q ok=%v", botID, ok)
	}
	if _, err := FormatBotMemoryOwner("desktop-user", ""); err == nil {
		t.Fatal("expected empty bot id error")
	}
	if _, ok := ParseBotMemoryOwner("desktop-user", "desktop-user"); ok {
		t.Fatal("non-bot owner should not parse")
	}
}

func TestBotMemoryCRUDIsolation(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	a, err := store.SaveBotMemory("desktop-user", "bot_a", "alpha fact", string(CategoryUserFact), []string{"topic:a"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.OwnerID != "desktop-user:bot:bot_a" {
		t.Fatalf("saved A = %#v", a)
	}
	if _, err := store.SaveBotMemory("desktop-user", "bot_b", "beta fact", string(CategoryPreference), nil); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveManualMemory("shared fact", CategoryInstruction, nil); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListBotMemories("desktop-user", "bot_a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Content != "alpha fact" {
		t.Fatalf("list A = %#v", listed)
	}
	listedB, err := store.ListBotMemories("desktop-user", "bot_b", "", "beta")
	if err != nil || len(listedB) != 1 {
		t.Fatalf("list B search = %#v err=%v", listedB, err)
	}

	if err := store.UpdateBotMemory("desktop-user", "bot_a", a.ID, "alpha fact updated", string(CategoryUserFact), []string{"topic:a2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateBotMemory("desktop-user", "bot_b", a.ID, "hijack", string(CategoryUserFact), nil); err == nil {
		t.Fatal("cross-bot update should fail")
	}
	if err := store.DeleteBotMemory("desktop-user", "bot_b", a.ID); err == nil {
		t.Fatal("cross-bot delete should fail")
	}
	if err := store.DeleteBotMemory("desktop-user", "bot_a", a.ID); err != nil {
		t.Fatal(err)
	}
	left, _ := store.ListBotMemories("desktop-user", "bot_a", "", "")
	if len(left) != 0 {
		t.Fatalf("after delete A still has %#v", left)
	}
}


func TestDeleteBotMemoriesBulk(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "mem.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Stop()

	a, err := store.SaveBotMemory("desktop-user", "bot_a", "one", string(CategoryUserFact), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.SaveBotMemory("desktop-user", "bot_a", "two", string(CategoryUserFact), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveBotMemory("desktop-user", "bot_b", "other", string(CategoryUserFact), nil); err != nil {
		t.Fatal(err)
	}
	deleted, skipped, err := store.DeleteBotMemories("desktop-user", "bot_a", []string{a.ID, b.ID, "missing", a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("deleted=%d skipped=%v", deleted, skipped)
	}
	foundMissing := false
	for _, s := range skipped {
		if s == "missing:not_found" {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("skipped=%v", skipped)
	}
	left, err := store.ListBotMemories("desktop-user", "bot_a", "", "")
	if err != nil || len(left) != 0 {
		t.Fatalf("left=%v err=%v", left, err)
	}
}
