package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestBrowserSimSignAndRawSend signs with Node.js (browser-simulated WASM) then sends via Go raw transport.
// This isolates signing from transport — if this works, the signing is correct and we just need
// to port the browser simulation to Go's wasm_host.go.
func TestBrowserSimSignAndRawSend(t *testing.T) {
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

	// Step 1: Get sign from Node.js with browser simulation
	wd, _ := os.Getwd()
	scriptPath := filepath.Join(wd, "browser_sim_signonly.mjs")
	cmd := exec.Command("node", scriptPath, bodyStr, "/chatApi/v2/chat")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Node sign failed: %v\n%s", err, string(out))
	}

	var sr struct {
		Sign      string  `json:"sign"`
		Nonce     string  `json:"nonce"`
		Timestamp float64 `json:"timestamp"`
	}
	if err := json.Unmarshal(out, &sr); err != nil {
		t.Fatalf("Parse sign result: %v\nRaw: %s", err, string(out))
	}
	t.Logf("Node sign: sign=%s nonce=%s ts=%.0f", sr.Sign, sr.Nonce, sr.Timestamp)

	// Step 2: Send via Go raw transport
	cookie := auth.GetCookie()
	headers := map[string]string{
		"content-type": "application/json",
		"accept":       "text/event-stream",
		"cookie":       cookie,
		"origin":       "https://ai.dangbei.com",
		"referer":      "https://ai.dangbei.com/",
		"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"sign":         sr.Sign,
		"nonce":        sr.Nonce,
		"timestamp":    fmt.Sprintf("%.0f", sr.Timestamp),
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
		if len(show) > 2000 {
			show = show[:2000]
		}
		t.Logf("Body (%d bytes): %s", len(buf), show)
	}

	if raw.StatusCode == 200 {
		t.Log("SUCCESS — browser-simulated signing works!")
	} else {
		t.Logf("Got status %d", raw.StatusCode)
	}
}
