package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallToolOnceDoesNotReplayAStaleSession(t *testing.T) {
	var calls int
	stale := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			w.Header().Set("Mcp-Session-Id", "sess-once")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/call"):
			calls++
			if stale {
				http.Error(w, "gone", http.StatusNotFound)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"pong"}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	cfg := MCPServerConfig{EndpointURL: srv.URL, Transport: "streamable-http"}
	out, err := CallToolOnce(context.Background(), cfg, "lookup", map[string]any{"q": "x"})
	if err != nil || out != "pong" {
		t.Fatalf("first %q %v", out, err)
	}
	stale = true
	_, err = CallToolOnce(context.Background(), cfg, "lookup", map[string]any{"q": "x"})
	if err == nil || !strings.Contains(err.Error(), "not retried") || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err %v", err)
	}
	if calls != 2 {
		t.Fatalf("tools/call count %d", calls)
	}
}

func TestSessionScopeDoesNotShareASession(t *testing.T) {
	var inits int
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			inits++
			sid := "sess-a"
			if inits > 1 {
				sid = "sess-b"
			}
			w.Header().Set("Mcp-Session-Id", sid)
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			sid := r.Header.Get("Mcp-Session-Id")
			seen[sid] = true
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"lookup","inputSchema":{"type":"object","properties":{}}}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	for _, scope := range []string{"tenant-a\x00alice", "tenant-b\x00bob"} {
		if _, err := ListToolEntries(context.Background(), MCPServerConfig{EndpointURL: srv.URL, SessionScope: scope}); err != nil {
			t.Fatal(err)
		}
	}
	if inits != 2 || !seen["sess-a"] || !seen["sess-b"] {
		t.Fatalf("inits %d sessions %#v", inits, seen)
	}
}
