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

// TestNoVersionHeader tests v2/chat WITHOUT the "version" header.
// Analysis of the JS bundle shows the browser NEVER sends a "version" header.
// The "version" key only exists in the internal Map, not in HTTP headers.
func TestNoVersionHeader(t *testing.T) {
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

	body := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	sr := nodeSign(t, body, "/chatApi/v2/chat")

	cookie := auth.GetCookie()
	token := extractTokenFromCookie(cookie)

	// Headers matching browser EXACTLY — NO "version" header
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
		"apptype":      "6",
		"lang":         "zh",
		"client-ver":   "1.0.2",
		"appversion":   "1.3.9",
		"token":        token,
		"deviceid":     "",
		// NO "version" header!
	}

	t.Log("Sending v2/chat WITHOUT version header...")
	raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(body), 20*time.Second)
	if err != nil {
		t.Fatalf("rawHTTPPost: %v", err)
	}
	defer raw.Close()

	t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
	bodyReader := raw.Body()
	defer bodyReader.Close()
	buf, _ := io.ReadAll(io.LimitReader(bodyReader, 4096))
	if len(buf) > 0 {
		t.Logf("Body (%d bytes): %s", len(buf), string(buf))
	} else {
		t.Log("Empty body (timeout?)")
	}
}

// TestNoVersionHeaderUTLS same but with Chrome TLS fingerprint
func TestNoVersionHeaderUTLS(t *testing.T) {
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

	body := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	sr := nodeSign(t, body, "/chatApi/v2/chat")

	cookie := auth.GetCookie()
	token := extractTokenFromCookie(cookie)

	resp, err := utlsRawPost(t, "ai-api.dangbei.net", "/ai-search/chatApi/v2/chat",
		map[string]string{
			"content-type": "application/json",
			"accept":       "text/event-stream",
			"cookie":       cookie,
			"origin":       "https://ai.dangbei.com",
			"referer":      "https://ai.dangbei.com/",
			"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			"sign":         sr.Sign,
			"nonce":        sr.Nonce,
			"timestamp":    fmt.Sprintf("%d", sr.Timestamp),
			"apptype":      "6",
			"lang":         "zh",
			"client-ver":   "1.0.2",
			"appversion":   "1.3.9",
			"token":        token,
			"deviceid":     "",
		}, []byte(body), 20*time.Second)
	if err != nil {
		t.Fatalf("utls post: %v", err)
	}

	t.Logf("Status: %s", resp.status)
	buf, _ := io.ReadAll(io.LimitReader(resp.body, 4096))
	resp.conn.Close()
	if len(buf) > 0 {
		t.Logf("Body (%d bytes): %s", len(buf), string(buf))
	} else {
		t.Log("Empty body (timeout?)")
	}
}
