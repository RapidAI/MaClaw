package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pkg/browser"
)

// ErrXAIDeviceFlowUnavailable means auth.x.ai has no RFC 8628 device endpoint.
// Callers fall back to the loopback authorization-code flow.
var ErrXAIDeviceFlowUnavailable = errors.New("xai device flow is not enabled")

const (
	xaiDeviceGrantType       = "urn:ietf:params:oauth:grant-type:device_code"
	xaiDeviceDefaultInterval = 5 * time.Second
	xaiDeviceMinInterval     = time.Second
	// These match the headers grok-build sends on the device grant. The live
	// auth.x.ai device endpoint accepted this surface for the Grok Build client.
	xaiGrokClientVersion = "maclaw"
	xaiGrokClientSurface = "ui"
)

// sleepDevicePoll is replaced in tests so a poll loop does not wait on the
// server-supplied interval.
var sleepDevicePoll = sleepWithContext

type xaiDeviceCode struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresIn               time.Duration
}

type xaiDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
	Error                   string `json:"error"`
	ErrorDesc               string `json:"error_description"`
}

// RunXAIDeviceFlowCtx signs in with xAI's device authorization grant.
// The browser opens an approval page that already contains the user code.
// This process polls the token endpoint until that approval lands, so the
// user does not copy a code back into MaClaw and the browser does not have
// to reach a loopback port.
func RunXAIDeviceFlowCtx(ctx context.Context) (*TokenResult, error) {
	cfg := XAIConfig()
	discovery, err := DiscoverOIDCEndpoints(ctx, XAIOAuthIssuer)
	if err != nil {
		return nil, err
	}
	cfg.TokenEndpoint = discovery.TokenEndpoint
	return runXAIDeviceFlow(ctx, cfg, browser.OpenURL)
}

func runXAIDeviceFlow(ctx context.Context, cfg Config, openURL func(string) error) (*TokenResult, error) {
	deviceEndpoint, err := deviceCodeEndpoint(cfg.TokenEndpoint)
	if err != nil {
		return nil, err
	}
	code, err := requestXAIDeviceCode(ctx, deviceEndpoint, cfg.ClientID, cfg.Scopes)
	if err != nil {
		return nil, err
	}
	approvalURL, err := xaiBrowserApprovalURL(code)
	if err != nil {
		return nil, err
	}
	if openURL == nil {
		return nil, fmt.Errorf("xai oauth: browser launcher is missing")
	}
	if err := openURL(approvalURL); err != nil {
		return nil, fmt.Errorf("xai oauth: open browser: %w", err)
	}
	log.Printf("[OAuth] xAI device login opened the browser; waiting for approval")
	result, err := pollXAIDeviceToken(ctx, cfg.TokenEndpoint, cfg.ClientID, code.DeviceCode, code.Interval, code.ExpiresIn)
	if err != nil {
		return nil, err
	}
	log.Printf("[OAuth] xAI device login approved")
	return result, nil
}

func deviceCodeEndpoint(tokenEndpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(tokenEndpoint))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("xai oauth: token endpoint is invalid")
	}
	if !strings.HasSuffix(u.Path, "/token") {
		return "", fmt.Errorf("xai oauth: token endpoint %q has no /token path", u.Path)
	}
	u.Path = strings.TrimSuffix(u.Path, "/token") + "/device/code"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func requestXAIDeviceCode(ctx context.Context, endpoint, clientID string, scopes []string) (*xaiDeviceCode, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", strings.Join(scopes, " "))
	form.Set("referrer", "grok-build")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("xai oauth: device code request: %w", err)
	}
	setXAIDeviceHeaders(req)
	resp, err := DoNoFollow(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, oauthWaitErr(ctx)
		}
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("xai oauth: device code read: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrXAIDeviceFlowUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xai oauth: device code failed (HTTP %d): %s", resp.StatusCode, truncateBody(body, 512))
	}
	var raw xaiDeviceCodeResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("xai oauth: device code parse: %w", err)
	}
	raw.UserCode = strings.TrimSpace(raw.UserCode)
	if strings.TrimSpace(raw.DeviceCode) == "" || !validXAIUserCode(raw.UserCode) {
		return nil, fmt.Errorf("xai oauth: device code response is missing device_code or user_code")
	}
	interval := time.Duration(raw.Interval) * time.Second
	if interval < xaiDeviceMinInterval {
		interval = xaiDeviceDefaultInterval
	}
	expires := time.Duration(raw.ExpiresIn) * time.Second
	if expires < time.Minute {
		expires = 15 * time.Minute
	}
	return &xaiDeviceCode{
		DeviceCode:              raw.DeviceCode,
		UserCode:                raw.UserCode,
		VerificationURI:         raw.VerificationURI,
		VerificationURIComplete: raw.VerificationURIComplete,
		Interval:                interval,
		ExpiresIn:               expires,
	}, nil
}

