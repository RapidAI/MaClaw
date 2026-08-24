package tool

import "testing"

type browserLeakMCPProvider struct{}

func (browserLeakMCPProvider) ListServers() []MCPServerView {
	return []MCPServerView{{ID: "srv", Name: "srv", HealthStatus: "healthy"}}
}

func (browserLeakMCPProvider) GetServerTools(serverID string) []MCPToolView {
	return []MCPToolView{
		{Name: "browser_click", Description: "legacy browser click", InputSchema: map[string]interface{}{}},
		{Name: "browser", Description: "merged browser", InputSchema: map[string]interface{}{}},
		{Name: "notes", Description: "notes", InputSchema: map[string]interface{}{}},
	}
}

func (browserLeakMCPProvider) CallTool(serverID, toolName string, args map[string]interface{}) (string, error) {
	return "", nil
}

func browserVisibilityToolDef(name, desc string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		},
	}
}

func toolDefinitionNames(defs []map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	for _, def := range defs {
		if name := ExtractToolName(def); name != "" {
			out[name] = true
		}
	}
	return out
}

func TestDefinitionGeneratorHidesInternalBrowserDispatchTools(t *testing.T) {
	gen := NewDefinitionGenerator(browserLeakMCPProvider{}, []map[string]interface{}{
		browserVisibilityToolDef("browser_click", "legacy browser click"),
		browserVisibilityToolDef("browser", "merged browser"),
		browserVisibilityToolDef("bash", "shell"),
	})
	gen.SetDeferredTools([]string{"browser_click", "notes"})

	if gen.IsDeferredTool("browser_click") {
		t.Fatal("internal browser dispatch tool must not become deferred/discoverable")
	}

	names := toolDefinitionNames(gen.Generate())
	if names["browser_click"] || names["srv_browser_click"] || names["notes"] {
		t.Fatalf("Generate leaked internal browser dispatch tool: %#v", names)
	}
	if !names["browser"] || !names["bash"] {
		t.Fatalf("Generate should keep merged browser and normal builtin tools: %#v", names)
	}

	deferredNames := toolDefinitionNames(gen.GenerateDeferred())
	if deferredNames["browser_click"] || deferredNames["srv_browser_click"] || deferredNames["notes"] {
		t.Fatalf("GenerateDeferred leaked dynamic MCP inventory: %#v", deferredNames)
	}
	if deferredNames["browser"] {
		t.Fatalf("GenerateDeferred should not include non-deferred static tools: %#v", deferredNames)
	}
}

func TestDynamicToolBuilderHidesInternalBrowserDispatchTools(t *testing.T) {
	reg := NewRegistry()
	for _, toolDef := range []RegisteredTool{
		{Name: "browser_click", Description: "legacy browser click", Category: CategoryBuiltin, Status: StatusAvailable},
		{Name: "browser", Description: "merged browser", Category: CategoryBuiltin, Status: StatusAvailable},
		{Name: "bash", Description: "shell", Category: CategoryBuiltin, Status: StatusAvailable},
	} {
		if err := reg.Register(toolDef); err != nil {
			t.Fatalf("register %s: %v", toolDef.Name, err)
		}
	}

	builder := NewDynamicToolBuilder(reg)
	names := toolDefinitionNames(builder.BuildAll())
	if names["browser_click"] {
		t.Fatalf("BuildAll leaked internal browser dispatch tool: %#v", names)
	}
	if !names["browser"] || !names["bash"] {
		t.Fatalf("BuildAll should keep merged browser and normal tools: %#v", names)
	}
}
