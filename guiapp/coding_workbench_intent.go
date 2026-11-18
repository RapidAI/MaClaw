package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const codingRequestUnderstandingMaxRunes = 240

const codingRequestUnderstandingSystemPrompt = `Restate the user's coding-workbench request as the system's understood goal. Return JSON only.

Schema: {"restatement":"..."}

Rules:
- 1 to 3 sentences.
- Do not copy the user's wording. Paraphrase the goal, the expected deliverable, and any constraint you can infer.
- If the request is a short follow-up, use the session context to name the current software and what will change.
- If the session record says project files were already written, the workspace is not read-only. Do not describe it as unable to change files.
- Do not invent features the user did not imply.
- Write in the same language as the user request.
- No markdown headings, no file lists, no step plan.`

func codingRequestShouldRestate(decision codingRequestDecision, userText string) bool {
	if strings.TrimSpace(userText) == "" || codingRequestLooksExplicitWorkspaceClear(userText) {
		return false
	}
	switch decision.Kind {
	case codingRequestImplementation, codingRequestOperational, codingRequestInquiry:
		return true
	default:
		return false
	}
}

func normalizeCodingRestatementKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func codingRestatementCopiesUser(restatement, userText string) bool {
	a := normalizeCodingRestatementKey(restatement)
	b := normalizeCodingRestatementKey(userText)
	return a != "" && a == b
}

