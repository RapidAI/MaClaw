package agentservice

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

var pilotTestNames = []string{
	"knowledge_search", "knowledge_list_sources", "knowledge_stats",
	"knowledge_list_source_labels", "knowledge_source_detail",
	"knowledge_context_pack", "knowledge_image_search",
	"web_search", "web_fetch", "ask_user",
	"read_file", "read_tool_result", "read_document", "list_directory",
	"Glob", "ripgrep", "FileRead",
	"write_file", "edit_file", "memory",
}

// pilotTestArgs returns per-name arg shapes for the parity run. web_search/
// web_fetch must NOT receive a non-empty query/url here — that would attempt
// a real network call. write_file/edit_file use t.TempDir() fixtures and
// actually execute: this host resolves absolute paths even without a
// workspace (resolveWorkspacePath falls back to filepath.Abs), so both paths
// perform the same real write inside the temp dir and must return identical
// deterministic output. edit_file's fixture is reset before the legacy call
// because a successful edit is not idempotent — parity requires both calls
// to observe the same initial state. memory has no store on the bare
// callbacks, so every shape fails deterministically before any state change.
func pilotTestArgs(t *testing.T, name string) []string {
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
		return []string{
			`{"path":"` + fixture + `","old_string":"alpha","new_string":"beta"}`,
			`{}`,
		}
	case "memory":
		return []string{`{"action":"recall","query":"x"}`, `{"action":"unknown_op"}`}
	default:
		return []string{`{}`, `{"query":"x","source_id":"nope","path":"/nonexistent-xyz/"}`}
	}
}

// editFixturePath extracts the fixture path from the edit_file arg shape so
// the parity loop can reset it between the dispatcher and legacy calls.
func editFixturePath(t *testing.T, argsJSON string) string {
	t.Helper()
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return ""
	}
	return args.Path
}

func TestToolDispatcherKillSwitchDefaultOff(t *testing.T) {
	t.Setenv(toolDispatcherEnvKey, "")
	cb := &coreAgentCallbacks{}
	if d := cb.ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env unset, want nil", d)
	}
	t.Setenv(toolDispatcherEnvKey, "off")
	if d := (&coreAgentCallbacks{}).ToolDispatcher(); d != nil {
		t.Fatalf("ToolDispatcher() = %v with env=off, want nil", d)
	}
}

func TestToolDispatcherPublishesWhenEnabled(t *testing.T) {
	t.Setenv(toolDispatcherEnvKey, "on")
	cb := &coreAgentCallbacks{}
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
	if len(handlers) != len(pilotTestNames) {
		t.Fatalf("handlers = %v, want %d pilot names", handlers, len(pilotTestNames))
	}
	for _, h := range handlers {
		if !strings.HasPrefix(h, "core:") {
			t.Fatalf("handler %q is not an ID-string registration (ID-keyed mode required)", h)
		}
	}
	if !stringSliceContains(handlers, "core:Glob") || !stringSliceContains(handlers, "core:ripgrep") || !stringSliceContains(handlers, "core:FileRead") {
		t.Fatalf("exact-case shared-host IDs missing from handlers: %v", handlers)
	}
}

// TestToolDispatcherIDPathWins proves dispatch serves pilot names through the
// ID path: a decoy NAME-keyed registration for a pilot name must never execute
// (ID handlers win over name handlers), and non-pilot names the resolver
// misses fall through with handled=false even though name-keyed lookups exist.
func TestToolDispatcherIDPathWins(t *testing.T) {
	cb := &coreAgentCallbacks{}
	d, skipped := buildCoreAgentPilotDispatcher(cb, coreAgentPilotHandlers(cb))
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
	// Resolver miss: unknown name falls through even though the name-handler
	// map is non-empty (decoy is a different name).
	if _, handled, _ := d.Dispatch("not_registered", `{}`, "call-1", agent.ToolCallExecutionContext{}); handled {
		t.Fatal("resolver miss must fall through to legacy (handled=false)")
	}
}

