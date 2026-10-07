// Browser device login reverse-engineered from the official qoder CLI:
// the browser page carries a one-time PKCE challenge, and the CLI polls the
// OpenAPI host once per second until the approval lands as a token.
package qoder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// DeviceLogin is one pending browser approval.
type DeviceLogin struct {
	nonce     string // random id tying the auth page to the poll session
	verifier  string // PKCE verifier echoed back on every poll
	MachineID string // stable per-install machine id (informational upstream)
}

// AuthURL is the page the user must open in a browser.
func (l *DeviceLogin) AuthURL(profile Profile) string {
	params := url.Values{}
	params.Set("challenge", pkceChallenge(l.verifier))
	params.Set("challenge_method", "S256")
	params.Set("nonce", l.nonce)
	params.Set("machine_id", l.MachineID)
	params.Set("client_id", profile.ClientID)
	return strings.TrimRight(profile.WebOrigin, "/") + "/device/selectAccounts?" + params.Encode()
}

// Nonce is the login session id shown for correlation.
func (l *DeviceLogin) Nonce() string { return l.nonce }

// IsApprovalURL reports whether raw is a browser page we are willing to open:
// HTTPS on a Qoder origin, or loopback HTTP for local test servers.
func IsApprovalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		switch host {
		case "qoder.cn", "www.qoder.cn", "qoder.com", "www.qoder.com":
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

// StartLogin generates a fresh PKCE session. It never touches the network.
func StartLogin(machineID string) (*DeviceLogin, error) {
	verifier, err := pkceVerifier()
	if err != nil {
		return nil, err
	}
	nonce, err := randomUUID()
	if err != nil {
		return nil, err
	}
	return &DeviceLogin{
		nonce:     nonce,
		verifier:  verifier,
		MachineID: strings.TrimSpace(machineID),
	}, nil
}

// Token is a device-login credential. Both the access and the refresh token
// rotate when refreshed, so callers must persist both new values.
type Token struct {
	AccessToken           string
	RefreshToken          string
	UserID                string
	UserName              string
	ExpiresAt             int64 // unix seconds; 0 = server did not report one
	RefreshTokenExpiresAt int64 // unix seconds (always rotated at refresh)
}

// pollResponse mirrors the /api/v1/deviceToken/poll payload.
type pollResponse struct {
	Token       string `json:"token"`
	DeviceToken string `json:"device_token"`
	AccessToken string `json:"access_token"`
	// Poll also accepts exchange-style key aliases.
	AccessTokenAlias         string `json:"access_token_alias"`
	RefreshToken             string `json:"refresh_token"`
	RefreshTokenSpelled      string `json:"refreshToken"`
	UserID                   string `json:"user_id"`
	UserIDAlt                string `json:"userId"`
	UID                      string `json:"uid"`
	UserName                 string `json:"user_name"`
	UserNameAlt              string `json:"userName"`
	Name                     string `json:"name"`
	ExpiresAt                string `json:"expires_at"`
	ExpiresAtAlt             string `json:"expiresAt"`
	ExpiresIn                int64  `json:"expires_in"`
	ExpiresInAlt             int64  `json:"expiresIn"`
	RefreshTokenExpiresAt    string `json:"refresh_token_expires_at"`
	RefreshTokenExpiresAtAlt string `json:"refreshTokenExpiresAt"`
	RefreshTokenExpiresIn    int64  `json:"refresh_token_expires_in"`
}

func (r pollResponse) tokenValue() string {
	switch {
	case r.Token != "":
		return r.Token
	case r.DeviceToken != "":
		return r.DeviceToken
	case r.AccessToken != "":
		return r.AccessToken
	default:
		return r.AccessTokenAlias
	}
}

func (r pollResponse) refreshTokenValue() string {
	if r.RefreshToken != "" {
		return r.RefreshToken
	}
	return r.RefreshTokenSpelled
}

func (r pollResponse) userIDValue() string {
	switch {
	case r.UserID != "":
		return r.UserID
	case r.UserIDAlt != "":
		return r.UserIDAlt
	default:
		return r.UID
	}
}

func (r pollResponse) userNameValue() string {
	switch {
	case r.UserName != "":
		return r.UserName
	case r.UserNameAlt != "":
		return r.UserNameAlt
	default:
		return r.Name
	}
}

// tokenFromPoll converts a successful poll payload into a Token.
// The poll reports time as absolute unix seconds (expires_at) while the
// refresh reports expires_at for both fields; expires_in is accepted as a
// relative fallback when no absolute value is present.
func tokenFromPoll(r pollResponse, now time.Time) Token {
	token := Token{
		AccessToken:  r.tokenValue(),
		RefreshToken: r.refreshTokenValue(),
		UserID:       r.userIDValue(),
		UserName:     r.userNameValue(),
	}
	if unix := numericOrUnix(r.ExpiresAt, r.ExpiresIn, now); unix > 0 {
		token.ExpiresAt = unix
	}
	if unix := numericOrUnix(r.RefreshTokenExpiresAt, r.RefreshTokenExpiresIn, now); unix > 0 {
		token.RefreshTokenExpiresAt = unix
	}
	return token
}

// numericOrUnix prefers an already-unix timestamp string; when instead the
// server answered with a duration value, convert relative to now.
func numericOrUnix(absolute string, relative int64, now time.Time) int64 {
	absolute = strings.TrimSpace(absolute)
	if absolute == "" {
		if relative > 0 {
			return now.Unix() + relative
		}
		return 0
	}
	var value int64
	if _, err := fmt.Sscanf(absolute, "%d", &value); err != nil {
		return 0
	}
	if value > now.Unix()-8*365*24*3600 && value < now.Unix()+100*365*24*3600 {
		return value
	}
	if value > 0 {
		return now.Unix() + value
	}
	return 0
}

// RefreshResponse mirrors the /api/v1/deviceToken/refresh payload.
type refreshResponse struct {
	DeviceToken           string `json:"device_token"`
	Token                 string `json:"token"`
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresAt             string `json:"expires_at"`
	RefreshTokenExpiresAt string `json:"refresh_token_expires_at"`
}

// PollUntilAuthorized polls once per second until the approval lands, the
// 5-minute window closes, or ctx ends. A 404 means "no approval yet"; any
// other failure aborts the login like the official CLI does.
func PollUntilAuthorized(ctx context.Context, profile Profile, login *DeviceLogin) (Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if login == nil || login.nonce == "" || login.verifier == "" {
		return Token{}, errors.New("没有正在进行的 Qoder 登录")
	}
	deadline := time.Now().Add(pollLifetime)
	endpoint := strings.TrimRight(profile.OpenAPIBase, "/") + "/api/v1/deviceToken/poll"
	for {
		if time.Now().After(deadline) {
			return Token{}, errors.New("Qoder 登录已超时，请重新登录")
		}
		token, status, err := pollOnce(ctx, endpoint, login)
		if err == nil {
			return token, nil
		}
		if status == http.StatusNotFound {
			// The approval has not landed yet (matches the CLI: 404 keeps
			// polling, any other non-200 ends the login).
			if err := sleepWithContext(ctx, pollInterval); err != nil {
				return Token{}, err
			}
			continue
		}
		if status != 0 {
			return Token{}, err
		}
		// Transport blips (network, proxy) stay retryable until the deadline
		// so the user does not lose an approval they already opened.
		if err := sleepWithContext(ctx, pollInterval); err != nil {
			return Token{}, err
		}
	}
}

func pollOnce(ctx context.Context, endpoint string, login *DeviceLogin) (Token, int, error) {
	params := url.Values{}
	params.Set("nonce", login.nonce)
	params.Set("verifier", login.verifier)
	params.Set("challenge_method", "S256")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return Token{}, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return Token{}, 0, fmt.Errorf("轮询 Qoder 登录失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return Token{}, resp.StatusCode, fmt.Errorf("读取 Qoder 登录响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Token{}, resp.StatusCode, fmt.Errorf("轮询 Qoder 登录失败 (HTTP %d): %s", resp.StatusCode, truncateForError(body))
	}
	var payload pollResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Token{}, resp.StatusCode, fmt.Errorf("Qoder 登录响应无法解析: %w", err)
	}
	token := tokenFromPoll(payload, time.Now())
	if token.AccessToken == "" {
		return Token{}, resp.StatusCode, errors.New("Qoder 登录响应缺少 token")
	}
	return token, resp.StatusCode, nil
}

