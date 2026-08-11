package im

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func urlValues(pairs ...string) url.Values {
	values := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		values.Set(pairs[i], pairs[i+1])
	}
	return values
}

func TestNormalizeThirdPartyIncomingRequestSupportsMedia(t *testing.T) {
	req := ThirdPartyIncomingRequest{
		ClientID:       " client-a ",
		EventID:        " evt-1 ",
		MessageID:      " msg-1 ",
		ConversationID: "",
		User:           ThirdPartyUserRef{ID: " user-1 ", Name: " Alice "},
		Message: ThirdPartyMessagePayload{
			Type:        "audio",
			Text:        " voice note ",
			FileName:    "note.ogg",
			ContentType: "audio/ogg",
			URL:         " http://gateway.example/api/im-gateway/v1/media/voice-media-id?mediaToken=token ",
			DurationMs:  1200,
		},
	}

	if err := NormalizeThirdPartyIncomingRequest(&req, ThirdPartyNormalizeOptions{
		RequireMessageID:      true,
		RequireUserID:         true,
		DefaultConversationID: "default-room",
	}); err != nil {
		t.Fatalf("NormalizeThirdPartyIncomingRequest() error = %v", err)
	}
	if req.Message.Type != "voice" {
		t.Fatalf("message type = %q, want voice", req.Message.Type)
	}
	if req.ConversationID != "default-room" {
		t.Fatalf("conversationID = %q, want default-room", req.ConversationID)
	}
	if len(req.Message.Attachments) != 1 {
		t.Fatalf("attachments len = %d, want 1", len(req.Message.Attachments))
	}
	att := req.Message.Attachments[0]
	if att.Type != "voice" || att.MimeType != "audio/ogg" || att.URL != "http://gateway.example/api/im-gateway/v1/media/voice-media-id?mediaToken=token" || att.DurationMs != 1200 {
		t.Fatalf("unexpected attachment: %#v", att)
	}
	content := ThirdPartyIncomingContent(req)
	for _, want := range []string{"voice note", "[Third-party voice message]", "fileName=note.ogg", "mimeType=audio/ogg", "durationMs=1200"} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q: %q", want, content)
		}
	}
}

func TestThirdPartyGatewayHandshakeResponseUsesSharedDefaults(t *testing.T) {
	resp := NewThirdPartyGatewayHandshakeResponse(ThirdPartyGatewayConfig{
		RequestID:  "req-1",
		ChannelID:  "thirdparty:client-a",
		ServerTime: 123,
	})
	if resp.ProtocolVersion != ThirdPartyProtocolVersion {
		t.Fatalf("protocol version = %q, want %q", resp.ProtocolVersion, ThirdPartyProtocolVersion)
	}
	if resp.Mode != ThirdPartyGatewayMode {
		t.Fatalf("mode = %q, want %q", resp.Mode, ThirdPartyGatewayMode)
	}
	if resp.Poll["recommendedTimeoutSec"] != ThirdPartyPollTimeoutSec || resp.PollTimeoutSec != ThirdPartyPollTimeoutSec {
		t.Fatalf("unexpected poll settings: %#v / %d", resp.Poll, resp.PollTimeoutSec)
	}
	if resp.Poll["maxLimit"] != ThirdPartyMaxPollLimit || resp.MaxBatchSize != ThirdPartyMaxBatchSize {
		t.Fatalf("unexpected batch settings: %#v / %d", resp.Poll, resp.MaxBatchSize)
	}
	if resp.Limits["maxDirectBytes"] != ThirdPartyMaxDirectBytes || resp.Limits["maxBodyBytes"] != ThirdPartyMaxBodyBytes || resp.Limits["maxMediaBytes"] != ThirdPartyMaxMediaBytes {
		t.Fatalf("unexpected limits: %#v", resp.Limits)
	}
	for _, key := range []string{"serverMedia", "server_media_upload", "server_media_download", "client_tools", "tool_call", "tool_plan", "tool_result", "tool_cancel"} {
		if !resp.Features[key] {
			t.Fatalf("feature %q missing from %#v", key, resp.Features)
		}
	}
	if resp.Delivery["guarantee"] != "at_least_once_by_cursor" {
		t.Fatalf("unexpected delivery: %#v", resp.Delivery)
	}
}

