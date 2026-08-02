package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// sanitizeDLURL strips query/fragment (which routinely carry credentials:
// presigned URLs, CDN signatures, tokens) before a URL hits the download log.
func sanitizeDLURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// DownloadLogf is an optional progress-log sink. The GUI wires it to the
// shared download log (~/.maclaw/logs/download.log) at startup; it stays nil
// in other contexts, in which case logging is skipped.
var DownloadLogf func(format string, args ...interface{})

func downloadLogf(format string, args ...interface{}) {
	if DownloadLogf != nil {
		DownloadLogf(format, args...)
	}
}

// BrowserDownloadResult describes a finished browser-side download.
type BrowserDownloadResult struct {
	SavedTo  string
	FileName string
	Bytes    int64
	FromURL  string
}

// DownloadViaBrowser downloads rawURL through the managed browser itself.
// It solves the cases plain HTTP cannot: the request runs with the browser's
// cookies, TLS fingerprint and JS environment, and inline-rendered files
// (e.g. PDFs that Chrome would open in the viewer) are forced into downloads
// via Fetch response interception plus Browser.setDownloadBehavior.
//
// Flow:
//  1. connect the browser-level CDP endpoint of the live agent session
//     (or the managed persistent browser, launching it if needed),
//  2. Browser.setDownloadBehavior{allow, downloadPath, eventsEnabled},
//  3. open a tab and navigate to rawURL; a Fetch interception at the
//     response stage rewrites non-HTML document responses to
//     `Content-Disposition: attachment` so the browser downloads them,
//  4. wait for Browser.downloadProgress state=completed, move the file to
//     destPath, restore the default download behavior.
func DownloadViaBrowser(ctx context.Context, rawURL, destPath string, timeoutSec int) (*BrowserDownloadResult, error) {
	addr := ""
	if sess := mostRecentLiveAgentSession(); sess != nil {
		addr = sess.Addr
	}
	if addr == "" {
		var err error
		addr, err = DiscoverOrLaunchPersistent()
		if err != nil {
			return nil, fmt.Errorf("no usable browser: %w", err)
		}
	}
	return downloadViaBrowserAt(ctx, addr, rawURL, destPath, timeoutSec)
}

// browserDownloadMu serializes browser-side downloads: Browser.setDownloadBehavior
// is browser-wide state, so two concurrent downloads would clobber each
// other's downloadPath and the deferred reset.
var browserDownloadMu sync.Mutex

