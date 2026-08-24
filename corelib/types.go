package corelib

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

// RequiredNodeVersion 是项目要求的最低 Node.js 版本。
const RequiredNodeVersion = "24.13.0"

const (
	CodeGenClientNameHeader = "X-Codegen-Client-Name"
	CodeGenClientName       = "tigerclaw"
	CodeGenDefaultModelID   = "qax-codegen/Auto"
	CodeGenAutoModelAlias   = "auto"
)

func SetCodeGenClientNameHeaderIfNeeded(req *http.Request) {
	SetCodeGenClientNameHeaderIfNeededWithName(req, CodeGenClientName)
}

func SetCodeGenClientNameHeaderIfNeededWithName(req *http.Request, clientName string) {
	if req != nil && req.URL != nil && IsCodeGenHostname(req.URL.Hostname()) {
		req.Header.Set(CodeGenClientNameHeader, NormalizeCodeGenClientName(clientName))
	}
}

func NormalizeCodeGenClientName(clientName string) string {
	name := strings.TrimSpace(clientName)
	if name == "" || strings.EqualFold(name, "openclaw") {
		return CodeGenClientName
	}
	return name
}

func IsCodeGenHostname(hostname string) bool {
	host := strings.ToLower(strings.TrimSpace(hostname))
	return host == "codegen.qianxin-inc.cn" || strings.HasSuffix(host, ".codegen.qianxin-inc.cn")
}

func IsCodeGenURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return IsCodeGenHostname(u.Hostname())
}

func NormalizeCodeGenModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, CodeGenAutoModelAlias) {
		return CodeGenDefaultModelID
	}
	return model
}

func NormalizeCodeGenModelForURL(rawURL, model string) string {
	if IsCodeGenURL(rawURL) {
		return NormalizeCodeGenModel(model)
	}
	return strings.TrimSpace(model)
}

func NormalizeCodeGenSSOModel(model string) string {
	return NormalizeCodeGenModel(model)
}

func NormalizeCodeGenSSOProvider(provider MaclawLLMProvider) MaclawLLMProvider {
	provider.Name = strings.TrimSpace(provider.Name)
	provider.AuthType = strings.TrimSpace(provider.AuthType)
	if !strings.EqualFold(provider.AuthType, "sso") {
		return provider
	}
	if !strings.EqualFold(provider.Name, "CodeGen") && !IsCodeGenURL(provider.URL) {
		return provider
	}
	provider.Name = "CodeGen"
	provider.AuthType = "sso"
	provider.Protocol = "openai"
	provider.Model = NormalizeCodeGenModel(provider.Model)
	if strings.TrimSpace(provider.AgentType) == "" || strings.EqualFold(strings.TrimSpace(provider.AgentType), "openclaw") {
		provider.AgentType = CodeGenClientName
	}
	provider.URL = strings.TrimRight(strings.TrimSpace(provider.URL), "/")
	provider.URL = strings.TrimSuffix(provider.URL, "/anthropic")
	return provider
}

// DefaultContextTokens is the fallback context limit when no explicit
// context_length is configured on the LLM provider.
const DefaultContextTokens = 110_000

// DefaultLLMTimeoutSec is the fallback timeout in seconds when no explicit
// timeout_sec is configured on the LLM provider.
//
// Usage varies by caller:
//   - transport.ResponseHeaderTimeout: wait for first response byte only
//   - http.Client.Timeout: total request timeout (connect + headers + body)
//   - context.WithTimeout: single operation deadline
//
// 600s is the default for slower coding/reasoning models while still keeping a
// bounded failure path when an upstream request stalls.
const DefaultLLMTimeoutSec = 600

const (
	MinAgentTimeoutSec     = 240
	DefaultAgentTimeoutSec = 600
	MaxAgentTimeoutSec     = 900
)

func NormalizeAgentTimeoutSec(seconds int) int {
	if seconds <= 0 {
		return DefaultAgentTimeoutSec
	}
	if seconds < MinAgentTimeoutSec {
		return MinAgentTimeoutSec
	}
	if seconds > MaxAgentTimeoutSec {
		return MaxAgentTimeoutSec
	}
	return seconds
}

const (
	MinSkillRunnerTimeoutSec     = 240
	DefaultSkillRunnerTimeoutSec = DefaultAgentTimeoutSec
	MaxSkillRunnerTimeoutSec     = 14400
)

func NormalizeSkillRunnerTimeoutSec(seconds int) int {
	if seconds <= 0 {
		return DefaultSkillRunnerTimeoutSec
	}
	if seconds < MinSkillRunnerTimeoutSec {
		return MinSkillRunnerTimeoutSec
	}
	if seconds > MaxSkillRunnerTimeoutSec {
		return MaxSkillRunnerTimeoutSec
	}
	return seconds
}

// ModelConfig 描述一个 LLM 模型的配置。
type ModelConfig struct {
	ModelName       string `json:"model_name"`
	ModelId         string `json:"model_id"`
	ModelUrl        string `json:"model_url"`
	ApiKey          string `json:"api_key"`
	WireApi         string `json:"wire_api"` // tool-facing protocol, e.g. "anthropic" or "responses"
	AgentType       string `json:"agent_type,omitempty"`
	IsCustom        bool   `json:"is_custom"`
	IsBuiltin       bool   `json:"is_builtin"`
	HasSubscription bool   `json:"has_subscription"`
}

// ProjectConfig 描述一个项目的配置。
type ProjectConfig struct {
	Id            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	YoloMode      bool   `json:"yolo_mode"`
	AdminMode     bool   `json:"admin_mode"`
	PythonProject bool   `json:"python_project"`
	PythonEnv     string `json:"python_env"`
	TeamMode      bool   `json:"team_mode"`
	UseProxy      bool   `json:"use_proxy"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     string `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
}

// PythonEnvironment 描述一个 Python 环境。
type PythonEnvironment struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "conda", "venv", or "system"
}

// ToolConfig 描述一个工具的模型配置。
type ToolConfig struct {
	CurrentModel string        `json:"current_model"`
	Models       []ModelConfig `json:"models"`
}

// CodeBuddyModel 描述 CodeBuddy 的模型配置。
type CodeBuddyModel struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	Vendor           string `json:"vendor"`
	ApiKey           string `json:"apiKey"`
	MaxInputTokens   int    `json:"maxInputTokens"`
	MaxOutputTokens  int    `json:"maxOutputTokens"`
	Url              string `json:"url"`
	SupportsToolCall bool   `json:"supportsToolCall"`
	SupportsImages   bool   `json:"supportsImages"`
}

// CodeBuddyFileConfig 描述 CodeBuddy 的文件配置格式。
type CodeBuddyFileConfig struct {
	Models          []CodeBuddyModel `json:"Models"`
	AvailableModels []string         `json:"availableModels"`
}

// MCPServerSource 标识 MCP 服务器的来源。
type MCPServerSource string

const (
	MCPSourceManual  MCPServerSource = "manual"
	MCPSourceMDNS    MCPServerSource = "mdns"
	MCPSourceProject MCPServerSource = "project"
	MCPSourceMarket  MCPServerSource = "marketplace"
)

// MCPServerEntry 描述一个 MCP 服务器注册条目。
type MCPServerEntry struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	EndpointURL string                  `json:"endpoint_url"`
	AuthType    string                  `json:"auth_type"` // "none", "api_key", "bearer"
	AuthSecret  string                  `json:"auth_secret"`
	Headers     map[string]string       `json:"headers,omitempty"` // custom HTTP headers (e.g. Authorization, X-Custom-Key)
	CreatedAt   string                  `json:"created_at"`
	Source      MCPServerSource         `json:"source"`
	Capability  *MCPServerCapabilityRef `json:"capability,omitempty"`
}

type MCPServerCapabilityRef struct {
	CapabilityID string `json:"capability_id"`
	VersionKey   string `json:"version_key,omitempty"`
	Source       string `json:"source,omitempty"`
	GlobalKey    string `json:"global_key,omitempty"`
}

// SkillCapabilityRef links an installed Skill to a marketplace capability.
// HubSkillID/HubVersion continue to track the underlying SkillHub package,
// while this ref tracks the enterprise capability and version policy.
type SkillCapabilityRef struct {
	CapabilityID string `json:"capability_id"`
	VersionKey   string `json:"version_key,omitempty"`
	Source       string `json:"source,omitempty"`
	GlobalKey    string `json:"global_key,omitempty"`
}

// LocalMCPServerEntry 描述一个本地 MCP 服务器配置（通过命令启动，如 npx）。
type LocalMCPServerEntry struct {
	ID         string                  `json:"id"`
	Name       string                  `json:"name"`
	Command    string                  `json:"command"`
	Args       []string                `json:"args,omitempty"`
	Env        map[string]string       `json:"env,omitempty"`
	Disabled   bool                    `json:"disabled,omitempty"`
	AutoStart  bool                    `json:"auto_start,omitempty"` // only start on app launch when true
	CreatedAt  string                  `json:"created_at"`
	Source     MCPServerSource         `json:"source,omitempty"`
	Capability *MCPServerCapabilityRef `json:"capability,omitempty"`
}

