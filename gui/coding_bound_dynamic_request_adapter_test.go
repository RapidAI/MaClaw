package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/gorilla/websocket"
)

func newCodingBoundDynamicRequestAdapterFixture(t *testing.T) (*IMMessageHandler, *trustedCodingInvocationIdentity, codingDynamicPlanPreparation, codingDynamicCatalogSnapshot) {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(app.closeSemanticInvocationStore)
	handler := &IMMessageHandler{app: app}
	identity := &trustedCodingInvocationIdentity{TenantID: "desktop", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn"}
	contract := agentservice.DynamicCapabilityContract{
		Provisions: []tool.CapabilityProvision{{Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Quality: 2}},
		Effects:    []tool.EffectClass{tool.EffectReadOnly},
	}
	catalog, err := agentservice.BuildDynamicSemanticCatalog([]agentservice.MCPToolEntry{{
		ServerID: "trusted-server", ToolName: "search",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, "required": []string{"query"}, "additionalProperties": false}, Contract: contract,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dynamic := codingDynamicCatalogSnapshot{Catalog: catalog, Coverage: tool.CatalogCoverage{State: tool.CatalogCoverageComplete}}
	prepared, err := prepareCodingDynamicSemanticPlan(identity, dynamic, []tool.CapabilityNeed{{ID: "search", Capability: "information.search.web", Qualifiers: map[string]string{"freshness": "current"}, Required: true}}, nil, nil, tool.PlanningBudget{}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	return handler, identity, prepared, dynamic
}

func codingBoundAdapterExecution(responseID string) agent.ToolCallExecutionContext {
	return agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "connection-a", SurfaceEpoch: "channel-epoch-a", ResponseID: responseID}
}

func TestCodingBoundDynamicRequestAdapterPublishesBindsAndRejectsMismatchedCalls(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	execution := codingBoundAdapterExecution("")
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) != 1 || adapter.surface == nil || adapter.surface.epoch != execution.SurfaceEpoch {
		t.Fatalf("adapter did not publish exactly one reservation-bound surface: definitions=%#v surface=%#v", definitions, adapter.surface)
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
	}
	if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "call-a", codingBoundAdapterExecution("response-a")); !strings.Contains(got.Result, "stale_surface") {
		t.Fatalf("unbound surface dispatched: %#v", got)
	}
	if err := adapter.BindToolSurfaceResponse(codingBoundAdapterExecution("response-a")); err != nil {
		t.Fatalf("bind response: %v", err)
	}
	for label, bad := range map[string]agent.ToolCallExecutionContext{
		"wrong protocol":    {Protocol: "other", ConnectionID: "connection-a", SurfaceEpoch: "channel-epoch-a", ResponseID: "response-a"},
		"wrong connection":  {Protocol: "test-provider/v1", ConnectionID: "connection-b", SurfaceEpoch: "channel-epoch-a", ResponseID: "response-a"},
		"wrong epoch":       {Protocol: "test-provider/v1", ConnectionID: "connection-a", SurfaceEpoch: "channel-epoch-b", ResponseID: "response-a"},
		"wrong response":    {Protocol: "test-provider/v1", ConnectionID: "connection-a", SurfaceEpoch: "channel-epoch-a", ResponseID: "response-b"},
		"missing tool call": {Protocol: "test-provider/v1", ConnectionID: "connection-a", SurfaceEpoch: "channel-epoch-a", ResponseID: "response-a"},
	} {
		callID := "call-a"
		if label == "missing tool call" {
			callID = ""
		}
		if got := adapter.ExecuteToolCallWithContext(alias, `{}`, callID, bad); !strings.Contains(got.Result, "stale_surface") {
			t.Fatalf("%s reached fixed bridge: %#v", label, got)
		}
	}
	// Invalid arguments reach the durable bridge but never observe the live
	// catalog/provider. Replaying the same call must return the journal result.
	first := adapter.ExecuteToolCallWithContext(alias, `{}`, "call-invalid", codingBoundAdapterExecution("response-a"))
	if !strings.Contains(first.Result, "parameter_required_field_missing: query") {
		t.Fatalf("invalid arguments did not reach durable rejection with the missing field localized: %#v", first)
	}
	replay := adapter.ExecuteToolCallWithContext(alias, `{}`, "call-invalid", codingBoundAdapterExecution("response-a"))
	if replay.Result != first.Result {
		t.Fatalf("invalid call was not replayed from journal: first=%#v replay=%#v", first, replay)
	}
}

func TestCodingBoundDynamicRequestAdapterRetiresOnBindFailureAndClose(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	execution := codingBoundAdapterExecution("")
	if got := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(got) == 0 {
		t.Fatal("adapter failed to publish fixture surface")
	}
	if err := adapter.BindToolSurfaceResponse(codingBoundAdapterExecution("")); err == nil {
		t.Fatal("empty provider response ID bound a dynamic surface")
	}
	if !adapter.terminal {
		t.Fatal("bind failure left holder non-terminal")
	}
	if got := adapter.BuildToolsForBoundModelRequest("", 1, codingBoundAdapterExecution("")); len(got) != 0 {
		t.Fatalf("terminal holder rendered successor definitions: %#v", got)
	}

	// A closed holder is terminal. Successor publication is separately covered
	// by the request-surface replace tests; it must never reuse this holder.
	second, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	second.Close(context.Canceled)
	if got := second.ExecuteToolCallWithContext("unknown", `{}`, "call-a", codingBoundAdapterExecution("response-b")); !strings.Contains(got.Result, "stale_surface") {
		t.Fatalf("closed holder accepted call: %#v", got)
	}
}

func TestCodingBoundDynamicRequestAdapterUsesOneLiveWSReservationAndRetiresPredecessor(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("read response.create: %v", err)
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-bound-ws","status":"completed"}}`))
	}))
	defer srv.Close()
	cfg := corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses-ws"}
	channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), handler, cfg, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		channel.Close(err)
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "ws-channel-epoch-a"
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) != 1 {
		t.Fatalf("reservation did not publish dynamic definitions: %#v", definitions)
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: adapter.ToolSurfaceAuditEvidence(execution), InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err != nil {
		t.Fatalf("set dispatch preparation: %v", err)
	}
	response, err := adapter.Do(context.Background(), []interface{}{map[string]interface{}{"role": "user", "content": "test"}}, definitions, nil, true)
	if err != nil || response == nil || response.ResponseID != "resp-bound-ws" {
		t.Fatalf("bound channel response=%#v err=%v", response, err)
	}
	execution.ResponseID = response.ResponseID
	if err := adapter.BindToolSurfaceResponse(execution); err != nil {
		t.Fatalf("bind live WS response: %v", err)
	}
	if _, err := adapter.Do(context.Background(), nil, definitions, nil, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("holder allowed a successor exchange on predecessor channel: %v", err)
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
	}
	adapter.Close(context.Canceled)
	if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "call-late", execution); !strings.Contains(got.Result, "stale_surface") {
		t.Fatalf("terminal WS holder accepted predecessor alias: %#v", got)
	}
	if _, _, err := adapter.surface.ResolveAlias(response.ResponseID, alias); err == nil {
		t.Fatal("cancelled live WS surface still resolved predecessor alias")
	}
}

