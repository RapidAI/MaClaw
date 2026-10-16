package lobsterai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

const exchangeAttempts = 3

// apiBaseTestOverride replaces the exchange/refresh/model base in tests. It
// is a same-package test seam — production code must leave it empty so the
// compile-time APIBase wins.
var apiBaseTestOverride string

func exchangeBase() string {
	if base := strings.TrimSpace(apiBaseTestOverride); base != "" {
		return strings.TrimRight(base, "/")
	}
	return APIBase
}

// LoginSession carries one pending browser approval. The local redirect
// listener parks the authorization code; Resolve exchanges it.
type LoginSession struct {
	State        string
	UUID         string
	FirstKeyfrom string // epoch millis string, kept lifetime-long upstream
	RedirectURI  string
	LoginURL     string

	mu     sync.Mutex
	code   string
	err    error
	done   bool
	notify chan struct{}
}

// Wait blocks until the browser redirect lands or the flow ends.
func (s *LoginSession) Wait(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("没有正在进行的 LobsterAI 登录")
	}
	select {
	case <-s.notify:
	case <-ctx.Done():
		return "", fmt.Errorf("LobsterAI 登录已取消或超时: %w", ctx.Err())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	if strings.TrimSpace(s.code) == "" {
		return "", errors.New("LobsterAI 登录回调未携带授权码")
	}
	return s.code, nil
}

// Cancel stops a pending login so Wait unblocks immediately.
func (s *LoginSession) Cancel() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	s.err = errors.New("LobsterAI 登录已取消")
	close(s.notify)
}

func (s *LoginSession) deliver(code, reject string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	if strings.TrimSpace(reject) != "" {
		s.err = errors.New(reject)
	} else {
		s.code = strings.TrimSpace(code)
	}
	s.done = true
	close(s.notify)
}

// Token is a LobsterAI access credential. RefreshToken rotates on refresh.
type Token struct {
	AccessToken   string
	RefreshToken  string
	ExpiresAt     int64 // unix seconds; falls back to the JWT exp claim
	UUID          string
	FirstKeyfrom  string
	LatestKeyfrom string
	UserID        string
	Nickname      string
}

// StartLogin binds a local redirect listener and returns the session plus the
// portal URL to open. port <= 0 picks a free ephemeral port; when a fixed
// port is requested and busy, the flow walks up a few ports.
func StartLogin(port int) (*LoginSession, error) {
	state, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("LobsterAI 登录初始化失败: %w", err)
	}
	sessionUUID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("LobsterAI 登录初始化失败: %w", err)
	}
	session := &LoginSession{
		State:        state,
		UUID:         sessionUUID,
		FirstKeyfrom: unixMillis(),
		notify:       make(chan struct{}),
	}
	listener, callbackPort, err := listenCallback(port, session)
	if err != nil {
		return nil, err
	}
	go serveLoop(listener, session)
	go func() {
		<-session.notify
		listener.Close()
	}()
	session.RedirectURI = fmt.Sprintf("http://127.0.0.1:%d%s", callbackPort, CallbackPath)
	session.LoginURL = BuildLoginURL(session.RedirectURI, session.State)
	return session, nil
}

// listenCallback binds the redirect port (walking up a few when occupied).
func listenCallback(port int, session *LoginSession) (net.Listener, int, error) {
	if port <= 0 {
		port = 0 // ephemeral
	}
	var raw net.Listener
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		raw, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+attempt))
		if err == nil {
			break
		}
		if port == 0 || !isAddrInUse(err) {
			return nil, 0, fmt.Errorf("LobsterAI 本地回调端口无法监听: %w", err)
		}
	}
	if raw == nil {
		return nil, 0, fmt.Errorf("LobsterAI 本地回调端口 %d-%d 均被占用", port, port+7)
	}
	callbackPort := 0
	if tcp, ok := raw.(*net.TCPListener); ok {
		callbackPort = tcp.Addr().(*net.TCPAddr).Port
	}
	return raw, callbackPort, nil
}

func isAddrInUse(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "address already in use") ||
		strings.Contains(text, "only one usage of each socket") ||
		strings.Contains(text, "10048")
}

// BuildLoginURL mirrors the desktop client's portal deep link: the SPA
// validates redirect_uri stays on 127.0.0.1 and echoes the state.
func BuildLoginURL(redirectURI, state string) string {
	return strings.TrimRight(PortalBase, "/") + LoginPath +
		"&redirect_uri=" + url.QueryEscape(redirectURI) +
		"&state=" + url.QueryEscape(state)
}

