package websearch

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

const (
	publicNetworkTextMaxBytes     = 2 * 1024 * 1024
	publicNetworkDownloadMaxBytes = 25 * 1024 * 1024
	publicNetworkTimeoutSeconds   = 60
)

// FetchOptions configures the Fetch operation.
type FetchOptions struct {
	MaxBytes                   int64               // max response body size (default 2MB text / 100MB save_path; hard caps apply)
	RenderJS                   bool                // attempt headless Chrome rendering
	SavePath                   string              // if set, save raw content to this file path instead of returning text
	SaveRoot                   string              // optional root that SavePath must remain under after resolving existing symlinks
	TimeoutS                   int                 // timeout in seconds (default 30, max 600)
	Offset                     int                 // rune offset for continued reading
	MaxChars                   int                 // max characters to return in Content (0 = full content)
	Headers                    map[string]string   // extra request headers (e.g. Cookie, Referer); applied after defaults
	DisableCookies             bool                // do not send or retain the shared HTTP cookie jar
	DisableBrowserAuthFallback bool                // do not escalate anti-bot failures through the managed browser session
	PublicNetworkOnly          bool                // only use direct public HTTP(S) targets; blocks private IPs and redirect SSRF
	HubDownload                *HubDownloadChannel // when set, HTTP(S) fetch/download goes through MaClaw Hub /searchproxy/download
}

// FetchResult contains the fetched content.
type FetchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
	BytesRead   int    `json:"bytes_read"`
	SavedTo     string `json:"saved_to,omitempty"`
	Truncated   bool   `json:"truncated"`
	TotalChars  int    `json:"total_chars"`
	HasMore     bool   `json:"has_more"`
	NextOffset  int    `json:"next_offset"`
	// ResponseClipped is set when the body continued past MaxBytes. offset
	// cannot read that suffix. The last character page says so.
	ResponseClipped bool `json:"response_clipped,omitempty"`
}

// ResponseClippedNote is the last-page sentence for a clipped body.
// Producers append it on one line. A later preview keeps that line.
const ResponseClippedNote = "The response continued past the fetched prefix. This text is not the whole document."

// continueFetchURLMax keeps a continuation hint inside the tool-result
// budget. A longer request URL is omitted; the caller already has it.
const continueFetchURLMax = 4096

// TrimFetchURL drops surrounding space and line breaks. The desktop cache
// key uses this string, and the continuation hint prints the same string.
func TrimFetchURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "")
	return strings.TrimSpace(raw)
}

// ContinueFetchURL is the caller's URL when a later page has to repeat it.
// The printed page URL can be the address after a redirect. A later call
// that uses only that address reads a different response. The result is
// empty when the printed URL is already the caller's URL, or when the
// caller URL is too long to put on the footer.
func ContinueFetchURL(requested, pageURL string) string {
	requested = TrimFetchURL(requested)
	if requested == "" || requested == strings.TrimSpace(pageURL) || len(requested) > continueFetchURLMax {
		return ""
	}
	return requested
}

// Fetch retrieves a URL and extracts readable text content.
// For HTML pages, it extracts the main text content.
// For other content types, it returns raw text or saves to file.
func Fetch(rawURL string, opts *FetchOptions) (*FetchResult, error) {
	return FetchCtx(context.Background(), rawURL, opts)
}

// FetchWithClient is Fetch with an optional caller-provided HTTP client for
// HTTP(S) requests. It keeps the existing extraction pipeline while allowing
// security-sensitive callers to enforce their own dial and redirect policy.
func FetchWithClient(rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	return FetchWithClientCtx(context.Background(), rawURL, opts, client)
}

func FetchCtx(parent context.Context, rawURL string, opts *FetchOptions) (*FetchResult, error) {
	return FetchWithClientCtx(parent, rawURL, opts, nil)
}

