package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolid"
)

// newIMPilotTestRegistry registers the pilot names the way production does:
// zero IDs derived at registration.
func newIMPilotTestRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	reg := NewToolRegistry()
	for _, name := range imPilotToolNames {
		if err := reg.Register(RegisteredTool{Name: name, Description: "pilot " + name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	return reg
}

func TestIMToolDispatcherKillSwitchDefaultOff(t *testing.T) {
	t.Setenv(imDispatcherEnvKey, "")
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{registry: newIMPilotTestRegistry(t)}}
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env unset, want nil", d)
	}
	t.Setenv(imDispatcherEnvKey, "off")
	if d := (&sharedAgentLoopCallbacks{handler: &IMMessageHandler{registry: newIMPilotTestRegistry(t)}}).ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env=off, want nil", d)
	}
}

func TestIMToolDispatcherPublishesWhenEnabled(t *testing.T) {
	t.Setenv(imDispatcherEnvKey, "on")
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{registry: newIMPilotTestRegistry(t)}}
	d := cb.ToolDispatcher()
	if d == nil {
		t.Fatal("ToolDispatcher() = nil with env=on, want pilot dispatcher")
	}
	if again := cb.ToolDispatcher(); again != d {
		t.Fatal("second ToolDispatcher() returned a different dispatcher; want cached value")
	}
	nd, ok := d.(*agent.NameDispatcher)
	if !ok {
		t.Fatalf("dispatcher type = %T, want *agent.NameDispatcher", d)
	}
	handlers := nd.Handlers()
	if len(handlers) != len(imPilotToolNames) {
		t.Fatalf("handlers = %v, want %d pilot names", handlers, len(imPilotToolNames))
	}
	for _, h := range handlers {
		if !strings.HasPrefix(h, "core:") {
			t.Fatalf("handler %q is not an ID-string registration (ID-keyed mode required)", h)
		}
	}
	found := false
	for _, h := range handlers {
		if h == "core:Glob" {
			found = true
		}
	}
	if !found {
		t.Fatalf("exact-case ID core:Glob missing from handlers: %v", handlers)
	}
}

// imPilotTestArgs returns per-name arg shapes for the parity run. Mutate
// names receive t.TempDir() paths: on nil-handler callbacks no write actually
// executes (the chain rejects with "handler unavailable" before any handler
// runs), so the temp files are pure input fixtures and t.TempDir() cleanup
// covers them; the shapes prove write arguments flow through the dispatched
// path identically to the legacy path.
func imPilotTestArgs(t *testing.T, name string) []string {
	tmp := t.TempDir()
	switch name {
	case "web_search":
		return []string{`{}`, `{"query":""}`}
	case "web_fetch":
		return []string{`{}`, `{"url":""}`}
	case "write_file":
		return []string{
			`{"path":"` + tmp + `/out.txt","content":"hello"}`,
			`{}`,
		}
	case "edit_file":
		return []string{
			`{"path":"` + tmp + `/out.txt","old_string":"a","new_string":"b"}`,
			`{}`,
		}
	case "memory":
		return []string{`{"action":"recall","query":"x"}`, `{"action":"unknown_op"}`}
	default:
		return []string{`{}`, `{"query":"x","path":"/nonexistent-xyz/"}`}
	}
}

// TestIMToolDispatcherParityMatchesLegacyEntry proves each pilot handler
// enters through the exact boundary the core loop calls
// (ExecuteToolCallWithContext): the dispatcher result must equal the legacy
// entry's result for identical inputs.
//
// Coverage statement: on a zero-value callbacks the epoch gate is inactive
// (no semantic/legacy surface snapshot) and every pilot name deterministically
// reaches "handler unavailable" through the full chain
// (petition/semantic/surface/MCP-gateway gates → ExecuteToolStructured →
// ExecuteTool → executeToolWithoutSemanticSurface), including the mutate-class
// names with real temp-dir write arguments — the write-time gates (credential
// fence, ACP permission, argument-size limit) all sit INSIDE
// ExecuteToolCallWithContext's downstream path, so both paths observe them
// identically. Actually executing a write would require a fully wired
// IMMessageHandler (memory store, credential/permission stores, audit);
// that fixture is out of unit-test scope here, and the equivalence claim is
// "same input → same output on both paths through the same entry", which the
// nil-handler boundary proves by construction. Loop-level gates
// (authorizeLoopTool, replan skip) run upstream of the dispatcher
// consultation in corelib/agent/loop.go and are untouched. A managed
// semanticCallSurface or snapshotted legacyToolSurface is NOT constructed
// here (too heavy for a unit test); those states are why the dispatcher
// carries the loop's ToolCallExecutionContext, which the handler forwards
// verbatim.
func TestIMToolDispatcherParityMatchesLegacyEntry(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{}
	d, skipped := buildIMPilotDispatcher(cb, newIMPilotTestRegistry(t), imPilotToolNames)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	for _, name := range imPilotToolNames {
		for _, argsJSON := range imPilotTestArgs(t, name) {
			execution := agent.ToolCallExecutionContext{SurfaceEpoch: "epoch-test", ResponseID: "resp-1"}
			dispResult, handled, err := d.Dispatch(name, argsJSON, "call-1", execution)
			if err != nil {
				t.Fatalf("Dispatch(%q) error = %v", name, err)
			}
			if !handled {
				t.Fatalf("Dispatch(%q) handled=false, want true", name)
			}
			legacy := cb.ExecuteToolCallWithContext(name, argsJSON, "call-1", execution)
			if dispResult.Result != legacy.Result || dispResult.Outcome != legacy.Outcome {
				t.Fatalf("name=%q args=%s dispatcher={%q,%s} legacy={%q,%s}",
					name, argsJSON, dispResult.Result, dispResult.Outcome, legacy.Result, legacy.Outcome)
			}
			if dispResult.Result != "handler unavailable" {
				t.Fatalf("name=%q args=%s result = %q, want handler unavailable on nil-handler callbacks", name, argsJSON, dispResult.Result)
			}
		}
	}
}