func TestThirdPartyGatewayErrorResponseShape(t *testing.T) {
	resp := NewThirdPartyGatewayErrorResponse("req-1", " bad_request ", " invalid ")
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if raw["ok"] != false || raw["requestId"] != "req-1" {
		t.Fatalf("unexpected error response envelope: %#v", raw)
	}
	if _, ok := raw["code"]; ok {
		t.Fatalf("error response should not expose top-level code: %#v", raw)
	}
	if _, ok := raw["message"]; ok {
		t.Fatalf("error response should not expose top-level message: %#v", raw)
	}
	errObj, ok := raw["error"].(map[string]any)
	if !ok || errObj["code"] != "bad_request" || errObj["message"] != "invalid" {
		t.Fatalf("unexpected nested error: %#v", raw["error"])
	}
}

func TestThirdPartyGatewayHealthResponseShape(t *testing.T) {
	resp := NewThirdPartyGatewayHealthResponse("gw_1", 1781028000000)
	if !resp.OK || resp.RequestID != "gw_1" || resp.Status != "connected" || resp.ProtocolVersion != ThirdPartyProtocolVersion || resp.ServerTime != 1781028000000 {
		t.Fatalf("unexpected health response: %#v", resp)
	}
}

func TestThirdPartyGatewaySuccessResponseShapes(t *testing.T) {
	if resp := NewThirdPartyGatewayOKResponse("gw_1"); !resp.OK || resp.RequestID != "gw_1" {
		t.Fatalf("unexpected ok response: %#v", resp)
	}
	if resp := NewThirdPartyMediaUploadCompleteResponse("gw_2", "media-1"); !resp.OK || resp.MediaID != "media-1" {
		t.Fatalf("unexpected media upload response: %#v", resp)
	}
	accepted := NewThirdPartyIncomingAcceptedResponse("gw_3", "mc-1", true)
	if !accepted.OK || !accepted.Accepted || !accepted.Duplicate || accepted.MaclawMessageID != "mc-1" {
		t.Fatalf("unexpected accepted response: %#v", accepted)
	}
	poll := NewThirdPartyOutgoingPollResponse("gw_4", nil, 12, false)
	if !poll.OK || poll.NextCursor != "12" || poll.HasMore || poll.Messages == nil || len(poll.Messages) != 0 {
		t.Fatalf("unexpected empty poll response: %#v", poll)
	}
}

func TestThirdPartyCapabilityMapIncludesListAndAliases(t *testing.T) {
	capabilities := ThirdPartyCapabilityMap()
	for _, capability := range ThirdPartyCapabilities() {
		if capabilities[capability] != true {
			t.Fatalf("capability %q missing from map %#v", capability, capabilities)
		}
	}
	for _, alias := range []string{"serverMedia", "longPolling"} {
		if capabilities[alias] != true {
			t.Fatalf("alias %q missing from map %#v", alias, capabilities)
		}
	}
}

func TestNormalizeThirdPartyHandshakeRejectsUnsupportedProtocolVersion(t *testing.T) {
	req := ThirdPartyHandshakeRequest{ClientID: "client-a", ProtocolVersion: "2"}
	if err := NormalizeThirdPartyHandshakeRequest(&req); err == nil || !strings.Contains(err.Error(), "protocolVersion") {
		t.Fatalf("expected protocolVersion error, got %v", err)
	}
}

