package guiapp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingResponsesWSReplacementContractVersion is the loopback contract suite
// version for the replacement-semantics certificate. It must change exactly
// when codingResponsesWSReplacementSemanticsVersion does; the equality test
// below pins the certificate to this suite in the same commit.
const codingResponsesWSReplacementContractVersion = "responses-ws-replacement-semantics-v1"

func TestCodingResponsesWSReplacementSemanticsVersionMatchesContractSuite(t *testing.T) {
	if codingResponsesWSReplacementSemanticsVersion != codingResponsesWSReplacementContractVersion {
		t.Fatalf("replacement semantics certificate version %q does not match contract suite version %q", codingResponsesWSReplacementSemanticsVersion, codingResponsesWSReplacementContractVersion)
	}
}

// codingResponsesWSReplacementContractCoverage maps every certificate field to
// the contract tests that prove it. Function references make a missing or
// renamed test a compile error; the meta-test below makes an unmapped
// certificate field a test failure.
var codingResponsesWSReplacementContractCoverage = map[string][]func(*testing.T){
	"Version":                      {TestCodingResponsesWSReplacementSemanticsVersionMatchesContractSuite},
	"Protocol":                     {TestCodingDynamicReplacementSemanticsCertificateIdentityMatchesAdapterRow},
	"Envelope":                     {TestCodingDynamicReplacementSemanticsCertificateIdentityMatchesAdapterRow},
	"ExplicitEmptySurfaceVerified": {TestResponsesWSReplacementContractExplicitEmptySurfaceIsSentAndCanonical},
	"RejectsToolBearingRedirects":  {TestResponsesWSReplacementContractToolBearingRedirectHasNoEnvelopePath},
	"PolicyProjectionVersion":      {TestResponsesWSReplacementContractPolicyProjectionIsFixedAndHostOwned},
	"AppendContractTested":         {TestResponsesWSReplacementContractAppendSemanticsFailsClosed},
	"RetainContractTested":         {TestResponsesWSReplacementContractRetainSemanticsFailsClosed},
}

// TestCodingDynamicReplacementSemanticsCertificateIdentityMatchesAdapterRow
// pins the certificate's identity fields (Protocol/Envelope/projection
// version) to the qualified adapter row they certify.
func TestCodingDynamicReplacementSemanticsCertificateIdentityMatchesAdapterRow(t *testing.T) {
	certificate := codingDynamicResponsesWSReplacementSemanticsCertificate()
	capability := codingDynamicProviderCorrelationForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if !certificate.validFor(capability) {
		t.Fatalf("compile-time certificate is not valid for the qualified row: %#v", certificate)
	}
	if certificate.Protocol != capability.Protocol || certificate.Envelope != agent.ToolSurfaceEnvelopeResponses || certificate.PolicyProjectionVersion != codingResponsesWSPolicyProjectionVersion {
		t.Fatalf("certificate identity drifted from the adapter row: %#v vs %#v", certificate, capability)
	}
}

func TestCodingDynamicReplacementSemanticsCertificateFieldsHaveContractCoverage(t *testing.T) {
	certificateType := reflect.TypeOf(codingDynamicReplacementSemanticsCertificate{})
	for i := 0; i < certificateType.NumField(); i++ {
		field := certificateType.Field(i)
		if len(codingResponsesWSReplacementContractCoverage[field.Name]) == 0 {
			t.Errorf("certificate field %s has no replacement contract coverage", field.Name)
		}
	}
}

func responsesWSReplacementContractSearchTool(name string) map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": "search the web",
			"parameters": map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{"query": map[string]interface{}{"type": "string"}},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	}
}

