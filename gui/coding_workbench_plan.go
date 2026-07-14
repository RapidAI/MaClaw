package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	codingWorkbenchPlanMaxTasks = 6
	codingWorkbenchPlanMinTasks = 2
)

// looksLikeComplexCodingTask is a cheap heuristic to decide whether pure coding
// should auto-plan then multi-step execute (vs a single SubAgent turn).
func looksLikeComplexCodingTask(userText string) bool {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return false
	}
	// Goal-continuation turns already carry the objective; avoid re-planning churn.
	if strings.HasPrefix(userText, "[系统续接]") || strings.Contains(userText, "继续推进目标") {
		return false
	}
	if utf8.RuneCountInString(userText) >= 120 {
		return true
	}
	if strings.Count(userText, "\n") >= 3 {
		return true
	}
	// Explicit multi-step markers.
	if numberedStepCount(userText) >= 2 {
		return true
	}
	lower := strings.ToLower(userText)
	hits := 0
	for _, kw := range codingComplexityKeywords {
		if strings.Contains(lower, kw) {
			hits++
			if hits >= 2 {
				return true
			}
		}
	}
	// Chinese multi-step connectors (avoid bare "并" — matches 合并/并发症 false positives).
	if utf8.RuneCountInString(userText) >= 18 {
		if strings.Contains(userText, "然后") || strings.Contains(userText, "之后") ||
			strings.Contains(userText, "以及") || strings.Contains(userText, "同时还") ||
			strings.Contains(userText, "并且") || strings.Contains(userText, "，并") ||
			strings.Contains(userText, "并补") || strings.Contains(userText, "并做") ||
			strings.Contains(userText, "并加") || strings.Contains(userText, "并写") {
			return true
		}
	}
	// Compound Chinese multi-clause punctuation.
	if strings.Count(userText, "。") >= 2 || strings.Count(userText, "；") >= 1 {
		return true
	}
	// English multi-sentence only when substantive (avoid "go 1.22" / version dots).
	if sentenceDotCount(userText) >= 2 && utf8.RuneCountInString(userText) >= 60 {
		return true
	}
	return false
}

var codingComplexityKeywords = []string{
	"并实现", "并测试", "并且", "同时还", "然后", "之后", "以及", "还要",
	"重构", "迁移", "端到端", "完整实现", "完整功能", "模块", "架构", "拆分",
	"分步", "多步", "前后端", "接口联调", "兼容", "回归",
	"implement", "refactor", "migrate", "end-to-end", "e2e", "and also",
	"then ", " plus ", "with tests", "unit test", "integration",
}

// sentenceDotCount counts '.' that look like sentence terminators, not version
// numbers (e.g. "go 1.22") or single-char extensions.
func sentenceDotCount(text string) int {
	n := 0
	runes := []rune(text)
	for i, r := range runes {
		if r != '.' {
			continue
		}
		// Digit on either side → likely version / decimal.
		if i > 0 && i+1 < len(runes) {
			prev, next := runes[i-1], runes[i+1]
			if prev >= '0' && prev <= '9' && next >= '0' && next <= '9' {
				continue
			}
		}
		n++
	}
	return n
}

// Digit / T-numbered steps only. Bare markdown bullets (- item) are NOT counted —
// they appear in ordinary "fix: - a - b" lists and would false-trigger multi-step plans.
var numberedStepLineRe = regexp.MustCompile(`(?m)^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：])\s+\S+`)

func numberedStepCount(text string) int {
	return len(numberedStepLineRe.FindAllString(text, -1))
}

