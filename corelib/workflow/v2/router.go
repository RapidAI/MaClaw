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
	// RunnerUp is set when a second template scored close to the winner
	// (ratio >= ambiguousTemplateRatio). GUI layer can show a disambiguation
	// panel letting the user pick the correct template.
	RunnerUp string
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
	ComplexityNone    TaskComplexity = "none"    // Not a coding task — route to normal agent loop
)

// ComplexityFunc assesses whether a coding task is simple (direct coding) or complex (needs SDD).
// Returns ComplexitySimple for quick tasks (bug fix, hello world, add a button).
// Returns ComplexityComplex for projects needing design/planning.
// When nil, assessComplexity conservatively returns ComplexityComplex.
type ComplexityFunc func(text string) TaskComplexity

// WorkflowRouter is the single decision point for message routing.
// It replaces QuickFilter + UIC + IUM + GateIntentClassifier + SteeringDetector.
type WorkflowRouter struct {
	machine        *StateMachine
	templates      *TemplateRegistry
	llmFunc        LLMConfirmFunc // optional confirmation after structured template match
	complexityFunc ComplexityFunc // optional; nil = conservative complex fallback
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

// GetComplexityFunc returns the current complexity function (nil if not set).
func (r *WorkflowRouter) GetComplexityFunc() ComplexityFunc {
	return r.complexityFunc
}

// HasTemplate returns true if a template with the given type string is registered.
func (r *WorkflowRouter) HasTemplate(workflowType string) bool {
	if r == nil || r.templates == nil {
		return false
	}
	return r.templates.Get(workflowType) != nil
}

// Route decides whether a message should go to a workflow or the normal agent loop.
func (r *WorkflowRouter) Route(userID, text string, attachments []Attachment) *RouteResult {
	return r.RouteWithHint(userID, text, attachments, "")
}

// RouteWithHint routes a message to the appropriate handler, with an optional
// semantic intent hint (e.g. "coding", "non_coding") from an external classifier.
// When BM25 template matching fails but the hint matches a registered template type,
// the hint is used as a fallback match signal.
func (r *WorkflowRouter) RouteWithHint(userID, text string, attachments []Attachment, semanticHint string) *RouteResult {
	if r == nil || r.templates == nil {
		return &RouteResult{Target: RouteToAgentLoop}
	}
	// Step 1: Active workflow takes priority
	if r.machine != nil {
		if state := r.machine.GetActive(userID); state != nil {
			result, err := r.machine.HandleInput(userID, text)
			if err != nil {
				return &RouteResult{Target: RouteToAgentLoop}
			}
			if result == nil {
				return &RouteResult{Target: RouteToAgentLoop}
			}
			if result.Action == ActionPassThrough {
				// PassThrough means the message is unrelated to the active workflow
				// (e.g. workflow is in PhaseExecuting, or confirm classifier said "unrelated").
				// Fall through to structured template matching below; the user may
				// be starting a new workflow. If no template matches, the message
				// goes to the normal agent loop.
			} else if result.Action == ActionRunPhase {
				// Phase is pending/running. Check if the user's message looks like a
				// completely new workflow task (not a continuation of the current one).
				// If so, fall through to structured template matching which will
				// start a new workflow.
				// startNewWorkflowV2 handles cancelling the old workflow before creating the new one.
				if r.looksLikeNewWorkflowTask(text) {
					// Fall through to template matching; startNewWorkflowV2 will cancel old + create new.
				} else {
					return &RouteResult{Target: RouteToWorkflow, HandleResult: result}
				}
			} else {
				return &RouteResult{Target: RouteToWorkflow, HandleResult: result}
			}
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

	if shouldSuppressWorkflowColdStart(text) {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 4: (Removed) Bug-fix detection was keyword-based and caused false positives.
	// e.g. "BUG修复验证报告" (document task) was misclassified as a code bug fix.
	// Complexity/task-type classification is now done via user choice in the GUI layer.

	// Step 4.5: Semantic hint veto — if UIC confidently identified a non-workflow
	// intent (e.g. "ssh", "search", "document_delivery") that doesn't correspond
	// to any registered workflow template, skip BM25 template matching entirely.
	// This prevents false positives where a single shared word (e.g. "服务器" in
	// "查询api2服务器信息" matching paper_reproduction's description) causes a
	// spurious workflow recommendation.
	if semanticHint != "" && r.templates.Get(semanticHint) == nil {
		return &RouteResult{Target: RouteToAgentLoop}
	}
	if semanticHint == "" && !hasWorkflowStartSignal(text) {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 4.6: Strong action keyword fallback hint — when embedder is not
	// confident enough (semanticHint=="") but hasWorkflowStartSignal passed
	// due to a strong coding action verb ("开发"/"实现"/"搭建"/"构建"/etc),
	// infer semanticHint="coding" so Step 5's BM25 fallback can activate.
	// This covers arbitrary target names ("hello world", "flappy bird", etc.)
	// that BM25 can't match against template descriptions.
	if semanticHint == "" && hasStrongCodingActionInText(strings.ToLower(text)) {
		semanticHint = "coding"
	}

	// Step 5: Structured template match.
	//
	// When semanticHint already points to a concrete registered workflow type,
	// treat it as authoritative for the primary route. BM25 still participates
	// in runner-up detection later, but it must not override a confident
	// external hint with a different template due to short-text overlap noise.
	matched := r.templates.MatchByText(text)
	if semanticHint != "" {
		if hinted := r.templates.Get(semanticHint); hinted != nil {
			matched = hinted
		}
	}
	if matched == nil && semanticHint != "" {
		// BM25 text matching failed, but an external classifier (e.g. UIC)
		// identified the intent. If a template with that type is registered,
		// use it as a fallback. This handles cases where the user's message
		// has domain-specific terms (paths, framework names) that dilute BM25
		// relevance against template descriptions.
		// NOTE: Step 4.5 already vetoed hints that don't match any template,
		// so Get(semanticHint) here is guaranteed non-nil.
		matched = r.templates.Get(semanticHint)
	}
	if matched == nil {
		return &RouteResult{Target: RouteToAgentLoop}
	}

	// Step 6: Optional LLM confirmation over the structured template candidate.
	if r.llmFunc != nil {
		if !r.llmFunc(text, matched.Type) {
			return &RouteResult{Target: RouteToAgentLoop}
		}
	}

	// Step 7: For coding tasks, let the user decide complexity.
	// The router returns RouteToWorkflow; the GUI layer will ask the user
	// whether to use simple (direct coding) or full SDD before proceeding.
	// Non-coding templates (PPT, business plan, etc.) always go to full workflow.

	// Step 8: Extract project path from text
	projectPath := ExtractProjectPathFromText(text)

	// Step 8.5: Detect ambiguous runner-up — if a second template scored close
	// to the winner (ratio >= 0.85), include it so the GUI layer can offer the
	// user a disambiguation choice instead of guessing.
	var runnerUp string
	if matched != nil {
		ranked := r.templates.RankedByText(text)
		if len(ranked) >= 2 && ranked[0].Score > 0 && ranked[1].Score > 0 {
			ratio := ranked[1].Score / ranked[0].Score
			if ratio >= ambiguousTemplateRatio {
				// Find the highest-scoring template that is NOT the matched one.
				// That's the runner-up candidate for disambiguation.
				for _, ts := range ranked {
					if ts.Type != matched.Type && ts.Score/ranked[0].Score >= ambiguousTemplateRatio {
						runnerUp = ts.Type
						break
					}
				}
			}
		}
	}

	return &RouteResult{
		Target:       RouteToWorkflow,
		WorkflowType: matched.Type,
		ProjectPath:  projectPath,
		RunnerUp:     runnerUp,
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
	// Without LLM: always default to complex (full SDD) — the safe choice.
	return ComplexityComplex
}

// --- Ambiguous Template Detection ---

// ambiguousTemplateRatio: when runner-up score / top score >= this ratio,
// the match is considered ambiguous and the GUI shows a disambiguation panel.
// 0.85 means the runner-up must be at least 85% of the top score.
const ambiguousTemplateRatio = 0.85

// --- Skip Signals ---

var skipSignals = []string{
	// Simplified Chinese
	"直接做", "不用问了", "按你的想法来", "跳过文档", "不需要文档",
	"直接开始", "不用三阶段", "直接编码", "不要文档", "别废话",
	// Traditional Chinese (only items that differ from simplified)
	"不用問了", "跳過文檔", "不需要文檔",
	"直接開始", "不用三階段", "直接編碼", "不要文檔",
	// English
	"skip workflow", "just do it", "skip docs",
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

func shouldSuppressWorkflowColdStart(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if isCasualChatMessage(lower) {
		return true
	}
	if len([]rune(trimmed)) <= 6 && !hasWorkflowStartSignal(lower) {
		return true
	}
	return false
}

func isCasualChatMessage(lower string) bool {
	normalized := strings.Trim(lower, " \t\r\n,.!?！？。~～")
	if normalized == "" {
		return true
	}
	casualExact := map[string]bool{
		"你好": true, "您好": true, "嗨": true, "hi": true, "hello": true,
		"你是谁": true, "你是谁呀": true, "你是谁啊": true, "你是誰": true,
		"你叫什么": true, "你叫什么名字": true, "你能做什么": true,
		"谢谢": true, "谢了": true, "thanks": true, "thank you": true,
		"who are you": true, "what can you do": true,
	}
	if casualExact[normalized] {
		return true
	}
	casualPrefixes := []string{
		"你是谁", "你是誰", "who are you", "what are you",
		"介绍一下你自己", "自我介绍", "你能干什么", "你能做什么",
	}
	for _, prefix := range casualPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func hasWorkflowStartSignal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, phrase := range explicitWorkflowObjectSignals {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	if hasGaokaoApplicationStartSignal(lower) {
		return true
	}
	// Check strong coding action first — these are sufficient on their own.
	if hasStrongCodingActionInText(lower) && len([]rune(lower)) >= 6 {
		return true
	}

	// Long text suppression: messages > 200 runes are almost certainly
	// technical discussions, problem descriptions, or pasted logs — not
	// imperative "start a workflow" commands. The weak action+object
	// combination (e.g. "生成" + "代码") produces false positives in long
	// text because keywords appear in descriptive context ("模型生成 HTML...
	// Hub 代码里的 timeout"). Require strong signals only for long messages.
	//
	// Strong signals (explicitWorkflowObjectSignals, strongCodingAction,
	// gaokaoApplicationStartSignal) are checked before this guard and
	// bypass it unconditionally.
	if len([]rune(lower)) > maxWeakSignalTextLength {
		return false
	}

	hasAction := false
	for _, action := range workflowActionSignals {
		if strings.Contains(lower, action) {
			hasAction = true
			break
		}
	}
	if !hasAction {
		return false
	}
	for _, object := range workflowObjectSignals {
		if strings.Contains(lower, object) {
			return true
		}
	}
	return false
}

// maxWeakSignalTextLength: messages longer than this (in runes) are suppressed
// from weak action+object workflow detection. Only strong signals (explicit
// workflow object phrases, strong coding verbs, gaokao signals) can trigger
// workflow start for long messages.
//
// Calibrated: longest legitimate imperative with a path is ~80 runes
// ("在d:\\workprj\\myproject 下开发一个带音效的贪吃蛇游戏，C++ cmake管理").
// 200 gives ample headroom while filtering 500+ char technical discussions.
const maxWeakSignalTextLength = 200

func hasGaokaoApplicationStartSignal(lowerText string) bool {
	hasObject := false
	for _, object := range gaokaoApplicationObjectSignals {
		if strings.Contains(lowerText, object) {
			hasObject = true
			break
		}
	}
	if !hasObject {
		return false
	}
	for _, action := range gaokaoApplicationActionSignals {
		if strings.Contains(lowerText, action) {
			return true
		}
	}
	return false
}

// strongCodingActionSignalsChinese are Chinese verbs that unambiguously indicate
// the user wants to CREATE a new project/program. They have very low false-positive
// risk because Chinese doesn't use these verbs for trivial operations.
// Includes both simplified and traditional Chinese variants.
var strongCodingActionSignalsChinese = []string{
	// Simplified
	"开发", "实现", "搭建", "构建",
	// Traditional
	"開發", "實現", "構建",
}

// strongCodingActionSignalsEnglish are English verbs that, combined with
// sufficient message length, suggest creating a new project. "build" and "create"
// are excluded because they are too ambiguous in short commands
// ("build the project", "create a folder").
var strongCodingActionSignalsEnglish = []string{
	"develop", "implement",
}

// hasStrongCodingActionInText checks if text contains a strong coding action verb.
// Used by both hasWorkflowStartSignal (Step 4.5 gate) and RouteWithHint (Step 4.6 hint).
func hasStrongCodingActionInText(lowerText string) bool {
	for _, action := range strongCodingActionSignalsChinese {
		if strings.Contains(lowerText, action) {
			return true
		}
	}
	for _, action := range strongCodingActionSignalsEnglish {
		if strings.Contains(lowerText, action) {
			return true
		}
	}
	// "创建"/"創建" is borderline — it can mean "create a folder" in Chinese too.
	// Accept it only when message length > 8 runes (rules out "创建文件夹" etc).
	if (strings.Contains(lowerText, "创建") || strings.Contains(lowerText, "創建")) && len([]rune(lowerText)) > 8 {
		return true
	}
	return false
}

var explicitWorkflowObjectSignals = []string{
	// Simplified Chinese
	"工作流", "申请书", "申报书", "项目申请", "基金申请", "国自然", "国家自然科学基金",
	"青年基金", "青年科学基金", "青基", "优青", "杰青", "长江学者",
	"高考志愿", "志愿填报", "报志愿", "中外合办学校", "境外校区志愿", "位次能报",
	// Traditional Chinese (only items that differ from simplified)
	"工作流程", "申請書", "項目申請", "基金申請",
	"傑青", "長江學者",
	// English
	"business plan", "slide deck", "grant proposal", "research proposal",
}

var gaokaoApplicationObjectSignals = []string{
	"高考", "志愿", "位次", "中外合办", "境外校区", "马来西亚分校", "芬兰校区",
}

var gaokaoApplicationActionSignals = []string{
	"填报", "报志愿", "能报", "可报", "冲稳保", "保底", "冲一冲", "稳一稳",
}

var workflowActionSignals = []string{
	// Simplified Chinese
	"写", "做", "生成", "制作", "设计", "开发", "实现", "创建", "起草", "撰写",
	"打磨", "预审", "审查", "分析", "策划", "规划", "准备", "申请", "申报",
	"复现", "跑实验", "输出",
	// Traditional Chinese (only items that differ from simplified)
	"寫", "製作", "設計", "開發", "實現", "創建", "撰寫",
	"預審", "策劃", "規劃", "準備", "申請",
	"複現",
	// English
	"build", "create", "generate", "make", "design",
	"develop", "implement", "write", "draft", "review", "analyze", "plan",
	"prepare", "apply", "reproduce",
}

var workflowObjectSignals = []string{
	// Simplified Chinese
	"项目", "应用", "系统", "工具", "游戏", "代码", "软件", "程序", "脚本",
	"网站", "网页", "页面", "接口", "服务", "平台", "插件", "组件", "模块",
	"ppt", "幻灯片", "演示文稿", "产品", "prd", "方案", "商业计划", "bp",
	"测试", "文献综述", "研究报告", "调研报告", "竞品", "立项", "活动",
	"投标", "标书", "合同", "尽调", "审计", "专利", "实验", "基金", "论文",
	"申请", "申报", "复现",
	// Traditional Chinese (only items that differ from simplified)
	"項目", "應用", "系統", "工具", "遊戲", "軟體", "程式", "腳本",
	"網站", "網頁", "頁面", "介面", "平台", "組件", "模組",
	"簡報", "產品", "商業計劃", "商業計畫",
	"測試", "文獻綜述", "研究報告", "競品",
	"投標", "標書", "合約", "專利", "實驗", "論文",
	"企劃", "企劃書",
	// English
	"application", "app", "service", "tool", "game", "code", "software",
	"program", "script", "website", "webpage", "api", "server", "plugin",
	"component", "module",
	"presentation", "slides", "deck", "proposal", "report", "review",
	"analysis", "experiment", "paper", "contract", "patent", "tender",
}

// looksLikeNewWorkflowTask checks whether a user message is a completely new
// workflow request unrelated to the current running phase. Uses the optional
// LLM confirmation function for semantic judgment when available, falling back
// to a conservative heuristic (structured template match + minimum length) only
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
	matched := r.templates.MatchByText(text)
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
