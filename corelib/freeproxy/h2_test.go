package freeproxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

// TestH2WithBrowserSign tests v2/chat over HTTP/2 using Go's net/http with
// explicit H2 transport. Browsers use HTTP/2 — maybe the server requires it.
func TestH2WithBrowserSign(t *testing.T) {
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

	// Get sign from Node.js browser-sim
	signJSON := nodeSign(t, body, "/chatApi/v2/chat")
	t.Logf("Sign: %+v", signJSON)

	// Extract token from cookie
	cookie := auth.GetCookie()
	token := extractTokenFromCookie(cookie)
	t.Logf("Token: %s", token)

	// Build HTTP/2 client
	h2Transport := &http2.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h2"},
		},
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			dialer := &tls.Dialer{Config: cfg}
			return dialer.DialContext(ctx, network, addr)
		},
	}
	h2Client := &http.Client{
		Transport: h2Transport,
		Timeout:   20 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://ai-api.dangbei.net/ai-search/chatApi/v2/chat",
		strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	// Set headers matching browser exactly
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Origin", "https://ai.dangbei.com")
	req.Header.Set("Referer", "https://ai.dangbei.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("sign", signJSON.Sign)
	req.Header.Set("nonce", signJSON.Nonce)
	req.Header.Set("timestamp", fmt.Sprintf("%d", signJSON.Timestamp))
	req.Header.Set("apptype", "6") // web
	req.Header.Set("lang", "zh")
	req.Header.Set("client-ver", "1.0.2")
	req.Header.Set("appversion", "1.3.9")
	req.Header.Set("version", "v2")
	req.Header.Set("token", token)
	req.Header.Set("deviceid", "")

	t.Log("Sending v2/chat via HTTP/2...")
	start := time.Now()
	resp, err := h2Client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Do (after %v): %v", elapsed, err)
	}
	defer resp.Body.Close()

	t.Logf("Response in %v: %s (proto=%s)", elapsed, resp.Status, resp.Proto)
	for k, v := range resp.Header {
		t.Logf("  %s: %s", k, v)
	}

	buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(buf) > 0 {
		t.Logf("Body (%d bytes): %s", len(buf), string(buf))
	} else {
		t.Log("Empty body (timeout?)")
	}
}

// TestH2Negotiation checks what protocol the server actually negotiates
func TestH2Negotiation(t *testing.T) {
	requireIntegrationTest(t)
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp", "ai-api.dangbei.net:443",
		&tls.Config{NextProtos: []string{"h2", "http/1.1"}},
	)
	if err != nil {
		t.Fatalf("TLS dial: %v", err)
	}
	defer conn.Close()

	t.Logf("Negotiated protocol: %q", conn.ConnectionState().NegotiatedProtocol)
	t.Logf("TLS version: 0x%04x", conn.ConnectionState().Version)
	t.Logf("Cipher suite: 0x%04x", conn.ConnectionState().CipherSuite)
}

// nodeSign and nodeSignResult are defined in v2_diag_test.go

func extractTokenFromCookie(cookie string) string {
	for _, part := range strings.Split(cookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == "token" {
			return kv[1]
		}
	}
	return ""
}
