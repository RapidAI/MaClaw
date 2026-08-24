package im

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

type memoryDeviceCredentialStore struct {
	values    map[string]string
	setErr    error
	setErrKey string
}

func (s *memoryDeviceCredentialStore) Set(_ context.Context, key, value string) error {
	if s.setErr != nil && (s.setErrKey == "" || s.setErrKey == key) {
		return s.setErr
	}
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func (s *memoryDeviceCredentialStore) Get(_ context.Context, key string) (string, error) {
	return s.values[key], nil
}

type deviceGatewayIdentityStore struct {
	provider *sqlite.Provider
	store    *store.Store
}

func newDeviceGatewayIdentityStore(t *testing.T) *deviceGatewayIdentityStore {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               t.TempDir() + `\hub-device-recovery.db`,
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  2,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		_ = provider.Close()
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return &deviceGatewayIdentityStore{provider: provider, store: sqlite.NewStore(provider)}
}

func TestDeviceGatewayPairsUploadsForwardsAndPollsReply(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	inboundMessages := make(chan IncomingMessage, 1)
	plugin.messageHandler = func(msg IncomingMessage) { inboundMessages <- msg }
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
	var inbound IncomingMessage
	select {
	case inbound = <-inboundMessages:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded incoming message")
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

func TestDeviceGatewayRejectsMalformedPairCodeBeforeRateLimit(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	for attempt := 0; attempt < deviceCodePairAttemptLimit+2; attempt++ {
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
			map[string]any{"clientId": "pet-malformed", "pairCode": "invalid"})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "bad_pairing_code") {
			t.Fatalf("attempt %d malformed pairing=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
}

func TestDeviceGatewayReplacingPairingCodeRevokesPreviousMachineCode(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223345"); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223346"); err != nil {
		t.Fatal(err)
	}

	oldCode := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-old-code", "pairCode": "223345"})
	if oldCode.Code != http.StatusUnauthorized {
		t.Fatalf("replaced pairing code status=%d body=%s", oldCode.Code, oldCode.Body.String())
	}

	newCode := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-new-code", "pairCode": "223346"})
	if newCode.Code != http.StatusCreated {
		t.Fatalf("replacement pairing code status=%d body=%s", newCode.Code, newCode.Body.String())
	}
}

func TestDeviceGatewayDisabledHardwareRejectsUnconsumedPairingCode(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223348"); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}

	disabled := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-disabled-pair", "pairCode": "223348"})
	if disabled.Code != http.StatusServiceUnavailable || !strings.Contains(disabled.Body.String(), "hardware_disabled") {
		t.Fatalf("disabled pairing=%d body=%s", disabled.Code, disabled.Body.String())
	}

	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	enabled := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-disabled-pair", "pairCode": "223348"})
	if enabled.Code != http.StatusCreated {
		t.Fatalf("pairing code was incorrectly consumed while disabled: status=%d body=%s", enabled.Code, enabled.Body.String())
	}
}

func TestDeviceGatewayDisabledHardwareRejectsPairingRegistration(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223349")
	if err == nil {
		t.Fatal("pairing registration succeeded while hardware was disabled")
	}
	var coded interface{ PairingErrorCode() string }
	if !errors.As(err, &coded) || coded.PairingErrorCode() != "HARDWARE_DISABLED" {
		t.Fatalf("disabled registration did not expose a stable code: %v", err)
	}
}

func TestDeviceGatewayRejectsPairingCodeCollisionAcrossMachines(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223347"); err != nil {
		t.Fatal(err)
	}
	err := gateway.RegisterPairing("gui-b", "tenant-b", "user-b", "223347")
	if err == nil {
		t.Fatal("cross-machine pairing-code collision was accepted")
	}
	var coded interface{ PairingErrorCode() string }
	if !errors.As(err, &coded) || coded.PairingErrorCode() != "PAIRING_CODE_COLLISION" {
		t.Fatalf("collision error does not expose a stable code: %v", err)
	}

	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "",
		map[string]any{"clientId": "pet-collision", "pairCode": "223347"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("original pairing changed after collision: status=%d body=%s", pair.Code, pair.Body.String())
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
	gateway.pruneDeviceMessagesLocked(state)
	if len(state.messages) != 0 || len(state.acked) != 0 {
		t.Fatalf("acknowledged queue was not pruned: messages=%#v acked=%#v", state.messages, state.acked)
	}
}

func TestDeviceGatewayQueuedMediaSurvivesCapacityEvictionUntilAck(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-queue-ref")
	state.capabilities = agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"}, Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true, DeliveryModes: []string{"url"}, MaxDownloadBytes: deviceGatewayMaxMediaBytes,
		},
	}})
	gateway.EnqueueReply("pet-queue-ref", "default", map[string]any{
		"type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("queued")),
	})
	gateway.mu.Lock()
	message := state.messages[0]
	mediaIDs := deviceMessageMediaIDs(message)
	if len(mediaIDs) != 1 || gateway.media[mediaIDs[0]] == nil || gateway.media[mediaIDs[0]].QueueRefs != 1 {
		gateway.mu.Unlock()
		t.Fatalf("queued media refs=%v media=%#v", mediaIDs, gateway.media)
	}
	protectedID := mediaIDs[0]
	protected := gateway.media[protectedID]
	protected.LastAccessedAt = time.Now().Add(-time.Hour)
	for i := 0; i < deviceGatewayMaxMediaObjectsPerClient-1; i++ {
		id := fmt.Sprintf("filler-%d", i)
		gateway.media[id] = &deviceMedia{ClientID: "pet-queue-ref", ID: id, Data: []byte{1}, Uploaded: true, LastAccessedAt: time.Now().Add(time.Duration(i) * time.Second)}
	}
	if !gateway.ensureMediaCapacityLocked("pet-queue-ref", 1, 0, "", time.Now().UTC()) {
		gateway.mu.Unlock()
		t.Fatal("capacity could not reclaim an unreferenced media object")
	}
	if gateway.media[protectedID] == nil {
		gateway.mu.Unlock()
		t.Fatal("capacity eviction removed media referenced by an outgoing message")
	}
	messageID, _ := message["id"].(string)
	state.acked[messageID] = true
	gateway.pruneDeviceMessagesLocked(state)
	if gateway.media[protectedID].QueueRefs != 0 {
		gateway.mu.Unlock()
		t.Fatalf("ACK left queue refs=%d", gateway.media[protectedID].QueueRefs)
	}
	if !gateway.evictOldestMediaLocked("pet-queue-ref", "") || gateway.media[protectedID] != nil {
		gateway.mu.Unlock()
		t.Fatal("unreferenced media could not be evicted after ACK")
	}
	gateway.mu.Unlock()
}

func TestDeviceGatewayQueueOverflowReleasesMediaReference(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-overflow-ref")
	media := &deviceMedia{ClientID: "pet-overflow-ref", ID: "overflow-media", Token: "token", Data: []byte("x"), Uploaded: true, LastAccessedAt: time.Now().UTC()}
	gateway.media[media.ID] = media
	gateway.queueDeviceMessageLocked(state, map[string]any{
		"seq": int64(1), "id": "media-message", "type": "audio",
		"url": "/api/im-gateway/v1/media/overflow-media?mediaToken=token",
	})
	for i := 0; i < deviceGatewayMaxQueuedMessages; i++ {
		gateway.queueDeviceMessageLocked(state, map[string]any{
			"seq": int64(i + 2), "id": fmt.Sprintf("text-%d", i), "type": "text", "text": "x",
		})
	}
	if media.QueueRefs != 0 {
		t.Fatalf("queue overflow left media refs=%d", media.QueueRefs)
	}
	if len(state.messages) != deviceGatewayMaxQueuedMessages || state.messages[0]["id"] == "media-message" {
		t.Fatalf("queue overflow retained dropped message: %#v", state.messages[0])
	}
}

func TestDeviceGatewayQueuedMediaGetsDeliveryGracePeriod(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-expiry-ref")
	now := time.Now().UTC()
	media := &deviceMedia{
		ClientID: "pet-expiry-ref", ID: "expiring-media", Token: "token", Data: []byte("x"),
		Uploaded: true, LastAccessedAt: now, ExpiresAt: now.Add(time.Second),
	}
	gateway.media[media.ID] = media
	gateway.queueDeviceMessageLocked(state, map[string]any{
		"seq": int64(1), "id": "media-message", "type": "audio",
		"url": "/api/im-gateway/v1/media/expiring-media?mediaToken=token",
	})
	if media.ExpiresAt.Before(now.Add(deviceGatewayQueuedMediaTTL - time.Second)) {
		t.Fatalf("queued media expiry=%s was not extended for delivery", media.ExpiresAt)
	}
	gateway.pruneMediaLocked(now.Add(2 * time.Second))
	if gateway.media[media.ID] == nil {
		t.Fatal("queued media expired before the delivery grace period")
	}
	state.acked["media-message"] = true
	gateway.pruneDeviceMessagesLocked(state)
	gateway.pruneMediaLocked(media.ExpiresAt.Add(time.Second))
	if gateway.media[media.ID] != nil {
		t.Fatal("unreferenced media survived beyond its delivery grace period")
	}
}

