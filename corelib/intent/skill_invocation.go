package intent

import "strings"

// ExplicitSkillInvocation reports whether the user is asking the current
// agent to run a named skill in this conversation.
//
// That is a different act from starting a workflow_v2 project. workflow_task
// means "open /workflow or the workflow panel". Naming a skill (使用 book-pdf
// skill, use X skill) means "follow that skill here". Mixing the two is how
// "使用book pdf skill生成书籍" was refused as an unmapped workflow_task.
//
// Hyphenated names without the word skill (使用 book_pdf, 使用book-pdf) are
// not guessed here: "使用 open-source 方法写一份商业计划书" is still a
// panel start. Routing releases those only when skill-doc inject already
// selected an agent-guided skill.
func ExplicitSkillInvocation(text string) bool {
	s := foldSkillText(text)
	if s == "" {
		return false
	}
	for _, at := range skillTokenOffsets(s) {
		if invokeCueBefore(s, at) {
			return true
		}
	}
	return false
}

func foldSkillText(text string) string {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return ""
	}
	return strings.NewReplacer("\u2019", "'", "\u2018", "'").Replace(s)
}

func skillTokenOffsets(lower string) []int {
	var out []int
	start := 0
	for {
		i := strings.Index(lower[start:], "skill")
		if i < 0 {
			break
		}
		at := start + i
		if skillTokenBoundary(lower, at, at+len("skill")) {
			out = append(out, at)
		}
		start = at + len("skill")
	}
	start = 0
	for {
		i := strings.Index(lower[start:], "技能")
		if i < 0 {
			break
		}
		at := start + i
		if !humanAbilityJineng(lower, at) {
			out = append(out, at)
		}
		start = at + len("技能")
	}
	return out
}

func skillTokenBoundary(s string, start, end int) bool {
	if start > 0 && asciiLetter(s[start-1]) {
		return false
	}
	if end < len(s) && asciiLetter(s[end]) {
		return false
	}
	return true
}

func asciiLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func humanAbilityJineng(s string, at int) bool {
	for _, prefix := range []string{"沟通", "专业", "核心", "语言", "社交", "表达", "软", "硬"} {
		if at >= len(prefix) && s[at-len(prefix):at] == prefix {
			return true
		}
	}
	return false
}

func invokeCueBefore(s string, skillAt int) bool {
	if skillAt > 0 && skillAt+len("skill") <= len(s) && s[skillAt:skillAt+len("skill")] == "skill" && s[skillAt-1] == '/' {
		return true
	}
	window := prefixBefore(s, skillAt, 160)
	word, at, ok := rightmostInvokeCue(window)
	if !ok {
		return false
	}
	return !cueNegated(window, word, at)
}

func skillInvocationNegated(text string) bool {
	s := foldSkillText(text)
	if s == "" {
		return false
	}
	tokens := skillTokenOffsets(s)
	if len(tokens) == 0 {
		return false
	}
	for _, at := range tokens {
		if invokeCueBefore(s, at) {
			return false
		}
	}
	for _, at := range tokens {
		window := prefixBefore(s, at, 160)
		word, cueAt, ok := rightmostInvokeCue(window)
		if ok && cueNegated(window, word, cueAt) {
			return true
		}
	}
	return false
}

func prefixBefore(s string, at, maxBytes int) string {
	if at <= 0 {
		return ""
	}
	from := at - maxBytes
	if from < 0 {
		from = 0
	}
	for from < at && from > 0 && s[from]&0xC0 == 0x80 {
		from++
	}
	return s[from:at]
}

var zhInvokeCues = []string{"使用", "调用", "运行", "执行", "启动", "用"}
var enInvokeCues = []string{"using", "invoke", "run", "use"}

func rightmostInvokeCue(window string) (word string, at int, ok bool) {
	at = -1
	for _, cue := range zhInvokeCues {
		start := 0
		for {
			i := strings.Index(window[start:], cue)
			if i < 0 {
				break
			}
			pos := start + i
			start = pos + len(cue)
			if zhCueLexicalCompound(window, cue, pos) {
				continue
			}
			if pos > at || (pos == at && len(cue) > len(word)) {
				word, at, ok = cue, pos, true
			}
		}
	}
	for _, cue := range enInvokeCues {
		for _, pos := range englishWordOffsets(window, cue) {
			if pos > at || (pos == at && len(cue) > len(word)) {
				word, at, ok = cue, pos, true
			}
		}
	}
	return word, at, ok
}

