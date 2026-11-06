package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
)

// One leading ">" is the same title the parser accepts: "> # Spec: 天气".
var hopSpecHeadingPattern = regexp.MustCompile(`(?m)^[ \t]*>?[ \t]*# Spec[:：]`)

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
	caller.task = task
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "把用户任务编译成 HopSpec v3 Markdown。只返回规约正文，不要代码围栏。必须包含 # Spec、Goal、Outputs、## Steps。每个步骤独占一行，类型放在方括号里，例如 1. [reason] 摘要、2. [act] 摘要、3. [check] 摘要、4. [exit]。优先使用 reason、带 hop_python body 的 act、check 和 exit。不得使用 commit。所有步骤编号连续，输出必须有类型，关键结果必须有 check。每个 check 和 confirm 必须单独写一行判定：  + → accepted % bool。要读取上一步的输出，在该步写：  - ← 名字。查询天气、新闻或价格时，在写出文本结果的那个 act 上写 [act free]，并单独一行  - 工具: web_search。工具行写在别的步骤上不算。hop_python 必须把该输出赋成 web_search 或 web_fetch 的结果。右边是这次调用，或是已经查到的变量。引号中的句子、引号中的句子 or 调用、以及只在某个分支里写出的字面量，都不算。没有工具白名单的 act free 不能执行。不调用工具的 act 不要写 free。不要声明需要调用方提供的 Inputs。"},
		map[string]interface{}{"role": "user", "content": task},
	}
	text, err := caller.compileText(ctx, messages)
	if err != nil {
		return "", err
	}
	spec := prepareCompiledHopMarkdown(text, task)
	if err := compiledHopSpecError(spec, task); err != nil {
		logHopCompileRejection("first", text, spec, err)
		if !compiledHopSpecShouldRetry(err) {
			return "", err
		}
		text, err = caller.compileText(ctx, compiledHopRewriteMessages(task, spec, err))
		if err != nil {
			return "", err
		}
		spec = prepareCompiledHopMarkdown(text, task)
		if err := compiledHopSpecError(spec, task); err != nil {
			logHopCompileRejection("rewrite", text, spec, err)
			return "", err
		}
	}
	return spec, nil
}

func prepareCompiledHopMarkdown(markdown, task string) string {
	normalized := normalizeCompiledHopMarkdown(markdown, task)
	return repairLiveLookupTools(repairMissingCheckVerdicts(repairMissingGoal(normalized, task)), task)
}

func compiledHopSpecError(spec, task string) error {
	if err := validateCompiledHopSpec(spec); err != nil {
		return err
	}
	return liveLookupToolsRequired(spec, task)
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
		map[string]interface{}{"role": "system", "content": "上一份 HopSpec 无法解析。只返回规约正文，从 # Spec: 标题 开始，不要解释、不要代码围栏、不要 think 标签。必须包含 Goal、Outputs、## Steps。每个步骤独占一行，类型放在方括号里，例如 1. [reason] 摘要、2. [act] 摘要、3. [check] 摘要、4. [exit]。不得使用 commit。每个 check 和 confirm 必须单独写一行判定：  + → accepted % bool。要读取上一步的输出，在该步写：  - ← 名字。查询天气、新闻或价格时，在写出文本结果的那个 act 上写 [act free]，并单独一行  - 工具: web_search。工具行写在别的步骤上不算。hop_python 必须把该输出赋成 web_search 或 web_fetch 的结果。右边是这次调用，或是已经查到的变量。引号中的句子、引号中的句子 or 调用、以及只在某个分支里写出的字面量，都不算。没有工具白名单的 act free 不能执行。不调用工具的 act 不要写 free。"},
		map[string]interface{}{"role": "user", "content": "任务:\n" + task + "\n\n上次输出:\n" + spec + "\n\n错误:\n" + reason.Error()},
	}
}

func compiledHopSpecShouldRetry(err error) bool {
	return err != nil && !strings.Contains(err.Error(), "commit is not allowed")
}

// hopSpecFromModelFields keeps a spec that the display filter removed.
// A field that parses to steps wins, in content, raw, reasoning order. A
// heading with no steps must not hide steps the model left in reasoning.
// Replan stays on call, which still returns display text.
func hopSpecFromModelFields(content, raw, reasoning string) string {
	type hopSpecField struct {
		name string
		text string
	}
	fields := []hopSpecField{{"content", content}, {"raw", raw}, {"reasoning", reasoning}}
	for _, field := range fields {
		if chosen := hopSpecFieldWithSteps(field.text); chosen != "" {
			if field.name != "content" {
				log.Printf("[hop] spec steps taken from %s", field.name)
			}
			return chosen
		}
	}
	// No field parsed. Keep the first steps section, in the same field order,
	// so a heading stub cannot hide it and a long reasoning draft cannot
	// hide the content the model already wrote.
	for _, field := range fields {
		for _, candidate := range []string{stripThinkingTags(field.text), field.text} {
			if len(hopSpecStepsBody(candidate)) == 0 {
				continue
			}
			if field.name != "content" {
				log.Printf("[hop] spec steps section taken from %s", field.name)
			}
			return candidate
		}
	}
	for _, field := range fields {
		if hopSpecHeadingPattern.MatchString(field.text) {
			return field.text
		}
	}
	// A Goal + Steps document with no heading is still the spec. Earlier prose
	// must not hide it, or title repair never sees the document.
	for _, field := range fields {
		if visible := stripThinkingTags(field.text); hopSpecHasGoalAndSteps(visible) {
			return visible
		}
		if hopSpecHasGoalAndSteps(field.text) {
			return field.text
		}
	}
	for _, field := range fields {
		if visible := stripThinkingTags(field.text); strings.TrimSpace(visible) != "" {
			return visible
		}
	}
	return ""
}

func hopSpecFieldWithSteps(field string) string {
	field = unwrapThinkContinuations(field)
	visible := stripThinkingTags(field)
	// A visible Steps section is the spec. A bracket line outside that
	// section is a fragment and must not hide a spec still in the raw field.
	if hopSpecStepsSectionCount(visible) > 0 {
		return visible
	}
	if steps, _ := hopSpecCountedSteps(field); steps > 0 {
		return field
	}
	return ""
}

func logHopCompileRejection(stage, raw, spec string, err error) {
	steps, _ := hopSpecCountedSteps(spec)
	log.Printf("[hop] compile rejected stage=%s raw_runes=%d spec_runes=%d heading=%t steps_heading=%t parsed_steps=%t err=%v",
		stage, len([]rune(raw)), len([]rune(spec)),
		hopSpecHeadingPattern.MatchString(spec), hopSpecHasStepsHeading(spec), steps > 0, err)
}

func hopSpecHasStepsHeading(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if hopSpecStepsHeading(line) {
			return true
		}
	}
	return false
}

// repairMissingGoal runs only on the compile path. A Goal heading whose
// sentence is the next paragraph is already in spec.Goal, and that sentence
// stays. When the parsed goal is still empty, the full task is inserted.
// An empty task does not invent a goal, and a pasted spec never reaches here.
func repairMissingGoal(markdown, task string) string {
	goal := hopSpecGoalFromTask(task)
	if goal == "" {
		return markdown
	}
	spec, _ := hopspec.Parse(markdown)
	if strings.TrimSpace(spec.Goal) != "" {
		return markdown
	}
	line := "Goal: " + goal
	lines := strings.Split(markdown, "\n")
	for i, raw := range lines {
		if !hopSpecHeadingPattern.MatchString(raw) {
			continue
		}
		head := append([]string{}, lines[:i+1]...)
		tail := append([]string{}, lines[i+1:]...)
		out := append(head, line)
		return strings.Join(append(out, tail...), "\n")
	}
	return line + "\n" + markdown
}

func hopSpecGoalFromTask(task string) string {
	return strings.Join(strings.Fields(task), " ")
}

func normalizeCompiledHopMarkdown(text, task string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = unwrapThinkContinuations(text)
	spec := extractHopSpecDocument(text)
	if spec == "" {
		spec = unwrapHopSpecThink(text)
		spec = stripHopJITCodeFence(spec)
	}
	spec = strings.TrimSpace(spec)
	if spec == "" || hopSpecHeadingPattern.MatchString(spec) || !hopSpecHasGoalAndSteps(spec) {
		return spec
	}
	return "# Spec: " + hopSpecTitleFromTask(task) + "\n" + spec
}

