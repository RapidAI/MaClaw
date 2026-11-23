package guiapp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudAgentRow is one cloud instance the task list can open.
// It is not a desktop machine and not a bot.
type CloudAgentRow struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	RuntimeStatus string    `json:"runtime_status"`
	LLMMode       string    `json:"llm_mode"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// ListCloudAgents reads the signed-in user's cloud instances.
func (a *App) ListCloudAgents() ([]CloudAgentRow, error) {
	var payload struct {
		Instances []CloudAgentRow `json:"instances"`
	}
	if err := a.remoteControlJSON(http.MethodGet, "/api/v1/cloud-agents", nil, &payload); err != nil {
		return nil, err
	}
	if payload.Instances == nil {
		return []CloudAgentRow{}, nil
	}
	return payload.Instances, nil
}

// CreateCloudAgent creates one session or coding instance.
// Kind is fixed by the Hub after this call.
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
	return row, err
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
}

// ConnectCloudAgentGit posts the coding credential to the user's cloud instance.
func (a *App) ConnectCloudAgentGit(instanceID string, body CloudGitConnect) (CloudGitPublic, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return CloudGitPublic{}, errors.New("缺少云端实例")
	}
	var view CloudGitPublic
	err := a.remoteControlJSON(http.MethodPost, "/api/v1/cloud-agents/"+url.PathEscape(instanceID)+"/git", body, &view)
	return view, err
}
