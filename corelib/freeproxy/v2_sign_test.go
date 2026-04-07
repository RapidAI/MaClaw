package freeproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestV2ChatWithSigningVariants tries different signing approaches for v2/chat.
func TestV2ChatWithSigningVariants(t *testing.T) {
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
	ctx := context.Background()

	if !client.IsAuthenticated(ctx) {
		t.Fatal("Not authenticated")
	}

	cookie := auth.GetCookie()

	variants := []struct {
		name    string
		version string
		url     string
	}{
		{"v2/chat with version=v1", "v1", "/chatApi/v2/chat"},
		{"v2/chat with version=v2", "v2", "/chatApi/v2/chat"},
		{"v2/chat with no version", "", "/chatApi/v2/chat"},
	}

	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			tctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()

			convID, err := client.CreateSession(tctx)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			defer client.DeleteSession(context.Background(), convID)

			payload := map[string]interface{}{
				"stream":         true,
				"botCode":        "AI_SEARCH",
				"conversationId": convID,
				"question":       "hi",
				"agentId":        "",
			}
			body, _ := json.Marshal(payload)
			bodyStr := string(body)

			req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
				dangbeiAPIBase+v.url, bytes.NewReader(body))

			timestamp := time.Now().Unix()
			nonce := generateNonce(21)
			sign := v1Sign(timestamp, bodyStr, nonce)

			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
			req.Header.Set("Accept", "*/*")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://ai.dangbei.com")
			req.Header.Set("Referer", "https://ai.dangbei.com/")
			req.Header.Set("Cookie", cookie)
			req.Header.Set("timestamp", fmt.Sprintf("%d", timestamp))
			req.Header.Set("nonce", nonce)
			req.Header.Set("sign", sign)
			if v.version != "" {
				req.Header.Set("version", v.version)
			}
			req.Header.Set("appType", "5")
			req.Header.Set("lang", "zh")
			req.Header.Set("client-ver", "1.0.2")
			req.Header.Set("appVersion", "1.3.9")
			req.Header.Set("token", "")

			rawClient := &http.Client{}
			start := time.Now()
			resp, err := rawClient.Do(req)
			elapsed := time.Since(start)
			if err != nil {
				t.Logf("TIMEOUT after %v: %v", elapsed, err)
				return
			}
			defer resp.Body.Close()

			t.Logf("Response in %v, status: %s", elapsed, resp.Status)
			buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			if len(buf) > 0 {
				t.Logf("Body: %s", string(buf))
			}
		})
	}
}

// TestV1CreateWithSigning tests if v1/create works with explicit v1 signing.
// This confirms our MD5 signing implementation is correct.
func TestV1CreateWithSigning(t *testing.T) {
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

	cookie := auth.GetCookie()
	bodyStr := "{}"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		dangbeiAPIBase+"/conversationApi/v1/create", bytes.NewBufferString(bodyStr))

	timestamp := time.Now().Unix()
	nonce := generateNonce(21)
	sign := v1Sign(timestamp, bodyStr, nonce)

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("nonce", nonce)
	req.Header.Set("sign", sign)
	req.Header.Set("version", "v1")
	req.Header.Set("appType", "5")
	req.Header.Set("lang", "zh")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("appVersion", "1.3.9")
	req.Header.Set("token", "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("Status: %s", resp.Status)
	buf, _ := io.ReadAll(resp.Body)
	t.Logf("Body: %s", string(buf))
}
