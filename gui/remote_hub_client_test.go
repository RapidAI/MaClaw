package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tts"
	"github.com/gorilla/websocket"
)

func TestResolveHubThirdPartyMediaReferencesRestoresOwnedOfficeDocument(t *testing.T) {
	app := &App{}
	gateway := newThirdPartyGatewayManager(app)
	gateway.media["office-media"] = &thirdPartyMediaObject{
		ClientID: "pet-1", ID: "office-media", Type: "file", FileName: "report.docx",
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: []byte("office"), Uploaded: true,
	}
	app.thirdPartyGateway = gateway
	client := &RemoteHubClient{app: app}
	message := IMUserMessage{
		Platform: "thirdparty", ClientToolContext: &agent.ClientToolContext{ClientID: "pet-1"},
		Attachments: []MessageAttachment{{Type: "image", SourceMediaID: "office-media"}},
	}
	if err := client.resolveHubThirdPartyMediaReferences(&message); err != nil {
		t.Fatal(err)
	}
	attachment := message.Attachments[0]
	if attachment.SourceMediaID != "" || attachment.Type != "file" || attachment.FileName != "report.docx" || attachment.MimeType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" || attachment.Size != 6 {
		t.Fatalf("resolved attachment = %#v", attachment)
	}
	decoded, err := base64.StdEncoding.DecodeString(attachment.Data)
	if err != nil || string(decoded) != "office" {
		t.Fatalf("resolved data = %q, err=%v", decoded, err)
	}
}

func TestResolveHubThirdPartyMediaReferencesRejectsDifferentClient(t *testing.T) {
	app := &App{}
	gateway := newThirdPartyGatewayManager(app)
	gateway.media["other-client-media"] = &thirdPartyMediaObject{ClientID: "pet-2", ID: "other-client-media", Data: []byte("office"), Uploaded: true}
	app.thirdPartyGateway = gateway
	message := IMUserMessage{Platform: "thirdparty", ClientToolContext: &agent.ClientToolContext{ClientID: "pet-1"}, Attachments: []MessageAttachment{{SourceMediaID: "other-client-media"}}}
	if err := (&RemoteHubClient{app: app}).resolveHubThirdPartyMediaReferences(&message); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("foreign media resolution error = %v", err)
	}
}
func TestRemoteHubClientListsAndDeletesHardwareBindings(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request HubEnvelope
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			switch request.Type {
			case "im.device_gateway_devices_list":
				_ = conn.WriteJSON(map[string]any{"type": "im.device_gateway_devices", "request_id": request.RequestID, "payload": map[string]any{"devices": []map[string]any{{"clientId": "esp32s3-a", "clientName": "Desk Pet", "online": true}}}})
			case "im.device_gateway_device_delete", "im.device_gateway_device_rename":
				_ = conn.WriteJSON(map[string]any{"type": "ack", "request_id": request.RequestID, "payload": map[string]any{"ok": true}})
			}
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &RemoteHubClient{app: &App{}, conn: conn, connected: true, machineID: "gui-a"}
	go client.readLoop()
	devices, err := client.ListHardwareDevices()
	if err != nil || len(devices) != 1 || devices[0].ClientID != "esp32s3-a" {
		t.Fatalf("devices=%#v err=%v", devices, err)
	}
	if err := client.DeleteHardwareDevice("esp32s3-a"); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.connected = false
	_ = client.conn.Close()
	client.conn = nil
	client.mu.Unlock()
}

func TestHardwareDeviceBindingsUsesFrontendJSONNames(t *testing.T) {
	body, err := json.Marshal(HardwareDeviceBindings{
		Devices:    []HardwareDeviceBinding{{ClientID: "esp32s3-a"}},
		MaxDevices: 5,
		BoundCount: 3,
	})
	if err != nil {
		t.Fatalf("marshal hardware bindings: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode hardware bindings: %v", err)
	}
	for _, key := range []string{"devices", "maxDevices", "boundCount"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("hardware bindings JSON missing %q: %s", key, body)
		}
	}
	for _, key := range []string{"Devices", "MaxDevices", "BoundCount"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("hardware bindings JSON leaked Go field %q: %s", key, body)
		}
	}
}

func TestRemoteHubClientConnectionLossStopsHardwareAgents(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	// Inject a bare isolated registry so this lifecycle test does not start the
	// App-wide background memory services that production configuration wires.
	registry := newHardwareAgentRuntimeRegistry(app, nil, nil)
	client := &RemoteHubClient{app: app, hardwareAgents: registry}
	handler, err := registry.handler("pet-alpha")
	if err != nil {
		t.Fatalf("create hardware runtime: %v", err)
	}
	loop := NewLoopContext("active hardware task", 1, nil)
	handler.setSessionLoopCtx("thirdparty:pet-alpha:default", loop)

	client.handleConnectionLoss(errors.New("test connection loss"))
	if !loop.IsCancelled() {
		t.Fatal("connection loss did not cancel the hardware runtime loop")
	}
	client.imHandlerMu.Lock()
	runtimes := client.hardwareAgents
	client.imHandlerMu.Unlock()
	if runtimes != nil {
		t.Fatal("connection loss retained hardware runtime registry")
	}
}

