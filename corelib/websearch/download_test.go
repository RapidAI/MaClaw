package websearch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func setupDownloadTestLog(t *testing.T) {
	t.Helper()
	restore := resetDownloadLoggerForTest(t.TempDir())
	t.Cleanup(restore)
}

func TestDownloadRetryOn429(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	const body = "pdf-content-after-retry"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "file.pdf")
	result, err := FetchCtx(context.Background(), srv.URL+"/x", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 attempts (429 then 200), got %d", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestDownloadNoRetryOn404(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "missing.pdf")
	_, err := FetchCtx(context.Background(), srv.URL+"/x", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("404 must not be retried, got %d attempts", got)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("no partial file should remain, stat err=%v", statErr)
	}
}

func TestDownloadEscalatesOnCloudflareChallenge(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	var sawBrowserHeaders atomic.Bool
	const body = "cleared-pdf"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("sec-ch-ua") == "" {
			w.Header().Set("cf-mitigated", "challenge")
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
			return
		}
		sawBrowserHeaders.Store(true)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "paper.pdf")
	result, err := FetchCtx(context.Background(), srv.URL+"/pdf", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !sawBrowserHeaders.Load() {
		t.Fatal("expected L1 escalation with browser-like headers")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 requests (L0 blocked, L1 ok), got %d", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestDownloadL2BrowserSessionCookies(t *testing.T) {
	setupDownloadTestLog(t)
	allowInsecureL2Hook = true // httptest is plaintext http; production keeps the https-only guard
	t.Cleanup(func() { allowInsecureL2Hook = false })
	var sawCookie atomic.Bool
	const body = "cookie-cleared-pdf"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "cf_clearance=abc123") {
			w.Header().Set("Server", "cloudflare")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("Just a moment..."))
			return
		}
		sawCookie.Store(true)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	SetBrowserAuthProvider(func(ctx context.Context, rawURL string) (map[string]string, error) {
		return map[string]string{
			"Cookie":     "cf_clearance=abc123",
			"User-Agent": "TestBrowser/1.0",
		}, nil
	})
	t.Cleanup(func() { SetBrowserAuthProvider(nil) })

	dest := filepath.Join(t.TempDir(), "paper.pdf")
	result, err := FetchCtx(context.Background(), srv.URL+"/pdf", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !sawCookie.Load() {
		t.Fatal("expected L2 escalation to inject browser session cookies")
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestDownloadAntiBotErrorGuidance(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Just a moment..."))
	}))
	t.Cleanup(srv.Close)

	SetBrowserAuthProvider(nil) // no browser session available
	dest := filepath.Join(t.TempDir(), "blocked.pdf")
	_, err := FetchCtx(context.Background(), srv.URL+"/pdf", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "browser") || !strings.Contains(err.Error(), "反爬") {
		t.Fatalf("error should guide agent to browser verification, got: %v", err)
	}
}

func TestDownloadClientTimeoutDisabledForSavePath(t *testing.T) {
	setupDownloadTestLog(t)
	const body = "slow-body"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	// A client-level Timeout covers body streaming; the download path must
	// disable it and rely on the ctx deadline instead.
	slowClient := &http.Client{Timeout: 150 * time.Millisecond}
	dest := filepath.Join(t.TempDir(), "slow.pdf")
	result, err := FetchWithClientCtx(context.Background(), srv.URL+"/slow", &FetchOptions{
		SavePath: dest, TimeoutS: 10, MaxBytes: 1 << 20,
	}, slowClient)
	if err != nil {
		t.Fatalf("FetchWithClientCtx: %v", err)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestDownloadCustomHeadersPassthrough(t *testing.T) {
	setupDownloadTestLog(t)
	var sawReferer atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "https://example.com/list" {
			sawReferer.Store(true)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "f.bin")
	_, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
		Headers: map[string]string{"Referer": "https://example.com/list"},
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !sawReferer.Load() {
		t.Fatal("custom Referer header did not reach the server")
	}
}

func TestDownloadLogWritten(t *testing.T) {
	dir := t.TempDir()
	restore := resetDownloadLoggerForTest(dir)
	t.Cleanup(restore)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "f.bin")
	if _, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}); err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}

	logPath := filepath.Join(dir, "logs", "download.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("download.log not written: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[download] start") || !strings.Contains(content, "[download] success") {
		t.Fatalf("log should contain start/success records, got:\n%s", content)
	}
}

