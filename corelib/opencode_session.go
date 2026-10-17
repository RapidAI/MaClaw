package corelib

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	OpenCodeSessionHeader = "x-opencode-session"
	openCodeSeedByteLimit = 8192
	// OpenCodeClientVersion is the semver carried in the official client's
	// User-Agent, `opencode/${InstallationVersion}` from
	// packages/opencode/src/session/llm/request.ts. The Zen free-tier gate
	// rejects a bare "opencode" and any version below 1.17.0.
	OpenCodeClientVersion = "1.18.31"
	openCodeBase62        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
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

// OpenCodeClientUserAgent is the User-Agent the official client sends.
// The preset names "opencode" and "OpenCode" are rewritten to opencode/<semver>.
// Any other identity, including a custom opencode/x.y.z, is kept as typed.
func OpenCodeClientUserAgent(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "opencode") {
		return "opencode/" + OpenCodeClientVersion
	}
	return name
}

// OpenCodeClientSessionID is the x-opencode-session value produced by
// Identifier.create("session") in packages/opencode/src/id/id.ts:
// "ses_" + 12 hex chars of the timestamp + 14 base62 chars.
// The same parts yield the same id until the hour changes, so one conversation
// stays on one session. A UUID is rejected by the free-tier gate.
func OpenCodeClientSessionID(parts ...string) string {
	return openCodeClientSessionIDAt(time.Now().UTC().Truncate(time.Hour), openCodeTimeHex, parts...)
}

// OpenCodeDescendingSessionID is the session id the Zen free tier accepts.
// Identifier.descending encodes ~(millis<<12|counter) in 48 bits. The free-tier
// gate allows only counter 0 through 63; counter 1 makes the low 12 bits ffe.
func OpenCodeDescendingSessionID(parts ...string) string {
	return openCodeClientSessionIDAt(time.Now().UTC().Truncate(time.Hour), openCodeDescendingTimeHex, parts...)
}

func openCodeClientSessionIDAt(now time.Time, timeHex func(int64) string, parts ...string) string {
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
		var rnd [16]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			sum := sha256.Sum256([]byte(now.Format(time.RFC3339Nano)))
			copy(rnd[:], sum[:])
		}
		h.Write(rnd[:])
	}
	sum := h.Sum(nil)
	var random [14]byte
	same := openCodeBase62[int(sum[0])%len(openCodeBase62)]
	allSame := true
	for i := range random {
		ch := openCodeBase62[int(sum[i%len(sum)])%len(openCodeBase62)]
		random[i] = ch
		if ch != same {
			allSame = false
		}
	}
	if allSame {
		random[len(random)-1] = openCodeBase62[(int(same)+1)%len(openCodeBase62)]
	}
	return "ses_" + timeHex(now.UnixMilli()) + string(random[:])
}

// openCodeTimeHex encodes milliseconds the way Identifier.create does:
// (millis<<12) stored in 6 big-endian bytes, which is 12 hex chars.
func openCodeTimeHex(ms int64) string {
	now := uint64(ms) << 12
	var raw [6]byte
	for i := 0; i < len(raw); i++ {
		raw[i] = byte((now >> (40 - 8*uint(i))) & 0xff)
	}
	return hex.EncodeToString(raw[:])
}

