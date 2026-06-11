package v2

import (
	"regexp"
	"strings"
)

// RouteTarget indicates where a message should be handled.
type RouteTarget string

const (
	RouteToAgentLoop    RouteTarget = "agent_loop"
	RouteToWorkflow     RouteTarget = "workflow"
	RouteToDirectCoding RouteTarget = "direct_coding" // Skip SDD, go straight to SubAgent
)

// RouteResult is returned by WorkflowRouter.Route.
type RouteResult struct {
	Target       RouteTarget
	WorkflowType string        // set when creating a new workflow
	ProjectPath  string        // extracted from user text
	HandleResult *HandleResult // set when an active workflow handled the message
}

// Attachment represents a message attachment (image, file, etc.)
type Attachment struct {
	Type string
	Name string
}

// LLMConfirmFunc is an optional function to confirm intent via LLM.
// Returns true if the message should trigger a workflow.
type LLMConfirmFunc func(text, workflowType string) bool

// TaskComplexity represents the assessed complexity of a coding task.
type TaskComplexity string

const (
	ComplexitySimple  TaskComplexity = "simple"  // Direct SubAgent execution, no SDD
	ComplexityComplex TaskComplexity = "complex" // Full SDD workflow (requirements → design → tasks → code)
)

// ComplexityFunc assesses whether a coding task is simple (direct coding) or complex (needs SDD).
// Returns ComplexitySimple for quick tasks (bug fix, hello world, add a button).
// Returns ComplexityComplex for projects needing design/planning.
// When nil, falls back to heuristic (message length + project path).
type ComplexityFunc func(text string) TaskComplexity

// WorkflowRouter is the single decision point for message routing.
// It replaces QuickFilter + UIC + IUM + GateIntentClassifier + SteeringDetector.
type WorkflowRouter struct {
	machine        *StateMachine
	templates      *TemplateRegistry
	llmFunc        LLMConfirmFunc // optional; nil = keyword-only
	complexityFunc ComplexityFunc // optional; nil = heuristic fallback
}

func NewWorkflowRouter(machine *StateMachine, templates *TemplateRegistry, llmFunc LLMConfirmFunc) *WorkflowRouter {
	return &WorkflowRouter{
		machine:   machine,
		templates: templates,
		llmFunc:   llmFunc,
	}
}

// SetComplexityFunc sets the LLM-based complexity assessor.
func (r *WorkflowRouter) SetComplexityFunc(fn ComplexityFunc) {
	r.complexityFunc = fn
}

