package guiapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

var hopSpecHeadingPattern = regexp.MustCompile(`(?m)^[ \t]*# Spec[:：]`)

func (a *App) CompileHopSpec(task string) (string, error) {
	return a.compileHopSpecWithIdentity(hopJITExecutionIdentity{}, task)
}

func (a *App) compileHopSpecWithIdentity(identity hopJITExecutionIdentity, task string) (string, error) {
	return compileHopSpec(context.Background(), task, a.newHopJITCallerWithIdentity(identity))
}

func (a *App) RunHopTask(task string) (hopjit.ReuseResult, error) {
	return a.runHopTaskWithIdentity(hopJITExecutionIdentity{}, task)
}

func (a *App) runHopTaskWithIdentity(identity hopJITExecutionIdentity, task string) (hopjit.ReuseResult, error) {
	store, err := a.openHopJITPersistence()
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	return runHopTaskWithPersistenceManaged(context.Background(), store, task, a.newHopJITCallerWithIdentity(identity), &a.hopJITRuns)
}

func runHopTaskWithPersistence(ctx context.Context, store hopjit.PersistenceProvider, task string, caller *guiHopJITCaller) (hopjit.ReuseResult, error) {
	return runHopTaskWithPersistenceManaged(ctx, store, task, caller, nil)
}

func runHopTaskWithPersistenceManaged(ctx context.Context, store hopjit.PersistenceProvider, task string, caller *guiHopJITCaller, runs *hopJITRunState) (hopjit.ReuseResult, error) {
	spec, err := compileHopSpec(ctx, task, caller)
	if err != nil {
		return hopjit.ReuseResult{}, err
	}
	return runHopSpecWithPersistenceManaged(ctx, store, spec, nil, caller, runs)
}

func compileHopSpec(ctx context.Context, task string, caller *guiHopJITCaller) (string, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return "", errors.New("HopJIT task is empty")
	}
	if caller == nil {
		return "", errors.New("HopJIT compiler caller is unavailable")
	}
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "把用户任务编译成 HopSpec v3 Markdown。只返回规约正文，不要代码围栏。必须包含 # Spec、Goal、Outputs、## Steps；优先使用 reason、带 hop_python body 的 act、check 和 exit。不得使用 commit。所有步骤编号连续，输出必须有类型，关键结果必须有 check。不要声明需要调用方提供的 Inputs。"},
		map[string]interface{}{"role": "user", "content": task},
	}
	text, err := caller.compileText(ctx, messages)
	if err != nil {
		return "", err
	}
	spec := normalizeCompiledHopMarkdown(text, task)
	if err := validateCompiledHopSpec(spec); err != nil {
		if !compiledHopSpecShouldRetry(err) {
			return "", err
		}
		text, err = caller.compileText(ctx, compiledHopRewriteMessages(task, spec, err))
		if err != nil {
			return "", err
		}
		spec = normalizeCompiledHopMarkdown(text, task)
		if err := validateCompiledHopSpec(spec); err != nil {
			return "", err
		}
	}
	return spec, nil
}

func (c *guiHopJITCaller) compileText(ctx context.Context, messages []interface{}) (string, error) {
	if c.invoke != nil {
		return c.invoke(ctx, messages, false)
	}
	if c.app == nil {
		return "", errors.New("HopJIT LLM app is unavailable")
	}
	resp, err := doSimpleLLMRequestWithOptions(ctx, c.app.GetMaclawLLMConfig(), messages, nil, 120*time.Second, simpleLLMRequestOptions{AwaitResponse: true})
	if err != nil {
		return "", err
	}
	return hopSpecFromModelFields(resp.Content, resp.RawContent, resp.ReasoningContent), nil
}

func compiledHopRewriteMessages(task, spec string, reason error) []interface{} {
	return []interface{}{
		map[string]interface{}{"role": "system", "content": "上一份 HopSpec 无法解析。只返回规约正文，从 # Spec: 标题 开始，不要解释、不要代码围栏、不要 think 标签。必须包含 Goal、Outputs、## Steps。不得使用 commit。"},
		map[string]interface{}{"role": "user", "content": "任务:\n" + task + "\n\n上次输出:\n" + spec + "\n\n错误:\n" + reason.Error()},
	}
}

func compiledHopSpecShouldRetry(err error) bool {
	return err != nil && !strings.Contains(err.Error(), "commit is not allowed")
}

