package im

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

type memoryDeviceCredentialStore struct {
	values map[string]string
}

func (s *memoryDeviceCredentialStore) Set(_ context.Context, key, value string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *memoryDeviceCredentialStore) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

func TestDeviceGatewayPairsUploadsForwardsAndPollsReply(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	var inbound IncomingMessage
	plugin.messageHandler = func(msg IncomingMessage) { inbound = msg }
	plugin.mu.Unlock()
	gateway := NewDeviceGateway(plugin)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "123456"); err != nil {
		t.Fatalf("register pairing: %v", err)
	}

	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": "123456"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if token == "" {
		t.Fatalf("missing gateway token: %#v", pairBody)
	}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-a", "protocolVersion": "1.1",
		"capabilities": map[string]any{
			"input":  map[string]any{"modalities": []string{"text", "audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}}},
			"output": map[string]any{"modalities": []string{"text"}},
		},
	})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}

	prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{"clientId": "pet-a", "type": "voice", "fileName": "voice.wav", "mimeType": "audio/wav", "sizeBytes": 3})
	if prepare.Code != http.StatusOK {
		t.Fatalf("prepare status=%d body=%s", prepare.Code, prepare.Body.String())
	}
	var prepareBody struct {
		Media struct {
			ID string `json:"id"`
		} `json:"media"`
		Upload struct {
			URL string `json:"url"`
		} `json:"upload"`
	}
	_ = json.NewDecoder(prepare.Body).Decode(&prepareBody)
	if prepareBody.Media.ID == "" || prepareBody.Upload.URL == "" {
		t.Fatalf("bad prepare response %#v", prepareBody)
	}
	uploadPath := mustDeviceURLPath(t, prepareBody.Upload.URL)
	upload := httptest.NewRequest(http.MethodPut, uploadPath, bytes.NewBufferString("wav"))
	upload.Header.Set("Content-Type", "audio/wav")
	uploadRecorder := httptest.NewRecorder()
	gateway.ServeHTTP(uploadRecorder, upload)
	if uploadRecorder.Code != http.StatusOK {
		t.Fatalf("upload status=%d body=%s", uploadRecorder.Code, uploadRecorder.Body.String())
	}

	incomingReq := map[string]any{"clientId": "pet-a", "eventId": "evt-a", "messageId": "msg-a", "conversationId": "default", "message": map[string]any{"type": "voice", "attachments": []map[string]any{{"id": prepareBody.Media.ID, "type": "voice", "mimeType": "audio/wav"}}}}
	incomingResp := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, incomingReq)
	if incomingResp.Code != http.StatusOK {
		t.Fatalf("incoming status=%d body=%s", incomingResp.Code, incomingResp.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for inbound.PlatformUID == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(inbound.Attachments) != 1 || inbound.Attachments[0].Data == "" || inbound.PlatformUID != "thirdparty:pet-a:default" {
		t.Fatalf("inbound=%#v", inbound)
	}

	gateway.EnqueueReply("pet-a", "default", map[string]any{"type": "text", "text": "hello"})
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-a&cursor=0", token, nil)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", poll.Code, poll.Body.String())
	}
	var pollBody struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&pollBody)
	if len(pollBody.Messages) != 1 || pollBody.Messages[0]["text"] != "hello" {
		t.Fatalf("poll=%#v", pollBody)
	}
}

func TestDeviceGatewayRejectsUndeclaredInputMedia(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "991122"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "text-only", "code": "991122"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "text-only", "protocolVersion": "1.1",
		"capabilities": map[string]any{"input": map[string]any{"modalities": []string{"text"}}, "output": map[string]any{"modalities": []string{"text"}}},
	})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake=%d %s", handshake.Code, handshake.Body.String())
	}
	prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{
		"clientId": "text-only", "type": "voice", "fileName": "voice.wav", "mimeType": "audio/wav", "sizeBytes": 12,
	})
	if prepare.Code != http.StatusBadRequest {
		t.Fatalf("undeclared media prepare=%d %s", prepare.Code, prepare.Body.String())
	}
	incoming := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{
		"clientId": "text-only", "eventId": "evt-audio", "message": map[string]any{"type": "voice"},
	})
	if incoming.Code != http.StatusBadRequest {
		t.Fatalf("undeclared incoming=%d %s", incoming.Code, incoming.Body.String())
	}
}

func TestDeviceGatewayLegacyClientCanUploadBeforeHandshake(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "991123"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "legacy-audio", "code": "991123"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{
		"clientId": "legacy-audio", "type": "voice", "fileName": "voice.wav", "mimeType": "audio/wav", "sizeBytes": 12,
	})
	if prepare.Code != http.StatusOK {
		t.Fatalf("legacy media prepare=%d %s", prepare.Code, prepare.Body.String())
	}
}