func responsesWSReplacementContractFrameTools(t *testing.T, frameData []byte) (interface{}, bool) {
	t.Helper()
	var frame map[string]interface{}
	if err := json.Unmarshal(frameData, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	tools, present := frame["tools"]
	return tools, present
}

// TestResponsesWSReplacementContractExplicitEmptySurfaceIsSentAndCanonical
// proves ExplicitEmptySurfaceVerified: an empty surface is written as an
// explicit `tools: []` replacement on the wire (never an omitted field whose
// meaning a stateful provider could inherit), and its canonical receipt
// encoding is complete and distinct from any non-empty surface.
func TestResponsesWSReplacementContractExplicitEmptySurfaceIsSentAndCanonical(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	policy := agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)
	frame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, present := responsesWSReplacementContractFrameTools(t, frame)
	if !present {
		t.Fatal("empty surface was serialized as an omitted tools field instead of an explicit empty replacement")
	}
	if list, ok := tools.([]interface{}); !ok || len(list) != 0 {
		t.Fatalf("empty surface serialized as %#v, want an explicit empty array", tools)
	}
	receipt, err := verifyResponsesWSFrameToolSurface(frame, nil, policy)
	if err != nil || !receipt.Verified || receipt.ExpectedToolCount != 0 || receipt.WireToolCount != 0 || receipt.PayloadDigest != receipt.WirePayloadHash {
		t.Fatalf("empty surface receipt=%+v err=%v", receipt, err)
	}
	// An empty replacement is canonically distinct from a non-empty surface and
	// deterministic for identical input.
	nonEmptyFrame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, []map[string]interface{}{responsesWSReplacementContractSearchTool("search")})
	if err != nil {
		t.Fatal(err)
	}
	nonEmptyReceipt, err := verifyResponsesWSFrameToolSurface(nonEmptyFrame, []map[string]interface{}{responsesWSReplacementContractSearchTool("search")}, policy)
	if err != nil || !nonEmptyReceipt.Verified {
		t.Fatalf("non-empty surface receipt=%+v err=%v", nonEmptyReceipt, err)
	}
	if receipt.PayloadDigest == nonEmptyReceipt.PayloadDigest {
		t.Fatal("canonical encoding cannot distinguish an explicit empty replacement from a non-empty surface")
	}
	secondEmpty, err := verifyResponsesWSFrameToolSurface(frame, nil, policy)
	if err != nil || secondEmpty.PayloadDigest != receipt.PayloadDigest {
		t.Fatalf("canonical empty-surface encoding is not deterministic: %+v vs %+v", secondEmpty, receipt)
	}
	// A required tool choice is not satisfiable on an explicit empty surface;
	// the manifest contract rejects the combination before any send.
	requiredPolicy := agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceRequired}}
	if _, err := verifyResponsesWSFrameToolSurface(frame, nil, requiredPolicy); err == nil {
		t.Fatal("required tool choice was accepted on an explicit empty surface")
	}

	// Wire proof: the fake provider receives the explicit empty array verbatim.
	var received []byte
	srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
		_, frame, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read response.create: %v", err)
			return
		}
		received = append([]byte(nil), frame...)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-empty-surface","status":"completed"}}`))
	})
	channel := reserveResponsesWSConformanceChannel(t, srv)
	dispatch, err := responsesWSConformanceDoVerified(t, channel, context.Background())
	if err != nil || dispatch.Response == nil {
		t.Fatalf("empty surface dispatch=%+v err=%v", dispatch, err)
	}
	tools, present = responsesWSReplacementContractFrameTools(t, received)
	if !present {
		t.Fatal("provider received a payload with the tools field omitted")
	}
	if list, ok := tools.([]interface{}); !ok || len(list) != 0 {
		t.Fatalf("provider received tools=%#v, want explicit empty replacement", tools)
	}
	if !dispatch.Receipt.Verified || dispatch.Receipt.ExpectedToolCount != 0 || dispatch.Receipt.WireToolCount != 0 || dispatch.Receipt.PayloadDigest != dispatch.Receipt.WirePayloadHash {
		t.Fatalf("dispatch receipt=%+v", dispatch.Receipt)
	}
}

