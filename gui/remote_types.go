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

func normalizeSessionStatus(status string) SessionStatus {
	switch SessionStatus(status) {
	case SessionStarting:
		return SessionStarting
	case SessionRunning:
		return SessionRunning
	case SessionBusy:
		return SessionBusy
	case SessionWaitingInput:
		return SessionWaitingInput
	case SessionError:
		return SessionError
	case SessionExited:
		return SessionExited
	default:
		return ""
	}
}

func (status SessionStatus) String() string {
	return string(status)
}

func (status SessionStatus) IsStarting() bool {
	return status == SessionStarting
}

func (status SessionStatus) IsRunning() bool {
	return status == SessionRunning
}

func (status SessionStatus) IsBusy() bool {
	return status == SessionBusy
}

func (status SessionStatus) IsWaitingInput() bool {
	return status == SessionWaitingInput
}

func (status SessionStatus) IsTerminal() bool {
	return status == SessionError || status == SessionExited
}

func (status SessionStatus) IsWaitingOrTerminal() bool {
	return status.IsWaitingInput() || status.IsTerminal()
}

const (
	RemoteLaunchSourceDesktop RemoteLaunchSource = "desktop"
	RemoteLaunchSourceMobile  RemoteLaunchSource = "mobile"
	RemoteLaunchSourceHandoff RemoteLaunchSource = "handoff"
	RemoteLaunchSourceAI      RemoteLaunchSource = "ai"
)

const (
	ThinkingIdle   ThinkingState = iota // Agent is idle / waiting for input
	ThinkingActive                      // Agent is actively processing (LLM call in flight)
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

	YoloMode           bool
	AdminMode          bool
	PythonEnv          string
	UseProxy           bool
	TeamMode           bool
	InjectResumePrompt bool

	// ResumeSessionID, if non-empty, tells the provider adapter to resume
	// a previous Claude Code session (via --resume <id>) instead of starting
	// a fresh conversation. This preserves conversation history and produces
	// much higher quality continuations than starting from scratch.
	ResumeSessionID string

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
	SessionID       string                   `json:"session_id"`
	MachineID       string                   `json:"machine_id"`
	Tool            string                   `json:"tool"`
	Title           string                   `json:"title"`
	Source          string                   `json:"source,omitempty"`
	Status          string                   `json:"status"`
	Severity        string                   `json:"severity"`
	WaitingForUser  bool                     `json:"waiting_for_user"`
	Thinking        bool                     `json:"thinking"`
	ThinkingSince   int64                    `json:"thinking_since,omitempty"`
	CurrentTask     string                   `json:"current_task"`
	ProgressSummary string                   `json:"progress_summary"`
	StepProgress    string                   `json:"step_progress,omitempty"`
	StepCount       int                      `json:"step_count,omitempty"`
	LastResult      string                   `json:"last_result"`
	SuggestedAction string                   `json:"suggested_action"`
	ImportantFiles  []string                 `json:"important_files"`
	LastCommand     string                   `json:"last_command"`
	PendingQuestion *PendingQuestionView     `json:"pending_question,omitempty"`
	TokenUsage      *RemoteSessionTokenUsage `json:"token_usage,omitempty"`
	UpdatedAt       int64                    `json:"updated_at"`
}

// PendingQuestionView contains sanitized AskUserQuestion data for the UI.
type PendingQuestionView struct {
	ToolUseID string                  `json:"tool_use_id,omitempty"`
	ToolName  string                  `json:"tool_name,omitempty"`
	Header    string                  `json:"header,omitempty"`
	Question  string                  `json:"question,omitempty"`
	Hint      string                  `json:"hint,omitempty"`
	Multi     bool                    `json:"multi,omitempty"`
	Options   []PendingQuestionOption `json:"options,omitempty"`
}

