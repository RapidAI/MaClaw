package hopspec

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Diagnostic struct {
	Code    string `json:"code"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (d Diagnostic) Error() string {
	if d.Line > 0 {
		return fmt.Sprintf("%s at line %d: %s", d.Code, d.Line, d.Message)
	}
	return d.Code + ": " + d.Message
}

var stepLinePattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)\.\s+\[([^]]+)\]\s*(.*)$`)

func Parse(markdown string) (SpecAST, []error) {
	var spec SpecAST
	var errs []error
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	byID := map[string]*Step{}
	bodyOpen := map[*Step]bool{}
	var current *Step
	section := ""
	addErr := func(code string, line int, message string) {
		errs = append(errs, Diagnostic{Code: code, Line: line, Message: message})
	}
	for index, raw := range lines {
		lineNo := index + 1
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "# Spec:") {
			spec.Title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# Spec:"))
			continue
		}
		if match := stepLinePattern.FindStringSubmatch(trimmed); match != nil {
			step, stepErr := parseStep(match[1], match[2], match[3], lineNo)
			if stepErr != nil {
				errs = append(errs, stepErr...)
				continue
			}
			if _, exists := byID[step.ID]; exists {
				addErr("DUPLICATE_STEP_ID", lineNo, "step "+step.ID+" is declared more than once")
				continue
			}
			parentID := stepParentID(step.ID)
			if parentID == "" {
				spec.Steps = append(spec.Steps, step)
			} else {
				parent, ok := byID[parentID]
				if !ok {
					addErr("MISSING_PARENT_STEP", lineNo, "step "+step.ID+" requires parent "+parentID)
					continue
				}
				parent.Children = append(parent.Children, step)
			}
			byID[step.ID] = step
			current = step
			section = "steps"
			continue
		}
		if current != nil && section == "steps" {
			if strings.HasPrefix(trimmed, "- ←") {
				step := current
				step.Inputs = append(step.Inputs, parseInputNames(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ←")))...)
				step.Source.LineEnd = lineNo
				continue
			}
			if strings.HasPrefix(trimmed, "- 工具:") || strings.HasPrefix(trimmed, "- Tools:") {
				_, value, _ := strings.Cut(trimmed, ":")
				current.Tools = append(current.Tools, parseInputNames(value)...)
				current.Source.LineEnd = lineNo
				continue
			}
			if strings.HasPrefix(trimmed, "+ →") {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "+ →"))
				decl := parseVariableDecl(value)
				step := current
				if step.Type == StepCall && step.Call != nil && !strings.Contains(value, "%") {
					if caller, callee, ok := strings.Cut(value, ":"); ok {
						step.Call.Outputs[strings.TrimSpace(caller)] = strings.TrimSpace(callee)
						decl.Type = "any"
					}
				}
				step.Outputs = append(step.Outputs, decl)
				step.Source.LineEnd = lineNo
				continue
			}
			if strings.HasPrefix(trimmed, ">") {
				step := current
				text := strings.TrimPrefix(strings.TrimLeft(raw, " \t"), ">")
				text = strings.TrimPrefix(text, " ")
				fence := strings.TrimSpace(text)
				if strings.HasPrefix(fence, "```hop_python") {
					bodyOpen[step] = true
					step.Source.LineEnd = lineNo
					continue
				}
				if bodyOpen[step] && fence == "```" {
					delete(bodyOpen, step)
					step.Source.LineEnd = lineNo
					continue
				}
				if bodyOpen[step] {
					if step.Body != "" {
						step.Body += "\n"
					}
					step.Body += text
				} else {
					text = strings.TrimSpace(text)
					if step.Instruction != "" && text != "" {
						step.Instruction += "\n"
					}
					step.Instruction += text
				}
				step.Source.LineEnd = lineNo
				continue
			}
		}
		if strings.HasPrefix(trimmed, "##") {
			section = canonicalSection(strings.TrimSpace(strings.TrimLeft(trimmed, "#")))
			current = nil
			continue
		}
		if strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
			section = canonicalSection(strings.TrimSuffix(trimmed, ":"))
			continue
		}
		if key, value, ok := strings.Cut(trimmed, ":"); ok && !strings.HasPrefix(trimmed, "-") {
			switch canonicalSection(key) {
			case "id":
				spec.ID = strings.TrimSpace(value)
			case "goal":
				spec.Goal = strings.TrimSpace(value)
			}
			if canonicalSection(key) != "" {
				continue
			}
		}
		if strings.HasPrefix(trimmed, "-") {
			switch section {
			case "constraints":
				spec.Constraints = append(spec.Constraints, strings.TrimSpace(strings.TrimPrefix(trimmed, "-")))
			case "inputs":
				spec.Inputs = append(spec.Inputs, parseVariableDecl(strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))))
			case "outputs":
				spec.Outputs = append(spec.Outputs, parseVariableDecl(strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))))
			}
		}
	}
	for step := range bodyOpen {
		addErr("UNCLOSED_HOP_PYTHON", step.Source.LineStart, "hop_python fence is not closed")
	}
	if spec.Title == "" {
		addErr("MISSING_SPEC_TITLE", 0, "expected # Spec: <title>")
	}
	return spec, errs
}