func TestDeviceGatewayPairAcceptsPairCode(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223343"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-pair-code", "pairCode": "223343"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
}

func TestDeviceGatewayPairAcceptsLegacyCode(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223344"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-legacy-code", "code": "223344"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
}

func TestDeviceGatewayOutgoingLongPollWakesOnReply(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223344"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-long-poll", "code": "223344"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-long-poll&cursor=0&limit=1&timeout=2", token, nil)
	}()
	select {
	case <-done:
		t.Fatal("long poll returned before a reply was available")
	case <-time.After(50 * time.Millisecond):
	}
	gateway.EnqueueReply("pet-long-poll", "default", map[string]any{"type": "text", "text": "wake"})
	select {
	case response := <-done:
		var body struct {
			Messages []map[string]any `json:"messages"`
		}
		_ = json.NewDecoder(response.Body).Decode(&body)
		if response.Code != http.StatusOK || len(body.Messages) != 1 || body.Messages[0]["text"] != "wake" {
			t.Fatalf("long poll response=%d %#v", response.Code, body)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll did not wake after enqueue")
	}
}

func TestDeviceGatewayOutgoingHonorsLimitAndHasMore(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223355"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-limit", "code": "223355"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	gateway.EnqueueReply("pet-limit", "default", map[string]any{"type": "text", "text": "one"})
	gateway.EnqueueReply("pet-limit", "default", map[string]any{"type": "text", "text": "two"})

	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-limit&cursor=0&limit=1", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
		HasMore  bool             `json:"hasMore"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 || !body.HasMore {
		t.Fatalf("limited poll=%#v", body)
	}
}

func TestDeviceGatewayAckPrunesDeliveredMessages(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-prune")
	state.next = 1
	state.messages = append(state.messages, map[string]any{"seq": int64(1), "id": "message-1", "type": "text", "text": "done"})
	state.acked["message-1"] = true
	pruneDeviceMessagesLocked(state)
	if len(state.messages) != 0 || len(state.acked) != 0 {
		t.Fatalf("acknowledged queue was not pruned: messages=%#v acked=%#v", state.messages, state.acked)
	}
}

func TestDeviceGatewayAckIgnoresUnknownMessageIDs(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223356"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-ack", "code": "223356"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-ack", "messageIds": []string{"unknown-1", "unknown-2"},
	})
	if ack.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ack.Code, ack.Body.String())
	}
	state := gateway.clients["pet-ack"]
	if state == nil || len(state.acked) != 0 {
		t.Fatalf("unknown message IDs were retained: %#v", state)
	}
}

func TestDeviceGatewayAckPreservesFailedDeliveryStatus(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223357"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-failed-ack", "code": "223357"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	gateway.EnqueueReply("pet-failed-ack", "default", map[string]any{"type": "text", "text": "fallback"})
	state := gateway.clients["pet-failed-ack"]
	if state == nil || len(state.messages) != 1 {
		t.Fatalf("reply was not queued: %#v", state)
	}
	messageID, _ := state.messages[0]["id"].(string)

	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-failed-ack", "messageIds": []string{messageID}, "status": "failed",
	})
	if ack.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ack.Code, ack.Body.String())
	}
	state = gateway.clients["pet-failed-ack"]
	if state == nil || len(state.messages) != 0 || state.ackStatus[messageID] != "failed" {
		t.Fatalf("failed receipt was not preserved: %#v", state)
	}
}

func TestDeviceGatewayAckRejectsOversizedBatch(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223358"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-ack-limit", "code": "223358"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	ids := make([]string, coreim.ThirdPartyMaxAckIDs+1)
	for index := range ids {
		ids[index] = fmt.Sprintf("message-%d", index)
	}
	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-ack-limit", "messageIds": ids,
	})
	if ack.Code != http.StatusBadRequest {
		t.Fatalf("oversized ACK status=%d body=%s", ack.Code, ack.Body.String())
	}
}

func TestDeviceGatewayAckReceiptMapIsBounded(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223359"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-ack-bounded", "code": "223359"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	for index := 0; index < deviceGatewayMaxAckReceipts+25; index++ {
		gateway.EnqueueReply("pet-ack-bounded", "default", map[string]any{"type": "text", "text": fmt.Sprintf("reply-%d", index)})
		state := gateway.clients["pet-ack-bounded"]
		messageID, _ := state.messages[len(state.messages)-1]["id"].(string)
		ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
			"clientId": "pet-ack-bounded", "messageIds": []string{messageID}, "status": "failed",
		})
		if ack.Code != http.StatusOK {
			t.Fatalf("ack %d status=%d body=%s", index, ack.Code, ack.Body.String())
		}
	}
	state := gateway.clients["pet-ack-bounded"]
	if len(state.ackStatus) > deviceGatewayMaxAckReceipts {
		t.Fatalf("ACK receipts grew to %d, limit %d", len(state.ackStatus), deviceGatewayMaxAckReceipts)
	}
}

func TestDeviceGatewayHandshakeAcceptsTopLevelClientCapabilities(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223360"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-top-level-caps", "code": "223360"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-top-level-caps", "protocolVersion": "1.1",
		"clientCapabilities": map[string]any{
			"output":   map[string]any{"modalities": []string{"text"}, "text": map[string]any{"maxChars": 240}},
			"features": map[string]any{"ambientDisplay": true},
		},
	})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	state := gateway.clients["pet-top-level-caps"]
	if state == nil || !state.capabilities.SupportsOutput("text") || !state.capabilities.Features.AmbientDisplay {
		t.Fatalf("top-level client capabilities were not retained: %#v", state)
	}
}

func TestDeviceGatewayQueueIsBounded(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	for index := 0; index < deviceGatewayMaxQueuedMessages+25; index++ {
		gateway.EnqueueReply("pet-bounded", "default", map[string]any{"type": "text", "text": "queued"})
	}
	state := gateway.clients["pet-bounded"]
	if state == nil || len(state.messages) != deviceGatewayMaxQueuedMessages {
		t.Fatalf("queued message count=%d, want %d", len(state.messages), deviceGatewayMaxQueuedMessages)
	}
	firstSeq, _ := state.messages[0]["seq"].(int64)
	if firstSeq != 26 {
		t.Fatalf("oldest retained sequence=%d, want 26", firstSeq)
	}
}

func TestDeviceGatewayTokenSurvivesRestart(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	first := NewPersistentDeviceGateway(nil, store)
	if err := first.RegisterPairing("gui-a", "tenant-a", "user-a", "654321"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, first, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": "654321"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&body)
	token, _ := body["gatewayToken"].(string)
	if token == "" {
		t.Fatal("pair did not return a token")
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	handshake := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-a"})
	if handshake.Code != http.StatusOK {
		t.Fatalf("persisted token rejected after restart: status=%d body=%s", handshake.Code, handshake.Body.String())
	}
}

func TestDeviceGatewayHandshakePublishesPetAssetByReference(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	rawFrame := bytes.Repeat([]byte{0x12, 0x34}, 32*32)
	asset := &DevicePetAsset{Encoding: "rgb565le", Width: 32, Height: 32, Data: base64.StdEncoding.EncodeToString(rawFrame), Frames: []string{base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x56, 0x78}, 32*32))}}
	if err := gateway.registerPairingWithPetProfileAsset("gui-a", "tenant-a", "user-a", "654322", "mini-claw", true, asset); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-asset", "code": "654322"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)

	localRenderer := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-asset", "capabilities": map[string]any{"features": map[string]any{"petStates": true}},
	})
	var localBody struct {
		Pet devicePetProfile `json:"pet"`
	}
	_ = json.NewDecoder(localRenderer.Body).Decode(&localBody)
	if localBody.Pet.Asset != nil || localBody.Pet.Skin != "mini-claw" {
		t.Fatalf("local renderer received unexpected profile=%#v", localBody.Pet)
	}

	assetRenderer := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-asset", "capabilities": map[string]any{"features": map[string]any{"petAsset": true, "petAnimation": true}},
	})
	var assetBody struct {
		Pet      devicePetProfile        `json:"pet"`
		PetAsset devicePetAssetReference `json:"petAsset"`
	}
	_ = json.NewDecoder(assetRenderer.Body).Decode(&assetBody)
	if assetBody.Pet.Asset != nil || len(assetBody.PetAsset.URLs) != 2 || assetBody.PetAsset.Revision == "" {
		t.Fatalf("animation renderer received unexpected profile=%#v reference=%#v", assetBody.Pet, assetBody.PetAsset)
	}
	for index, rawURL := range assetBody.PetAsset.URLs {
		media := deviceGatewayRequest(t, gateway, http.MethodGet, mustDeviceURLPath(t, rawURL), "", nil)
		if media.Code != http.StatusOK {
			t.Fatalf("frame %d status=%d body=%s", index, media.Code, media.Body.String())
		}
		if got := media.Body.Bytes(); len(got) != len(rawFrame) {
			t.Fatalf("frame %d bytes=%d want=%d", index, len(got), len(rawFrame))
		}
	}
}

func TestDeviceGatewayPetProfileUpdateIncludesAssetForCapableHardware(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	frame := bytes.Repeat([]byte{0xAA, 0x55}, 32*32)
	asset := &DevicePetAsset{Encoding: "rgb565le", Width: 32, Height: 32, Data: base64.StdEncoding.EncodeToString(frame)}
	if err := gateway.registerPairingWithPetProfileAsset("gui-a", "tenant-a", "user-a", "654323", "clawmate", true, asset); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-update-asset", "code": "654323"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-update-asset", "capabilities": map[string]any{"features": map[string]any{"petAsset": true}},
	})
	gateway.updateMachinePetProfileAsset("gui-a", "mini-claw", true, asset)
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-update-asset&cursor=0", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 {
		t.Fatalf("messages=%#v", body.Messages)
	}
	ref, ok := body.Messages[0]["pet_asset"].(map[string]any)
	if !ok || ref["revision"] == "" {
		t.Fatalf("pet asset reference missing: %#v", body.Messages[0])
	}
}

func TestDeviceGatewayHandshakeAdvertisesAvailableMeetingModes(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.SetMeetingRecordingHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gateway.SetMeetingRecordingModes(true, false)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "334400"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "meeting-pet", "code": "334400"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "meeting-pet"})
	var body struct {
		Meeting struct {
			Modes map[string]bool `json:"modes"`
		} `json:"meetingRecording"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if handshake.Code != http.StatusOK || !body.Meeting.Modes["keep"] || !body.Meeting.Modes["transcript"] || body.Meeting.Modes["minutes"] {
		t.Fatalf("meeting modes status=%d body=%s", handshake.Code, handshake.Body.String())
	}
}

