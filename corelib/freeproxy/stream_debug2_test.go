package freeproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStreamDebugForceHTTP1 forces HTTP/1.1 (no HTTP/2) to test if the
// server has issues with Go's HTTP/2 implementation.
func TestStreamDebugForceHTTP1(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(configDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	payload := map[string]interface{}{
		"stream":         true,
		"botCode":        "AI_SEARCH",
		"conversationId": convID,
		"question":       "hi",
		"agentId":        "",
	}
	body, _ := json.Marshal(payload)
	t.Logf("Body: %s", string(body))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()

	// Force HTTP/1.1 by disabling HTTP/2 in TLS ALPN
	h1Client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				NextProtos: []string{"http/1.1"}, // only offer HTTP/1.1
			},
			DisableCompression: true,
		},
	}

	t.Log("Sending request (forced HTTP/1.1)...")
	start := time.Now()
	resp, err := h1Client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do (after %v): %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v", elapsed)
	t.Logf("Status: %s", resp.Status)
	t.Logf("Proto: %s", resp.Proto)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	// Read body
	buf := make([]byte, 8192)
	totalRead := 0
	for totalRead < len(buf) {
		n, readErr := resp.Body.Read(buf[totalRead:])
		if n > 0 {
			totalRead += n
			fmt.Printf("[+%d bytes, total=%d, elapsed=%v]\n", n, totalRead, time.Since(start))
		}
		if readErr == io.EOF {
			t.Logf("EOF after %d bytes", totalRead)
			break
		}
		if readErr != nil {
			t.Logf("Read error after %d bytes: %v", totalRead, readErr)
			break
		}
	}
	if totalRead > 0 {
		show := totalRead
		if show > 4000 {
			show = 4000
		}
		t.Logf("Body:\n%s", string(buf[:show]))
	}
}