func parseStep(id, descriptor, summary string, line int) (*Step, []error) {
	descriptor = strings.TrimSpace(descriptor)
	if descriptor == "" {
		return nil, []error{Diagnostic{Code: "MISSING_STEP_TYPE", Line: line, Message: "step type is required"}}
	}
	typeToken := descriptor
	remainder := ""
	if index := strings.IndexAny(descriptor, " \t"); index >= 0 {
		typeToken, remainder = descriptor[:index], strings.TrimSpace(descriptor[index+1:])
	}
	condition := ""
	if strings.HasPrefix(descriptor, "case(") && strings.HasSuffix(descriptor, ")") {
		typeToken = "case"
		condition = strings.TrimSpace(descriptor[len("case(") : len(descriptor)-1])
		remainder = ""
	}
	stepType, ok := canonicalStepType(typeToken)
	if !ok {
		return nil, []error{Diagnostic{Code: "UNKNOWN_STEP_TYPE", Line: line, Message: "unknown step type " + typeToken}}
	}
	fields := strings.Fields(remainder)
	if stepType == StepOnFail && len(fields) > 0 && fields[0] == "fail" {
		fields = fields[1:]
	}
	step := &Step{
		ID: id, Type: stepType, Summary: strings.TrimSpace(summary), Attributes: map[string]string{}, Condition: condition,
		Source: SourceLocation{LineStart: line, LineEnd: line},
	}
	for _, field := range fields {
		if key, value, found := strings.Cut(field, "="); found {
			step.Attributes[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
		} else {
			step.Attributes[strings.ToLower(strings.TrimSpace(field))] = "true"
		}
	}
	if len(step.Attributes) == 0 {
		step.Attributes = nil
	}
	step.Adaptive = step.Attributes["adaptive"] == "true"
	step.Parallel = step.Attributes["parallel"] == "true" || step.Attributes["并行"] == "true"
	var errs []error
	if raw := step.Attributes["retry"]; raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			errs = append(errs, Diagnostic{Code: "INVALID_RETRY", Line: line, Message: "retry must be a non-negative integer"})
		} else {
			step.Retry = &value
		}
	}
	if raw := step.Attributes["max"]; raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			errs = append(errs, Diagnostic{Code: "INVALID_LOOP_MAX", Line: line, Message: "loop max must be a positive integer"})
		} else {
			step.MaxIterations = value
		}
	}
	step.ForEach = parseForEach(descriptor)
	if step.Type == StepCall {
		step.Call = parseCallSpec(remainder)
	}
	if step.Type == StepCase && step.Condition == "" {
		if start := strings.LastIndex(step.Summary, "("); start >= 0 && strings.HasSuffix(step.Summary, ")") {
			step.Condition = strings.TrimSpace(step.Summary[start+1 : len(step.Summary)-1])
			step.Summary = strings.TrimSpace(step.Summary[:start])
		}
	}
	return step, errs
}

