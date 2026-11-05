package botcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogInstallUninstallAndSearch(t *testing.T) {
	root := t.TempDir()
	mcps := filepath.Join(root, "mcps")
	if err := os.MkdirAll(filepath.Join(mcps, "codegraph", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mcps, "codegraph", "tools", "search.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "skills-lock.json")
	if err := os.WriteFile(lockPath, []byte(`{"version":1,"skills":{"impeccable":{"source":"pbakaus/impeccable","sourceType":"github","skillPath":".agents/skills/impeccable/SKILL.md"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := OpenAssignments(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := LoadCatalog(mcps, lockPath, "", store, "bot_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%#v", items)
	}
	if err := store.Install("bot_1", KindMCP, "codegraph"); err != nil {
		t.Fatal(err)
	}
	if err := store.Install("bot_1", KindSkill, "impeccable"); err != nil {
		t.Fatal(err)
	}
	items, err = LoadCatalog(mcps, lockPath, "code", store, "bot_1")
	if err != nil || len(items) != 1 || !items[0].Installed || items[0].ID != "codegraph" {
		t.Fatalf("search=%#v err=%v", items, err)
	}
	other, err := LoadCatalog(mcps, lockPath, "", store, "bot_2")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range other {
		if item.Installed {
			t.Fatalf("bot_2 should have empty installs: %#v", other)
		}
	}
	if err := store.Uninstall("bot_1", KindMCP, "codegraph"); err != nil {
		t.Fatal(err)
	}
	if store.IsInstalled("bot_1", KindMCP, "codegraph") {
		t.Fatal("expected uninstall")
	}
	// Reload from disk.
	store2, err := OpenAssignments(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if !store2.IsInstalled("bot_1", KindSkill, "impeccable") {
		t.Fatal("skill assignment should persist")
	}
}

func TestAssignmentAllowFailOpenAndRestrict(t *testing.T) {
	root := t.TempDir()
	store, err := OpenAssignments(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Allowed("bot_x", KindSkill, "anything") {
		t.Fatal("fail-open when empty")
	}
	if err := store.Install("bot_x", KindSkill, "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := store.Install("bot_x", KindMCP, "codegraph"); err != nil {
		t.Fatal(err)
	}
	if !store.Allowed("bot_x", KindSkill, "alpha") || store.Allowed("bot_x", KindSkill, "beta") {
		t.Fatal("skill allowlist")
	}
	if !store.Allowed("bot_x", KindMCP, "codegraph") || store.Allowed("bot_x", KindMCP, "other") {
		t.Fatal("mcp allowlist")
	}
	view := store.RuntimeFor("bot_x")
	if len(view.Skills) != 1 || view.Skills[0] != "alpha" || len(view.MCPs) != 1 || view.MCPs[0] != "codegraph" {
		t.Fatalf("runtime=%#v", view)
	}
}