// serveLoop answers redirects on the session listener until it ends.
func serveLoop(raw net.Listener, session *LoginSession) {
	for {
		conn, err := raw.Accept()
		if err != nil {
			return
		}
		go serveConn(conn, session)
	}
}

// serveConn parses one browser redirect; requests without code+state are
// noise. The page answer is neutral — the exchange result surfaces in-app.
func serveConn(conn net.Conn, session *LoginSession) {
	defer conn.Close()
	requestLine, err := readLine(conn)
	if err != nil {
		return
	}
	method, target := splitRequestLine(requestLine)
	if method == "" || target == "" {
		writePage(conn)
		return
	}
	rawURL := "http://127.0.0.1" + queryOf(target)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		writePage(conn)
		return
	}
	query := parsed.Query()
	code := strings.TrimSpace(query.Get("code"))
	state := strings.TrimSpace(query.Get("state"))
	reject := ""
	switch {
	case strings.TrimSpace(query.Get("error")) != "":
		reject = "授权被拒绝: " + query.Get("error")
	case code == "" && state == "":
		// preflight/probe traffic; keep the listener open.
		writePage(conn)
		return
	case code == "":
		reject = "登录回调缺少授权码"
	case !strings.EqualFold(state, session.State):
		reject = "回调与本机发起的登录不匹配（state 校验失败），已拒绝；请重新发起登录"
	}
	session.deliver(code, reject)
	writePage(conn)
}

func readLine(conn net.Conn) (string, error) {
	buf := make([]byte, 8192)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			break
		}
		if idx := bytes.Index(buf[:total], []byte("\r\n")); idx >= 0 {
			return string(buf[:idx]), nil
		}
	}
	if total == 0 {
		return "", io.EOF
	}
	return string(buf[:total]), nil
}

func splitRequestLine(line string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(line))
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func queryOf(target string) string {
	if idx := strings.Index(target, "?"); idx >= 0 {
		return target[idx:]
	}
	return ""
}

func writePage(conn net.Conn) {
	const body = `<!doctype html><meta charset="utf-8"><title>登录进度</title>` +
		`<body style="font-family:system-ui,'Segoe UI',sans-serif;max-width:420px;margin:64px auto;text-align:center">` +
		`<h2 style="font-weight:600;font-size:18px">登录信息已接收</h2>` +
		`<p style="color:#555;font-size:14px">正在换取登录凭证，请回到应用等待登录结果。</p></body>`
	payload := []byte(body)
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(payload)) +
		"Cache-Control: no-store\r\n" +
		"Access-Control-Allow-Origin: *\r\n" +
		"Connection: close\r\n\r\n" + string(payload)))
}

// Resolve exchanges the waited-for code into a Token. Identity fields ride
// along for the later refresh.
func (s *LoginSession) Resolve(ctx context.Context, code string) (*Token, error) {
	return exchangeAuthCode(ctx, code, s.UUID, s.FirstKeyfrom)
}

// exchangeAuthCode trades the portal code for the account token pair.
func exchangeAuthCode(ctx context.Context, code, sessionUUID, firstKeyfrom string) (*Token, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("授权码为空，请重新登录")
	}
	return postJSON(ctx, exchangeBase()+ExchangePath, map[string]any{
		"authCode":      strings.TrimSpace(code),
		"firstKeyfrom":  strings.TrimSpace(firstKeyfrom),
		"latestKeyfrom": unixMillis(),
		"uuid":          strings.TrimSpace(sessionUUID),
		"version":       ClientVersion,
	})
}

// Refresh renews the access token. expiresIn comes back in seconds; zero
// falls back to the JWT exp claim.
func Refresh(ctx context.Context, refreshToken, sessionUUID, firstKeyfrom, userID string) (*Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh_token 为空，请重新登录")
	}
	var lastErr error
	for attempt := 0; attempt < exchangeAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, time.Duration(attempt)*time.Second); err != nil {
				return nil, err
			}
		}
		body := map[string]any{
			"refreshToken":  refreshToken,
			"firstKeyfrom":  strings.TrimSpace(firstKeyfrom),
			"latestKeyfrom": unixMillis(),
			"version":       ClientVersion,
		}
		if strings.TrimSpace(sessionUUID) != "" {
			body["uuid"] = strings.TrimSpace(sessionUUID)
		}
		if strings.TrimSpace(userID) != "" {
			body["userId"] = strings.TrimSpace(userID)
		}
		token, err := postJSON(ctx, exchangeBase()+RefreshPath, body)
		if err != nil {
			var stale *upstreamRejection
			if errors.As(err, &stale) {
				return nil, fmt.Errorf("LobsterAI 登录已失效，请重新登录: %s", stale.Message)
			}
			lastErr = err
			continue
		}
		if strings.TrimSpace(token.RefreshToken) == "" {
			token.RefreshToken = refreshToken
		}
		if strings.TrimSpace(token.UUID) == "" {
			token.UUID = strings.TrimSpace(sessionUUID)
		}
		if strings.TrimSpace(token.FirstKeyfrom) == "" {
			token.FirstKeyfrom = strings.TrimSpace(firstKeyfrom)
		}
		return token, nil
	}
	if lastErr == nil {
		lastErr = errors.New("刷新 LobsterAI 登录失败")
	}
	return nil, lastErr
}