func FetchWithClientCtx(parent context.Context, rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if rawURL == "" {
		return nil, fmt.Errorf("URL is empty")
	}
	rawURL = CanonicalFetchURL(rawURL)
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL == nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if opts == nil {
		opts = &FetchOptions{}
	}
	if opts.MaxBytes <= 0 {
		if strings.TrimSpace(opts.SavePath) != "" {
			// File downloads (download_file / web_fetch save_path): allow large PDFs.
			opts.MaxBytes = 100 * 1024 * 1024
		} else {
			opts.MaxBytes = 2 * 1024 * 1024 // 2MB default for text extraction
		}
	}
	// Text extraction stays capped at 10MB; explicit save-to-file may go up to 100MB
	// (arxiv papers in the wild regularly exceed 10MB).
	maxCap := int64(10 * 1024 * 1024)
	if strings.TrimSpace(opts.SavePath) != "" {
		maxCap = 100 * 1024 * 1024
	}
	if opts.MaxBytes > maxCap {
		opts.MaxBytes = maxCap
	}
	if opts.TimeoutS <= 0 {
		opts.TimeoutS = 30
	}
	if opts.TimeoutS > 600 {
		opts.TimeoutS = 600
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.MaxChars < 0 {
		opts.MaxChars = 0
	}
	if opts.PublicNetworkOnly {
		// Strip credentials and cap limits before URL admission so a reserved
		// host (example.invalid) cannot retain caller headers or oversized
		// limits on the way out, and save-path escape checks still run.
		opts.DisableCookies = true
		opts.DisableBrowserAuthFallback = true
		opts.RenderJS = false
		opts.Headers = nil
		if strings.TrimSpace(opts.SavePath) != "" {
			if opts.MaxBytes > publicNetworkDownloadMaxBytes {
				opts.MaxBytes = publicNetworkDownloadMaxBytes
			}
		} else if opts.MaxBytes > publicNetworkTextMaxBytes {
			opts.MaxBytes = publicNetworkTextMaxBytes
		}
		if opts.TimeoutS > publicNetworkTimeoutSeconds {
			opts.TimeoutS = publicNetworkTimeoutSeconds
		}
		if strings.TrimSpace(opts.SavePath) != "" && strings.TrimSpace(opts.SaveRoot) != "" {
			if err := validateSavePathInsideRoot(opts.SavePath, opts.SaveRoot); err != nil {
				return nil, err
			}
		}
		publicURL, err := validatePublicHTTPURL(rawURL)
		if err != nil {
			return nil, err
		}
		rawURL = publicURL.String()
		client = newPublicHTTPClient(time.Duration(opts.TimeoutS) * time.Second)
	}

	// Dispatch by parsed scheme so FTP:// and ftps:// match after
	// CanonicalFetchURL leaves the original casing in place.
	switch strings.ToLower(parsedURL.Scheme) {
	case "ftps":
		return nil, fmt.Errorf("FTPS (FTP over TLS) is not supported yet, use ftp:// instead")
	case "ftp":
		return fetchFTPCtx(parent, rawURL, opts)
	}

	// render_js only affects text extraction; with save_path the caller wants
	// the raw file, so skip headless Chrome entirely (it cannot save files
	// and is Cloudflare's favorite headless target).
	if opts.RenderJS && strings.TrimSpace(opts.SavePath) != "" {
		dlogf("[download] render_js ignored for save_path download url=%q", rawURL)
		opts.RenderJS = false
	}

	// Try headless Chrome first if requested
	if opts.RenderJS {
		if client != nil {
			return nil, fmt.Errorf("RenderJS cannot be used with a custom HTTP client")
		}
		if result, err := fetchWithChromeCtx(parent, rawURL, opts); err == nil {
			return result, nil
		} else if parent.Err() != nil {
			return nil, parent.Err()
		}
		// Fallback to HTTP if Chrome fails
	}

	return fetchHTTPWithClientCtx(parent, rawURL, opts, client)
}

// validateSavePathInsideRoot rejects save paths that escape root through an
// existing symlink or junction. Group downloads accept an LLM-provided path,
// so lexical containment alone is not sufficient when a workbench contains a
// link to another local directory.
func validateSavePathInsideRoot(savePath, root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	savePath = filepath.Clean(strings.TrimSpace(savePath))
	if root == "" || savePath == "" {
		return fmt.Errorf("public download path and root are required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("prepare public download root: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve public download root: %w", err)
	}
	if info, err := os.Lstat(savePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("public download save_path must not be a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect public download save_path: %w", err)
	}

	ancestor := filepath.Dir(savePath)
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect public download directory: %w", err)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return fmt.Errorf("public download save_path has no existing parent")
		}
		ancestor = parent
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return fmt.Errorf("resolve public download directory: %w", err)
	}
	if !pathIsContained(realAncestor, realRoot) {
		return fmt.Errorf("public download save_path escapes its working directory")
	}
	return nil
}

func pathIsContained(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		root = strings.ToLower(root)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../"))
}

func fetchHTTP(rawURL string, opts *FetchOptions) (*FetchResult, error) {
	return fetchHTTPWithClient(rawURL, opts, nil)
}

func fetchHTTPWithClient(rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	return fetchHTTPWithClientCtx(context.Background(), rawURL, opts, client)
}

func fetchHTTPWithClientCtx(parent context.Context, rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if shouldRouteMaclawHubDownload(opts) {
		if err := prepareHubDownload(opts); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(opts.TimeoutS)*time.Second)
	defer cancel()

	if shouldRouteMaclawHubDownload(opts) {
		return fetchViaMaclawHub(ctx, rawURL, opts)
	}

	if client == nil {
		client = httpClient()
	}

	// File downloads get their own pipeline: retries, streaming to disk,
	// progress logging, and the anti-bot escalation chain.
	if strings.TrimSpace(opts.SavePath) != "" {
		return downloadToFile(ctx, rawURL, opts, client)
	}

	return fetchTextHTTP(ctx, rawURL, opts, client)
}

