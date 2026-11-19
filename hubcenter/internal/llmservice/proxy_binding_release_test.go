package llmservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyBindingReleaseRequiresNodeID(t *testing.T) {
	cfg := &ProxyConfig{}
	srv := httptest.NewServer(ProxyBindingReleaseHandler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["code"] != InvalidBindingReleaseCode {
		t.Fatalf("code = %v, want %s", payload["code"], InvalidBindingReleaseCode)
	}
}

func TestProxyBindingReleaseWithoutBindingSupport(t *testing.T) {
	cfg := &ProxyConfig{}
	srv := httptest.NewServer(ProxyBindingReleaseHandler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"node_id":"hc-1"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["released"] != float64(0) {
		t.Fatalf("released = %v, want 0", payload["released"])
	}
}

func TestProxyBindingReleaseRejectsUnauthenticatedHub(t *testing.T) {
	called := false
	cfg := &ProxyConfig{
		ReleaseBinding: func(_ context.Context, _, _ string) (int, error) {
			called = true
			return 1, nil
		},
		VerifyHubSecret: func(_ context.Context, _, _ string) error {
			return context.Canceled
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/binding/release", strings.NewReader(`{"node_id":"hc-1"}`))
	req.Header.Set("X-Hub-ID", "hub-1")
	req.Header.Set("Authorization", "Bearer wrong-secret")
	rec := httptest.NewRecorder()
	ProxyBindingReleaseHandler(cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("unauthenticated request reached ReleaseBinding")
	}
}

func TestProxyBindingReleaseReturnsCount(t *testing.T) {
	var gotHubID, gotNodeID string
	cfg := &ProxyConfig{
		ReleaseBinding: func(_ context.Context, hubID, nodeID string) (int, error) {
			gotHubID, gotNodeID = hubID, nodeID
			return 2, nil
		},
		VerifyHubSecret: func(_ context.Context, hubID, rawSecret string) error {
			if hubID != "hub-1" || rawSecret != "hub-secret" {
				return context.Canceled
			}
			return nil
		},
	}
	srv := httptest.NewServer(ProxyBindingReleaseHandler(cfg))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"node_id":"hc-1"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Hub-ID", "hub-1")
	req.Header.Set("Authorization", "Bearer hub-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["released"] != float64(2) {
		t.Fatalf("released = %v, want 2", payload["released"])
	}
	if gotHubID != "hub-1" || gotNodeID != "hc-1" {
		t.Fatalf("ReleaseBinding called with hub=%q node=%q", gotHubID, gotNodeID)
	}
}

func TestProxyBindingReleaseRejectsNonJSONBody(t *testing.T) {
	cfg := &ProxyConfig{}
	srv := httptest.NewServer(ProxyBindingReleaseHandler(cfg))
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`not-json`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["code"] != InvalidBindingReleaseCode {
		t.Fatalf("code = %v, want %s", payload["code"], InvalidBindingReleaseCode)
	}
}

func TestProxyBindingReleaseRejectsNonPost(t *testing.T) {
	cfg := &ProxyConfig{}
	srv := httptest.NewServer(ProxyBindingReleaseHandler(cfg))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
