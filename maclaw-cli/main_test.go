package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestAskAutoDiscoversGUIConfig(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var ack coreim.ThirdPartyAckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gui-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			if got := r.URL.Query().Get("clientId"); got != defaultClientID {
				t.Fatalf("clientId = %q", got)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", []coreim.ThirdPartyOutgoingMessage{{
				ID:             "out-1",
				ConversationID: "conv-a",
				Type:           "text",
				Text:           "pong",
				CreatedAt:      1,
			}}, 1, false))
		case "/api/im-gateway/v1/ack":
			if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
				t.Fatalf("decode ack: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-ack"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{
		ThirdPartyGatewayEnabled: true,
		ThirdPartyGatewayToken:   "gui-token",
		ThirdPartyGatewayHost:    "127.0.0.1",
		ThirdPartyGatewayPort:    mustPort(t, server.URL),
	})
	t.Setenv("MACLAW_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_GATEWAY_URL", "")
	t.Setenv("MACLAW_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"ask", "--text", "ping", "--conversation", "conv-a", "--timeout", "0"}); err != nil {
		t.Fatalf("run ask: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != defaultClientID || incoming.Message.Text != "ping" || incoming.ConversationID != "conv-a" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if len(ack.MessageIDs) != 1 || ack.MessageIDs[0] != "out-1" {
		t.Fatalf("ack = %#v", ack)
	}
	if !strings.Contains(stdout.String(), `"text": "pong"`) {
		t.Fatalf("stdout missing response: %s", stdout.String())
	}
}

func TestHandshakeAdvertisesTools(t *testing.T) {
	var handshake coreim.ThirdPartyHandshakeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&handshake); err != nil {
			t.Fatalf("decode handshake: %v", err)
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{RequestID: "req-h"}))
	}))
	defer server.Close()
	toolsPath := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"demo.echo","description":"Echo","risk":"read","inputSchema":{"type":"object"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"handshake", "--tools", toolsPath}); err != nil {
		t.Fatalf("handshake: %v stderr=%s", err, stderr.String())
	}
	if handshake.ClientID != defaultClientID || len(handshake.Tools) != 1 || handshake.Tools[0].Name != "demo.echo" {
		t.Fatalf("handshake = %#v", handshake)
	}
	if handshake.Capabilities["client_tools"] != true {
		t.Fatalf("capabilities missing client_tools: %#v", handshake.Capabilities)
	}
}

func TestRequireTokenExplainsGUIAutoDiscovery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	err := c.run([]string{"send", "--config", filepath.Join(t.TempDir(), "missing.json"), "--text", "hello"})
	if err == nil || !strings.Contains(err.Error(), "enable GUI IM third-party access") {
		t.Fatalf("expected discovery hint, got %v", err)
	}
}

func TestBootstrapWritesZeroConfigGatewaySettings(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"bootstrap", "--config", cfgPath, "--host", "127.0.0.1", "--port", "18777"}); err != nil {
		t.Fatalf("bootstrap: %v stderr=%s", err, stderr.String())
	}
	cfg, err := loadAppConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ThirdPartyGatewayEnabled || cfg.ThirdPartyGatewayHost != "127.0.0.1" || cfg.ThirdPartyGatewayPort != 18777 {
		t.Fatalf("gateway config = %#v", cfg)
	}
	if len(cfg.ThirdPartyGatewayToken) != 64 {
		t.Fatalf("token length = %d", len(cfg.ThirdPartyGatewayToken))
	}
	if !cfg.IsThirdPartyGatewayLocalMode() {
		t.Fatal("expected local mode enabled")
	}
	if strings.Contains(stdout.String(), cfg.ThirdPartyGatewayToken) {
		t.Fatal("bootstrap output leaked token")
	}
}