func TestCodingBoundDynamicRequestAdapterDirectDispatchRequiresFrozenInvocationPolicy(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	calls := 0
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "policy-required-connection"},
		do: func(context.Context, []interface{}, []map[string]interface{}, llm.TokenCallback, bool) (*llm.Response, error) {
			calls++
			return &llm.Response{}, nil
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "policy-required-epoch"
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) == 0 {
		t.Fatal("fixture did not publish a dynamic request surface")
	}
	if _, err := adapter.Do(context.Background(), nil, definitions, nil, true); err == nil || !strings.Contains(err.Error(), "invocation policy was not set") {
		t.Fatalf("direct dispatch without policy accepted: %v", err)
	}
	if calls != 0 {
		t.Fatalf("direct dispatch wrote before policy handoff: %d", calls)
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: adapter.ToolSurfaceAuditEvidence(execution), InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat)}); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("terminal reservation accepted late dispatch preparation: %v", err)
	}
}

func TestCodingBoundDynamicRequestAdapterVerifiesResponseWSFrameAsCompleteReplacement(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	execution := codingBoundAdapterExecution("")
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) != 1 {
		t.Fatalf("definitions=%#v", definitions)
	}
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	frame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "find"}}, definitions)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifyResponsesWSFrameToolSurface(frame, definitions, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses))
	if err != nil || !receipt.Verified || receipt.PayloadDigest != receipt.WirePayloadHash || receipt.ExpectedToolCount != 1 || receipt.WireToolCount != 1 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}

	var mutated map[string]interface{}
	if err := json.Unmarshal(frame, &mutated); err != nil {
		t.Fatal(err)
	}
	mutated["tools"] = []interface{}{}
	corrupt, err := json.Marshal(mutated)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = verifyResponsesWSFrameToolSurface(corrupt, definitions, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses))
	if err == nil || receipt.Verified || !strings.Contains(receipt.Failure, "surface_integrity_failure") {
		t.Fatalf("corrupt response WS frame accepted: receipt=%+v err=%v", receipt, err)
	}
}

