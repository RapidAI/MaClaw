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

func firstCodingContextSnippet(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\n。！？"); i > 8 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(truncateRunesV2(s, maxRunes))
}

func extractCodingRewriteTarget(userText string) string {
	userText = strings.TrimSpace(userText)
	lower := strings.ToLower(userText)
	for _, prefix := range []string{"改为", "改成", "换成", "change to", "port to"} {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		// Map lower index back onto the original string by rune-safe offset on the
		// ASCII/same-width prefixes we use here.
		origIdx := strings.Index(userText, prefix)
		if origIdx < 0 {
			origIdx = idx
		}
		rest := strings.TrimSpace(userText[origIdx+len(prefix):])
		rest = strings.Trim(rest, " \t。.!！?？")
		if rest == "" {
			continue
		}
		if utf8.RuneCountInString(rest) > 24 {
			rest = truncateRunesV2(rest, 24)
		}
		return rest
	}
	return ""
}

func fallbackRewriteRestatement(userText, prior string) (string, bool) {
	text := strings.TrimSpace(userText)
	lower := strings.ToLower(text)
	target := ""
	switch {
	case codingTextMentionsGUI(text):
		target = "图形界面版本"
	case strings.Contains(text, "重写") || strings.Contains(text, "重寫") || codingHasASCIIWord(lower, "rewrite"):
		target = "重写后的实现"
	case strings.Contains(text, "移植") || strings.Contains(lower, "port to"):
		target = "移植后的实现"
	default:
		target = extractCodingRewriteTarget(text)
	}
	if target == "" {
		return "", false
	}
	if prior != "" {
		return "把现有的「" + prior + "」改成" + target + "，保留原有玩法或核心行为，改完后重新构建并确认能运行。", true
	}
	return "把当前项目改成" + target + "，保留原有核心行为，改完后重新构建并确认能运行。", true
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

func codingHasASCIIWord(lower, word string) bool {
	if lower == "" || word == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(lower[start:], word)
		if idx < 0 {
			return false
		}
		idx += start
		leftOK := idx == 0 || !isASCIIAlphaNumByte(lower[idx-1])
		right := idx + len(word)
		rightOK := right == len(lower) || !isASCIIAlphaNumByte(lower[right])
		if leftOK && rightOK {
			return true
		}
		start = idx + 1
	}
}

func isASCIIAlphaNumByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z'
}

func codingTextMentionsGUI(text string) bool {
	if strings.Contains(text, "图形界面") || strings.Contains(text, "图形版") || strings.Contains(text, "圖形界面") || strings.Contains(text, "圖形版") {
		return true
	}
	return codingHasASCIIWord(strings.ToLower(text), "gui")
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
			if line == "" || codingReadmeLineLooksGeneric(line) {
				continue
			}
			return firstCodingContextSnippet(line, 40)
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
	raw := string(data)
	lower := strings.ToLower(raw)
	idx := strings.Index(lower, "project(")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(raw[idx+len("project("):])
	rest = strings.TrimLeft(rest, " \t\"'")
	end := len(rest)
	for i, r := range rest {
		if r == ')' || r == ' ' || r == '\t' || r == '\n' || r == ',' {
			end = i
			break
		}
	}
	name := strings.TrimSpace(rest[:end])
	name = strings.Trim(name, "\"'")
	if codingSourceStemLooksGeneric(name) || codingSessionContextLooksGeneric(name) {
		return ""
	}
	return firstCodingContextSnippet(name, 40)
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

func codingWorkspaceIdentityHint(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return ""
	}
	if got := codingReadmeIdentityHint(projectPath); got != "" {
		return got
	}
	if got := codingCMakeProjectIdentityHint(projectPath); got != "" {
		return got
	}
	if got := codingTopLevelSourceIdentityHint(projectPath); got != "" {
		return got
	}
	return codingBinaryIdentityHint(projectPath)
}

