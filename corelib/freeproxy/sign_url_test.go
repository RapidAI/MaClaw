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

// TestSignWithDifferentURLs tries signing with different URL formats to find the correct one.
func TestSignWithDifferentURLs(t *testing.T) {
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

	// Try different URL formats and appType values
	urlVariants := []string{
		"/chatApi/v2/chat",
		"/ai-search/chatApi/v2/chat",
		"https://ai-api.dangbei.net/ai-search/chatApi/v2/chat",
	}

	// From JS: mac=4, win=5, web=6, h5=7
	appTypes := []string{"5", "6"}

	for _, signURL := range urlVariants {
		for _, appType := range appTypes {
			name := fmt.Sprintf("url=%s_appType=%s", signURL, appType)
			t.Run(name, func(t *testing.T) {
				sr, err := signer.Sign(ctx, bodyStr, signURL)
				if err != nil {
					t.Fatalf("Sign: %v", err)
				}
				t.Logf("sign=%s ts=%d", sr.Sign, sr.Timestamp)

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
					"apptype":      appType,
					"lang":         "zh",
					"client-ver":   "1.0.2",
					"appversion":   "1.3.9",
					"version":      "v2",
					"token":        "",
					"connection":   "close",
				}

				raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 10*time.Second)
				if err != nil {
					t.Logf("TIMEOUT: %v", err)
					return
				}
				defer raw.Close()

				t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
				bodyReader := raw.Body()
				defer bodyReader.Close()
				buf, _ := io.ReadAll(io.LimitReader(bodyReader, 2048))
				if len(buf) > 0 {
					show := string(buf)
					if len(show) > 500 {
						show = show[:500]
					}
					t.Logf("Body: %s", show)
				}
			})
		}
	}
}
