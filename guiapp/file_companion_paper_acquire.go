package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"golang.org/x/net/html"
)

const (
	companionPaperNoticeArg = "companion-paper-notice"

	companionPaperCodeBadScheme  = "bad-scheme"
	companionPaperCodeNoPDF      = "no-pdf"
	companionPaperCodeNotPDF     = "not-pdf"
	companionPaperCodeTooBig     = "too-big"
	companionPaperCodeTimeout    = "timeout"
	companionPaperCodeSaveFailed = "save-failed"

	companionPaperURLMax       = 8 * 1024
	companionPaperHTMLMaxBytes = 2 << 20
	companionPaperHTMLTimeoutS = 15
	companionPaperPDFMaxBytes  = 25 << 20
	companionPaperPDFTimeoutS  = 60
	companionPaperMaxFetches   = 3
)

// paperFetchHook replaces the public fetch after ValidatePublicHTTPURL.
// Success-path tests use it so a loopback server is never the request URL.
var paperFetchHook func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error)

// companionPaperExecHook replaces the open-file / notice process start in tests.
var companionPaperExecHook func(argv []string) error

func companionPaperNoticeCode(code string) bool {
	switch code {
	case companionPaperCodeBadScheme, companionPaperCodeNoPDF, companionPaperCodeNotPDF,
		companionPaperCodeTooBig, companionPaperCodeTimeout, companionPaperCodeSaveFailed:
		return true
	default:
		return false
	}
}

// paperFetch is the only HTTP client the browser right-click uses.
// PublicNetworkOnly strips cookies and blocks private redirects. HubDownload
// stays nil so the URL is not uploaded to the Hub.
func paperFetch(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
	if opts == nil {
		opts = &websearch.FetchOptions{}
	}
	opts.PublicNetworkOnly = true
	opts.HubDownload = nil
	if _, err := websearch.ValidatePublicHTTPURL(rawURL); err != nil {
		return nil, err
	}
	if paperFetchHook != nil {
		return paperFetchHook(ctx, rawURL, opts)
	}
	return websearch.FetchCtx(ctx, rawURL, opts)
}

// companionPaperFromArgs reports a maclaw://companion-paper open.
// Nothing in v1 issues a nonce, so every such URL is refused.
func companionPaperFromArgs(args []string) bool {
	for _, arg := range args {
		if companionPaperURL(arg) {
			return true
		}
	}
	return false
}

func companionPaperURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "maclaw") && strings.EqualFold(parsed.Host, "companion-paper")
}

// acquireCompanionPaper downloads one public paper and returns its local PDF
// path. The second result is empty on success and one of the six notice codes
// otherwise. It does not start the companion window; acceptCompanionPaper does.
func acquireCompanionPaper(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if code := admitCompanionPaperURL(raw); code != "" {
		logCompanionPaper(raw, code)
		return "", code
	}
	path, code := resolveCompanionPaper(raw)
	if code == "" {
		logCompanionPaper(raw, "ok")
		return path, ""
	}
	logCompanionPaper(raw, code)
	return "", code
}

// acceptCompanionPaper downloads, then starts the companion on the PDF or on
// a notice. The returned code is empty when the open-file launch was started.
func acceptCompanionPaper(raw string) string {
	path, code := acquireCompanionPaper(raw)
	if code == "" {
		if err := deliverCompanionPaperOpen(path); err != nil {
			code = companionPaperCodeSaveFailed
			_ = deliverCompanionPaperNotice(code)
			return code
		}
		return ""
	}
	_ = deliverCompanionPaperNotice(code)
	return code
}

func deliverCompanionPaperOpen(path string) error {
	argv, err := companionPaperCommand(companionPaperOpenArg, path)
	if err != nil {
		return err
	}
	return deliverCompanionPaperArgv(argv)
}

func deliverCompanionPaperNotice(code string) error {
	if !companionPaperNoticeCode(code) {
		code = companionPaperCodeSaveFailed
	}
	argv, err := companionPaperCommand(companionPaperNoticeArg, code)
	if err != nil {
		return err
	}
	return deliverCompanionPaperArgv(argv)
}

func companionPaperCommand(mode, arg string) ([]string, error) {
	exe, err := companionExecutable()
	if err != nil {
		return nil, err
	}
	return []string{exe, mode, arg}, nil
}

func deliverCompanionPaperArgv(argv []string) error {
	if companionPaperExecHook != nil {
		return companionPaperExecHook(argv)
	}
	if len(argv) == 0 {
		return errors.New("missing executable")
	}
	allowCompanionForeground()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = filepath.Dir(argv[0])
	return cmd.Start()
}

func admitCompanionPaperURL(raw string) string {
	if raw == "" || strings.ContainsRune(raw, 0) || len(raw) > companionPaperURLMax {
		return companionPaperCodeBadScheme
	}
	if _, err := websearch.ValidatePublicHTTPURL(raw); err != nil {
		return companionPaperCodeBadScheme
	}
	return ""
}