// NLSkillStep 描述自然语言技能中的单个操作步骤。
// StepPollConfig configures repeated execution of a step until a condition is met.
// Used for async tasks where the runner needs to poll for completion.
type StepPollConfig struct {
	Interval    int    `json:"interval"`               // poll interval in seconds (default 5)
	MaxAttempts int    `json:"max_attempts"`           // max poll attempts (default 20)
	UntilMatch  string `json:"until_match,omitempty"`  // regex: stop when output matches
	UntilStatus string `json:"until_status,omitempty"` // shorthand: stop when output contains this string
}

// StepLoopConfig configures iterative execution of a step with verification.
// This implements the "do → verify → improve" cycle from the "5 Skill
// Architecture Patterns" article (Pattern 3: Iterative Loop).
//
// Unlike Poll (passive waiting for async results), Loop is active iteration:
// execute → verify → fix → repeat until verification passes or max iterations.
type StepLoopConfig struct {
	// MaxIterations is the hard upper bound on loop iterations. Required.
	MaxIterations int `json:"max_iterations" yaml:"max_iterations"`
	// UntilStep is the label of the verification step. After each loop body
	// execution, this step runs. If its output matches UntilMatch, the loop exits.
	UntilStep string `json:"until_step,omitempty" yaml:"until_step,omitempty"`
	// UntilMatch is a regex pattern. When the verification step's output matches,
	// the loop exits successfully.
	UntilMatch string `json:"until_match,omitempty" yaml:"until_match,omitempty"`
	// OnFailStep is the label of the repair step (optional). When verification
	// fails, this step runs before the next iteration.
	OnFailStep string `json:"on_fail_step,omitempty" yaml:"on_fail_step,omitempty"`
}

type NLSkillStep struct {
	Action    string                 `json:"action"`
	Params    map[string]interface{} `json:"params"`
	OnError   string                 `json:"on_error"`            // "stop" (default), "continue"
	Name      string                 `json:"name,omitempty"`      // optional descriptive name
	Condition string                 `json:"condition,omitempty"` // "" (always), "on_failure", "on_success"
	When      string                 `json:"when,omitempty"`      // conditional expression, e.g. "{{operation}} == generate"
	Label     string                 `json:"label,omitempty"`     // step selector label for api_workflow mode
	Capture   map[string]string      `json:"capture,omitempty"`   // output capture: varName → regex pattern (first submatch group)
	Poll      *StepPollConfig        `json:"poll,omitempty"`      // poll config for async steps
	// Loop configures iterative execution with verification gate.
	// See StepLoopConfig for the "do → verify → improve" cycle.
	Loop *StepLoopConfig `json:"loop,omitempty"`
	// FallbackStep holds the original step before solidification promotion.
	// When a promoted bash step fails, the Runner reverts to this step.
	// See corelib/skill/solidify.go for the revert mechanism.
	FallbackStep *NLSkillStep `json:"fallback_step,omitempty"`
}

// NLSkillOperation describes a named operation within an api_workflow skill.
// Operations map user intent to step labels, enabling the runner to execute
// only the relevant subset of steps.
type NLSkillOperation struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Params      []string `json:"params,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

// NLSkillEntry 描述一个自然语言技能条目。
type NLSkillEntry struct {
	// SkillID is the globally unique, immutable identifier for this skill.
	// Format: "publisher.skill-name" (e.g. "lovstudio.any2pdf").
	// Once uploaded to Hub/SkillMarket, it is bound to the uploader's account
	// and cannot be changed. Used for deterministic dependency resolution.
	// Empty for legacy skills that have not declared an id.
	SkillID string `json:"skill_id,omitempty"`

	Name          string        `json:"name"`
	DirName       string        `json:"dir_name,omitempty"` // 目录名（当与 Name 不同时用于别名查找）
	Description   string        `json:"description"`
	Triggers      []string      `json:"triggers"`
	Steps         []NLSkillStep `json:"steps"`
	Status        string        `json:"status"` // "active", "disabled"
	CreatedAt     string        `json:"created_at"`
	Source        string        `json:"source"` // "manual" | "learned" | "hub" | "crafted" | "file" | "zip_import" | "github" | "clawhub" | "auto_hub" | "auto_github" | "auto_clawhub"
	SourceProject string        `json:"source_project"`
	// ExperienceDomain scopes a self-learned skill to the kind of work it was
	// distilled from ("coding" or "general"). Empty means universal: every
	// deliberately installed skill stays visible to every agent.
	ExperienceDomain string `json:"experience_domain,omitempty"`
	HubSkillID       string `json:"hub_skill_id,omitempty"`
	HubVersion       string `json:"hub_version,omitempty"`
	// Version is the semantic version from skill.yaml (e.g. "1.3.0").
	// Used for dependency constraint checking. Distinct from HubVersion which
	// may be a Hub-internal integer version counter.
	Version     string              `json:"version,omitempty"`
	Capability  *SkillCapabilityRef `json:"capability,omitempty"`
	TrustLevel  string              `json:"trust_level,omitempty"`
	Type        string              `json:"type,omitempty"`         // "executable" (default) | "knowledge"
	Content     string              `json:"content,omitempty"`      // Markdown content for knowledge-type skills
	Platforms   []string            `json:"platforms,omitempty"`    // "windows","linux","macos"; empty = universal
	RequiresGUI bool                `json:"requires_gui,omitempty"` // Linux 下是否需要 GUI 环境

	// Tool availability conditions
	Capabilities        []string `json:"capabilities,omitempty"`
	RequiresTools       []string `json:"requires_tools,omitempty"`
	FallbackForTools    []string `json:"fallback_for_tools,omitempty"`
	RequiresToolsets    []string `json:"requires_toolsets,omitempty"`
	FallbackForToolsets []string `json:"fallback_for_toolsets,omitempty"`

	// Credential file mounting
	RequiredCredentialFiles []string `json:"required_credential_files,omitempty"`

	// Dependency auto-install
	RequiresPython []string `json:"requires_python,omitempty"` // pip packages to auto-install
	RequiresNode   []string `json:"requires_node,omitempty"`   // npm packages to auto-install
	RequiresBins   []string `json:"requires_bins,omitempty"`   // command-line binaries expected on PATH

	// Plugin namespace
	Publisher string `json:"publisher,omitempty"` // e.g. "lovstudio"

	SkillDir           string              `json:"skill_dir,omitempty"`       // 自包含 skill 目录的绝对路径（运行时填充）
	Mode               string              `json:"mode,omitempty"`            // "sequential" (default) | "interactive" | "api_workflow"
	ExecMode           string              `json:"exec_mode,omitempty"`       // "all" (default) | "first" | "named"
	GlobalTimeout      int                 `json:"global_timeout,omitempty"`  // per-skill global timeout in seconds (0 = use configured Skill Runner default)
	ProducesArtifact   bool                `json:"produces_artifact"`         // true = expects file output (default); false = diagnostic/instruction only
	Operations         []NLSkillOperation  `json:"operations,omitempty"`      // named operations for api_workflow mode
	RequiredArgs       []string            `json:"required_args,omitempty"`   // required template variables (e.g. "input", "output")
	RequiredEnv        []string            `json:"required_env,omitempty"`    // required environment variables (e.g. "API_KEY")
	PreferredShell     string              `json:"preferred_shell,omitempty"` // "bash" or "cmd"; empty = auto-detect
	UsageCount         int                 `json:"usage_count"`
	SuccessCount       int                 `json:"success_count"`
	FailureCount       int                 `json:"failure_count"`
	WorkaroundCount    int                 `json:"workaround_count"`
	LastUsedAt         string              `json:"last_used_at,omitempty"`
	LastError          string              `json:"last_error,omitempty"`
	RepairAttemptCount int                 `json:"repair_attempt_count,omitempty"`
	LastRepairAt       string              `json:"last_repair_at,omitempty"`
	RepairHistory      []SkillRepairRecord `json:"repair_history,omitempty"`
	OptimizationCount  int                 `json:"optimization_count,omitempty"`
	LastOptimizedAt    string              `json:"last_optimized_at,omitempty"`
	DiscoveredFrom     string              `json:"discovered_from,omitempty"`   // nudge candidate ContextKey (auto_discovered skills)
	TotalTokensCost    int                 `json:"total_tokens_cost,omitempty"` // cumulative LLM tokens across all invocations

	// Params is the parameter schema for this skill. When explicitly declared
	// in skill.yaml, it provides aliases, CLI flags, defaults, and descriptions.
	// When absent, SynthesizeParams auto-generates it from command templates.
	// All skills flow through the same BindParams path regardless of source.
	Params []NLSkillParam `json:"params,omitempty"`

	// SolidificationCandidates tracks craft_tool steps that are candidates
	// for promotion to bash steps. See corelib/skill/solidify.go.
	SolidificationCandidates []SolidificationCandidate `json:"solidification_candidates,omitempty"`

	// Stateful marks this skill as having cross-invocation persistent state.
	// When true, the Runner loads state from {skillDir}/.state/state.json
	// before execution and saves it after. See corelib/skill/state.go.
	Stateful bool `json:"stateful,omitempty"`

	// References lists on-demand reference documents in the skill's
	// references/ directory. These are NOT injected into LLM context by
	// default — only an index (filename + description) is injected.
	// LLM loads full content via manage_skill(action=read_ref).
	References []SkillReference `json:"references,omitempty"`

	// Pipeline declares a multi-skill orchestration sequence.
	// When Mode=="pipeline", the Runner delegates to PipelineRunner
	// instead of executing Steps directly.
	Pipeline []SkillPipelineStep `json:"pipeline,omitempty"`
}

// NLSkillParam describes a single parameter in a skill's parameter schema.
// This is the contract between the LLM (which provides args) and the skill
// (which consumes them via command template placeholders or CLI flags).
type NLSkillParam struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty"`      // JSON Schema type when known: string/number/integer/boolean/array/object
	Aliases     []string `json:"aliases,omitempty"`   // alternative names the LLM might use
	CLIFlag     string   `json:"cli_flag,omitempty"`  // e.g. "--format" — appended to command
	Default     string   `json:"default,omitempty"`   // default value when not provided
	Required    bool     `json:"required,omitempty"`  // must be provided for execution
	Synthetic   bool     `json:"synthetic,omitempty"` // true = auto-generated from template
}