func codingRequestPriorContext(mem stickyCodingWorkbenchMemory) string {
	// A session plan that is a requirement paragraph is not a work name.
	// Stuffing a truncated copy into 「…」 or （…） splits lists and parentheses.
	if name := codingShortWorkName(mem.SessionPlan); name != "" {
		return name
	}
	if name := codingShortWorkName(mem.LastSummary); name != "" {
		return name
	}
	if name := codingShortWorkName(codingWorkspaceIdentityHint(mem.ProjectPath)); name != "" {
		return name
	}
	return ""
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

func codingRequestHasHowOrWhyMarker(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"怎么", "如何", "为什么", "为何", "為何", "how to", "how do", "how does", "what is", "why "} {
		if strings.Contains(text, marker) || strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func codingRequestLooksLikeQuestion(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	if codingRequestHasHowOrWhyMarker(text) {
		return true
	}
	return strings.HasSuffix(text, "?") || strings.HasSuffix(text, "？")
}

func codingRequestLooksLikeFailure(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{"失败", "失敗", "报错", "報錯", "崩溃", "崩潰"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return codingHasASCIIWord(lower, "error") || codingHasASCIIWord(lower, "crash") || codingHasASCIIWord(lower, "bug")
}

func codingRequestLooksRunOnly(userText string) bool {
	text := strings.TrimSpace(userText)
	if text == "" || codingTextMentionsGUI(text) || codingRequestLooksLikeQuestion(text) || codingRequestLooksLikeFailure(text) {
		return false
	}
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"改", "加", "写", "寫", "实现", "實現", "修", "移植", "重写", "重寫",
		"add ", "fix ", "implement", "rewrite", "port to",
	} {
		if strings.Contains(text, marker) || strings.Contains(lower, marker) {
			return false
		}
	}
	if strings.Contains(text, "运行一下") || strings.Contains(text, "启动一下") || strings.Contains(text, "跑一下") ||
		strings.Contains(lower, "run the app") || strings.Contains(lower, "run it") || strings.Contains(lower, "run the program") {
		return true
	}
	if utf8.RuneCountInString(text) <= 16 && (strings.Contains(text, "运行") || strings.Contains(text, "启动") || strings.HasPrefix(lower, "run ")) {
		return true
	}
	return false
}

func fallbackCodingRequestRestatement(userText string, mem stickyCodingWorkbenchMemory) string {
	userText = strings.TrimSpace(userText)
	prior := codingRequestPriorContext(mem)
	if rewrite, ok := fallbackRewriteRestatement(userText, prior); ok && !codingRequestHasHowOrWhyMarker(userText) {
		return rewrite
	}
	if codingRequestLooksRunOnly(userText) {
		return "运行当前已有程序并确认它能正常工作，这一步不改源代码。"
	}
	// A session that already wrote project files keeps a writable contract.
	// Do not announce a read-only turn from the wording of this message.
	if codingRequestLooksLikeQuestion(userText) && !codingSessionHasImplementationTrajectory(mem) {
		return "先只读相关代码，回答你这次的问题，不改文件。"
	}
	if codingRequestLooksLikeQuestion(userText) {
		// This session already wrote files, so the answer must not claim the
		// tree is read-only. State the question, then the limit on edits.
		q := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(userText), "？?"))
		if q == "" {
			q = strings.TrimSpace(userText)
		}
		return "对照现有实现，回答「" + q + "」。"
	}
	if utf8.RuneCountInString(userText) <= 48 {
		// A title stays a title. A stored requirement is restated as the
		// work itself. A new short ask is that ask, not a continue template.
		ask := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(userText), "。！？!?"))
		if codingWorkbenchContinueCue(userText) {
			echo := codingClosedRequirementSnippet(mem.SessionPlan)
			if echo != "" || codingRequirementGoalCut(mem.SessionPlan) >= 0 {
				if sentence := codingDescribeStoredGoal(echo, mem.SessionPlan); sentence != "" {
					return sentence
				}
			}
			if prior != "" {
				return "继续做" + codingQuoteWorkName(prior) + "，从上次停下的地方接着做。"
			}
		} else if ask != "" && codingClosedRequirementSnippet(mem.SessionPlan) != "" {
			return codingDescribeNewAsk("", ask)
		} else if prior != "" && ask != "" {
			return codingDescribeNewAsk(prior, ask)
		}
	}
	return "先看清当前项目里相关的实现和约束，再按你这次的要求修改并验证。"
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

func codingRestatementFallbackIsSpecific(userText string, mem stickyCodingWorkbenchMemory) bool {
	if codingRequestHasHowOrWhyMarker(userText) {
		return false
	}
	_, ok := fallbackRewriteRestatement(userText, codingRequestPriorContext(mem))
	return ok
}

// codingRequestUnderstandingKeepsHostFallback reports a host restatement that
// must not be replaced by the restatement model. A run/build follow-up already
// says the existing program will be launched without source edits. The session
// record tells that model the workspace is not read-only, and it would
// otherwise overwrite the launch constraint.
func codingRequestUnderstandingKeepsHostFallback(userText string, mem stickyCodingWorkbenchMemory) bool {
	if codingRestatementFallbackIsSpecific(userText, mem) || codingRequestLooksRunOnly(userText) {
		return true
	}
	// A short follow-up already has a host restatement based on the session
	// plan. The restatement model also sees the previous turn, which may
	// describe a rejected inquiry as a read-only environment, and would
	// replace that host text.
	text := strings.TrimSpace(userText)
	if utf8.RuneCountInString(text) > 48 || codingRequestLooksLikeQuestion(text) {
		return false
	}
	if codingSessionHasImplementationTrajectory(mem) && codingRequestPriorContext(mem) != "" {
		return true
	}
	// A bare continue may still be refined when the only context is a title.
	// A new short ask is already quoted; replacing it drops that sentence.
	if codingWorkbenchContinueCue(text) {
		return codingClosedRequirementSnippet(mem.SessionPlan) != ""
	}
	return codingClosedRequirementSnippet(mem.SessionPlan) != "" || codingRequestPriorContext(mem) != ""
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
	fallback := fallbackCodingRequestRestatement(userText, mem)
	if codingRestatementFallbackIsSpecific(userText, mem) {
		return fallback
	}
	if got := h.llmCodingRequestRestatement(userText, mem); got != "" {
		return got
	}
	return fallback
}

func (h *IMMessageHandler) publishCodingRequestUnderstanding(userID, userText string, mem stickyCodingWorkbenchMemory, onToken func(string)) string {
	if isGoalSystemContinuation(userText) {
		return ""
	}
	if !codingRequestShouldRestateEarly(userText) {
		return ""
	}
	got := strings.TrimSpace(fallbackCodingRequestRestatement(userText, mem))
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
	if !codingRequestShouldRestate(decision, userText) || codingRequestUnderstandingKeepsHostFallback(userText, mem) {
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
	shown := h.publishCodingRequestUnderstanding(userID, userText, mem, onToken)
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
