package v2

// types.go — Workflow domain types.
//
// This file contains domain-level types used across the workflow system:
// WorkflowType constants, StructuredIntent, tool policy maps and functions,
// OpsApprovedCommand, EngineState (GUI runtime state), TemplateSpec (template
// definitions for the engine adapter), and supporting types.
//
// The EngineState type (formerly EngineState) is the in-memory state used
// by the WorkflowEngine adapter that bridges V2 StateMachine to GUI consumers.
// It is distinct from v2.WorkflowState (state.go) which is the native V2
// serializable state used by StateMachine and Store.

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// WorkflowType
// ---------------------------------------------------------------------------

// WorkflowType identifies the kind of workflow template.
type WorkflowType string

const (
	WorkflowNone                    WorkflowType = "none"
	WorkflowCoding                  WorkflowType = "coding"
	WorkflowProductDesign           WorkflowType = "product_design"
	WorkflowInnovation              WorkflowType = "innovation"
	WorkflowBusinessPlan            WorkflowType = "business_plan"
	WorkflowTesting                 WorkflowType = "testing"
	WorkflowLiteratureReview        WorkflowType = "literature_review"
	WorkflowResearchReport          WorkflowType = "research_report"
	WorkflowExperimentDesign        WorkflowType = "experiment_design"
	WorkflowGrantProposal           WorkflowType = "grant_proposal"
	WorkflowPaperWriting            WorkflowType = "paper_writing"
	WorkflowProjectProposal         WorkflowType = "project_proposal"
	WorkflowEventPlanning           WorkflowType = "event_planning"
	WorkflowCompetitiveAnalysis     WorkflowType = "competitive_analysis"
	WorkflowPresentationDesign      WorkflowType = "presentation_design"
	WorkflowBidResponse             WorkflowType = "bid_response"
	WorkflowContractReview          WorkflowType = "contract_review"
	WorkflowDueDiligence            WorkflowType = "due_diligence"
	WorkflowComplianceAudit         WorkflowType = "compliance_audit"
	WorkflowPatentAnalysis          WorkflowType = "patent_analysis"
	WorkflowOpsMaintenance          WorkflowType = "ops_maintenance"
	WorkflowMaintenance             WorkflowType = "maintenance"
	WorkflowChangjiangScholar       WorkflowType = "changjiang_scholar"
	WorkflowChangjiangScholarReview WorkflowType = "changjiang_scholar_review"
	WorkflowNSFCDistinguishedYouth  WorkflowType = "nsfc_distinguished_youth"
	WorkflowNSFCExcellentYouth      WorkflowType = "nsfc_excellent_youth"
	WorkflowNSFCYouth               WorkflowType = "nsfc_youth"
	WorkflowNSFCGeneral             WorkflowType = "nsfc_general"
	WorkflowNSFCKey                 WorkflowType = "nsfc_key"
	WorkflowPaperReproduction       WorkflowType = "paper_reproduction"
	WorkflowPatentApplication       WorkflowType = "patent_application"
	WorkflowUSPatentApplication     WorkflowType = "us_patent_application"
)

// Phase IDs for the coding workflow.
const (
	PhaseCodingRequirements   = "requirements"
	PhaseCodingTechDesign     = "tech_design"
	PhaseCodingTaskBreakdown  = "task_breakdown"
	PhaseCodingImplementation = "implementation"
	PhaseCodingReview         = "review"
)

// ---------------------------------------------------------------------------
// ToolFilterPolicy — legacy alias for ToolPolicy
// ---------------------------------------------------------------------------

// ToolFilterPolicy is a legacy alias for ToolPolicy. Kept as a type alias so
// consumers that reference workflow.ToolFilterPolicy continue to compile
// after migrating to the v2 import path.
type ToolFilterPolicy = ToolPolicy