// downloadViaBrowserAt is DownloadViaBrowser against an explicit CDP HTTP
// address (e.g. http://127.0.0.1:9222). Separated for testing.
func downloadViaBrowserAt(ctx context.Context, cdpAddr, rawURL, destPath string, timeoutSec int) (*BrowserDownloadResult, error) {
	browserDownloadMu.Lock()
	defer browserDownloadMu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	if strings.TrimSpace(rawURL) == "" {
		return nil, fmt.Errorf("empty url")
	}
	if strings.TrimSpace(destPath) == "" {
		return nil, fmt.Errorf("empty dest path")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}

	downloadLogf("[browser-download] start url=%q dest=%q cdp=%s timeout=%ds", sanitizeDLURL(rawURL), destPath, cdpAddr, timeoutSec)
	start := time.Now()

	bwsURL, err := browserWebSocketURL(cdpAddr)
	if err != nil {
		return nil, err
	}
	bws, err := ConnectCDP(bwsURL)
	if err != nil {
		return nil, fmt.Errorf("connect browser endpoint: %w", err)
	}
	defer bws.Close()

	// Temp download dir on the same volume as dest for an atomic-ish rename.
	tmpDir, err := os.MkdirTemp(filepath.Dir(destPath), ".maclaw-dl-*")
	if err != nil {
		return nil, fmt.Errorf("create temp download dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if _, err := bws.Send("Browser.setDownloadBehavior", map[string]interface{}{
		"behavior":      "allow",
		"downloadPath":  tmpDir,
		"eventsEnabled": true,
	}, DefaultCmdTimeout); err != nil {
		return nil, fmt.Errorf("Browser.setDownloadBehavior: %w", err)
	}
	defer func() {
		// Restore default behavior so the user's normal downloads are unaffected.
		_, _ = bws.Send("Browser.setDownloadBehavior", map[string]interface{}{"behavior": "default"}, 5*time.Second)
	}()

	created, err := bws.Send("Target.createTarget", map[string]interface{}{"url": "about:blank"}, DefaultCmdTimeout)
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget: %w", err)
	}
	var tgt struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(created, &tgt); err != nil || tgt.TargetID == "" {
		return nil, fmt.Errorf("parse createTarget result: %v body_len=%d", err, len(created))
	}
	defer func() {
		_, _ = bws.Send("Target.closeTarget", map[string]interface{}{"targetId": tgt.TargetID}, 5*time.Second)
	}()

	pageWS, err := pageWSURLForTarget(cdpAddr, tgt.TargetID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	pws, err := ConnectCDP(pageWS)
	if err != nil {
		return nil, fmt.Errorf("connect page target: %w", err)
	}
	defer pws.Close()

	if _, err := pws.Send("Page.enable", nil, DefaultCmdTimeout); err != nil {
		return nil, fmt.Errorf("Page.enable: %w", err)
	}
	if _, err := pws.Send("Fetch.enable", map[string]interface{}{
		"patterns": []map[string]interface{}{{"urlPattern": "*", "requestStage": "Response"}},
	}, DefaultCmdTimeout); err != nil {
		return nil, fmt.Errorf("Fetch.enable: %w", err)
	}

	// Respond to Fetch.requestPaused events until the page client closes.
	go serveFetchInterception(ctx, pws, rawURL)

	navRaw, err := pws.Send("Page.navigate", map[string]interface{}{"url": rawURL}, DefaultCmdTimeout)
	if err != nil {
		return nil, fmt.Errorf("Page.navigate: %w", err)
	}
	// Navigation failures (DNS, connection refused, and also the happy-path
	// "navigation became a download" net::ERR_ABORTED) come back in the
	// result's errorText field, not as CDP protocol errors.
	var navRes struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(navRaw, &navRes)
	if navRes.ErrorText != "" {
		if !strings.Contains(navRes.ErrorText, "ERR_ABORTED") {
			return nil, fmt.Errorf("Page.navigate failed: %s", navRes.ErrorText)
		}
		downloadLogf("[browser-download] navigation became a download (%s) url=%q", navRes.ErrorText, sanitizeDLURL(rawURL))
	}

	fileName, written, err := waitBrowserDownload(ctx, bws, rawURL, tmpDir)
	if err != nil {
		downloadLogf("[browser-download] failed url=%q err=%v dur=%s", sanitizeDLURL(rawURL), err, time.Since(start).Round(time.Millisecond))
		return nil, err
	}

	src := filepath.Join(tmpDir, fileName)
	if _, err := os.Stat(src); err != nil {
		return nil, fmt.Errorf("download reported complete but file missing: %s (%w)", fileName, err)
	}
	if err := moveFile(src, destPath); err != nil {
		return nil, fmt.Errorf("move downloaded file: %w", err)
	}
	st, _ := os.Stat(destPath)
	var size int64
	if st != nil {
		size = st.Size()
	} else {
		size = written
	}
	downloadLogf("[browser-download] success url=%q file=%q bytes=%d dur=%s saved=%q",
		sanitizeDLURL(rawURL), fileName, size, time.Since(start).Round(time.Millisecond), destPath)
	return &BrowserDownloadResult{
		SavedTo:  destPath,
		FileName: fileName,
		Bytes:    size,
		FromURL:  rawURL,
	}, nil
}

// browserWebSocketURL resolves the browser-level CDP websocket from /json/version.
func browserWebSocketURL(cdpHTTP string) (string, error) {
	return browserWebSocketURLCtx(context.Background(), cdpHTTP)
}

func browserWebSocketURLCtx(ctx context.Context, cdpHTTP string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(cdpHTTP, "/")+"/json/version", nil)
	if err != nil {
		return "", fmt.Errorf("query /json/version: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query /json/version: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read /json/version: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/json/version: unexpected HTTP %s body_len=%d", resp.Status, len(body))
	}
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("parse /json/version: %v body_len=%d", err, len(body))
	}
	return v.WebSocketDebuggerURL, nil
}

// pageWSURLForTarget polls /json until the page target appears and returns
// its websocket URL.
func pageWSURLForTarget(cdpHTTP, targetID string, timeout time.Duration) (string, error) {
	return pageWSURLForTargetCtx(context.Background(), cdpHTTP, targetID, timeout)
}

func pageWSURLForTargetCtx(ctx context.Context, cdpHTTP, targetID string, timeout time.Duration) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for {
		targets, err := DiscoverTargetsContext(pollCtx, cdpHTTP)
		lastErr = err
		if err == nil {
			for _, t := range targets {
				if t.ID == targetID && t.WebSocketDebugURL != "" {
					return t.WebSocketDebugURL, nil
				}
			}
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-pollCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return "", fmt.Errorf("page target %s not found in /json within %s (last discovery err: %v): %w", targetID, timeout, lastErr, pollCtx.Err())
		case <-timer.C:
		}
	}
}