func TestLooksLikeAntiBot(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		server  string
		cf      string
		body    string
		blocked bool
	}{
		{"cf header", 403, "", "challenge", "", true},
		{"cf server", 503, "cloudflare", "", "", true},
		{"cf body", 403, "", "", "<title>Just a moment...</title>", true},
		{"bare 403", 403, "nginx", "", "forbidden", true},
		{"404 not blocked", 404, "cloudflare", "", "", false},
		{"500 not blocked", 500, "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.server != "" {
				h.Set("Server", tc.server)
			}
			if tc.cf != "" {
				h.Set("cf-mitigated", tc.cf)
			}
			if got := looksLikeAntiBot(tc.status, h, []byte(tc.body)); got != tc.blocked {
				t.Fatalf("looksLikeAntiBot = %t, want %t", got, tc.blocked)
			}
		})
	}
}

func TestBodyContainsAntiBotMarkerDoesNotMatchTurnstileCSSCatalog(t *testing.T) {
	// Bing's ordinary result page includes "cf-turnstile-wrapper" in a large
	// CSS class catalog even when no verification widget is active. A bare
	// substring match made every otherwise-valid Bing response look blocked.
	body := []byte(`<html><body><li class="b_algo"><h2><a href="https://example.com">Result</a></h2></li><script>var classes=["cf-turnstile-wrapper"];</script></body></html>`)
	if bodyContainsAntiBotMarker(body) {
		t.Fatal("ordinary Bing result page was misclassified as an anti-bot challenge")
	}
}

func TestBodyContainsAntiBotMarkerMatchesActiveTurnstileWidget(t *testing.T) {
	body := []byte(`<html><body><div class="cf-turnstile" data-sitekey="public-key"></div></body></html>`)
	if !bodyContainsAntiBotMarker(body) {
		t.Fatal("active Turnstile widget was not classified as an anti-bot challenge")
	}
}

func TestDownloadInterruptedStreamRetried(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	const body = "full-body-after-reconnect"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// Hijack and close the connection mid-body to simulate a cut stream.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("server does not support hijacking")
				return
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			fmt.Fprintf(buf, "HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\npartial")
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "stream.bin")
	result, err := FetchCtx(context.Background(), srv.URL+"/s", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected retry after interrupted stream, got %d attempts", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("body mismatch: %q", data)
	}
}

func TestDownloadRejectsOversizedBody(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 4096))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "big.bin")
	_, err := FetchCtx(context.Background(), srv.URL+"/big", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "max_bytes") {
		t.Fatalf("expected max_bytes error, got %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("oversized partial file must be removed, stat err=%v", statErr)
	}
}

func TestApplyRequestHeadersPrecedence(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://x/", nil)
	opts := &FetchOptions{Headers: map[string]string{
		"Cookie":     "user=1",
		"User-Agent": "UserUA/1.0",
	}}
	extra := map[string]string{ // L2 browser-injected auth
		"Cookie":     "browser=1",
		"User-Agent": "BrowserUA/1.0",
		"Referer":    "https://ref/",
	}
	applyRequestHeaders(req, opts, extra)
	if got := req.Header.Get("Cookie"); got != "user=1" {
		t.Fatalf("caller cookie must win over injected: %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != "UserUA/1.0" {
		t.Fatalf("caller UA must win over injected: %q", got)
	}
	if got := req.Header.Get("Referer"); got != "https://ref/" {
		t.Fatalf("level header should apply when caller did not set it: %q", got)
	}
}

func TestDownloadEscalatesOnDisguised200Challenge(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	const body = "real-pdf-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("sec-ch-ua") == "" {
			// WAF answers 200 with an HTML challenge page instead of a 403.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><title>Just a moment...</title><body>cf-chl challenge</body></html>"))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "paper.pdf")
	result, err := FetchCtx(context.Background(), srv.URL+"/pdf", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected L0 disguised-200 then L1 success, got %d requests", got)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("challenge HTML must not be saved as the file: %q", data)
	}
}

func TestDownloadKeepsLegitHTMLFile(t *testing.T) {
	setupDownloadTestLog(t)
	const body = "<html><body><h1>Normal documentation page</h1></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "page.html")
	result, err := FetchCtx(context.Background(), srv.URL+"/page", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	data, _ := os.ReadFile(result.SavedTo)
	if string(data) != body {
		t.Fatalf("legit HTML download must be kept: %q", data)
	}
}

func TestDownloadHonorsRetryAfter(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	const body = "after-rate-limit"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	start := time.Now()
	dest := filepath.Join(t.TempDir(), "f.bin")
	if _, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}); err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("Retry-After: 2 was not honored (elapsed %s)", elapsed)
	}
}

