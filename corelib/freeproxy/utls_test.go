package freeproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tls "github.com/refraction-networking/utls"
)

// TestUTLSChromeSign tests v2/chat using Chrome's TLS fingerprint via utls.
// If the server does JA3/JA4 fingerprinting, this should bypass it.
func TestUTLSChromeSign(t *testing.T) {
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
	sr := nodeSign(t, body, "/chatApi/v2/chat")

	cookie := auth.GetCookie()
	token := extractTokenFromCookie(cookie)

	// Build raw HTTP request with Chrome TLS fingerprint
	resp, err := utlsRawPost(t, "ai-api.dangbei.net", "/ai-search/chatApi/v2/chat",
		map[string]string{
			"host":         "ai-api.dangbei.net",
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
			"version":      "v2",
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

type utlsResponse struct {
	status  string
	headers map[string]string
	body    io.Reader
	conn    net.Conn
}

// utlsRawPost sends a raw HTTP/1.1 POST using Chrome's TLS fingerprint
func utlsRawPost(t *testing.T, host, path string, headers map[string]string, body []byte, timeout time.Duration) (*utlsResponse, error) {
	t.Helper()

	// TCP connect
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tcpConn, err := dialer.Dial("tcp", host+":443")
	if err != nil {
		return nil, fmt.Errorf("tcp dial: %w", err)
	}

	// uTLS with Chrome fingerprint
	tlsConn := tls.UClient(tcpConn, &tls.Config{
		ServerName: host,
	}, tls.HelloChrome_131)

	if err := tlsConn.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("tls handshake: %w", err)
	}

	t.Logf("uTLS negotiated: proto=%q version=0x%04x cipher=0x%04x",
		tlsConn.ConnectionState().NegotiatedProtocol,
		tlsConn.ConnectionState().Version,
		tlsConn.ConnectionState().CipherSuite)

	if timeout > 0 {
		tlsConn.SetDeadline(time.Now().Add(timeout))
	}

	// Build raw HTTP/1.1 request
	var req strings.Builder
	req.WriteString(fmt.Sprintf("POST %s HTTP/1.1\r\n", path))
	req.WriteString(fmt.Sprintf("Host: %s\r\n", host))
	req.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))

	// Write headers in specific order matching Chrome
	headerOrder := []string{
		"content-type", "accept", "cookie", "origin", "referer",
		"user-agent", "sign", "nonce", "timestamp", "apptype",
		"lang", "client-ver", "appversion", "version", "token", "deviceid",
	}
	for _, k := range headerOrder {
		if v, ok := headers[k]; ok {
			req.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
		}
	}
	req.WriteString("Connection: keep-alive\r\n")
	req.WriteString("\r\n")

	reqBytes := append([]byte(req.String()), body...)
	if _, err := tlsConn.Write(reqBytes); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("write: %w", err)
	}

	// Parse response
	reader := bufio.NewReader(tlsConn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("read status: %w", err)
	}

	resp := &utlsResponse{
		status:  strings.TrimSpace(statusLine),
		headers: make(map[string]string),
		conn:    tlsConn,
	}

	// Parse headers
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			tlsConn.Close()
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		idx := strings.IndexByte(line, ':')
		if idx < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(line[:idx]))
		v := strings.TrimSpace(line[idx+1:])
		resp.headers[k] = v
	}

	// Determine body reader
	if te, ok := resp.headers["transfer-encoding"]; ok && strings.Contains(te, "chunked") {
		resp.body = newSimpleChunkedReader(reader)
	} else if cl, ok := resp.headers["content-length"]; ok {
		n, _ := strconv.ParseInt(cl, 10, 64)
		resp.body = io.LimitReader(reader, n)
	} else {
		resp.body = reader
	}

	return resp, nil
}

type simpleChunkedReader struct {
	r         *bufio.Reader
	remaining int64
	done      bool
}

func newSimpleChunkedReader(r *bufio.Reader) *simpleChunkedReader {
	return &simpleChunkedReader{r: r}
}

func (c *simpleChunkedReader) Read(p []byte) (int, error) {
	if c.done {
		return 0, io.EOF
	}
	for c.remaining == 0 {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimRight(line, "\r\n")
		if idx := strings.IndexByte(line, ';'); idx >= 0 {
			line = line[:idx]
		}
		size, err := strconv.ParseInt(strings.TrimSpace(line), 16, 64)
		if err != nil {
			return 0, fmt.Errorf("bad chunk: %q: %w", line, err)
		}
		if size == 0 {
			c.done = true
			c.r.ReadString('\n')
			return 0, io.EOF
		}
		c.remaining = size
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.r.Read(p)
	c.remaining -= int64(n)
	if c.remaining == 0 {
		c.r.ReadString('\n')
	}
	return n, err
}
