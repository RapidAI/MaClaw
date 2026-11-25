package guiapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// CloudAgentRow is one cloud instance the task list can open.
// It is not a desktop machine and not a bot.
type CloudAgentRow struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// SrvNodeID is the private node maclawsrv this instance was provisioned
	// on; empty means the official (public) cloud service.
	SrvNodeID     string    `json:"srv_node_id,omitempty"`
	RuntimeStatus string    `json:"runtime_status"`
	LLMMode       string    `json:"llm_mode"`
	LLMLabel      string    `json:"llm_label"`
	LLMModel      string    `json:"llm_model"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// CloudAgentList is the signed-in user's cloud place.
// Enabled is the Hub switch. False means the new-task place does not offer 云端.
type CloudAgentList struct {
	Instances []CloudAgentRow `json:"instances"`
	Enabled   bool            `json:"enabled"`
}

// ListCloudAgents reads the signed-in user's cloud instances and the Hub switch.
func (a *App) ListCloudAgents() (CloudAgentList, error) {
	var payload CloudAgentList
	if err := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents", nil, &payload); err != nil {
		return CloudAgentList{}, err
	}
	if payload.Instances == nil {
		payload.Instances = []CloudAgentRow{}
	}
	return payload, nil
}

// CreateCloudAgent creates one session or coding instance.
// Kind is fixed by the Hub after this call. When the desktop has a 云端 Agent
// default model configured (设置 → 云端 Agent), it is applied to the new
// instance right after creation; a failed apply keeps the official model
// instead of failing the whole create.
func (a *App) CreateCloudAgent(name, kind string) (CloudAgentRow, error) {
	kind = strings.TrimSpace(kind)
	if kind != "session" && kind != "coding" {
		return CloudAgentRow{}, errors.New("实例类型无效")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents", map[string]string{
		"name":     strings.TrimSpace(name),
		"kind":     kind,
		"llm_mode": "official",
	}, &row)
	if err != nil {
		return row, err
	}
	if def, ok := a.cloudAgentDefaultLLM(); ok {
		if applied, applyErr := a.SetCloudAgentLLM(row.ID, def); applyErr != nil {
			log.Printf("[cloud-agent] apply default llm to %s: %v", row.ID, applyErr)
		} else {
			row = applied
		}
	}
	return row, nil
}

// cloudAgentDefaultLLM resolves the 云端 Agent global default model from the
// desktop config. It matches the provider by stable ID first, then by name,
// and skips hub-service rows or rows without real endpoint credentials — the
// same usability rule the settings panel applies. The second return reports
// whether a custom default should be applied at all.
func (a *App) cloudAgentDefaultLLM() (CloudLLMSet, bool) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return CloudLLMSet{}, false
	}
	providerID := strings.TrimSpace(cfg.CloudAgentLLMProvider)
	if providerID == "" {
		return CloudLLMSet{}, false
	}
	for _, provider := range cfg.MaclawLLMProviders {
		id, name := strings.TrimSpace(provider.ID), strings.TrimSpace(provider.Name)
		if id != providerID && name != providerID {
			continue
		}
		if provider.IsHubService || isHubServiceProviderName(name) || strings.TrimSpace(provider.URL) == "" || strings.TrimSpace(provider.Key) == "" {
			return CloudLLMSet{}, false
		}
		model := strings.TrimSpace(cfg.CloudAgentLLMModel)
		if model == "" {
			model = strings.TrimSpace(provider.Model)
		}
		return CloudLLMSet{
			URL:             provider.URL,
			Key:             provider.Key,
			Model:           model,
			Label:           name,
			Protocol:        provider.Protocol,
			WireAPI:         provider.WireAPI,
			AgentType:       provider.AgentType,
			ContextLength:   provider.ContextLength,
			TimeoutSec:      provider.TimeoutSec,
			MaxOutputTokens: provider.MaxOutputTokens,
		}, true
	}
	return CloudLLMSet{}, false
}

// RenameCloudAgent renames an instance.
func (a *App) RenameCloudAgent(instanceID, name string) (CloudAgentRow, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudAgentRow{}, errors.New("缺少云端实例")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPatch, "/api/v1/cloud-agents/"+url.PathEscape(instanceID), map[string]string{
		"name": strings.TrimSpace(name),
	}, &row)
	return row, err
}

// StopCloudAgent freezes an instance. Its workspace files remain browsable.
func (a *App) StopCloudAgent(instanceID string) (CloudAgentRow, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudAgentRow{}, errors.New("缺少云端实例")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/stop", map[string]string{}, &row)
	return row, err
}

// ResumeCloudAgent wakes a stopped instance.
func (a *App) ResumeCloudAgent(instanceID string) (CloudAgentRow, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudAgentRow{}, errors.New("缺少云端实例")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/resume", map[string]string{}, &row)
	return row, err
}

// DeleteCloudAgent removes an instance that never received a task.
// A failed create uses it so a retry does not fill the instance quota.
func (a *App) DeleteCloudAgent(instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errors.New("缺少云端实例")
	}
	err := a.remoteControlJSON(http.MethodDelete, "/api/v1/cloud-agents/"+url.PathEscape(instanceID), nil, nil)
	if err == nil {
		cloudAgentGrantCache.Lock()
		delete(cloudAgentGrantCache.byID, instanceID)
		cloudAgentGrantCache.Unlock()
	}
	return err
}

// OpenCloudAgentGrant opens this desktop's control grant for one instance.
// The grant id is the cloud page's playback key, together with the session key.
func (a *App) OpenCloudAgentGrant(instanceID string) (string, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return "", errors.New("缺少云端实例")
	}
	var payload struct {
		GrantID string `json:"grant_id"`
	}
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/control-grant", map[string]string{}, &payload)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.GrantID) == "" {
		return "", errors.New("没有打开云端控制")
	}
	return payload.GrantID, nil
}

// CloudLLMSet is one cloud instance's custom model choice. Key travels only
// to the user's own maclawsrv (hub forwards it without storing). The wire
// fields must ride along: a provider like 智谱编程 is anthropic-protocol and
// fails every turn when the srv falls back to chat/completions.
type CloudLLMSet struct {
	URL             string `json:"url"`
	Key             string `json:"key"`
	Model           string `json:"model"`
	Label           string `json:"label"`
	Protocol        string `json:"protocol,omitempty"`
	WireAPI         string `json:"wire_api,omitempty"`
	AgentType       string `json:"agent_type,omitempty"`
	ContextLength   int    `json:"context_length,omitempty"`
	TimeoutSec      int    `json:"timeout_sec,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

