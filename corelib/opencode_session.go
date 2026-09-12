package corelib

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	OpenCodeSessionHeader = "x-opencode-session"
	openCodeSeedByteLimit = 8192
)

type openCodeSessionContextKey struct{}

// IsOpenCodeHost reports whether hostname is OpenCode Zen / Go (opencode.ai or a subdomain).
func IsOpenCodeHost(hostname string) bool {
	host := strings.ToLower(strings.TrimSpace(hostname))
	return host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai")
}

// IsOpenCodeURL reports whether rawURL targets an OpenCode host.
func IsOpenCodeURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return IsOpenCodeHost(u.Hostname())
}

// ShouldAttachOpenCodeSession reports whether this config should carry
// x-opencode-session: OpenCode hosts, and Hub/HubCenter endpoints that may
// later dispatch to OpenCode.
func ShouldAttachOpenCodeSession(cfg MaclawLLMConfig) bool {
	return IsOpenCodeURL(cfg.URL) || cfg.ShouldSendWorkloadHints()
}

// WithOpenCodeSessionID stores a conversation affinity id on ctx for outbound
// OpenCode requests. Empty ids are ignored.
func WithOpenCodeSessionID(ctx context.Context, sessionID string) context.Context {
	sessionID = strings.TrimSpace(sessionID)
	if ctx == nil || sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, openCodeSessionContextKey{}, sessionID)
}

// OpenCodeSessionIDFromContext returns the affinity id previously stored on ctx.
func OpenCodeSessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(openCodeSessionContextKey{}).(string)
	return strings.TrimSpace(value)
}

// BindOpenCodeSessionID copies a context affinity id onto cfg when cfg has none.
func BindOpenCodeSessionID(ctx context.Context, cfg MaclawLLMConfig) MaclawLLMConfig {
	if strings.TrimSpace(cfg.SessionID) != "" {
		return cfg
	}
	if id := OpenCodeSessionIDFromContext(ctx); id != "" {
		cfg.SessionID = id
	}
	return cfg
}

// ResolveOpenCodeSessionID returns explicit when set, otherwise a fresh UUIDv4.
func ResolveOpenCodeSessionID(explicit string) string {
	if id := strings.TrimSpace(explicit); id != "" {
		return id
	}
	return newOpenCodeSessionUUID(nil, 0x40)
}

// StableOpenCodeSessionID returns a UUIDv5-shaped id derived from parts so the
// same conversation seeds the same OpenCode routing key. Empty parts are skipped.
// If every part is empty a random id is returned so the request is still accepted.
func StableOpenCodeSessionID(parts ...string) string {
	h := sha256.New()
	wrote := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if wrote {
			h.Write([]byte{0})
		}
		h.Write([]byte(part))
		wrote = true
	}
	if !wrote {
		return newOpenCodeSessionUUID(nil, 0x40)
	}
	sum := h.Sum(nil)
	return newOpenCodeSessionUUID(sum[:16], 0x50)
}

// OpenCodeConversationSeed extracts a stable conversation fingerprint from an
// OpenAI-style or Responses-style request body.
func OpenCodeConversationSeed(body map[string]any) string {
	if len(body) == 0 {
		return ""
	}
	if seed := OpenCodeConversationSeedFromMessages(body["messages"]); seed != "" {
		return seed
	}
	return OpenCodeConversationSeedFromMessages(body["input"])
}

// OpenCodeConversationSeedFromMessages returns the first user-turn text from a
// chat/completions messages array or a Responses API input value.
func OpenCodeConversationSeedFromMessages(messages any) string {
	switch list := messages.(type) {
	case string:
		return truncateOpenCodeSeed(list)
	case []any:
		return firstUserContentSeed(list)
	case []map[string]any:
		return firstUserContentSeed(anySlice(list))
	case []map[string]string:
		return firstUserContentSeed(anySlice(list))
	default:
		return ""
	}
}

func anySlice[T any](list []T) []any {
	items := make([]any, len(list))
	for i, item := range list {
		items[i] = item
	}
	return items
}

func firstUserContentSeed(messages []any) string {
	for _, item := range messages {
		if strings.ToLower(strings.TrimSpace(messageStringField(item, "role"))) != "user" {
			continue
		}
		if seed := messageStringField(item, "content"); seed != "" {
			return seed
		}
		if seed := messageStringField(item, "text"); seed != "" {
			return seed
		}
	}
	return ""
}

