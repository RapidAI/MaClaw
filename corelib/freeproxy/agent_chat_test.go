package freeproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAgentChatV1 tests the /agentApi/v1/agentChat endpoint which uses v1 signing.
func TestAgentChatV1(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	// Create agent conversation (must use agentApi/v1/create)
	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	// agentApi/v1/agentChat
	payload := map[string]interface{}{
		"stream":         true,
		"botCode":        "AI_SEARCH",
		"conversationId": convID,
		"question":       "hi",
		"agentId":        dangbeiAgentID,
	}
	body, _ := json.Marshal(payload)
	bodyStr := string(body)
	t.Logf("Body: %s", bodyStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/agentApi/v1/agentChat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()
	for k, v := range v1SignHeaders(bodyStr) {
		req.Header.Set(k, v)
	}

	rawClient := &http.Client{}

	t.Log("Sending agentApi/v1/agentChat request...")
	start := time.Now()
	resp, err := rawClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do (after %v): %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v, status: %s", elapsed, resp.Status)
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
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			t.Logf("Read error: %v", readErr)
			break
		}
	}
	if totalRead > 0 {
		show := totalRead
		if show > 4000 {
			show = 4000
		}
		t.Logf("Body (%d bytes):\n%s", totalRead, string(buf[:show]))
	}
}

// TestV2ChatWithV1Signing tests if v2/chat works with v1 MD5 signing.
func TestV2ChatWithV1Signing(t *testing.T) {
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
		"agentId":        dangbeiAgentID,
	}
	body, _ := json.Marshal(payload)
	bodyStr := string(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()

	// Try v1 signing on v2 endpoint
	for k, v := range v1SignHeaders(bodyStr) {
		req.Header.Set(k, v)
	}

	rawClient := &http.Client{}

	t.Log("Sending v2/chat with v1 signing...")
	start := time.Now()
	resp, err := rawClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do (after %v): %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v, status: %s", elapsed, resp.Status)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	buf := make([]byte, 4096)
	totalRead := 0
	for totalRead < len(buf) {
		n, readErr := resp.Body.Read(buf[totalRead:])
		if n > 0 {
			totalRead += n
		}
		if readErr != nil {
			break
		}
	}
	if totalRead > 0 {
		t.Logf("Body (%d bytes):\n%s", totalRead, string(buf[:totalRead]))
	}
}
