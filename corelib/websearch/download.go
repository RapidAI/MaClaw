package websearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sanitizeURLForLog strips query and fragment (which routinely carry
// credentials: presigned S3/R2 URLs, CDN signatures, tokens) before a URL is
// written to the download log.
func sanitizeURLForLog(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// BrowserAuthProvider returns extra request headers (Cookie, User-Agent, ...)
// harvested from a live browser session that already passed the target site's
// anti-bot checks for rawURL. The GUI layer registers it at startup; it stays
// nil in non-GUI contexts, in which case the L2 escalation is skipped.
type BrowserAuthProvider func(ctx context.Context, rawURL string) (map[string]string, error)

var (
	browserAuthMu sync.RWMutex
	browserAuth   BrowserAuthProvider
)

// SetBrowserAuthProvider installs the browser-session auth hook used by the
// anti-bot escalation chain (L2). Safe to call multiple times.
func SetBrowserAuthProvider(p BrowserAuthProvider) {
	browserAuthMu.Lock()
	browserAuth = p
	browserAuthMu.Unlock()
}

func getBrowserAuthProvider() BrowserAuthProvider {
	browserAuthMu.RLock()
	defer browserAuthMu.RUnlock()
	return browserAuth
}

const maxDownloadAttemptsPerLevel = 3

// allowInsecureL2Hook lets tests exercise the L2 cookie escalation against
// plaintext httptest servers. Never set in production.
var allowInsecureL2Hook = false

// downloadLevel is one rung of the anti-bot escalation chain.
type downloadLevel struct {
	headers map[string]string
	utls    bool // dial with the Chrome TLS fingerprint (uTLS) for this level
}

// downloadToFile streams a URL to opts.SavePath with retries and an automatic
// anti-bot escalation chain:
//
//	L0:   default headers (random desktop UA).
//	L1:   full Chrome-like header set, when the response looks like an
//	      anti-bot interstitial (403/503 with Cloudflare markers, or a
//	      challenge page disguised as HTTP 200).
//	L1.5: same Chrome headers plus a Chrome TLS fingerprint (uTLS
//	      HelloChrome_Auto), defeating JA3/JA4-based detection.
//	L2:   cookies + UA exported from the persistent browser session over the
//	      fingerprint client, when the GUI registered a BrowserAuthProvider.
func downloadToFile(ctx context.Context, rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	// The shared client's fixed 30s Timeout covers body streaming and would
	// kill large/slow downloads; the ctx deadline (opts.TimeoutS, up to 600s)
	// is authoritative here, so disable the client-level timeout on a copy.
	c := *client
	c.Timeout = 0

	start := time.Now()
	logURL := sanitizeURLForLog(rawURL)
	dlogf("[download] start url=%q save_path=%q max_bytes=%d timeout=%ds custom_headers=%s",
		logURL, opts.SavePath, opts.MaxBytes, opts.TimeoutS, redactHeaderKeys(opts.Headers))

	levels := []downloadLevel{{}} // L0: default headers, plain client
	var utlsClient *http.Client   // built lazily on first fingerprint level
	clientFor := func(lv downloadLevel) *http.Client {
		if !lv.utls {
			return &c
		}
		if utlsClient == nil {
			// Share the cookie jar and redirect policy with the plain client.
			utlsClient = chromeTLSClient(c.Jar, c.CheckRedirect)
			dlogf("[download] chrome TLS fingerprint client engaged url=%q", logURL)
		}
		return utlsClient
	}

	var lastErr error
	var lastBlocked bool
	var waitHint time.Duration // Retry-After from the previous attempt
	attempt := 0
	for level := 0; level < len(levels); level++ {
		lv := levels[level]
		for try := 0; try < maxDownloadAttemptsPerLevel; try++ {
			attempt++
			if try > 0 {
				backoff := time.Duration(1<<uint(try-1)) * time.Second // 1s, 2s
				if waitHint > backoff {
					backoff = waitHint
				}
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				dlogf("[download] retry attempt=%d level=%d backoff=%s", attempt, level, backoff)
				select {
				case <-ctx.Done():
					dlogf("[download] aborted url=%q: %v", logURL, ctx.Err())
					return nil, ctx.Err()
				case <-time.After(backoff):
				}
			}
			out := performDownload(ctx, rawURL, logURL, opts, clientFor(lv), lv.headers)
			waitHint = out.retryAfter
			if out.err == nil {
				dur := time.Since(start)
				dlogf("[download] success url=%q final=%q bytes=%d dur=%s speed=%s saved=%q attempts=%d level=%d",
					logURL, sanitizeURLForLog(out.result.URL), out.result.BytesRead, dur.Round(time.Millisecond),
					humanSpeed(int64(out.result.BytesRead), dur), out.result.SavedTo, attempt, level)
				return out.result, nil
			}
			lastErr = out.err
			lastBlocked = out.blocked
			dlogf("[download] attempt=%d level=%d try=%d failed: %v (blocked=%t retryable=%t)",
				attempt, level, try+1, out.err, out.blocked, out.retryable)
			if out.blocked {
				levels = appendNextLevel(levels, level, ctx, rawURL)
				break // escalate to next level
			}
			if !out.retryable {
				dlogf("[download] failed url=%q err=%v dur=%s", logURL, out.err, time.Since(start).Round(time.Millisecond))
				return nil, out.err
			}
		}
	}
	dlogf("[download] failed url=%q err=%v dur=%s", logURL, lastErr, time.Since(start).Round(time.Millisecond))
	if lastBlocked {
		return nil, fmt.Errorf("%v（目标站点存在反爬验证。请先用 browser 工具打开 %s 完成人机验证后重试；仍失败则用 download_file(url, save_path, via_browser=true) 让浏览器直接下载）", lastErr, rawURL)
	}
	return nil, lastErr
}

// appendNextLevel prepares the header set for the next escalation level.
func appendNextLevel(levels []downloadLevel, level int, ctx context.Context, rawURL string) []downloadLevel {
	switch level {
	case 0:
		dlogf("[download] escalate to L1: browser-like headers url=%q", sanitizeURLForLog(rawURL))
		return append(levels, downloadLevel{headers: browserLikeHeaders()})
	case 1:
		dlogf("[download] escalate to L1.5: chrome TLS fingerprint url=%q", sanitizeURLForLog(rawURL))
		return append(levels, downloadLevel{headers: browserLikeHeaders(), utls: true})
	case 2:
		if u, err := url.Parse(rawURL); (err != nil || !strings.EqualFold(u.Scheme, "https")) && !allowInsecureL2Hook {
			// Injecting the browser session's cookies (including Secure and
			// HttpOnly ones like cf_clearance) into a plaintext http:// request
			// would leak session credentials; skip L2 for non-https URLs.
			dlogf("[download] L2 skipped: non-https URL, not injecting session cookies")
			return levels
		}
		p := getBrowserAuthProvider()
		if p == nil {
			dlogf("[download] L2 unavailable: no browser auth provider registered")
			return levels
		}
		hdrs, err := p(ctx, rawURL)
		if err != nil || len(hdrs) == 0 {
			dlogf("[download] L2 unavailable: %v", err)
			return levels
		}
		dlogf("[download] escalate to L2: browser session auth injected headers=%s", redactHeaderKeys(hdrs))
		return append(levels, downloadLevel{headers: hdrs, utls: true})
	}
	return levels
}

type downloadOutcome struct {
	result     *FetchResult
	err        error
	blocked    bool          // anti-bot interstitial detected → escalate to next level
	retryable  bool          // transient failure (network error, 429, 5xx) → retry same level
	retryAfter time.Duration // server-provided Retry-After hint
}

// performDownload executes a single HTTP GET and streams the body to disk.
func performDownload(ctx context.Context, rawURL, logURL string, opts *FetchOptions, client *http.Client, extraHeaders map[string]string) *downloadOutcome {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return &downloadOutcome{err: err}
	}
	applyRequestHeaders(req, opts, extraHeaders)

	resp, err := client.Do(req)
	if err != nil {
		return &downloadOutcome{err: fmt.Errorf("HTTP request failed: %w", err), retryable: true}
	}
	defer resp.Body.Close()

	// A 3xx here means the redirect policy gave up (>10 hops →
	// http.ErrUseLastResponse) or the Location header was missing/invalid —
	// the client returned the last response with err == nil. Saving its body
	// would silently produce a corrupt "successful" file (anti-bot clearance
	// redirect loops hit exactly this path), so treat it as a failure.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		blocked := bodyContainsAntiBotMarker(peek)
		dlogf("[download] http_error status=%d redirect-not-followed blocked=%t", resp.StatusCode, blocked)
		return &downloadOutcome{
			err:     fmt.Errorf("HTTP %d: redirect not followed (loop or missing Location)", resp.StatusCode),
			blocked: blocked,
		}
	}

	if resp.StatusCode >= 400 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		blocked := looksLikeAntiBot(resp.StatusCode, resp.Header, peek)
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		var retryAfter time.Duration
		if retryable {
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
		dlogf("[download] http_error status=%d server=%q blocked=%t", resp.StatusCode, resp.Header.Get("Server"), blocked)
		return &downloadOutcome{
			err:        fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status),
			blocked:    blocked,
			retryable:  retryable,
			retryAfter: retryAfter,
		}
	}

	dir := filepath.Dir(opts.SavePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return &downloadOutcome{err: fmt.Errorf("create directory failed: %w", err)}
	}
	f, err := os.Create(opts.SavePath)
	if err != nil {
		return &downloadOutcome{err: fmt.Errorf("create file failed: %w", err)}
	}
	pw := &progressWriter{w: f, url: logURL, total: resp.ContentLength, lastLog: time.Now()}
	// Read one byte past MaxBytes so an oversized body is detected as an
	// error instead of being silently truncated into a corrupt file.
	n, copyErr := io.CopyBuffer(pw, io.LimitReader(resp.Body, opts.MaxBytes+1), make([]byte, 64*1024))
	closeErr := f.Close()
	if copyErr == nil && n > opts.MaxBytes {
		_ = os.Remove(opts.SavePath)
		return &downloadOutcome{err: fmt.Errorf("file exceeds max_bytes limit (%d bytes)", opts.MaxBytes)}
	}
	if copyErr != nil {
		_ = os.Remove(opts.SavePath)
		return &downloadOutcome{err: fmt.Errorf("download interrupted after %d bytes: %w", n, copyErr), retryable: true}
	}
	if closeErr != nil {
		_ = os.Remove(opts.SavePath)
		return &downloadOutcome{err: fmt.Errorf("write file failed: %w", closeErr)}
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	// Some WAFs answer with HTTP 200 + an HTML challenge page instead of a
	// 403. Without this check the challenge HTML would be saved as a
	// "successful" download (a corrupt fake file). Detect it, drop the file,
	// and treat it as blocked so the escalation chain continues.
	if savedFileLooksLikeChallenge(opts.SavePath, resp.Header.Get("Content-Type")) {
		_ = os.Remove(opts.SavePath)
		dlogf("[download] http_error status=200 challenge page disguised as file url=%q", logURL)
		return &downloadOutcome{
			err:     fmt.Errorf("HTTP 200 but body is an anti-bot challenge page, not the requested file"),
			blocked: true,
		}
	}

	return &downloadOutcome{result: &FetchResult{
		URL:         finalURL,
		ContentType: resp.Header.Get("Content-Type"),
		BytesRead:   int(n),
		SavedTo:     opts.SavePath,
		Content:     fmt.Sprintf("文件已保存到 %s (%d 字节)", opts.SavePath, n),
	}}
}

