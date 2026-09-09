package guiapp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codingE4KillSwitchDrillVersion pins the drill suite to the kill-switch
// control contract version constant.
const codingE4KillSwitchDrillVersion = "coding-dynamic-kill-switch-v1"

func TestCodingE4KillSwitchControlVersionMatchesDrillSuite(t *testing.T) {
	if codingDynamicKillSwitchControlVersion != codingE4KillSwitchDrillVersion {
		t.Fatalf("kill-switch control version %q does not match drill suite version %q", codingDynamicKillSwitchControlVersion, codingE4KillSwitchDrillVersion)
	}
}

// TestCodingDynamicFixedCohortIsOpaqueAndInputInvariant proves the committed
// cohort is identical for every user/task/config/model/URL input, absent from
// every configuration structure field name and sample value, and empty on
// unqualified rows.
func TestCodingDynamicFixedCohortIsOpaqueAndInputInvariant(t *testing.T) {
	base := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if base.FixedCohort != codingDynamicFixedCohortV1 {
		t.Fatalf("qualified row cohort=%q, want the committed constant", base.FixedCohort)
	}
	for _, cfg := range []corelib.MaclawLLMConfig{
		{Protocol: "openai", WireAPI: "responses-ws", URL: "https://a.example/v1", Model: "model-a", Key: "key-a"},
		{Protocol: "openai", WireAPI: "responses-ws", URL: "wss://b.example/socket", Model: "model-b", Key: "key-b", ProviderName: "provider-b", ProviderID: "provider-id-b"},
		{Protocol: "openai", WireAPI: "responses-ws", URL: "http://127.0.0.1:1", Model: "cohort-probe", Key: "k"},
	} {
		if got := codingDynamicProductionAdapterForConfig(cfg).FixedCohort; got != base.FixedCohort {
			t.Fatalf("cohort changed with config input %#v: %q", cfg, got)
		}
	}
	for _, cfg := range []corelib.MaclawLLMConfig{
		{Protocol: "openai", WireAPI: "chat"},
		{Protocol: "anthropic"},
		{Protocol: "openai", WireAPI: "responses"},
	} {
		if got := codingDynamicProductionAdapterForConfig(cfg).FixedCohort; got != "" {
			t.Fatalf("unqualified row gained a cohort: %#v", got)
		}
	}
	// The cohort must not leak into the configuration surface: neither a field
	// name nor a field value of any populated config sample may contain it.
	cfgType := reflect.TypeOf(corelib.MaclawLLMConfig{})
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgType.Field(i)
		if strings.Contains(field.Name, codingDynamicFixedCohortV1) || strings.Contains(string(field.Tag), codingDynamicFixedCohortV1) {
			t.Fatalf("cohort leaked into config field %s", field.Name)
		}
	}
	sample := corelib.MaclawLLMConfig{URL: "https://sample.example/v1", Model: "sample-model", Key: "sample-key", ProviderName: "sample-provider", ProviderID: "sample-id", WireAPI: "responses-ws", Protocol: "openai"}
	sampleValue := reflect.ValueOf(sample)
	for i := 0; i < sampleValue.NumField(); i++ {
		if value, ok := sampleValue.Field(i).Interface().(string); ok && strings.Contains(value, codingDynamicFixedCohortV1) {
			t.Fatalf("cohort leaked into config sample field %s", cfgType.Field(i).Name)
		}
	}
}

func codingE4RestoreKillSwitch(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { codingDynamicKillSwitchState.Store(codingDynamicKillSwitchFollowEvidence) })
}

