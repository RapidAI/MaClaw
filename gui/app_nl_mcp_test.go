package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMCPRegistrySessionsAreScopedByOwner(t *testing.T) {
	var counter atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			w.Header().Set("Mcp-Session-Id", "session-"+string(rune('a'+counter.Add(1)-1)))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	if err := registry.ensureSession(target, "agent-a"); err != nil {
		t.Fatalf("ensureSession(agent-a): %v", err)
	}
	if err := registry.ensureSession(target, "agent-b"); err != nil {
		t.Fatalf("ensureSession(agent-b): %v", err)
	}

	a, okA := registry.getSessionForOwner("remote", "agent-a")
	b, okB := registry.getSessionForOwner("remote", "agent-b")
	if !okA || !okB || a.SessionID == "" || b.SessionID == "" || a.SessionID == b.SessionID {
		t.Fatalf("sessions not isolated: a=%#v ok=%v b=%#v ok=%v", a, okA, b, okB)
	}
}

func TestMCPRegistryEnsureSessionSingleflightPerOwner(t *testing.T) {
	var initCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Mcp-Session-Id") == "" {
			initCount.Add(1)
			w.Header().Set("Mcp-Session-Id", "session-owner-a")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer server.Close()

	registry := NewMCPRegistry(nil)
	target := &corelib.MCPServerEntry{ID: "remote", Name: "Remote", EndpointURL: server.URL}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- registry.ensureSession(target, "agent-a")
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("ensureSession() error = %v", err)
		}
	}
	if got := initCount.Load(); got != 1 {
		t.Fatalf("initialize count = %d, want 1 for same owner", got)
	}
}
