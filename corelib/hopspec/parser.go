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
	var plainBody *Step
	plainIndent := 0
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
		// A hop_python: label has no fence. The prompt forbids fences, so the
		// following lines are the body until the next step, heading, or declaration.
		if plainBody != nil {
			if PlainHopPythonEnds(trimmed) {
				finishPlainPython(plainBody)
				plainBody = nil
			} else {
				appendPlainPythonLine(plainBody, raw, plainIndent)
				plainBody.Source.LineEnd = lineNo
				continue
			}
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
			if current.Type == StepAct || current.Type == StepCheck || current.Type == StepCommit {
				if inline, ok := PlainHopPythonLabel(trimmed); ok {
					plainBody = current
					plainIndent = leadingSpaceCount(raw)
					if inline != "" {
						appendPlainPythonLine(current, inline, 0)
					}
					current.Source.LineEnd = lineNo
					continue
				}
			}
		}
		// One blockquote mark, then the same line. "> ## 目标" opens the goal.
		// A quoted line inside an open step was kept above, so it stays instruction.
		// The step matcher already ran, so "> 1. [exit]" is not a step.
		if rest, ok := oneBlockquote(trimmed); ok {
			trimmed = rest
			if trimmed == "" {
				continue
			}
		}
		if name, value, ok := atxSection(trimmed); ok && !(name == "" && section == "goal") {
			if value != "" {
				switch name {
				case "id":
					spec.ID = value
				case "goal":
					spec.Goal = goalValue(value)
				}
			}
			section = name
			current = nil
			continue
		}
		// Section labels only. Step bodies and hop_python lines are consumed above.
		// An unknown ## inside the goal stays above, so its sentence is the goal.
		if !strings.HasPrefix(trimmed, "-") {
			if name, value, ok := sectionLabel(trimmed); ok {
				prev := section
				if noteSectionLabel(&spec, &section, name, value) {
					if prev == "steps" && section != "steps" {
						current = nil
					}
					continue
				}
			}
			if section != "steps" {
				if name, ok := wholeLineSection(trimmed); ok {
					section = name
					continue
				}
			}
		}
		// One bullet or enumerator, then the same heading: "- 步骤", "1. 目标".
		// This stays outside an open step, so a bullet in the step body is not a heading.
		// The remainder must be a section name, so "1. 查询天气" stays prose.
		if section != "steps" {
			if rest := stripSectionMark(trimmed); rest != "" && rest != trimmed {
				if name, value, ok := sectionLabel(rest); ok {
					if noteSectionLabel(&spec, &section, name, value) {
						continue
					}
				} else if name, ok := wholeLineSection(rest); ok {
					section = name
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
	if plainBody != nil {
		finishPlainPython(plainBody)
	}
	for step := range bodyOpen {
		addErr("UNCLOSED_HOP_PYTHON", step.Source.LineStart, "hop_python fence is not closed")
	}
	if spec.Title == "" {
		addErr("MISSING_SPEC_TITLE", 0, "expected # Spec: <title>")
	}
	return spec, errs
}

// PlainHopPythonLabel reads a fence-less body label. "hop_python:" and
// "- hop_python:" open the body. Text after the colon is the first line.
// A YAML marker such as "hop_python: |" is only the label.
func PlainHopPythonLabel(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"- ", "－ ", "* "} {
		if strings.HasPrefix(trimmed, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			break
		}
	}
	if rest, ok := oneBlockquote(trimmed); ok {
		trimmed = rest
	}
	const key = "hop_python"
	if len(trimmed) < len(key) || !strings.EqualFold(trimmed[:len(key)], key) {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[len(key):])
	if rest == "" {
		return "", true
	}
	for _, colon := range []string{":", "："} {
		if !strings.HasPrefix(rest, colon) {
			continue
		}
		inline := strings.TrimSpace(rest[len(colon):])
		switch inline {
		case "", "|", "|-", "|+":
			return "", true
		default:
			return inline, true
		}
	}
	return "", false
}

func plainPythonStops(line string) bool {
	if SectionHeading(line) != "" {
		return true
	}
	if _, ok := inputDeclValue(line); ok {
		return true
	}
	if _, ok := toolDeclValue(line); ok {
		return true
	}
	if _, ok := outputDeclValue(line); ok {
		return true
	}
	return false
}

// PlainHopPythonEnds reports the line that closes a fence-less hop_python
// body: the next step, a section heading, or an input, tool, or output line.
func PlainHopPythonEnds(line string) bool {
	trimmed := strings.TrimSpace(line)
	if _, _, _, stepLine := matchStepLine(trimmed); stepLine {
		return true
	}
	return plainPythonStops(trimmed)
}

// finishPlainPython shifts the body so its top statement is at column 0.
// The engine rejects a top-level line that is still indented. Lines that
// were indented further keep that relative indent.
func finishPlainPython(step *Step) {
	step.Body = dedentPlainPython(step.Body)
}

func dedentPlainPython(body string) string {
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	min := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if n := leadingSpaceCount(line); min < 0 || n < min {
			min = n
		}
	}
	if min <= 0 {
		return body
	}
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || leadingSpaceCount(line) < min {
			continue
		}
		lines[i] = line[min:]
	}
	return strings.Join(lines, "\n")
}