// cdpHeader is one entry of CDP's header name/value arrays.
type cdpHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// serveFetchInterception answers Fetch.requestPaused events. Responses that
// are documents with a downloadable (non-HTML) content type are rewritten to
// `Content-Disposition: attachment` so Chrome downloads them instead of
// rendering (the inline-PDF case); everything else continues untouched.
//
// Events go through a dedicated drainer: CDPClient.readLoop drops events when
// its 64-slot buffer fills, and a dropped requestPaused leaves the
// corresponding Chrome request hanging forever.
func serveFetchInterception(ctx context.Context, pws *CDPClient, rawURL string) {
	events := make(chan CDPEvent, 256)
	go func() {
		defer close(events)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-pws.Events():
				if !ok {
					return
				}
				select {
				case events <- ev:
				default:
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Method != "Fetch.requestPaused" {
				continue
			}
			var p struct {
				RequestID          string      `json:"requestId"`
				ResourceType       string      `json:"resourceType"`
				ResponseStatusCode int         `json:"responseStatusCode"`
				ResponseHeaders    []cdpHeader `json:"responseHeaders"`
				Request            struct {
					URL string `json:"url"`
				} `json:"request"`
			}
			if err := json.Unmarshal(ev.Params, &p); err != nil || p.RequestID == "" {
				continue
			}
			ct := headerValue(p.ResponseHeaders, "Content-Type")
			cd := headerValue(p.ResponseHeaders, "Content-Disposition")
			if force, filename := shouldForceAttachment(p.ResourceType, p.ResponseStatusCode, ct, cd, p.Request.URL, rawURL); force {
				headers := setCDPHeader(p.ResponseHeaders, "Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
				downloadLogf("[browser-download] force attachment url=%q ct=%q file=%q", sanitizeDLURL(p.Request.URL), ct, filename)
				_, _ = pws.Send("Fetch.continueResponse", map[string]interface{}{
					"requestId":       p.RequestID,
					"responseCode":    p.ResponseStatusCode,
					"responseHeaders": headers,
				}, DefaultCmdTimeout)
			} else {
				_, _ = pws.Send("Fetch.continueRequest", map[string]interface{}{
					"requestId": p.RequestID,
				}, DefaultCmdTimeout)
			}
		}
	}
}

// shouldForceAttachment decides whether a paused response should be rewritten
// to a forced download, and derives the filename to use.
func shouldForceAttachment(resourceType string, status int, contentType, contentDisposition, reqURL, targetURL string) (bool, string) {
	if resourceType != "Document" {
		return false, ""
	}
	if status < 200 || status >= 300 {
		return false, ""
	}
	if strings.Contains(strings.ToLower(contentDisposition), "attachment") {
		return false, "" // already a download
	}
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "html") || strings.Contains(ct, "json") || ct == "" {
		return false, "" // a renderable page or API response, not a file
	}
	return true, downloadFilenameFromCD(contentDisposition, targetURL, reqURL)
}

// downloadFilenameFromCD picks a filename: existing Content-Disposition
// filename if present, else the URL path basename, else download.bin.
func downloadFilenameFromCD(contentDisposition, targetURL, reqURL string) string {
	if fn := filenameFromDisposition(contentDisposition); fn != "" {
		return fn
	}
	for _, raw := range []string{targetURL, reqURL} {
		if u, err := url.Parse(raw); err == nil {
			base := filepath.Base(strings.TrimSuffix(u.Path, "/"))
			if base != "" && base != "." && base != "/" {
				return sanitizeFilename(base)
			}
		}
	}
	return "download.bin"
}

func filenameFromDisposition(cd string) string {
	for _, part := range strings.Split(cd, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			v := strings.Trim(part[len("filename="):], `"`)
			return sanitizeFilename(v)
		}
	}
	return ""
}

// windowsReservedNames cannot be used as file base names on Windows
// (CON, PRN, AUX, NUL, COM1-9, LPT1-9); creating them fails outright.
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// sanitizeFilename strips path components and characters/names unsafe on
// Windows.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r == 0, r == '<', r == '>', r == ':', r == '"', r == '|', r == '?', r == '*':
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	// Windows also rejects trailing dots/spaces in file names.
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." {
		return "download.bin"
	}
	stem := name
	if i := strings.Index(name, "."); i > 0 {
		stem = name[:i]
	}
	if windowsReservedNames[strings.ToLower(stem)] {
		name = "_" + name
	}
	return name
}

