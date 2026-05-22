package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestIsVEEvent(t *testing.T) {
	tests := []struct {
		msgType string
		want    bool
	}{
		{"ve:list_update", true},
		{"ve:status_change", true},
		{"ve:auth_request", true},
		{"ve:approved", true},
		{"ve:rejected", true},
		{"ve:disabled", true},
		{"ve:group_config", true},
		{"session.start", false},
		{"im.user_message", false},
		{"error", false},
		{"", false},
		{"ve", false},             // no colon
		{"VE:list_update", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.msgType, func(t *testing.T) {
			got := isVEEvent(tt.msgType)
			if got != tt.want {
				t.Errorf("isVEEvent(%q) = %v, want %v", tt.msgType, got, tt.want)
			}
		})
	}
}

func TestNormalizeHubInboundMessageType_VEEvents(t *testing.T) {
	tests := []struct {
		msgType string
		want    hubInboundMessageType
	}{
		{"ve:list_update", hubInboundMessageVEEvent},
		{"ve:status_change", hubInboundMessageVEEvent},
		{"ve:auth_request", hubInboundMessageVEEvent},
		{"ve:approved", hubInboundMessageVEEvent},
		{"ve:rejected", hubInboundMessageVEEvent},
		{"ve:disabled", hubInboundMessageVEEvent},
		{"ve:group_config", hubInboundMessageVEEvent},
		// Non-VE events should not match
		{"session.start", hubInboundMessageSessionStart},
		{"im.user_message", hubInboundMessageIMUserMessage},
		{"error", hubInboundMessageError},
		{"ack", hubInboundMessageAck},
		// Unknown types
		{"unknown_type", hubInboundMessageUnknown},
		{"", hubInboundMessageUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.msgType, func(t *testing.T) {
			got := normalizeHubInboundMessageType(tt.msgType)
			if got != tt.want {
				t.Errorf("normalizeHubInboundMessageType(%q) = %q, want %q", tt.msgType, got, tt.want)
			}
		})
	}
}

func TestNormalizeHubInboundMessageType_VEEventWithWhitespace(t *testing.T) {
	// Whitespace should be trimmed
	got := normalizeHubInboundMessageType("  ve:list_update  ")
	if got != hubInboundMessageVEEvent {
		t.Errorf("expected hubInboundMessageVEEvent, got %q", got)
	}
}

func TestShouldHandleIncomingDigitalEmployeeMessage(t *testing.T) {
	for _, kind := range []a2a.MessageKind{"", a2a.MessageQuestion, a2a.MessageStatement} {
		if !shouldHandleIncomingDigitalEmployeeMessage(kind) {
			t.Fatalf("kind %q should be handled by the digital employee", kind)
		}
	}
	for _, kind := range []a2a.MessageKind{a2a.MessageAnswer, a2a.MessageStreamChunk, a2a.MessageStreamEnd, a2a.MessageHandoff} {
		if shouldHandleIncomingDigitalEmployeeMessage(kind) {
			t.Fatalf("kind %q should not trigger a digital employee reply", kind)
		}
	}
}

func TestShouldDigitalEmployeeRespondToDiscussionHonorsRole(t *testing.T) {
	for _, role := range []string{"", "speak", "speaker", "review", "participant"} {
		if !shouldDigitalEmployeeRespondToDiscussion(role, a2a.MessageStatement) {
			t.Fatalf("role %q should allow digital employee response", role)
		}
	}
	for _, role := range []string{"initiator", "observe", "observer", "readonly", "read_only"} {
		if shouldDigitalEmployeeRespondToDiscussion(role, a2a.MessageStatement) {
			t.Fatalf("role %q should not allow digital employee response", role)
		}
	}
	if shouldDigitalEmployeeRespondToDiscussion("speak", a2a.MessageAnswer) {
		t.Fatalf("answer messages should not trigger another digital employee response")
	}
}

