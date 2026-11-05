package guiapp

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/botcatalog"
)

func TestResolveCatalogBotID(t *testing.T) {
	if got := ResolveCatalogBotID("desktop-user:bot:scout", "", "", nil); got != "scout" {
		t.Fatalf("owner parse got %q", got)
	}
	if got := ResolveCatalogBotID("desktop-user", "alice:bot_9", "", nil); got != "bot_9" {
		t.Fatalf("session got %q", got)
	}
	if got := ResolveCatalogBotID("desktop-user", "", "explicit", []string{"a", "b"}); got != "explicit" {
		t.Fatalf("explicit got %q", got)
	}
	if got := ResolveCatalogBotID("desktop-user", "", "", []string{"only"}); got != "only" {
		t.Fatalf("active got %q", got)
	}
}

func TestBotCatalogRuntimeGateAndActivate(t *testing.T) {
	dir := t.TempDir()
	botCatalogMu.Lock()
	botCatalogStore = nil
	botCatalogMu.Unlock()
	app := &App{
		testHomeDir:          dir,
		botCatalogMcpsDir:    filepath.Join(dir, "mcps"),
		botCatalogSkillsLock: filepath.Join(dir, "skills-lock.json"),
	}
	_ = filepath.Join(dir, ".maclaw", "data")
	if err := app.InstallBotCatalogItem("bot_1", string(botcatalog.KindSkill), "alpha"); err != nil {
		t.Fatal(err)
	}
	if err := app.InstallBotCatalogItem("bot_1", string(botcatalog.KindMCP), "codegraph"); err != nil {
		t.Fatal(err)
	}
	rt, err := app.GetBotCatalogRuntime("bot_1")
	if err != nil || len(rt.Skills) != 1 || len(rt.MCPs) != 1 {
		t.Fatalf("rt=%#v err=%v", rt, err)
	}
	rej := app.botCatalogToolRejection("desktop-user:bot:bot_1", "manage_skill", `{"action":"run","name":"beta"}`)
	if rej == "" {
		t.Fatal("expected rejection")
	}
	ok := app.botCatalogToolRejection("desktop-user:bot:bot_1", "manage_skill", `{"action":"run","name":"alpha"}`)
	if ok != "" {
		t.Fatalf("unexpected %q", ok)
	}
	mcpRej := app.botCatalogToolRejection("desktop-user:bot:bot_1", "call_mcp_tool", `{"server_id":"other"}`)
	if mcpRej == "" {
		t.Fatal("expected mcp rejection")
	}
}