// Route decides whether a message should go to a workflow or the normal agent loop.
func (r *WorkflowRouter) Route(userID, text string, attachments []Attachment) *RouteResult {
	// Step 1: Active workflow takes priority
	if state := r.machine.GetActive(userID); state != nil {
		result, err := r.machine.HandleInput(userID, text)
		if err != nil {
			return &RouteResult{Target: RouteToAgentLoop}
		}
		if result.Action == ActionPassThrough {
			// PassThrough means the message is unrelated to the active workflow
			// (e.g. workflow is in PhaseExecuting, or confirm classifier said "unrelated").
			// Fall through to keyword matching below — the user may be starting a NEW workflow.
			// If no keyword match, the message goes to the normal agent loop.
		} else if result.Action == ActionRunPhase {
			// Phase is pending/running. Check if the user's message looks like a
			// completely new workflow task (not a continuation of the current one).
			// If so, fall through to keyword matching which will start a new workflow.
			// startNewWorkflowV2 handles cancelling the old workflow before creating the new one.
			if r.looksLikeNewWorkflowTask(text) {
				// Fall through to keyword matching → startNewWorkflowV2 will cancel old + create new
			} else {
				return &RouteResult{Target: RouteToWorkflow, HandleResult: result}
			}
		} else {
			return &RouteResult{Target: RouteToWorkflow, HandleResult: result}
		}
	}

	// Step 2: Attachment-heavy messages with short text → agent loop
	if len(attachments) > 0 && len([]rune(text)) < 50 {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 3: Skip signals → agent loop
	if hasSkipSignal(text) {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 4: Bug-fix / maintenance tasks → agent loop (no three-phase needed)
	if isBugFixOnly(text) {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 5: Keyword match against templates
	matched := r.templates.MatchByKeywords(text)
	if matched == nil {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 6: Optional LLM confirmation (failure → use keyword result)
	if r.llmFunc != nil {
		if !r.llmFunc(text, matched.Type) {
			return &RouteResult{Target: RouteToAgentLoop}
		}
	}

	// Step 7: For coding tasks, assess complexity to decide SDD vs direct coding.
	// Simple/medium tasks go directly to SubAgent without the full workflow.
	if matched.Type == "coding" {
		complexity := r.assessComplexity(text)
		if complexity == ComplexitySimple {
			projectPath := ExtractProjectPathFromText(text)
			return &RouteResult{
				Target:       RouteToDirectCoding,
				WorkflowType: "coding",
				ProjectPath:  projectPath,
			}
		}
	}

	// Step 8: Extract project path from text
	projectPath := ExtractProjectPathFromText(text)

	return &RouteResult{
		Target:       RouteToWorkflow,
		WorkflowType: matched.Type,
		ProjectPath:  projectPath,
	}
}

// assessComplexity determines whether a coding task is simple (direct SubAgent)
// or complex (needs full SDD workflow). Uses LLM semantic judgment when available.
// Without LLM, defaults to complex (full SDD) — the safe choice that preserves
// the three-phase design process. Direct coding only triggers with LLM confirmation.
func (r *WorkflowRouter) assessComplexity(text string) TaskComplexity {
	// Use LLM-based complexity assessment when available
	if r.complexityFunc != nil {
		return r.complexityFunc(text)
	}
	// Without LLM: always default to complex (full SDD).
	// Bug-fix tasks are already handled by isBugFixOnly() in Step 4.
	// Users can say "直接做" (skip signal) to bypass SDD entirely.
	return ComplexityComplex
}

// --- Skip Signals ---

var skipSignals = []string{
	"直接做", "不用问了", "按你的想法来", "跳过文档", "不需要文档",
	"直接开始", "不用三阶段", "skip workflow", "直接编码",
	"不要文档", "别废话",
}

func hasSkipSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, sig := range skipSignals {
		if strings.Contains(lower, sig) {
			return true
		}
	}
	return false
}

// --- Bug-fix detection ---

var bugFixKeywords = []string{
	"修bug", "修复", "调试", "排查", "报错", "崩溃",
	"白屏", "闪退", "卡住", "不显示", "不生效", "fix bug",
	"debug", "修改bug", "解决bug",
}

var creationKeywords = []string{
	"开发", "游戏", "应用", "工具", "系统", "前端", "后端",
	"写代码", "编写", "实现", "创建",
}

func isBugFixOnly(text string) bool {
	lower := strings.ToLower(text)
	hasBugFix := false
	for _, kw := range bugFixKeywords {
		if strings.Contains(lower, kw) {
			hasBugFix = true
			break
		}
	}
	if !hasBugFix {
		return false
	}
	for _, kw := range creationKeywords {
		if strings.Contains(lower, kw) {
			return false // has creation intent too — not bug-fix-only
		}
	}
	return true
}

// looksLikeNewWorkflowTask checks whether a user message is a completely new
// workflow request unrelated to the current running phase. Uses the optional
// LLM confirmation function for semantic judgment when available, falling back
// to a conservative heuristic (template keyword match + minimum length) only
// when no LLM is configured.
//
// The LLM path asks: "The user already has an active coding workflow in progress.
// Is this message a brand new, independent project request?" — this avoids
// false positives from sub-task descriptions within the current project.
func (r *WorkflowRouter) looksLikeNewWorkflowTask(text string) bool {
	// Short messages (< 15 runes) are never new tasks — they are continuations
	// like "继续", "开工", "确认", "好的" etc.
	if len([]rune(text)) < 15 {
		return false
	}
	// Must match at least one template to even be a candidate
	matched := r.templates.MatchByKeywords(text)
	if matched == nil {
		return false
	}
	// Use LLM semantic judgment when available
	if r.llmFunc != nil {
		return r.llmFunc(text, matched.Type)
	}
	// No LLM available — conservative fallback: only trigger if message is
	// long enough and contains an explicit project path (strong new-task signal)
	if len([]rune(text)) >= 20 && ExtractProjectPathFromText(text) != "" {
		return true
	}
	// Without LLM, be conservative: don't cancel the running workflow
	return false
}

// --- Project Path Extraction ---

var projectPathPatterns = []*regexp.Regexp{
	// "在 d:\game2 下" / "到 d:\project 中"
	regexp.MustCompile(`(?i)(?:在|到|去)\s*([a-zA-Z]:\\[^\s,，。、]+)`),
	// "在 /home/user/project 下"
	regexp.MustCompile(`(?i)(?:在|到|去)\s*(\/[^\s,，。、]+)`),
	// "在 ~/project 下"
	regexp.MustCompile(`(?i)(?:在|到|去)\s*(~\/[^\s,，。、]+)`),
	// Standalone Windows path (e.g. "d:\game2 开发贪吃蛇")
	regexp.MustCompile(`([a-zA-Z]:\\(?:[^\s\\]+\\)*[^\s\\,，。、]+)`),
}

// ExtractProjectPathFromText extracts an explicit project path from user text.
// Returns empty string if no path found.
func ExtractProjectPathFromText(text string) string {
	for _, re := range projectPathPatterns {
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			path := strings.TrimRight(matches[1], " 下里中内目录")
			// Truncate at the first non-path character (Chinese, etc.)
			// Valid path chars: ASCII letters, digits, _, -, ., \, /, space, ()
			path = truncateToValidPathChars(path)
			if path != "" {
				return path
			}
		}
	}
	return ""
}

// TruncateToValidPathChars removes trailing non-ASCII/non-path characters.
// Stops at the first rune that's not a valid Windows/Unix path character.
// Returns empty string if the result is too short to be a meaningful path
// (e.g. just a drive letter like "D:").
func TruncateToValidPathChars(path string) string {
	return truncateToValidPathChars(path)
}

// truncateToValidPathChars removes trailing non-ASCII/non-path characters.
// Stops at the first rune that's not a valid Windows/Unix path character.
// Returns empty string if the result is too short to be a meaningful path
// (e.g. just a drive letter like "D:").
func truncateToValidPathChars(path string) string {
	runes := []rune(path)
	end := len(runes)
	for i, r := range runes {
		if r > 127 { // Non-ASCII (Chinese, etc.) — not part of a path
			end = i
			break
		}
	}
	result := strings.TrimRight(string(runes[:end]), " \\/")
	// A bare drive letter (e.g. "D:") is not a valid project path
	if len(result) <= 2 {
		return ""
	}
	return result
}
