package remote

// SessionStatus 表示会话的运行状态。
type SessionStatus string

const (
	SessionStarting     SessionStatus = "starting"
	SessionRunning      SessionStatus = "running"
	SessionBusy         SessionStatus = "busy"
	SessionWaitingInput SessionStatus = "waiting_input"
	SessionError        SessionStatus = "error"
	SessionExited       SessionStatus = "exited"
)

// ThinkingState 表示 Agent 的思考状态。
type ThinkingState int

const (
	ThinkingIdle   ThinkingState = iota // Agent is idle / waiting for input
	ThinkingActive                      // Agent is actively processing
)

// RemoteLaunchSource 标识会话的启动来源。
type RemoteLaunchSource string

const (
	RemoteLaunchSourceDesktop RemoteLaunchSource = "desktop"
	RemoteLaunchSourceMobile  RemoteLaunchSource = "mobile"
	RemoteLaunchSourceHandoff RemoteLaunchSource = "handoff"
	RemoteLaunchSourceAI      RemoteLaunchSource = "ai"
)

// NormalizeRemoteLaunchSource 规范化启动来源值。
func NormalizeRemoteLaunchSource(source RemoteLaunchSource) RemoteLaunchSource {
	switch source {
	case RemoteLaunchSourceMobile, RemoteLaunchSourceHandoff, RemoteLaunchSourceAI:
		return source
	default:
		return RemoteLaunchSourceDesktop
	}
}

// IsHeadlessLaunchSource 判断启动来源是否为无头模式（无本地桌面会话）。
func IsHeadlessLaunchSource(source RemoteLaunchSource) bool {
	return source == RemoteLaunchSourceMobile || source == RemoteLaunchSourceHandoff || source == RemoteLaunchSourceAI
}

// LaunchSpec 描述启动一个远程会话所需的参数。
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
	YoloMode     bool
	AdminMode    bool
	PythonEnv    string
	UseProxy     bool
	TeamMode     bool

	// ResumeSessionID, if non-empty, tells the provider adapter to resume
	// a previous tool session (e.g. Claude Code --resume <id>) instead of
	// starting a fresh conversation.
	ResumeSessionID string

	Env          map[string]string
}

// CommandSpec 描述要执行的命令。
type CommandSpec struct {
	Command string
	Args    []string
	Cwd     string
	Env     map[string]string
	Cols    int
	Rows    int
}

// SessionSummary 是会话的摘要信息。
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

// SessionPreview 是会话的预览输出。
type SessionPreview struct {
	SessionID    string   `json:"session_id"`
	OutputSeq    int64    `json:"output_seq"`
	PreviewLines []string `json:"preview_lines"`
	UpdatedAt    int64    `json:"updated_at"`
}

// SessionPreviewDelta 是会话预览的增量更新。
type SessionPreviewDelta struct {
	SessionID   string   `json:"session_id"`
	OutputSeq   int64    `json:"output_seq"`
	AppendLines []string `json:"append_lines"`
	UpdatedAt   int64    `json:"updated_at"`
}

// ImportantEvent 描述一个重要事件。
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

// PTYExit 描述 PTY 进程的退出信息。
type PTYExit struct {
	Code   *int
	Signal string
	Err    error
}

// PTYSession 定义 PTY 会话的接口。
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

// StallState 表示会话的停滞检测状态。
type StallState int

const (
	StallStateNormal    StallState = iota // 正常运行
	StallStateSuspected                   // 疑似停滞
	StallStateStuck                       // 已达最大 nudge 次数
)

// CompletionLevel 表示任务完成程度。
type CompletionLevel int

const (
	CompletionUncertain  CompletionLevel = iota // 无法确定
	CompletionCompleted                         // 任务完成
	CompletionIncomplete                        // 任务未完成
)

// OutputResult 是输出管道的处理结果。
type OutputResult struct {
	Summary      *SessionSummary
	PreviewDelta *SessionPreviewDelta
	Events       []ImportantEvent
}

// SessionOutputImage 是从 SDK 输出中提取的图片。
type SessionOutputImage struct {
	ImageID      string `json:"image_id"`
	MediaType    string `json:"media_type"`
	Data         string `json:"data"`
	AfterLineIdx int    `json:"after_line_idx"`
}

// ImageTransferMessage 描述通过 Hub 传输的图片消息。
type ImageTransferMessage struct {
	ImageID   string `json:"image_id"`
	SessionID string `json:"session_id"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

// ExecutionMode 描述提供商的启动模式。
type ExecutionMode string

const (
	ExecModePTY         ExecutionMode = "pty"
	ExecModeSDK         ExecutionMode = "sdk"
	ExecModeCodexSDK    ExecutionMode = "codex-sdk"
	ExecModeIFlowSDK    ExecutionMode = "iflow-sdk"
	ExecModeOpenCodeSDK ExecutionMode = "opencode-sdk"
	ExecModeKiloSDK     ExecutionMode = "kilo-sdk"
	ExecModeGeminiACP   ExecutionMode = "gemini-acp"
)

// ExecutionHandle 表示一个正在运行的远程执行实例。
type ExecutionHandle interface {
	PID() int
	Write(data []byte) error
	Interrupt() error
	Kill() error
	Output() <-chan []byte
	Exit() <-chan PTYExit
	Close() error
}

// ExecutionStrategy 描述远程命令的启动和托管方式。
type ExecutionStrategy interface {
	Start(cmd CommandSpec) (ExecutionHandle, error)
}

// ProviderAdapter 描述一个托管 CLI 提供商的启动和控制方式。
type ProviderAdapter interface {
	ProviderName() string
	BuildCommand(spec LaunchSpec) (CommandSpec, error)
	ExecutionMode() ExecutionMode
}

// WorkspaceMode 描述工作区模式。
type WorkspaceMode string

const (
	WorkspaceModeDirect      WorkspaceMode = "direct"
	WorkspaceModeGitWorktree WorkspaceMode = "git_worktree"
)

// PreparedWorkspace 描述准备好的工作区。
type PreparedWorkspace struct {
	ProjectPath string
	RootPath    string
	Mode        WorkspaceMode
	IsGitRepo   bool
	GitRoot     string
	Release     func()
}

// WorkspacePreparer 定义工作区准备接口。
type WorkspacePreparer interface {
	Prepare(sessionID string, spec LaunchSpec) (*PreparedWorkspace, error)
}
