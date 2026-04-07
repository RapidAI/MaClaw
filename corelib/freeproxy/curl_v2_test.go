package freeproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCurlV2Chat uses curl.exe to send a signed v2/chat request.
// curl uses Schannel on Windows (different TLS stack than Go/Node.js).
// If curl also times out, it's not a TLS fingerprinting issue.
func TestCurlV2Chat(t *testing.T) {
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

	cookie := auth.GetCookie()

	// Extract token from cookie
	tokenValue := ""
	for _, c := range strings.Split(cookie, ";") {
		parts := strings.SplitN(strings.TrimSpace(c), "=", 2)
		if len(parts) == 2 && parts[0] == "token" {
			tokenValue = parts[1]
		}
	}

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	// Get sign from Node.js browser-sim
	sr := nodeSign(t, bodyStr, "/chatApi/v2/chat")

	// Build curl command
	url := "https://ai-api.dangbei.net/ai-search/chatApi/v2/chat"
	args := []string{
		"-v",
		"--max-time", "15",
		"-X", "POST",
		"-H", "content-type: application/json",
		"-H", "accept: text/event-stream",
		"-H", fmt.Sprintf("cookie: %s", cookie),
		"-H", "origin: https://ai.dangbei.com",
		"-H", "referer: https://ai.dangbei.com/",
		"-H", "user-agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"-H", fmt.Sprintf("sign: %s", sr.Sign),
		"-H", fmt.Sprintf("nonce: %s", sr.Nonce),
		"-H", fmt.Sprintf("timestamp: %d", sr.Timestamp),
		"-H", "deviceid: ",
		"-H", "apptype: 6",
		"-H", "lang: zh",
		"-H", "client-ver: 1.0.2",
		"-H", "appversion: 1.3.9",
		"-H", "version: v2",
		"-H", fmt.Sprintf("token: %s", tokenValue),
		"-d", bodyStr,
		url,
	}

	t.Logf("Running curl with sign=%s nonce=%s ts=%d token=%s", sr.Sign, sr.Nonce, sr.Timestamp, tokenValue)

	cmd := exec.Command("curl.exe", args...)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	t.Logf("Completed in %v", elapsed)
	outStr := string(out)
	if len(outStr) > 3000 {
		outStr = outStr[:3000]
	}
	t.Logf("Output:\n%s", outStr)
	if err != nil {
		t.Logf("Exit: %v", err)
	}
}

// TestCurlV2ChatNoSign uses curl to send v2/chat WITHOUT signing but with a valid conversationId.
// This confirms the "hang" behavior is server-side (not client TLS).
func TestCurlV2ChatNoSign(t *testing.T) {
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

	cookie := auth.GetCookie()

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	url := "https://ai-api.dangbei.net/ai-search/chatApi/v2/chat"
	args := []string{
		"-v",
		"--max-time", "10",
		"-X", "POST",
		"-H", "content-type: application/json",
		"-H", fmt.Sprintf("cookie: %s", cookie),
		"-H", "origin: https://ai.dangbei.com",
		"-d", bodyStr,
		url,
	}

	cmd := exec.Command("curl.exe", args...)
	start := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	t.Logf("Completed in %v (no sign)", elapsed)
	outStr := string(out)
	if len(outStr) > 2000 {
		outStr = outStr[:2000]
	}
	t.Logf("Output:\n%s", outStr)
	_ = err
	_ = json.Marshal // suppress unused import
}
