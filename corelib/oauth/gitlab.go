package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// GitLab account — Personal Access Token connection. GitLab has no device
// flow and OAuth apps must be registered per instance, so the desktop keeps
// a PAT the user mints in the browser. The token is validated against
// /api/v4/user and stored under the "gitlab" provider alongside its origin
// (gitlab.com or a self-hosted host).
// ─────────────────────────────────────────────────────────────────────────────

// GitLabAccountProviderID is the credential store provider id for the
// GitLab connection.
const GitLabAccountProviderID = "gitlab"

// GitLabDefaultOrigin is used when the user leaves the origin field empty.
const GitLabDefaultOrigin = "https://gitlab.com"

// GitLabUser is the subset of GET /api/v4/user surfaced in the settings UI.
// email comes back for the token's own user when the token has read_user.
type GitLabUser struct {
	Username  string `json:"username"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
	WebURL    string `json:"web_url"`
}

// GitLabTokenInfo is the subset of GET /api/v4/personal_access_tokens/self
// used to report the PAT's scopes and expiry in the UI.
type GitLabTokenInfo struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt string   `json:"expires_at"`
	Active    bool     `json:"active"`
	Revoked   bool     `json:"revoked"`
}

// NormalizeGitLabOrigin reduces the accepted origin forms to
// "https://<host>". Empty input falls back to gitlab.com. Only site roots
// are accepted — GitLab's API paths are derived from the origin, so a
// pasted project URL would silently hit the wrong endpoints.
func NormalizeGitLabOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = GitLabDefaultOrigin
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") || strings.Contains(lower, "git@") {
		return "", fmt.Errorf("GitLab 地址只接受 HTTPS，例如 https://gitlab.com")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || parsed.Host == "" || !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("GitLab 地址只接受 HTTPS，例如 https://gitlab.com")
	}
	if path := strings.Trim(parsed.Path, "/"); path != "" {
		return "", fmt.Errorf("GitLab 地址只填站点地址（例如 https://gitlab.example.com），不要带仓库或页面路径")
	}
	return strings.ToLower(parsed.Scheme) + "://" + parsed.Host, nil
}

// gitLabAPI does one authenticated api/v4 request and returns the body.
// A 401/403 is reported as a revoked token so callers can drop it.
func gitLabAPI(ctx context.Context, origin, token, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", origin+"/api/v4/"+strings.TrimPrefix(path, "/"), nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab api: create request: %w", err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MaClaw/1.0")

	resp, err := DoNoFollow(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab api: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("gitlab api: read response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("gitlab api: token is invalid or revoked (HTTP %d)", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("gitlab api: HTTP %d: body_len=%d", resp.StatusCode, len(body))
	}
	return body, nil
}

// FetchGitLabUser returns the account the PAT belongs to.
func FetchGitLabUser(ctx context.Context, origin, token string) (*GitLabUser, error) {
	body, err := gitLabAPI(ctx, origin, token, "user")
	if err != nil {
		return nil, err
	}
	var user GitLabUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("gitlab /user: parse response: %w", err)
	}
	if user.Username == "" {
		return nil, fmt.Errorf("gitlab /user: missing username in response")
	}
	return &user, nil
}

// FetchGitLabTokenInfo reads the PAT's own record for scopes and expiry.
// The endpoint needs an api or read_api scope, so callers treat errors as
// "scope unknown", not as a dead token.
func FetchGitLabTokenInfo(ctx context.Context, origin, token string) (*GitLabTokenInfo, error) {
	body, err := gitLabAPI(ctx, origin, token, "personal_access_tokens/self")
	if err != nil {
		return nil, err
	}
	var info GitLabTokenInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("gitlab token info: parse response: %w", err)
	}
	return &info, nil
}

// GitLabScopesHaveRepo reports the token can clone and push over HTTPS:
// api covers everything, write_repository covers pushes, and a stored empty
// scope list means the scope could not be read — treated as OK because the
// srv-side clone will surface a real permission error anyway.
func GitLabScopesHaveRepo(scopeList string) bool {
	scopeList = strings.TrimSpace(scopeList)
	if scopeList == "" {
		return true
	}
	for _, part := range strings.FieldsFunc(scopeList, func(r rune) bool {
		return r == ',' || r == ' '
	}) {
		if part == "api" || part == "write_repository" {
			return true
		}
	}
	return false
}

// GitLabTokenPageURL is where the user mints a PAT. GitLab 16+ hosts it
// under /-/user_settings; an older self-hosted instance renders a redirect
// or a 404 the user can navigate from. The name/scopes query params prefill
// the creation form (GitLab 15.5+); an older instance ignores them.
func GitLabTokenPageURL(origin string) string {
	return strings.TrimSuffix(origin, "/") + "/-/user_settings/personal_access_tokens?name=MaClaw&scopes=api"
}
