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

func TestRegisterBrowserToolsPassesContinuationSchema(t *testing.T) {
	registry := NewToolRegistry()
	app := &App{}
	app.browserSessions = NewBrowserAgentManager(app)
	registerBrowserTools(registry, app)

	tool, ok := registry.Get("browser_extract")
	if !ok || tool == nil {
		t.Fatal("browser_extract missing")
	}
	for _, field := range []string{"offset", "max_chars"} {
		entry, ok := tool.InputSchema[field]
		if !ok {
			t.Fatalf("browser_extract missing %q", field)
		}
		meta, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("schema[%q] = %#v", field, entry)
		}
		if meta["type"] != "integer" {
			t.Fatalf("schema[%q].type = %#v", field, meta["type"])
		}
	}
}
