package windsurf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// ClientID is the public Devin Desktop / Windsurf IDE OAuth client.
	ClientID = "3GUryQ7ldAeKEuD2obYnppsnmj58eP5u"
	// Website is the account site the installed Devin IDE opens.
	Website = "https://windsurf.com"
	// RegisterAPI exchanges a Firebase ID token for an API key.
	RegisterAPI = "https://register.windsurf.com"
	// WebBackendAPI serves Devin Auth1 session exchange.
	WebBackendAPI = "https://web-backend.windsurf.com"
	// DefaultAPIServer is the API host the installed IDE uses by default.
	DefaultAPIServer = "https://server.codeium.com"
	// SelfServeAPIServer serves Devin session tokens.
	SelfServeAPIServer = "https://server.self-serve.windsurf.com"
	// TokenPage is where the IDE's backup login shows an auth token.
	TokenPage = "https://windsurf.com/show-auth-token"

	devinSessionPrefix = "devin-session-token$"
	seatService        = "exa.seat_management_pb.SeatManagementService"
)

// Session is a Devin account credential the proxy can call with.
type Session struct {
	APIKey       string
	Name         string
	APIServerURL string
}

type serviceEndpoints struct {
	website    string
	register   string
	webBackend string
	apiServer  string
}

func defaultEndpoints() serviceEndpoints {
	return serviceEndpoints{website: Website, register: RegisterAPI, webBackend: WebBackendAPI, apiServer: DefaultAPIServer}
}

// LoginURL is the Devin account page the installed IDE opens for a normal login.
// redirectURI receives the Firebase token as a query parameter.
func LoginURL(redirectURI, state string) string {
	return loginURL(Website, redirectURI, state)
}

func loginURL(website, redirectURI, state string) string {
	q := url.Values{}
	q.Set("response_type", "token")
	q.Set("client_id", ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("redirect_parameters_type", "query")
	return strings.TrimRight(website, "/") + "/devin/account/login?" + q.Encode()
}

// ResolveToken turns a pasted Devin or Windsurf credential into an API session.
// Accepted forms match the IDE: Firebase ID token, Auth1 token, devin-session-token,
// sk-ws API key, or the account-page code that ExchangeDevinCode turns into a session.
func ResolveToken(ctx context.Context, token string) (Session, error) {
	return resolveToken(ctx, defaultEndpoints(), token)
}

func resolveToken(ctx context.Context, ep serviceEndpoints, token string) (Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Session{}, fmt.Errorf("请粘贴 Devin 授权令牌")
	}
	switch {
	case strings.HasPrefix(token, "sk-ws-"):
		return Session{APIKey: token, APIServerURL: DefaultAPIServer}, nil
	case strings.HasPrefix(token, devinSessionPrefix):
		return Session{APIKey: token, APIServerURL: SelfServeAPIServer}, nil
	case strings.HasPrefix(token, "auth1_"):
		return exchangeAuth1(ctx, ep, token)
	case strings.HasPrefix(token, "ott$"), strings.HasPrefix(token, "oidc$"), looksLikeJWT(token):
		return registerUser(ctx, ep, token)
	default:
		if len(token) < 20 {
			return Session{}, fmt.Errorf("无法识别的 Devin 授权令牌")
		}
		// The Devin account page returns a short code. The installed IDE
		// exchanges it for a devin-session-token instead of using it as an API key.
		return exchangeDevinCode(ctx, ep, token)
	}
}

func looksLikeJWT(token string) bool {
	parts := strings.Split(token, ".")
	return len(parts) == 3 && strings.HasPrefix(parts[0], "eyJ") && parts[1] != "" && parts[2] != ""
}