func leadingSpaceCount(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func appendPlainPythonLine(step *Step, raw string, baseIndent int) {
	line := strings.TrimRight(raw, " \t")
	spaces := leadingSpaceCount(line)
	if baseIndent > 0 && spaces >= baseIndent {
		line = line[baseIndent:]
	} else if baseIndent > 0 {
		line = strings.TrimSpace(line)
	}
	if strings.TrimSpace(line) == "" {
		return
	}
	if step.Body != "" {
		step.Body += "\n"
	}
	step.Body += line
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

// atxSection reads a markdown heading. ## always starts a section, so an
// unknown heading still ends the current step. A single # is a section only
// when the label is a real one, so "# 注释" inside a goal stays prose.
// "1. 目标" under the hashes is that same section. "1. 说明" is not.
// A closing hash run, a fullwidth ＃, "目标 查询", and "【目标】" are the same heading.
func atxSection(line string) (string, string, bool) {
	level, heading, ok := atxMarks(line)
	if !ok {
		return "", "", false
	}
	label, value := heading, ""
	if key, rest, split := splitSectionColon(heading); split {
		label, value = key, rest
	}
	name := sectionToken(label)
	if name == "" && strings.TrimSpace(value) == "" {
		if inline, rest, split := splitInlineHeading(heading); split {
			name, value = inline, rest
		}
	}
	if glossOnly(value) {
		value = ""
	}
	if level < 2 && name == "" {
		return "", "", false
	}
	return name, cleanSectionToken(value), true
}

// atxMarks reads the opening hash run. Fullwidth ＃ is the same mark.
// Hashes that close the heading are not part of the label.
func atxMarks(line string) (int, string, bool) {
	level := 0
	rest := line
	for {
		switch {
		case strings.HasPrefix(rest, "#"):
			rest = rest[len("#"):]
		case strings.HasPrefix(rest, "＃"):
			rest = rest[len("＃"):]
		default:
			if level == 0 {
				return 0, "", false
			}
			heading := strings.TrimSpace(rest)
			heading = strings.TrimSpace(strings.TrimRight(heading, "#＃"))
			return level, heading, true
		}
		level++
	}
}

// sectionToken is the canonical section for a heading label. The label may
// be the bare name or one enumerator in front of it: "目标", "1. 目标",
// or "1. **目标**". Brackets or a parenthetical gloss, such as "【目标】"
// or "目标（必填）", are the same label. A sentence such as "1. 说明" is not.
func sectionToken(label string) string {
	label = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(label), ":："))
	if name := canonicalSection(cleanSectionToken(label)); name != "" {
		return name
	}
	cleaned := cleanSectionToken(label)
	if rest := stripSectionMark(cleaned); rest != "" && rest != cleaned {
		if name := canonicalSection(cleanSectionToken(rest)); name != "" {
			return name
		}
		cleaned = cleanSectionToken(rest)
	}
	if punct := trimHeadingPunct(cleaned); punct != cleaned {
		if name := canonicalSection(punct); name != "" {
			return name
		}
		cleaned = punct
	}
	if name, _, pure, ok := aliasSection(cleaned); ok && pure {
		return name
	}
	if name, after, ok := parenSection(cleaned); ok && strings.TrimSpace(after) == "" {
		return name
	}
	inner, trailing, ok := unwrapSectionBrackets(cleaned)
	if !ok {
		return ""
	}
	// "【目标】（必填）" is the goal. "【目标】查询" is not a heading:
	// the trailing sentence is not a gloss, and a whole-line heading has no value.
	if trailing = strings.TrimSpace(trailing); trailing != "" && !glossOnly(trailing) {
		return ""
	}
	inner = strings.TrimSpace(inner)
	if name := canonicalSection(cleanSectionToken(inner)); name != "" {
		return name
	}
	if rest := stripSectionMark(inner); rest != "" && rest != inner {
		if name := canonicalSection(cleanSectionToken(rest)); name != "" {
			return name
		}
		inner = strings.TrimSpace(cleanSectionToken(rest))
	}
	if name, after, ok := parenSection(inner); ok && strings.TrimSpace(after) == "" {
		return name
	}
	return ""
}