func TestEscalationLevelOrder(t *testing.T) {
	setupDownloadTestLog(t)
	allowInsecureL2Hook = true
	t.Cleanup(func() { allowInsecureL2Hook = false })
	// Server blocks everything (CF markers) until the browser-session cookie
	// arrives: exercises L0 → L1 → L1.5 → L2 in order.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if !strings.Contains(r.Header.Get("Cookie"), "cf_clearance=z") {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	SetBrowserAuthProvider(func(ctx context.Context, rawURL string) (map[string]string, error) {
		return map[string]string{"Cookie": "cf_clearance=z"}, nil
	})
	t.Cleanup(func() { SetBrowserAuthProvider(nil) })

	dest := filepath.Join(t.TempDir(), "f.bin")
	if _, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	}); err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("expected 4 requests (L0,L1,L1.5,L2), got %d", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("3"); d != 3*time.Second {
		t.Fatalf("seconds: %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Fatalf("empty: %v", d)
	}
	if d := parseRetryAfter("-5"); d != 0 {
		t.Fatalf("negative: %v", d)
	}
	if d := parseRetryAfter(time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)); d < time.Second || d > 3*time.Second {
		t.Fatalf("http date: %v", d)
	}
	if d := parseRetryAfter("garbage"); d != 0 {
		t.Fatalf("garbage: %v", d)
	}
}

func TestL2SkippedForPlainHTTP(t *testing.T) {
	setupDownloadTestLog(t)
	var sawCookie atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			sawCookie.Store(true)
		}
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	SetBrowserAuthProvider(func(ctx context.Context, rawURL string) (map[string]string, error) {
		return map[string]string{"Cookie": "session=secret"}, nil
	})
	t.Cleanup(func() { SetBrowserAuthProvider(nil) })

	dest := filepath.Join(t.TempDir(), "f.bin")
	_, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err == nil {
		t.Fatal("expected failure (everything blocked)")
	}
	if sawCookie.Load() {
		t.Fatal("session cookie must never be injected into a plaintext http request")
	}
}

func TestDownloadRedirectLoopNotSavedAsSuccess(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Infinite redirect loop: the client's policy eventually returns the
		// last 3xx response with err == nil.
		w.Header().Set("Location", r.URL.String())
		w.WriteHeader(http.StatusFound)
		_, _ = w.Write([]byte("<html>redirect</html>"))
	}))
	t.Cleanup(srv.Close)

	dest := filepath.Join(t.TempDir(), "f.bin")
	_, err := FetchCtx(context.Background(), srv.URL+"/f", &FetchOptions{
		SavePath: dest, TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err == nil {
		t.Fatal("expected redirect-loop error")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatalf("3xx body must not be saved as a successful download, stat err=%v", statErr)
	}
}

func TestSanitizeURLForLog(t *testing.T) {
	got := sanitizeURLForLog("https://s3.example.com/bucket/file.pdf?X-Amz-Signature=abc&X-Amz-Expires=3600#frag")
	if got != "https://s3.example.com/bucket/file.pdf" {
		t.Fatalf("query/fragment must be stripped: %q", got)
	}
	if got := sanitizeURLForLog("https://example.com/no-query"); got != "https://example.com/no-query" {
		t.Fatalf("no-query url: %q", got)
	}
}

func TestTextFetchClientTimeoutDisabled(t *testing.T) {
	// Same fix as the download path: the shared client's 30s Timeout used to
	// override the tool's timeout parameter for text extraction too.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("slow text content"))
	}))
	t.Cleanup(srv.Close)

	slowClient := &http.Client{Timeout: 150 * time.Millisecond}
	result, err := FetchWithClientCtx(context.Background(), srv.URL+"/t", &FetchOptions{
		TimeoutS: 10, MaxBytes: 1 << 20,
	}, slowClient)
	if err != nil {
		t.Fatalf("FetchWithClientCtx: %v", err)
	}
	if !strings.Contains(result.Content, "slow text content") {
		t.Fatalf("content mismatch: %q", result.Content)
	}
}

