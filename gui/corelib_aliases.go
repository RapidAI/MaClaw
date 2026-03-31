package main

// corelib_aliases.go — type aliases that map corelib types into gui's package main.
// This allows gui/ code to continue using bare type names (e.g. AppConfig)
// while the canonical definitions live in corelib/.
// These aliases can be removed incrementally as gui/ code is refactored to
// use qualified corelib.XXX references.

import (
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ── types.go aliases ────────────────────────────────────────────────────────

type ModelConfig = corelib.ModelConfig
type ProjectConfig = corelib.ProjectConfig
type PythonEnvironment = corelib.PythonEnvironment
type ToolConfig = corelib.ToolConfig
type CodeBuddyModel = corelib.CodeBuddyModel
type CodeBuddyFileConfig = corelib.CodeBuddyFileConfig
type MCPServerSource = corelib.MCPServerSource
type MCPServerEntry = corelib.MCPServerEntry
type LocalMCPServerEntry = corelib.LocalMCPServerEntry
type NLSkillStep = corelib.NLSkillStep
type NLSkillEntry = corelib.NLSkillEntry
type MaclawLLMProvider = corelib.MaclawLLMProvider
type MaclawLLMConfig = corelib.MaclawLLMConfig
type SkillHubEntry = corelib.SkillHubEntry
type Skill = corelib.Skill
type TokenUsageStat = corelib.TokenUsageStat

// ── app_config.go alias ─────────────────────────────────────────────────────

type AppConfig = corelib.AppConfig

// ── corelib/config aliases ──────────────────────────────────────────────────

type ConfigSection = config.ConfigSection
type ConfigKeySchema = config.ConfigKeySchema
type ConfigChange = config.ConfigChange
type ImportReport = config.ImportReport

// ── corelib/memory aliases ──────────────────────────────────────────────────
// NOTE: gui uses MemoryEntry/MemoryCategory (different names from corelib's
// memory.Entry/memory.Category). Only CompressResult shares the same name.

type CompressResult = memory.CompressResult

// ── corelib/tool aliases ────────────────────────────────────────────────────
// NOTE: gui uses different names for most tool types (ToolCategory vs tool.Category,
// ToolRegistry vs tool.Registry, etc.). Only ProgressCallback shares the same name.

type ProgressCallback = tool.ProgressCallback

// ── corelib/security aliases ────────────────────────────────────────────────
// NOTE: gui uses different struct names for implementations (SecurityFirewall
// vs security.Firewall, etc.) so only pure data types are aliased.

type RiskLevel = security.RiskLevel
type RiskAssessment = security.RiskAssessment
type PolicyAction = security.PolicyAction
type PolicyRule = security.PolicyRule
type AuditEntry = security.AuditEntry
type RiskPattern = security.RiskPattern
type AuditAction = security.AuditAction
type AuditFilter = security.AuditFilter

// ── constants ───────────────────────────────────────────────────────────────

const (
	MCPSourceManual  = corelib.MCPSourceManual
	MCPSourceMDNS    = corelib.MCPSourceMDNS
	MCPSourceProject = corelib.MCPSourceProject
)

// Security constants
const (
	RiskLow      = security.RiskLow
	RiskMedium   = security.RiskMedium
	RiskHigh     = security.RiskHigh
	RiskCritical = security.RiskCritical

	PolicyAllow = security.PolicyAllow
	PolicyDeny  = security.PolicyDeny
	PolicyAsk   = security.PolicyAsk
	PolicyAudit = security.PolicyAudit

	AuditActionHubSkillInstall = security.AuditActionHubSkillInstall
	AuditActionHubSkillUpdate  = security.AuditActionHubSkillUpdate
	AuditActionHubSkillReject  = security.AuditActionHubSkillReject
)

// riskLevelOrder re-exports the corelib security level ordering.
var riskLevelOrder = security.RiskLevelOrder

// RequiredNodeVersion re-exports the corelib constant.
const RequiredNodeVersion = corelib.RequiredNodeVersion

// maxAgentIterationsCap re-exports the corelib constant (unexported for gui compatibility).
const maxAgentIterationsCap = config.MaxAgentIterationsCap

// minAgentIterations re-exports the corelib constant (unexported for gui compatibility).
const minAgentIterations = config.MinAgentIterations

// ── corelib/swarm type aliases ──────────────────────────────────────────────

type SwarmMode = swarm.SwarmMode
type SwarmStatus = swarm.SwarmStatus
type SwarmPhase = swarm.SwarmPhase
type AgentRole = swarm.AgentRole
type FailureType = swarm.FailureType

type SwarmRun = swarm.SwarmRun
type SwarmAgent = swarm.SwarmAgent
type SubTask = swarm.SubTask
type TaskGroup = swarm.TaskGroup
type WorktreeInfo = swarm.WorktreeInfo
type ProjectState = swarm.ProjectState
type SwarmRound = swarm.SwarmRound
type BranchInfo = swarm.BranchInfo
type MergeResult = swarm.MergeResult
type TestFailure = swarm.TestFailure
type ClassifiedFailure = swarm.ClassifiedFailure
type SwarmReport = swarm.SwarmReport
type ReportStatistics = swarm.ReportStatistics
type AgentRecord = swarm.AgentRecord
type TimelineEvent = swarm.TimelineEvent
type SwarmRunRequest = swarm.SwarmRunRequest
type TaskListInput = swarm.TaskListInput
type SwarmRunSummary = swarm.SwarmRunSummary
type PromptTemplate = swarm.PromptTemplate
type PromptContext = swarm.PromptContext

// ── corelib/swarm constants ─────────────────────────────────────────────────

// SwarmMode constants
const (
	SwarmModeGreenfield  = swarm.SwarmModeGreenfield
	SwarmModeMaintenance = swarm.SwarmModeMaintenance
)

// SwarmStatus constants
const (
	SwarmStatusPending   = swarm.SwarmStatusPending
	SwarmStatusRunning   = swarm.SwarmStatusRunning
	SwarmStatusPaused    = swarm.SwarmStatusPaused
	SwarmStatusCompleted = swarm.SwarmStatusCompleted
	SwarmStatusFailed    = swarm.SwarmStatusFailed
	SwarmStatusCancelled = swarm.SwarmStatusCancelled
)

// SwarmPhase constants
const (
	PhaseRequirements   = swarm.PhaseRequirements
	PhaseDesign         = swarm.PhaseDesign
	PhaseTaskSplit      = swarm.PhaseTaskSplit
	PhaseArchitecture   = swarm.PhaseArchitecture
	PhaseConflictDetect = swarm.PhaseConflictDetect
	PhaseDevelopment    = swarm.PhaseDevelopment
	PhaseMerge          = swarm.PhaseMerge
	PhaseCompile        = swarm.PhaseCompile
	PhaseTest           = swarm.PhaseTest
	PhaseDocument       = swarm.PhaseDocument
	PhaseReport         = swarm.PhaseReport
)

// AgentRole constants
const (
	RoleArchitect  = swarm.RoleArchitect
	RoleDesigner   = swarm.RoleDesigner
	RoleDeveloper  = swarm.RoleDeveloper
	RoleTestWriter = swarm.RoleTestWriter
	RoleCompiler   = swarm.RoleCompiler
	RoleTester     = swarm.RoleTester
	RoleDocumenter = swarm.RoleDocumenter
)

// FailureType constants
const (
	FailureTypeBug                  = swarm.FailureTypeBug
	FailureTypeFeatureGap           = swarm.FailureTypeFeatureGap
	FailureTypeRequirementDeviation = swarm.FailureTypeRequirementDeviation
)

// NewSwarmRunID re-exports the corelib/swarm function.
var NewSwarmRunID = swarm.NewSwarmRunID

// ── corelib/swarm DocType aliases ────────────────────────────────────────────

type DocType = swarm.DocType

const (
	DocTypeRequirements = swarm.DocTypeRequirements
	DocTypeDesign       = swarm.DocTypeDesign
	DocTypeTaskPlan     = swarm.DocTypeTaskPlan
)

// SwarmDocGenerator re-exports the corelib/swarm type.
type SwarmDocGenerator = swarm.SwarmDocGenerator

// NewSwarmDocGenerator re-exports the corelib/swarm constructor.
var NewSwarmDocGenerator = swarm.NewSwarmDocGenerator

// ── corelib/swarm component aliases ─────────────────────────────────────────

type TaskSplitter = swarm.TaskSplitter
type FeedbackLoop = swarm.FeedbackLoop
type TaskVerifier = swarm.TaskVerifier
type TaskVerdict = swarm.TaskVerdict

// NewTaskSplitter re-exports the corelib/swarm constructor.
var NewTaskSplitter = swarm.NewTaskSplitter

// NewFeedbackLoop re-exports the corelib/swarm constructor.
var NewFeedbackLoop = swarm.NewFeedbackLoop

// NewTaskVerifier re-exports the corelib/swarm constructor.
var NewTaskVerifier = swarm.NewTaskVerifier

// SwarmReporter re-exports the corelib/swarm type.
type SwarmReporter = swarm.SwarmReporter

// NewSwarmReporter re-exports the corelib/swarm constructor.
var NewSwarmReporter = swarm.NewSwarmReporter

// MarshalReport re-exports the corelib/swarm function.
var MarshalReport = swarm.MarshalReport

// UnmarshalReport re-exports the corelib/swarm function.
var UnmarshalReport = swarm.UnmarshalReport

// RenderPrompt re-exports the corelib/swarm function.
var RenderPrompt = swarm.RenderPrompt

// RenderSpecPrompt re-exports the corelib/swarm function.
var RenderSpecPrompt = swarm.RenderSpecPrompt

// DetermineStrategy re-exports the corelib/swarm function.
var DetermineStrategy = swarm.DetermineStrategy

// TopologicalSort re-exports the corelib/swarm function.
var TopologicalSort = swarm.TopologicalSort

// Agent scheduler constants re-exported from corelib/swarm.
const (
	DefaultMaxDeveloperAgents = swarm.DefaultMaxDeveloperAgents
	MinMaxDeveloperAgents     = swarm.MinMaxDeveloperAgents
	MaxMaxDeveloperAgents     = swarm.MaxMaxDeveloperAgents
	DefaultAgentTimeout       = swarm.DefaultAgentTimeout
	MaxAgentRetries           = swarm.MaxAgentRetries
)

// ValidateMaxAgents re-exports the corelib/swarm function.
var ValidateMaxAgents = swarm.ValidateMaxAgents

// InferTestCommand re-exports the corelib/swarm function.
var InferTestCommand = swarm.InferTestCommand

// MergeController re-exports the corelib/swarm type.
type MergeController = swarm.MergeController

// NewMergeController re-exports the corelib/swarm constructor.
var NewMergeController = swarm.NewMergeController

// ── corelib/swarm orchestrator option aliases ───────────────────────────────

// OrchestratorOption re-exports the corelib/swarm option type.
type OrchestratorOption = swarm.OrchestratorOption

// WithAppContext re-exports the corelib/swarm option function.
var WithAppContext = swarm.WithAppContext

// WithLLMCaller re-exports the corelib/swarm option function.
var WithLLMCaller = swarm.WithLLMCaller

// WithMaxRounds re-exports the corelib/swarm option function.
var WithMaxRounds = swarm.WithMaxRounds

// WithMaxAgents re-exports the corelib/swarm option function.
var WithMaxAgents = swarm.WithMaxAgents

// extractJSON delegates to the corelib/swarm exported helper.
// gui/swarm_pipeline.go and gui/swarm_feedback.go still call this until they
// are migrated in later tasks.
func extractJSON(data []byte) []byte {
	return swarm.ExtractJSON(data)
}

// extractJSONObject delegates to the corelib/swarm exported helper.
func extractJSONObject(data []byte) []byte {
	return swarm.ExtractJSONObject(data)
}

// truncateForPrompt delegates to the corelib/swarm exported helper.
func truncateForPrompt(text string, maxChars int) string {
	return swarm.TruncateForPrompt(text, maxChars)
}

// runTestShellCommand delegates to the corelib/swarm exported helper.
func runTestShellCommand(workDir, cmd string, timeout time.Duration) (string, int) {
	return swarm.RunTestShellCommand(workDir, cmd, timeout)
}

// buildTargetedTestCmd delegates to the corelib/swarm exported helper.
func buildTargetedTestCmd(baseCmd, testFile string) string {
	return swarm.BuildTargetedTestCmd(baseCmd, testFile)
}

// countTestFailures delegates to the corelib/swarm exported helper.
func countTestFailures(output string) int {
	return swarm.CountTestFailures(output)
}

// countTestTotal delegates to the corelib/swarm exported helper.
func countTestTotal(output string) int {
	return swarm.CountTestTotal(output)
}

// extractFailingSummary delegates to the corelib/swarm exported helper.
func extractFailingSummary(output string) string {
	return swarm.ExtractFailingSummary(output)
}



// ── remote function aliases ─────────────────────────────────────────────────

// downsizeScreenshotBase64 wraps remote.DownsizeScreenshotBase64 for use
// within gui/ without a package qualifier.
var downsizeScreenshotBase64 = remote.DownsizeScreenshotBase64

// ── corelib/scheduler aliases ───────────────────────────────────────────────
// These replace the former gui/scheduled_task.go and gui/scheduled_task_calendar.go
// duplicates. The canonical implementation now lives solely in corelib/scheduler.

type ScheduledTask = scheduler.ScheduledTask
type ScheduledTaskManager = scheduler.Manager
type TaskExecutor = scheduler.TaskExecutor

// NewScheduledTaskManager wraps scheduler.NewManager for gui compatibility.
var NewScheduledTaskManager = scheduler.NewManager

// isRecurringTask wraps scheduler.IsRecurringTask (lowercase for gui-internal use).
var isRecurringTask = scheduler.IsRecurringTask

// SyncTaskToSystemCalendar wraps scheduler.SyncTaskToSystemCalendar.
var SyncTaskToSystemCalendar = scheduler.SyncTaskToSystemCalendar

// FormatInterval wraps scheduler.FormatInterval.
var FormatInterval = scheduler.FormatInterval

// truncateStr wraps scheduler.TruncateStr (lowercase for gui-internal use).
func truncateStr(s string, maxLen int) string {
	return scheduler.TruncateStr(s, maxLen)
}