// applyRequestHeaders sets the default headers, then lets escalation-level
// headers override them, and caller-supplied headers (FetchOptions.Headers)
// win last — an explicit `cookie=` from the caller must not be clobbered by
// auto-injected browser-session cookies.
func applyRequestHeaders(req *http.Request, opts *FetchOptions, extra map[string]string) {
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7,ja;q=0.6")
	req.Header.Set("Accept-Encoding", "identity") // avoid compressed responses for simplicity
	for k, v := range extra {
		if strings.TrimSpace(k) != "" {
			req.Header.Set(k, v)
		}
	}
	for k, v := range opts.Headers {
		if strings.TrimSpace(k) != "" {
			req.Header.Set(k, v)
		}
	}
}

// browserLikeHeaders mimics a real Chrome navigation request closely enough
// to pass header-based bot filtering (L1).
func browserLikeHeaders() map[string]string {
	return map[string]string{
		"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":           "zh-CN,zh;q=0.9,en;q=0.8",
		"sec-ch-ua":                 `"Chromium";v="131", "Not_A Brand";v="24"`,
		"sec-ch-ua-mobile":          "?0",
		"sec-ch-ua-platform":        `"Windows"`,
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
	}
}

// antiBotBodyMarkers are lowercase substrings found in anti-bot challenge /
// interstitial pages (Cloudflare and similar WAFs).
var antiBotBodyMarkers = []string{
	"just a moment", "cf-chl", "challenge-platform", "cf_clearance",
	"attention required! | cloudflare", "verify you are human",
}

