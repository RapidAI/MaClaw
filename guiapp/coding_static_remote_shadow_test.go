package guiapp

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func remoteShadowTestIdentity() *trustedCodingInvocationIdentity {
	return &trustedCodingInvocationIdentity{TenantID: "t", PrincipalID: "p", SessionID: "s", RootTaskID: "r", TurnID: "turn-1"}
}

func remoteShadowFixtureAgent(sessionID string) *RemoteCodingSubAgent {
	return &RemoteCodingSubAgent{
		dynamicInvocationIdentity: remoteShadowTestIdentity(),
		sessionID:                 sessionID,
		role:                      codingRoleWorker,
	}
}

func TestCodingStaticRemoteShadowPlanComputesForVerifiedBinding(t *testing.T) {
	agent := remoteShadowFixtureAgent("ssh-session-42")
	prepared := prepareCodingStaticRemoteShadowPlanForRemoteSubagent(agent, codingRequestImplementation, time.Now().UTC())
	if prepared == nil {
		t.Fatal("shadow plan must compute for a verified binding")
	}
	if len(prepared.Plan.Selections) != 2 {
		t.Fatalf("selections = %d, want 2 (fs.read.remote + repo.inspect.remote)", len(prepared.Plan.Selections))
	}
	caps := map[string]bool{}
	for _, selection := range prepared.Plan.Selections {
		caps[string(selection.FitProof.MatchedCapability)] = true
		// The FitProof binding must be keyed to exactly this verified session.
		if !strings.Contains(selection.Provider.ProviderID, "ssh-session-42") {
			t.Fatalf("selection %q provider %q is not bound to the verified session", selection.AdapterName, selection.Provider.ProviderID)
		}
	}
	if !caps[string(tool.CapabilityFSReadRemote)] || !caps[string(tool.CapabilityRepoInspectRemote)] {
		t.Fatalf("capabilities = %v, want fs.read.remote + repo.inspect.remote", caps)
	}
	if prepared.Plan.CatalogGeneration == 0 {
		t.Fatal("shadow catalog generation must be recorded for the observation")
	}
}