func TestRemoteHubClientHardwareRequestsPropagateCorrelatedErrors(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request HubEnvelope
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			code := "LIST_FAILED"
			if request.Type == "im.device_gateway_device_delete" {
				code = "HARDWARE_NOT_OWNED"
			}
			_ = conn.WriteJSON(map[string]any{"type": "error", "request_id": request.RequestID, "payload": map[string]any{"code": code, "message": "rejected for test"}})
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &RemoteHubClient{conn: conn, connected: true, machineID: "gui-a"}
	go client.readLoop()
	if _, err := client.ListHardwareDevices(); err == nil || !strings.Contains(err.Error(), "LIST_FAILED") {
		t.Fatalf("list error=%v", err)
	}
	if err := client.DeleteHardwareDevice("owned-elsewhere"); err == nil || !strings.Contains(err.Error(), "HARDWARE_NOT_OWNED") {
		t.Fatalf("delete error=%v", err)
	}
	client.mu.Lock()
	client.connected = false
	_ = client.conn.Close()
	client.conn = nil
	client.mu.Unlock()
}

func TestSendDeviceGatewayPairingWaitsForHubConfirmation(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	messageCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Errorf("read websocket message: %v", err)
			return
		}
		messageCh <- message
		if err := conn.WriteJSON(map[string]any{
			"type": "ack", "request_id": message["request_id"], "payload": map[string]any{"ok": true},
		}); err != nil {
			t.Errorf("write websocket ack: %v", err)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &RemoteHubClient{app: app, conn: conn, connected: true, machineID: "gui-a"}
	go client.readLoop()

	if err := client.SendDeviceGatewayPairing("123456"); err != nil {
		t.Fatalf("SendDeviceGatewayPairing: %v", err)
	}
	message := <-messageCh
	if message["type"] != "im.device_gateway_pairing" || strings.TrimSpace(fmt.Sprint(message["request_id"])) == "" {
		t.Fatalf("pairing message is not Hub-confirmed: %#v", message)
	}
	payload, _ := message["payload"].(map[string]any)
	if payload["pairCode"] != "123456" {
		t.Fatalf("pairing payload=%#v", payload)
	}
}

func TestSendDeviceGatewayPairingPropagatesHubRejection(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Errorf("read websocket message: %v", err)
			return
		}
		_ = conn.WriteJSON(map[string]any{
			"type": "error", "request_id": message["request_id"], "payload": map[string]any{"code": "PAIRING_CODE_COLLISION", "message": "rejected for test"},
		})
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &RemoteHubClient{app: app, conn: conn, connected: true, machineID: "gui-a"}
	go client.readLoop()
	if err := client.SendDeviceGatewayPairing("123456"); err == nil || !strings.Contains(err.Error(), "PAIRING_CODE_COLLISION") {
		t.Fatalf("pairing rejection error=%v", err)
	}
}

func TestRemoteHubClientIndependentHardwareRequestsDoNotSerialize(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	requests := make(chan HubEnvelope, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request HubEnvelope
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			if request.Type != "im.device_gateway_reply" {
				continue
			}
			requests <- request
			if err := conn.WriteJSON(map[string]any{"type": "ack", "request_id": request.RequestID, "payload": map[string]any{"ok": true}}); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &RemoteHubClient{app: &App{}, conn: conn, connected: true, machineID: "gui-a"}
	defer func() {
		client.mu.Lock()
		client.connected = false
		if client.conn != nil {
			_ = client.conn.Close()
		}
		client.conn = nil
		client.mu.Unlock()
	}()
	go client.readLoop()

	errCh := make(chan error, 2)
	go func() {
		errCh <- client.SendDeviceGatewayHardwareConfigForClient("esp32-a", map[string]any{"volume": 31})
	}()
	go func() {
		errCh <- client.SendDeviceGatewayPetProfileForClient("esp32-b", map[string]any{"skin": "clawmate"})
	}()
	for range 2 {
		select {
		case <-requests:
		case <-time.After(time.Second):
			t.Fatal("independent hardware request was not sent promptly")
		}
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("independent hardware request failed: %v", err)
		}
	}
}