func extractHopSpecDocument(text string) string {
	locs := hopSpecRegionLocs(text)
	if len(locs) == 0 {
		return ""
	}
	candidates := make([]string, len(locs))
	for index, loc := range locs {
		end := len(text)
		if index+1 < len(locs) {
			end = locs[index+1][0]
		}
		candidates[index] = finishHopSpecSlice(text, loc[0], end)
	}
	// A heading inside think is a draft. It does not compete with a spec
	// written outside the tag. A spec has a Steps section, or a Goal plus
	// step lines. An outside heading with only an example step does not
	// hide the spec. Among the regions that remain, more non-exit steps
	// win, then more step lines. A tie stays with the earlier region.
	if index := pickHopSpecCandidate(text, locs, candidates, true, true); index >= 0 {
		return candidates[index]
	}
	if index := pickHopSpecCandidate(text, locs, candidates, false, true); index >= 0 {
		return candidates[index]
	}
	if index := pickHopSpecCandidate(text, locs, candidates, true, false); index >= 0 {
		return candidates[index]
	}
	if index := pickHopSpecCandidate(text, locs, candidates, false, false); index >= 0 {
		return candidates[index]
	}
	// No bracket steps. Keep the first Steps body. A later longer prose
	// region must not hide it, and an empty heading must not hide a later body.
	best := 0
	for index := 1; index < len(candidates); index++ {
		if len(hopSpecStepsBody(candidates[best])) == 0 && len(hopSpecStepsBody(candidates[index])) > 0 {
			best = index
		}
	}
	return candidates[best]
}

func finishHopSpecSlice(text string, start, end int) string {
	sliced := text[start:end]
	if headingInsideThink(text, start) {
		if closeAt := strings.Index(strings.ToLower(sliced), "</think>"); closeAt >= 0 {
			inside := sliced[:closeAt]
			after := sliced[closeAt+len("</think>"):]
			// A Steps section after the closer is the spec. A longer draft
			// inside the tag must not win. A bracket line that is not in a
			// Steps section is an explanation, and the two sides are not merged.
			if keepHopSpecAfterThink(inside, after) {
				sliced = after
			} else {
				sliced = inside
			}
		}
	}
	return cutAtBareFence(sliced)
}

func pickHopSpecCandidate(text string, locs [][]int, candidates []string, outsideOnly, documents bool) int {
	best := -1
	bestSteps, bestWork := 0, 0
	for index := range candidates {
		if outsideOnly && headingInsideThink(text, locs[index][0]) {
			continue
		}
		if documents && !hopSpecRegionIsDocument(candidates[index]) {
			continue
		}
		steps, work := hopSpecCountedSteps(candidates[index])
		if steps == 0 {
			continue
		}
		if best < 0 || work > bestWork || (work == bestWork && steps > bestSteps) {
			best, bestSteps, bestWork = index, steps, work
		}
	}
	return best
}

// hopSpecRegionLocs returns # Spec headings that start a region.
// A heading inside a hop_python fence that opened after an earlier heading
// is code, not a second spec, so the steps after that fence stay with the spec.
func hopSpecRegionLocs(text string) [][]int {
	locs := hopSpecHeadingPattern.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return nil
	}
	lines := strings.Split(text, "\n")
	offset := 0
	hiding := false
	openCode := false
	next := 0
	var out [][]int
	for _, line := range lines {
		lineEnd := offset + len(line)
		for next < len(locs) && locs[next][0] >= offset && locs[next][0] <= lineEnd {
			if !hiding {
				out = append(out, locs[next])
			}
			next++
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```hop_python") {
			openCode = true
			if len(out) > 0 {
				hiding = true
			}
		} else if trimmed == "```" && openCode {
			openCode = false
			hiding = false
		}
		offset = lineEnd + 1
	}
	return out
}

// hopSpecRegionIsDocument reports whether a region is a spec rather than an
// example step under a heading. A Steps section with a bracket step qualifies.
// So does a Goal together with any bracket step, including when the model
// omitted the Steps heading.
func hopSpecRegionIsDocument(text string) bool {
	if steps, _ := hopSpecCountedSteps(text); steps == 0 {
		return false
	}
	if hopSpecStepsSectionCount(text) > 0 {
		return true
	}
	for _, line := range strings.Split(text, "\n") {
		if hopSpecGoalLine(line) {
			return true
		}
	}
	return false
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
// A bare fence is that kind of block only when the Steps section has no step
// line yet and step lines follow it. A step line before that heading does not
// end the document. A step line after a fence whose Steps section already has
// steps is an example, not more of the spec. Fences quoted with ">" are body
// lines, not wrappers.
func cutAtBareFence(text string) string {
	lines := strings.Split(text, "\n")
	interior := hopPythonInterior(lines)
	suffixSteps := make([]int, len(lines)+1)
	for index := len(lines) - 1; index >= 0; index-- {
		suffixSteps[index] = suffixSteps[index+1]
		if !interior[index] && hopspec.CountStepLines(strings.TrimSpace(lines[index])) > 0 {
			suffixSteps[index]++
		}
	}
	openFence := false
	inSteps := false
	seenStepsHeading := false
	stepsInSection := 0
	for index, line := range lines {
		if interior[index] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if hopSpecStepsHeading(trimmed) {
			inSteps = true
			seenStepsHeading = true
		} else if inSteps && strings.HasPrefix(trimmed, "##") {
			inSteps = false
		} else if inSteps && hopspec.CountStepLines(trimmed) > 0 {
			stepsInSection++
		}
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
		// Once a Steps heading exists, only step lines in that section count.
		// Before any heading, any earlier step line still ends the document.
		already := stepsInSection
		if !seenStepsHeading {
			already = suffixSteps[0] - suffixSteps[index]
		}
		if already == 0 && suffixSteps[index+1] > 0 {
			openFence = true
			continue
		}
		return strings.TrimSpace(strings.Join(lines[:index], "\n"))
	}
	return strings.TrimSpace(text)
}

// keepHopSpecAfterThink reports whether the text after a think closer is the
// spec. A Steps section there wins over a longer draft. A bracket line that
// is not in a Steps section does not replace the spec inside the tag.
func keepHopSpecAfterThink(inside, after string) bool {
	if hopSpecStepsSectionCount(after) > 0 {
		return true
	}
	insideSteps, _ := hopSpecCountedSteps(inside)
	if hopSpecStepsSectionCount(inside) > 0 || insideSteps > 0 {
		return false
	}
	afterSteps, _ := hopSpecCountedSteps(after)
	return afterSteps > 0
}

// unwrapThinkContinuations lifts step lines out of a think block when that
// block is only the rest of the list. A block that contains # Spec or a
// Steps heading stays intact so a draft cannot be merged into the spec.
func unwrapThinkContinuations(text string) string {
	lower := strings.ToLower(text)
	searchFrom := 0
	for {
		rel := strings.Index(lower[searchFrom:], "<think>")
		if rel < 0 {
			return text
		}
		open := searchFrom + rel
		restLower := lower[open+len("<think>"):]
		closeRel := strings.Index(restLower, "</think>")
		body := ""
		afterStart := len(text)
		if closeRel < 0 {
			body = text[open+len("<think>"):]
		} else {
			body = text[open+len("<think>") : open+len("<think>")+closeRel]
			afterStart = open + len("<think>") + closeRel + len("</think>")
		}
		if thinkContinuation(body) {
			replaced := text[:open] + strings.TrimSpace(body)
			if afterStart < len(text) {
				replaced += "\n" + text[afterStart:]
			}
			return unwrapThinkContinuations(replaced)
		}
		if closeRel < 0 {
			return text
		}
		searchFrom = afterStart
	}
}

func thinkContinuation(body string) bool {
	steps, _ := hopSpecCountedSteps(body)
	if steps == 0 {
		return false
	}
	return !hopSpecHeadingPattern.MatchString(body) && !hopSpecHasStepsHeading(body)
}

// unwrapHopSpecThink drops a think wrapper when the document has no # Spec
// heading yet. The Steps section is kept whether it sits inside the tag or
// after it. A fragment on the other side is not merged in.
func unwrapHopSpecThink(text string) string {
	lower := strings.ToLower(text)
	openAt := strings.Index(lower, "<think>")
	closeAt := strings.Index(lower, "</think>")
	if openAt < 0 || closeAt < openAt {
		return text
	}
	inside := text[openAt+len("<think>") : closeAt]
	after := text[closeAt+len("</think>"):]
	visible := text[:openAt] + after
	if keepHopSpecAfterThink(inside, after) {
		return visible
	}
	insideSteps, _ := hopSpecCountedSteps(inside)
	if hopSpecStepsSectionCount(inside) > 0 || insideSteps > 0 || strings.TrimSpace(visible) == "" {
		return inside
	}
	return visible
}

// hopSpecCountedSteps counts bracket step lines outside hop_python.
// A line inside that fence is code, so it cannot make another region win
// or make a later fence look like the end of the spec.
func hopSpecCountedSteps(markdown string) (steps, work int) {
	return hopspec.StepLineCounts(hopSpecOutsideCode(markdown))
}

func hopSpecOutsideCode(markdown string) string {
	lines := strings.Split(markdown, "\n")
	interior := hopPythonInterior(lines)
	kept := make([]string, 0, len(lines))
	for index, line := range lines {
		if !interior[index] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func hopPythonInterior(lines []string) []bool {
	interior := make([]bool, len(lines))
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "```hop_python") {
			continue
		}
		end := -1
		for next := index + 1; next < len(lines); next++ {
			if strings.TrimSpace(lines[next]) == "```" {
				end = next
				break
			}
		}
		// An unclosed fence is not code. The steps after it still count.
		if end < 0 {
			continue
		}
		for inner := index + 1; inner < end; inner++ {
			interior[inner] = true
		}
	}
	return interior
}

// hopSpecStepsSectionCount counts bracket step lines that sit in a Steps
// section. A step line before that heading is not part of the section.
// Lines inside hop_python are not part of the section.
func hopSpecStepsSectionCount(markdown string) int {
	markdown = hopSpecOutsideCode(markdown)
	inSteps := false
	count := 0
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if hopSpecStepsHeading(trimmed) {
			inSteps = true
			continue
		}
		if !inSteps {
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			break
		}
		if hopspec.CountStepLines(trimmed) > 0 {
			count++
		}
	}
	return count
}

