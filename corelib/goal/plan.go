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
		var asObjects []map[string]interface{}
		if json.Unmarshal([]byte(text), &asObjects) == nil {
			return stepsFromObjects(asObjects)
		}
		var asStrings []string
		if json.Unmarshal([]byte(text), &asStrings) == nil {
			return stepsFromStrings(asStrings)
		}
		return stepsFromStrings(strings.Split(text, "\n"))
	case []string:
		return stepsFromStrings(v)
	case []interface{}:
		steps := make([]Step, 0, len(v))
		for _, item := range v {
			switch step := item.(type) {
			case string:
				steps = append(steps, Step{Title: step})
			case map[string]interface{}:
				steps = append(steps, stepFromMap(step))
			default:
				return nil, fmt.Errorf("invalid step")
			}
		}
		return normalizeSteps(steps)
	default:
		return nil, fmt.Errorf("invalid steps")
	}
}

func stepsFromStrings(values []string) ([]Step, error) {
	steps := make([]Step, 0, len(values))
	for _, value := range values {
		steps = append(steps, Step{Title: value})
	}
	return normalizeSteps(steps)
}

func stepsFromObjects(values []map[string]interface{}) ([]Step, error) {
	steps := make([]Step, 0, len(values))
	for _, value := range values {
		steps = append(steps, stepFromMap(value))
	}
	return normalizeSteps(steps)
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
	return Step{Title: title, Detail: detail}
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