func xaiBrowserApprovalURL(code *xaiDeviceCode) (string, error) {
	if code == nil {
		return "", fmt.Errorf("xai oauth: device code is missing")
	}
	for _, raw := range []string{code.VerificationURIComplete, code.VerificationURI} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if ok, err := xaiApprovalURLAllowed(raw); err != nil {
			return "", err
		} else if ok {
			return raw, nil
		}
	}
	return "", fmt.Errorf("xai oauth: device login did not return an accounts.x.ai approval page")
}

func validXAIUserCode(code string) bool {
	if code == "" {
		return false
	}
	for _, r := range code {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

func xaiApprovalURLAllowed(raw string) (bool, error) {
	if strings.IndexFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return false, fmt.Errorf("xai oauth: approval URL is invalid")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false, fmt.Errorf("xai oauth: approval URL is invalid")
	}
	if u.Scheme != "https" {
		return false, nil
	}
	switch strings.ToLower(u.Hostname()) {
	case "accounts.x.ai", "auth.x.ai", "console.x.ai":
		return true, nil
	default:
		return false, nil
	}
}

func pollXAIDeviceToken(ctx context.Context, tokenEndpoint, clientID, deviceCode string, interval, expires time.Duration) (*TokenResult, error) {
	if interval < xaiDeviceMinInterval {
		interval = xaiDeviceDefaultInterval
	}
	if expires < time.Minute {
		expires = 15 * time.Minute
	}
	deadline := time.Now().Add(expires)
	for {
		if err := sleepDevicePoll(ctx, interval); err != nil {
			return nil, oauthWaitErr(ctx)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("xai oauth: device login expired")
		}
		result, retry, wait, err := postXAIDeviceToken(ctx, tokenEndpoint, clientID, deviceCode)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		if wait > 0 {
			interval += wait
		}
		if !retry {
			return nil, fmt.Errorf("xai oauth: device login stopped")
		}
	}
}

func postXAIDeviceToken(ctx context.Context, tokenEndpoint, clientID, deviceCode string) (result *TokenResult, retry bool, slowDown time.Duration, err error) {
	form := url.Values{}
	form.Set("grant_type", xaiDeviceGrantType)
	form.Set("device_code", deviceCode)
	form.Set("client_id", clientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, false, 0, fmt.Errorf("xai oauth: device token request: %w", err)
	}
	setXAIDeviceHeaders(req)
	resp, err := DoNoFollow(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, 0, oauthWaitErr(ctx)
		}
		// A blip while the user is still approving should not end the login.
		return nil, true, 0, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, true, 0, nil
	}
	if resp.StatusCode == http.StatusOK {
		var tok tokenResponse
		if err := json.Unmarshal(body, &tok); err != nil {
			return nil, false, 0, fmt.Errorf("xai oauth: device token parse: %w", err)
		}
		if tok.AccessToken == "" {
			return nil, false, 0, fmt.Errorf("xai oauth: device token response missing access_token")
		}
		if tok.ExpiresIn <= 0 {
			tok.ExpiresIn = 3600
		}
		return &TokenResult{
			AccessToken:    tok.AccessToken,
			RawAccessToken: tok.AccessToken,
			RefreshToken:   strings.TrimSpace(tok.RefreshToken),
			ExpiresIn:      tok.ExpiresIn,
		}, false, 0, nil
	}
	if resp.StatusCode >= 500 {
		return nil, true, 0, nil
	}
	var tok tokenResponse
	if json.Unmarshal(body, &tok) != nil || tok.Error == "" {
		return nil, false, 0, fmt.Errorf("xai oauth: device token failed (HTTP %d): %s", resp.StatusCode, truncateBody(body, 512))
	}
	switch tok.Error {
	case "authorization_pending":
		return nil, true, 0, nil
	case "slow_down":
		return nil, true, 5 * time.Second, nil
	case "access_denied":
		return nil, false, 0, fmt.Errorf("xai oauth: authorization denied")
	case "expired_token":
		return nil, false, 0, fmt.Errorf("xai oauth: device login expired")
	default:
		desc := tok.ErrorDesc
		if desc == "" {
			desc = tok.Error
		}
		return nil, false, 0, fmt.Errorf("xai oauth: device token failed: %s", desc)
	}
}

func setXAIDeviceHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("x-grok-client-version", xaiGrokClientVersion)
	req.Header.Set("x-grok-client-surface", xaiGrokClientSurface)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