func TestDecodeVEDiscussionPayloadWrappedAndLegacy(t *testing.T) {
	envelope := a2a.GroupEnvelope{SessionID: "disc-1", Message: &a2a.GroupDiscussionMessage{Content: "hello"}}
	wrappedRaw, err := json.Marshal(map[string]any{"envelope": envelope, "target_role": "initiator"})
	if err != nil {
		t.Fatalf("marshal wrapped: %v", err)
	}
	got, role, err := decodeVEDiscussionPayload(wrappedRaw)
	if err != nil || role != "initiator" || got.SessionID != "disc-1" || got.Message == nil || got.Message.Content != "hello" {
		t.Fatalf("wrapped decode got=%+v role=%q err=%v", got, role, err)
	}
	legacyRaw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	got, role, err = decodeVEDiscussionPayload(legacyRaw)
	if err != nil || role != "" || got.SessionID != "disc-1" {
		t.Fatalf("legacy decode got=%+v role=%q err=%v", got, role, err)
	}
}

func TestBuildVEDiscussionStreamEventPayloadIncludesSenderIdentity(t *testing.T) {
	payload := buildVEDiscussionStreamEventPayload(a2a.GroupEnvelope{
		FromID:    "envelope-sender",
		SessionID: "disc-1",
		Message: &a2a.GroupDiscussionMessage{
			FromID:  "ve-a",
			Content: "hello",
		},
	}, "disc-1", "hello")

	if payload["session_id"] != "disc-1" || payload["content"] != "hello" || payload["chunk"] != "hello" {
		t.Fatalf("payload content fields = %+v", payload)
	}
	if payload["from_id"] != "ve-a" || payload["sender_id"] != "ve-a" {
		t.Fatalf("payload sender fields = %+v", payload)
	}
}

func TestBuildVEDiscussionStreamEventPayloadFallsBackToEnvelopeSender(t *testing.T) {
	payload := buildVEDiscussionStreamEventPayload(a2a.GroupEnvelope{
		FromID:  "envelope-sender",
		Message: &a2a.GroupDiscussionMessage{Content: "hello"},
	}, "disc-1", "hello")

	if payload["from_id"] != "envelope-sender" || payload["sender_id"] != "envelope-sender" {
		t.Fatalf("payload sender fallback = %+v", payload)
	}
}

func TestShouldEmitVEDiscussionAttachmentMessageToFrontend(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, FileAttachments: []a2a.FileAttachment{{FileURL: "/api/ve/files/file-1", Filename: "report.pdf"}}}
	if !shouldEmitVEDiscussionMessageToFrontend("speak", msg) {
		t.Fatal("attachment-bearing participant messages should be visible in the frontend")
	}
	if shouldEmitVEDiscussionMessageToFrontend("executor", msg) {
		t.Fatal("executor-targeted attachment messages should still route to the local executor")
	}
	if !shouldEmitVEDiscussionMessageToFrontend("initiator", a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: "hello"}) {
		t.Fatal("initiator messages should be visible in the frontend")
	}
}

func TestLocalizeVEDiscussionAttachmentPathsDownloadsFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/download/file-1" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("session_id"); got != "disc-1" {
			t.Fatalf("session_id = %q, want disc-1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		_, _ = w.Write([]byte("remote attachment body"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-1", RemoteMachineToken: "token-1"}
	app.configCacheValid = true
	client := &RemoteHubClient{app: app}
	msg := &a2a.GroupDiscussionMessage{FileAttachments: []a2a.FileAttachment{{FileURL: "/api/ve/files/file-1", Filename: "remote.txt"}}}

	client.localizeVEDiscussionAttachmentPaths("disc-1", msg)

	if got := msg.FileAttachments[0].LocalPath; got == "" {
		t.Fatal("LocalPath should be populated")
	} else if data, err := os.ReadFile(got); err != nil || string(data) != "remote attachment body" {
		t.Fatalf("downloaded file data = %q err=%v", string(data), err)
	}
	if msg.FileAttachments[0].SizeBytes != int64(len("remote attachment body")) {
		t.Fatalf("SizeBytes = %d", msg.FileAttachments[0].SizeBytes)
	}
}