func TestDeviceGatewayHandshakeOmitsMeetingWhenHandlerMissing(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.SetMeetingRecordingModes(true, true)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "334401"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "no-meeting-pet", "code": "334401"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", paired["gatewayToken"].(string), map[string]any{"clientId": "no-meeting-pet"})
	var body map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if _, ok := body["meetingRecording"]; ok {
		t.Fatalf("unexpected meeting capability: %#v", body)
	}
}
func TestDeviceGatewayNegotiatesAndForwardsClientCapabilities(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	var inbound IncomingMessage
	plugin.messageHandler = func(msg IncomingMessage) { inbound = msg }
	plugin.mu.Unlock()
	gateway := NewDeviceGateway(plugin)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "334455"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": "334455"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)

	declared := agent.ClientCapabilities{Input: agent.ClientInputCapabilities{Modalities: []string{"text"}}, Output: agent.ClientOutputCapabilities{
		Modalities:   []string{"text", "image", "unsupported"},
		Preferred:    []string{"image", "text"},
		Combinations: [][]string{{"text", "image"}},
		Text:         &agent.ClientTextCapabilities{MaxChars: 240, Markdown: false, Locale: "zh-CN"},
		Image:        &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}, MaxWidth: 360, MaxHeight: 360},
	}}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-a", "protocolVersion": "1.1", "capabilities": declared})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	var handshakeBody struct {
		ProtocolVersion string                   `json:"protocolVersion"`
		Capabilities    agent.ClientCapabilities `json:"capabilitiesAccepted"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&handshakeBody)
	if handshakeBody.ProtocolVersion != "1.1" || len(handshakeBody.Capabilities.Output.Modalities) != 2 {
		t.Fatalf("negotiated capabilities=%#v", handshakeBody)
	}

	incoming := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/incoming", token, map[string]any{"clientId": "pet-a", "eventId": "evt-cap", "conversationId": "default", "message": map[string]any{"type": "text", "text": "hello"}})
	if incoming.Code != http.StatusOK {
		t.Fatalf("incoming status=%d body=%s", incoming.Code, incoming.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for inbound.ClientCapabilities == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if inbound.ClientCapabilities == nil || !inbound.ClientCapabilities.SupportsOutputCombination("text", "image") {
		t.Fatalf("inbound capabilities=%#v", inbound.ClientCapabilities)
	}
}

func TestDeviceGatewayLegacyHandshakeDefaultsToTextOnly(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445566"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "legacy", "code": "445566"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "legacy"})
	var body struct {
		Capabilities agent.ClientCapabilities `json:"capabilitiesAccepted"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if !body.Capabilities.SupportsOutput("text") || body.Capabilities.SupportsOutput("image") {
		t.Fatalf("legacy capabilities=%#v", body.Capabilities)
	}
}