func logCompanionPaper(raw, result string) {
	host := ""
	if u, err := url.Parse(raw); err == nil && u != nil {
		host = strings.ToLower(u.Hostname())
	}
	logFileCompanion("paper host=%s arxiv=%s result=%s", host, arxivIDFromInput(raw), result)
}

func resolveCompanionPaper(raw string) (string, string) {
	if arxivIDFromInput(raw) != "" {
		path, err := (&App{}).openArxivPaper(raw, arxivOpenOptions{PublicNetworkOnly: true})
		if err != nil {
			return "", companionPaperCodeFromErr(err)
		}
		if !fileIsPDF(path) {
			return "", companionPaperCodeNotPDF
		}
		return path, ""
	}
	if paperURLPathPDF(raw) {
		return savePublicPaper(raw, raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(companionPaperHTMLTimeoutS)*time.Second)
	defer cancel()
	result, err := paperFetch(ctx, paperRequestURL(raw), &websearch.FetchOptions{
		TimeoutS: companionPaperHTMLTimeoutS,
		MaxBytes: companionPaperHTMLMaxBytes,
		RawBody:  true,
	})
	if err != nil {
		code := companionPaperCodeFromErr(err)
		if code == companionPaperCodeTimeout || code == companionPaperCodeTooBig || code == companionPaperCodeBadScheme {
			return "", code
		}
		result = nil
	}
	if result != nil && paperContentTypePDF(result.ContentType) {
		page := strings.TrimSpace(result.URL)
		if page == "" {
			page = raw
		}
		return savePublicPaper(raw, page)
	}
	var candidates []string
	if result != nil && paperContentTypeHTML(result.ContentType) {
		candidates = append(candidates, paperHTMLPDFLinks(raw, result.Content)...)
	}
	candidates = append(candidates, paperWhitelistCandidates(raw)...)
	return fetchPaperCandidates(raw, candidates)
}

func fetchPaperCandidates(page string, candidates []string) (string, string) {
	seen := map[string]bool{}
	bad := 0
	tried := 0
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err := websearch.ValidatePublicHTTPURL(candidate); err != nil {
			bad++
			continue
		}
		if tried >= companionPaperMaxFetches {
			break
		}
		tried++
		path, code := savePublicPaper(page, candidate)
		if code == "" {
			return path, ""
		}
		if code == companionPaperCodeNotPDF {
			continue
		}
		return "", code
	}
	if tried == 0 && bad > 0 {
		return "", companionPaperCodeBadScheme
	}
	return "", companionPaperCodeNoPDF
}

func savePublicPaper(page, pdfURL string) (string, string) {
	dir, err := (&App{}).arxivPaperDir()
	if err != nil {
		return "", companionPaperCodeSaveFailed
	}
	target := filepath.Join(dir, companionPaperFileName(canonicalPaperURL(page)))
	if fileIsPDF(target) {
		return target, ""
	}
	stageFile, err := os.CreateTemp(dir, companionPaperFileName(canonicalPaperURL(page))+".*.part")
	if err != nil {
		return "", companionPaperCodeSaveFailed
	}
	staged := stageFile.Name()
	_ = stageFile.Close()
	_ = os.Remove(staged)
	defer func() {
		if _, statErr := os.Stat(staged); statErr == nil {
			_ = os.Remove(staged)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(companionPaperPDFTimeoutS)*time.Second)
	defer cancel()
	result, err := paperFetch(ctx, paperRequestURL(pdfURL), &websearch.FetchOptions{
		SavePath: staged,
		MaxBytes: companionPaperPDFMaxBytes,
		TimeoutS: companionPaperPDFTimeoutS,
	})
	if err != nil {
		return "", companionPaperCodeFromErr(err)
	}
	saved := staged
	if result != nil && strings.TrimSpace(result.SavedTo) != "" {
		saved = strings.TrimSpace(result.SavedTo)
	}
	if !fileIsPDF(saved) {
		_ = os.Remove(saved)
		return "", companionPaperCodeNotPDF
	}
	if err := arxivRename(saved, target); err != nil {
		if fileIsPDF(target) {
			_ = os.Remove(saved)
			return target, ""
		}
		_ = os.Remove(saved)
		return "", companionPaperCodeSaveFailed
	}
	return target, ""
}

// paperRequestURL is the URL that is fetched. Fragment, userinfo, and
// credential query keys are removed. Every other query pair keeps its
// original encoding and order. url.Values.Encode sorts keys and turns
// spaces into '+', which changes a signed or order-sensitive PDF URL.
// The filename still uses canonicalPaperURL.
func paperRequestURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return raw
		}
		return trimmed
	}
	u.Fragment = ""
	u.User = nil
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		u.Host = host + ":" + port
	} else {
		u.Host = host
	}
	u.RawQuery = stripSecretPaperQuery(u.RawQuery)
	return u.String()
}

func stripSecretPaperQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		key := part
		if i := strings.IndexByte(part, '='); i >= 0 {
			key = part[:i]
		}
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			decoded = key
		}
		if secretPaperQueryKey(decoded) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