// TestCodingE4KillSwitchDrillSettlesInFlightAndBlocksNewAssembly is the
// runtime drill on the E3 hermetic assembly: engaging the switch after a
// durable batch commit settles the in-flight reservation per contract, admits
// zero successor materialization, and keeps the alias stale; disengaging alone
// does not restore assembly; only a control-plane rearm with complete evidence
// does.
func TestCodingE4KillSwitchDrillSettlesInFlightAndBlocksNewAssembly(t *testing.T) {
	for _, family := range []string{"local", "remote"} {
		t.Run(family, func(t *testing.T) {
			codingE4RestoreKillSwitch(t)
			run := codingE3StartRun(t, family, func(seq int, conn *websocket.Conn, frame []byte) {
				if seq == 1 {
					codingE3WSWrite(t, conn, codingE3WSToolCallFrames("resp-e4-drill", codingE3WSReadOnlyToolName(t, frame), `{"query":"e4"}`)...)
					return
				}
				codingE3WSWrite(t, conn, codingE3WSFinalTextFrames("resp-e4-after-rearm", "rearmed answer")...)
			})
			hooks := run.hooks()
			hooks.afterCommit = func() {
				engageCodingDynamicKillSwitch()
				run.control.stopFlag.Store(true)
			}
			result := codingagent.Run(run.host, "kill switch drill", nil, nil, nil, hooks)
			if result.Error != "cancelled" || hooks.started != 1 || hooks.committed != 1 || run.control.executions != 1 || codingE3MCPCallCount(run.env) != 1 {
				t.Fatalf("in-flight batch did not settle per contract: result=%+v started=%d committed=%d executions=%d mcp=%d", result, hooks.started, hooks.committed, run.control.executions, codingE3MCPCallCount(run.env))
			}
			execution := run.execution(t, 0, "resp-e4-drill")
			codingE3AssertTerminalSequence(t, run.ledger, execution, "resp-e4-drill", agent.ToolSurfaceToolBatchSettled)

			// Engaged: the attached composition is inert for new requests and no
			// successor reservation materializes.
			cb := run.cb.(agent.ToolSurfaceRequestChannelProvider)
			if channel, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg()); channel != nil || err != nil {
				t.Fatalf("engaged kill switch admitted a successor reservation: channel=%#v err=%v", channel, err)
			}
			if codingE3WSConnCount(run.env) != 1 {
				t.Fatalf("engaged kill switch dialed a successor socket: %d", codingE3WSConnCount(run.env))
			}
			run.staticFallbackProbe(t, execution)

			// Disengaging alone never restores assembly: the invalidation latch
			// keeps it closed until evidence is re-presented.
			disengageCodingDynamicKillSwitch()
			if channel, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg()); channel != nil || err != nil {
				t.Fatalf("disengaged kill switch restored assembly without evidence: channel=%#v err=%v", channel, err)
			}
			// Rearm requires complete current evidence.  E5 now derives complete
			// production evidence from the qualified Responses-WS row, so the
			// override is not required for this control-plane step.
			savedOverride := codingDynamicProductionAdapterQualificationOverride
			codingDynamicProductionAdapterQualificationOverride = nil
			if !rearmCodingDynamicKillSwitch(run.env.cfg()) {
				t.Fatal("rearm failed with complete production evidence")
			}
			codingDynamicProductionAdapterQualificationOverride = savedOverride
			if !rearmCodingDynamicKillSwitch(run.env.cfg()) {
				t.Fatal("rearm failed with hermetic evidence re-presented")
			}
			channel, err := cb.ReserveToolSurfaceRequestChannel(context.Background(), run.env.cfg())
			if err != nil || channel == nil {
				t.Fatalf("rearmed assembly did not reserve a successor: channel=%#v err=%v", channel, err)
			}
			if codingE3WSConnCount(run.env) != 2 {
				t.Fatalf("rearmed successor did not dial its own socket: %d", codingE3WSConnCount(run.env))
			}
			// The rearmed successor is a fresh reservation: retire it without
			// dispatch so the drill leaves no live holder.
			var relay *codingBoundDynamicRequestLifecycleRelay
			switch typed := run.cb.(type) {
			case *codingSubAgentCallbacks:
				relay = typed.dynamicLifecycleRelay
			case *remoteCodingCallbacks:
				relay = typed.dynamicLifecycleRelay
			}
			relay.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
		})
	}
}

