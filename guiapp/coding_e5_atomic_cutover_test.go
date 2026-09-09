package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// E5 keeps the first dynamic family behind the single semantic holder.  The
// legacy selectors remain deny-only compatibility probes and can never reach
// an executable dispatcher.
func TestCodingE5FirstFamilyLegacyDispatcherIsDenyOnly(t *testing.T) {
	if !codingDynamicFirstFamilyLegacyRemoved() {
		t.Fatal("first-family cutover marker is not installed")
	}
	for _, name := range []string{"manage_skill", "call_mcp_tool"} {
		if !isLegacyCodingDynamicGateway(name) {
			t.Fatalf("legacy selector %q lost its deny-only fence", name)
		}
		local := (&codingSubAgentCallbacks{subagent: &CodingSubAgent{}}).ExecuteToolStructured(name, `{}`)
		if !strings.Contains(local.Result, "catalog_incomplete") {
			t.Fatalf("legacy selector %q reached an executable path: %#v", name, local)
		}
	}
	// The first local inspection family is likewise stale once a semantic
	// holder owns the request.  Use an inert relay solely to exercise the
	// callback fence; no provider or coordinator is contacted by this check.
	localBound := &codingSubAgentCallbacks{dynamicLifecycleRelay: &codingBoundDynamicRequestLifecycleRelay{}}
	remoteBound := &remoteCodingCallbacks{dynamicLifecycleRelay: &codingBoundDynamicRequestLifecycleRelay{}}
	for _, name := range []string{"Glob", "ripgrep", "read_file", "list_directory", "git_diff"} {
		if got := localBound.ExecuteToolStructured(name, `{}`); !strings.Contains(got.Result, "catalog_incomplete") {
			t.Fatalf("bound local legacy selector %q reached dispatcher: %#v", name, got)
		}
		if got := remoteBound.ExecuteToolStructured(name, `{}`); !strings.Contains(got.Result, "catalog_incomplete") {
			t.Fatalf("bound remote legacy selector %q reached dispatcher: %#v", name, got)
		}
	}
}

func TestCodingE5QualificationDerivesWiredAndEnabled(t *testing.T) {
	// An actual configured Responses-WS endpoint receives all E1-E4 evidence;
	// the two release gates are then derived by the composition predicate.
	q := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{
		Protocol: "openai", WireAPI: "responses-ws", URL: "wss://loopback.invalid/responses",
	})
	if !q.Wired || !q.Enabled || !q.eligible() {
		t.Fatalf("qualified endpoint did not derive release gates: %#v", q)
	}
	// Evidence is independent of endpoint text; an empty endpoint is rejected
	// later as a runtime readiness check during relay construction.
	bare := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if !bare.Wired || !bare.Enabled || !bare.eligible() {
		t.Fatalf("qualification evidence unexpectedly depended on endpoint text: %#v", bare)
	}
}

func TestCodingE5QualificationOverrideCannotOpenAliasPrompt(t *testing.T) {
	saved := codingDynamicProductionAdapterQualificationOverride
	t.Cleanup(func() { codingDynamicProductionAdapterQualificationOverride = saved })
	qualified := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	codingDynamicProductionAdapterQualificationOverride = &qualified
	identity := &trustedCodingInvocationIdentity{TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	local := &codingSubAgentCallbacks{subagent: &CodingSubAgent{cfg: corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}, dynamicInvocationIdentity: identity}, dynamicLifecycleRelay: &codingBoundDynamicRequestLifecycleRelay{}}
	remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{cfg: corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}, dynamicInvocationIdentity: identity}, dynamicLifecycleRelay: &codingBoundDynamicRequestLifecycleRelay{}}
	if local.codingDynamicAliasesMayMaterialize() || remote.codingDynamicAliasesMayMaterialize() {
		t.Fatal("hermetic qualification override leaked into alias prompt")
	}
}