// resolveCodingWorkbenchTasks returns the TaskItems to run for a pure-coding turn.
// Complex requests may be expanded into an ordered multi-step plan via LLM.
// Simple requests stay a single task. Planner failures fall back to single-task.
func (h *IMMessageHandler) resolveCodingWorkbenchTasks(
	userID, userText, projectPath string,
	sessionMem stickyCodingWorkbenchMemory,
	onProgress func(string),
	onToken func(string),
) (tasks []*v2.TaskItem, planMarkdown string, planned bool) {
	userText = strings.TrimSpace(userText)
	if userText == "" {
		userText = "执行编程任务"
	}
	single := []*v2.TaskItem{{
		Index:       1,
		Title:       truncateRunesV2(userText, 80),
		Description: userText,
	}}
	fallbackSingle := func(reason string) ([]*v2.TaskItem, string, bool) {
		if reason != "" {
			log.Printf("[coding-plan] single-task path user=%s reason=%s", userID, reason)
		}
		// Drop stale multi-step plan so the banner does not show outdated steps.
		h.clearStickyCodingExecutionPlan(userID)
		h.clearStickyCodingStepStatuses(userID)
		return single, "", false
	}
	// Plan mode off: never multi-step.
	planMode := normalizeCodingPlanMode(sessionMem.PlanMode)
	if planMode == codingPlanModeOff {
		return fallbackSingle("plan mode off")
	}
	// /plan skip: one-shot single-task for the next user request.
	if sessionMem.SkipNextPlan {
		if h != nil && userID != "" {
			h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
				mem.SkipNextPlan = false
			})
		}
		return fallbackSingle("skip next plan")
	}
	if !looksLikeComplexCodingTask(userText) {
		return fallbackSingle("")
	}
	// Short follow-ups in an ongoing session usually mean "continue/fix", not replan.
	if sessionMem.TurnCount > 0 && utf8.RuneCountInString(userText) < 80 && numberedStepCount(userText) < 2 {
		return fallbackSingle("short follow-up")
	}

	// Prefer steps already written by the user (numbered / T1 list) — no LLM needed.
	if userPlan := extractUserProvidedCodingPlan(userText); len(userPlan) >= codingWorkbenchPlanMinTasks {
		tasks = userPlan
		log.Printf("[coding-plan] using user-provided steps user=%s steps=%d", userID, len(tasks))
	} else {
		if onProgress != nil {
			onProgress("复杂编程任务：正在自动规划步骤…")
		}
		_, tasks = h.planCodingWorkbenchTasks(userID, userText, projectPath, sessionMem)
		if len(tasks) < codingWorkbenchPlanMinTasks {
			return fallbackSingle(fmt.Sprintf("planner returned %d tasks", len(tasks)))
		}
	}
	if len(tasks) > codingWorkbenchPlanMaxTasks {
		tasks = tasks[:codingWorkbenchPlanMaxTasks]
	}
	tasks = finalizeCodingWorkbenchTasks(tasks, userText)
	// Allow independent explore-only steps to run in parallel waves (TaskRunner MaxParallel).
	tasks = softenExploreOnlyPlanDeps(tasks)
	if len(tasks) < codingWorkbenchPlanMinTasks {
		return fallbackSingle("finalize dropped below min steps")
	}
	// Always rebuild markdown after finalize so indices/deps match execution.
	planMarkdown = formatCodingWorkbenchPlanMarkdown(userText, tasks)
	// Single sticky write: execution plan + seed session goal when empty.
	if userID != "" {
		sessionSeed := ""
		if strings.TrimSpace(sessionMem.SessionPlan) == "" {
			sessionSeed = truncateRunesV2(userText, 400)
		}
		h.persistCodingWorkbenchPlans(userID, planMarkdown, sessionSeed)
		// Seed step statuses as pending for live Todo UI.
		h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	}
	// Approve mode: plan only; execution waits for /plan approve.
	// Caller (runCodingTemplateSubAgent) checks pending vs planned+approve.
	if planMode == codingPlanModeApprove {
		if userID != "" {
			h.storeStickyPendingCodingPlan(userID, userText, planMarkdown, tasks)
		}
		if onProgress != nil {
			onProgress(fmt.Sprintf("已规划 %d 个执行步骤，等待批准后执行", len(tasks)))
		}
		if onToken != nil {
			onToken("\n\n## 自动规划（待批准）\n\n" + planMarkdown + "\n\n")
		}
		log.Printf("[coding-plan] multi-step plan awaiting approve user=%s steps=%d", userID, len(tasks))
		return tasks, planMarkdown, true
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("已规划 %d 个执行步骤，开始按计划实现", len(tasks)))
	}
	if onToken != nil {
		onToken("\n\n## 自动规划\n\n" + planMarkdown + "\n\n---\n\n## 按计划执行\n\n")
	}
	log.Printf("[coding-plan] multi-step plan user=%s steps=%d", userID, len(tasks))
	return tasks, planMarkdown, true
}