// parenSection reads a section name with one parenthetical gloss.
// "目标（必填）" and "Goal (required)" are the goal. The text after the
// closer is an inline value. "说明（补充）" and "查询（北京）" are not sections.
func parenSection(label string) (string, string, bool) {
	pairs := [][2]string{{"（", "）"}, {"(", ")"}}
	for _, pair := range pairs {
		idx := strings.Index(label, pair[0])
		if idx <= 0 {
			continue
		}
		head := strings.TrimSpace(label[:idx])
		name := canonicalSection(cleanSectionToken(head))
		if name == "" {
			continue
		}
		rest := label[idx+len(pair[0]):]
		end := strings.Index(rest, pair[1])
		if end < 0 {
			continue
		}
		return name, strings.TrimSpace(rest[end+len(pair[1]):]), true
	}
	return "", "", false
}

// glossOnly reports a value that is only a parenthetical note, such as "（Goal）".
func glossOnly(value string) bool {
	value = strings.TrimSpace(value)
	for _, pair := range [][2]string{{"（", "）"}, {"(", ")"}} {
		if len(value) <= len(pair[0])+len(pair[1]) || !strings.HasPrefix(value, pair[0]) || !strings.HasSuffix(value, pair[1]) {
			continue
		}
		inner := value[len(pair[0]) : len(value)-len(pair[1])]
		if !strings.Contains(inner, pair[0]) {
			return true
		}
	}
	return false
}

// splitInlineHeading reads a heading whose name is not the whole line.
// "目标 查询北京天气", "1. 目标 查询北京天气", and "【目标】查询北京天气" are the
// goal. "说明 补充" and "目标查询北京天气" are not headings.
func splitInlineHeading(heading string) (string, string, bool) {
	base := heading
	if rest := stripSectionMark(heading); rest != "" && rest != heading {
		base = rest
	}
	fields := strings.Fields(base)
	if len(fields) >= 2 {
		if name := sectionToken(fields[0]); name != "" {
			return name, strings.TrimSpace(strings.TrimPrefix(base, fields[0])), true
		}
	}
	inner, trailing, ok := unwrapSectionBrackets(strings.TrimSpace(base))
	if ok && strings.TrimSpace(trailing) != "" {
		if name := sectionToken(inner); name != "" {
			return name, strings.TrimSpace(trailing), true
		}
	}
	if name, after, ok := parenSection(strings.TrimSpace(base)); ok {
		return name, after, true
	}
	if name, value, pure, ok := aliasSection(strings.TrimSpace(base)); ok && !pure {
		return name, value, true
	}
	if name, value, ok := punctHeading(strings.TrimSpace(base)); ok {
		return name, value, true
	}
	return "", "", false
}

// trimHeadingPunct drops a trailing sentence mark. "目标。" is 目标.
// A sentence such as "查询。" is still not a section name.
func trimHeadingPunct(label string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(label), "。！？!?．."))
}