func TestDeviceGatewayEnqueueReplyEnforcesNegotiatedOutput(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.mu.Lock()
	state := gateway.clientLocked("pet-media")
	state.capabilities = agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text", "image"},
		Text:       &agent.ClientTextCapabilities{MaxChars: 4},
		Image:      &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	gateway.mu.Unlock()
	gateway.EnqueueReply("pet-media", "default", map[string]any{"type": "audio", "mime_type": "audio/wav", "file_data": "x"})
	gateway.EnqueueReply("pet-media", "default", map[string]any{"type": "image", "mime_type": "image/jpeg", "image_data": "x"})
	gateway.EnqueueReply("pet-media", "default", map[string]any{"type": "image", "mime_type": "image/png", "image_data": "x"})
	gateway.EnqueueReply("pet-media", "default", map[string]any{"type": "text", "text": "123456"})
	gateway.mu.Lock()
	messages := append([]map[string]any(nil), gateway.clients["pet-media"].messages...)
	gateway.mu.Unlock()
	if len(messages) != 2 || messages[0]["type"] != "image" || messages[1]["text"] != "1234" {
		t.Fatalf("device reply filtering=%#v", messages)
	}
}

func TestDeviceGatewayEnqueueReplyPreservesHardwareFeatureMessages(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.mu.Lock()
	state := gateway.clientLocked("pet-features")
	state.capabilities = agent.NormalizeClientCapabilities(&agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{
			Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 5},
		},
		Features: agent.ClientFeatureCapabilities{PetStates: true, AmbientDisplay: true, MeetingRecorder: true},
	})
	gateway.mu.Unlock()
	gateway.EnqueueReply("pet-features", "default", map[string]any{"type": "pet_state", "extra": map[string]any{"state": "thinking"}})
	gateway.EnqueueReply("pet-features", "default", map[string]any{"type": "ambient", "ambient": map[string]any{"weather": "sunny"}})
	gateway.EnqueueReply("pet-features", "default", map[string]any{"type": "meeting_result", "text": "123456789"})
	gateway.mu.Lock()
	messages := append([]map[string]any(nil), gateway.clients["pet-features"].messages...)
	gateway.mu.Unlock()
	if len(messages) != 3 || messages[0]["type"] != "pet_state" || messages[1]["type"] != "ambient" || messages[2]["type"] != "meeting_result" {
		t.Fatalf("feature messages=%#v", messages)
	}
	if messages[2]["text"] != "12345" {
		t.Fatalf("meeting result text=%#v", messages[2]["text"])
	}
}

