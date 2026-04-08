package main

import "testing"

func TestRegisterBrowserToolsIncludesSessionMVP(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)
	for _, name := range []string{
		"browser_session_start",
		"browser_session_stop",
		"browser_observe",
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_wait",
		"browser_back",
		"browser_refresh",
		"browser_extract",
	} {
		tool, ok := registry.Get(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if tool.Handler == nil {
			t.Fatalf("tool %q handler is nil", name)
		}
	}
}