// aliasSection reads a bilingual heading. "目标/Goal" and "步骤｜Steps" are
// that section. The other side is an alias when it is also a section name.
// "目标/查询北京天气" keeps the sentence. "说明/补充" is not a section.
func aliasSection(label string) (string, string, bool, bool) {
	sepAt, sepLen := indexAliasSep(label)
	if sepAt <= 0 {
		return "", "", false, false
	}
	name := aliasName(label[:sepAt])
	if name == "" {
		return "", "", false, false
	}
	right := strings.TrimSpace(label[sepAt+sepLen:])
	if right == "" || aliasName(right) != "" {
		return name, "", true, true
	}
	return name, right, false, true
}

func aliasName(side string) string {
	side = cleanSectionToken(trimHeadingPunct(side))
	if name := canonicalSection(side); name != "" {
		return name
	}
	if name, after, ok := parenSection(side); ok && after == "" {
		return name
	}
	return ""
}

func indexAliasSep(label string) (int, int) {
	best, size := -1, 0
	for _, sep := range []string{"/", "／", "|", "｜"} {
		at := strings.Index(label, sep)
		if at >= 0 && (best < 0 || at < best) {
			best, size = at, len(sep)
		}
	}
	return best, size
}

// punctHeading reads a section name ended by a sentence mark, then a value.
// "目标。查询北京天气" is the goal. "查询。天气" is not a heading.
func punctHeading(label string) (string, string, bool) {
	best, sep := -1, ""
	for _, mark := range []string{"。", "！", "？", "!", "?", "．"} {
		at := strings.Index(label, mark)
		if at > 0 && (best < 0 || at < best) {
			best, sep = at, mark
		}
	}
	if best <= 0 {
		return "", "", false
	}
	name := aliasName(label[:best])
	if name == "" {
		return "", "", false
	}
	value := strings.TrimSpace(label[best+len(sep):])
	if value == "" {
		return "", "", false
	}
	return name, value, true
}

// unwrapSectionBrackets peels one bracket pair around a section name.
// The text after the closer is the inline value, and it may be empty.
func unwrapSectionBrackets(label string) (string, string, bool) {
	pairs := [][2]string{{"【", "】"}, {"［", "］"}, {"[", "]"}, {"「", "」"}, {"『", "』"}}
	for _, pair := range pairs {
		if !strings.HasPrefix(label, pair[0]) {
			continue
		}
		rest := label[len(pair[0]):]
		end := strings.Index(rest, pair[1])
		if end <= 0 {
			return "", "", false
		}
		return rest[:end], rest[end+len(pair[1]):], true
	}
	return "", "", false
}

// sectionLabel reads a section label that is not an ATX heading.
// "**目标**", "**目标：**", and "Goal: 查询" are labels. A sentence is not.
func sectionLabel(line string) (string, string, bool) {
	if name, value, ok := emphasisLabel(line); ok {
		return name, value, true
	}
	if label, ok := bareSectionLabel(line); ok {
		name := plainSectionName(label)
		if name == "" {
			return "", "", false
		}
		return name, "", true
	}
	key, value, ok := splitSectionColon(line)
	if !ok {
		return "", "", false
	}
	name := plainSectionName(key)
	if name == "" {
		return "", "", false
	}
	return name, cleanSectionToken(value), true
}

// plainSectionName is a section name with no enumerator. "目标（必填）" is
// the goal. "1. 目标" is not, so a numbered line inside a step stays put.
func plainSectionName(label string) string {
	label = cleanSectionToken(label)
	if name := canonicalSection(label); name != "" {
		return name
	}
	if name, after, ok := parenSection(label); ok && after == "" {
		return name
	}
	if punct := trimHeadingPunct(label); punct != label {
		if name := canonicalSection(punct); name != "" {
			return name
		}
	}
	if name, _, pure, ok := aliasSection(label); ok && pure {
		return name
	}
	// "【目标】：查询" is the goal. The colon was already split off.
	// An enumerator stays in place, so "1. 目标：查询" inside a step is not a heading.
	inner, trailing, ok := unwrapSectionBrackets(label)
	if !ok {
		return ""
	}
	if trailing = strings.TrimSpace(trailing); trailing != "" && !glossOnly(trailing) {
		return ""
	}
	return plainSectionName(strings.TrimSpace(inner))
}

