package freeproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestV2DiagVariants tests v2/chat with browser-sim signing and various header combinations
// to find what exactly causes the timeout.
func TestV2DiagVariants(t *testing.T) {
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

	// Test 1: v2/chat with NO conversationId (should get 400 quickly)
	t.Run("no_convId_no_sign", func(t *testing.T) {
		bodyStr := `{"stream":true,"botCode":"AI_SEARCH","question":"hi","agentId":""}`
		headers := map[string]string{
			"content-type": "application/json",
			"accept":       "text/event-stream",
			"cookie":       cookie,
			"origin":       "https://ai.dangbei.com",
			"referer":      "https://ai.dangbei.com/",
			"user-agent":   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			"connection":   "keep-alive",
		}
		raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 10*time.Second)
		if err != nil {
			t.Logf("Error: %v", err)
			return
		}
		defer raw.Close()
		t.Logf("Status: %d", raw.StatusCode)
		bodyReader := raw.Body()
		defer bodyReader.Close()
		buf, _ := io.ReadAll(io.LimitReader(bodyReader, 1024))
		t.Logf("Body: %s", string(buf))
	})

	// Test 2: v2/chat with conversationId + browser-sim sign, stream=false
	t.Run("stream_false_with_sign", func(t *testing.T) {
		convID, err := client.CreateSession(ctx)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		defer client.DeleteSession(context.Background(), convID)

		bodyStr := fmt.Sprintf(
			`{"stream":false,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
			convID,
		)

		sr := nodeSign(t, bodyStr, "/chatApi/v2/chat")

		headers := map[string]string{
			"content-type": "application/json",
			"accept":       "application/json",
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

		raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 15*time.Second)
		if err != nil {
			t.Logf("Error: %v", err)
			return
		}
		defer raw.Close()
		t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
		bodyReader := raw.Body()
		defer bodyReader.Close()
		buf, _ := io.ReadAll(io.LimitReader(bodyReader, 2048))
		t.Logf("Body: %s", string(buf))
	})

	// Test 3: v2/chat with conversationId + v1 sign + version=v2
	t.Run("v1_sign_v2_version", func(t *testing.T) {
		convID, err := client.CreateSession(ctx)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		defer client.DeleteSession(context.Background(), convID)

		bodyStr := fmt.Sprintf(
			`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
			convID,
		)

		timestamp := time.Now().Unix()
		nonce := generateNonce(21)
		sign := v1Sign(timestamp, bodyStr, nonce)

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
			"version":      "v2",
			"token":        "",
			"connection":   "keep-alive",
		}

		raw, err := rawHTTPPost("ai-api.dangbei.net", "/ai-search/chatApi/v2/chat", headers, []byte(bodyStr), 15*time.Second)
		if err != nil {
			t.Logf("Error (expected timeout): %v", err)
			return
		}
		defer raw.Close()
		t.Logf("Status: %d %s", raw.StatusCode, raw.StatusText)
		bodyReader := raw.Body()
		defer bodyReader.Close()
		buf, _ := io.ReadAll(io.LimitReader(bodyReader, 2048))
		t.Logf("Body: %s", string(buf))
	})

	// Test 4: agentApi/v1/agentChat with v1 sign via raw transport (known working endpoint)
	t.Run("v1_agentChat_v1_sign", func(t *testing.T) {
		convID, err := client.CreateSession(ctx)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		defer client.DeleteSession(context.Background(), convID)

		bodyStr := fmt.Sprintf(
			`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"say hello","agentId":""}`,
			convID,
		)

		timestamp := time.Now().Unix()
		nonce := generateNonce(21)
		sign := v1Sign(timestamp, bodyStr, nonce)

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
			t.Logf("Error: %v", err)
			return
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
	})
}

type nodeSignResult struct {
	Sign      string
	Nonce     string
	Timestamp int64
}

func nodeSign(t *testing.T, body, url string) nodeSignResult {
	t.Helper()
	wd, _ := os.Getwd()
	scriptPath := filepath.Join(wd, "browser_sim_signonly.mjs")
	cmd := exec.Command("node", scriptPath, body, url)
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
	return nodeSignResult{
		Sign:      sr.Sign,
		Nonce:     sr.Nonce,
		Timestamp: int64(sr.Timestamp),
	}
}


// TestV2NetHTTPWithBrowserSimSign uses net/http (not raw transport) with browser-sim signing.
// This tests whether the issue is raw transport or signing.
func TestV2NetHTTPWithBrowserSimSign(t *testing.T) {
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

	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	sr := nodeSign(t, bodyStr, "/chatApi/v2/chat")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader([]byte(bodyStr)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = client.authHeaders()
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("sign", sr.Sign)
	req.Header.Set("nonce", sr.Nonce)
	req.Header.Set("timestamp", fmt.Sprintf("%d", sr.Timestamp))
	req.Header.Set("appType", "5")
	req.Header.Set("lang", "zh")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("appVersion", "1.3.9")
	req.Header.Set("version", "v2")
	req.Header.Set("token", "")

	httpClient := &http.Client{Timeout: 20 * time.Second}
	t.Log("Sending v2/chat via net/http with browser-sim sign...")
	start := time.Now()
	resp, err := httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do (after %v): %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v, status: %s", elapsed, resp.Status)
	for k, v := range resp.Header {
		t.Logf("  %s: %v", k, v)
	}

	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(buf) > 0 {
		show := string(buf)
		if len(show) > 2000 {
			show = show[:2000]
		}
		t.Logf("Body (%d bytes): %s", len(buf), show)
	}

	if resp.StatusCode == 200 {
		t.Log("SUCCESS!")
	}
}