func compiledHopSpecNoStepsError(markdown string) error {
	const shape = "each step must be a numbered line like 1. [act] 摘要, ending with [exit]"
	lines := hopSpecStepsBody(markdown)
	if len(lines) == 0 {
		return errors.New("compiled HopSpec contains no steps: " + shape)
	}
	if len(lines) > 4 {
		lines = lines[:4]
	}
	return errors.New("compiled HopSpec contains no steps: " + strings.Join(lines, " | ") + " — " + shape)
}

func hopSpecStepsBody(markdown string) []string {
	markdown = hopSpecOutsideCode(markdown)
	inSteps := false
	var lines []string
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if hopSpecStepsHeading(trimmed) {
			inSteps = true
			continue
		}
		if !inSteps || trimmed == "" || strings.HasPrefix(trimmed, "```") {
			continue
		}
		if strings.HasPrefix(trimmed, "##") {
			break
		}
		lines = append(lines, trimmed)
	}
	return lines
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
	return hopspec.SectionHeading(line) == "goal"
}

func hopSpecStepsHeading(line string) bool {
	return hopspec.SectionHeading(line) == "steps"
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

// repairLiveLookupTools makes every text output of a weather, news, or price
// task a retrieved value. The engine runs a hop_python body instead of the
// tool agent, so a body that does not assign the output from web_search or
// web_fetch is removed and that same act lists the tool. A call the engine
// cannot evaluate is removed the same way: keyword equals, a positional tool
// call, or a name that is not already assigned. A body that already assigns
// the output from a call the engine can run keeps the body and gains the
// missing tool line. A body that copies an earlier retrieved input gains the
// missing "- ← name" line and does not search again. Each uncovered output is
// repaired on the act that writes it. A tool line on another step does not
// count. The engine does not run a reason body, so a reason step that writes
// the text output becomes an act. A later check, confirm, or ask that repeats
// that text output and does not retrieve it loses the output line, so the
// host fill cannot replace the retrieval. The bool line stays. A check,
// confirm, or ask that is the only writer of that text output loses the line
// when an earlier reason or act can publish it. The line moves onto that
// step, a reason becomes an act, and that act lists web_search. An earlier
// act that already lists another tool is not the destination. A check whose
// body calls web_search or web_fetch keeps the line, because that call still
// runs. When no earlier reason or act can take the line, one rewrite still
// runs. With no text spec output, the lookup acts themselves are repaired
// the same way.
func repairLiveLookupTools(markdown, task string) string {
	if !hopLiveLookupTask(task) {
		return markdown
	}
	spec, _ := hopspec.Parse(markdown)
	if liveLookupCovered(spec) {
		return markdown
	}
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	var reps []hopSpanReplace
	if len(textOutputNames(spec)) > 0 {
		reps = trackedLookupRepairs(spec, lines)
	} else {
		reps = untrackedLookupRepairs(spec, lines)
	}
	if len(reps) == 0 {
		return markdown
	}
	return applyHopSpanReplacements(lines, reps)
}

type hopSpanReplace struct {
	start int
	end   int
	lines []string
}

func trackedLookupRepairs(spec hopspec.SpecAST, lines []string) []hopSpanReplace {
	wanted := textOutputNames(spec)
	final := retrievedNames(spec)
	repairable := map[string]hopspec.Step{}
	for _, step := range spec.AllSteps() {
		if step.Type != hopspec.StepAct && step.Type != hopspec.StepReason {
			continue
		}
		for _, out := range step.Outputs {
			if wanted[out.Name] && final[out.Name] == "" {
				repairable[out.Name] = step
			}
		}
	}
	producers := map[string]bool{}
	for _, step := range repairable {
		producers[step.ID] = true
	}
	moves, moving := hostLookupMoves(spec, lines, repairable, wanted, final)
	prior := map[string]string{}
	var reps []hopSpanReplace
	for _, step := range spec.AllSteps() {
		if producers[step.ID] || len(moves[step.ID]) > 0 {
			names := uncoveredOutputNames(step, wanted, final)
			// nil means every text output. An empty list means only the lines
			// moved onto this step, so a covered output stays out of the check.
			if len(moves[step.ID]) > 0 && names == nil {
				names = []string{}
			}
			if rep, virtual, ok := repairLookupAct(lines, step, prior, names, moves[step.ID]); ok {
				reps = append(reps, rep)
				prior = retrieveStep(prior, virtual)
				continue
			}
		}
		if rep, virtual, ok := stripRepeatedLookupOutputs(lines, step, prior, wanted, final, repairable, moving); ok {
			reps = append(reps, rep)
			prior = retrieveStep(prior, virtual)
			continue
		}
		prior = retrieveStep(prior, step)
	}
	return reps
}

func untrackedLookupRepairs(spec hopspec.SpecAST, lines []string) []hopSpanReplace {
	steps := spec.AllSteps()
	matched := map[string]bool{}
	var actCount int
	var onlyID string
	var onlyLive bool
	prior := map[string]string{}
	for _, step := range steps {
		prior = retrieveStep(prior, step)
		if step.Type != hopspec.StepAct || step.Source.LineEnd < step.Source.LineStart || step.Source.LineStart < 1 {
			continue
		}
		actCount++
		onlyID = step.ID
		onlyLive = actPublishesLive(step, prior)
		if (hopLiveLookupTask(step.Summary) || hopLiveLookupTask(step.Instruction)) && !onlyLive {
			matched[step.ID] = true
		}
	}
	if len(matched) == 0 && actCount == 1 && !onlyLive {
		matched[onlyID] = true
	}
	prior = map[string]string{}
	var reps []hopSpanReplace
	for _, step := range steps {
		if matched[step.ID] {
			if rep, virtual, ok := repairLookupAct(lines, step, prior, nil, nil); ok {
				reps = append(reps, rep)
				prior = retrieveStep(prior, virtual)
				continue
			}
		}
		prior = retrieveStep(prior, step)
	}
	return reps
}

// stripRepeatedLookupOutputs removes a text output from a later check,
// confirm, or ask. Those steps publish every declared output from the host,
// so a repeated name replaces the retrieval. A body that already republishes
// the retrieved input stays. The bool verdict stays.
func stripRepeatedLookupOutputs(lines []string, step hopspec.Step, prior map[string]string, wanted map[string]bool, final map[string]string, repairable map[string]hopspec.Step, moving map[string]bool) (hopSpanReplace, hopspec.Step, bool) {
	if !hostFillsLookupText(step) || step.Source.LineEnd < step.Source.LineStart || step.Source.LineStart < 1 || step.Source.LineEnd > len(lines) {
		return hopSpanReplace{}, step, false
	}
	keptLive := retrieveStep(prior, step)
	names := map[string]bool{}
	for _, out := range step.Outputs {
		writer, ok := repairable[out.Name]
		earlierWriter := ok && writer.Source.LineStart < step.Source.LineStart
		if !wanted[out.Name] || final[out.Name] != "" || keptLive[out.Name] != "" || (!earlierWriter && !moving[out.Name]) {
			continue
		}
		names[out.Name] = true
	}
	if len(names) == 0 {
		return hopSpanReplace{}, step, false
	}
	raw := lines[step.Source.LineStart-1 : step.Source.LineEnd]
	kept := make([]string, 0, len(raw))
	skipping := false
	removed := false
	for _, line := range raw {
		if skipping {
			if opener, closer := hopPythonFenceLine(line); closer && !opener {
				skipping = false
			}
			kept = append(kept, line)
			continue
		}
		if opener, _ := hopPythonFenceLine(line); opener {
			skipping = true
			kept = append(kept, line)
			continue
		}
		if name, ok := hopStepOutputName(line); ok && names[name] {
			removed = true
			continue
		}
		kept = append(kept, line)
	}
	if !removed {
		return hopSpanReplace{}, step, false
	}
	virtual := step
	outputs := make([]hopspec.VariableDecl, 0, len(step.Outputs))
	for _, out := range step.Outputs {
		if names[out.Name] {
			continue
		}
		outputs = append(outputs, out)
	}
	virtual.Outputs = outputs
	return hopSpanReplace{
		start: step.Source.LineStart,
		end:   step.Source.LineEnd,
		lines: kept,
	}, virtual, true
}

func hostFillsLookupText(step hopspec.Step) bool {
	switch step.Type {
	case hopspec.StepCheck, hopspec.StepConfirm, hopspec.StepAsk:
		return true
	default:
		return false
	}
}

type movedLookupOutput struct {
	name string
	line string
	decl hopspec.VariableDecl
}

// hostLookupMoves moves an uncovered text output off the check, confirm, or
// ask that is its only writer. The host would fill that step from the model
// or a person. An earlier reason or act publishes it instead, and the act
// repair lists web_search. A step that already lists another tool is skipped:
// adding web_search there leaves the host search off. A body that calls
// web_search or web_fetch stays, because removing the output would still run
// that call. With no earlier owner, the name is left for one rewrite.
func hostLookupMoves(spec hopspec.SpecAST, lines []string, repairable map[string]hopspec.Step, wanted map[string]bool, final map[string]string) (map[string][]movedLookupOutput, map[string]bool) {
	steps := spec.AllSteps()
	moves := map[string][]movedLookupOutput{}
	moving := map[string]bool{}
	for _, decl := range spec.Outputs {
		name := decl.Name
		if !wanted[name] || final[name] != "" {
			continue
		}
		if _, ok := repairable[name]; ok {
			continue
		}
		writers := hostWritersOf(steps, name)
		if len(writers) == 0 || hostBodyCallsRetrieval(writers) {
			continue
		}
		last := writers[0]
		for _, step := range writers[1:] {
			if step.Source.LineStart >= last.Source.LineStart {
				last = step
			}
		}
		dest, ok := lookupTextDestination(steps, last)
		if !ok {
			continue
		}
		item := movedLookupOutput{name: name, decl: outputDeclNamed(last, name)}
		if line, ok := lookupOutputSourceLine(lines, last, name); ok {
			item.line = line
		} else if item.decl.Type != "" {
			item.line = "  + → " + name + " % " + item.decl.Type
		} else {
			item.line = "  + → " + name + " % text"
		}
		moves[dest.ID] = append(moves[dest.ID], item)
		moving[name] = true
	}
	return moves, moving
}

func hostWritersOf(steps []hopspec.Step, name string) []hopspec.Step {
	var out []hopspec.Step
	seen := map[string]bool{}
	for _, step := range steps {
		if seen[step.ID] || !hostFillsLookupText(step) || !stepWritesOutput(step, name) {
			continue
		}
		seen[step.ID] = true
		out = append(out, step)
	}
	return out
}

func stepWritesOutput(step hopspec.Step, name string) bool {
	for _, out := range step.Outputs {
		if out.Name == name {
			return true
		}
	}
	return false
}

func hostBodyCallsRetrieval(steps []hopspec.Step) bool {
	for _, step := range steps {
		if hopPythonCalls(step.Body, "web_search") || hopPythonCalls(step.Body, "web_fetch") {
			return true
		}
	}
	return false
}

// lookupTextDestination is the last earlier reason or act that can publish
// the text. A step whose outputs can all hold text wins over a later step
// that also declares a bool or number, so the host can search once. An act
// that lists a tool other than web_search is not a destination.
func lookupTextDestination(steps []hopspec.Step, before hopspec.Step) (hopspec.Step, bool) {
	var last, lastText hopspec.Step
	var have, haveText bool
	for _, step := range steps {
		if step.Source.LineStart < 1 || step.Source.LineStart >= before.Source.LineStart || !stepCanOwnLookupText(step) {
			continue
		}
		if !have || step.Source.LineStart >= last.Source.LineStart {
			last = step
			have = true
		}
		if stepOutputsHoldText(step) && (!haveText || step.Source.LineStart >= lastText.Source.LineStart) {
			lastText = step
			haveText = true
		}
	}
	if haveText {
		return lastText, true
	}
	return last, have
}

func stepCanOwnLookupText(step hopspec.Step) bool {
	if (step.Type != hopspec.StepReason && step.Type != hopspec.StepAct) || step.Source.LineEnd < step.Source.LineStart || step.Source.LineStart < 1 {
		return false
	}
	for _, tool := range step.Tools {
		if tool != "web_search" {
			return false
		}
	}
	return true
}

func stepOutputsHoldText(step hopspec.Step) bool {
	for _, out := range step.Outputs {
		if out.Name != "" && !actFreeRetrievalStringType(out.Type) {
			return false
		}
	}
	return true
}

func outputDeclNamed(step hopspec.Step, name string) hopspec.VariableDecl {
	for _, out := range step.Outputs {
		if out.Name == name {
			return out
		}
	}
	return hopspec.VariableDecl{Name: name, Type: "text"}
}

func lookupOutputSourceLine(lines []string, step hopspec.Step, name string) (string, bool) {
	if step.Source.LineEnd < step.Source.LineStart || step.Source.LineStart < 1 || step.Source.LineEnd > len(lines) {
		return "", false
	}
	skipping := false
	for _, line := range lines[step.Source.LineStart-1 : step.Source.LineEnd] {
		if skipping {
			if opener, closer := hopPythonFenceLine(line); closer && !opener {
				skipping = false
			}
			continue
		}
		if opener, _ := hopPythonFenceLine(line); opener {
			skipping = true
			continue
		}
		if got, ok := hopStepOutputName(line); ok && got == name {
			return line, true
		}
	}
	return "", false
}

func hopStepOutputName(line string) (string, bool) {
	value, ok := hopMarkedValue(strings.TrimSpace(line), []string{"+", "＋"}, []string{"→", "->"})
	if !ok {
		return "", false
	}
	if before, _, found := strings.Cut(value, "#"); found {
		value = strings.TrimSpace(before)
	}
	name := value
	if before, _, found := strings.Cut(value, "%"); found {
		name = strings.TrimSpace(before)
	} else {
		for _, colon := range []string{":", "："} {
			if before, _, found := strings.Cut(value, colon); found {
				name = strings.TrimSpace(before)
				break
			}
		}
	}
	if name == "" {
		return "", false
	}
	return name, true
}

func hopMarkedValue(trimmed string, marks, arrows []string) (string, bool) {
	for _, mark := range marks {
		if !strings.HasPrefix(trimmed, mark) {
			continue
		}
		rest := strings.TrimSpace(trimmed[len(mark):])
		for _, arrow := range arrows {
			if strings.HasPrefix(rest, arrow) {
				return strings.TrimSpace(rest[len(arrow):]), true
			}
		}
		return "", false
	}
	return "", false
}

func uncoveredOutputNames(step hopspec.Step, wanted map[string]bool, final map[string]string) []string {
	var names []string
	seen := map[string]bool{}
	for _, out := range step.Outputs {
		if !wanted[out.Name] || final[out.Name] != "" || seen[out.Name] {
			continue
		}
		seen[out.Name] = true
		names = append(names, out.Name)
	}
	return names
}

// repairLookupAct makes this act publish a retrieved value.
// A nil names list uses the act's text outputs, then every assignment in the body.
// The returned step is the act after this edit, so a later copy in the same
// pass can read the value this repair just retrieved.
func repairLookupAct(lines []string, step hopspec.Step, prior map[string]string, names []string, moved []movedLookupOutput) (hopSpanReplace, hopspec.Step, bool) {
	promote := step.Type == hopspec.StepReason
	if (step.Type != hopspec.StepAct && !promote) || step.Source.LineEnd < step.Source.LineStart || step.Source.LineStart < 1 || step.Source.LineEnd > len(lines) {
		return hopSpanReplace{}, step, false
	}
	if names == nil {
		names = stepTextOutputNames(step)
	}
	for _, item := range moved {
		names = appendLookupName(names, item.name)
	}
	body := strings.TrimSpace(step.Body)
	dropBody := false
	var inputs []string
	var tools []string
	switch {
	case body != "":
		if bound, ok := hopRetrieval(body, nil, names); ok {
			tools = undeclaredCallTools(step, body, bound, names)
			break
		}
		if missing, ok := missingCopyInputs(step, prior, names); ok {
			inputs = missing
			if bound, ok := hopRetrieval(body, prior, names); ok {
				tools = undeclaredCallTools(step, body, bound, names)
			}
			break
		}
		dropBody = true
		if tool := lookupToolToDeclare(step); tool != "" {
			tools = append(tools, tool)
		}
	default:
		if tool := lookupToolToDeclare(step); tool != "" {
			tools = append(tools, tool)
		}
	}
	if !dropBody && len(tools) == 0 && len(inputs) == 0 && !promote && len(moved) == 0 {
		return hopSpanReplace{}, step, false
	}
	virtual := previewLookupStep(step, dropBody, inputs, tools)
	if promote {
		virtual.Type = hopspec.StepAct
	}
	virtual.Outputs = appendMovedLookupDecls(virtual.Outputs, moved)
	replaced := liveLookupActLines(lines[step.Source.LineStart-1:step.Source.LineEnd], dropBody, inputs, tools)
	replaced = appendMovedLookupLines(replaced, moved)
	if promote && len(replaced) > 0 {
		replaced[0] = promoteReasonToAct(replaced[0])
	}
	return hopSpanReplace{
		start: step.Source.LineStart,
		end:   step.Source.LineEnd,
		lines: replaced,
	}, virtual, true
}

func appendLookupName(names []string, name string) []string {
	if name == "" {
		return names
	}
	for _, have := range names {
		if have == name {
			return names
		}
	}
	return append(names, name)
}

func appendMovedLookupDecls(outputs []hopspec.VariableDecl, moved []movedLookupOutput) []hopspec.VariableDecl {
	next := make([]hopspec.VariableDecl, 0, len(outputs)+len(moved))
	have := map[string]bool{}
	for _, out := range outputs {
		next = append(next, out)
		have[out.Name] = true
	}
	for _, item := range moved {
		if item.name == "" || have[item.name] {
			continue
		}
		have[item.name] = true
		decl := item.decl
		if decl.Name == "" {
			decl.Name = item.name
		}
		next = append(next, decl)
	}
	return next
}

func appendMovedLookupLines(lines []string, moved []movedLookupOutput) []string {
	have := map[string]bool{}
	for _, line := range lines {
		if name, ok := hopStepOutputName(line); ok {
			have[name] = true
		}
	}
	for _, item := range moved {
		if item.name == "" || item.line == "" || have[item.name] {
			continue
		}
		have[item.name] = true
		lines = append(lines, item.line)
	}
	return lines
}

// promoteReasonToAct rewrites the step bracket. The engine runs hop_python
// and the tool agent on an act. A reason step never runs its body.
func promoteReasonToAct(line string) string {
	start, end, ok := bracketInner(line)
	if !ok {
		return line
	}
	inner := line[start:end]
	pad := len(inner) - len(strings.TrimLeft(inner, " \t"))
	token := strings.TrimLeft(inner, " \t")
	if cut := strings.IndexAny(token, " \t"); cut >= 0 {
		token = token[:cut]
	}
	switch strings.ToLower(token) {
	case "reason", "推理":
	default:
		return line
	}
	return line[:start+pad] + "act" + line[start+pad+len(token):]
}

func bracketInner(line string) (int, int, bool) {
	pairs := []struct{ open, close string }{{"[", "]"}, {"［", "］"}, {"【", "】"}}
	best := -1
	open, close := "", ""
	for _, pair := range pairs {
		at := strings.Index(line, pair.open)
		if at >= 0 && (best < 0 || at < best) {
			best, open, close = at, pair.open, pair.close
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	inner := best + len(open)
	rel := strings.Index(line[inner:], close)
	if rel < 0 {
		return 0, 0, false
	}
	return inner, inner + rel, true
}

func previewLookupStep(step hopspec.Step, dropBody bool, inputs, tools []string) hopspec.Step {
	if dropBody {
		step.Body = ""
	}
	if len(inputs) > 0 {
		step.Inputs = append(append([]string{}, step.Inputs...), inputs...)
	}
	if len(tools) > 0 {
		step.Tools = append(append([]string{}, step.Tools...), tools...)
	}
	return step
}

func undeclaredCallTools(step hopspec.Step, body string, bound map[string]string, names []string) []string {
	called := map[string]bool{}
	if hopPythonCalls(body, "web_search") {
		called["web_search"] = true
	}
	if hopPythonCalls(body, "web_fetch") {
		called["web_fetch"] = true
	}
	var tools []string
	for _, tool := range retrievalTools(bound, names) {
		if called[tool] && !hopStepListsTool(step, tool) {
			tools = append(tools, tool)
		}
	}
	return tools
}

func lookupToolToDeclare(step hopspec.Step) string {
	if hopStepListsTool(step, "web_search") || hopStepListsTool(step, "web_fetch") {
		return ""
	}
	return "web_search"
}

func missingCopyInputs(step hopspec.Step, prior map[string]string, names []string) ([]string, bool) {
	if len(names) == 0 || strings.TrimSpace(step.Body) == "" {
		return nil, false
	}
	// The binder keeps names it was given, so a name already in prior is not
	// a copy. The body has to assign each output from a retrieved input.
	if _, ok := hopRetrieval(step.Body, prior, names); !ok {
		return nil, false
	}
	statements, ok := parseHopBindBody(step.Body)
	if !ok {
		return nil, false
	}
	assigned := map[string]bool{}
	for _, name := range hopBindTargets(statements) {
		assigned[name] = true
	}
	for _, name := range names {
		if !assigned[name] {
			return nil, false
		}
	}
	have := map[string]bool{}
	for _, in := range step.Inputs {
		have[in] = true
	}
	var missing []string
	for _, read := range hopPythonReads(step.Body) {
		if prior[read] == "" || have[read] {
			continue
		}
		have[read] = true
		missing = append(missing, read)
	}
	return missing, true
}

func liveLookupActLines(raw []string, dropBody bool, inputs, tools []string) []string {
	kept := make([]string, 0, len(raw)+len(inputs)+len(tools))
	skipping := false
	for _, line := range raw {
		if skipping {
			if opener, closer := hopPythonFenceLine(line); closer && !opener {
				skipping = false
			}
			continue
		}
		if dropBody {
			if opener, _ := hopPythonFenceLine(line); opener {
				skipping = true
				continue
			}
		}
		kept = append(kept, line)
	}
	for _, name := range inputs {
		kept = append(kept, "  - ← "+name)
	}
	for _, tool := range tools {
		kept = append(kept, "  - 工具: "+tool)
	}
	return kept
}

func hopPythonFenceLine(line string) (opener, closer bool) {
	body := strings.TrimSpace(line)
	if strings.HasPrefix(body, ">") {
		body = strings.TrimSpace(strings.TrimPrefix(body, ">"))
	}
	if strings.HasPrefix(body, "```hop_python") {
		return true, false
	}
	if body == "```" {
		return false, true
	}
	return false, false
}

func applyHopSpanReplacements(lines []string, reps []hopSpanReplace) string {
	sort.Slice(reps, func(i, j int) bool { return reps[i].start > reps[j].start })
	for _, rep := range reps {
		if rep.start < 1 || rep.end < rep.start || rep.end > len(lines) {
			continue
		}
		next := make([]string, 0, len(lines)-(rep.end-rep.start+1)+len(rep.lines))
		next = append(next, lines[:rep.start-1]...)
		next = append(next, rep.lines...)
		next = append(next, lines[rep.end:]...)
		lines = next
	}
	return strings.Join(lines, "\n")
}

// liveLookupCovered reports whether every text spec output still holds a
// retrieved value at the end of the spec. A later exit that repeats the
// declaration does not write the variable. A hop_python act that assigns
// the output from an input already retrieved by an earlier act keeps that
// value. With no text spec output, every lookup act must publish a retrieved
// value. When no act is a lookup, any retrieving act counts.
func liveLookupCovered(spec hopspec.SpecAST) bool {
	wanted := textOutputNames(spec)
	if len(wanted) == 0 {
		return untrackedLookupCovered(spec)
	}
	live := retrievedNames(spec)
	for name := range wanted {
		if live[name] == "" {
			return false
		}
	}
	return true
}

func untrackedLookupCovered(spec hopspec.SpecAST) bool {
	var lookup, lookupLive int
	var anyLive bool
	prior := map[string]string{}
	for _, step := range spec.AllSteps() {
		prior = retrieveStep(prior, step)
		if step.Type != hopspec.StepAct {
			continue
		}
		live := actPublishesLive(step, prior)
		if live {
			anyLive = true
		}
		if hopLiveLookupTask(step.Summary) || hopLiveLookupTask(step.Instruction) {
			lookup++
			if live {
				lookupLive++
			}
		}
	}
	if lookup > 0 {
		return lookupLive == lookup
	}
	return anyLive
}

func actPublishesLive(step hopspec.Step, after map[string]string) bool {
	if step.Type != hopspec.StepAct {
		return false
	}
	names := stepTextOutputNames(step)
	if len(names) == 0 {
		return actRetrieves(step, nil)
	}
	for _, name := range names {
		if after[name] == "" {
			return false
		}
	}
	return true
}

// retrievedNames is the tool behind each variable after every step that
// actually writes variables. Exit, break, and continue do not. A body-less
// act with web_search or web_fetch marks its outputs retrieved. Any other
// host-filled output is not. A body marks an output retrieved when every
// path assigns it from a listed tool, or from an input that is already
// retrieved. The left side of or is that value. A quoted sentence outside
// the call is not.
func retrievedNames(spec hopspec.SpecAST) map[string]string {
	live := map[string]string{}
	for _, step := range spec.AllSteps() {
		live = retrieveStep(live, step)
	}
	return live
}

// stepRunsHopPython reports whether the engine evaluates this step's body.
// Reason, ask, and confirm never do, so a body there cannot retrieve.
func stepRunsHopPython(step hopspec.Step) bool {
	switch step.Type {
	case hopspec.StepAct, hopspec.StepCheck, hopspec.StepCommit:
		return true
	default:
		return false
	}
}

func retrieveStep(live map[string]string, step hopspec.Step) map[string]string {
	next := cloneHopBind(live)
	if !stepWritesVariables(step) {
		return next
	}
	// Reason, ask, and confirm do not run hop_python. Their body cannot retrieve.
	if strings.TrimSpace(step.Body) == "" || !stepRunsHopPython(step) {
		tool := ""
		if step.Type == hopspec.StepAct {
			switch {
			case hopStepListsTool(step, "web_search"):
				tool = "web_search"
			case hopStepListsTool(step, "web_fetch"):
				tool = "web_fetch"
			}
		}
		for _, out := range step.Outputs {
			if out.Name == "" {
				continue
			}
			if tool == "" {
				delete(next, out.Name)
				continue
			}
			next[out.Name] = tool
		}
		return next
	}
	incoming := map[string]string{}
	for _, in := range step.Inputs {
		if next[in] != "" {
			incoming[in] = next[in]
		}
	}
	bound := bodyRetrievalBindings(step.Body, incoming)
	callsSearch := hopPythonCalls(step.Body, "web_search")
	callsFetch := hopPythonCalls(step.Body, "web_fetch")
	for _, out := range step.Outputs {
		if out.Name == "" {
			continue
		}
		tool := bound[out.Name]
		needsList := tool == "web_search" && callsSearch || tool == "web_fetch" && callsFetch
		if tool == "" || (needsList && !hopStepListsTool(step, tool)) {
			delete(next, out.Name)
			continue
		}
		next[out.Name] = tool
	}
	return next
}

func stepWritesVariables(step hopspec.Step) bool {
	switch step.Type {
	case hopspec.StepReason, hopspec.StepAct, hopspec.StepCheck, hopspec.StepCommit,
		hopspec.StepConfirm, hopspec.StepAsk, hopspec.StepCall:
		return true
	default:
		return false
	}
}

func textOutputNames(spec hopspec.SpecAST) map[string]bool {
	names := map[string]bool{}
	for _, decl := range spec.Outputs {
		if decl.Name == "" || decl.Type == "bool" {
			continue
		}
		names[decl.Name] = true
	}
	return names
}

func stepTextOutputNames(step hopspec.Step) []string {
	var names []string
	seen := map[string]bool{}
	for _, out := range step.Outputs {
		if out.Name == "" || out.Type == "bool" || seen[out.Name] {
			continue
		}
		seen[out.Name] = true
		names = append(names, out.Name)
	}
	return names
}

// actRetrieves reports that this act itself obtains live text. A hop_python
// body does so only when the named outputs are assigned from web_search or
// web_fetch and that tool is listed. A later literal assignment is not a lookup.
// A nil names list uses the act's text outputs, then every assignment.
func actRetrieves(step hopspec.Step, names []string) bool {
	if step.Type != hopspec.StepAct {
		return false
	}
	if strings.TrimSpace(step.Body) == "" {
		return hopStepListsTool(step, "web_search") || hopStepListsTool(step, "web_fetch")
	}
	if names == nil {
		names = stepTextOutputNames(step)
	}
	bound, ok := hopRetrieval(step.Body, nil, names)
	if !ok {
		return false
	}
	tools := retrievalTools(bound, names)
	if len(tools) == 0 {
		return false
	}
	for _, tool := range tools {
		if !hopStepListsTool(step, tool) {
			return false
		}
	}
	return true
}

func bodyRetrievalBindings(body string, incoming map[string]string) map[string]string {
	// The step fails before it publishes when the interpreter cannot run the
	// body. An input of the same name is not the value that body would write.
	if err := hopjit.HopPythonRuns(body, hopDefinedNames(incoming)); err != nil {
		return map[string]string{}
	}
	statements, ok := parseHopBindBody(body)
	if !ok {
		return map[string]string{}
	}
	return bindHopStatements(statements, incoming)
}

// hopRetrieval is the tool behind each assignment. An empty names list requires
// every assignment to be a retrieval, so a literal next to a call does not pass.
// A non-empty list checks only those outputs. incoming holds values an earlier
// step already retrieved. The left side of or is the value hop_python keeps.
// The interpreter must be able to evaluate the body with those names assigned.
func hopRetrieval(body string, incoming map[string]string, names []string) (map[string]string, bool) {
	if err := hopjit.HopPythonRuns(body, hopDefinedNames(incoming)); err != nil {
		return nil, false
	}
	statements, ok := parseHopBindBody(body)
	if !ok {
		return nil, false
	}
	bound := bindHopStatements(statements, incoming)
	targets := names
	if len(targets) == 0 {
		targets = hopBindTargets(statements)
		if len(targets) == 0 {
			return nil, false
		}
	}
	for _, name := range targets {
		if bound[name] == "" {
			return nil, false
		}
	}
	return bound, true
}

func retrievalTools(bound map[string]string, names []string) []string {
	consider := names
	if len(consider) == 0 {
		consider = make([]string, 0, len(bound))
		for name := range bound {
			consider = append(consider, name)
		}
	}
	have := map[string]bool{}
	for _, name := range consider {
		if tool := bound[name]; tool == "web_search" || tool == "web_fetch" {
			have[tool] = true
		}
	}
	var tools []string
	for _, tool := range []string{"web_search", "web_fetch"} {
		if have[tool] {
			tools = append(tools, tool)
		}
	}
	return tools
}

func hopBindTargets(statements []*hopBindStmt) []string {
	var names []string
	seen := map[string]bool{}
	var walk func([]*hopBindStmt)
	walk = func(statements []*hopBindStmt) {
		for _, statement := range statements {
			if statement.kind == "assign" && statement.target != "" && !seen[statement.target] {
				seen[statement.target] = true
				names = append(names, statement.target)
			}
			walk(statement.children)
		}
	}
	walk(statements)
	return names
}

type hopBindStmt struct {
	kind       string
	target     string
	expression string
	indent     int
	children   []*hopBindStmt
}

func parseHopBindBody(body string) ([]*hopBindStmt, bool) {
	var flat []*hopBindStmt
	for _, raw := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(raw) == "" || strings.HasPrefix(strings.TrimSpace(raw), "#") {
			continue
		}
		if strings.ContainsRune(raw, '\t') {
			return nil, false
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		text := strings.TrimSpace(stripHopPythonLineComment(raw))
		if text == "" {
			continue
		}
		statement := &hopBindStmt{indent: indent}
		switch {
		case strings.HasPrefix(text, "if ") && strings.HasSuffix(text, ":"):
			statement.kind = "if"
			statement.expression = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "if "), ":"))
		case strings.HasPrefix(text, "elif ") && strings.HasSuffix(text, ":"):
			statement.kind = "elif"
			statement.expression = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "elif "), ":"))
		case text == "else:":
			statement.kind = "else"
		case strings.HasPrefix(text, "for ") || strings.HasPrefix(text, "while ") || strings.HasPrefix(text, "import ") || strings.HasPrefix(text, "from "):
			return nil, false
		default:
			target, expression, ok := hopPythonAssign(text)
			if !ok {
				return nil, false
			}
			statement.kind = "assign"
			statement.target = target
			statement.expression = expression
		}
		flat = append(flat, statement)
	}
	index := 0
	statements, ok := parseHopBindBlock(flat, &index, 0)
	if !ok || index != len(flat) || !validHopBindChain(statements) {
		return nil, false
	}
	return statements, true
}