func TestDeviceGatewayOutgoingResponseIsolatedFromQueueMutation(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-clone")
	state.messages = []map[string]any{{
		"seq": int64(1), "id": "message-1", "type": "text", "text": "original",
		"extra": map[string]any{"nested": "original"},
	}}
	gateway.tokens["token"] = devicePrincipal{ClientID: "pet-clone", MachineID: "gui-a"}
	enabled := true
	gateway.hardware["gui-a"] = deviceHardwareConfig{Enabled: &enabled}

	response := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-clone&cursor=0", "token", nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(response.Body).Decode(&body)
	if len(body.Messages) != 1 {
		t.Fatalf("outgoing body=%s", response.Body.String())
	}
	body.Messages[0]["text"] = "mutated"
	body.Messages[0]["extra"].(map[string]any)["nested"] = "mutated"
	if state.messages[0]["text"] != "original" || state.messages[0]["extra"].(map[string]any)["nested"] != "original" {
		t.Fatalf("response mutation changed queued message: %#v", state.messages[0])
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
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
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

func TestDeviceGatewayHandshakeAdvertisesEnforcedPollLimits(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223361"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-poll-limits", "pairCode": "223361"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", paired["gatewayToken"].(string), map[string]any{"clientId": "pet-poll-limits"})
	var body struct {
		Poll struct {
			TimeoutSec    int `json:"timeoutSec"`
			MaxTimeoutSec int `json:"maxTimeoutSec"`
			MaxBatchSize  int `json:"maxBatchSize"`
			MaxLimit      int `json:"maxLimit"`
		} `json:"poll"`
	}
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if handshake.Code != http.StatusOK || body.Poll.TimeoutSec != 30 || body.Poll.MaxTimeoutSec != 30 || body.Poll.MaxBatchSize != 20 || body.Poll.MaxLimit != 20 {
		t.Fatalf("poll settings status=%d body=%s", handshake.Code, handshake.Body.String())
	}
}

func TestDeviceGatewayOutgoingClampsPollLimitAndTimeout(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "223362"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-poll-clamp", "pairCode": "223362"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token := paired["gatewayToken"].(string)
	for index := 0; index < 25; index++ {
		gateway.EnqueueReply("pet-poll-clamp", "default", map[string]any{"type": "text", "text": "queued"})
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-poll-clamp&cursor=0&limit=100&timeout=-1", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
		HasMore  bool             `json:"hasMore"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if poll.Code != http.StatusOK || len(body.Messages) != 20 || !body.HasMore {
		t.Fatalf("clamped poll status=%d body=%s", poll.Code, poll.Body.String())
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

func TestDeviceGatewayRestoresCredentialsAfterLocalHubReinstall(t *testing.T) {
	originalStore := &memoryDeviceCredentialStore{values: make(map[string]string)}
	original := NewPersistentDeviceGateway(nil, originalStore)
	if err := original.RegisterPairing("gui-a", "tenant-a", "user-a", "654320"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, original, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-reinstall", "pairCode": "654320"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	snapshot, err := original.ExportPersistedCredentials()
	if err != nil {
		t.Fatalf("ExportPersistedCredentials: %v", err)
	}

	// A reinstalled Hub starts with an empty database. It restores the opaque
	// snapshot before its ESP32 performs the next authenticated handshake.
	reinstalledStore := &memoryDeviceCredentialStore{values: make(map[string]string)}
	reinstalled := NewPersistentDeviceGateway(nil, reinstalledStore)
	called := 0
	if err := reinstalled.RestoreMissingCredentials(context.Background(), func(context.Context) (string, bool, error) {
		called++
		return snapshot, true, nil
	}); err != nil {
		t.Fatalf("RestoreMissingCredentials: %v", err)
	}
	if called != 1 {
		t.Fatalf("recovery calls=%d, want 1", called)
	}
	handshake := deviceGatewayRequest(t, reinstalled, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-reinstall"})
	if handshake.Code != http.StatusOK {
		t.Fatalf("recovered token rejected: status=%d body=%s", handshake.Code, handshake.Body.String())
	}

	// Existing local state remains authoritative and is never replaced by an
	// older remote recovery copy.
	if err := reinstalled.RestoreMissingCredentials(context.Background(), func(context.Context) (string, bool, error) {
		t.Fatal("recovery should not run when local credentials exist")
		return "", false, nil
	}); err != nil {
		t.Fatalf("unexpected local-state recovery error: %v", err)
	}
}

func TestDeviceGatewayRecoveryRestoresGUIIdentityAndHardwareCommandPath(t *testing.T) {
	ctx := context.Background()
	originalDB := newDeviceGatewayIdentityStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	// A newly created SQLite DB already contains this tenant. Deliberately
	// customize its old-Hub metadata to prove recovery does not confuse that
	// bootstrap row with a conflicting identity in the new database.
	tenant, err := originalDB.store.Tenants.GetByID(ctx, store.DefaultTenantID)
	if err != nil || tenant == nil {
		t.Fatalf("load default tenant: tenant=%#v err=%v", tenant, err)
	}
	if _, err := originalDB.provider.Write.ExecContext(ctx, `UPDATE tenants SET name = ?, settings_json = ? WHERE id = ?`, "Old Hub Default Tenant", `{"hardware":true}`, tenant.ID); err != nil {
		t.Fatalf("customize old default tenant: %v", err)
	}
	tenant, err = originalDB.store.Tenants.GetByID(ctx, tenant.ID)
	if err != nil || tenant == nil {
		t.Fatalf("reload default tenant: tenant=%#v err=%v", tenant, err)
	}
	user := &store.User{ID: "user-recovery", TenantID: tenant.ID, Email: "recovery@example.com", SN: "SN-recovery", Status: "active", EnrollmentStatus: "approved", EmailVerified: true, CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Users.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	const machineToken = "existing-gui-machine-token"
	machine := &store.Machine{ID: "machine-recovery", TenantID: tenant.ID, UserID: user.ID, ClientID: "gui-client-recovery", Name: "Recovery GUI", Platform: "windows", MachineTokenHash: tokenHashForDeviceGatewayTest(machineToken), Status: "offline", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Machines.Create(ctx, machine); err != nil {
		t.Fatalf("create machine: %v", err)
	}

	original := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	original.SetCredentialIdentityRepositories(originalDB.store.Tenants, originalDB.store.Users, originalDB.store.Machines)
	original.SetCredentialIdentityRestorer(sqlite.NewDeviceCredentialIdentityRestorer(originalDB.provider))
	if err := original.RegisterPairing(machine.ID, tenant.ID, user.ID, "654324"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, original, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-identity-recovery", "pairCode": "654324"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	hardwareToken, _ := paired["gatewayToken"].(string)
	snapshot, err := original.ExportPersistedCredentials()
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	var exported persistedDeviceCredentials
	if err := json.Unmarshal([]byte(snapshot), &exported); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if len(exported.Tenants) != 1 || len(exported.Users) != 1 || len(exported.Machines) != 1 || exported.Machines[0].MachineTokenHash != machine.MachineTokenHash {
		t.Fatalf("identity snapshot missing or unsafe: %#v", exported)
	}

	// Model a fully reinstalled Hub: brand-new SQLite and no local credentials.
	reinstalledDB := newDeviceGatewayIdentityStore(t)
	reinstalled := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	reinstalled.SetCredentialIdentityRepositories(reinstalledDB.store.Tenants, reinstalledDB.store.Users, reinstalledDB.store.Machines)
	reinstalledRestorer := sqlite.NewDeviceCredentialIdentityRestorer(reinstalledDB.provider)
	reinstalled.SetCredentialIdentityRestorer(reinstalledRestorer)
	reinstalled.SetCredentialSnapshotRestorer(reinstalledRestorer)
	if err := reinstalled.RestoreMissingCredentials(ctx, func(context.Context) (string, bool, error) { return snapshot, true, nil }); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}

	identity := auth.NewIdentityService(reinstalledDB.store.Users, reinstalledDB.store.Enrollments, reinstalledDB.store.EmailBlocks, reinstalledDB.store.Machines, reinstalledDB.store.ViewerTokens, reinstalledDB.store.LoginTokens, reinstalledDB.store.System, nil, "open", true, nil, "")
	principal, err := identity.AuthenticateMachine(ctx, machine.ID, machineToken)
	if err != nil || principal == nil || principal.TenantID != tenant.ID || principal.UserID != user.ID {
		t.Fatalf("existing GUI did not authenticate after recovery: principal=%#v err=%v", principal, err)
	}
	if handshake := deviceGatewayRequest(t, reinstalled, http.MethodPost, "/api/im-gateway/v1/handshake", hardwareToken, map[string]any{"clientId": "pet-identity-recovery"}); handshake.Code != http.StatusOK {
		t.Fatalf("recovered hardware bearer rejected: status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	if delivered := reinstalled.EnqueueMachineReplyCount(machine.ID, "control", map[string]any{"type": "text", "text": "recovered command"}); delivered != 1 {
		t.Fatalf("recovered GUI command delivery count=%d, want 1", delivered)
	}
	poll := deviceGatewayRequest(t, reinstalled, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-identity-recovery&cursor=0", hardwareToken, nil)
	if poll.Code != http.StatusOK || !strings.Contains(poll.Body.String(), "recovered command") {
		t.Fatalf("recovered command not delivered: status=%d body=%s", poll.Code, poll.Body.String())
	}
}

func TestDeviceGatewayRecoveryPersistsLegacyHardwareOnlySnapshot(t *testing.T) {
	db := newDeviceGatewayIdentityStore(t)
	legacyToken := "legacy-hardware-token"
	legacySnapshot, err := json.Marshal(persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			legacyToken: {
				ClientID:  "pet-legacy-recovery",
				MachineID: "legacy-machine",
				TenantID:  "tenant-legacy",
				UserID:    "user-legacy",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal legacy snapshot: %v", err)
	}

	// This models a Hub Center backup produced before GUI identity snapshots
	// were added. Recover it into a new SQLite database, then recreate the
	// gateway to prove the token was made durable rather than merely loaded in
	// memory for the current process.
	restorer := sqlite.NewDeviceCredentialIdentityRestorer(db.provider)
	recovered := NewPersistentDeviceGateway(nil, db.store.System)
	recovered.SetCredentialSnapshotRestorer(restorer)
	if err := recovered.RestorePersistedCredentials(string(legacySnapshot)); err != nil {
		t.Fatalf("restore legacy snapshot: %v", err)
	}
	restarted := NewPersistentDeviceGateway(nil, db.store.System)
	handshake := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", legacyToken, map[string]any{"clientId": "pet-legacy-recovery"})
	if handshake.Code != http.StatusOK {
		t.Fatalf("legacy recovered token did not survive restart: status=%d body=%s", handshake.Code, handshake.Body.String())
	}
}

func TestDeviceGatewayRecoveryRejectsIdentitySnapshotWithoutMachineClientID(t *testing.T) {
	db := newDeviceGatewayIdentityStore(t)
	snapshot, err := json.Marshal(persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			"hardware-token": {ClientID: "pet-missing-machine-client", MachineID: "machine-missing-client", TenantID: "tenant-missing-client", UserID: "user-missing-client"},
		},
		Tenants: []store.Tenant{{ID: "tenant-missing-client"}},
		Users:   []store.User{{ID: "user-missing-client", TenantID: "tenant-missing-client", Email: "missing-client@example.com"}},
		Machines: []store.Machine{{
			ID: "machine-missing-client", TenantID: "tenant-missing-client", UserID: "user-missing-client",
			MachineTokenHash: tokenHashForDeviceGatewayTest("machine-token"),
		}},
	})
	if err != nil {
		t.Fatalf("marshal malformed identity snapshot: %v", err)
	}

	gateway := NewPersistentDeviceGateway(nil, db.store.System)
	gateway.SetCredentialSnapshotRestorer(sqlite.NewDeviceCredentialIdentityRestorer(db.provider))
	if err := gateway.RestorePersistedCredentials(string(snapshot)); err == nil || !strings.Contains(err.Error(), "invalid recovered machine identity") {
		t.Fatalf("missing machine client ID recovery error=%v, want invalid identity", err)
	}
	if raw, err := db.store.System.Get(context.Background(), deviceGatewayCredentialsKey); err != nil || raw != "" {
		t.Fatalf("invalid snapshot was persisted: raw=%q err=%v", raw, err)
	}
}

func TestDeviceGatewayRecoveryNormalizesIdentitySnapshotBeforePersisting(t *testing.T) {
	db := newDeviceGatewayIdentityStore(t)
	snapshot, err := json.Marshal(persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			" hardware-token ": {ClientID: " pet-normalized ", MachineID: " machine-normalized ", TenantID: " tenant-normalized ", UserID: " user-normalized "},
		},
		Tenants: []store.Tenant{{ID: " tenant-normalized ", Slug: " normalized "}},
		Users:   []store.User{{ID: " user-normalized ", TenantID: " tenant-normalized ", Email: " normalized@example.com "}},
		Machines: []store.Machine{{
			ID: " machine-normalized ", TenantID: " tenant-normalized ", UserID: " user-normalized ", ClientID: " desktop-normalized ",
			MachineTokenHash: " " + tokenHashForDeviceGatewayTest("machine-token") + " ",
		}},
	})
	if err != nil {
		t.Fatalf("marshal whitespace snapshot: %v", err)
	}

	gateway := NewPersistentDeviceGateway(nil, db.store.System)
	gateway.SetCredentialSnapshotRestorer(sqlite.NewDeviceCredentialIdentityRestorer(db.provider))
	if err := gateway.RestorePersistedCredentials(string(snapshot)); err != nil {
		t.Fatalf("restore whitespace snapshot: %v", err)
	}
	restarted := NewPersistentDeviceGateway(nil, db.store.System)
	if handshake := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", "hardware-token", map[string]any{"clientId": "pet-normalized"}); handshake.Code != http.StatusOK {
		t.Fatalf("normalized recovered token rejected: status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	machine, err := db.store.Machines.GetByUserAndClientID(context.Background(), "user-normalized", "desktop-normalized")
	if err != nil || machine == nil {
		t.Fatalf("normalized machine was not restored: machine=%#v err=%v", machine, err)
	}
	if raw, err := db.store.System.Get(context.Background(), deviceGatewayCredentialsKey); err != nil || strings.Contains(raw, " hardware-token ") {
		t.Fatalf("recovered snapshot was not normalized: raw=%q err=%v", raw, err)
	}
}

func TestDeviceGatewayRecoveryNormalizesEveryWhitespaceTokenKey(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	snapshot := persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			" first-hardware-token ":  {ClientID: "pet-first", MachineID: "machine-whitespace-tokens", TenantID: "tenant-recovery", UserID: "user-recovery"},
			" second-hardware-token ": {ClientID: "pet-second", MachineID: "machine-whitespace-tokens", TenantID: "tenant-recovery", UserID: "user-recovery"},
		},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RestorePersistedCredentials(string(raw)); err != nil {
		t.Fatalf("restore whitespace token keys: %v", err)
	}
	if got := len(gateway.tokens); got != 2 {
		t.Fatalf("normalized token count=%d, want 2", got)
	}
	for _, token := range []string{"first-hardware-token", "second-hardware-token"} {
		if _, ok := gateway.tokens[token]; !ok {
			t.Fatalf("normalized token %q missing: %#v", token, gateway.tokens)
		}
	}
	if _, ok := gateway.tokens[" first-hardware-token "]; ok {
		t.Fatalf("whitespace token key survived normalization: %#v", gateway.tokens)
	}
}

func TestDeviceGatewayRecoveryRejectsMalformedHardwareStateBeforePersisting(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	base := persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			"hardware-token": {ClientID: "pet-recovery", MachineID: "machine-recovery", TenantID: "tenant-recovery", UserID: "user-recovery"},
		},
		MachineHardware: map[string]deviceHardwareConfig{
			"machine-recovery": {DeviceVolumes: map[string]int{"pet-recovery": 50}},
		},
	}
	tests := []struct {
		name    string
		mutate  func(*persistedDeviceCredentials)
		message string
	}{
		{
			name: "out of range volume",
			mutate: func(snapshot *persistedDeviceCredentials) {
				snapshot.MachineHardware["machine-recovery"] = deviceHardwareConfig{DeviceVolumes: map[string]int{"pet-recovery": 101}}
			},
			message: "invalid recovered hardware device volume",
		},
		{
			name: "orphaned client override",
			mutate: func(snapshot *persistedDeviceCredentials) {
				snapshot.MachineHardware["machine-recovery"] = deviceHardwareConfig{DeviceBrightness: map[string]int{"other-pet": 50}}
			},
			message: "recovered hardware setting is not owned by its machine",
		},
		{
			name: "invalid welcome audio",
			mutate: func(snapshot *persistedDeviceCredentials) {
				snapshot.MachineHardware["machine-recovery"] = deviceHardwareConfig{WelcomeEnabled: true, WelcomeAudio: "not-base64"}
			},
			message: "invalid recovered hardware welcome audio",
		},
		{
			name: "duplicate client bearer",
			mutate: func(snapshot *persistedDeviceCredentials) {
				snapshot.Tokens["second-hardware-token"] = devicePrincipal{ClientID: "pet-recovery", MachineID: "machine-recovery", TenantID: "tenant-recovery", UserID: "user-recovery"}
			},
			message: "duplicate recovered hardware client",
		},
		{
			name: "too many recovered machine bindings",
			mutate: func(snapshot *persistedDeviceCredentials) {
				for index := 0; index < maxMachineHardwareDevices; index++ {
					snapshot.Tokens[fmt.Sprintf("extra-hardware-token-%d", index)] = devicePrincipal{
						ClientID:  fmt.Sprintf("pet-recovery-%d", index),
						MachineID: "machine-recovery",
						TenantID:  "tenant-recovery",
						UserID:    "user-recovery",
					}
				}
			},
			message: "recovered hardware binding limit exceeded",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			snapshot.Tokens = make(map[string]devicePrincipal, len(base.Tokens))
			for token, principal := range base.Tokens {
				snapshot.Tokens[token] = principal
			}
			snapshot.MachineHardware = make(map[string]deviceHardwareConfig, len(base.MachineHardware))
			for machineID, config := range base.MachineHardware {
				snapshot.MachineHardware[machineID] = cloneDeviceHardwareConfig(config)
			}
			test.mutate(&snapshot)
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			gateway := NewPersistentDeviceGateway(nil, store)
			if err := gateway.RestorePersistedCredentials(string(raw)); err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("RestorePersistedCredentials error=%v, want %q", err, test.message)
			}
			if persisted := store.values[deviceGatewayCredentialsKey]; persisted != "" {
				t.Fatalf("invalid recovered snapshot was persisted: %s", persisted)
			}
		})
	}
}

func TestDeviceGatewayRecoveryRejectsOversizedSnapshotBeforePersisting(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	oversized := strings.Repeat(" ", deviceGatewayCredentialsMaxBytes+1)
	gateway := NewPersistentDeviceGateway(nil, store)

	err := gateway.RestorePersistedCredentials(oversized)
	if err == nil || !strings.Contains(err.Error(), "recovered device credentials exceed") {
		t.Fatalf("RestorePersistedCredentials error=%v, want snapshot size error", err)
	}
	if persisted := store.values[deviceGatewayCredentialsKey]; persisted != "" {
		t.Fatalf("oversized snapshot was persisted: %d bytes", len(persisted))
	}

	store.values[deviceGatewayCredentialsKey] = oversized
	restarted := NewPersistentDeviceGateway(nil, store)
	if devices := restarted.ListMachineDevices("machine-oversized"); len(devices) != 0 {
		t.Fatalf("oversized local credentials were loaded: %#v", devices)
	}
}

func TestNewPersistentDeviceGatewayRejectsMalformedLocalHardwareState(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	raw, err := json.Marshal(persistedDeviceCredentials{
		Tokens: map[string]devicePrincipal{
			"hardware-token": {ClientID: "pet-local-corrupt", MachineID: "machine-local-corrupt", TenantID: "tenant-local-corrupt", UserID: "user-local-corrupt"},
		},
		MachineHardware: map[string]deviceHardwareConfig{
			"machine-local-corrupt": {DeviceScreenSleepSeconds: map[string]int{"pet-local-corrupt": -1}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store.values[deviceGatewayCredentialsKey] = string(raw)

	gateway := NewPersistentDeviceGateway(nil, store)
	if devices := gateway.ListMachineDevices("machine-local-corrupt"); len(devices) != 0 {
		t.Fatalf("invalid local credentials were loaded: %#v", devices)
	}
	if response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", "hardware-token", map[string]any{"clientId": "pet-local-corrupt"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid local hardware token was accepted: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeviceGatewayRecoveryRejectsConflictingIdentityWithoutPartialRestore(t *testing.T) {
	ctx := context.Background()
	originalDB := newDeviceGatewayIdentityStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	tenant := &store.Tenant{ID: "tenant-conflict", Slug: "conflict", Name: "Conflict", Status: "active", SettingsJSON: "{}", CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Tenants.Create(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	user := &store.User{ID: "user-conflict", TenantID: tenant.ID, Email: "conflict@example.com", SN: "SN-conflict", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	machine := &store.Machine{ID: "machine-conflict", TenantID: tenant.ID, UserID: user.ID, ClientID: "gui-conflict", Name: "Conflict GUI", MachineTokenHash: tokenHashForDeviceGatewayTest("conflict-token"), Status: "offline", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Machines.Create(ctx, machine); err != nil {
		t.Fatal(err)
	}
	original := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	original.SetCredentialIdentityRepositories(originalDB.store.Tenants, originalDB.store.Users, originalDB.store.Machines)
	original.SetCredentialIdentityRestorer(sqlite.NewDeviceCredentialIdentityRestorer(originalDB.provider))
	if err := original.RegisterPairing(machine.ID, tenant.ID, user.ID, "654325"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, original, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-conflict", "pairCode": "654325"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	snapshot, err := original.ExportPersistedCredentials()
	if err != nil {
		t.Fatal(err)
	}

	localDB := newDeviceGatewayIdentityStore(t)
	// Same natural-key email but a different user ID models local state that
	// must never be overwritten by a remote recovery snapshot.
	if err := localDB.store.Tenants.Create(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := localDB.store.Users.Create(ctx, &store.User{ID: "other-user", TenantID: tenant.ID, Email: user.Email, SN: "SN-other", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reinstalled := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	reinstalled.SetCredentialIdentityRepositories(localDB.store.Tenants, localDB.store.Users, localDB.store.Machines)
	localRestorer := sqlite.NewDeviceCredentialIdentityRestorer(localDB.provider)
	reinstalled.SetCredentialIdentityRestorer(localRestorer)
	reinstalled.SetCredentialSnapshotRestorer(localRestorer)
	if err := reinstalled.RestoreMissingCredentials(ctx, func(context.Context) (string, bool, error) { return snapshot, true, nil }); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting recovery error=%v, want conflict", err)
	}
	if got := reinstalled.ListMachineDevices(machine.ID); len(got) != 0 {
		t.Fatalf("hardware credentials restored despite identity conflict: %#v", got)
	}
	if restored, err := localDB.store.Users.GetByID(ctx, user.ID); err != nil || restored != nil {
		t.Fatalf("recovery partially created user: user=%#v err=%v", restored, err)
	}
}

func TestDeviceGatewayRecoveryRejectsConflictingCustomTenantWithoutPartialRestore(t *testing.T) {
	ctx := context.Background()
	originalDB := newDeviceGatewayIdentityStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	tenant := &store.Tenant{ID: "tenant-custom-conflict", Slug: "custom-conflict", Name: "Original Tenant", Status: "active", SettingsJSON: `{"plan":"original"}`, CreatedByAdminID: "test", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Tenants.Create(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	user := &store.User{ID: "user-custom-conflict", TenantID: tenant.ID, Email: "custom-conflict@example.com", SN: "SN-custom-conflict", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Users.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	machine := &store.Machine{ID: "machine-custom-conflict", TenantID: tenant.ID, UserID: user.ID, ClientID: "gui-custom-conflict", Name: "Custom Conflict GUI", MachineTokenHash: tokenHashForDeviceGatewayTest("custom-conflict-token"), Status: "offline", CreatedAt: now, UpdatedAt: now}
	if err := originalDB.store.Machines.Create(ctx, machine); err != nil {
		t.Fatal(err)
	}
	original := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	original.SetCredentialIdentityRepositories(originalDB.store.Tenants, originalDB.store.Users, originalDB.store.Machines)
	if err := original.RegisterPairing(machine.ID, tenant.ID, user.ID, "654326"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, original, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-custom-conflict", "pairCode": "654326"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	snapshot, err := original.ExportPersistedCredentials()
	if err != nil {
		t.Fatal(err)
	}

	localDB := newDeviceGatewayIdentityStore(t)
	if err := localDB.store.Tenants.Create(ctx, &store.Tenant{ID: tenant.ID, Slug: tenant.Slug, Name: "Local Tenant", Status: "disabled", SettingsJSON: `{"plan":"local"}`, CreatedByAdminID: "local", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reinstalled := NewPersistentDeviceGateway(nil, &memoryDeviceCredentialStore{values: make(map[string]string)})
	reinstalled.SetCredentialIdentityRepositories(localDB.store.Tenants, localDB.store.Users, localDB.store.Machines)
	if err := reinstalled.RestoreMissingCredentials(ctx, func(context.Context) (string, bool, error) { return snapshot, true, nil }); err == nil || !strings.Contains(err.Error(), "tenant") || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("custom tenant conflict error=%v, want tenant conflict", err)
	}
	if got := reinstalled.ListMachineDevices(machine.ID); len(got) != 0 {
		t.Fatalf("hardware credentials restored despite custom tenant conflict: %#v", got)
	}
	if restored, err := localDB.store.Users.GetByID(ctx, user.ID); err != nil || restored != nil {
		t.Fatalf("custom tenant conflict partially created user: user=%#v err=%v", restored, err)
	}
}

func tokenHashForDeviceGatewayTest(raw string) string {
	// Authentication intentionally uses SHA-256 without a salt so the same
	// existing GUI token can be verified by a recovered machine record.
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func TestDeviceGatewayRecoveryErrorDoesNotResolveEmptyState(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	resolved, err := gateway.RestoreMissingCredentialsResult(context.Background(), func(context.Context) (string, bool, error) {
		return "", false, fmt.Errorf("hub center unavailable")
	})
	if err == nil || resolved {
		t.Fatalf("RestoreMissingCredentialsResult() = (%v, %v), want unresolved error", resolved, err)
	}
	if raw := store.values[deviceGatewayCredentialsKey]; raw != "" {
		t.Fatalf("failed recovery wrote local credentials: %s", raw)
	}
}

func TestDeviceGatewayCredentialBackupIsOrderedAndCoalescesLatestSnapshot(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	started := make(chan string, 1)
	releaseFirst := make(chan struct{})
	completed := make(chan string, 2)
	gateway.SetCredentialBackup(func(_ context.Context, snapshot string) error {
		started <- snapshot
		if len(completed) == 0 {
			<-releaseFirst
		}
		completed <- snapshot
		return nil
	})

	gateway.mu.Lock()
	gateway.tokens["first-token"] = devicePrincipal{ClientID: "pet", MachineID: "machine"}
	if err := gateway.persistTokensLocked(); err != nil {
		gateway.mu.Unlock()
		t.Fatalf("persist first snapshot: %v", err)
	}
	gateway.mu.Unlock()
	first := <-started

	gateway.mu.Lock()
	gateway.tokens["second-token"] = devicePrincipal{ClientID: "pet", MachineID: "machine"}
	if err := gateway.persistTokensLocked(); err != nil {
		gateway.mu.Unlock()
		t.Fatalf("persist second snapshot: %v", err)
	}
	gateway.tokens["third-token"] = devicePrincipal{ClientID: "pet", MachineID: "machine"}
	if err := gateway.persistTokensLocked(); err != nil {
		gateway.mu.Unlock()
		t.Fatalf("persist third snapshot: %v", err)
	}
	gateway.mu.Unlock()
	close(releaseFirst)

	select {
	case got := <-completed:
		if got != first {
			t.Fatal("first backup completion did not match its started snapshot")
		}
	case <-time.After(time.Second):
		t.Fatal("first backup did not complete")
	}
	select {
	case latest := <-completed:
		var saved persistedDeviceCredentials
		if err := json.Unmarshal([]byte(latest), &saved); err != nil {
			t.Fatalf("decode latest backup: %v", err)
		}
		if len(saved.Tokens) != 3 {
			t.Fatalf("latest backup tokens=%d, want 3", len(saved.Tokens))
		}
	case <-time.After(time.Second):
		t.Fatal("latest coalesced backup did not complete")
	}
	select {
	case unexpected := <-completed:
		t.Fatalf("unexpected redundant backup: %s", unexpected)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDeviceGatewayCredentialBackupOutboxSurvivesRestart(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	failed := make(chan struct{}, 1)
	gateway.SetCredentialBackup(func(context.Context, string) error {
		failed <- struct{}{}
		return fmt.Errorf("hub center unavailable")
	})
	gateway.mu.Lock()
	gateway.tokens["pending-token"] = devicePrincipal{ClientID: "pet", MachineID: "machine"}
	if err := gateway.persistTokensLocked(); err != nil {
		gateway.mu.Unlock()
		t.Fatalf("persist credentials: %v", err)
	}
	gateway.mu.Unlock()
	select {
	case <-failed:
	case <-time.After(time.Second):
		t.Fatal("expected failed backup attempt")
	}
	if strings.TrimSpace(store.values[deviceGatewayCredentialOutboxKey]) == "" {
		t.Fatal("pending backup snapshot was not persisted")
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	delivered := make(chan string, 1)
	restarted.SetCredentialBackup(func(_ context.Context, snapshot string) error {
		delivered <- snapshot
		return nil
	})
	select {
	case snapshot := <-delivered:
		var saved persistedDeviceCredentials
		if err := json.Unmarshal([]byte(snapshot), &saved); err != nil || len(saved.Tokens) != 1 {
			t.Fatalf("restored outbox snapshot = %q, err=%v", snapshot, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restarted gateway did not flush pending backup")
	}
	deadline := time.Now().Add(time.Second)
	for strings.TrimSpace(store.values[deviceGatewayCredentialOutboxKey]) != "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pending := store.values[deviceGatewayCredentialOutboxKey]; pending != "" {
		t.Fatalf("outbox not cleared after successful backup: %q", pending)
	}
}

func TestDeviceGatewayRestoreDoesNotOverwritePairingCompletedDuringFetch(t *testing.T) {
	remoteStore := &memoryDeviceCredentialStore{values: make(map[string]string)}
	remote := NewPersistentDeviceGateway(nil, remoteStore)
	if err := remote.RegisterPairing("gui-remote", "tenant-a", "user-remote", "654321"); err != nil {
		t.Fatal(err)
	}
	remotePair := deviceGatewayRequest(t, remote, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-remote", "pairCode": "654321"})
	if remotePair.Code != http.StatusCreated {
		t.Fatalf("remote pair status=%d body=%s", remotePair.Code, remotePair.Body.String())
	}
	snapshot, err := remote.ExportPersistedCredentials()
	if err != nil {
		t.Fatalf("ExportPersistedCredentials: %v", err)
	}

	localStore := &memoryDeviceCredentialStore{values: make(map[string]string)}
	local := NewPersistentDeviceGateway(nil, localStore)
	if err := local.RegisterPairing("gui-local", "tenant-a", "user-local", "654322"); err != nil {
		t.Fatal(err)
	}
	restoreStarted := make(chan struct{})
	allowRestore := make(chan struct{})
	restoreDone := make(chan error, 1)
	go func() {
		restoreDone <- local.RestoreMissingCredentials(context.Background(), func(context.Context) (string, bool, error) {
			close(restoreStarted)
			<-allowRestore
			return snapshot, true, nil
		})
	}()
	<-restoreStarted
	localPair := deviceGatewayRequest(t, local, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-local", "pairCode": "654322"})
	if localPair.Code != http.StatusCreated {
		t.Fatalf("local pair status=%d body=%s", localPair.Code, localPair.Body.String())
	}
	var paired map[string]any
	_ = json.NewDecoder(localPair.Body).Decode(&paired)
	localToken, _ := paired["gatewayToken"].(string)
	close(allowRestore)
	if err := <-restoreDone; err != nil {
		t.Fatalf("RestoreMissingCredentials: %v", err)
	}
	if handshake := deviceGatewayRequest(t, local, http.MethodPost, "/api/im-gateway/v1/handshake", localToken, map[string]any{"clientId": "pet-local"}); handshake.Code != http.StatusOK {
		t.Fatalf("local pairing was overwritten by recovery: status=%d body=%s", handshake.Code, handshake.Body.String())
	}
}

func TestDeviceGatewayListsMultipleBindingsAndDeleteRevokesOnlyTarget(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	pairDevice := func(code, clientID, name string) string {
		if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", code); err != nil {
			t.Fatal(err)
		}
		pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "pairCode": code})
		var body map[string]any
		_ = json.NewDecoder(pair.Body).Decode(&body)
		token, _ := body["gatewayToken"].(string)
		handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": clientID, "clientName": name, "protocolVersion": "1.1"})
		if handshake.Code != http.StatusOK {
			t.Fatalf("handshake %s: %s", clientID, handshake.Body.String())
		}
		return token
	}
	tokenA := pairDevice("610001", "pet-a", "Kitchen Pet")
	tokenB := pairDevice("610002", "pet-b", "Desk Pet")
	devices := gateway.ListMachineDevices("gui-a")
	if len(devices) != 2 {
		t.Fatalf("devices=%#v", devices)
	}
	if err := gateway.DeleteMachineDevice("gui-a", "pet-a"); err != nil {
		t.Fatal(err)
	}
	if owner := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenA, map[string]any{"clientId": "pet-a"}); owner.Code != http.StatusUnauthorized {
		t.Fatalf("deleted token status=%d", owner.Code)
	}
	if kept := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenB, map[string]any{"clientId": "pet-b"}); kept.Code != http.StatusOK {
		t.Fatalf("other token status=%d body=%s", kept.Code, kept.Body.String())
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	if got := restarted.ListMachineDevices("gui-a"); len(got) != 1 || got[0].ClientID != "pet-b" {
		t.Fatalf("persisted devices=%#v", got)
	}
}

func TestDeviceGatewayDeleteSuppressesLateAcceptedRelay(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	inboundMessages := make(chan IncomingMessage, 1)
	plugin.messageHandler = func(msg IncomingMessage) { inboundMessages <- msg }
	plugin.mu.Unlock()

	gateway := NewDeviceGateway(plugin)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610003"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-a", "pairCode": "610003"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if token == "" {
		t.Fatalf("missing gateway token: %s", pair.Body.String())
	}

	// Model an HTTP request which completed validation immediately before the
	// owner unbound its hardware. The late goroutine must re-check the bearer
	// rather than letting the old request create a GUI Agent after deletion.
	payload, err := json.Marshal(map[string]any{
		"tenant_id": "tenant-a", "platform_uid": "thirdparty:pet-a:default",
		"reply_target": "thirdparty:pet-a:default", "message_id": "late-1",
		"message_type": "text", "text": "must not reach GUI",
		"client_tool_context": agent.ClientToolContext{ClientID: "pet-a", ConversationID: "default", ReplyToMessageID: "late-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.DeleteMachineDevice("gui-a", "pet-a"); err != nil {
		t.Fatal(err)
	}
	gateway.forwardIncomingDeviceMessage(gateway.deviceDispatchMutex("pet-a"), token, devicePrincipal{ClientID: "pet-a"}, "gui-a", payload)
	select {
	case received := <-inboundMessages:
		t.Fatalf("deleted device was relayed to GUI: %#v", received)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDeviceGatewayMigratesExplicitLegacyMachineBindings(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("legacy-client", "tenant-a", "user-a", "610101"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "legacy-pet", "pairCode": "610101"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	if err := gateway.UpdateMachineDeviceVolume("legacy-client", "legacy-pet", 37); err != nil {
		t.Fatal(err)
	}
	if err := gateway.MigrateMachineHardwareBindings("machine-current", "tenant-a", "user-a", []string{"legacy-client"}); err != nil {
		t.Fatal(err)
	}
	if old := gateway.ListMachineDevices("legacy-client"); len(old) != 0 {
		t.Fatalf("legacy owner still lists devices: %#v", old)
	}
	devices := gateway.ListMachineDevices("machine-current")
	if len(devices) != 1 || devices[0].ClientID != "legacy-pet" || devices[0].Volume == nil || *devices[0].Volume != 37 {
		t.Fatalf("migrated devices=%#v", devices)
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	if got := restarted.ListMachineDevices("machine-current"); len(got) != 1 || got[0].ClientID != "legacy-pet" || got[0].Volume == nil || *got[0].Volume != 37 {
		t.Fatalf("persisted migrated devices=%#v", got)
	}
}

func TestDeviceGatewayMigrationMovesLiveDeviceMediaToCurrentMachine(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("legacy-client", "tenant-a", "user-a", "610106"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "legacy-media-pet", "pairCode": "610106"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}

	gateway.mu.Lock()
	gateway.media["legacy-media"] = &deviceMedia{
		ID: "legacy-media", ClientID: "legacy-media-pet", MachineID: "legacy-client", Token: "media-token",
		Data: []byte("media"), Uploaded: true, LastAccessedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	gateway.mu.Unlock()
	if err := gateway.MigrateMachineHardwareBindings("machine-current", "tenant-a", "user-a", []string{"legacy-client"}); err != nil {
		t.Fatal(err)
	}
	if got := gateway.media["legacy-media"].MachineID; got != "machine-current" {
		t.Fatalf("migrated media owner=%q, want machine-current", got)
	}
	if err := gateway.UpdateMachineHardwareEnabled("legacy-client", false); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.mediaForDownload(httptest.NewRequest(http.MethodGet, "/api/im-gateway/v1/media/legacy-media?mediaToken=media-token", nil), "legacy-media"); err != nil {
		t.Fatalf("media incorrectly remained gated by legacy hardware: %v", err)
	}
	if err := gateway.UpdateMachineHardwareEnabled("machine-current", false); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.mediaForDownload(httptest.NewRequest(http.MethodGet, "/api/im-gateway/v1/media/legacy-media?mediaToken=media-token", nil), "legacy-media"); err == nil {
		t.Fatal("media was not gated by current hardware")
	}
}

func TestDeviceGatewayDoesNotMigrateAnotherOwnerOrMergeActiveMachine(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	pair := func(machineID, tenantID, userID, code, clientID string) {
		if err := gateway.RegisterPairing(machineID, tenantID, userID, code); err != nil {
			t.Fatal(err)
		}
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "pairCode": code})
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d", clientID, response.Code)
		}
	}
	pair("legacy-other-user", "tenant-a", "user-b", "610102", "other-user-pet")
	pair("legacy-same-user", "tenant-a", "user-a", "610103", "same-user-pet")
	pair("machine-current", "tenant-a", "user-a", "610104", "current-pet")
	if err := gateway.MigrateMachineHardwareBindings("machine-current", "tenant-a", "user-a", []string{"legacy-other-user", "legacy-same-user"}); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("machine-current")
	if len(devices) != 1 || devices[0].ClientID != "current-pet" {
		t.Fatalf("active machine was merged: %#v", devices)
	}
	if got := gateway.ListMachineDevices("legacy-other-user"); len(got) != 1 || got[0].ClientID != "other-user-pet" {
		t.Fatalf("different owner migrated: %#v", got)
	}
	if got := gateway.ListMachineDevices("legacy-same-user"); len(got) != 1 || got[0].ClientID != "same-user-pet" {
		t.Fatalf("legacy binding unexpectedly moved: %#v", got)
	}
}

func TestDeviceGatewaySelectedPreviewReportsOfflineRatherThanOwnershipFailure(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("machine-recovered", "tenant-a", "user-a", "610108"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-recovered", "pairCode": "610108"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}

	result := gateway.EnqueueMachineClientReplyResult("machine-recovered", "pet-recovered", "system", map[string]any{
		"reply_type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("wav")),
		"extra": map[string]any{"hardware_audio_preview": true},
	})
	if result.Queued != 0 || result.Reason != MachineClientReplyOffline {
		t.Fatalf("selected offline preview result=%+v", result)
	}
}

func TestDeviceGatewayMigrationMergesOnlyMovedDeviceVolumesIntoCurrentConfig(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	pair := func(machineID, code, clientID string) {
		if err := gateway.RegisterPairing(machineID, "tenant-a", "user-a", code); err != nil {
			t.Fatal(err)
		}
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "pairCode": code})
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", clientID, response.Code, response.Body.String())
		}
	}
	pair("legacy-client", "610105", "legacy-pet")
	if err := gateway.UpdateMachineDeviceVolume("legacy-client", "legacy-pet", 37); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceScreenSleepTimeout("legacy-client", "legacy-pet", 1800); err != nil {
		t.Fatal(err)
	}
	// An empty current machine may already have durable settings from the GUI.
	// Those settings remain authoritative while the old device volume moves.
	if err := gateway.UpdateMachineAllowCustomPets("machine-current", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.MigrateMachineHardwareBindings("machine-current", "tenant-a", "user-a", []string{"legacy-client"}); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("machine-current")
	volumes := map[string]int{}
	for _, device := range devices {
		if device.Volume != nil {
			volumes[device.ClientID] = *device.Volume
		}
	}
	if len(devices) != 1 || volumes["legacy-pet"] != 37 || devices[0].ScreenSleepSeconds == nil || *devices[0].ScreenSleepSeconds != 1800 || !gateway.hardware["machine-current"].AllowCustomPets {
		t.Fatalf("merged devices=%#v volumes=%#v", devices, volumes)
	}
	if _, ok := gateway.hardware["legacy-client"]; ok {
		t.Fatalf("orphaned legacy hardware config=%#v", gateway.hardware["legacy-client"])
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	devices = restarted.ListMachineDevices("machine-current")
	volumes = map[string]int{}
	for _, device := range devices {
		if device.Volume != nil {
			volumes[device.ClientID] = *device.Volume
		}
	}
	if len(devices) != 1 || volumes["legacy-pet"] != 37 || devices[0].ScreenSleepSeconds == nil || *devices[0].ScreenSleepSeconds != 1800 || !restarted.hardware["machine-current"].AllowCustomPets {
		t.Fatalf("persisted merged devices=%#v volumes=%#v", devices, volumes)
	}
}

func TestDeviceGatewayDeleteRollsBackWhenPersistenceFails(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610003"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-rollback", "pairCode": "610003"})
	var body map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&body)
	token, _ := body["gatewayToken"].(string)
	gateway.EnqueueReply("pet-rollback", "default", map[string]any{"type": "text", "text": "keep me"})
	store.setErr = fmt.Errorf("store unavailable")
	if err := gateway.DeleteMachineDevice("gui-a", "pet-rollback"); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if devices := gateway.ListMachineDevices("gui-a"); len(devices) != 1 || devices[0].ClientID != "pet-rollback" {
		t.Fatalf("binding was not rolled back: %#v", devices)
	}
	if state := gateway.clients["pet-rollback"]; state == nil || len(state.messages) != 1 {
		t.Fatalf("client queue was not rolled back: %#v", state)
	}
	if response := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-rollback&cursor=0", token, nil); response.Code != http.StatusOK {
		t.Fatalf("rolled-back token status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeviceGatewayHardwareConfigRollsBackWhenPersistenceFails(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.UpdateMachineVolume("gui-a", 20); err != nil {
		t.Fatal(err)
	}
	store.setErr = fmt.Errorf("store unavailable")
	if err := gateway.UpdateMachineVolume("gui-a", 80); err == nil {
		t.Fatal("volume update unexpectedly succeeded")
	}
	if got := gateway.hardware["gui-a"].Volume; got == nil || *got != 20 {
		t.Fatalf("volume was not rolled back: %#v", got)
	}
	if err := gateway.UpdateMachineWelcome("gui-new", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err == nil {
		t.Fatal("welcome update unexpectedly succeeded")
	}
	if _, ok := gateway.hardware["gui-new"]; ok {
		t.Fatalf("failed new hardware config was retained: %#v", gateway.hardware["gui-new"])
	}
}

func TestDeviceGatewayHardwareEnabledPersistsAndGatesESP32Traffic(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610006"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-switch", "pairCode": "610006"})
	var body map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&body)
	token, _ := body["gatewayToken"].(string)

	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	disabled := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-switch"})
	if disabled.Code != http.StatusServiceUnavailable || !strings.Contains(disabled.Body.String(), "hardware_disabled") {
		t.Fatalf("disabled handshake=%d %s", disabled.Code, disabled.Body.String())
	}
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	enabled := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-switch"})
	if enabled.Code != http.StatusOK {
		t.Fatalf("enabled handshake=%d %s", enabled.Code, enabled.Body.String())
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	if !restarted.machineHardwareEnabled("gui-a") {
		t.Fatal("enabled hardware state was not persisted")
	}
	if err := restarted.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	poll := deviceGatewayRequest(t, restarted, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-switch&cursor=0", token, nil)
	if poll.Code != http.StatusServiceUnavailable || !strings.Contains(poll.Body.String(), "hardware_disabled") {
		t.Fatalf("disabled poll=%d %s", poll.Code, poll.Body.String())
	}
	if devices := restarted.ListMachineDevices("gui-a"); len(devices) != 1 || devices[0].ClientID != "pet-switch" {
		t.Fatalf("disabling hardware removed pairing: %#v", devices)
	}
}

func TestDeviceGatewayHardwareDisabledBlocksPreparedMediaTransfer(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610016"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-media-switch", "pairCode": "610016"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{
		"clientId": "pet-media-switch", "type": "voice", "fileName": "voice.wav", "mimeType": "audio/wav", "sizeBytes": 3,
	})
	var prepared struct {
		Media struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"media"`
		Upload struct {
			URL string `json:"url"`
		} `json:"upload"`
	}
	_ = json.NewDecoder(prepare.Body).Decode(&prepared)
	if prepared.Media.ID == "" || prepared.Upload.URL == "" {
		t.Fatalf("prepared media=%#v body=%s", prepared, prepare.Body.String())
	}
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	uploadRequest := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, bytes.NewBufferString("wav"))
	uploadRequest.ContentLength = 3
	if err := gateway.storeMediaUpload(uploadRequest, prepared.Media.ID); err == nil || !strings.Contains(err.Error(), "hardware is disabled") {
		t.Fatalf("disabled upload error=%v", err)
	}

	// A media object uploaded while enabled must also stop being downloadable
	// after the master switch turns off.
	gateway.mu.Lock()
	media := gateway.media[prepared.Media.ID]
	media.Data = []byte("wav")
	media.Uploaded = true
	gateway.mu.Unlock()
	downloadRequest := httptest.NewRequest(http.MethodGet, prepared.Media.URL, nil)
	if _, err := gateway.mediaForDownload(downloadRequest, prepared.Media.ID); err == nil {
		t.Fatal("disabled hardware media remained downloadable")
	}
}

func TestDeviceGatewayHardwareDisabledWakesPendingLongPoll(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610017"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-poll-switch", "pairCode": "610017"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-poll-switch&cursor=0&timeout=30", token, nil)
	}()
	select {
	case <-done:
		t.Fatal("long poll returned before hardware state changed")
	case <-time.After(50 * time.Millisecond):
	}

	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "hardware_disabled") {
			t.Fatalf("disabled long poll=%d %s", response.Code, response.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("disabling hardware did not wake the pending long poll")
	}

	gateway.EnqueueMachineReply("gui-a", "system", map[string]any{"reply_type": "text", "text": "must not queue"})
	gateway.mu.Lock()
	queued := len(gateway.clientLocked("pet-poll-switch").messages)
	gateway.mu.Unlock()
	if queued != 0 {
		t.Fatalf("disabled hardware accepted %d queued messages", queued)
	}
}