// wholeLineSection reads a line that is only the section name, such as "目标", "步骤", or "【目标】".
func wholeLineSection(line string) (string, bool) {
	if strings.ContainsAny(line, ":：") {
		return "", false
	}
	name := sectionToken(line)
	if name == "" {
		return "", false
	}
	return name, true
}

// SectionHeading is the section a heading line opens. A sentence that only
// contains a colon, such as "输出：温度", is not a heading. A list mark in
// front of the name, such as "1. 目标" or "- 步骤", is the same heading.
// The compiler uses this so a title is added for the same headings the parser accepts.
func SectionHeading(line string) string {
	line = strings.TrimSpace(line)
	if name := headingName(line); name != "" {
		return name
	}
	rest, ok := oneBlockquote(line)
	if !ok {
		return ""
	}
	return headingName(rest)
}

func headingName(line string) string {
	if name := namedHeading(line); name != "" {
		return name
	}
	rest := stripSectionMark(line)
	if rest == "" || rest == line {
		return ""
	}
	return namedHeading(rest)
}

// oneBlockquote strips one leading ">". The caller checks the remainder,
// so "> 查询北京天气" stays prose and "> 1. [exit]" is not matched as a step.
func oneBlockquote(line string) (string, bool) {
	if !strings.HasPrefix(line, ">") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, ">")), true
}

func namedHeading(line string) string {
	if name, _, ok := atxSection(line); ok && name != "" {
		return name
	}
	if name, value, ok := sectionLabel(line); ok && (value == "" || name == "goal" || name == "id") {
		return name
	}
	if name, ok := wholeLineSection(line); ok {
		return name
	}
	return ""
}

var sectionEnumPattern = regexp.MustCompile(`^(?:\d+|[一二三四五六七八九十]+)[.、．]\s*|^[（(](?:\d+|[一二三四五六七八九十]+)[）)]\s*`)

// stripSectionMark removes one bullet or enumerator. The caller checks
// whether the remainder is a section name, so "1. 查询天气" stays prose.
func stripSectionMark(line string) string {
	for _, prefix := range []string{"- ", "－ ", "* ", "• ", "· ", "● "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	if loc := sectionEnumPattern.FindStringIndex(line); loc != nil {
		return strings.TrimSpace(line[loc[1]:])
	}
	return line
}

// emphasisLabel reads a label wrapped in * or _. The text after the closing
// mark is the value, which may be empty when the sentence is the next line.
func emphasisLabel(line string) (string, string, bool) {
	rest := strings.TrimSpace(line)
	mark := ""
	switch {
	case strings.HasPrefix(rest, "**"), strings.HasPrefix(rest, "__"):
		mark = rest[:2]
	case strings.HasPrefix(rest, "*"), strings.HasPrefix(rest, "_"):
		mark = rest[:1]
	default:
		return "", "", false
	}
	rest = strings.TrimSpace(rest[len(mark):])
	end := strings.Index(rest, mark)
	if end <= 0 {
		return "", "", false
	}
	label := strings.TrimRight(strings.TrimSpace(rest[:end]), ":：")
	name := sectionToken(label)
	if name == "" {
		return "", "", false
	}
	return name, cleanSectionToken(strings.TrimSpace(rest[end+len(mark):])), true
}

// noteSectionLabel stores a Goal or Id value and moves to that section.
// An empty label is a heading, so "输出：" opens Outputs. A sentence such as
// "输出：温度和风力" stays in the open goal. A sentence under an open step
// does not leave the step, so the step's later output line still attaches.
func noteSectionLabel(spec *SpecAST, section *string, name, value string) bool {
	if name == "" {
		return false
	}
	// A sentence stays in the open goal. "标识：必须写清城市" is that sentence.
	// "Id: slug" before the goal still assigns the id, because the goal is not open.
	if name != "goal" && *section == "goal" && value != "" {
		return false
	}
	switch name {
	case "id":
		if value != "" {
			spec.ID = value
		}
	case "goal":
		value = goalValue(value)
		if value != "" {
			if *section == "goal" && spec.Goal != "" && spec.Goal != value {
				spec.Goal = appendGoalBody(spec.Goal, value)
			} else {
				spec.Goal = value
			}
		}
	}
	if value == "" || ((name == "goal" || name == "id") && *section != "steps") {
		*section = name
	}
	return true
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
	text := goalValue(line)
	switch {
	case strings.HasPrefix(text, "- "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "- "))
	case strings.HasPrefix(text, "－ "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "－ "))
	case strings.HasPrefix(text, "* "):
		text = strings.TrimSpace(strings.TrimPrefix(text, "* "))
	}
	return text
}

