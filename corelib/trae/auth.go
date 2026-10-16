package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

const exchangeAttempts = 3

// LoginSession carries one pending browser approval. The authorization URL
// and the token exchange share the machine/device identity, so the later
// exchange cannot disagree with the login page about the device.
type LoginSession struct {
	TraceID   string
	MachineID string
	DeviceID  string

	mu      sync.Mutex
	details *CallbackInfo
	err     error
	done    bool
	notify  chan struct{}
}

// Wait blocks until the browser redirect lands, the flow is cancelled, or ctx
// ends. It returns the callback contents for Resolve to exchange.
func (s *LoginSession) Wait(ctx context.Context) (*CallbackInfo, error) {
	if s == nil {
		return nil, errors.New("没有正在进行的 Trae 登录")
	}
	select {
	case <-s.notify:
	case <-ctx.Done():
		return nil, fmt.Errorf("Trae 登录已取消或超时: %w", ctx.Err())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	if s.details == nil {
		return nil, errors.New("Trae 登录回调未携带凭据")
	}
	return s.details, nil
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
	s.err = errors.New("Trae 登录已取消")
	close(s.notify)
}

// finish closes the pending session. Closing the channel wakes every waiter.
func (s *LoginSession) finish(details *CallbackInfo, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.details = details
	s.err = err
	s.done = true
	close(s.notify)
}

// CallbackInfo is the account view the authorization page hands back. One of
// AuthCode / RefreshToken / UserJwtToken carries the credential.
type CallbackInfo struct {
	AuthCode     string
	RefreshToken string
	UserJwtToken string
	Host         string
	UserRegion   string
	LoginTraceID string
	UserID       string
	EnterpriseID string
	DisplayName  string
	// DenyReason carries the console's own denial text (error /
	// error_description) when the user rejected the authorization.
	DenyReason string
}

// Token is a Trae access credential. RefreshToken rotates on refresh, so
// callers must persist both values.
type Token struct {
	AccessToken           string
	RefreshToken          string
	ExpiresAt             int64 // unix seconds; 0 when upstream stays silent
	RefreshTokenExpiresAt int64
	UserID                string
	EnterpriseID          string
	DisplayName           string
	MachineID             string // per-account stable device fingerprint
	DeviceID              string
}

// StartLogin builds a pending approval for the edition and starts the local
// redirect listener. The returned URL goes to the browser; Wait blocks until
// the redirect lands. port > 0 binds that port (walking up a few when
// occupied); port <= 0 picks an ephemeral port.
func StartLogin(profile Profile, port int) (*LoginSession, string, error) {
	if strings.TrimSpace(profile.ConsoleBase) == "" {
		return nil, "", errors.New("未知的 Trae 版本")
	}
	randoms := []string{randomHex(32), randomHex(16)}
	for i, value := range randoms {
		if value == "" {
			return nil, "", fmt.Errorf("Trae 登录初始化失败: 第 %d 个随机标识生成失败", i+1)
		}
	}
	traceID := randomHex(8)
	if traceID == "" {
		traceID = fmt.Sprintf("%x", time.Now().UnixNano())
	}
	session := &LoginSession{
		TraceID:   traceID,
		MachineID: randoms[0],
		DeviceID:  randoms[1],
		notify:    make(chan struct{}),
	}
	listener, callbackPort, err := listenCallback(port, session)
	if err != nil {
		return nil, "", err
	}
	loginURL := BuildAuthorizeURL(profile, LoginOptions{
		MachineID: session.MachineID,
		DeviceID:  session.DeviceID,
		TraceID:   session.TraceID,
		Port:      callbackPort,
	})
	// The console probes its fixed local port to confirm a native client is
	// online; best-effort serve it so the page does not stall on "认证中".
	if aux := listenProbePort(session); aux == nil {
		log.Printf("[trae] probe port %d busy; continuing with the callback port only", ProbePort)
	}
	go func() {
		<-session.notify
		listener.Close()
	}()
	return session, loginURL, nil
}

// LoginOptions is the device identity baked into the authorization URL.
type LoginOptions struct {
	MachineID string
	DeviceID  string
	TraceID   string
	Port      int
}

// BuildAuthorizeURL mirrors the simplified native form the working community
// runners exercise: 18 parameters and NO PKCE challenge. Carrying a
// code_challenge steers the console toward the newer AuthCode contract, whose
// exchange demands a registered device proof we cannot produce — the observed
// failures are 10101 "无效参数" (cn) and 20405 "Device proof required"
// (global). Without the challenge the console redirects back with a refresh
// token (or userJwt) that the legacy exchange accepts.
// Do not reshuffle the values without a fresh capture.
func BuildAuthorizeURL(profile Profile, options LoginOptions) string {
	port := options.Port
	if port <= 0 {
		port = CallbackPort
	}
	query := url.Values{}
	query.Set("login_version", "1")
	query.Set("auth_from", "solo")
	query.Set("login_channel", "native_ide")
	query.Set("plugin_version", LoginPluginVersion)
	query.Set("auth_type", "local")
	query.Set("client_id", profile.ClientID)
	query.Set("redirect", "0")
	query.Set("login_trace_id", options.TraceID)
	query.Set("auth_callback_url", fmt.Sprintf("http://127.0.0.1:%d%s", port, CallbackPath))
	query.Set("machine_id", options.MachineID)
	query.Set("device_id", options.DeviceID)
	query.Set("x_device_id", options.DeviceID)
	query.Set("x_machine_id", options.MachineID)
	query.Set("x_device_brand", "PC")
	query.Set("x_device_type", "PC")
	query.Set("x_os_version", "1.0")
	query.Set("x_app_version", profile.IDEVersion)
	query.Set("x_app_type", "stable")
	return strings.TrimRight(profile.ConsoleBase, "/") + AuthorizePath + "?" + query.Encode()
}

// IsApprovalURL reports whether raw is a console authorization page we are
// willing to open: HTTPS on a Trae console origin, or loopback HTTP for local
// test servers.
func IsApprovalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		switch host {
		case "www.trae.cn", "trae.cn", "www.trae.ai", "trae.ai":
			return true
		default:
			return false
		}
	case "http":
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

// listenCallback binds the redirect port (walking up a few when occupied) and
// serves the callback until the session ends.
func listenCallback(port int, session *LoginSession) (*net.TCPListener, int, error) {
	if port < 0 {
		port = 0
	}
	var raw net.Listener
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		raw, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+attempt))
		if err == nil {
			port += attempt
			break
		}
		if !isAddrInUse(err) {
			return nil, 0, fmt.Errorf("本地回调端口 %d 无法监听: %w", port, err)
		}
	}
	if raw == nil {
		return nil, 0, fmt.Errorf("本地回调端口 %d-%d 均被占用，无法监听授权回调", port, port+7)
	}
	listener, ok := raw.(*net.TCPListener)
	if !ok {
		listener = nil
		_ = raw.Close()
		return nil, 0, fmt.Errorf("本地回调监听类型异常")
	}
	// An ephemeral bind (port 0) reports its real port here; the login URL
	// must advertise exactly what is listening, or the console redirects to
	// the default port with nothing behind it.
	if port <= 0 {
		if addr, isTCP := listener.Addr().(*net.TCPAddr); isTCP {
			port = addr.Port
		}
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveCallbackConn(conn, session)
		}
	}()
	return listener, port, nil
}

