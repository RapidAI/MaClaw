package httpthreat

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

var (
	redactDetector = security.NewSensitiveDetector()
	bearerRE       = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-+/=]+`)
	longB64RE      = regexp.MustCompile(`[A-Za-z0-9+/]{48,}={0,2}`)
	hexBlobRE      = regexp.MustCompile(`\b[a-fA-F0-9]{40,}\b`)
	idCardRE       = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	cardRE         = regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`)
)

var dropHeaderKeys = map[string]struct{}{
	"cookie": {}, "authorization": {}, "proxy-authorization": {},
	"x-api-key": {}, "x-auth-token": {},
}

func ClassAction(class string) string {
	switch strings.TrimSpace(class) {
	case ClassBenign:
		return ActionAllow
	case ClassScan:
		return ActionObserve
	case ClassAbuse:
		return ActionRateLimit
	case ClassAuthAbuse:
		return ActionChallenge
	case ClassExploit, ClassMalware, ClassExfil, ClassFraud:
		return ActionBlock
	default:
		return ""
	}
}

func ActionRank(action string) int {
	switch action {
	case ActionAllow:
		return 0
	case ActionObserve:
		return 1
	case ActionRateLimit:
		return 2
	case ActionChallenge:
		return 3
	case ActionBlock:
		return 4
	default:
		return -1
	}
}

func MaxAction(a, b string) string {
	if ActionRank(a) >= ActionRank(b) {
		if a == "" {
			return b
		}
		return a
	}
	return b
}

func IsTrainableClass(class string) bool {
	class = strings.TrimSpace(class)
	for _, item := range TrainableClasses {
		if item == class {
			return true
		}
	}
	return false
}

func NormalizePipeline(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case PipelineShadow, PipelineCanary, PipelineOn:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return PipelineOff
	}
}

func RedactText(s string) string {
	s = redactDetector.Redact(s)
	s = bearerRE.ReplaceAllString(s, "[REDACTED]")
	s = idCardRE.ReplaceAllString(s, "[REDACTED]")
	s = cardRE.ReplaceAllString(s, "[REDACTED]")
	s = longB64RE.ReplaceAllString(s, "[REDACTED]")
	s = hexBlobRE.ReplaceAllString(s, "[REDACTED]")
	return s
}

func normalizeMethod(m string) string {
	return strings.ToUpper(strings.TrimSpace(m))
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if h, p, err := net.SplitHostPort(host); err == nil {
		if p == "80" || p == "443" {
			return h
		}
		return strings.ToLower(h) + ":" + p
	}
	return host
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if dec, err := url.PathUnescape(p); err == nil {
		p = dec
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == ".." {
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, part)
	}
	joined := strings.Join(out, "/")
	if strings.HasPrefix(p, "/") && !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return joined
}

func normalizeQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.TrimPrefix(q, "?")
	if q == "" {
		return ""
	}
	values, err := url.ParseQuery(q)
	if err != nil {
		return RedactText(q)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		vals := values[k]
		for j, v := range vals {
			if b.Len() > 0 && (i > 0 || j > 0) {
				b.WriteByte('&')
			}
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(RedactText(v)))
		}
	}
	return b.String()
}

func normalizeReferer(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return RedactText(ref)
	}
	return normalizeHost(u.Host) + normalizePath(u.Path)
}

func dropSensitiveHeaders(h map[string]string) {
	for k := range h {
		if _, ok := dropHeaderKeys[strings.ToLower(strings.TrimSpace(k))]; ok {
			delete(h, k)
		}
	}
}

func truncateRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(s)
	return string(runes[:n])
}

func applyHTTP2Pseudo(tx *Transaction) {
	if tx == nil {
		return
	}
	if strings.TrimSpace(tx.Method) == "" {
		tx.Method = headerGet(tx.Headers, ":method")
	}
	if strings.TrimSpace(tx.Host) == "" {
		tx.Host = headerGet(tx.Headers, ":authority")
	}
	if strings.TrimSpace(tx.Path) == "" {
		tx.Path = headerGet(tx.Headers, ":path")
	}
}

func headerGet(h map[string]string, key string) string {
	if len(h) == 0 {
		return ""
	}
	want := strings.ToLower(strings.TrimSpace(key))
	for k, v := range h {
		if strings.ToLower(strings.TrimSpace(k)) == want {
			return v
		}
	}
	return ""
}

// TransactionFromHTTP prefers a JSON Transaction body (mirror collector),
// otherwise parses the live request. Request-side STATUS stays empty.
func TransactionFromHTTP(r *http.Request) Transaction {
	if r == nil {
		return Transaction{}
	}
	if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var tx Transaction
		if json.Unmarshal(raw, &tx) == nil && strings.TrimSpace(tx.Method+tx.Host+tx.Path) != "" {
			tx.Status = ""
			dropSensitiveHeaders(tx.Headers)
			return tx
		}
	}
	return FromRequest(r)
}

