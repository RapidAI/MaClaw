package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestThirdPartyGatewayEnqueueEnforcesClientCapabilities(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("text-device", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 5},
	}})
	m.enqueue("text-device", thirdPartyOutgoingMessage{Type: "image", Data: "png"})
	m.enqueue("text-device", thirdPartyOutgoingMessage{Type: "text", Text: "123456789"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["text-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "text" || messages[0].Text != "12345" {
		t.Fatalf("adapted messages=%#v", messages)
	}
}

func TestThirdPartyGatewayAgentResponseSelectsPreferredLegalCombination(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("combo-device", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities:   []string{"text", "image"},
		Preferred:    []string{"image", "text"},
		Combinations: [][]string{{"text"}, {"image"}},
		Text:         &agent.ClientTextCapabilities{},
		Image:        &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	m.enqueueAgentResponse("combo-device", "room", "in-1", &IMAgentResponse{Text: "answer", ImageKey: "png"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["combo-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "image" || messages[0].Text != "" {
		t.Fatalf("preferred legal combination not enforced: %#v", messages)
	}
	if messages[0].Metadata["acp_turn"] != "final" {
		t.Fatalf("selected media response must terminate ACP turn: %#v", messages[0].Metadata)
	}
}

func TestThirdPartyGatewayAgentResponseAllowsDeclaredMultimodalCombination(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("combo-device", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities:   []string{"text", "image"},
		Combinations: [][]string{{"text", "image"}},
		Text:         &agent.ClientTextCapabilities{},
		Image:        &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
	}})
	m.enqueueAgentResponse("combo-device", "room", "in-1", &IMAgentResponse{Text: "answer", ImageKey: "png"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["combo-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 2 || messages[0].Type != "text" || messages[1].Type != "image" {
		t.Fatalf("declared multimodal combination not delivered: %#v", messages)
	}
}

func TestThirdPartyGatewayAgentResponseEnqueuesVoiceBeforeText(t *testing.T) {
	// ESP32 firmware ends the current command when a text message arrives, so
	// the voice reply must be queued first or it is dropped as unrelated.
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("pet", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities:   []string{"text", "audio"},
		Combinations: [][]string{{"text", "audio"}},
		Text:         &agent.ClientTextCapabilities{},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes: []string{"inline"}, MaxInlineBytes: 8192, MaxDownloadBytes: 262144,
		},
	}})
	m.enqueueAgentResponse("pet", "room", "in-1", &IMAgentResponse{
		Text:          "answer",
		VoiceData:     base64.StdEncoding.EncodeToString([]byte("RIFF-voice")),
		VoiceFileName: "reply.wav",
		VoiceMimeType: "audio/wav",
	})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["pet"].Messages...)
	m.mu.Unlock()
	if len(messages) != 2 || messages[0].Type != "voice" || messages[1].Type != "text" {
		t.Fatalf("voice must be enqueued before text: %#v", messages)
	}
	if messages[0].ReplyToMessageID != "in-1" || messages[1].ReplyToMessageID != "in-1" {
		t.Fatalf("both messages must reply to the same command: %#v", messages)
	}
}