func TestDeviceGatewayAudioUsesMediaURLWhenInlineLimitIsExceeded(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.mu.Lock()
	state := gateway.clientLocked("pet-audio-url")
	state.capabilities = agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes: []string{"inline", "url"}, MaxInlineBytes: 4, MaxDownloadBytes: 1024,
		},
	}})
	gateway.mu.Unlock()
	wav := []byte("0123456789")
	gateway.EnqueueReply("pet-audio-url", "default", map[string]any{
		"type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString(wav),
	})
	gateway.mu.Lock()
	messages := append([]map[string]any(nil), gateway.clients["pet-audio-url"].messages...)
	gateway.mu.Unlock()
	if len(messages) != 1 || messages[0]["file_data"] != nil || messages[0]["url"] == "" || messages[0]["sizeBytes"] != int64(len(wav)) {
		t.Fatalf("URL audio message=%#v", messages)
	}
	url, _ := messages[0]["url"].(string)
	request := httptest.NewRequest(http.MethodGet, url, nil)
	response := httptest.NewRecorder()
	gateway.handleMedia(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), wav) {
		t.Fatalf("media download status=%d body=%q", response.Code, response.Body.Bytes())
	}
}

func TestDeviceGatewayRelaysAmbientWeatherToPairedHardware(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "112233"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": "112233"})
	var body map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&body)
	token, _ := body["gatewayToken"].(string)
	if token == "" {
		t.Fatalf("pair status=%d body=%#v", pair.Code, body)
	}

	gateway.UpdateMachineAmbient("gui-a", map[string]any{
		"weather":   map[string]any{"summary": "多云", "temperatureC": float64(31), "location": "上海"},
		"expiresAt": float64(time.Now().Add(time.Hour).UnixMilli()),
	})
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-a&cursor=0", token, nil)
	var polled struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 || polled.Messages[0]["type"] != "ambient" {
		t.Fatalf("ambient poll status=%d messages=%#v", poll.Code, polled.Messages)
	}

	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-a"})
	var handshakeBody map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&handshakeBody)
	if _, ok := handshakeBody["ambient"].(map[string]any); !ok {
		t.Fatalf("handshake did not include latest ambient: %#v", handshakeBody)
	}
}

