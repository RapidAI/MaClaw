package guiapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// newGitHubHTTPClient is the write-client transport. It never follows
// redirects: a 3xx must not replay the bearer token onto another host.
// Tests replace it to point the dialer at httptest without changing the host pin.
var newGitHubHTTPClient = defaultGitHubHTTPClient

func defaultGitHubHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// gitHubHookSpec is the repository webhook we register with the user's token.
// Secret is the unpadded base64url string, sent as config.secret on every
// create and every PATCH. GitHub clears the secret when a PATCH omits it.
type gitHubHookSpec struct {
	URL    string
	Secret string
	Events []string
	Active bool
}

func gitHubHookBody(spec gitHubHookSpec) map[string]any {
	events := spec.Events
	if events == nil {
		events = []string{}
	}
	return map[string]any{
		"name":   "web",
		"active": spec.Active,
		"events": events,
		"config": map[string]string{
			"url":          spec.URL,
			"content_type": "json",
			"insecure_ssl": "0",
			"secret":       spec.Secret,
		},
	}
}

// canonicalGitHubOwnerRepo parses owner and repo from the canonical clone URL.
// normalizeForgeCloneURL only returns the repo name; comparing that alone
// would treat another owner's same-named repo as this checkout. This is the
// GitHub webhook path, so GitLab origins stay refused even when a GitLab
// account is connected (empty gitlabOrigin).
func canonicalGitHubOwnerRepo(raw string) (owner, repo string, err error) {
	cloneURL, repoName, service, err := normalizeForgeCloneURL(raw, "")
	if err != nil {
		return "", "", err
	}
	if service != cloneServiceGitHub {
		return "", "", fmt.Errorf("仅支持克隆 github.com 仓库")
	}
	parsed, err := url.Parse(cloneURL)
	if err != nil {
		return "", "", err
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("仓库地址缺少 owner/repo")
	}
	owner = segments[0]
	repo = strings.TrimSuffix(segments[1], ".git")
	if repo == "" {
		repo = repoName
	}
	return owner, repo, nil
}

func sameGitHubRepo(origin, owner, repo string) bool {
	gotOwner, gotRepo, err := canonicalGitHubOwnerRepo(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(gotOwner, owner) && strings.EqualFold(gotRepo, repo)
}

func gitHubReposURL(owner, repo string, parts ...string) (string, error) {
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" || strings.ContainsAny(owner, "/?#") || strings.ContainsAny(repo, "/?#") {
		return "", fmt.Errorf("github api: invalid owner or repo")
	}
	base, err := url.Parse("https://api.github.com/")
	if err != nil {
		return "", err
	}
	segs := append([]string{"repos", owner, repo}, parts...)
	return base.JoinPath(segs...).String(), nil
}

// doGitHubJSON performs a write against api.github.com. Hosts other than
// api.github.com, including suffixes such as api.github.com.evil.com, are
// rejected before a connection is opened. Redirects are returned as errors
// and are not followed.
func (a *App) doGitHubJSON(method, rawURL string, payload any) (int, []byte, error) {
	status, body, _, err := a.doGitHubExchange(method, rawURL, payload)
	return status, body, err
}

// doGitHubExchange is doGitHubJSON plus the response headers. Comment pages
// and replay continuation read Link. The host pin is unchanged.
func (a *App) doGitHubExchange(method, rawURL string, payload any) (int, []byte, http.Header, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "api.github.com") {
		return 0, nil, nil, fmt.Errorf("github api: only https api.github.com paths are allowed")
	}
	token := ""
	if a != nil {
		token = a.loadGitHubAccessToken()
	}
	if token == "" {
		return 0, nil, nil, fmt.Errorf("GitHub 未连接")
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(strings.ToUpper(method), parsed.String(), body)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MaClaw/1.0")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := newGitHubHTTPClient()
	if client == nil {
		client = defaultGitHubHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("github api: request failed: %w", err)
	}
	defer resp.Body.Close()
	hdr := resp.Header.Clone()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return resp.StatusCode, nil, hdr, fmt.Errorf("github api: unexpected redirect (HTTP %d)", resp.StatusCode)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return resp.StatusCode, nil, hdr, fmt.Errorf("github api: read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized && strings.Contains(strings.ToLower(string(respBody)), "invalid or revoked") {
		a.clearGitHubCredential()
		return resp.StatusCode, respBody, hdr, fmt.Errorf("github api: HTTP 401: token is invalid or revoked")
	}
	if resp.StatusCode >= 400 {
		detail := strings.TrimSpace(string(respBody))
		if len(detail) > 512 {
			detail = detail[:512]
		}
		return resp.StatusCode, respBody, hdr, fmt.Errorf("github api: HTTP %d: %s", resp.StatusCode, detail)
	}
	return resp.StatusCode, respBody, hdr, nil
}

func (a *App) clearGitHubCredential() {
	if a == nil || a.credentialStore == nil {
		return
	}
	_ = a.credentialStore.Delete(oauth.GitHubAccountProviderID)
	a.setGitHubTokenCache("")
}

func (a *App) createGitHubRepoHook(owner, repo string, spec gitHubHookSpec) (int64, error) {
	rawURL, err := gitHubReposURL(owner, repo, "hooks")
	if err != nil {
		return 0, err
	}
	_, body, err := a.doGitHubJSON(http.MethodPost, rawURL, gitHubHookBody(spec))
	if err != nil {
		return 0, err
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return 0, fmt.Errorf("github hook: parse id: %w", err)
	}
	if created.ID <= 0 {
		return 0, fmt.Errorf("github hook: missing id")
	}
	return created.ID, nil
}

func (a *App) updateGitHubRepoHook(owner, repo string, hookID int64, spec gitHubHookSpec) error {
	if hookID <= 0 {
		return fmt.Errorf("github hook: missing id")
	}
	rawURL, err := gitHubReposURL(owner, repo, "hooks", fmt.Sprintf("%d", hookID))
	if err != nil {
		return err
	}
	_, _, err = a.doGitHubJSON(http.MethodPatch, rawURL, gitHubHookBody(spec))
	return err
}

func (a *App) deleteGitHubRepoHook(owner, repo string, hookID int64) error {
	if hookID <= 0 {
		return fmt.Errorf("github hook: missing id")
	}
	rawURL, err := gitHubReposURL(owner, repo, "hooks", fmt.Sprintf("%d", hookID))
	if err != nil {
		return err
	}
	status, _, err := a.doGitHubJSON(http.MethodDelete, rawURL, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}
