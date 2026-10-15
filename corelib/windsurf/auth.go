package windsurf

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
}

func defaultEndpoints() serviceEndpoints {
	return serviceEndpoints{website: Website, register: RegisterAPI, webBackend: WebBackendAPI}
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
// sk-ws API key, or the backup auth token from the account page.
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
	case looksLikeJWT(token):
		return registerUser(ctx, ep, token)
	default:
		if len(token) < 20 {
			return Session{}, fmt.Errorf("无法识别的 Devin 授权令牌")
		}
		return Session{APIKey: token, APIServerURL: DefaultAPIServer}, nil
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
	if server == "" {
		server = DefaultAPIServer
	}
	return Session{APIKey: key, Name: strings.TrimSpace(parsed.Name), APIServerURL: strings.TrimRight(server, "/")}, nil
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
	server := DefaultAPIServer
	if strings.HasPrefix(sessionToken, devinSessionPrefix) {
		server = SelfServeAPIServer
	}
	return Session{APIKey: sessionToken, Name: strings.TrimSpace(parsed.Name), APIServerURL: server}, nil
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

// CanonicalAPIServer keeps a Devin API host and fills the IDE default when
// the saved URL is empty or points somewhere else.
func CanonicalAPIServer(apiKey, raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if isDevinAPIServer(raw) {
		return raw
	}
	if strings.HasPrefix(strings.TrimSpace(apiKey), devinSessionPrefix) {
		return SelfServeAPIServer
	}
	return DefaultAPIServer
}

func isDevinAPIServer(raw string) bool {
	if !strings.HasPrefix(raw, "https://") {
		return false
	}
	host := strings.TrimPrefix(raw, "https://")
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	return host == "server.codeium.com" || host == "server-beta.codeium.com" ||
		strings.HasSuffix(host, ".windsurf.com") || strings.HasSuffix(host, ".codeium.com")
}