func parseCodingRequestRestatement(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var payload struct {
		Restatement string `json:"restatement"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Restatement)
}

func codingSessionContextLooksGeneric(s string) bool {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return true
	}
	lower := strings.ToLower(trim)
	for _, prefix := range []string{
		"新建本地编程任务",
		"新建远程编程任务",
		"新建本機程式任務",
		"新建遠端程式任務",
		"new local coding task",
		"new remote coding task",
	} {
		if strings.HasPrefix(trim, prefix) || strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func codingSourceStemLooksGeneric(stem string) bool {
	switch strings.ToLower(strings.TrimSpace(stem)) {
	case "", "main", "app", "src", "index", "program", "test", "tests", "cmake", "makefile", "build":
		return true
	default:
		return false
	}
}

func codingReadmeLineLooksGeneric(line string) bool {
	if codingSessionContextLooksGeneric(line) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "todo", "wip", "demo", "test", "project", "readme", "untitled":
		return true
	default:
		return false
	}
}

func codingReadmeIdentityHint(dir string) string {
	for _, name := range []string{"README.md", "readme.md", "README.txt", "readme.txt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			continue
		}
		if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
			data = data[3:]
		}
		if len(data) > 2048 {
			data = data[:2048]
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
			line = strings.TrimSpace(strings.TrimLeft(line, "#"))
			// A sentence is not a title. Cutting at the first 。 would keep
			// the words in front of it and hide the project name. A later
			// short line can still be the title. Other prose stops the
			// walk so a following fragment does not replace the CMake name.
			if line == "" || codingReadmeLineLooksGeneric(line) || codingWorkNameIsSentence(line) {
				continue
			}
			if codingShortWorkName(line) == "" {
				return ""
			}
			return line
		}
	}
	return ""
}

func codingCollectUniqueSourceStems(dir string, limit int) map[string]string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]string{}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".go", ".rs", ".py", ".exe":
		default:
			continue
		}
		n++
		if n > 48 {
			break
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if codingSourceStemLooksGeneric(stem) {
			continue
		}
		key := strings.ToLower(stem)
		if _, ok := seen[key]; !ok {
			seen[key] = stem
		}
		if limit > 0 && len(seen) > limit {
			return seen
		}
	}
	return seen
}

func codingUniqueMapValue(seen map[string]string) string {
	if len(seen) != 1 {
		return ""
	}
	for _, stem := range seen {
		return stem
	}
	return ""
}

func codingTopLevelSourceIdentityHint(dir string) string {
	if got := codingUniqueMapValue(codingCollectUniqueSourceStems(dir, 2)); got != "" {
		return got
	}
	for _, sub := range []string{"src", "source", "app"} {
		if got := codingUniqueMapValue(codingCollectUniqueSourceStems(filepath.Join(dir, sub), 2)); got != "" {
			return got
		}
	}
	return ""
}

func codingCMakeProjectIdentityHint(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "CMakeLists.txt"))
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > 4096 {
		data = data[:4096]
	}
	return codingCMakeProjectName(string(data))
}

// codingCMakeProjectName returns the first literal project() name. A call
// in a comment, or a ${variable} name, is not that name and must not hide
// a later project() or the source stem.
func codingCMakeProjectName(raw string) string {
	lower := strings.ToLower(raw)
	from := 0
	for from < len(lower) {
		rel := strings.Index(lower[from:], "project(")
		if rel < 0 {
			return ""
		}
		idx := from + rel
		restFrom := idx + len("project(")
		from = restFrom
		if codingCMakeCallIsCommented(raw, idx) {
			continue
		}
		name := codingCMakeProjectToken(raw[restFrom:])
		if name == "" || strings.ContainsAny(name, "${}") || codingSourceStemLooksGeneric(name) || codingSessionContextLooksGeneric(name) {
			continue
		}
		return name
	}
	return ""
}

func codingCMakeCallIsCommented(raw string, idx int) bool {
	lineStart := strings.LastIndex(raw[:idx], "\n") + 1
	return strings.Contains(raw[lineStart:idx], "#")
}

func codingCMakeProjectToken(rest string) string {
	rest = strings.TrimSpace(rest)
	rest = strings.TrimLeft(rest, " \t\"'")
	end := len(rest)
	for i, r := range rest {
		if r == ')' || r == ' ' || r == '\t' || r == '\n' || r == ',' {
			end = i
			break
		}
	}
	return strings.Trim(strings.TrimSpace(rest[:end]), "\"'")
}

func codingBinaryIdentityHint(dir string) string {
	skip := map[string]struct{}{
		"cmake": {}, "all_build": {}, "zero_check": {}, "install": {}, "run_tests": {}, "ctest": {},
	}
	seen := map[string]string{}
	for _, sub := range []string{".", "build", "out", "bin"} {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue
		}
		n := 0
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.ToLower(filepath.Ext(name)) != ".exe" {
				continue
			}
			n++
			if n > 32 {
				break
			}
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			key := strings.ToLower(stem)
			if codingSourceStemLooksGeneric(stem) {
				continue
			}
			if _, blocked := skip[key]; blocked {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = stem
			}
		}
	}
	return codingUniqueMapValue(seen)
}

// codingWorkspaceWorkName is the first project identity that can name the
// work. A later file is read only when the earlier line is not a usable
// title, so a readme hit does not scan the source tree or the build dir.
func codingWorkspaceWorkName(projectPath, userText string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return ""
	}
	for _, hint := range []func(string) string{
		codingReadmeIdentityHint,
		codingCMakeProjectIdentityHint,
		codingTopLevelSourceIdentityHint,
		codingBinaryIdentityHint,
	} {
		if name := codingUsableWorkName(hint(projectPath), userText); name != "" {
			return name
		}
	}
	return ""
}

// codingPriorRepeatsAsk reports a work name that is only the current request.
// A new task is often titled with the user's own sentence; wrapping that
// sentence again ("在「同一句话」里，这次同一句话") adds no context.
func codingPriorRepeatsAsk(name, ask string) bool {
	a := normalizeCodingRestatementKey(name)
	b := normalizeCodingRestatementKey(ask)
	return a != "" && a == b
}

// codingUsableWorkName is a short title that names something other than the
// request. The stored text is compared before it is shortened, so a comma
// note on the same sentence does not become a different name. A requirement
// paragraph is not a title and returns empty so the next candidate is tried.
func codingUsableWorkName(raw, userText string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || codingPriorRepeatsAsk(raw, userText) {
		return ""
	}
	name := codingShortWorkName(raw)
	if name == "" || codingPriorRepeatsAsk(name, userText) {
		return ""
	}
	return name
}

// codingRequestWorkName is the first stored title that names something other
// than the sentence the user just sent. A task title equal to that sentence
// is skipped, and the summary or project identity behind it is still used.
func codingRequestWorkName(mem stickyCodingWorkbenchMemory, userText string) string {
	for _, raw := range []string{mem.SessionPlan, mem.LastSummary} {
		if name := codingUsableWorkName(raw, userText); name != "" {
			return name
		}
	}
	return codingWorkspaceWorkName(mem.ProjectPath, userText)
}

// codingShortWorkName is a title that can sit inside parentheses.
// A requirement sentence, a colon clause, or a numbered list is not a title.
func codingShortWorkName(s string) string {
	raw := strings.TrimSpace(s)
	if raw == "" || codingSessionContextLooksGeneric(raw) {
		return ""
	}
	if strings.Contains(raw, "\n") || strings.Contains(raw, "（") || strings.Contains(raw, "(") ||
		strings.Contains(raw, "）") || strings.Contains(raw, ")") ||
		strings.Contains(raw, "：") || strings.Contains(raw, ":") ||
		strings.Contains(raw, "——") || strings.Contains(raw, "、") {
		return ""
	}
	name := raw
	// "控制台贪吃蛇, ANSI" is a title plus a short note. A comma that starts
	// the rest of a sentence is not a title boundary.
	if i := strings.IndexAny(raw, "，,"); i > 0 {
		head := strings.TrimSpace(raw[:i])
		rest := strings.TrimSpace(strings.TrimLeft(raw[i:], "，,"))
		if head == "" || rest == "" || utf8.RuneCountInString(rest) > 12 || strings.ContainsAny(rest, "，,。；;") {
			return ""
		}
		name = head
	}
	// "已完成编译。" and "编译已经完成。详见 simd" are status sentences.
	// Keeping the words in front of the mark would hide the project name.
	if codingWorkNameIsSentence(name) {
		return ""
	}
	name = strings.Trim(name, " 。；;")
	if name == "" || codingSessionContextLooksGeneric(name) {
		return ""
	}
	n := utf8.RuneCountInString(name)
	if n < 2 || n > 24 {
		return ""
	}
	return name
}

// codingWorkNameIsSentence reports a line that closes a sentence or
// continues past one. A dot inside snake.cpp or v1.2 joins an identifier
// and is not that boundary.
func codingWorkNameIsSentence(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for i, r := range s {
		switch r {
		case '。', '！', '？', '；', ';', '…', '!', '?':
			return true
		case '.':
			if !codingDotJoinsIdentifier(s, i) {
				return true
			}
		}
	}
	return false
}

func codingDotJoinsIdentifier(s string, dotByte int) bool {
	if dotByte <= 0 || dotByte+1 >= len(s) {
		return false
	}
	prev, _ := utf8.DecodeLastRuneInString(s[:dotByte])
	next, _ := utf8.DecodeRuneInString(s[dotByte+1:])
	return codingIdentifierRune(prev) && codingIdentifierRune(next)
}

func codingIdentifierRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// codingRequirementGoalCut is the byte index where a numbered breakdown
// starts. A fullwidth colon is not a cut: "完成度：六条线都要有证据" is one
// requirement, and stopping at the colon drops the constraint. An ASCII
// colon stays in the text so a label like "10:30" is left intact.
func codingRequirementGoalCut(s string) int {
	cut := -1
	for _, marker := range []string{"（1）", "(1)"} {
		i := codingListMarkerIndex(s, marker)
		if i >= 0 && (cut < 0 || i < cut) {
			cut = i
		}
	}
	return cut
}

// codingListMarkerIndex finds a breakdown marker that starts a list.
// "foo(1)" is a call, not item 1, so the marker needs a boundary before it.
func codingListMarkerIndex(s, marker string) int {
	start := 0
	for start < len(s) {
		rel := strings.Index(s[start:], marker)
		if rel < 0 {
			return -1
		}
		i := start + rel
		if i == 0 || codingListMarkerBoundary(s, i) {
			return i
		}
		start = i + len(marker)
	}
	return -1
}

func codingListMarkerBoundary(s string, i int) bool {
	r, size := utf8.DecodeLastRuneInString(s[:i])
	if r == utf8.RuneError && size <= 1 {
		return false
	}
	switch r {
	case ' ', '\t', '\n', '，', ',', '、', '。', '；', ';', '：', ':', '—', '-', '（', '(':
		return true
	default:
		return false
	}
}

// codingRequirementGoalClause is the goal before a breakdown. The list that
// follows does not belong in the one-line understanding.
func codingRequirementGoalClause(s string) string {
	i := codingRequirementGoalCut(s)
	if i <= 0 {
		return ""
	}
	head := strings.TrimSpace(s[:i])
	head = strings.TrimRight(head, " ，,、—-：: \t")
	// A long preamble still ends at the list. Falling through to the rune
	// window would take the first list item back in.
	if utf8.RuneCountInString(head) > 80 {
		runes := []rune(head)
		runes = trimCodingSnippetToBoundary(runes[:80])
		head = strings.TrimSpace(string(trimUnbalancedCodingParens(runes)))
		head = strings.TrimRight(head, " ，,、—-：: \t")
	}
	if strings.Count(head, "（") != strings.Count(head, "）") || strings.Count(head, "(") != strings.Count(head, ")") {
		// Dropping the whole head would fall through into the numbered list.
		head = strings.TrimSpace(string(trimUnbalancedCodingParens([]rune(head))))
		head = strings.TrimRight(head, " ，,、—-：: \t")
	}
	n := utf8.RuneCountInString(head)
	// Two characters is enough when a list was cut off. A shorter head is
	// not a sentence, and copying the list body would bring "（1）" back.
	if n < 2 || n > 80 {
		return ""
	}
	return head
}

// codingClosedRequirementSnippet is the session requirement with any cut
// kept on a clause boundary and with parentheses still balanced.
func codingClosedRequirementSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || codingSessionContextLooksGeneric(s) || codingShortWorkName(s) != "" {
		return ""
	}
	if codingRequirementGoalCut(s) >= 0 {
		return codingRequirementGoalClause(s)
	}
	const maxRunes = 100
	runes := []rune(s)
	limit := len(runes)
	trimmed := false
	if limit > maxRunes {
		limit = maxRunes
		trimmed = true
	}
	window := runes[:limit]
	if !trimmed {
		if balanced, ok := takeBalancedCodingParens(window); ok {
			return strings.TrimSpace(string(balanced))
		}
	}
	cutAtSentence := false
	for i, r := range window {
		if i > 8 && (r == '。' || r == '！' || r == '？' || r == '\n') {
			window = window[:i]
			trimmed = true
			cutAtSentence = true
			break
		}
	}
	// A hard rune cap must back up to a clause boundary. A sentence cut is
	// already on a boundary; backing up again would drop the clause before
	// the first comma.
	if trimmed && !cutAtSentence {
		window = trimCodingSnippetToBoundary(window)
	}
	window = trimUnbalancedCodingParens(window)
	out := strings.Trim(string(window), " \t\n，,、：:；;。—")
	if utf8.RuneCountInString(out) < 8 {
		return ""
	}
	if trimmed {
		out += "…"
	}
	return out
}

func takeBalancedCodingParens(runes []rune) ([]rune, bool) {
	open := 0
	for _, r := range runes {
		switch r {
		case '（', '(':
			open++
		case '）', ')':
			if open == 0 {
				return nil, false
			}
			open--
		}
	}
	if open != 0 {
		return nil, false
	}
	return runes, true
}

func trimCodingSnippetToBoundary(runes []rune) []rune {
	for i := len(runes) - 1; i > 8; i-- {
		switch runes[i] {
		case '、', '，', ',', '。', '；', ';', '\n', ' ':
			return runes[:i]
		}
	}
	return runes
}

func trimUnbalancedCodingParens(runes []rune) []rune {
	var openAt []int
	for i, r := range runes {
		switch r {
		case '（', '(':
			openAt = append(openAt, i)
		case '）', ')':
			if len(openAt) > 0 {
				openAt = openAt[:len(openAt)-1]
			}
		}
	}
	if len(openAt) == 0 {
		return runes
	}
	return runes[:openAt[0]]
}

func attachCodingWorkRoot(mem *stickyCodingWorkbenchMemory, workRoot string) {
	if mem == nil {
		return
	}
	workRoot = strings.TrimSpace(workRoot)
	if workRoot == "" {
		return
	}
	mem.ProjectPath = workRoot
}

func fallbackCodingRequestRestatement(userText string, mem stickyCodingWorkbenchMemory) string {
	return fallbackCodingRequestRestatementFor(userText, mem, codingRequestDecision{})
}

// fallbackCodingRequestRestatementFor names the work from the request decision.
// That decision judges the whole request. The sentence does not scan the
// wording for question words, change verbs, or a GUI mention.
func fallbackCodingRequestRestatementFor(userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision) string {
	userText = strings.TrimSpace(userText)
	prior := codingRequestWorkName(mem, userText)
	continueCue := codingWorkbenchContinueCue(userText)
	if !continueCue {
		switch decision.Kind {
		case codingRequestInquiry:
			return codingDescribeQuestion(prior, userText, codingSessionHasImplementationTrajectory(mem))
		case codingRequestOperational:
			if decision.Acceptance != codingOperationalAcceptanceCommand {
				line := "运行当前已有程序并确认它能正常工作，这一步不改源代码。"
				if prior != "" {
					return "在" + codingQuoteWorkName(prior) + "里，" + line
				}
				return line
			}
		}
	}
	if utf8.RuneCountInString(userText) <= 48 {
		// A title stays a title. A stored requirement is restated as the
		// work itself. A new short ask is that ask, not a continue template.
		ask := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(userText), "。！？!?"))
		if continueCue {
			echo := codingClosedRequirementSnippet(mem.SessionPlan)
			if echo != "" || codingRequirementGoalCut(mem.SessionPlan) >= 0 {
				if sentence := codingDescribeStoredGoal(echo, mem.SessionPlan); sentence != "" {
					return sentence
				}
			}
			if prior != "" {
				return "继续做" + codingQuoteWorkName(prior) + "，从上次停下的地方接着做。"
			}
			// A bare continue with nothing stored has no ask to name.
			return "先看清当前项目里相关的实现和约束，再按你这次的要求修改并验证。"
		}
		if ask != "" {
			if codingRequirementGoalCut(ask) >= 0 {
				// A list-style ask names its goal or items, not the markers.
				if head, more := codingRequestHeadline(ask); head != "" {
					return codingDescribeHeadline(prior, head, more)
				}
			}
			// A new ask names itself even with no stored plan or work name.
			return codingDescribeNewAsk(prior, ask)
		}
	}
	if !continueCue {
		if head, more := codingRequestHeadline(userText); head != "" {
			// A long ask still leads with its own first requirement instead of
			// a process description.
			return codingDescribeHeadline(prior, head, more)
		}
	}
	return "先看清当前项目里相关的实现和约束，再按你这次的要求修改并验证。"
}

// codingDescribeHeadline renders a named lead requirement. more keeps the
// parts past the head acknowledged instead of claiming them out of scope.
func codingDescribeHeadline(prior, head string, more bool) string {
	line := "先处理「" + head + "」"
	if more {
		line += "，其余要求一并处理"
	}
	if prior != "" {
		return "在" + codingQuoteWorkName(prior) + "里，" + line + "。"
	}
	return line + "。"
}

// codingLeadingInterjection marks a discourse opener that is not itself a
// requirement; the headline peels it so the head names the real ask.
func codingLeadingInterjection(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "太好了", "好的", "好", "嗯", "嗯嗯", "谢谢", "多谢", "辛苦了",
		"帮我", "麻烦", "麻烦你", "ok", "okay", "thanks", "great", "nice":
		return true
	}
	return false
}

// codingRequestHeadline extracts an ask's lead requirement: the first
// clause at a sentence boundary, or the first comma clause when that
// clause is already a usable phrase. Leading interjections and fragments
// under two runes are peeled. more reports the request continues past head.
func codingRequestHeadline(userText string) (head string, more bool) {
	text := strings.TrimSpace(userText)
	if text == "" {
		return "", false
	}
	rest := ""
	head = strings.TrimSpace(strings.TrimRight(text, "。！？!?：:，,、 "))
	for {
		i := strings.IndexAny(head, "。！？!?；;\n，,")
		if i < 0 {
			break
		}
		r, size := utf8.DecodeRuneInString(head[i:])
		first := strings.TrimSpace(head[:i])
		if first == "" || codingLeadingInterjection(first) || utf8.RuneCountInString(first) < 2 {
			head = strings.TrimSpace(head[i+size:])
			continue
		}
		// A sentence boundary always cuts; a comma clause must already be a
		// usable phrase before it can stand alone.
		if (r != '，' && r != ',') || utf8.RuneCountInString(first) >= 6 {
			head, rest = first, head[i:]
		}
		break
	}
	head = strings.TrimSpace(head)
	if cut := codingRequirementGoalCut(head); cut >= 0 {
		// A goal head before a numbered breakdown is the one-line
		// requirement; the list that follows stays acknowledged via more.
		goal := strings.TrimRight(strings.TrimSpace(head[:cut]), " ，,、—-：: \t")
		list := head[cut:]
		if utf8.RuneCountInString(goal) >= 2 && !codingLeadingInterjection(goal) {
			head, rest = goal, list
		} else {
			head = ""
			spans := codingNumberedMarkerSpans(list)
			items := codingNumberedListItems(list)
			if len(items) > 0 && len(items) == len(spans) {
				// A bare list of short items names them all — but only
				// when no item hid a longer clause behind a separator.
				covered := true
				for i, item := range items {
					end := len(list)
					if i+1 < len(spans) {
						end = spans[i+1].marker
					}
					if raw := strings.Trim(strings.TrimSpace(list[spans[i].content:end]), "、，,；; "); raw != item {
						covered = false
						break
					}
				}
				if covered {
					head = strings.Join(items, "、")
				}
			}
			if head == "" && len(spans) > 0 {
				// Long items name the first one in full; the rest stays
				// acknowledged.
				end := len(list)
				if len(spans) > 1 {
					end = spans[1].marker
				}
				head = strings.Trim(strings.TrimSpace(list[spans[0].content:end]), "、，,；; ")
				if tail := list[end:]; strings.TrimSpace(tail) != "" {
					rest = tail
				}
			}
		}
	}
	head = strings.NewReplacer("「", "", "」", "").Replace(head)
	if utf8.RuneCountInString(head) < 2 {
		return "", false
	}
	if utf8.RuneCountInString(head) > 40 {
		runes := trimCodingSnippetToBoundary([]rune(truncateRunesV2(head, 40)))
		runes = trimUnbalancedCodingParens(runes)
		head = strings.TrimRight(strings.TrimSpace(string(runes)), " ，,、：:；;—")
		if utf8.RuneCountInString(head) < 2 {
			return "", false
		}
		return head + "…", true
	}
	return head, strings.TrimSpace(rest) != ""
}

// codingDescribeStoredGoal turns a stored requirement into one sentence
// about the work. "你要的不是再写代码，而是核查：对6条线" becomes
// "对6条线，做核查，而不是再写代码。" Complete list titles are named;
// the "（1）" markers are not.
func codingDescribeStoredGoal(echo, source string) string {
	echo = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(echo), "。"))
	if echo == "" {
		return codingAppendListItems("", source)
	}
	var sentence string
	if neg, pos, ok := codingSplitNotBut(echo); ok {
		pos = codingSoftenGoalColon(pos)
		neg = strings.TrimSpace(strings.TrimRight(neg, "。"))
		if pos != "" && neg != "" {
			sentence = pos + "，而不是" + neg + "。"
		}
	}
	if sentence == "" {
		softened := codingSoftenGoalColon(echo)
		if softened == "" {
			return ""
		}
		sentence = softened + "。"
	}
	return codingAppendListItems(sentence, source)
}

func codingSplitNotBut(s string) (neg, pos string, ok bool) {
	i := strings.Index(s, "不是")
	if i < 0 {
		return "", "", false
	}
	rest := s[i+len("不是"):]
	j := strings.Index(rest, "而是")
	if j < 0 {
		return "", "", false
	}
	neg = strings.Trim(rest[:j], "，, \t")
	pos = strings.TrimSpace(rest[j+len("而是"):])
	if neg == "" || pos == "" || utf8.RuneCountInString(neg) > 24 || utf8.RuneCountInString(pos) < 4 {
		return "", "", false
	}
	return neg, pos, true
}

// codingSoftenGoalColon turns "一份核查：对6条线" into "对6条线，做一份核查".
// A verb on the left, such as "请核对：对6条线", stays in front. Putting
// 做 before 请核对 does not read as a sentence. Any other colon becomes
// a comma so the constraint after it remains.
func codingSoftenGoalColon(s string) string {
	s = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s), "。"))
	i := strings.Index(s, "：")
	if i <= 0 {
		return s
	}
	left := strings.TrimSpace(s[:i])
	right := strings.TrimSpace(strings.TrimRight(s[i+len("："):], "。"))
	if left == "" || right == "" {
		return s
	}
	if strings.HasPrefix(right, "对") && codingLeftIsNounPhrase(left) {
		return right + "，做" + left
	}
	return left + "，" + right
}

func codingLeftIsNounPhrase(left string) bool {
	for _, p := range []string{"一份", "一个", "一项", "一套"} {
		if strings.HasPrefix(left, p) {
			return true
		}
	}
	return false
}

func codingAppendListItems(sentence, source string) string {
	items := codingNumberedListItems(source)
	var missing []string
	for _, item := range items {
		if sentence != "" && strings.Contains(sentence, item) {
			continue
		}
		missing = append(missing, item)
	}
	if len(missing) == 0 {
		return sentence
	}
	joined := strings.Join(missing, "、")
	if strings.TrimSpace(sentence) == "" {
		return "这几项：" + joined + "。"
	}
	if strings.Contains(sentence, "，而不是") {
		return strings.Replace(sentence, "，而不是", "，包括 "+joined+"，而不是", 1)
	}
	sentence = strings.TrimRight(sentence, "。")
	return sentence + "，包括" + joined + "。"
}

type codingListSpan struct {
	marker  int
	content int
}

func codingNumberedListItems(s string) []string {
	spans := codingNumberedMarkerSpans(s)
	var items []string
	for i, span := range spans {
		end := len(s)
		if i+1 < len(spans) {
			end = spans[i+1].marker
		}
		chunk := s[span.content:end]
		if j := strings.IndexAny(chunk, "、，,。；;\n"); j >= 0 {
			chunk = chunk[:j]
		}
		chunk = strings.Trim(chunk, " \t—-：:、，")
		n := utf8.RuneCountInString(chunk)
		if n < 2 || n > 16 || strings.ContainsAny(chunk, "（()）") {
			continue
		}
		items = append(items, chunk)
		if len(items) == 6 {
			break
		}
	}
	return items
}

func codingNumberedMarkerSpans(s string) []codingListSpan {
	var out []codingListSpan
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r != '（' && r != '(') || (i > 0 && !codingListMarkerBoundary(s, i)) {
			i += size
			continue
		}
		rest := s[i+size:]
		digits := 0
		for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
			digits++
		}
		if digits == 0 || digits > 2 {
			i += size
			continue
		}
		close, csize := utf8.DecodeRuneInString(rest[digits:])
		if (r == '（' && close != '）') || (r == '(' && close != ')') {
			i += size
			continue
		}
		content := i + size + digits + csize
		out = append(out, codingListSpan{marker: i, content: content})
		i = content
	}
	return out
}

func codingQuoteWorkName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "「") || strings.Contains(name, "」") {
		return name
	}
	return "「" + name + "」"
}

// codingDescribeQuestion says what the turn will deliver. Quoting the
// question alone ("这次<问句>，其它先不动") reads as an edit scope and, when
// the task title is that same sentence, repeats it twice.
func codingDescribeQuestion(prior, userText string, written bool) string {
	q := codingQuestionQuote(userText)
	line := "对照当前实现，回答「" + q + "」"
	if written {
		line = "对照现有实现，回答「" + q + "」"
	}
	if prior = strings.TrimSpace(prior); prior != "" {
		line = "在" + codingQuoteWorkName(prior) + "里，" + line
	}
	if written {
		return line + "。"
	}
	return line + "，这一步只读不改文件。"
}

// codingQuestionQuote keeps the question name short enough for the chat
// bubble. A pasted paragraph must not become the whole reply. Corner quotes
// are stripped so the wrapper stays one pair.
func codingQuestionQuote(userText string) string {
	q := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(userText), "？?。！! "))
	if q == "" {
		q = strings.TrimSpace(userText)
	}
	q = strings.NewReplacer("「", "", "」", "").Replace(q)
	q = strings.TrimSpace(q)
	if q == "" {
		q = strings.TrimSpace(userText)
	}
	const maxRunes = 80
	if utf8.RuneCountInString(q) <= maxRunes {
		return q
	}
	runes := []rune(q)
	window := trimUnbalancedCodingParens(trimCodingSnippetToBoundary(runes[:maxRunes]))
	out := strings.TrimRight(strings.TrimSpace(string(window)), " ，,、：:；;—")
	if utf8.RuneCountInString(out) < 8 {
		out = strings.TrimSpace(string(runes[:maxRunes]))
	}
	return out + "…"
}

func codingDescribeNewAsk(name, ask string) string {
	ask = strings.TrimSpace(ask)
	if ask == "" {
		return ""
	}
	if !strings.HasPrefix(ask, "这次") && !strings.HasPrefix(ask, "继续") && !strings.HasPrefix(ask, "请") && !strings.HasPrefix(ask, "先") && !strings.HasPrefix(ask, "把") {
		ask = "这次" + ask
	}
	tail := "，其它先不动。"
	if codingAskAlreadyCoversTheRest(ask) {
		tail = "。"
	}
	if name = strings.TrimSpace(name); name != "" {
		return "在" + codingQuoteWorkName(name) + "里，" + ask + tail
	}
	return ask + tail
}

func codingAskAlreadyCoversTheRest(ask string) bool {
	for _, marker := range []string{"全部", "所有", "都做", "都查", "都改", "剩下的都"} {
		if strings.Contains(ask, marker) {
			return true
		}
	}
	return false
}

func buildCodingUnderstandingUserPrompt(userText string, mem stickyCodingWorkbenchMemory) string {
	var b strings.Builder
	b.WriteString("User request:\n")
	b.WriteString(strings.TrimSpace(userText))
	if s := strings.TrimSpace(mem.SessionPlan); s != "" {
		b.WriteString("\n\nSession goal:\n")
		b.WriteString(truncateRunesV2(s, 400))
	}
	if s := strings.TrimSpace(mem.LastSummary); s != "" {
		b.WriteString("\n\nPrevious turn result:\n")
		b.WriteString(truncateRunesV2(s, 600))
	}
	if len(mem.FilesCreated) > 0 {
		b.WriteString("\n\nFiles created earlier: ")
		b.WriteString(strings.Join(mem.FilesCreated, ", "))
	}
	if len(mem.FilesModified) > 0 {
		files := uniqueSortedSubAgentStrings(mem.FilesModified)
		if len(files) > 12 {
			files = files[:12]
		}
		b.WriteString("\n\nFiles modified earlier: ")
		b.WriteString(strings.Join(files, ", "))
	}
	if codingSessionHasImplementationTrajectory(mem) {
		b.WriteString("\n\nSession record: project files were already written in this session. The workspace is not read-only. Restate only the current request.")
	}
	return b.String()
}

func codingRequestShouldRestateEarly(userText string) bool {
	return strings.TrimSpace(userText) != "" && !codingRequestLooksExplicitWorkspaceClear(userText)
}

// codingRequestUnderstandingKeepsHostFallback reports a host restatement that
// must not be replaced by the restatement model. Inquiry and a project launch
// already say what this turn will do. The sentence came from the request
// decision, and a second model pass would guess it again from the wording.
func codingRequestUnderstandingKeepsHostFallback(userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision) bool {
	if decision.Kind == codingRequestInquiry || (decision.Kind == codingRequestOperational && decision.Acceptance != codingOperationalAcceptanceCommand) {
		return true
	}
	// A short follow-up already has a host restatement based on the session
	// plan. The restatement model also sees the previous turn, which may
	// describe a rejected inquiry as a read-only environment, and would
	// replace that host text.
	text := strings.TrimSpace(userText)
	if utf8.RuneCountInString(text) > 48 {
		return false
	}
	prior := codingRequestWorkName(mem, text)
	if codingSessionHasImplementationTrajectory(mem) && prior != "" {
		return true
	}
	// A bare continue may still be refined when the only context is a title.
	// A new short ask is already quoted; replacing it drops that sentence.
	if codingWorkbenchContinueCue(text) {
		return codingClosedRequirementSnippet(mem.SessionPlan) != ""
	}
	return codingClosedRequirementSnippet(mem.SessionPlan) != "" || prior != ""
}

func (h *IMMessageHandler) llmCodingRequestRestatement(userText string, mem stickyCodingWorkbenchMemory) string {
	if h == nil || h.client == nil {
		return ""
	}
	cfg := h.getCodingLightweightLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		cfg = h.getCodingLLMConfig()
	}
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	got := parseCodingRequestRestatement(h.callLightweightLLM(cfg, codingRequestUnderstandingSystemPrompt, buildCodingUnderstandingUserPrompt(userText, mem), 20))
	if got == "" || codingRestatementCopiesUser(got, userText) || utf8.RuneCountInString(got) < 8 {
		return ""
	}
	return truncateRunesV2(got, codingRequestUnderstandingMaxRunes)
}

func (h *IMMessageHandler) resolveCodingRequestUnderstanding(userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision) string {
	if !codingRequestShouldRestate(decision, userText) {
		return ""
	}
	fallback := fallbackCodingRequestRestatementFor(userText, mem, decision)
	if codingRequestUnderstandingKeepsHostFallback(userText, mem, decision) {
		return fallback
	}
	if got := h.llmCodingRequestRestatement(userText, mem); got != "" {
		return got
	}
	return fallback
}

func (h *IMMessageHandler) publishCodingRequestUnderstanding(userID, userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision, onToken func(string)) string {
	if isGoalSystemContinuation(userText) {
		return ""
	}
	if !codingRequestShouldRestateEarly(userText) {
		return ""
	}
	got := strings.TrimSpace(fallbackCodingRequestRestatementFor(userText, mem, decision))
	if got == "" {
		return ""
	}
	if strings.TrimSpace(userID) != "" {
		h.setStickyCodingRequirementRestatement(userID, got)
		h.emitCodingWorkbenchStepsUpdate(userID)
	}
	emitCodingRequestUnderstanding(onToken, got)
	return got
}

func (h *IMMessageHandler) refineCodingRequestUnderstanding(userID, userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision) string {
	if isGoalSystemContinuation(userText) {
		return ""
	}
	if !codingRequestShouldRestate(decision, userText) || codingRequestUnderstandingKeepsHostFallback(userText, mem, decision) {
		return strings.TrimSpace(mem.RequirementRestatement)
	}
	got := h.llmCodingRequestRestatement(userText, mem)
	if got == "" {
		return strings.TrimSpace(mem.RequirementRestatement)
	}
	if strings.TrimSpace(userID) != "" {
		h.setStickyCodingRequirementRestatement(userID, got)
		h.emitCodingWorkbenchStepsUpdate(userID)
	}
	return got
}

func (h *IMMessageHandler) resolveAndStoreCodingRequestUnderstanding(userID, userText string, mem stickyCodingWorkbenchMemory, decision codingRequestDecision, onToken func(string)) string {
	shown := h.publishCodingRequestUnderstanding(userID, userText, mem, decision, onToken)
	if shown != "" {
		if strings.TrimSpace(userID) != "" {
			mem = h.getStickyCodingWorkbenchMemory(userID)
		}
		if strings.TrimSpace(mem.RequirementRestatement) == "" {
			mem.RequirementRestatement = shown
		}
	}
	if refined := h.refineCodingRequestUnderstanding(userID, userText, mem, decision); refined != "" {
		return refined
	}
	return shown
}

func formatCodingRequestUnderstandingText(restatement string) string {
	// Codex-style: the first assistant prose is the paraphrase itself.
	// Do not wrap it in an audit heading — that belongs on the checklist only.
	return strings.TrimSpace(restatement)
}

func emitCodingRequestUnderstanding(onToken func(string), restatement string) {
	text := formatCodingRequestUnderstandingText(restatement)
	if onToken == nil || text == "" {
		return
	}
	onToken(text)
}

func joinCodingUnderstandingAndBody(understanding, body string) string {
	understanding = strings.TrimSpace(understanding)
	body = strings.TrimSpace(body)
	if understanding == "" {
		return body
	}
	if body == "" {
		return understanding
	}
	if strings.HasPrefix(body, understanding) {
		return body
	}
	return understanding + "\n\n" + body
}

func defaultModerateCodingPlan(userText, restatement string) []*v2.TaskItem {
	if strings.TrimSpace(restatement) == "" {
		restatement = strings.TrimSpace(userText)
	}
	return []*v2.TaskItem{
		{
			Index:       1,
			Title:       "阅读现有实现并确认改动范围",
			Description: "先读当前相关代码和构建方式，确认要改的形态和必须保留的行为。",
		},
		{
			Index:       2,
			Title:       "按已理解需求实现并验证",
			Description: "按已向用户复述的需求改写：" + truncateRunesV2(restatement, 160) + "。构建并确认可运行，不扩大范围。",
			DependsOn:   []int{1},
		},
	}
}

func extractCodingPlanGoal(markdown string) string {
	for _, line := range strings.Split(markdown, "\n") {
		trim := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trim, "**目标**:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func codingPlanGoalText(userText, restatement string) string {
	if s := strings.TrimSpace(restatement); s != "" {
		return s
	}
	return strings.TrimSpace(userText)
}