func TestRemoteHubClientRejectsInvalidHardwareConfigBeforeSending(t *testing.T) {
	client := &RemoteHubClient{}
	for _, extra := range []map[string]any{
		nil,
		{},
		{"unknown": 1},
		{"volume": 100.5},
		{"brightness": -1},
		{"volume": 101},
		{"screenSleepSeconds": 120},
	} {
		if err := client.SendDeviceGatewayHardwareConfigForClient("esp32-a", extra); err == nil {
			t.Fatalf("invalid hardware config was accepted: %#v", extra)
		}
	}
}

func TestRemoteHubClientConnectAndSyncSessions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	messageCh := make(chan map[string]any, 16)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			messageCh <- msg

			msgType, _ := msg["type"].(string)
			switch msgType {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{
					"type":    "auth.ok",
					"payload": map[string]any{"role": "machine"},
				})
			case "machine.hello", "session.created", "session.summary", "session.preview_delta", "session.important_event":
				_ = conn.WriteJSON(map[string]any{
					"type":    "ack",
					"payload": map[string]any{"ok": true},
				})
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token-1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewRemoteSessionManager(app)
	now := time.Now()
	manager.sessions["sess-1"] = &RemoteSession{
		ID:          "sess-1",
		Tool:        "claude",
		Title:       "project-a",
		ProjectPath: filepath.Clean(`D:\workprj\proj-a`),
		Status:      SessionBusy,
		CreatedAt:   now,
		UpdatedAt:   now,
		Summary: SessionSummary{
			SessionID:       "sess-1",
			Tool:            "claude",
			Title:           "project-a",
			Status:          string(SessionBusy),
			Severity:        "info",
			CurrentTask:     "Running command",
			ProgressSummary: "Checking project",
			UpdatedAt:       now.Unix(),
		},
		Preview: SessionPreview{
			SessionID:    "sess-1",
			OutputSeq:    2,
			PreviewLines: []string{"line one", "line two"},
			UpdatedAt:    now.Unix(),
		},
		Events: []ImportantEvent{
			{SessionID: "sess-1", Type: "session.init", Summary: "Session started"},
			{SessionID: "sess-1", Type: "command.started", Summary: "Running go test ./..."},
		},
		TokenUsage: RemoteSessionTokenUsage{
			InputTokens:       1200,
			OutputTokens:      80,
			CachedInputTokens: 768,
			CacheWriteTokens:  128,
		},
	}

	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	messages := collectMessagesUntilTypes(t, messageCh, []string{
		"auth.machine",
		"machine.hello",
		"session.created",
		"session.important_event",
		"session.summary",
		"session.preview_delta",
	}, 5*time.Second)
	gotTypes := messageTypes(messages)
	assertContainsType(t, gotTypes, "auth.machine")
	assertContainsType(t, gotTypes, "machine.hello")
	assertContainsType(t, gotTypes, "session.created")
	assertContainsType(t, gotTypes, "session.important_event")
	assertContainsType(t, gotTypes, "session.summary")
	assertContainsType(t, gotTypes, "session.preview_delta")
	summaryMsg := findMessageByType(t, messages, "session.summary")
	payload, ok := summaryMsg["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected summary payload map, got %#v", summaryMsg["payload"])
	}
	usage, ok := payload["token_usage"].(map[string]any)
	if !ok {
		t.Fatalf("expected diagnostic token_usage in summary payload, got %#v", payload["token_usage"])
	}
	if usage["input_tokens"] != float64(1200) || usage["cached_input_tokens"] != float64(768) {
		t.Fatalf("unexpected token_usage payload: %#v", usage)
	}
}

