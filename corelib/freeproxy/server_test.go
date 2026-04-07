package freeproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

// TestServerHealthAndModels starts the proxy server and verifies /health and /v1/models.
func TestServerHealthAndModels(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()

	addr := srv.listener.Addr().String()
	base := "http://" + addr

	// Test /health
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /health status = %d, want 200", resp.StatusCode)
	}
	var health map[string]string
	json.NewDecoder(resp.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Errorf("health status = %q, want %q", health["status"], "ok")
	}

	// Test /v1/models
	resp2, err := http.Get(base + "/v1/models")
	if err != nil {
		t.Fatalf("GET /v1/models: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /v1/models status = %d, want 200", resp2.StatusCode)
	}
	var models struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	json.NewDecoder(resp2.Body).Decode(&models)
	if models.Object != "list" {
		t.Errorf("models.object = %q, want %q", models.Object, "list")
	}
	// Should have free-proxy + all AvailableModels
	wantCount := 1 + len(AvailableModels())
	if len(models.Data) != wantCount {
		t.Errorf("models count = %d, want %d", len(models.Data), wantCount)
	}
	// First model should be "free-proxy"
	if len(models.Data) > 0 {
		if id, _ := models.Data[0]["id"].(string); id != "free-proxy" {
			t.Errorf("first model id = %q, want %q", id, "free-proxy")
		}
	}
}

// TestServerCompletionNoAuth verifies that /v1/chat/completions returns 401
// when no cookie is set (not logged in).
func TestServerCompletionNoAuth(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()

	addr := srv.listener.Addr().String()
	base := "http://" + addr

	body, _ := json.Marshal(map[string]interface{}{
		"model":    "deepseek_r1",
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, string(respBody))
	}

	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)
	if errResp.Error.Message == "" {
		t.Error("expected error message in response")
	}
}

// TestServerCompletionMethodNotAllowed verifies GET is rejected.
func TestServerCompletionMethodNotAllowed(t *testing.T) {
	srv, cancel := startTestServer(t)
	defer cancel()

	addr := srv.listener.Addr().String()
	resp, err := http.Get(fmt.Sprintf("http://%s/v1/chat/completions", addr))
	if err != nil {
		t.Fatalf("GET /v1/chat/completions: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}
