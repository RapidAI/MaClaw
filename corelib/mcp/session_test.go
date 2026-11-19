package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListToolsOpensStreamableSession(t *testing.T) {
	var sawInit, sawNotice, sawList bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("accept %q", r.Header.Get("Accept"))
		}
		switch {
		case strings.Contains(text, `"initialize"`):
			sawInit = true
			w.Header().Set("Mcp-Session-Id", "sess-1")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			sawNotice = true
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				t.Errorf("notice session %q", r.Header.Get("Mcp-Session-Id"))
			}
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			sawList = true
			if r.Header.Get("Mcp-Session-Id") != "sess-1" {
				http.Error(w, "no session", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"ping\",\"description\":\"say pong\",\"inputSchema\":{\"type\":\"object\",\"properties\":{\"q\":{\"type\":\"string\",\"description\":\"words to search\"}},\"required\":[\"q\"]}}]}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n")
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	out, err := ListTools(context.Background(), MCPServerConfig{
		EndpointURL: srv.URL,
		Headers:     map[string]string{"Mcp-Session-Id": "stale"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawInit || !sawNotice || !sawList {
		t.Fatalf("handshake init=%v notice=%v list=%v", sawInit, sawNotice, sawList)
	}
	if !strings.Contains(out, "ping") || !strings.Contains(out, "q:string*") || !strings.Contains(out, "words to search") {
		t.Fatalf("list %q", out)
	}
}

func TestListToolsFollowsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list") && strings.Contains(text, `"cursor"`):
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"second","description":"page two"}]}}`)
		case strings.Contains(text, "tools/list"):
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"first","description":"page one"}],"nextCursor":"p2"}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Fatalf("pages %q", out)
	}
}

func TestListToolsReusesSessionAndStopsRepeatedCursor(t *testing.T) {
	var inits, lists int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			inits++
			w.Header().Set("Mcp-Session-Id", "sess-keep")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			lists++
			if r.Header.Get("Mcp-Session-Id") != "sess-keep" {
				http.Error(w, "no session", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"once","description":"one"}],"nextCursor":"same"}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	cfg := MCPServerConfig{EndpointURL: srv.URL}
	out, err := ListTools(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "- once") != 1 {
		t.Fatalf("duplicate page listed twice: %q", out)
	}
	if _, err := ListTools(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if inits != 1 {
		t.Fatalf("initialize calls %d", inits)
	}
	// Two listings, each one extra page, then the repeated cursor stops.
	// Without that stop each listing would walk the five-page cap.
	if lists != 4 {
		t.Fatalf("tools/list calls %d", lists)
	}
}

func TestListToolsAuthFailureSkipsToolCall(t *testing.T) {
	var lists int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "tools/list") {
			lists++
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	_, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("err %v", err)
	}
	if lists != 0 {
		t.Fatalf("listed after auth failure: %d", lists)
	}
}

func TestListToolsSessionStaysWithItsCredential(t *testing.T) {
	var inits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		auth := r.Header.Get("Authorization")
		want := "sess-a"
		if strings.Contains(auth, "bbbb") {
			want = "sess-b"
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			inits++
			w.Header().Set("Mcp-Session-Id", want)
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			if r.Header.Get("Mcp-Session-Id") != want {
				t.Errorf("notice session %s for %s", r.Header.Get("Mcp-Session-Id"), auth)
			}
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			if r.Header.Get("Mcp-Session-Id") != want {
				http.Error(w, "wrong session", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"ok"}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	for _, token := range []string{"Bearer aaaa", "Bearer bbbb"} {
		_, err := ListTools(context.Background(), MCPServerConfig{
			EndpointURL: srv.URL,
			Headers:     map[string]string{"Authorization": token},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if inits != 2 {
		t.Fatalf("credentials shared one session, initialize calls %d", inits)
	}
}

func TestListToolsReadsSSEWithoutEventStreamType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		switch {
		case strings.Contains(text, `"initialize"`):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		default:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[{\"name\":\"plain\",\"description\":\"from sse\"}]}}\n\n")
		}
	}))
	defer srv.Close()
	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil || !strings.Contains(out, "plain") {
		t.Fatalf("sse sniff %q %v", out, err)
	}
}

func TestHandshakeRetriesUnsupportedProtocol(t *testing.T) {
	var versions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			versions = append(versions, text)
			if strings.Contains(text, "2025-06-18") {
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"Unsupported protocol version"}}`)
				return
			}
			w.Header().Set("Mcp-Session-Id", "sess-ok")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		default:
			if r.Header.Get("Mcp-Session-Id") != "sess-ok" {
				http.Error(w, "no session", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"pong"}]}}`)
		}
	}))
	defer srv.Close()
	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil || !strings.Contains(out, "ping") || len(versions) != 2 || !strings.Contains(versions[0], "2025-06-18") || !strings.Contains(versions[1], "2025-03-26") {
		t.Fatalf("retry %q versions=%v err=%v", out, versions, err)
	}
}

func TestHandshakeRetriesProtocolRejectionStatus(t *testing.T) {
	var versions []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		switch {
		case strings.Contains(text, `"initialize"`):
			versions = append(versions, text)
			if strings.Contains(text, "2025-06-18") {
				http.Error(w, `{"error":"unsupported protocol version"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "sess-ok")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"pong"}]}}`)
		}
	}))
	defer srv.Close()
	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil || !strings.Contains(out, "ping") || len(versions) != 2 {
		t.Fatalf("status retry %q versions=%d err=%v", out, len(versions), err)
	}
}

func TestListToolsWithoutSessionInitializesEveryCall(t *testing.T) {
	var inits int
	ready := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			inits++
			ready = true
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			if !ready {
				http.Error(w, "not initialized", http.StatusBadRequest)
				return
			}
			ready = false
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"pong"}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	cfg := MCPServerConfig{EndpointURL: srv.URL}
	for i := 0; i < 2; i++ {
		out, err := ListTools(context.Background(), cfg)
		if err != nil || !strings.Contains(out, "ping") {
			t.Fatalf("call %d %q %v", i, out, err)
		}
	}
	if inits != 2 {
		t.Fatalf("initialize calls %d", inits)
	}
}

func TestListToolsSendsNegotiatedProtocol(t *testing.T) {
	var saw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			w.Header().Set("Mcp-Session-Id", "sess-proto")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26"}}`)
		case strings.Contains(text, "notifications/initialized"):
			if got := r.Header.Get("MCP-Protocol-Version"); got != "2025-03-26" {
				t.Errorf("notice protocol %q", got)
			}
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			saw = r.Header.Get("MCP-Protocol-Version")
			if r.Header.Get("Mcp-Session-Id") != "sess-proto" || saw != "2025-03-26" {
				http.Error(w, "missing protocol", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"pong"}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil || !strings.Contains(out, "ping") || saw != "2025-03-26" {
		t.Fatalf("protocol header %q out %q err %v", saw, out, err)
	}
}

func TestInitializeOmitsProtocolHeader(t *testing.T) {
	var saw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, `"initialize"`):
			if r.Header.Get("MCP-Protocol-Version") != "" {
				http.Error(w, `{"error":"bad header"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Mcp-Session-Id", "sess-init")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}`)
		case strings.Contains(text, "notifications/initialized"):
			if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
				t.Errorf("notice protocol %q", r.Header.Get("MCP-Protocol-Version"))
			}
			w.WriteHeader(http.StatusAccepted)
		case strings.Contains(text, "tools/list"):
			saw = r.Header.Get("MCP-Protocol-Version")
			if saw != "2025-06-18" || r.Header.Get("Mcp-Session-Id") != "sess-init" {
				http.Error(w, "missing protocol", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"ping","description":"pong"}]}}`)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	out, err := ListTools(context.Background(), MCPServerConfig{EndpointURL: srv.URL})
	if err != nil || !strings.Contains(out, "ping") || saw != "2025-06-18" {
		t.Fatalf("init header out %q saw %q err %v", out, saw, err)
	}
}
