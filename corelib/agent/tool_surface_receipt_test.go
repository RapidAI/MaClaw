package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

type receiptRecorder struct {
	receipts []ToolSurfaceReceipt
}

type lifecycleEventRecorder struct {
	events []ToolSurfaceEvent
}

func (r *lifecycleEventRecorder) OnToolSurfaceEvent(event ToolSurfaceEvent) {
	r.events = append(r.events, event)
}

func TestToolSurfaceLifecycleManifestConstructionFailureClosesTerminal(t *testing.T) {
	invalid := []map[string]interface{}{{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "invalid_description",
			"description": 42,
			"parameters":  map[string]interface{}{"type": "object"},
		},
	}}
	events := &lifecycleEventRecorder{}
	client, err := NewToolSurfaceReceiptHTTPClientWithLifecycleEvents(nil, invalid, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), ToolSurfacePlanEvidence{}, nil, events)
	if err == nil || client != nil {
		t.Fatalf("invalid manifest unexpectedly created client=%#v err=%v", client, err)
	}
	if len(events.events) != 2 {
		t.Fatalf("events=%+v, want integrity failure plus terminal", events.events)
	}
	if integrity := events.events[0]; integrity.Kind != ToolSurfaceEventIntegrityFailure || integrity.FailureKind != ToolSurfaceFailureIntegrity {
		t.Fatalf("integrity event=%+v", integrity)
	}
	if terminal := events.events[1]; terminal.Kind != ToolSurfaceEventTerminalReason || terminal.TerminalReason != ToolSurfaceIntegrityFailure || terminal.FailureKind != ToolSurfaceFailureIntegrity || terminal.PayloadDigest != "" || terminal.AuditDigest != "" || terminal.ExpectedToolCount != 0 || terminal.ReplacementMode != "" {
		t.Fatalf("pre-manifest terminal must be a redacted uncorrelated integrity failure: %+v", terminal)
	}
}

func TestToolSurfaceReceiptBlocksAutomaticRedirectAfterFirstSend(t *testing.T) {
	definitions := receiptDefinitions()
	recorder := &receiptRecorder{}
	var first, target int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/first":
			first++
			http.Redirect(w, request, "/target", http.StatusTemporaryRedirect)
		case "/target":
			target++
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
	}))
	defer server.Close()

	client, err := newToolSurfaceReceiptHTTPClient(server.Client(), definitions, recorder)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/first", receiptRequestBody(t, []interface{}{definitions[0], definitions[1]}))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "tool_surface_redirect_blocked") {
		t.Fatalf("redirect error=%v, want tool_surface_redirect_blocked", err)
	}
	if first != 1 || target != 0 {
		t.Fatalf("request counts first=%d target=%d; redirect must not create a second send", first, target)
	}
	if len(recorder.receipts) != 1 || !recorder.receipts[0].Verified || recorder.receipts[0].Handoff != ToolSurfaceHandoffStarted {
		t.Fatalf("receipts=%+v", recorder.receipts)
	}
}

func TestToolSurfaceReceiptMarksTransportFailureAsAmbiguous(t *testing.T) {
	definitions := receiptDefinitions()
	recorder := &receiptRecorder{}
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("simulated write failure")
	})}, definitions, recorder)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, []interface{}{definitions[0], definitions[1]}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("transport failure unexpectedly succeeded")
	}
	if len(recorder.receipts) != 1 || !recorder.receipts[0].Verified || recorder.receipts[0].Handoff != ToolSurfaceHandoffAmbiguous {
		t.Fatalf("receipts=%+v", recorder.receipts)
	}
}

func TestToolSurfaceReceiptDisablesStandardLibraryReplayAtFinalBoundary(t *testing.T) {
	definitions := receiptDefinitions()
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.GetBody != nil {
			t.Fatal("receipt transport left outbound request rewindable")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}, definitions, nil)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, []interface{}{definitions[0], definitions[1]}))
	if err != nil {
		t.Fatal(err)
	}
	// This header would otherwise make a POST replayable to net/http when a
	// reused connection encounters an ambiguous failure; without GetBody, the
	// concrete transport cannot rewind a consumed body for that replay.
	request.Header.Set("Idempotency-Key", "caller-provided-key")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func (r *receiptRecorder) OnToolSurfaceReceipt(receipt ToolSurfaceReceipt) {
	r.receipts = append(r.receipts, receipt)
}

func receiptDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		tooldef.BuildToolDef("read_file", "Read a file", map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}}}),
		tooldef.BuildToolDef("search", "Search", map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}}),
	}
}

func receiptRequestBody(t *testing.T, tools interface{}) io.ReadCloser {
	t.Helper()
	data, err := json.Marshal(map[string]interface{}{"model": "test", "tools": tools})
	if err != nil {
		t.Fatal(err)
	}
	return io.NopCloser(strings.NewReader(string(data)))
}

func TestToolSurfaceReceiptAcceptsCanonicalWireReplacement(t *testing.T) {
	definitions := receiptDefinitions()
	recorder := &receiptRecorder{}
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}, definitions, recorder)
	if err != nil {
		t.Fatal(err)
	}
	// Wire ordering may differ from the host's representation. Completeness is
	// set equality over canonical definitions, not incidental slice order.
	reversed := []interface{}{definitions[1], definitions[0]}
	request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, reversed))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("verified replacement was rejected: %v", err)
	}
	response.Body.Close()
	if len(recorder.receipts) != 1 || !recorder.receipts[0].Verified || recorder.receipts[0].ReplacementMode != "replace" {
		t.Fatalf("receipts=%+v", recorder.receipts)
	}
}

func TestToolSurfaceReceiptRejectsDroppedOrAppendedWireDefinitions(t *testing.T) {
	definitions := receiptDefinitions()
	for name, wire := range map[string]interface{}{
		"dropped":  []interface{}{definitions[0]},
		"appended": []interface{}{definitions[0], definitions[1], tooldef.BuildToolDef("history_alias", "stale", map[string]interface{}{"type": "object"})},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &receiptRecorder{}
			client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("transport must not run after surface-integrity failure")
				return nil, nil
			})}, definitions, recorder)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, wire))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Do(request)
			if err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
				t.Fatalf("error=%v, want surface-integrity failure", err)
			}
			if len(recorder.receipts) != 1 || recorder.receipts[0].Verified || !strings.Contains(recorder.receipts[0].Failure, "surface_integrity_failure") {
				t.Fatalf("receipts=%+v", recorder.receipts)
			}
		})
	}
}

func TestToolSurfaceReceiptRejectsWireDescriptionMutation(t *testing.T) {
	definitions := receiptDefinitions()
	mutated := tooldef.BuildToolDef("read_file", "Run arbitrary shell commands", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
	})
	recorder := &receiptRecorder{}
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run after a model-visible description mutation")
		return nil, nil
	})}, definitions, recorder)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, []interface{}{mutated, definitions[1]}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
		t.Fatalf("error=%v, want surface-integrity failure", err)
	}
	if len(recorder.receipts) != 1 || recorder.receipts[0].Verified || !strings.Contains(recorder.receipts[0].Failure, "surface_integrity_failure") {
		t.Fatalf("receipts=%+v", recorder.receipts)
	}
}

func TestToolSurfaceReceiptRejectsWireToolTypeMutation(t *testing.T) {
	definitions := receiptDefinitions()
	mutated := tooldef.BuildToolDef("read_file", "Read a file", map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
		},
	})
	mutated["type"] = "web_search_preview"
	recorder := &receiptRecorder{}
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run after a tool-type mutation")
		return nil, nil
	})}, definitions, recorder)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test", receiptRequestBody(t, []interface{}{mutated, definitions[1]}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
		t.Fatalf("error=%v, want surface-integrity failure", err)
	}
	if len(recorder.receipts) != 1 || recorder.receipts[0].Verified || !strings.Contains(recorder.receipts[0].Failure, "surface_integrity_failure") {
		t.Fatalf("receipts=%+v", recorder.receipts)
	}
}

func TestToolSurfaceReceiptMakesEmptySurfaceAnExplicitReplacement(t *testing.T) {
	recorder := &receiptRecorder{}
	var sawTools bool
	client, err := newToolSurfaceReceiptHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var payload map[string]interface{}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		tools, present := payload["tools"]
		sawTools = present
		if values, ok := tools.([]interface{}); !ok || len(values) != 0 {
			t.Fatalf("tools=%#v, want explicit empty replacement", tools)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}, nil, recorder)
	if err != nil {
		t.Fatal(err)
	}
	data := io.NopCloser(strings.NewReader(`{"model":"test"}`))
	request, err := http.NewRequest(http.MethodPost, "https://example.test", data)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("empty replacement failed: %v", err)
	}
	response.Body.Close()
	if !sawTools || len(recorder.receipts) != 1 || !recorder.receipts[0].Verified {
		t.Fatalf("sawTools=%v receipts=%+v", sawTools, recorder.receipts)
	}
}

