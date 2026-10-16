package tool

import "testing"

func TestRouteForScopeIsKeywordIndependentAndDeterministic(t *testing.T) {
	r := NewRouter(nil)
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "z_tool"}},
		{"type": "function", "function": map[string]interface{}{"name": "a_tool"}},
		{"type": "function", "function": map[string]interface{}{"name": "hidden"}},
	}
	got := r.RouteForScope(defs, []string{"z_tool", "a_tool", "missing"})
	if len(got) != 2 || ExtractToolName(got[0]) != "a_tool" || ExtractToolName(got[1]) != "z_tool" {
		t.Fatalf("scoped tools = %#v", got)
	}
}
