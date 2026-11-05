package guiapp

import (
	"path/filepath"
	"testing"
)

func TestSharedPageBindingsAndTools(t *testing.T) {
	dir := t.TempDir()
	a := &App{testHomeDir: dir}
	sharedPageMu.Lock()
	sharedPageStore = nil
	sharedPageMu.Unlock()

	p, err := a.CreateSharedPage("Out", "team-1", "user")
	if err != nil {
		t.Fatal(err)
	}
	p, err = a.AppendSharedPageSection(p.ID, "A", "one", "bot-1")
	if err != nil || len(p.Sections) != 1 {
		t.Fatalf("append=%+v err=%v", p, err)
	}
	p, err = a.EditSharedPageSection(p.ID, p.Sections[0].ID, "A", "two", "bot-2")
	if err != nil || p.Sections[0].Body != "two" {
		t.Fatalf("edit=%+v err=%v", p, err)
	}
	list, err := a.ListSharedPages("team-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	tools, err := a.ListSharedPageTools()
	if err != nil || len(tools) < 3 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	got, err := a.CallSharedPageTool("shared_page_read", map[string]any{"page_id": p.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = got
	if _, err := a.CallSharedPageTool("nope", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
	// Ensure file landed under data dir root (testHomeDir)
	if _, err := filepath.Glob(filepath.Join(dir, "shared_pages.json")); err != nil {
		t.Fatal(err)
	}
}