func TestNormalizeThirdPartyHandshakeAcceptsLegacyAndStructuredClientCapabilities(t *testing.T) {
	legacy := ThirdPartyHandshakeRequest{ClientID: "client-a", ProtocolVersion: ThirdPartyProtocolLegacyVersion}
	if err := NormalizeThirdPartyHandshakeRequest(&legacy); err != nil {
		t.Fatalf("legacy protocol rejected: %v", err)
	}
	req := ThirdPartyHandshakeRequest{ClientID: "client-a", ProtocolVersion: ThirdPartyProtocolVersion, Capabilities: map[string]any{
		"output": map[string]any{
			"modalities": []any{"text", "image", "bad"},
			"text":       map[string]any{"maxChars": float64(240), "locale": "zh-CN"},
		},
	}}
	if err := NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.ClientCapabilities == nil || !req.ClientCapabilities.SupportsOutput("text") || !req.ClientCapabilities.SupportsOutput("image") || req.ClientCapabilities.Output.Text.MaxChars != 240 {
		t.Fatalf("normalized capabilities=%#v", req.ClientCapabilities)
	}
	response := NewThirdPartyGatewayHandshakeResponse(ThirdPartyGatewayConfig{ClientCapabilities: &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{Modalities: []string{"text"}}}})
	if response.CapabilitiesAccepted == nil || !response.CapabilitiesAccepted.SupportsOutput("text") {
		t.Fatalf("accepted capabilities=%#v", response.CapabilitiesAccepted)
	}
}

func TestNormalizeThirdPartyHandshakeAcceptsFeatureOnlyClientCapabilities(t *testing.T) {
	req := ThirdPartyHandshakeRequest{ClientID: "client-a", ProtocolVersion: ThirdPartyProtocolVersion, Capabilities: map[string]any{
		"features": map[string]any{"volumeControl": true, "ambientDisplay": true},
	}}
	if err := NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.ClientCapabilities == nil || !req.ClientCapabilities.Features.VolumeControl || !req.ClientCapabilities.Features.AmbientDisplay {
		t.Fatalf("normalized feature-only capabilities=%#v", req.ClientCapabilities)
	}
}

func TestDecodeThirdPartyGatewayJSONRejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/im-gateway/v1/handshake", strings.NewReader(`{"clientId":"client-a","extra":true}`))
	var hs ThirdPartyHandshakeRequest
	if err := DecodeThirdPartyGatewayJSON(httptest.NewRecorder(), req, &hs, 0); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestDecodeThirdPartyGatewayJSONRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/im-gateway/v1/handshake", strings.NewReader(`{"clientId":"client-a"} {}`))
	var hs ThirdPartyHandshakeRequest
	if err := DecodeThirdPartyGatewayJSON(httptest.NewRecorder(), req, &hs, 0); err == nil || !strings.Contains(err.Error(), "single JSON value") {
		t.Fatalf("expected trailing JSON error, got %v", err)
	}
}

func TestDecodeThirdPartyGatewayJSONRejectsOversizedBodyWithNilWriter(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/im-gateway/v1/handshake", strings.NewReader(`{"clientId":"client-a"}`))
	var hs ThirdPartyHandshakeRequest
	if err := DecodeThirdPartyGatewayJSON(nil, req, &hs, 8); err == nil {
		t.Fatalf("expected body size error")
	}
}

func TestNormalizeThirdPartyAckRequest(t *testing.T) {
	req := ThirdPartyAckRequest{
		ClientID:   " client-a ",
		MessageIDs: []string{" msg-1 ", "", "msg-2"},
		Status:     "ok",
	}
	if err := NormalizeThirdPartyAckRequest(&req, 10); err != nil {
		t.Fatalf("NormalizeThirdPartyAckRequest: %v", err)
	}
	if req.ClientID != "client-a" || req.Status != "delivered" || len(req.MessageIDs) != 2 || req.MessageIDs[0] != "msg-1" {
		t.Fatalf("unexpected normalized ack: %#v", req)
	}

	req = ThirdPartyAckRequest{ClientID: "client-a", MessageIDs: make([]string, 11)}
	if err := NormalizeThirdPartyAckRequest(&req, 10); err == nil || !strings.Contains(err.Error(), "messageIds exceeds") {
		t.Fatalf("expected max ack ids error, got %v", err)
	}
}