// fetchTextHTTP is the text-extraction path (no save_path). It runs through
// the same anti-bot escalation chain as file downloads: retries on transient
// errors, browser-like headers, chrome TLS fingerprint, and browser-session
// cookies when a plain fetch is blocked.
func fetchTextHTTP(ctx context.Context, rawURL string, opts *FetchOptions, client *http.Client) (*FetchResult, error) {
	return runFetchChain(ctx, rawURL, "[fetch]", maxDownloadAttemptsPerLevel, opts, client, func(c *http.Client, extra map[string]string) *fetchAttempt {
		return performTextFetch(ctx, rawURL, opts, c, extra, false)
	})
}

// performTextFetch executes a single GET and extracts readable content.
// In rawMode it returns the raw (UTF-8-normalized) body without readable-text
// extraction — used by the HTML search endpoints that parse the page
// structure themselves.
func performTextFetch(ctx context.Context, rawURL string, opts *FetchOptions, client *http.Client, extra map[string]string, rawMode bool) *fetchAttempt {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return &fetchAttempt{err: err}
	}
	applyRequestHeaders(req, opts, extra)

	resp, err := client.Do(req)
	if err != nil {
		return &fetchAttempt{err: fmt.Errorf("HTTP request failed: %w", err), retryable: true}
	}
	defer resp.Body.Close()

	// A leftover 3xx means the redirect policy gave up or Location was
	// missing/invalid; treat it as a failure, never as content.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return &fetchAttempt{
			err:     fmt.Errorf("HTTP %d: redirect not followed (loop or missing Location)", resp.StatusCode),
			blocked: bodyContainsAntiBotMarker(peek),
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
		return &fetchAttempt{
			err:        fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status),
			blocked:    blocked,
			retryable:  retryable,
			retryAfter: retryAfter,
		}
	}

	body, clipped, err := readBoundedBody(resp.Body, opts.MaxBytes)
	if err != nil {
		return &fetchAttempt{err: fmt.Errorf("read body failed: %w", err), retryable: true}
	}

	ct := resp.Header.Get("Content-Type")
	// A WAF challenge page disguised as a successful response: escalate
	// instead of returning the interstitial HTML as if it were the page
	// content.
	if isHTMLContent(ct) && bodyContainsAntiBotMarker(body) {
		return &fetchAttempt{
			err:     fmt.Errorf("HTTP %d but body is an anti-bot challenge page", resp.StatusCode),
			blocked: true,
		}
	}

	attempt := completeTextFetch(rawURL, opts, resp, body, rawMode)
	if attempt != nil && attempt.result != nil {
		attempt.result.ResponseClipped = clipped
	}
	return attempt
}

// readBoundedBody reads at most max bytes. clipped is true when the reader
// still had another byte. A cut that splits a UTF-8 sequence drops that
// incomplete tail so a valid document stays valid; a non-UTF-8 body loses
// at most the last three bytes.
func readBoundedBody(r io.Reader, max int64) ([]byte, bool, error) {
	if max < 0 {
		max = 0
	}
	body, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) <= max {
		return body, false, nil
	}
	return dropPartialTrailingRune(body[:max]), true, nil
}

func dropPartialTrailingRune(body []byte) []byte {
	if len(body) == 0 || utf8.Valid(body) {
		return body
	}
	start := len(body) - 1
	for start > 0 && start > len(body)-utf8.UTFMax && !utf8.RuneStart(body[start]) {
		start--
	}
	if !utf8.FullRune(body[start:]) {
		return body[:start]
	}
	return body
}

func completeTextFetch(rawURL string, opts *FetchOptions, resp *http.Response, body []byte, rawMode bool) *fetchAttempt {
	ct := resp.Header.Get("Content-Type")
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	result := &FetchResult{
		URL:         finalURL,
		ContentType: ct,
		BytesRead:   len(body),
	}

	// Detect and convert encoding
	body = ensureUTF8(body, ct)

	if rawMode {
		result.Content = string(body)
		applyContentWindow(result, opts.Offset, opts.MaxChars)
		return &fetchAttempt{result: result}
	}

	// Extract content based on content type
	if isHTMLContent(ct) {
		title, text := extractReadableContent(body)
		result.Title = title
		result.Content = text
	} else if isTextContent(ct) {
		result.Content = string(body)
	} else {
		result.Content = fmt.Sprintf("[二进制内容: %s, %d 字节。如需下载请使用 save_path 参数]", ct, len(body))
	}

	applyContentWindow(result, opts.Offset, opts.MaxChars)
	return &fetchAttempt{result: result}
}

