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

// TestRawTransportUnsigned tests raw HTTP transport to v2/chat WITHOUT signing.
// Uses different body variants to find which field causes the server to hang.
func TestRawTransportUnsigned(t *testing.T) {
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

	cookie := auth.GetCookie()

	variants := []struct {
		name string
		body string
	}{
		{"minimal", `{"stream":true}`},
		{"with_botCode", `{"stream":true,"botCode":"AI_SEARCH"}`},
		{"with_convId", `{"stream":true,"botCode":"AI_SEARCH","conversationId":"test123"}`},
		{"with_question", `{"stream":true,"botCode":"AI_SEARCH","conversationId":"test123","question":"hi"}`},
		{"full", `{"stream":true,"botCode":"AI_SEARCH","conversationId":"test123","question":"hi","agentId":""}`},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			headers := map[string]string{
				"content-type": "application/json",
				"cookie":       cookie,
				"origin":       "https://ai.dangbei.com",
				"connection":   "close",
			}

			raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(v.body), 8*time.Second)
			if err != nil {
				t.Logf("TIMEOUT: %v", err)
				return
			}
			defer raw.Close()

			t.Logf("Status: %d", raw.StatusCode)
			bodyReader := raw.Body()
			defer bodyReader.Close()
			buf, _ := io.ReadAll(io.LimitReader(bodyReader, 1024))
			if len(buf) > 0 {
				t.Logf("Body: %s", string(buf))
			}
		})
	}
}

// TestRawTransportSigned tests raw HTTP transport to v2/chat WITH WASM signing.
func TestRawTransportSigned(t *testing.T) {
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

	cookie := auth.GetCookie()

	signer := NewWasmSigner(cacheDir)
	defer signer.Close(ctx)
	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init signer: %v", err)
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

	sr, err := signer.Sign(ctx, bodyStr, "/chatApi/v2/chat")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)

	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"sign":         sr.Sign,
		"nonce":        sr.Nonce,
		"timestamp":    fmt.Sprintf("%d", sr.Timestamp),
		"apptype":      "5",
		"lang":         "zh",
		"client-ver":   "1.0.2",
		"appversion":   "1.3.9",
		"version":      "v2",
		"token":        "",
		"connection":   "keep-alive",
	}

	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 20*time.Second)
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
		if len(show) > 1000 {
			show = show[:1000]
		}
		t.Logf("Body (%d bytes): %s", len(buf), show)
	}

	if raw.StatusCode == 200 {
		t.Log("SUCCESS — signed request got 200")
	} else {
		t.Logf("Got status %d — signing may be incorrect", raw.StatusCode)
	}
}

// TestRawTransportE2E tests the full flow: create session, sign, stream via raw transport.
func TestRawTransportE2E(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	cr := CompletionRequest{
		ConversationID: convID,
		Prompt:         "say hello in one word",
	}

	var tokens []string
	fullText, _, err := client.StreamCompletion(ctx, cr, func(token string) {
		tokens = append(tokens, token)
		fmt.Print(token)
	})
	fmt.Println()

	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	t.Logf("Full text (%d chars, %d tokens): %s", len(fullText), len(tokens), fullText)

	if len(fullText) == 0 {
		t.Error("Empty response")
	}
}