func isAddrInUse(err error) bool {
	// Windows reports this as WSAEADDRINUSE; every platform renders it inside
	// the syscall error text, so a text probe is the portable answer.
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "address already in use") ||
		strings.Contains(text, "only one usage of each socket") ||
		strings.Contains(text, "10048")
}

// listenProbePort best-effort serves the console's fixed online probe.
func listenProbePort(session *LoginSession) *net.TCPListener {
	raw, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", ProbePort))
	if err != nil {
		return nil
	}
	tcpListener, ok := raw.(*net.TCPListener)
	if !ok {
		_ = raw.Close()
		return nil
	}
	listener := tcpListener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go respondPlain(conn)
		}
	}()
	go func() {
		<-session.notify
		listener.Close()
	}()
	return listener
}

// respondPlain answers the online probe with "ok" plus the CORS blanket the
// browser page demands. Text/plain keeps intermediaries from HTML-sniffing.
func respondPlain(conn net.Conn) {
	defer conn.Close()
	readRequestHead(conn)
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"Content-Length: 2\r\n" +
		"Cache-Control: no-store\r\n" +
		"Access-Control-Allow-Origin: *\r\n" +
		"Access-Control-Allow-Methods: GET, POST, OPTIONS\r\n" +
		"Access-Control-Allow-Headers: *\r\n" +
		"Connection: close\r\n\r\nok"))
}