// ---------------------------------------------------------------------------
// Encoding detection & conversion
// ---------------------------------------------------------------------------

// ensureUTF8 detects the encoding of body and converts to UTF-8 if needed.
func ensureUTF8(body []byte, contentType string) []byte {
	if utf8.Valid(body) {
		return body
	}

	// Try to detect encoding from Content-Type header
	enc := detectEncodingFromCT(contentType)
	if enc == nil {
		// Try to detect from HTML meta charset
		enc = detectEncodingFromMeta(body)
	}
	if enc == nil {
		// Common fallback: try GBK for Chinese content
		enc = simplifiedchinese.GBK
	}

	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(body), enc.NewDecoder()))
	if err != nil {
		return body // return as-is on error
	}
	return decoded
}

func detectEncodingFromCT(ct string) encoding.Encoding {
	_, params, _ := mime.ParseMediaType(ct)
	charset := strings.ToLower(params["charset"])
	return charsetToEncoding(charset)
}

func detectEncodingFromMeta(body []byte) encoding.Encoding {
	// Quick scan for <meta charset="xxx"> or <meta http-equiv="Content-Type" content="...charset=xxx">
	s := strings.ToLower(string(body[:min(4096, len(body))]))

	// <meta charset="gbk">
	if idx := strings.Index(s, "charset="); idx >= 0 {
		rest := s[idx+8:]
		rest = strings.TrimLeft(rest, `"' `)
		end := strings.IndexAny(rest, `"';> `)
		if end > 0 {
			return charsetToEncoding(rest[:end])
		}
	}
	return nil
}

func charsetToEncoding(charset string) encoding.Encoding {
	charset = strings.TrimSpace(strings.ToLower(charset))
	switch charset {
	case "gbk", "gb2312", "gb18030":
		return simplifiedchinese.GBK
	case "big5":
		return traditionalchinese.Big5
	case "euc-jp":
		return japanese.EUCJP
	case "shift_jis", "shift-jis", "sjis":
		return japanese.ShiftJIS
	case "iso-2022-jp":
		return japanese.ISO2022JP
	case "euc-kr":
		return korean.EUCKR
	case "windows-1252", "iso-8859-1", "latin1":
		return charmap.Windows1252
	case "windows-1251":
		return charmap.Windows1251
	case "utf-8", "utf8", "":
		return nil
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// HTML content extraction (Readability-lite)
// ---------------------------------------------------------------------------

// extractReadableContent extracts title and main text from HTML.
func extractReadableContent(body []byte) (title, text string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		// Fallback: strip tags
		return "", stripTags(string(body))
	}

	title = extractTitle(doc)
	text = extractMainText(doc)
	return title, text
}

func extractTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		return getTextContent(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := extractTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// extractMainText tries to find the main content area, falling back to full body text.
func extractMainText(doc *html.Node) string {
	// Try to find <main>, <article>, or common content divs
	if node := findNode(doc, "main"); node != nil {
		return getReadableText(node)
	}
	if node := findNode(doc, "article"); node != nil {
		return getReadableText(node)
	}
	// Try role="main"
	if node := findNodeByAttr(doc, "role", "main"); node != nil {
		return getReadableText(node)
	}
	// Try common content IDs
	for _, id := range []string{"content", "main-content", "article", "post-content", "entry-content"} {
		if node := findNodeByAttr(doc, "id", id); node != nil {
			return getReadableText(node)
		}
	}
	// Fallback: extract from body
	if body := findNode(doc, "body"); body != nil {
		return getReadableText(body)
	}
	return getReadableText(doc)
}

// getReadableText extracts text from a node, skipping script/style/nav/header/footer.
func getReadableText(n *html.Node) string {
	var sb strings.Builder
	walkReadable(n, &sb)
	text := sb.String()
	// Collapse multiple blank lines
	text = collapseBlankLines(text)
	return strings.TrimSpace(text)
}

var skipTags = map[string]bool{
	"script": true, "style": true, "noscript": true,
	"nav": true, "header": true, "footer": true,
	"aside": true, "iframe": true, "svg": true,
	"form": true, "button": true, "input": true,
	"select": true, "textarea": true,
}

var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "tr": true, "blockquote": true, "pre": true,
	"table": true, "ul": true, "ol": true, "dl": true, "dt": true, "dd": true,
	"br": true, "hr": true, "main": true, "figure": true, "figcaption": true,
}

func walkReadable(n *html.Node, sb *strings.Builder) {
	if n.Type == html.ElementNode {
		if skipTags[n.Data] {
			return
		}
		// Check for hidden elements
		for _, a := range n.Attr {
			if a.Key == "style" && (strings.Contains(a.Val, "display:none") || strings.Contains(a.Val, "display: none")) {
				return
			}
			if a.Key == "hidden" {
				return
			}
			if a.Key == "aria-hidden" && a.Val == "true" {
				return
			}
		}
		if blockTags[n.Data] {
			sb.WriteString("\n")
		}
	}
	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkReadable(c, sb)
	}
	if n.Type == html.ElementNode && blockTags[n.Data] {
		sb.WriteString("\n")
	}
}

