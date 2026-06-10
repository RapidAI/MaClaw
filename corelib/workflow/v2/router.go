package v2

import (
	"regexp"
	"strings"
)

// RouteTarget indicates where a message should be handled.
type RouteTarget string

const (
	RouteToAgentLoop RouteTarget = "agent_loop"
	RouteToWorkflow  RouteTarget = "workflow"
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

// WorkflowRouter is the single decision point for message routing.
// It replaces QuickFilter + UIC + IUM + GateIntentClassifier + SteeringDetector.
type WorkflowRouter struct {
	machine   *StateMachine
	templates *TemplateRegistry
	llmFunc   LLMConfirmFunc // optional; nil = keyword-only
}

func NewWorkflowRouter(machine *StateMachine, templates *TemplateRegistry, llmFunc LLMConfirmFunc) *WorkflowRouter {
	return &WorkflowRouter{
		machine:   machine,
		templates: templates,
		llmFunc:   llmFunc,
	}
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

	// Step 7: Extract project path from text
	projectPath := ExtractProjectPathFromText(text)

	return &RouteResult{
		Target:       RouteToWorkflow,
		WorkflowType: matched.Type,
		ProjectPath:  projectPath,
	}
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
			return path
		}
	}
	return ""
}
