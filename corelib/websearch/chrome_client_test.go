package websearch

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/RapidAI/CodeClaw/corelib/proxyutil"
)

// withInsecureChromeTLS lets the uTLS client talk to httptest's self-signed
// cert servers.
func withInsecureChromeTLS(t *testing.T) {
	t.Helper()
	prev := chromeTLSConfigHook
	chromeTLSConfigHook = func(cfg *utls.Config) { cfg.InsecureSkipVerify = true }
	t.Cleanup(func() { chromeTLSConfigHook = prev })
}

func TestChromeTLSClientDownloadHTTP1(t *testing.T) {
	withInsecureChromeTLS(t)
	setupDownloadTestLog(t)
	protoCh := make(chan string, 1)
	const body = "chrome-fp-h1-body"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protoCh <- r.Proto
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "h1.bin")
	result, err := FetchWithClientCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}, chromeTLSClient(nil, nil))
	if err != nil {
		t.Fatalf("FetchWithClientCtx: %v", err)
	}
	if got := <-protoCh; got != "HTTP/1.1" {
		t.Fatalf("expected HTTP/1.1, got %q", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestChromeTLSClientDownloadHTTP2(t *testing.T) {
	withInsecureChromeTLS(t)
	setupDownloadTestLog(t)
	protoCh := make(chan string, 1)
	const body = "chrome-fp-h2-body"
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protoCh <- r.Proto
		_, _ = w.Write([]byte(body))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "h2.bin")
	result, err := FetchWithClientCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}, chromeTLSClient(nil, nil))
	if err != nil {
		t.Fatalf("FetchWithClientCtx: %v", err)
	}
	if got := <-protoCh; got != "HTTP/2.0" {
		t.Fatalf("expected HTTP/2.0, got %q", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestChromeTLSClientSendsConfiguredHeaders(t *testing.T) {
	withInsecureChromeTLS(t)
	setupDownloadTestLog(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("sec-ch-ua") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "f.bin")
	opts := &FetchOptions{SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20}
	// Call performDownload directly (bypassing the escalation chain, which
	// would otherwise recover from the 403 on its own) to verify header
	// forwarding through the fingerprint client.
	client := chromeTLSClient(nil, nil)
	out := performDownload(context.Background(), srv.URL+"/f", srv.URL+"/f", opts, client, nil)
	if out.err == nil {
		t.Fatal("expected 403 without browser headers")
	}
	out = performDownload(context.Background(), srv.URL+"/f", srv.URL+"/f", opts, client, browserLikeHeaders())
	if out.err != nil {
		t.Fatalf("with browser headers: %v", out.err)
	}
}

func TestConnClosingReadCloserClosesConn(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	body := &connClosingReadCloser{ReadCloser: io.NopCloser(strings.NewReader("x")), conn: c1}
	if err := body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = c1.SetWriteDeadline(time.Now())
	if _, err := c1.Write([]byte("x")); err == nil {
		t.Fatal("underlying conn not closed with body")
	}
}

// TestChromeTLSClientViaCONNECTProxy runs the full path through a fake HTTP
// CONNECT proxy that tunnels to an httptest TLS server.
func TestChromeTLSClientViaCONNECTProxy(t *testing.T) {
	withInsecureChromeTLS(t)
	setupDownloadTestLog(t)

	const body = "proxied-chrome-fp-body"
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(target.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				br := bufio.NewReader(conn)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != "CONNECT" {
					return
				}
				up, err := net.Dial("tcp", req.Host)
				if err != nil {
					return
				}
				defer up.Close()
				_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { _, _ = io.Copy(up, conn) }()
				_, _ = io.Copy(conn, up)
			}(conn)
		}
	}()

	proxyHost, proxyPort, _ := net.SplitHostPort(ln.Addr().String())
	SetProxy(proxyutil.Config{Enabled: true, Protocol: "http", Host: proxyHost, Port: proxyPort})
	t.Cleanup(func() { SetProxy(proxyutil.Config{Enabled: false}) })

	dest := filepath.Join(t.TempDir(), "proxied.bin")
	result, err := FetchWithClientCtx(context.Background(), target.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}, chromeTLSClient(nil, nil))
	if err != nil {
		t.Fatalf("FetchWithClientCtx via proxy: %v", err)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}
