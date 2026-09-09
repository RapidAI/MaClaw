package logx

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
)

// RedactHandler is a slog.Handler wrapper applying the shared redaction
// boundary to every record: sensitive attribute keys are masked outright,
// and string values pass through RedactString (bearer tokens, key=value
// secrets, URL userinfo, absolute file paths). Grouped attributes are
// redacted recursively; error/Stringer values carried via slog.Any are
// redacted through their string form.
type RedactHandler struct {
	next slog.Handler
}

// NewRedactHandler wraps next with the shared redaction boundary.
func NewRedactHandler(next slog.Handler) *RedactHandler {
	return &RedactHandler{next: next}
}

// Enabled reports whether the wrapped handler is enabled for the level.
func (h *RedactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle redacts the record and forwards it.
func (h *RedactHandler) Handle(ctx context.Context, rec slog.Record) error {
	redacted := slog.NewRecord(rec.Time, rec.Level, RedactString(rec.Message), rec.PC)
	rec.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(redactAttr(attr))
		return true
	})
	return h.next.Handle(ctx, redacted)
}

// WithAttrs returns a wrapped handler with the attributes redacted eagerly.
func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, redactAttr(attr))
	}
	return &RedactHandler{next: h.next.WithAttrs(redacted)}
}

// WithGroup returns a wrapped handler with the given group.
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{next: h.next.WithGroup(name)}
}

const redactedPlaceholder = "[redacted]"

// sensitiveKeyMarkers are attribute key fragments whose values are masked.
// Keep aligned with the value-level keyValueSecretRE vocabulary below.
var sensitiveKeyMarkers = []string{
	"secret", "token", "password", "passwd", "authorization", "cookie",
	"api_key", "api-key", "apikey", "api_secret", "apisecret", "private_key",
	"credential", "access_key", "session_key",
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

var (
	bearerTokenRE = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{8,}`)
	// keyValueSecretRE matches key=value / key:value secrets, including
	// quoted values with spaces. The key vocabulary must stay aligned with
	// sensitiveKeyMarkers.
	keyValueSecretRE = regexp.MustCompile(`(?i)\b(api[_-]?key|api[_-]?secret|access[_-]?token|refresh[_-]?token|token|password|passwd|secret|authorization|cookie|credential)\s*[:=]\s*(?:"[^"]{4,}"|'[^']{4,}'|[^\s"&]{4,})`)
	// windowsAbsPathRE matches drive-letter absolute paths like C:\Users\foo.
	windowsAbsPathRE = regexp.MustCompile(`\b[A-Za-z]:[\\/][^\s"']+`)
	// homeishPathRE matches unix absolute paths under user/home directories.
	// Note: \b cannot precede '/' (both non-word chars), so no \b here.
	homeishPathRE = regexp.MustCompile(`(?:/home/|/Users/|/root/)[^\s"']+`)
	// urlSchemes scanned for userinfo redaction (matched case-insensitively).
	urlSchemes = []string{"https://", "http://", "wss://", "ws://"}
)

