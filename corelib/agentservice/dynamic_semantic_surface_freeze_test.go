package agentservice

import (
	"strings"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// Both hosts enforce the same ban and must enforce the same list. Checking
// against the shared names rather than a remembered pair is what keeps them
// from drifting: the two filters were written independently, in different
// shapes, and a gateway added to one would not have shown up in a search for
// the other.
func TestEveryNamedGatewayIsClosedOutOfTheManagedSurface(t *testing.T) {
	gateways := coretool.LegacyDynamicGatewayNames()
	if len(gateways) == 0 {
		t.Fatal("no gateway is named, so nothing here is being enforced")
	}
	defs := []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "granted_capability"}},
	}
	for _, name := range gateways {
		defs = append(defs, map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": name}})
	}
	closed := closedManagedSemanticDefinitions(defs)
	if len(closed) != 1 || tooldef.Name(closed[0]) != "granted_capability" {
		names := make([]string, 0, len(closed))
		for _, def := range closed {
			names = append(names, tooldef.Name(def))
		}
		t.Fatalf("closed surface = %v, want only the granted capability", names)
	}
}

func TestManagedDynamicBuildToolsRejectsLegacyGatewayUnion(t *testing.T) {
	routing := testDynamicSemanticRouting(t)
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{{
		ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract(),
	}}}
	callback := testManagedDynamicCallback(routing, provider, nil)
	defs := callback.BuildTools("lookup")
	if len(defs) == 0 {
		t.Fatal("expected managed semantic definitions")
	}
	for _, def := range defs {
		name := tooldef.Name(def)
		if name == "call_mcp_tool" || name == "manage_skill" {
			t.Fatalf("managed BuildTools leaked %q", name)
		}
	}
	poisoned := append(append([]map[string]interface{}{}, defs...),
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "call_mcp_tool"}},
		map[string]interface{}{"type": "function", "function": map[string]interface{}{"name": "manage_skill"}},
	)
	closed := closedManagedSemanticDefinitions(poisoned)
	if len(closed) != len(defs) {
		t.Fatalf("closed count=%d want %d", len(closed), len(defs))
	}
	for _, def := range closed {
		name := tooldef.Name(def)
		if name == "call_mcp_tool" || name == "manage_skill" {
			t.Fatalf("legacy gateway survived close: %q", name)
		}
	}
	_ = strings.TrimSpace
}