// extractUserProvidedCodingPlan parses an explicit multi-step list from the user
// message (numbered bullets or T1: headings) so we do not re-plan with the LLM.
func extractUserProvidedCodingPlan(userText string) []*v2.TaskItem {
	userText = strings.TrimSpace(userText)
	if userText == "" || numberedStepCount(userText) < codingWorkbenchPlanMinTasks {
		// Also allow T1/T2 headings without line-start numbered pattern.
		if tasks := sanitizeParsedCodingTasks(v2.ParseTaskList(userText)); len(tasks) >= codingWorkbenchPlanMinTasks {
			return tasks
		}
		return nil
	}
	if tasks := sanitizeParsedCodingTasks(v2.ParseTaskList(userText)); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks
	}
	// User-authored lists: require 1. / 2. or T1: (not bare "- bullet" lists).
	if tasks := parseCodingWorkbenchPlanNumbered(userText, false); len(tasks) >= codingWorkbenchPlanMinTasks {
		return tasks
	}
	return nil
}

func (h *IMMessageHandler) planCodingWorkbenchTasks(
	userID, userText, projectPath string,
	sessionMem stickyCodingWorkbenchMemory,
) (planMarkdown string, tasks []*v2.TaskItem) {
	if h == nil {
		return "", nil
	}
	cfg := h.getLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getMaclawLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return "", nil
	}

	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("User request:\n")
	ctxBuilder.WriteString(truncateRunesV2(userText, 2000))
	ctxBuilder.WriteString("\n")
	if p := strings.TrimSpace(projectPath); p != "" {
		ctxBuilder.WriteString("\nProject path: ")
		ctxBuilder.WriteString(p)
		ctxBuilder.WriteString("\n")
	}
	if s := strings.TrimSpace(sessionMem.SessionPlan); s != "" {
		ctxBuilder.WriteString("\nSession goal:\n")
		ctxBuilder.WriteString(truncateRunesV2(s, 400))
		ctxBuilder.WriteString("\n")
	}
	if s := strings.TrimSpace(sessionMem.LastSummary); s != "" {
		ctxBuilder.WriteString("\nPrevious turn summary:\n")
		ctxBuilder.WriteString(truncateRunesV2(s, 500))
		ctxBuilder.WriteString("\n")
	}

	system := `You are a senior software engineering planner for a pure coding workbench.
Break complex coding requests into an ordered execution plan of concrete steps.

Rules:
- Output 2-6 steps only.
- Prefer JSON when possible (see schema). Markdown T1: headings are also accepted.
- Each step must be implementable by a coding agent with file/shell tools.
- Order steps so dependencies are satisfied (explore → implement → verify).
- Keep titles short (<= 40 chars). Descriptions actionable and specific.
- Do NOT write code. Planning only.
- If the request is already a single trivial change, return exactly one step.

Preferred JSON schema:
{"steps":[{"title":"...","description":"...","depends_on":[1]}]}
depends_on uses 1-based step indices and is optional.

Alternatively Markdown:
### T1: title
描述: ...
### T2: title
描述: ...
依赖: T1
...`

	raw := h.callLightweightLLM(cfg, system, ctxBuilder.String(), 45)
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	tasks = parseCodingWorkbenchPlan(raw)
	if len(tasks) == 0 {
		return "", nil
	}
	return formatCodingWorkbenchPlanMarkdown(userText, tasks), tasks
}

type codingWorkbenchPlanJSON struct {
	Steps []codingWorkbenchPlanStepJSON `json:"steps"`
	Tasks []codingWorkbenchPlanStepJSON `json:"tasks"` // alias
}

type codingWorkbenchPlanStepJSON struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	DependsOn   []int  `json:"depends_on"`
}