func TestCodingStaticRemoteShadowRejectsBindingSwap(t *testing.T) {
	// A local-flavoured binding (wrong HostKind) can never satisfy the remote
	// envelope, even with a plausible handle: the specs vanish and the plan
	// degrades to catalog_incomplete instead of inventing a remote read
	// selection. Local and remote bindings are different types and different
	// completion rules by design.
	swapped := codingStaticRemoteExecutionEnvelope{
		Identity: remoteShadowTestIdentity(),
		Session:  codingStaticRemoteSessionBinding{SessionHandle: "local-ws-handle", HostKind: "local"},
		Posture:  codingRequestImplementation,
		Role:     codingRoleWorker,
	}
	if swapped.Session.complete() {
		t.Fatal("a local-host binding must never complete a remote session binding")
	}
	specs, err := codingStaticRemoteReadOnlyProviderSpecs(swapped)
	if err != nil || specs != nil {
		t.Fatalf("swapped binding specs = %v, %v; want nil, nil (rejected, no fallback)", specs, err)
	}
	prepared, err := prepareCodingStaticRemoteShadowPlan(swapped, nil, tool.PlanningBudget{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("shadow plan with swapped binding must degrade, not error: %v", err)
	}
	if len(prepared.Plan.Selections) != 0 {
		t.Fatalf("swapped binding produced %d selections, want 0", len(prepared.Plan.Selections))
	}
	foundIncomplete := false
	for _, unmet := range prepared.Plan.Unmet {
		if unmet.ReasonCode == "catalog_incomplete" {
			foundIncomplete = true
		}
	}
	if !foundIncomplete {
		t.Fatalf("swapped binding Unmet = %+v, want catalog_incomplete", prepared.Plan.Unmet)
	}
	// The reverse direction is structural: the local envelope requires a
	// codingStaticWorkspaceBinding (a different type), so a remote session
	// handle cannot be passed where a local workspace is expected.
}

func TestCodingStaticRemoteShadowAbsentBindingDegrades(t *testing.T) {
	// Q① accepted degradation: no verified session → no remote read
	// selections; the gap is recorded as catalog_incomplete while the legacy
	// band keeps serving ssh_read_file / ssh_list_dir.
	agent := remoteShadowFixtureAgent("")
	prepared := prepareCodingStaticRemoteShadowPlanForRemoteSubagent(agent, codingRequestImplementation, time.Now().UTC())
	if prepared == nil {
		t.Fatal("absent binding must still produce an observable (degraded) plan")
	}
	if len(prepared.Plan.Selections) != 0 {
		t.Fatalf("absent binding produced %d selections, want 0", len(prepared.Plan.Selections))
	}
	observation := newCodingStaticCompatibilitySurfaceObservation(codingStaticCompatibilityHostRemote, 7, codingRequestImplementation, nil, agent.dynamicInvocationIdentity, prepared)
	if observation.ShadowState != "catalog_incomplete" {
		t.Fatalf("ShadowState = %q, want catalog_incomplete", observation.ShadowState)
	}
	found := false
	for _, reason := range observation.UnmetReasons {
		if reason == "catalog_incomplete" {
			found = true
		}
	}
	if !found {
		t.Fatalf("UnmetReasons = %v, want catalog_incomplete recorded as the accepted-degradation reason", observation.UnmetReasons)
	}
}

func remoteShadowLegacyDefs() []map[string]interface{} {
	return []map[string]interface{}{
		{"type": "function", "function": map[string]interface{}{"name": "ssh_read_file", "description": "read remote file"}},
		{"type": "function", "function": map[string]interface{}{"name": "ssh_list_dir", "description": "list remote dir"}},
		{"type": "function", "function": map[string]interface{}{"name": "ssh_bash", "description": "remote shell"}},
	}
}

func TestCodingStaticRemoteShadowObservationMatchesLocalShape(t *testing.T) {
	agent := remoteShadowFixtureAgent("ssh-session-42")
	prepared := prepareCodingStaticRemoteShadowPlanForRemoteSubagent(agent, codingRequestImplementation, time.Now().UTC())
	if prepared == nil {
		t.Fatal("shadow plan must compute")
	}
	observation := newCodingStaticCompatibilitySurfaceObservation(codingStaticCompatibilityHostRemote, 3, codingRequestImplementation, remoteShadowLegacyDefs(), agent.dynamicInvocationIdentity, prepared)
	if observation.HostKind != codingStaticCompatibilityHostRemote {
		t.Fatalf("HostKind = %q", observation.HostKind)
	}
	if observation.ShadowState != "prepared" {
		t.Fatalf("ShadowState = %q, want prepared", observation.ShadowState)
	}
	if observation.PlanID == "" || observation.CatalogGeneration == 0 {
		t.Fatalf("plan identity must be recorded: %+v", observation)
	}
	if observation.RootTaskID != "r" || observation.TurnID != "turn-1" {
		t.Fatalf("identity projection = %q/%q", observation.RootTaskID, observation.TurnID)
	}
	// 对账: the shadow covers the legacy remote read capability, so
	// fs.read.remote must not appear as legacy-only; repo.inspect.remote has
	// no legacy remote tool yet and is honestly recorded as shadow-only.
	for _, cap := range observation.LegacyOnlyCapabilities {
		if cap == string(tool.CapabilityFSReadRemote) {
			t.Fatalf("fs.read.remote must not be legacy-only when the shadow covers it: %v", observation.LegacyOnlyCapabilities)
		}
	}
	foundShadowOnlyRepo := false
	for _, cap := range observation.ShadowOnlyCapabilities {
		if cap == string(tool.CapabilityRepoInspectRemote) {
			foundShadowOnlyRepo = true
		}
	}
	if !foundShadowOnlyRepo {
		t.Fatalf("repo.inspect.remote must be recorded shadow-only (no legacy remote git tool yet): %v", observation.ShadowOnlyCapabilities)
	}
	// ssh_bash's shell.execute.remote_host is effectful and outside the
	// read-only shadow: it must show as legacy-only.
	foundShellLegacyOnly := false
	for _, cap := range observation.LegacyOnlyCapabilities {
		if cap == string(tool.CapabilityShellExecuteRemoteHost) {
			foundShellLegacyOnly = true
		}
	}
	if !foundShellLegacyOnly {
		t.Fatalf("shell.execute.remote_host must be legacy-only: %v", observation.LegacyOnlyCapabilities)
	}
}

func TestCodingStaticRemoteShadowRedLines(t *testing.T) {
	agent := remoteShadowFixtureAgent("ssh-session-42")
	cb := &remoteCodingCallbacks{agent: agent}
	legacy := namesOfCodingStaticCompatDefs(remoteShadowLegacyDefs())
	cb.recordStaticCompatibilitySurface(remoteShadowLegacyDefs(), 9)
	observation := cb.lastStaticCompatibilitySurfaceObservation()
	if observation.ShadowState != "prepared" {
		t.Fatalf("recorded ShadowState = %q, want prepared (real plan, not not_prepared)", observation.ShadowState)
	}
	// Red line 1: the rendered surface is unchanged — the observation records
	// exactly the legacy names that were rendered, never shadow names.
	if len(observation.RenderedToolNames) != len(legacy) {
		t.Fatalf("rendered names changed: %v vs %v", observation.RenderedToolNames, legacy)
	}
	for _, name := range observation.RenderedToolNames {
		if strings.HasPrefix(name, "coding_static_") {
			t.Fatalf("shadow adapter name leaked into the rendered surface: %v", name)
		}
	}
	// Red line 2: shadow plan never drives rendering — with the real surface
	// installed, the fence admits exactly the legacy names and never a shadow
	// adapter name.
	cb.setStaticCompatibilitySurface(remoteShadowLegacyDefs())
	for _, name := range legacy {
		if !cb.staticCompatibilityToolAllowed(name) {
			t.Fatalf("legacy name %q was rejected after shadow wiring", name)
		}
	}
	if cb.staticCompatibilityToolAllowed(codingStaticRemoteReadAdapter) {
		t.Fatal("shadow adapter name must never be admitted by the legacy surface fence")
	}
	// Red line 3: dynamic aliases stay closed.
	if cb.codingDynamicAliasesMayMaterialize() {
		t.Fatal("codingDynamicAliasesMayMaterialize must stay false")
	}
}

func namesOfCodingStaticCompatDefs(defs []map[string]interface{}) []string {
	var out []string
	for _, def := range defs {
		if fn, ok := def["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}