// Refresh exchanges a refresh token for a fresh device token pair. Both
// tokens rotate server-side; callers must persist both values when non-empty.
func Refresh(ctx context.Context, profile Profile, refreshToken, machineID string) (Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, errors.New("refresh_token 为空，请重新登录")
	}
	body := map[string]string{"refresh_token": refreshToken}
	if machineID = strings.TrimSpace(machineID); machineID != "" {
		body["machine_id"] = machineID
	}
	var lastErr error
	for attempt := 0; attempt < refreshAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, time.Duration(attempt)*time.Second); err != nil {
				return Token{}, err
			}
		}
		payload, err := postJSON(ctx, strings.TrimRight(profile.OpenAPIBase, "/")+"/api/v1/deviceToken/refresh", body, "")
		if err != nil {
			var apiErr apiError
			if errors.As(err, &apiErr) && !apiErr.retryable() {
				return Token{}, err
			}
			lastErr = err
			continue
		}
		tokenValue := payload.DeviceToken
		if tokenValue == "" {
			tokenValue = payload.Token
		}
		if tokenValue == "" {
			tokenValue = payload.AccessToken
		}
		if tokenValue == "" {
			return Token{}, errors.New("Qoder 刷新响应缺少 device_token")
		}
		now := time.Now()
		token := Token{AccessToken: tokenValue, RefreshToken: payload.RefreshToken}
		if unix := numericOrUnix(payload.ExpiresAt, 0, now); unix > 0 {
			token.ExpiresAt = unix
		}
		if unix := numericOrUnix(payload.RefreshTokenExpiresAt, 0, now); unix > 0 {
			token.RefreshTokenExpiresAt = unix
		}
		if token.RefreshToken == "" {
			token.RefreshToken = refreshToken // the server may reuse the token
		}
		return token, nil
	}
	if lastErr == nil {
		lastErr = errors.New("Qoder 刷新登录失败")
	}
	return Token{}, lastErr
}