func TestThirdPartyGatewayAgentResponseKeepsTextWhenAudioCombinationWins(t *testing.T) {
	// A client declaring [text,audio] with only singleton combinations
	// resolves to ["audio"]; the text reply must still be enqueued or old
	// firmware that reads text alongside voice regresses to silence.
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("pet", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities:   []string{"text", "audio"},
		Preferred:    []string{"audio", "text"},
		Combinations: [][]string{{"text"}, {"audio"}},
		Text:         &agent.ClientTextCapabilities{},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes: []string{"inline"}, MaxInlineBytes: 8192, MaxDownloadBytes: 262144,
		},
	}})
	m.enqueueAgentResponse("pet", "room", "in-1", &IMAgentResponse{
		Text:          "answer",
		VoiceData:     base64.StdEncoding.EncodeToString([]byte("RIFF-voice")),
		VoiceFileName: "reply.wav",
		VoiceMimeType: "audio/wav",
	})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["pet"].Messages...)
	m.mu.Unlock()
	if len(messages) != 2 || messages[0].Type != "voice" || messages[1].Type != "text" {
		t.Fatalf("text must be kept alongside voice: %#v", messages)
	}
}
func TestThirdPartyGatewayEnqueuePreservesHardwareFeatureMessages(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("pet", &agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{
			Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 5},
		},
		Features: agent.ClientFeatureCapabilities{PetStates: true, AmbientDisplay: true, MeetingRecorder: true},
	})
	m.enqueue("pet", thirdPartyOutgoingMessage{ID: "pet-state", Type: "pet_state", Extra: map[string]any{"state": "thinking"}})
	m.enqueue("pet", thirdPartyOutgoingMessage{ID: "ambient", Type: "ambient", Extra: map[string]any{"weather": "sunny"}})
	m.enqueue("pet", thirdPartyOutgoingMessage{ID: "meeting", Type: "meeting_result", Text: "123456789"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["pet"].Messages...)
	m.mu.Unlock()
	if len(messages) != 3 || messages[0].Type != "pet_state" || messages[1].Type != "ambient" || messages[2].Type != "meeting_result" {
		t.Fatalf("feature messages=%#v", messages)
	}
	if messages[2].Text != "12345" {
		t.Fatalf("meeting result text=%q", messages[2].Text)
	}
}
func TestThirdPartyGatewayHandshakeCapabilitiesReachLocalAgentContract(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("pet", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{MaxChars: 240, Locale: "zh-CN"},
	}})
	capabilities := m.clientCapabilities("pet")
	prompt := agent.BuildClientCapabilityPrompt(&capabilities)
	if !strings.Contains(prompt, "Output modalities: text") || !strings.Contains(prompt, "max 240 Unicode characters") {
		t.Fatalf("capability prompt=%q", prompt)
	}
}