func TestToolSurfaceReceiptRejectsInvocationPolicyMutationBeforeTransport(t *testing.T) {
	definitions := receiptDefinitions()
	cases := []struct {
		name     string
		expected ToolSurfaceInvocationPolicy
		payload  map[string]interface{}
	}{
		{
			name:     "tool choice auto to required",
			expected: ToolSurfaceInvocationPolicy{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceAuto}},
			payload:  map[string]interface{}{"tools": []interface{}{definitions[0], definitions[1]}, "tool_choice": "required"},
		},
		{
			name:     "specific function changes",
			expected: ToolSurfaceInvocationPolicy{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "read_file"}},
			payload:  map[string]interface{}{"tools": []interface{}{definitions[0], definitions[1]}, "tool_choice": map[string]interface{}{"type": "function", "name": "search"}},
		},
		{
			name:     "parallel absent to false",
			expected: ToolSurfaceInvocationPolicy{Envelope: ToolSurfaceEnvelopeResponses, ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceProviderDefault}},
			payload:  map[string]interface{}{"tools": []interface{}{definitions[0], definitions[1]}, "parallel_tool_calls": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transportCalled := false
			client, err := NewToolSurfaceReceiptHTTPClientWithInvocationPolicy(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				transportCalled = true
				return nil, fmt.Errorf("transport must not run after policy mutation")
			})}, definitions, tc.expected, nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequest(http.MethodPost, "https://example.test", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Do(request)
			if err == nil || !strings.Contains(err.Error(), "surface_integrity_failure") {
				t.Fatalf("error=%v, want surface_integrity_failure", err)
			}
			if transportCalled {
				t.Fatal("transport received a policy-mutated surface")
			}
		})
	}
}

func TestToolSurfaceReceiptAcceptsProviderNativeSpecificToolChoiceProjection(t *testing.T) {
	definitions := receiptDefinitions()
	policy := ToolSurfaceInvocationPolicy{
		Envelope:          ToolSurfaceEnvelopeOpenAIChat,
		ToolChoice:        ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "read_file"},
		ParallelToolCalls: ToolSurfaceOptionalBool{Present: true, Value: false},
	}
	payload := map[string]interface{}{
		"tools": []interface{}{definitions[0], definitions[1]},
		"tool_choice": map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": "read_file"},
		},
		"parallel_tool_calls": false,
	}
	receipt, err := VerifyToolSurfaceRequestPayload(definitions, payload, policy)
	if err != nil || !receipt.Verified || receipt.PayloadDigest == "" || receipt.WirePayloadHash != receipt.PayloadDigest {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}

func TestToolSurfaceReceiptRejectsSpecificToolChoiceOutsideRenderedSurface(t *testing.T) {
	definitions := receiptDefinitions()
	policy := ToolSurfaceInvocationPolicy{
		Envelope:   ToolSurfaceEnvelopeResponses,
		ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceSpecific, Name: "not_rendered"},
	}
	payload := map[string]interface{}{
		"tools":       []interface{}{definitions[0], definitions[1]},
		"tool_choice": map[string]interface{}{"type": "function", "name": "not_rendered"},
	}
	receipt, err := VerifyToolSurfaceRequestPayload(definitions, payload, policy)
	if err == nil || receipt.Verified || !strings.Contains(receipt.Failure, "not present in tool surface") {
		t.Fatalf("orphaned specific tool choice accepted: receipt=%+v err=%v", receipt, err)
	}
}

func TestToolSurfaceReceiptRejectsRequiredPolicyOnEmptySurface(t *testing.T) {
	policy := ToolSurfaceInvocationPolicy{
		Envelope:   ToolSurfaceEnvelopeResponses,
		ToolChoice: ToolSurfaceToolChoice{Mode: ToolSurfaceToolChoiceRequired},
	}
	receipt, err := VerifyToolSurfaceRequestPayload(nil, map[string]interface{}{
		"tools":       []interface{}{},
		"tool_choice": "required",
	}, policy)
	if err == nil || receipt.Verified || !strings.Contains(receipt.Failure, "not satisfiable on an empty tool surface") {
		t.Fatalf("required policy on empty surface accepted: receipt=%+v err=%v", receipt, err)
	}
}