type apiError struct {
	status int
	detail string
}

func (e apiError) Error() string {
	return fmt.Sprintf("Qoder 接口请求失败 (HTTP %d): %s", e.status, e.detail)
}

func (e apiError) retryable() bool {
	return e.status >= 500
}

func postJSON(ctx context.Context, endpoint string, body any, bearer string) (refreshResponse, error) {
	var payload refreshResponse
	raw, err := json.Marshal(body)
	if err != nil {
		return payload, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return payload, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return payload, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return payload, err
	}
	if resp.StatusCode != http.StatusOK {
		return payload, apiError{status: resp.StatusCode, detail: truncateForError(data)}
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 512 {
		text = text[:512]
	}
	return text
}

// ─────────────────────────────────────────────────────────────────────────────
// PKCE (S256) — verifier alphabet copied from the CLI (RFC 7636 base64url set)
// ─────────────────────────────────────────────────────────────────────────────

const pkceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// pkceVerifier mirrors the official CLI: 43–128 chars drawn from the RFC 7636
// alphabet, with the raw random bytes mapped modulo the alphabet size.
func pkceVerifier() (string, error) {
	length := 43 + int(randByte()%86) // window copied from the CLI (43 + 86*rand)
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = pkceAlphabet[int(b)%len(pkceAlphabet)]
	}
	return string(out), nil
}

func randByte() byte {
	buf := make([]byte, 1)
	if _, err := rand.Read(buf); err != nil {
		return 0
	}
	return buf[0]
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomUUID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return uuidFormat(raw), nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if ctx == nil {
		timer := time.NewTimer(d)
		defer timer.Stop()
		<-timer.C
		return nil
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
