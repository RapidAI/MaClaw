package gitforge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// AuthorizeScopes checks the scopes a person is about to approve.
// An empty request uses the development set: code, issues, pull requests,
// pipelines, and organization listing. Anything outside that set is refused.
func AuthorizeScopes(service, requested string) (string, error) {
	service = strings.ToLower(strings.TrimSpace(service))
	defaults := []string{"read:user", "repo", "workflow", "read:org"}
	allowed := map[string]bool{
		"read:user": true, "repo": true, "public_repo": true, "workflow": true, "read:org": true,
	}
	if service == GitLab {
		defaults = []string{"read_user", "api"}
		allowed = map[string]bool{
			"read_user": true, "api": true, "read_api": true, "read_repository": true, "write_repository": true,
		}
	} else if service != GitHub {
		return "", fmt.Errorf("host must be github or gitlab")
	}
	parts := defaults
	if strings.TrimSpace(requested) != "" {
		parts = strings.Fields(strings.ReplaceAll(requested, ",", " "))
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		if !allowed[part] {
			return "", fmt.Errorf("scope %s is not available", part)
		}
		seen[part] = true
		out = append(out, part)
	}
	if seen["repo"] {
		filtered := out[:0]
		for _, part := range out {
			if part != "public_repo" {
				filtered = append(filtered, part)
			}
		}
		out = filtered
	}
	if len(out) == 0 {
		return "", fmt.Errorf("scope is empty")
	}
	return strings.Join(out, " "), nil
}

var userCodeRe = regexp.MustCompile(`^[A-Za-z0-9-]{4,32}$`)

// AuthorizeLink is the page the person opens. The code is filled in.
// Only the forge's own host is used, never a host from an API response.
func AuthorizeLink(service, gitlabHost, userCode string) (string, error) {
	if !userCodeRe.MatchString(userCode) {
		return "", fmt.Errorf("user code is invalid")
	}
	q := url.QueryEscape(userCode)
	if service == GitLab {
		base, err := gitlabOrigin(gitlabHost)
		if err != nil {
			return "", err
		}
		return base + "/oauth/authorize_device?user_code=" + q, nil
	}
	if service != GitHub {
		return "", fmt.Errorf("host must be github or gitlab")
	}
	return "https://github.com/login/device?user_code=" + q, nil
}

func gitlabOrigin(raw string) (string, error) {
	saved, err := parseGitLabHost(raw)
	if err != nil {
		return "", err
	}
	if saved == nil {
		return "https://gitlab.com", nil
	}
	return saved.base, nil
}

// StartGitLabDevice begins a GitLab device-code login. The device code is
// for the poller only and must not be shown to the model or the person.
func StartGitLabDevice(ctx context.Context, client *http.Client, gitlabHost, clientID, scope string) (userCode, deviceCode string, interval int, err error) {
	base, err := gitlabOrigin(gitlabHost)
	if err != nil {
		return "", "", 0, err
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" || strings.ContainsAny(clientID, "\r\n\x00 ") {
		return "", "", 0, fmt.Errorf("gitlab oauth client is not configured")
	}
	form := url.Values{"client_id": {clientID}, "scope": {scope}}
	body, status, err := postForm(ctx, client, base+"/oauth/authorize_device", form)
	if err != nil || status != http.StatusOK {
		return "", "", 0, fmt.Errorf("gitlab device code was refused")
	}
	var row struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		Interval   int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &row); err != nil || row.DeviceCode == "" || row.UserCode == "" {
		return "", "", 0, fmt.Errorf("gitlab device code was refused")
	}
	if row.Interval < 1 {
		row.Interval = 5
	}
	return row.UserCode, row.DeviceCode, row.Interval, nil
}

// PollGitLabDevice waits until the person approves or the code expires.
// The access token is the return value and is not included in an error.
func PollGitLabDevice(ctx context.Context, client *http.Client, gitlabHost, clientID, clientSecret, deviceCode string, interval int) (string, error) {
	base, err := gitlabOrigin(gitlabHost)
	if err != nil {
		return "", err
	}
	if interval < 1 {
		interval = 5
	}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("gitlab authorization timed out")
		case <-ticker.C:
			token, retry, slow, err := exchangeGitLabDevice(ctx, client, base, clientID, clientSecret, deviceCode)
			if err != nil {
				return "", err
			}
			if slow {
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
			}
			if !retry {
				return token, nil
			}
		}
	}
}

func exchangeGitLabDevice(ctx context.Context, client *http.Client, base, clientID, clientSecret, deviceCode string) (token string, retry, slow bool, err error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	if strings.TrimSpace(clientSecret) != "" {
		form.Set("client_secret", clientSecret)
	}
	body, status, err := postForm(ctx, client, base+"/oauth/token", form)
	if err != nil || status >= 500 || status == 0 {
		return "", true, false, nil
	}
	if status != http.StatusOK && status != http.StatusBadRequest {
		return "", false, false, fmt.Errorf("gitlab authorization was refused")
	}
	var row struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &row); err != nil {
		return "", false, false, fmt.Errorf("gitlab authorization was refused")
	}
	switch row.Error {
	case "":
		if row.AccessToken == "" {
			return "", false, false, fmt.Errorf("gitlab authorization was refused")
		}
		return row.AccessToken, false, false, nil
	case "authorization_pending":
		return "", true, false, nil
	case "slow_down":
		return "", true, true, nil
	case "expired_token":
		return "", false, false, fmt.Errorf("gitlab authorization expired")
	case "access_denied":
		return "", false, false, fmt.Errorf("gitlab authorization denied")
	default:
		return "", false, false, fmt.Errorf("gitlab authorization was refused")
	}
}

func postForm(ctx context.Context, client *http.Client, endpoint string, form url.Values) ([]byte, int, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: refuseRedirect}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("authorization request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("authorization request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("authorization request failed")
	}
	return body, resp.StatusCode, nil
}