func readRequestHead(conn net.Conn) {
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		n, err := conn.Read(buf)
		if err != nil || n == 0 {
			_ = conn.SetReadDeadline(time.Time{})
			return
		}
		if bytes.Contains(buf[:n], []byte("\r\n\r\n")) {
			_ = conn.SetReadDeadline(time.Time{})
			return
		}
	}
}

// serveCallbackConn parses one browser redirect. Requests without a
// credential parameter are the console's online probe, answered politely.
func serveCallbackConn(conn net.Conn, session *LoginSession) {
	defer conn.Close()
	requestLine, err := readCallbackHead(conn)
	if err != nil {
		return
	}
	method, target := splitRequestLine(requestLine)
	if method == "" || target == "" {
		respondPlain(conn)
		return
	}
	params := parseQueryParams(queryOf(target))
	if credentialMarker(params) == "" {
		respondPlain(conn)
		return
	}
	denied := session.receiveCallback(params)
	writeCallbackPage(conn, denied)
}

func readCallbackHead(conn net.Conn) (string, error) {
	buf := make([]byte, 8192)
	total := 0
	deadline := time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)
	defer conn.SetReadDeadline(time.Time{})
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
		return target[idx+1:]
	}
	return ""
}

func credentialMarker(params map[string]string) string {
	for _, marker := range []string{"authCodeInfo", "code", "refreshToken", "refresh_token", "accessToken", "access_token", "userJwt"} {
		if _, ok := params[marker]; ok {
			return marker
		}
	}
	return ""
}

// parseQueryParams splits a callback query with double-encoding tolerance:
// split on & before unescaping, then unescape repeatedly, then split each
// pair at its first = (JWT values legitimately contain =).
func parseQueryParams(raw string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, value := pair, ""
		if idx := strings.Index(pair, "="); idx >= 0 {
			key, value = pair[:idx], pair[idx+1:]
		}
		out[repeatDecode(key)] = repeatDecode(value)
	}
	return out
}

func repeatDecode(raw string) string {
	text := strings.TrimSpace(raw)
	for deep := 0; deep < 3; deep++ {
		decoded, err := url.QueryUnescape(text)
		if err != nil || decoded == text {
			break
		}
		text = decoded
	}
	return strings.TrimSpace(text)
}

// receiveCallback validates CSRF and parks the credentials on the session.
// The exchange itself happens in the app layer so expiry allows a fresh call.
// Returns whether the callback was rejected, for the local page copy.
func (s *LoginSession) receiveCallback(params map[string]string) bool {
	details := parseCallbackDetails(params)
	details.LoginTraceID = firstValue(params, "loginTraceID", "login_trace_id")
	if strings.TrimSpace(details.LoginTraceID) != "" && s.TraceID != "" && details.LoginTraceID != s.TraceID {
		s.finish(nil, errors.New("回调与本机发起的登录不匹配（loginTraceID 校验失败），已拒绝；请重新发起登录"))
		return true
	}
	if strings.TrimSpace(details.RejectReason()) != "" {
		s.finish(details, fmt.Errorf("授权被拒绝: %s", details.RejectReason()))
		return true
	}
	s.finish(details, nil)
	return false
}

