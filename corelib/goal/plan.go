package goal

import (
	"encoding/json"
	"fmt"
	"strings"
)

func cloneGoal(g *Goal) *Goal {
	if g == nil {
		return nil
	}
	cp := *g
	if len(g.AcceptanceCriteria) > 0 {
		cp.AcceptanceCriteria = append([]string(nil), g.AcceptanceCriteria...)
	}
	if len(g.Steps) > 0 {
		cp.Steps = make([]Step, len(g.Steps))
		copy(cp.Steps, g.Steps)
	}
	return &cp
}

// EffectivePhase treats a legacy goal with no phase as planning until steps exist.
func (g *Goal) EffectivePhase() Phase {
	if g == nil {
		return PhasePlan
	}
	switch g.Phase {
	case PhaseExecute:
		return PhaseExecute
	case PhasePlan:
		return PhasePlan
	default:
		if len(g.Steps) > 0 {
			return PhaseExecute
		}
		return PhasePlan
	}
}

// CurrentStep returns the first step that still needs work. A progress report
// keeps that step current, so the next turn does not relabel the leftover
// work as the following step.
func (g *Goal) CurrentStep() (Step, bool) {
	if g == nil {
		return Step{}, false
	}
	for _, step := range g.Steps {
		if step.Status == "" || step.Status == StepPending || step.Status == StepProgress {
			return step, true
		}
	}
	return Step{}, false
}

// ReportedStep is where a model step report should land. Index is the
// current step when the model omitted one. FollowUp is set when the model
// wrote about a step that is already done while a later step is current.
// Refusal is set only when the index would skip ahead.
type ReportedStep struct {
	Index    int
	FollowUp Step
	Current  Step
	Refusal  string
}

// ResolveReportedStep binds a report to the only step this turn can advance.
// A missing index uses that step. A report for an earlier done step is a
// note, not a skip and not a failure. A later index is still refused.
func (g *Goal) ResolveReportedStep(index int, indexPresent bool) ReportedStep {
	current, ok := g.CurrentStep()
	if !ok {
		return ReportedStep{Refusal: "没有待执行的步骤。"}
	}
	if !indexPresent || index <= 0 {
		return ReportedStep{Index: current.Index, Current: current}
	}
	if index == current.Index {
		return ReportedStep{Index: index, Current: current}
	}
	for _, step := range g.Steps {
		if step.Index == index && step.Status == StepDone {
			return ReportedStep{FollowUp: step, Current: current}
		}
	}
	return ReportedStep{Refusal: g.StepReportRefusal(index)}
}

// FormatDoneStepNote tells the model the earlier step stayed closed and names
// the only index that advances the goal. The first line stays a landed step
// result so the call is not retried as a failure.
func FormatDoneStepNote(done, current Step, saved bool) string {
	var b strings.Builder
	if saved {
		b.WriteString(fmt.Sprintf("步骤 %d 已完成，补充说明已记下。\n", done.Index))
	} else {
		b.WriteString(fmt.Sprintf("步骤 %d 已完成。\n", done.Index))
	}
	b.WriteString(fmt.Sprintf("本轮当前步骤仍是 %d：%s。\n", current.Index, current.Title))
	b.WriteString(fmt.Sprintf("请继续做步骤 %d，并用 goal(action=\"step\", index=%d, status=\"progress\" 或 \"done\", result=\"...\") 提交。不要再提交步骤 %d。", current.Index, current.Index, done.Index))
	return b.String()
}

// StepReportRefusal rejects a step report that would close or skip a
// different step than the one this turn is executing.
func (g *Goal) StepReportRefusal(index int) string {
	if g == nil {
		return "当前没有活跃目标。"
	}
	current, ok := g.CurrentStep()
	if !ok {
		return "没有待执行的步骤。"
	}
	if index != current.Index {
		return fmt.Sprintf("本轮只能提交步骤 %d：%s", current.Index, current.Title)
	}
	return ""
}

// ReadyToComplete reports whether every saved step finished successfully.
func (g *Goal) ReadyToComplete() bool {
	if g == nil || len(g.Steps) == 0 {
		return false
	}
	for _, step := range g.Steps {
		if step.Status != StepDone {
			return false
		}
	}
	return true
}