func parseHopBindBlock(flat []*hopBindStmt, index *int, indent int) ([]*hopBindStmt, bool) {
	var out []*hopBindStmt
	for *index < len(flat) {
		statement := flat[*index]
		if statement.indent < indent {
			break
		}
		if statement.indent > indent {
			return nil, false
		}
		*index++
		if statement.kind == "if" || statement.kind == "elif" || statement.kind == "else" {
			if *index < len(flat) && flat[*index].indent > indent {
				children, ok := parseHopBindBlock(flat, index, flat[*index].indent)
				if !ok {
					return nil, false
				}
				statement.children = children
			} else {
				return nil, false
			}
		}
		out = append(out, statement)
	}
	return out, true
}

func validHopBindChain(statements []*hopBindStmt) bool {
	for index, statement := range statements {
		if statement.kind == "elif" || statement.kind == "else" {
			if index == 0 {
				return false
			}
			prev := statements[index-1].kind
			if prev != "if" && prev != "elif" {
				return false
			}
		}
		if statement.kind == "else" && index+1 < len(statements) {
			next := statements[index+1].kind
			if next == "elif" || next == "else" {
				return false
			}
		}
		if !validHopBindChain(statement.children) {
			return false
		}
	}
	return true
}

func bindHopStatements(statements []*hopBindStmt, incoming map[string]string) map[string]string {
	bound := cloneHopBind(incoming)
	for index := 0; index < len(statements); index++ {
		statement := statements[index]
		switch statement.kind {
		case "assign":
			bound[statement.target] = rhsRetrievalTool(statement.expression, bound)
		case "if":
			chain := []*hopBindStmt{statement}
			for index+1 < len(statements) {
				next := statements[index+1]
				if next.kind != "elif" && next.kind != "else" {
					break
				}
				index++
				chain = append(chain, statements[index])
				if statements[index].kind == "else" {
					break
				}
			}
			bound = mergeHopBindPaths(bound, chain)
		}
	}
	return bound
}