// parseCallbackDetails lifts the credential and the account view out of one
// callback. Missing fields stay empty; Resolve decides what to do.
func parseCallbackDetails(params map[string]string) *CallbackInfo {
	details := &CallbackInfo{
		Host:         strings.TrimRight(firstValue(params, "host"), "/"),
		UserRegion:   firstValue(params, "userRegion", "user_region"),
		LoginTraceID: firstValue(params, "loginTraceID", "login_trace_id"),
		UserID:       firstValue(params, "UserID", "userId", "user_id"),
		EnterpriseID: firstValue(params, "TenantID", "EnterpriseID", "enterprise_id"),
		DisplayName:  firstValue(params, "userName", "user_name", "nickname"),
		DenyReason:   firstValue(params, "error_description", "error_msg", "error"),
	}
	if info := params["userInfo"]; strings.TrimSpace(info) != "" {
		var user struct {
			UserID       string `json:"UserID"`
			UserId       string `json:"userId"`
			ScreenName   string `json:"ScreenName"`
			NickName     string `json:"NickName"`
			Nickname     string `json:"nickname"`
			TenantID     string `json:"TenantID"`
			EnterpriseID string `json:"EnterpriseID"`
		}
		if json.Unmarshal([]byte(info), &user) == nil {
			details.UserID = firstNonEmpty(user.UserID, user.UserId, details.UserID)
			details.DisplayName = firstNonEmpty(user.ScreenName, user.NickName, user.Nickname, details.DisplayName)
			details.EnterpriseID = firstNonEmpty(user.TenantID, user.EnterpriseID, details.EnterpriseID)
		}
	}
	if payload := params["userJwt"]; strings.TrimSpace(payload) != "" {
		var jwt struct {
			Token        string `json:"Token"`
			RefreshToken string `json:"RefreshToken"`
			Refresh      string `json:"refreshToken"`
		}
		if json.Unmarshal([]byte(payload), &jwt) == nil {
			details.UserJwtToken = strings.TrimSpace(jwt.Token)
			details.RefreshToken = firstNonEmpty(jwt.RefreshToken, jwt.Refresh, details.RefreshToken)
		}
	}
	if payload := params["authCodeInfo"]; strings.TrimSpace(payload) != "" {
		var code struct {
			AuthCode string `json:"AuthCode"`
			Code     string `json:"Code"`
		}
		if json.Unmarshal([]byte(payload), &code) == nil {
			details.AuthCode = firstNonEmpty(code.AuthCode, code.Code, details.AuthCode)
		}
	}
	if details.AuthCode == "" {
		details.AuthCode = firstValue(params, "code")
	}
	if details.RefreshToken == "" {
		details.RefreshToken = firstValue(params, "refreshToken", "refresh_token", "RefreshToken")
	}
	return details
}

func firstValue(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(strings.Trim(value, `"`))
		}
	}
	return ""
}

// RejectReason reports why the callback cannot be used, empty when usable.
// A console-side denial (error / error_description) wins over the generic
// missing-credential message so the app can show the upstream's own words.
func (c *CallbackInfo) RejectReason() string {
	if c == nil {
		return "回调缺失"
	}
	if firstNonEmpty(c.AuthCode, c.RefreshToken, c.UserJwtToken) == "" {
		if strings.TrimSpace(c.DenyReason) != "" {
			return c.DenyReason
		}
		return "回调既无 AuthCode 也无 refreshToken"
	}
	return ""
}

