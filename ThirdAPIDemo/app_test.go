package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestNormalizeConfigRejectsNonHTTPBaseURL(t *testing.T) {
	_, err := normalizeConfig(GatewayConfig{
		BaseURL:        "ftp://example.test/gateway",
		APIKey:         "token",
		ClientID:       "client-a",
		ConversationID: "room-a",
		UserID:         "user-a",
	})
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected http scheme rejection, got %v", err)
	}
}

func TestConnectAdvertisesSharedCapabilities(t *testing.T) {
	var got HandshakeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode handshake: %v", err)
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{
			RequestID: "gw_test",
			ChannelID: "thirdparty:" + got.ClientID,
		}))
	}))
	defer server.Close()

	result, err := NewApp().Connect(ConnectInput{GatewayConfig: GatewayConfig{
		BaseURL:  server.URL + "/api/im-gateway/v1",
		APIKey:   "token",
		ClientID: "client-a",
	}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if result.Cursor != "0" {
		t.Fatalf("cursor = %q, want 0", result.Cursor)
	}
	want := coreim.ThirdPartyCapabilityMap()
	for key := range want {
		if got.Capabilities[key] != true {
			t.Fatalf("capability %q missing from handshake: %#v", key, got.Capabilities)
		}
	}
	if len(got.Tools) == 0 {
		t.Fatalf("expected demo tools in handshake")
	}
}

func TestGatewayErrorEnvelopeIsReadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayErrorResponse("gw_test", "bad_request", "clientId is required"))
	}))
	defer server.Close()

	var out map[string]any
	err := doGatewayJSON(context.Background(), http.MethodPost, server.URL, "token", map[string]string{"clientId": ""}, &out)
	if err == nil {
		t.Fatalf("expected gateway error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "[bad_request] clientId is required") || !strings.Contains(msg, "requestId=gw_test") {
		t.Fatalf("unexpected error message: %s", msg)
	}
	if strings.Contains(msg, `"error"`) {
		t.Fatalf("error should be formatted, not raw JSON: %s", msg)
	}
}

func TestUploadFileToURLRejectsNonHTTPURL(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "upload-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := tmp.WriteString("body"); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp: %v", err)
	}

	err = uploadFileToURL(context.Background(), "http://gateway.example/api/im-gateway/v1", "ftp://example.test/out.txt", "token", tmp.Name(), "text/plain")
	if err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected upload URL scheme rejection, got %v", err)
	}
}

func TestGatewayMediaURLValidation(t *testing.T) {
	base := "http://gateway.example/api/im-gateway/v1"
	download := "http://gateway.example/api/im-gateway/v1/media/media-1?mediaToken=token"
	upload := "http://gateway.example/api/im-gateway/v1/media/media-1/upload?mediaToken=token"
	if !isGatewayMediaDownloadURL(base, download) {
		t.Fatalf("expected gateway media download URL")
	}
	if !isGatewayMediaUploadURL(base, upload) {
		t.Fatalf("expected gateway media upload URL")
	}
	for _, raw := range []string{
		"https://gateway.example/api/im-gateway/v1/media/media-1?mediaToken=token",
		"http://other.example/api/im-gateway/v1/media/media-1?mediaToken=token",
		"http://gateway.example/api/im-gateway/v1/media/media-1",
		"http://gateway.example/api/im-gateway/v1/not-media/media-1?mediaToken=token",
	} {
		if isGatewayMediaDownloadURL(base, raw) || isGatewayMediaUploadURL(base, raw) {
			t.Fatalf("expected non-gateway media URL to be rejected: %s", raw)
		}
	}
}

func TestNormalizeMediaAttachmentsKeepsIDOnlyServerMedia(t *testing.T) {
	items := normalizeMediaAttachments([]MediaAttachment{{ID: " media-123 ", Type: "image"}})
	if len(items) != 1 {
		t.Fatalf("expected id-only media reference to be kept, got %#v", items)
	}
	if items[0].ID != "media-123" || items[0].Type != "image" {
		t.Fatalf("unexpected normalized media reference: %#v", items[0])
	}
}

func TestNormalizeMediaAttachmentsDropsFileNameOnly(t *testing.T) {
	items := normalizeMediaAttachments([]MediaAttachment{{FileName: "missing.pdf", Type: "file"}})
	if len(items) != 0 {
		t.Fatalf("fileName-only media reference should be dropped, got %#v", items)
	}
}