// TestResponsesWSReplacementContractToolBearingRedirectHasNoEnvelopePath
// proves RejectsToolBearingRedirects for the Responses WebSocket envelope.
// This envelope has no redirect semantic at all: the channel owns exactly one
// write, so a "redirect" could only exist as an implicit second send. The
// contract assertions target that shape rather than the transport's single-use
// fence (E1): neither a completed nor a failed dispatch lets any later call
// inherit the reservation's receipt, and a rejected pre-write setup leaves no
// receipt a redirect could carry.
func TestResponsesWSReplacementContractToolBearingRedirectHasNoEnvelopePath(t *testing.T) {
	t.Run("completed dispatch cannot be re-sent or inherit its receipt", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			responsesWSConformanceReadCreate(t, conn)
			responsesWSConformanceWrite(t, conn, `{"type":"response.completed","response":{"id":"resp-redirect-done","status":"completed"}}`)
		})
		channel := reserveResponsesWSConformanceChannel(t, srv)
		dispatch, err := responsesWSConformanceDoVerified(t, channel, context.Background())
		if err != nil || !dispatch.Receipt.Verified {
			t.Fatalf("dispatch=%+v err=%v", dispatch, err)
		}
		second, err := responsesWSConformanceDoVerified(t, channel, context.Background())
		if err == nil || !strings.Contains(err.Error(), "already used") {
			t.Fatalf("redirect-shaped second send was accepted: %v", err)
		}
		if second.Receipt != (agent.ToolSurfaceReceipt{}) || second.Response != nil {
			t.Fatalf("second send inherited the predecessor receipt/response: %+v", second)
		}
	})

	t.Run("failed pre-write dispatch leaves no receipt a redirect could carry", func(t *testing.T) {
		srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
			_, frame, err := conn.ReadMessage()
			if err == nil && len(frame) > 0 {
				t.Errorf("redirect-shaped retry wrote a frame after a failed dispatch")
			}
		})
		cfg := responsesWSConformanceConfig(srv)
		channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), &IMMessageHandler{}, cfg, srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		defer channel.Close(nil)
		// Dispatch without the atomic preparation: fails before any write.
		if _, err := channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(context.Background(), nil, nil, nil, true); err == nil || !strings.Contains(err.Error(), "audit evidence was not set") {
			t.Fatalf("unprepared dispatch accepted: %v", err)
		}
		if err := channel.(agent.ToolSurfaceDispatchPreparationRequestChannel).SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: agent.ToolSurfacePlanEvidence{}, InvocationPolicy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)}); err == nil {
			t.Fatal("failed dispatch accepted late preparation as a redirect-shaped retry")
		}
		retry, err := channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(context.Background(), nil, nil, nil, true)
		if err == nil || !strings.Contains(err.Error(), "already used") {
			t.Fatalf("failed dispatch admitted a redirect-shaped retry: %v", err)
		}
		if retry.Receipt != (agent.ToolSurfaceReceipt{}) || retry.Response != nil {
			t.Fatalf("redirect-shaped retry inherited receipt/response: %+v", retry)
		}
	})
}

// TestResponsesWSReplacementContractPolicyProjectionIsFixedAndHostOwned proves
// PolicyProjectionVersion: the tool_choice/parallel_tool_calls mapping into
// the Responses envelope is the fixed host-owned projection pinned by
// codingResponsesWSPolicyProjectionVersion, and conversation content can never
// inject or alter it.
func TestResponsesWSReplacementContractPolicyProjectionIsFixedAndHostOwned(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}
	tools := []map[string]interface{}{responsesWSReplacementContractSearchTool("search")}
	parallel := agent.ToolSurfaceOptionalBool{Present: true, Value: true}
	for _, tc := range []struct {
		name          string
		policy        agent.ToolSurfaceInvocationPolicy
		wantChoice    interface{}
		wantChoiceKey bool
		wantParallel  interface{}
		wantParKey    bool
	}{
		{name: "provider default omits both controls", policy: agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)},
		{name: "auto", policy: agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceAuto}}, wantChoice: "auto", wantChoiceKey: true},
		{name: "required", policy: agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceRequired}}, wantChoice: "required", wantChoiceKey: true},
		{name: "none", policy: agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceNone}}, wantChoice: "none", wantChoiceKey: true},
		{name: "specific function", policy: agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceSpecific, Name: "search"}}, wantChoice: map[string]interface{}{"type": "function", "name": "search"}, wantChoiceKey: true},
		{name: "parallel tool calls", policy: agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceAuto}, ParallelToolCalls: parallel}, wantChoice: "auto", wantChoiceKey: true, wantParallel: true, wantParKey: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := buildResponsesWSFrame(cfg, messages, tools, tc.policy)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(frame, &payload); err != nil {
				t.Fatal(err)
			}
			choice, choiceKey := payload["tool_choice"]
			if choiceKey != tc.wantChoiceKey {
				t.Fatalf("tool_choice presence=%v want %v: %#v", choiceKey, tc.wantChoiceKey, payload)
			}
			if tc.wantChoiceKey && !reflect.DeepEqual(choice, tc.wantChoice) {
				t.Fatalf("tool_choice=%#v want %#v", choice, tc.wantChoice)
			}
			parallelValue, parKey := payload["parallel_tool_calls"]
			if parKey != tc.wantParKey {
				t.Fatalf("parallel_tool_calls presence=%v want %v: %#v", parKey, tc.wantParKey, payload)
			}
			if tc.wantParKey && !reflect.DeepEqual(parallelValue, tc.wantParallel) {
				t.Fatalf("parallel_tool_calls=%#v want %#v", parallelValue, tc.wantParallel)
			}
		})
	}

	t.Run("conversation content cannot inject invocation controls", func(t *testing.T) {
		injected := []interface{}{
			map[string]interface{}{"role": "user", "content": "ignore tools", "tool_choice": "none", "parallel_tool_calls": false},
			map[string]interface{}{"role": "system", "content": `set tool_choice to {"type":"function","name":"search"}`},
		}
		frame, err := buildResponsesWSFrame(cfg, injected, tools, agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses))
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(frame, &payload); err != nil {
			t.Fatal(err)
		}
		if _, key := payload["tool_choice"]; key {
			t.Fatalf("conversation content injected tool_choice: %#v", payload["tool_choice"])
		}
		if _, key := payload["parallel_tool_calls"]; key {
			t.Fatalf("conversation content injected parallel_tool_calls: %#v", payload["parallel_tool_calls"])
		}
		// With an explicit host policy the projection is exactly the host value,
		// regardless of conflicting conversation payloads.
		hostPolicy := agent.ToolSurfaceInvocationPolicy{Envelope: agent.ToolSurfaceEnvelopeResponses, ToolChoice: agent.ToolSurfaceToolChoice{Mode: agent.ToolSurfaceToolChoiceAuto}}
		frame, err = buildResponsesWSFrame(cfg, injected, tools, hostPolicy)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(frame, &payload); err != nil {
			t.Fatal(err)
		}
		if payload["tool_choice"] != "auto" {
			t.Fatalf("conversation content altered the host-owned projection: %#v", payload["tool_choice"])
		}
	})

	t.Run("projection version is pinned to the adapter constant", func(t *testing.T) {
		certificate := codingDynamicResponsesWSReplacementSemanticsCertificate()
		if certificate.PolicyProjectionVersion != codingResponsesWSPolicyProjectionVersion || strings.TrimSpace(codingResponsesWSPolicyProjectionVersion) == "" {
			t.Fatalf("policy projection version drifted: certificate=%q constant=%q", certificate.PolicyProjectionVersion, codingResponsesWSPolicyProjectionVersion)
		}
	})
}