// SolidificationCandidate tracks a craft_tool step's promotion progress
// toward becoming a fixed bash step. See corelib/skill/solidify.go for the
// three-stage pipeline (Record → Promote → Revert).
type SolidificationCandidate struct {
	StepIndex    int      `json:"step_index"`
	ScriptPath   string   `json:"script_path"`
	Language     string   `json:"language"`
	ParamSlots   []string `json:"param_slots,omitempty"`
	SuccessCount int      `json:"success_count"`
	// Signature is the structural hash of the last recorded script.
	// Consecutive successes only count toward promotion when all scripts
	// share the same signature (same code structure, different parameters).
	// A signature change resets the streak.
	Signature string `json:"signature,omitempty"`
	LastUsed  string `json:"last_used,omitempty"`
}

// SkillRepairRecord stores a single self-repair attempt for audit trail.
type SkillRepairRecord struct {
	Timestamp   string `json:"timestamp"`
	ErrorClass  string `json:"error_class,omitempty"`
	Explanation string `json:"explanation"`
	Success     bool   `json:"success"`
	// Via identifies the repair channel, e.g. "reviewed_draft" for a
	// human-approved repair draft; empty means the automatic pipeline.
	Via string `json:"via,omitempty"`
}

// SkillReference describes an on-demand reference document in a skill's
// references/ directory. Only the index (filename + description) is injected
// into LLM context; full content is loaded via manage_skill(action=read_ref).
type SkillReference struct {
	Filename    string `json:"filename"`
	Description string `json:"description,omitempty"` // one-line summary (from first heading)
	TokenCount  int    `json:"token_count,omitempty"` // estimated token count
}

// SkillPipelineStep declares one step in a skill pipeline (Pattern 5:
// Multi-Phase + Checkpoint). See corelib/skill/pipeline.go.
type SkillPipelineStep struct {
	Skill              string            `json:"skill" yaml:"skill"`
	Params             map[string]string `json:"params,omitempty" yaml:"params,omitempty"`
	Checkpoint         bool              `json:"checkpoint,omitempty" yaml:"checkpoint,omitempty"`
	CheckpointMessage  string            `json:"checkpoint_message,omitempty" yaml:"checkpoint_message,omitempty"`
	ContinueOnFail     bool              `json:"continue_on_fail,omitempty" yaml:"continue_on_fail,omitempty"`
	TimeImpactOnReject string            `json:"time_impact_on_reject,omitempty" yaml:"time_impact_on_reject,omitempty"`
}

// QualifiedID returns the canonical unique identifier for this skill.
// Priority: SkillID (publisher.name format) > Publisher:Name > bare Name.
// This is the stable identity used for App→Skill dependency binding.
// Once a skill is published, its QualifiedID is immutable.
func (e *NLSkillEntry) QualifiedID() string {
	// Prefer the new SkillID field (format: "publisher.skill-name")
	if sid := strings.TrimSpace(e.SkillID); sid != "" {
		return sid
	}
	// Legacy fallback: publisher:name
	name := strings.TrimSpace(e.Name)
	if name == "" {
		return ""
	}
	publisher := strings.TrimSpace(e.Publisher)
	if publisher != "" {
		return publisher + ":" + name
	}
	return name
}

// NormalizeSkillMatchQuery trims whitespace and strips a trailing @version
// pin (e.g. "paper_pdf_translator@1.0.0" → "paper_pdf_translator") so app
// dependency refs and run_skill calls share one lookup key.
//
// Both the query and the stored identity (HubSkillID / SkillID) must be
// normalized before comparison — enterprise hub refs often look like
// "enterprise_hub:skill:paper_pdf_translator@6c2a9af36010" and upload
// submission ids embed that form. Stripping only the query side leaves a
// permanent mismatch against the unstripped HubSkillID.
func NormalizeSkillMatchQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	if at := strings.IndexByte(query, '@'); at > 0 {
		query = strings.TrimSpace(query[:at])
	}
	return query
}

// ExtractSkillPackageIDFromHubRef pulls the stable package / skill id out of
// hub version keys and dual-upload submission ids, e.g.:
//
//	enterprise_hub:skill:paper_pdf_translator@6c2a9af36010
//	sub-…;enterprise_hub=enterprise_hub:skill:paper_pdf_translator@6c2a9af36010
//
// Returns "" when no package segment is present.
func ExtractSkillPackageIDFromHubRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	const marker = ":skill:"
	lower := strings.ToLower(ref)
	i := strings.LastIndex(lower, marker)
	if i < 0 {
		return ""
	}
	rest := ref[i+len(marker):]
	if at := strings.IndexByte(rest, '@'); at >= 0 {
		rest = rest[:at]
	}
	if semi := strings.IndexByte(rest, ';'); semi >= 0 {
		rest = rest[:semi]
	}
	rest = strings.TrimSpace(rest)
	// Reject empty / submission-shaped package segments, but keep ordinary
	// package names that merely start with "sub-" (e.g. sub-process-monitor).
	if rest == "" || IsUploadSubmissionSkillRef(rest) {
		return ""
	}
	return rest
}

// IsUploadSubmissionSkillRef reports whether ref is an upload/submission tracking
// id (not a stable package id suitable as the preferred run_skill argument).
//
// Matches:
//   - dual-upload composites: "sub-…;enterprise_hub=…"
//   - bare enterprise upload tokens: "enterprise_hub=…"
//   - lifecycle submission ids: "sub-<digits>…" (timestamp-prefixed)
//
// Does NOT match ordinary package names that merely start with "sub-"
// (e.g. "sub-process-monitor") — those remain valid runtime identities.
func IsUploadSubmissionSkillRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	lower := strings.ToLower(ref)
	if strings.Contains(lower, ";enterprise_hub=") || strings.HasPrefix(lower, "enterprise_hub=") {
		return true
	}
	// Lifecycle submission ids: sub-<unix-millis>-… (digit after prefix).
	if strings.HasPrefix(lower, "sub-") && len(lower) > 4 {
		c := lower[4]
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// StableSkillIdentityFromRef returns a run-safe skill identity from a hub ref,
// version key, or upload submission id. Prefer an embedded package id; reject
// bare upload tracking ids; then try optional fallbacks (Name, prior HubSkillID).
//
// Used by PreferredRuntimeSkillRef, file scanner, MarkUploaded, and auto-install.
func StableSkillIdentityFromRef(ref string, fallbacks ...string) string {
	try := func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" {
			return ""
		}
		if pkg := ExtractSkillPackageIDFromHubRef(v); pkg != "" {
			return pkg
		}
		if !IsUploadSubmissionSkillRef(v) {
			return v
		}
		return ""
	}
	if out := try(ref); out != "" {
		return out
	}
	for _, fb := range fallbacks {
		if out := try(fb); out != "" {
			return out
		}
	}
	return ""
}

// skillIdentityMatchKeys returns the normalized comparison keys for a skill
// identity string: the @version-stripped form and, when present, the embedded
// package id from hub/submission refs.
func skillIdentityMatchKeys(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	keys := make([]string, 0, 2)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = NormalizeSkillMatchQuery(v)
		if v == "" {
			return
		}
		// Case-fold for EqualFold-style map keys without allocating per compare.
		v = strings.ToLower(v)
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		keys = append(keys, v)
	}
	add(raw)
	// Extract from the original string (before @ strip) so version keys still yield package ids.
	add(ExtractSkillPackageIDFromHubRef(raw))
	return keys
}

func skillIdentityKeysOverlap(a, b string) bool {
	ak := skillIdentityMatchKeys(a)
	if len(ak) == 0 {
		return false
	}
	bk := skillIdentityMatchKeys(b)
	// Key sets are tiny (≤2: stripped ref + optional package id).
	for _, x := range ak {
		for _, y := range bk {
			if x == y {
				return true
			}
		}
	}
	return false
}