// --- Text-path (web_fetch) anti-bot chain ---

func TestTextFetchEscalatesOnCloudflare(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("sec-ch-ua") == "" {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("the real article body"))
	}))
	t.Cleanup(srv.Close)

	result, err := FetchCtx(context.Background(), srv.URL+"/article", &FetchOptions{
		TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !strings.Contains(result.Content, "the real article body") {
		t.Fatalf("content mismatch: %q", result.Content)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected L0 blocked then L1 success, got %d requests", got)
	}
}

func TestTextFetchEscalatesOnDisguised200(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("sec-ch-ua") == "" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><title>Just a moment...</title></html>"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><article><p>real page text</p></article></body></html>"))
	}))
	t.Cleanup(srv.Close)

	result, err := FetchCtx(context.Background(), srv.URL+"/p", &FetchOptions{
		TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !strings.Contains(result.Content, "real page text") {
		t.Fatalf("challenge HTML must not be returned as content: %q", result.Content)
	}
}

func TestTextFetchRetries429(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok after retries"))
	}))
	t.Cleanup(srv.Close)

	result, err := FetchCtx(context.Background(), srv.URL+"/t", &FetchOptions{
		TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("FetchCtx: %v", err)
	}
	if !strings.Contains(result.Content, "ok after retries") {
		t.Fatalf("content mismatch: %q", result.Content)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestTextFetchBlockedErrorGuidance(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Just a moment..."))
	}))
	t.Cleanup(srv.Close)

	SetBrowserAuthProvider(nil)
	_, err := FetchCtx(context.Background(), srv.URL+"/p", &FetchOptions{
		TimeoutS: 30, MaxBytes: 1 << 20,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "反爬") || !strings.Contains(err.Error(), "browser") {
		t.Fatalf("error should guide to browser verification, got: %v", err)
	}
}

// --- Search endpoints through the anti-bot chain ---