func TestCodingBoundDynamicRequestAdapterMalformedWSFrameRetainsAuditProjection(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, codingBoundAdapterExecution(""))
	evidence := agent.ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "ws-malformed-frame-plan",
		PlanSnapshotDigest: "ws-malformed-frame-snapshot",
		CatalogGeneration:  13,
		Omitted:            []agent.ToolSurfaceOmission{{NeedID: "network", ReasonCode: "policy_denied"}},
	}
	// The valid empty payload call supplies the canonical manifest projection
	// expected for this rendered surface; malformed-frame verification must not
	// lose it merely because JSON parsing fails before the tools field is read.
	manifestReceipt, err := agent.VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses), evidence)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifyResponsesWSFrameToolSurface([]byte(`{`), definitions, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses), evidence)
	if err == nil || receipt.Verified || !strings.Contains(receipt.Failure, "surface_integrity_failure") {
		t.Fatalf("malformed frame receipt=%+v err=%v", receipt, err)
	}
	if receipt.PayloadDigest != manifestReceipt.PayloadDigest || receipt.AuditDigest != manifestReceipt.AuditDigest || receipt.ExpectedToolCount != manifestReceipt.ExpectedToolCount || receipt.ReplacementMode != manifestReceipt.ReplacementMode {
		t.Fatalf("malformed frame lost manifest projection: receipt=%+v manifest=%+v", receipt, manifestReceipt)
	}
}

func TestCodingBoundDynamicRequestAdapterRejectsWSInvocationPolicyMutation(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, codingBoundAdapterExecution(""))
	if len(definitions) != 1 {
		t.Fatalf("definitions=%#v", definitions)
	}
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	frame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "find"}}, definitions)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(frame, &payload); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]interface{}){
		"provider default to auto": func(frame map[string]interface{}) { frame["tool_choice"] = "auto" },
		"parallel absent to false": func(frame map[string]interface{}) { frame["parallel_tool_calls"] = false },
	} {
		t.Run(name, func(t *testing.T) {
			copyPayload := make(map[string]interface{}, len(payload)+1)
			for key, value := range payload {
				copyPayload[key] = value
			}
			mutate(copyPayload)
			corrupt, err := json.Marshal(copyPayload)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := verifyResponsesWSFrameToolSurface(corrupt, definitions, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses))
			if err == nil || receipt.Verified || !strings.Contains(receipt.Failure, "surface_integrity_failure") {
				t.Fatalf("policy-mutated frame accepted: receipt=%+v err=%v", receipt, err)
			}
		})
	}
}

func TestCodingBoundDynamicRequestAdapterCloseCancelsBridgeContextBeforeRetiringSurface(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	execution := codingBoundAdapterExecution("")
	if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("adapter failed to publish fixture surface")
	}
	var alias string
	for name := range adapter.surface.aliases {
		alias = name
	}
	execution.ResponseID = "response-cancel"
	if err := adapter.BindToolSurfaceResponse(execution); err != nil {
		t.Fatal(err)
	}
	adapter.Close(context.Canceled)
	if adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
		t.Fatal("close did not cancel the fixed bridge context")
	}
	if got := adapter.ExecuteToolCallWithContext(alias, `{}`, "late-call", execution); !strings.Contains(got.Result, "stale_surface") {
		t.Fatalf("cancelled bridge accepted a call: %#v", got)
	}
	if _, _, err := adapter.surface.ResolveAlias(execution.ResponseID, alias); err == nil {
		t.Fatal("close left durable alias resolvable")
	}
}