// CompletionRefusal explains why complete must be rejected. Empty means the
// summary may close the goal.
func CompletionRefusal(g *Goal, summary string) string {
	if g == nil {
		return "当前没有活跃目标。"
	}
	var reasons []string
	if !g.ReadyToComplete() {
		if g.EffectivePhase() != PhaseExecute || len(g.Steps) == 0 {
			reasons = append(reasons, "目标尚未分解为步骤")
		} else {
			for _, step := range g.Steps {
				if step.Status != StepDone {
					reasons = append(reasons, fmt.Sprintf("步骤 %d 未完成：%s", step.Index, step.Title))
				}
			}
		}
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		reasons = append(reasons, "缺少完成总结")
	}
	if len(g.AcceptanceCriteria) == 0 {
		reasons = append(reasons, "缺少验收标准")
	} else if missing := MissingCriteria(summary, g.AcceptanceCriteria); len(missing) > 0 {
		reasons = append(reasons, "总结未覆盖验收标准："+strings.Join(missing, "；"))
	}
	if len(reasons) == 0 {
		return ""
	}
	return "错误: 不能完成目标\n" + strings.Join(reasons, "\n")
}

// FormatCompletionReport is the user-visible closeout after a valid complete.
func FormatCompletionReport(g *Goal, summary string) string {
	if g == nil {
		return strings.TrimSpace(summary)
	}
	var b strings.Builder
	b.WriteString("目标已完成\n")
	b.WriteString("目标: " + g.Objective + "\n")
	if strings.TrimSpace(g.Understanding) != "" {
		b.WriteString("理解: " + g.Understanding + "\n")
	}
	if len(g.Steps) > 0 {
		b.WriteString("步骤结果:\n")
		for _, step := range g.Steps {
			b.WriteString(fmt.Sprintf("  %d. %s — %s\n", step.Index, step.Title, step.Result))
		}
	}
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("验收核对:\n")
		for _, criterion := range g.AcceptanceCriteria {
			b.WriteString("  • " + criterion + "\n")
		}
	}
	b.WriteString("总结: " + strings.TrimSpace(summary))
	return b.String()
}

// MissingCriteria lists acceptance criteria that the summary does not mention.
func MissingCriteria(summary string, criteria []string) []string {
	summary = strings.ToLower(strings.TrimSpace(summary))
	var missing []string
	for _, criterion := range compactStrings(criteria) {
		if !strings.Contains(summary, strings.ToLower(criterion)) {
			missing = append(missing, criterion)
		}
	}
	return missing
}

func normalizeSteps(steps []Step) ([]Step, error) {
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		title := strings.TrimSpace(step.Title)
		if title == "" {
			continue
		}
		out = append(out, Step{
			Index:  len(out) + 1,
			Title:  title,
			Detail: strings.TrimSpace(step.Detail),
			Status: StepPending,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("steps cannot be empty")
	}
	return out, nil
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func canonicalStepStatus(raw string) (StepStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "done", "complete", "completed":
		return StepDone, true
	case "progress", "in_progress":
		return StepProgress, true
	case "failed", "fail":
		return StepFailed, true
	default:
		return "", false
	}
}

// ParseSteps accepts a string, a string list, or objects with title/detail.
func ParseSteps(raw interface{}) ([]Step, error) {
	if raw == nil {
		return nil, fmt.Errorf("steps cannot be empty")
	}
	switch v := raw.(type) {
	case []Step:
		return normalizeSteps(v)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil, fmt.Errorf("steps cannot be empty")
		}
		if steps, handled, err := stepsFromEncodedJSON(text, 0); handled {
			return steps, err
		}
		// Not a document: split into lines and give each one the same
		// element semantics an array element gets — a JSON-document line
		// expands, a plain line stays a title.
		lines := strings.Split(text, "\n")
		values := make([]interface{}, len(lines))
		for i, line := range lines {
			values[i] = line
		}
		return stepsFromValues(values, 0)
	case []string:
		values := make([]interface{}, len(v))
		for i, item := range v {
			values[i] = item
		}
		return stepsFromValues(values, 0)
	case map[string]interface{}:
		return normalizeSteps([]Step{stepFromMap(v)})
	case []interface{}:
		return stepsFromValues(v, 0)
	default:
		return nil, fmt.Errorf("invalid steps")
	}
}

// stepsJSONDepth bounds JSON-in-JSON unwrapping: a model that double-encodes
// the steps array expands once, deeper nesting stays a title.
const stepsJSONDepth = 3