func TestSearchBingDirectEscalatesOnCloudflare(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("sec-ch-ua") == "" {
			w.Header().Set("cf-mitigated", "challenge")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><li class="b_algo"><h2><a href="https://example.com/paper">Deep Paper</a></h2><div class="b_caption"><p>great snippet</p></div></li></body></html>`))
	}))
	t.Cleanup(srv.Close)

	orig := defaultBingSearchURL
	defaultBingSearchURL = srv.URL
	t.Cleanup(func() { defaultBingSearchURL = orig })

	results, err := searchBingDirect(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("searchBingDirect: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Deep Paper" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected L0 blocked then L1 success, got %d requests", got)
	}
}

func TestFetchRawHTMLReturnsUnextractedBody(t *testing.T) {
	setupDownloadTestLog(t)
	const body = `<html><body><li class="b_algo"><h2><a href="https://x/">T</a></h2></li>raw <b>markup</b> kept</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	html, err := fetchRawHTMLWithChain(context.Background(), srv.URL+"/s", nil)
	if err != nil {
		t.Fatalf("fetchRawHTMLWithChain: %v", err)
	}
	if html != body {
		t.Fatalf("raw mode must return the unextracted body:\ngot:  %q\nwant: %q", html, body)
	}
}

func TestParseDDGResultsSkipsAdTrackerLinks(t *testing.T) {
	html := `<html><body>` +
		`<a class="result__a" href="https://duckduckgo.com/y.js?ad_domain=codecademy.com&u3=https%3A%2F%2Fwww.codecademy.com">Codecademy Ad</a>` +
		`<a class="result__snippet">ad snippet</a>` +
		`<a class="result__a" href="https://go.dev/">The Go Programming Language</a>` +
		`<a class="result__snippet">go snippet</a>` +
		`</body></html>`
	results := parseDDGResults(html, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 organic result, got %d: %+v", len(results), results)
	}
	if results[0].URL != "https://go.dev/" {
		t.Fatalf("ad tracker link was not filtered: %+v", results[0])
	}
}

func TestParseDDGResultsParsesLiteMarkup(t *testing.T) {
	html := `<html><body>` +
		`<a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F" class="result-link">Go &amp; Programming</a>` +
		`<td class="result-snippet">Official <b>Go</b> documentation.</td>` +
		`<a class="result-link" href="https://example.com/second">Second result</a>` +
		`</body></html>`

	results := parseDDGResults(html, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 Lite results, got %d: %+v", len(results), results)
	}
	if results[0].URL != "https://go.dev/" || results[0].Title != "Go & Programming" {
		t.Fatalf("unexpected first Lite result: %+v", results[0])
	}
	if results[0].Snippet != "Official Go documentation." {
		t.Fatalf("unexpected Lite snippet: %q", results[0].Snippet)
	}
	if results[1].Snippet != "" {
		t.Fatalf("snippet leaked into next Lite result: %q", results[1].Snippet)
	}
}

func TestParseDDGResultsDoesNotUseNextLiteResultSnippet(t *testing.T) {
	html := `<html><body>` +
		`<a class="result-link" href="https://example.com/first">First result</a>` +
		`<a class="result-link" href="https://example.com/second">Second result</a>` +
		`<td class="result-snippet">Second snippet</td>` +
		`</body></html>`

	results := parseDDGResults(html, 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 Lite results, got %d: %+v", len(results), results)
	}
	if results[0].Snippet != "" {
		t.Fatalf("first result borrowed the next result snippet: %q", results[0].Snippet)
	}
	if results[1].Snippet != "Second snippet" {
		t.Fatalf("second Lite snippet mismatch: %q", results[1].Snippet)
	}
}

func TestParseDDGResultsAcceptsMixedCaseLiteMarkup(t *testing.T) {
	html := `<html><body><A HREF="https://go.dev/" CLASS="result-link">Go</A>` +
		`<SPAN CLASS="result-snippet">Official docs</SPAN></body></html>`
	results := parseDDGResults(html, 5)
	if len(results) != 1 || results[0].Snippet != "Official docs" {
		t.Fatalf("mixed-case Lite markup was not parsed: %+v", results)
	}
}

func TestParseDDGResultsDoesNotBorrowNestedLinkHref(t *testing.T) {
	html := `<html><body>` +
		`<a class="result-link"><span><a href="https://example.com/nested">Nested</a></span>Broken result</a>` +
		`<a class="result-link" href="https://go.dev/">Go</a>` +
		`</body></html>`
	results := parseDDGResults(html, 5)
	if len(results) != 1 || results[0].URL != "https://go.dev/" {
		t.Fatalf("nested href was incorrectly used: %+v", results)
	}
}

func TestSearchDuckDuckGoReturnsErrorForUnparseableSuccess(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "" {
			t.Fatal("DuckDuckGo search query was not supplied")
		}
		_, _ = w.Write([]byte(`<html><body>no search results here</body></html>`))
	}))
	t.Cleanup(srv.Close)

	_, err := searchDuckDuckGo(context.Background(), corelib.WebSearchProvider{BaseURL: srv.URL}, "golang", 3)
	if err == nil || !strings.Contains(err.Error(), "no parseable results") {
		t.Fatalf("expected a clear parse error, got %v", err)
	}
}