func TestCodingBoundDynamicRequestAdapterLifecycleTerminalReasonsAllRetireDurableSurface(t *testing.T) {
	for _, reason := range []codingBoundDynamicRequestTerminalReason{
		codingBoundDynamicRequestSteered,
		codingBoundDynamicRequestNestedExit,
		codingBoundDynamicRequestRuntimeClosed,
		"unrecognized",
	} {
		t.Run(string(reason), func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
			if err != nil {
				t.Fatal(err)
			}
			execution := codingBoundAdapterExecution("")
			if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
				t.Fatal("adapter failed to publish fixture surface")
			}
			var alias string
			for name := range adapter.surface.aliases {
				alias = name
			}
			execution.ResponseID = "response-" + string(reason)
			if err := adapter.BindToolSurfaceResponse(execution); err != nil {
				t.Fatal(err)
			}
			adapter.CloseForLifecycle(reason)
			if adapter.executionCtx == nil || adapter.executionCtx.Err() == nil || !adapter.terminal {
				t.Fatalf("lifecycle reason %q did not close holder", reason)
			}
			if _, _, err := adapter.surface.ResolveAlias(execution.ResponseID, alias); err == nil {
				t.Fatalf("lifecycle reason %q left alias resolvable", reason)
			}
		})
	}
}

func TestCodingBoundDynamicRequestAdapterSettledLifecycleFinishesRequestWithoutCancellingRoute(t *testing.T) {
	for _, reason := range []codingBoundDynamicRequestTerminalReason{
		codingBoundDynamicRequestResponseSettled,
		codingBoundDynamicRequestToolBatchSettled,
	} {
		t.Run(string(reason), func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
			if err != nil {
				t.Fatal(err)
			}
			execution := codingBoundAdapterExecution("")
			if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
				t.Fatal("adapter failed to publish fixture surface")
			}
			var alias string
			for name := range adapter.surface.aliases {
				alias = name
			}
			execution.ResponseID = "response-" + string(reason)
			if err := adapter.BindToolSurfaceResponse(execution); err != nil {
				t.Fatal(err)
			}
			coordinator := adapter.surface.coordinator
			if coordinator == nil {
				t.Fatal("fixture surface has no coordinator")
			}

			adapter.CloseForLifecycle(reason)
			if !adapter.terminal || adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
				t.Fatalf("settled lifecycle did not terminally close holder: %#v", adapter)
			}
			if _, _, err := adapter.surface.ResolveAlias(execution.ResponseID, alias); err == nil {
				t.Fatal("finished request left predecessor alias resolvable")
			}
			if err := coordinator.Routes.IsCurrent(adapter.surface.scope); err != nil {
				t.Fatalf("settled request cancelled its current route: %v", err)
			}
		})
	}
}

func TestCodingBoundDynamicRequestAdapterExposesDurableTerminalFailure(t *testing.T) {
	for _, reason := range []codingBoundDynamicRequestTerminalReason{
		codingBoundDynamicRequestResponseSettled,
		codingBoundDynamicRequestRuntimeClosed,
	} {
		t.Run(string(reason), func(t *testing.T) {
			handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
			var closeCause error
			channel := &testCodingBoundDynamicRequestChannel{
				execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "terminal-failure-connection"},
				close:     func(cause error) { closeCause = cause },
			}
			adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
			if err != nil {
				t.Fatal(err)
			}
			execution := adapter.ExecutionContext()
			execution.SurfaceEpoch = "terminal-failure-epoch"
			if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
				t.Fatal("adapter failed to publish fixture surface")
			}
			if reason == codingBoundDynamicRequestResponseSettled {
				execution.ResponseID = "terminal-failure-response"
				if err := adapter.BindToolSurfaceResponse(execution); err != nil {
					t.Fatal(err)
				}
			}
			// Closing the coordinator makes both the requested durable terminal
			// write and the settled-path cancellation fallback fail. The holder
			// must still fence local execution and surface that failure to its
			// lifecycle owner/channel instead of reporting a clean terminal.
			if err := adapter.surface.coordinator.Close(); err != nil {
				t.Fatal(err)
			}
			if err := adapter.CloseForLifecycle(reason); err == nil {
				t.Fatal("durable terminal failure was silently accepted")
			}
			if !adapter.terminal || adapter.terminalDurabilityErr == nil || adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
				t.Fatalf("failed durable terminal left holder usable: %#v", adapter)
			}
			if closeCause == nil {
				t.Fatal("channel close hid durable terminal failure")
			}
			if got := adapter.ExecuteToolCallWithContext("late", `{}`, "late-call", execution); got.Result != "[system rejected] stale_surface" {
				t.Fatalf("durable terminal failure allowed dispatch: %#v", got)
			}
		})
	}
}

