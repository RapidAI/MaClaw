package main

import "testing"

func TestRegisterBrowserToolsIncludesActionTools(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)
	for _, name := range []string{"browser_click", "browser_type", "browser_back", "browser_refresh", "browser_extract"} {
		tool, ok := registry.Get(name)
		if !ok || tool == nil {
			t.Fatalf("tool %q missing", name)
		}
		if tool.Handler == nil {
			t.Fatalf("tool %q handler nil", name)
		}
	}
}