func headerValue(headers []cdpHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func setCDPHeader(headers []cdpHeader, name, value string) []cdpHeader {
	out := make([]cdpHeader, 0, len(headers)+1)
	replaced := false
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			if !replaced {
				out = append(out, cdpHeader{Name: h.Name, Value: value})
				replaced = true
			}
			continue
		}
		out = append(out, h)
	}
	if !replaced {
		out = append(out, cdpHeader{Name: name, Value: value})
	}
	return out
}

// waitBrowserDownload listens on the browser-level event stream until the
// download for rawURL completes or fails, returning the on-disk filename.
//
// Events are drained by a dedicated goroutine: CDPClient.readLoop drops
// events once its 64-slot buffer fills, and a busy browser can burst
// downloadProgress events — a dedicated drainer makes losing the "completed"
// event practically impossible. Progress events are matched by guid so a
// concurrent unrelated download (e.g. the user saving something) cannot
// finish this wait early.
func waitBrowserDownload(ctx context.Context, bws *CDPClient, rawURL, tmpDir string) (string, int64, error) {
	events := make(chan CDPEvent, 256)
	go func() {
		defer close(events)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-bws.Events():
				if !ok {
					return
				}
				select {
				case events <- ev:
				default: // local backstop; 256 deep with a dedicated consumer
				}
			}
		}
	}()

	type candidate struct {
		url      string
		filename string
	}
	candidates := map[string]candidate{}
	guid := ""
	var fileName string
	var lastLog time.Time
	for {
		select {
		case <-ctx.Done():
			return "", 0, fmt.Errorf("browser download timed out (the URL may be a web page, not a file): %w", ctx.Err())
		case ev, ok := <-events:
			if !ok {
				return "", 0, fmt.Errorf("browser CDP connection closed during download")
			}
			switch ev.Method {
			case "Browser.downloadWillBegin":
				var p struct {
					GUID              string `json:"guid"`
					URL               string `json:"url"`
					SuggestedFilename string `json:"suggestedFilename"`
				}
				if err := json.Unmarshal(ev.Params, &p); err != nil || p.GUID == "" {
					continue
				}
				candidates[p.GUID] = candidate{url: p.URL, filename: sanitizeFilename(p.SuggestedFilename)}
				downloadLogf("[browser-download] begin guid=%s url=%q suggested=%q", p.GUID, sanitizeDLURL(p.URL), candidates[p.GUID].filename)
				// Prefer the download whose URL is the one we navigated to.
				if guid == "" && p.URL == rawURL {
					guid = p.GUID
					fileName = candidates[p.GUID].filename
				}
			case "Browser.downloadProgress":
				var p struct {
					GUID          string `json:"guid"`
					State         string `json:"state"`
					ReceivedBytes int64  `json:"receivedBytes"`
					TotalBytes    int64  `json:"totalBytes"`
				}
				if err := json.Unmarshal(ev.Params, &p); err != nil {
					continue
				}
				if guid == "" {
					// Redirects may change the final URL; fall back to a
					// candidate on the same host. Unrelated downloads (other
					// hosts, e.g. the user saving something) never match.
					for g, c := range candidates {
						if sameHost(c.url, rawURL) {
							guid = g
							fileName = c.filename
							break
						}
					}
					if guid == "" {
						continue
					}
				}
				if p.GUID != guid {
					continue
				}
				switch p.State {
				case "completed":
					if fileName == "" {
						fileName = findCompletedFile(tmpDir)
					}
					if fileName == "" {
						return "", 0, fmt.Errorf("download completed but filename unknown")
					}
					return fileName, p.ReceivedBytes, nil
				case "canceled":
					return "", 0, fmt.Errorf("browser download canceled")
				default: // inProgress
					if time.Since(lastLog) >= 5*time.Second {
						downloadLogf("[browser-download] progress url=%q received=%d total=%d", sanitizeDLURL(rawURL), p.ReceivedBytes, p.TotalBytes)
						lastLog = time.Now()
					}
				}
			}
		}
	}
}

// sameHost reports whether two URLs share the same host.
func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Host, ub.Host)
}

// findCompletedFile returns the single finished file in tmpDir (fallback when
// the downloadWillBegin event was missed).
func findCompletedFile(tmpDir string) string {
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return ""
	}
	found := ""
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(strings.ToLower(e.Name()), ".crdownload") {
			continue
		}
		if found != "" {
			return "" // ambiguous
		}
		found = e.Name()
	}
	return found
}

// moveFile renames src to dst, falling back to a copy across devices, and
// replacing an existing dst (os.Rename cannot overwrite on Windows).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if _, err := os.Stat(dst); err == nil {
		_ = os.Remove(dst)
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