func zhCueLexicalCompound(s, cue string, at int) bool {
	switch cue {
	case "用":
		if at >= len("使") && s[at-len("使"):at] == "使" {
			return true
		}
		return hasAnyPrefix(s[at+len("用"):], "户", "于", "处", "途", "意", "品", "量", "功", "工", "心", "法", "力")
	case "使用":
		return hasAnyPrefix(s[at+len("使用"):], "者", "率", "权", "费")
	case "运行":
		return hasAnyPrefix(s[at+len("运行"):], "时")
	case "启动":
		return hasAnyPrefix(s[at+len("启动"):], "器", "项", "时")
	case "执行":
		return hasAnyPrefix(s[at+len("执行"):], "力", "官")
	default:
		return false
	}
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func englishWordOffsets(s, word string) []int {
	var out []int
	start := 0
	for {
		i := strings.Index(s[start:], word)
		if i < 0 {
			return out
		}
		at := start + i
		end := at + len(word)
		if (at == 0 || !asciiLetter(s[at-1])) && (end == len(s) || !asciiLetter(s[end])) {
			out = append(out, at)
		}
		start = at + len(word)
	}
}

func cueNegated(window, word string, at int) bool {
	if at < 0 || at > len(window) {
		return false
	}
	before := window[:at]
	for _, cue := range enInvokeCues {
		if word == cue {
			return englishCueNegated(before)
		}
	}
	for _, p := range []string{"不要", "无需", "禁止", "别", "勿", "未", "不"} {
		if strings.HasSuffix(before, p) {
			return true
		}
	}
	return false
}

func englishCueNegated(before string) bool {
	t := strings.TrimRight(before, " \t")
	for _, n := range []string{
		"shouldn't", "should not", "don't", "dont", "do not", "never",
		"can't", "cannot", "won't", "wont",
	} {
		if !strings.HasSuffix(t, n) {
			continue
		}
		prev := t[:len(t)-len(n)]
		if prev == "" || !asciiLetter(prev[len(prev)-1]) {
			return true
		}
	}
	return false
}

// ReleaseNamedSkillFromWorkflowIntercept drops governed labels when the user
// named a skill to run in this turn. It does not grant a capability: an empty
// result is ungoverned chat — the same path the main assistant uses after
// skill-doc inject. Leftover Primary/WorkflowType would HostReject coding,
// lock onto generate_pdf, or still look like a workflow_v2 start.
func ReleaseNamedSkillFromWorkflowIntercept(text string, result *ClassificationResult) {
	ReleaseNamedSkillIntercept(text, false, result)
}

// ReleaseNamedSkillIntercept is the routing-time form: agentGuidedInjected is
// true when skill-doc inject already selected an imported agent-guided skill
// (Book-PDF) for this turn. Those turns must not HostReject as workflow_task
// even when the user omitted the word "skill".
func ReleaseNamedSkillIntercept(text string, agentGuidedInjected bool, result *ClassificationResult) {
	if result == nil || !namedSkillTurn(text, agentGuidedInjected) {
		return
	}
	if result.Primary == "" && len(result.Secondary) == 0 && result.WorkflowType == "" && result.RunnerUp == "" && !result.CreationOriented && len(result.ToolNames) == 0 {
		return
	}
	result.Primary = ""
	result.Secondary = nil
	result.WorkflowType = ""
	result.ToolNames = nil
	result.CreationOriented = false
	result.RunnerUp = ""
	result.RunnerUpScore = 0
	if result.Reason == "" {
		result.Reason = "named skill invocation is not a workflow_v2 start"
		return
	}
	if strings.Contains(result.Reason, "named skill") {
		return
	}
	result.Reason += "; named skill invocation released governed intercept"
}

func namedSkillTurn(text string, agentGuidedInjected bool) bool {
	if ExplicitSkillInvocation(text) {
		return true
	}
	return agentGuidedInjected && !skillInvocationNegated(text)
}

// NamedSkillInterceptCandidate is whether this classification would HostReject
// as a workflow panel start or steal the turn onto generate_pdf. Routing only
// consults skill-doc inject for these. ToolNames must not qualify: search and
// live_data always carry web_search, and scanning skills on those turns is how
// every weather question paid Book-PDF matching.
func NamedSkillInterceptCandidate(result ClassificationResult) bool {
	if result.CreationOriented {
		return true
	}
	if wt := strings.TrimSpace(result.WorkflowType); wt != "" && wt != "coding" {
		return true
	}
	for _, label := range result.Labels() {
		if label == LabelWorkflowTask || label == LabelDocumentGenerate {
			return true
		}
	}
	return false
}