func TestThirdPartyGatewayHandshakeQueuesPersistedVolume(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("volume-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{VolumeControl: true}})
	m.enqueueHardwareConfigForClient("volume-device", 0)
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["volume-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "hardware_config" || messages[0].Extra["volume"] != 0 {
		t.Fatalf("persisted volume message=%#v", messages)
	}
}

func TestThirdPartyGatewayDoesNotDuplicatePendingVolume(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("volume-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{VolumeControl: true}})
	m.enqueueHardwareConfigForClient("volume-device", 55)
	m.enqueueHardwareConfigForClient("volume-device", 55)
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["volume-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Extra["volume"] != 55 {
		t.Fatalf("duplicate local volume messages=%#v", messages)
	}
}

func TestThirdPartyGatewayPendingVolumeKeepsLatestValue(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("volume-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{VolumeControl: true}})
	for _, volume := range []int{22, 78, 41} {
		m.enqueueHardwareConfigForClient("volume-device", volume)
	}
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["volume-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Extra["volume"] != 41 {
		t.Fatalf("latest local volume message=%#v", messages)
	}
}

func TestThirdPartyGatewayBroadcastVolumeKeepsLatestValue(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("volume-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{VolumeControl: true}})
	for _, volume := range []int{22, 78, 41} {
		m.broadcastHardwareConfig(map[string]any{"volume": volume})
	}
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["volume-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Extra["volume"] != 41 {
		t.Fatalf("latest local broadcast volume=%#v", messages)
	}
}

func TestThirdPartyGatewayBroadcastPetProfile(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("animated-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{PetAnimation: true, PetAsset: true}})
	m.setClientCapabilities("plain-device", &agent.ClientCapabilities{})
	m.mu.Lock()
	notify := m.notifyCh
	m.mu.Unlock()
	profile := map[string]any{
		"skin":          "tiger",
		"motionEnabled": false,
		// The broadcast path validates the asset before publishing it as a
		// media-URL reference, so the fixture must be a well-formed rgb565le
		// frame (width*height*2 bytes after base64 decode).
		"asset": devicePetAsset{Encoding: "rgb565le", Width: 128, Height: 128,
			Data: base64.StdEncoding.EncodeToString(make([]byte, 128*128*2))},
	}
	m.broadcastPetProfile(profile)
	for _, clientID := range []string{"animated-device", "plain-device"} {
		m.mu.Lock()
		messages := append([]thirdPartyOutgoingMessage(nil), m.clients[clientID].Messages...)
		m.mu.Unlock()
		if len(messages) != 1 || messages[0].Type != "pet_profile" {
			t.Fatalf("%s pet messages=%#v", clientID, messages)
		}
		msg := messages[0]
		if msg.PetSkin != "tiger" {
			t.Fatalf("%s pet_skin=%q", clientID, msg.PetSkin)
		}
		if msg.PetMotionEnabled == nil || *msg.PetMotionEnabled {
			t.Fatalf("%s pet_motion_enabled=%v", clientID, msg.PetMotionEnabled)
		}
		if clientID == "animated-device" {
			// The asset is delivered as a media-URL reference, never inline.
			ref, ok := msg.Extra["pet_asset"].(map[string]any)
			if !ok || ref["revision"] == "" {
				t.Fatalf("animated device missing pet_asset reference: %#v", msg.Extra)
			}
			urls, ok := ref["urls"].([]string)
			if !ok || len(urls) != 1 || !strings.Contains(urls[0], "/api/im-gateway/v1/media/") {
				t.Fatalf("animated device pet_asset urls=%#v", ref["urls"])
			}
		} else if msg.Extra != nil {
			t.Fatalf("plain device should not receive the bulky asset: %#v", msg.Extra)
		}
	}
	select {
	case <-notify:
	case <-time.After(time.Second):
		t.Fatal("broadcastPetProfile did not wake long-polling clients")
	}
}

func TestThirdPartyGatewayBroadcastPetProfileKeepsLatestValue(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("animated-device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{PetAnimation: true, PetAsset: true}})
	asset := devicePetAsset{Encoding: "rgb565le", Width: 128, Height: 128,
		Data: base64.StdEncoding.EncodeToString(make([]byte, 128*128*2))}
	profiles := []map[string]any{
		{"skin": "tiger", "motionEnabled": true, "asset": asset},
		{"skin": "panda", "motionEnabled": false, "asset": asset},
		{"skin": "fox", "motionEnabled": true, "asset": asset},
	}
	for _, profile := range profiles {
		m.broadcastPetProfile(profile)
	}
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["animated-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("pending pet_profile messages=%#v", messages)
	}
	msg := messages[0]
	if msg.PetSkin != "fox" {
		t.Fatalf("coalesced pet_skin=%q", msg.PetSkin)
	}
	if msg.PetMotionEnabled == nil || !*msg.PetMotionEnabled {
		t.Fatalf("coalesced pet_motion_enabled=%v", msg.PetMotionEnabled)
	}
	if ref, ok := msg.Extra["pet_asset"].(map[string]any); !ok || ref["revision"] == "" {
		t.Fatalf("coalesced message lost pet_asset: %#v", msg.Extra)
	}
}

func TestThirdPartyGatewayBroadcastPetProfileOmitsMissingMotionEnabled(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("plain-device", &agent.ClientCapabilities{})
	m.broadcastPetProfile(map[string]any{"skin": "tiger"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["plain-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].PetMotionEnabled != nil {
		t.Fatalf("missing motionEnabled must stay nil, messages=%#v", messages)
	}
	// A later profile that includes the key still updates the queued message.
	m.broadcastPetProfile(map[string]any{"skin": "fox", "motionEnabled": false})
	m.mu.Lock()
	messages = append([]thirdPartyOutgoingMessage(nil), m.clients["plain-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].PetMotionEnabled == nil || *messages[0].PetMotionEnabled {
		t.Fatalf("merged pet_motion_enabled=%v, messages=%#v", messages[0].PetMotionEnabled, messages)
	}
	// A merge whose profile omits the key must preserve the queued value
	// instead of resetting it to nil.
	m.broadcastPetProfile(map[string]any{"skin": "panda"})
	m.mu.Lock()
	messages = append([]thirdPartyOutgoingMessage(nil), m.clients["plain-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("merge appended instead of rewriting: %#v", messages)
	}
	if messages[0].PetSkin != "panda" {
		t.Fatalf("merged pet_skin=%q", messages[0].PetSkin)
	}
	if messages[0].PetMotionEnabled == nil || *messages[0].PetMotionEnabled {
		t.Fatalf("omitted motionEnabled reset the queued value: %v", messages[0].PetMotionEnabled)
	}
}

func TestThirdPartyGatewayBroadcastPetProfilePreservesMissingSkin(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("plain-device", &agent.ClientCapabilities{})
	m.broadcastPetProfile(map[string]any{"skin": "tiger", "motionEnabled": true})
	// A profile without the "skin" key must not blank the queued pet_skin.
	m.broadcastPetProfile(map[string]any{"motionEnabled": false})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["plain-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 {
		t.Fatalf("merge appended instead of rewriting: %#v", messages)
	}
	if messages[0].PetSkin != "tiger" {
		t.Fatalf("missing skin key blanked the queued value: %q", messages[0].PetSkin)
	}
	if messages[0].PetMotionEnabled == nil || *messages[0].PetMotionEnabled {
		t.Fatalf("merged pet_motion_enabled=%v", messages[0].PetMotionEnabled)
	}
}

func TestThirdPartyGatewayBroadcastPetProfileDoesNotMergeAcked(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("plain-device", &agent.ClientCapabilities{})
	// Hand-build an acked pet_profile that still sits in Messages (the state
	// right after handleAck, before prune removes it): the merge must skip it
	// and append a fresh message instead of rewriting history.
	m.mu.Lock()
	state := m.ensureClientLocked("plain-device")
	state.Messages = append(state.Messages, thirdPartyOutgoingMessage{
		ID:        "mc_pet_acked",
		Seq:       1,
		Type:      "pet_profile",
		PetSkin:   "tiger",
		CreatedAt: 1,
	})
	state.Acked["mc_pet_acked"] = "delivered"
	state.NextSeq = 2
	m.mu.Unlock()
	m.broadcastPetProfile(map[string]any{"skin": "fox"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["plain-device"].Messages...)
	m.mu.Unlock()
	// Correct behavior: the acked entry is skipped, a fresh message is
	// appended, and prune then evicts the acked one — leaving exactly one
	// message with a new ID/Seq. Had the merge rewritten the acked entry in
	// place, the update would have died with it (ID "mc_pet_acked", Seq 1).
	if len(messages) != 1 {
		t.Fatalf("messages after broadcast=%#v", messages)
	}
	if messages[0].ID == "mc_pet_acked" || messages[0].Seq != 2 {
		t.Fatalf("acked message was rewritten instead of appending: %#v", messages[0])
	}
	if messages[0].PetSkin != "fox" {
		t.Fatalf("appended message pet_skin=%q", messages[0].PetSkin)
	}
}

func TestThirdPartyGatewayHardwareMessagesKeepCursorSequenceContiguous(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("device", &agent.ClientCapabilities{
		Output: agent.ClientOutputCapabilities{Modalities: []string{"audio"}, Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true, DeliveryModes: []string{"inline", "url"}, MaxInlineBytes: 1024, MaxDownloadBytes: 1024,
		}},
		Features: agent.ClientFeatureCapabilities{VolumeControl: true},
	})
	m.enqueueHardwareConfigForClient("device", 31)
	m.enqueueHardwareAudioForBoot("device", "boot-a", base64.StdEncoding.EncodeToString([]byte("wav")))
	m.broadcastHardwareConfig(map[string]any{"volume": 32})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 2 {
		t.Fatalf("hardware messages=%#v", messages)
	}
	if messages[0].Type != "hardware_config" || messages[0].Extra["volume"] != 32 {
		t.Fatalf("latest volume was not coalesced: %#v", messages)
	}
	for index, message := range messages {
		want := int64(index + 1)
		if message.Seq != want {
			t.Fatalf("message %d seq=%d, want contiguous %d: %#v", index, message.Seq, want, messages)
		}
	}
	polled, next, hasMore := m.messagesAfter("device", 0, 3)
	if len(polled) != 2 || next != 2 || hasMore {
		t.Fatalf("cursor poll=%#v next=%d hasMore=%t", polled, next, hasMore)
	}
}

func TestThirdPartyGatewayAckPrunesDeliveredMessages(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("device", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text"}, Text: &agent.ClientTextCapabilities{},
	}})
	queued := m.enqueue("device", thirdPartyOutgoingMessage{Type: "text", Text: "done"})
	m.ack("device", thirdPartyAckRequest{ClientID: "device", MessageIDs: []string{queued.ID}, Status: "delivered"})
	m.mu.Lock()
	state := m.clients["device"]
	messageCount := len(state.Messages)
	ackCount := len(state.Acked)
	m.mu.Unlock()
	if messageCount != 0 || ackCount != 0 {
		t.Fatalf("delivered messages were retained: messages=%d acked=%d", messageCount, ackCount)
	}
}

func TestThirdPartyGatewayPruneKeepsUnacknowledgedControlMessage(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("device", &agent.ClientCapabilities{Features: agent.ClientFeatureCapabilities{VolumeControl: true}})
	m.mu.Lock()
	state := m.clients["device"]
	for seq := int64(1); seq <= thirdPartyHistoryLimit; seq++ {
		id := fmt.Sprintf("delivered-%d", seq)
		state.Messages = append(state.Messages, thirdPartyOutgoingMessage{ID: id, Seq: seq, Type: "text"})
		state.Acked[id] = "delivered"
	}
	state.NextSeq = thirdPartyHistoryLimit + 1
	m.mu.Unlock()
	m.enqueueHardwareConfigForClient("device", 26)
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "hardware_config" || messages[0].Extra["volume"] != 26 {
		t.Fatalf("unacknowledged control message was lost after history prune: %#v", messages)
	}
}

func TestThirdPartyGatewayHandshakeQueuesHardwareConfigBeforeResponse(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if _, err := app.PatchConfigFields(map[string]interface{}{"hardware_volume": 43}); err != nil {
		t.Fatalf("save volume: %v", err)
	}
	m := newThirdPartyGatewayManager(app)
	req := httptest.NewRequest(http.MethodPost, "/api/im-gateway/v1/handshake", strings.NewReader(`{"clientId":"volume-device","clientCapabilities":{"features":{"volumeControl":true}}}`))
	req.Header.Set("Authorization", "Bearer gateway-token")
	// Supply the expected token through the app config; the request must finish
	// with the queued control message already visible to an immediate poll.
	if _, err := app.PatchConfigFields(map[string]interface{}{"thirdparty_gateway_token": "gateway-token"}); err != nil {
		t.Fatalf("save token: %v", err)
	}
	response := httptest.NewRecorder()
	m.handleHandshake(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("handshake status=%d body=%s", response.Code, response.Body.String())
	}
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["volume-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].Type != "hardware_config" || messages[0].Extra["volume"] != 43 {
		encoded, _ := json.Marshal(messages)
		t.Fatalf("handshake did not synchronously queue volume: %s", encoded)
	}
}

func TestThirdPartyGatewayEnqueueRejectsUnsupportedMediaEncodingAndOversizedFile(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("media-device", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"image", "audio", "file"},
		Image:      &agent.ClientImageCapabilities{MimeTypes: []string{"image/png"}},
		Audio:      &agent.ClientAudioCapabilities{MimeTypes: []string{"audio/wav"}, Playback: true},
		File:       &agent.ClientFileCapabilities{MimeTypes: []string{"application/pdf"}, MaxBytes: 8},
	}})
	m.enqueue("media-device", thirdPartyOutgoingMessage{ID: "jpg", Type: "image", MimeType: "image/jpeg", Data: "x"})
	m.enqueue("media-device", thirdPartyOutgoingMessage{ID: "mp3", Type: "audio", MimeType: "audio/mpeg", Data: "x"})
	m.enqueue("media-device", thirdPartyOutgoingMessage{ID: "big", Type: "file", MimeType: "application/pdf", SizeBytes: 9, Data: "x"})
	m.enqueue("media-device", thirdPartyOutgoingMessage{ID: "png", Type: "image", MimeType: "image/png", Data: "x"})
	m.mu.Lock()
	messages := append([]thirdPartyOutgoingMessage(nil), m.clients["media-device"].Messages...)
	m.mu.Unlock()
	if len(messages) != 1 || messages[0].ID != "png" {
		t.Fatalf("media capability filtering=%#v", messages)
	}
}

func TestDecodeThirdPartyMediaSupportsBase64(t *testing.T) {
	raw := []byte("image-bytes")
	data, name, mimeType, err := decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:     "image",
		FileName: "screen.png",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(raw),
	})
	if err != nil {
		t.Fatalf("decode base64 media: %v", err)
	}
	if string(data) != string(raw) || name != "screen.png" || mimeType != "image/png" {
		t.Fatalf("unexpected base64 media: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyGatewayAudioUsesMediaURLWhenInlineLimitIsExceeded(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.setClientCapabilities("audio-url", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"audio"},
		Audio: &agent.ClientAudioCapabilities{
			MimeTypes: []string{"audio/wav"}, Playback: true,
			DeliveryModes: []string{"inline", "url"}, MaxInlineBytes: 4, MaxDownloadBytes: 1024,
		},
	}})
	wav := []byte("0123456789")
	queued := m.enqueue("audio-url", thirdPartyOutgoingMessage{
		Type: "audio", MimeType: "audio/wav", Data: base64.StdEncoding.EncodeToString(wav),
	})
	if queued.ID == "" || queued.Data != "" || queued.URL == "" || queued.SizeBytes != int64(len(wav)) {
		t.Fatalf("URL audio message=%#v", queued)
	}
	request := httptest.NewRequest(http.MethodGet, queued.URL, nil)
	response := httptest.NewRecorder()
	m.handleMedia(response, request)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), wav) {
		t.Fatalf("media download status=%d body=%q", response.Code, response.Body.Bytes())
	}
}

