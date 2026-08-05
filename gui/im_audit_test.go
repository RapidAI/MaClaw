package main

import (
	"strings"
	"testing"
	"time"
)

func TestIMAuditFinalizerAcceptsThirdPartyWithoutChangingMessagePlatformKinds(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	for _, platform := range []string{"thirdparty", "thirdparty:esp32-pet"} {
		var result *IMAgentResponse
		finalize := h.imAuditFinalizer(IMUserMessage{
			UserID:   "device-user",
			Platform: platform,
			Text:     "hello",
		}, "hello", &result)
		if finalize == nil {
			t.Errorf("third-party platform %q was excluded from audit", platform)
		}
	}
	if got := normalizeIMMessagePlatformKind("thirdparty:esp32-pet"); got != imMessagePlatformUnknown {
		t.Fatalf("third-party unexpectedly changed global message platform kind to %q", got)
	}
}

func TestIMAuditFinalizerPersistsThirdPartyExchange(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	result := &IMAgentResponse{Text: "assistant reply"}
	finalize := h.imAuditFinalizer(IMUserMessage{
		UserID: "thirdparty:esp32-pet:default", Platform: "thirdparty:esp32-pet", Text: "user request",
	}, "user request", &result)
	if finalize == nil {
		t.Fatal("third-party audit finalizer was not installed")
	}
	finalize()
	store := h.app.getIMAuditStore()
	defer store.Close()
	deadline := time.Now().Add(time.Second)
	for {
		rows, err := store.Query("thirdparty", "", "", 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows.Messages) == 2 {
			if rows.Messages[0].Content != "user request" || rows.Messages[1].Content != "assistant reply" {
				t.Fatalf("unexpected exchange: %#v", rows.Messages)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("audit exchange was not persisted: %#v", rows.Messages)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestIMAuditFinalizerStillSkipsDesktopAndUnknownPlaybackTargets(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	for _, platform := range []string{"desktop", "tui", "unrelated-transport"} {
		var result *IMAgentResponse
		if finalize := h.imAuditFinalizer(IMUserMessage{
			UserID: "user", Platform: platform, Text: "hello",
		}, "hello", &result); finalize != nil {
			t.Errorf("platform %q should remain excluded from IM audit", platform)
		}
	}
}

func TestIMAuditThirdPartyWritesWaitForQueueCapacity(t *testing.T) {
	store := &IMAuditStore{
		writeCh: make(chan IMAuditMessage, 1),
		closing: make(chan struct{}),
	}
	store.writeCh <- IMAuditMessage{Content: "occupy queue"}
	h := &IMMessageHandler{}
	done := make(chan struct{})
	go func() {
		h.writeIMAuditMessage(store, IMAuditMessage{Platform: "thirdparty:esp32", Content: "must not drop"})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("third-party audit write returned while the queue was full")
	case <-time.After(20 * time.Millisecond):
	}
	<-store.writeCh
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("third-party audit write did not resume after queue capacity was available")
	}
	if got := (<-store.writeCh).Content; got != "must not drop" {
		t.Fatalf("queued content=%q", got)
	}
}

func TestIMAuditFinalizerPersistsAttachmentOnlyThirdPartyMessageWithoutPayload(t *testing.T) {
	h := &IMMessageHandler{app: &App{testHomeDir: t.TempDir()}}
	result := &IMAgentResponse{Text: "image received"}
	finalize := h.imAuditFinalizer(IMUserMessage{
		UserID: "thirdparty:esp32:default", Platform: "thirdparty:esp32",
		Attachments: []MessageAttachment{{Type: "image", FileName: "camera.jpg", MimeType: "image/jpeg", Data: "sensitive-base64"}},
	}, "", &result)
	if finalize == nil {
		t.Fatal("attachment-only third-party message was excluded from audit")
	}
	finalize()
	store := h.app.getIMAuditStore()
	defer store.Close()
	rows, err := store.Query("thirdparty", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Messages) != 2 || rows.Messages[0].Content != "[image: camera.jpg]" {
		t.Fatalf("unexpected attachment audit rows: %#v", rows.Messages)
	}
	if strings.Contains(rows.Messages[0].Content, "sensitive-base64") {
		t.Fatal("attachment payload leaked into audit history")
	}
}

func TestNormalizeHubThirdPartyAuditIdentityRecoversDevicePlatform(t *testing.T) {
	platform, userID := normalizeHubThirdPartyAuditIdentity("thirdparty", "thirdparty:ESP32-PET:default")
	if platform != "thirdparty:esp32-pet" || userID != "thirdparty:ESP32-PET:default" {
		t.Fatalf("platform=%q userID=%q", platform, userID)
	}
	for _, test := range []struct{ platform, userID string }{
		{platform: "telegram", userID: "thirdparty:esp32:default"},
		{platform: "thirdparty", userID: "ordinary-user"},
	} {
		gotPlatform, gotUserID := normalizeHubThirdPartyAuditIdentity(test.platform, test.userID)
		if gotPlatform != test.platform || gotUserID != test.userID {
			t.Fatalf("identity %q/%q changed to %q/%q", test.platform, test.userID, gotPlatform, gotUserID)
		}
	}
}