// writeCallbackPage answers the redirect with a neutral status page. The page
// never carries credentials; denied callbacks switch to a rejection notice.
func writeCallbackPage(conn net.Conn, denied bool) {
	title, message := "登录信息已接收", "正在换取登录凭证，请回到应用等待登录结果。"
	if denied {
		title, message = "登录未完成", "本次授权未提交有效登录信息，可以关闭本页并回到应用重新发起登录。"
	}
	body := fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>%s</title>`+
		`<body style="font-family:system-ui,'Segoe UI',sans-serif;max-width:420px;margin:64px auto;text-align:center">`+
		`<h2 style="font-weight:600;font-size:18px">%s</h2>`+
		`<p style="color:#555;font-size:14px">%s</p></body>`, title, title, message)
	payload := []byte(body)
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(payload)) +
		"Cache-Control: no-store\r\n" +
		"Access-Control-Allow-Origin: *\r\n" +
		"Connection: close\r\n\r\n"))
	_, _ = conn.Write(payload)
}

// Resolve exchanges a completed callback into a Token. The app layer calls it
// once; the machine/device identity stays the one printed into the
// authorization URL.
func (s *LoginSession) Resolve(ctx context.Context, profile Profile, details *CallbackInfo) (*Token, error) {
	if details == nil {
		return nil, errors.New("登录未完成")
	}
	switch {
	case strings.TrimSpace(details.RefreshToken) != "":
		token, err := refreshExchange(ctx, profile, details.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("换取 Trae 登录凭证失败: %w", err)
		}
		token.UserID = firstNonEmpty(token.UserID, details.UserID)
		token.EnterpriseID = firstNonEmpty(token.EnterpriseID, details.EnterpriseID)
		token.DisplayName = firstNonEmpty(token.DisplayName, details.DisplayName)
		s.stampIdentity(token)
		return token, nil
	case strings.TrimSpace(details.UserJwtToken) != "":
		// Legacy callback handed a live JWT directly; keep it and derive the
		// stable identity from its claims rather than re-exchanging.
		token := &Token{
			AccessToken:  details.UserJwtToken,
			RefreshToken: details.RefreshToken,
			UserID:       details.UserID,
			EnterpriseID: details.EnterpriseID,
			DisplayName:  details.DisplayName,
		}
		s.stampIdentity(token)
		return token, nil
	case strings.TrimSpace(details.AuthCode) != "":
		// Our authorize URL carries no PKCE challenge, so the simplified
		// console should hand back a refreshToken/userJwt. An AuthCode here
		// means the upstream flipped to the device-proof contract we cannot
		// service — retrying rarely changes that, so surface it honestly.
		return nil, fmt.Errorf("控制台回调携带 AuthCode（设备证明协议），未返回 refreshToken；请重新发起一次登录重试")
	default:
		return nil, errors.New("回调不带可用的凭据")
	}
}

// stampIdentity keeps the login-time fingerprint: the refresh token was
// minted against the device pair printed into the authorization URL, so the
// chat headers must reuse exactly that pair. claims only fill the account id.
func (s *LoginSession) stampIdentity(token *Token) {
	claims := parseJWTClaims(token.AccessToken)
	token.UserID = firstNonEmpty(token.UserID, claims.UserID, claims.UID)
	token.MachineID = s.MachineID
	token.DeviceID = s.DeviceID
}

// refreshExchange turns a refresh token into a fresh access token pair on
// the profile's account hosts, in probe order (cn keeps two: api.trae.cn and
// api.trae.com.cn publish the legacy exchange side by side). The legacy
// request carries the upstream-tolerated client secret "-" alone.
func refreshExchange(ctx context.Context, profile Profile, refreshToken string) (*Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, errors.New("refresh_token 为空，请重新登录")
	}
	var lastErr error
	for _, host := range profile.AuthHosts() {
		endpoint := strings.TrimRight(host, "/") + ExchangePath
		for attempt := 0; attempt < exchangeAttempts; attempt++ {
			if attempt > 0 {
				if err := sleepWithContext(ctx, time.Duration(attempt)*time.Second); err != nil {
					return nil, err
				}
			}
			token, err := postExchange(ctx, profile, endpoint, map[string]any{
				"ClientID":     profile.ClientID,
				"RefreshToken": refreshToken,
				"ClientSecret": "-",
				"UserID":       "",
			}, false)
			if err != nil {
				var stale *upstreamRejection
				if errors.As(err, &stale) {
					// A definitive 4xx refusal on this host: no point hammering
					// the same endpoint again, but the sibling host may still
					// publish the same contract, so fall through to it. The
					// wording stays neutral — "登录已过期" only makes sense on
					// the refresh path, and the callers add their own context.
					lastErr = fmt.Errorf("Trae 账号主机拒绝了本次交换: %s", stale.Message)
					break
				}
				lastErr = err
				continue
			}
			if strings.TrimSpace(token.RefreshToken) == "" {
				token.RefreshToken = refreshToken
			}
			// The refresh token binds to the login-time device pair; the
			// standalone refresh inherits whatever pair the caller carries
			// (the app layer keeps it in the credential store).
			return token, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("刷新 Trae 登录失败")
	}
	return nil, lastErr
}

// getUserInfoAt reads the account profile on one account host.
func getUserInfoAt(ctx context.Context, host string, profile Profile, token *Token) (string, string, string, error) {
	raw, err := json.Marshal(map[string]any{
		"ReqSource":  "IDE",
		"IDEVersion": profile.IDEVersion,
	})
	if err != nil {
		return "", "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(host, "/")+UserInfoPath, bytes.NewReader(raw))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgentPrefix+profile.IDEVersion)
	req.Header.Set("X-Cloudide-Token", token.AccessToken)
	// The account APIs ride the shared proxy-aware OAuth client so the
	// MaClaw LLM-scope proxy config applies to login/refresh/userinfo.
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("HTTP %d %s", resp.StatusCode, summarizeBody(body))
	}
	var payload struct {
		Result struct {
			UserID       string `json:"UserID"`
			ScreenName   string `json:"ScreenName"`
			EnterpriseID string `json:"EnterpriseID"`
		} `json:"Result"`
	}
	if json.Unmarshal(body, &payload) == nil {
		return strings.TrimSpace(payload.Result.UserID),
			strings.TrimSpace(payload.Result.ScreenName),
			strings.TrimSpace(payload.Result.EnterpriseID), nil
	}
	var fallback any
	if json.Unmarshal(body, &fallback) != nil {
		return "", "", "", errors.New("账号信息响应无法解析")
	}
	return firstKeyAny(fallback, "UserID", "userId"),
		firstKeyAny(fallback, "ScreenName", "nickname"),
		firstKeyAny(fallback, "EnterpriseID", "enterpriseId"), nil
}

// GetUserInfo reads the account profile behind a fresh JWT across the realm's
// account hosts. It fills in the display name when the browser redirect did
// not carry account details.
func GetUserInfo(ctx context.Context, profile Profile, token *Token) (string, string, string, error) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", "", "", errors.New("missing access token")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var lastErr error
	for _, host := range profile.AuthHosts() {
		userID, name, ent, err := getUserInfoAt(ctx, host, profile, token)
		if err == nil {
			return userID, name, ent, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("账号信息读取失败")
	}
	return "", "", "", lastErr
}

// upstreamRejection marks a refusal retrying cannot fix (bad grants).
type upstreamRejection struct {
	Message string
}

func (e *upstreamRejection) Error() string { return e.Message }

type upstreamEnvelope struct {
	ResponseMetadata struct {
		Error struct {
			Code    any    `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
	} `json:"ResponseMetadata"`
	Code any    `json:"code"`
	Msg  string `json:"message"`
}

