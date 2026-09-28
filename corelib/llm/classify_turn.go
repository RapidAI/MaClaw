package llm

import (
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ClassifyHints carries optional loop context for rule-based turn routing.
type ClassifyHints struct {
	// HasAttachments is true when the user message includes images/files that
	// may need vision or heavy multimodal handling.
	HasAttachments bool
	// ToolHeavy is true when the turn already involves tools / coding / workflow
	// execution and should prefer a stronger model.
	ToolHeavy bool
	// ForceReasoning forces TaskReasoning (e.g. coding subagent entry).
	ForceReasoning bool
	// PreferFast biases short, simple turns toward TaskFast when ambiguous.
	PreferFast bool
}

// ClassifyResult is the outcome of rule-based turn classification.
type ClassifyResult struct {
	Task   TaskType
	Reason string
}

// ClassifyTurn picks a TaskType from user text + hints using cheap heuristics.
// It never calls a model. Callers feed the result into ModelRouter.RouteWithAux.
func ClassifyTurn(userText string, hints ClassifyHints) ClassifyResult {
	if hints.ForceReasoning || hints.ToolHeavy {
		return ClassifyResult{Task: TaskReasoning, Reason: "tool-heavy or forced reasoning path"}
	}
	if hints.HasAttachments {
		return ClassifyResult{Task: TaskVision, Reason: "message has attachments"}
	}

	text := strings.TrimSpace(userText)
	if text == "" {
		return ClassifyResult{Task: TaskFast, Reason: "empty user text"}
	}
	lower := strings.ToLower(text)
	runes := utf8.RuneCountInString(text)

	// Vision cues in text (screenshot / image analysis) even without attachments.
	if containsAny(lower, visionCues...) {
		return ClassifyResult{Task: TaskVision, Reason: "vision-related request"}
	}

	// Summary / compression jobs → cheap model.
	if containsAny(lower, summaryCues...) {
		return ClassifyResult{Task: TaskSummary, Reason: "summarization request"}
	}

	// Coding / debugging / complex work → strong model.
	if containsAny(lower, reasoningCues...) {
		return ClassifyResult{Task: TaskReasoning, Reason: "coding or complex analysis cues"}
	}

	// Short chit-chat / simple Q&A → fast.
	if runes <= 48 && !looksLikeMultiStep(lower) {
		if containsAny(lower, intentCues...) {
			return ClassifyResult{Task: TaskIntent, Reason: "short intent/classification style question"}
		}
		return ClassifyResult{Task: TaskFast, Reason: "short simple turn"}
	}

	// Medium length but still simple greeting / thanks / status.
	if runes <= 120 && containsAny(lower, fastCues...) && !looksLikeMultiStep(lower) {
		return ClassifyResult{Task: TaskFast, Reason: "lightweight conversational turn"}
	}

	if hints.PreferFast && runes <= 200 && !looksLikeMultiStep(lower) {
		return ClassifyResult{Task: TaskFast, Reason: "prefer-fast bias"}
	}

	return ClassifyResult{Task: TaskDefault, Reason: "default agent turn"}
}

func looksLikeMultiStep(lower string) bool {
	return containsAny(lower,
		"step by step", "multi-step", "然后", "接着", "第一步", "第二步",
		"and then", "first ", "second ", "todo", "checklist", "plan to",
	)
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if cueMatches(haystack, n) {
			return true
		}
	}
	return false
}

// cueMatches keeps short ASCII cues from firing inside a longer token.
// "api" is the sharp case: substring matching treated https://api.example
// and apikey as a coding task (production 2026-09-27) and routed a
// knowledge-save turn onto the reasoning workload.
func cueMatches(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	if needle == "api" {
		return matchAPICue(haystack)
	}
	return strings.Contains(haystack, needle)
}

func matchAPICue(lower string) bool {
	const needle = "api"
	start := 0
	for {
		idx := strings.Index(lower[start:], needle)
		if idx < 0 {
			return false
		}
		idx += start
		if apiCueAt(lower, idx) {
			return true
		}
		start = idx + len(needle)
	}
}

