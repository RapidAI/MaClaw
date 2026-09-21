package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func newRemoteCodingPilotTestRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	reg := NewToolRegistry()
	for _, name := range remoteCodingPilotToolNames {
		if err := reg.Register(RegisteredTool{Name: name, Description: "pilot " + name}); err != nil {
			t.Fatalf("register %q: %v", name, err)
		}
	}
	return reg
}

func remoteCodingPilotTestArgs(name string) []string {
	switch name {
	case "web_search":
		return []string{`{}`, `{"query":""}`}
	case "web_fetch":
		return []string{`{}`, `{"url":""}`}
	default:
		return []string{`{}`, `{"query":"x","source_id":"nope"}`}
	}
}

func TestRemoteCodingToolDispatcherKillSwitchDefaultOff(t *testing.T) {
	t.Setenv(remoteCodingPilotEnvKey, "")
	reg := newRemoteCodingPilotTestRegistry(t)
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env unset, want nil", d)
	}
	t.Setenv(remoteCodingPilotEnvKey, "off")
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env=off, want nil", d)
	}
}

func TestRemoteCodingToolDispatcherPublishesWhenEnabled(t *testing.T) {
	t.Setenv(remoteCodingPilotEnvKey, "on")
	reg := newRemoteCodingPilotTestRegistry(t)
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
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
	if len(handlers) != len(remoteCodingPilotToolNames) {
		t.Fatalf("handlers = %v, want %d pilot names", handlers, len(remoteCodingPilotToolNames))
	}
	for _, h := range handlers {
		if !strings.HasPrefix(h, "core:") {
			t.Fatalf("handler %q is not an ID-string registration (ID-keyed mode required)", h)
		}
	}
}

// TestRemoteCodingToolDispatcherParityMatchesLegacyEntry proves each pilot
// handler enters through the exact boundary RunLoop calls
// (ExecuteToolCallWithContext): the dispatcher result must equal the legacy
// entry's result for identical inputs on the same callbacks.
//
// Coverage statement: the minimal fixture (agent without handler stores) hits
// the remote admission fences with an empty ToolCallExecutionContext — the
// remote epoch check allows the empty epoch explicitly
// (staticCompatibilityExecutionEpochAllowed returns true for ""), and every
// pilot name then deterministically produces its nil-handler local outcome
// ("web_search unavailable: host handler missing", "web_fetch unavailable",
// "项目知识库未配置…") without any ssh_* relay firing. The ssh_* names and the
// dynamic alias surface are NOT exercised here: they require a bound remote
// session/relay fixture beyond unit scope, and they are not pilot names.
// Loop-level gates (authorizeLoopTool, replan skip) run upstream of the
// dispatcher consultation for both paths.
func TestRemoteCodingToolDispatcherParityMatchesLegacyEntry(t *testing.T) {
	reg := newRemoteCodingPilotTestRegistry(t)
	for _, name := range remoteCodingPilotToolNames {
		for _, argsJSON := range remoteCodingPilotTestArgs(name) {
			cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
			d, skipped := buildRemoteCodingPilotDispatcher(cb, reg, remoteCodingPilotToolNames)
			if len(skipped) != 0 {
				t.Fatalf("skipped = %v, want none", skipped)
			}
			dispResult, handled, err := d.Dispatch(name, argsJSON, "call-1", agent.ToolCallExecutionContext{})
			if err != nil {
				t.Fatalf("Dispatch(%q) error = %v", name, err)
			}
			if !handled {
				t.Fatalf("Dispatch(%q) handled=false, want true", name)
			}
			legacy := cb.ExecuteToolCallWithContext(name, argsJSON, "call-1", agent.ToolCallExecutionContext{})
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

// TestRemoteCodingToolDispatcherIDPathWins proves dispatch serves pilot names
// through the ID path: a decoy NAME-keyed registration for a pilot name must
// never execute, and resolver misses fall through with handled=false.
func TestRemoteCodingToolDispatcherIDPathWins(t *testing.T) {
	reg := newRemoteCodingPilotTestRegistry(t)
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
	d, skipped := buildRemoteCodingPilotDispatcher(cb, reg, remoteCodingPilotToolNames)
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

func TestRemoteCodingToolDispatcherNonPilotNamesFallThrough(t *testing.T) {
	t.Setenv(remoteCodingPilotEnvKey, "on")
	reg := newRemoteCodingPilotTestRegistry(t)
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
	d := cb.ToolDispatcher()
	// Excluded categories: ssh relay names, execute/mutate kinds, spawn,
	// unregistered remote-served names, dynamic aliases, unknown.
	for _, name := range []string{"ssh_read_file", "ssh_bash", "bash", "write_file", "ripgrep", "current_datetime", codingSubAgentSpawnToolName, "unknown_tool", ""} {
		if _, handled, err := d.Dispatch(name, `{}`, "call-1", agent.ToolCallExecutionContext{}); handled || err != nil {
			t.Fatalf("Dispatch(%q) = handled=%v err=%v, want handled=false err=nil", name, handled, err)
		}
	}
}

func TestRemoteCodingBuildPilotDispatcherSkipsUnregisteredNames(t *testing.T) {
	reg := newRemoteCodingPilotTestRegistry(t)
	cb := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{handler: &IMMessageHandler{registry: reg}}}
	names := append(append([]string(nil), remoteCodingPilotToolNames...), "not_a_real_tool")
	d, skipped := buildRemoteCodingPilotDispatcher(cb, reg, names)
	if len(skipped) != 1 || skipped[0] != "not_a_real_tool" {
		t.Fatalf("skipped = %v, want [not_a_real_tool]", skipped)
	}
	for _, h := range d.Handlers() {
		if h == "core:not_a_real_tool" {
			t.Fatal("unregistered name must not be registered")
		}
	}
	if len(d.Handlers()) != len(remoteCodingPilotToolNames) {
		t.Fatalf("handlers = %v, want the %d pilot names", d.Handlers(), len(remoteCodingPilotToolNames))
	}
}