func TestCodingBoundDynamicRequestAdapterPublicationFailureLatchesSuccessorAdmission(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	adapter, err := newCodingBoundDynamicRequestAdapter(handler, identity, prepared, dynamic)
	if err != nil {
		t.Fatal(err)
	}
	execution := codingBoundAdapterExecution("")
	coordinator, err := handler.app.semanticExecutionCoordinatorForApp()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the app's cached coordinator pointer, but close the underlying DB so
	// publication cannot silently create a replacement coordinator. The adapter
	// must not report this as an ordinary stale holder: a relay needs the
	// failure latch to prevent a successor from being published over an
	// uncertain route outcome should a future publisher fail after its first
	// durable write.
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	rendered := adapter.RenderPublishedBoundToolSurface("", 0, execution)
	if rendered.Published || rendered.Failure == "" {
		t.Fatalf("unavailable coordinator published surface: %#v", rendered)
	}
	if !adapter.terminal || adapter.terminalDurabilityErr == nil {
		t.Fatalf("publication failure was not latched: %#v", adapter)
	}
	if got := adapter.ExecuteToolCallWithContext("late", `{}`, "late-call", execution); got.Result != "[system rejected] stale_surface" {
		t.Fatalf("publication failure allowed dispatch: %#v", got)
	}
}

func TestCodingBoundDynamicRequestAdapterLifecycleCloseCancelsInFlightDispatch(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	dispatchStarted := make(chan struct{})
	dispatchResult := make(chan error, 1)
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "inflight-dispatch-connection"},
		do: func(ctx context.Context, _ []interface{}, _ []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
			close(dispatchStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "inflight-dispatch-epoch"
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) == 0 {
		t.Fatal("fixture did not publish dynamic request surface")
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{
		AuditEvidence:    adapter.ToolSurfaceAuditEvidence(execution),
		InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat),
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := adapter.DoVerified(context.Background(), nil, definitions, nil, true)
		dispatchResult <- err
	}()
	select {
	case <-dispatchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not start")
	}
	if err := adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed); err != nil {
		t.Fatalf("close lifecycle: %v", err)
	}
	select {
	case err := <-dispatchResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dispatch error=%v, want lifecycle cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight dispatch was not cancelled by lifecycle close")
	}
}

func TestCodingBoundDynamicRequestAdapterSuccessfulTransportCloseDoesNotPreemptLifecycleTerminal(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	closeCalls := 0
	var closeCause error
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "success-close-connection"},
		close: func(cause error) {
			closeCalls++
			closeCause = cause
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "success-close-epoch"
	if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("fixture did not publish dynamic request surface")
	}
	// The channel's successful request cleanup cannot close this socket: only a
	// semantic disposition may choose whether this request is finished or the
	// entire route must be cancelled.
	adapter.Close(nil)
	if closeCalls != 0 || adapter.terminal {
		t.Fatalf("successful transport close preempted lifecycle: calls=%d terminal=%v cause=%v", closeCalls, adapter.terminal, closeCause)
	}
	if err := adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed); err != nil {
		t.Fatal(err)
	}
	if closeCalls != 1 || closeCause == nil {
		t.Fatalf("lifecycle terminal did not own channel close: calls=%d cause=%v", closeCalls, closeCause)
	}
}