func TestParseThirdPartyPollQuery(t *testing.T) {
	cfg := ThirdPartyGatewayConfig{PollTimeoutSec: 30, MaxTimeoutSec: 60, MaxBatchSize: 20, MaxPollLimit: 100}
	req, err := ParseThirdPartyPollQuery(urlValues("clientId", " client-a ", "cursor", "5", "limit", "250"), cfg)
	if err != nil {
		t.Fatalf("ParseThirdPartyPollQuery: %v", err)
	}
	if req.ClientID != "client-a" || req.Cursor != 5 || req.Limit != 100 || req.TimeoutSec != 30 {
		t.Fatalf("unexpected poll request: %#v", req)
	}

	req, err = ParseThirdPartyPollQuery(urlValues("clientId", "client-a", "timeout", "0"), cfg)
	if err != nil {
		t.Fatalf("ParseThirdPartyPollQuery timeout=0: %v", err)
	}
	if req.TimeoutSec != 0 || !req.TimeoutSet || req.Limit != 20 {
		t.Fatalf("timeout=0 should mean immediate poll: %#v", req)
	}

	if _, err := ParseThirdPartyPollQuery(urlValues("clientId", "client-a", "cursor", "-1"), cfg); err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("expected cursor error, got %v", err)
	}
	if _, err := ParseThirdPartyPollQuery(urlValues("clientId", "client-a", "limit", "bad"), cfg); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestThirdPartyServerMediaRequestFromURL(t *testing.T) {
	if _, _, err := ThirdPartyServerMediaRequestFromURL("ftp://example.test/secret.txt"); err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
	if _, _, err := ThirdPartyServerMediaRequestFromURL("https://third-party.example.test/file.pdf"); err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected external URL rejection, got %v", err)
	}
	id, req, err := ThirdPartyServerMediaRequestFromURL("http://127.0.0.1:18777/api/im-gateway/v1/media/media-123?mediaToken=token")
	if err != nil {
		t.Fatalf("ThirdPartyServerMediaRequestFromURL: %v", err)
	}
	if id != "media-123" || req.Method != http.MethodGet || req.URL.Query().Get("mediaToken") != "token" {
		t.Fatalf("unexpected server media request: id=%q method=%s url=%s", id, req.Method, req.URL.String())
	}
}

func TestThirdPartyMediaTokenAndSafeFileName(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/media/id?mediaToken=secret", nil)
	if !ThirdPartyMediaTokenOK(req, "secret") || ThirdPartyMediaTokenOK(req, "other") {
		t.Fatalf("query media token check failed")
	}
	req = httptest.NewRequest(http.MethodGet, "/media/id", nil)
	req.Header.Set("X-Media-Token", "header-secret")
	if !ThirdPartyMediaTokenOK(req, "header-secret") || ThirdPartyMediaTokenOK(req, "") {
		t.Fatalf("header media token check failed")
	}
	if got := SafeThirdPartyFileName(`..\bad:name?.txt`); got != "bad_name_.txt" {
		t.Fatalf("SafeThirdPartyFileName = %q", got)
	}
	if got := SafeThirdPartyFileName(""); got != "file" {
		t.Fatalf("empty SafeThirdPartyFileName = %q", got)
	}
}

