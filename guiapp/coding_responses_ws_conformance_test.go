package guiapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingResponsesWSConformanceVersion is the loopback conformance suite
// version for the repository's own Responses WebSocket single-use channel
// adapter. It must change exactly when codingResponsesWSAdapterVersion does;
// the equality test below is the E1 machine evidence for the qualification
// AdapterVersion field.
const codingResponsesWSConformanceVersion = "responses-ws-adapter-v1"

func TestCodingResponsesWSAdapterVersionMatchesConformanceSuite(t *testing.T) {
	if codingResponsesWSAdapterVersion != codingResponsesWSConformanceVersion {
		t.Fatalf("adapter implementation version %q does not match conformance suite version %q", codingResponsesWSAdapterVersion, codingResponsesWSConformanceVersion)
	}
}

// codingResponsesWSConformanceCoverage maps every capability Has* field of the
// qualified responses-ws adapter row to the conformance tests that prove it.
// Function references make a missing or renamed test a compile error; the
// meta-test below makes an unmapped capability field a test failure.
var codingResponsesWSConformanceCoverage = map[string][]func(*testing.T){
	"HasTransportConnectionID":   {TestResponsesWSConformanceConnectionIdentityIsHostOwnedAndUniquePerDial},
	"HasProviderResponseID":      {TestResponsesWSConformanceProviderResponseIDReachesBindPath},
	"HasProviderToolCallID":      {TestResponsesWSConformanceProviderToolCallIDExtractedFromItemEvents},
	"HasCancellationFence":       {TestResponsesWSConformanceCancelledSocketNeverBindsLateFrames},
	"HasReplayIdentitySemantics": {TestResponsesWSConformanceSingleSocketSingleFrameNoReplay},
}

func TestCodingDynamicProviderCorrelationCapabilityFieldsHaveConformanceCoverage(t *testing.T) {
	capabilityType := reflect.TypeOf(codingDynamicProviderCorrelationCapability{})
	hasFields := 0
	for i := 0; i < capabilityType.NumField(); i++ {
		field := capabilityType.Field(i)
		if !strings.HasPrefix(field.Name, "Has") || field.Type.Kind() != reflect.Bool {
			continue
		}
		hasFields++
		if len(codingResponsesWSConformanceCoverage[field.Name]) == 0 {
			t.Errorf("capability field %s has no loopback conformance coverage", field.Name)
		}
	}
	if hasFields != 5 {
		t.Fatalf("capability field count changed without updating the conformance coverage map: %d", hasFields)
	}
	// The conformance-proven row is the only eligible row, and the row's
	// eligibility still does not wire the Coding callback composition.
	qualified := codingDynamicProviderCorrelationForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if !qualified.eligible() || qualified.AdapterKey != "responses-ws-single-use-channel" {
		t.Fatalf("responses-ws row is not the qualified adapter row: %#v", qualified)
	}
	for _, cfg := range []corelib.MaclawLLMConfig{{Protocol: "openai", WireAPI: "chat"}, {Protocol: "anthropic"}, {Protocol: "openai", WireAPI: "responses"}} {
		if other := codingDynamicProviderCorrelationForConfig(cfg); other.eligible() {
			t.Fatalf("unqualified adapter row became eligible: %#v", other)
		}
	}
}

// newResponsesWSConformanceProvider starts a loopback fake Responses WebSocket
// provider. serve runs on the upgraded connection.
func newResponsesWSConformanceProvider(t *testing.T, serve func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		serve(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func responsesWSConformanceConfig(srv *httptest.Server) corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{URL: srv.URL, Key: "test-key", Model: "test-model", Protocol: "openai", WireAPI: "responses-ws"}
}

func responsesWSConformanceReadCreate(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Errorf("read response.create: %v", err)
	}
}

func responsesWSConformanceWrite(t *testing.T, conn *websocket.Conn, frames ...string) {
	t.Helper()
	for _, frame := range frames {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Errorf("write frame: %v", err)
			return
		}
	}
}