func TestAskCreatesAndReusesStatefulSession(t *testing.T) {
	var conversations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			var incoming coreim.ThirdPartyIncomingRequest
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			conversations = append(conversations, incoming.ConversationID)
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", []coreim.ThirdPartyOutgoingMessage{{
				ID:             "out-1",
				ConversationID: "ignored",
				Type:           "text",
				Text:           "ok",
				CreatedAt:      1,
			}}, 7, false))
		case "/api/im-gateway/v1/ack":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-ack"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"ask", "--text", "start", "--timeout", "0"}); err != nil {
		t.Fatalf("first ask: %v stderr=%s", err, stderr.String())
	}
	if err := c.run([]string{"ask", "--text", "continue", "--timeout", "0"}); err != nil {
		t.Fatalf("second ask: %v stderr=%s", err, stderr.String())
	}
	if len(conversations) != 2 || conversations[0] == "" || conversations[0] != conversations[1] {
		t.Fatalf("conversations = %#v", conversations)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != conversations[0] {
		t.Fatalf("current session = %q want %q", st.CurrentSession, conversations[0])
	}
	sess := findSession(st, st.CurrentSession)
	if sess == nil || sess.Cursor != "7" {
		t.Fatalf("session state = %#v", st)
	}
}

func TestSessionUseCommandSetsCurrentSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "use", "project-42"}); err != nil {
		t.Fatalf("session use: %v stderr=%s", err, stderr.String())
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != "project-42" || findSession(st, "project-42") == nil {
		t.Fatalf("state = %#v", st)
	}
}

func TestRequireSessionRejectsImplicitCurrentSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "global", Sessions: []sessionState{{ID: "global", Cursor: "0", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"continue", "--require-session", "should fail"})
	if err == nil || !strings.Contains(err.Error(), "missing explicit session") {
		t.Fatalf("expected explicit session error, got %v", err)
	}
}