// MatchesQualifiedID checks if the skill matches the given qualified ID exactly.
// This is stricter than MatchesName — it only matches against SkillID,
// publisher-qualified ID (Publisher:Name), and HubSkillID.
// It does NOT match bare display Name / DirName (QualifiedID() without a
// Publisher falls back to Name and must not widen this check).
// Used for App dependency resolution and collision preference where
// deterministic identity is required.
//
// Both sides are normalized (including @version strip and package extraction
// from dual-upload / enterprise hub refs) so submission-shaped HubSkillIDs still
// match package ids and the reverse.
func (e *NLSkillEntry) MatchesQualifiedID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	candidates := make([]string, 0, 3)
	if sid := strings.TrimSpace(e.SkillID); sid != "" {
		candidates = append(candidates, sid)
	}
	// Only treat publisher:name as stable when Publisher is set; otherwise
	// QualifiedID() returns the bare display Name.
	if pub := strings.TrimSpace(e.Publisher); pub != "" {
		if name := strings.TrimSpace(e.Name); name != "" {
			candidates = append(candidates, pub+":"+name)
		}
	}
	if hub := strings.TrimSpace(e.HubSkillID); hub != "" {
		candidates = append(candidates, hub)
	}
	for _, c := range candidates {
		if skillIdentityKeysOverlap(c, id) {
			return true
		}
	}
	return false
}

// MatchesName checks if the skill matches the given name by comparing against
// stable IDs first (SkillID / HubSkillID / Publisher:Name), then display Name,
// DirName, and SkillDir basename. This allows run_skill / MaClaw Apps to find
// skills by any of their known identifiers.
//
// Hub-installed skills commonly have display Name "Paper PDF Translator" while
// apps/runtime still request the stable HubSkillID "paper_pdf_translator".
// That identity is matched via HubSkillID (and loose Name/DirName normalisation).
// Submission/version-key queries also match when their embedded package id
// equals Name (HubSkillID may be empty on pure file skills).
func (e *NLSkillEntry) MatchesName(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	// Deterministic IDs first (SkillID / HubSkillID / Publisher:Name).
	if e.MatchesQualifiedID(query) {
		return true
	}
	// Loose display keys: raw query and embedded package id (if any).
	looseQueries := []string{NormalizeSkillMatchQuery(query)}
	if pkg := ExtractSkillPackageIDFromHubRef(query); pkg != "" {
		looseQueries = append(looseQueries, NormalizeSkillMatchQuery(pkg))
	}
	for _, q := range looseQueries {
		if q == "" {
			continue
		}
		if skillIdentityEqual(e.Name, q) {
			return true
		}
		if e.DirName != "" && skillIdentityEqual(e.DirName, q) {
			return true
		}
		// Fallback: match by directory basename from SkillDir for skills
		// that were loaded from config without DirName populated.
		if e.SkillDir != "" && e.DirName == "" {
			base := filepath.Base(e.SkillDir)
			if skillIdentityEqual(base, q) {
				return true
			}
		}
	}
	return false
}

// skillIdentityEqual reports whether two skill identity strings refer to the
// same skill. EqualFold first; then normalize spaces/hyphens to underscores so
// "Paper PDF Translator" matches "paper_pdf_translator".
func skillIdentityEqual(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	return NormalizeSkillIdentityKey(a) == NormalizeSkillIdentityKey(b)
}

// NormalizeSkillIdentityKey lowercases and collapses spaces/hyphens to
// underscores so "Paper PDF Translator" and "paper_pdf_translator" share a key.
func NormalizeSkillIdentityKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevUnderscore := false
	for _, r := range s {
		switch {
		case r == ' ' || r == '-' || r == '_':
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		default:
			b.WriteRune(r)
			prevUnderscore = false
		}
	}
	return strings.Trim(b.String(), "_")
}

// SkillIdentityKeys returns every lookup key that should resolve to this skill
// in maps/indexes used by app install planning and runtime resolution.
// Keys are lowercased; both raw and underscore-normalized forms are included.
//
// Coordinate model (authoring → plan → run):
//   - Declared app dependency id SHOULD be a stable id (HubSkillID / SkillID)
//   - Local registry may store a localized display Name
//   - Indexes and MatchesName must accept both coordinate systems
func (e NLSkillEntry) SkillIdentityKeys() []string {
	raw := []string{
		e.SkillID,
		e.HubSkillID,
		e.Name,
		e.DirName,
	}
	if pkg := ExtractSkillPackageIDFromHubRef(e.HubSkillID); pkg != "" {
		raw = append(raw, pkg)
	}
	if pkg := ExtractSkillPackageIDFromHubRef(e.SkillID); pkg != "" {
		raw = append(raw, pkg)
	}
	if pub := strings.TrimSpace(e.Publisher); pub != "" {
		if name := strings.TrimSpace(e.Name); name != "" {
			raw = append(raw, pub+":"+name)
		}
	}
	if e.SkillDir != "" {
		raw = append(raw, filepath.Base(e.SkillDir))
	}
	return CollectSkillIdentityKeys(raw...)
}

// PreferredRuntimeSkillRef is the preferred argument for run_skill /
// RunNLSkillAsync. Stable package identity (HubSkillID, SkillID) is preferred
// over localized display Name so MaClaw Apps that declare appSkill.id as a hub
// id keep one coordinate from authoring through execution.
//
// Upload submission ids are never returned as run handles — they resolve to the
// embedded package id (e.g. paper_pdf_translator) or fall back to Name.
func (e NLSkillEntry) PreferredRuntimeSkillRef() string {
	pubName := ""
	if pub := strings.TrimSpace(e.Publisher); pub != "" {
		if name := strings.TrimSpace(e.Name); name != "" {
			pubName = pub + ":" + name
		}
	}
	if ref := StableSkillIdentityFromRef(e.HubSkillID, e.SkillID, pubName, e.Name); ref != "" {
		return ref
	}
	return strings.TrimSpace(e.Name)
}