func TestNormalizeThirdPartyIncomingRequestCopiesTopLevelMediaID(t *testing.T) {
	req := ThirdPartyIncomingRequest{
		ClientID: "client-a",
		EventID:  "evt-media-id",
		Message: ThirdPartyMessagePayload{
			ID:       "media-server-id",
			Type:     "file",
			FileName: "report.pdf",
			URL:      "http://gateway.example/api/im-gateway/v1/media/media-server-id?mediaToken=token",
		},
	}
	if err := NormalizeThirdPartyIncomingRequest(&req, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err != nil {
		t.Fatalf("NormalizeThirdPartyIncomingRequest: %v", err)
	}
	if len(req.Message.Attachments) != 1 || req.Message.Attachments[0].ID != "media-server-id" {
		t.Fatalf("attachment ID not copied: %#v", req.Message.Attachments)
	}
}

func TestNormalizeThirdPartyIncomingRequestAllowsServerMediaIDOnly(t *testing.T) {
	req := ThirdPartyIncomingRequest{
		ClientID: "client-a",
		EventID:  "evt-media-id-only",
		Message: ThirdPartyMessagePayload{
			ID:   "media-server-id",
			Type: "file",
		},
	}
	if err := NormalizeThirdPartyIncomingRequest(&req, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err != nil {
		t.Fatalf("NormalizeThirdPartyIncomingRequest: %v", err)
	}
	if len(req.Message.Attachments) != 1 || req.Message.Attachments[0].ID != "media-server-id" {
		t.Fatalf("attachment ID not preserved: %#v", req.Message.Attachments)
	}
}

func TestNormalizeThirdPartyIncomingRequestRejectsInvalidPayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ThirdPartyIncomingRequest
	}{
		{
			name: "empty text",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "text"},
			},
		},
		{
			name: "unsupported type",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "video", URL: "https://example.test/v.mp4"},
			},
		},
		{
			name: "missing media reference",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "image"},
			},
		},
		{
			name: "file name only",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "file", FileName: "missing.pdf"},
			},
		},
		{
			name: "negative size",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "file", FileName: "bad.bin", SizeBytes: -1},
			},
		},
		{
			name: "too many attachments",
			req: ThirdPartyIncomingRequest{
				ClientID: "client-a", EventID: "evt-1",
				Message: ThirdPartyMessagePayload{Type: "file", Attachments: make([]ThirdPartyMediaReference, ThirdPartyMaxAttachments+1)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "too many attachments" {
				for i := range tc.req.Message.Attachments {
					tc.req.Message.Attachments[i] = ThirdPartyMediaReference{Type: "file", FileName: "file.txt"}
				}
			}
			if err := NormalizeThirdPartyIncomingRequest(&tc.req, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestNormalizeThirdPartyIncomingRequestRejectsOversizeDirectData(t *testing.T) {
	req := ThirdPartyIncomingRequest{
		ClientID: "client-a",
		EventID:  "evt-oversize",
		Message: ThirdPartyMessagePayload{
			Type: "file",
			Attachments: []ThirdPartyMediaReference{{
				Type:     "file",
				FileName: "big.bin",
				Data:     base64.StdEncoding.EncodeToString(make([]byte, ThirdPartyMaxDirectBytes+1)),
			}},
		},
	}
	if err := NormalizeThirdPartyIncomingRequest(&req, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err == nil || !strings.Contains(err.Error(), "use server media upload URL") {
		t.Fatalf("expected oversize direct data error, got %v", err)
	}
}

func TestNormalizeThirdPartyIncomingRequestRejectsDirectDataSizeMismatch(t *testing.T) {
	req := ThirdPartyIncomingRequest{
		ClientID: "client-a",
		EventID:  "evt-size-mismatch",
		Message: ThirdPartyMessagePayload{
			Type: "file",
			Attachments: []ThirdPartyMediaReference{{
				Type:      "file",
				FileName:  "note.txt",
				Data:      base64.StdEncoding.EncodeToString([]byte("hello")),
				SizeBytes: 6,
			}},
		},
	}
	if err := NormalizeThirdPartyIncomingRequest(&req, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err == nil || !strings.Contains(err.Error(), "sizeBytes mismatch") {
		t.Fatalf("expected direct data size mismatch error, got %v", err)
	}
}

func TestNormalizeThirdPartyMediaPrepareRequest(t *testing.T) {
	req := ThirdPartyMediaPrepareRequest{
		ClientID:   " client-a ",
		Type:       "audio",
		FileName:   " report.pdf ",
		MimeType:   " application/pdf ",
		SizeBytes:  10,
		DurationMs: 1000,
	}
	if err := NormalizeThirdPartyMediaPrepareRequest(&req, 20); err != nil {
		t.Fatalf("NormalizeThirdPartyMediaPrepareRequest: %v", err)
	}
	if req.ClientID != "client-a" || req.Type != "voice" || req.FileName != "report.pdf" || req.MimeType != "application/pdf" {
		t.Fatalf("unexpected normalized prepare request: %#v", req)
	}

	req = ThirdPartyMediaPrepareRequest{ClientID: "client-a", SizeBytes: 21}
	if err := NormalizeThirdPartyMediaPrepareRequest(&req, 20); err == nil || !strings.Contains(err.Error(), "sizeBytes exceeds") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	req = ThirdPartyMediaPrepareRequest{ClientID: "client-a", DurationMs: -1}
	if err := NormalizeThirdPartyMediaPrepareRequest(&req, 20); err == nil || !strings.Contains(err.Error(), "durationMs") {
		t.Fatalf("expected duration error, got %v", err)
	}
}

func TestThirdPartyMediaMaxBytesForDocuments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fileName string
		mimeType string
		want     int64
	}{
		{name: "pdf", fileName: "report.pdf", want: agent.MaxOfficeReadFileBytes},
		{name: "docx mime", fileName: "image.png", mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", want: agent.MaxOfficeReadFileBytes},
		{name: "xlsx", fileName: "book.xlsx", want: agent.MaxOfficeReadFileBytes},
		{name: "ordinary media", fileName: "photo.png", mimeType: "image/png", want: ThirdPartyMaxMediaBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ThirdPartyMediaMaxBytesFor(tc.fileName, tc.mimeType); got != tc.want {
				t.Fatalf("ThirdPartyMediaMaxBytesFor(%q, %q) = %d, want %d", tc.fileName, tc.mimeType, got, tc.want)
			}
		})
	}

	req := ThirdPartyMediaPrepareRequest{ClientID: "client-a", FileName: "report.pptx", SizeBytes: agent.MaxOfficeReadFileBytes + 1}
	if err := NormalizeThirdPartyMediaPrepareRequest(&req, ThirdPartyMaxMediaBytes); err == nil || !strings.Contains(err.Error(), "sizeBytes exceeds") {
		t.Fatalf("expected Office document limit error, got %v", err)
	}

	incoming := ThirdPartyIncomingRequest{ClientID: "client-a", EventID: "evt-office-size", Message: ThirdPartyMessagePayload{Type: "file", Attachments: []ThirdPartyMediaReference{{ID: "media-a", Type: "file", FileName: "image.png", MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", SizeBytes: agent.MaxOfficeReadFileBytes + 1}}}}
	if err := NormalizeThirdPartyIncomingRequest(&incoming, ThirdPartyNormalizeOptions{DefaultConversationID: "default"}); err == nil || !strings.Contains(err.Error(), "sizeBytes exceeds") {
		t.Fatalf("expected Office reference limit error, got %v", err)
	}
}
func TestThirdPartyToolProtocolValidation(t *testing.T) {
	hs := ThirdPartyHandshakeRequest{
		ClientID: " demo-client ",
		Tools: []ThirdPartyToolDefinition{{
			Name:        " demo.echo ",
			Description: "Echo input",
			InputSchema: map[string]any{"type": "object"},
			Risk:        "low",
		}},
	}
	if err := NormalizeThirdPartyHandshakeRequest(&hs); err != nil {
		t.Fatalf("NormalizeThirdPartyHandshakeRequest: %v", err)
	}
	if hs.ClientID != "demo-client" || hs.Tools[0].Name != "demo.echo" || hs.Tools[0].Risk != "read" {
		t.Fatalf("unexpected normalized handshake: %#v", hs)
	}

	msg := ThirdPartyOutgoingMessage{
		ID:   "tool-message-1",
		Type: "tool_call",
		ToolCall: &ThirdPartyToolCall{
			ID:             " call-1 ",
			Name:           "demo.get_time",
			IdempotencyKey: "call-1",
			Arguments:      map[string]any{"timezone": "Asia/Shanghai"},
		},
	}
	if err := NormalizeThirdPartyOutgoingMessage(&msg); err != nil {
		t.Fatalf("NormalizeThirdPartyOutgoingMessage: %v", err)
	}
	if msg.ToolCall.ID != "call-1" || msg.ToolCall.Risk != "read" {
		t.Fatalf("unexpected normalized tool call: %#v", msg.ToolCall)
	}
}

func TestThirdPartyHandshakeRejectsDuplicateToolNamesAfterNormalization(t *testing.T) {
	hs := ThirdPartyHandshakeRequest{
		ClientID: "demo-client",
		Tools: []ThirdPartyToolDefinition{
			{Name: "demo.echo"},
			{Name: " demo.echo "},
		},
	}
	if err := NormalizeThirdPartyHandshakeRequest(&hs); err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate tool error, got %v", err)
	}
}

func TestThirdPartyToolResultValidationAndContent(t *testing.T) {
	req := ThirdPartyToolResultRequest{
		ClientID:   " demo-client ",
		ResultID:   " result-1 ",
		ToolCallID: " call-1 ",
		Status:     "ok",
		Result:     map[string]any{"echo": "hello"},
	}
	if err := NormalizeThirdPartyToolResultRequest(&req); err != nil {
		t.Fatalf("NormalizeThirdPartyToolResultRequest: %v", err)
	}
	if req.Status != "success" {
		t.Fatalf("status = %q, want success", req.Status)
	}
	if eventID := ThirdPartyToolResultEventID(req); eventID != "tool_result:result-1" {
		t.Fatalf("eventID = %q, want tool_result:result-1", eventID)
	}
	content := ThirdPartyToolResultContent(req)
	for _, want := range []string{"[Client tool result]", "toolCallId=call-1", "status=success", `"echo": "hello"`} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q: %s", want, content)
		}
	}
}

func TestThirdPartyToolResultRejectsUnstableIdempotencyKey(t *testing.T) {
	req := ThirdPartyToolResultRequest{
		ClientID:       "demo-client",
		ToolCallID:     "call-1",
		Status:         "success",
		IdempotencyKey: "***",
	}
	if err := NormalizeThirdPartyToolResultRequest(&req); err == nil || !strings.Contains(err.Error(), "idempotencyKey") {
		t.Fatalf("expected idempotencyKey validation error, got %v", err)
	}
}

func TestThirdPartyToolCancelRejectsAmbiguousCorrelation(t *testing.T) {
	cancel := ThirdPartyToolCancel{ToolCallID: "call-1", ToolPlanID: "plan-1"}
	if err := NormalizeThirdPartyToolCancel(&cancel); err == nil {
		t.Fatal("expected mutually exclusive cancellation targets to fail")
	}
	cancel = ThirdPartyToolCancel{ToolCallID: "call-1", StepID: "step-1"}
	if err := NormalizeThirdPartyToolCancel(&cancel); err == nil {
		t.Fatal("expected stepId without toolPlanId to fail")
	}
}

func TestThirdPartyToolResultRejectsAmbiguousCorrelationAndStatusPayload(t *testing.T) {
	tests := []struct {
		name string
		req  ThirdPartyToolResultRequest
		want string
	}{
		{
			name: "call and plan",
			req:  ThirdPartyToolResultRequest{ClientID: "demo", ToolCallID: "call-1", ToolPlanID: "plan-1", Status: "success"},
			want: "mutually exclusive",
		},
		{
			name: "orphan step",
			req:  ThirdPartyToolResultRequest{ClientID: "demo", ToolCallID: "call-1", StepID: "step-1", Status: "success"},
			want: "stepId requires",
		},
		{
			name: "plan without step",
			req:  ThirdPartyToolResultRequest{ClientID: "demo", ToolPlanID: "plan-1", Status: "success"},
			want: "stepId is required",
		},
		{
			name: "success with error",
			req:  ThirdPartyToolResultRequest{ClientID: "demo", ToolCallID: "call-1", Status: "success", Error: &ThirdPartyToolError{Message: "bad"}},
			want: "must be omitted",
		},
		{
			name: "empty failure",
			req:  ThirdPartyToolResultRequest{ClientID: "demo", ToolCallID: "call-1", Status: "error"},
			want: "error or text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := NormalizeThirdPartyToolResultRequest(&tt.req); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestThirdPartyToolCallRejectsUnstableIdempotencyKey(t *testing.T) {
	msg := ThirdPartyOutgoingMessage{
		ID:   "tool-message-1",
		Type: "tool_call",
		ToolCall: &ThirdPartyToolCall{
			ID:             "call-1",
			Name:           "demo.echo",
			IdempotencyKey: "***",
		},
	}
	if err := NormalizeThirdPartyOutgoingMessage(&msg); err == nil || !strings.Contains(err.Error(), "idempotencyKey") {
		t.Fatalf("expected idempotencyKey validation error, got %v", err)
	}
}

func TestThirdPartyToolPlanRejectsUnknownDependency(t *testing.T) {
	plan := ThirdPartyToolPlan{
		ID:   "plan-1",
		Mode: "dag",
		Steps: []ThirdPartyToolPlanStep{{
			ID:             "step-1",
			Tool:           "demo.echo",
			IdempotencyKey: "plan-1:step-1",
			DependsOn:      []string{"missing-step"},
		}},
	}
	if err := NormalizeThirdPartyToolPlan(&plan); err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("expected unknown dependency error, got %v", err)
	}
}

func TestThirdPartyToolPlanRejectsDependencyCycle(t *testing.T) {
	plan := ThirdPartyToolPlan{
		ID:   "plan-1",
		Mode: "dag",
		Steps: []ThirdPartyToolPlanStep{
			{ID: "step-1", Tool: "demo.echo", IdempotencyKey: "plan-1:step-1", DependsOn: []string{"step-2"}},
			{ID: "step-2", Tool: "demo.echo", IdempotencyKey: "plan-1:step-2", DependsOn: []string{"step-1"}},
		},
	}
	if err := NormalizeThirdPartyToolPlan(&plan); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected dependency cycle error, got %v", err)
	}
}

func TestThirdPartyToolPlanRejectsUnknownMode(t *testing.T) {
	plan := ThirdPartyToolPlan{
		ID:    "plan-1",
		Mode:  "surprise",
		Steps: []ThirdPartyToolPlanStep{{ID: "step-1", Tool: "demo.echo", IdempotencyKey: "plan-1:step-1"}},
	}
	if err := NormalizeThirdPartyToolPlan(&plan); err == nil || !strings.Contains(err.Error(), "toolPlan.mode") {
		t.Fatalf("expected mode validation error, got %v", err)
	}
}

func TestThirdPartyToolPlanDefaultsEmptyModeToSequential(t *testing.T) {
	plan := ThirdPartyToolPlan{
		ID:    "plan-1",
		Steps: []ThirdPartyToolPlanStep{{ID: "step-1", Tool: "demo.echo", IdempotencyKey: "plan-1:step-1"}},
	}
	if err := NormalizeThirdPartyToolPlan(&plan); err != nil {
		t.Fatalf("NormalizeThirdPartyToolPlan: %v", err)
	}
	if plan.Mode != "sequential" {
		t.Fatalf("mode = %q, want sequential", plan.Mode)
	}
}