func postExchange(ctx context.Context, profile Profile, endpoint string, payload map[string]any, withCloudideEmptyToken bool) (*Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgentPrefix+profile.IDEVersion)
	if withCloudideEmptyToken {
		// The header must exist and be empty on the new contract; a stale
		// token makes the server refuse the exchange.
		req.Header.Set("x-cloudide-token", "")
	}
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		// Transient upstream problems (5xx, rate limiting) keep a plain error
		// so the refresh loop retries instead of demanding a re-login.
		return nil, fmt.Errorf("Trae 上游暂时不可用 (HTTP %d): %s", resp.StatusCode, summarizeBody(body))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &upstreamRejection{Message: fmt.Sprintf("HTTP %d %s", resp.StatusCode, summarizeBody(body))}
	}
	var envelope upstreamEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil {
		if metaCode := strings.TrimSpace(anyTrim(envelope.ResponseMetadata.Error.Code)); metaCode != "" && metaCode != "0" {
			return nil, &upstreamRejection{Message: fmt.Sprintf("%s: %s", metaCode, envelope.ResponseMetadata.Error.Message)}
		}
		if topCode := strings.TrimSpace(anyTrim(envelope.Code)); topCode != "" && topCode != "0" {
			return nil, &upstreamRejection{Message: fmt.Sprintf("%s: %s", topCode, envelope.Msg)}
		}
	}
	token := tokenFromExchange(body)
	if token.AccessToken == "" {
		return nil, errors.New("登录响应缺少 token")
	}
	return token, nil
}

// tokenFromExchange walks a tolerant set of token shapes: the account hosts
// nest tokens under Result {Token, RefreshToken, TokenExpireAt,
// RefreshExpireAt}, other shapes use plain AccessToken/RefreshToken aliases.
func tokenFromExchange(raw []byte) *Token {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return &Token{}
	}
	access := firstKeyAny(payload, "Token", "AccessToken", "access_token", "Jwt", "JWT")
	refresh := firstKeyAny(payload, "RefreshToken", "refresh_token", "refreshToken")
	if refresh != "" && refresh == access {
		refresh = ""
	}
	token := &Token{AccessToken: access, RefreshToken: refresh}
	if at := numericAt(firstKeyAnyNumber(payload, "TokenExpireAt", "expires_at", "ExpiresAt")); at > 0 {
		token.ExpiresAt = at
	}
	if token.ExpiresAt == 0 {
		if duration := firstKeyAnyNumber(payload, "TokenExpireDuration", "expires_in", "ExpiresIn"); duration > 0 {
			token.ExpiresAt = time.Now().Add(time.Duration(duration) * time.Second).Unix()
		}
	}
	if at := numericAt(firstKeyAnyNumber(payload, "RefreshExpireAt", "refresh_token_expires_at")); at > 0 {
		token.RefreshTokenExpiresAt = at
	}
	bindUserFields(payload, token)
	return token
}