// reserveResponsesWSConformanceChannel reserves one live socket channel with
// its atomic dispatch preparation already frozen, ready for a single dispatch.
func reserveResponsesWSConformanceChannel(t *testing.T, srv *httptest.Server) agent.ToolSurfaceRequestChannel {
	t.Helper()
	channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), &IMMessageHandler{}, responsesWSConformanceConfig(srv), srv.Client())
	if err != nil {
		t.Fatalf("reserve conformance channel: %v", err)
	}
	t.Cleanup(func() { channel.Close(nil) })
	if err := channel.(agent.ToolSurfaceDispatchPreparationRequestChannel).SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: agent.ToolSurfacePlanEvidence{}, InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err != nil {
		t.Fatalf("set dispatch preparation: %v", err)
	}
	return channel
}

func responsesWSConformanceDoVerified(t *testing.T, channel agent.ToolSurfaceRequestChannel, ctx context.Context) (agent.VerifiedToolSurfaceDispatch, error) {
	t.Helper()
	return channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(ctx, []interface{}{map[string]interface{}{"role": "user", "content": "conformance"}}, nil, nil, true)
}

// TestResponsesWSConformanceConnectionIdentityIsHostOwnedAndUniquePerDial
// proves HasTransportConnectionID: every dial produces a distinct host-owned
// opaque connection ID that carries no URL, model, key, or provider label.
func TestResponsesWSConformanceConnectionIdentityIsHostOwnedAndUniquePerDial(t *testing.T) {
	srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-identity","status":"completed"}}`))
	})
	cfg := responsesWSConformanceConfig(srv)
	first, err := reserveCodingResponsesWSRequestChannel(context.Background(), &IMMessageHandler{}, cfg, srv.Client())
	if err != nil {
		t.Fatalf("reserve first channel: %v", err)
	}
	defer first.Close(nil)
	second, err := reserveCodingResponsesWSRequestChannel(context.Background(), &IMMessageHandler{}, cfg, srv.Client())
	if err != nil {
		t.Fatalf("reserve second channel: %v", err)
	}
	defer second.Close(nil)
	a, b := first.ExecutionContext(), second.ExecutionContext()
	if a.Protocol != "openai-responses-ws" || b.Protocol != "openai-responses-ws" {
		t.Fatalf("live protocol tuple mismatch: %+v %+v", a, b)
	}
	if !strings.HasPrefix(a.ConnectionID, "responses-ws:") || !strings.HasPrefix(b.ConnectionID, "responses-ws:") {
		t.Fatalf("connection IDs are not host-owned opaque values: %q %q", a.ConnectionID, b.ConnectionID)
	}
	if a.ConnectionID == b.ConnectionID {
		t.Fatalf("two live sockets share a connection identity: %q", a.ConnectionID)
	}
	for _, forbidden := range []string{srv.URL, cfg.Model, cfg.Key, cfg.ProviderID, cfg.ProviderName} {
		if forbidden != "" && (strings.Contains(a.ConnectionID, forbidden) || strings.Contains(b.ConnectionID, forbidden)) {
			t.Fatalf("connection ID derived from a configuration value %q: %q %q", forbidden, a.ConnectionID, b.ConnectionID)
		}
	}
}

