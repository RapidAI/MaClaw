package acpagent

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func TestBridgeInitializeAndSessionPrompt(t *testing.T) {
	var (
		mu       sync.Mutex
		incoming []coreim.ThirdPartyIncomingRequest
		cursor   int64
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/health"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "status": "connected", "protocolVersion": "1",
			})
		case strings.HasSuffix(path, "/handshake"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "protocolVersion": "1", "capabilities": []string{"text"},
			})
		case strings.HasSuffix(path, "/incoming"):
			var req coreim.ThirdPartyIncomingRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			incoming = append(incoming, req)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "accepted": true, "maclawMessageId": "mc-1",
			})
		case strings.Contains(path, "outgoing"):
			mu.Lock()
			msgs := []coreim.ThirdPartyOutgoingMessage{}
			next := cursor
			if cursor == 0 && len(incoming) > 0 {
				cursor = 1
				next = 1
				msgs = []coreim.ThirdPartyOutgoingMessage{{
					ID:               "out-1",
					ConversationID:   incoming[0].ConversationID,
					ReplyToMessageID: incoming[0].MessageID,
					Type:             "text",
					Text:             "hello from gui",
					CreatedAt:        time.Now().UnixMilli(),
					Metadata:         map[string]string{"acp_turn": "final"},
				}}
			}
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(coreim.ThirdPartyOutgoingPollResponse{
				OK: true, Messages: msgs, NextCursor: FormatCursor(next), HasMore: false,
			})
		case strings.HasSuffix(path, "/ack"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	gw := NewGatewayClient(GatewayEndpoint{BaseURL: srv.URL, Token: "test-token", OK: true})
	bridge, err := NewBridge(BridgeOptions{
		Gateway:          gw,
		IdleAfterContent: 50 * time.Millisecond,
		PollTimeoutSec:   1,
		MaxPromptWait:    5 * time.Second,
		SkipReadyCheck:   true,
	})
	if err != nil {
		t.Fatal(err)
	}

	pr, pw := io.Pipe()
	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- bridge.ServeStdio(pr, &out)
	}()

	writeLine := func(v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pw.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}

	writeLine(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": 1, "clientInfo": map[string]any{"name": "test"}},
	})
	writeLine(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "session/new",
		"params": map[string]any{"cwd": t.TempDir()},
	})

	sessionID := waitForSessionID(t, &out, 3*time.Second)
	if sessionID == "" {
		t.Fatalf("no sessionId in output: %s", out.String())
	}

	writeLine(map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "session/prompt",
		"params": map[string]any{
			"sessionId": sessionID,
			"prompt":    []map[string]any{{"type": "text", "text": "ping"}},
		},
	})

	stopReason, sawUpdate := waitForPromptDone(t, &out, 5*time.Second)
	_ = pw.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	if !sawUpdate {
		t.Fatalf("expected session/update, output=%s", out.String())
	}
	if stopReason != StopEndTurn {
		t.Fatalf("stopReason=%q output=%s", stopReason, out.String())
	}
	mu.Lock()
	n := len(incoming)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("incoming count = %d", n)
	}
	if !strings.Contains(incoming[0].Message.Text, "ping") {
		t.Fatalf("incoming text = %q", incoming[0].Message.Text)
	}
}

func waitForSessionID(t *testing.T, out *bytes.Buffer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var resp Response
			if json.Unmarshal([]byte(line), &resp) != nil {
				continue
			}
			if string(bytes.TrimSpace(resp.ID)) == "2" && resp.Result != nil {
				raw, _ := json.Marshal(resp.Result)
				var nr SessionNewResult
				_ = json.Unmarshal(raw, &nr)
				if nr.SessionID != "" {
					return nr.SessionID
				}
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	return ""
}

func waitForPromptDone(t *testing.T, out *bytes.Buffer, timeout time.Duration) (stopReason string, sawUpdate bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(out.String(), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var envelope map[string]any
			if json.Unmarshal([]byte(line), &envelope) != nil {
				continue
			}
			if envelope["method"] == "session/update" {
				sawUpdate = true
			}
			var resp Response
			if json.Unmarshal([]byte(line), &resp) == nil && string(bytes.TrimSpace(resp.ID)) == "3" {
				raw, _ := json.Marshal(resp.Result)
				var pr SessionPromptResult
				_ = json.Unmarshal(raw, &pr)
				if pr.StopReason != "" {
					return pr.StopReason, sawUpdate
				}
			}
		}
		time.Sleep(15 * time.Millisecond)
	}
	return stopReason, sawUpdate
}

func TestIsTurnFinal(t *testing.T) {
	final := coreim.ThirdPartyOutgoingMessage{
		Type: "text", Text: "done", ReplyToMessageID: "msg1",
		Metadata: map[string]string{"acp_turn": "final"},
	}
	if !isTurnFinal(final, "msg1") {
		t.Fatal("expected final by metadata")
	}
	progress := coreim.ThirdPartyOutgoingMessage{Type: "text", Text: "…", Progress: true, Metadata: map[string]string{"acp_turn": "final"}}
	if isTurnFinal(progress, "msg1") {
		t.Fatal("progress must not be final")
	}
	// Intermediate text with replyTo alone is NOT final (avoids early end).
	reply := coreim.ThirdPartyOutgoingMessage{Type: "text", Text: "hi", ReplyToMessageID: "msg1"}
	if isTurnFinal(reply, "msg1") {
		t.Fatal("bare replyTo text must not end turn without acp_turn=final")
	}
	errMsg := coreim.ThirdPartyOutgoingMessage{Type: "error", Error: "fail", ReplyToMessageID: "msg1"}
	if !isTurnFinal(errMsg, "msg1") {
		t.Fatal("expected error to be final")
	}
}

func TestDiscoverGatewayEnvOverride(t *testing.T) {
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:19999/api/im-gateway/v1")
	t.Setenv("MACLAW_GATEWAY_TOKEN", "env-token")
	ep := DiscoverGateway("")
	if ep.Token != "env-token" {
		t.Fatalf("token = %q", ep.Token)
	}
	if !strings.Contains(ep.BaseURL, "19999") {
		t.Fatalf("base = %q", ep.BaseURL)
	}
}