func mergeHopBindPaths(before map[string]string, chain []*hopBindStmt) map[string]string {
	paths := make([]map[string]string, 0, len(chain)+1)
	for _, branch := range chain {
		paths = append(paths, bindHopStatements(branch.children, before))
	}
	if chain[len(chain)-1].kind != "else" {
		paths = append(paths, cloneHopBind(before))
	}
	return intersectHopBind(paths)
}

func intersectHopBind(paths []map[string]string) map[string]string {
	out := map[string]string{}
	if len(paths) == 0 {
		return out
	}
	for name, tool := range paths[0] {
		if tool == "" {
			continue
		}
		same := true
		for _, path := range paths[1:] {
			if path[name] != tool {
				same = false
				break
			}
		}
		if same {
			out[name] = tool
		}
	}
	return out
}

func hopDefinedNames(incoming map[string]string) []string {
	if len(incoming) == 0 {
		return nil
	}
	names := make([]string, 0, len(incoming))
	for name := range incoming {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cloneHopBind(incoming map[string]string) map[string]string {
	out := map[string]string{}
	for name, tool := range incoming {
		if tool != "" {
			out[name] = tool
		}
	}
	return out
}

func rhsRetrievalTool(rhs string, bound map[string]string) string {
	// or returns the left value when it is truthy and does not evaluate the right.
	if left, _, ok := splitTopLevelHopOr(rhs); ok {
		return plainRetrieval(strings.TrimSpace(left), bound)
	}
	return plainRetrieval(strings.TrimSpace(rhs), bound)
}

func plainRetrieval(rhs string, bound map[string]string) string {
	if hopPythonHasContentString(maskHopPythonCallArgs(rhs)) {
		return ""
	}
	if hopPythonCalls(rhs, "web_search") {
		return "web_search"
	}
	if hopPythonCalls(rhs, "web_fetch") {
		return "web_fetch"
	}
	tool := ""
	found := false
	for _, name := range hopPythonReadNames(rhs) {
		if hopPythonKeyword(name) {
			continue
		}
		found = true
		if bound[name] == "" {
			return ""
		}
		if tool == "" {
			tool = bound[name]
			continue
		}
		if tool != bound[name] {
			tool = "web_search"
		}
	}
	if !found {
		return ""
	}
	return tool
}

// splitTopLevelHopOr splits on or outside calls. Grouping parentheses stay
// open. The left operand is the value hop_python returns when it is truthy.
func splitTopLevelHopOr(expression string) (string, string, bool) {
	runes := []rune(expression)
	var quote rune
	escaped := false
	var stack []byte
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '"', '\'':
			quote = r
			continue
		case '(':
			if hopCallParen(runes, index) {
				stack = append(stack, 'c')
			} else {
				stack = append(stack, 'g')
			}
			continue
		case ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		if hopCallOpen(stack) || !hopIdentStart(r) || (index > 0 && hopIdentCont(runes[index-1])) {
			continue
		}
		start := index
		index++
		for index < len(runes) && hopIdentCont(runes[index]) {
			index++
		}
		if string(runes[start:index]) != "or" {
			index--
			continue
		}
		return string(runes[:start]), string(runes[index:]), true
	}
	return "", "", false
}

func hopCallOpen(stack []byte) bool {
	for _, frame := range stack {
		if frame == 'c' {
			return true
		}
	}
	return false
}

func hopCallParen(runes []rune, paren int) bool {
	prev := paren - 1
	for prev >= 0 && unicode.IsSpace(runes[prev]) {
		prev--
	}
	if prev < 0 || !hopIdentCont(runes[prev]) {
		return false
	}
	for prev >= 0 && hopIdentCont(runes[prev]) {
		prev--
	}
	return hopIdentStart(runes[prev+1])
}

func hopIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func hopIdentCont(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// maskHopPythonCallArgs blanks call arguments so a query string is not the
// value of the expression. A quoted sentence outside the call stays visible.
func maskHopPythonCallArgs(expression string) string {
	runes := []rune(expression)
	out := make([]rune, len(runes))
	copy(out, runes)
	var quote rune
	escaped := false
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if !hopIdentStart(r) || (index > 0 && hopIdentCont(runes[index-1])) {
			continue
		}
		index++
		for index < len(runes) && hopIdentCont(runes[index]) {
			index++
		}
		next := index
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			next++
		}
		if next >= len(runes) || runes[next] != '(' {
			index--
			continue
		}
		depth := 0
		quote = 0
		escaped = false
		for next < len(runes) {
			ch := runes[next]
			if quote != 0 {
				out[next] = ' '
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == quote {
					quote = 0
				}
				next++
				continue
			}
			if ch == '"' || ch == '\'' {
				quote = ch
				out[next] = ' '
				next++
				continue
			}
			if ch == '(' {
				depth++
				if depth > 1 {
					out[next] = ' '
				}
				next++
				continue
			}
			if ch == ')' {
				depth--
				if depth == 0 {
					next++
					break
				}
				out[next] = ' '
				next++
				continue
			}
			out[next] = ' '
			next++
		}
		if next == 0 {
			break
		}
		index = next - 1
		quote = 0
		escaped = false
	}
	return string(out)
}