func TestToolSurfaceReceiptAuditDigestRecordsStablePlanOmissions(t *testing.T) {
	definitions := receiptDefinitions()
	policy := DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat)
	first := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-immutable-1",
		PlanSnapshotDigest: "snapshot-immutable-1",
		CatalogGeneration:  7,
		Omitted: []ToolSurfaceOmission{
			{NeedID: "write", ReasonCode: "budget_exhausted"},
			{NeedID: "shell", ReasonCode: "policy_denied"},
			{NeedID: "write", ReasonCode: "budget_exhausted"}, // deduplicated audit fact
		},
	}
	second := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-immutable-1",
		PlanSnapshotDigest: "snapshot-immutable-1",
		CatalogGeneration:  7,
		Omitted: []ToolSurfaceOmission{
			{NeedID: "shell", ReasonCode: "policy_denied"},
			{NeedID: "write", ReasonCode: "budget_exhausted"},
		},
	}
	firstReceipt, err := VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, policy, first)
	if err != nil {
		t.Fatal(err)
	}
	secondReceipt, err := VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, policy, second)
	if err != nil {
		t.Fatal(err)
	}
	if firstReceipt.PayloadDigest != secondReceipt.PayloadDigest || firstReceipt.AuditDigest != secondReceipt.AuditDigest {
		t.Fatalf("equivalent omission evidence was not stable: first=%+v second=%+v", firstReceipt, secondReceipt)
	}
	changed := second
	changed.Omitted = []ToolSurfaceOmission{{NeedID: "write", ReasonCode: "transport_unsupported"}}
	changedReceipt, err := VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, policy, changed)
	if err != nil {
		t.Fatal(err)
	}
	if changedReceipt.PayloadDigest != firstReceipt.PayloadDigest || changedReceipt.AuditDigest == firstReceipt.AuditDigest {
		t.Fatalf("omission audit changed payload proof or failed to change audit proof: first=%+v changed=%+v", firstReceipt, changedReceipt)
	}
}

func TestToolSurfaceReceiptLifecycleCarriesImmutableAuditEvidenceToFinalReceipt(t *testing.T) {
	definitions := receiptDefinitions()
	evidence := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-static-audit-1",
		PlanSnapshotDigest: "snapshot-static-audit-1",
		CatalogGeneration:  9,
		Omitted: []ToolSurfaceOmission{
			{NeedID: "shell", ReasonCode: "policy_denied"},
		},
	}
	receipts := &receiptRecorder{}
	events := &lifecycleEventRecorder{}
	client, err := NewToolSurfaceReceiptHTTPClientWithLifecycleEvents(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}, definitions, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), evidence, receipts, events)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]interface{}{"tools": definitions})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if len(receipts.receipts) != 1 || !receipts.receipts[0].Verified {
		t.Fatalf("receipts=%+v", receipts.receipts)
	}
	if len(events.events) != 2 {
		t.Fatalf("events=%+v", events.events)
	}
	manifest, omission := events.events[0], events.events[1]
	if manifest.Kind != ToolSurfaceEventManifestCreated || manifest.AuditDigest == "" {
		t.Fatalf("manifest=%+v", manifest)
	}
	if omission.Kind != ToolSurfaceEventOmissionReason || omission.AuditDigest != manifest.AuditDigest {
		t.Fatalf("omission=%+v manifest=%+v", omission, manifest)
	}
	if receipt := receipts.receipts[0]; receipt.AuditDigest != manifest.AuditDigest {
		t.Fatalf("receipt audit digest diverged: receipt=%+v manifest=%+v", receipt, manifest)
	}
}

func TestToolSurfaceReceiptLifecycleCarriesAuditEvidenceToRejectedReceipt(t *testing.T) {
	definitions := receiptDefinitions()
	evidence := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-static-rejected-audit-1",
		PlanSnapshotDigest: "snapshot-static-rejected-audit-1",
		CatalogGeneration:  10,
	}
	receipts := &receiptRecorder{}
	events := &lifecycleEventRecorder{}
	client, err := NewToolSurfaceReceiptHTTPClientWithLifecycleEvents(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run after a rejected surface")
		return nil, nil
	})}, definitions, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), evidence, receipts, events)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]interface{}{"tools": definitions[:1]})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("reduced wire surface unexpectedly passed")
	}
	if len(receipts.receipts) != 1 || receipts.receipts[0].Verified {
		t.Fatalf("receipts=%+v", receipts.receipts)
	}
	if len(events.events) != 1 || events.events[0].Kind != ToolSurfaceEventManifestCreated {
		t.Fatalf("events=%+v", events.events)
	}
	if receipt := receipts.receipts[0]; receipt.AuditDigest != events.events[0].AuditDigest {
		t.Fatalf("rejected receipt audit digest diverged: receipt=%+v manifest=%+v", receipt, events.events[0])
	}
}