// RedactString applies the shared value-level redaction: bearer tokens,
// key=value secrets, URL userinfo, and absolute user file paths.
func RedactString(s string) string {
	if s == "" {
		return s
	}
	// Cheap prefilter: skip the regex machinery for plainly safe text.
	lower := strings.ToLower(s)
	maybeSecret := strings.Contains(s, "earer ") ||
		strings.Contains(lower, "key") || strings.Contains(lower, "token") ||
		strings.Contains(lower, "password") || strings.Contains(lower, "passwd") ||
		strings.Contains(lower, "secret") || strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "cookie") || strings.Contains(lower, "credential")
	if maybeSecret {
		out := bearerTokenRE.ReplaceAllString(s, "Bearer "+redactedPlaceholder)
		out = keyValueSecretRE.ReplaceAllStringFunc(out, redactKeyValue)
		s = out
	}
	if strings.Contains(lower, "://") {
		s = redactURLUserinfo(s)
	}
	if strings.Contains(s, `:\`) || strings.Contains(s, ":/") ||
		strings.Contains(s, "/home/") || strings.Contains(s, "/Users/") || strings.Contains(s, "/root/") {
		s = windowsAbsPathRE.ReplaceAllStringFunc(s, redactPath)
		s = homeishPathRE.ReplaceAllStringFunc(s, redactPath)
	}
	return s
}

// redactKeyValue masks the value part of a key=value / key:value secret,
// preserving the key, the separator, and any surrounding quotes.
func redactKeyValue(m string) string {
	sep := strings.IndexAny(m, ":=")
	if sep < 0 {
		return redactedPlaceholder
	}
	rest := m[sep+1:]
	quoted := len(rest) >= 2 && (rest[0] == '"' || rest[0] == '\'')
	if quoted {
		return m[:sep+1] + string(rest[0]) + redactedPlaceholder + string(rest[0])
	}
	return m[:sep+1] + redactedPlaceholder
}

func redactAttr(attr slog.Attr) slog.Attr {
	attr.Value = attr.Value.Resolve()
	if sensitiveKey(attr.Key) {
		switch attr.Value.Kind() {
		case slog.KindInt64, slog.KindUint64, slog.KindFloat64, slog.KindBool:
			// Numeric/bool values under sensitive-sounding keys are counters
			// (token_count, cookies_enabled), not secrets — keep them.
		default:
			attr.Value = slog.StringValue(redactedPlaceholder)
		}
		return attr
	}
	switch attr.Value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(RedactString(attr.Value.String()))
	case slog.KindGroup:
		group := attr.Value.Group()
		out := make([]slog.Attr, 0, len(group))
		for _, ga := range group {
			out = append(out, redactAttr(ga))
		}
		attr.Value = slog.GroupValue(out...)
	case slog.KindAny:
		switch v := attr.Value.Any().(type) {
		case error:
			attr.Value = slog.StringValue(RedactString(v.Error()))
		case fmt.Stringer:
			attr.Value = slog.StringValue(RedactString(v.String()))
		}
	}
	return attr
}

// redactURLUserinfo strips userinfo (user:password@) from URLs embedded in
// the text. Single pass over a builder: replacement offsets never go stale,
// fragments are preserved, and scheme matching is case-insensitive.
func redactURLUserinfo(s string) string {
	lower := strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	pos := 0
	for pos < len(s) {
		idx := -1
		for _, scheme := range urlSchemes {
			if i := strings.Index(lower[pos:], scheme); i >= 0 && (idx < 0 || pos+i < idx) {
				idx = pos + i
			}
		}
		if idx < 0 {
			break
		}
		rest := s[idx:]
		end := strings.IndexAny(rest, " \t\n\"'")
		if end < 0 {
			end = len(rest)
		}
		u, err := url.Parse(rest[:end])
		if err != nil || u.User == nil || u.User.Username() == "" {
			pos = idx + end
			continue
		}
		b.WriteString(s[pos:idx])
		b.WriteString(strings.ToLower(u.Scheme))
		b.WriteString("://")
		b.WriteString(redactedPlaceholder)
		b.WriteString("@")
		b.WriteString(u.Host) // Host never contains userinfo
		b.WriteString(u.RequestURI())
		if u.Fragment != "" {
			b.WriteString("#")
			b.WriteString(u.EscapedFragment())
		}
		pos = idx + end
	}
	b.WriteString(s[pos:])
	return b.String()
}

func redactPath(p string) string {
	trimmed := strings.TrimRight(p, `/\`)
	base := trimmed
	if i := strings.LastIndexAny(trimmed, `/\`); i >= 0 {
		base = trimmed[i+1:]
	}
	if base == "" || base == "." {
		return redactedPlaceholder
	}
	return redactedPlaceholder + "/" + base
}
