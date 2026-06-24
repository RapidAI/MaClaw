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

// FetchOptions configures the Fetch operation.
type FetchOptions struct {
	MaxBytes int64  // max response body size (default 2MB, max 10MB)
	RenderJS bool   // attempt headless Chrome rendering
	SavePath string // if set, save raw content to this file path instead of returning text
	TimeoutS int    // timeout in seconds (default 30, max 600)
	Offset   int    // rune offset for continued reading
	MaxChars int    // max characters to return in Content (0 = full content)
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
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") &&
		!strings.HasPrefix(rawURL, "ftp://") {
		rawURL = "https://" + rawURL
	}
	if _, err := url.Parse(rawURL); err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if opts == nil {
		opts = &FetchOptions{}
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 2 * 1024 * 1024 // 2MB default
	}
	if opts.MaxBytes > 10*1024*1024 {
		opts.MaxBytes = 10 * 1024 * 1024 // 10MB cap
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

	// Dispatch by scheme
	if strings.HasPrefix(rawURL, "ftps://") {
		return nil, fmt.Errorf("FTPS (FTP over TLS) is not supported yet, use ftp:// instead")
	}
	if strings.HasPrefix(rawURL, "ftp://") {
		return fetchFTPCtx(parent, rawURL, opts)
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
	ctx, cancel := context.WithTimeout(parent, time.Duration(opts.TimeoutS)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7,ja;q=0.6")
	req.Header.Set("Accept-Encoding", "identity") // avoid compressed responses for simplicity

	if client == nil {
		client = httpClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxBytes))
	if err != nil {
		return nil, fmt.Errorf("read body failed: %w", err)
	}

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

	// If save path is specified, write raw bytes to file
	if opts.SavePath != "" {
		dir := filepath.Dir(opts.SavePath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create directory failed: %w", err)
		}
		if err := os.WriteFile(opts.SavePath, body, 0o644); err != nil {
			return nil, fmt.Errorf("write file failed: %w", err)
		}
		result.SavedTo = opts.SavePath
		result.Content = fmt.Sprintf("文件已保存到 %s (%d 字节)", opts.SavePath, len(body))
		return result, nil
	}

	// Detect and convert encoding
	body = ensureUTF8(body, ct)

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
	return result, nil
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
	runes := []rune(result.Content)
	total := len(runes)
	result.TotalChars = total
	if offset > total {
		offset = total
	}
	end := total
	if maxChars > 0 {
		end = min(total, offset+maxChars)
	}
	if offset > 0 || end < total {
		result.Content = string(runes[offset:end])
	} else {
		result.Content = string(runes)
	}
	result.Truncated = offset > 0 || end < total
	result.HasMore = end < total
	if result.HasMore {
		result.NextOffset = end
	} else {
		result.NextOffset = 0
	}
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

	// Read data with size limit
	body, err := io.ReadAll(io.LimitReader(dataConn, opts.MaxBytes))
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
		URL:         rawURL,
		ContentType: "application/octet-stream (FTP)",
		BytesRead:   len(body),
	}

	// Save to file if requested
	if opts.SavePath != "" {
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

	body, readErr := io.ReadAll(io.LimitReader(stdoutPipe, opts.MaxBytes))
	waitErr := cmd.Wait()
	if ctxErr := parent.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if readErr != nil {
		return nil, fmt.Errorf("Chrome read output failed: %w", readErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("Chrome exited with error: %w", waitErr)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("Chrome returned empty output")
	}

	title, text := extractReadableContent(body)
	result := &FetchResult{
		URL:         rawURL,
		Title:       title,
		ContentType: "text/html (Chrome rendered)",
		Content:     text,
		BytesRead:   len(body),
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