func TestToolSurfaceReceiptLifecycleRejectedReceiptRetainsManifestProjection(t *testing.T) {
	definitions := receiptDefinitions()
	evidence := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-static-rejected-projection-1",
		PlanSnapshotDigest: "snapshot-static-rejected-projection-1",
		CatalogGeneration:  10,
	}
	receipts := &receiptRecorder{}
	events := &lifecycleEventRecorder{}
	client, err := NewToolSurfaceReceiptHTTPClientWithLifecycleEvents(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not run after a rejected surface")
		return nil, nil
	})}, definitions, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), evidence, receipts, events)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]interface{}{"tools": definitions[:1]})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.test/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(request); err == nil {
		t.Fatal("reduced wire surface unexpectedly passed")
	}
	if len(events.events) != 1 || events.events[0].Kind != ToolSurfaceEventManifestCreated || len(receipts.receipts) != 1 {
		t.Fatalf("events=%+v receipts=%+v", events.events, receipts.receipts)
	}
	manifest, receipt := events.events[0], receipts.receipts[0]
	if receipt.PayloadDigest != manifest.PayloadDigest || receipt.AuditDigest != manifest.AuditDigest || receipt.ExpectedToolCount != manifest.ExpectedToolCount || receipt.ReplacementMode != manifest.ReplacementMode || receipt.WireToolCount != 1 {
		t.Fatalf("rejected receipt lost manifest projection: receipt=%+v manifest=%+v", receipt, manifest)
	}
}

func TestToolSurfaceRequestPayloadFailureCarriesPlanAuditEvidence(t *testing.T) {
	definitions := receiptDefinitions()
	evidence := ToolSurfacePlanEvidence{
		Available:          true,
		PlanID:             "plan-ws-rejected-audit-1",
		PlanSnapshotDigest: "snapshot-ws-rejected-audit-1",
		CatalogGeneration:  11,
		Omitted:            []ToolSurfaceOmission{{NeedID: "network", ReasonCode: "policy_denied"}},
	}
	policy := DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeResponses)
	_, manifest, _, err := newToolSurfaceLifecycleManifest(definitions, policy, evidence)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := VerifyToolSurfaceRequestPayloadWithAuditEvidence(definitions, map[string]interface{}{"tools": "not-an-array"}, policy, evidence)
	if err == nil || receipt.Verified {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	if receipt.AuditDigest == "" || receipt.AuditDigest != manifest.AuditDigest {
		t.Fatalf("failed request audit digest diverged: receipt=%+v manifest=%+v", receipt, manifest)
	}
	if receipt.PayloadDigest != manifest.PayloadDigest || receipt.ExpectedToolCount != manifest.ExpectedToolCount || receipt.ReplacementMode != manifest.ReplacementMode {
		t.Fatalf("failed request lost manifest projection: receipt=%+v manifest=%+v", receipt, manifest)
	}
}

func TestToolSurfaceReceiptRejectsInventedUnavailablePlanEvidence(t *testing.T) {
	definitions := receiptDefinitions()
	_, err := VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), ToolSurfacePlanEvidence{
		PlanID: "static-inventory-must-not-become-a-plan",
	})
	if err == nil || !strings.Contains(err.Error(), "unavailable plan evidence") {
		t.Fatalf("error=%v, want unavailable-plan-evidence rejection", err)
	}
	receipt, err := VerifyToolSurfaceWirePayloadWithAuditEvidence(definitions, definitions, DefaultToolSurfaceInvocationPolicy(ToolSurfaceEnvelopeOpenAIChat), ToolSurfacePlanEvidence{})
	if err != nil || !receipt.Verified || receipt.AuditDigest == "" {
		t.Fatalf("static unavailable evidence receipt=%+v err=%v", receipt, err)
	}
}