func TestDeviceGatewayGeneratedMediaInheritsMachineHardwareGate(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610018"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-generated-media", "pairCode": "610018"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	gatewayToken, _ := paired["gatewayToken"].(string)
	if strings.TrimSpace(gatewayToken) == "" {
		t.Fatal("pairing did not return a gateway token")
	}

	state := gateway.clientLocked("pet-generated-media")
	state.capabilities = agent.NormalizeClientCapabilities(&agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"}, Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true, DeliveryModes: []string{"url"}, MaxDownloadBytes: deviceGatewayMaxMediaBytes,
		},
	}})
	gateway.EnqueueReply("pet-generated-media", "default", map[string]any{
		"type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("generated")),
	})
	gateway.mu.Lock()
	mediaIDs := deviceMessageMediaIDs(state.messages[0])
	media := gateway.media[mediaIDs[0]]
	gateway.mu.Unlock()
	if media == nil || media.MachineID != "gui-a" {
		t.Fatalf("generated media ownership=%#v", media)
	}
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, state.messages[0]["url"].(string), nil)
	if _, err := gateway.mediaForDownload(request, media.ID); err == nil {
		t.Fatal("generated media remained downloadable after hardware was disabled")
	}
}

func TestDeviceGatewayHardwareEnabledRollsBackWhenPersistenceFails(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	store.setErr = fmt.Errorf("store unavailable")
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", false); err == nil {
		t.Fatal("hardware state update unexpectedly succeeded")
	}
	if !gateway.machineHardwareEnabled("gui-a") {
		t.Fatal("hardware state was not rolled back")
	}
}