func TestLocalizeVEDiscussionAttachmentPathsDoesNotTrustRemoteLocalPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ve/files/download/file-unsafe" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("safe body"))
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}
	app.configCacheValid = true
	client := &RemoteHubClient{app: app}
	msg := &a2a.GroupDiscussionMessage{
		TextAttachments:  []a2a.TextAttachment{{Filename: "note.txt", LocalPath: `C:\secret\note.txt`, Content: "bm90ZQ=="}},
		ImageAttachments: []a2a.ImageAttachment{{Filename: "no-url.png", LocalPath: `C:\secret\image.png`}},
		FileAttachments:  []a2a.FileAttachment{{FileURL: "/api/ve/files/file-unsafe", Filename: "remote.txt", LocalPath: `C:\secret\remote.txt`}},
	}

	client.localizeVEDiscussionAttachmentPaths("disc-unsafe", msg)

	if msg.TextAttachments[0].LocalPath != "" {
		t.Fatalf("trusted remote text LocalPath: %q", msg.TextAttachments[0].LocalPath)
	}
	if msg.ImageAttachments[0].LocalPath != "" {
		t.Fatalf("trusted remote image LocalPath without file URL: %q", msg.ImageAttachments[0].LocalPath)
	}
	if got := msg.FileAttachments[0].LocalPath; got == "" || got == `C:\secret\remote.txt` {
		t.Fatalf("file LocalPath = %q, want downloaded local cache path", got)
	}
}

func TestCachePushedVEDiscussionSnapshotFallsBackToRemoteClientID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("participant_id"); got != "client-1" {
			t.Fatalf("participant_id = %q, want client-1", got)
		}
		if got := r.Header.Get("X-Machine-ID"); got != "client-1" {
			t.Fatalf("X-Machine-ID = %q, want client-1", got)
		}
		_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{
			Discussion: a2a.HubDiscussionSummary{ID: "disc-client", Role: "initiator", Status: string(a2a.SessionOpen)},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteClientID:     "client-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}
	app.configCacheValid = true
	client := &RemoteHubClient{app: app}

	client.cachePushedVEDiscussionSnapshot(a2a.GroupEnvelope{SessionID: "disc-client", Message: &a2a.GroupDiscussionMessage{SessionID: "disc-client", Kind: a2a.MessageAnswer}})

	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	cached, ok, err := store.CachedDetail(t.Context(), "disc-client")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if !cached.Discussion.Readonly || cached.Discussion.LocalRelation != "" {
		t.Fatalf("role-only initiator detail should remain read-only/unknown relation: %+v", cached.Discussion)
	}
}

