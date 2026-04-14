package remote

import (
	"errors"
	"time"
)

// ErrRemoteSessionsUnavailable is returned when remote sessions are not initialized.
var ErrRemoteSessionsUnavailable = errors.New("remote sessions are not initialized")

// RemoteConnectionStatus 远程连接状态。
type RemoteConnectionStatus struct {
	Enabled      bool   `json:"enabled"`
	HubURL       string `json:"hub_url"`
	MachineID    string `json:"machine_id"`
	Connected    bool   `json:"connected"`
	LastError    string `json:"last_error"`
	SessionCount int    `json:"session_count"`
}

// RemoteSmokeSnapshot 冒烟测试快照。
type RemoteSmokeSnapshot struct {
	Exists bool               `json:"exists"`
	Path   string             `json:"path"`
	Report *RemoteSmokeReport `json:"report,omitempty"`
}

// RemoteSmokeReport 冒烟测试报告。
type RemoteSmokeReport struct {
	Tool            string                       `json:"tool"`
	ProjectPath     string                       `json:"project_path"`
	UseProxy        bool                         `json:"use_proxy"`
	Phase           string                       `json:"phase"`
	Success         bool                         `json:"success"`
	LastUpdated     string                       `json:"last_updated"`
	RecommendedNext string                       `json:"recommended_next,omitempty"`
	Connection      RemoteConnectionStatus       `json:"connection"`
	Readiness       RemoteToolReadiness          `json:"readiness"`
	PTYProbe        *RemotePTYProbeResult        `json:"pty_probe,omitempty"`
	LaunchProbe     *RemoteToolLaunchProbeResult `json:"launch_probe,omitempty"`
	Activation      *RemoteActivationResult      `json:"activation,omitempty"`
	StartedSession  *RemoteSessionView           `json:"started_session,omitempty"`
	HubVisibility   *RemoteHubVisibilityResult   `json:"hub_visibility,omitempty"`
}

// RemoteHubVisibilityResult Hub 可见性检查结果。
type RemoteHubVisibilityResult struct {
	Attempted      bool   `json:"attempted"`
	Verified       bool   `json:"verified"`
	HubURL         string `json:"hub_url"`
	UserID         string `json:"user_id"`
	MachineID      string `json:"machine_id"`
	SessionID      string `json:"session_id"`
	MachineVisible bool   `json:"machine_visible"`
	SessionVisible bool   `json:"session_visible"`
	SessionStatus  string `json:"session_status,omitempty"`
	HostOnline     bool   `json:"host_online"`
	Message        string `json:"message,omitempty"`
}

// ProviderView 服务商视图。
type ProviderView struct {
	Name      string `json:"name"`
	ModelID   string `json:"model_id"`
	IsDefault bool   `json:"is_default"`
}

// RemoteSessionView 远程会话视图（用于前端展示）。
type RemoteSessionView struct {
	ID             string               `json:"id"`
	Tool           string               `json:"tool"`
	Title          string               `json:"title"`
	LaunchSource   string               `json:"launch_source,omitempty"`
	ProjectPath    string               `json:"project_path"`
	WorkspacePath  string               `json:"workspace_path"`
	WorkspaceRoot  string               `json:"workspace_root"`
	WorkspaceMode  WorkspaceMode        `json:"workspace_mode"`
	WorkspaceIsGit bool                 `json:"workspace_is_git"`
	ModelID        string               `json:"model_id"`
	Provider       string               `json:"provider,omitempty"`
	ExecutionMode  string               `json:"execution_mode"`
	Status         SessionStatus        `json:"status"`
	Thinking       bool                 `json:"thinking"`
	ThinkingSince  int64                `json:"thinking_since,omitempty"`
	PID            int                  `json:"pid"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	Summary        SessionSummary       `json:"summary"`
	Preview        SessionPreview       `json:"preview"`
	Events         []ImportantEvent     `json:"events"`
	RawOutputLines []string             `json:"raw_output_lines"`
	OutputImages   []SessionOutputImage `json:"output_images,omitempty"`
}

// RemoteToolMetadataView 远程工具元数据视图。
type RemoteToolMetadataView struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Supported   bool   `json:"supported"`
}