func TestDeviceGatewayPairingTransfersClientIDToNewMachine(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	pair := func(machineID, code string) *httptest.ResponseRecorder {
		if err := gateway.RegisterPairing(machineID, "tenant-a", "user-a", code); err != nil {
			t.Fatal(err)
		}
		return deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "shared-physical-id", "pairCode": code})
	}
	first := pair("gui-a", "610004")
	var firstBody map[string]any
	_ = json.NewDecoder(first.Body).Decode(&firstBody)
	firstToken, _ := firstBody["gatewayToken"].(string)
	second := pair("gui-b", "610005")
	if second.Code != http.StatusCreated {
		t.Fatalf("transfer status=%d body=%s", second.Code, second.Body.String())
	}
	if response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", firstToken, map[string]any{"clientId": "shared-physical-id"}); response.Code != http.StatusUnauthorized {
		t.Fatalf("old token remained valid: status=%d body=%s", response.Code, response.Body.String())
	}
	if got := gateway.ListMachineDevices("gui-a"); len(got) != 0 {
		t.Fatalf("old machine retained transferred binding: %#v", got)
	}
	if got := gateway.ListMachineDevices("gui-b"); len(got) != 1 || got[0].ClientID != "shared-physical-id" {
		t.Fatalf("new machine did not acquire binding: %#v", got)
	}
}

func TestDeviceGatewayPairingReservationSurvivesHubRestart(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	original := NewPersistentDeviceGateway(nil, store)
	if err := original.RegisterPairing("gui-a", "tenant-a", "user-a", "610007"); err != nil {
		t.Fatal(err)
	}
	if raw := store.values[deviceGatewayPairingsKey]; !strings.Contains(raw, "610007") {
		t.Fatalf("pairing reservation was not persisted: %q", raw)
	}

	restarted := NewPersistentDeviceGateway(nil, store)
	paired := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-restart-pair", "pairCode": "610007"})
	if paired.Code != http.StatusCreated {
		t.Fatalf("restarted pair status=%d body=%s", paired.Code, paired.Body.String())
	}
	if raw := store.values[deviceGatewayPairingsKey]; strings.Contains(raw, "610007") {
		t.Fatalf("successful pairing reservation remained durable: %q", raw)
	}

	restartedAgain := NewPersistentDeviceGateway(nil, store)
	reused := deviceGatewayRequest(t, restartedAgain, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-restart-reuse", "pairCode": "610007"})
	if reused.Code != http.StatusUnauthorized {
		t.Fatalf("reused persisted pair status=%d body=%s", reused.Code, reused.Body.String())
	}
}

func TestDeviceGatewayPairingReservationDropsExpiredRecordOnRestart(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: map[string]string{
		deviceGatewayPairingsKey: `{"610009":{"machineId":"gui-a","tenantId":"tenant-a","userId":"user-a","pet":{"skin":"clawmate"},"expiresAt":"2000-01-01T00:00:00Z"}}`,
	}}
	restarted := NewPersistentDeviceGateway(nil, store)
	response := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-expired-restart", "pairCode": "610009"})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired restart pairing status=%d body=%s", response.Code, response.Body.String())
	}
	if raw := store.values[deviceGatewayPairingsKey]; strings.Contains(raw, "610009") {
		t.Fatalf("expired pairing reservation was not compacted: %q", raw)
	}
}
func TestDeviceGatewayPairingCodeSurvivesTransientPersistenceFailure(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610008"); err != nil {
		t.Fatal(err)
	}
	store.setErr = fmt.Errorf("store unavailable")
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-retry-pair", "pairCode": "610008"})
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first pair status=%d body=%s", first.Code, first.Body.String())
	}
	if _, ok := gateway.pairings["610008"]; !ok {
		t.Fatal("pairing code was consumed before credential persistence succeeded")
	}
	store.setErr = nil
	second := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-retry-pair", "pairCode": "610008"})
	if second.Code != http.StatusCreated {
		t.Fatalf("retry pair status=%d body=%s", second.Code, second.Body.String())
	}
	if _, ok := gateway.pairings["610008"]; ok {
		t.Fatal("successful one-time pairing code remained usable")
	}
	third := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-other", "pairCode": "610008"})
	if third.Code != http.StatusUnauthorized {
		t.Fatalf("reused pair status=%d body=%s", third.Code, third.Body.String())
	}
}

func TestDeviceGatewayPairingReservationRestoresAfterSuccessfulRollbackPersistence(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610013"); err != nil {
		t.Fatal(err)
	}

	// Let deletion of the reservation reach storage, then fail only the token
	// write. The rollback must restore the code in memory and persist it before
	// a process restart can lose it.
	store.setErr = fmt.Errorf("store unavailable")
	store.setErrKey = deviceGatewayCredentialsKey
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-rollback-persist", "pairCode": "610013"})
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first pair status=%d body=%s", first.Code, first.Body.String())
	}
	if _, ok := gateway.pairings["610013"]; !ok {
		t.Fatal("pairing code was not restored in memory")
	}

	store.setErr = nil
	store.setErrKey = ""
	restarted := NewPersistentDeviceGateway(nil, store)
	second := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-rollback-persist", "pairCode": "610013"})
	if second.Code != http.StatusCreated {
		t.Fatalf("retry pair status=%d body=%s", second.Code, second.Body.String())
	}
}
func TestDeviceGatewayPairingCodeAllowsOnlyOneConcurrentExchange(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610012"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, clientID := range []string{"pet-concurrent-a", "pet-concurrent-b"} {
		clientID := clientID
		go func() {
			<-start
			response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "pairCode": "610012"})
			results <- response.Code
		}()
	}
	close(start)
	created, rejected := 0, 0
	for range 2 {
		switch <-results {
		case http.StatusCreated:
			created++
		case http.StatusUnauthorized:
			rejected++
		}
	}
	if created != 1 || rejected != 1 || len(gateway.tokens) != 1 {
		t.Fatalf("concurrent exchange created=%d rejected=%d tokens=%d", created, rejected, len(gateway.tokens))
	}
}

