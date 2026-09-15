package llmservice

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestEgressProviderHopsWhenLocalNodeNotAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	svc := NewService(&mockSystemSettings{})
	provider := llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         upstream.URL + "/v1",
		AllowedNodeIDs: []string{"hc-us"},
	}
	if err := svc.AddProvider(context.Background(), provider); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	peer := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{
		Service: svc,
		NodeID:  "hc-us",
	}, func(*http.Request) error { return nil }))
	t.Cleanup(peer.Close)

	cfg := &ProxyConfig{
		Service: svc,
		NodeID:  "hc-cn",
		LookupAccessPeer: func(nodeID string) (string, bool, int64) {
			if nodeID == "hc-us" {
				return peer.URL, true, 10
			}
			return "", false, 0
		},
	}
	got, err := egressProvider(context.Background(), cfg, &provider, map[string]any{
		"model":    "gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, "gpt-4o", "gpt-4o")
	if err != nil {
		t.Fatalf("egressProvider: %v", err)
	}
	if got == nil || got.StatusCode != http.StatusOK {
		t.Fatalf("hop response = %#v", got)
	}
	if !strings.Contains(string(got.Body), "pong") {
		t.Fatalf("body = %s", got.Body)
	}
}

func TestEgressProviderDoesNotCallLocalWhenRestrictedAndNoPeer(t *testing.T) {
	called := false
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	t.Cleanup(upstream.Close)

	provider := &llmpool.ProviderConfig{
		ID:             "openai",
		APIURL:         upstream.URL + "/v1",
		AllowedNodeIDs: []string{"hc-us"},
	}
	_, err := egressProvider(context.Background(), &ProxyConfig{NodeID: "hc-cn"}, provider, map[string]any{"model": "gpt-4o"}, "gpt-4o", "gpt-4o")
	if err == nil {
		t.Fatal("expected hop failure, got success")
	}
	if called {
		t.Fatal("restricted provider must not egress from a disallowed node")
	}
}

func TestUpstreamHopHandlerRejectsNodeOutsideScope(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         "https://api.openai.com/v1",
		AllowedNodeIDs: []string{"hc-us"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	handler := UpstreamHopHandler(&ProxyConfig{Service: svc, NodeID: "hc-cn"}, func(*http.Request) error { return nil })
	body, _ := json.Marshal(upstreamHopRequest{Kind: upstreamHopKindFwd, ProviderID: "openai", Body: map[string]any{"model": "gpt-4o"}})
	req := httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(string(body)))
	req.Header.Set(upstreamHopHeader, "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.Bytes())
	}
}

func TestUpstreamHopHandlerRequiresAuthAndHopHeader(t *testing.T) {
	handler := UpstreamHopHandler(&ProxyConfig{Service: NewService(&mockSystemSettings{}), NodeID: "hc-1"}, func(*http.Request) error {
		return io.EOF
	})
	req := httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d", rec.Code)
	}

	handler = UpstreamHopHandler(&ProxyConfig{Service: NewService(&mockSystemSettings{}), NodeID: "hc-1"}, func(*http.Request) error { return nil })
	req = httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(`{"provider_id":"openai"}`))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing hop header status = %d", rec.Code)
	}
}

func TestHopPeerBaseURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	if got := hopPeerBaseURL("javascript:alert(1)"); got != "" {
		t.Fatalf("javascript URL = %q", got)
	}
	if got := hopPeerBaseURL("https://evil:pass@hubs.example"); got != "" {
		t.Fatalf("userinfo URL = %q", got)
	}
	if got := hopPeerBaseURL("https://hc-us.internal:8443/"); got != "https://hc-us.internal:8443" {
		t.Fatalf("valid URL = %q", got)
	}
}

