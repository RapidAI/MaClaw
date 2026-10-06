package guiapp

import (
	"context"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// Task titles start as the truncated first user command
// (recentTaskDisplayTitle), which reads badly when the first instruction is a
// long sentence or carries a URL. This file adds an async LLM summarization
// pass on top: after a task record is created, a lightweight P2 request
// condenses the first instruction into a short human title
// ("安装 huashu-art-motion 技能") and applies it through the same RenameTask
// path as a manual rename, so the sidebar and the open tab update through the
// existing rename/index event fan-out. The only frontend involvement is a
// tab-title fallback while the rename event is in flight (taskConfigSend.ts).

const (
	// autoTaskTitleMaxRunes caps the generated title. It matches the frontend
	// DESCRIBED_TITLE_MAX so the display-time describeTaskTitle pass-through
	// never rewrites what we stored here.
	autoTaskTitleMaxRunes = 24
	// taskTitleLLMTimeout bounds the background naming call.
	taskTitleLLMTimeout = 30 * time.Second
	// taskTitleInputMaxRunes bounds the instruction sent to the LLM.
	taskTitleInputMaxRunes = 400
)

// taskTitleSystemPrompt instructs the model to produce a short human-readable
// task title. Kept inline per project convention (no template files).
const taskTitleSystemPrompt = `你为任务列表生成简短标题。根据用户创建任务时的第一条指令，概括任务目标，输出一个具体的短标题。
规则：
- 用指令的主要语言（中文指令输出中文标题）
- 保留关键对象名（软件名、项目名、技能名、文件名），去掉 URL、域名、路径、主机名、账号密码等敏感或冗长内容
- 2 到 20 个字，不以标点结尾，不加"任务："之类前缀，不加引号
- 只输出标题本身，单行，不要解释`

// taskTitleInFlight deduplicates concurrent summarization runs per task path.
var taskTitleInFlight sync.Map

// taskTitleQuoteRunes and taskTitleTrailingPunct bound what the sanitizer
// strips from the model output. Trimming alternates because models like to
// wrap a title in one quote plus one trailing period (「...」。).
const (
	taskTitleQuoteRunes    = "\"'`“”‘’「」『』【】《》"
	taskTitleTrailingPunct = "。．.，,、；;！!？?～~·…—-_-*#→ "
)

// taskTitleLLMFn overrides the title-pass LLM entry point in tests so unit
// tests never issue real requests. Nil means the real implementation.
var taskTitleLLMFn func(*App, string) string

// containsWebAddress reports whether a lowercased string carries a URL or
// host-like web address. Bare "http" is deliberately not matched so a title
// about HTTP itself survives ("修复 HTTP 接口超时").
func containsWebAddress(lower string) bool {
	return strings.Contains(lower, "http://") || strings.Contains(lower, "https://") ||
		strings.Contains(lower, "http:") || strings.Contains(lower, "www.")
}

// The display layer (describeTaskTitle.ts) strips host-like tokens and masks
// secrets from whatever it shows, so the generated title must not rely on
// either: a stored title containing a host would render mangled, and a secret
// would leak into the task list. These patterns mirror that layer.
var (
	// taskTitleDottedTokenRe finds dotted tokens (nginx.example.com, main.go).
	taskTitleDottedTokenRe = regexp.MustCompile(`[a-z0-9-]+(?:\.[a-z0-9-]+)+`)
	// taskTitleIPv4Re finds addresses the display layer strips (192.168.1.1).
	taskTitleIPv4Re = regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}`)
	// taskTitleFileExtRe marks dotted tokens that are filenames, not hosts
	// (main.go, package.json) — same list as the frontend FILE_EXT_RE.
	taskTitleFileExtRe = regexp.MustCompile(`^(?:pdf|docx?|pptx?|xlsx?|png|jpe?g|gif|svg|webp|md|txt|json|ya?ml|tsx?|jsx?|mjs|cjs|go|py|css|html?|zip|gz|mp[34]|wav|csv|log)$`)
	// taskTitleTLDRe requires the last label to look like a TLD (letters only).
	taskTitleTLDRe = regexp.MustCompile(`^[a-z]{2,}$`)
	// Secret label/value pairs to scrub, mirroring the display layer's masking:
	// an explicit separator, or a bare value only when it carries a digit (so
	// "token 刷新机制" survives); Chinese labels count any following token.
	taskTitleSecretENValueRe = regexp.MustCompile(`(?i)(?:password|passwd|pwd|token|secret|api[\s_-]?key)\s*[:：=＝]\s*\S+`)
	taskTitleSecretENDigitRe = regexp.MustCompile(`(?i)(?:password|passwd|pwd|token|secret|api[\s_-]?key)\s+\S*\d\S*`)
	taskTitleSecretCNRe      = regexp.MustCompile(`(?:密码|口令)\s*[:：=]?\s*[^\s，,。；;]+`)
)

// hasHostToken reports whether s carries a host-like token or IP the display
// layer would strip ("部署 nginx.example.com 证书", "连接 192.168.1.1").
func hasHostToken(s string) bool {
	lower := strings.ToLower(s)
	if taskTitleIPv4Re.MatchString(lower) {
		return true
	}
	for _, token := range taskTitleDottedTokenRe.FindAllString(lower, -1) {
		last := token[strings.LastIndexByte(token, '.')+1:]
		if taskTitleFileExtRe.MatchString(last) {
			continue
		}
		if taskTitleTLDRe.MatchString(last) {
			return true
		}
	}
	return false
}

// taskTitleNeedsSummarization reports whether the rule-derived title is still
// raw enough to be worth an LLM pass. Already-short clean instructions
// (「北京天气」) pass through untouched to save the round trip.
func taskTitleNeedsSummarization(instruction string) bool {
	trimmed := strings.TrimSpace(instruction)
	if trimmed == "" {
		return false
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return true
	}
	if containsWebAddress(strings.ToLower(trimmed)) || hasHostToken(trimmed) {
		return true
	}
	return len([]rune(trimmed)) > autoTaskTitleMaxRunes
}

// scrubTaskTitleSecrets removes label/value secret pairs the way the display
// layer masks them, so a secret echoed by the model never reaches the list.
func scrubTaskTitleSecrets(s string) string {
	s = taskTitleSecretENValueRe.ReplaceAllString(s, "")
	s = taskTitleSecretENDigitRe.ReplaceAllString(s, "")
	return strings.TrimSpace(taskTitleSecretCNRe.ReplaceAllString(s, ""))
}

// sanitizeLLMTaskTitle normalizes raw LLM output into a display-safe short
// title. Colons are flattened so the stored title never trips the display-time
// rewrite branch, and anything that still carries a URL or host is rejected
// (checked on the raw line first: the colon flattening below would hide
// "https://"). Returns "" when nothing usable remains; the caller keeps the
// existing title.
func sanitizeLLMTaskTitle(raw string) string {
	line := raw
	if idx := strings.IndexAny(line, "\r\n"); idx >= 0 {
		line = line[:idx]
	}
	line = scrubTaskTitleSecrets(line)
	if containsWebAddress(strings.ToLower(line)) || hasHostToken(line) {
		return ""
	}
	line = strings.TrimSpace(line)
	line = strings.Trim(line, taskTitleQuoteRunes)
	for i := 0; i < 4; i++ {
		before := line
		line = strings.Trim(line, taskTitleTrailingPunct)
		line = strings.Trim(line, taskTitleQuoteRunes)
		line = strings.TrimSpace(line)
		if line == before {
			break
		}
	}
	line = strings.ReplaceAll(line, "：", " ")
	line = strings.ReplaceAll(line, ":", " ")
	line = strings.Join(strings.Fields(line), " ")
	runes := []rune(line)
	if len(runes) > autoTaskTitleMaxRunes {
		// Never cut inside a Latin word: back up to the last separator still
		// inside the budget (but keep at least half of it, so a long single
		// CJK title or one long identifier is not gutted). The search runs on
		// runes — LastIndexAny would mix byte indices with the rune budget.
		end := autoTaskTitleMaxRunes
		for i := autoTaskTitleMaxRunes - 1; i >= autoTaskTitleMaxRunes/2; i-- {
			if r := runes[i]; r == ' ' || r == '-' || r == '_' {
				end = i
				break
			}
		}
		line = string(runes[:end])
	}
	line = strings.TrimSpace(line)
	line = strings.Trim(line, taskTitleTrailingPunct)
	line = strings.TrimSpace(line)
	if line == "" || containsWebAddress(strings.ToLower(line)) {
		return ""
	}
	return line
}

// generateTaskTitleWithLLM asks the configured LLM for a concise task title
// derived from the first instruction. Returns "" on any failure (LLM
// unconfigured, call error, unusable output) so the caller keeps the
// rule-derived title; naming must never block or fail task creation.
func (a *App) generateTaskTitleWithLLM(instruction string) string {
	if a == nil {
		return ""
	}
	// Resolve the config once: isMaclawLLMConfigured would re-read it behind
	// its own GetMaclawLLMConfig call.
	cfg := attachLightweightHubHint(a.GetMaclawLLMConfig(), llm.TaskSummary)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": taskTitleSystemPrompt},
		map[string]string{"role": "user", "content": "任务指令：\n" + truncateRunes(strings.TrimSpace(instruction), taskTitleInputMaxRunes)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskTitleLLMTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "task-title"})
	client := &http.Client{Timeout: taskTitleLLMTimeout}
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, taskTitleLLMTimeout)
	if err != nil {
		log.Printf("[task-title] LLM summarize failed: %v", err)
		return ""
	}
	return sanitizeLLMTaskTitle(resp.Content)
}

// scheduleTaskTitleSummarization fires the async title pass for a freshly
// created task. The cheap gates run synchronously; the LLM call and rename
// happen on a goroutine so task creation latency is untouched. Without a Wails
// app context (unit tests, headless helpers) there is no event bus to refresh
// the UI, so the pass is skipped entirely — it also keeps test binaries from
// issuing real LLM requests.
func (a *App) scheduleTaskTitleSummarization(projectPath, instruction string) {
	if a == nil || a.ctx == nil {
		return
	}
	projectPath = normalizeProjectSessionPath(strings.TrimSpace(projectPath))
	instruction = strings.TrimSpace(instruction)
	if projectPath == "" || instruction == "" || !taskTitleNeedsSummarization(instruction) {
		return
	}
	if _, busy := taskTitleInFlight.LoadOrStore(projectPath, struct{}{}); busy {
		return
	}
	go func() {
		defer taskTitleInFlight.Delete(projectPath)
		generate := taskTitleLLMFn
		if generate == nil {
			generate = (*App).generateTaskTitleWithLLM
		}
		title := generate(a, instruction)
		if title == "" {
			return
		}
		a.applyAutoTaskTitle(projectPath, instruction, title)
	}()
}

// applyAutoTaskTitle renames the task to the generated title unless the user
// renamed it while the LLM call was in flight, or deleted it. It reuses
// RenameTask so the custom-name preference and all rename events fan out
// exactly like a manual rename.
func (a *App) applyAutoTaskTitle(projectPath, originalInstruction, title string) {
	a.ensureMemoryStore()
	if a.memoryStore == nil {
		return
	}
	pi := a.memoryStore.ProjectIndex()
	if pi == nil || pi.Get(projectPath) == nil {
		return
	}
	if existing := strings.TrimSpace(pi.CustomName(projectPath)); existing != "" && existing != recentTaskDisplayTitle(originalInstruction) {
		log.Printf("[task-title] skip auto rename project=%q: user renamed during generation", projectPath)
		return
	}
	if strings.TrimSpace(pi.GetDisplayName(projectPath)) == title {
		return
	}
	renamed := a.RenameTask(projectPath, title)
	log.Printf("[task-title] auto title applied project=%q title=%q", projectPath, renamed)
}