// TestToolDispatcherParityMatchesExecuteToolCall proves each pilot handler
// enters through the same legacy entry point as the executor chain: the
// dispatcher result must equal ExecuteToolCall's result for identical inputs.
//
// Coverage statement (review finding 2): this exercises ExecuteToolCall's
// real prefix — CanonicalizeToolCallJSON, the runtimeToolInvoker branch
// (absent here), and the ExecuteToolStructured preamble guards — under
// (a) an empty callbacks (all guards no-op) and (b) a callbacks with a
// populated AppConfig and an ACTIVE hardware-expert allow set configured to
// admit the pilot names, where the test also asserts a non-pilot name is
// rejected by the same guard, proving the guard ran. A fully managed
// dynamicSemanticSurface with HostCallJournal replay is NOT constructed here
// (too heavy for a unit test); that surface never reaches the switch for
// first-party pilot names anyway — ExecuteToolCall's semantic branch only
// fires for grant-surface names, and the pilot names are first-party.
// Regarding CanonicalizeToolCallJSON: it only rewrites browser and
// local-file-search tool calls, none of which are pilot names, so no arg
// shape among the pilot names triggers a rewrite; the canonicalizer still
// runs on every dispatched call (it is inside executeToolCallLegacy) — it is
// simply a pass-through for these names.
func TestToolDispatcherParityMatchesExecuteToolCall(t *testing.T) {
	t.Setenv(toolDispatcherEnvKey, "")
	cases := []struct {
		name string
		cb   *coreAgentCallbacks
	}{
		{"empty callbacks", &coreAgentCallbacks{}},
		{"guards active, pilot allowed", &coreAgentCallbacks{
			appCfg: corelib.AppConfig{Language: "zh"},
			instance: Instance{Metadata: map[string]string{
				"hardware_assistant_mode":    "expert",
				"hardware_expert_tools_json": `["knowledge_search","knowledge_list_sources","knowledge_stats","knowledge_list_source_labels","knowledge_source_detail","knowledge_context_pack","knowledge_image_search","web_search","web_fetch","ask_user","read_file","read_tool_result","read_document","list_directory","Glob","ripgrep","FileRead"]`,
			}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, skipped := buildCoreAgentPilotDispatcher(tc.cb, coreAgentPilotHandlers(tc.cb))
			if len(skipped) != 0 {
				t.Fatalf("skipped = %v, want none", skipped)
			}
			for _, name := range pilotTestNames {
				for _, argsJSON := range pilotTestArgs(t, name) {
					dispResult, handled, err := d.Dispatch(name, argsJSON, "call-1", agent.ToolCallExecutionContext{})
					if err != nil {
						t.Fatalf("Dispatch(%q) error = %v", name, err)
					}
					if !handled {
						t.Fatalf("Dispatch(%q) handled=false, want true", name)
					}
					// edit_file is not idempotent: reset the fixture so the
					// legacy call observes the same initial state.
					if name == "edit_file" {
						if fixture := editFixturePath(t, argsJSON); fixture != "" {
							if err := os.WriteFile(fixture, []byte("alpha"), 0o600); err != nil {
								t.Fatalf("edit fixture reset: %v", err)
							}
						}
					}
					legacy := tc.cb.ExecuteToolCall(name, argsJSON, "call-1")
					if dispResult.Result != legacy.Result || dispResult.Outcome != legacy.Outcome {
						t.Fatalf("name=%q args=%s dispatcher={%q,%s} legacy={%q,%s}",
							name, argsJSON, dispResult.Result, dispResult.Outcome, legacy.Result, legacy.Outcome)
					}
					if dispResult.Result == "" {
						t.Fatalf("name=%q args=%s empty result", name, argsJSON)
					}
				}
			}
		})
	}

	t.Run("guard is active for non pilot names", func(t *testing.T) {
		cb := &coreAgentCallbacks{
			instance: Instance{Metadata: map[string]string{
				"hardware_assistant_mode":    "expert",
				"hardware_expert_tools_json": `["knowledge_search"]`,
			}},
		}
		blocked := cb.ExecuteToolCall("web_search", `{"query":"x"}`, "call-2")
		if !strings.Contains(blocked.Result, "not allowed by the selected hardware expert") {
			t.Fatalf("web_search under expert allow set = %q, want hardware-expert rejection (guard must be active)", blocked.Result)
		}
		allowed := cb.ExecuteToolCall("knowledge_search", `{"query":"x"}`, "call-3")
		if !strings.HasPrefix(allowed.Result, "Error: knowledge base is not configured") {
			t.Fatalf("knowledge_search under expert allow set = %q, want guard pass-through to knowledge error", allowed.Result)
		}
	})
}

func TestToolDispatcherNonPilotNamesFallThrough(t *testing.T) {
	t.Setenv(toolDispatcherEnvKey, "on")
	cb := &coreAgentCallbacks{}
	d := cb.ToolDispatcher()
	for _, name := range []string{"bash", "ssh", "task", "database", "delegate_task", "unknown_tool", ""} {
		if _, handled, err := d.Dispatch(name, `{}`, "call-1", agent.ToolCallExecutionContext{}); handled || err != nil {
			t.Fatalf("Dispatch(%q) = handled=%v err=%v, want handled=false err=nil", name, handled, err)
		}
	}
}

func TestBuildCoreAgentPilotDispatcherSkipsUnknownNames(t *testing.T) {
	cb := &coreAgentCallbacks{}
	handlers := coreAgentPilotHandlers(cb)
	handlers["not_a_real_tool"] = func(_, _, _ string, _ agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
		return agent.ToolExecutionResult{}, nil
	}
	d, skipped := buildCoreAgentPilotDispatcher(cb, handlers)
	if len(skipped) != 1 || skipped[0] != "not_a_real_tool" {
		t.Fatalf("skipped = %v, want [not_a_real_tool]", skipped)
	}
	for _, name := range d.Handlers() {
		if name == "not_a_real_tool" {
			t.Fatal("unknown name must not be registered")
		}
	}
	if len(d.Handlers()) != len(pilotTestNames) {
		t.Fatalf("handlers = %v, want the %d pilot names", d.Handlers(), len(pilotTestNames))
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