// CollectSkillIdentityKeys normalizes and dedupes raw identity strings into
// lowercase map keys (including underscore-normalized display names).
func CollectSkillIdentityKeys(parts ...string) []string {
	out := make([]string, 0, len(parts)*2)
	seen := map[string]struct{}{}
	add := func(v string) {
		v = NormalizeSkillMatchQuery(v)
		if v == "" {
			return
		}
		for _, key := range []string{strings.ToLower(v), NormalizeSkillIdentityKey(v)} {
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	for _, p := range parts {
		add(p)
	}
	return out
}

// learnedSources is the set of Source values that classify a skill as
// "learned" (self-generated by Maclaw from experience or crafted via craft_tool).
// Auto-installed skills (auto_hub, auto_github, auto_clawhub) are NOT included
// because they originate from external hubs, not from Maclaw's own learning.
var learnedSources = map[string]bool{
	"learned": true,
	"crafted": true,
}

// IsLearnedSource returns true if the given source value indicates a skill
// that was self-generated by Maclaw (learned from experience or crafted via craft_tool).
func IsLearnedSource(source string) bool {
	return learnedSources[strings.ToLower(strings.TrimSpace(source))]
}

// Experience domains partition self-learned skills by the kind of work they
// were distilled from. Coding sessions and general assistant sessions build up
// separate pools so a chat about the weather never surfaces as a capability in
// a coding turn, while every coding task still shares what other coding tasks
// learned.
const (
	SkillDomainCoding  = "coding"
	SkillDomainGeneral = "general"
	// SkillDomainUniversal is the empty domain carried by deliberately
	// installed skills (manual, hub, market, github). The user chose to install
	// them, so they stay visible everywhere.
	SkillDomainUniversal = ""
)

// NormalizeSkillExperienceDomain maps a raw domain value onto a known domain,
// falling back to universal for anything unrecognized so an unknown value can
// never hide a skill.
func NormalizeSkillExperienceDomain(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case SkillDomainCoding:
		return SkillDomainCoding
	case SkillDomainGeneral:
		return SkillDomainGeneral
	default:
		return SkillDomainUniversal
	}
}

// SkillVisibleInExperienceDomain reports whether a skill carrying skillDomain
// may be offered to an agent working in agentDomain. Universal skills are
// always visible; a domain-scoped skill is visible only within its own domain.
// An agent with no resolved domain sees everything, so a new call site cannot
// silently hide installed capabilities by forgetting to pass a domain.
func SkillVisibleInExperienceDomain(skillDomain, agentDomain string) bool {
	skillDomain = NormalizeSkillExperienceDomain(skillDomain)
	agentDomain = NormalizeSkillExperienceDomain(agentDomain)
	if skillDomain == SkillDomainUniversal || agentDomain == SkillDomainUniversal {
		return true
	}
	return skillDomain == agentDomain
}

// MaclawLLMProvider 描述一个 MaClaw LLM 提供商配置。
type MaclawLLMProvider struct {
	// ID is a stable opaque provider identifier. Name remains a user-editable
	// label kept for legacy APIs and display.
	ID              string   `json:"id,omitempty"`
	Name            string   `json:"name"`
	URL             string   `json:"url"`
	Key             string   `json:"key"`
	Model           string   `json:"model"`
	Protocol        string   `json:"protocol,omitempty"`
	ContextLength   int      `json:"context_length,omitempty"`
	TimeoutSec      int      `json:"timeout_sec,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"` // per-request output token limit; 0 = use system default
	Models          []string `json:"models,omitempty"`            // provider-specific model IDs, when discovered from the service
	// ImportSource marks a provider created by scanning another local agent.
	// Values: "codex", "claude_code", "opencode". The settings UI treats these
	// as model-select-only (URL/key/protocol stay hidden).
	ImportSource string `json:"import_source,omitempty"`
	// ConnectionTestPassed is set only after the provider's current connection
	// configuration has completed a successful test. Model assignment surfaces
	// use it to avoid offering unverified providers for execution.
	ConnectionTestPassed bool `json:"connection_test_passed,omitempty"`
	IsCustom             bool `json:"is_custom,omitempty"`
	IsHubService         bool `json:"is_hub_service,omitempty"`
	// VisionModels records the provider model IDs whose image-input capability
	// has been confirmed. SupportsVision remains the compatibility projection
	// for Model, so older configuration files keep their existing behaviour.
	VisionModels   []string `json:"vision_models,omitempty"`
	SupportsVision bool     `json:"supports_vision"`
	AgentType      string   `json:"agent_type,omitempty"` // "openclaw" (default) or "claude" → controls User-Agent header
	// ── 新增 OAuth 字段 ──
	AuthType                 string  `json:"auth_type,omitempty"`
	RefreshToken             string  `json:"refresh_token,omitempty"`
	TokenExpiresAt           int64   `json:"token_expires_at,omitempty"`
	OAuthAccessToken         string  `json:"oauth_access_token,omitempty"` // 原始 access_token，仅用于 Costs/Usage API 查询
	WireAPI                  string  `json:"wire_api,omitempty"`           // "chat" or "responses"; empty defaults to "chat"
	InputPricePerMTokensRMB  float64 `json:"input_price_per_m_tokens_rmb,omitempty"`
	OutputPricePerMTokensRMB float64 `json:"output_price_per_m_tokens_rmb,omitempty"`
}

// UserAgent returns the User-Agent header value for LLM API requests.
func (p MaclawLLMProvider) UserAgent() string {
	if agentType := strings.TrimSpace(p.AgentType); agentType != "" {
		return agentType
	}
	return "openclaw"
}

// IsCodexSubscriptionOAuthProvider reports whether this provider targets
// ChatGPT's Codex subscription backend.
func (p MaclawLLMProvider) IsCodexSubscriptionOAuthProvider() bool {
	if strings.TrimSpace(p.Name) != "OpenAI" ||
		!strings.Contains(strings.ToLower(p.URL), "chatgpt.com") {
		return false
	}
	return true
}

// CodexSubscriptionOAuthToken returns the OAuth JWT for ChatGPT's Codex
// subscription backend. The backend does not accept platform sk- API keys.
func (p MaclawLLMProvider) CodexSubscriptionOAuthToken() string {
	if !p.IsCodexSubscriptionOAuthProvider() {
		return ""
	}
	return strings.TrimSpace(p.OAuthAccessToken)
}

// MaclawLLMConfig 是 MaClaw 桌面 Agent 的 LLM 配置。
type MaclawLLMConfig struct {
	URL             string `json:"url"`
	Key             string `json:"key"`
	Model           string `json:"model"`
	Protocol        string `json:"protocol,omitempty"`
	ContextLength   int    `json:"context_length,omitempty"`
	TimeoutSec      int    `json:"timeout_sec,omitempty"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"` // per-request output token limit; 0 = use system default
	SupportsVision  bool   `json:"supports_vision"`
	AgentType       string `json:"agent_type,omitempty"`    // "openclaw" (default) or "claude" → controls User-Agent header
	WireAPI         string `json:"wire_api,omitempty"`      // "chat" or "responses"; empty defaults to "chat"
	ProviderName    string `json:"provider_name,omitempty"` // human-readable provider name (e.g. "智谱编程")
	ProviderID      string `json:"provider_id,omitempty"`   // stable shared-provider identity for usage attribution
	// Profile identifies the configured execution profile that produced this
	// snapshot (assistant or coding). RouteSource is filled by a later routing
	// layer when it replaces the profile's base model.
	Profile                  string `json:"profile,omitempty"`
	RouteSource              string `json:"route_source,omitempty"`
	AuthType                 string `json:"auth_type,omitempty"`
	MaclawAgentMaxIterations int    `json:"maclaw_agent_max_iterations,omitempty"`

	// EnablePromptCache hints to the LLM client that the system prompt is
	// stable across iterations and should be marked for provider-side caching.
	// When true: Anthropic → cache_control:{type:"ephemeral"} on system block;
	// OpenAI/DeepSeek → implicit prefix caching (no client action needed).
	// Used by CodingSubAgent where system prompt never changes across 80 iterations.
	EnablePromptCache bool `json:"enable_prompt_cache,omitempty"`

	// ReasoningEffort is optional OpenAI-style reasoning_effort
	// (none|minimal|low|medium|high). Empty = provider default.
	// Set by cost-route Phase 3 when MACLAW_COST_ROUTE=on.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ThinkingMode controls DeepSeek-style request body thinking object:
	// "" = auto (existing IsDeepSeekThinkingModeModel default),
	// "enabled" | "disabled" for explicit override (cost-route Phase 3).
	ThinkingMode string `json:"thinking_mode,omitempty"`

	// HubManaged and the *Hint fields are request-only L1 signals for Hub /
	// HubCenter. They are never persisted in config files. Desktop and TUI
	// populate them when the endpoint already owns routing (model=auto).
	HubManaged        bool   `json:"-"`
	WorkloadClassHint string `json:"-"`
	WorkflowTypeHint  string `json:"-"`
	PhaseKindHint     string `json:"-"`
	TaskTypeHint      string `json:"-"`
}

// IsResponsesAPI reports whether this config targets the OpenAI Responses API.
// This matches both "responses" (HTTP+SSE) and "responses-ws" (WebSocket) so
// that non-streaming fallback paths work for WebSocket providers.
func (c MaclawLLMConfig) IsResponsesAPI() bool {
	w := strings.ToLower(strings.TrimSpace(c.WireAPI))
	return w == "responses" || w == "responses-ws"
}

// IsResponsesWebSocket reports whether this config targets the OpenAI
// Responses API over WebSocket transport.
func (c MaclawLLMConfig) IsResponsesWebSocket() bool {
	return strings.EqualFold(strings.TrimSpace(c.WireAPI), "responses-ws")
}

// EffectiveMaxOutputTokens returns the max output token limit for this config.
// Priority: user-configured value > model-aware default.
//
// Design principle: the default should be large enough that truncation never
// happens for models that support it, while relying on the binary-halving
// downgrade (in stream.go) to auto-discover the actual limit for models that
// reject the value. API providers charge by actual output consumed, not by
// max_tokens requested, so over-requesting has zero cost impact.
func (c MaclawLLMConfig) EffectiveMaxOutputTokens() int {
	if c.MaxOutputTokens > 0 {
		return c.MaxOutputTokens
	}
	// 2026 mainstream models: DeepSeek V4 384K, GLM-5.1 131K, GLM-5.2 384K,
	// Claude 4 64K, GPT-4o 16K, Kimi K2 128K, Qwen 3.5 32K+.
	// Use 65536 as the universal default — covers all mainstream models.
	// The few legacy models with lower limits (GLM-4: 4096, GPT-3.5: 4096)
	// will trigger the binary-halving downgrade on first request, then cache.
	return 65536
}

type MaclawLLMTestResult struct {
	Message           string `json:"message"`
	SupportsVision    bool   `json:"supports_vision"`
	VisionProbeStatus string `json:"vision_probe_status,omitempty"`
}

// WebSearchProvider 描述一个网页搜索 provider 配置。
type WebSearchProvider struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Key     string `json:"key,omitempty"`
	BaseURL string `json:"base_url,omitempty"`
}

const (
	WebSearchStrategyVersion = 1

	WebSearchPresetMainland      = "mainland"
	WebSearchPresetInternational = "international"
	WebSearchPresetCustom        = "custom"

	WebSearchModePriority  = "priority"
	WebSearchModeSmart     = "smart"
	WebSearchModeAggregate = "aggregate"

	WebSearchTransportAPI      = "api"
	WebSearchTransportHTTPHTML = "http_html"
	WebSearchTransportBrowser  = "browser"
)