func TestDeviceGatewayPetProfileUpdateWakesPairedHardware(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairingWithPetProfile("gui-a", "tenant-a", "user-a", "123456", "clawmate", true); err != nil {
		t.Fatalf("register pairing: %v", err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": "123456"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if pair.Code != http.StatusCreated || token == "" {
		t.Fatalf("pair status=%d body=%#v", pair.Code, pairBody)
	}

	gateway.UpdateMachinePetProfile("gui-a", "mini-claw", false)
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-a&cursor=0", token, nil)
	var polled struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 || polled.Messages[0]["type"] != "pet_profile" || polled.Messages[0]["pet_skin"] != "mini-claw" {
		t.Fatalf("pet profile poll status=%d messages=%#v", poll.Code, polled.Messages)
	}

	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-a"})
	var handshakeBody struct {
		Pet devicePetProfile `json:"pet"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&handshakeBody)
	if handshakeBody.Pet.Skin != "mini-claw" || handshakeBody.Pet.MotionEnabled {
		t.Fatalf("handshake pet=%#v", handshakeBody.Pet)
	}
}

func TestDeviceGatewayPetProfileUpdatePersistsAndDeduplicates(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	// Two physical devices (distinct clientIds) pair under the same user:
	// each must keep its own token. Pairing the same clientId twice would
	// intentionally revoke the first credential instead.
	for i, code := range []string{"123451", "123452"} {
		if err := gateway.RegisterPairingWithPetProfile("gui-a", "tenant-a", "user-a", code, "clawmate", true); err != nil {
			t.Fatalf("register pairing %s: %v", code, err)
		}
		clientID := []string{"pet-a", "pet-b"}[i]
		pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": code})
		if pair.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", code, pair.Code, pair.Body.String())
		}
	}

	gateway.UpdateMachinePetProfile("gui-a", "mini-claw", false)
	persisted := NewPersistentDeviceGateway(nil, store)
	if len(persisted.tokens) != 2 {
		t.Fatalf("persisted token count=%d, want 2", len(persisted.tokens))
	}
	for _, principal := range persisted.tokens {
		if principal.Pet.Skin != "mini-claw" || principal.Pet.MotionEnabled {
			t.Fatalf("persisted pet profile=%#v", principal.Pet)
		}
	}
	state := gateway.clients["pet-a"]
	if state == nil || len(state.messages) != 1 {
		t.Fatalf("duplicate client profile notifications=%#v", state)
	}

	// Replaying an identical reconnect sync must be a no-op rather than causing
	// another screen refresh and animation reset.
	gateway.UpdateMachinePetProfile("gui-a", "mini-claw", false)
	if len(state.messages) != 1 {
		t.Fatalf("identical profile update queued %d messages, want 1", len(state.messages))
	}
}

func TestDeviceGatewayWelcomePlaysOncePerBootAndPersists(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445566"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-welcome", "code": "445566"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if pair.Code != http.StatusCreated || token == "" {
		t.Fatalf("pair status=%d body=%#v", pair.Code, pairBody)
	}
	if err := gateway.UpdateMachineWelcome("gui-a", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}, "playback": true}}}
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-a", "capabilities": capabilities})
	if first.Code != http.StatusOK {
		t.Fatalf("first handshake status=%d body=%s", first.Code, first.Body.String())
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=0", token, nil)
	var polled struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 || polled.Messages[0]["type"] != "audio" || polled.Messages[0]["bootSessionId"] != "boot-a" {
		t.Fatalf("first boot messages=%#v", polled.Messages)
	}
	// A capability refresh is a normal handshake, not a new boot.
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-a", "capabilities": capabilities})
	poll = deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=1", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 0 {
		t.Fatalf("same boot queued another welcome: %#v", polled.Messages)
	}
	// A Hub restart retains both the sound and the last boot ID.
	restarted := NewPersistentDeviceGateway(nil, store)
	_ = deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-a", "capabilities": capabilities})
	poll = deviceGatewayRequest(t, restarted, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=0", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 0 {
		t.Fatalf("restart replayed same boot welcome: %#v", polled.Messages)
	}
	_ = deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-b", "capabilities": capabilities})
	poll = deviceGatewayRequest(t, restarted, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=0", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 || polled.Messages[0]["type"] != "audio" || polled.Messages[0]["bootSessionId"] != "boot-b" {
		t.Fatalf("new boot messages=%#v", polled.Messages)
	}
}

func TestDeviceGatewayWelcomeCanClearPersistedAudio(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.UpdateMachineWelcome("gui-a", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineWelcome("gui-a", true, "", true); err != nil {
		t.Fatal(err)
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	config, ok := restarted.hardware["gui-a"]
	if !ok || !config.WelcomeEnabled || config.WelcomeAudio != "" {
		t.Fatalf("persisted welcome config = %#v, want enabled with cleared audio", config)
	}
}

func TestDeviceGatewayRepairingRevokesPreviousCredentialAndQueue(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445569"); err != nil {
		t.Fatal(err)
	}
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "repaired-pet", "code": "445569"})
	var firstBody map[string]any
	_ = json.NewDecoder(first.Body).Decode(&firstBody)
	firstToken, _ := firstBody["gatewayToken"].(string)
	if first.Code != http.StatusCreated || firstToken == "" {
		t.Fatalf("first pair status=%d body=%#v", first.Code, firstBody)
	}
	gateway.EnqueueReply("repaired-pet", "system", map[string]any{"type": "text", "text": "stale"})
	if err := gateway.RegisterPairing("gui-b", "tenant-b", "user-b", "445570"); err != nil {
		t.Fatal(err)
	}
	second := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "repaired-pet", "code": "445570"})
	var secondBody map[string]any
	_ = json.NewDecoder(second.Body).Decode(&secondBody)
	secondToken, _ := secondBody["gatewayToken"].(string)
	if second.Code != http.StatusCreated || secondToken == "" || secondToken == firstToken {
		t.Fatalf("second pair status=%d body=%#v", second.Code, secondBody)
	}

	oldHandshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", firstToken, map[string]any{"clientId": "repaired-pet"})
	if oldHandshake.Code != http.StatusUnauthorized {
		t.Fatalf("old token handshake status=%d body=%s", oldHandshake.Code, oldHandshake.Body.String())
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=repaired-pet&cursor=0", secondToken, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if poll.Code != http.StatusOK || len(outgoing.Messages) != 0 {
		t.Fatalf("new device inherited stale queue status=%d messages=%#v", poll.Code, outgoing.Messages)
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	if len(restarted.tokens) != 1 {
		t.Fatalf("persisted token count=%d, want exactly one replacement credential", len(restarted.tokens))
	}
}

func TestDeviceGatewayMachineHardwareReplyHonorsCapabilities(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445567"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-volume", "code": "445567"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-volume", "capabilities": map[string]any{"features": map[string]any{"volumeControl": true}}})
	gateway.EnqueueMachineReply("gui-a", "system", map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"volume": 35}})
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-volume&cursor=0", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 || body.Messages[0]["type"] != "hardware_config" {
		t.Fatalf("hardware reply=%#v", body.Messages)
	}
	gateway.EnqueueMachineReply("gui-a", "system", map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"volume": 101}})
	poll = deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-volume&cursor=1", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 0 {
		t.Fatalf("out-of-range volume was queued: %#v", body.Messages)
	}
}

func TestDeviceGatewayVolumePersistsAndSyncsOnHandshake(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445568"); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineVolume("gui-a", 0); err != nil {
		t.Fatalf("UpdateMachineVolume(mute): %v", err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-volume-persist", "code": "445568"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"features": map[string]any{"volumeControl": true}}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-volume-persist", "capabilities": capabilities})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-volume-persist&cursor=0", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 || body.Messages[0]["type"] != "hardware_config" {
		t.Fatalf("persisted volume message=%#v", body.Messages)
	}
	extra, _ := body.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(0) {
		t.Fatalf("persisted volume=%#v, want mute", extra)
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	handshake = deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-volume-persist", "capabilities": capabilities})
	if handshake.Code != http.StatusOK {
		t.Fatalf("restart handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	poll = deviceGatewayRequest(t, restarted, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-volume-persist&cursor=0", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 {
		t.Fatalf("restart volume message=%#v", body.Messages)
	}
	extra, _ = body.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(0) {
		t.Fatalf("restart persisted volume=%#v, want mute", extra)
	}
}

func TestDeviceGatewayHandshakeDoesNotDuplicatePendingVolume(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445571"); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineVolume("gui-a", 55); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "volume-repeat", "code": "445571"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"features": map[string]any{"volumeControl": true}}
	for range 3 {
		handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "volume-repeat", "capabilities": capabilities})
		if handshake.Code != http.StatusOK {
			t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
		}
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=volume-repeat&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("duplicate persisted volume messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(55) {
		t.Fatalf("coalesced volume=%#v, want 55", extra)
	}
}

func TestDeviceGatewayPendingVolumeKeepsLatestValue(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445572"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "volume-latest", "code": "445572"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"features": map[string]any{"volumeControl": true}}
	for _, volume := range []int{22, 78, 41} {
		if err := gateway.UpdateMachineVolume("gui-a", volume); err != nil {
			t.Fatal(err)
		}
		handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "volume-latest", "capabilities": capabilities})
		if handshake.Code != http.StatusOK {
			t.Fatalf("handshake status=%d", handshake.Code)
		}
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=volume-latest&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("pending volume messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(41) {
		t.Fatalf("latest volume=%#v, want 41", extra)
	}
}

func TestDeviceGatewayBroadcastVolumeKeepsLatestValue(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445573"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "volume-broadcast", "code": "445573"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "volume-broadcast", "capabilities": map[string]any{"features": map[string]any{"volumeControl": true}}})
	for _, volume := range []int{22, 78, 41} {
		gateway.EnqueueMachineReply("gui-a", "system", map[string]any{"reply_type": "hardware_config", "extra": map[string]any{"volume": volume}})
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=volume-broadcast&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("broadcast volume messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(41) {
		t.Fatalf("broadcast latest volume=%#v, want 41", extra)
	}
}

func TestDeviceGatewayPetProfileUpdateRefreshesPendingPairing(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairingWithPetProfile("gui-a", "tenant-a", "user-a", "123453", "clawmate", true); err != nil {
		t.Fatal(err)
	}
	gateway.UpdateMachinePetProfile("gui-a", "focus-claw", false)
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-pending", "code": "123453"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-pending"})
	var body struct {
		Pet devicePetProfile `json:"pet"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if handshake.Code != http.StatusOK || body.Pet.Skin != "focus-claw" || body.Pet.MotionEnabled {
		t.Fatalf("pending pairing profile status=%d pet=%#v", handshake.Code, body.Pet)
	}
}

func TestNormalizeDeviceAmbientAcceptsIntegerTemperatureAndRejectsOutOfRange(t *testing.T) {
	ambient, ok := normalizeDeviceAmbient(map[string]any{
		"weather": map[string]any{"summary": "晴", "temperatureC": int32(-6), "location": "北京"},
	})
	if !ok {
		t.Fatal("integer temperature should be accepted")
	}
	weather := ambient["weather"].(map[string]any)
	if weather["temperatureC"] != -6 {
		t.Fatalf("unexpected normalized temperature: %#v", weather)
	}
	if _, ok := normalizeDeviceAmbient(map[string]any{
		"weather": map[string]any{"summary": "晴", "temperatureC": 81},
	}); ok {
		t.Fatal("out-of-range temperature should be rejected")
	}
}

func TestNormalizeDeviceAmbientKeepsOnlyValidDynamicGlyphs(t *testing.T) {
	bitmap := make([]byte, deviceGlyphBytesPerGlyph)
	bitmap[0] = 0x80
	ambient, ok := normalizeDeviceAmbient(map[string]any{
		"weather": map[string]any{"summary": "ok", "temperatureC": 24},
		"glyphs": map[string]any{
			"U+56FE": base64.StdEncoding.EncodeToString(bitmap),
			"U+ZZZZ": "not-valid",
		},
	})
	if !ok {
		t.Fatal("ambient with a valid glyph should be accepted")
	}
	glyphs, ok := ambient["glyphs"].(map[string]string)
	if !ok || len(glyphs) != 1 || glyphs["U+56FE"] == "" {
		t.Fatalf("unexpected normalized glyphs: %#v", ambient["glyphs"])
	}
}

func deviceGatewayRequest(t *testing.T, gateway *DeviceGateway, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&raw).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &raw)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	return rec
}