func TestDeviceGatewayPairingValidatesClientIDBeforeConsumingCode(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610009"); err != nil {
		t.Fatal(err)
	}
	tooLong := strings.Repeat("a", coreim.ThirdPartyMaxIDChars+1)
	invalid := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": tooLong, "pairCode": "610009"})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid client status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	valid := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-valid", "pairCode": "610009"})
	if valid.Code != http.StatusCreated {
		t.Fatalf("valid retry status=%d body=%s", valid.Code, valid.Body.String())
	}
}

func TestDeviceGatewayPairingRejectsSixthDeviceUntilOneIsRemoved(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	pair := func(clientID, code string) *httptest.ResponseRecorder {
		if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", code); err != nil {
			t.Fatal(err)
		}
		return deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "pairCode": code})
	}
	for i := 1; i <= 5; i++ {
		response := pair(fmt.Sprintf("pet-%d", i), fmt.Sprintf("%06d", 620000+i))
		if response.Code != http.StatusCreated {
			t.Fatalf("pair device %d status=%d body=%s", i, response.Code, response.Body.String())
		}
	}
	sixth := pair("pet-6", "620006")
	if sixth.Code != http.StatusConflict || !strings.Contains(sixth.Body.String(), "hardware_device_limit_reached") || !strings.Contains(sixth.Body.String(), "remove a bound device") {
		t.Fatalf("sixth device status=%d body=%s", sixth.Code, sixth.Body.String())
	}
	if got := gateway.MachineHardwareBindingState("gui-a"); got.MaxDevices != 5 || got.BoundCount != 5 {
		t.Fatalf("binding state after rejection=%+v", got)
	}
	if err := gateway.DeleteMachineDevice("gui-a", "pet-3"); err != nil {
		t.Fatal(err)
	}
	if response := pair("pet-6", "620007"); response.Code != http.StatusCreated {
		t.Fatalf("pair after removal status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDeviceGatewayRepairingPurgesPreviousClientMedia(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610010"); err != nil {
		t.Fatal(err)
	}
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-media-repair", "pairCode": "610010"})
	var firstBody map[string]any
	_ = json.NewDecoder(first.Body).Decode(&firstBody)
	token, _ := firstBody["gatewayToken"].(string)
	prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{"clientId": "pet-media-repair", "type": "voice", "fileName": "voice.wav", "mimeType": "audio/wav", "sizeBytes": 3})
	var prepared struct {
		Media struct {
			ID string `json:"id"`
		} `json:"media"`
	}
	_ = json.NewDecoder(prepare.Body).Decode(&prepared)
	if prepared.Media.ID == "" || gateway.media[prepared.Media.ID] == nil {
		t.Fatalf("prepared media=%s body=%s", prepared.Media.ID, prepare.Body.String())
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610011"); err != nil {
		t.Fatal(err)
	}
	repaired := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-media-repair", "pairCode": "610011"})
	if repaired.Code != http.StatusCreated {
		t.Fatalf("repair status=%d body=%s", repaired.Code, repaired.Body.String())
	}
	if gateway.media[prepared.Media.ID] != nil {
		t.Fatal("media authorized by the revoked credential survived re-pairing")
	}
}

func TestDeviceGatewayMachineClientReplyRequiresOwnership(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	for _, pairing := range []struct {
		machineID string
		code      string
		clientID  string
	}{{"gui-a", "610006", "pet-a"}, {"gui-b", "610007", "pet-b"}} {
		if err := gateway.RegisterPairing(pairing.machineID, "tenant-a", "user-a", pairing.code); err != nil {
			t.Fatal(err)
		}
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": pairing.clientID, "pairCode": pairing.code})
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", pairing.clientID, response.Code, response.Body.String())
		}
	}
	if queued := gateway.EnqueueMachineClientReplyCount("gui-a", "pet-b", "default", map[string]any{"type": "text", "text": "blocked"}); queued != 0 {
		t.Fatalf("cross-machine reply queued=%d", queued)
	}
	if state := gateway.clients["pet-b"]; state != nil && len(state.messages) != 0 {
		t.Fatalf("cross-machine reply reached target: %#v", state.messages)
	}
	if queued := gateway.EnqueueMachineClientReplyCount("gui-a", "pet-a", "default", map[string]any{"type": "text", "text": "allowed"}); queued != 1 {
		t.Fatalf("owned reply queued=%d, want 1", queued)
	}
}

func TestDeviceGatewayHandshakePublishesPetAssetByReference(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	rawFrame := bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32)
	asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, FrameMS: 450, Data: base64.StdEncoding.EncodeToString(rawFrame)}
	for i := 1; i < devicePetAssetMaxFrames; i++ {
		asset.Frames = append(asset.Frames, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{byte(i), byte(i * 3), 0x80}, 32*32)))
	}
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
		"clientId": "pet-asset", "capabilities": map[string]any{"features": map[string]any{"petAsset": true, "petAnimation": true, "petAssetMaxFrames": devicePetAssetMaxFrames}},
	})
	var assetBody struct {
		Pet      devicePetProfile        `json:"pet"`
		PetAsset devicePetAssetReference `json:"petAsset"`
	}
	_ = json.NewDecoder(assetRenderer.Body).Decode(&assetBody)
	if assetBody.Pet.Asset != nil || len(assetBody.PetAsset.URLs) != devicePetAssetMaxFrames || assetBody.PetAsset.FrameMS != 450 || assetBody.PetAsset.Revision == "" {
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

func TestDeviceGatewayLegacyPetAssetClientReceivesTwoFrames(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	frame := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32))
	asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, FrameMS: 450, Data: frame}
	asset.Frames = make([]string, devicePetAssetMaxFrames-1)
	for index := range asset.Frames {
		asset.Frames[index] = frame
	}
	gateway.mu.Lock()
	reference := gateway.preparePetAssetLocked("legacy-pet", asset, true, 0)
	gateway.mu.Unlock()
	if reference == nil || len(reference.URLs) != 2 {
		t.Fatalf("legacy reference=%#v", reference)
	}
	if reference.FrameMS != 1800 {
		t.Fatalf("legacy frameMs=%d, want 1800", reference.FrameMS)
	}
}

func TestDevicePetAssetFrameRateChangesRevisionAndProfile(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	frame := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32))
	slow := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, FrameMS: 450, Data: frame}
	fast := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, FrameMS: 225, Data: frame}
	if devicePetAssetsEqual(slow, fast) {
		t.Fatal("assets with different frame rates compare equal")
	}
	gateway.mu.Lock()
	slowRef := gateway.preparePetAssetLocked("rate-pet", slow, false, 0)
	fastRef := gateway.preparePetAssetLocked("rate-pet", fast, false, 0)
	gateway.mu.Unlock()
	if slowRef == nil || fastRef == nil || slowRef.Revision == fastRef.Revision {
		t.Fatalf("frame rate did not change revision: slow=%#v fast=%#v", slowRef, fastRef)
	}
}
func TestNormalizeDevicePetAssetAcceptsEightFramesAndRejectsNine(t *testing.T) {
	frame := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32))
	asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: frame, FrameMS: 450}
	asset.Frames = make([]string, devicePetAssetMaxFrames-1)
	for i := range asset.Frames {
		asset.Frames[i] = frame
	}
	if got := normalizeDevicePetAsset(asset); got == nil || got.FrameMS != 450 {
		t.Fatalf("eight-frame asset rejected: %#v", got)
	}
	asset.Frames = append(asset.Frames, frame)
	if got := normalizeDevicePetAsset(asset); got != nil {
		t.Fatalf("nine-frame asset accepted: %#v", got)
	}
}

func TestNormalizeDevicePetAssetDefaultsOnlyMissingFrameCadence(t *testing.T) {
	frame := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32))
	missing := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: frame}
	if got := normalizeDevicePetAsset(missing); got == nil || got.FrameMS != 450 {
		t.Fatalf("missing frame cadence was not defaulted: %#v", got)
	}
	for _, cadence := range []int{1, 49, 10001} {
		asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: frame, FrameMS: cadence}
		if got := normalizeDevicePetAsset(asset); got != nil {
			t.Fatalf("invalid frame cadence %d accepted: %#v", cadence, got)
		}
	}
}

func TestDevicePetAssetFromMapRejectsNonIntegralDescriptorNumbers(t *testing.T) {
	frame := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x12, 0x34, 0xff}, 32*32))
	valid := map[string]any{"encoding": "rgb565a8", "width": float64(32), "height": float64(32), "frameMs": float64(450), "data": frame}
	if got := DevicePetAssetFromMap(valid); got == nil || got.Width != 32 || got.Height != 32 || got.FrameMS != 450 {
		t.Fatalf("valid map descriptor rejected: %#v", got)
	}
	for _, invalid := range []map[string]any{
		{"encoding": "rgb565a8", "width": 32.5, "height": 32, "data": frame},
		{"encoding": "rgb565a8", "width": 32, "height": 32.5, "data": frame},
		{"encoding": "rgb565a8", "width": 32, "height": 32, "frameMs": 0, "data": frame},
		{"encoding": "rgb565a8", "width": 32, "height": 32, "frameMs": 450.5, "data": frame},
		{"encoding": "rgb565a8", "width": 32, "height": 32, "frameMs": "450", "data": frame},
	} {
		if got := DevicePetAssetFromMap(invalid); got != nil {
			t.Fatalf("non-integral or typed-invalid map descriptor accepted: %#v", got)
		}
	}
}

func TestDeviceGatewayAcceptsHighResolutionPetAsset(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	rawFrame := bytes.Repeat([]byte{0x12, 0x34, 0xff}, 256*256)
	asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 256, Height: 256, Data: base64.StdEncoding.EncodeToString(rawFrame)}
	if normalized := normalizeDevicePetAsset(asset); normalized == nil {
		t.Fatal("256px pet asset was rejected")
	}

	gateway.mu.Lock()
	reference := gateway.preparePetAssetLocked("pet-hires", asset, false, 0)
	gateway.mu.Unlock()
	if reference == nil || reference.Width != 256 || reference.Height != 256 || len(reference.URLs) != 1 {
		t.Fatalf("high-resolution reference=%#v", reference)
	}
}

func TestDeviceGatewayPetProfileUpdateIncludesAssetForCapableHardware(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	frame := bytes.Repeat([]byte{0xAA, 0x55, 0xff}, 32*32)
	asset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: base64.StdEncoding.EncodeToString(frame)}
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
	hashes, ok := ref["sha256"].([]any)
	if !ok || len(hashes) != 1 || len(hashes[0].(string)) != 64 {
		t.Fatalf("pet asset SHA-256 missing: %#v", ref)
	}
}

func TestDeviceGatewayPetProfileReplacementRejectsStaleAck(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	firstFrame := bytes.Repeat([]byte{0xAA, 0x55, 0xff}, 32*32)
	secondFrame := bytes.Repeat([]byte{0x11, 0x22, 0x80}, 32*32)
	firstAsset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: base64.StdEncoding.EncodeToString(firstFrame)}
	secondAsset := &DevicePetAsset{Encoding: "rgb565a8", Width: 32, Height: 32, Data: base64.StdEncoding.EncodeToString(secondFrame)}
	if err := gateway.registerPairingWithPetProfileAsset("gui-a", "tenant-a", "user-a", "654324", "clawmate", true, firstAsset); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-stale-ack", "code": "654324"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "pet-stale-ack", "capabilities": map[string]any{"features": map[string]any{"petAsset": true}},
	})

	gateway.updateMachinePetProfileAsset("gui-a", "tiger", true, firstAsset)
	firstPoll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-stale-ack&cursor=0", token, nil)
	var firstBody struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(firstPoll.Body).Decode(&firstBody)
	if len(firstBody.Messages) != 1 {
		t.Fatalf("first messages=%#v", firstBody.Messages)
	}
	oldID, _ := firstBody.Messages[0]["id"].(string)
	oldSeq, _ := firstBody.Messages[0]["seq"].(float64)

	gateway.updateMachinePetProfileAsset("gui-a", "fox", true, secondAsset)
	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-stale-ack", "messageIds": []string{oldID}, "status": "delivered",
	})
	if ack.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ack.Code, ack.Body.String())
	}

	secondPoll := deviceGatewayRequest(t, gateway, http.MethodGet,
		fmt.Sprintf("/api/im-gateway/v1/outgoing?clientId=pet-stale-ack&cursor=%d", int64(oldSeq)), token, nil)
	var secondBody struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(secondPoll.Body).Decode(&secondBody)
	if len(secondBody.Messages) != 1 {
		t.Fatalf("stale ACK removed replacement: %#v", secondBody.Messages)
	}
	if secondBody.Messages[0]["id"] == oldID || secondBody.Messages[0]["pet_skin"] != "fox" {
		t.Fatalf("unexpected replacement=%#v oldID=%q", secondBody.Messages[0], oldID)
	}
}

func TestDeviceGatewayHandshakeAdvertisesAvailableMeetingModes(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	gateway.SetMeetingRecordingHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gateway.SetMeetingRecordingModes(true, false)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "334400"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "meeting-pet", "code": "334400"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "meeting-pet", "capabilities": map[string]any{"features": map[string]any{"meetingRecorder": true}}})
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