func getTextContent(n *html.Node) string {
	var sb strings.Builder
	walkText(n, &sb)
	return strings.TrimSpace(sb.String())
}

func walkText(n *html.Node, sb *strings.Builder) {
	if n.Type == html.TextNode {
		sb.WriteString(n.Data)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, sb)
	}
}

func findNode(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findNode(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func findNodeByAttr(n *html.Node, key, val string) *html.Node {
	if n.Type == html.ElementNode {
		for _, a := range n.Attr {
			if a.Key == key && a.Val == val {
				return n
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findNodeByAttr(c, key, val); found != nil {
			return found
		}
	}
	return nil
}

func stripTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			out.WriteRune(' ')
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return out.String()
}

var multiBlankLine = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(s string) string {
	return multiBlankLine.ReplaceAllString(s, "\n\n")
}

func applyContentWindow(result *FetchResult, offset, maxChars int) {
	if result == nil {
		return
	}
	if offset < 0 {
		offset = 0
	}
	// Valid text keeps its original bytes. A rune round-trip copies the
	// whole extract, and the desktop cache stores that extract. Invalid
	// UTF-8 still uses the rune replacement so each bad byte is one U+FFFD.
	page, total, end, ok := sliceValidRuneWindow(result.Content, offset, maxChars)
	if !ok {
		applyContentWindowRunes(result, offset, maxChars)
		return
	}
	result.Content = page
	result.TotalChars = total
	result.Truncated = end < total
	result.HasMore = end < total
	// A finished read ends at total. next_offset stays on that cursor so a
	// completed page is distinguishable from a missing range.
	result.NextOffset = end
}

// sliceValidRuneWindow returns runes [offset, offset+maxChars). maxChars <= 0
// means the rest of the string. The page shares the original bytes. ok is
// false when the string is not valid UTF-8; the caller then uses the rune
// replacement window.
func sliceValidRuneWindow(content string, offset, maxChars int) (page string, total, end int, ok bool) {
	if offset < 0 {
		offset = 0
	}
	i := 0
	prefix := 0
	for i < len(content) && prefix < offset {
		if !advanceValidRune(content, &i) {
			return "", 0, 0, false
		}
		prefix++
	}
	start := i
	taken := 0
	for i < len(content) && (maxChars <= 0 || taken < maxChars) {
		if !advanceValidRune(content, &i) {
			return "", 0, 0, false
		}
		taken++
	}
	pageEnd := i
	tail := 0
	for i < len(content) {
		if !advanceValidRune(content, &i) {
			return "", 0, 0, false
		}
		tail++
	}
	end = prefix + taken
	total = end + tail
	if offset == 0 && end == total {
		return content, total, end, true
	}
	if taken == 0 {
		return "", total, end, true
	}
	return content[start:pageEnd], total, end, true
}

func advanceValidRune(content string, i *int) bool {
	r, size := utf8.DecodeRuneInString(content[*i:])
	if size < 1 || (r == utf8.RuneError && size == 1) {
		return false
	}
	*i += size
	return true
}

func applyContentWindowRunes(result *FetchResult, offset, maxChars int) {
	if result == nil {
		return
	}
	if offset < 0 {
		offset = 0
	}
	runes := []rune(result.Content)
	total := len(runes)
	result.TotalChars = total
	if offset > total {
		offset = total
	}
	end := total
	if maxChars > 0 && offset < total && total-offset > maxChars {
		end = offset + maxChars
	}
	if offset > 0 || end < total {
		result.Content = string(runes[offset:end])
	} else {
		result.Content = string(runes)
	}
	result.Truncated = end < total
	result.HasMore = end < total
	result.NextOffset = end
}

// ClampFetchPage shortens Content to byteBudget on a UTF-8 boundary and
// moves NextOffset to the first rune that did not fit. A 16384-rune page
// is about 64KB of 4-byte text. The model-visible web_fetch result is
// 32768 bytes, and a later head/tail cut would drop the middle while the
// footer still named the end of the uncut window. TotalChars stays as the
// fetch recorded it; a zero total is not invented.
func ClampFetchPage(result *FetchResult, byteBudget int) {
	if result == nil || byteBudget < 1 || len(result.Content) <= byteBudget {
		return
	}
	shown := utf8.RuneCountInString(result.Content)
	start := result.NextOffset - shown
	if start < 0 || result.NextOffset < shown {
		start = 0
	}
	keptBytes := 0
	kept := 0
	for i := 0; i < len(result.Content); {
		_, size := utf8.DecodeRuneInString(result.Content[i:])
		if size <= 0 {
			break
		}
		if keptBytes+size > byteBudget {
			break
		}
		keptBytes += size
		kept++
		i += size
	}
	if kept == 0 {
		// One rune is wider than the budget. Keep it so the cursor still
		// moves; the caller floors the budget above this for normal pages.
		_, size := utf8.DecodeRuneInString(result.Content)
		if size <= 0 || size >= len(result.Content) {
			return
		}
		keptBytes = size
		kept = 1
	}
	if keptBytes >= len(result.Content) {
		return
	}
	result.Content = result.Content[:keptBytes]
	result.NextOffset = start + kept
	if result.TotalChars > 0 {
		more := result.NextOffset < result.TotalChars
		result.Truncated = more
		result.HasMore = more
	}
}

// VisiblePageStart is the rune offset of Content. When NextOffset is the
// exclusive end of this slice, the start is that cursor minus the rune
// count. A page that does not carry that cursor falls back to requested.
func VisiblePageStart(result *FetchResult, requested int) int {
	if requested < 0 {
		requested = 0
	}
	if result == nil {
		return requested
	}
	shown := utf8.RuneCountInString(result.Content)
	if result.NextOffset < shown {
		return requested
	}
	start := result.NextOffset - shown
	if start < 0 {
		return 0
	}
	return start
}

// FitFetchPage shrinks Content until format(result) is within byteBudget.
// A page that already fits is returned unchanged. format must only read
// the result. The returned text is the last format call.
func FitFetchPage(result *FetchResult, byteBudget int, format func(*FetchResult) string) string {
	if result == nil || format == nil {
		return ""
	}
	if byteBudget < 1 {
		return format(result)
	}
	const slack = 64
	const floorBudget = 256
	text := format(result)
	if len(text) <= byteBudget {
		return text
	}
	for i := 0; i < 8 && len(text) > byteBudget; i++ {
		overflow := len(text) - byteBudget
		budget := len(result.Content) - overflow - slack
		if budget < floorBudget {
			budget = floorBudget
		}
		if budget >= len(result.Content) {
			budget = len(result.Content) / 2
			if budget < 1 {
				budget = 1
			}
		}
		before := len(result.Content)
		ClampFetchPage(result, budget)
		if len(result.Content) >= before {
			break
		}
		text = format(result)
	}
	for len(text) > byteBudget && len(result.Content) > 1 {
		budget := len(result.Content) / 2
		if budget < 1 {
			budget = 1
		}
		before := len(result.Content)
		ClampFetchPage(result, budget)
		if len(result.Content) >= before {
			break
		}
		text = format(result)
	}
	return text
}

// ---------------------------------------------------------------------------
// Content type helpers
// ---------------------------------------------------------------------------

func isHTMLContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}

func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "text/") ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "yaml") ||
		strings.Contains(ct, "csv")
}