// stepsFromEncodedJSON decodes a steps document the model sent as text. The
// whole steps argument, or one list element, can arrive as a JSON string —
// sometimes fenced or double-encoded — instead of a decoded array. handled is
// false when the text is not a JSON document so the caller keeps it as a
// plain step title; handled with err set surfaces the same "no usable steps"
// error the object form would have produced.
func stepsFromEncodedJSON(text string, depth int) (steps []Step, handled bool, err error) {
	if depth >= stepsJSONDepth {
		return nil, false, nil
	}
	text = stripStepsCodeFence(strings.TrimSpace(strings.TrimPrefix(text, "\uFEFF")))
	// Only documents can unwrap; a scalar decode ("42", "true") never is a
	// steps list anyway, so skip the unmarshal attempt on plain titles.
	if !strings.HasPrefix(text, "[") && !strings.HasPrefix(text, "{") && !strings.HasPrefix(text, "\"") {
		return nil, false, nil
	}
	for _, candidate := range stepsDecodeCandidates(text) {
		var decoded interface{}
		if jsonErr := json.Unmarshal([]byte(candidate), &decoded); jsonErr != nil {
			continue
		}
		return stepsFromDecodedJSON(decoded, depth)
	}
	return nil, false, nil
}

// stepsDecodeCandidates lists the text plus tolerant completions for a
// truncated document: a missing closer and a trailing partial element cut to
// the last complete object. A candidate that decodes to a non-steps shape is
// still filtered by stepsFromDecodedJSON, so plain titles stay untouched.
func stepsDecodeCandidates(text string) []string {
	candidates := []string{text}
	if text == "" {
		return candidates
	}
	var open, close byte
	switch text[0] {
	case '[':
		open, close = '[', ']'
	case '{':
		open, close = '{', '}'
	}
	// A lone opener ("[", "{") is a title character, not a document: it needs
	// at least one content byte before any completion is plausible.
	if open == 0 || len(text) < 2 || text[len(text)-1] == close {
		return candidates
	}
	// Missing closer: "[{...},{...}" -> append it.
	candidates = append(candidates, text+string(close))
	if open == '[' {
		// Trailing partial element: "[{...},{..." -> cut to the last complete
		// object and close the array.
		if last := strings.LastIndex(text, "}"); last >= 0 {
			candidates = append(candidates, text[:last+1]+"]")
		}
	}
	return candidates
}

// stepsFromDecodedJSON interprets one decoded JSON value: a step object, a
// {"steps": [...]} wrapper, an array of either, or a string holding another
// document. Scalar values are not steps.
func stepsFromDecodedJSON(decoded interface{}, depth int) (steps []Step, handled bool, err error) {
	if depth >= stepsJSONDepth {
		return nil, false, nil
	}
	switch v := decoded.(type) {
	case string:
		return stepsFromEncodedJSON(v, depth+1)
	case map[string]interface{}:
		// A "steps" member means the object wraps the list; stepFromMap never
		// reads that key, so it cannot be a step's own field. A scalar or
		// missing member decodes to nothing and falls through to stepFromMap.
		if inner, handled, err := stepsFromDecodedJSON(v["steps"], depth+1); handled {
			return inner, true, err
		}
		steps, err := normalizeSteps([]Step{stepFromMap(v)})
		return steps, true, err
	case []interface{}:
		// A document that decodes to non-step elements — "[1,2,3]" — was a
		// title before, so it is not a steps document here either.
		for _, item := range v {
			switch item.(type) {
			case string, map[string]interface{}:
			default:
				return nil, false, nil
			}
		}
		steps, err := stepsFromValues(v, depth+1)
		return steps, true, err
	default:
		// Numbers, bools, and null decode fine but are not steps; keep the
		// text as a title the way the split-lines fallback always did.
		return nil, false, nil
	}
}

