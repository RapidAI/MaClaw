package guiapp

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"

	"github.com/pkg/browser"
)

// ─────────────────────────────────────────────────────────────────────────────
// GitLab 连接 — Wails Bindings (Personal Access Token)
// GitLab 没有设备码流程，OAuth 应用又必须按实例注册，所以账号登录是
// 浏览器辅助 PAT：面板打开令牌创建页，用户粘贴令牌，后端验证并存入
// credentials.json 的 "gitlab" provider（BaseURL 记 origin，支持自建实例）。
// ─────────────────────────────────────────────────────────────────────────────

// GitLabConnectionStatus 是设置页 GitHub & GitLab tab 展示的 GitLab 连接状态。
type GitLabConnectionStatus struct {
	Connected bool   `json:"connected"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	HTMLURL   string `json:"html_url,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Scopes    string `json:"scopes,omitempty"`
}

// gitlabCredential returns the stored GitLab credential, or nil.
func (a *App) gitlabCredential() *oauth.StoredCredential {
	if a.credentialStore == nil {
		return nil
	}
	cred, err := a.credentialStore.Read(oauth.GitLabAccountProviderID)
	if err != nil || cred == nil {
		return nil
	}
	return cred
}

// gitlabConnectionStatus builds the status view from the stored credential.
func (a *App) gitlabConnectionStatus() GitLabConnectionStatus {
	cred := a.gitlabCredential()
	if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		return GitLabConnectionStatus{}
	}
	return GitLabConnectionStatus{
		Connected: true,
		Login:     cred.Login,
		Name:      cred.DisplayName,
		AvatarURL: cred.AvatarURL,
		HTMLURL:   cred.HTMLURL,
		Origin:    cred.BaseURL,
		Scopes:    cred.Scopes,
	}
}

// GetGitLabConnection returns the stored connection state. With refresh the
// account profile is re-fetched so renamed users / revoked PATs surface.
func (a *App) GetGitLabConnection(refresh bool) GitLabConnectionStatus {
	status := a.gitlabConnectionStatus()
	if !refresh || !status.Connected {
		return status
	}
	cred := a.gitlabCredential()
	if cred == nil {
		// A concurrent DisconnectGitLab can land between the status read and
		// this fetch — report the (now empty) state instead of dereferencing.
		return a.gitlabConnectionStatus()
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user, err := oauth.FetchGitLabUser(ctx, cred.BaseURL, strings.TrimSpace(cred.AccessToken))
	if err != nil {
		log.Printf("[gitlab] connection refresh failed: %v", err)
		if strings.Contains(err.Error(), "invalid or revoked") {
			_ = a.credentialStore.Delete(oauth.GitLabAccountProviderID)
			return a.gitlabConnectionStatus()
		}
		return status // transient error: keep the stored snapshot
	}
	_ = a.credentialStore.Modify(oauth.GitLabAccountProviderID, func(old *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		if old == nil {
			return nil, nil
		}
		old.Login = user.Username
		old.DisplayName = user.Name
		old.UserID = strconv.FormatInt(user.ID, 10)
		old.Email = strings.TrimSpace(user.Email)
		old.AvatarURL = user.AvatarURL
		old.HTMLURL = user.WebURL
		return old, nil
	})
	return a.gitlabConnectionStatus()
}

// ConnectGitLab validates a pasted PAT against the origin and persists it as
// the GitLab account. The token page is opened by OpenGitLabTokenPage.
func (a *App) ConnectGitLab(origin, token string) (GitLabConnectionStatus, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return GitLabConnectionStatus{}, fmt.Errorf("请粘贴 GitLab 访问令牌")
	}
	normalized, err := oauth.NormalizeGitLabOrigin(origin)
	if err != nil {
		return GitLabConnectionStatus{}, err
	}
	if a.credentialStore == nil {
		return GitLabConnectionStatus{}, fmt.Errorf("credential store 未初始化")
	}
	if cfg, err := a.LoadConfig(); err == nil {
		oauth.ApplyProxyFromAppConfig(cfg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user, err := oauth.FetchGitLabUser(ctx, normalized, token)
	if err != nil {
		return GitLabConnectionStatus{}, fmt.Errorf("GitLab 令牌验证失败: %w", err)
	}

	// Scopes are best-effort: a PAT without read_api cannot introspect itself,
	// and an empty list is treated as "unknown" rather than a missing grant.
	scopes := ""
	if info, infoErr := oauth.FetchGitLabTokenInfo(ctx, normalized, token); infoErr == nil {
		scopes = strings.Join(info.Scopes, ",")
	}

	if err := a.credentialStore.Modify(oauth.GitLabAccountProviderID, func(_ *oauth.StoredCredential) (*oauth.StoredCredential, error) {
		return &oauth.StoredCredential{
			Type:        "token",
			AccessToken: token,
			BaseURL:     normalized,
			Login:       user.Username,
			DisplayName: user.Name,
			UserID:      strconv.FormatInt(user.ID, 10),
			Email:       strings.TrimSpace(user.Email),
			AvatarURL:   user.AvatarURL,
			HTMLURL:     user.WebURL,
			Scopes:      scopes,
		}, nil
	}); err != nil {
		return GitLabConnectionStatus{}, err
	}
	go a.syncCloudAgentGitLabCredentials()
	log.Printf("[gitlab] connected account=%s origin=%s scopes=%s", user.Username, normalized, scopes)
	return a.gitlabConnectionStatus(), nil
}

// DisconnectGitLab removes the stored credential (logout).
func (a *App) DisconnectGitLab() error {
	if a.credentialStore == nil {
		return fmt.Errorf("credential store 未初始化")
	}
	return a.credentialStore.Delete(oauth.GitLabAccountProviderID)
}

// OpenGitLabTokenPage opens the PAT creation page for the (possibly rough)
// origin in the system browser, so the user never has to find the URL.
func (a *App) OpenGitLabTokenPage(origin string) error {
	normalized, err := oauth.NormalizeGitLabOrigin(origin)
	if err != nil {
		return err
	}
	return browser.OpenURL(oauth.GitLabTokenPageURL(normalized))
}
