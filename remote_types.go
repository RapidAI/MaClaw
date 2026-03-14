package main

import "time"

type SessionStatus string
type RemoteLaunchSource string

const (
	SessionStarting     SessionStatus = "starting"
	SessionRunning      SessionStatus = "running"
	SessionBusy         SessionStatus = "busy"
	SessionWaitingInput SessionStatus = "waiting_input"
	SessionError        SessionStatus = "error"
	SessionExited       SessionStatus = "exited"
)

const (
	RemoteLaunchSourceDesktop RemoteLaunchSource = "desktop"
	RemoteLaunchSourceMobile  RemoteLaunchSource = "mobile"
	RemoteLaunchSourceHandoff RemoteLaunchSource = "handoff"
)

func normalizeRemoteLaunchSource(source RemoteLaunchSource) RemoteLaunchSource {
	switch source {
	case RemoteLaunchSourceMobile, RemoteLaunchSourceHandoff:
		return source
	default:
		return RemoteLaunchSourceDesktop
	}
}

type LaunchSpec struct {
	SessionID    string
	Tool         string
	ProjectPath  string
	ModelName    string
	ModelID      string
	BinaryName   string
	Title        string
	LaunchSource RemoteLaunchSource

	YoloMode  bool
	AdminMode bool
	PythonEnv string
	UseProxy  bool
	TeamMode  bool

	Env map[string]string
}

type CommandSpec struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
	Cols    int
	Rows    int
}

type SessionSummary struct {
	SessionID       string   `json:"session_id"`
	MachineID       string   `json:"machine_id"`
	Tool            string   `json:"tool"`
	Title           string   `json:"title"`
	Source          string   `json:"source,omitempty"`
	Status          string   `json:"status"`
	Severity        string   `json:"severity"`
	WaitingForUser  bool     `json:"waiting_for_user"`
	CurrentTask     string   `json:"current_task"`
	ProgressSummary string   `json:"progress_summary"`
	LastResult      string   `json:"last_result"`
	SuggestedAction string   `json:"suggested_action"`
	ImportantFiles  []string `json:"important_files"`
	LastCommand     string   `json:"last_command"`
	UpdatedAt       int64    `json:"updated_at"`
}

type SessionPreview struct {
	SessionID    string   `json:"session_id"`
	OutputSeq    int64    `json:"output_seq"`
	PreviewLines []string `json:"preview_lines"`
	UpdatedAt    int64    `json:"updated_at"`
}

type SessionPreviewDelta struct {
	SessionID   string   `json:"session_id"`
	OutputSeq   int64    `json:"output_seq"`
	AppendLines []string `json:"append_lines"`
	UpdatedAt   int64    `json:"updated_at"`
}

type ImportantEvent struct {
	EventID     string `json:"event_id"`
	SessionID   string `json:"session_id"`
	MachineID   string `json:"machine_id"`
	Type        string `json:"type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Count       int    `json:"count,omitempty"`
	Grouped     bool   `json:"grouped,omitempty"`
	RelatedFile string `json:"related_file,omitempty"`
	Command     string `json:"command,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type PTYExit struct {
	Code   *int
	Signal string
	Err    error
}

type PTYSession interface {
	Start(cmd CommandSpec) (pid int, err error)
	Write(data []byte) error
	Interrupt() error
	Kill() error
	Resize(cols, rows int) error
	Close() error
	Output() <-chan []byte
	Exit() <-chan PTYExit
}

type RemoteSession struct {
	ID             string
	Tool           string
	Title          string
	LaunchSource   RemoteLaunchSource
	ProjectPath    string
	WorkspacePath  string
	WorkspaceRoot  string
	WorkspaceMode  WorkspaceMode
	WorkspaceIsGit bool
	ModelID        string

	Status    SessionStatus
	PID       int
	ExitCode  *int
	CreatedAt time.Time
	UpdatedAt time.Time

	Summary SessionSummary
	Preview SessionPreview
	Events  []ImportantEvent

	Exec     ExecutionHandle
	Provider ProviderAdapter

	workspaceRelease func()
}

type OutputResult struct {
	Summary      *SessionSummary
	PreviewDelta *SessionPreviewDelta
	Events       []ImportantEvent
}