func apiCueAt(lower string, idx int) bool {
	if idx > 0 && apiCuePrefixContinues(lower[idx-1]) {
		return false
	}
	after := idx + len("api")
	if after < len(lower) && apiCueSuffixContinues(lower[after]) {
		return false
	}
	return !apiCueInsideURL(lower, idx)
}

// A letter before "api" still counts: "openapi" is a coding word. Digits and
// "_-." are glue, so ".api" is a hostname label rather than the cue.
func apiCuePrefixContinues(b byte) bool {
	return (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.'
}

// Anything after "api" that continues the token is a different word:
// apikey, api_key, api-key, api.example.
func apiCueSuffixContinues(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.'
}

func apiCueInsideURL(lower string, idx int) bool {
	lineStart := strings.LastIndexAny(lower[:idx], " \t\n") + 1
	token := lower[lineStart:]
	if end := strings.IndexAny(token, " \t\n"); end >= 0 {
		token = token[:end]
	}
	// Wrapping punctuation, "(https://…/api)", hides a strict prefix check.
	// The cue is inside the URL when a scheme appears earlier in the token.
	rel := idx - lineStart
	for _, scheme := range []string{"https://", "http://"} {
		if at := strings.Index(token, scheme); at >= 0 && at < rel {
			return true
		}
	}
	return false
}

var visionCues = []string{
	"screenshot", "image", "photo", "picture", "ocr", "截图", "图片", "照片", "看图", "识别图",
	"这张图", "这幅图", "图中有", "图里有", "图里写", "图上有",
}

var summaryCues = []string{
	"summarize", "summary", "tldr", "tl;dr", "condense", "summarise",
	"总结", "摘要", "概括", "简述", "精简", "压缩一下",
}

var reasoningCues = []string{
	// coding
	"code", "coding", "debug", "bug", "stack trace", "compile", "refactor",
	"pull request", "github", "gitlab", "unit test", "typescript", "golang",
	"python", "rust", "java", "sql", "function ", "class ", "error:", "exception",
	"代码", "编程", "实现", "调试", "报错", "崩溃", "编译", "重构", "单元测试",
	"接口", "api", "pr ", "diff", "commit", "git ", "终端", "shell", "powershell",
	// short ops / shell / file work that often misroutes to TaskFast when terse
	"run ", "npm ", "pip ", "docker ", "kubectl", "go test", "go build",
	"cargo ", "make ", "chmod ", "mkdir ", "rm -", "grep ", "curl ",
	"bash", "powershell", "cmd.exe", "命令行", "执行命令", "跑一下", "运行一下",
	"写个脚本", "改一下文件", "改文件", "打开终端", "帮我修", "修一下", "修bug",
	// complex analysis
	"architecture", "design doc", "root cause", "investigate",
	"架构", "根因", "排查", "方案设计", "竞品分析", "合同审查",
}

var intentCues = []string{
	"which ", "what kind", "classify", "is this", "yes or no",
	"是不是", "属于", "分类", "意图", "要不要", "能不能",
}

var fastCues = []string{
	"hello", "hi ", "hey", "thanks", "thank you", "good morning", "good night",
	"你好", "您好", "谢谢", "早", "晚安", "在吗", "ok", "okay", "收到", "好的",
	"what time", "几点", "天气", "weather",
}

// DecideTurn classifies the turn and resolves an LLM config via the router.
// source is "route" | "aux" | "primary".
func DecideTurn(
	router *ModelRouter,
	primary corelib.MaclawLLMConfig,
	aux corelib.AuxiliaryLLMConfig,
	userText string,
	hints ClassifyHints,
) (cfg corelib.MaclawLLMConfig, task TaskType, source, reason string) {
	classified := ClassifyTurn(userText, hints)
	task = classified.Task
	reason = classified.Reason

	before := primary
	cfg = router.RouteWithAux(task, primary, aux)

	switch {
	case router != nil && router.HasRoute(task):
		source = "route"
	case aux.IsConfigured() && isLightweightTask(task) &&
		(cfg.Model != before.Model || cfg.URL != before.URL):
		source = "aux"
	default:
		source = "primary"
		if task != TaskDefault && task != TaskReasoning {
			// Classification wanted a light path but no aux/route was available.
			reason = reason + "; no aux/route — stayed on primary"
		}
	}
	return cfg, task, source, reason
}