// ---------------------------------------------------------------------------
// FTP file download
// ---------------------------------------------------------------------------

// fetchFTP downloads a file via FTP protocol using raw TCP commands.
// Supports anonymous login and ftp://user:pass@host/path format.
func fetchFTP(rawURL string, opts *FetchOptions) (*FetchResult, error) {
	return fetchFTPCtx(context.Background(), rawURL, opts)
}

func fetchFTPCtx(parent context.Context, rawURL string, opts *FetchOptions) (*FetchResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid FTP URL: %w", err)
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "21"
	}
	remotePath := u.Path
	if remotePath == "" || remotePath == "/" {
		return nil, fmt.Errorf("FTP URL must specify a file path, directory listing is not supported")
	}

	user := "anonymous"
	pass := "anonymous@"
	if u.User != nil {
		user = u.User.Username()
		if p, ok := u.User.Password(); ok {
			pass = p
		}
	}

	deadline := time.Now().Add(time.Duration(opts.TimeoutS) * time.Second)

	// Connect to FTP server (dial timeout capped to half of total timeout)
	dialTimeout := time.Duration(opts.TimeoutS) * time.Second / 2
	if dialTimeout > 15*time.Second {
		dialTimeout = 15 * time.Second
	}
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(parent, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("FTP connect failed: %w", err)
	}
	defer conn.Close()
	stopClosingControlConn := closeConnOnContextDone(parent, conn)
	defer stopClosingControlConn()
	conn.SetDeadline(deadline)

	reader := bufio.NewReader(conn)

	// Helper: read FTP response line(s)
	readResp := func() (int, string, error) {
		var full strings.Builder
		for range 100 { // safety limit: max 100 lines per response
			line, err := reader.ReadString('\n')
			if err != nil {
				if ctxErr := parent.Err(); ctxErr != nil {
					return 0, full.String(), ctxErr
				}
				return 0, full.String(), err
			}
			full.WriteString(line)
			// FTP multi-line: "123-..." continues, "123 ..." ends
			if len(line) >= 4 && line[3] == ' ' {
				code := 0
				fmt.Sscanf(line[:3], "%d", &code)
				return code, full.String(), nil
			}
		}
		return 0, full.String(), fmt.Errorf("FTP response too many lines")
	}

	// Helper: send FTP command
	sendCmd := func(cmd string) (int, string, error) {
		_, err := fmt.Fprintf(conn, "%s\r\n", cmd)
		if err != nil {
			if ctxErr := parent.Err(); ctxErr != nil {
				return 0, "", ctxErr
			}
			return 0, "", err
		}
		return readResp()
	}

	// Read welcome banner
	if code, _, err := readResp(); err != nil || code/100 != 2 {
		if err != nil {
			return nil, fmt.Errorf("FTP banner read failed: %w", err)
		}
		return nil, fmt.Errorf("FTP server rejected connection (code %d)", code)
	}

	// Login
	if code, _, err := sendCmd("USER " + user); err != nil {
		return nil, fmt.Errorf("FTP USER failed: %w", err)
	} else if code/100 == 3 {
		// Server wants password
		if code, _, err := sendCmd("PASS " + pass); err != nil || code/100 != 2 {
			if err != nil {
				return nil, fmt.Errorf("FTP PASS failed: %w", err)
			}
			return nil, fmt.Errorf("FTP login failed (code %d)", code)
		}
	} else if code/100 != 2 {
		return nil, fmt.Errorf("FTP login failed (code %d)", code)
	}

	// Set binary mode
	if code, _, err := sendCmd("TYPE I"); err != nil || code/100 != 2 {
		if err != nil {
			return nil, fmt.Errorf("FTP TYPE I failed: %w", err)
		}
	}

	// Enter passive mode
	code, resp, err := sendCmd("PASV")
	if err != nil || code != 227 {
		if err != nil {
			return nil, fmt.Errorf("FTP PASV failed: %w", err)
		}
		return nil, fmt.Errorf("FTP PASV failed (code %d)", code)
	}
	dataHost, dataPort, err := parsePASV(resp)
	if err != nil {
		return nil, fmt.Errorf("FTP PASV parse failed: %w", err)
	}

	// Open data connection
	if err := parent.Err(); err != nil {
		return nil, err
	}
	dataDialer := &net.Dialer{Timeout: 10 * time.Second}
	dataConn, err := dataDialer.DialContext(parent, "tcp", net.JoinHostPort(dataHost, dataPort))
	if err != nil {
		return nil, fmt.Errorf("FTP data connect failed: %w", err)
	}
	defer dataConn.Close()
	stopClosingDataConn := closeConnOnContextDone(parent, dataConn)
	defer stopClosingDataConn()
	dataConn.SetDeadline(deadline)

	// Send RETR command
	code, _, err = sendCmd("RETR " + remotePath)
	if err != nil {
		return nil, fmt.Errorf("FTP RETR failed: %w", err)
	}
	if code/100 != 1 && code/100 != 2 {
		return nil, fmt.Errorf("FTP RETR failed (code %d)", code)
	}

	// Read data with size limit. One extra byte distinguishes a finished
	// file from a body the cap cut short.
	body, clipped, err := readBoundedBody(dataConn, opts.MaxBytes)
	dataConn.Close()
	if err != nil {
		if ctxErr := parent.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("FTP data read failed: %w", err)
	}

	// Read RETR completion response
	readResp()

	// Quit
	sendCmd("QUIT")

	result := &FetchResult{
		URL:             rawURL,
		ContentType:     "application/octet-stream (FTP)",
		BytesRead:       len(body),
		ResponseClipped: clipped,
	}

	// Save to file if requested. A clipped body would be a short file
	// reported as the whole download.
	if opts.SavePath != "" {
		if clipped {
			return nil, fmt.Errorf("file exceeds max_bytes limit (%d bytes)", opts.MaxBytes)
		}
		dir := filepath.Dir(opts.SavePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create directory failed: %w", err)
		}
		if err := os.WriteFile(opts.SavePath, body, 0o644); err != nil {
			return nil, fmt.Errorf("write file failed: %w", err)
		}
		result.SavedTo = opts.SavePath
		result.Content = fmt.Sprintf("FTP 文件已保存到 %s (%d 字节)", opts.SavePath, len(body))
		return result, nil
	}

	// Return as text if it looks like text, otherwise prompt to use save_path
	if utf8.Valid(body) && !containsBinaryBytes(body) {
		result.Content = string(body)
	} else {
		result.Content = fmt.Sprintf("[二进制文件: %d 字节。请使用 save_path 参数下载到本地]", len(body))
	}
	applyContentWindow(result, opts.Offset, opts.MaxChars)
	return result, nil
}