// TestIMToolDispatcherResolutionIsRegistryBacked proves the resolver speaks in
// the registry's canonical IDs, not derived guesses: a tool registered with
// an explicit non-core ID resolves through that exact ID.
func TestIMToolDispatcherResolutionIsRegistryBacked(t *testing.T) {
	reg := newIMPilotTestRegistry(t)
	const customName = "custom_lookup"
	if err := reg.Register(RegisteredTool{Name: customName, ID: toolid.MustParse("host:custom")}); err != nil {
		t.Fatalf("register custom: %v", err)
	}
	cb := &sharedAgentLoopCallbacks{}
	d, skipped := buildIMPilotDispatcher(cb, reg, []string{customName})
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	found := false
	for _, h := range d.Handlers() {
		if h == "host:custom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("explicit non-core ID not in handlers %v — resolution must come from the registry", d.Handlers())
	}
	res, handled, err := d.Dispatch(customName, `{}`, "call-1", agent.ToolCallExecutionContext{})
	if err != nil || !handled {
		t.Fatalf("Dispatch(%q) = %+v handled=%v err=%v", customName, res, handled, err)
	}
	if res.Result != "handler unavailable" {
		t.Fatalf("result = %q, want handler unavailable", res.Result)
	}
}

// TestIMToolDispatcherIDPathWins proves dispatch serves pilot names through
// the ID path: a decoy NAME-keyed registration for a pilot name must never
// execute, and resolver misses fall through with handled=false.
func TestIMToolDispatcherIDPathWins(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{}
	d, skipped := buildIMPilotDispatcher(cb, newIMPilotTestRegistry(t), imPilotToolNames)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	decoy := agent.ToolExecutionResult{Result: "decoy name handler", Outcome: agent.ToolExecutionOutcomeOK}
	if err := d.Register("web_search", func(_, _, _ string, _ agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
		return decoy, nil
	}); err != nil {
		t.Fatalf("decoy name registration: %v", err)
	}
	res, handled, err := d.Dispatch("web_search", `{}`, "call-1", agent.ToolCallExecutionContext{})
	if err != nil || !handled {
		t.Fatalf("Dispatch(web_search) = %+v handled=%v err=%v", res, handled, err)
	}
	if res.Result == decoy.Result {
		t.Fatal("name-keyed decoy handler executed — ID path must win over name handlers")
	}
	if _, handled, _ := d.Dispatch("not_registered", `{}`, "call-1", agent.ToolCallExecutionContext{}); handled {
		t.Fatal("resolver miss must fall through to legacy (handled=false)")
	}
}

func TestIMToolDispatcherNonPilotNamesFallThrough(t *testing.T) {
	t.Setenv(imDispatcherEnvKey, "on")
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{registry: newIMPilotTestRegistry(t)}}
	d := cb.ToolDispatcher()
	// Excluded categories: execute-kind, task/delegate, database, MCP-gateway
	// style, unknown. (write_file/edit_file/memory are now pilot names.)
	for _, name := range []string{"bash", "ssh", "task", "delegate_task", "database", "manage_skill", "unknown_tool", ""} {
		if _, handled, err := d.Dispatch(name, `{}`, "call-1", agent.ToolCallExecutionContext{}); handled || err != nil {
			t.Fatalf("Dispatch(%q) = handled=%v err=%v, want handled=false err=nil", name, handled, err)
		}
	}
}

// TestIMBuildPilotDispatcherSkipsUnregisteredNames: the registry is the domain
// authority — names it does not serve are skipped with a log, never a crash.
func TestIMBuildPilotDispatcherSkipsUnregisteredNames(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{}
	reg := newIMPilotTestRegistry(t)
	names := append(append([]string(nil), imPilotToolNames...), "not_a_real_tool")
	d, skipped := buildIMPilotDispatcher(cb, reg, names)
	if len(skipped) != 1 || skipped[0] != "not_a_real_tool" {
		t.Fatalf("skipped = %v, want [not_a_real_tool]", skipped)
	}
	for _, h := range d.Handlers() {
		if h == "core:not_a_real_tool" {
			t.Fatal("unregistered name must not be registered")
		}
	}
	if len(d.Handlers()) != len(imPilotToolNames) {
		t.Fatalf("handlers = %v, want the %d pilot names", d.Handlers(), len(imPilotToolNames))
	}
}