func mustDeviceURLPath(t *testing.T, raw string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req.URL.RequestURI()
}

func TestDeviceGatewayPairCodeFromTranscript(t *testing.T) {
	cases := map[string]string{
		"645432":                       "645432",
		"请配对 六 四 五 四 三 二":              "645432",
		"零幺两三四五":                       "012345",
		"six four five four three two": "645432",
	}
	for input, want := range cases {
		got, ok := deviceGatewayPairCodeFromTranscript(input)
		if !ok || got != want {
			t.Errorf("deviceGatewayPairCodeFromTranscript(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if _, ok := deviceGatewayPairCodeFromTranscript("六码 64 54 32 七"); ok {
		t.Fatal("accepted transcript containing seven digits")
	}
}

func TestDeviceGatewayVoicePairConsumesCodeAndIssuesToken(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "645432"); err != nil {
		t.Fatal(err)
	}
	gateway.SetVoicePairTranscriber(func(_ context.Context, path, contentType string) (string, error) {
		if contentType != "audio/wav" {
			t.Fatalf("contentType=%q", contentType)
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) != 44 {
			t.Fatalf("temporary WAV len=%d err=%v", len(data), err)
		}
		return "六 四 五 四 三 二", nil
	})
	wav := make([]byte, 44)
	req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair/voice", bytes.NewReader(wav))
	req.Header.Set("X-MaClaw-Client-ID", "pet-voice")
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !bytes.Contains(rec.Body.Bytes(), []byte("gatewayToken")) {
		t.Fatalf("voice pair status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair/voice", bytes.NewReader(wav))
	req.Header.Set("X-MaClaw-Client-ID", "pet-voice")
	rec = httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("voice pairing code reused: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeviceGatewayVoicePairRateLimitIsPerClientAndAddress(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	base := time.Date(2026, 8, 4, 13, 0, 0, 0, time.UTC)
	key := "203.0.113.8\x00pet-a"
	for i := 0; i < deviceVoicePairAttemptLimit; i++ {
		if !gateway.allowVoicePairAttempt(key, base) {
			t.Fatalf("attempt %d unexpectedly denied", i+1)
		}
	}
	if gateway.allowVoicePairAttempt(key, base) {
		t.Fatal("attempt above limit was allowed")
	}
	if !gateway.allowVoicePairAttempt("203.0.113.8\x00pet-b", base) {
		t.Fatal("different client ID shared rate limit")
	}
	if !gateway.allowVoicePairAttempt(key, base.Add(deviceVoicePairAttemptWindow)) {
		t.Fatal("rate limit did not reset after window")
	}
}
