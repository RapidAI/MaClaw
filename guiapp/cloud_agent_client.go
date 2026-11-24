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

// DeleteCloudAgent removes an instance that never received a task.
// A failed create uses it so a retry does not fill the instance quota.
func (a *App) DeleteCloudAgent(instanceID string) error {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return errors.New("缺少云端实例")
	}
	return a.remoteControlJSON(http.MethodDelete, "/api/v1/cloud-agents/"+url.PathEscape(instanceID), nil, nil)
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
