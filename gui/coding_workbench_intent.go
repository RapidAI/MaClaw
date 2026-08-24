package main

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

func usableCodingSessionContext(s string) string {
	if codingSessionContextLooksGeneric(s) {
		return ""
	}
	return firstCodingContextSnippet(s, 72)
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
	prior := usableCodingSessionContext(mem.SessionPlan)
	if prior == "" {
		prior = usableCodingSessionContext(mem.LastSummary)
	}
	if prior == "" {
		prior = usableCodingSessionContext(codingWorkspaceIdentityHint(mem.ProjectPath))
	}
	return prior
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
	if codingRequestLooksLikeQuestion(userText) {
		return "先只读查看相关代码，回答你这次想弄清的问题，不改文件。"
	}
	if prior != "" && utf8.RuneCountInString(userText) <= 48 {
		return "在已有工作（" + prior + "）上继续你这次提出的改动，先对齐现有实现再动手，不扩大到未提到的功能。"
	}
	return "按你的这次要求改当前项目：先看清现有实现和约束，再做对应修改并验证结果，不把范围扩到没提到的功能。"
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
	got := parseCodingRequestRestatement(h.callLightweightLLMOnce(cfg, codingRequestUnderstandingSystemPrompt, buildCodingUnderstandingUserPrompt(userText, mem), 8))
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
	if !codingRequestShouldRestate(decision, userText) || codingRestatementFallbackIsSpecific(userText, mem) {
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
