package main

import (
	"crypto/rand"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Enum / constant types
// ---------------------------------------------------------------------------

// SwarmMode represents the operating mode of a swarm run.
type SwarmMode string

const (
	SwarmModeGreenfield  SwarmMode = "greenfield"
	SwarmModeMaintenance SwarmMode = "maintenance"
)

// SwarmStatus represents the lifecycle status of a swarm run.
type SwarmStatus string

const (
	SwarmStatusPending   SwarmStatus = "pending"
	SwarmStatusRunning   SwarmStatus = "running"
	SwarmStatusPaused    SwarmStatus = "paused"
	SwarmStatusCompleted SwarmStatus = "completed"
	SwarmStatusFailed    SwarmStatus = "failed"
	SwarmStatusCancelled SwarmStatus = "cancelled"
)

// SwarmPhase represents the current execution phase of a swarm run.
type SwarmPhase string

const (
	PhaseRequirements   SwarmPhase = "requirements"   // Spec Phase 1: 需求生成与确认
	PhaseDesign         SwarmPhase = "design"          // Spec Phase 2: 结构化设计
	PhaseTaskSplit      SwarmPhase = "task_split"      // Spec Phase 3: 任务分解（含验收标准）
	PhaseArchitecture   SwarmPhase = "architecture"
	PhaseConflictDetect SwarmPhase = "conflict_detect"
	PhaseDevelopment    SwarmPhase = "development"     // Spec Phase 4: 执行
	PhaseMerge          SwarmPhase = "merge"
	PhaseCompile        SwarmPhase = "compile"
	PhaseTest           SwarmPhase = "test"
	PhaseDocument       SwarmPhase = "document"
	PhaseReport         SwarmPhase = "report"
)

// AgentRole represents the role type of a swarm agent.
type AgentRole string

const (
	RoleArchitect  AgentRole = "architect"
	RoleDesigner   AgentRole = "designer"
	RoleDeveloper  AgentRole = "developer"
	RoleTestWriter AgentRole = "test_writer"
	RoleCompiler   AgentRole = "compiler"
	RoleTester     AgentRole = "tester"
	RoleDocumenter AgentRole = "documenter"
)

// FailureType classifies a test failure for the feedback loop.
type FailureType string

const (
	FailureTypeBug                  FailureType = "bug"
	FailureTypeFeatureGap           FailureType = "feature_gap"
	FailureTypeRequirementDeviation FailureType = "requirement_deviation"
)

// ---------------------------------------------------------------------------
// Core structs
// ---------------------------------------------------------------------------

// SwarmRun represents a single swarm execution instance with its full
// lifecycle from task decomposition through report generation.
type SwarmRun struct {
	ID          string      `json:"run_id"`
	Mode        SwarmMode   `json:"mode"`
	Status      SwarmStatus `json:"status"`
	Phase       SwarmPhase  `json:"phase"`
	ProjectPath string      `json:"project_path"`
	TechStack   string      `json:"tech_stack,omitempty"`
	Tool        string      `json:"tool"` // coding tool to use (e.g. "claude", "cursor")

	// Spec-driven artifacts (Kiro spec model)
	Requirements string `json:"requirements,omitempty"` // Phase 1 输出：结构化需求文档
	DesignDoc    string `json:"design_doc,omitempty"`   // Phase 2 输出：结构化设计文档

	// Tasks & Agents
	Tasks      []SubTask    `json:"tasks"`
	TaskGroups []TaskGroup  `json:"task_groups,omitempty"`
	Agents     []SwarmAgent `json:"agents"`

	// Feedback loop
	CurrentRound int          `json:"current_round"`
	MaxRounds    int          `json:"max_rounds"`
	RoundHistory []SwarmRound `json:"round_history"`

	// Worktree state
	ProjectState *ProjectState `json:"project_state,omitempty"`

	// Timeline
	Timeline    []TimelineEvent `json:"timeline"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`

	// User input channel (used for requirement deviation confirmation).
	userInputCh chan string `json:"-"`
}

// SwarmAgent represents a single agent instance in the swarm. Each agent is
// essentially a RemoteSession with a role-specific system prompt working in
// its own git worktree.
type SwarmAgent struct {
	ID           string    `json:"id"`
	Role         AgentRole `json:"role"`
	SessionID    string    `json:"session_id"`
	TaskIndex    int       `json:"task_index"`
	WorktreePath string    `json:"worktree_path"`
	BranchName   string    `json:"branch_name"`
	Status       string    `json:"status"` // "pending","running","completed","failed"
	RetryCount   int       `json:"retry_count"`
	Output       string    `json:"output,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// SubTask represents a decomposed unit of work that can be assigned to a
// developer agent.
type SubTask struct {
	Index              int      `json:"index"`
	Description        string   `json:"description"`
	ExpectedFiles      []string `json:"expected_files"`
	Dependencies       []int    `json:"dependencies"`
	GroupID            int      `json:"group_id"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"` // 验收标准
	TestFile           string   `json:"test_file,omitempty"`          // TDD: 测试文件路径
	TestCode           string   `json:"test_code,omitempty"`          // TDD: 测试代码内容（由 test_writer agent 生成）
}

// TaskGroup groups tasks that share file dependencies and must be executed
// serially within the group while different groups run in parallel.
type TaskGroup struct {
	ID            int      `json:"id"`
	TaskIndices   []int    `json:"task_indices"`
	ConflictFiles []string `json:"conflict_files"`
}

// WorktreeInfo holds metadata about a created git worktree.
type WorktreeInfo struct {
	Path       string `json:"path"`
	BranchName string `json:"branch_name"`
	RunID      string `json:"run_id"`
}

// ProjectState captures the original state of the project directory before
// worktree operations so it can be restored afterwards.
type ProjectState struct {
	HadGitRepo     bool   `json:"had_git_repo"`
	HadCommits     bool   `json:"had_commits"`
	StashCreated   bool   `json:"stash_created"`
	OriginalBranch string `json:"original_branch"`
}

// SwarmRound records a single feedback round (develop → compile → test cycle).
type SwarmRound struct {
	Number    int        `json:"number"`
	Reason    string     `json:"reason"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Result    string     `json:"result"` // "success","partial","failed"
}

// BranchInfo describes a worktree branch for merge ordering.
type BranchInfo struct {
	Name      string `json:"name"`
	AgentID   string `json:"agent_id"`
	TaskIndex int    `json:"task_index"`
	Order     int    `json:"order"`
}

// MergeResult captures the outcome of merging all worktree branches back to
// the main branch.
type MergeResult struct {
	Success        bool     `json:"success"`
	MergedBranches []string `json:"merged_branches"`
	FailedBranches []string `json:"failed_branches"`
	CompileErrors  []string `json:"compile_errors,omitempty"`
}

// ---------------------------------------------------------------------------
// Test failure types
// ---------------------------------------------------------------------------

// TestFailure describes a single failing test case.
type TestFailure struct {
	TestName    string `json:"test_name"`
	ErrorOutput string `json:"error_output"`
	FilePath    string `json:"file_path,omitempty"`
}

// ClassifiedFailure extends TestFailure with an LLM-assigned failure type and
// reasoning.
type ClassifiedFailure struct {
	TestFailure
	Type   FailureType `json:"type"`
	Reason string      `json:"reason"`
}

// ---------------------------------------------------------------------------
// Report types
// ---------------------------------------------------------------------------

// SwarmReport is the complete execution report generated at the end of a
// swarm run.
type SwarmReport struct {
	RunID       string      `json:"run_id"`
	Mode        SwarmMode   `json:"mode"`
	Status      SwarmStatus `json:"status"`
	ProjectPath string      `json:"project_path"`

	Statistics ReportStatistics `json:"statistics"`
	Rounds     []SwarmRound     `json:"rounds"`
	Agents     []AgentRecord    `json:"agents"`
	Timeline   []TimelineEvent  `json:"timeline"`
	OpenIssues []string         `json:"open_issues,omitempty"`

	CreatedAt time.Time `json:"created_at"`
}

// ReportStatistics holds aggregate metrics for the swarm run.
type ReportStatistics struct {
	TotalTasks     int `json:"total_tasks"`
	CompletedTasks int `json:"completed_tasks"`
	FailedTasks    int `json:"failed_tasks"`
	TotalRounds    int `json:"total_rounds"`
	LinesAdded     int `json:"lines_added"`
	LinesModified  int `json:"lines_modified"`
	LinesDeleted   int `json:"lines_deleted"`
}

// AgentRecord captures the execution record of a single agent for reporting.
type AgentRecord struct {
	AgentID     string    `json:"agent_id"`
	Role        AgentRole `json:"role"`
	TaskIndex   int       `json:"task_index"`
	Status      string    `json:"status"`
	Duration    float64   `json:"duration_seconds"`
	DiffSummary string    `json:"diff_summary,omitempty"`
}

// TimelineEvent records a single event in the swarm execution timeline.
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	AgentID   string    `json:"agent_id,omitempty"`
	Phase     string    `json:"phase,omitempty"`
}

// ---------------------------------------------------------------------------
// Request / summary types
// ---------------------------------------------------------------------------

// SwarmRunRequest is the input payload for starting a new swarm run.
type SwarmRunRequest struct {
	Mode         SwarmMode      `json:"mode"`
	ProjectPath  string         `json:"project_path"`
	Requirements string         `json:"requirements,omitempty"`
	TechStack    string         `json:"tech_stack,omitempty"`
	TaskInput    *TaskListInput `json:"task_input,omitempty"`
	MaxAgents    int            `json:"max_agents,omitempty"`
	MaxRounds    int            `json:"max_rounds,omitempty"`
	Tool         string         `json:"tool"`
}

// TaskListInput describes the source and content of a task list for
// maintenance mode.
type TaskListInput struct {
	Source string `json:"source"` // "manual", "github", "feishu", "jira"
	Text   string `json:"text,omitempty"`
	URL    string `json:"url,omitempty"`
}

// SwarmRunSummary is a lightweight view of a SwarmRun used in list responses.
type SwarmRunSummary struct {
	ID        string      `json:"run_id"`
	Mode      SwarmMode   `json:"mode"`
	Status    SwarmStatus `json:"status"`
	Phase     SwarmPhase  `json:"phase"`
	TaskCount int         `json:"task_count"`
	Round     int         `json:"current_round"`
	CreatedAt time.Time   `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Prompt template types
// ---------------------------------------------------------------------------

// PromptTemplate pairs an agent role with its Go text/template string.
type PromptTemplate struct {
	Role     AgentRole
	Template string // Go text/template format
}

// PromptContext provides the variables available for prompt template rendering.
type PromptContext struct {
	ProjectName        string
	TechStack          string
	TaskDesc           string
	ArchDesign         string
	InterfaceDefs      string
	CompileErrors      string
	TestCommand        string
	Requirements       string
	FeatureList        string
	ProjectStruct      string
	APIList            string
	ChangeLog          string
	AcceptanceCriteria string // 验收标准（换行分隔）
	TestFile           string // TDD: 测试文件路径
	TestCode           string // TDD: 已生成的测试代码
}

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

// NewSwarmRunID generates a unique run ID using a timestamp and random suffix.
// Format: swarm_{unix_timestamp}_{random_hex}
func NewSwarmRunID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("swarm_%d_%08x", time.Now().UnixNano(),
		uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
}