// TestResponsesWSConformanceProviderResponseIDReachesBindPath proves
// HasProviderResponseID: the provider response.id from the lifecycle envelope
// lands in llm.Response.ResponseID and is the value the durable holder binds;
// conflicting IDs fail the stream, and a missing ID can never bind.
func TestResponsesWSConformanceProviderResponseIDReachesBindPath(t *testing.T) {
	t.Run("provider response id binds through the live holder", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			responsesWSConformanceReadCreate(t, conn)
			responsesWSConformanceWrite(t, conn,
				`{"type":"response.created","response":{"id":"resp-conformance-bind","status":"in_progress"}}`,
				`{"type":"response.output_text.delta","delta":"bound"}`,
				`{"type":"response.completed","response":{"id":"resp-conformance-bind","status":"completed"}}`,
			)
		})
		handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
		channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), handler, responsesWSConformanceConfig(srv), srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
		if err != nil {
			channel.Close(err)
			t.Fatal(err)
		}
		execution := adapter.ExecutionContext()
		execution.SurfaceEpoch = "conformance-bind-epoch"
		definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
		if len(definitions) != 1 {
			t.Fatalf("publish definitions=%#v", definitions)
		}
		if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: adapter.ToolSurfaceAuditEvidence(execution), InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err != nil {
			t.Fatal(err)
		}
		dispatch, err := adapter.DoVerified(context.Background(), []interface{}{map[string]interface{}{"role": "user", "content": "bind"}}, definitions, nil, true)
		if err != nil || dispatch.Response == nil || dispatch.Response.ResponseID != "resp-conformance-bind" {
			t.Fatalf("dispatch=%+v err=%v", dispatch, err)
		}
		execution.ResponseID = dispatch.Response.ResponseID
		if err := adapter.BindToolSurfaceResponse(execution); err != nil {
			t.Fatalf("provider response id did not reach the binder: %v", err)
		}
		alias := testCodingBoundAdapterAlias(adapter)
		if _, _, err := adapter.surface.ResolveAlias(execution.ResponseID, alias); err != nil {
			t.Fatalf("bound alias did not resolve: %v", err)
		}
		adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
	})

	t.Run("conflicting stream response ids fail closed", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			responsesWSConformanceReadCreate(t, conn)
			responsesWSConformanceWrite(t, conn,
				`{"type":"response.created","response":{"id":"resp-conformance-a","status":"in_progress"}}`,
				`{"type":"response.completed","response":{"id":"resp-conformance-b","status":"completed"}}`,
			)
		})
		channel := reserveResponsesWSConformanceChannel(t, srv)
		if _, err := responsesWSConformanceDoVerified(t, channel, context.Background()); err == nil || !strings.Contains(err.Error(), "response ID changed") {
			t.Fatalf("conflicting response IDs were accepted: %v", err)
		}
	})

	t.Run("missing provider response id cannot bind", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			responsesWSConformanceReadCreate(t, conn)
			responsesWSConformanceWrite(t, conn, `{"type":"response.completed","response":{"status":"completed"}}`)
		})
		handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
		channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), handler, responsesWSConformanceConfig(srv), srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
		if err != nil {
			channel.Close(err)
			t.Fatal(err)
		}
		execution := adapter.ExecutionContext()
		execution.SurfaceEpoch = "conformance-missing-id-epoch"
		if definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution); len(definitions) != 1 {
			t.Fatalf("publish definitions=%#v", definitions)
		}
		if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: adapter.ToolSurfaceAuditEvidence(execution), InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err != nil {
			t.Fatal(err)
		}
		dispatch, err := adapter.DoVerified(context.Background(), []interface{}{map[string]interface{}{"role": "user", "content": "missing id"}}, nil, nil, true)
		if err != nil || dispatch.Response == nil || dispatch.Response.ResponseID != "" {
			t.Fatalf("dispatch=%+v err=%v", dispatch, err)
		}
		execution.ResponseID = dispatch.Response.ResponseID
		if err := adapter.BindToolSurfaceResponse(execution); err == nil {
			t.Fatal("a response without a provider response id bound the durable holder")
		}
		if !adapter.terminal {
			t.Fatal("bind failure left the holder non-terminal")
		}
		testCodingBoundAdapterAliasStaleProbe(t, adapter, execution)
	})
}