// looksLikeAntiBot reports whether a failed response is an anti-bot
// interstitial rather than a genuine error.
func looksLikeAntiBot(status int, h http.Header, bodyPeek []byte) bool {
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable {
		return false
	}
	if strings.Contains(strings.ToLower(h.Get("Server")), "cloudflare") {
		return true
	}
	if h.Get("cf-mitigated") != "" {
		return true
	}
	if bodyContainsAntiBotMarker(bodyPeek) {
		return true
	}
	// A bare 403 is often plain header-based bot filtering; escalating to
	// browser-like headers is cheap and harmless.
	return status == http.StatusForbidden
}

// savedFileLooksLikeChallenge reports whether a just-saved download is
// actually an HTML anti-bot challenge page (WAFs sometimes answer 200).
// Only inspected when the response Content-Type is HTML — a real file
// download with a binary type is never touched.
func savedFileLooksLikeChallenge(path, contentType string) bool {
	if !strings.Contains(strings.ToLower(contentType), "html") {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	return bodyContainsAntiBotMarker(buf[:n])
}

func bodyContainsAntiBotMarker(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	s := strings.ToLower(string(body))
	for _, marker := range antiBotBodyMarkers {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// progressWriter counts streamed bytes and logs progress periodically.
type progressWriter struct {
	w       io.Writer
	url     string
	n       int64
	total   int64
	lastN   int64
	lastLog time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.n-p.lastN >= 8<<20 || time.Since(p.lastLog) >= 5*time.Second {
		dlogf("[download] progress url=%q received=%d total=%d", p.url, p.n, p.total)
		p.lastN = p.n
		p.lastLog = time.Now()
	}
	return n, err
}

func humanSpeed(bytes int64, dur time.Duration) string {
	if dur <= 0 {
		return "-"
	}
	bps := float64(bytes) / dur.Seconds()
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.1f KB/s", bps/(1<<10))
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

// parseRetryAfter parses a Retry-After header (seconds or HTTP date).
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