func registerUser(ctx context.Context, ep serviceEndpoints, firebaseToken string) (Session, error) {
	body, _ := json.Marshal(map[string]string{"firebase_id_token": firebaseToken})
	endpoint := strings.TrimRight(ep.register, "/") + "/" + seatService + "/RegisterUser"
	status, raw, err := postJSON(ctx, endpoint, body, nil)
	if err != nil {
		return Session{}, fmt.Errorf("Devin 注册登录失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return Session{}, fmt.Errorf("Devin 注册登录失败: %s", connectError(status, raw))
	}
	var parsed struct {
		APIKey       string `json:"api_key"`
		APIKeyCamel  string `json:"apiKey"`
		Name         string `json:"name"`
		APIServerURL string `json:"api_server_url"`
		APIServerAlt string `json:"apiServerUrl"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Session{}, fmt.Errorf("Devin 注册登录返回了无法识别的内容")
	}
	key := firstNonEmpty(parsed.APIKey, parsed.APIKeyCamel)
	if key == "" {
		return Session{}, fmt.Errorf("Devin 注册登录未返回 API Key")
	}
	server := firstNonEmpty(parsed.APIServerURL, parsed.APIServerAlt)
	return Session{APIKey: key, Name: strings.TrimSpace(parsed.Name), APIServerURL: CanonicalAPIServer(key, server)}, nil
}

func exchangeAuth1(ctx context.Context, ep serviceEndpoints, auth1 string) (Session, error) {
	body, _ := json.Marshal(map[string]string{"auth1Token": auth1, "orgId": ""})
	endpoint := strings.TrimRight(ep.webBackend, "/") + "/" + seatService + "/WindsurfPostAuth"
	headers := map[string]string{"X-Devin-Auth1-Token": auth1}
	status, raw, err := postJSON(ctx, endpoint, body, headers)
	if err != nil {
		return Session{}, fmt.Errorf("Devin 登录失败: %w", err)
	}
	if status < 200 || status >= 300 {
		return Session{}, fmt.Errorf("Devin 登录失败: %s", connectError(status, raw))
	}
	var parsed struct {
		SessionToken    string `json:"sessionToken"`
		SessionTokenAlt string `json:"session_token"`
		Name            string `json:"name"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Session{}, fmt.Errorf("Devin 登录返回了无法识别的内容")
	}
	sessionToken := firstNonEmpty(parsed.SessionToken, parsed.SessionTokenAlt)
	if sessionToken == "" {
		return Session{}, fmt.Errorf("Devin 登录未返回会话")
	}
	return Session{APIKey: sessionToken, Name: strings.TrimSpace(parsed.Name), APIServerURL: CanonicalAPIServer(sessionToken, "")}, nil
}

func postJSON(ctx context.Context, endpoint string, body []byte, headers map[string]string) (int, []byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, raw, nil
}

func connectError(status int, raw []byte) string {
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		msg := firstNonEmpty(parsed.Message, parsed.Error.Message)
		if msg != "" {
			return msg
		}
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fmt.Sprintf("HTTP %d", status)
	}
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// TokenFromPaste accepts a raw Devin credential or a callback URL copied from
// the browser. A URL contributes only its auth parameter, never the whole address.
func TokenFromPaste(raw string) (string, error) {
	raw = strings.Trim(strings.TrimSpace(raw), "\"'`")
	if raw == "" {
		return "", fmt.Errorf("请粘贴 Devin 授权令牌")
	}
	if strings.Contains(raw, "://") {
		token, ok := tokenFromURL(raw)
		if !ok {
			return "", fmt.Errorf("这段内容里没有 Devin 授权令牌")
		}
		return token, nil
	}
	return raw, nil
}

func tokenFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", false
	}
	values := parsed.Query()
	if parsed.Fragment != "" {
		if fragment, err := url.ParseQuery(parsed.Fragment); err == nil {
			for key, items := range fragment {
				if len(values[key]) == 0 {
					values[key] = items
				}
			}
		}
	}
	token := firstNonEmpty(values.Get("firebase_id_token"), values.Get("access_token"), values.Get("token"), values.Get("id_token"), values.Get("devin_code"), values.Get("code"))
	return token, token != ""
}

// CanonicalAPIServer keeps a Devin API host and fills the IDE default when
// the saved URL is empty or points somewhere else. A devin-session-token only
// works on the self-serve host; server.codeium.com rejects it as an invalid key.
func CanonicalAPIServer(apiKey, raw string) string {
	apiKey = strings.TrimSpace(apiKey)
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if devinSessionKey(apiKey) && (raw == "" || isCodeiumAPIServer(raw)) {
		return SelfServeAPIServer
	}
	if remembered := notedDevinHost(apiKey); remembered != "" && (raw == "" || isDevinAPIServer(raw)) {
		return remembered
	}
	if isDevinAPIServer(raw) {
		return raw
	}
	if devinSessionKey(apiKey) {
		return SelfServeAPIServer
	}
	return DefaultAPIServer
}

// routeDevinAPIServer sends a devin-session-token away from server.codeium.com.
// A host learned from a successful call wins over the saved codeium URL.
// Any other host, including a test server, is left unchanged.
func routeDevinAPIServer(apiKey, raw string) string {
	apiKey = strings.TrimSpace(apiKey)
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if devinSessionKey(apiKey) && (raw == "" || isCodeiumAPIServer(raw)) {
		return SelfServeAPIServer
	}
	if remembered := notedDevinHost(apiKey); remembered != "" && (raw == "" || isDevinAPIServer(raw)) {
		return remembered
	}
	if raw == "" {
		return CanonicalAPIServer(apiKey, raw)
	}
	return raw
}

