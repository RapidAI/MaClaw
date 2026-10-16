package main

import (
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func newTUIPilotTestCallbacks() *tuiCallbacks {
	reg := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(reg, agent.CoreToolDeps{})
	return &tuiCallbacks{app: &TUIApp{toolRegistry: reg}}
}

// tuiPilotTestArgs returns per-name arg shapes for the parity run. The bare
// RegisterCoreTools registry actually executes writes, so the mutate shapes
// use t.TempDir() fixtures: write_file writes a fixed file on BOTH paths
// (same path, same content → identical deterministic output; the second call
// overwrites the first, which is fine — outputs are compared, not file
// bytes), and edit_file edits a fixture the test pre-writes. memory has no
// store on the bare registry, so every shape fails deterministically before
// any state change.
func tuiPilotTestArgs(t *testing.T, name string) []string {
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
		fixture := tmp + "/edit.txt"
		if err := os.WriteFile(fixture, []byte("alpha"), 0o600); err != nil {
			t.Fatalf("edit fixture: %v", err)
		}
		return []string{
			`{"path":"` + fixture + `","old_string":"alpha","new_string":"beta"}`,
			`{}`,
		}
	case "memory":
		return []string{`{"action":"recall","query":"x"}`, `{"action":"unknown_op"}`}
	default:
		return []string{`{}`, `{"query":"x","path":"/nonexistent-xyz/","question":""}`}
	}
}

func TestTUIToolDispatcherKillSwitchDefaultOff(t *testing.T) {
	t.Setenv(tuiDispatcherEnvKey, "")
	cb := newTUIPilotTestCallbacks()
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env unset, want nil", d)
	}
	t.Setenv(tuiDispatcherEnvKey, "off")
	if d := newTUIPilotTestCallbacks().ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env=off, want nil", d)
	}
}

func TestTUIToolDispatcherPublishesWhenEnabled(t *testing.T) {
	t.Setenv(tuiDispatcherEnvKey, "on")
	cb := newTUIPilotTestCallbacks()
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
	if len(handlers) != len(tuiPilotToolNames) {
		t.Fatalf("handlers = %v, want %d pilot names", handlers, len(tuiPilotToolNames))
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

// TestTUIToolDispatcherParityMatchesLegacyEntry proves each pilot handler
// enters through the exact legacy entry (tuiCallbacks.ExecuteTool): the
// dispatcher result text must equal ExecuteTool's result for identical
// inputs, and the core loop's outcome classification is what the dispatcher
// branch applies to an empty Outcome — the legacy final rung does the same.
//
// Coverage statement: the bare RegisterCoreTools registry actually executes,
// so the mutate-class shapes perform REAL writes inside t.TempDir() on both
// paths (fixed path + fixed content → identical deterministic output; both
// calls hit the same entry and the second overwrites the first). This is
// stronger than the guiapp IM pilot's nil-handler boundary: TUI's entry is
// wireable with a bare registry, so writes are covered end-to-end through
// ExecuteTool's full prefix (spawn special case, argument parse,
// read-only-child guard, _ctx injection). Loop-level gates (authorizeLoopTool
// admission, replan skip) run upstream of the dispatcher consultation in
// corelib/agent/loop.go and are untouched. A snapshotted read-only-child
// state is NOT constructed here; the guard is verified in place by the
// runtimeReadOnlyChild=false path both calls share.
func TestTUIToolDispatcherParityMatchesLegacyEntry(t *testing.T) {
	cb := newTUIPilotTestCallbacks()
	d, skipped := buildTUIPilotDispatcher(cb, tuiPilotToolNames)
	if len(skipped) != 0 {
		t.Fatalf("skipped = %v, want none", skipped)
	}
	for _, name := range tuiPilotToolNames {
		for _, argsJSON := range tuiPilotTestArgs(t, name) {
			dispResult, handled, err := d.Dispatch(name, argsJSON, "call-1", agent.ToolCallExecutionContext{})
			if err != nil {
				t.Fatalf("Dispatch(%q) error = %v", name, err)
			}
			if !handled {
				t.Fatalf("Dispatch(%q) handled=false, want true", name)
			}
			legacy := cb.ExecuteTool(name, argsJSON)
			if dispResult.Result != legacy {
				t.Fatalf("name=%q args=%s dispatcher=%q legacy=%q", name, argsJSON, dispResult.Result, legacy)
			}
			if dispResult.Result == "" {
				t.Fatalf("name=%q args=%s empty result", name, argsJSON)
			}
		}
	}
}

// TestTUIToolDispatcherIDPathWins proves dispatch serves pilot names through
// the ID path: a decoy NAME-keyed registration for a pilot name must never
// execute, and resolver misses fall through with handled=false.
func TestTUIToolDispatcherIDPathWins(t *testing.T) {
	cb := newTUIPilotTestCallbacks()
	d, skipped := buildTUIPilotDispatcher(cb, tuiPilotToolNames)
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

func TestTUIToolDispatcherNonPilotNamesFallThrough(t *testing.T) {
	t.Setenv(tuiDispatcherEnvKey, "on")
	cb := newTUIPilotTestCallbacks()
	d := cb.ToolDispatcher()
	for _, name := range []string{"bash", "ssh", "task", "unknown_tool", ""} {
		if _, handled, err := d.Dispatch(name, `{}`, "call-1", agent.ToolCallExecutionContext{}); handled || err != nil {
			t.Fatalf("Dispatch(%q) = handled=%v err=%v, want handled=false err=nil", name, handled, err)
		}
	}
}

func TestTUIBuildPilotDispatcherSkipsUnknownNames(t *testing.T) {
	cb := newTUIPilotTestCallbacks()
	names := append(append([]string(nil), tuiPilotToolNames...), "not_a_real_tool")
	d, skipped := buildTUIPilotDispatcher(cb, names)
	if len(skipped) != 1 || skipped[0] != "not_a_real_tool" {
		t.Fatalf("skipped = %v, want [not_a_real_tool]", skipped)
	}
	for _, h := range d.Handlers() {
		if h == "core:not_a_real_tool" {
			t.Fatal("unknown name must not be registered")
		}
	}
	if len(d.Handlers()) != len(tuiPilotToolNames) {
		t.Fatalf("handlers = %v, want the %d pilot names", d.Handlers(), len(tuiPilotToolNames))
	}
}