// SetCloudAgentLLM pins a cloud instance to one of the desktop's configured
// providers. The secret is stored only on the instance's srv.
func (a *App) SetCloudAgentLLM(instanceID string, in CloudLLMSet) (CloudAgentRow, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudAgentRow{}, errors.New("缺少云端实例")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/llm", in, &row)
	return row, err
}

// ResetCloudAgentLLM returns a cloud instance to the Hub's official model.
func (a *App) ResetCloudAgentLLM(instanceID string) (CloudAgentRow, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudAgentRow{}, errors.New("缺少云端实例")
	}
	var row CloudAgentRow
	err := a.remoteControlJSON(http.MethodPatch, "/api/v1/cloud-agents/"+url.PathEscape(instanceID), map[string]string{
		"llm_mode": "official",
	}, &row)
	return row, err
}

// CloudGitConnect is the programming-instance repository form.
// The token is sent once and is not part of CloudGitPublic.
type CloudGitConnect struct {
	Service     string `json:"service"`
	Origin      string `json:"origin"`
	Repo        string `json:"repo"`
	Ref         string `json:"ref,omitempty"`
	Token       string `json:"token,omitempty"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	// Source "account" binds the instance to the signed-in GitHub/GitLab
	// account so the credential sync can refresh the token after re-login.
	Source string `json:"source,omitempty"`
}

// CloudGitPublic is the repository view without a token.
type CloudGitPublic struct {
	Connected   bool   `json:"connected"`
	Service     string `json:"service,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Ref         string `json:"ref,omitempty"`
	AuthorName  string `json:"author_name,omitempty"`
	AuthorEmail string `json:"author_email,omitempty"`
	Source      string `json:"source,omitempty"`
}

// ConnectCloudAgentGit posts the coding credential to the user's cloud instance.
func (a *App) ConnectCloudAgentGit(instanceID string, body CloudGitConnect) (CloudGitPublic, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudGitPublic{}, errors.New("缺少云端实例")
	}
	if err := a.prepareCloudGitAccountConnect(&body); err != nil {
		return CloudGitPublic{}, err
	}
	return a.postCloudAgentGit(instanceID, body)
}

// postCloudAgentGit is the trusted internal write path. Exported callers go
// through ConnectCloudAgentGit first so source="account" cannot be forged
// around a manually supplied token.
func (a *App) postCloudAgentGit(instanceID string, body CloudGitConnect) (CloudGitPublic, error) {
	var view CloudGitPublic
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/git", body, &view)
	return view, err
}

