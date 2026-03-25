package main

import (
	"sync"
	"time"
)

type SessionStatus string
type ThinkingState int
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
	RemoteLaunchSourceAI      RemoteLaunchSource = "ai"
)

const (
	ThinkingIdle     ThinkingState = iota // Agent is idle / waiting for input
	ThinkingActive                        // Agent is actively processing (LLM call in flight)
)

func normalizeRemoteLaunchSource(source RemoteLaunchSource) RemoteLaunchSource {
	switch source {
	case RemoteLaunchSourceMobile, RemoteLaunchSourceHandoff, RemoteLaunchSourceAI:
		return source
	default:
		return RemoteLaunchSourceDesktop
	}
}

// isHeadlessLaunchSource returns true for launch sources that have no
// local desktop session (mobile PWA, handoff from Hub). These sources
// cannot display OS-level dialogs such as UAC prompts or permission
// confirmation windows.
func isHeadlessLaunchSource(source RemoteLaunchSource) bool {
	return source == RemoteLaunchSourceMobile || source == RemoteLaunchSourceHandoff || source == RemoteLaunchSourceAI
}

type LaunchSpec struct {
	SessionID    string
	Tool         string
	ProjectPath  string
	ModelName    string
	ModelID      string
	IsBuiltin    bool
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
	Thinking        bool     `json:"thinking"`
	ThinkingSince   int64    `json:"thinking_since,omitempty"`
	CurrentTask     string   `json:"current_task"`
	ProgressSummary string   `json:"progress_summary"`
	StepProgress    string   `json:"step_progress,omitempty"`
	StepCount       int      `json:"step_count,omitempty"`
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

// StallState represents the stall detection state of a session.
type StallState int

const (
	StallStateNormal    StallState = iota // 正常运行
	StallStateSuspected                   // 疑似停滞，正在 nudge
	StallStateStuck                       // 已达最大 nudge 次数，需要 Agent 介入
)

// CompletionLevel represents the semantic task completion level.
type CompletionLevel int

const (
	CompletionUncertain  CompletionLevel = iota // 无法确定
	CompletionCompleted                         // 任务完成
	CompletionIncomplete                        // 任务未完成
)

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

	// Stall detection and completion analysis fields (protected by mu).
	StallState      StallState      // current stall state, updated by StallDetector
	CompletionLevel CompletionLevel // latest completion analysis result
	LastNudgeCount  int             // nudge count from the most recent stall episode
	ThinkingState   ThinkingState   // current thinking state (idle/active)
	ThinkingSince   time.Time       // when the current thinking state started

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

	// Permissions manages tool-use permission requests for this session.
	// Initialized based on the session's YoloMode setting.
	Permissions *PermissionHandler

	// LaunchFingerprint is a hash of the LaunchSpec fields that affect
	// session behavior. Used to detect parameter changes across launches.
	LaunchFP string

	// ResumeContext stores context from a previous session that exited
	// mid-task, enabling the Agent to create a new session and continue
	// where the previous one left off.
	ResumeContext *SessionResumeContext

	// PendingUserQuestion tracks a pending AskUserQuestion tool_use block.
	// When Claude Code uses AskUserQuestion, it waits for a tool_result
	// with the user's answer. The next WriteInput call will be wrapped
	// as a tool_result instead of a new user message.
	PendingUserQuestion *PendingToolUse

	workspaceRelease func()
	configCleanup    func() // restores tool config files modified by onboarding
}

// PendingToolUse tracks a tool_use block that requires user interaction.
type PendingToolUse struct {
	ToolUseID string
	ToolName  string
}

// SessionResumeContext captures the state of a session that exited mid-task,
// so the Agent can create a new session and continue the work.
type SessionResumeContext struct {
	OriginalTask    string   `json:"original_task"`     // the user's original request
	CompletedFiles  []string `json:"completed_files"`   // files that were created/modified
	LastProgress    string   `json:"last_progress"`     // last progress summary
	LastOutput      string   `json:"last_output"`       // tail of output before exit
	ResumeCount     int      `json:"resume_count"`      // how many times we've resumed
	ProjectPath     string   `json:"project_path"`      // project path for new session
	Tool            string   `json:"tool"`              // tool name (claude, gemini, etc.)
	ExitReason      string   `json:"exit_reason"`       // "token_limit", "api_error", "unknown"
}

// isStructuredSession returns true for sessions that use a structured
// protocol (SDK JSON, ACP JSON-RPC, etc.) rather than a raw PTY.
// These tools (Claude Code, Gemini CLI, Codex, iFlow) are known to exit
// with code 1 as their normal termination — this should NOT be treated
// as a failure.
func (s *RemoteSession) isStructuredSession() bool {
	if s.Exec == nil {
		return false
	}
	switch s.Exec.(type) {
	case *SDKExecutionHandle, *GeminiACPExecutionHandle, *CodexSDKExecutionHandle, *IFlowSDKExecutionHandle:
		return true
	}
	return false
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