func TestHopTestReplyRejectsErrorEnvelope(t *testing.T) {
	t.Parallel()
	if _, err := hopTestReply([]byte(`{"error":{"message":"nope"}}`)); err == "" {
		t.Fatal("expected error envelope to fail")
	}
	reply, err := hopTestReply([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	if err != "" || reply != "pong" {
		t.Fatalf("reply=%q err=%q", reply, err)
	}
	reply, err = hopTestReply([]byte(`{"content":[{"type":"text","text":"pong"}]}`))
	if err != "" || reply != "pong" {
		t.Fatalf("anthropic reply=%q err=%q", reply, err)
	}
	reply, err = hopTestReply([]byte(`{"output_text":"pong"}`))
	if err != "" || reply != "pong" {
		t.Fatalf("responses output_text=%q err=%q", reply, err)
	}
	reply, err = hopTestReply([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}]}`))
	if err != "" || reply != "pong" {
		t.Fatalf("responses output=%q err=%q", reply, err)
	}
}

type hopCaptureWriter struct {
	buf   strings.Builder
	wrote bool
}

func (w *hopCaptureWriter) Write(p []byte) (int, error) {
	w.wrote = true
	return w.buf.Write(p)
}

func (w *hopCaptureWriter) Flush() {}

func TestHopStreamRejectsJSONContentType(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":"not a stream"}`))
	}))
	t.Cleanup(peer.Close)
	cfg := &ProxyConfig{
		LookupAccessPeer: func(string) (string, bool, int64) { return peer.URL, true, 1 },
	}
	dst := &hopCaptureWriter{}
	if _, err := hopStream(context.Background(), cfg, "hc-us", []byte(`{}`), dst, "", nil); err == nil {
		t.Fatal("JSON hop body must not be treated as an SSE stream")
	}
	if dst.wrote {
		t.Fatal("JSON hop body must not be copied onto the client stream")
	}
}

func TestTestProviderChatWithScopeHonorsHopFailure(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(upstreamHopTestResponse{Success: false})
	}))
	t.Cleanup(peer.Close)
	provider := &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-us"}}
	cfg := &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(string) (string, bool, int64) {
			return peer.URL, true, 1
		},
	}
	_, errMsg, _, _ := TestProviderChatWithScope(context.Background(), cfg, provider, "gpt-4o")
	if errMsg == "" {
		t.Fatal("hop success=false must fail the test")
	}
}

func TestHopProviderForwardRejectsEmptyHopStatus(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(peer.Close)
	provider := &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-us"}}
	cfg := &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(string) (string, bool, int64) {
			return peer.URL, true, 1
		},
	}
	if _, err := hopProviderForward(context.Background(), cfg, provider, map[string]any{"model": "gpt-4o"}, "gpt-4o", "gpt-4o"); err == nil {
		t.Fatal("empty hop status should fail")
	}
}

func TestEgressProviderStreamHopsUpstreamRateLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	t.Cleanup(upstream.Close)

	svc := NewService(&mockSystemSettings{})
	provider := llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         upstream.URL + "/v1",
		Protocol:       "openai",
		AllowedNodeIDs: []string{"hc-us"},
	}
	if err := svc.AddProvider(context.Background(), provider); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	peer := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{
		Service: svc,
		NodeID:  "hc-us",
	}, func(*http.Request) error { return nil }))
	t.Cleanup(peer.Close)

	dst := &hopCaptureWriter{}
	got, err := egressProviderStream(context.Background(), &ProxyConfig{
		Service: svc,
		NodeID:  "hc-cn",
		LookupAccessPeer: func(nodeID string) (string, bool, int64) {
			if nodeID == "hc-us" {
				return peer.URL, true, 10
			}
			return "", false, 0
		},
	}, &provider, map[string]any{
		"model":    "gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}, "gpt-4o", "gpt-4o", dst)
	if err != nil {
		t.Fatalf("egressProviderStream: %v", err)
	}
	if dst.wrote {
		t.Fatal("rate-limit hop must not copy an empty body onto the client stream")
	}
	if got == nil || got.statusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %+v, want 429 so caller can fail over", got)
	}
}