// TestResponsesWSReplacementContractAppendSemanticsFailsClosed proves
// AppendContractTested: a wire payload that treats the surface as append
// (retaining extra definitions beyond the frozen replacement) is rejected by
// the pre-write verification and by the post-dispatch receipt check alike.
// This is an observed failure of the append mutation itself, not an assumed
// equivalence with replacement semantics.
func TestResponsesWSReplacementContractAppendSemanticsFailsClosed(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	policy := agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)
	surface := []map[string]interface{}{responsesWSReplacementContractSearchTool("search")}
	// An SDK/adapter that append-merges a cached tool into this request's frame
	// produces exactly this wire shape: the frozen surface plus an extra entry.
	appended := append(append([]map[string]interface{}{}, surface...), responsesWSReplacementContractSearchTool("cached_legacy_tool"))
	appendedFrame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, appended)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifyResponsesWSFrameToolSurface(appendedFrame, surface, policy)
	if err == nil {
		t.Fatalf("append-mutated payload passed pre-write verification: receipt=%+v", receipt)
	}
	if receipt.Verified {
		t.Fatalf("append-mutated payload produced a verified receipt: %+v", receipt)
	}
	if err := agent.VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(surface, policy, agent.ToolSurfacePlanEvidence{}, receipt); err == nil {
		t.Fatal("append-mutated receipt passed the post-dispatch bind-path check")
	}
}

// TestResponsesWSReplacementContractRetainSemanticsFailsClosed proves
// RetainContractTested: a wire payload that retains a predecessor tool after
// this request's explicit empty replacement is rejected by the same two
// boundaries, independently of the append case.
func TestResponsesWSReplacementContractRetainSemanticsFailsClosed(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{URL: "https://example.test", Model: "test", WireAPI: "responses-ws"}
	policy := agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)
	// This request's frozen surface is the explicit empty replacement; a
	// retain-semantics serializer would still write the predecessor's tool.
	retainedFrame, err := buildResponsesWSFrame(cfg, []interface{}{map[string]interface{}{"role": "user", "content": "hi"}}, []map[string]interface{}{responsesWSReplacementContractSearchTool("predecessor_tool")})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := verifyResponsesWSFrameToolSurface(retainedFrame, nil, policy)
	if err == nil {
		t.Fatalf("retain-mutated payload passed pre-write verification: receipt=%+v", receipt)
	}
	if receipt.Verified {
		t.Fatalf("retain-mutated payload produced a verified receipt: %+v", receipt)
	}
	if err := agent.VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(nil, policy, agent.ToolSurfacePlanEvidence{}, receipt); err == nil {
		t.Fatal("retain-mutated receipt passed the post-dispatch bind-path check")
	}
}

