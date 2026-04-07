package freeproxy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestV1AgentChatRawTransport tests v1/agentChat with v1 MD5 signing via raw transport.
// This verifies that raw transport itself works correctly.
func TestV1AgentChatRawTransport(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(cacheDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx := context.Background()
	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	timestamp := time.Now().Unix()
	nonce := generateNonce(21)
	sign := v1Sign(timestamp, bodyStr, nonce)
	t.Logf("v1 sign=%s nonce=%s ts=%d", sign, nonce, timestamp)

	cookie := auth.GetCookie()
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"sign":         sign,
		"nonce":        nonce,
		"timestamp":    fmt.Sprintf("%d", timestamp),
		"apptype":      "5",
		"lang":         "zh",
		"client-ver":   "1.0.2",
		"appversion":   "1.3.9",
		"version":      "v1",
		"token":        "",
		"connection":   "keep-alive",
	}

	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/agentApi/v1/agentChat", headers, []byte(bodyStr), 20*time.Second)
	if err != nil {
		t.Fatalf("rawHTTPPost: %v", err)
	}
	defer raw.Close()

	t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
	for k, v := range raw.Headers {
		t.Logf("  %s: %s", k, v)
	}

	bodyReader := raw.Body()
	defer bodyReader.Close()
	buf, _ := io.ReadAll(io.LimitReader(bodyReader, 4096))
	if len(buf) > 0 {
		show := string(buf)
		if len(show) > 2000 {
			show = show[:2000]
		}
		t.Logf("Body (%d bytes): %s", len(buf), show)
	}

	if raw.StatusCode == 200 {
		t.Log("SUCCESS — v1 signing + raw transport works!")
	}
}

// TestV2ChatWithV1SignRawTransport tests v2/chat with v1 MD5 signing via raw transport.
// If v2/chat accepts v1 signing, we don't need WASM at all.
func TestV2ChatWithV1SignRawTransport(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	auth := NewAuthStore(cacheDir)
	if err := auth.Load(); err != nil {
		t.Fatalf("Load auth: %v", err)
	}
	if !auth.HasAuth() {
		t.Skip("No persisted cookie")
	}

	client := NewDangbeiClient(auth)
	ctx := context.Background()
	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	timestamp := time.Now().Unix()
	nonce := generateNonce(21)
	sign := v1Sign(timestamp, bodyStr, nonce)
	t.Logf("v1 sign=%s nonce=%s ts=%d", sign, nonce, timestamp)

	cookie := auth.GetCookie()

	// Try v2/chat with v1 signing but version=v1 header
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"sign":         sign,
		"nonce":        nonce,
		"timestamp":    fmt.Sprintf("%d", timestamp),
		"apptype":      "5",
		"lang":         "zh",
		"client-ver":   "1.0.2",
		"appversion":   "1.3.9",
		"version":      "v1",
		"token":        "",
		"connection":   "keep-alive",
	}

	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 20*time.Second)
	if err != nil {
		t.Fatalf("rawHTTPPost: %v", err)
	}
	defer raw.Close()

	t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)

	bodyReader := raw.Body()
	defer bodyReader.Close()
	buf, _ := io.ReadAll(io.LimitReader(bodyReader, 4096))
	if len(buf) > 0 {
		show := string(buf)
		if len(show) > 2000 {
			show = show[:2000]
		}
		t.Logf("Body (%d bytes): %s", len(buf), show)
	}

	if raw.StatusCode == 200 {
		t.Log("SUCCESS — v2/chat accepts v1 signing!")
	}
}