func TestHopProviderForwardTriesNextAllowedNodeAfter429(t *testing.T) {
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate"}`))
	}))
	t.Cleanup(limited.Close)
	okUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(okUp.Close)

	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         limited.URL + "/v1",
		AllowedNodeIDs: []string{"hc-a", "hc-b"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	peerA := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{
		Service: svc,
		NodeID:  "hc-a",
	}, func(*http.Request) error { return nil }))
	t.Cleanup(peerA.Close)

	svcB := NewService(&mockSystemSettings{})
	if err := svcB.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         okUp.URL + "/v1",
		AllowedNodeIDs: []string{"hc-a", "hc-b"},
	}); err != nil {
		t.Fatalf("AddProvider B: %v", err)
	}
	peerB := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{
		Service: svcB,
		NodeID:  "hc-b",
	}, func(*http.Request) error { return nil }))
	t.Cleanup(peerB.Close)

	provider := &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-a", "hc-b"}}
	got, err := hopProviderForward(context.Background(), &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(nodeID string) (string, bool, int64) {
			switch nodeID {
			case "hc-a":
				return peerA.URL, true, 5
			case "hc-b":
				return peerB.URL, true, 20
			default:
				return "", false, 0
			}
		},
	}, provider, map[string]any{"model": "gpt-4o"}, "gpt-4o", "gpt-4o")
	if err != nil {
		t.Fatalf("hopProviderForward: %v", err)
	}
	if got == nil || got.StatusCode != http.StatusOK || !strings.Contains(string(got.Body), "pong") {
		t.Fatalf("wanted failover to second allowed node, got %+v", got)
	}
}

func TestHopStreamMeasuresUsageFromSSE(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(peer.Close)
	cfg := &ProxyConfig{
		LookupAccessPeer: func(string) (string, bool, int64) { return peer.URL, true, 1 },
	}
	dst := &hopCaptureWriter{}
	got, err := hopStream(context.Background(), cfg, "hc-us", []byte(`{}`), dst, "gpt-4o", map[string]any{"model": "gpt-4o"})
	if err != nil {
		t.Fatalf("hopStream: %v", err)
	}
	if got == nil || !got.wroteBusinessStream {
		t.Fatalf("result = %+v", got)
	}
	if !got.inputTokensObserved || got.inputTokens != 12 || !got.outputTokensObserved || got.outputTokens != 3 {
		t.Fatalf("usage = in:%d/%v out:%d/%v", got.inputTokens, got.inputTokensObserved, got.outputTokens, got.outputTokensObserved)
	}
	if !strings.Contains(dst.buf.String(), "hi") {
		t.Fatalf("stream body = %q", dst.buf.String())
	}
}

func TestProbeProviderModelsWithScopeDoesNotHopUnsaved(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	t.Cleanup(upstream.Close)
	hopped := false
	provider := &llmpool.ProviderConfig{AllowedNodeIDs: []string{"hc-us"}, APIURL: upstream.URL + "/v1"}
	cfg := &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(string) (string, bool, int64) {
			hopped = true
			return "http://127.0.0.1:1", true, 1
		},
	}
	models, err := ProbeProviderModelsWithScope(context.Background(), cfg, provider, provider.APIURL, "", "openai")
	if err != nil {
		t.Fatalf("probe unsaved: %v", err)
	}
	if hopped {
		t.Fatal("unsaved provider must probe locally, not hop")
	}
	if len(models) != 1 || models[0] != "gpt-4o" {
		t.Fatalf("models = %#v", models)
	}
}