func TestCachePushedVEDiscussionSnapshotCachesHubDetail(t *testing.T) {
	now := time.Now().UTC().Round(0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/consultations/disc-push/detail" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("participant_id"); got != "machine-1" {
			t.Fatalf("participant_id = %q, want machine-1", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{
			Discussion: a2a.HubDiscussionSummary{
				ID:            "disc-push",
				Role:          "review",
				LocalRelation: "owned_ve_invited",
				Readonly:      true,
				Status:        string(a2a.SessionOpen),
				Topic:         "Contract review",
				MessageCount:  1,
				UpdatedAt:     now,
			},
			Messages: []a2a.Message{{
				ID:        "msg-1",
				SessionID: "disc-push",
				FromID:    "expert-1",
				Kind:      a2a.MessageAnswer,
				Content:   "Use the revised indemnity clause.",
				CreatedAt: now,
			}},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}
	app.configCacheValid = true
	client := &RemoteHubClient{app: app}

	client.cachePushedVEDiscussionSnapshot(a2a.GroupEnvelope{SessionID: "disc-push", Message: &a2a.GroupDiscussionMessage{SessionID: "disc-push", Kind: a2a.MessageAnswer}})

	store, err := app.openGroupDiscussionHistoryStore()
	if err != nil {
		t.Fatalf("openGroupDiscussionHistoryStore: %v", err)
	}
	defer store.Close()
	cached, ok, err := store.CachedDetail(t.Context(), "disc-push")
	if err != nil || !ok {
		t.Fatalf("CachedDetail ok=%v err=%v", ok, err)
	}
	if len(cached.Messages) != 1 || cached.Messages[0].Content != "Use the revised indemnity clause." {
		t.Fatalf("cached messages = %+v", cached.Messages)
	}
	if !cached.Discussion.Readonly || cached.Discussion.LocalRelation != "owned_ve_invited" {
		t.Fatalf("cached discussion relation/readonly = %+v", cached.Discussion)
	}
}

func TestCachePushedVEDiscussionMessageCoalescesConcurrentRefreshWithFollowup(t *testing.T) {
	firstDetailStarted := make(chan struct{})
	releaseFirstDetail := make(chan struct{})
	var releaseOnce sync.Once
	var detailCalls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/a2a/consultations/disc-push/detail" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		call := atomic.AddInt32(&detailCalls, 1)
		if call == 1 {
			close(firstDetailStarted)
			<-releaseFirstDetail
		}
		_ = json.NewEncoder(w).Encode(a2a.HubDiscussionDetail{
			Discussion: a2a.HubDiscussionSummary{ID: "disc-push", Status: string(a2a.SessionOpen)},
		})
	}))
	defer server.Close()
	defer releaseOnce.Do(func() { close(releaseFirstDetail) })

	app := &App{testHomeDir: t.TempDir()}
	app.configCache = corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
		GroupDiscussion:    corelib.GroupDiscussionConfig{Enabled: false},
	}
	app.configCacheValid = true
	client := &RemoteHubClient{app: app}
	envelope := a2a.GroupEnvelope{SessionID: "disc-push", Message: &a2a.GroupDiscussionMessage{SessionID: "disc-push", Kind: a2a.MessageAnswer}}

	client.cachePushedVEDiscussionMessage(envelope)
	select {
	case <-firstDetailStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first detail refresh")
	}
	client.cachePushedVEDiscussionMessage(envelope)
	client.cachePushedVEDiscussionMessage(envelope)
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&detailCalls); got != 1 {
		t.Fatalf("detail calls while refresh in flight = %d, want 1", got)
	}
	releaseOnce.Do(func() { close(releaseFirstDetail) })
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&detailCalls) < 2 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for coalesced follow-up refresh")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if got := atomic.LoadInt32(&detailCalls); got != 2 {
		t.Fatalf("detail calls after follow-up = %d, want 2", got)
	}
	cleanupDeadline := time.After(2 * time.Second)
	for {
		if _, ok := client.veDetailRefresh.Load("disc-push"); !ok {
			break
		}
		select {
		case <-cleanupDeadline:
			t.Fatal("timed out waiting for detail refresh cleanup")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestShouldRouteVEDiscussionToLocalDispatcherSupportsSpeakRole(t *testing.T) {
	if !shouldRouteVEDiscussionToLocalDispatcher("speak", a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: "hello"}) {
		t.Fatal("speak role should route registered local sessions to the local dispatcher")
	}
	if !shouldRouteVEDiscussionToLocalDispatcher("executor", a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: "hello"}) {
		t.Fatal("executor role should remain supported for compatibility")
	}
	if shouldRouteVEDiscussionToLocalDispatcher("observe", a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: "hello"}) {
		t.Fatal("observe role must not route to the local dispatcher")
	}
	if shouldRouteVEDiscussionToLocalDispatcher("speak", a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: "hello"}) {
		t.Fatal("stream chunks must not trigger a local dispatcher response")
	}
}
