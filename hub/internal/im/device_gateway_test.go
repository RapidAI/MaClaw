package im

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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

func TestDeviceGatewayHandshakeOmitsPetAssetUnlessAnimationSupported(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	asset := &DevicePetAsset{Encoding: "rgb565le", Width: 32, Height: 32, Data: "asset-data"}
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
		"clientId": "pet-asset", "capabilities": map[string]any{"features": map[string]any{"petAnimation": true}},
	})
	var assetBody struct {
		Pet devicePetProfile `json:"pet"`
	}
	_ = json.NewDecoder(assetRenderer.Body).Decode(&assetBody)
	if assetBody.Pet.Asset == nil || assetBody.Pet.Asset.Data != "asset-data" {
		t.Fatalf("animation renderer did not receive asset=%#v", assetBody.Pet)
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

	declared := agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
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
	for _, code := range []string{"123451", "123452"} {
		if err := gateway.RegisterPairingWithPetProfile("gui-a", "tenant-a", "user-a", code, "clawmate", true); err != nil {
			t.Fatalf("register pairing %s: %v", code, err)
		}
		pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "code": code})
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