func TestDeviceGatewayHandshakeOmitsMeetingWhenClientDidNotDeclareRecorder(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.SetMeetingRecordingHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	gateway.SetMeetingRecordingModes(true, true)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "334402"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "no-recorder-pet", "code": "334402"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", paired["gatewayToken"].(string), map[string]any{"clientId": "no-recorder-pet"})
	var body map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if _, ok := body["meetingRecording"]; ok {
		t.Fatalf("meeting endpoint leaked to undeclared client: %#v", body)
	}
}
func TestDeviceGatewayNegotiatesAndForwardsClientCapabilities(t *testing.T) {
	plugin := NewRemoteGatewayPlugin("thirdparty", nil, nil, nil)
	plugin.mu.Lock()
	plugin.owners["tenant-a"] = &gatewayOwner{TenantID: "tenant-a", MachineID: "gui-a"}
	inboundMessages := make(chan IncomingMessage, 1)
	plugin.messageHandler = func(msg IncomingMessage) { inboundMessages <- msg }
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
	var inbound IncomingMessage
	select {
	case inbound = <-inboundMessages:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded capabilities")
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

// This is the wire-level compatibility contract consumed by the ESP Gateway
// Transport parser.  Do not reduce this to a Go-struct assertion: the ESP
// must receive a concrete JSON capabilitiesAccepted object whose mandatory
// output.modality list survives the Hub's legacy normalization path, while
// feature-only and unknown legacy fields never widen the accepted surface.
func TestDeviceGatewayHandshakeCapabilitiesAcceptedWireContract(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445568"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "wire-contract", "code": "445568"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)

	// The full ESP surface verifies the canonical field names and that a Hub
	// normalization cannot advertise unsupported input/output values.
	declared := map[string]any{
		"input":  map[string]any{"modalities": []string{"text", "audio", "unsupported"}},
		"output": map[string]any{"modalities": []string{"text", "audio", "image", "unsupported"}},
		"features": map[string]any{
			"petStates": true, "petAnimation": true, "petAsset": true,
			"petAssetMaxFrames": 99, "ambientDisplay": true, "meetingRecorder": true,
			"volumeControl": true, "brightnessControl": true, "screenSleepControl": true,
			"futureUnknownFeature": true,
		},
	}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "wire-contract", "protocolVersion": "1.1", "clientCapabilities": declared,
	})
	if handshake.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	var response map[string]any
	if err := json.NewDecoder(handshake.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	accepted, ok := response["capabilitiesAccepted"].(map[string]any)
	if !ok {
		t.Fatalf("missing capabilitiesAccepted wire object: %#v", response)
	}
	output, ok := accepted["output"].(map[string]any)
	if !ok {
		t.Fatalf("accepted output missing: %#v", accepted)
	}
	modalities, ok := output["modalities"].([]any)
	if !ok || len(modalities) != 3 {
		t.Fatalf("accepted output modalities=%#v", output["modalities"])
	}
	for _, want := range []string{"text", "audio", "image"} {
		found := false
		for _, got := range modalities {
			if got == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("accepted output missing %q: %#v", want, modalities)
		}
	}
	features, ok := accepted["features"].(map[string]any)
	if !ok {
		t.Fatalf("accepted feature object missing: %#v", accepted)
	}
	for _, name := range []string{"petStates", "petAnimation", "petAsset", "ambientDisplay", "meetingRecorder", "volumeControl", "brightnessControl", "screenSleepControl"} {
		if features[name] != true {
			t.Fatalf("accepted feature %q=%#v, features=%#v", name, features[name], features)
		}
	}
	if _, leaked := features["futureUnknownFeature"]; leaked {
		t.Fatalf("unknown client feature leaked into accepted contract: %#v", features)
	}
	if maxFrames, ok := features["petAssetMaxFrames"].(float64); !ok || maxFrames != 8 {
		t.Fatalf("petAssetMaxFrames was not bounded: %#v", features["petAssetMaxFrames"])
	}
}

func TestDeviceGatewayLegacyHandshakeCapabilitiesAcceptedWireContract(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445569"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "legacy-wire-contract", "code": "445569"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)

	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "legacy-wire-contract"})
	if handshake.Code != http.StatusOK {
		t.Fatalf("legacy handshake status=%d body=%s", handshake.Code, handshake.Body.String())
	}
	var response map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&response)
	accepted, ok := response["capabilitiesAccepted"].(map[string]any)
	if !ok {
		t.Fatalf("legacy response omitted capabilitiesAccepted: %#v", response)
	}
	output, ok := accepted["output"].(map[string]any)
	if !ok {
		t.Fatalf("legacy accepted output missing: %#v", accepted)
	}
	modalities, ok := output["modalities"].([]any)
	if !ok || len(modalities) != 1 || modalities[0] != "text" {
		t.Fatalf("legacy accepted output must be text-only: %#v", output["modalities"])
	}
	// Input and Features are value structs, so encoding/json may retain empty
	// objects despite omitempty.  They must nevertheless carry no accepted
	// value that can widen a legacy text-only client.
	if input, present := accepted["input"].(map[string]any); present && len(input) != 0 {
		t.Fatalf("legacy input unexpectedly widened accepted contract: %#v", accepted)
	}
	if features, present := accepted["features"].(map[string]any); present && len(features) != 0 {
		t.Fatalf("legacy features unexpectedly widened accepted contract: %#v", accepted)
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
	gateway.EnqueueReply("pet-media", "default", map[string]any{"type": "image", "mime_type": "image/png", "image_data": encodedGatewayTestImage(t, "png", 1, 1)})
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

func TestDeviceGatewayHandshakeSharesAmbientWithNewlyPairedClient(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "112233"); err != nil {
		t.Fatal(err)
	}
	firstPair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-old", "code": "112233"})
	var first map[string]any
	_ = json.NewDecoder(firstPair.Body).Decode(&first)
	if firstPair.Code != http.StatusCreated {
		t.Fatalf("initial pair status=%d body=%#v", firstPair.Code, first)
	}

	gateway.UpdateMachineAmbient("gui-a", map[string]any{
		"weather":   map[string]any{"summary": "晴", "temperatureC": 26, "location": "北京"},
		"expiresAt": time.Now().Add(time.Hour).UnixMilli(),
	})
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445566"); err != nil {
		t.Fatal(err)
	}
	secondPair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-new", "code": "445566"})
	var second map[string]any
	_ = json.NewDecoder(secondPair.Body).Decode(&second)
	token, _ := second["gatewayToken"].(string)
	if secondPair.Code != http.StatusCreated || token == "" {
		t.Fatalf("replacement pair status=%d body=%#v", secondPair.Code, second)
	}
	handshake := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-new"})
	var body map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	if handshake.Code != http.StatusOK || body["ambient"] == nil {
		t.Fatalf("new client did not receive shared ambient: status=%d body=%#v", handshake.Code, body)
	}
}

func TestDeviceGatewayAmbientSurvivesHubRestartForReplacementDevice(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	first := NewPersistentDeviceGateway(nil, store)
	if err := first.RegisterPairing("gui-a", "tenant-a", "user-a", "445566"); err != nil {
		t.Fatal(err)
	}
	firstPair := deviceGatewayRequest(t, first, http.MethodPost,
		"/api/device-gateway/v1/pair", "", map[string]any{
			"clientId": "pet-old", "code": "445566",
		})
	if firstPair.Code != http.StatusCreated {
		t.Fatalf("initial pair status=%d body=%s", firstPair.Code, firstPair.Body.String())
	}
	first.UpdateMachineAmbient("gui-a", map[string]any{
		"weather":   map[string]any{"summary": "晴", "temperatureC": 26, "location": "北京"},
		"expiresAt": time.Now().Add(time.Hour).UnixMilli(),
	})

	// Recreate the Hub from durable state, then pair a replacement physical
	// device before the GUI has produced another ambient refresh.
	restarted := NewPersistentDeviceGateway(nil, store)
	if err := restarted.RegisterPairing("gui-a", "tenant-a", "user-a", "445567"); err != nil {
		t.Fatal(err)
	}
	replacementPair := deviceGatewayRequest(t, restarted, http.MethodPost,
		"/api/device-gateway/v1/pair", "", map[string]any{
			"clientId": "pet-new", "code": "445567",
		})
	var paired map[string]any
	_ = json.NewDecoder(replacementPair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	if replacementPair.Code != http.StatusCreated || token == "" {
		t.Fatalf("replacement pair status=%d body=%#v", replacementPair.Code, paired)
	}
	handshake := deviceGatewayRequest(t, restarted, http.MethodPost,
		"/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-new"})
	var body map[string]any
	_ = json.NewDecoder(handshake.Body).Decode(&body)
	ambient, ok := body["ambient"].(map[string]any)
	if handshake.Code != http.StatusOK || !ok {
		t.Fatalf("replacement handshake missing durable ambient: status=%d body=%#v",
			handshake.Code, body)
	}
	weather, _ := ambient["weather"].(map[string]any)
	if weather["location"] != "北京" || weather["temperatureC"] != float64(26) {
		t.Fatalf("replacement ambient=%#v", ambient)
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
	var firstHandshake map[string]any
	_ = json.NewDecoder(first.Body).Decode(&firstHandshake)
	if firstHandshake["startupWelcomeQueued"] != true {
		t.Fatalf("first handshake startupWelcomeQueued=%#v, want true", firstHandshake["startupWelcomeQueued"])
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=0", token, nil)
	var polled struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 || polled.Messages[0]["type"] != "audio" || polled.Messages[0]["bootSessionId"] != "boot-a" {
		t.Fatalf("first boot messages=%#v", polled.Messages)
	}
	welcomeID, _ := polled.Messages[0]["id"].(string)
	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-welcome", "messageIds": []string{welcomeID}, "status": "delivered",
	})
	if ack.Code != http.StatusOK {
		t.Fatalf("welcome ack status=%d body=%s", ack.Code, ack.Body.String())
	}
	// A capability refresh is a normal handshake, not a new boot.
	retry := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-a", "capabilities": capabilities})
	var retryHandshake map[string]any
	_ = json.NewDecoder(retry.Body).Decode(&retryHandshake)
	if retryHandshake["startupWelcomeQueued"] != false {
		t.Fatalf("acknowledged same-boot Welcome rearmed gate: %#v", retryHandshake)
	}
	poll = deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-welcome&cursor=1", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 0 {
		t.Fatalf("same boot queued another welcome: %#v", polled.Messages)
	}
	// A Hub restart retains both the sound and the last boot ID.
	restarted := NewPersistentDeviceGateway(nil, store)
	restartHandshake := deviceGatewayRequest(t, restarted, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-welcome", "bootSessionId": "boot-a", "capabilities": capabilities})
	var restartBody map[string]any
	_ = json.NewDecoder(restartHandshake.Body).Decode(&restartBody)
	if restartBody["startupWelcomeQueued"] != false {
		t.Fatalf("persisted same boot should not arm Welcome gate: %#v", restartBody)
	}
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

func TestDeviceGatewayHandshakeKeepsPendingStartupWelcomeGateArmed(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445577"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-pending-welcome", "code": "445577"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if err := gateway.UpdateMachineWelcome("gui-a", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}, "playback": true}}}
	request := map[string]any{"clientId": "pet-pending-welcome", "bootSessionId": "boot-pending", "capabilities": capabilities}
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, request)
	retry := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, request)
	for index, response := range []*httptest.ResponseRecorder{first, retry} {
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		if body["startupWelcomeQueued"] != true {
			t.Fatalf("handshake %d startupWelcomeQueued=%#v, want true", index+1, body["startupWelcomeQueued"])
		}
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-pending-welcome&cursor=0", token, nil)
	var polled struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&polled)
	if len(polled.Messages) != 1 {
		t.Fatalf("retry duplicated pending Welcome: %#v", polled.Messages)
	}
}

func TestDeviceGatewayNewBootReplacesPendingStartupWelcome(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445579"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-reboot-welcome", "code": "445579"})
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
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-reboot-welcome", "bootSessionId": "boot-old", "capabilities": capabilities})
	if first.Code != http.StatusOK {
		t.Fatalf("first handshake status=%d body=%s", first.Code, first.Body.String())
	}
	// Simulate power loss before the old Welcome can be acknowledged.
	second := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-reboot-welcome", "bootSessionId": "boot-current", "capabilities": capabilities})
	var secondBody map[string]any
	_ = json.NewDecoder(second.Body).Decode(&secondBody)
	if second.Code != http.StatusOK || secondBody["startupWelcomeQueued"] != true {
		t.Fatalf("second handshake status=%d body=%#v", second.Code, secondBody)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-reboot-welcome&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 || outgoing.Messages[0]["bootSessionId"] != "boot-current" {
		t.Fatalf("new boot outgoing=%#v, want only current Welcome", outgoing.Messages)
	}
}

func TestDeviceGatewayColdBootDropsPreviousRuntimeResultsAndRebuildsStartupState(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445580"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-clean-boot", "code": "445580"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if pair.Code != http.StatusCreated || token == "" {
		t.Fatalf("pair status=%d body=%#v", pair.Code, pairBody)
	}
	if err := gateway.UpdateMachineWelcome("gui-a", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineVolume("gui-a", 37); err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{
		"output":   map[string]any{"modalities": []string{"text", "audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}, "playback": true}},
		"features": map[string]any{"volumeControl": true},
	}
	first := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-clean-boot", "bootSessionId": "boot-old", "capabilities": capabilities})
	if first.Code != http.StatusOK {
		t.Fatalf("first handshake status=%d body=%s", first.Code, first.Body.String())
	}
	// Queue the previous session's result after startup. It is deliberately left
	// unacknowledged to reproduce a device losing power before delivery/ACK.
	gateway.EnqueueReply("pet-clean-boot", "default", map[string]any{
		"reply_type": "text", "text": "previous command result", "replyTo": "old-command",
	})

	second := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-clean-boot", "bootSessionId": "boot-current", "capabilities": capabilities})
	var secondBody map[string]any
	_ = json.NewDecoder(second.Body).Decode(&secondBody)
	if second.Code != http.StatusOK || secondBody["startupWelcomeQueued"] != true {
		t.Fatalf("second handshake status=%d body=%#v", second.Code, secondBody)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-clean-boot&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 2 {
		t.Fatalf("cold boot outgoing=%#v, want rebuilt volume + Welcome", outgoing.Messages)
	}
	types := map[string]int{}
	for _, message := range outgoing.Messages {
		typeName, _ := message["type"].(string)
		types[typeName]++
		if message["text"] == "previous command result" || message["replyTo"] == "old-command" {
			t.Fatalf("previous runtime result survived cold boot: %#v", message)
		}
	}
	if types["hardware_config"] != 1 || types["audio"] != 1 {
		t.Fatalf("rebuilt startup message types=%#v messages=%#v", types, outgoing.Messages)
	}
}

func TestDeviceGatewayColdBootRejectsLateReplyFromPreviousCommand(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-late-reply")
	state.bootSessionID = "boot-old"
	state.activeReplies["old-command"] = struct{}{}
	state.activeOrder = append(state.activeOrder, "old-command")

	gateway.mu.Lock()
	gateway.resetDeviceRuntimeQueueForBootLocked(state)
	state.bootSessionID = "boot-current"
	gateway.mu.Unlock()

	gateway.EnqueueReply("pet-late-reply", "default", map[string]any{
		"type": "text", "text": "late old result", "replyTo": "old-command",
	})
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(state.messages) != 0 {
		t.Fatalf("late previous-boot reply entered current queue: %#v", state.messages)
	}
}

func TestDeviceGatewayCurrentBootAcceptsCorrelatedReply(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	state := gateway.clientLocked("pet-current-reply")
	state.bootSessionID = "boot-current"
	gateway.activateDeviceReply("pet-current-reply", "current-command")

	gateway.EnqueueReply("pet-current-reply", "default", map[string]any{
		"type": "text", "text": "current result", "replyToMessageId": "current-command",
	})
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	if len(state.messages) != 1 || state.messages[0]["text"] != "current result" {
		t.Fatalf("current-boot reply was rejected: %#v", state.messages)
	}
}

func TestDeviceGatewayHandshakeKeepsFailedStartupWelcomeGateArmedUntilPruned(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445578"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-failed-welcome", "code": "445578"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if err := gateway.UpdateMachineWelcome("gui-a", true, base64.StdEncoding.EncodeToString([]byte("wav")), true); err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"audio"}, "audio": map[string]any{"mimeTypes": []string{"audio/wav"}, "playback": true}}}
	request := map[string]any{"clientId": "pet-failed-welcome", "bootSessionId": "boot-failed", "capabilities": capabilities}
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, request)

	gateway.mu.Lock()
	state := gateway.clients["pet-failed-welcome"]
	if state == nil || len(state.messages) != 1 {
		gateway.mu.Unlock()
		t.Fatalf("startup Welcome queue=%#v", state)
	}
	welcomeID, _ := state.messages[0]["id"].(string)
	state.acked[welcomeID] = true
	state.ackStatus[welcomeID] = "failed"
	gateway.mu.Unlock()

	// Observe the state before normal ACK pruning. Even if another request has
	// already recorded the ACK flag, the still-present message means a device
	// repeating this handshake must keep its startup gate armed.
	retry := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, request)
	var body map[string]any
	_ = json.NewDecoder(retry.Body).Decode(&body)
	if body["startupWelcomeQueued"] != true {
		t.Fatalf("failed pending Welcome disarmed startup gate: %#v", body)
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

func TestDeviceGatewayRepairingSameMachineRevokesPreviousCredentialAndQueue(t *testing.T) {
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
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445570"); err != nil {
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

func TestDeviceGatewayMachineAudioReplyPreservesHardwarePreview(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445566"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-preview", "code": "445566"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"audio"}, "audio": map[string]any{
		"mimeTypes": []string{"audio/wav"}, "playback": true, "deliveryModes": []string{"inline"}, "maxInlineBytes": 1024,
	}}}
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-preview", "capabilities": capabilities})
	queued := gateway.EnqueueMachineReplyCount("gui-a", "system", map[string]any{
		"reply_type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("wav")),
		"extra": map[string]any{"hardware_audio_preview": true, "hardware_audio_preview_request_id": "preview-request-1"},
	})
	if queued != 1 {
		t.Fatalf("audio preview queued=%d, want 1", queued)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-preview&cursor=0", token, nil)
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&body)
	if len(body.Messages) != 1 || body.Messages[0]["type"] != "audio" {
		t.Fatalf("audio preview reply=%#v", body.Messages)
	}
	extra, _ := body.Messages[0]["extra"].(map[string]any)
	if extra["hardware_audio_preview"] != true {
		t.Fatalf("audio preview marker=%#v, want true", extra)
	}
	if extra["hardware_audio_preview_request_id"] != "preview-request-1" {
		t.Fatalf("audio preview request marker=%#v", extra)
	}
}