// WebSearchStrategy controls which engines web_search tries and in what order.
// Engine order is represented explicitly by Priority so GUI drag-and-drop and
// non-GUI clients share the exact same behavior.
type WebSearchStrategy struct {
	Version                   int                     `json:"version"`
	Preset                    string                  `json:"preset"`
	Mode                      string                  `json:"mode"`
	Engines                   []WebSearchEngineConfig `json:"engines"`
	BrowserFallbackEnabled    bool                    `json:"browser_fallback_enabled"`
	BrowserFallbackEngineID   string                  `json:"browser_fallback_engine_id"`
	BrowserHumanAssistEnabled bool                    `json:"browser_human_assist_enabled,omitempty"`
	HedgingDelayMS            int                     `json:"hedging_delay_ms,omitempty"`
	MinResultsBeforeHedge     int                     `json:"min_results_before_hedge,omitempty"`
}

// WebSearchEngineConfig is one built-in engine in a search strategy. APIKey
// remains compatible with the existing provider secret storage during the v1
// migration; UI-facing views must mask it.
type WebSearchEngineConfig struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Transport string `json:"transport"`
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

// UserAgent returns the User-Agent header value for LLM API requests.
// Returns AgentType as the User-Agent string.
// Default is "openclaw" when AgentType is empty.
func (c MaclawLLMConfig) UserAgent() string {
	if agentType := strings.TrimSpace(c.AgentType); agentType != "" {
		return agentType
	}
	return "openclaw"
}

func (c MaclawLLMConfig) UpstreamModel() string {
	return NormalizeCodeGenModelForURL(c.URL, c.Model)
}

func (c MaclawLLMConfig) NeedsConservativeOpenAICompatSanitization() bool {
	if IsCodeGenURL(c.URL) {
		return true
	}
	if IsMaclawOfficialHubLLMURL(c.URL) {
		return true
	}
	return IsQwenOpenAICompat(c)
}

func IsMaclawOfficialHubLLMURL(rawURL string) bool {
	text := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(text, "hub.mypapers.top/api/llm/v1")
}

// WithHubWorkloadHints marks this snapshot as Hub-managed and attaches
// classifier hints. Desktop never remaps these onto a local model_routes pick.
func (c MaclawLLMConfig) WithHubWorkloadHints(taskType, workflowType, phaseKind string) MaclawLLMConfig {
	c.HubManaged = true
	c.TaskTypeHint = strings.TrimSpace(taskType)
	c.WorkflowTypeHint = strings.TrimSpace(workflowType)
	c.PhaseKindHint = strings.TrimSpace(phaseKind)
	return c
}

// ShouldSendWorkloadHints reports whether L1 hint headers may leave this client.
func (c MaclawLLMConfig) ShouldSendWorkloadHints() bool {
	return c.HubManaged || IsHubManagedLLMEndpoint(c.URL, c.Model)
}

// IsHubManagedLLMEndpoint reports desktop/TUI configs that must not apply
// local model_routes after Hub or HubCenter already owns L1.
func IsHubManagedLLMEndpoint(rawURL, model string) bool {
	if IsMaclawOfficialHubLLMURL(rawURL) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(rawURL))
	if !strings.Contains(text, "/api/llm/v1") {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(model))
	return name == "" || name == "auto" || name == "default" || strings.HasPrefix(name, "official-")
}

func IsQwenOpenAICompat(cfg MaclawLLMConfig) bool {
	text := strings.ToLower(strings.Join([]string{
		cfg.Model,
		cfg.ProviderName,
		cfg.URL,
	}, " "))
	for _, token := range []string{
		"qwen",
		"tongyi",
		"dashscope",
		"bailian",
		"aliyuncs.com",
		"阿里",
		"通义",
		"百炼",
	} {
		if strings.Contains(text, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

// EffectiveTimeoutSec returns the configured response-header timeout in seconds,
// clamped to the supported agent range.
func (c MaclawLLMConfig) EffectiveTimeoutSec() int {
	return NormalizeAgentTimeoutSec(c.TimeoutSec)
}

// AnthropicMessagesEndpoint returns the Anthropic Messages API endpoint
// derived from the configured URL. If the URL already ends with "/v1",
// it appends "/messages" directly; otherwise it appends "/v1/messages".
// This avoids double "/v1" when the base URL is e.g. "https://host/api/v1".
func AnthropicMessagesEndpoint(baseURL string) string {
	return appendV1Path(baseURL, "/messages")
}

// AnthropicBaseURL returns the SDK base URL for an Anthropic-compatible
// endpoint, stripping message-specific suffixes if the caller provided them.
func AnthropicBaseURL(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	lower := strings.ToLower(base)
	for _, suffix := range []string{"/v1/messages", "/messages", "/v1"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimRight(base[:len(base)-len(suffix)], "/")
		}
	}
	return base
}

// SetAnthropicAuthHeaders sets both x-api-key and Authorization Bearer headers
// on the request for Anthropic-protocol compatibility. Standard Anthropic uses
// x-api-key; CodeGen and other gateways use Authorization Bearer.
func SetAnthropicAuthHeaders(req *http.Request, key string) {
	if key == "" {
		return
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("Authorization", "Bearer "+key)
}

// IsAnthropicWireAPI reports whether a tool-facing provider uses the
// Anthropic-compatible wire protocol.
func IsAnthropicWireAPI(wireAPI string) bool {
	return strings.EqualFold(strings.TrimSpace(wireAPI), "anthropic")
}

// NeedsSystemMerge returns true for providers that do not support the "system"
// role in the messages array (e.g. MiniMax). For these providers the system
// content must be merged into the first user message instead.
func NeedsSystemMerge(cfg MaclawLLMConfig) bool {
	return strings.Contains(cfg.URL, "minimaxi.com")
}

// IsDeepSeekThinking reports whether this config targets a DeepSeek model
// that uses thinking mode (reasoning_content). DeepSeek V4+ models have
// thinking mode enabled by default. When tools are present in the request,
// the API requires reasoning_content to be preserved on ALL assistant
// messages — not just those with tool_calls.
//
// Detection: URL contains "deepseek.com" OR model name starts with "deepseek".
// This covers both direct DeepSeek API and third-party proxies that use
// DeepSeek model names.
func IsDeepSeekThinking(cfg MaclawLLMConfig) bool {
	lower := strings.ToLower(cfg.URL)
	if strings.Contains(lower, "deepseek.com") {
		return true
	}
	model := strings.ToLower(cfg.Model)
	return strings.HasPrefix(model, "deepseek")
}

// IsDeepSeekThinkingModeModel reports whether this config targets a DeepSeek
// model that should have thinking mode explicitly enabled. This is narrower
// than IsDeepSeekThinking (which covers message serialization compatibility):
// only V4+ models that support and default to thinking mode are included.
//
// Known thinking-capable models:
//   - deepseek-v4-flash, deepseek-v4-pro, deepseek-v4 (V4 family)
//   - deepseek-reasoner (V3 thinking alias, maps to V4-Flash thinking mode)
//
// Excluded (non-thinking or legacy):
//   - deepseek-chat (V4-Flash non-thinking alias)
//   - deepseek-coder, deepseek-coder-v2 (V2/V3 legacy, no thinking support)
func IsDeepSeekThinkingModeModel(cfg MaclawLLMConfig) bool {
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	switch {
	case strings.Contains(model, "deepseek-v4"):
		// All V4 variants (deepseek-v4-flash, deepseek-v4-pro, etc.)
		return true
	case model == "deepseek-reasoner":
		// V3 thinking alias, now maps to V4-Flash thinking mode
		return true
	default:
		return false
	}
}

const (
	ZhipuCodingProviderName      = "智谱编程"
	ZhipuCodingDefaultModel      = "glm-5.3"
	zhipuCodingTUIProviderName   = "智谱 GLM (Coding)"
	zhipuCodingTUIProviderNameEn = "Zhipu GLM Coding"
)

// IsZhipuCodingProviderName reports whether name is the GUI or TUI preset for
// Zhipu's Anthropic coding endpoint. The two surfaces historically used
// different display names for the same provider.
func IsZhipuCodingProviderName(name string) bool {
	name = strings.TrimSpace(name)
	return strings.EqualFold(name, ZhipuCodingProviderName) ||
		strings.EqualFold(name, zhipuCodingTUIProviderName) ||
		strings.EqualFold(name, zhipuCodingTUIProviderNameEn)
}

// MaclawLLMProviderNameEqual reports whether two provider names refer to the
// same saved entry. GUI and TUI historically used different display names for
// the Zhipu coding preset.
func MaclawLLMProviderNameEqual(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if strings.EqualFold(a, b) {
		return true
	}
	return IsZhipuCodingProviderName(a) && IsZhipuCodingProviderName(b)
}

// MaclawLLMLegacyProviderID is the stable in-memory ID for a name-only
// provider. The GUI uses it so settings can reference a legacy entry without
// writing config; the next controlled save persists a real ID.
func MaclawLLMLegacyProviderID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	sum := sha256.Sum256([]byte(name))
	return "legacy_llmp_" + hex.EncodeToString(sum[:8])
}

func isLegacyZhipuCodingDefaultModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "glm-5.2", "glm-5.1", "glm-5", "glm-5.0":
		return true
	default:
		return false
	}
}

