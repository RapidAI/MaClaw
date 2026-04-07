package freeproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWasmSignerInit(t *testing.T) {
	requireIntegrationTest(t)
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".maclaw", "freeproxy")

	signer := NewWasmSigner(cacheDir)
	signer.debug = true
	ctx := context.Background()
	defer signer.Close(ctx)

	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Log("WASM signer initialized successfully")

	body := `{"stream":true,"botCode":"AI_SEARCH","conversationId":"test123","question":"hi","agentId":""}`
	url := "/chatApi/v2/chat"

	sr, err := signer.Sign(ctx, body, url)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("Sign:      %s", sr.Sign)
	t.Logf("Nonce:     %s", sr.Nonce)
	t.Logf("Timestamp: %d", sr.Timestamp)

	if sr.Sign == "" {
		t.Fatal("empty sign")
	}
	if sr.Nonce == "" {
		t.Fatal("empty nonce")
	}
	if sr.Timestamp == 0 {
		t.Fatal("zero timestamp")
	}
}

// TestWasmSignE2E tests the full signing + v2/chat flow with multiple URL variants.
func TestWasmSignE2E(t *testing.T) {
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

	signer := NewWasmSigner(cacheDir)
	defer signer.Close(ctx)
	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init signer: %v", err)
	}

	cookie := auth.GetCookie()

	urlVariants := []string{
		"/chatApi/v2/chat",
	}

	for _, signURL := range urlVariants {
		t.Run(signURL, func(t *testing.T) {
			tctx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()

			convID, err := client.CreateSession(tctx)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			defer client.DeleteSession(context.Background(), convID)

			// Use exact same key order as browser JS: stream, botCode, conversationId, question, agentId
			bodyStr := fmt.Sprintf(
				`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
				convID,
			)
			body := []byte(bodyStr)

			sr, err := signer.Sign(tctx, bodyStr, signURL)
			if err != nil {
				t.Fatalf("Sign: %v", err)
			}
			t.Logf("sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)

			req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
				dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://ai.dangbei.com")
			req.Header.Set("Referer", "https://ai.dangbei.com/")
			req.Header.Set("Cookie", cookie)
			req.Header.Set("sign", sr.Sign)
			req.Header.Set("nonce", sr.Nonce)
			req.Header.Set("timestamp", fmt.Sprintf("%d", sr.Timestamp))
			req.Header.Set("appType", "5")
			req.Header.Set("lang", "zh")
			req.Header.Set("client-ver", "1.0.2")
			req.Header.Set("appVersion", "1.3.9")
			req.Header.Set("version", "v2")

			start := time.Now()
			resp, err := http.DefaultClient.Do(req)
			elapsed := time.Since(start)
			if err != nil {
				t.Logf("FAILED after %v: %v", elapsed, err)
				return
			}
			defer resp.Body.Close()

			t.Logf("Response in %v, status: %s", elapsed, resp.Status)
			buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
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

// TestWasmSignRawDebug does a raw HTTP request with WASM signing and verbose connection debug.
func TestWasmSignRawDebug(t *testing.T) {
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

	signer := NewWasmSigner(cacheDir)
	defer signer.Close(ctx)
	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init signer: %v", err)
	}

	cookie := auth.GetCookie()

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	// Use exact same key order as browser JS
	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)
	body := []byte(bodyStr)

	// Sign with the URL path
	signURL := "/chatApi/v2/chat"
	sr, err := signer.Sign(ctx, bodyStr, signURL)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)

	// Build request
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))

	// Set ALL headers exactly as browser would
	req.Header = http.Header{}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("appType", "5")
	req.Header.Set("appVersion", "1.3.9")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("lang", "zh")
	req.Header.Set("nonce", sr.Nonce)
	req.Header.Set("sign", sr.Sign)
	req.Header.Set("timestamp", fmt.Sprintf("%d", sr.Timestamp))
	req.Header.Set("token", "")
	req.Header.Set("version", "v2")

	// Log all headers
	for k, v := range req.Header {
		t.Logf("  Header: %s = %s", k, v)
	}

	// Use a transport that forces HTTP/2 to ensure lowercase headers
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
	}
	httpClient := &http.Client{Transport: transport}

	start := time.Now()
	resp, err := httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("FAILED after %v: %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v, status: %s", elapsed, resp.Status)
	for k, v := range resp.Header {
		t.Logf("  Resp Header: %s = %s", k, v)
	}

	// Read first chunk with timeout
	buf := make([]byte, 4096)
	n, readErr := resp.Body.Read(buf)
	if n > 0 {
		t.Logf("Body (%d bytes): %s", n, string(buf[:n]))
	}
	if readErr != nil {
		t.Logf("Read error: %v", readErr)
	}
}

// TestWasmSignTCPDebug does a raw TCP-level request to see exactly what the server sends back.
func TestWasmSignTCPDebug(t *testing.T) {
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

	signer := NewWasmSigner(cacheDir)
	defer signer.Close(ctx)
	if err := signer.Init(ctx); err != nil {
		t.Fatalf("Init signer: %v", err)
	}

	cookie := auth.GetCookie()

	convID, err := client.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	t.Logf("ConversationID: %s", convID)
	defer client.DeleteSession(context.Background(), convID)

	// Use exact same key order as browser JS
	bodyStr := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)
	body := []byte(bodyStr)

	sr, err := signer.Sign(ctx, bodyStr, "/chatApi/v2/chat")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)
	t.Logf("body=%s", bodyStr)
	t.Logf("signURL=/chatApi/v2/chat")

	// Build raw HTTP request manually to avoid Go's header canonicalization
	tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	req.Header = http.Header{}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("sign", sr.Sign)
	req.Header.Set("nonce", sr.Nonce)
	req.Header.Set("timestamp", fmt.Sprintf("%d", sr.Timestamp))
	req.Header.Set("appType", "5")
	req.Header.Set("appVersion", "1.3.9")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("lang", "zh")
	req.Header.Set("version", "v2")

	// Use a transport with response header timeout to distinguish
	// "no response at all" from "response headers received but no body"
	transport := &http.Transport{
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          make(map[string]func(authority string, c *tls.Conn) http.RoundTripper), // disable HTTP/2
	}
	httpClient := &http.Client{Transport: transport}

	start := time.Now()
	resp, err := httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("FAILED after %v: %v", elapsed, err)
		// Now try WITHOUT signing to compare
		t.Log("--- Trying without signing for comparison ---")
		req2, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
			dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
		req2.Header = http.Header{}
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Cookie", cookie)
		req2.Header.Set("Origin", "https://ai.dangbei.com")
		transport2 := &http.Transport{ResponseHeaderTimeout: 10 * time.Second}
		resp2, err2 := (&http.Client{Transport: transport2}).Do(req2)
		if err2 != nil {
			t.Logf("Without signing also failed: %v", err2)
		} else {
			defer resp2.Body.Close()
			t.Logf("Without signing: status=%s", resp2.Status)
			buf2, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024))
			t.Logf("Without signing body: %s", string(buf2))
		}

		// Try a simple GET to the same domain to verify connectivity
		t.Log("--- Trying simple POST to v1/create for comparison ---")
		req3, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
			dangbeiAPIBase+"/conversationApi/v1/create", bytes.NewReader([]byte("{}")))
		req3.Header = http.Header{}
		req3.Header.Set("Content-Type", "application/json")
		req3.Header.Set("Cookie", cookie)
		transport3 := &http.Transport{ResponseHeaderTimeout: 10 * time.Second}
		resp3, err3 := (&http.Client{Transport: transport3}).Do(req3)
		if err3 != nil {
			t.Logf("v1/create also failed: %v", err3)
		} else {
			defer resp3.Body.Close()
			t.Logf("v1/create: status=%s", resp3.Status)
			buf3, _ := io.ReadAll(io.LimitReader(resp3.Body, 1024))
			t.Logf("v1/create body: %s", string(buf3))
		}
		return
	}
	defer resp.Body.Close()

	t.Logf("Response in %v, status: %s", elapsed, resp.Status)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(buf) > 0 {
		t.Logf("Body: %s", string(buf[:min(len(buf), 500)]))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestV2ChatWithV1MD5 tests v2/chat with v1 MD5 signing to confirm the endpoint behavior.
func TestV2ChatWithV1MD5(t *testing.T) {
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
	bodyStr := string(body)

	// Use v1 MD5 signing
	timestamp := time.Now().Unix()
	nonce := generateNonce(21)
	sign := v1Sign(timestamp, bodyStr, nonce)
	t.Logf("v1 sign=%s nonce=%s ts=%d", sign, nonce, timestamp)

	tctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", bytes.NewReader(body))
	req.Header = http.Header{}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("sign", sign)
	req.Header.Set("nonce", nonce)
	req.Header.Set("timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("appType", "5")
	req.Header.Set("appVersion", "1.3.9")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("lang", "zh")
	req.Header.Set("version", "v1")

	transport := &http.Transport{ResponseHeaderTimeout: 10 * time.Second}
	start := time.Now()
	resp, err := (&http.Client{Transport: transport}).Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("v1-signed v2/chat FAILED after %v: %v", elapsed, err)
		return
	}
	defer resp.Body.Close()
	t.Logf("v1-signed v2/chat: status=%s in %v", resp.Status, elapsed)
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if len(buf) > 0 {
		t.Logf("Body: %s", string(buf[:min(len(buf), 500)]))
	}
}

// TestFindSignInterceptor downloads the _app JS bundle and searches for the axios/fetch
// interceptor that adds sign/nonce/timestamp headers.
func TestFindSignInterceptor(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)
	t.Logf("Bundle size: %d", len(content))

	// Search for sign-related patterns
	patterns := []string{
		"sign", "nonce", "timestamp",
	}
	for _, p := range patterns {
		// Find all occurrences where it's used as a header key
		idx := 0
		count := 0
		for {
			i := indexOf(content, p, idx)
			if i < 0 || count > 5 {
				break
			}
			// Check context - is this a header assignment?
			start := i - 100
			if start < 0 {
				start = 0
			}
			end := i + 100
			if end > len(content) {
				end = len(content)
			}
			ctx := content[start:end]
			// Only show if it looks like a header assignment
			if containsAny(ctx, "header", "Header", "interceptor", "request", "config") {
				t.Logf("=== %s at %d ===\n%s", p, i, ctx)
				count++
			}
			idx = i + len(p)
		}
	}

	// Also search for the axios interceptor pattern
	interceptorPatterns := []string{
		"interceptors.request",
		"interceptors.response",
		"use(function",
		"No(",
		".No(",
	}
	for _, p := range interceptorPatterns {
		i := indexOf(content, p, 0)
		if i >= 0 {
			start := i - 300
			if start < 0 {
				start = 0
			}
			end := i + 500
			if end > len(content) {
				end = len(content)
			}
			t.Logf("=== %s at %d ===\n%s", p, i, content[start:end])
		}
	}
}

func indexOf(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}
	i := strings.Index(s[start:], substr)
	if i < 0 {
		return -1
	}
	return start + i
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestExtractFullInterceptor extracts the complete request interceptor code.
func TestExtractFullInterceptor(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// The interceptor starts at "b.S.interceptors.request.use"
	idx := strings.Index(content, "b.S.interceptors.request.use")
	if idx < 0 {
		t.Fatal("interceptor not found")
	}

	// Extract a large chunk
	end := idx + 3000
	if end > len(content) {
		end = len(content)
	}
	t.Logf("=== Full interceptor (from %d) ===\n%s", idx, content[idx:end])
}

// TestExtractBodyFunction extracts the O(e,t) function that gets the body string for signing.
func TestExtractBodyFunction(t *testing.T) {
	requireIntegrationTest(t)
	resp, err := http.Get("https://ai.dangbei.com/_next/static/chunks/pages/_app-3da91045335ded21.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	content := string(body)

	// The interceptor is around offset 1877680. O(e,t) is defined before it.
	// Search backwards from the interceptor for function definitions
	interceptorIdx := strings.Index(content, "b.S.interceptors.request.use")
	if interceptorIdx < 0 {
		t.Fatal("interceptor not found")
	}

	// Look at the code before the interceptor (where O, S, k, etc. are defined)
	start := interceptorIdx - 2000
	if start < 0 {
		start = 0
	}
	t.Logf("=== Code before interceptor ===\n%s", content[start:interceptorIdx])
}

// TestV2ChatHTTP1Only tests v2/chat forcing HTTP/1.1 only (no HTTP/2).
func TestV2ChatHTTP1Only(t *testing.T) {
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

	// Simple test: POST to v2/chat WITHOUT signing, forcing HTTP/1.1
	body := `{"stream":true,"botCode":"AI_SEARCH","conversationId":"test","question":"hi","agentId":""}`

	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(tctx, http.MethodPost,
		dangbeiAPIBase+"/chatApi/v2/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://ai.dangbei.com")

	// Force HTTP/1.1 by setting TLS config to NOT advertise h2
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"}, // Only offer HTTP/1.1
		},
		ResponseHeaderTimeout: 8 * time.Second,
	}
	httpClient := &http.Client{Transport: transport}

	start := time.Now()
	resp, err := httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Logf("HTTP/1.1 only FAILED after %v: %v", elapsed, err)
		return
	}
	defer resp.Body.Close()

	t.Logf("HTTP/1.1 only: status=%s proto=%s in %v", resp.Status, resp.Proto, elapsed)
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if len(buf) > 0 {
		t.Logf("Body: %s", string(buf))
	}
}

// TestV2ChatRawTLS does a raw TLS+HTTP/1.1 request to v2/chat to debug the connection.
func TestV2ChatRawTLS(t *testing.T) {
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
	body := `{"stream":true}`

	// Raw TLS connection
	conn, err := tls.Dial("tcp", "ai-api.dangbei.net:443", &tls.Config{
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer conn.Close()

	t.Logf("TLS connected, negotiated proto: %s", conn.ConnectionState().NegotiatedProtocol)

	// Send raw HTTP request
	reqStr := fmt.Sprintf("POST /ai-search/chatApi/v2/chat HTTP/1.1\r\n"+
		"Host: ai-api.dangbei.net\r\n"+
		"Content-Type: application/json\r\n"+
		"Content-Length: %d\r\n"+
		"Cookie: %s\r\n"+
		"Origin: https://ai.dangbei.com\r\n"+
		"Connection: close\r\n"+
		"\r\n"+
		"%s", len(body), cookie, body)

	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read response with timeout
	conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		t.Logf("Read error after writing: %v", err)
	}
	if n > 0 {
		t.Logf("Response (%d bytes):\n%s", n, string(buf[:n]))
	} else {
		t.Log("No response received")
	}
}

// TestV2ChatRawTLSWithSign does a raw TLS request with WASM signing.
func TestV2ChatRawTLSWithSign(t *testing.T) {
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

	body := fmt.Sprintf(
		`{"stream":true,"botCode":"AI_SEARCH","conversationId":"%s","question":"hi","agentId":""}`,
		convID,
	)

	sr, err := signer.Sign(ctx, body, "/chatApi/v2/chat")
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	t.Logf("sign=%s nonce=%s ts=%d", sr.Sign, sr.Nonce, sr.Timestamp)

	// Raw TLS connection
	conn, err := tls.Dial("tcp", "ai-api.dangbei.net:443", &tls.Config{
		NextProtos: []string{"http/1.1"},
	})
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer conn.Close()

	reqStr := fmt.Sprintf("POST /ai-search/chatApi/v2/chat HTTP/1.1\r\n"+
		"Host: ai-api.dangbei.net\r\n"+
		"Content-Type: application/json\r\n"+
		"Content-Length: %d\r\n"+
		"Accept: text/event-stream\r\n"+
		"Cookie: %s\r\n"+
		"Origin: https://ai.dangbei.com\r\n"+
		"Referer: https://ai.dangbei.com/\r\n"+
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36\r\n"+
		"sign: %s\r\n"+
		"nonce: %s\r\n"+
		"timestamp: %d\r\n"+
		"version: v2\r\n"+
		"appType: 5\r\n"+
		"lang: zh\r\n"+
		"client-ver: 1.0.2\r\n"+
		"appVersion: 1.3.9\r\n"+
		"Connection: close\r\n"+
		"\r\n"+
		"%s", len(body), cookie, sr.Sign, sr.Nonce, sr.Timestamp, body)

	t.Logf("Sending %d bytes request", len(reqStr))
	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var allBuf []byte
	buf := make([]byte, 4096)
	for {
		n, readErr := conn.Read(buf)
		if n > 0 {
			allBuf = append(allBuf, buf[:n]...)
		}
		if readErr != nil {
			t.Logf("Read done: %v (total %d bytes)", readErr, len(allBuf))
			break
		}
		// If we got enough, stop
		if len(allBuf) > 8192 {
			break
		}
	}
	if len(allBuf) > 0 {
		show := string(allBuf)
		if len(show) > 2000 {
			show = show[:2000]
		}
		t.Logf("Response:\n%s", show)
	}
}
