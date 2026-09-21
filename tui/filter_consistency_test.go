package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// filterConsistencyTestRegistry registers a representative registry: names
// inside and outside the light-turn allowlist.
func filterConsistencyTestRegistry(t *testing.T) *agent.CoreToolRegistry {
	t.Helper()
	reg := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(reg, agent.CoreToolDeps{})
	return reg
}

func namesOf(defs []map[string]interface{}) []string {
	var out []string
	for _, def := range defs {
		if name := tooldef.Name(def); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestPipeCallbacksBuildToolsDefaultStateUnchanged: with no env override the
// light filter is a provable no-op — output is byte-identical to the raw
// registry definitions.
func TestPipeCallbacksBuildToolsDefaultStateUnchanged(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	reg := filterConsistencyTestRegistry(t)
	cb := &pipeCallbacks{app: &TUIApp{toolRegistry: reg}}
	got := namesOf(cb.BuildTools("do something"))
	want := namesOf(reg.BuildDefinitions())
	if len(got) != len(want) {
		t.Fatalf("default-state surface changed: got %d tools, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default-state surface changed at %d: got %v want %v", i, got, want)
		}
	}
}

// TestPipeCallbacksBuildToolsEnvForcedLightPolicyPin: policy-fix pin (phase0
// baseline §1.2/§1.3 finding #2) — a globally forced-light env now yields the
// light surface on pipe mode too, matching its light system prompt.
func TestPipeCallbacksBuildToolsEnvForcedLightPolicyPin(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	reg := filterConsistencyTestRegistry(t)
	cb := &pipeCallbacks{app: &TUIApp{toolRegistry: reg}}
	names := namesOf(cb.BuildTools("do something"))
	if containsName(names, "bash") || containsName(names, "read_file") || containsName(names, "write_file") {
		t.Fatalf("forced-light pipe surface leaked full-only tools: %v", names)
	}
	for _, allowlisted := range []string{"web_search", "knowledge_search", "memory"} {
		if !containsName(names, allowlisted) {
			t.Fatalf("forced-light pipe surface missing allowlisted %q: %v", allowlisted, names)
		}
	}
}

func TestRPCCallbacksBuildToolsDefaultStateUnchanged(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	reg := filterConsistencyTestRegistry(t)
	cb := &rpcCallbacks{app: &TUIApp{toolRegistry: reg}}
	got := namesOf(cb.BuildTools("do something"))
	want := namesOf(reg.BuildDefinitions())
	if len(got) != len(want) {
		t.Fatalf("default-state surface changed: got %d tools, want %d", len(got), len(want))
	}
}

// TestRPCCallbacksBuildToolsEnvForcedLightPolicyPin: policy-fix pin — forced
// light yields the light surface on RPC mode. Note the blast radius: RPC mode
// trusts the caller (IsToolAllowed is恒true), so a forced-light deployment
// narrows what RPC clients can see; that is the intended policy semantic.
func TestRPCCallbacksBuildToolsEnvForcedLightPolicyPin(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	reg := filterConsistencyTestRegistry(t)
	cb := &rpcCallbacks{app: &TUIApp{toolRegistry: reg}}
	names := namesOf(cb.BuildTools("do something"))
	if containsName(names, "bash") || containsName(names, "read_file") {
		t.Fatalf("forced-light rpc surface leaked full-only tools: %v", names)
	}
	if !containsName(names, "web_search") {
		t.Fatalf("forced-light rpc surface missing web_search: %v", names)
	}
}

func TestWeixinCallbacksBuildToolsDefaultStateUnchanged(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	reg := filterConsistencyTestRegistry(t)
	cb := &tuiWeixinCallbacks{app: &TUIApp{toolRegistry: reg}}
	got := namesOf(cb.BuildTools("do something"))
	want := namesOf(reg.BuildDefinitions())
	if len(got) != len(want) {
		t.Fatalf("default-state surface changed: got %d tools, want %d", len(got), len(want))
	}
}

// TestWeixinCallbacksBuildToolsEnvForcedLightPolicyPin: policy-fix pin —
// forced light yields the light surface on the WeChat gateway.
func TestWeixinCallbacksBuildToolsEnvForcedLightPolicyPin(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	reg := filterConsistencyTestRegistry(t)
	cb := &tuiWeixinCallbacks{app: &TUIApp{toolRegistry: reg}}
	names := namesOf(cb.BuildTools("do something"))
	if containsName(names, "bash") || containsName(names, "read_file") {
		t.Fatalf("forced-light weixin surface leaked full-only tools: %v", names)
	}
	if !containsName(names, "web_search") {
		t.Fatalf("forced-light weixin surface missing web_search: %v", names)
	}
}

// TestBtwAndLoopCycleFiltersStayAbsent: the two deliberate exceptions keep
// their historical shape — /btw's minimal set and /loop's fixed full-profile
// cycle are policy choices, not missing filters.
func TestBtwAndLoopCycleFiltersStayAbsent(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	reg := filterConsistencyTestRegistry(t)
	btw := &tuiBtwCallbacks{app: &TUIApp{toolRegistry: reg}}
	if names := namesOf(btw.BuildTools("side query")); len(names) != len(namesOf(buildTuiBtwToolDefinitions(btw.app))) {
		t.Fatalf("btw surface changed under forced light: %v", names)
	}
	cycle := &tuiLoopCycleCallbacks{parent: &tuiLoopCommandCallbacks{app: &TUIApp{toolRegistry: reg}}}
	cycleNames := namesOf(cycle.BuildTools("make tests pass"))
	if len(cycleNames) != len(loopCycleToolNames) {
		t.Fatalf("loop-cycle surface changed under forced light: %v", cycleNames)
	}
}

func TestSchedulerCallbacksBuildToolsDefaultStateUnchanged(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	reg := filterConsistencyTestRegistry(t)
	cb := &tuiSchedulerCallbacks{app: &TUIApp{toolRegistry: reg}}
	got := namesOf(cb.BuildTools("run the backup"))
	want := namesOf(reg.BuildDefinitions())
	if len(got) != len(want) {
		t.Fatalf("default-state surface changed: got %d tools, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default-state surface changed at %d: got %v want %v", i, got, want)
		}
	}
}

// TestSchedulerCallbacksBuildToolsEnvForcedLightPolicyPin: policy-fix pin —
// a forced-light env yields the light surface for background scheduled tasks
// too, matching the light system prompt the scheduler's RunLoop turn builds.
func TestSchedulerCallbacksBuildToolsEnvForcedLightPolicyPin(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	reg := filterConsistencyTestRegistry(t)
	cb := &tuiSchedulerCallbacks{app: &TUIApp{toolRegistry: reg}}
	names := namesOf(cb.BuildTools("run the backup"))
	if containsName(names, "bash") || containsName(names, "read_file") || containsName(names, "write_file") {
		t.Fatalf("forced-light scheduler surface leaked full-only tools: %v", names)
	}
	if !containsName(names, "web_search") || !containsName(names, "knowledge_search") {
		t.Fatalf("forced-light scheduler surface missing allowlisted lookups: %v", names)
	}
}