// MigrateZhipuCodingModel upgrades former built-in Zhipu coding defaults to
// glm-5.3. Explicit custom model IDs and unrelated providers are left unchanged.
func MigrateZhipuCodingModel(providerName, model string) string {
	model = strings.TrimSpace(model)
	if !IsZhipuCodingProviderName(providerName) {
		return model
	}
	if isLegacyZhipuCodingDefaultModel(model) {
		return ZhipuCodingDefaultModel
	}
	base, suffix := model, ""
	if i := strings.IndexByte(model, '['); i > 0 {
		base, suffix = model[:i], model[i:]
	}
	if strings.EqualFold(base, ZhipuCodingDefaultModel) {
		return ZhipuCodingDefaultModel + suffix
	}
	return model
}

// ApplyZhipuCodingDefaultMigration upgrades former built-in models on Zhipu
// coding preset providers. Custom selections are preserved.
func ApplyZhipuCodingDefaultMigration(providers []MaclawLLMProvider) {
	for i := range providers {
		if IsZhipuCodingProviderName(providers[i].Name) {
			providers[i].Model = MigrateZhipuCodingModel(providers[i].Name, providers[i].Model)
		}
	}
}

// ApplyZhipuCodingConfigMigration upgrades former built-in Zhipu coding
// defaults on the provider list, legacy flat model, and dual-profile models.
func ApplyZhipuCodingConfigMigration(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	// Callers often pass a by-value AppConfig that still shares the provider
	// slice and profile pointer. Clone before mutating so a save cannot leave
	// the caller with upgraded providers and a stale flat model (or vice versa).
	detachSharedMaclawLLMConfig(cfg)
	ApplyZhipuCodingDefaultMigration(cfg.MaclawLLMProviders)
	cfg.MaclawLLMModel = MigrateZhipuCodingModel(zhipuCodingMigrationProviderName(cfg), cfg.MaclawLLMModel)
	migrateZhipuCodingProfiles(cfg)
}

func detachSharedMaclawLLMConfig(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	if n := len(cfg.MaclawLLMProviders); n > 0 {
		cfg.MaclawLLMProviders = append([]MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
	}
	if cfg.MaclawLLMProfiles != nil {
		profiles := *cfg.MaclawLLMProfiles
		cfg.MaclawLLMProfiles = &profiles
	}
}

func zhipuCodingMigrationProviderName(cfg *AppConfig) string {
	if cfg == nil {
		return ""
	}
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	if current != "" {
		for _, provider := range cfg.MaclawLLMProviders {
			if MaclawLLMProviderNameEqual(provider.Name, current) {
				return provider.Name
			}
		}
		return current
	}
	if len(cfg.MaclawLLMProviders) == 1 {
		return cfg.MaclawLLMProviders[0].Name
	}
	return ""
}

func migrateZhipuCodingProfiles(cfg *AppConfig) {
	if cfg == nil || cfg.MaclawLLMProfiles == nil {
		return
	}
	migrate := func(profile *MaclawLLMProfile) {
		if profile == nil {
			return
		}
		name := zhipuCodingProviderNameForID(cfg.MaclawLLMProviders, profile.ProviderID)
		if name == "" {
			return
		}
		profile.Model = MigrateZhipuCodingModel(name, profile.Model)
	}
	migrate(&cfg.MaclawLLMProfiles.Assistant)
	migrate(&cfg.MaclawLLMProfiles.Coding)
	if strings.TrimSpace(cfg.MaclawLLMProfiles.Caption.ProviderID) != "" || strings.TrimSpace(cfg.MaclawLLMProfiles.Caption.Model) != "" {
		migrate(&cfg.MaclawLLMProfiles.Caption)
	}
}

func zhipuCodingProviderNameForID(providers []MaclawLLMProvider, providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ""
	}
	for _, provider := range providers {
		id := strings.TrimSpace(provider.ID)
		if id == providerID || MaclawLLMProviderNameEqual(provider.Name, providerID) {
			return provider.Name
		}
		if MaclawLLMLegacyProviderID(provider.Name) == providerID {
			return provider.Name
		}
	}
	return ""
}

// IsAlwaysOnThinkingModel reports whether the selected model rejects
// thinking.type=disabled. GLM-5.3 always thinks; sending disabled fails.
func IsAlwaysOnThinkingModel(cfg MaclawLLMConfig) bool {
	model := strings.ToLower(strings.TrimSpace(cfg.Model))
	if i := strings.IndexByte(model, '['); i > 0 {
		model = strings.TrimSpace(model[:i])
	}
	return model == strings.ToLower(ZhipuCodingDefaultModel)
}

func IsDeepSeekFlashOpenAICompat(cfg MaclawLLMConfig) bool {
	text := strings.ToLower(strings.Join([]string{
		cfg.Model,
		cfg.ProviderName,
		cfg.URL,
	}, " "))
	return strings.Contains(text, "deepseek") && strings.Contains(text, "flash")
}

func IsGLMCodingPlanUserAgent(agent string) bool {
	normalized := strings.ToLower(strings.TrimSpace(agent))
	for _, supported := range []string{
		"claude code",
		"cline",
		"opencode",
		"codex",
		"roo code",
		"kilo code",
		"cursor",
		"crush",
		"goose",
	} {
		if normalized == supported {
			return true
		}
	}
	return false
}

func NormalizeGLMCodingPlanOpenAIBaseURL(rawURL, agent string) string {
	if !IsGLMCodingPlanUserAgent(agent) {
		return rawURL
	}
	text := strings.TrimSpace(rawURL)
	lower := strings.ToLower(text)
	const from = "open.bigmodel.cn/api/paas/v4"
	if !strings.Contains(lower, from) || strings.Contains(lower, "open.bigmodel.cn/api/coding/paas/v4") {
		return rawURL
	}
	idx := strings.Index(lower, from)
	if idx < 0 {
		return rawURL
	}
	return text[:idx] + "open.bigmodel.cn/api/coding/paas/v4" + text[idx+len(from):]
}

func IsGLMCodingPlanOpenAICompat(cfg MaclawLLMConfig) bool {
	if !IsGLMCodingPlanUserAgent(cfg.UserAgent()) {
		return false
	}
	return strings.Contains(strings.ToLower(NormalizeGLMCodingPlanOpenAIBaseURL(cfg.URL, cfg.UserAgent())), "open.bigmodel.cn/api/coding/paas/v4")
}