func TestHopJSONRejectsNonJSONContentType(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>upstream proxy error</html>`))
	}))
	t.Cleanup(peer.Close)
	cfg := &ProxyConfig{
		LookupAccessPeer: func(string) (string, bool, int64) { return peer.URL, true, 1 },
	}
	if _, err := hopJSON(context.Background(), cfg, "hc-us", []byte(`{}`)); err == nil {
		t.Fatal("HTML hop body must not be treated as a provider response")
	}
}

func TestHopRetryableTestStatus(t *testing.T) {
	t.Parallel()
	if got := hopRetryableTestStatus("HTTP 429"); got != http.StatusTooManyRequests {
		t.Fatalf("429 = %d", got)
	}
	if got := hopRetryableTestStatus("HTTP 503"); got != http.StatusServiceUnavailable {
		t.Fatalf("503 = %d", got)
	}
	if got := hopRetryableTestStatus(`forward to openai: POST "http://127.0.0.1:9/v1/chat/completions": 429 Too Many Requests "rate"`); got != http.StatusTooManyRequests {
		t.Fatalf("forward 429 = %d", got)
	}
	if got := hopRetryableTestStatus("HTTP 400"); got != 0 {
		t.Fatalf("400 should not retry, got %d", got)
	}
	if got := hopRetryableTestStatus("HTTP 403: hosted in China"); got != 0 {
		t.Fatalf("403 is the provider answer, not a hop retry, got %d", got)
	}
	if got := hopRetryableTestStatus("model returned no completion content"); got != 0 {
		t.Fatalf("non-HTTP = %d", got)
	}
}

func TestHopTestHTTPErrorKeepsUpstreamMessage(t *testing.T) {
	t.Parallel()
	got := hopTestHTTPError(http.StatusForbidden, []byte(`{"error":{"code":403,"message":"The latest version of this model is only available hosted in China"}}`))
	if !strings.Contains(got, "hosted in China") || !strings.Contains(got, "HTTP 403") {
		t.Fatalf("got %q", got)
	}
	got = hopTestHTTPError(http.StatusForbidden, []byte(`{"type":"error","error":{"type":"RegionError","message":"hosted in China and requires explicit opt in"}}`))
	if !strings.Contains(got, "hosted in China") {
		t.Fatalf("opencode envelope = %q", got)
	}
}

func TestHopCandidateNodeIDsSkipsLocalAndInvalidURLs(t *testing.T) {
	cfg := &ProxyConfig{
		NodeID: "hc-2",
		LookupAccessPeer: func(nodeID string) (string, bool, int64) {
			switch nodeID {
			case "hc-1":
				return "http://10.0.0.1:9388", true, 20
			case "hc-3":
				return "not-a-url", true, 1
			default:
				return "", false, 0
			}
		},
	}
	got := hopCandidateNodeIDs(cfg, &llmpool.ProviderConfig{AllowedNodeIDs: []string{"hc-1", "hc-2", "hc-3"}})
	if len(got) != 1 || got[0] != "hc-1" {
		t.Fatalf("candidates = %#v, want [hc-1]", got)
	}
}

func TestUpstreamHopHandlerEnforcesConcurrency(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-block
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok","choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         upstream.URL + "/v1",
		MaxConcurrency: 1,
		AllowedNodeIDs: []string{"hc-us"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	handler := UpstreamHopHandler(&ProxyConfig{
		Service:     svc,
		NodeID:      "hc-us",
		Concurrency: llmpool.NewConcurrencyController(),
	}, func(*http.Request) error { return nil })

	body, _ := json.Marshal(upstreamHopRequest{
		Kind:       upstreamHopKindFwd,
		ProviderID: "openai",
		Body:       map[string]any{"model": "gpt-4o"},
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(string(body)))
		req.Header.Set(upstreamHopHeader, "1")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first hop did not reach upstream")
	}

	req := httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(string(body)))
	req.Header.Set(upstreamHopHeader, "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("busy hop status = %d body=%s, want 429", rec.Code, rec.Body.Bytes())
	}
	close(block)
	<-done
}

func TestTestProviderChatWithScopeRetriesRetryableHopStatus(t *testing.T) {
	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate"}`))
	}))
	t.Cleanup(limited.Close)
	okUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`))
	}))
	t.Cleanup(okUp.Close)

	svcA := NewService(&mockSystemSettings{})
	if err := svcA.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         limited.URL + "/v1",
		AllowedNodeIDs: []string{"hc-a", "hc-b"},
		Models:         []string{"gpt-4o"},
	}); err != nil {
		t.Fatalf("AddProvider A: %v", err)
	}
	peerA := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{Service: svcA, NodeID: "hc-a"}, func(*http.Request) error { return nil }))
	t.Cleanup(peerA.Close)

	svcB := NewService(&mockSystemSettings{})
	if err := svcB.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         okUp.URL + "/v1",
		AllowedNodeIDs: []string{"hc-a", "hc-b"},
		Models:         []string{"gpt-4o"},
	}); err != nil {
		t.Fatalf("AddProvider B: %v", err)
	}
	peerB := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{Service: svcB, NodeID: "hc-b"}, func(*http.Request) error { return nil }))
	t.Cleanup(peerB.Close)

	provider := &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-a", "hc-b"}, Models: []string{"gpt-4o"}}
	reply, errMsg, _, _ := TestProviderChatWithScope(context.Background(), &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(nodeID string) (string, bool, int64) {
			switch nodeID {
			case "hc-a":
				return peerA.URL, true, 5
			case "hc-b":
				return peerB.URL, true, 20
			default:
				return "", false, 0
			}
		},
	}, provider, "gpt-4o")
	if errMsg != "" {
		t.Fatalf("expected failover to second allowed node, err=%q", errMsg)
	}
	if reply != "pong" {
		t.Fatalf("reply=%q", reply)
	}
}

func TestTestProviderChatWithScopeSurfacesUpstream403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"hosted in China and requires explicit opt in"}}`))
	}))
	t.Cleanup(upstream.Close)
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         upstream.URL + "/v1",
		AllowedNodeIDs: []string{"hc-us"},
		Models:         []string{"gpt-4o"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	peer := httptest.NewServer(UpstreamHopHandler(&ProxyConfig{Service: svc, NodeID: "hc-us"}, func(*http.Request) error { return nil }))
	t.Cleanup(peer.Close)
	_, errMsg, _, _ := TestProviderChatWithScope(context.Background(), &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(string) (string, bool, int64) {
			return peer.URL, true, 1
		},
	}, &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-us"}, Models: []string{"gpt-4o"}}, "gpt-4o")
	if errMsg == "" {
		t.Fatal("expected upstream 403")
	}
	if strings.Contains(errMsg, "hop HTTP") {
		t.Fatalf("must not wrap provider 403 as hop HTTP: %q", errMsg)
	}
	if !strings.Contains(errMsg, "hosted in China") {
		t.Fatalf("err=%q, want upstream message", errMsg)
	}
}