func secretPaperQueryKey(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "token", "access_token", "key", "sig", "signature", "expires":
		return true
	default:
		return strings.HasPrefix(lower, "utm_")
	}
}

func companionPaperFileName(canonical string) string {
	sum := sha256.Sum256([]byte(canonical))
	return "paper-" + hex.EncodeToString(sum[:8]) + ".pdf"
}

func canonicalPaperURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return strings.TrimSpace(raw)
	}
	u.Fragment = ""
	u.User = nil
	u.Host = strings.ToLower(u.Host)
	q := u.Query()
	for _, key := range append([]string(nil), queryKeys(q)...) {
		if secretPaperQueryKey(key) {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func queryKeys(q url.Values) []string {
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	return keys
}

func paperURLPathPDF(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".pdf")
}

func companionPaperCodeFromErr(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return companionPaperCodeTimeout
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "exceeds max_bytes"):
		return companionPaperCodeTooBig
	case strings.Contains(lower, "deadline"), strings.Contains(lower, "timed out"), strings.Contains(lower, "timeout"), strings.Contains(msg, "超时"):
		return companionPaperCodeTimeout
	case strings.Contains(msg, "没有拿到 PDF"):
		return companionPaperCodeNotPDF
	case strings.Contains(msg, "保存失败"):
		return companionPaperCodeSaveFailed
	case strings.Contains(lower, "not public"),
		strings.Contains(lower, "non-public"),
		strings.Contains(lower, "credentials"),
		strings.Contains(lower, "only supports http"),
		strings.Contains(lower, "host is required"):
		return companionPaperCodeBadScheme
	default:
		return companionPaperCodeSaveFailed
	}
}

func paperContentTypePDF(contentType string) bool {
	return paperMediaType(contentType) == "application/pdf"
}

func paperContentTypeHTML(contentType string) bool {
	switch paperMediaType(contentType) {
	case "text/html", "application/xhtml+xml":
		return true
	default:
		return false
	}
}

func paperMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	}
	return strings.ToLower(mediaType)
}

func paperHTMLPDFLinks(page, body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil || doc == nil {
		return nil
	}
	base, err := url.Parse(page)
	if err != nil {
		base = nil
	}
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.ElementNode {
			switch strings.ToLower(n.Data) {
			case "meta":
				name := strings.ToLower(paperAttr(n, "name"))
				prop := strings.ToLower(paperAttr(n, "property"))
				if name == "citation_pdf_url" || prop == "citation_pdf_url" {
					if content := strings.TrimSpace(paperAttr(n, "content")); content != "" {
						out = append(out, resolvePaperRef(base, content))
					}
				}
			case "link":
				rel := strings.ToLower(paperAttr(n, "rel"))
				kind := strings.ToLower(paperAttr(n, "type"))
				if strings.Contains(rel, "alternate") && kind == "application/pdf" {
					if href := strings.TrimSpace(paperAttr(n, "href")); href != "" {
						out = append(out, resolvePaperRef(base, href))
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return out
}

func paperAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func resolvePaperRef(base *url.URL, ref string) string {
	parsed, err := url.Parse(strings.TrimSpace(ref))
	if err != nil || parsed == nil {
		return ""
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	parsed.Fragment = ""
	return parsed.String()
}

var (
	openReviewIDRe = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,64}$`)
	pmcArticleRe   = regexp.MustCompile(`(?i)^/pmc/articles/(PMC[0-9]+)/?$`)
)

func paperWhitelistCandidates(raw string) []string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	path := u.Path
	switch host {
	case "aclanthology.org", "www.aclanthology.org":
		if path != "" && path != "/" && !strings.HasSuffix(strings.ToLower(path), ".pdf") {
			clone := *u
			clone.Host = host
			clone.Path = strings.TrimSuffix(path, "/") + ".pdf"
			clone.RawQuery = ""
			clone.Fragment = ""
			return []string{clone.String()}
		}
	case "openreview.net", "www.openreview.net":
		if path == "/forum" || path == "/pdf" {
			id := u.Query().Get("id")
			if openReviewIDRe.MatchString(id) {
				return []string{"https://openreview.net/pdf?id=" + url.QueryEscape(id)}
			}
		}
	case "biorxiv.org", "www.biorxiv.org", "medrxiv.org", "www.medrxiv.org":
		if strings.Contains(path, "/content/10.1101/") && !strings.HasSuffix(strings.ToLower(path), ".pdf") {
			clone := *u
			clone.Host = host
			clone.Path = strings.TrimSuffix(path, "/") + ".full.pdf"
			clone.RawQuery = ""
			clone.Fragment = ""
			return []string{clone.String()}
		}
	case "ncbi.nlm.nih.gov", "www.ncbi.nlm.nih.gov":
		if m := pmcArticleRe.FindStringSubmatch(path); m != nil {
			return []string{"https://" + host + "/pmc/articles/" + m[1] + "/pdf/"}
		}
	}
	return nil
}