func stepsFromValues(values []interface{}, depth int) ([]Step, error) {
	steps := make([]Step, 0, len(values))
	for _, item := range values {
		switch step := item.(type) {
		case string:
			if expanded, handled, err := stepsFromEncodedJSON(step, depth+1); handled {
				if err != nil {
					return nil, err
				}
				steps = append(steps, expanded...)
				continue
			}
			steps = append(steps, Step{Title: step})
		case map[string]interface{}:
			s := stepFromMap(step)
			if strings.TrimSpace(s.Title) == "" {
				// An untitled object may be a {"steps": [...]} wrapper. A
				// failed unwrap keeps the empty-title element, which
				// normalizeSteps drops like it always did.
				if expanded, handled, err := stepsFromDecodedJSON(step, depth+1); handled && err == nil && len(expanded) > 0 {
					steps = append(steps, expanded...)
					continue
				}
			} else if expanded, handled, err := stepsFromEncodedJSON(s.Title, depth+1); handled {
				if err != nil {
					return nil, err
				}
				if s.Detail != "" && len(expanded) > 0 && expanded[0].Detail == "" {
					expanded[0].Detail = s.Detail
				}
				steps = append(steps, expanded...)
				continue
			}
			steps = append(steps, s)
		default:
			return nil, fmt.Errorf("invalid step")
		}
	}
	return normalizeSteps(steps)
}

// repairEncodedStepTitles expands a stored step whose title is itself a steps
// document. SavePlan parses every encoding the model sends; rows written by a
// build without that kept the JSON document as the title — the whole plan
// collapsed into one step and marking it done closed the goal early. The
// first decoded step keeps the recorded status and result, because that is
// the work the model actually reported; the rest come back pending so the
// goal resumes on the step after it.
func repairEncodedStepTitles(steps []Step) ([]Step, bool) {
	changed := false
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		expanded, handled, err := stepsFromEncodedJSON(step.Title, 0)
		if !handled || err != nil || len(expanded) == 0 {
			out = append(out, step)
			continue
		}
		changed = true
		for i, item := range expanded {
			if i == 0 {
				item.Status = step.Status
				item.Result = step.Result
				if item.Detail == "" {
					item.Detail = step.Detail
				}
			}
			if item.Status == "" {
				item.Status = StepPending
			}
			out = append(out, item)
		}
	}
	if !changed {
		return steps, false
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out, true
}

// stripStepsCodeFence removes one ``` / ```json wrapper. Unfenced text is
// returned unchanged.
func stripStepsCodeFence(text string) string {
	if !strings.HasPrefix(text, "```") {
		return text
	}
	body := text[3:]
	if lineEnd := strings.IndexByte(body, '\n'); lineEnd >= 0 {
		// The info string ("json", nothing, or a word) ends at the newline.
		body = body[lineEnd+1:]
	} else if idx := strings.IndexAny(body, `[{"`); idx > 0 {
		// Single-line fence: drop a "json"-style info token only up to the
		// first JSON document marker — a later space may be inside the doc.
		body = body[idx:]
	}
	body = strings.TrimSpace(body)
	if rest, ok := strings.CutSuffix(body, "```"); ok {
		body = strings.TrimSpace(rest)
	}
	return body
}

func stepFromMap(raw map[string]interface{}) Step {
	title, _ := raw["title"].(string)
	if strings.TrimSpace(title) == "" {
		title, _ = raw["name"].(string)
	}
	detail, _ := raw["detail"].(string)
	if strings.TrimSpace(detail) == "" {
		detail, _ = raw["description"].(string)
	}
	// The plan schema used to declare steps as a bare object, so models emit
	// detail and leave title out. An empty title is dropped later and the
	// whole plan is rejected as having no steps.
	if strings.TrimSpace(title) == "" {
		title = stepTitleFromDetail(detail)
	}
	return Step{Title: title, Detail: detail}
}

func stepTitleFromDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	if i := strings.IndexByte(detail, '\n'); i >= 0 {
		detail = strings.TrimSpace(detail[:i])
	}
	runes := []rune(detail)
	cut := len(runes)
	const maxTitle = 48
	if cut > maxTitle {
		cut = maxTitle
	}
	for i, r := range runes[:cut] {
		if (r == '。' || r == '；' || r == ';') && i >= 8 {
			cut = i
			break
		}
	}
	return strings.TrimRight(strings.TrimSpace(string(runes[:cut])), "。；;")
}

// ParseStringList accepts a string, a JSON string array, or a mixed array.
func ParseStringList(raw interface{}) []string {
	switch v := raw.(type) {
	case []string:
		return compactStrings(v)
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return nil
		}
		var parsed []string
		if json.Unmarshal([]byte(text), &parsed) == nil {
			return compactStrings(parsed)
		}
		return compactStrings([]string{text})
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return compactStrings(out)
	default:
		return nil
	}
}