func TestDeviceGatewayHardwarePreviewSkipsOfflineDevice(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445570"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-offline-preview", "code": "445570"})
	if pair.Code != http.StatusCreated {
		t.Fatalf("pair status=%d body=%s", pair.Code, pair.Body.String())
	}
	queued := gateway.EnqueueMachineReplyCount("gui-a", "system", map[string]any{
		"reply_type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("wav")),
		"extra": map[string]any{"hardware_audio_preview": true},
	})
	if queued != 0 {
		t.Fatalf("offline audio preview queued=%d, want 0", queued)
	}
}

func TestDeviceGatewayAuthenticatedPollRefreshesOnlinePresence(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445571"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-presence", "code": "445571"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	if token == "" {
		t.Fatalf("pair response=%s", pair.Body.String())
	}
	gateway.mu.Lock()
	for bearer, principal := range gateway.tokens {
		if principal.ClientID == "pet-presence" {
			principal.LastSeenAt = time.Now().Add(-2 * time.Minute)
			gateway.tokens[bearer] = principal
		}
	}
	gateway.mu.Unlock()

	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-presence&cursor=0", token, nil)
	if poll.Code != http.StatusOK {
		t.Fatalf("poll status=%d body=%s", poll.Code, poll.Body.String())
	}
	devices := gateway.ListMachineDevices("gui-a")
	if len(devices) != 1 || !devices[0].Online {
		t.Fatalf("devices after authenticated poll=%#v", devices)
	}
}

func TestDeviceGatewayHardwarePreviewAckNotifiesMachine(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	sender := &captureMachineSender{}
	gateway.SetMachineMessageSender(sender)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445569"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-preview-ack", "code": "445569"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"audio"}, "audio": map[string]any{
		"mimeTypes": []string{"audio/wav"}, "playback": true, "deliveryModes": []string{"inline"}, "maxInlineBytes": 1024,
	}}}
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "pet-preview-ack", "capabilities": capabilities})
	gateway.EnqueueMachineReply("gui-a", "system", map[string]any{
		"reply_type": "audio", "mime_type": "audio/wav", "file_data": base64.StdEncoding.EncodeToString([]byte("wav")),
		"extra": map[string]any{"hardware_audio_preview": true, "hardware_audio_preview_request_id": "preview-request-ack"},
	})
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=pet-preview-ack&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("preview outgoing=%#v", outgoing.Messages)
	}
	messageID, _ := outgoing.Messages[0]["id"].(string)
	ack := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/ack", token, map[string]any{
		"clientId": "pet-preview-ack", "messageIds": []string{messageID}, "status": "delivered",
	})
	if ack.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ack.Code, ack.Body.String())
	}
	if sender.machineID != "gui-a" {
		t.Fatalf("receipt machine=%q, want gui-a", sender.machineID)
	}
	message, ok := sender.msg.(map[string]any)
	if !ok || message["type"] != "im.device_gateway_playback_receipt" || message["request_id"] != "preview-request-ack" {
		t.Fatalf("playback receipt=%#v", sender.msg)
	}
	payload, _ := message["payload"].(map[string]any)
	if payload["clientId"] != "pet-preview-ack" || payload["messageId"] != messageID || payload["status"] != "delivered" {
		t.Fatalf("playback receipt payload=%#v", payload)
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
	capabilities := map[string]any{"output": map[string]any{"modalities": []string{"text"}}, "features": map[string]any{"volumeControl": true}}
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

func TestDeviceGatewayDeviceVolumeIsIndependentAndPersists(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445574"); err != nil {
		t.Fatal(err)
	}
	pair := func(clientID string) string {
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": "445574"})
		if response.Code == http.StatusUnauthorized {
			// Pairing codes are single-use, so reserve a fresh one for the second device.
			if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445575"); err != nil {
				t.Fatal(err)
			}
			response = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": "445575"})
		}
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", clientID, response.Code, response.Body.String())
		}
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		token, _ := body["gatewayToken"].(string)
		return token
	}
	tokenA := pair("volume-a")
	tokenB := pair("volume-b")
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenA, map[string]any{"clientId": "volume-a"})
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenB, map[string]any{"clientId": "volume-b"})
	if err := gateway.UpdateMachineDeviceVolume("gui-a", "volume-a", 21); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceVolume("gui-a", "volume-b", 83); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("gui-a")
	byClient := make(map[string]*int, len(devices))
	for _, device := range devices {
		byClient[device.ClientID] = device.Volume
	}
	if len(devices) != 2 || byClient["volume-a"] == nil || *byClient["volume-a"] != 21 || byClient["volume-b"] == nil || *byClient["volume-b"] != 83 {
		t.Fatalf("listed volumes=%#v", devices)
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	devices = restarted.ListMachineDevices("gui-a")
	byClient = make(map[string]*int, len(devices))
	for _, device := range devices {
		byClient[device.ClientID] = device.Volume
	}
	if len(devices) != 2 || byClient["volume-a"] == nil || *byClient["volume-a"] != 21 || byClient["volume-b"] == nil || *byClient["volume-b"] != 83 {
		t.Fatalf("persisted listed volumes=%#v", devices)
	}
}

func TestDeviceGatewayDevicePetIsIndependentAndPersists(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.UpdateMachineAllowCustomPets("gui-a", true); err != nil {
		t.Fatal(err)
	}
	for index, code := range []string{"445576", "445577"} {
		if err := gateway.RegisterPairingWithPetProfile("gui-a", "tenant-a", "user-a", code, "clawmate", true); err != nil {
			t.Fatal(err)
		}
		clientID := []string{"pet-a", "pet-b"}[index]
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": code})
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", clientID, response.Code, response.Body.String())
		}
	}
	if err := gateway.UpdateMachineDevicePetProfileAsset("gui-a", "pet-a", "focus-claw", false, nil); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("gui-a")
	byClient := map[string]string{}
	for _, device := range devices {
		byClient[device.ClientID] = device.PetSkin
	}
	if byClient["pet-a"] != "focus-claw" || byClient["pet-b"] != "clawmate" {
		t.Fatalf("independent device pets=%#v", byClient)
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	devices = restarted.ListMachineDevices("gui-a")
	byClient = map[string]string{}
	for _, device := range devices {
		byClient[device.ClientID] = device.PetSkin
	}
	if byClient["pet-a"] != "focus-claw" || byClient["pet-b"] != "clawmate" {
		t.Fatalf("persisted device pets=%#v", byClient)
	}
	if err := gateway.UpdateMachineDevicePetProfileAsset("gui-b", "pet-a", "other", true, nil); err == nil {
		t.Fatal("cross-machine device pet update succeeded")
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

func TestDeviceGatewayDeviceBrightnessIsIndependentAndPersists(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445580"); err != nil {
		t.Fatal(err)
	}
	pair := func(clientID string) string {
		response := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": "445580"})
		if response.Code == http.StatusUnauthorized {
			// Pairing codes are single-use, so reserve a fresh one for the second device.
			if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445581"); err != nil {
				t.Fatal(err)
			}
			response = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": clientID, "code": "445581"})
		}
		if response.Code != http.StatusCreated {
			t.Fatalf("pair %s status=%d body=%s", clientID, response.Code, response.Body.String())
		}
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		token, _ := body["gatewayToken"].(string)
		return token
	}
	tokenA := pair("brightness-a")
	tokenB := pair("brightness-b")
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenA, map[string]any{"clientId": "brightness-a"})
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", tokenB, map[string]any{"clientId": "brightness-b"})
	if err := gateway.UpdateMachineDeviceBrightness("gui-a", "brightness-a", 15); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceBrightness("gui-a", "brightness-b", 90); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("gui-a")
	byClient := make(map[string]*int, len(devices))
	for _, device := range devices {
		byClient[device.ClientID] = device.Brightness
	}
	if len(devices) != 2 || byClient["brightness-a"] == nil || *byClient["brightness-a"] != 15 || byClient["brightness-b"] == nil || *byClient["brightness-b"] != 90 {
		t.Fatalf("listed brightness=%#v", devices)
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	devices = restarted.ListMachineDevices("gui-a")
	byClient = make(map[string]*int, len(devices))
	for _, device := range devices {
		byClient[device.ClientID] = device.Brightness
	}
	if len(devices) != 2 || byClient["brightness-a"] == nil || *byClient["brightness-a"] != 15 || byClient["brightness-b"] == nil || *byClient["brightness-b"] != 90 {
		t.Fatalf("persisted listed brightness=%#v", devices)
	}
	// A cross-machine update must fail, mirroring the volume ownership rule.
	if err := restarted.UpdateMachineDeviceBrightness("gui-b", "brightness-a", 50); err == nil {
		t.Fatal("cross-machine device brightness update succeeded")
	}
}

func TestDeviceGatewayDeviceScreenSleepTimeoutIsIndependentAndCapabilityGated(t *testing.T) {
	store := &memoryDeviceCredentialStore{values: make(map[string]string)}
	gateway := NewPersistentDeviceGateway(nil, store)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445584"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "sleep-a", "code": "445584"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{
		"clientId": "sleep-a", "capabilities": map[string]any{"features": map[string]any{"screenSleepControl": true}},
	})
	if err := gateway.UpdateMachineDeviceScreenSleepTimeout("gui-a", "sleep-a", 1800); err != nil {
		t.Fatal(err)
	}
	devices := gateway.ListMachineDevices("gui-a")
	if len(devices) != 1 || devices[0].ScreenSleepSeconds == nil || *devices[0].ScreenSleepSeconds != 1800 {
		t.Fatalf("listed screen sleep timeout=%#v", devices)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=sleep-a&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("screen sleep message=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["screenSleepSeconds"] != float64(1800) {
		t.Fatalf("screen sleep extra=%#v", extra)
	}
	restarted := NewPersistentDeviceGateway(nil, store)
	devices = restarted.ListMachineDevices("gui-a")
	if len(devices) != 1 || devices[0].ScreenSleepSeconds == nil || *devices[0].ScreenSleepSeconds != 1800 {
		t.Fatalf("persisted screen sleep timeout=%#v", devices)
	}
	if err := restarted.UpdateMachineDeviceScreenSleepTimeout("gui-a", "sleep-a", 120); err == nil {
		t.Fatal("unsupported screen sleep timeout was accepted")
	}
}

func TestDeviceGatewayScreenSleepRequiresDeclaredCapability(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445585"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "sleep-caps", "code": "445585"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	// Store the selection before the device firmware advertises support. It is
	// durable but must not be sent to a client that cannot safely consume it.
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "sleep-caps", "capabilities": map[string]any{"features": map[string]any{"brightnessControl": true}}})
	if err := gateway.UpdateMachineDeviceScreenSleepTimeout("gui-a", "sleep-caps", 0); err != nil {
		t.Fatal(err)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=sleep-caps&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 0 {
		t.Fatalf("screen sleep reached device without screenSleepControl: %#v", outgoing.Messages)
	}
	devices := gateway.ListMachineDevices("gui-a")
	if len(devices) != 1 || devices[0].ScreenSleepSeconds == nil || *devices[0].ScreenSleepSeconds != 0 {
		t.Fatalf("durable screen sleep=%#v", devices)
	}

	// A later capability handshake receives the stored setting, including zero
	// which explicitly means never turn the display off automatically.
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "sleep-caps", "capabilities": map[string]any{"features": map[string]any{"screenSleepControl": true}}})
	poll = deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=sleep-caps&cursor=0", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 || outgoing.Messages[0]["type"] != "hardware_config" {
		t.Fatalf("declared screen sleep messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["screenSleepSeconds"] != float64(0) {
		t.Fatalf("declared screen sleep=%#v, want 0", extra)
	}
}

func TestDeviceGatewayBrightnessRequiresDeclaredCapability(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445582"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "brightness-caps", "code": "445582"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	// The device declares no brightnessControl: the durable value is stored but
	// never leaves Hub, and no error is raised.
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "brightness-caps", "capabilities": map[string]any{"features": map[string]any{"volumeControl": true}}})
	if err := gateway.UpdateMachineDeviceBrightness("gui-a", "brightness-caps", 70); err != nil {
		t.Fatal(err)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=brightness-caps&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 0 {
		t.Fatalf("brightness reached a device without brightnessControl: %#v", outgoing.Messages)
	}
	devices := gateway.ListMachineDevices("gui-a")
	if len(devices) != 1 || devices[0].Brightness == nil || *devices[0].Brightness != 70 {
		t.Fatalf("durable brightness=%#v", devices)
	}
	// Once the device declares brightnessControl, the handshake picks the level up.
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "brightness-caps", "capabilities": map[string]any{"features": map[string]any{"volumeControl": true, "brightnessControl": true}}})
	poll = deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=brightness-caps&cursor=0", token, nil)
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 || outgoing.Messages[0]["type"] != "hardware_config" {
		t.Fatalf("declared brightness messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["brightness"] != float64(70) {
		t.Fatalf("declared brightness=%#v, want 70", extra)
	}
}

