package freeproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStreamDebugRawResponse does a raw v2/chat POST and dumps the response
// headers + first bytes to diagnose why streaming times out.
func TestStreamDebugRawResponse(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Verify auth first
	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	// Create conversation
	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	// Build the exact same request as StreamCompletion
	payload := map[string]interface{}{
		"stream":         true,
		"botCode":        "AI_SEARCH",
		"conversationId": convID,
		"question":       "hi",
		"agentId":        "",
	}
	body, _ := json.Marshal(payload)
	t.Logf("Request body: %s", string(body))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()

	// Dump the full request for debugging
	reqDump, _ := httputil.DumpRequestOut(req, true)
	t.Logf("=== REQUEST ===\n%s", string(reqDump))

	// Use a raw http.Client with no timeout (context handles it)
	rawClient := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true, // don't auto-decompress, see raw bytes
		},
	}

	t.Log("Sending request...")
	resp, err := rawClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("=== RESPONSE STATUS: %s ===", resp.Status)
	t.Logf("=== RESPONSE HEADERS ===")
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	// Read first 4KB of body byte-by-byte with a per-byte timeout
	t.Log("=== READING BODY (first 4KB) ===")
	buf := make([]byte, 4096)
	totalRead := 0
	for totalRead < len(buf) {
		n, err := resp.Body.Read(buf[totalRead:])
		totalRead += n
		if err == io.EOF {
			t.Logf("EOF after %d bytes", totalRead)
			break
		}
		if err != nil {
			t.Logf("Read error after %d bytes: %v", totalRead, err)
			break
		}
	}

	t.Logf("Total bytes read: %d", totalRead)
	if totalRead > 0 {
		t.Logf("Body (raw):\n%s", string(buf[:totalRead]))
	}
}

// TestStreamDebugWithAcceptSSE tries with Accept: text/event-stream header.
func TestStreamDebugWithAcceptSSE(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()
	// Try SSE accept header
	req.Header.Set("Accept", "text/event-stream")

	rawClient := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}

	t.Log("Sending request with Accept: text/event-stream...")
	resp, err := rawClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("Status: %s", resp.Status)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	// Read up to 4KB
	buf := make([]byte, 4096)
	totalRead := 0
	for totalRead < len(buf) {
		n, err := resp.Body.Read(buf[totalRead:])
		totalRead += n
		if err != nil {
			t.Logf("Read done: %d bytes, err=%v", totalRead, err)
			break
		}
	}
	if totalRead > 0 {
		t.Logf("Body:\n%s", string(buf[:totalRead]))
	}
}

// TestStreamDebugFetchStyle tries to mimic browser Fetch API more closely
// with specific headers that the JS bundle might be sending.
func TestStreamDebugFetchStyle(t *testing.T) {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	// Mimic browser Fetch API headers exactly
	cookie := auth.GetCookie()
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Sec-Ch-Ua", `"Chromium";v="131", "Not_A Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	rawClient := &http.Client{
		Transport: &http.Transport{
			DisableCompression: false, // let Go handle gzip
		},
	}

	t.Log("Sending fetch-style request...")
	resp, err := rawClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("Status: %s", resp.Status)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	// Stream read with progress logging
	buf := make([]byte, 8192)
	totalRead := 0
	for totalRead < len(buf) {
		n, err := resp.Body.Read(buf[totalRead:])
		if n > 0 {
			totalRead += n
			fmt.Printf("[+%d bytes, total=%d]\n", n, totalRead)
		}
		if err != nil {
			t.Logf("Read done: %d bytes, err=%v", totalRead, err)
			break
		}
	}
	if totalRead > 0 {
		// Print first 2000 chars
		show := totalRead
		if show > 2000 {
			show = 2000
		}
		t.Logf("Body (first %d chars):\n%s", show, string(buf[:show]))
	}
}