func TestContinueAliasUsesPositionalPromptAndSavedCursor(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var pollCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			pollCursor = r.URL.Query().Get("cursor")
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 12, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "project-42", Sessions: []sessionState{{ID: "project-42", Cursor: "9", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"continue", "--timeout", "0", "keep", "going"}); err != nil {
		t.Fatalf("continue: %v stderr=%s", err, stderr.String())
	}
	if incoming.ConversationID != "project-42" || incoming.Message.Text != "keep going" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if pollCursor != "9" {
		t.Fatalf("poll cursor = %q", pollCursor)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if sess := findSessionForClient(st, "project-42", defaultClientID); sess == nil || sess.Cursor != "12" {
		t.Fatalf("state = %#v", st)
	}
}

func TestExplicitSessionLoadsSavedCursorForSingleShotAgentCalls(t *testing.T) {
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursors = append(cursors, r.URL.Query().Get("cursor"))
			next := int64(3)
			if len(cursors) == 2 {
				next = 6
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var out1, err1 bytes.Buffer
	if err := testCLI(&out1, &err1).run([]string{"continue", "--session", "task-123", "--timeout", "0", "first"}); err != nil {
		t.Fatalf("first call: %v stderr=%s", err, err1.String())
	}
	var out2, err2 bytes.Buffer
	if err := testCLI(&out2, &err2).run([]string{"continue", "--session", "task-123", "--timeout", "0", "second"}); err != nil {
		t.Fatalf("second call: %v stderr=%s", err, err2.String())
	}
	if got := strings.Join(cursors, ","); got != "0,3" {
		t.Fatalf("cursors = %s", got)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if sess := findSession(st, "task-123"); sess == nil || sess.Cursor != "6" {
		t.Fatalf("state = %#v", st)
	}
}

func TestInvokeContinueUsesJSONRequest(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var pollCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			pollCursor = r.URL.Query().Get("cursor")
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 4, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	req := `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"from json","timeoutSec":1,"ack":false,"pretty":false}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != "planner" || incoming.ConversationID != "task-json" || incoming.Message.Text != "from json" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if pollCursor != "0" {
		t.Fatalf("poll cursor = %q", pollCursor)
	}
	var out askResult
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode invoke output: %v: %s", err, stdout.String())
	}
	if out.SessionID != "task-json" || out.NextCursor != "4" {
		t.Fatalf("out = %#v", out)
	}
}

func TestInvokeDryRunPrintsCommandMapping(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"from json","lockTimeoutSec":9}`})
	if err != nil {
		t.Fatalf("invoke dry-run: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	if out["ok"] != true || out["action"] != "continue" {
		t.Fatalf("dry-run output = %#v", out)
	}
	argv, ok := out["argv"].([]any)
	if !ok || len(argv) == 0 || argv[0] != "continue" {
		t.Fatalf("argv = %#v", out["argv"])
	}
	if !containsAnyAdjacent(argv, "--client", "planner") || !containsAnyAdjacent(argv, "--session", "task-json") || !containsAnyAdjacent(argv, "--lock-timeout", "9") {
		t.Fatalf("argv missing expected flags: %#v", argv)
	}
}

func TestInvokeDryRunNormalizesAskAndRun(t *testing.T) {
	for _, action := range []string{"ask", "run"} {
		var stdout, stderr bytes.Buffer
		err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", fmt.Sprintf(`{"action":%q,"clientId":"planner","sessionId":"task-json","text":"from json"}`, action)})
		if err != nil {
			t.Fatalf("invoke dry-run %s: %v stderr=%s", action, err, stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("decode dry-run %s: %v: %s", action, err, stdout.String())
		}
		if out["action"] != "continue" {
			t.Fatalf("dry-run action for %s = %#v", action, out["action"])
		}
		argv := out["argv"].([]any)
		if argv[0] != "continue" {
			t.Fatalf("argv for %s = %#v", action, argv)
		}
	}
}

func TestInvokeSendSupportsRichPayload(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))

	req := `{"action":"send","clientId":"planner","sessionId":"task-json","eventId":"evt-001","messageId":"msg-001","message":{"type":"file","fileName":"report.txt","url":"file:///tmp/report.txt"}}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke send: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != "planner" || incoming.ConversationID != "task-json" || incoming.EventID != "evt-001" || incoming.MessageID != "msg-001" {
		t.Fatalf("incoming ids = %#v", incoming)
	}
	if incoming.Message.Type != "file" || incoming.Message.FileName != "report.txt" || incoming.Message.URL != "file:///tmp/report.txt" {
		t.Fatalf("incoming message = %#v", incoming.Message)
	}
}

func TestInvokeToolResultSupportsIdempotencyKey(t *testing.T) {
	var result coreim.ThirdPartyToolResultRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/tool-result":
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Fatalf("decode tool-result: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-tool"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))

	req := `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","toolCallId":"tc-1","status":"success","idempotencyKey":"tc-1-success","result":{"ok":true}}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke tool-result: %v stderr=%s", err, stderr.String())
	}
	if result.ClientID != "desktop-agent" || result.ConversationID != "task-json" || result.ToolCallID != "tc-1" || result.IdempotencyKey != "tc-1-success" {
		t.Fatalf("tool-result = %#v", result)
	}
	if result.Result["ok"] != true {
		t.Fatalf("result payload = %#v", result.Result)
	}
}