// hopSpecFromModelFields keeps a spec that the display filter removed.
// Content wins when it already contains the heading. Replan stays on call,
// which still returns display text, so an old spec in reasoning cannot replace new steps.
func hopSpecFromModelFields(content, raw, reasoning string) string {
	fields := []string{content, raw, reasoning}
	for _, field := range fields {
		if hopSpecHeadingPattern.MatchString(field) {
			return field
		}
	}
	// A Goal + Steps document with no heading is still the spec. Earlier prose
	// must not hide it, or title repair never sees the document.
	for _, field := range fields {
		if visible := stripThinkingTags(field); hopSpecHasGoalAndSteps(visible) {
			return visible
		}
		if hopSpecHasGoalAndSteps(field) {
			return field
		}
	}
	for _, field := range fields {
		if visible := stripThinkingTags(field); strings.TrimSpace(visible) != "" {
			return visible
		}
	}
	return ""
}

func normalizeCompiledHopMarkdown(text, task string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	spec := extractHopSpecDocument(text)
	if spec == "" {
		spec = stripHopJITCodeFence(text)
	}
	spec = strings.TrimSpace(spec)
	if spec == "" || hopSpecHeadingPattern.MatchString(spec) || !hopSpecHasGoalAndSteps(spec) {
		return spec
	}
	return "# Spec: " + hopSpecTitleFromTask(task) + "\n" + spec
}

func extractHopSpecDocument(text string) string {
	loc := hopSpecHeadingPattern.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	sliced := text[loc[0]:]
	if headingInsideThink(text, loc[0]) {
		if end := strings.Index(strings.ToLower(sliced), "</think>"); end >= 0 {
			sliced = sliced[:end]
		}
	}
	return cutAtBareFence(sliced)
}

func headingInsideThink(text string, headingAt int) bool {
	lower := strings.ToLower(text[:headingAt])
	open := strings.LastIndex(lower, "<think>")
	if open < 0 {
		return false
	}
	return strings.LastIndex(lower, "</think>") < open
}

// cutAtBareFence drops the markdown wrapper that closes after the heading.
// A fenced block that opened after the heading, including hop_python, stays:
// its closing fence belongs to that block, and the steps after it stay too.
// Fences quoted with ">" are body lines, not wrappers.
func cutAtBareFence(text string) string {
	lines := strings.Split(text, "\n")
	openFence := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```") {
			continue
		}
		if trimmed != "```" {
			openFence = true
			continue
		}
		if openFence {
			openFence = false
			continue
		}
		return strings.TrimSpace(strings.Join(lines[:index], "\n"))
	}
	return strings.TrimSpace(text)
}

func hopSpecHasGoalAndSteps(text string) bool {
	hasGoal := false
	hasSteps := false
	for _, line := range strings.Split(text, "\n") {
		if hopSpecGoalLine(line) {
			hasGoal = true
		}
		if hopSpecStepsHeading(line) {
			hasSteps = true
		}
	}
	return hasGoal && hasSteps
}

func hopSpecGoalLine(line string) bool {
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	line = strings.TrimLeft(line, "*_")
	line = strings.TrimSpace(line)
	for _, label := range []string{"Goal", "目标"} {
		if len(line) < len(label) {
			continue
		}
		head := line[:len(label)]
		if !strings.EqualFold(head, label) {
			continue
		}
		rest := strings.TrimSpace(line[len(label):])
		if rest == "" || strings.HasPrefix(rest, ":") || strings.HasPrefix(rest, "：") {
			return true
		}
	}
	return false
}

func hopSpecStepsHeading(line string) bool {
	line = strings.TrimSpace(line)
	line = strings.TrimRight(line, ":：")
	switch strings.ToLower(line) {
	case "## steps", "## 步骤":
		return true
	default:
		return false
	}
}

func hopSpecTitleFromTask(task string) string {
	line := strings.TrimSpace(task)
	if index := strings.IndexAny(line, "\r\n"); index >= 0 {
		line = strings.TrimSpace(line[:index])
	}
	runes := []rune(line)
	if len(runes) > 40 {
		line = string(runes[:40])
	}
	if line == "" {
		return "任务"
	}
	return line
}

func validateCompiledHopSpec(markdown string) error {
	spec, parseErrs := hopspec.Parse(markdown)
	if len(parseErrs) > 0 {
		return errors.Join(parseErrs...)
	}
	if validationErrs := hopspec.Validate(spec); len(validationErrs) > 0 {
		return errors.Join(validationErrs...)
	}
	if len(spec.Steps) == 0 {
		return errors.New("compiled HopSpec contains no steps")
	}
	for _, step := range spec.AllSteps() {
		if step.Type == hopspec.StepCommit {
			return fmt.Errorf("compiled HopSpec commit is not allowed: step %s", step.ID)
		}
	}
	return nil
}