// MergeSystemIntoUser extracts system messages and prepends their content to
// the first user message. Returns a new slice; the original is not modified.
func MergeSystemIntoUser(messages []interface{}) []interface{} {
	var systemParts []string
	var rest []interface{}
	for _, m := range messages {
		role := ""
		content := ""
		switch mm := m.(type) {
		case map[string]interface{}:
			role, _ = mm["role"].(string)
			content, _ = mm["content"].(string)
		case map[string]string:
			role = mm["role"]
			content = mm["content"]
		}
		if role == "system" {
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		rest = append(rest, m)
	}
	if len(systemParts) == 0 {
		return messages // no system messages, return original slice as-is
	}
	prefix := strings.Join(systemParts, "\n")
	for i, m := range rest {
		switch mm := m.(type) {
		case map[string]interface{}:
			if r, _ := mm["role"].(string); r == "user" {
				c, _ := mm["content"].(string)
				patched := make(map[string]interface{}, len(mm))
				for k, v := range mm {
					patched[k] = v
				}
				patched["content"] = prefix + "\n\n" + c
				rest[i] = patched
				return rest
			}
		case map[string]string:
			if mm["role"] == "user" {
				patched := make(map[string]string, len(mm))
				for k, v := range mm {
					patched[k] = v
				}
				patched["content"] = prefix + "\n\n" + patched["content"]
				rest[i] = patched
				return rest
			}
		}
	}
	rest = append([]interface{}{map[string]interface{}{"role": "user", "content": prefix}}, rest...)
	return rest
}

// EffectiveContextTokens returns the usable context window in tokens.
// It uses the configured ContextLength, falling back to DefaultContextTokens.
// A safety margin of 20% is reserved for the model's output.
func (c MaclawLLMConfig) EffectiveContextTokens() int {
	limit := c.ContextLength
	if limit <= 0 {
		limit = DefaultContextTokens
	}
	return limit * 80 / 100 // reserve 20% for output
}

// TokenUsageStat 记录某个 LLM 服务商的累计 token 用量。
type TokenUsageStat struct {
	// Attribution is present only in LLMProfileTokenUsage. Provider-only legacy
	// records deliberately leave these fields blank rather than guessing.
	Profile                  string  `json:"profile,omitempty"`
	ProviderID               string  `json:"provider_id,omitempty"`
	ProviderDisplayName      string  `json:"provider_display_name,omitempty"`
	FinalModel               string  `json:"final_model,omitempty"`
	RouteSource              string  `json:"route_source,omitempty"`
	InputTokens              int64   `json:"input_tokens"`
	OutputTokens             int64   `json:"output_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	CachedInputTokens        int64   `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens         int64   `json:"cache_write_tokens,omitempty"`
	InputPricePerMTokensRMB  float64 `json:"input_price_per_m_tokens_rmb,omitempty"`
	OutputPricePerMTokensRMB float64 `json:"output_price_per_m_tokens_rmb,omitempty"`
	InputCostRMB             float64 `json:"input_cost_rmb,omitempty"`
	OutputCostRMB            float64 `json:"output_cost_rmb,omitempty"`
	TotalCostRMB             float64 `json:"total_cost_rmb,omitempty"`
	Requests                 int64   `json:"requests,omitempty"`
	CachedRequests           int64   `json:"cached_requests,omitempty"`
	LocalCacheRequests       int64   `json:"local_cache_requests,omitempty"`
	LocalCacheHits           int64   `json:"local_cache_hits,omitempty"`
}

// AdaptivePromptStat is a compact process-level adaptive system-prompt cost
// snapshot. Safe for machine.heartbeat: no user/tenant/session labels.
// Reported values are cumulative for the reporting process (GUI/TUI/…).
type AdaptivePromptStat struct {
	LightTurns      int64  `json:"light_turns"`
	FullTurns       int64  `json:"full_turns"`
	LightPercent    int    `json:"light_percent"`
	EstTokensSaved  int64  `json:"est_tokens_saved,omitempty"`
	LightToolDenies int64  `json:"light_tool_denies,omitempty"`
	LightUpgrades   int64  `json:"light_upgrades,omitempty"`
	AbEligibleLight int64  `json:"ab_eligible_light,omitempty"`
	AbSampleFull    int64  `json:"ab_sample_full,omitempty"`
	LastProfile     string `json:"last_profile,omitempty"`
	LastTask        string `json:"last_task,omitempty"`
	LastDeniedTool  string `json:"last_denied_tool,omitempty"`
	Summary         string `json:"summary,omitempty"`
}

// Empty reports whether no adaptive-prompt activity is present.
func (s AdaptivePromptStat) Empty() bool {
	return s.LightTurns == 0 && s.FullTurns == 0 && s.EstTokensSaved == 0 &&
		s.LightToolDenies == 0 && s.LightUpgrades == 0 &&
		s.AbEligibleLight == 0 && s.AbSampleFull == 0 &&
		strings.TrimSpace(s.LastProfile) == "" && strings.TrimSpace(s.Summary) == ""
}

// CostOpsStat is a compact process-level cost-route + local daily fleet snapshot
// for machine.heartbeat. No user/session labels.
type CostOpsStat struct {
	// Cost-route tier counters (shadow/on decisions).
	RouteDecisions int64  `json:"route_decisions,omitempty"`
	RouteApplied   int64  `json:"route_applied,omitempty"`
	RouteShadow    int64  `json:"route_shadow,omitempty"`
	LastTier       string `json:"last_tier,omitempty"`
	LastMode       string `json:"last_mode,omitempty"`
	RouteSummary   string `json:"route_summary,omitempty"`
	// Local durable daily fleet (this machine's llm_cost_daily.json sum).
	DailyCostUSD   float64 `json:"daily_cost_usd,omitempty"`
	DailyCalls     int     `json:"daily_calls,omitempty"`
	DailyInstances int     `json:"daily_instances,omitempty"`
	DailySummary   string  `json:"daily_summary,omitempty"`
	// Mode is MACLAW_COST_ROUTE effective mode at report time.
	CostRouteMode string `json:"cost_route_mode,omitempty"`
}

// Empty reports whether no cost-ops activity is present.
func (s CostOpsStat) Empty() bool {
	return s.RouteDecisions == 0 && s.RouteApplied == 0 && s.RouteShadow == 0 &&
		s.DailyCostUSD <= 0 && s.DailyCalls == 0 &&
		strings.TrimSpace(s.LastTier) == "" && strings.TrimSpace(s.RouteSummary) == "" &&
		strings.TrimSpace(s.DailySummary) == ""
}

// Add merges other counters into s (fleet rollup of process/machine snapshots).
func (s *CostOpsStat) Add(other CostOpsStat) {
	if s == nil {
		return
	}
	s.RouteDecisions += other.RouteDecisions
	s.RouteApplied += other.RouteApplied
	s.RouteShadow += other.RouteShadow
	s.DailyCostUSD += other.DailyCostUSD
	s.DailyCalls += other.DailyCalls
	s.DailyInstances += other.DailyInstances
	if t := strings.TrimSpace(other.LastTier); t != "" {
		s.LastTier = t
	}
	if m := strings.TrimSpace(other.LastMode); m != "" {
		s.LastMode = m
	}
	if m := strings.TrimSpace(other.CostRouteMode); m != "" {
		s.CostRouteMode = m
	}
	if sum := strings.TrimSpace(other.RouteSummary); sum != "" {
		s.RouteSummary = sum
	}
	if sum := strings.TrimSpace(other.DailySummary); sum != "" {
		s.DailySummary = sum
	}
}

// Add merges other counters into s (absolute fleet rollup of process snapshots).
func (s *AdaptivePromptStat) Add(other AdaptivePromptStat) {
	if s == nil {
		return
	}
	s.LightTurns += other.LightTurns
	s.FullTurns += other.FullTurns
	s.EstTokensSaved += other.EstTokensSaved
	s.LightToolDenies += other.LightToolDenies
	s.LightUpgrades += other.LightUpgrades
	s.AbEligibleLight += other.AbEligibleLight
	s.AbSampleFull += other.AbSampleFull
	if total := s.LightTurns + s.FullTurns; total > 0 {
		s.LightPercent = int((s.LightTurns * 100) / total)
	}
	// Prefer non-empty last_* from other when present (caller orders newest last).
	if p := strings.TrimSpace(other.LastProfile); p != "" {
		s.LastProfile = p
	}
	if t := strings.TrimSpace(other.LastTask); t != "" {
		s.LastTask = t
	}
	if d := strings.TrimSpace(other.LastDeniedTool); d != "" {
		s.LastDeniedTool = d
	}
	if sum := strings.TrimSpace(other.Summary); sum != "" {
		s.Summary = sum
	}
}

const (
	DefaultLLMInputPricePerMTokensRMB  = 1.0
	DefaultLLMOutputPricePerMTokensRMB = 2.0
)

func NormalizeLLMTokenPricePerMTokensRMB(value, fallback float64) float64 {
	if !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 {
		return value
	}
	if !math.IsNaN(fallback) && !math.IsInf(fallback, 0) && fallback >= 0 {
		return fallback
	}
	return 0
}

func CalculateLLMCostRMB(inputTokens, outputTokens int64, inputPricePerM, outputPricePerM float64) (float64, float64, float64) {
	inputPricePerM = NormalizeLLMTokenPricePerMTokensRMB(inputPricePerM, DefaultLLMInputPricePerMTokensRMB)
	outputPricePerM = NormalizeLLMTokenPricePerMTokensRMB(outputPricePerM, DefaultLLMOutputPricePerMTokensRMB)
	inputCost := float64(inputTokens) * inputPricePerM / 1_000_000
	outputCost := float64(outputTokens) * outputPricePerM / 1_000_000
	return inputCost, outputCost, inputCost + outputCost
}

// SkillHubEntry 描述一个 SkillHUB 注册端点。
type SkillHubEntry struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Type  string `json:"type,omitempty"` // "standard", "clawhub", "clawhub_mirror", ""(auto-detect)
}

// Skill 描述一个技能。
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "address" or "zip"
	Value       string `json:"value"`
	Installed   bool   `json:"installed"`
}

// ---------------------------------------------------------------------------
// Token estimation (CJK-aware) — single source of truth
// ---------------------------------------------------------------------------

// EstimateTextTokens estimates the token count of a text string using
// character-based heuristics: ~4 chars/token for ASCII, ~1.5 chars/token
// for CJK. This is the single source of truth for token estimation across
// the entire codebase. All packages MUST call this function instead of
// implementing their own estimation.
func EstimateTextTokens(text string) int {
	var asciiChars, cjkChars int
	for _, r := range text {
		if IsCJKRune(r) {
			cjkChars++
		} else {
			asciiChars++
		}
	}
	// Integer ceiling division: (n + d - 1) / d
	asciiTokens := (asciiChars + 3) / 4
	// For CJK: ceil(cjkChars / 1.5) = (cjkChars*2 + 2) / 3
	cjkTokens := (cjkChars*2 + 2) / 3
	return asciiTokens + cjkTokens
}

// IsCJKRune returns true if the rune is a CJK character.
// Covers: CJK Unified Ideographs, Extension A, Compatibility Ideographs,
// Radicals Supplement, Symbols/Punctuation, Hiragana, Katakana, Hangul.
func IsCJKRune(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF:
		return true
	case r >= 0x3400 && r <= 0x4DBF:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0x2E80 && r <= 0x2EFF:
		return true
	case r >= 0x3000 && r <= 0x303F:
		return true
	case r >= 0x3040 && r <= 0x309F:
		return true
	case r >= 0x30A0 && r <= 0x30FF:
		return true
	case r >= 0xAC00 && r <= 0xD7AF:
		return true
	default:
		return false
	}
}