func bindUserFields(payload any, token *Token) {
	user := findObject(payload, "user", "User")
	if user == nil {
		return
	}
	id := numericToStringForID(firstKeyAnyValue(user, "id", "ID", "userId", "UserID", "yid", "uid"))
	if id != "" && token.UserID == "" {
		token.UserID = id
	}
}

func findObject(payload any, keys ...string) any {
	switch tree := payload.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := tree[key]; ok {
				if _, isMap := raw.(map[string]any); isMap {
					return raw
				}
			}
		}
		for _, child := range tree {
			if nested := findObject(child, keys...); nested != nil {
				return nested
			}
		}
	case []any:
		for _, child := range tree {
			if nested := findObject(child, keys...); nested != nil {
				return nested
			}
		}
	}
	return nil
}

func firstKeyAny(payload any, keys ...string) string {
	value := firstKeyAnyValue(payload, keys...)
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	if number, ok := value.(float64); ok {
		return decimal(uint64(number))
	}
	return ""
}

func firstKeyAnyNumber(payload any, keys ...string) int64 {
	value := firstKeyAnyValue(payload, keys...)
	return coerceInt(value)
}

func firstKeyAnyValue(payload any, keys ...string) any {
	return firstKeyRecursiveDepth(payload, keys, 4)
}

func firstKeyRecursiveDepth(payload any, keys []string, depth int) any {
	if depth < 0 {
		return nil
	}
	switch tree := payload.(type) {
	case map[string]any:
		for key, value := range tree {
			for _, candidate := range keys {
				if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(key)) {
					return value
				}
			}
		}
		for _, child := range tree {
			if found := firstKeyRecursiveDepth(child, keys, depth-1); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range tree {
			if found := firstKeyRecursiveDepth(child, keys, depth-1); found != nil {
				return found
			}
		}
	}
	return nil
}

func numericToStringForID(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v > 0 {
			return decimal(uint64(v))
		}
	}
	return ""
}

func coerceInt(raw any) int64 {
	switch v := raw.(type) {
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0
		}
		var parsed float64
		if _, err := fmt.Sscanf(text, "%g", &parsed); err != nil {
			return 0
		}
		return int64(parsed)
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

// numericAt prefers absolute unix-seconds values (>1e12 means milliseconds);
// smaller positive numbers are treated as relative durations.
func numericAt(raw any) int64 {
	number := coerceInt(raw)
	if number <= 0 {
		return 0
	}
	switch {
	case number >= 1_000_000_000_000:
		return number / 1000 // millisecond epoch
	case number >= unixEpochFloor:
		return number
	default:
		return time.Now().Add(time.Duration(number) * time.Second).Unix()
	}
}

const unixEpochFloor = 1_000_000_000

func anyTrim(raw any) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == 0 {
			return "0"
		}
		return decimal(uint64(v))
	default:
		return ""
	}
}

// summarizeBody keeps error pages readable without leaking unrelated payload.
func summarizeBody(body []byte) string {
	const keep = 256
	runes := []rune(strings.TrimSpace(string(body)))
	if len(runes) > keep {
		runes = runes[:keep]
	}
	return strings.TrimSpace(string(runes))
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

// Refresh exchanges a refresh token for a fresh access token pair on the
// edition's account hosts. Both tokens rotate server-side; callers must
// persist both values when non-empty. The device pair rides along: pass the
// pair stored with the credential (the one the token family was minted
// against) and empty strings when nothing is known yet.
func Refresh(ctx context.Context, profile Profile, refreshToken, machineID, deviceID string) (*Token, error) {
	token, err := refreshExchange(ctx, profile, refreshToken)
	if err != nil {
		return nil, err
	}
	token.MachineID = strings.TrimSpace(machineID)
	token.DeviceID = strings.TrimSpace(deviceID)
	return token, nil
}