const (
	ToolFilterNone          ToolFilterPolicy = ToolPolicyNone
	ToolFilterDocOnly       ToolFilterPolicy = ToolPolicyDocOnly
	ToolFilterPlanning      ToolFilterPolicy = ToolPolicyPlanning
	ToolFilterFull          ToolFilterPolicy = ToolPolicyFull
	ToolFilterOpsControlled ToolFilterPolicy = ToolPolicyOpsControlled
)

// ---------------------------------------------------------------------------
// StructuredIntent
// ---------------------------------------------------------------------------

// StructuredIntent is the output of the intent understanding phase.
type StructuredIntent struct {
	Category      WorkflowType `json:"category"`
	Summary       string       `json:"summary"`
	Goals         []string     `json:"goals"`
	Constraints   []string     `json:"constraints"`
	OpenQuestions []string     `json:"open_questions"`
	Confidence    float64      `json:"confidence"`
	Ready         bool         `json:"ready"`
}

// ---------------------------------------------------------------------------
// PhaseKind / MutationScope
// ---------------------------------------------------------------------------

// PhaseKind is the normalized semantic role of a workflow phase.
type PhaseKind string

const (
	PhaseKindUnknown            PhaseKind = ""
	PhaseKindIntake             PhaseKind = "intake"
	PhaseKindDocumentPlanning   PhaseKind = "document_planning"
	PhaseKindCodePlanning       PhaseKind = "code_planning"
	PhaseKindArtifactGeneration PhaseKind = "artifact_generation"
	PhaseKindExecution          PhaseKind = "execution"
	PhaseKindOpsRiskPolicy      PhaseKind = "ops_risk_policy"
	PhaseKindOpsExecution       PhaseKind = "ops_execution"
	PhaseKindReview             PhaseKind = "review"
)

// MutationScope describes what state a phase may mutate.
type MutationScope string

const (
	MutationScopeUnknown     MutationScope = ""
	MutationScopeNone        MutationScope = "none"
	MutationScopeWorkflowDoc MutationScope = "workflow_doc"
	MutationScopeArtifact    MutationScope = "artifact"
	MutationScopeProject     MutationScope = "project"
	MutationScopeOps         MutationScope = "ops"
)

// ---------------------------------------------------------------------------
// Tool policy allowed-tools maps
// ---------------------------------------------------------------------------

// DocOnlyAllowedTools is the canonical set of tool names permitted during
// doc_only workflow phases.
var DocOnlyAllowedTools = map[string]bool{
	"read_file":                true,
	"memory":                   true,
	"generate_pdf":             true,
	"office":                   true,
	"send_file":                true,
	"web_search":               true,
	"web_fetch":                true,
	"open":                     true,
	"set_nickname":             true,
	"list_directory":           true,
	"bash":                     true,
	"manage_skill":             true,
	"get_skill_run":            true,
	"list_skills":              true,
	"search_skill_hub":         true,
	"install_skill_hub":        true,
	"search_and_install_skill": true,
}

// PlanningAllowedTools is the canonical set for reviewable coding-planning phases.
var PlanningAllowedTools = map[string]bool{
	"read_file":                true,
	"list_directory":           true,
	"memory":                   true,
	"send_file":                true,
	"web_search":               true,
	"web_fetch":                true,
	"open":                     true,
	"set_nickname":             true,
	"manage_skill":             true,
	"get_skill_run":            true,
	"list_skills":              true,
	"search_skill_hub":         true,
	"install_skill_hub":        true,
	"search_and_install_skill": true,
}

// OpsControlledAllowedTools is the canonical tool set for controlled server
// operations phases.
var OpsControlledAllowedTools = map[string]bool{
	"bash":           true,
	"ssh":            true,
	"read_file":      true,
	"list_directory": true,
	"memory":         true,
	"async_wait":     true,
	"send_file":      true,
	"web_search":     true,
	"web_fetch":      true,
	"set_nickname":   true,
}

// ---------------------------------------------------------------------------
// Tool policy functions
// ---------------------------------------------------------------------------