func messageStringField(v any, key string) string {
	switch m := v.(type) {
	case map[string]any:
		return stringifyOpenCodeSeed(m[key])
	case map[string]string:
		return truncateOpenCodeSeed(m[key])
	default:
		return ""
	}
}

func stringifyOpenCodeSeed(value any) string {
	switch v := value.(type) {
	case string:
		return truncateOpenCodeSeed(v)
	case []any:
		var b strings.Builder
		for _, part := range v {
			switch p := part.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if text, ok := p["text"].(string); ok {
					b.WriteString(text)
					continue
				}
				if seed := stringifyOpenCodeSeed(p["content"]); seed != "" {
					b.WriteString(seed)
				}
			}
			if b.Len() >= openCodeSeedByteLimit {
				break
			}
		}
		return truncateOpenCodeSeed(b.String())
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return truncateOpenCodeSeed(text)
		}
		return stringifyOpenCodeSeed(v["content"])
	case map[string]string:
		if text := strings.TrimSpace(v["text"]); text != "" {
			return truncateOpenCodeSeed(text)
		}
		return truncateOpenCodeSeed(v["content"])
	}
	return ""
}

func truncateOpenCodeSeed(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= openCodeSeedByteLimit {
		return s
	}
	cut := openCodeSeedByteLimit
	for cut > 0 && !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut]
}

// OpenCodeSessionHeaderIfNeeded returns the OpenCode affinity header when rawURL
// is an OpenCode host. A caller-supplied session id is preferred; otherwise a
// fresh id is generated so OpenCode Go does not reject the request with HTTP 400.
func OpenCodeSessionHeaderIfNeeded(rawURL, sessionID string) (name, value string, ok bool) {
	if !IsOpenCodeURL(rawURL) {
		return "", "", false
	}
	return OpenCodeSessionHeader, ResolveOpenCodeSessionID(sessionID), true
}

// OpenCodeSessionHeaderForConfig is OpenCodeSessionHeaderIfNeeded using cfg.URL
// and also attaches the header for Hub-managed endpoints that may proxy to OpenCode.
func OpenCodeSessionHeaderForConfig(cfg MaclawLLMConfig) (name, value string, ok bool) {
	if !ShouldAttachOpenCodeSession(cfg) {
		return "", "", false
	}
	return OpenCodeSessionHeader, ResolveOpenCodeSessionID(cfg.SessionID), true
}

// SetOpenCodeSessionHeaderIfNeeded stamps x-opencode-session on requests to
// OpenCode hosts. An existing header wins. sessionID, then the request context,
// then a fresh UUID are used as fallbacks.
func SetOpenCodeSessionHeaderIfNeeded(req *http.Request, sessionID string) {
	if req == nil {
		return
	}
	url := ""
	if req.URL != nil {
		url = req.URL.String()
	}
	ApplyOpenCodeSessionHeader(req, MaclawLLMConfig{URL: url, SessionID: sessionID})
}

// ApplyOpenCodeSessionHeader stamps x-opencode-session for OpenCode hosts and
// for Hub-managed endpoints that later dispatch to OpenCode.
func ApplyOpenCodeSessionHeader(req *http.Request, cfg MaclawLLMConfig) {
	if req == nil {
		return
	}
	openCodeHost := req.URL != nil && IsOpenCodeHost(req.URL.Hostname())
	if !openCodeHost && !ShouldAttachOpenCodeSession(cfg) {
		return
	}
	if strings.TrimSpace(req.Header.Get(OpenCodeSessionHeader)) != "" {
		return
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = OpenCodeSessionIDFromContext(req.Context())
	}
	req.Header.Set(OpenCodeSessionHeader, ResolveOpenCodeSessionID(sessionID))
}

func newOpenCodeSessionUUID(raw []byte, version byte) string {
	var b [16]byte
	if len(raw) >= 16 {
		copy(b[:], raw[:16])
	} else if _, err := rand.Read(b[:]); err != nil {
		sum := sha256.Sum256([]byte("opencode-session-fallback"))
		copy(b[:], sum[:16])
	}
	b[6] = (b[6] & 0x0f) | version
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
