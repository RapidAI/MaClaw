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
	"math"
	"net/http"
	"net/url"
	"strconv"
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

// AuthURL is the page the user must open in a browser. The official CLI hands
// the browser the query in a fixed order — challenge, challenge_method, nonce,
// machine_id, client_id (sorted params would put client_id and machine_id in
// front) — so emit the exact same order for approval-page parity.
func (l *DeviceLogin) AuthURL(profile Profile) string {
	values := []struct{ key, value string }{
		{"challenge", pkceChallenge(l.verifier)},
		{"challenge_method", "S256"},
		{"nonce", l.nonce},
		{"machine_id", l.MachineID},
		{"client_id", profile.ClientID},
	}
	parts := make([]string, 0, len(values))
	for _, kv := range values {
		parts = append(parts, url.QueryEscape(kv.key)+"="+url.QueryEscape(kv.value))
	}
	return strings.TrimRight(profile.WebOrigin, "/") + "/device/selectAccounts?" + strings.Join(parts, "&")
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
	return &DeviceLogin{
		nonce:     newUUID(),
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

// pollResponse mirrors the /api/v1/deviceToken/poll payload. Every field
// carries the key aliases the CLI's tolerant parsers accept.
type pollResponse struct {
	Token                    string         `json:"token"`
	DeviceToken              string         `json:"device_token"`
	AccessToken              string         `json:"access_token"`
	RefreshToken             string         `json:"refresh_token"`
	RefreshTokenSpelled      string         `json:"refreshToken"`
	UserID                   string         `json:"user_id"`
	UserIDAlt                string         `json:"userId"`
	UID                      string         `json:"uid"`
	UserName                 string         `json:"user_name"`
	UserNameAlt              string         `json:"userName"`
	Name                     string         `json:"name"`
	ExpiresAt                stringOrNumber `json:"expires_at"`
	ExpiresAtAlt             stringOrNumber `json:"expiresAt"`
	ExpiresIn                stringOrNumber `json:"expires_in"`
	ExpiresInAlt             stringOrNumber `json:"expiresIn"`
	RefreshTokenExpiresAt    stringOrNumber `json:"refresh_token_expires_at"`
	RefreshTokenExpiresAtAlt stringOrNumber `json:"refreshTokenExpiresAt"`
	RefreshTokenExpiresIn    stringOrNumber `json:"refresh_token_expires_in"`
}

func (r pollResponse) tokenValue() string {
	switch {
	case r.Token != "":
		return r.Token
	case r.DeviceToken != "":
		return r.DeviceToken
	default:
		return r.AccessToken
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
// The poll reports time as an absolute unix-seconds value (expires_at) with
// expires_in accepted as a relative fallback, mirroring the CLI parsers.
func tokenFromPoll(r pollResponse, now time.Time) Token {
	token := Token{
		AccessToken:  r.tokenValue(),
		RefreshToken: r.refreshTokenValue(),
		UserID:       r.userIDValue(),
		UserName:     r.userNameValue(),
	}
	if unix := numericOrUnix(firstNonEmpty(r.ExpiresAt, r.ExpiresAtAlt), relativeSeconds(firstNonEmpty(r.ExpiresIn, r.ExpiresInAlt)), now); unix > 0 {
		token.ExpiresAt = unix
	}
	if unix := numericOrUnix(firstNonEmpty(r.RefreshTokenExpiresAt, r.RefreshTokenExpiresAtAlt), relativeSeconds(firstNonEmpty(r.RefreshTokenExpiresIn)), now); unix > 0 {
		token.RefreshTokenExpiresAt = unix
	}
	return token
}

func firstNonEmpty(values ...stringOrNumber) stringOrNumber {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// relativeSeconds reads an expires_in-style duration, tolerating numbers and
// quoted integers alike. Anything unparseable or non-positive is 0.
func relativeSeconds(value stringOrNumber) int64 {
	text := strings.TrimSpace(string(value))
	if text == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		floatValue, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil || !isFinite(floatValue) {
			return 0
		}
		parsed = int64(floatValue)
	}
	if parsed <= 0 {
		return 0
	}
	return parsed
}

// stringOrNumber accepts a JSON string or a bare number for one field, since
// the CLI's own parsers treat both spellings alike (expires_at arrives as a
// string in some deployments and as a number in others).
type stringOrNumber string

func (s *stringOrNumber) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		*s = ""
		return nil
	}
	if text[0] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		*s = stringOrNumber(strings.TrimSpace(decoded))
		return nil
	}
	*s = stringOrNumber(text)
	return nil
}

// numericOrUnix reads an absolute timestamp when the value looks absolute, and
// otherwise treats a small number as a duration relative to now, so either
// server answer lands on a real expiry. Integers, quoted integers, and float
// literals are all accepted; the refresh/poll endpoints have also been
// observed answering RFC3339 timestamps ("2026-11-06T13:43:44Z"), which parse
// as absolute unix seconds. Unparseable values are 0.
func numericOrUnix(absolute stringOrNumber, relative int64, now time.Time) int64 {
	text := strings.TrimSpace(string(absolute))
	if text == "" {
		if relative > 0 {
			return now.Unix() + relative
		}
		return 0
	}
	var value int64
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		floatValue, floatErr := strconv.ParseFloat(text, 64)
		if floatErr != nil || !isFinite(floatValue) {
			// Not numeric — the last accepted spelling is an RFC3339 wall
			// clock; anything else cannot be turned into an expiry.
			if ts, terr := time.Parse(time.RFC3339, text); terr == nil {
				return ts.Unix()
			}
			return 0
		}
		value = int64(floatValue)
	} else {
		value = parsed
	}
	if value >= unixEpochFloor {
		return value
	}
	if value > 0 {
		return now.Unix() + value
	}
	return 0
}

