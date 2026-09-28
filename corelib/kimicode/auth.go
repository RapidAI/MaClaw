// Package kimicode implements Kimi Code's public device-code login.
// The flow is the RFC 8628 grant published by Moonshot's Kimi Code CLI:
// request a device code, approve it in the browser, then poll for a token.
package kimicode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

const (
	// Name is the built-in provider label shown in 服务商管理.
	Name = "Kimi Code"
	// StoreID is the credential-store key for this provider's OAuth tokens.
	StoreID = "kimi-code"
	// DefaultBaseURL is the mainland Kimi Code OpenAI-compatible API.
	DefaultBaseURL = "https://api.kimi.com/coding/v1"
	// GlobalBaseURL is the international Kimi Code API paired with auth.kimi.ai.
	GlobalBaseURL = "https://api.kimi.ai/coding/v1"
	// DefaultModel is the managed coding alias used before a catalog fetch.
	DefaultModel = "kimi-for-coding"
	// DefaultOAuthHost is the mainland device-authorization host.
	DefaultOAuthHost = "https://auth.kimi.com"
	// ClientID is the public Kimi Code device-flow client. It has no secret.
	ClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"
	// Platform is the host identity sent as X-Msh-Platform. MaClaw reports
	// itself rather than claiming to be the official Kimi Code CLI.
	Platform = "maclaw"

	productName          = "MaClaw"
	productVersion       = "1.0.0"
	defaultPollInterval  = 5
	maxPollInterval      = 60
	defaultLoginLifetime = 15 * time.Minute
	maxLoginLifetime     = 30 * time.Minute
	refreshAttempts      = 3
	globalOAuthHost      = "auth.kimi.ai"
	globalAPIHost        = "api.kimi.ai"
)

// ErrExpired means the device code timed out before the user approved it.
var ErrExpired = errors.New("授权已过期，请重新登录")

// ErrDenied means the user rejected the device authorization.
var ErrDenied = errors.New("授权被拒绝")

// DeviceAuthorization is one pending browser login.
type DeviceAuthorization struct {
	UserCode                string
	DeviceCode              string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               int
	Interval                int
}

// Token is a Kimi Code access credential. RefreshToken rotates on refresh.
type Token struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

type pollKind int

const (
	pollSuccess pollKind = iota
	pollPending
	pollSlowDown
	pollExpired
	pollDenied
)

type pollOutcome struct {
	kind        pollKind
	token       Token
	description string
}

// IsProviderName reports whether name is the built-in Kimi Code provider.
func IsProviderName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), Name)
}

// IsCodingEndpoint reports whether rawURL is a Kimi Code managed API.
func IsCodingEndpoint(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "api.kimi.com" && host != "api.kimi.ai" {
		return false
	}
	return strings.Contains(strings.ToLower(parsed.Path), "/coding")
}

// OAuthHost returns the device-flow host, honoring the official env overrides.
// A host without a scheme is treated as HTTPS.
func OAuthHost() string {
	if host := oauthHostFromEnv(); host != "" {
		return host
	}
	return DefaultOAuthHost
}

// OAuthHostForBaseURL picks the auth host that issued tokens for a saved
// coding API. The international API refreshes at auth.kimi.ai even after the
// process environment no longer names that host.
func OAuthHostForBaseURL(baseURL string) string {
	if host := oauthHostFromEnv(); host != "" {
		return host
	}
	if endpointHost(baseURL) == globalAPIHost {
		return "https://" + globalOAuthHost
	}
	return DefaultOAuthHost
}

func oauthHostFromEnv() string {
	for _, key := range []string{"KIMI_CODE_OAUTH_HOST", "KIMI_OAUTH_HOST"} {
		if host := normalizeOAuthHost(os.Getenv(key)); host != "" {
			return host
		}
	}
	return ""
}

