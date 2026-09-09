package guiapp

import (
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"testing"
)

func TestToolRouterRouteForScope(t *testing.T) {
	r := NewToolRouter(nil)
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "z_tool"}},
		{"type": "function", "function": map[string]interface{}{"name": "a_tool"}},
	}
	got := r.RouteForScope(defs, []string{"z_tool", "a_tool"})
	if len(got) != 2 || coretool.ExtractToolName(got[0]) != "a_tool" || coretool.ExtractToolName(got[1]) != "z_tool" {
		t.Fatalf("scoped route = %#v", got)
	}
}