// TestResponsesWSConformanceProviderToolCallIDExtractedFromItemEvents proves
// HasProviderToolCallID: every function_call reaching the response owns the
// provider-issued call ID, whether the provider delivers the complete item at
// output_item.added or only at output_item.done. A call without a provider
// call ID never enters managed dispatch.
func TestResponsesWSConformanceProviderToolCallIDExtractedFromItemEvents(t *testing.T) {
	for _, tc := range []struct {
		name     string
		frames   []string
		wantID   string
		wantName string
		wantArgs string
	}{
		{
			name: "call id delivered at item added",
			frames: []string{
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call-at-added","name":"search"}}`,
				`{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"query\":"}`,
				`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"query\":\"added\"}"}`,
				`{"type":"response.completed","response":{"id":"resp-toolcall-added","status":"completed"}}`,
			},
			wantID: "call-at-added", wantName: "search", wantArgs: `{"query":"added"}`,
		},
		{
			name: "call id delivered only at item done",
			frames: []string{
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call"}}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call-at-done","name":"search","arguments":"{\"query\":\"done\"}"}}`,
				`{"type":"response.completed","response":{"id":"resp-toolcall-done","status":"completed"}}`,
			},
			wantID: "call-at-done", wantName: "search", wantArgs: `{"query":"done"}`,
		},
		{
			name: "arguments delivered only at item done",
			frames: []string{
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call-args-done","name":"search"}}`,
				`{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call-args-done","name":"search","arguments":"{\"query\":\"argsdone\"}"}}`,
				`{"type":"response.completed","response":{"id":"resp-toolcall-argsdone","status":"completed"}}`,
			},
			wantID: "call-args-done", wantName: "search", wantArgs: `{"query":"argsdone"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
				responsesWSConformanceReadCreate(t, conn)
				responsesWSConformanceWrite(t, conn, tc.frames...)
			})
			channel := reserveResponsesWSConformanceChannel(t, srv)
			dispatch, err := responsesWSConformanceDoVerified(t, channel, context.Background())
			if err != nil || dispatch.Response == nil || len(dispatch.Response.Choices) != 1 {
				t.Fatalf("dispatch=%+v err=%v", dispatch, err)
			}
			calls := dispatch.Response.Choices[0].Message.ToolCalls
			if len(calls) != 1 || calls[0].ID != tc.wantID || calls[0].Function.Name != tc.wantName || calls[0].Function.Arguments != tc.wantArgs {
				t.Fatalf("tool calls=%+v, want id=%q name=%q args=%q", calls, tc.wantID, tc.wantName, tc.wantArgs)
			}
		})
	}

	t.Run("missing provider call id never enters managed dispatch", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			responsesWSConformanceReadCreate(t, conn)
			responsesWSConformanceWrite(t, conn,
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","name":"search"}}`,
				`{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"query\":\"noid\"}"}`,
				`{"type":"response.completed","response":{"id":"resp-toolcall-noid","status":"completed"}}`,
			)
		})
		handler, identity, prepared, dynamic := newCodingBoundDynamicRequestAdapterFixture(t)
		channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), handler, responsesWSConformanceConfig(srv), srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		adapter, err := newCodingBoundDynamicRequestAdapterForChannel(handler, identity, prepared, dynamic, channel)
		if err != nil {
			channel.Close(err)
			t.Fatal(err)
		}
		execution := adapter.ExecutionContext()
		execution.SurfaceEpoch = "conformance-noid-epoch"
		definitions := adapter.BuildToolsForBoundModelRequest("", 0, execution)
		if len(definitions) != 1 {
			t.Fatalf("publish definitions=%#v", definitions)
		}
		if err := adapter.SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: adapter.ToolSurfaceAuditEvidence(execution), InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err != nil {
			t.Fatal(err)
		}
		dispatch, err := adapter.DoVerified(context.Background(), []interface{}{map[string]interface{}{"role": "user", "content": "no id"}}, definitions, nil, true)
		if err != nil || dispatch.Response == nil {
			t.Fatalf("dispatch=%+v err=%v", dispatch, err)
		}
		calls := dispatch.Response.Choices[0].Message.ToolCalls
		if len(calls) != 1 || calls[0].ID != "" {
			t.Fatalf("expected exactly one call with an empty provider call id: %+v", calls)
		}
		execution.ResponseID = dispatch.Response.ResponseID
		if err := adapter.BindToolSurfaceResponse(execution); err != nil {
			t.Fatal(err)
		}
		alias := testCodingBoundAdapterAlias(adapter)
		// The model-visible alias itself is bound, but the empty provider call
		// ID must fail closed before any admission or provider I/O.
		if got := adapter.ExecuteToolCallWithContext(alias, `{"query":"noid"}`, "", execution); got.Outcome != agent.ToolExecutionOutcomeError || got.Result != "[system rejected] stale_surface" {
			t.Fatalf("call without a provider call id reached managed dispatch: %#v", got)
		}
		adapter.CloseForLifecycle(codingBoundDynamicRequestRuntimeClosed)
	})
}

