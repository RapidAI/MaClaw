package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func newCodingPilotTestRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	reg := NewToolRegistry()
	for _, name := range codingPilotToolNames {
		if err := reg.Register(RegisteredTool{Name: name, Description: "pilot " + name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	return reg
}

func codingPilotTestArgs(name string) []string {
	switch name {
	case "web_search":
		return []string{`{}`, `{"query":""}`}
	case "web_fetch":
		return []string{`{}`, `{"url":""}`}
	case "memory":
		return []string{`{"action":"recall","query":"x"}`}
	default:
		return []string{`{}`, `{"query":"x","path":"/nonexistent-xyz/","pattern":"alpha"}`}
	}
}

// admitCodingPilotCall renders the model surface and returns the fresh epoch
// so a call passes the static-compatibility fences exactly like RunLoop's
// request boundary does.
func admitCodingPilotCall(cb *codingSubAgentCallbacks) string {
	epoch := cb.BeginToolSurfaceEpoch(0)
	_ = cb.BuildToolsForModelRequest("implement", 0)
	return epoch
}

func TestCodingToolDispatcherKillSwitchDefaultOff(t *testing.T) {
	t.Setenv(codingPilotEnvKey, "")
	reg := newCodingPilotTestRegistry(t)
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	cb.subagent.handler = &IMMessageHandler{registry: reg}
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env unset, want nil", d)
	}
	t.Setenv(codingPilotEnvKey, "off")
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env=off, want nil", d)
	}
}

func TestCodingToolDispatcherPublishesWhenEnabled(t *testing.T) {
	t.Setenv(codingPilotEnvKey, "on")
	reg := newCodingPilotTestRegistry(t)
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	cb.subagent.handler = &IMMessageHandler{registry: reg}
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
	if len(handlers) != len(codingPilotToolNames) {
		t.Fatalf("handlers = %v, want %d pilot names", handlers, len(codingPilotToolNames))
	}
	for _, h := range handlers {
		if !strings.HasPrefix(h, "core:") {
			t.Fatalf("handler %q is not an ID-string registration (ID-keyed mode required)", h)
		}
	}
}

// TestCodingToolDispatcherParityMatchesLegacyEntry proves each pilot handler
// enters through the exact boundary RunLoop calls (ExecuteToolCallWithContext):
// the dispatcher result must equal the legacy entry's result for identical
// inputs on the same callbacks.
//
// Coverage statement: the correlated-local fixture (real projectPath, nil
// handler stores) is admitted through the same surface-render + epoch path
// RunLoop uses; every pilot name deterministically produces its per-name
// admission/execution outcome (nil-handler rejections and empty tempdir
// searches), and each comparison uses a FRESH epoch + surface render because
// the static-compatibility epoch is single-use after a successful sibling.
// The effectful families (write_file/edit_file/bash/spawn) and the dynamic
// Skill/MCP alias surface are NOT constructed here: they carry extra
// correlation requirements and lifecycle wiring beyond unit scope, and the
// pilot list is read-only by design. Loop-level gates (authorizeLoopTool,
// replan skip) run upstream of the dispatcher consultation for both paths.
func TestCodingToolDispatcherParityMatchesLegacyEntry(t *testing.T) {
	reg := newCodingPilotTestRegistry(t)
	d, skipped := buildCodingPilotDispatcher(nil, reg, codingPilotToolNames)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	for _, name := range codingPilotToolNames {
		for _, argsJSON := range codingPilotTestArgs(name) {
			cb := correlatedLocalCodingCallbacksForTest(t, true)
			cb.subagent.handler = &IMMessageHandler{registry: reg}
			// The dispatcher handlers close over their own callbacks; rebuild the
			// dispatcher against this callbacks so both paths share it.
			d, _ = buildCodingPilotDispatcher(cb, reg, codingPilotToolNames)
			epoch := admitCodingPilotCall(cb)
			dispResult, handled, err := d.Dispatch(name, argsJSON, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
			if err != nil {
				t.Fatalf("Dispatch(%q) error = %v", name, err)
			}
			if !handled {
				t.Fatalf("Dispatch(%q) handled=false, want true", name)
			}
			epoch2 := admitCodingPilotCall(cb)
			legacy := cb.ExecuteToolCallWithContext(name, argsJSON, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch2})
			if dispResult.Result != legacy.Result || dispResult.Outcome != legacy.Outcome {
				t.Fatalf("name=%q args=%s dispatcher={%q,%s} legacy={%q,%s}",
					name, argsJSON, dispResult.Result, dispResult.Outcome, legacy.Result, legacy.Outcome)
			}
			if dispResult.Result == "" {
				t.Fatalf("name=%q args=%s empty result", name, argsJSON)
			}
		}
	}
}

// TestCodingToolDispatcherIDPathWins proves dispatch serves pilot names
// through the ID path: a decoy NAME-keyed registration for a pilot name must
// never execute, and resolver misses fall through with handled=false.
func TestCodingToolDispatcherIDPathWins(t *testing.T) {
	reg := newCodingPilotTestRegistry(t)
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	cb.subagent.handler = &IMMessageHandler{registry: reg}
	d, skipped := buildCodingPilotDispatcher(cb, reg, codingPilotToolNames)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	decoy := agent.ToolExecutionResult{Result: "decoy name handler", Outcome: agent.ToolExecutionOutcomeOK}
	if err := d.Register("web_search", func(_, _, _ string, _ agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
		return decoy, nil
	}); err != nil {
		t.Fatalf("decoy name registration: %v", err)
	}
	epoch := admitCodingPilotCall(cb)
	res, handled, err := d.Dispatch("web_search", `{}`, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch})
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

func TestCodingToolDispatcherNonPilotNamesFallThrough(t *testing.T) {
	t.Setenv(codingPilotEnvKey, "on")
	reg := newCodingPilotTestRegistry(t)
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	cb.subagent.handler = &IMMessageHandler{registry: reg}
	d := cb.ToolDispatcher()
	// Excluded categories: execute-kind, mutate-kind, spawn, dynamic gateways.
	for _, name := range []string{"bash", "write_file", "edit_file", codingSubAgentSpawnToolName, "unknown_tool", ""} {
		if _, handled, err := d.Dispatch(name, `{}`, "call-1", agent.ToolCallExecutionContext{}); handled || err != nil {
			t.Fatalf("Dispatch(%q) = handled=%v err=%v, want handled=false err=nil", name, handled, err)
		}
	}
}

func TestCodingBuildPilotDispatcherSkipsUnregisteredNames(t *testing.T) {
	reg := newCodingPilotTestRegistry(t)
	cb := correlatedLocalCodingCallbacksForTest(t, true)
	names := append(append([]string(nil), codingPilotToolNames...), "not_a_real_tool")
	d, skipped := buildCodingPilotDispatcher(cb, reg, names)
	if len(skipped) != 1 || skipped[0] != "not_a_real_tool" {
		t.Fatalf("skipped = %v, want [not_a_real_tool]", skipped)
	}
	for _, h := range d.Handlers() {
		if h == "core:not_a_real_tool" {
			t.Fatal("unregistered name must not be registered")
		}
	}
	if len(d.Handlers()) != len(codingPilotToolNames) {
		t.Fatalf("handlers = %v, want the %d pilot names", d.Handlers(), len(codingPilotToolNames))
	}
}