func TestThirdPartyServerMediaRequestFromURLRejectsUnsafeInputs(t *testing.T) {
	if _, _, err := thirdPartyServerMediaRequestFromURL("ftp://example.test/secret.txt"); err == nil || !strings.Contains(err.Error(), "http or https") {
		t.Fatalf("expected file URL rejection, got %v", err)
	}
	if _, _, err := thirdPartyServerMediaRequestFromURL("https://third-party.example.test/file.pdf"); err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected external URL rejection, got %v", err)
	}
	id, req, err := thirdPartyServerMediaRequestFromURL("http://127.0.0.1:18777/api/im-gateway/v1/media/media-123?mediaToken=token")
	if err != nil {
		t.Fatalf("parse server media URL: %v", err)
	}
	if id != "media-123" || req.URL.Query().Get("mediaToken") != "token" {
		t.Fatalf("unexpected server media request: id=%q url=%s", id, req.URL.String())
	}
}

func TestThirdPartyGatewayServerMediaUploadFlow(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID:  "client-a",
		Type:      "file",
		FileName:  "report.pdf",
		MimeType:  "application/pdf",
		SizeBytes: 8,
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	if prepared.Media.URL == "" || prepared.Upload.URL == "" || prepared.Upload.Method != http.MethodPut {
		t.Fatalf("unexpected prepared media: %#v", prepared)
	}

	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("pdf-body"))
	uploadReq.Header.Set("Content-Type", "application/pdf")
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}
	downloadReq := httptest.NewRequest(http.MethodGet, prepared.Download.URL, nil)
	media, err := m.mediaForDownload(downloadReq, prepared.Media.ID)
	if err != nil {
		t.Fatalf("mediaForDownload: %v", err)
	}
	if string(media.Data) != "pdf-body" || media.MimeType != "application/pdf" || media.FileName != "report.pdf" {
		t.Fatalf("unexpected media: %#v", media)
	}
}