// ListModels reads the remote catalog; the caller falls back to Allowlist on
// failure.
func ListModels(ctx context.Context, accessToken string) ([]UpstreamModel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, errors.New("需要已登录的访问令牌")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint := APIBase + ModelsPath + "?keyfrom=" + url.QueryEscape(unixMillis())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-LobsterAI-Client-Capabilities", ClientCapabilities)
	req.Header.Set("X-LobsterAI-Client-Version", ClientVersion)
	// The catalog rides the shared proxy-aware OAuth client so the MaClaw
	// LLM-scope proxy config applies here too.
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, summarizeBody(raw))
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, errors.New("模型目录响应无法解析")
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("模型目录 code=%d %s", envelope.Code, envelope.Msg)
	}
	var entries []struct {
		ModelID   string `json:"modelId"`
		ModelName string `json:"modelName"`
	}
	if err := json.Unmarshal(envelope.Data, &entries); err != nil {
		return nil, fmt.Errorf("模型目录响应无法解析: %w", err)
	}
	models := make([]UpstreamModel, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ModelID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(entry.ModelName)
		if name == "" {
			name = id
		}
		models = append(models, UpstreamModel{ID: id, Name: name})
	}
	return models, nil
}

// upstreamRejection marks a refusal retrying cannot fix.
type upstreamRejection struct {
	Message string
}

func (e *upstreamRejection) Error() string { return e.Message }

func postJSON(ctx context.Context, endpoint string, body any) (*Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		// Transient upstream problems (5xx, rate limiting) keep a plain error
		// so the refresh loop retries instead of demanding a re-login.
		return nil, fmt.Errorf("LobsterAI 上游暂时不可用 (HTTP %d): %s", resp.StatusCode, summarizeBody(data))
	}
	if resp.StatusCode >= 400 {
		return nil, &upstreamRejection{Message: fmt.Sprintf("HTTP %d %s", resp.StatusCode, summarizeBody(data))}
	}
	var envelope struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("登录响应无法解析: %w", err)
	}
	if envelope.Code != 0 {
		return nil, &upstreamRejection{Message: fmt.Sprintf("code=%d %s", envelope.Code, envelope.Msg)}
	}
	var payload struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		User         struct {
			ID       string `json:"id"`
			Yid      string `json:"yid"`
			UserID   string `json:"userId"`
			Nickname string `json:"nickname"`
		} `json:"user"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return nil, fmt.Errorf("登录响应无法解析: %w", err)
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, errors.New("登录响应缺少 accessToken")
	}
	token := &Token{
		AccessToken:   strings.TrimSpace(payload.AccessToken),
		RefreshToken:  strings.TrimSpace(payload.RefreshToken),
		ExpiresAt:     time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).Unix(),
		UUID:          strings.TrimSpace(lookupString(envelope.Data, "uuid")),
		FirstKeyfrom:  strings.TrimSpace(lookupString(envelope.Data, "firstKeyfrom")),
		LatestKeyfrom: strings.TrimSpace(lookupString(envelope.Data, "latestKeyfrom")),
		UserID:        firstNonEmpty(payload.User.ID, payload.User.UserID, payload.User.Yid),
		Nickname:      strings.TrimSpace(payload.User.Nickname),
	}
	if payload.ExpiresIn <= 0 {
		if exp := jwtExpiry(token.AccessToken); exp > 0 {
			token.ExpiresAt = exp
		}
	}
	return token, nil
}

// lookupString fishes one top-level string out of an exchange payload.
func lookupString(raw json.RawMessage, key string) string {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if text, ok := payload[key].(string); ok {
		return text
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// jwtExpiry decodes the access token's exp claim (unix seconds); 0 when the
// token is not a readable JWT.
func jwtExpiry(token string) int64 {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return 0
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return 0
	}
	return claims.Exp
}

func summarizeBody(body []byte) string {
	runes := []rune(strings.TrimSpace(string(body)))
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return strings.TrimSpace(string(runes))
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:]), nil
}

func unixMillis() string {
	return fmt.Sprintf("%d", time.Now().UnixMilli())
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