// prepareCloudGitAccountConnect resolves source="account" inside the backend.
// The webview chooses a repository and submits no token; the credential store
// is the only place where the GitHub/GitLab secret is read. This keeps the
// simple path independent of a live /user refresh and prevents token round-trips
// through the UI layer.
func (a *App) prepareCloudGitAccountConnect(body *CloudGitConnect) error {
	if body == nil || !strings.EqualFold(strings.TrimSpace(body.Source), "account") {
		return nil
	}
	if strings.TrimSpace(body.Token) != "" {
		return fmt.Errorf("使用已连接账号时不能同时提交手动令牌")
	}
	body.Source = "account"
	switch strings.ToLower(strings.TrimSpace(body.Service)) {
	case "github":
		cred := a.loadGitHubCredential()
		if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
			return fmt.Errorf("请先在设置中连接 GitHub")
		}
		if !oauth.GitHubScopesHaveRepo(cred.Scopes) {
			return fmt.Errorf("当前 GitHub 连接没有仓库权限，请在设置中断开 GitHub 后重新登录")
		}
		account := cloudGitHubAccountFromCredential(cred)
		body.Service = "github"
		body.Token = strings.TrimSpace(cred.AccessToken)
		if origin := strings.TrimSpace(body.Origin); origin == "" {
			body.Origin = "https://github.com"
		} else if !accountHTTPSRootOrigin(origin) {
			return fmt.Errorf("GitHub 账号令牌只能用于 github.com 的 HTTPS 站点地址；自建地址请使用手动令牌")
		} else if host := hostOfOrigin(origin); host != "github.com" && host != "www.github.com" {
			return fmt.Errorf("GitHub 账号令牌只能用于 github.com；自建地址请使用手动令牌")
		}
		if strings.TrimSpace(body.AuthorName) == "" {
			body.AuthorName = account.Name
		}
		if strings.TrimSpace(body.AuthorEmail) == "" {
			body.AuthorEmail = account.Email
		}
	case "gitlab":
		cred := a.gitlabCredential()
		if cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
			return fmt.Errorf("请先在设置中连接 GitLab")
		}
		if !oauth.GitLabScopesHaveRepo(cred.Scopes) {
			return fmt.Errorf("当前 GitLab 令牌没有仓库写权限，请在设置中重新连接 GitLab")
		}
		body.Service = "gitlab"
		body.Token = strings.TrimSpace(cred.AccessToken)
		accountOrigin := strings.TrimSpace(cred.BaseURL)
		accountHost := hostOfOrigin(accountOrigin)
		if !accountHTTPSRootOrigin(accountOrigin) {
			return fmt.Errorf("GitLab 连接缺少有效的 HTTPS 站点地址，请在设置中重新连接 GitLab")
		}
		if origin := strings.TrimSpace(body.Origin); origin == "" {
			body.Origin = accountOrigin
		} else if !accountHTTPSRootOrigin(origin) {
			return fmt.Errorf("GitLab 账号令牌只能用于已连接实例的 HTTPS 站点地址；其他服务器请使用手动令牌")
		} else if hostOfOrigin(origin) != accountHost {
			return fmt.Errorf("GitLab 账号令牌只能用于已连接的实例；其他服务器请使用手动令牌")
		}
		if strings.TrimSpace(body.AuthorName) == "" {
			body.AuthorName = strings.TrimSpace(cred.DisplayName)
			if body.AuthorName == "" {
				body.AuthorName = strings.TrimSpace(cred.Login)
			}
		}
		if strings.TrimSpace(body.AuthorEmail) == "" {
			body.AuthorEmail = cloudGitLabCommitEmail(cred)
		}
	default:
		return fmt.Errorf("代码平台只支持 GitHub 或 GitLab")
	}
	return nil
}

// accountHTTPSRootOrigin is the fail-closed origin check for account-bound
// credentials. The token may be embedded into the clone URL by the srv, so an
// account token must never ride plaintext HTTP, userinfo, query strings, or a
// non-root path even when the hostname happens to match.
func accountHTTPSRootOrigin(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || parsed.Host == "" || parsed.User != nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, "https") &&
		(parsed.Path == "" || parsed.Path == "/") &&
		parsed.RawQuery == "" && parsed.Fragment == ""
}

// GetCloudAgentGit reads the instance's git view (no token) — which repo is
// bound and whether the credential came from the signed-in account.
func (a *App) GetCloudAgentGit(instanceID string) (CloudGitPublic, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudGitPublic{}, errors.New("缺少云端实例")
	}
	var view CloudGitPublic
	err := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/git", nil, &view)
	return view, err
}

// CloudSrvNode is one of the signed-in user's own private maclawsrv
// candidates: a registered edge node that has srv installed.
type CloudSrvNode struct {
	NodeID  string `json:"node_id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Online  bool   `json:"online"`
}

// CloudSrvState is the user's cloud-srv choice plus the private nodes they
// may pick. An empty NodeID means the public (system default) maclawsrv.
type CloudSrvState struct {
	NodeID string         `json:"node_id"`
	Nodes  []CloudSrvNode `json:"nodes"`
}

// GetCloudSrvChoice reads the signed-in user's public/private choice and
// their srv-capable private nodes.
func (a *App) GetCloudSrvChoice() (CloudSrvState, error) {
	var payload CloudSrvState
	if err := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents/srv", nil, &payload); err != nil {
		return CloudSrvState{}, err
	}
	if payload.Nodes == nil {
		payload.Nodes = []CloudSrvNode{}
	}
	return payload, nil
}

// SetCloudSrvChoice pins the user's cloud tasks to one private node srv, or
// back to the public maclawsrv when nodeID is empty. The hub answers the
// same payload as GetCloudSrvChoice so no refetch is needed.
func (a *App) SetCloudSrvChoice(nodeID string) (CloudSrvState, error) {
	var payload CloudSrvState
	err := a.remoteControlJSON(http.MethodPut, "/api/v1/cloud-agents/srv", map[string]string{
		"node_id": strings.TrimSpace(nodeID),
	}, &payload)
	if err != nil {
		return CloudSrvState{}, err
	}
	if payload.Nodes == nil {
		payload.Nodes = []CloudSrvNode{}
	}
	return payload, nil
}
