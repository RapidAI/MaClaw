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

var stepLinePattern = regexp.MustCompile(`^(\d+(?:\.\d+)*)[.、．]\s*[\[［【]([^\]］】]+)[\]］】]\s*(.*)$`)

func Parse(markdown string) (SpecAST, []error) {
	var spec SpecAST
	var errs []error
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	byID := map[string]*Step{}
	bodyOpen := map[*Step]bool{}
	var current *Step
	section := ""
	skipUntil := -1
	addErr := func(code string, line int, message string) {
		errs = append(errs, Diagnostic{Code: code, Line: line, Message: message})
	}
	for index, raw := range lines {
		// A closed raw hop_python fence is code. Step lines inside it are
		// not steps. On an act, check, or commit step the fence is the body.
		// The quoted "> ```hop_python" form is handled below.
		if index <= skipUntil {
			continue
		}
		lineNo := index + 1
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "```hop_python") {
			if end := takeRawHopPython(lines, index, current); end >= 0 {
				skipUntil = end
				continue
			}
		}
		if trimmed == "" {
			continue
		}
		if title, ok := specTitle(trimmed); ok {
			spec.Title = title
			continue
		}
		if id, descriptor, summary, ok := matchStepLine(trimmed); ok {
			step, stepErr := parseStep(id, descriptor, summary, lineNo)
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
			if value, ok := inputDeclValue(trimmed); ok {
				step := current
				step.Inputs = append(step.Inputs, parseInputNames(value)...)
				step.Source.LineEnd = lineNo
				continue
			}
			if value, ok := toolDeclValue(trimmed); ok {
				current.Tools = append(current.Tools, parseInputNames(value)...)
				current.Source.LineEnd = lineNo
				continue
			}
			if value, ok := outputDeclValue(trimmed); ok {
				decl := parseVariableDecl(value)
				step := current
				if step.Type == StepCall && step.Call != nil && !strings.Contains(value, "%") {
					if caller, callee, ok := splitSectionColon(value); ok {
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
			heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			label, value := heading, ""
			if key, rest, ok := splitSectionColon(heading); ok {
				label, value = key, rest
			}
			section = canonicalSection(cleanSectionToken(strings.TrimRight(label, ":：")))
			if value = cleanSectionToken(value); value != "" {
				switch section {
				case "id":
					spec.ID = value
				case "goal":
					spec.Goal = value
				}
			}
			current = nil
			continue
		}
		// Section labels only. Step bodies and hop_python lines are consumed above.
		if !strings.HasPrefix(trimmed, "-") {
			if label, ok := bareSectionLabel(trimmed); ok {
				name := canonicalSection(cleanSectionToken(label))
				// A goal sentence may end with a colon. Only a real section
				// label leaves the goal; the sentence stays in the body.
				if name != "" || section != "goal" {
					section = name
					continue
				}
			}
			if key, value, ok := splitSectionColon(trimmed); ok {
				key = cleanSectionToken(key)
				value = cleanSectionToken(value)
				if name := canonicalSection(key); name != "" {
					// The label enters the section even when the value is only
					// emphasis, as in "**目标：**" with the sentence on the next line.
					section = name
					switch name {
					case "id":
						if value != "" {
							spec.ID = value
						}
					case "goal":
						if value != "" {
							spec.Goal = value
						}
					}
					continue
				}
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
		if section == "goal" {
			spec.Goal = appendGoalBody(spec.Goal, trimmed)
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

func rawHopPythonEnd(lines []string, from int) int {
	for index := from; index < len(lines); index++ {
		if strings.TrimSpace(lines[index]) == "```" {
			return index
		}
	}
	return -1
}

// takeRawHopPython consumes a closed raw fence. The interior is code, not
// steps. On an act, check, or commit step it is that step's hop_python body.
// An unclosed fence is left for the normal line scan.
func takeRawHopPython(lines []string, opener int, current *Step) int {
	end := rawHopPythonEnd(lines, opener+1)
	if end < 0 {
		return -1
	}
	if current != nil && (current.Type == StepAct || current.Type == StepCheck || current.Type == StepCommit) {
		if body := rawHopPythonBody(lines, opener, end); body != "" {
			if current.Body != "" {
				current.Body += "\n"
			}
			current.Body += body
			current.Source.LineEnd = end + 1
		}
	}
	return end
}

func rawHopPythonBody(lines []string, opener, end int) string {
	indent := len(lines[opener]) - len(strings.TrimLeft(lines[opener], " "))
	prefix := strings.Repeat(" ", indent)
	var b strings.Builder
	for index := opener + 1; index < end; index++ {
		line := lines[index]
		if indent > 0 && strings.HasPrefix(line, prefix) {
			line = line[indent:]
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return strings.Trim(b.String(), "\n")
}

// CountStepLines counts bracketed step lines. It does not parse the spec:
// a line still counts when a later structural check will reject it.
// StepLineCounts also returns how many of those steps are not a bare exit.
func CountStepLines(markdown string) int {
	steps, _ := StepLineCounts(markdown)
	return steps
}

func StepLineCounts(markdown string) (steps, work int) {
	for _, line := range strings.Split(markdown, "\n") {
		_, descriptor, _, ok := matchStepLine(strings.TrimSpace(line))
		if !ok {
			continue
		}
		steps++
		token := descriptor
		if index := strings.IndexAny(token, " \t"); index >= 0 {
			token = token[:index]
		}
		if typ, known := canonicalStepType(token); known && typ != StepExit {
			work++
		}
	}
	return steps, work
}

// matchStepLine reads one bracketed step. Fullwidth brackets and the
// separators 、 and ． are the same line as "1. [act] 摘要". The summary is
// not rewritten. A prose numbered line is not a step.
func matchStepLine(trimmed string) (id, descriptor, summary string, ok bool) {
	match := stepLinePattern.FindStringSubmatch(trimmed)
	if match == nil {
		return "", "", "", false
	}
	return match[1], match[2], match[3], true
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
			if target, source, ok := splitSectionColon(item); ok {
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

// appendGoalBody keeps the prose under a Goal heading. An inline
// "Goal: value" is stored by the heading handler and is not passed here.
// A following paragraph, one list marker, or a leading ">" is the same body.
func appendGoalBody(goal, line string) string {
	text := goalBodyText(line)
	if text == "" {
		return goal
	}
	if goal == "" {
		return text
	}
	return goal + "\n" + text
}

func goalBodyText(line string) string {
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, ">") {
		text = strings.TrimSpace(strings.TrimPrefix(text, ">"))
	}
	switch {
	case strings.HasPrefix(text, "- "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "- "))
	case strings.HasPrefix(text, "－ "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "－ "))
	case strings.HasPrefix(text, "* "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "* "))
	}
	return cleanSectionToken(text)
}

// cleanSectionToken drops markdown emphasis around a section label or value.
// Emphasis can sit on either side of the colon, so stars and spaces are peeled
// until the token is stable: "**目标：** 查询" yields 目标 and 查询.
func cleanSectionToken(value string) string {
	for {
		next := strings.TrimSpace(strings.Trim(strings.TrimSpace(value), "*_"))
		if next == value {
			return value
		}
		value = next
	}
}

func specTitle(line string) (string, bool) {
	for _, prefix := range []string{"# Spec:", "# Spec："} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", false
}

func bareSectionLabel(line string) (string, bool) {
	for _, colon := range []string{":", "："} {
		if strings.HasSuffix(line, colon) {
			return strings.TrimSpace(strings.TrimSuffix(line, colon)), true
		}
	}
	return "", false
}

// splitSectionColon splits on the first ASCII or fullwidth colon.
// A later colon, including one inside a value, stays in the value.
func splitSectionColon(line string) (string, string, bool) {
	ascii := strings.Index(line, ":")
	wide := strings.Index(line, "：")
	switch {
	case ascii < 0 && wide < 0:
		return "", "", false
	case wide < 0 || (ascii >= 0 && ascii < wide):
		return line[:ascii], line[ascii+1:], true
	default:
		return line[:wide], line[wide+len("："):], true
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

// toolDeclValue reads a step tool whitelist. The mark may be "-" or "－".
// Space before the label is optional, and either colon may have spaces
// around it. The label is 工具, Tools, or tools, and it must end at the
// colon so a longer word is left alone.
func toolDeclValue(trimmed string) (string, bool) {
	rest := ""
	switch {
	case strings.HasPrefix(trimmed, "-"):
		rest = trimmed[len("-"):]
	case strings.HasPrefix(trimmed, "－"):
		rest = trimmed[len("－"):]
	default:
		return "", false
	}
	rest = strings.TrimSpace(rest)
	for _, label := range []string{"工具", "Tools", "tools"} {
		if !strings.HasPrefix(rest, label) {
			continue
		}
		after := strings.TrimSpace(rest[len(label):])
		if strings.HasPrefix(after, ":") {
			return strings.TrimSpace(strings.TrimPrefix(after, ":")), true
		}
		if strings.HasPrefix(after, "：") {
			return strings.TrimSpace(strings.TrimPrefix(after, "：")), true
		}
	}
	return "", false
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

// outputDeclValue reads a step output. The plus may be "+" or "＋", and the
// arrow may be "→" or "->", with or without a space between them.
func outputDeclValue(trimmed string) (string, bool) {
	return markedDeclValue(trimmed, []string{"+", "＋"}, []string{"→", "->"})
}

// inputDeclValue reads a step input. The hyphen may be "-" or "－", and the
// arrow may be "←" or "<-", with or without a space between them.
func inputDeclValue(trimmed string) (string, bool) {
	return markedDeclValue(trimmed, []string{"-", "－"}, []string{"←", "<-"})
}

func markedDeclValue(trimmed string, marks, arrows []string) (string, bool) {
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

func parseVariableDecl(value string) VariableDecl {
	value = strings.TrimSpace(value)
	description := ""
	if before, after, ok := strings.Cut(value, "#"); ok {
		value, description = strings.TrimSpace(before), strings.TrimSpace(after)
	}
	name, typ := value, ""
	if before, after, ok := strings.Cut(value, "%"); ok {
		name, typ = strings.TrimSpace(before), strings.TrimSpace(after)
	} else if before, after, ok := splitSectionColon(value); ok {
		name, typ = strings.TrimSpace(before), strings.TrimSpace(after)
	}
	return VariableDecl{Name: name, Type: canonicalBoolType(typ), Description: description}
}

// canonicalBoolType stores the verdict synonyms as bool. A trailing 。．.，,
// is punctuation, not part of the type. Other types stay as written, so
// text and string are not rewritten.
func canonicalBoolType(typ string) string {
	candidate := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(typ), "。．.，,"))
	switch strings.ToLower(candidate) {
	case "bool", "boolean", "布尔", "真假":
		return "bool"
	default:
		return typ
	}
}