func devinSessionKey(apiKey string) bool {
	return strings.HasPrefix(strings.TrimSpace(apiKey), devinSessionPrefix)
}

// NotedAPIServer is the official Devin host that accepted this key.
// An empty result means the key has not succeeded against an official host yet.
func NotedAPIServer(apiKey string) string {
	return notedDevinHost(strings.TrimSpace(apiKey))
}

// RememberDevinAPIServer records an official host that accepted the key.
// Test servers and lookalike hosts are ignored.
func RememberDevinAPIServer(apiKey, server string) {
	noteDevinHost(apiKey, server)
}

var (
	devinHostMu    sync.Mutex
	devinHostCache = map[string]string{}
	devinMintCache = map[string]string{}
	// devinFallbackHook replaces the official-host retry in tests.
	devinFallbackHook func(server string) (string, bool)
)

func resetDevinHosts() {
	devinHostMu.Lock()
	devinHostCache = map[string]string{}
	devinMintCache = map[string]string{}
	devinHostMu.Unlock()
}

func devinKeyID(apiKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(apiKey)))
	return hex.EncodeToString(sum[:])
}

func noteDevinHost(apiKey, server string) {
	apiKey = strings.TrimSpace(apiKey)
	server = strings.TrimRight(strings.TrimSpace(server), "/")
	if apiKey == "" || !isDevinAPIServer(server) {
		return
	}
	devinHostMu.Lock()
	devinHostCache[devinKeyID(apiKey)] = server
	devinHostMu.Unlock()
}

func notedDevinHost(apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}
	devinHostMu.Lock()
	defer devinHostMu.Unlock()
	return devinHostCache[devinKeyID(apiKey)]
}

// MintedSession is the devin-session-token exchanged from an API key.
// The boolean is false when this key is already a session token or has not
// been exchanged yet.
func MintedSession(apiKey string) (string, bool) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || devinSessionKey(apiKey) {
		return "", false
	}
	devinHostMu.Lock()
	defer devinHostMu.Unlock()
	token := devinMintCache[devinKeyID(apiKey)]
	return token, devinSessionKey(token)
}

func rememberMintedSession(apiKey, token string) {
	apiKey = strings.TrimSpace(apiKey)
	token = strings.TrimSpace(token)
	if apiKey == "" || !devinSessionKey(token) {
		return
	}
	devinHostMu.Lock()
	devinMintCache[devinKeyID(apiKey)] = token
	devinHostMu.Unlock()
	noteDevinHost(token, SelfServeAPIServer)
}

// resolveAccess turns a RegisterUser API key into the session token the
// installed Devin IDE uses. Session tokens and test servers are left as-is.
func resolveAccess(ctx context.Context, apiServer, apiKey string) (string, string) {
	apiKey = strings.TrimSpace(apiKey)
	routed := routeDevinAPIServer(apiKey, apiServer)
	if apiKey == "" || devinSessionKey(apiKey) || !isDevinAPIServer(routed) {
		return routed, apiKey
	}
	if looksLikeDevinCode(apiKey) {
		session, err := exchangeDevinCode(ctx, defaultEndpoints(), apiKey)
		if err == nil && devinSessionKey(session.APIKey) {
			rememberMintedSession(apiKey, session.APIKey)
			return SelfServeAPIServer, session.APIKey
		}
		return routed, apiKey
	}
	if token, ok := MintedSession(apiKey); ok {
		return SelfServeAPIServer, token
	}
	token, err := mintDevinSession(ctx, routed, apiKey)
	if err != nil || !devinSessionKey(token) {
		return routed, apiKey
	}
	rememberMintedSession(apiKey, token)
	return SelfServeAPIServer, token
}

func mintDevinSession(ctx context.Context, apiServer, apiKey string) (string, error) {
	apiServer = strings.TrimRight(strings.TrimSpace(apiServer), "/")
	apiKey = strings.TrimSpace(apiKey)
	if apiServer == "" || apiKey == "" {
		return "", fmt.Errorf("请先完成 Devin 登录")
	}
	body := encodeBytes(1, buildMetadata(apiKey, "", newID(), uint64(time.Now().UnixMilli()), newID()))
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoint := apiServer + "/" + seatService + "/GetSelfDevinSessionToken"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	setDevinHeaders(req, apiKey, "application/proto")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s", connectError(resp.StatusCode, raw))
	}
	token := sessionTokenFromBody(raw)
	if !devinSessionKey(token) {
		return "", fmt.Errorf("Devin 未返回会话令牌")
	}
	return token, nil
}