func TestRemoteHubClientFlushesQueuedIMProactiveMessageAfterConnect(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	received := make(chan map[string]any, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			case "im.proactive_message":
				received <- msg
				return
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-queue", RemoteMachineToken: "token-queue"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	client := NewRemoteHubClient(app, NewRemoteSessionManager(app))
	if err := client.SendIMProactiveMessageToTarget("task complete", "telegram", "group-42"); err != nil {
		t.Fatalf("queue completion notice: %v", err)
	}
	if got := len(client.pendingProactive); got != 1 {
		t.Fatalf("queued notices = %d, want 1", got)
	}
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Disconnect()
	select {
	case msg := <-received:
		payload, ok := msg["payload"].(map[string]any)
		if !ok || payload["text"] != "task complete" || payload["platform"] != "telegram" || payload["platform_uid"] != "group-42" {
			t.Fatalf("proactive payload = %#v", msg["payload"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued proactive message was not flushed after connect")
	}
}

func TestRemoteHubClientConnectAndSyncToolsWithMissingConfigSelector(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const toolName = "browser"
	original, existed := remoteToolCatalog[toolName]
	brokenMeta := original
	brokenMeta.ConfigSelector = nil
	remoteToolCatalog[toolName] = brokenMeta
	defer func() {
		if existed {
			remoteToolCatalog[toolName] = original
		} else {
			delete(remoteToolCatalog, toolName)
		}
	}()

	messageCh := make(chan map[string]any, 16)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			messageCh <- msg

			msgType, _ := msg["type"].(string)
			switch msgType {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{
					"type":    "auth.ok",
					"payload": map[string]any{"role": "machine"},
				})
			case "machine.hello", "machine.tools":
				_ = conn.WriteJSON(map[string]any{
					"type":    "ack",
					"payload": map[string]any{"ok": true},
				})
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-tools-1",
		RemoteMachineToken: "token-tools-1",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewRemoteHubClient(app, NewRemoteSessionManager(app))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	gotMessages := collectMessagesUntilTypes(t, messageCh, []string{
		"auth.machine",
		"machine.hello",
		"machine.tools",
	}, 5*time.Second)
	gotTypes := messageTypes(gotMessages)
	assertContainsType(t, gotTypes, "auth.machine")
	assertContainsType(t, gotTypes, "machine.hello")
	assertContainsType(t, gotTypes, "machine.tools")

	toolsMsg := findMessageByType(t, gotMessages, "machine.tools")
	payload, _ := toolsMsg["payload"].(map[string]any)
	tools, _ := payload["tools"].([]any)
	broken := findToolPayload(t, tools, toolName)
	if providers, ok := broken["providers"]; ok && providers != nil {
		t.Fatalf("expected broken tool payload to omit providers after config error, got %v", broken)
	}
	if displayName, _ := broken["display_name"].(string); displayName != original.DisplayName {
		t.Fatalf("display_name = %q, want %q", displayName, original.DisplayName)
	}
	current, _ := broken["current_provider"].(string)
	if current != "" {
		t.Fatalf("current_provider = %q, want empty", current)
	}
}

func TestRemoteHubClientReadLoopStoresHubError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var once sync.Once
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			// Background SyncTools/SyncSessions use plain HTTP; do not Upgrade.
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			msgType, _ := msg["type"].(string)
			switch msgType {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{
					"type":    "auth.ok",
					"payload": map[string]any{"role": "machine"},
				})
			case "machine.hello":
				once.Do(func() {
					_ = conn.WriteJSON(map[string]any{
						"type":    "error",
						"payload": map[string]any{"message": "hub says no"},
					})
				})
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-2",
		RemoteMachineToken: "token-2",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	client := NewRemoteHubClient(app, NewRemoteSessionManager(app))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if client.LastError() == "hub says no" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("LastError() = %q, want %q", client.LastError(), "hub says no")
}

func TestRemoteHubClientHardwareReplyWaitsForMatchingAck(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	ackSeen := make(chan string, 1)
	releasePlaybackReceipt := make(chan struct{})

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var msg map[string]any
			if conn.ReadJSON(&msg) != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			case "machine.hello":
				_ = conn.WriteJSON(map[string]any{"type": "ack", "request_id": msg["request_id"], "payload": map[string]any{"ok": true}})
			case "im.device_gateway_reply":
				requestID, _ := msg["request_id"].(string)
				if !strings.HasPrefix(requestID, "device-hardware-") || strings.HasPrefix(requestID, "device-hardware-state-") {
					_ = conn.WriteJSON(map[string]any{"type": "ack", "request_id": requestID, "payload": map[string]any{"ok": true}})
					continue
				}
				payload, _ := msg["payload"].(map[string]any)
				if payload["clientId"] != "pet-preview" {
					t.Errorf("hardware preview clientId=%#v, want %q", payload["clientId"], "pet-preview")
					return
				}
				reply, _ := payload["reply"].(map[string]any)
				extra, _ := reply["extra"].(map[string]any)
				if extra["hardware_audio_preview"] != true || extra["hardware_audio_preview_request_id"] != requestID {
					t.Errorf("hardware preview correlation extra=%#v requestID=%q", extra, requestID)
					return
				}
				_ = conn.WriteJSON(map[string]any{"type": "ack", "request_id": requestID, "payload": map[string]any{"ok": true}})
				ackSeen <- requestID
				<-releasePlaybackReceipt
				_ = conn.WriteJSON(map[string]any{
					"type": "im.device_gateway_playback_receipt", "request_id": requestID,
					"payload": map[string]any{"clientId": "pet-preview", "messageId": "audio-1", "status": "delivered"},
				})
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-hardware", RemoteMachineToken: "token"}); err != nil {
		t.Fatal(err)
	}
	client := NewRemoteHubClient(app, NewRemoteSessionManager(app))
	if err := client.Connect(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Disconnect() }()
	result := make(chan error, 1)
	go func() {
		result <- client.SendDeviceGatewayHardwareReplyConfirmed("pet-preview", map[string]any{"reply_type": "audio", "file_data": "d2F2"})
	}()
	select {
	case <-ackSeen:
	case <-time.After(time.Second):
		t.Fatal("Hub acceptance ACK was not observed")
	}
	select {
	case err := <-result:
		t.Fatalf("hardware reply completed before ESP32 playback receipt: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releasePlaybackReceipt)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("confirmed hardware reply: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("hardware reply did not complete after ESP32 playback receipt")
	}
}

func TestRemoteHubClientHardwareReplyReturnsMatchingHubError(t *testing.T) {
	client := &RemoteHubClient{}
	waiter := make(chan error, 1)
	client.playbackWaiters.Store("hardware-error-1", waiter)
	client.completeHubPlaybackRequest("hardware-error-1", errors.New("invalid welcome audio"))
	select {
	case err := <-waiter:
		if err == nil || !strings.Contains(err.Error(), "invalid welcome audio") {
			t.Fatalf("matching request error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("matching request error was not delivered")
	}
}

func TestHubRequestErrorUsesStructuredMessage(t *testing.T) {
	err := hubRequestError(json.RawMessage(`{"code":"NO_COMPATIBLE_HARDWARE","message":"no online remote ESP32 supports welcome audio playback"}`))
	if got := err.Error(); !strings.Contains(got, "NO_COMPATIBLE_HARDWARE") || !strings.Contains(got, "no online remote ESP32") || strings.Contains(got, `{"code"`) {
		t.Fatalf("structured Hub request error=%q", got)
	}
}

func TestRemoteHubClientDisconnectCompletesHubRequestWaiter(t *testing.T) {
	client := &RemoteHubClient{}
	waiter := make(chan error, 1)
	client.requestWaiters.Store("hardware-acceptance-1", waiter)
	if err := client.Disconnect(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waiter:
		if err == nil || !strings.Contains(err.Error(), "disconnected") {
			t.Fatalf("disconnect request error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Hub request waiter was not completed on disconnect")
	}
}

func TestRemoteHubClientHardwareReplyReturnsPlaybackFailure(t *testing.T) {
	client := &RemoteHubClient{}
	waiter := make(chan error, 1)
	client.playbackWaiters.Store("hardware-failed-1", waiter)
	payload, _ := json.Marshal(map[string]any{"clientId": "pet-failed", "status": "failed"})
	client.handleDeviceGatewayPlaybackReceipt(inboundHubEnvelope{RequestID: "hardware-failed-1", Payload: payload})
	select {
	case err := <-waiter:
		if err == nil || !strings.Contains(err.Error(), "playback failed") {
			t.Fatalf("playback failure error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("playback failure was not delivered")
	}
}

func TestRemoteHubClientHandlesSessionInput(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		authed := false
		sent := false
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				authed = true
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			case "machine.hello":
				_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
				if authed && !sent {
					sent = true
					_ = conn.WriteJSON(map[string]any{
						"type":       "session.input",
						"session_id": "sess-input",
						"payload":    map[string]any{"text": "Continue."},
					})
				}
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-3", RemoteMachineToken: "token-3"}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewRemoteSessionManager(app)
	execHandle := newFakeExecutionHandle(1)
	manager.sessions["sess-input"] = &RemoteSession{ID: "sess-input", Exec: execHandle, Provider: &fakeProviderAdapter{}}

	client := NewRemoteHubClient(app, manager)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(execHandle.writes) > 0 {
			if string(execHandle.writes[0]) != "Continue." {
				t.Fatalf("unexpected write payload: %q", string(execHandle.writes[0]))
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for session.input to reach PTY")
}

func TestRemoteHubClientHandlesInterruptAndKill(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			switch msg["type"] {
			case "auth.machine":
				_ = conn.WriteJSON(map[string]any{"type": "auth.ok", "payload": map[string]any{"role": "machine"}})
			case "machine.hello":
				_ = conn.WriteJSON(map[string]any{"type": "ack", "payload": map[string]any{"ok": true}})
				_ = conn.WriteJSON(map[string]any{"type": "session.interrupt", "session_id": "sess-control", "payload": map[string]any{}})
				_ = conn.WriteJSON(map[string]any{"type": "session.kill", "session_id": "sess-control", "payload": map[string]any{}})
			}
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineID: "machine-4", RemoteMachineToken: "token-4"}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewRemoteSessionManager(app)
	execHandle := newFakeExecutionHandle(2)
	manager.sessions["sess-control"] = &RemoteSession{ID: "sess-control", Exec: execHandle, Provider: &fakeProviderAdapter{}}

	client := NewRemoteHubClient(app, manager)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if execHandle.interruptCalls == 1 && execHandle.killCalls == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("interruptCalls=%d killCalls=%d, want 1/1", execHandle.interruptCalls, execHandle.killCalls)
}

func TestRemoteHubClientReconnectsAndResyncsSessions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var connectionCount atomic.Int32
	var authCount atomic.Int32
	var summaryConnIDsMu sync.Mutex
	summaryConnIDs := make([]int32, 0, 2)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}

		connID := connectionCount.Add(1)
		defer conn.Close()

		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			switch msg["type"] {
			case "auth.machine":
				authCount.Add(1)
				_ = conn.WriteJSON(map[string]any{
					"type":    "auth.ok",
					"payload": map[string]any{"role": "machine"},
				})
			case "machine.hello":
				_ = conn.WriteJSON(map[string]any{
					"type":    "ack",
					"payload": map[string]any{"ok": true},
				})
				if connID == 1 {
					_ = conn.Close()
					return
				}
			case "session.summary":
				summaryConnIDsMu.Lock()
				summaryConnIDs = append(summaryConnIDs, connID)
				summaryConnIDsMu.Unlock()
				_ = conn.WriteJSON(map[string]any{
					"type":    "ack",
					"payload": map[string]any{"ok": true},
				})
			case "session.created", "session.preview_delta", "session.important_event":
				_ = conn.WriteJSON(map[string]any{
					"type":    "ack",
					"payload": map[string]any{"ok": true},
				})
			}
		}
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:       server.URL,
		RemoteMachineID:    "machine-reconnect",
		RemoteMachineToken: "token-reconnect",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	manager := NewRemoteSessionManager(app)
	now := time.Now()
	manager.sessions["sess-reconnect"] = &RemoteSession{
		ID:          "sess-reconnect",
		Tool:        "claude",
		Title:       "project-r",
		ProjectPath: filepath.Clean(`D:\workprj\proj-r`),
		Status:      SessionBusy,
		CreatedAt:   now,
		UpdatedAt:   now,
		Summary: SessionSummary{
			SessionID:       "sess-reconnect",
			Tool:            "claude",
			Title:           "project-r",
			Status:          string(SessionBusy),
			Severity:        "info",
			CurrentTask:     "Reconnecting",
			ProgressSummary: "Resyncing session",
			UpdatedAt:       now.Unix(),
		},
		Preview: SessionPreview{
			SessionID:    "sess-reconnect",
			OutputSeq:    1,
			PreviewLines: []string{"preview line"},
			UpdatedAt:    now.Unix(),
		},
	}

	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		summaryConnIDsMu.Lock()
		hasReconnectedSummary := false
		for _, id := range summaryConnIDs {
			if id >= 2 {
				hasReconnectedSummary = true
				break
			}
		}
		summaryConnIDsMu.Unlock()

		if client.IsConnected() && connectionCount.Load() >= 2 && authCount.Load() >= 2 && hasReconnectedSummary {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	summaryConnIDsMu.Lock()
	defer summaryConnIDsMu.Unlock()
	t.Fatalf("reconnect did not complete: connected=%v connections=%d auth=%d summaryConnIDs=%v lastError=%q",
		client.IsConnected(), connectionCount.Load(), authCount.Load(), summaryConnIDs, client.LastError())
}

func TestHubAckHasConfigOnlyMatchesHubConfigPayload(t *testing.T) {
	if !hubAckHasConfig(json.RawMessage(`{"ok":true,"hub_config":{"capability_market_policy":{}}}`)) {
		t.Fatal("expected ack with hub_config to match")
	}
	if hubAckHasConfig(json.RawMessage(`{"ok":true}`)) {
		t.Fatal("expected generic ack to skip capability sync")
	}
	if hubAckHasConfig(json.RawMessage(`{"ok":true,"hub_config":null}`)) {
		t.Fatal("expected null hub_config to skip capability sync")
	}
}

func TestSendDeviceGatewayHardwareEnabledUsesDurableControlMessage(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	messageCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Errorf("read websocket message: %v", err)
			return
		}
		messageCh <- message
		if err := conn.WriteJSON(map[string]any{
			"type": "ack", "request_id": message["request_id"], "payload": map[string]any{"ok": true},
		}); err != nil {
			t.Errorf("write websocket ack: %v", err)
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &RemoteHubClient{conn: conn, connected: true, machineID: "gui-a"}
	client.app = &App{}
	go client.readLoop()
	if err := client.SendDeviceGatewayHardwareEnabled(false); err != nil {
		t.Fatal(err)
	}

	message := <-messageCh
	if message["type"] != "im.device_gateway_reply" {
		t.Fatalf("message=%#v", message)
	}
	if strings.TrimSpace(fmt.Sprint(message["request_id"])) == "" {
		t.Fatalf("hardware state request is not correlated: %#v", message)
	}
	payload, _ := message["payload"].(map[string]any)
	reply, _ := payload["reply"].(map[string]any)
	if payload["clientId"] != "*" || reply["reply_type"] != "hardware_enabled" || reply["enabled"] != false {
		t.Fatalf("hardware state payload=%#v", payload)
	}
}

func TestSyncDeviceGatewayHardwareStateWaitsForHardwareMutation(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.HardwareEnabled = true
	}); err != nil {
		t.Fatalf("seed hardware state: %v", err)
	}

	client := &RemoteHubClient{app: app}
	app.imGatewaySyncMu.Lock()
	done := make(chan struct{})
	go func() {
		client.syncDeviceGatewayHardwareState()
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("reconnect hardware sync ran while a hardware mutation held the lock")
	case <-time.After(50 * time.Millisecond):
	}

	app.imGatewaySyncMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reconnect hardware sync did not finish after the mutation lock was released")
	}
}

func TestSendDeviceGatewayTextReplyMarksTerminalTurn(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	messageCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer conn.Close()
		var message map[string]any
		if err := conn.ReadJSON(&message); err != nil {
			t.Errorf("read websocket message: %v", err)
			return
		}
		messageCh <- message
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &RemoteHubClient{conn: conn, connected: true, machineID: "gui-a"}
	if err := client.SendDeviceGatewayReply("pet-a", "default", GatewayReplyPayload{
		ReplyType: gatewayReplyTypeText, Text: "done", SourceMessageID: "mc_in_1",
	}); err != nil {
		t.Fatal(err)
	}

	message := <-messageCh
	payload, _ := message["payload"].(map[string]any)
	reply, _ := payload["reply"].(map[string]any)
	metadata, _ := reply["metadata"].(map[string]any)
	if reply["replyTo"] != "mc_in_1" || reply["replyToMessageId"] != "mc_in_1" {
		t.Fatalf("reply correlation=%#v", reply)
	}
	if progress, ok := reply["progress"].(bool); !ok || progress {
		t.Fatalf("terminal progress=%#v, want false", reply["progress"])
	}
	if metadata["acp_turn"] != "final" {
		t.Fatalf("terminal metadata=%#v", metadata)
	}
}

func TestSendDeviceGatewaySpeechEndPreservesControlType(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	messageCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var message map[string]any
		if conn.ReadJSON(&message) == nil {
			messageCh <- message
		}
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &RemoteHubClient{conn: conn, connected: true, machineID: "gui-a"}
	if err := client.sendDeviceSpeechEnd("pet-a", "default", "mc_in_1", 3, 1); err != nil {
		t.Fatal(err)
	}
	message := <-messageCh
	payload, _ := message["payload"].(map[string]any)
	reply, _ := payload["reply"].(map[string]any)
	if reply["reply_type"] != "speech_end" || reply["type"] != "speech_end" || reply["replyTo"] != "mc_in_1" {
		t.Fatalf("speech_end payload=%#v", reply)
	}
}

func TestPrepareHubDeviceSpeechArmsResultBeforeSynthesis(t *testing.T) {
	localMode := false
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{
		ThirdPartyGatewayLocalMode: &localMode, TTSEnabled: true,
	}, ttsManager: &tts.Manager{}}
	app.thirdPartyGateway = newThirdPartyGatewayManager(app)
	app.thirdPartyGateway.setClientCapabilities("pet", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text", "audio"}, Text: &agent.ClientTextCapabilities{},
		Audio: &agent.ClientAudioCapabilities{MimeTypes: []string{"audio/mpeg"}, Playback: true, DeliveryModes: []string{"inline"}, MaxInlineBytes: 512 << 10},
	}})
	client := NewRemoteHubClient(app, nil)
	message := IMUserMessage{Platform: "thirdparty:pet", ClientToolContext: &agent.ClientToolContext{
		ClientID: "pet", ConversationID: "default", ReplyToMessageID: "mc_in_1",
	}}
	resp := &IMAgentResponse{Text: "这是完整结果，应先显示结果页面，再开始语音合成。"}
	armed := client.prepareHubDeviceSpeech(message, "im-request", resp)
	if !armed || resp.PendingVoiceParts <= 0 || len(resp.VoiceParts) != 0 {
		t.Fatalf("deferred hub speech not armed: armed=%t response=%#v", armed, resp)
	}
	turnKey := "pet\x00default"
	client.hubSpeechMu.Lock()
	turn := client.hubSpeechTurns[turnKey]
	startedBeforeResult := turn != nil && turn.started
	client.hubSpeechMu.Unlock()
	if startedBeforeResult {
		t.Fatal("hub speech started before the terminal result crossed the device reply queue")
	}
	client.cancelAllHubDeviceSpeech()
}

func TestPrepareHubDeviceSpeechUsesAttachedAudioInsteadOfSyntheticTTS(t *testing.T) {
	localMode := false
	app := &App{configCacheValid: true, configCache: corelib.AppConfig{
		ThirdPartyGatewayLocalMode: &localMode, TTSEnabled: true,
	}, ttsManager: &tts.Manager{}}
	app.thirdPartyGateway = newThirdPartyGatewayManager(app)
	app.thirdPartyGateway.setClientCapabilities("pet", &agent.ClientCapabilities{Output: agent.ClientOutputCapabilities{
		Modalities: []string{"text", "audio"}, Text: &agent.ClientTextCapabilities{},
		Audio: &agent.ClientAudioCapabilities{MimeTypes: []string{"audio/mpeg"}, Playback: true, DeliveryModes: []string{"inline"}, MaxInlineBytes: 512 << 10},
	}})
	client := NewRemoteHubClient(app, nil)
	message := IMUserMessage{Platform: "thirdparty:pet", ClientToolContext: &agent.ClientToolContext{
		ClientID: "pet", ConversationID: "default", ReplyToMessageID: "mc_in_audio",
	}}
	resp := &IMAgentResponse{
		Text: "音频结果说明", FileData: base64.StdEncoding.EncodeToString([]byte("mp3")),
		FileName: "answer.mp3", FileMimeType: "audio/mpeg",
	}
	if client.prepareHubDeviceSpeech(message, "im-request", resp) {
		t.Fatal("attached audio incorrectly armed a duplicate synthetic TTS turn")
	}
	if resp.PendingVoiceParts != 0 || len(client.hubSpeechTurns) != 0 {
		t.Fatalf("duplicate speech state: pending=%d turns=%d", resp.PendingVoiceParts, len(client.hubSpeechTurns))
	}
}

func TestStartHubDeviceSpeechRejectsMismatchedResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &RemoteHubClient{hubSpeechTurns: map[string]*hubDeviceSpeechTurn{
		"pet\x00default": {ctx: ctx, cancel: cancel, replyTo: "mc_in_1", parts: []string{"结果"}, expected: 1},
	}}
	client.startHubDeviceSpeechAfterResult("pet", "default", "another-turn")
	client.hubSpeechMu.Lock()
	started := client.hubSpeechTurns["pet\x00default"].started
	client.hubSpeechMu.Unlock()
	if started {
		t.Fatal("mismatched terminal result started a stale speech turn")
	}
}