// TestResponsesWSConformanceCancelledSocketNeverBindsLateFrames proves
// HasCancellationFence: once the request context is cancelled the socket is
// closed, the dispatch returns the context error (so no partial stream can be
// bound as this reservation's response), frames arriving afterwards never
// enter the response, and the one-shot channel admits no successor dispatch.
func TestResponsesWSConformanceCancelledSocketNeverBindsLateFrames(t *testing.T) {
	createdSent := make(chan struct{})
	allowLate := make(chan struct{})
	closedObserved := make(chan bool, 1)
	srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
		responsesWSConformanceReadCreate(t, conn)
		responsesWSConformanceWrite(t, conn, `{"type":"response.created","response":{"id":"resp-cancel-fence","status":"in_progress"}}`)
		close(createdSent)
		<-allowLate
		// These frames race the cancellation. They belong to no reservation.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"late-text"}`))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-cancel-fence","status":"completed"}}`))
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := conn.ReadMessage()
		closedObserved <- err != nil
	})
	channel := reserveResponsesWSConformanceChannel(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		dispatch agent.VerifiedToolSurfaceDispatch
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		dispatch, err := channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(ctx, nil, nil, nil, true)
		done <- outcome{dispatch: dispatch, err: err}
	}()
	select {
	case <-createdSent:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start the response")
	}
	cancel()
	var result outcome
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled dispatch did not unwind")
	}
	close(allowLate)
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("cancelled dispatch err=%v, want context.Canceled so the partial stream can never bind", result.err)
	}
	if result.dispatch.Response != nil {
		for _, choice := range result.dispatch.Response.Choices {
			if strings.Contains(choice.Message.Content, "late-text") || len(choice.Message.ToolCalls) != 0 {
				t.Fatalf("late post-cancel frames entered the reservation response: %+v", choice.Message)
			}
		}
	}
	if _, err := channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(context.Background(), nil, nil, nil, true); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("cancelled channel admitted a successor dispatch: %v", err)
	}
	select {
	case closed := <-closedObserved:
		if !closed {
			t.Fatal("provider still observed an open socket after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not observe the cancellation socket close")
	}
}

// TestResponsesWSConformanceSingleSocketSingleFrameNoReplay proves
// HasReplayIdentitySemantics: one reservation writes exactly one
// response.create frame, a second dispatch or late preparation on the same
// channel is rejected without any further write, and any successor request
// must open a new socket with a new connection identity.
func TestResponsesWSConformanceSingleSocketSingleFrameNoReplay(t *testing.T) {
	var frames int32
	srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
		if _, _, err := conn.ReadMessage(); err == nil {
			atomic.AddInt32(&frames, 1)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-replay","status":"completed"}}`))
		}
		// Count any replay attempt on this socket; a compliant client closes
		// instead, which ends this read with an error and adds nothing.
		if _, _, err := conn.ReadMessage(); err == nil {
			atomic.AddInt32(&frames, 1)
		}
	})
	frameCount := func() int { return int(atomic.LoadInt32(&frames)) }
	channel := reserveResponsesWSConformanceChannel(t, srv)
	firstID := channel.ExecutionContext().ConnectionID
	dispatch, err := responsesWSConformanceDoVerified(t, channel, context.Background())
	if err != nil || dispatch.Response == nil || dispatch.Response.ResponseID != "resp-replay" {
		t.Fatalf("dispatch=%+v err=%v", dispatch, err)
	}
	if frameCount() != 1 {
		t.Fatalf("single dispatch wrote %d frames, want exactly one", frameCount())
	}
	if _, err := responsesWSConformanceDoVerified(t, channel, context.Background()); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("same channel admitted a replay dispatch: %v", err)
	}
	if err := channel.(agent.ToolSurfaceDispatchPreparationRequestChannel).SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: agent.ToolSurfacePlanEvidence{}, InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err == nil || !strings.Contains(err.Error(), "after dispatch attempt") {
		t.Fatalf("used channel accepted late dispatch preparation: %v", err)
	}
	if frameCount() != 1 {
		t.Fatalf("rejected replay still wrote frames: %d", frameCount())
	}
	// A successor reservation is a new socket with a new identity, never a
	// replay of the previous one.
	successor := reserveResponsesWSConformanceChannel(t, srv)
	if successor.ExecutionContext().ConnectionID == firstID {
		t.Fatalf("successor reservation reused connection identity %q", firstID)
	}
	if _, err := responsesWSConformanceDoVerified(t, successor, context.Background()); err != nil {
		t.Fatalf("successor reservation could not dispatch on its own socket: %v", err)
	}
	if frameCount() != 2 {
		t.Fatalf("successor reservation did not write exactly its own frame: %d", frameCount())
	}
}