func TestHopProviderForwardSurfacesRetryableHopStatus(t *testing.T) {
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"busy"}`))
	}))
	t.Cleanup(peer.Close)
	provider := &llmpool.ProviderConfig{ID: "openai", AllowedNodeIDs: []string{"hc-us"}}
	got, err := hopProviderForward(context.Background(), &ProxyConfig{
		NodeID: "hc-cn",
		LookupAccessPeer: func(string) (string, bool, int64) {
			return peer.URL, true, 1
		},
	}, provider, map[string]any{"model": "gpt-4o"}, "gpt-4o", "gpt-4o")
	if err != nil {
		t.Fatalf("retryable hop HTTP should surface as a provider status, err=%v", err)
	}
	if got == nil || got.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("got %+v, want HTTP 429 so origin can fail over providers", got)
	}
}

func TestUpstreamHopHandlerMapsUpstreamRateLimitToHopStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate"}`))
	}))
	t.Cleanup(upstream.Close)
	svc := NewService(&mockSystemSettings{})
	if err := svc.AddProvider(context.Background(), llmpool.ProviderConfig{
		ID:             "openai",
		Name:           "OpenAI",
		APIURL:         upstream.URL + "/v1",
		AllowedNodeIDs: []string{"hc-us"},
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	handler := UpstreamHopHandler(&ProxyConfig{Service: svc, NodeID: "hc-us"}, func(*http.Request) error { return nil })
	body, _ := json.Marshal(upstreamHopRequest{Kind: upstreamHopKindFwd, ProviderID: "openai", Body: map[string]any{"model": "gpt-4o"}})
	req := httptest.NewRequest(http.MethodPost, upstreamHopPath, strings.NewReader(string(body)))
	req.Header.Set(upstreamHopHeader, "1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hop HTTP = %d body=%s, want 200 wrapping upstream 429", rec.Code, rec.Body.Bytes())
	}
	var out upstreamHopForwardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode hop: %v body=%s", err, rec.Body.Bytes())
	}
	if out.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status_code = %d, want 429 so origin can fail over", out.StatusCode)
	}
}

func TestForwardToProviderSurfacesUpstreamHTTPStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate"}}`))
	}))
	t.Cleanup(upstream.Close)
	got, err := forwardToProvider(context.Background(), nil, &llmpool.ProviderConfig{
		ID:     "openai",
		APIURL: upstream.URL + "/v1",
	}, map[string]any{"model": "gpt-4o", "messages": []any{map[string]any{"role": "user", "content": "hi"}}}, "gpt-4o", "gpt-4o")
	if err != nil {
		t.Fatalf("HTTP 429 must not be discarded as a transport error: %v", err)
	}
	if got == nil || got.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("got %+v, want HTTP 429", got)
	}
}