func TestDeviceGatewayPendingHardwareConfigMergesPerDeviceLevels(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "445583"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "level-merge", "code": "445583"})
	var pairBody map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&pairBody)
	token, _ := pairBody["gatewayToken"].(string)
	capabilities := map[string]any{"features": map[string]any{"volumeControl": true, "brightnessControl": true, "screenSleepControl": true}}
	// Persist both levels before the first handshake so they land in one
	// pending hardware_config message.
	if err := gateway.UpdateMachineDeviceVolume("gui-a", "level-merge", 33); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceBrightness("gui-a", "level-merge", 66); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceScreenSleepTimeout("gui-a", "level-merge", 600); err != nil {
		t.Fatal(err)
	}
	_ = deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/handshake", token, map[string]any{"clientId": "level-merge", "capabilities": capabilities})
	// Updating one level while the message is still pending must not clobber
	// the other.
	if err := gateway.UpdateMachineDeviceBrightness("gui-a", "level-merge", 80); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceVolume("gui-a", "level-merge", 44); err != nil {
		t.Fatal(err)
	}
	if err := gateway.UpdateMachineDeviceScreenSleepTimeout("gui-a", "level-merge", 1800); err != nil {
		t.Fatal(err)
	}
	poll := deviceGatewayRequest(t, gateway, http.MethodGet, "/api/im-gateway/v1/outgoing?clientId=level-merge&cursor=0", token, nil)
	var outgoing struct {
		Messages []map[string]any `json:"messages"`
	}
	_ = json.NewDecoder(poll.Body).Decode(&outgoing)
	if len(outgoing.Messages) != 1 {
		t.Fatalf("merged hardware_config messages=%#v", outgoing.Messages)
	}
	extra, _ := outgoing.Messages[0]["extra"].(map[string]any)
	if extra["volume"] != float64(44) || extra["brightness"] != float64(80) || extra["screenSleepSeconds"] != float64(1800) {
		t.Fatalf("merged levels=%#v, want volume=44 brightness=80 screenSleepSeconds=1800", extra)
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

func TestDeviceGatewayCodePairRateLimitsByConnectionAddress(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	for i := 0; i < deviceCodePairAttemptLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(fmt.Sprintf(`{"clientId":"pet-%d","pairCode":"000000"}`, i)))
		req.RemoteAddr = "203.0.113.20:4321"
		// A spoofed forwarding header must not create a fresh limiter bucket.
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		gateway.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"clientId":"pet-over-limit","pairCode":"000000"}`))
	req.RemoteAddr = "203.0.113.20:9999"
	req.Header.Set("X-Forwarded-For", "192.0.2.250")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gateway.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over-limit status=%d retry=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}

	other := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair", bytes.NewBufferString(`{"clientId":"pet-other-address","pairCode":"000000"}`))
	other.RemoteAddr = "203.0.113.21:4321"
	other.Header.Set("Content-Type", "application/json")
	otherRec := httptest.NewRecorder()
	gateway.ServeHTTP(otherRec, other)
	if otherRec.Code != http.StatusUnauthorized {
		t.Fatalf("other address status=%d body=%s", otherRec.Code, otherRec.Body.String())
	}
}

func TestDeviceGatewayUploadCannotRestoreMediaRemovedDuringRead(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	mediaID, mediaToken := "media-race", "media-token"
	gateway.media[mediaID] = &deviceMedia{ClientID: "pet-race", ID: mediaID, Token: mediaToken, SizeBytes: 3, LastAccessedAt: time.Now().UTC()}
	reader, writer := io.Pipe()
	req := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/"+mediaID+"/upload?mediaToken="+mediaToken, reader)
	req.ContentLength = 3
	done := make(chan error, 1)
	go func() { done <- gateway.storeMediaUpload(req, mediaID) }()
	_, _ = writer.Write([]byte("a"))
	gateway.mu.Lock()
	delete(gateway.media, mediaID)
	gateway.mu.Unlock()
	_, _ = writer.Write([]byte("bc"))
	_ = writer.Close()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "media not found") {
		t.Fatalf("upload error=%v", err)
	}
	if gateway.media[mediaID] != nil {
		t.Fatal("removed media was restored by an in-flight upload")
	}
}

func TestDeviceGatewayMediaPrepareNeverExceedsObjectLimit(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	if err := gateway.UpdateMachineHardwareEnabled("gui-a", true); err != nil {
		t.Fatal(err)
	}
	if err := gateway.RegisterPairing("gui-a", "tenant-a", "user-a", "610013"); err != nil {
		t.Fatal(err)
	}
	pair := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/device-gateway/v1/pair", "", map[string]any{"clientId": "pet-media-limit", "pairCode": "610013"})
	var paired map[string]any
	_ = json.NewDecoder(pair.Body).Decode(&paired)
	token, _ := paired["gatewayToken"].(string)
	for i := 0; i < deviceGatewayMaxMediaObjects+5; i++ {
		prepare := deviceGatewayRequest(t, gateway, http.MethodPost, "/api/im-gateway/v1/media/upload-url", token, map[string]any{"clientId": "pet-media-limit", "type": "voice", "fileName": fmt.Sprintf("%d.wav", i), "mimeType": "audio/wav", "sizeBytes": 1})
		if prepare.Code != http.StatusOK {
			t.Fatalf("prepare %d status=%d body=%s", i, prepare.Code, prepare.Body.String())
		}
		if len(gateway.media) > deviceGatewayMaxMediaObjects {
			t.Fatalf("media count=%d after prepare %d", len(gateway.media), i)
		}
	}
	if len(gateway.media) != deviceGatewayMaxMediaObjectsPerClient {
		t.Fatalf("final media count=%d, want per-client limit %d", len(gateway.media), deviceGatewayMaxMediaObjectsPerClient)
	}
}

func TestDeviceGatewayIncomingAttachmentUsesConsistentSnapshot(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["snapshot"] = &deviceMedia{
		ClientID: "pet-snapshot", ID: "snapshot", Token: "token", Type: "voice",
		FileName: "old.wav", MimeType: "audio/wav", Data: []byte("old"), Uploaded: true,
		LastAccessedAt: time.Now().UTC(),
	}
	refs := []struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		FileName  string `json:"fileName"`
		MimeType  string `json:"mimeType"`
		Data      string `json:"data"`
		SizeBytes int64  `json:"sizeBytes"`
	}{{ID: "snapshot"}}

	const iterations = 200
	start := make(chan struct{})
	done := make(chan struct{})
	go func() {
		<-start
		for i := 0; i < iterations; i++ {
			gateway.mu.Lock()
			media := gateway.media["snapshot"]
			media.FileName = fmt.Sprintf("%d.wav", i)
			media.Data = bytes.Repeat([]byte{byte(i)}, 64)
			gateway.mu.Unlock()
		}
		close(done)
	}()
	close(start)
	for i := 0; i < iterations; i++ {
		attachments, err := gateway.incomingAttachments("pet-snapshot", refs)
		if err != nil || len(attachments) != 1 || attachments[0].Size <= 0 || attachments[0].Data == "" {
			t.Fatalf("attachment snapshot=%#v err=%v", attachments, err)
		}
	}
	<-done
}

func TestDeviceGatewayIncomingAttachmentsBoundExpandedBytes(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	payload := bytes.Repeat([]byte{1}, int(deviceGatewayMaxMediaBytes))
	gateway.media["large"] = &deviceMedia{
		ClientID: "pet-attachments", ID: "large", Token: "token", Data: payload,
		Uploaded: true, LastAccessedAt: time.Now().UTC(),
	}
	refs := []struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		FileName  string `json:"fileName"`
		MimeType  string `json:"mimeType"`
		Data      string `json:"data"`
		SizeBytes int64  `json:"sizeBytes"`
	}{{ID: "large"}, {ID: "large"}}
	if _, err := gateway.incomingAttachments("pet-attachments", refs); err == nil || !strings.Contains(err.Error(), "attachments exceed") {
		t.Fatalf("expanded attachment error=%v", err)
	}
}

func TestDeviceGatewayConcurrentUploadAllowsSingleWriter(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["upload-once"] = &deviceMedia{
		ClientID: "pet-upload", ID: "upload-once", Token: "token", SizeBytes: 3,
		LastAccessedAt: time.Now().UTC(),
	}
	reader, writer := io.Pipe()
	first := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/upload-once/upload?mediaToken=token", reader)
	first.ContentLength = 3
	firstDone := make(chan error, 1)
	go func() { firstDone <- gateway.storeMediaUpload(first, "upload-once") }()
	_, _ = writer.Write([]byte("a"))

	second := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/upload-once/upload?mediaToken=token", bytes.NewBufferString("xyz"))
	second.ContentLength = 3
	if err := gateway.storeMediaUpload(second, "upload-once"); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("second upload error=%v", err)
	}
	_, _ = writer.Write([]byte("bc"))
	_ = writer.Close()
	if err := <-firstDone; err != nil {
		t.Fatalf("first upload error=%v", err)
	}
	if got := string(gateway.media["upload-once"].Data); got != "abc" {
		t.Fatalf("stored media=%q, want abc", got)
	}
}

func TestDeviceGatewayFailedUploadReleasesWriterClaim(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["retry-upload"] = &deviceMedia{
		ClientID: "pet-upload", ID: "retry-upload", Token: "token", SizeBytes: 3,
		LastAccessedAt: time.Now().UTC(),
	}
	bad := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/retry-upload/upload?mediaToken=token", bytes.NewBufferString("xx"))
	bad.ContentLength = 2
	if err := gateway.storeMediaUpload(bad, "retry-upload"); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("bad upload error=%v", err)
	}
	good := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/retry-upload/upload?mediaToken=token", bytes.NewBufferString("abc"))
	good.ContentLength = 3
	if err := gateway.storeMediaUpload(good, "retry-upload"); err != nil {
		t.Fatalf("retry upload error=%v", err)
	}
}

func TestDeviceGatewayUploadRejectsDeclaredLengthMismatchBeforeReading(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["length-mismatch"] = &deviceMedia{
		ClientID: "pet-upload", ID: "length-mismatch", Token: "token", SizeBytes: 3,
		LastAccessedAt: time.Now().UTC(),
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/length-mismatch/upload?mediaToken=token", reader)
	req.ContentLength = 2
	done := make(chan error, 1)
	go func() { done <- gateway.storeMediaUpload(req, "length-mismatch") }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "size mismatch") {
			t.Fatalf("length mismatch error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("upload read the request body before rejecting Content-Length")
	}
	if gateway.media["length-mismatch"].Uploading || gateway.media["length-mismatch"].UploadReservedBytes != 0 {
		t.Fatal("rejected Content-Length left an upload claim")
	}
}

func TestDeviceGatewayUploadStopsReadingAtDeclaredSize(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["oversized-body"] = &deviceMedia{
		ClientID: "pet-upload", ID: "oversized-body", Token: "token", SizeBytes: 3,
		LastAccessedAt: time.Now().UTC(),
	}
	req := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/oversized-body/upload?mediaToken=token", bytes.NewBufferString("abcd"))
	// Simulate a chunked request: expected media size still bounds the read.
	req.ContentLength = -1
	if err := gateway.storeMediaUpload(req, "oversized-body"); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("oversized body error=%v", err)
	}
	if gateway.media["oversized-body"].Uploaded || gateway.media["oversized-body"].Uploading {
		t.Fatal("oversized body was published or retained an upload claim")
	}
}

func TestDeviceGatewayCompletedUploadIsImmutableAndRetryIsIdempotent(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	gateway.media["immutable"] = &deviceMedia{
		ClientID: "pet-upload", ID: "immutable", Token: "token", SizeBytes: 3,
		Data: []byte("old"), Uploaded: true, LastAccessedAt: time.Now().UTC(),
	}
	retry := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/immutable/upload?mediaToken=token", bytes.NewBufferString("new"))
	retry.ContentLength = 3
	if err := gateway.storeMediaUpload(retry, "immutable"); err != nil {
		t.Fatalf("idempotent retry error=%v", err)
	}
	if got := string(gateway.media["immutable"].Data); got != "old" {
		t.Fatalf("completed media was overwritten with %q", got)
	}
}

func TestDeviceGatewayConcurrentUploadsReserveAggregateCapacity(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	const uploads = 3
	readers := make([]*io.PipeReader, 0, uploads)
	writers := make([]*io.PipeWriter, 0, uploads)
	done := make(chan error, uploads)
	for i := 0; i < uploads; i++ {
		id := fmt.Sprintf("reserved-%d", i)
		gateway.mu.Lock()
		gateway.media[id] = &deviceMedia{
			ClientID: "pet-reserved", ID: id, Token: "token", SizeBytes: deviceGatewayMaxMediaBytes / 2,
			LastAccessedAt: time.Now().UTC(),
		}
		gateway.mu.Unlock()
		reader, writer := io.Pipe()
		readers = append(readers, reader)
		writers = append(writers, writer)
		req := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/"+id+"/upload?mediaToken=token", reader)
		req.ContentLength = deviceGatewayMaxMediaBytes / 2
		go func(id string) { done <- gateway.storeMediaUpload(req, id) }(id)
	}
	defer func() {
		for _, reader := range readers {
			_ = reader.Close()
		}
		for _, writer := range writers {
			_ = writer.Close()
		}
		for range uploads {
			<-done
		}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		gateway.mu.Lock()
		allocated := gateway.mediaAllocatedBytesLocked("pet-reserved")
		gateway.mu.Unlock()
		if allocated >= 3*(deviceGatewayMaxMediaBytes/2) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("uploads did not claim reservations, allocated=%d", allocated)
		}
		time.Sleep(time.Millisecond)
	}
	gateway.mu.Lock()
	allocated := gateway.mediaAllocatedBytesLocked("pet-reserved")
	gateway.mu.Unlock()
	if allocated > deviceGatewayMaxMediaResidentBytesPerClient {
		t.Fatalf("concurrent reservations=%d exceed per-client budget=%d", allocated, deviceGatewayMaxMediaResidentBytesPerClient)
	}
	overflowID := "reserved-overflow"
	gateway.mu.Lock()
	gateway.media[overflowID] = &deviceMedia{ClientID: "pet-reserved", ID: overflowID, Token: "token", SizeBytes: deviceGatewayMaxMediaBytes / 2, LastAccessedAt: time.Now().UTC()}
	gateway.mu.Unlock()
	overflow := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/"+overflowID+"/upload?mediaToken=token", bytes.NewReader(nil))
	overflow.ContentLength = deviceGatewayMaxMediaBytes / 2
	if err := gateway.storeMediaUpload(overflow, overflowID); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("overflow reservation error=%v", err)
	}
}

func TestDeviceGatewayMediaResidentBudgetAndClientFairness(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	now := time.Now().UTC()
	protected := &deviceMedia{ClientID: "pet-b", ID: "pet-b-media", Data: bytes.Repeat([]byte{2}, 1024), Uploaded: true, LastAccessedAt: now.Add(-time.Hour)}
	gateway.media[protected.ID] = protected

	chunkSize := deviceGatewayMaxMediaResidentBytesPerClient / 4
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("pet-a-%d", i)
		gateway.mu.Lock()
		if !gateway.ensureMediaCapacityLocked("pet-a", 1, chunkSize, "", now.Add(time.Duration(i)*time.Second)) {
			gateway.mu.Unlock()
			t.Fatalf("capacity rejected media %d", i)
		}
		gateway.media[id] = &deviceMedia{ClientID: "pet-a", ID: id, Data: make([]byte, chunkSize), Uploaded: true, LastAccessedAt: now.Add(time.Duration(i) * time.Second)}
		gateway.mu.Unlock()
	}
	if gateway.media[protected.ID] == nil {
		t.Fatal("one client's quota pressure evicted another client's media")
	}
	gateway.mu.Lock()
	clientBytes := gateway.mediaResidentBytesLocked("pet-a")
	totalBytes := gateway.mediaResidentBytesLocked("")
	gateway.mu.Unlock()
	if clientBytes > deviceGatewayMaxMediaResidentBytesPerClient || totalBytes > deviceGatewayMaxMediaResidentBytes {
		t.Fatalf("resident bytes client=%d total=%d", clientBytes, totalBytes)
	}
}

func TestDeviceGatewayUploadCapacityDoesNotEvictItself(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	now := time.Now().UTC()
	media := &deviceMedia{ClientID: "pet-capacity", ID: "current", Token: "token", SizeBytes: deviceGatewayMaxMediaBytes, LastAccessedAt: now.Add(-time.Hour)}
	gateway.media[media.ID] = media
	gateway.media["old"] = &deviceMedia{ClientID: "pet-capacity", ID: "old", Data: bytes.Repeat([]byte{1}, int(deviceGatewayMaxMediaBytes)), Uploaded: true, LastAccessedAt: now.Add(-2 * time.Hour)}

	body := bytes.Repeat([]byte{3}, int(deviceGatewayMaxMediaBytes))
	req := httptest.NewRequest(http.MethodPut, "/api/im-gateway/v1/media/current/upload?mediaToken=token", bytes.NewReader(body))
	req.ContentLength = deviceGatewayMaxMediaBytes
	if err := gateway.storeMediaUpload(req, "current"); err != nil {
		t.Fatalf("upload error=%v", err)
	}
	if gateway.media["current"] == nil || !gateway.media["current"].Uploaded {
		t.Fatal("capacity eviction removed the in-flight media object")
	}
	if gateway.media["old"] != nil {
		t.Fatal("older same-client media was not reclaimed")
	}
}

func TestDeviceGatewayMediaExpiryIsAbsolute(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	now := time.Now().UTC()
	gateway.media["expired"] = &deviceMedia{
		ClientID: "pet-expired", ID: "expired", Token: "token", Data: []byte("old"),
		Uploaded: true, LastAccessedAt: now, ExpiresAt: now.Add(-time.Second),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/im-gateway/v1/media/expired?mediaToken=token", nil)
	if _, err := gateway.mediaForDownload(request, "expired"); err == nil {
		t.Fatal("expired media remained downloadable after recent access")
	}
	if gateway.media["expired"] != nil {
		t.Fatal("expired media was not removed")
	}
}

func TestDeviceGatewayLegacyMediaExpiryUsesIdleFallback(t *testing.T) {
	gateway := NewDeviceGateway(nil)
	now := time.Now().UTC()
	gateway.media["legacy"] = &deviceMedia{
		ClientID: "pet-legacy", ID: "legacy", Token: "token", Data: []byte("ok"),
		Uploaded: true, LastAccessedAt: now.Add(-deviceGatewayMediaTTL + time.Minute),
	}
	request := httptest.NewRequest(http.MethodGet, "/api/im-gateway/v1/media/legacy?mediaToken=token", nil)
	if media, err := gateway.mediaForDownload(request, "legacy"); err != nil || string(media.Data) != "ok" {
		t.Fatalf("legacy media download=%#v err=%v", media, err)
	}
}
