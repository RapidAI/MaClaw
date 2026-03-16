package main

import (
	"sync"
	"time"
)

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
	// mu protects mutable fields that are written by output/exit loop
	// goroutines and read by the UI thread (via toRemoteSessionView).
	// Immutable fields (ID, Tool, Title, etc.) do not need locking.
	mu sync.RWMutex

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

	// RawOutputLines stores the most recent PTY output lines with only
	// ANSI stripping applied (no noise filtering, no event extraction).
	// Used by the desktop console for a terminal-like raw view.
	RawOutputLines []string

	// OutputImages stores images extracted from SDK output (assistant
	// responses, tool results) so the desktop console can render them
	// inline.  Each entry records the raw-output-line index at which
	// the image was produced, allowing the frontend to interleave
	// images with text.
	OutputImages []SessionOutputImage

	Exec     ExecutionHandle
	Provider ProviderAdapter

	workspaceRelease func()
	configCleanup    func() // restores tool config files modified by onboarding
}

type OutputResult struct {
	Summary      *SessionSummary
	PreviewDelta *SessionPreviewDelta
	Events       []ImportantEvent
}

// SessionOutputImage is an image extracted from SDK output, tagged with
// the raw-output-line index so the desktop console can render it inline.
type SessionOutputImage struct {
	ImageID      string `json:"image_id"`
	MediaType    string `json:"media_type"`
	Data         string `json:"data"`          // base64-encoded
	AfterLineIdx int    `json:"after_line_idx"` // insert after this raw-output-line index
}

// ImageTransferMessage represents an image being transferred between desktop and mobile via Hub.
type ImageTransferMessage struct {
	ImageID   string `json:"image_id"`
	SessionID string `json:"session_id"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`      // base64-encoded image data
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}