func TestBaiduCaptchaRedirectTriggersEscalation(t *testing.T) {
	setupDownloadTestLog(t)
	var calls int32
	mux := http.NewServeMux()
	mux.HandleFunc("/s", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Header.Get("sec-ch-ua") == "" {
			// Baidu-style: 302 to a wappass captcha page rather than a 403.
			http.Redirect(w, r, "/static/captcha/tuxing_v2.html", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="result c-container"><h3 class="t"><a href="https://example.com/go">Go Result</a></h3></div></body></html>`))
	})
	mux.HandleFunc("/static/captcha/tuxing_v2.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>百度安全验证</title></head><body>captcha</body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	orig := defaultBaiduSearchURL
	defaultBaiduSearchURL = srv.URL + "/s"
	t.Cleanup(func() { defaultBaiduSearchURL = orig })

	if _, err := searchBaiduDirect(context.Background(), "golang", 3); err != nil {
		t.Fatalf("searchBaiduDirect: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Fatalf("expected captcha redirect to trigger escalation, got %d /s requests", got)
	}
}

func TestParseBaiduResultsCurrentMarkup(t *testing.T) {
	// Mirrors Baidu's current SERP structure (captured from a live page):
	// container carries mu="REAL_URL", title in first <h3>, snippet in the
	// summaryData JSON comment.
	html := `<div class="result c-container xpath-log new-pmd"
			srcid="1599" id="2" tpl="www_index"
			mu="https://www.jianshu.com/p/bb8d36af8807"
			data-op="{'y':'F1FE9'}">
			<div class="cosc-card"><div class="title-wrapper_4oy6O"><h3 class="cosc-title t cos-line-clamp-1 title_4QsBx">
			<a class="cosc-title-a cos-link cosc-title-md " href="http://www.baidu.com/link?url=c7NJxxxx"><span><!--s-text--><em>Golang</em>并发编程: 从原理到实战<!--/s-text--></span></a></h3></div>
			<!--s-data:{"summaryData":{"generalLines":[{"prefixTime":"2025年6月","data":[{"text":"在构建大型并发系统时,**并发编程**(Concurrent Programming)已成为核心技术。"}]},"isSingleLine":false}}-->
			</div>`
	results := parseBaiduResults(html, 5)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %+v", len(results), results)
	}
	r := results[0]
	if r.URL != "https://www.jianshu.com/p/bb8d36af8807" {
		t.Fatalf("mu URL not used: %q", r.URL)
	}
	if !strings.Contains(r.Title, "并发编程") {
		t.Fatalf("title mismatch: %q", r.Title)
	}
	if !strings.Contains(r.Snippet, "并发编程") {
		t.Fatalf("snippet mismatch: %q", r.Snippet)
	}
}

func TestFallbackChainUsesBrowserSearchHook(t *testing.T) {
	setupDownloadTestLog(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	for _, p := range []*string{&defaultBingSearchURL, &defaultBaiduSearchURL, &defaultLegacySearchURL} {
		orig := *p
		*p = srv.URL
		t.Cleanup(func() { *p = orig })
	}
	lastGoodEndpointMu.Lock()
	lastGoodEndpointName = ""
	lastGoodEndpointMu.Unlock()

	SetBrowserSearchProvider(func(ctx context.Context, engineID, query string, maxResults int, _ bool) ([]BrowserSearchHit, error) {
		return []BrowserSearchHit{{Title: "Browser Hit", URL: "https://go.dev/", Snippet: "via browser"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	results, err := searchDirectFallbackChain(context.Background(), "golang", 3, "")
	if err != nil {
		t.Fatalf("searchDirectFallbackChain: %v", err)
	}
	if len(results) != 1 || results[0].Title != "Browser Hit" {
		t.Fatalf("hook results not used: %+v", results)
	}

	// Without the hook the same failures must surface as an error.
	SetBrowserSearchProvider(nil)
	if _, err := searchDirectFallbackChain(context.Background(), "golang", 3, ""); err == nil {
		t.Fatal("expected error when hook is nil and all endpoints fail")
	}
}
