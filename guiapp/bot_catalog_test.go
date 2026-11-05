package guiapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBotCatalogBindingsInstallUninstall(t *testing.T) {
	root := t.TempDir()
	mcps := filepath.Join(root, "mcps")
	if err := os.MkdirAll(filepath.Join(mcps, "tasks", "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(mcps, "tasks", "tools", "list.json"), []byte(`{}`), 0o644)
	lock := filepath.Join(root, "skills-lock.json")
	_ = os.WriteFile(lock, []byte(`{"version":1,"skills":{"impeccable":{"source":"x","sourceType":"github","skillPath":"SKILL.md"}}}`), 0o644)

	// Reset package-level store between tests.
	botCatalogMu.Lock()
	botCatalogStore = nil
	botCatalogMu.Unlock()

	app := &App{
		testHomeDir:          root,
		botCatalogMcpsDir:    mcps,
		botCatalogSkillsLock: lock,
	}
	items, err := app.ListBotCatalog("bot_z", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%#v", items)
	}
	if err := app.InstallBotCatalogItem("bot_z", "mcp", "tasks"); err != nil {
		t.Fatal(err)
	}
	items, err = app.ListBotCatalog("bot_z", "tasks")
	if err != nil || len(items) != 1 || !items[0].Installed {
		t.Fatalf("after install %#v err=%v", items, err)
	}
	if err := app.UninstallBotCatalogItem("bot_z", "mcp", "tasks"); err != nil {
		t.Fatal(err)
	}
	items, err = app.ListBotCatalog("bot_z", "tasks")
	if err != nil || len(items) != 1 || items[0].Installed {
		t.Fatalf("after uninstall %#v err=%v", items, err)
	}
}
