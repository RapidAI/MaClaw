package guiapp

import (
	"strings"
	"testing"
)

func TestRegisterSharedPageToolsInLiveRegistry(t *testing.T) {
	reg := NewToolRegistry()
	app := &App{testHomeDir: t.TempDir()}
	registerSharedPageTools(reg, app)
	for _, name := range []string{"shared_page_list", "shared_page_read", "shared_page_append", "shared_page_edit"} {
		rt, ok := reg.Get(name)
		if !ok || rt.Handler == nil {
			t.Fatalf("missing %s", name)
		}
	}
	page, err := app.CreateSharedPage("Doc", "g1", "user")
	if err != nil {
		t.Fatal(err)
	}
	out := executeSharedPageTool(app, "shared_page_append", map[string]interface{}{
		"page_id": page.ID, "title": "A", "body": "hello", "actor": "bot_1",
	})
	if strings.HasPrefix(out, "error:") || !strings.Contains(out, "hello") {
		t.Fatalf("append out=%s", out)
	}
	listed := executeSharedPageTool(app, "shared_page_list", map[string]interface{}{"group_id": "g1"})
	if !strings.Contains(listed, page.ID) {
		t.Fatalf("list=%s", listed)
	}
}

func TestAgentGroupRunLinksSharedPage(t *testing.T) {
	// Covered in agent_group_test; keep a smoke assert on CallSharedPageTool names.
	app := &App{testHomeDir: t.TempDir()}
	specs, err := app.ListSharedPageTools()
	if err != nil || len(specs) < 4 {
		t.Fatalf("specs=%v err=%v", specs, err)
	}
}