// goalValue keeps the sentence of a goal. One matching quote pair is that
// sentence. A YAML block marker, such as "|" or ">", is not the sentence, so
// the following paragraph is the goal. One leading ">" in front of a sentence
// is the same sentence. An id value does not come through here.
func goalValue(value string) string {
	text := cleanSectionToken(unwrapGoalQuotes(cleanSectionToken(value)))
	if yamlBlockMarker(text) {
		return ""
	}
	rest, ok := oneBlockquote(text)
	if !ok {
		return text
	}
	text = cleanSectionToken(unwrapGoalQuotes(rest))
	if yamlBlockMarker(text) {
		return ""
	}
	return text
}

func unwrapGoalQuotes(value string) string {
	pairs := [][2]string{
		{`"`, `"`},
		{`'`, `'`},
		{"“", "”"},
		{"‘", "’"},
		{"「", "」"},
		{"『", "』"},
	}
	for _, pair := range pairs {
		open, close := pair[0], pair[1]
		if len(value) < len(open)+len(close) || !strings.HasPrefix(value, open) || !strings.HasSuffix(value, close) {
			continue
		}
		inner := value[len(open) : len(value)-len(close)]
		if strings.Contains(inner, open) {
			return value
		}
		return strings.TrimSpace(inner)
	}
	return value
}

func yamlBlockMarker(text string) bool {
	switch text {
	case "|", "|-", "|+", ">", ">-", ">+":
		return true
	default:
		return false
	}
}

// cleanSectionToken drops markdown emphasis around a section label or value.
// Emphasis can sit on either side of the colon, so stars and spaces are peeled
// until the token is stable: "**目标：** 查询" yields 目标 and 查询.
// One code span around the whole token is the same token: "`目标`" is 目标.
// A span in the middle of a sentence, such as "使用 `web_search`", stays.
func cleanSectionToken(value string) string {
	for {
		next := strings.TrimSpace(strings.Trim(strings.TrimSpace(value), "*_"))
		next = unwrapCodeSpan(next)
		if next == value {
			return value
		}
		value = next
	}
}

// unwrapCodeSpan peels one matching backtick run around the whole token.
// The inner text must not contain that same run.
func unwrapCodeSpan(value string) string {
	if !strings.HasPrefix(value, "`") || !strings.HasSuffix(value, "`") {
		return value
	}
	n := 0
	for n < len(value) && value[n] == '`' {
		n++
	}
	if n == 0 || len(value) < n*2 || !strings.HasSuffix(value, strings.Repeat("`", n)) {
		return value
	}
	inner := value[n : len(value)-n]
	if strings.TrimSpace(inner) == "" || strings.Contains(inner, strings.Repeat("`", n)) {
		return value
	}
	return strings.TrimSpace(inner)
}

// specTitle reads "# Spec: title". One leading ">" is the same title.
// ">> # Spec" is not, and this runs before step matching, so "> 1. [exit]"
// is still not a step.
func specTitle(line string) (string, bool) {
	if title, ok := specTitlePrefix(line); ok {
		return title, true
	}
	rest, ok := oneBlockquote(line)
	if !ok {
		return "", false
	}
	return specTitlePrefix(rest)
}

func specTitlePrefix(line string) (string, bool) {
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
