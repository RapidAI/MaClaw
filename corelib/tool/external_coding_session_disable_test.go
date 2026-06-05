package tool

import "testing"

type disabledToolMCPProvider struct{}

func (disabledToolMCPProvider) ListServers() []MCPServerView {
	return []MCPServerView{{ID: "remote", HealthStatus: "healthy"}}
}

func (disabledToolMCPProvider) GetServerTools(string) []MCPToolView {
	return []MCPToolView{
		{Name: "create_session", Description: "legacy create"},
		{Name: "send_and_observe", Description: "legacy observe"},
		{Name: "safe_remote_tool", Description: "safe"},
	}
}

func (disabledToolMCPProvider) CallTool(string, string, map[string]interface{}) (string, error) {
	return "", nil
}

type disabledToolLocalProvider struct{}

func (disabledToolLocalProvider) GetAllTools() []MCPToolSet {
	return []MCPToolSet{{ServerID: "local", Tools: []MCPToolView{
		{Name: "control_session", Description: "legacy control"},
		{Name: "safe_local_tool", Description: "safe"},
	}}}
}

func (disabledToolLocalProvider) CallTool(string, string, map[string]interface{}) (string, error) {
	return "", nil
}

func TestExternalCodingSessionToolsFilteredAcrossCoreToolSurfaces(t *testing.T) {
	for _, name := range []string{" create_session ", "SEND_AND_OBSERVE", "Control_Session"} {
		if !IsDisabledExternalCodingSessionTool(name) {
			t.Fatalf("IsDisabledExternalCodingSessionTool(%q) = false, want true", name)
		}
	}

	builtins := []map[string]interface{}{
		makeToolDef("create_session", "legacy create"),
		makeToolDef("send_and_observe", "legacy observe"),
		makeToolDef("control_session", "legacy control"),
		makeToolDef("safe_builtin", "safe"),
	}

	gen := NewDefinitionGenerator(disabledToolMCPProvider{}, builtins)
	gen.SetLocalMCPProvider(disabledToolLocalProvider{})
	gen.SetDeferredTools([]string{"create_session", "send_and_observe", "control_session"})
	assertNoDisabledToolDefs(t, gen.Generate())
	if gen.IsDeferredTool("create_session") || gen.IsDeferredTool("send_and_observe") || gen.IsDeferredTool("control_session") {
		t.Fatal("disabled external coding-session tools must not be deferred/discoverable")
	}

	reg := NewRegistry()
	for _, name := range []string{"create_session", "send_and_observe", "control_session", "safe_registry_tool"} {
		if err := reg.Register(RegisteredTool{Name: name, Description: "registered", Status: StatusAvailable}); err != nil {
			t.Fatalf("Register(%s): %v", name, err)
		}
	}
	for _, name := range []string{"create_session", "send_and_observe", "control_session"} {
		if _, ok := reg.Get(name); ok {
			t.Fatalf("disabled tool %s should not be registered", name)
		}
	}
	assertNoDisabledToolDefs(t, NewDynamicToolBuilder(reg).BuildAll())
	assertNoDisabledToolDefs(t, NewDynamicToolBuilder(reg).Build("run coding session"))
	assertNoDisabledToolDefs(t, FilterCodingTools(builtins))
}

func assertNoDisabledToolDefs(t *testing.T, defs []map[string]interface{}) {
	t.Helper()
	for _, def := range defs {
		name := ExtractToolName(def)
		if IsDisabledExternalCodingSessionTool(name) {
			t.Fatalf("disabled external coding-session tool leaked: %s", name)
		}
	}
}