func TestThirdPartyGatewayRejectsUploadSizeMismatch(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID:  "client-a",
		Type:      "file",
		FileName:  "report.pdf",
		MimeType:  "application/pdf",
		SizeBytes: 8,
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("short"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected size mismatch error, got %v", err)
	}
}

func TestThirdPartyGatewayLocalMessageCanReadServerMediaIDOnly(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}

	data, name, mimeType, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{ID: prepared.Media.ID, Type: "file"}},
	})
	if err != nil {
		t.Fatalf("decode server media by id: %v", err)
	}
	if string(data) != "report-body" || name != "report.txt" || mimeType != "text/plain" {
		t.Fatalf("unexpected server media decode: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyGatewayLocalMessageCanReadServerMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}

	data, name, mimeType, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", URL: prepared.Media.URL}},
	})
	if err != nil {
		t.Fatalf("decode server media by url: %v", err)
	}
	if string(data) != "report-body" || name != "report.txt" || mimeType != "text/plain" {
		t.Fatalf("unexpected server media decode: data=%q name=%q mime=%q", string(data), name, mimeType)
	}
}

func TestThirdPartyGatewayRejectsExternalMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	_, _, _, err := m.decodeThirdPartyMedia(thirdPartyMessagePayload{
		Type:        "file",
		Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", FileName: "report.pdf", URL: "https://third-party.example.test/report.pdf"}},
	})
	if err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected external media URL rejection, got %v", err)
	}
}