func TestExecuteDemoToolCallAllowlist(t *testing.T) {
	result := executeDemoToolCall(ToolCall{
		ID:        "call-1",
		Name:      "demo.echo",
		Arguments: map[string]any{"text": "hello"},
	})
	if result.Status != "success" || result.ToolCallID != "call-1" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got := result.Result["arguments"].(map[string]any)["text"]; got != "hello" {
		t.Fatalf("echo text = %#v", got)
	}
}

func TestDemoToolResultIDIsStable(t *testing.T) {
	result := ToolResultRequest{ClientID: "client-a", ConversationID: "room-a", ToolCallID: "call-1", Status: "success"}
	first := demoToolResultID(result)
	second := demoToolResultID(result)
	if first == "" || first != second {
		t.Fatalf("result id should be stable, got %q and %q", first, second)
	}
	otherRoom := demoToolResultID(ToolResultRequest{ClientID: "client-a", ConversationID: "room-b", ToolCallID: "call-1", Status: "success"})
	if first == otherRoom {
		t.Fatalf("result id should include conversation scope, got %q for both rooms", first)
	}
}

func TestExecuteDemoToolPlanRejectsUnsupportedMode(t *testing.T) {
	results := executeDemoToolPlan(ToolPlan{ID: "plan-1", Mode: "parallel", Steps: []ToolPlanStep{{ID: "step-1", Tool: "demo.echo"}}})
	if len(results) != 1 || results[0].Status != "rejected" || results[0].Error == nil || results[0].Error.Code != "unsupported_plan_mode" {
		t.Fatalf("unexpected results: %#v", results)
	}
}

func TestExecuteToolMessageRejectsInvalidToolMessage(t *testing.T) {
	_, err := NewApp().ExecuteToolMessage(ToolExecuteInput{
		GatewayConfig: GatewayConfig{BaseURL: "http://127.0.0.1:18777/api/im-gateway/v1", APIKey: "token"},
		Message: OutgoingMessage{
			ID:   "msg-1",
			Type: "tool_plan",
			ToolPlan: &ToolPlan{
				ID:    "plan-1",
				Mode:  "surprise",
				Steps: []ToolPlanStep{{ID: "step-1", Tool: "demo.echo"}},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "toolPlan.mode") {
		t.Fatalf("expected toolPlan.mode error, got %v", err)
	}
}

func TestExecuteDemoToolPlanBlocksFailedDependency(t *testing.T) {
	results := executeDemoToolPlan(ToolPlan{
		ID:   "plan-1",
		Mode: "dag",
		Steps: []ToolPlanStep{
			{ID: "step-1", Tool: "plc.read_register"},
			{ID: "step-2", Tool: "demo.echo", DependsOn: []string{"step-1"}},
		},
	})
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2: %#v", len(results), results)
	}
	if results[0].Status != "error" || results[1].Status != "rejected" {
		t.Fatalf("unexpected dependency handling: %#v", results)
	}
}

func TestExecuteDemoToolPlanDAGRunsReadyDependenciesFirst(t *testing.T) {
	results := executeDemoToolPlan(ToolPlan{
		ID:   "plan-1",
		Mode: "dag",
		Steps: []ToolPlanStep{
			{ID: "step-2", Tool: "demo.echo", DependsOn: []string{"step-1"}},
			{ID: "step-1", Tool: "demo.echo", Arguments: map[string]any{"text": "first"}},
		},
	})
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2: %#v", len(results), results)
	}
	if results[0].StepID != "step-1" || results[0].Status != "success" || results[1].StepID != "step-2" || results[1].Status != "success" {
		t.Fatalf("DAG should execute dependency before dependent: %#v", results)
	}
}

func TestExecuteDemoToolPlanDAGKeepsPlanOrderForIndependentSteps(t *testing.T) {
	results := executeDemoToolPlan(ToolPlan{
		ID:   "plan-1",
		Mode: "dag",
		Steps: []ToolPlanStep{
			{ID: "step-b", Tool: "demo.echo", Arguments: map[string]any{"text": "b"}},
			{ID: "step-a", Tool: "demo.echo", Arguments: map[string]any{"text": "a"}},
		},
	})
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2: %#v", len(results), results)
	}
	if results[0].StepID != "step-b" || results[1].StepID != "step-a" {
		t.Fatalf("independent DAG steps should keep plan order: %#v", results)
	}
}

func TestExecuteDemoToolCallRejectsUnknownTool(t *testing.T) {
	result := executeDemoToolCall(ToolCall{ID: "call-2", Name: "plc.write_register"})
	if result.Status != "rejected" || result.Error == nil || result.Error.Code != "tool_not_allowed" {
		t.Fatalf("unexpected rejection result: %#v", result)
	}
}