func parseCodingWorkbenchPlan(raw string) []*v2.TaskItem {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip fenced code if present.
	if i := strings.Index(raw, "```"); i >= 0 {
		rest := raw[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			raw = strings.TrimSpace(rest[:j])
		}
	}
	// Try JSON object / array.
	if tasks := parseCodingWorkbenchPlanJSON(raw); len(tasks) > 0 {
		return tasks
	}
	// Markdown / T1 list via shared parser.
	if tasks := v2.ParseTaskList(raw); len(tasks) > 0 {
		return sanitizeParsedCodingTasks(tasks)
	}
	// Fallback: numbered lines (allow bullets from LLM output).
	return parseCodingWorkbenchPlanNumbered(raw, true)
}

func parseCodingWorkbenchPlanJSON(raw string) []*v2.TaskItem {
	// Object with steps/tasks.
	var obj codingWorkbenchPlanJSON
	if err := json.Unmarshal([]byte(raw), &obj); err == nil {
		steps := obj.Steps
		if len(steps) == 0 {
			steps = obj.Tasks
		}
		if len(steps) > 0 {
			return stepsJSONToTasks(steps)
		}
	}
	// Bare array.
	var arr []codingWorkbenchPlanStepJSON
	if err := json.Unmarshal([]byte(raw), &arr); err == nil && len(arr) > 0 {
		return stepsJSONToTasks(arr)
	}
	// Find embedded JSON (only recurse when the slice is a proper substring).
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			sub := raw[i : j+1]
			if sub != raw {
				return parseCodingWorkbenchPlanJSON(sub)
			}
		}
	}
	if i := strings.Index(raw, "["); i >= 0 {
		if j := strings.LastIndex(raw, "]"); j > i {
			sub := raw[i : j+1]
			if sub != raw {
				return parseCodingWorkbenchPlanJSON(sub)
			}
		}
	}
	return nil
}

func stepsJSONToTasks(steps []codingWorkbenchPlanStepJSON) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(steps))
	for _, s := range steps {
		title := strings.TrimSpace(s.Title)
		desc := strings.TrimSpace(s.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = truncateRunesV2(desc, 40)
		}
		if desc == "" {
			desc = title
		}
		deps := make([]int, 0, len(s.DependsOn))
		for _, d := range s.DependsOn {
			if d > 0 && d <= len(steps) {
				deps = append(deps, d)
			}
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			DependsOn:   deps,
		})
	}
	return out
}

func sanitizeParsedCodingTasks(tasks []*v2.TaskItem) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		desc := strings.TrimSpace(t.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("步骤 %d", len(out)+1)
		}
		if desc == "" {
			desc = title
		}
		// Preserve DependsOn; finalizeCodingWorkbenchTasks reindexes/clamps.
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			Files:       t.Files,
			DependsOn:   append([]int(nil), t.DependsOn...),
		})
	}
	return out
}

// finalizeCodingWorkbenchTasks reindexes 1..N, clamps deps, injects overall
// request context, and chains sequential depends_on when the planner omitted them
// (so a failed early step skips later work in TaskRunner).
func finalizeCodingWorkbenchTasks(tasks []*v2.TaskItem, userText string) []*v2.TaskItem {
	out := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		desc := strings.TrimSpace(t.Description)
		if title == "" && desc == "" {
			continue
		}
		if title == "" {
			title = fmt.Sprintf("步骤 %d", len(out)+1)
		}
		if desc == "" {
			desc = title
		}
		// Compact overall request footer (avoid duplicating the full user blob).
		overall := truncateRunesV2(strings.TrimSpace(userText), 400)
		if overall != "" && !strings.Contains(desc, overall) && !strings.Contains(desc, "## Overall request") {
			desc = desc + "\n\n## Overall request\n" + overall
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       title,
			Description: desc,
			Files:       append([]string(nil), t.Files...),
			DependsOn:   append([]int(nil), t.DependsOn...),
		})
	}
	n := len(out)
	if n == 0 {
		return out
	}
	// Remap/clamp depends_on to current 1..N indices; drop self-deps.
	anyDeps := false
	for _, t := range out {
		if len(t.DependsOn) > 0 {
			anyDeps = true
			break
		}
	}
	if !anyDeps && n >= 2 {
		// Default sequential chain: T2 depends on T1, …
		for i := 1; i < n; i++ {
			out[i].DependsOn = []int{out[i-1].Index}
		}
	} else {
		for _, t := range out {
			if len(t.DependsOn) == 0 {
				continue
			}
			deps := make([]int, 0, len(t.DependsOn))
			seen := map[int]bool{}
			for _, d := range t.DependsOn {
				// Only earlier steps — prevents cycles and forward-deps that never run.
				if d < 1 || d >= t.Index || d > n || seen[d] {
					continue
				}
				seen[d] = true
				deps = append(deps, d)
			}
			t.DependsOn = deps
		}
		// Steps with empty deps after the first still chain to previous so a
		// mid-plan failure cannot silently run independent later steps.
		for i := 1; i < n; i++ {
			if len(out[i].DependsOn) == 0 {
				out[i].DependsOn = []int{out[i-1].Index}
			}
		}
	}
	return out
}