func hopPythonAssign(line string) (string, string, bool) {
	text := stripHopPythonLineComment(line)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", false
	}
	target, expression, ok := strings.Cut(text, "=")
	if !ok {
		return "", "", false
	}
	target = strings.TrimSpace(target)
	expression = strings.TrimSpace(expression)
	if strings.HasSuffix(target, "=") || strings.HasPrefix(expression, "=") || !hopPythonName(target) {
		return "", "", false
	}
	return target, expression, true
}

func stripHopPythonLineComment(line string) string {
	return hopjit.StripHopPythonLineComment(line)
}

func hopPythonHasContentString(expression string) bool {
	var quote rune
	escaped := false
	var buf []rune
	flush := func() bool {
		for _, r := range buf {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return true
			}
		}
		buf = buf[:0]
		return false
	}
	for _, r := range expression {
		if quote != 0 {
			if escaped {
				escaped = false
				buf = append(buf, r)
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				if flush() {
					return true
				}
				quote = 0
				continue
			}
			buf = append(buf, r)
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
		}
	}
	return false
}

func hopStepListsTool(step hopspec.Step, name string) bool {
	for _, tool := range step.Tools {
		if tool == name {
			return true
		}
	}
	return false
}

// hopPythonCalls reports a call of name, ignoring quoted text and comments.
// Space before the parenthesis still counts.
func hopPythonCalls(body, name string) bool {
	text := stripHopPythonStrings(body)
	var code strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	rest := code.String()
	for {
		index := strings.Index(rest, name)
		if index < 0 {
			return false
		}
		if index > 0 && hopIdentByte(rest[index-1]) {
			rest = rest[index+len(name):]
			continue
		}
		after := strings.TrimLeft(rest[index+len(name):], " \t")
		if strings.HasPrefix(after, "(") {
			return true
		}
		rest = rest[index+len(name):]
	}
}

func hopIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func hopLiveLookupTask(task string) bool {
	task = strings.ToLower(task)
	for _, word := range []string{"天气", "新闻", "价格", "weather", "news", "price"} {
		if strings.Contains(task, word) {
			return true
		}
	}
	return false
}

func liveLookupToolsRequired(markdown, task string) error {
	if !hopLiveLookupTask(task) {
		return nil
	}
	spec, _ := hopspec.Parse(markdown)
	if liveLookupCovered(spec) {
		return nil
	}
	return errors.New("LIVE_LOOKUP_TOOLS_REQUIRED: the step that writes each text output must list web_search or web_fetch and, in hop_python, assign that output from the call. A tool line on another step does not count")
}

func insertHopLineAfter(markdown string, line int, text string) string {
	lines := strings.Split(markdown, "\n")
	at := line
	if at > len(lines) {
		at = len(lines)
	}
	if at < 0 {
		at = 0
	}
	next := make([]string, 0, len(lines)+1)
	next = append(next, lines[:at]...)
	next = append(next, text)
	next = append(next, lines[at:]...)
	return strings.Join(next, "\n")
}

// repairMissingCheckVerdicts gives a check or confirm the bool output the
// validator requires. A step with no hop_python body gets a fresh name,
// because the host fills that output. A body keeps the name it assigns to a
// bool value: a literal, a comparison, is/is not, or not/and/or. Any other
// body is left unchanged, so the missing verdict still fails validation and
// the rewrite can see it. A body that reads an already declared variable gets
// that input line when the step omitted it; the body only receives listed
// inputs. A name declared by this repair is visible to later steps. Validate
// still rejects a check whose only output is text.
func repairMissingCheckVerdicts(markdown string) string {
	spec, _ := hopspec.Parse(markdown)
	steps := spec.AllSteps()
	if len(steps) == 0 {
		return markdown
	}
	taken := map[string]bool{}
	blocked := map[string]bool{}
	for _, step := range steps {
		for _, output := range step.Outputs {
			if output.Name == "" {
				continue
			}
			taken[output.Name] = true
			if output.Type != "bool" {
				blocked[output.Name] = true
			}
		}
	}
	for _, decl := range append(append([]hopspec.VariableDecl{}, spec.Inputs...), spec.Outputs...) {
		if decl.Name != "" && decl.Type != "bool" {
			taken[decl.Name] = true
			blocked[decl.Name] = true
		}
	}
	declared := map[string]bool{}
	for _, decl := range append(append([]hopspec.VariableDecl{}, spec.Inputs...), spec.Outputs...) {
		if decl.Name != "" {
			declared[decl.Name] = true
		}
	}
	type verdictInsert struct {
		line  int
		lines []string
	}
	var inserts []verdictInsert
	for _, step := range steps {
		if (step.Type == hopspec.StepCheck || step.Type == hopspec.StepConfirm) && step.Source.LineEnd >= 1 {
			if block := checkRepairLines(step, taken, blocked, declared); len(block) > 0 {
				inserts = append(inserts, verdictInsert{line: step.Source.LineEnd, lines: block})
			}
		}
		for _, output := range step.Outputs {
			if output.Name != "" {
				declared[output.Name] = true
			}
		}
	}
	if len(inserts) == 0 {
		return markdown
	}
	sort.Slice(inserts, func(i, j int) bool { return inserts[i].line > inserts[j].line })
	lines := strings.Split(markdown, "\n")
	for _, insert := range inserts {
		at := insert.line
		if at > len(lines) {
			at = len(lines)
		}
		next := make([]string, 0, len(lines)+len(insert.lines))
		next = append(next, lines[:at]...)
		next = append(next, insert.lines...)
		next = append(next, lines[at:]...)
		lines = next
	}
	return strings.Join(lines, "\n")
}