func collectMessageTypes(t *testing.T, messageCh <-chan map[string]any, count int, timeout time.Duration) []string {
	return messageTypes(collectMessages(t, messageCh, count, timeout))
}

func collectMessages(t *testing.T, messageCh <-chan map[string]any, count int, timeout time.Duration) []map[string]any {
	t.Helper()
	got := make([]map[string]any, 0, count)
	deadline := time.After(timeout)
	for len(got) < count {
		select {
		case msg := <-messageCh:
			got = append(got, msg)
		case <-deadline:
			t.Fatalf("timed out waiting for %d websocket messages, got %v", count, messageTypes(got))
		}
	}
	return got
}

func collectMessagesUntilTypes(t *testing.T, messageCh <-chan map[string]any, required []string, timeout time.Duration) []map[string]any {
	t.Helper()
	missing := make(map[string]struct{}, len(required))
	for _, messageType := range required {
		missing[messageType] = struct{}{}
	}

	got := make([]map[string]any, 0, len(required))
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(missing) != 0 {
		select {
		case msg := <-messageCh:
			got = append(got, msg)
			if messageType, _ := msg["type"].(string); messageType != "" {
				delete(missing, messageType)
			}
		case <-deadline.C:
			unreceived := make([]string, 0, len(missing))
			for messageType := range missing {
				unreceived = append(unreceived, messageType)
			}
			sort.Strings(unreceived)
			t.Fatalf("timed out waiting for websocket message types %v; got %v", unreceived, messageTypes(got))
		}
	}
	return got
}

func messageTypes(messages []map[string]any) []string {
	got := make([]string, 0, len(messages))
	for _, msg := range messages {
		msgType, _ := msg["type"].(string)
		got = append(got, msgType)
	}
	return got
}

func assertContainsType(t *testing.T, got []string, want string) {
	t.Helper()
	for _, item := range got {
		if item == want {
			return
		}
	}
	t.Fatalf("message types %v do not contain %q", got, want)
}

func findMessageByType(t *testing.T, messages []map[string]any, want string) map[string]any {
	t.Helper()
	for _, msg := range messages {
		if msgType, _ := msg["type"].(string); msgType == want {
			return msg
		}
	}
	t.Fatalf("messages %v do not contain type %q", messageTypes(messages), want)
	return nil
}

func findToolPayload(t *testing.T, tools []any, want string) map[string]any {
	t.Helper()
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if name == want {
			return tool
		}
	}
	t.Fatalf("tool payloads %v do not contain %q", tools, want)
	return nil
}

func decodeInboundPayload(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return out
}