func normalizeOAuthHost(raw string) string {
	host := strings.TrimRight(strings.TrimSpace(raw), "/")
	if host == "" {
		return ""
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	return host
}

// RequestDeviceAuthorization starts a browser login against the configured host.
func RequestDeviceAuthorization(ctx context.Context) (DeviceAuthorization, error) {
	return RequestDeviceAuthorizationAt(ctx, "")
}

// RequestDeviceAuthorizationAt starts a browser login against host. An empty
// host uses the current OAuth host. Callers that also poll must pass the same
// host they used here.
func RequestDeviceAuthorizationAt(ctx context.Context, host string) (DeviceAuthorization, error) {
	if strings.TrimSpace(host) == "" {
		host = OAuthHost()
	}
	return requestDeviceAuthorization(ctx, host, deviceHeaders())
}

func requestDeviceAuthorization(ctx context.Context, host string, headers http.Header) (DeviceAuthorization, error) {
	status, payload, err := postForm(ctx, strings.TrimRight(host, "/")+"/api/oauth/device_authorization", url.Values{
		"client_id": {ClientID},
	}, headers)
	if err != nil {
		return DeviceAuthorization{}, err
	}
	if status != http.StatusOK {
		return DeviceAuthorization{}, fmt.Errorf("申请设备码失败 (HTTP %d): %s", status, errorDetail(payload))
	}
	auth := DeviceAuthorization{
		UserCode:                stringField(payload, "user_code"),
		DeviceCode:              stringField(payload, "device_code"),
		VerificationURI:         stringField(payload, "verification_uri"),
		VerificationURIComplete: stringField(payload, "verification_uri_complete"),
		ExpiresIn:               intField(payload, "expires_in"),
		Interval:                intField(payload, "interval"),
	}
	if auth.UserCode == "" || auth.DeviceCode == "" || (auth.VerificationURIComplete == "" && auth.VerificationURI == "") {
		return DeviceAuthorization{}, errors.New("设备码响应缺少 user_code、device_code 或验证地址")
	}
	if auth.Interval <= 0 {
		auth.Interval = defaultPollInterval
	}
	return auth, nil
}

type terminalError struct {
	error
}

func (e terminalError) Unwrap() error { return e.error }

// PollUntilAuthorized polls until the browser approval finishes, the code
// expires, or ctx is cancelled. slow_down widens the interval. A rejected
// request fails immediately. Transport and 5xx failures keep retrying until
// the login context ends, so a short outage does not abandon an approval the
// user is still completing.
func PollUntilAuthorized(ctx context.Context, deviceCode string, intervalSec int) (Token, error) {
	return PollUntilAuthorizedAt(ctx, deviceCode, intervalSec, "")
}

// PollUntilAuthorizedAt polls host. An empty host uses the current OAuth host.
// Login passes the host that issued the device code so a later environment
// change cannot send the poll somewhere else.
func PollUntilAuthorizedAt(ctx context.Context, deviceCode string, intervalSec int, host string) (Token, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if intervalSec <= 0 {
		intervalSec = defaultPollInterval
	}
	if strings.TrimSpace(host) == "" {
		host = OAuthHost()
	}
	headers := deviceHeaders()
	for {
		outcome, err := pollDeviceToken(ctx, host, deviceCode, headers)
		if err != nil {
			var terminal terminalError
			if errors.As(err, &terminal) {
				return Token{}, err
			}
			if ctx.Err() != nil {
				return Token{}, ctx.Err()
			}
			if waitErr := waitInterval(ctx, intervalSec); waitErr != nil {
				return Token{}, waitErr
			}
			continue
		}
		switch outcome.kind {
		case pollSuccess:
			return outcome.token, nil
		case pollExpired:
			return Token{}, ErrExpired
		case pollDenied:
			if outcome.description != "" {
				return Token{}, fmt.Errorf("%w: %s", ErrDenied, outcome.description)
			}
			return Token{}, ErrDenied
		case pollSlowDown:
			intervalSec += 5
			if intervalSec > maxPollInterval {
				intervalSec = maxPollInterval
			}
		}
		if err := waitInterval(ctx, intervalSec); err != nil {
			return Token{}, err
		}
	}
}

func pollDeviceToken(ctx context.Context, host, deviceCode string, headers http.Header) (pollOutcome, error) {
	status, payload, err := postForm(ctx, strings.TrimRight(host, "/")+"/api/oauth/token", url.Values{
		"client_id":   {ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}, headers)
	if err != nil {
		return pollOutcome{}, err
	}
	if status == http.StatusOK && stringField(payload, "access_token") != "" {
		token, err := tokenFromPayload(payload)
		if err != nil {
			return pollOutcome{}, terminalError{err}
		}
		return pollOutcome{kind: pollSuccess, token: token}, nil
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return pollOutcome{}, fmt.Errorf("轮询授权失败 (HTTP %d)", status)
	}
	switch stringField(payload, "error") {
	case "authorization_pending":
		return pollOutcome{kind: pollPending}, nil
	case "slow_down":
		return pollOutcome{kind: pollSlowDown}, nil
	case "expired_token":
		return pollOutcome{kind: pollExpired}, nil
	case "access_denied":
		return pollOutcome{kind: pollDenied, description: stringField(payload, "error_description")}, nil
	default:
		return pollOutcome{}, terminalError{fmt.Errorf("轮询授权失败 (HTTP %d): %s", status, errorDetail(payload))}
	}
}

// Refresh exchanges a refresh token for a new access token. Kimi Code rotates
// the refresh token, so callers must persist RefreshToken when it is non-empty.
// baseURL selects the auth host when no environment override is set.
func Refresh(ctx context.Context, refreshToken, baseURL string) (Token, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Token{}, errors.New("refresh_token 为空，请重新登录")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	headers := deviceHeaders()
	endpoint := strings.TrimRight(OAuthHostForBaseURL(baseURL), "/") + "/api/oauth/token"
	var lastErr error
	for attempt := 0; attempt < refreshAttempts; attempt++ {
		if attempt > 0 {
			if err := waitInterval(ctx, attempt); err != nil {
				return Token{}, err
			}
		}
		status, payload, err := postForm(ctx, endpoint, url.Values{
			"client_id":     {ClientID},
			"grant_type":    {"refresh_token"},
			"refresh_token": {refreshToken},
		}, headers)
		if err != nil {
			lastErr = err
			continue
		}
		if status == http.StatusOK && stringField(payload, "access_token") != "" {
			return tokenFromPayload(payload)
		}
		detail := errorDetail(payload)
		if status == http.StatusUnauthorized || status == http.StatusForbidden || stringField(payload, "error") == "invalid_grant" {
			return Token{}, fmt.Errorf("刷新登录已失效，请重新登录: %s", detail)
		}
		lastErr = fmt.Errorf("刷新登录失败 (HTTP %d): %s", status, detail)
		if status != http.StatusTooManyRequests && status < 500 {
			return Token{}, lastErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("刷新登录失败")
	}
	return Token{}, lastErr
}

// ApplyHTTPHeaders stamps the device identity used by Kimi Code OAuth and the
// managed coding API. It does not add an Authorization header.
func ApplyHTTPHeaders(h http.Header) {
	if h == nil {
		return
	}
	for key, value := range deviceHeaders() {
		if len(value) == 0 {
			continue
		}
		h.Set(key, value[0])
	}
}

// MaxLoginLifetime is the backstop for a device login whose server window is
// not known yet, including the initial device-code request.
func MaxLoginLifetime() time.Duration {
	return maxLoginLifetime
}

// BaseURLForOAuthHost returns the coding API that matches the OAuth host.
// The international auth host uses api.kimi.ai. Every other host stays on the
// mainland default.
func BaseURLForOAuthHost(host string) string {
	if endpointHost(host) == globalOAuthHost {
		return GlobalBaseURL
	}
	return DefaultBaseURL
}

// CanonicalBaseURL keeps an already selected international coding endpoint and
// folds every other value back to the mainland default.
func CanonicalBaseURL(raw string) string {
	if endpointHost(raw) == globalAPIHost && strings.Contains(strings.ToLower(raw), "/coding") {
		return GlobalBaseURL
	}
	return DefaultBaseURL
}

func endpointHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// IsApprovalURL reports whether raw is a browser URL we can open for approval.
// Public pages must be HTTPS on a Kimi host. Plain HTTP is only accepted on loopback.
func IsApprovalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		switch host {
		case "kimi.com", "www.kimi.com", "kimi.ai", "www.kimi.ai", "auth.kimi.com", "auth.kimi.ai":
			return true
		default:
			return false
		}
	case "http":
		switch host {
		case "localhost", "127.0.0.1", "::1":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

// LoginLifetime is how long a device approval may stay pending.
// The server-reported window is kept, bounded to a minute through half an hour.
func LoginLifetime(expiresIn int) time.Duration {
	if expiresIn <= 0 {
		return defaultLoginLifetime
	}
	lifetime := time.Duration(expiresIn) * time.Second
	if lifetime > maxLoginLifetime {
		return maxLoginLifetime
	}
	if lifetime < time.Minute {
		return time.Minute
	}
	return lifetime
}

func tokenFromPayload(payload map[string]any) (Token, error) {
	token := Token{
		AccessToken:  stringField(payload, "access_token"),
		RefreshToken: stringField(payload, "refresh_token"),
		ExpiresIn:    intField(payload, "expires_in"),
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return Token{}, errors.New("授权响应缺少 access_token、refresh_token 或 expires_in")
	}
	return token, nil
}

func postForm(ctx context.Context, endpoint string, form url.Values, headers http.Header) (int, map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, fmt.Errorf("创建授权请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("读取授权响应失败: %w", err)
	}
	payload := map[string]any{}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &payload)
	}
	return resp.StatusCode, payload, nil
}

func waitInterval(ctx context.Context, seconds int) error {
	if seconds < 1 {
		seconds = 1
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var (
	headerMu    sync.Mutex
	headerKey   string
	headerCache http.Header
)

func deviceHeaders() http.Header {
	platform := Platform
	if override := strings.TrimSpace(os.Getenv("KIMI_CODE_OAUTH_PLATFORM")); override != "" {
		platform = override
	}
	platform = asciiHeader(platform)
	key := platform + "\n" + deviceIDPath()
	headerMu.Lock()
	defer headerMu.Unlock()
	if headerCache != nil && headerKey == key {
		return headerCache.Clone()
	}
	version := asciiHeader(productVersion)
	headers := make(http.Header)
	headers.Set("User-Agent", productName+"/"+version)
	headers.Set("X-Msh-Platform", platform)
	headers.Set("X-Msh-Version", version)
	headers.Set("X-Msh-Device-Name", asciiHeader(hostName()))
	headers.Set("X-Msh-Device-Model", asciiHeader(deviceModel()))
	headers.Set("X-Msh-Os-Version", asciiHeader(runtime.GOOS))
	headers.Set("X-Msh-Device-Id", deviceID())
	headerKey = key
	headerCache = headers
	return headers.Clone()
}

func deviceModel() string {
	return runtime.GOOS + " " + runtime.GOARCH
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "maclaw"
	}
	return name
}

var (
	deviceMu         sync.Mutex
	deviceCachedPath string
	deviceCachedID   string
)

func deviceID() string {
	path := deviceIDPath()
	deviceMu.Lock()
	defer deviceMu.Unlock()
	if deviceCachedID != "" && path == deviceCachedPath {
		return deviceCachedID
	}
	id := readOrCreateDeviceID(path)
	deviceCachedPath = path
	deviceCachedID = id
	return id
}

func readOrCreateDeviceID(path string) string {
	if path != "" {
		if text, err := os.ReadFile(path); err == nil {
			if id := asciiHeader(strings.TrimSpace(string(text))); id != "" && id != "unknown" {
				return id
			}
		}
	}
	id := newDeviceID()
	if path == "" {
		return id
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
		_ = os.WriteFile(path, []byte(id), 0o600)
	}
	return id
}

func deviceIDPath() string {
	if path := strings.TrimSpace(os.Getenv("KIMI_CODE_DEVICE_ID_FILE")); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".maclaw", "kimi-code-device-id")
	}
	return filepath.Join(home, ".maclaw", "kimi-code-device-id")
}

func newDeviceID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "maclaw-device"
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:])
}

func asciiHeader(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if r >= 0x20 && r <= 0x7e {
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func intField(payload map[string]any, key string) int {
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func errorDetail(payload map[string]any) string {
	if payload == nil {
		return "unknown"
	}
	if desc := stringField(payload, "error_description"); desc != "" {
		return desc
	}
	if code := stringField(payload, "error"); code != "" {
		return code
	}
	if msg := stringField(payload, "message"); msg != "" {
		return msg
	}
	return "unknown"
}