func TestThirdPartyGatewayValidateIncomingRejectsExternalMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	req := thirdPartyIncomingRequest{
		ClientID:       "client-a",
		EventID:        "evt-url",
		MessageID:      "msg-url",
		ConversationID: "room-a",
		User:           thirdPartyUserRef{ID: "user-a"},
		Message: thirdPartyMessagePayload{
			Type:        "file",
			Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", FileName: "report.pdf", URL: "https://third-party.example.test/report.pdf"}},
		},
	}
	if err := normalizeIncomingRequest(&req); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := m.validateIncomingMediaReferences(&req); err == nil || !strings.Contains(err.Error(), "server media URL") {
		t.Fatalf("expected server media URL error, got %v", err)
	}
}

func TestThirdPartyGatewayValidateIncomingAcceptsServerMediaURL(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	prepared, err := m.prepareMedia(coreim.ThirdPartyMediaPrepareRequest{
		ClientID: "client-a",
		Type:     "file",
		FileName: "report.txt",
		MimeType: "text/plain",
	}, "http://127.0.0.1:18777/api/im-gateway/v1")
	if err != nil {
		t.Fatalf("prepareMedia: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPut, prepared.Upload.URL, strings.NewReader("report-body"))
	if err := m.storeMediaUpload(uploadReq, prepared.Media.ID); err != nil {
		t.Fatalf("storeMediaUpload: %v", err)
	}
	req := thirdPartyIncomingRequest{
		ClientID:       "client-a",
		EventID:        "evt-url",
		MessageID:      "msg-url",
		ConversationID: "room-a",
		User:           thirdPartyUserRef{ID: "user-a"},
		Message: thirdPartyMessagePayload{
			Type:        "file",
			Attachments: []coreim.ThirdPartyMediaReference{{Type: "file", URL: prepared.Media.URL}},
		},
	}
	if err := normalizeIncomingRequest(&req); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := m.validateIncomingMediaReferences(&req); err != nil {
		t.Fatalf("validate incoming media: %v", err)
	}
	if req.Message.Attachments[0].ID != prepared.Media.ID || req.Message.Attachments[0].FileName != "report.txt" {
		t.Fatalf("expected server URL normalized to media metadata: %#v", req.Message.Attachments[0])
	}
}

func TestThirdPartyGatewayRequestBaseURLSanitizesForwardedHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/im-gateway/v1/media/upload-url", nil)
	req.Host = "gateway.example.test"
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "evil.example\\@bad")
	if got := thirdPartyGatewayRequestBaseURL(req); got != "http://127.0.0.1/api/im-gateway/v1" {
		t.Fatalf("bad forwarded headers should be sanitized, got %q", got)
	}

	req.Header.Set("X-Forwarded-Proto", "https, http")
	req.Header.Set("X-Forwarded-Host", "maclaw.example.test:18443, proxy.local")
	if got := thirdPartyGatewayRequestBaseURL(req); got != "https://maclaw.example.test:18443/api/im-gateway/v1" {
		t.Fatalf("safe forwarded headers should be preserved, got %q", got)
	}
}