// IsToolAllowedByPolicy returns true if the named tool is permitted by the
// given policy.
func IsToolAllowedByPolicy(policy ToolFilterPolicy, name string) bool {
	name = strings.TrimSpace(name)
	switch policy {
	case ToolPolicyDocOnly:
		return DocOnlyAllowedTools[name]
	case ToolPolicyPlanning:
		return PlanningAllowedTools[name]
	case ToolPolicyOpsControlled:
		return OpsControlledAllowedTools[name]
	default:
		return true
	}
}

// FilterToolDefinitions filters a list of tool definitions by policy.
func FilterToolDefinitions(policy ToolFilterPolicy, tools []map[string]interface{}) []map[string]interface{} {
	if policy == ToolPolicyNone || policy == ToolPolicyFull || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		nameVal := toolDefinitionName(def)
		if IsToolAllowedByPolicy(policy, nameVal) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// ValidateToolCallByPolicy returns an error if the tool call is not allowed.
func ValidateToolCallByPolicy(policy ToolFilterPolicy, name string, args map[string]interface{}) error {
	return ValidateToolCallByPolicyWithApproval(policy, name, args, nil)
}

// ValidateToolCallByPolicyWithApproval validates a tool call against the policy
// with optional ops-approved commands for OpsControlled phases.
func ValidateToolCallByPolicyWithApproval(policy ToolFilterPolicy, name string, args map[string]interface{}, approved []OpsApprovedCommand) error {
	name = strings.TrimSpace(name)
	if !IsToolAllowedByPolicy(policy, name) {
		return fmt.Errorf("%s is not allowed in current workflow phase", name)
	}
	if policy != ToolPolicyOpsControlled || (name != "bash" && name != "ssh") {
		return nil
	}
	desc := opsCommandDescriptor(name, args)
	if isHighRiskOpsCommand(desc) {
		return fmt.Errorf("%s requires a reviewed runbook before execution", name)
	}
	if !isMutatingOpsCommand(name, args, desc) {
		return nil
	}
	if len(approved) == 0 {
		return fmt.Errorf("%s requires allowed_commands in the approved risk-policy before execution", name)
	}
	for _, item := range approved {
		if opsApprovedCommandMatches(item, name, args, desc) {
			return nil
		}
	}
	return fmt.Errorf("%s command is outside the approved risk-policy allowed_commands manifest", name)
}

// RequiredToolNamesForPolicy returns the minimum tools a policy phase expects.
func RequiredToolNamesForPolicy(policy ToolFilterPolicy) []string {
	switch policy {
	case ToolPolicyFull:
		return []string{"bash", "read_file", "list_directory", "write_file", "edit_file"}
	case ToolPolicyDocOnly:
		return []string{"read_file", "list_directory", "send_file"}
	case ToolPolicyPlanning:
		return []string{"read_file", "list_directory", "send_file"}
	case ToolPolicyOpsControlled:
		return []string{"bash", "ssh", "read_file", "list_directory"}
	default:
		return nil
	}
}

// toolDefinitionName extracts the tool name from a tool definition map.
func toolDefinitionName(def map[string]interface{}) string {
	if def == nil {
		return ""
	}
	if name, _ := def["name"].(string); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	if fn, _ := def["function"].(map[string]interface{}); fn != nil {
		name, _ := fn["name"].(string)
		return strings.TrimSpace(name)
	}
	return ""
}

// ---------------------------------------------------------------------------
// OpsApprovedCommand + helpers
// ---------------------------------------------------------------------------

// OpsApprovedCommand is an entry in a risk-policy approved commands manifest.
type OpsApprovedCommand struct {
	Tool                string                 `json:"tool"`
	Action              string                 `json:"action,omitempty"`
	Target              string                 `json:"target,omitempty"`
	Command             string                 `json:"command,omitempty"`
	Args                map[string]interface{} `json:"args,omitempty"`
	RiskLevel           OpsRiskLevel           `json:"risk_level,omitempty"`
	ApprovalRequirement OpsApprovalRequirement `json:"approval_requirement,omitempty"`
}

func opsCommandDescriptor(name string, args map[string]interface{}) string {
	if args == nil {
		return name
	}
	switch name {
	case "bash":
		cmd, _ := args["command"].(string)
		return "bash: " + cmd
	case "ssh":
		action, _ := args["action"].(string)
		cmd, _ := args["command"].(string)
		return "ssh(" + action + "): " + cmd
	default:
		return name
	}
}

func isHighRiskOpsCommand(desc string) bool {
	lower := strings.ToLower(desc)
	for _, kw := range []string{"rm -rf /", "mkfs", "dd if=", "format c:", "> /dev/sd"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isMutatingOpsCommand(name string, args map[string]interface{}, desc string) bool {
	if name == "bash" || (name == "ssh" && args != nil) {
		action, _ := args["action"].(string)
		if action == "exec" || action == "submit_task" || name == "bash" {
			return true
		}
	}
	return false
}

func opsApprovedCommandMatches(item OpsApprovedCommand, name string, args map[string]interface{}, desc string) bool {
	if item.Tool != "" && item.Tool != name {
		return false
	}
	if item.Command != "" && !strings.Contains(desc, item.Command) {
		return false
	}
	if item.Action != "" && args != nil {
		action, _ := args["action"].(string)
		if action != item.Action {
			return false
		}
	}
	if item.Target != "" && args != nil {
		target, _ := args["target"].(string)
		host, _ := args["host"].(string)
		sessionID, _ := args["session_id"].(string)
		if target != item.Target && host != item.Target && sessionID != item.Target {
			return false
		}
	}
	return true
}

// ExtractOpsApprovedCommands parses a risk-policy text block into approved commands.
func ExtractOpsApprovedCommands(text string) []OpsApprovedCommand {
	var out []OpsApprovedCommand
	var current *OpsApprovedCommand
	flush := func() {
		if current != nil && (current.Tool != "" || current.Command != "") {
			out = append(out, *current)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			current = &OpsApprovedCommand{}
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		}
		if current == nil || !strings.Contains(trimmed, ":") {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch key {
		case "tool":
			current.Tool = val
		case "action":
			current.Action = val
		case "target":
			current.Target = val
		case "command":
			current.Command = val
		}
	}
	flush()
	return out
}

// ---------------------------------------------------------------------------
// FilterResult / UnderstandingState / ReviewIntent
// ---------------------------------------------------------------------------

// FilterResult classifies an incoming user message.
type FilterResult string

const (
	FilterSimpleDirective     FilterResult = "simple_directive"
	FilterActiveWorkflow      FilterResult = "active_workflow"
	FilterActiveUnderstanding FilterResult = "active_understanding"
	FilterNeedsUnderstanding  FilterResult = "needs_understanding"
)

// UnderstandingState tracks the lifecycle of an intent understanding session.
type UnderstandingState string

const (
	UnderstandingActive    UnderstandingState = "active"
	UnderstandingConfirmed UnderstandingState = "confirmed"
	UnderstandingCancelled UnderstandingState = "cancelled"
	UnderstandingExpired   UnderstandingState = "expired"
)

// ReviewIntent is the classified user intent while a NeedsConfirm phase is
// waiting for user review.
type ReviewIntent string

const (
	ReviewIntentConfirm    ReviewIntent = "confirm"
	ReviewIntentSupplement ReviewIntent = "supplement"
	ReviewIntentSkip       ReviewIntent = "skip"
	ReviewIntentCancel     ReviewIntent = "cancel"
	ReviewIntentSwitchTask ReviewIntent = "switch_task"
	ReviewIntentOther      ReviewIntent = "other"
)

// ---------------------------------------------------------------------------
// InputRequirement / WorkflowInputPayload / WorkflowInputAttachment
// ---------------------------------------------------------------------------

// InputRequirement describes a document/file that the user must provide
// before a workflow can begin its first phase.
type InputRequirement struct {
	Description  string   `json:"description"`
	FileTypes    []string `json:"file_types,omitempty"`
	AcceptText   bool     `json:"accept_text,omitempty"`
	AnalysisHint string   `json:"analysis_hint,omitempty"`
}

// Clone returns a deep copy of the InputRequirement.
func (r *InputRequirement) Clone() *InputRequirement {
	if r == nil {
		return nil
	}
	cp := *r
	cp.FileTypes = append([]string(nil), r.FileTypes...)
	return &cp
}

// WorkflowInputAttachment records a user-provided file/image/audio attachment.
type WorkflowInputAttachment struct {
	Type     string `json:"type,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// WorkflowInputPayload is the durable input contract for document-driven workflows.
type WorkflowInputPayload struct {
	Text        string                    `json:"text,omitempty"`
	Attachments []WorkflowInputAttachment `json:"attachments,omitempty"`
	ReceivedAt  time.Time                 `json:"received_at,omitempty"`
}

// Clone returns a deep copy of the WorkflowInputPayload.
func (p *WorkflowInputPayload) Clone() *WorkflowInputPayload {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Attachments = append([]WorkflowInputAttachment(nil), p.Attachments...)
	return &cp
}

// ---------------------------------------------------------------------------
// UnderstandingSession / UnderstandingRound
// ---------------------------------------------------------------------------

// UnderstandingSession holds the state of a multi-round intent clarification.
type UnderstandingSession struct {
	ID        string               `json:"id"`
	UserID    string               `json:"user_id"`
	Intent    StructuredIntent     `json:"intent"`
	Rounds    []UnderstandingRound `json:"rounds"`
	State     UnderstandingState   `json:"state"`
	CreatedAt time.Time            `json:"created_at"`
	UpdatedAt time.Time            `json:"updated_at"`
}

// UnderstandingRound records one exchange in an intent understanding session.
type UnderstandingRound struct {
	UserText      string    `json:"user_text"`
	AssistantText string    `json:"assistant_text"`
	Timestamp     time.Time `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// QualityGateResult / GateCheckItem
// ---------------------------------------------------------------------------

// QualityGateResult records the outcome of a phase quality gate check.
type QualityGateResult struct {
	PhaseID   string          `json:"phase_id"`
	Passed    bool            `json:"passed"`
	Items     []GateCheckItem `json:"items"`
	CheckedAt time.Time       `json:"checked_at"`
}

// GateCheckItem is a single item in a quality gate checklist.
type GateCheckItem struct {
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	Note        string `json:"note,omitempty"`
}

// ---------------------------------------------------------------------------
// EngineCallbacks / LLMCaller / ArtifactSaver / PersistenceStore
// ---------------------------------------------------------------------------

// EngineCallbacks defines the adapter interface that GUI/TUI must implement.
type EngineCallbacks interface {
	SendTextToUser(userID, text string) error
	EmitPhaseUpdate(userID string, state *EngineState) error
	EmitDocUpdate(userID, phaseID, content string) error
	EmitGateResult(userID, phaseID string, result *QualityGateResult) error
	GetLang() string
}

// LLMCaller abstracts LLM invocation for testability.
type LLMCaller interface {
	DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error)
}

// ArtifactSaver abstracts saving workflow phase outputs to long-term memory.
type ArtifactSaver interface {
	SaveArtifact(title, content string, tags []string, sourceURL string) error
}

// FullArtifactSaver extends ArtifactSaver with full content storage.
type FullArtifactSaver interface {
	ArtifactSaver
	SaveArtifactFull(title, summary, fullContent string, tags []string, sourceURL string) error
}

// PersistenceStore abstracts workflow state persistence.
type PersistenceStore interface {
	SaveUnderstandingSession(session *UnderstandingSession) error
	LoadUnderstandingSession(userID string) (*UnderstandingSession, error)
	DeleteUnderstandingSession(userID string) error
	SaveWorkflowState(state *EngineState) error
	LoadWorkflowState(userID string) (*EngineState, error)
	DeleteWorkflowState(id string) error
	ListActiveWorkflows() ([]*EngineState, error)
	CleanupExpired(olderThan time.Duration) error
}

// NullPersistenceStore is a no-op PersistenceStore implementation.
type NullPersistenceStore struct{}

var _ PersistenceStore = (*NullPersistenceStore)(nil)

func (NullPersistenceStore) SaveUnderstandingSession(_ *UnderstandingSession) error           { return nil }
func (NullPersistenceStore) LoadUnderstandingSession(_ string) (*UnderstandingSession, error) { return nil, nil }
func (NullPersistenceStore) DeleteUnderstandingSession(_ string) error                        { return nil }
func (NullPersistenceStore) SaveWorkflowState(_ *EngineState) error                      { return nil }
func (NullPersistenceStore) LoadWorkflowState(_ string) (*EngineState, error)             { return nil, nil }
func (NullPersistenceStore) DeleteWorkflowState(_ string) error                               { return nil }
func (NullPersistenceStore) ListActiveWorkflows() ([]*EngineState, error)                 { return nil, nil }
func (NullPersistenceStore) CleanupExpired(_ time.Duration) error                             { return nil }

// ---------------------------------------------------------------------------
// EngineState — GUI runtime state (distinct from WorkflowState in state.go)
// ---------------------------------------------------------------------------

// EngineState holds the runtime state used by the WorkflowEngine adapter layer
// and its consumers. Named EngineState to avoid collision with the V2
// WorkflowState in the same package.
type EngineState struct {
	ID                             string                        `json:"id"`
	UserID                         string                        `json:"user_id"`
	Type                           WorkflowType                  `json:"type"`
	Intent                         StructuredIntent              `json:"intent"`
	CurrentPhase                   string                        `json:"current_phase"`
	PhaseIndex                     int                           `json:"phase_index"`
	PhaseOutputs                   map[string]string             `json:"phase_outputs"`
	GateResults                    map[string]*QualityGateResult `json:"gate_results"`
	Status                         WorkflowStatus                `json:"status"`
	CreatedAt                      time.Time                     `json:"created_at"`
	UpdatedAt                      time.Time                     `json:"updated_at"`
	PhaseFormData                  map[string]interface{}        `json:"phase_form_data,omitempty"`
	PhaseFormSubmitted             bool                          `json:"phase_form_submitted,omitempty"`
	PhaseFormSkipped               bool                          `json:"phase_form_skipped,omitempty"`
	InputReceived                  bool                          `json:"input_received,omitempty"`
	InputPayload                   *WorkflowInputPayload         `json:"input_payload,omitempty"`
	ProjectPath                    string                        `json:"project_path,omitempty"`
	PendingReviewPhaseID           string                        `json:"pending_review_phase_id,omitempty"`
	PendingReviewRevisionRequested bool                          `json:"pending_review_revision_requested,omitempty"`
}

// IsWaitingForInput returns true if the workflow is waiting for user input.
func (ws *EngineState) IsWaitingForInput(tmpl *TemplateSpec) bool {
	if ws == nil || tmpl == nil {
		return false
	}
	return tmpl.NeedsInputDocument() && !ws.InputReceived && ws.PhaseIndex == 0
}

// ---------------------------------------------------------------------------
// TemplateSpec / PhaseSpec — extended template types for WorkflowEngine
// ---------------------------------------------------------------------------

// PhaseSpec defines a single phase within a TemplateSpec.
type PhaseSpec struct {
	ID                  string           `json:"id"`
	Name                string           `json:"name"`
	Description         string           `json:"description"`
	Prompt              string           `json:"prompt"`
	Deliverable         string           `json:"deliverable"`
	Checklist           []string         `json:"checklist"`
	NeedsConfirm        bool             `json:"needs_confirm"`
	CanSkip             bool             `json:"can_skip"`
	ToolPolicy          ToolFilterPolicy `json:"tool_policy"`
	Kind                PhaseKind        `json:"kind,omitempty"`
	MutationScope       MutationScope    `json:"mutation_scope,omitempty"`
	InputSchema         *PhaseInputSchemaSpec `json:"input_schema,omitempty"`
	DisableOrchestrator bool             `json:"disable_orchestrator,omitempty"`
}

// PhaseInputSchemaSpec declares a structured form for a phase.
type PhaseInputSchemaSpec struct {
	Title           string                `json:"title"`
	Description     string                `json:"description,omitempty"`
	TitleI18N       map[string]string     `json:"title_i18n,omitempty"`
	DescriptionI18N map[string]string     `json:"description_i18n,omitempty"`
	Fields          []PhaseInputFieldSpec   `json:"fields"`
}

// Clone returns a deep copy of the PhaseInputSchemaSpec.
func (s *PhaseInputSchemaSpec) Clone() *PhaseInputSchemaSpec {
	if s == nil {
		return nil
	}
	cp := *s
	cp.TitleI18N = cloneStringMap(s.TitleI18N)
	cp.DescriptionI18N = cloneStringMap(s.DescriptionI18N)
	cp.Fields = make([]PhaseInputFieldSpec, len(s.Fields))
	copy(cp.Fields, s.Fields)
	for i := range cp.Fields {
		cp.Fields[i].LabelI18N = cloneStringMap(s.Fields[i].LabelI18N)
		cp.Fields[i].DescriptionI18N = cloneStringMap(s.Fields[i].DescriptionI18N)
		cp.Fields[i].PlaceholderI18N = cloneStringMap(s.Fields[i].PlaceholderI18N)
		cp.Fields[i].Options = append([]PhaseInputOptionSpec(nil), s.Fields[i].Options...)
	}
	return &cp
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	cp := make(map[string]string, len(src))
	for k, v := range src {
		cp[k] = v
	}
	return cp
}

// PhaseInputFieldSpec defines a single form field.
type PhaseInputFieldSpec struct {
	Name            string               `json:"name"`
	Label           string               `json:"label"`
	Type            string               `json:"type"`
	Required        bool                 `json:"required,omitempty"`
	Description     string               `json:"description,omitempty"`
	Placeholder     string               `json:"placeholder,omitempty"`
	LabelI18N       map[string]string    `json:"label_i18n,omitempty"`
	DescriptionI18N map[string]string    `json:"description_i18n,omitempty"`
	PlaceholderI18N map[string]string    `json:"placeholder_i18n,omitempty"`
	Options         []PhaseInputOptionSpec `json:"options,omitempty"`
	Default         interface{}          `json:"default,omitempty"`
	Min             *float64             `json:"min,omitempty"`
	Max             *float64             `json:"max,omitempty"`
	MinLength       *int                 `json:"min_length,omitempty"`
	MaxLength       *int                 `json:"max_length,omitempty"`
	Pattern         string               `json:"pattern,omitempty"`
}

// PhaseInputOptionSpec defines a selectable option.
type PhaseInputOptionSpec struct {
	Label     string            `json:"label"`
	Value     string            `json:"value"`
	LabelI18N map[string]string `json:"label_i18n,omitempty"`
}

// TemplateSpec defines a complete workflow template with ordered phases.
type TemplateSpec struct {
	Type          WorkflowType      `json:"type"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Keywords      []string          `json:"keywords"`
	Phases        []PhaseSpec `json:"phases"`
	RequiresInput *InputRequirement `json:"requires_input,omitempty"`
}

// NeedsInputDocument returns true if the template requires user input.
func (t *TemplateSpec) NeedsInputDocument() bool {
	return t != nil && t.RequiresInput != nil && t.RequiresInput.Description != ""
}

// ---------------------------------------------------------------------------
// WorkflowResponse
// ---------------------------------------------------------------------------

// WorkflowResponse is the engine's response to a user input during a workflow.
type WorkflowResponse struct {
	Text                 string             `json:"text,omitempty"`
	PhasePrompt          string             `json:"phase_prompt,omitempty"`
	ToolFilter           ToolFilterPolicy   `json:"tool_filter,omitempty"`
	RunAgentLoop         bool               `json:"run_agent_loop,omitempty"`
	Advance              bool               `json:"advance,omitempty"`
	Complete             bool               `json:"complete,omitempty"`
	DocContent           string             `json:"doc_content,omitempty"`
	GateResult           *QualityGateResult `json:"gate_result,omitempty"`
	ShowForm             bool               `json:"show_form,omitempty"`
	FormSchema           *PhaseInputSchemaSpec `json:"form_schema,omitempty"`
	DefaultInput         bool               `json:"default_input,omitempty"`
	PendingReview        bool               `json:"pending_review,omitempty"`
	PendingConfirm       bool               `json:"pending_confirm,omitempty"`
	ActivateOrchestrator bool               `json:"activate_orchestrator,omitempty"`
	TaskBreakdownText    string             `json:"task_breakdown_text,omitempty"`
	RequirementsContext  string             `json:"requirements_context,omitempty"`
	DesignContext        string             `json:"design_context,omitempty"`
}

// ---------------------------------------------------------------------------
// Ops types
// ---------------------------------------------------------------------------

// OpsRiskDecision is the LLM's high-level decision about an ops plan.
type OpsRiskDecision string

const (
	OpsRiskDecisionUnknown          OpsRiskDecision = ""
	OpsRiskDecisionApprove          OpsRiskDecision = "approve"
	OpsRiskDecisionEscalate         OpsRiskDecision = "escalate"
	OpsRiskDecisionReject           OpsRiskDecision = "reject"
	OpsRiskDecisionDocumentOnly     OpsRiskDecision = "document_only"
	OpsRiskDecisionPropose          OpsRiskDecision = "propose"
	OpsRiskDecisionApprovalRequired OpsRiskDecision = "approval_required"
	OpsRiskDecisionAutoExecute      OpsRiskDecision = "auto_execute"
	OpsRiskDecisionDeny             OpsRiskDecision = "deny"
)

// OpsRiskLevel is the assessed risk level of an ops plan.
type OpsRiskLevel string

const (
	OpsRiskLevelUnknown  OpsRiskLevel = ""
	OpsRiskLevelLow      OpsRiskLevel = "low"
	OpsRiskLevelMedium   OpsRiskLevel = "medium"
	OpsRiskLevelHigh     OpsRiskLevel = "high"
	OpsRiskLevelCritical OpsRiskLevel = "critical"
	OpsRiskLevelL0       OpsRiskLevel = "L0"
	OpsRiskLevelL1       OpsRiskLevel = "L1"
	OpsRiskLevelL2       OpsRiskLevel = "L2"
	OpsRiskLevelL3       OpsRiskLevel = "L3"
	OpsRiskLevelL4       OpsRiskLevel = "L4"
)

// OpsApprovalRequirement classifies what approval is needed.
type OpsApprovalRequirement string

const (
	OpsApprovalRequirementUnknown OpsApprovalRequirement = ""
	OpsApprovalRequirementNone    OpsApprovalRequirement = "none"
	OpsApprovalRequirementUser    OpsApprovalRequirement = "user"
	OpsApprovalRequirementAdmin   OpsApprovalRequirement = "admin"
	OpsApprovalRequirementSingle  OpsApprovalRequirement = "single"
	OpsApprovalRequirementDouble  OpsApprovalRequirement = "double"
)

// Stub functions for ops assessment — only ExtractOpsRiskDecision,
// ExtractOpsApprovalRequirement, and OpsApprovalDigest have production consumers.
func ExtractOpsRiskDecision(_ string) OpsRiskDecision             { return OpsRiskDecisionUnknown }
func ExtractOpsApprovalRequirement(_ string) OpsApprovalRequirement {
	return OpsApprovalRequirementUnknown
}
func OpsApprovalDigest(_ string) string { return "" }
