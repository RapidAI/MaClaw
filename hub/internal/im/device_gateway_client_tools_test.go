package im

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestDeviceGatewayForwardsClientToolsAndAcceptsToolResult(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	incoming := make(chan IncomingMessage, 2)
	plugin.messageHandler = func(msg IncomingMessage) { incoming <- msg }
	plugin.mu.Unlock()
	gateway := NewDeviceGateway(plugin)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "552211"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-tools", "code": "552211"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	tools := []agent.ClientToolDefinition{{Name: "alarm_list", InputSchema: map[string]any{"type": "object"}}}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-tools", "tools": tools})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake=%d %s", handshake.Code, handshake.Body.String())
	}
	accepted := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{"clientId": "pet-tools", "eventId": "e1", "conversationId": "default", "message": map[string]any{"type": "text", "text": "list"}})
	if accepted.Code != http.StatusOK {
		t.Fatalf("incoming=%d %s", accepted.Code, accepted.Body.String())
	}
	select {
	case msg := <-incoming:
		if len(msg.ClientTools) != 1 || msg.ClientToolContext == nil || msg.ClientToolContext.ClientID != "pet-tools" {
			t.Fatalf("message=%#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("client tool message not forwarded")
	}
	result := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/tool-result", token, map[string]any{"clientId": "pet-tools", "resultId": "r1", "conversationId": "default", "toolCallId": "c1", "status": "success", "result": map[string]any{"count": 0}})
	if result.Code != http.StatusOK {
		t.Fatalf("tool result=%d %s", result.Code, result.Body.String())
	}
	select {
	case msg := <-incoming:
		if msg.MessageID != "tool_result:r1" {
			t.Fatalf("tool result message id=%q", msg.MessageID)
		}
	case <-time.After(time.Second):
		t.Fatal("tool result not forwarded")
	}
	replay := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/tool-result", token, map[string]any{"clientId": "pet-tools", "resultId": "r1", "conversationId": "default", "toolCallId": "c1", "status": "success", "result": map[string]any{"count": 0}})
	if replay.Code != http.StatusOK {
		t.Fatalf("tool result replay=%d %s", replay.Code, replay.Body.String())
	}
	var replayBody map[string]any
	_ = json.NewDecoder(replay.Body).Decode(&replayBody)
	if replayBody["duplicate"] != true {
		t.Fatalf("tool result replay body=%#v", replayBody)
	}
	select {
	case msg := <-incoming:
		t.Fatalf("duplicate tool result forwarded: %#v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDeviceGatewayBoundsReplayState(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	for i := 0; i < deviceGatewayMaxSeenEvents+25; i++ {
		if gateway.markDeviceEvent("pet-bounded", fmt.Sprintf("event-%d", i)) {
			t.Fatalf("unexpected duplicate at %d", i)
		}
	}
	gateway.mu.Lock()
	state := gateway.clients["pet-bounded"]
	seenLen := len(state.seenEvents)
	orderLen := len(state.seenOrder)
	_, oldestKept := state.seenEvents["event-0"]
	_, newestKept := state.seenEvents[fmt.Sprintf("event-%d", deviceGatewayMaxSeenEvents+24)]
	gateway.mu.Unlock()
	if seenLen != deviceGatewayMaxSeenEvents || orderLen != deviceGatewayMaxSeenEvents || oldestKept || !newestKept {
		t.Fatalf("seen state not bounded: seen=%d order=%d oldest=%v newest=%v", seenLen, orderLen, oldestKept, newestKept)
	}
}

func TestDeviceGatewayRejectedIncomingRemainsRetryable(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	incoming := make(chan IncomingMessage, 1)
	plugin.messageHandler = func(msg IncomingMessage) { incoming <- msg }
	plugin.mu.Unlock()

	gateway := NewDeviceGateway(plugin)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "552212"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-retry", "code": "552212"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)

	const eventID = "retry-after-validation"
	rejected := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{
		"clientId": "pet-retry", "eventId": eventID,
		"message": map[string]any{"type": "text", "text": "bad", "attachments": []map[string]any{{"id": "missing-media", "type": "voice", "mimeType": "audio/wav"}}},
	})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("rejected incoming=%d %s", rejected.Code, rejected.Body.String())
	}

	retry := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{
		"clientId": "pet-retry", "eventId": eventID,
		"message": map[string]any{"type": "text", "text": "corrected"},
	})
	if retry.Code != http.StatusOK {
		t.Fatalf("corrected retry=%d %s", retry.Code, retry.Body.String())
	}
	var retryBody map[string]any
	_ = json.NewDecoder(retry.Body).Decode(&retryBody)
	if retryBody["duplicate"] == true {
		t.Fatalf("corrected retry was suppressed: %#v", retryBody)
	}
	select {
	case msg := <-incoming:
		if msg.Text != "corrected" {
			t.Fatalf("forwarded retry=%#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("corrected retry was not forwarded")
	}
}

func TestDeviceGatewayUnsupportedAttachmentRemainsRetryable(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	incoming := make(chan IncomingMessage, 1)
	plugin.messageHandler = func(msg IncomingMessage) { incoming <- msg }
	plugin.mu.Unlock()

	gateway := NewDeviceGateway(plugin)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "552213"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-cap-retry", "code": "552213"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-cap-retry", "protocolVersion": "1.1",
		"clientCapabilities": map[string]any{
			"input":  map[string]any{"modalities": []string{"text", "audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}}},
			"output": map[string]any{"modalities": []string{"text"}},
		},
	})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake=%d %s", handshake.Code, handshake.Body.String())
	}
	gateway.mu.Lock()
	gateway.media["unsupported-mp3"] = &deviceMedia{
		ClientID: "pet-cap-retry", ID: "unsupported-mp3", Type: "voice",
		MimeType: "audio/mpeg", Data: []byte("mp3"), Uploaded: true,
		LastAccessedAt: time.Now().UTC(),
	}
	gateway.mu.Unlock()

	const eventID = "retry-after-capability-validation"
	rejected := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{
		"clientId": "pet-cap-retry", "eventId": eventID,
		"message": map[string]any{"type": "text", "text": "bad", "attachments": []map[string]any{{"id": "unsupported-mp3", "type": "voice", "mimeType": "audio/mpeg"}}},
	})
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("unsupported attachment=%d %s", rejected.Code, rejected.Body.String())
	}

	retry := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{
		"clientId": "pet-cap-retry", "eventId": eventID,
		"message": map[string]any{"type": "text", "text": "corrected"},
	})
	if retry.Code != http.StatusOK {
		t.Fatalf("corrected retry=%d %s", retry.Code, retry.Body.String())
	}
	var retryBody map[string]any
	_ = json.NewDecoder(retry.Body).Decode(&retryBody)
	if retryBody["duplicate"] == true {
		t.Fatalf("corrected retry was suppressed: %#v", retryBody)
	}
	select {
	case msg := <-incoming:
		if msg.Text != "corrected" {
			t.Fatalf("forwarded retry=%#v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("corrected retry was not forwarded")
	}
}
