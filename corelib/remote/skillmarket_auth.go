package remote

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

// SkillMarketAuthClient provides authentication operations for SkillMarket.
// Shared by GUI, TUI, and MaClawSrv upload paths.
type SkillMarketAuthClient struct {
	client *http.Client
}

// NewSkillMarketAuthClient creates a new auth client using the shared hub HTTP client.
func NewSkillMarketAuthClient() *SkillMarketAuthClient {
	return &SkillMarketAuthClient{client: NewHubHTTPClient()}
}

// SkillMarketAuthResult holds the result of a successful login/verify operation.
type SkillMarketAuthResult struct {
	SessionToken string    `json:"session_token"`
	Email        string    `json:"email"`
	UserID       string    `json:"user_id"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Register creates a new SkillMarket account with email and password.
// The user will receive an activation email.
func (c *SkillMarketAuthClient) Register(ctx context.Context, baseURL, email, password string) error {
	payload, _ := json.Marshal(map[string]string{
		"email":    strings.TrimSpace(email),
		"password": password,
	})
	resp, err := c.doPost(ctx, baseURL+"/api/v1/auth/register", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}
	return c.parseError(resp)
}

// Login authenticates with email + password and returns a session token.
func (c *SkillMarketAuthClient) Login(ctx context.Context, baseURL, email, password string) (*SkillMarketAuthResult, error) {
	payload, _ := json.Marshal(map[string]string{
		"email":    strings.TrimSpace(email),
		"password": password,
	})
	resp, err := c.doPost(ctx, baseURL+"/api/v1/auth/login", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}
	var result SkillMarketAuthResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode login response: %w", err)
	}
	return &result, nil
}

// SendLookup sends a verification email for passwordless login.
// The user clicks the link in the email to get a session token.
func (c *SkillMarketAuthClient) SendLookup(ctx context.Context, baseURL, email string) error {
	payload, _ := json.Marshal(map[string]string{
		"email": strings.TrimSpace(email),
	})
	resp, err := c.doPost(ctx, baseURL+"/api/v1/auth/lookup", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return c.parseError(resp)
}

// VerifyIdentity exchanges a verification token (from email link) for a session token.
func (c *SkillMarketAuthClient) VerifyIdentity(ctx context.Context, baseURL, verifyToken string) (*SkillMarketAuthResult, error) {
	endpoint := baseURL + "/api/v1/auth/verify-identity?token=" + url.QueryEscape(verifyToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("verify identity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}
	var result SkillMarketAuthResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode verify response: %w", err)
	}
	return &result, nil
}

// ValidateToken checks if a session token is still valid.
// Returns true if valid, false if expired/invalid.
func (c *SkillMarketAuthClient) ValidateToken(ctx context.Context, baseURL, token string) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v1/auth/session", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("validate token: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK, nil
}

// MachineLogin exchanges Hub enrollment credentials (email + machine_id + viewer_token)
// for a SkillMarket session token. This enables zero-friction SkillMarket access
// after Hub registration — no separate login step required.
func (c *SkillMarketAuthClient) MachineLogin(ctx context.Context, baseURL, email, machineID, viewerToken string) (*SkillMarketAuthResult, error) {
	payload, _ := json.Marshal(map[string]string{
		"email":        strings.TrimSpace(email),
		"machine_id":   strings.TrimSpace(machineID),
		"viewer_token": strings.TrimSpace(viewerToken),
	})
	resp, err := c.doPost(ctx, baseURL+"/api/v1/auth/machine-login", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp)
	}
	var result SkillMarketAuthResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode machine-login response: %w", err)
	}
	return &result, nil
}

// EnsureValidToken checks if the given token is valid. If not, returns an error
// with instructions for the user to log in.
func (c *SkillMarketAuthClient) EnsureValidToken(ctx context.Context, baseURL, email, token string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", &SkillMarketAuthRequiredError{Email: email, BaseURL: baseURL}
	}
	valid, err := c.ValidateToken(ctx, baseURL, token)
	if err != nil {
		// Network error — return token as-is, let the upload attempt handle it
		return token, nil
	}
	if valid {
		return token, nil
	}
	return "", &SkillMarketTokenExpiredError{Email: email, BaseURL: baseURL}
}

// SkillMarketAuthRequiredError indicates the user needs to log in.
type SkillMarketAuthRequiredError struct {
	Email   string
	BaseURL string
}

func (e *SkillMarketAuthRequiredError) Error() string {
	return fmt.Sprintf("未登录 SkillMarket。请先登录:\n"+
		"  maclaw-tui skillmarket login --email %s --password <密码>\n"+
		"  或使用邮件验证: maclaw-tui skillmarket lookup --email %s",
		e.Email, e.Email)
}

// SkillMarketTokenExpiredError indicates the session has expired.
type SkillMarketTokenExpiredError struct {
	Email   string
	BaseURL string
}

func (e *SkillMarketTokenExpiredError) Error() string {
	return fmt.Sprintf("SkillMarket session 已过期，请重新登录:\n"+
		"  maclaw-tui skillmarket login --email %s --password <密码>\n"+
		"  或使用邮件验证: maclaw-tui skillmarket lookup --email %s",
		e.Email, e.Email)
}

func (c *SkillMarketAuthClient) doPost(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	return resp, nil
}

func (c *SkillMarketAuthClient) parseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	// Try to extract error message from JSON response
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Error != "" {
			msg = errResp.Error
		} else if errResp.Message != "" {
			msg = errResp.Message
		}
	}
	return fmt.Errorf("SkillMarket API error (HTTP %d): %s", resp.StatusCode, msg)
}