// TestResponsesWSReplacementContractReceiptDigestEqualsWirePayloadHash is the
// ReceiptDispatchVersion evidence: the receipt returned by the same one-shot
// dispatch that produced the response is computed over the exact bytes the
// provider received, and RunLoop's bind-path verifier accepts precisely that
// receipt. A transport-local log or a tampered copy cannot stand in.
func TestResponsesWSReplacementContractReceiptDigestEqualsWirePayloadHash(t *testing.T) {
	tools := []map[string]interface{}{responsesWSReplacementContractSearchTool("search")}
	var received []byte
	srv := newResponsesWSConformanceProvider(t, func(conn *websocket.Conn) {
		_, frame, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("read response.create: %v", err)
			return
		}
		received = append([]byte(nil), frame...)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp-receipt-digest","status":"completed"}}`))
	})
	cfg := responsesWSConformanceConfig(srv)
	channel, err := reserveCodingResponsesWSRequestChannel(context.Background(), &IMMessageHandler{}, cfg, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close(nil)
	policy := agent.DefaultToolSurfaceInvocationPolicy(agent.ToolSurfaceEnvelopeResponses)
	if err := channel.(agent.ToolSurfaceDispatchPreparationRequestChannel).SetToolSurfaceDispatchPreparation(agent.ToolSurfaceDispatchPreparation{AuditEvidence: agent.ToolSurfacePlanEvidence{}, InvocationPolicy: policy}); err != nil {
		t.Fatal(err)
	}
	dispatch, err := channel.(agent.VerifiedToolSurfaceRequestChannel).DoVerified(context.Background(), []interface{}{map[string]interface{}{"role": "user", "content": "receipt"}}, tools, nil, true)
	if err != nil || dispatch.Response == nil {
		t.Fatalf("dispatch=%+v err=%v", dispatch, err)
	}
	if len(received) == 0 {
		t.Fatal("provider received no frame")
	}
	if !dispatch.Receipt.Verified || dispatch.Receipt.Handoff != agent.ToolSurfaceHandoffStarted {
		t.Fatalf("dispatch receipt=%+v", dispatch.Receipt)
	}
	// Recompute the receipt over the exact bytes the provider received, with
	// the same frozen surface and policy: it must be identical to the receipt
	// the single dispatch returned, and the bind-path verifier must accept it.
	var receivedPayload map[string]interface{}
	if err := json.Unmarshal(received, &receivedPayload); err != nil {
		t.Fatal(err)
	}
	recomputed, err := agent.VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools, receivedPayload, policy, agent.ToolSurfacePlanEvidence{})
	if err != nil || !recomputed.Verified {
		t.Fatalf("recomputed receipt=%+v err=%v", recomputed, err)
	}
	if recomputed.PayloadDigest != dispatch.Receipt.PayloadDigest || recomputed.WirePayloadHash != dispatch.Receipt.WirePayloadHash || dispatch.Receipt.PayloadDigest != dispatch.Receipt.WirePayloadHash {
		t.Fatalf("dispatch receipt does not match the wire payload: dispatch=%+v recomputed=%+v", dispatch.Receipt, recomputed)
	}
	if err := agent.VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(tools, policy, agent.ToolSurfacePlanEvidence{}, dispatch.Receipt); err != nil {
		t.Fatalf("bind-path verifier rejected the dispatch receipt: %v", err)
	}
	// A payload mutated after the fact (for example a retained tool injected by
	// a middlebox) must fail the same verification against this receipt.
	receivedPayload["tools"] = []interface{}{map[string]interface{}{"type": "function", "name": "middlebox_tool", "parameters": map[string]interface{}{"type": "object"}}}
	tampered, err := json.Marshal(receivedPayload)
	if err != nil {
		t.Fatal(err)
	}
	var tamperedPayload map[string]interface{}
	if err := json.Unmarshal(tampered, &tamperedPayload); err != nil {
		t.Fatal(err)
	}
	tamperedReceipt, tamperedErr := agent.VerifyToolSurfaceRequestPayloadWithAuditEvidence(tools, tamperedPayload, policy, agent.ToolSurfacePlanEvidence{})
	if tamperedErr == nil || tamperedReceipt.Verified {
		t.Fatalf("tampered payload passed verification: receipt=%+v err=%v", tamperedReceipt, tamperedErr)
	}
	if err := agent.VerifyToolSurfaceReceiptForRenderedToolsWithAuditEvidence(tools, policy, agent.ToolSurfacePlanEvidence{}, tamperedReceipt); err == nil {
		t.Fatal("bind-path verifier accepted a tampered payload")
	}
}