func TestThirdPartyGatewayAckSuppressesRedelivery(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	m.enqueue("client-a", thirdPartyOutgoingMessage{ID: "out-1", Type: "text", Text: "one"})
	m.enqueue("client-a", thirdPartyOutgoingMessage{ID: "out-2", Type: "text", Text: "two"})

	req := thirdPartyAckRequest{ClientID: "client-a", MessageIDs: []string{"missing", "out-1"}, Status: "ok"}
	if err := coreim.NormalizeThirdPartyAckRequest(&req, thirdPartyMaxAckIDs); err != nil {
		t.Fatalf("NormalizeThirdPartyAckRequest: %v", err)
	}
	m.ack(req.ClientID, req)

	msgs, next, hasMore := m.messagesAfter("client-a", 0, 20)
	if len(msgs) != 1 || msgs[0].ID != "out-2" || next != 2 || hasMore {
		t.Fatalf("acked message should not be redelivered: msgs=%#v next=%d hasMore=%v", msgs, next, hasMore)
	}
	if _, ok := m.clients["client-a"].Acked["missing"]; ok {
		t.Fatalf("unknown ack id should not be stored")
	}
}

func TestThirdPartyGatewayVoicePairRateLimit(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	for i := 0; i < voicePairMaxAttempts; i++ {
		if !m.allowVoicePairAttempt("192.168.1.10") {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if m.allowVoicePairAttempt("192.168.1.10") {
		t.Fatal("attempt beyond the window limit should be rejected")
	}
	// A different source IP has its own budget.
	if !m.allowVoicePairAttempt("192.168.1.11") {
		t.Fatal("attempts from another IP must not share the budget")
	}
	// Expired attempts stop counting against the window.
	m.mu.Lock()
	m.voicePairAttempts["192.168.1.10"] = []time.Time{time.Now().Add(-2 * voicePairWindow)}
	m.mu.Unlock()
	if !m.allowVoicePairAttempt("192.168.1.10") {
		t.Fatal("expired attempts must free the window")
	}
}

func TestThirdPartyGatewayVoicePairHandlerRateLimited(t *testing.T) {
	m := newThirdPartyGatewayManager(nil)
	for i := 0; i < voicePairMaxAttempts; i++ {
		m.allowVoicePairAttempt("192.168.1.12")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/device-gateway/v1/pair/voice", strings.NewReader("x"))
	req.RemoteAddr = "192.168.1.12:4321"
	rec := httptest.NewRecorder()
	m.handleDeviceVoicePair(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited voice pair status=%d body=%s", rec.Code, rec.Body.String())
	}
}