func TestSameSessionDifferentClientsKeepIndependentCursors(t *testing.T) {
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursors = append(cursors, r.URL.Query().Get("clientId")+"="+r.URL.Query().Get("cursor"))
			next := int64(11)
			if r.URL.Query().Get("clientId") == "agent-b" {
				next = 21
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{Sessions: []sessionState{
		{ID: "same", ClientID: "agent-a", Cursor: "10", CreatedAt: 1, UpdatedAt: 1},
		{ID: "same", ClientID: "agent-b", Cursor: "20", CreatedAt: 1, UpdatedAt: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var outA, errA bytes.Buffer
	if err := testCLI(&outA, &errA).run([]string{"continue", "--client", "agent-a", "--session", "same", "--timeout", "0", "a"}); err != nil {
		t.Fatalf("agent-a: %v", err)
	}
	var outB, errB bytes.Buffer
	if err := testCLI(&outB, &errB).run([]string{"continue", "--client", "agent-b", "--session", "same", "--timeout", "0", "b"}); err != nil {
		t.Fatalf("agent-b: %v", err)
	}
	if got := strings.Join(cursors, ","); got != "agent-a=10,agent-b=20" {
		t.Fatalf("cursors = %s", got)
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-a").Cursor != "11" || findSessionForClient(st, "same", "agent-b").Cursor != "21" {
		t.Fatalf("state = %#v", st)
	}
}

func TestSessionRenameDeleteAndResetCursor(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "old", Sessions: []sessionState{{ID: "old", Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"session", "rename", "old", "new"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := c.run([]string{"session", "reset-cursor", "--id", "new"}); err != nil {
		t.Fatalf("reset-cursor: %v", err)
	}
	st, _ := loadCLIState(statePath)
	if st.CurrentSession != "new" || findSession(st, "old") != nil || findSession(st, "new").Cursor != "0" {
		t.Fatalf("after reset = %#v", st)
	}
	if err := c.run([]string{"session", "delete", "new"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if st.CurrentSession != "" || findSession(st, "new") != nil {
		t.Fatalf("after delete = %#v", st)
	}
}

func TestSessionRenameSupportsIDFlag(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "old", Sessions: []sessionState{{ID: "old", ClientID: defaultClientID, Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "rename", "--id", "old", "new"}); err != nil {
		t.Fatalf("rename --id: %v stderr=%s", err, stderr.String())
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "new", defaultClientID) == nil {
		t.Fatalf("state = %#v", st)
	}
}

func TestSessionCommandsRespectClientKey(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "same", Sessions: []sessionState{
		{ID: "same", ClientID: "agent-a", Cursor: "5", CreatedAt: 1, UpdatedAt: 1},
		{ID: "same", ClientID: "agent-b", Cursor: "9", CreatedAt: 1, UpdatedAt: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"session", "rename", "--client", "agent-a", "same", "renamed"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "renamed", "agent-a") == nil || findSessionForClient(st, "same", "agent-b") == nil {
		t.Fatalf("after client rename = %#v", st)
	}
	if err := c.run([]string{"session", "reset-cursor", "--client", "agent-b", "--id", "same"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-b").Cursor != "0" || findSessionForClient(st, "renamed", "agent-a").Cursor != "5" {
		t.Fatalf("after client reset = %#v", st)
	}
	if err := c.run([]string{"session", "delete", "--client", "agent-b", "same"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-b") != nil || findSessionForClient(st, "renamed", "agent-a") == nil {
		t.Fatalf("after client delete = %#v", st)
	}
}

func TestSessionCommandsUseStateLock(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "locked", Sessions: []sessionState{{ID: "locked", Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	lock, err := acquireStateLock(statePath)
	if err != nil {
		t.Fatalf("state lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- testCLI(&stdout, &stderr).run([]string{"session", "list"})
	}()
	select {
	case err := <-done:
		t.Fatalf("session list should block on state lock, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	lock.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("session list after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session list did not finish after releasing state lock")
	}
}

func TestSaveCLIStateUsesAtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "a", Sessions: []sessionState{{ID: "a", Cursor: "1", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := saveCLIState(statePath, cliState{CurrentSession: "b", Sessions: []sessionState{{ID: "b", Cursor: "2", CreatedAt: 1, UpdatedAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != "b" {
		t.Fatalf("state = %#v", st)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "state.json.tmp.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %#v", matches)
	}
}

func TestCleanPromptArgsDropsTrailingFlags(t *testing.T) {
	got := strings.Join(cleanPromptArgs([]string{"keep", "going", "--timeout", "0", "--pretty=false"}), " ")
	if got != "keep going" {
		t.Fatalf("cleanPromptArgs = %q", got)
	}
}

func TestLockTimeoutCanComeFromEnvAndInvoke(t *testing.T) {
	t.Setenv("MACLAW_CLI_LOCK_TIMEOUT_SEC", "17")
	cfgp, _ := newFlagSet("test")
	if cfgp.LockTimeoutSec != 17 {
		t.Fatalf("env lock timeout = %d", cfgp.LockTimeoutSec)
	}
	action, args, err := invokeArgs(invokeRequest{Action: "continue", ClientID: "planner", SessionID: "task-123", Text: "go", LockTimeoutSec: 23}, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if action != "continue" {
		t.Fatalf("action = %q", action)
	}
	if !containsAdjacent(args, "--lock-timeout", "23") {
		t.Fatalf("invoke args missing lock timeout: %#v", args)
	}
	action, args, err = invokeArgs(invokeRequest{Action: "watch", ClientID: "planner", SessionID: "task-123", Count: 2}, "watch")
	if err != nil {
		t.Fatal(err)
	}
	if action != "watch" || !containsAdjacent(args, "--count", "2") {
		t.Fatalf("watch invoke args = action %q args %#v", action, args)
	}
}

func TestVersionIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"version"}); err != nil {
		t.Fatalf("version: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode version: %v\n%s", err, stdout.String())
	}
	if out["name"] != "maclaw-cli" || out["version"] != cliVersion {
		t.Fatalf("version output = %#v", out)
	}
}

func TestAgentSpecIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"agent-spec"}); err != nil {
		t.Fatalf("agent-spec: %v stderr=%s", err, stderr.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &spec); err != nil {
		t.Fatalf("decode spec: %v\n%s", err, stdout.String())
	}
	if spec["name"] != "maclaw-cli" {
		t.Fatalf("name = %#v", spec["name"])
	}
	if spec["version"] != cliVersion {
		t.Fatalf("version = %#v", spec["version"])
	}
	flags, ok := spec["requiredFlagsForAutomation"].([]any)
	if !ok || len(flags) != 3 {
		t.Fatalf("requiredFlagsForAutomation = %#v", spec["requiredFlagsForAutomation"])
	}
	if !strings.Contains(fmt.Sprint(spec["goldenRule"]), "--session") {
		t.Fatalf("goldenRule = %#v", spec["goldenRule"])
	}
	if !strings.Contains(fmt.Sprint(spec["commands"]), "invoke") {
		t.Fatalf("commands missing invoke: %#v", spec["commands"])
	}
	if !strings.Contains(fmt.Sprint(spec["importantFlags"]), "--json-errors") {
		t.Fatalf("importantFlags missing json-errors: %#v", spec["importantFlags"])
	}
}

func TestInvokeSchemaIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"invoke-schema"}); err != nil {
		t.Fatalf("invoke-schema: %v stderr=%s", err, stderr.String())
	}
	var schema map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		t.Fatalf("decode schema: %v\n%s", err, stdout.String())
	}
	if schema["title"] != "maclaw-cli invoke request" {
		t.Fatalf("title = %#v", schema["title"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || props["action"] == nil || props["sessionId"] == nil {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	data, err := os.ReadFile(filepath.Join("invoke.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fileSchema map[string]any
	if err := json.Unmarshal(data, &fileSchema); err != nil {
		t.Fatalf("decode static schema: %v", err)
	}
	if fileSchema["title"] != schema["title"] {
		t.Fatalf("static schema title = %#v", fileSchema["title"])
	}
	if !jsonEqual(t, invokeSchema(), fileSchema) {
		codeSchema, _ := json.MarshalIndent(invokeSchema(), "", "  ")
		staticSchema, _ := json.MarshalIndent(fileSchema, "", "  ")
		t.Fatalf("static schema differs from invokeSchema()\ncode:\n%s\nstatic:\n%s", codeSchema, staticSchema)
	}
}

func TestManifestIsMachineReadable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["name"] != "maclaw-cli" || manifest["schema"] != "invoke.schema.json" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest["version"] != cliVersion {
		t.Fatalf("manifest version = %#v", manifest["version"])
	}
	entrypoints, ok := manifest["entrypoints"].(map[string]any)
	if !ok || entrypoints["agentSpec"] != "maclaw-cli agent-spec" || entrypoints["invokeSchema"] != "maclaw-cli invoke-schema" {
		t.Fatalf("entrypoints = %#v", manifest["entrypoints"])
	}
}

func TestSessionRunLockPathIsPerClientSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	a := sessionRunLockPath(statePath, "agent-a", "task-1")
	b := sessionRunLockPath(statePath, "agent-b", "task-1")
	c := sessionRunLockPath(statePath, "agent-a", "task-2")
	if a == b || a == c || b == c {
		t.Fatalf("lock paths should differ: %q %q %q", a, b, c)
	}
	if filepath.Dir(filepath.Dir(a)) != filepath.Dir(statePath) {
		t.Fatalf("lock path should live under state dir: %q", a)
	}
}

func TestAcquireRunLockBlocksSameClientSession(t *testing.T) {
	cfg := config{StatePath: filepath.Join(t.TempDir(), "state.json"), ClientID: "agent-a", ConversationID: "task-1"}
	first, err := acquireRunLock(cfg)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		second, err := acquireRunLock(cfg)
		if second != nil {
			second.Release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second lock should block, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	first.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second lock after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestAcquireLockFileRemovesStaleLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "state.json.lock")
	if err := os.WriteFile(lockPath, []byte("dead\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLockFile(lockPath, "state lock", 1)
	if err != nil {
		t.Fatalf("acquire stale lock: %v", err)
	}
	lock.Release()
}

func TestAcquireRunLockAllowsDifferentClientSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	first, err := acquireRunLock(config{StatePath: statePath, ClientID: "agent-a", ConversationID: "task-1"})
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.Release()
	second, err := acquireRunLock(config{StatePath: statePath, ClientID: "agent-b", ConversationID: "task-1"})
	if err != nil {
		t.Fatalf("second lock: %v", err)
	}
	second.Release()
}

func TestWriteCLIErrorCanEmitJSON(t *testing.T) {
	var stderr bytes.Buffer
	writeCLIError(&stderr, []string{"continue", "--json-errors"}, errors.New("boom"))
	var env map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("decode stderr JSON: %v: %s", err, stderr.String())
	}
	if env["ok"] != false || !strings.Contains(fmt.Sprint(env["error"]), "boom") {
		t.Fatalf("error envelope = %#v", env)
	}
}

func TestInvokeErrorsDefaultToJSON(t *testing.T) {
	var stderr bytes.Buffer
	writeCLIError(&stderr, []string{"invoke"}, errors.New("bad json"))
	var env map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("decode invoke stderr JSON: %v: %s", err, stderr.String())
	}
	if env["ok"] != false || !strings.Contains(fmt.Sprint(env["error"]), "bad json") {
		t.Fatalf("invoke error envelope = %#v", env)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	adata, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bdata, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	var av, bv any
	if err := json.Unmarshal(adata, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(bdata, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}

func containsAdjacent(values []string, key, value string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == key && values[i+1] == value {
			return true
		}
	}
	return false
}

func containsAnyAdjacent(values []any, key, value string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == key && values[i+1] == value {
			return true
		}
	}
	return false
}

func testCLI(stdout, stderr *bytes.Buffer) *cli {
	return &cli{
		stdin:  strings.NewReader(""),
		stdout: stdout,
		stderr: stderr,
		client: &http.Client{},
		now: func() time.Time {
			return time.UnixMilli(1781028000000)
		},
	}
}

func writeConfig(t *testing.T, cfg corelib.AppConfig) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(rawURL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parse port from %s: %v", rawURL, err)
	}
	return port
}