// softenExploreOnlyPlanDeps removes sequential chain deps between consecutive
// explore/read-only steps so TaskRunner can schedule them in a parallel wave.
// Implement/verify steps keep their depends_on chain.
func softenExploreOnlyPlanDeps(tasks []*v2.TaskItem) []*v2.TaskItem {
	if len(tasks) < 2 {
		return tasks
	}
	isExplore := func(t *v2.TaskItem) bool {
		if t == nil {
			return false
		}
		// Prefer title only: Description often includes "## Overall request" with
		// implement/build words from the user goal that would false-negative.
		title := strings.ToLower(strings.TrimSpace(t.Title))
		desc := strings.ToLower(strings.TrimSpace(t.Description))
		if i := strings.Index(desc, "\n\n## overall request"); i >= 0 {
			desc = strings.TrimSpace(desc[:i])
		}
		blob := title + " " + desc
		// Exclude implement/verify keywords first.
		for _, kw := range []string{
			"implement", "实现", "编码", "fix", "修复", "write", "edit",
			"verify", "test", "build", "验证", "测试", "构建", "编译", "验收",
		} {
			if strings.Contains(blob, kw) {
				return false
			}
		}
		for _, kw := range []string{
			"explor", "探查", "定位", "map ", "read", "阅读", "survey", "定位代码",
			"了解", "分析现状", "inspect", "locate",
		} {
			if strings.Contains(blob, kw) {
				return true
			}
		}
		return false
	}
	for i := 1; i < len(tasks); i++ {
		if tasks[i] == nil || tasks[i-1] == nil {
			continue
		}
		if !isExplore(tasks[i]) || !isExplore(tasks[i-1]) {
			continue
		}
		// Only drop pure sequential single-dep on previous explore step.
		if len(tasks[i].DependsOn) == 1 && tasks[i].DependsOn[0] == tasks[i-1].Index {
			tasks[i].DependsOn = nil
		}
	}
	return tasks
}

// parseCodingWorkbenchPlanNumbered extracts ordered steps from numbered lines.
// allowBullets: LLM plans may use "- step"; user-authored plans should not
// (false) so ordinary bullet lists are not treated as execution plans.
func parseCodingWorkbenchPlanNumbered(raw string, allowBullets bool) []*v2.TaskItem {
	lines := strings.Split(raw, "\n")
	var out []*v2.TaskItem
	pat := `^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：])\s+(.+)$`
	if allowBullets {
		pat = `^\s*(?:\d+[\.\)]|[Tt]\d+\s*[:：]|[-*•])\s+(.+)$`
	}
	re := regexp.MustCompile(pat)
	for _, line := range lines {
		m := re.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		title := strings.TrimSpace(m[1])
		if title == "" {
			continue
		}
		out = append(out, &v2.TaskItem{
			Index:       len(out) + 1,
			Title:       truncateRunesV2(title, 80),
			Description: title,
		})
	}
	return out
}