// FromRequest builds a request-side Transaction. STATUS is empty. Cookie /
// Authorization and kin are dropped. The request body is peeked then restored
// so a later handler still sees the original stream.
func FromRequest(r *http.Request) Transaction {
	if r == nil {
		return Transaction{}
	}
	path := ""
	query := ""
	if r.URL != nil {
		path = r.URL.Path
		query = r.URL.RawQuery
	}
	tx := Transaction{
		Method:      r.Method,
		Host:        r.Host,
		Path:        path,
		Query:       query,
		UserAgent:   r.UserAgent(),
		ContentType: r.Header.Get("Content-Type"),
		Referer:     r.Referer(),
		Upgrade:     r.Header.Get("Upgrade"),
		SiteID:      strings.TrimSpace(r.Header.Get("X-Site-ID")),
		SourceID:    sourceIDFromRemote(r.RemoteAddr),
		Headers:     copySafeHeaders(r.Header),
	}
	if strings.TrimSpace(tx.Host) == "" {
		tx.Host = r.Header.Get("Host")
	}
	applyHTTP2Pseudo(&tx)
	if strings.Contains(tx.Path, "?") && tx.Query == "" {
		if i := strings.IndexByte(tx.Path, '?'); i >= 0 {
			tx.Query = tx.Path[i+1:]
			tx.Path = tx.Path[:i]
		}
	}
	if r.Body != nil && !skipRequestBody(tx) {
		buf, _ := io.ReadAll(io.LimitReader(r.Body, MaxRequestBodyBytes))
		tx.Body = string(buf)
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))
	}
	return tx
}

func skipRequestBody(tx Transaction) bool {
	ct := strings.ToLower(strings.TrimSpace(tx.ContentType))
	if strings.HasPrefix(ct, "application/grpc") {
		return true
	}
	up := strings.ToLower(strings.TrimSpace(tx.Upgrade) + " " + headerGet(tx.Headers, "upgrade"))
	return strings.Contains(up, "websocket")
}

func sourceIDFromRemote(addr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

func copySafeHeaders(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, vs := range h {
		if _, drop := dropHeaderKeys[strings.ToLower(strings.TrimSpace(k))]; drop {
			continue
		}
		if len(vs) == 0 {
			continue
		}
		out[k] = vs[0]
	}
	return out
}

func skipDetectPath(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	if strings.HasPrefix(p, "/api/httpthreat/") {
		return true
	}
	if p == "/admin" || strings.HasPrefix(p, "/admin/") || strings.HasPrefix(p, "/api/v1/admin/") {
		return true
	}
	switch p {
	case "/health", "/healthz", "/ready", "/readyz", "/live", "/livez", "/metrics", "/version", "/openapi.json", "/api/v1/openapi.json":
		return true
	}
	return false
}

// SkipCorpus drops WS upgrade, gRPC frames, health probes, and empty previews.
func SkipCorpus(tx Transaction) bool {
	ct := strings.ToLower(strings.TrimSpace(tx.ContentType))
	if strings.HasPrefix(ct, "application/grpc") {
		return true
	}
	up := strings.ToLower(strings.TrimSpace(tx.Upgrade) + " " + headerGet(tx.Headers, "upgrade"))
	if strings.Contains(up, "websocket") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(tx.Path)) {
	case "/health", "/healthz", "/ready", "/readyz", "/live", "/livez":
		return true
	}
	if strings.TrimSpace(tx.Method+tx.Host+tx.Path+tx.Query+tx.UserAgent+tx.Body) == "" {
		return true
	}
	return false
}

// BuildPreview redacts, normalizes, then truncates to MaxPreviewRunes.
func BuildPreview(tx Transaction) string {
	dropSensitiveHeaders(tx.Headers)
	lines := []string{
		"METHOD " + normalizeMethod(tx.Method),
		"HOST " + normalizeHost(tx.Host),
		"PATH " + RedactText(normalizePath(tx.Path)),
		"QUERY " + normalizeQuery(tx.Query),
		"UA " + RedactText(strings.TrimSpace(tx.UserAgent)),
		"CT " + strings.ToLower(strings.TrimSpace(tx.ContentType)),
		"STATUS " + strings.TrimSpace(tx.Status),
		"REF " + normalizeReferer(tx.Referer),
		"BODY " + RedactText(strings.TrimSpace(tx.Body)),
	}
	return truncateRunes(strings.Join(lines, "\n"), MaxPreviewRunes)
}

// SampleID is sha1(tenant + LF + encoder + LF + preview). Content addressing only.
func SampleID(tenantID, encoderID, preview string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(tenantID) + "\n" + strings.TrimSpace(encoderID) + "\n" + preview))
	return hex.EncodeToString(sum[:])
}