func openCodeDescendingTimeHex(ms int64) string {
	v := (uint64(ms) << 12) | 1
	v &= (1 << 48) - 1
	v = (^v) & ((1 << 48) - 1)
	var raw [6]byte
	for i := 0; i < len(raw); i++ {
		raw[i] = byte((v >> (40 - 8*uint(i))) & 0xff)
	}
	return hex.EncodeToString(raw[:])
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
// is an OpenCode host. The value is the official ses_ id. A caller-supplied
// official id is kept; any other id, including a UUID, is folded into one
// session for the current hour.
func OpenCodeSessionHeaderIfNeeded(rawURL, sessionID string) (name, value string, ok bool) {
	if !IsOpenCodeURL(rawURL) {
		return "", "", false
	}
	return OpenCodeSessionHeader, openCodeWireSessionID(sessionID), true
}

// OpenCodeSessionHeaderForConfig is OpenCodeSessionHeaderIfNeeded using cfg.URL
// and also attaches the header for Hub-managed endpoints that may proxy to OpenCode.
// Only a direct OpenCode host gets the ses_ id. Hub keeps the affinity id it was given.
func OpenCodeSessionHeaderForConfig(cfg MaclawLLMConfig) (name, value string, ok bool) {
	if !ShouldAttachOpenCodeSession(cfg) {
		return "", "", false
	}
	sessionID := strings.TrimSpace(cfg.SessionID)
	if IsOpenCodeURL(cfg.URL) {
		sessionID = openCodeWireSessionID(sessionID)
	} else {
		sessionID = ResolveOpenCodeSessionID(sessionID)
	}
	return OpenCodeSessionHeader, sessionID, true
}

// OpenCodeWireUserAgent rewrites the saved "opencode" / "OpenCode" preset to
// the official client User-Agent when rawURL is an OpenCode host. Every other
// destination, including GLM, keeps the saved name. A custom identity,
// including opencode/x.y.z, is kept as typed.
func OpenCodeWireUserAgent(rawURL, agent string) string {
	agent = strings.TrimSpace(agent)
	if !IsOpenCodeURL(rawURL) || !strings.EqualFold(agent, "opencode") {
		return agent
	}
	return OpenCodeClientUserAgent(agent)
}

// openCodeWireSessionID is the x-opencode-session value the Zen free tier accepts.
// The id must use Identifier.descending with a counter of 0 through 63.
// An id already in that window is kept. Anything else is derived from that
// value so one conversation stays on one session until the hour changes.
func openCodeWireSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if openCodeFreeTierSessionID(sessionID) {
		return sessionID
	}
	if sessionID == "" {
		return OpenCodeDescendingSessionID()
	}
	return OpenCodeDescendingSessionID(sessionID)
}