func parseCallSpec(descriptor string) *CallSpec {
	descriptor = strings.TrimSpace(descriptor)
	call := &CallSpec{Inputs: map[string]string{}, Outputs: map[string]string{}}
	open := strings.Index(descriptor, "(")
	close := strings.LastIndex(descriptor, ")")
	if open >= 0 && close > open {
		call.Callee = strings.TrimSpace(descriptor[:open])
		mapping := descriptor[open+1 : close]
		for _, item := range strings.Split(mapping, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if target, source, ok := strings.Cut(item, ":"); ok {
				call.Inputs[strings.TrimSpace(target)] = strings.TrimSpace(source)
			} else {
				call.Inputs[item] = item
			}
		}
	} else if fields := strings.Fields(descriptor); len(fields) > 0 {
		call.Callee = strings.TrimSpace(fields[0])
	}
	return call
}

func parseForEach(descriptor string) *ForEachSpec {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`for-each\s+([^\s,]+)\s+in\s+([^\s,]+)`),
		regexp.MustCompile(`遍历\s+([^\s,]+)\s+于\s+([^\s,]+)`),
	}
	var spec *ForEachSpec
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(descriptor); match != nil {
			spec = &ForEachSpec{Item: match[1], List: match[2]}
			break
		}
	}
	if spec == nil {
		return nil
	}
	collectPatterns := []*regexp.Regexp{
		regexp.MustCompile(`collect\s+([^\s,]+)\s+into\s+([^\s,]+)`),
		regexp.MustCompile(`收集\s+([^\s,]+)\s+入\s+([^\s,]+)`),
	}
	for _, pattern := range collectPatterns {
		if match := pattern.FindStringSubmatch(descriptor); match != nil {
			spec.CollectItem, spec.CollectInto = match[1], match[2]
			break
		}
	}
	return spec
}

func canonicalStepType(value string) (StepType, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "reason", "推理":
		return StepReason, true
	case "act", "探索":
		return StepAct, true
	case "check", "核验":
		return StepCheck, true
	case "confirm", "确认":
		return StepConfirm, true
	case "ask", "询问":
		return StepAsk, true
	case "commit", "提交":
		return StepCommit, true
	case "call", "调用":
		return StepCall, true
	case "subtask", "子任务":
		return StepSubtask, true
	case "loop", "循环":
		return StepLoop, true
	case "branch", "分支":
		return StepBranch, true
	case "case":
		return StepCase, true
	case "break", "退出循环":
		return StepBreak, true
	case "continue", "继续循环":
		return StepContinue, true
	case "exit", "退出":
		return StepExit, true
	case "on", "on_fail", "失败兜底":
		return StepOnFail, true
	default:
		return "", false
	}
}

func canonicalSection(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "id", "标识":
		return "id"
	case "goal", "目标":
		return "goal"
	case "constraints", "约束":
		return "constraints"
	case "inputs", "输入":
		return "inputs"
	case "outputs", "输出":
		return "outputs"
	case "steps", "步骤":
		return "steps"
	case "types", "类型":
		return "types"
	default:
		return ""
	}
}

func stepParentID(id string) string {
	if index := strings.LastIndex(id, "."); index >= 0 {
		return id[:index]
	}
	return ""
}

func parseInputNames(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func parseVariableDecl(value string) VariableDecl {
	value = strings.TrimSpace(value)
	description := ""
	if before, after, ok := strings.Cut(value, "#"); ok {
		value, description = strings.TrimSpace(before), strings.TrimSpace(after)
	}
	name, typ := value, ""
	if before, after, ok := strings.Cut(value, "%"); ok {
		name, typ = strings.TrimSpace(before), strings.TrimSpace(after)
	} else if before, after, ok := strings.Cut(value, ":"); ok {
		name, typ = strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return VariableDecl{Name: name, Type: typ, Description: description}
}