// parsePASV extracts host and port from a PASV response like "227 Entering Passive Mode (h1,h2,h3,h4,p1,p2)".
func parsePASV(resp string) (string, string, error) {
	start := strings.Index(resp, "(")
	end := strings.Index(resp, ")")
	if start < 0 || end < 0 || end <= start {
		return "", "", fmt.Errorf("cannot parse PASV response: %s", resp)
	}
	parts := strings.Split(resp[start+1:end], ",")
	if len(parts) != 6 {
		return "", "", fmt.Errorf("PASV response has %d parts, expected 6", len(parts))
	}
	host := parts[0] + "." + parts[1] + "." + parts[2] + "." + parts[3]
	p1, err1 := strconv.Atoi(strings.TrimSpace(parts[4]))
	p2, err2 := strconv.Atoi(strings.TrimSpace(parts[5]))
	if err1 != nil || err2 != nil {
		return "", "", fmt.Errorf("PASV port parse failed: p1=%q p2=%q", parts[4], parts[5])
	}
	port := p1*256 + p2
	if port <= 0 || port > 65535 {
		return "", "", fmt.Errorf("PASV port out of range: %d", port)
	}
	return host, strconv.Itoa(port), nil
}

// containsBinaryBytes checks if data contains null bytes or other binary indicators.
func containsBinaryBytes(data []byte) bool {
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Connection cancellation helpers
// ---------------------------------------------------------------------------

// closeConnOnContextDone closes conn when ctx is cancelled so blocking socket reads wake up.
func closeConnOnContextDone(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil || ctx.Done() == nil {
		return func() {}
	}
	done := make(chan struct{})
	var stopOnce sync.Once
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() {
		stopOnce.Do(func() {
			close(done)
		})
	}
}

// ---------------------------------------------------------------------------
// Headless Chrome rendering (optional)
// ---------------------------------------------------------------------------

// fetchWithChrome uses headless Chrome via CDP to render JS-heavy pages.
func fetchWithChrome(rawURL string, opts *FetchOptions) (*FetchResult, error) {
	return fetchWithChromeCtx(context.Background(), rawURL, opts)
}

func fetchWithChromeCtx(parent context.Context, rawURL string, opts *FetchOptions) (*FetchResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	chromePath := findChrome()
	if chromePath == "" {
		return nil, fmt.Errorf("Chrome not found")
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(opts.TimeoutS)*time.Second)
	defer cancel()

	// Use Chrome's --dump-dom flag to get rendered HTML
	cmd := exec.CommandContext(ctx, chromePath,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--disable-background-networking",
		"--disable-sync",
		"--disable-translate",
		"--mute-audio",
		"--dump-dom",
		rawURL,
	)

	// Limit stdout to opts.MaxBytes to prevent OOM on huge pages
	var stderr bytes.Buffer
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("Chrome stdout pipe: %w", err)
	}
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("Chrome start failed: %w", err)
	}

	body, clipped, readErr := readBoundedBody(stdoutPipe, opts.MaxBytes)
	// Stop reading at the cap, then discard the rest. Leaving those bytes
	// in the pipe fills it, dump-dom blocks on write, and Wait never returns.
	_, _ = io.Copy(io.Discard, stdoutPipe)
	waitErr := cmd.Wait()
	if ctxErr := parent.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if readErr != nil {
		return nil, fmt.Errorf("Chrome read output failed: %w", readErr)
	}
	// The prefix is already in hand. A timeout while discarding the rest
	// still returns that prefix; ResponseClipped says the dump went on.
	if ctxErr := ctx.Err(); ctxErr != nil && !clipped {
		return nil, ctxErr
	}
	if waitErr != nil && !clipped {
		return nil, fmt.Errorf("Chrome exited with error: %w", waitErr)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("Chrome returned empty output")
	}

	title, text := extractReadableContent(body)
	result := &FetchResult{
		URL:             rawURL,
		Title:           title,
		ContentType:     "text/html (Chrome rendered)",
		Content:         text,
		BytesRead:       len(body),
		ResponseClipped: clipped,
	}
	applyContentWindow(result, opts.Offset, opts.MaxChars)
	return result, nil
}

// findChrome locates Chrome/Chromium executable on the system.
func findChrome() string {
	candidates := []string{}
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("LocalAppData"), "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default: // linux
		candidates = []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
			"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
			"/snap/bin/chromium",
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
		// Try PATH lookup for short names
		if !strings.Contains(c, string(os.PathSeparator)) {
			if p, err := exec.LookPath(c); err == nil {
				return p
			}
		}
	}
	return ""
}