func openCodeFreeTierSessionID(id string) bool {
	if !isOfficialOpenCodeSessionID(id) {
		return false
	}
	tail := id[13:16]
	if tail[0] != 'f' || tail[1] < 'c' || tail[1] > 'f' {
		return false
	}
	c := tail[2]
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

func isOfficialOpenCodeSessionID(id string) bool {
	const hexLen, tailLen = 12, 14
	if len(id) != len("ses_")+hexLen+tailLen || !strings.HasPrefix(id, "ses_") {
		return false
	}
	body := id[len("ses_"):]
	for i := 0; i < hexLen; i++ {
		c := body[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	for i := hexLen; i < hexLen+tailLen; i++ {
		c := body[i]
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// rewriteOpenCodeUserAgent runs only after the request host is already known
// to be OpenCode. It does not parse the URL again: the caller made that check.
func rewriteOpenCodeUserAgent(req *http.Request, agent string) {
	if req == nil {
		return
	}
	ua := strings.TrimSpace(req.Header.Get("User-Agent"))
	if ua == "" {
		ua = strings.TrimSpace(agent)
	}
	if !strings.EqualFold(ua, "opencode") {
		return
	}
	req.Header.Set("User-Agent", "opencode/"+OpenCodeClientVersion)
}

// SetOpenCodeSessionHeaderIfNeeded stamps x-opencode-session on requests to
// OpenCode hosts. An existing official id wins. sessionID, then the request
// context, then a fresh ses_ id are used as fallbacks.
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
// A direct OpenCode host also receives the official client User-Agent when the
// saved identity is the opencode preset, and a session id in the ses_ shape.
// Hub-managed requests keep the affinity id and the saved User-Agent.
func ApplyOpenCodeSessionHeader(req *http.Request, cfg MaclawLLMConfig) {
	if req == nil {
		return
	}
	openCodeHost := req.URL != nil && IsOpenCodeHost(req.URL.Hostname())
	if !openCodeHost && !ShouldAttachOpenCodeSession(cfg) {
		return
	}
	existing := strings.TrimSpace(req.Header.Get(OpenCodeSessionHeader))
	switch {
	case existing == "":
		sessionID := strings.TrimSpace(cfg.SessionID)
		if sessionID == "" {
			sessionID = OpenCodeSessionIDFromContext(req.Context())
		}
		if openCodeHost {
			sessionID = openCodeWireSessionID(sessionID)
		} else {
			sessionID = ResolveOpenCodeSessionID(sessionID)
		}
		req.Header.Set(OpenCodeSessionHeader, sessionID)
	case openCodeHost && !openCodeFreeTierSessionID(existing):
		req.Header.Set(OpenCodeSessionHeader, openCodeWireSessionID(existing))
	}
	if openCodeHost {
		agent := ""
		if strings.TrimSpace(req.Header.Get("User-Agent")) == "" {
			agent = cfg.UserAgent()
		}
		rewriteOpenCodeUserAgent(req, agent)
		if strings.TrimSpace(req.Header.Get("x-opencode-client")) == "" {
			req.Header.Set("x-opencode-client", "cli")
		}
	}
}

func openCodeModelID(model string) string {
	model = strings.TrimSpace(model)
	const prefix = "opencode/"
	if len(model) > len(prefix) && strings.EqualFold(model[:len(prefix)], prefix) {
		model = strings.TrimSpace(model[len(prefix):])
	}
	return strings.ToLower(model)
}

// IsOpenCodeZenFreeModel reports Zen catalog models that the Go gateway does
// not serve. Metered ids such as glm-5.3-flash stay false. Go-native free ids
// also stay false so they are not pulled back onto Zen.
func IsOpenCodeZenFreeModel(model string) bool {
	switch openCodeModelID(model) {
	case "big-pickle", "glm-5.3-flash-free", "deepseek-v4.1-flash-free":
		return true
	case "cat-jam-free", "longcat-2.5-preview-free":
		return false
	default:
		return strings.HasSuffix(openCodeModelID(model), "-free")
	}
}

// OpenCodeZenAgentModel reports a Zen free model that rejects a request
// unless it streams and advertises the official bash and read tools.
// Every Zen-only free id is gated this way. Go-native free ids are not.
func OpenCodeZenAgentModel(model string) bool {
	if openCodeModelID(model) == "big-pickle" {
		return false
	}
	return IsOpenCodeZenFreeModel(model)
}

// OpenCodeUsesResponsesAPI reports Zen free models that reject Chat
// Completions and the Anthropic messages API. They answer on the Responses
// API when the request streams and includes the official bash and read tools.
func OpenCodeUsesResponsesAPI(rawURL, model string) bool {
	if !IsOpenCodeURL(rawURL) {
		return false
	}
	return openCodeModelID(model) == "muse-spark-1.2-contributor-free"
}

// OpenCodeChatEndpoint sends Zen-only free models to /zen/v1 and every other
// OpenCode model to /zen/go/v1. URLs that are not OpenCode are returned as given.
func OpenCodeChatEndpoint(rawURL, model string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" || !IsOpenCodeHost(parsed.Hostname()) {
		return rawURL
	}
	path := parsed.EscapedPath()
	const zenPrefix = "/zen/v1"
	const goPrefix = "/zen/go/v1"
	free := IsOpenCodeZenFreeModel(model)
	switch {
	case free && (path == goPrefix || strings.HasPrefix(path, goPrefix+"/")):
		parsed.Path = zenPrefix + strings.TrimPrefix(path, goPrefix)
	case !free && (path == zenPrefix || strings.HasPrefix(path, zenPrefix+"/")):
		parsed.Path = goPrefix + strings.TrimPrefix(path, zenPrefix)
	default:
		return rawURL
	}
	parsed.RawPath = ""
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}
	return parsed.String()
}

// OpenCodeUpstreamBase is the SDK base URL for model. A chat completions
// suffix is removed so the client appends it once.
func OpenCodeUpstreamBase(rawURL, model string) string {
	endpoint := strings.TrimRight(OpenCodeChatEndpoint(rawURL, model), "/")
	endpoint = strings.TrimSuffix(endpoint, "/chat/completions")
	return strings.TrimRight(endpoint, "/")
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