func TestCodingBoundDynamicRequestAdapterTerminalClosesUnderlyingChannelOnce(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	closeCalls := 0
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "single-terminal-close-connection"},
		close: func(error) {
			closeCalls++
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "single-terminal-close-epoch"
	if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) == 0 {
		t.Fatal("fixture did not publish dynamic request surface")
	}

	// An error path may first retire from DoVerified/Close and later receive the
	// loop's generic cleanup and final lifecycle disposition. They all observe
	// the same semantic terminal, but only the first may release the transport.
	adapter.Close(fmt.Errorf("dispatch failed"))
	adapter.Close(fmt.Errorf("RunLoop cleanup"))
	if err := adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed); err != nil {
		t.Fatalf("idempotent lifecycle close: %v", err)
	}
	if closeCalls != 1 {
		t.Fatalf("underlying channel close calls=%d, want exactly one", closeCalls)
	}
}

func TestCodingBoundDynamicRequestAdapterCloseCancelsInFlightDispatch(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	dispatchStarted := make(chan struct{})
	dispatchResult := make(chan error, 1)
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "close-inflight-dispatch-connection"},
		do: func(ctx context.Context, _ []interface{}, _ []map[string]interface{}, _ llm.TokenCallback, _ bool) (*llm.Response, error) {
			close(dispatchStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "close-inflight-dispatch-epoch"
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) == 0 {
		t.Fatal("fixture did not publish dynamic request surface")
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{
		AuditEvidence:    adapter.ToolSurfaceAuditEvidence(execution),
		InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat),
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, err := adapter.DoVerified(context.Background(), nil, definitions, nil, true)
		dispatchResult <- err
	}()
	select {
	case <-dispatchStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("dispatch did not start")
	}
	adapter.Close(fmt.Errorf("transport teardown"))
	select {
	case err := <-dispatchResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dispatch error=%v, want close cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight dispatch was not cancelled by close")
	}
}

func TestCodingBoundDynamicRequestAdapterLifecycleTerminalAfterPreparationDoesNotStartTransport(t *testing.T) {
	handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
	dispatches := 0
	channel := &testCodingBoundDynamicRequestChannel{
		execution: agent.ToolCallExecutionContext{Protocol: "test-provider/v1", ConnectionID: "terminal-after-preparation-connection"},
		do: func(context.Context, []interface{}, []map[string]interface{}, llm.TokenCallback, bool) (*llm.Response, error) {
			dispatches++
			return nil, fmt.Errorf("transport should not start after lifecycle terminal")
		},
	}
	adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
	if err != nil {
		t.Fatal(err)
	}
	execution := adapter.ExecutionContext()
	execution.SurfaceEpoch = "terminal-after-preparation-epoch"
	definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
	if len(definitions) == 0 {
		t.Fatal("fixture did not publish dynamic request surface")
	}
	if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{
		AuditEvidence:    adapter.ToolSurfaceAuditEvidence(execution),
		InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeOpenAIChat),
	}); err != nil {
		t.Fatal(err)
	}
	// The lower channel has accepted the frozen frame and lifecycle cancellation
	// begins before DoVerified can commit the actual transport handoff. A real
	// CloseForLifecycle performs this cancellation before it acquires the holder
	// mutex for durable retirement; keep the terminal write out of this test so
	// it exercises the post-preparation handoff fence itself.
	if adapter.cancelExecution != nil {
		adapter.cancelExecution()
	}
	if _, err := adapter.DoVerified(context.Background(), nil, definitions, nil, true); err == nil || !strings.Contains(err.Error(), "stale_surface") {
		t.Fatalf("DoVerified after lifecycle terminal error=%v, want stale_surface", err)
	}
	if dispatches != 0 {
		t.Fatalf("lifecycle-terminal request started transport %d times", dispatches)
	}
	if adapter.executionCtx == nil || adapter.executionCtx.Err() == nil {
		t.Fatalf("lifecycle cancellation did not fence holder: %#v", adapter)
	}
	if err := adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed); err != nil {
		t.Fatalf("close lifecycle after cancelled handoff: %v", err)
	}
}
