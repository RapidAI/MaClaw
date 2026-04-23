package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/RapidAI/CodeClaw/corelib"
)

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
	}

	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = client.Disconnect() }()

	gotTypes := collectMessageTypes(t, messageCh, 8, 5*time.Second)
	assertContainsType(t, gotTypes, "auth.machine")
	assertContainsType(t, gotTypes, "machine.hello")
	assertContainsType(t, gotTypes, "session.created")
	assertContainsType(t, gotTypes, "session.important_event")
	assertContainsType(t, gotTypes, "session.summary")
	assertContainsType(t, gotTypes, "session.preview_delta")
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

	gotMessages := collectMessages(t, messageCh, 4, 5*time.Second)
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

func TestRemoteHubClientHandlesSessionInput(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