// checkRepairLines is the output line and any omitted inputs for one check
// or confirm. A body that is not a bool verdict is left untouched.
func checkRepairLines(step hopspec.Step, taken, blocked, declared map[string]bool) []string {
	var verdict string
	var fresh string
	if !stepHasBoolOutput(step) {
		name, ok := checkVerdictName(step, taken, blocked)
		if !ok {
			return nil
		}
		taken[name] = true
		fresh = name
		verdict = "  + → " + name + " % bool"
	}
	var lines []string
	if strings.TrimSpace(step.Body) != "" {
		have := map[string]bool{}
		for _, name := range step.Inputs {
			have[name] = true
		}
		for _, name := range hopPythonReads(step.Body) {
			if !declared[name] || have[name] {
				continue
			}
			have[name] = true
			lines = append(lines, "  - ← "+name)
		}
	}
	if verdict != "" {
		lines = append(lines, verdict)
		// Reads above ran first, so this step does not take its own new
		// output as an input. Later steps can.
		declared[fresh] = true
	}
	return lines
}

func stepHasBoolOutput(step hopspec.Step) bool {
	for _, output := range step.Outputs {
		if output.Type == "bool" {
			return true
		}
	}
	return false
}

// checkVerdictName is the bool output to declare. A hop_python body contributes
// a name only when that assignment evaluates to a bool. A different fresh
// name would be missing when the body runs. A step with no body can take a
// fresh name, because the host returns that output.
func checkVerdictName(step hopspec.Step, taken, blocked map[string]bool) (string, bool) {
	if strings.TrimSpace(step.Body) == "" {
		return freshCheckVerdictName(step.ID, taken), true
	}
	return pickBodyVerdict(hopPythonBoolTargets(step.Body), blocked)
}

func hopPythonBoolTargets(body string) []string {
	var targets []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(body, "\n") {
		text := strings.TrimSpace(stripHopPythonLineComment(raw))
		if text == "" {
			continue
		}
		target, expression, ok := strings.Cut(text, "=")
		if !ok {
			continue
		}
		target = strings.TrimSpace(target)
		expression = strings.TrimSpace(expression)
		if strings.HasSuffix(target, "=") || strings.HasPrefix(expression, "=") {
			continue
		}
		if !hopPythonName(target) || !hopPythonBoolExpression(expression) || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets
}

// hopPythonReads lists variables a body evaluates. Assignment targets, calls,
// keyword names, and attribute fields are not reads. Only these names are
// copied into the body.
func hopPythonReads(body string) []string {
	var reads []string
	seen := map[string]bool{}
	add := func(expression, lhs string) {
		for _, name := range hopPythonReadNames(stripHopPythonStrings(expression)) {
			if name == lhs || hopPythonKeyword(name) || seen[name] {
				continue
			}
			seen[name] = true
			reads = append(reads, name)
		}
	}
	for _, raw := range strings.Split(body, "\n") {
		text := strings.TrimSpace(stripHopPythonLineComment(raw))
		if text == "" {
			continue
		}
		switch {
		case strings.HasPrefix(text, "if ") && strings.HasSuffix(text, ":"):
			add(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "if "), ":")), "")
		case strings.HasPrefix(text, "elif ") && strings.HasSuffix(text, ":"):
			add(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "elif "), ":")), "")
		default:
			target, expression, ok := strings.Cut(text, "=")
			if !ok {
				continue
			}
			target = strings.TrimSpace(target)
			expression = strings.TrimSpace(expression)
			if strings.HasSuffix(target, "=") || strings.HasPrefix(expression, "=") || !hopPythonName(target) {
				continue
			}
			add(expression, target)
		}
	}
	return reads
}

func hopPythonReadNames(expression string) []string {
	var names []string
	runes := []rune(expression)
	for index := 0; index < len(runes); {
		r := runes[index]
		if !unicode.IsLetter(r) && r != '_' {
			index++
			continue
		}
		start := index
		index++
		for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index]) || runes[index] == '_') {
			index++
		}
		if hopPythonAttribute(runes, start) {
			continue
		}
		next := index
		for next < len(runes) && unicode.IsSpace(runes[next]) {
			next++
		}
		if next < len(runes) && (runes[next] == '(' || runes[next] == ':') {
			continue
		}
		names = append(names, string(runes[start:index]))
	}
	return names
}

// hopPythonAttribute reports a name that follows a dot, such as city in
// item.city. That is a field, not a variable the step reads.
func hopPythonAttribute(runes []rune, start int) bool {
	prev := start - 1
	for prev >= 0 && unicode.IsSpace(runes[prev]) {
		prev--
	}
	return prev >= 0 && runes[prev] == '.'
}

func hopPythonKeyword(name string) bool {
	switch name {
	case "and", "or", "not", "is", "if", "elif", "else", "None", "none", "null",
		"true", "True", "TRUE", "false", "False", "FALSE":
		return true
	default:
		return false
	}
}

func hopPythonBoolLiteral(expression string) bool {
	switch expression {
	case "true", "True", "TRUE", "false", "False", "FALSE":
		return true
	default:
		return false
	}
}

// hopPythonBoolExpression reports whether the assignment's value is a bool.
// Comparisons, is/is not, and not/and/or return bool. The same text inside a
// quoted string does not.
func hopPythonBoolExpression(expression string) bool {
	if hopPythonBoolLiteral(expression) {
		return true
	}
	stripped := stripHopPythonStrings(expression)
	if strings.Contains(stripped, "==") || strings.Contains(stripped, "!=") ||
		strings.Contains(stripped, "<=") || strings.Contains(stripped, ">=") ||
		strings.Contains(stripped, "<") || strings.Contains(stripped, ">") {
		return true
	}
	return containsHopKeyword(stripped, "and", "or", "not", "is")
}

func stripHopPythonStrings(expression string) string {
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range expression {
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func containsHopKeyword(expression string, words ...string) bool {
	want := map[string]bool{}
	for _, word := range words {
		want[word] = true
	}
	runes := []rune(expression)
	for index := 0; index < len(runes); {
		r := runes[index]
		if !unicode.IsLetter(r) && r != '_' {
			index++
			continue
		}
		start := index
		index++
		for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsDigit(runes[index]) || runes[index] == '_') {
			index++
		}
		if want[string(runes[start:index])] {
			return true
		}
	}
	return false
}

func hopPythonName(name string) bool {
	if name == "" {
		return false
	}
	for index, r := range name {
		if index == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func pickBodyVerdict(targets []string, blocked map[string]bool) (string, bool) {
	rank := func(name string) int {
		switch name {
		case "accepted":
			return 0
		case "verdict":
			return 1
		default:
			return 2
		}
	}
	best := ""
	for _, name := range targets {
		if blocked[name] {
			continue
		}
		if best == "" || rank(name) < rank(best) {
			best = name
		}
	}
	return best, best != ""
}

func freshCheckVerdictName(stepID string, used map[string]bool) string {
	base := "accepted_" + strings.ReplaceAll(stepID, ".", "_")
	for _, name := range []string{"accepted", "verdict", base} {
		if name != "" && !used[name] {
			return name
		}
	}
	for n := 2; ; n++ {
		name := base + "_" + strconv.Itoa(n)
		if !used[name] {
			return name
		}
	}
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
		return compiledHopSpecNoStepsError(markdown)
	}
	for _, step := range spec.AllSteps() {
		if step.Type == hopspec.StepCommit {
			return fmt.Errorf("compiled HopSpec commit is not allowed: step %s", step.ID)
		}
		if step.Type == hopspec.StepAct && step.Free() && len(step.Tools) == 0 && strings.TrimSpace(step.Body) == "" {
			return fmt.Errorf("ACT_FREE_TOOLS_REQUIRED at line %d: step %s requires an explicit tool whitelist", step.Source.LineStart, step.ID)
		}
	}
	return nil
}