func looksLikeDevinCode(token string) bool {
	token = strings.TrimSpace(token)
	if len(token) < 20 || devinSessionKey(token) || looksLikeJWT(token) {
		return false
	}
	for _, prefix := range []string{"sk-ws-", "auth1_", "ott$", "oidc$"} {
		if strings.HasPrefix(token, prefix) {
			return false
		}
	}
	return true
}

func exchangeDevinCode(ctx context.Context, ep serviceEndpoints, code string) (Session, error) {
	code = strings.TrimSpace(code)
	var last error
	for _, server := range devinCodeServers(ep) {
		token, err := postDevinCode(ctx, server, code)
		if err != nil {
			last = err
			continue
		}
		name := currentUserName(ctx, server, token)
		if name == "" && server == DefaultAPIServer {
			name = currentUserName(ctx, SelfServeAPIServer, token)
		}
		return Session{APIKey: token, Name: name, APIServerURL: CanonicalAPIServer(token, "")}, nil
	}
	if last == nil {
		last = fmt.Errorf("Devin 登录未返回会话")
	}
	return Session{}, fmt.Errorf("Devin 登录失败: %w", last)
}

func devinCodeServers(ep serviceEndpoints) []string {
	server := strings.TrimRight(strings.TrimSpace(ep.apiServer), "/")
	if server == "" {
		server = DefaultAPIServer
	}
	if server == DefaultAPIServer {
		return []string{DefaultAPIServer, SelfServeAPIServer}
	}
	return []string{server}
}

func postDevinCode(ctx context.Context, apiServer, code string) (string, error) {
	apiServer = strings.TrimRight(strings.TrimSpace(apiServer), "/")
	body := encodeString(1, code)
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	endpoint := apiServer + "/" + seatService + "/ExchangeDevinCode"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s", connectError(resp.StatusCode, raw))
	}
	token := sessionTokenFromBody(raw)
	if !devinSessionKey(token) {
		return "", fmt.Errorf("Devin 登录未返回会话")
	}
	return token, nil
}

func currentUserName(ctx context.Context, apiServer, sessionToken string) string {
	apiServer = strings.TrimRight(strings.TrimSpace(apiServer), "/")
	sessionToken = strings.TrimSpace(sessionToken)
	if apiServer == "" || sessionToken == "" {
		return ""
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	endpoint := apiServer + "/" + seatService + "/GetCurrentUserName"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(nil))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/proto")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("X-Auth-Token", sessionToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "{") {
		var parsed struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &parsed) == nil {
			return strings.TrimSpace(parsed.Name)
		}
	}
	fields, err := iterFields(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fieldString(fields, 1))
}

func sessionTokenFromBody(raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "{") {
		var parsed struct {
			SessionToken    string `json:"sessionToken"`
			SessionTokenAlt string `json:"session_token"`
		}
		if json.Unmarshal(raw, &parsed) == nil {
			return firstNonEmpty(parsed.SessionToken, parsed.SessionTokenAlt)
		}
	}
	fields, err := iterFields(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fieldString(fields, 1))
}

func devinHostFallback(server string) (string, bool) {
	if devinFallbackHook != nil {
		return devinFallbackHook(server)
	}
	return officialDevinHostFallback(server)
}

func officialDevinHostFallback(server string) (string, bool) {
	switch {
	case isCodeiumAPIServer(server):
		return SelfServeAPIServer, true
	case devinAPIHost(server) == "server.self-serve.windsurf.com":
		return DefaultAPIServer, true
	default:
		return "", false
	}
}

func devinAuthRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid api key") || strings.Contains(msg, "invalid_api_key") || strings.Contains(msg, "unauthenticated")
}

func isCodeiumAPIServer(raw string) bool {
	host := devinAPIHost(raw)
	return host == "server.codeium.com" || host == "server-beta.codeium.com"
}

func isDevinAPIServer(raw string) bool {
	host := devinAPIHost(raw)
	if host == "" {
		return false
	}
	return host == "server.codeium.com" || host == "server-beta.codeium.com" ||
		strings.HasSuffix(host, ".windsurf.com") || strings.HasSuffix(host, ".codeium.com")
}

func devinAPIHost(raw string) string {
	if !strings.HasPrefix(raw, "https://") {
		return ""
	}
	host := strings.TrimPrefix(raw, "https://")
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	return strings.ToLower(host)
}