type PendingQuestionOption struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
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
	ModelName      string
	JobID          string
	RunID          string

	Status    SessionStatus
	PID       int
	ExitCode  *int
	CreatedAt time.Time
	UpdatedAt time.Time

	// Stall detection and completion analysis fields (protected by mu).
	StallState        StallState      // current stall state, updated by StallDetector
	CompletionLevel   CompletionLevel // latest completion analysis result
	LastNudgeCount    int             // nudge count from the most recent stall episode
	AutoContinueCount int             // times Agent auto-continued within a live session (e.g. gemini-acp cancelled)
	ThinkingState     ThinkingState   // current thinking state (idle/active)
	ThinkingSince     time.Time       // when the current thinking state started

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

	// TokenUsage stores provider-reported remote-tool diagnostics only. It is
	// intentionally separate from Maclaw LLM provider usage and billing.
	TokenUsage RemoteSessionTokenUsage

	Exec     ExecutionHandle
	Provider ProviderAdapter

	// AgentLoop tracks a background AI-assistant task that is surfaced via the
	// remote session list. These sessions do not have an ExecutionHandle, so
	// interruption/cancellation is routed through the loop context instead.
	AgentLoop *LoopContext

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

	// InjectResumePrompt marks sessions that were explicitly launched to
	// continue an unfinished-slot task, allowing startup feedback to inject
	// the checkpoint prompt only for those sessions.
	InjectResumePrompt bool

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
	Question  *PendingQuestionView
	RawInput  map[string]interface{}
}

// SessionResumeContext captures the state of a session that exited mid-task,
// so the Agent can create a new session and continue the work.
type SessionResumeContext struct {
	OriginalTask   string   `json:"original_task"`   // the user's original request
	CompletedFiles []string `json:"completed_files"` // files that were created/modified
	LastProgress   string   `json:"last_progress"`   // last progress summary
	LastOutput     string   `json:"last_output"`     // tail of output before exit
	ResumeCount    int      `json:"resume_count"`    // how many times we've resumed
	ProjectPath    string   `json:"project_path"`    // project path for new session
	Tool           string   `json:"tool"`            // tool name (claude, gemini, etc.)
	ExitReason     string   `json:"exit_reason"`     // "token_limit", "api_error", "unknown"

	// ResumeSessionID is the provider-native session/thread id that can be
	// passed back via create_session(..., resume_session_id=...) to continue
	// the exact structured conversation history when the provider supports it.
	ResumeSessionID string `json:"resume_session_id,omitempty"`

	// ClaudeSessionID is kept for backward compatibility with older saved
	// resume contexts and tests. New code should prefer ResumeSessionID.
	ClaudeSessionID string `json:"claude_session_id,omitempty"`
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

type RemoteSessionTokenUsage struct {
	InputTokens       int `json:"input_tokens,omitempty"`
	OutputTokens      int `json:"output_tokens,omitempty"`
	CachedInputTokens int `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens  int `json:"cache_write_tokens,omitempty"`
}

func (u RemoteSessionTokenUsage) IsZero() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedInputTokens == 0 && u.CacheWriteTokens == 0
}

func (r OutputResult) SummaryText() string {
	if r.Summary != nil {
		if r.Summary.LastResult != "" {
			return r.Summary.LastResult
		}
		if r.Summary.ProgressSummary != "" {
			return r.Summary.ProgressSummary
		}
		if r.Summary.CurrentTask != "" {
			return r.Summary.CurrentTask
		}
	}
	if len(r.Events) > 0 {
		last := r.Events[len(r.Events)-1]
		if last.Summary != "" {
			return last.Summary
		}
		if last.Title != "" {
			return last.Title
		}
	}
	return ""
}

// SessionOutputImage is an image extracted from SDK output, tagged with
// the raw-output-line index so the desktop console can render it inline.
type SessionOutputImage struct {
	ImageID      string `json:"image_id"`
	MediaType    string `json:"media_type"`
	Data         string `json:"data"`           // base64-encoded
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