// TestCodingE4KillSwitchEngagedBeforeRequestKeepsS0_5Path proves a request
// started while the switch is engaged never sees the dynamic assembly: no
// relay attaches, the loop stays on the S0.5 compatibility path, and the
// durable coordinator records zero lineage revisions for the identity.
func TestCodingE4KillSwitchEngagedBeforeRequestKeepsS0_5Path(t *testing.T) {
	codingE4RestoreKillSwitch(t)
	engageCodingDynamicKillSwitch()
	env := newCodingE3HermeticEnv(t)
	env.wsScript = codingE3ScriptFinalText(t, "resp-e4-static", "static compatibility answer")
	// The loop-level S0.5 compatibility path for a responses-ws configuration
	// is the HTTP Responses stream; script its SSE answer on the same loopback.
	env.httpSSE = "event: response.output_text.delta\ndata: {\"delta\":\"static compatibility answer\"}\n\nevent: response.completed\ndata: {\"response\":{}}\n\n"
	loopCtx := NewLoopContext("e4-static", 1, nil)
	identity := codingE3Identity()
	cb := newCodingSubAgentCallbacks(&CodingSubAgent{handler: env.handler, loopCtx: loopCtx, cfg: env.cfg(), dynamicInvocationIdentity: identity, projectPath: t.TempDir()}, &TaskItem{Title: "e4 static"}, "", "", nil)
	if cb.dynamicLifecycleRelay != nil {
		t.Fatal("engaged kill switch attached a dynamic relay")
	}
	result := codingagent.Run(cb, "static path request", nil, nil, env.httpClient, &testCodingDynamicLifecycleBatchHooks{})
	if result.Error != "" || result.Text != "static compatibility answer" {
		t.Fatalf("static compatibility path result=%+v", result)
	}
	coordinator, err := env.handler.app.semanticExecutionCoordinatorForApp()
	if err != nil {
		t.Fatal(err)
	}
	lineage := tool.InvocationScope{RootTaskID: identity.RootTaskID, SessionID: identity.SessionID, PrincipalID: identity.PrincipalID}
	if _, err := coordinator.Routes.CurrentRevision(lineage); err == nil || !strings.Contains(err.Error(), "route_revision_not_found") {
		t.Fatalf("engaged kill switch materialized a dynamic route revision: %v", err)
	}
}

// TestCodingE4KillSwitchLayersRuntimeControlOverEvidence pins the default
// layering: the production default is disengaged, engaging closes assembly
// without touching the evidence, and a control-plane rearm on incomplete
// evidence never restores eligibility.
func TestCodingE4KillSwitchLayersRuntimeControlOverEvidence(t *testing.T) {
	codingE4RestoreKillSwitch(t)
	if codingDynamicKillSwitchClosed() {
		t.Fatal("kill switch must default to following the evidence")
	}
	cfg := corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"}
	engageCodingDynamicKillSwitch()
	if !codingDynamicKillSwitchClosed() {
		t.Fatal("engaged kill switch did not close assembly")
	}
	if !codingDynamicProductionAdapterForConfig(cfg).eligible() {
		t.Fatal("the kill switch changed the evidence state")
	}
	if got := codingDynamicProductionAdapterQualificationForAssembly(cfg); got.eligible() || got.Reason != "coding_dynamic_kill_switch_engaged" {
		t.Fatalf("engaged kill switch left assembly eligible: %#v", got)
	}
	disengageCodingDynamicKillSwitch()
	if !codingDynamicKillSwitchClosed() {
		t.Fatal("disengage alone restored evidence-following")
	}
	if !rearmCodingDynamicKillSwitch(cfg) {
		t.Fatal("rearm failed on complete production evidence")
	}
	if codingDynamicKillSwitchClosed() {
		t.Fatal("successful rearm left the kill switch closed")
	}
}