func formatCodingWorkbenchPlanMarkdown(userText string, tasks []*v2.TaskItem) string {
	var b strings.Builder
	b.WriteString("**目标**: ")
	b.WriteString(truncateRunesV2(strings.TrimSpace(userText), 200))
	b.WriteString("\n\n")
	for _, t := range tasks {
		if t == nil {
			continue
		}
		title := strings.TrimSpace(t.Title)
		b.WriteString(fmt.Sprintf("### T%d: %s\n", t.Index, title))
		if d := planStepDescriptionForDisplay(t.Description, title); d != "" {
			b.WriteString("描述: ")
			b.WriteString(d)
			b.WriteString("\n")
		}
		if len(t.DependsOn) > 0 {
			b.WriteString("依赖: ")
			for i, d := range t.DependsOn {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(fmt.Sprintf("T%d", d))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// planStepDescriptionForDisplay strips the injected Overall request footer so
// the auto-plan UI stays compact (execution still uses full Description).
func planStepDescriptionForDisplay(desc, title string) string {
	desc = strings.TrimSpace(desc)
	if desc == "" || desc == title {
		return ""
	}
	if i := strings.Index(desc, "\n\n## Overall request"); i >= 0 {
		desc = strings.TrimSpace(desc[:i])
	}
	if desc == "" || desc == title {
		return ""
	}
	return truncateRunesV2(desc, 300)
}

// codingWorkbenchRunHeader summarizes multi-step TaskRunner outcomes for the user.
func codingWorkbenchRunHeader(planned bool, stepCount int, results []v2.TaskRunResult) string {
	if len(results) == 0 {
		return "编码未完成"
	}
	if !planned || stepCount <= 1 {
		if results[0].Status == v2.TaskFailed {
			return "编码未完成"
		}
		if results[0].Status == v2.TaskSkipped {
			return "编码已取消/跳过"
		}
		return "编码完成"
	}
	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		switch r.Status {
		case v2.TaskPassed:
			passed++
		case v2.TaskFailed:
			failed++
		case v2.TaskSkipped:
			skipped++
		}
	}
	switch {
	case passed == 0 && failed == 0 && skipped == 0:
		return "编码未完成"
	case failed == 0 && skipped == 0 && passed > 0:
		return fmt.Sprintf("编码完成（已按 %d 步计划执行）", stepCount)
	case failed == 0 && skipped > 0 && passed > 0:
		return fmt.Sprintf("编码部分完成（%d/%d 步通过，%d 步跳过）", passed, stepCount, skipped)
	case passed == 0 && (failed > 0 || skipped > 0):
		return fmt.Sprintf("编码未完成（计划 %d 步，通过 %d）", stepCount, passed)
	default:
		return fmt.Sprintf("编码部分完成（%d/%d 步通过，%d 失败，%d 跳过）", passed, stepCount, failed, skipped)
	}
}

// setStickyCodingExecutionPlan stores the multi-step plan for continuity/banner.
func (h *IMMessageHandler) setStickyCodingExecutionPlan(userID, plan string) {
	h.persistCodingWorkbenchPlans(userID, plan, "")
}

// clearStickyCodingExecutionPlan drops a stale multi-step plan (e.g. after a
// simple single-task turn so the UI banner does not keep showing old steps).
func (h *IMMessageHandler) clearStickyCodingExecutionPlan(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.ExecutionPlan = ""
	})
}

// persistCodingWorkbenchPlans writes ExecutionPlan and optionally seeds SessionPlan
// in one sticky disk write.
func (h *IMMessageHandler) persistCodingWorkbenchPlans(userID, executionPlan, sessionPlanIfEmpty string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		if ep := truncateRunesForSubAgent(strings.TrimSpace(executionPlan), 2000); ep != "" {
			mem.ExecutionPlan = ep
		}
		if seed := truncateRunesForSubAgent(strings.TrimSpace(sessionPlanIfEmpty), 800); seed != "" {
			if strings.TrimSpace(mem.SessionPlan) == "" {
				mem.SessionPlan = seed
			}
		}
	})
}