func isFinite(value float64) bool {
	return !math.IsInf(value, 0) && !math.IsNaN(value)
}

// unixEpochFloor separates 10-digit absolute unix seconds (2001-09-09 and
// later) from small relative durations.
const unixEpochFloor = int64(1_000_000_000)

// RefreshResponse mirrors the /api/v1/deviceToken/refresh payload.
type refreshResponse struct {
	DeviceToken           string         `json:"device_token"`
	Token                 string         `json:"token"`
	AccessToken           string         `json:"access_token"`
	RefreshToken          string         `json:"refresh_token"`
	ExpiresAt             stringOrNumber `json:"expires_at"`
	RefreshTokenExpiresAt stringOrNumber `json:"refresh_token_expires_at"`
}

// pollWait is the delay between polls. Tests shorten it; production uses the
// CLI's one-per-second cadence.
var pollWait = pollInterval

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
			if err := sleepWithContext(ctx, pollWait); err != nil {
				return Token{}, err
			}
			continue
		}
		if status != 0 {
			return Token{}, err
		}
		// Transport blips (network, proxy) stay retryable until the deadline
		// so the user does not lose an approval they already opened.
		if err := sleepWithContext(ctx, pollWait); err != nil {
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

// sessionRefreshPath is the OpenAPI endpoint that rotates a device token.
func sessionRefreshPath(profile Profile) string {
	return strings.TrimRight(profile.OpenAPIBase, "/") + "/api/v1/deviceToken/refresh"
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
		payload, err := postJSON(ctx, sessionRefreshPath(profile), body)
		if err != nil {
			var apiErr apiError
			if errors.As(err, &apiErr) && !apiErr.retryable() {
				if apiErr.status == http.StatusUnauthorized || apiErr.status == http.StatusForbidden {
					return Token{}, fmt.Errorf("Qoder 登录已失效，请重新登录")
				}
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

func postJSON(ctx context.Context, endpoint string, body any) (refreshResponse, error) {
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

// truncateForError keeps the first 512 runes of an error body so CJK server
// messages stay readable instead of ending mid-character.
func truncateForError(body []byte) string {
	text := strings.TrimSpace(string(body))
	runes := []rune(text)
	if len(runes) > 512 {
		runes = runes[:512]
	}
	return strings.TrimSpace(string(runes))
}

// ─────────────────────────────────────────────────────────────────────────────
// PKCE (S256) — verifier alphabet copied from the CLI (RFC 7636 base64url set)
// ─────────────────────────────────────────────────────────────────────────────

const pkceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// pkceVerifier mirrors the official CLI's 43–128-char window over the RFC
// 7636 alphabet, but samples uniformly with rejection sampling — a raw byte
// modulo 66 favors the first 58 letters by up to a third, and RFC 7636
// expects a uniform verifier.
func pkceVerifier() (string, error) {
	length := 43 + int(randByte()%86)
	buf := make([]byte, length+length/2+8) // generous: ~1.5 draws per char covers rejection
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 0, length)
	const alphabetMax = byte(198) // 3 × 66 accepted values
	for _, b := range buf {
		if b < alphabetMax {
			out = append(out, pkceAlphabet[b%byte(len(pkceAlphabet))])
			if len(out) == length {
				break
			}
		}
	}
	for len(out) < length {
		// Practically unreachable with the >1.5× draw estimate; top up rather
		// than return a short verifier.
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < alphabetMax {
				out = append(out, pkceAlphabet[b%byte(len(pkceAlphabet))])
				if len(out) == length {
					break
				}
			}
		}
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
