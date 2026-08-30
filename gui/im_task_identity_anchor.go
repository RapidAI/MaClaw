package main

// Task identity anchors are a host-owned charter for the current tab/session.
// They protect multi-turn work from semantic drift: compaction, cancelled
// turns, leftover workspace files, and vague follow-ups ("继续改进 ppt") must
// not silently switch the task to another topic that happens to share the
// working directory. Person/document binding is one specialization; original
// request + primary deliverable is the general case. This is an
// execution-context invariant, not an LLM instruction that can be lost during
// compaction.

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type taskIdentityAnchor struct {
	Subject         string
	SourcePaths     []string
	OriginalRequest string
	WorkKind        string
	PrimaryFiles    []string
	UpdatedAt       time.Time
}

var taskAnchorSubjectPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?:撰写|编写|整理|生成|改写|浓缩|概括|介绍|总结|写)\s*(?:一份|一篇|个)?\s*([\p{Han}]{2,4}?)(?:的)?(?:个人|学术|科研|研究|简历|简介|资料)`),
	regexp.MustCompile(`(?:关于|针对|为)\s*([\p{Han}]{2,4})\s*(?:撰写|编写|整理|生成|改写|浓缩|概括|介绍|总结|的)`),
}

var taskAnchorDocumentLeadingNamePattern = regexp.MustCompile(`^\s*([\p{Han}]{2,4})(?:[·・]|\s|软件|人工智能|教授|博士|研究员|简历)`)

var taskAnchorSourcePathPattern = regexp.MustCompile(`(?m)^\s*([A-Za-z]:\\[^\r\n]+|/[^\r\n]+)\s*$`)
var taskAnchorAutoExtractPathPattern = regexp.MustCompile(`--- auto_extract: begin\s+path="([^"]+)"`)

// updateTaskIdentityAnchorFromUserText creates or refreshes an anchor only
// when a turn contains durable evidence: an explicit local source or subject.
// Generic continuation turns must retain the prior anchor unchanged.
func (h *IMMessageHandler) updateTaskIdentityAnchorFromUserText(userID, userText string) {
	if h == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(userText) == "" {
		return
	}
	subject := extractTaskAnchorSubject(userText)
	paths := extractTaskAnchorSourcePaths(userText)
	prior, _ := h.taskAnchors.Load(userID)
	anchor, _ := prior.(taskIdentityAnchor)
	subjectChanged := subject != "" && anchor.Subject != "" && subject != anchor.Subject
	// A new explicit person/source pair starts a new task. Do not carry the
	// prior source list into it: that would turn a useful anchor into a blended
	// dossier and merely invert the identity-mixing bug.
	if subjectChanged {
		anchor = taskIdentityAnchor{}
	}
	if subject != "" {
		anchor.Subject = subject
	}
	if len(paths) > 0 {
		if subjectChanged {
			// An explicit new subject changes the dossier even when the text
			// mentions the same pathname as an earlier turn.
			anchor.SourcePaths = mergeTaskAnchorPaths(nil, paths)
		} else if len(anchor.SourcePaths) == 0 {
			anchor.SourcePaths = mergeTaskAnchorPaths(nil, paths)
		} else {
			anchor.SourcePaths = mergeTaskAnchorPaths(anchor.SourcePaths, paths)
		}
	}
	// OriginalRequest is write-once for the session. Generic follow-ups
	// ("继续改进ppt") and cancelled-turn residue must not replace the tab's
	// first substantive request, or a later math-style instruction can hijack
	// a PPT tab.
	if strings.TrimSpace(anchor.OriginalRequest) == "" {
		if recovered := firstSubstantiveTaskRequest(h.taskAnchorHistory(userID)); recovered != "" {
			anchor.OriginalRequest = recovered
		} else if request := firstSubstantiveTaskRequest([]agent.ConversationEntry{{Role: "user", Content: userText}}); request != "" {
			anchor.OriginalRequest = request
		}
	}
	if strings.TrimSpace(anchor.WorkKind) == "" {
		anchor.WorkKind = extractTaskWorkKind(anchor.OriginalRequest)
	}
	if !taskIdentityAnchorActive(anchor) {
		return
	}
	anchor.UpdatedAt = time.Now()
	h.taskAnchors.Store(userID, anchor)
}

func (h *IMMessageHandler) taskIdentityAnchorForUser(userID string) (taskIdentityAnchor, bool) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return taskIdentityAnchor{}, false
	}
	value, ok := h.taskAnchors.Load(userID)
	if !ok {
		return taskIdentityAnchor{}, false
	}
	anchor, ok := value.(taskIdentityAnchor)
	if !ok || !taskIdentityAnchorActive(anchor) {
		return taskIdentityAnchor{}, false
	}
	return anchor, true
}

func taskIdentityAnchorActive(anchor taskIdentityAnchor) bool {
	return strings.TrimSpace(anchor.Subject) != "" ||
		len(anchor.SourcePaths) > 0 ||
		strings.TrimSpace(anchor.OriginalRequest) != ""
}

func (h *IMMessageHandler) clearTaskIdentityAnchor(userID string) {
	if h != nil {
		h.taskAnchors.Delete(userID)
	}
}

func taskIdentityAnchorPrompt(anchor taskIdentityAnchor) string {
	if !taskIdentityAnchorActive(anchor) {
		return ""
	}
	var b strings.Builder
	b.WriteString("[任务身份与来源锚点 — 强制约束]\n")
	if req := strings.TrimSpace(anchor.OriginalRequest); req != "" {
		fmt.Fprintf(&b, "- 当前任务目标：%s\n", req)
	}
	if kind := strings.TrimSpace(anchor.WorkKind); kind != "" {
		fmt.Fprintf(&b, "- 任务类型：%s。含糊的续写（例如「继续改进 ppt」「需要专业风格」）一律指该任务，而不是工作区里其他主题的文件。\n", kind)
	}
	if anchor.Subject != "" {
		fmt.Fprintf(&b, "- 当前任务对象：%s。续写、浓缩、改写、翻译或生成资料时，必须保持该对象；不得替换为其他人。\n", anchor.Subject)
	}
	if len(anchor.SourcePaths) > 0 {
		b.WriteString("- 当前任务的优先事实来源：")
		b.WriteString(strings.Join(anchor.SourcePaths, "；"))
		b.WriteString("。\n")
		b.WriteString("- 若历史正文已被压缩，先重读上述来源；不得用长期记忆、历史会话、工作区中另一任务的材料补替。来源无法读取时，应说明缺少证据。\n")
	}
	if len(anchor.PrimaryFiles) > 0 {
		b.WriteString("- 当前任务主产物：")
		b.WriteString(strings.Join(anchor.PrimaryFiles, "；"))
		b.WriteString("。覆盖、美化、加页或加图表时请改这些文件（或其文件名前缀变体），不要新建或改写其他主题的讲义/PPT。\n")
	} else {
		b.WriteString("- 工作区可能含有其他任务留下的文件。不要把那些文件当成当前任务产物。\n")
	}
	b.WriteString("- 只有用户明确要求检索历史会话或切换对象/任务时，才可跨任务检索或更换该锚点。\n")
	return strings.TrimSpace(b.String())
}

// appendTaskIdentityAnchorPrompt keeps the same immutable turn snapshot on
// every system-prompt rebuild. Tool-loop route escalation and consumed-grant
// refreshes replace the prompt wholesale; without this helper those rebuilds
// silently dropped the anchor that was present on the first model request.
func appendTaskIdentityAnchorPrompt(systemPrompt string, anchor *taskIdentityAnchor) string {
	if anchor == nil {
		return systemPrompt
	}
	anchorPrompt := taskIdentityAnchorPrompt(*anchor)
	if anchorPrompt == "" || strings.Contains(systemPrompt, "[任务身份与来源锚点 — 强制约束]") {
		return systemPrompt
	}
	return strings.TrimSpace(systemPrompt) + "\n\n" + anchorPrompt
}

func extractTaskAnchorSubject(text string) string {
	for _, pattern := range taskAnchorSubjectPatterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) > 1 {
			if subject := normalizeTaskAnchorSubject(matches[1]); subject != "" {
				return subject
			}
		}
	}
	// Resume/CV extracts commonly begin with a spaced Chinese name. This is a
	// fallback only when an attached document also establishes a source path.
	if len(extractTaskAnchorSourcePaths(text)) > 0 {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if len([]rune(line)) > 36 {
				break
			}
			candidate := strings.ReplaceAll(line, " ", "")
			if matched := taskAnchorDocumentLeadingNamePattern.FindStringSubmatch(candidate); len(matched) > 1 {
				return normalizeTaskAnchorSubject(matched[1])
			}
		}
	}
	return ""
}

// taskIdentityAnchorForTurn takes a stable snapshot before a loop begins.
// That prevents a concurrently-started later user turn from changing the
// subject/source constraints of the earlier loop mid-execution.
func (h *IMMessageHandler) taskIdentityAnchorForTurn(userID, userText string) (taskIdentityAnchor, bool) {
	if h == nil || strings.TrimSpace(userID) == "" {
		return taskIdentityAnchor{}, false
	}
	// update is intentionally co-located with the snapshot: callers must not
	// load an anchor, yield, and then decide which source applies.
	h.updateTaskIdentityAnchorFromUserText(userID, userText)
	return h.taskIdentityAnchorForUser(userID)
}

func normalizeTaskAnchorSubject(subject string) string {
	subject = strings.ReplaceAll(strings.TrimSpace(subject), " ", "")
	runes := []rune(subject)
	if len(runes) < 2 || len(runes) > 4 {
		return ""
	}
	return subject
}

func extractTaskAnchorSourcePaths(text string) []string {
	var paths []string
	for _, match := range taskAnchorSourcePathPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			paths = append(paths, strings.TrimSpace(match[1]))
		}
	}
	for _, match := range taskAnchorAutoExtractPathPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			paths = append(paths, strings.TrimSpace(match[1]))
		}
	}
	return mergeTaskAnchorPaths(nil, paths)
}

func mergeTaskAnchorPaths(existing, incoming []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	paths := make([]string, 0, len(existing)+len(incoming))
	for _, path := range append(append([]string(nil), existing...), incoming...) {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, clean)
	}
	return paths
}

func explicitCrossTaskRecallRequested(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "历史会话") || strings.Contains(text, "历史对话") || strings.Contains(text, "以前的聊天") || strings.Contains(text, "旧对话") || strings.Contains(text, "此前版本") || strings.Contains(text, "之前的对话") || strings.Contains(text, "session history")
}

// taskAnchorAllowsCrossTaskRecall must inspect the current user turn, not a
// mutable session flag. Tool calls can occur after a later turn has started;
// keeping consent in the anchor would then let a stale tool call borrow the
// next turn's permission (or vice versa).
func taskAnchorAllowsCrossTaskRecall(userText string) bool {
	return explicitCrossTaskRecallRequested(userText)
}

// taskAnchorAllowsLongTermMemoryRecall is intentionally narrower than the
// history-search consent above. A request to inspect old chats does not also
// authorize injecting durable memory into a source-bound writing task. The
// permission must be explicit in the current user turn because a later turn
// may start while an earlier loop is still executing.
func taskAnchorAllowsLongTermMemoryRecall(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if explicitCrossTaskRecallRequested(text) && !strings.Contains(text, "长期记忆") && !strings.Contains(text, "long-term memory") {
		return false
	}
	return strings.Contains(text, "长期记忆") ||
		strings.Contains(text, "从记忆") ||
		strings.Contains(text, "使用记忆") ||
		strings.Contains(text, "调用记忆") ||
		strings.Contains(text, "之前记住") ||
		strings.Contains(text, "保存的记忆") ||
		strings.Contains(text, "long-term memory") ||
		strings.Contains(text, "use memory") ||
		strings.Contains(text, "recall memory")
}

// taskAnchorBlocksMemoryRead prevents recalled material from another task
// becoming an unlabelled substitute for an attachment whose body was compacted
// out of history. Writes are handled independently, so ordinary task notes can
// still be saved under the identity-aware write gate.
func taskAnchorBlocksMemoryRead(anchor taskIdentityAnchor, action memoryToolAction, userText string) bool {
	if taskAnchorAllowsLongTermMemoryRecall(userText) {
		return false
	}
	if len(anchor.SourcePaths) == 0 && strings.TrimSpace(anchor.OriginalRequest) == "" {
		return false
	}
	switch action {
	case memoryToolActionRecall,
		memoryToolActionThemes,
		memoryToolActionScenes,
		memoryToolActionTrace,
		memoryToolActionCandidates,
		memoryToolActionDerived,
		memoryToolActionSummary,
		memoryToolActionList:
		return true
	default:
		return false
	}
}

func stripTaskAnchorDocumentBodies(text string) string {
	return agent.StripAutoExtractBodies(text)
}

// anchoredMemorySaveRejected prevents a hallucinated identity substitution
// from being promoted into durable memory. It deliberately targets only
// person-profile-like writes so ordinary operational notes remain permitted.
func anchoredMemorySaveRejected(anchor taskIdentityAnchor, content string) bool {
	if anchor.Subject == "" {
		return false
	}
	if strings.Contains(content, anchor.Subject) {
		// A profile may mention the anchored person while still being headed by a
		// different person's dossier. The leading identity is the strongest
		// low-cost signal available at this write boundary.
		return anchoredMemoryProfileHeadingMismatch(anchor, content)
	}
	return containsTaskAnchorProfileTerm(content) && containsLikelyPersonName(content)
}

var taskAnchorProfileHeadingPattern = regexp.MustCompile(`^\s*[“\"']?([\p{Han}]{2,4})\s*[，,。；;：:]`)
var taskAnchorLikelyPersonNamePattern = regexp.MustCompile(`(?:^|[\s；;。])([\p{Han}]{2,4})\s*[，,。；;：:]`)

func anchoredMemoryProfileHeadingMismatch(anchor taskIdentityAnchor, content string) bool {
	if !containsTaskAnchorProfileTerm(content) {
		return false
	}
	match := taskAnchorProfileHeadingPattern.FindStringSubmatch(content)
	return len(match) > 1 && normalizeTaskAnchorSubject(match[1]) != anchor.Subject
}

func containsTaskAnchorProfileTerm(content string) bool {
	for _, term := range []string{"学术", "科研", "简历", "个人简介", "个人介绍", "博士", "研究方向", "申请人"} {
		if strings.Contains(content, term) {
			return true
		}
	}
	return false
}

// A bare profile keyword (for example “科研计划已更新”) is not identity data
// and should remain a valid operational memory. Reject only when the write
// also carries a plausible person's name while omitting the anchored subject.
func containsLikelyPersonName(content string) bool {
	return taskAnchorLikelyPersonNamePattern.MatchString(content)
}

func (h *IMMessageHandler) taskAnchorHistory(userID string) []agent.ConversationEntry {
	if h == nil || h.memory == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	return h.memory.LoadAll(userID)
}

func firstSubstantiveTaskRequest(entries []agent.ConversationEntry) string {
	var best string
	var bestTS int64
	found := false
	for _, entry := range entries {
		if entry.Role != "user" {
			continue
		}
		text, _ := entry.Content.(string)
		text = strings.TrimSpace(stripTaskAnchorDocumentBodies(text))
		if !isSubstantiveTaskRequest(text) {
			continue
		}
		ts := entry.Timestamp
		if !found || (ts > 0 && (bestTS == 0 || ts < bestTS)) {
			best = truncateRunes(text, 500)
			bestTS = ts
			found = true
		}
	}
	return best
}

func isSubstantiveTaskRequest(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "[系统]") {
		return false
	}
	if utf8RuneCount(text) < 8 {
		return false
	}
	return !isTaskAnchorContinuationText(text)
}

func isTaskAnchorContinuationText(text string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	if compact == "" {
		return true
	}
	cues := []string{
		"继续改进", "继续改", "继续做", "忘掉前面", "不用管前面", "忽略前面",
		"太朴素", "更新云端", "需要专业风格", "ppt需要",
	}
	for _, cue := range cues {
		if strings.Contains(compact, strings.ReplaceAll(cue, " ", "")) && utf8RuneCount(text) <= 80 {
			return true
		}
	}
	switch compact {
	case "继续", "continue", "goon":
		return true
	}
	return false
}

func extractTaskWorkKind(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "ppt") || strings.Contains(text, "幻灯") || strings.Contains(text, "演讲") || strings.Contains(text, "演示文稿"):
		return "ppt"
	case strings.Contains(text, "讲义") || strings.Contains(text, "书籍") || strings.Contains(lower, "handbook") || strings.Contains(lower, "book-pdf") || strings.Contains(lower, "book_pdf"):
		return "document"
	default:
		return ""
	}
}

func taskAnchorDeliverableWriteBlockReason(anchor *taskIdentityAnchor, toolName string, args map[string]interface{}) string {
	if anchor == nil || !taskIdentityAnchorActive(*anchor) {
		return ""
	}
	path := taskAnchorDeliverablePath(toolName, args)
	if path == "" || !taskAnchorShouldGateDeliverable(*anchor, path) {
		return ""
	}
	if !taskAnchorDeliverableContradicts(*anchor, path) {
		return ""
	}
	var b strings.Builder
	b.WriteString("当前任务")
	if req := strings.TrimSpace(anchor.OriginalRequest); req != "" {
		fmt.Fprintf(&b, "是「%s」", truncateRunes(req, 80))
	}
	if len(anchor.PrimaryFiles) > 0 {
		fmt.Fprintf(&b, "（主产物 %s）", strings.Join(anchor.PrimaryFiles, "；"))
	}
	fmt.Fprintf(&b, "。不要改写工作区里其他主题的文件（%s）。请覆盖或修订当前任务主产物。", filepath.Base(path))
	return b.String()
}

func taskAnchorDeliverablePath(toolName string, args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	switch strings.TrimSpace(toolName) {
	case "office":
		action := strings.ToLower(strings.TrimSpace(stringVal(args, "action")))
		switch action {
		case "write_pptx", "generate_pptx", "generate_pdf":
		default:
			return ""
		}
	case "write_file":
	default:
		return ""
	}
	for _, key := range []string{"file_path", "path"} {
		if p := strings.TrimSpace(stringVal(args, key)); p != "" {
			return p
		}
	}
	return ""
}

func taskAnchorShouldGateDeliverable(anchor taskIdentityAnchor, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".pptx", ".ppt":
		return true
	case ".pdf":
		return anchor.WorkKind == "document"
	default:
		return false
	}
}

func taskAnchorDeliverableContradicts(anchor taskIdentityAnchor, path string) bool {
	base := filepath.Base(path)
	if len(anchor.PrimaryFiles) > 0 {
		return !taskAnchorPrimaryVariant(base, anchor.PrimaryFiles) && !taskAnchorSharesDistinctiveTokens(base, taskAnchorCharterText(anchor))
	}
	charter := taskAnchorCharterText(anchor)
	charterASCII := taskAnchorDistinctiveASCIITokens(charter)
	fileASCII := taskAnchorDistinctiveASCIITokens(base)
	if len(charterASCII) == 0 || len(fileASCII) == 0 {
		return false
	}
	return !tokenSetsIntersect(charterASCII, fileASCII)
}

func taskAnchorCharterText(anchor taskIdentityAnchor) string {
	return strings.TrimSpace(strings.Join([]string{anchor.OriginalRequest, anchor.Subject, strings.Join(anchor.SourcePaths, " ")}, " "))
}

func taskAnchorPrimaryVariant(filename string, primary []string) bool {
	stem := taskAnchorFileStem(filename)
	if stem == "" {
		return false
	}
	lower := strings.ToLower(stem)
	for _, item := range primary {
		ps := strings.ToLower(taskAnchorFileStem(filepath.Base(item)))
		if ps == "" {
			continue
		}
		if lower == ps || strings.HasPrefix(lower, ps+"-") || strings.HasPrefix(lower, ps+"_") {
			return true
		}
		if taskAnchorSharesDistinctiveTokens(stem, ps) {
			return true
		}
	}
	return false
}

func taskAnchorFileStem(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

func taskAnchorSharesDistinctiveTokens(filename, charter string) bool {
	return tokenSetsIntersect(taskAnchorDistinctiveASCIITokens(filename), taskAnchorDistinctiveASCIITokens(charter))
}

var taskAnchorASCIIStopwords = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "with": {}, "from": {}, "this": {}, "that": {},
	"ppt": {}, "pptx": {}, "pdf": {}, "doc": {}, "docx": {}, "md": {}, "txt": {},
	"com": {}, "www": {}, "org": {}, "http": {}, "https": {}, "github": {},
	"user": {}, "users": {}, "file": {}, "files": {}, "path": {}, "data": {},
	"title": {}, "slides": {}, "slide": {}, "page": {}, "pages": {},
	"ai": {}, "llm": {}, "app": {}, "new": {}, "old": {},
	"into": {}, "over": {}, "about": {}, "after": {}, "before": {}, "please": {},
}

var taskAnchorTokenRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]{2,}`)

func taskAnchorDistinctiveASCIITokens(text string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, match := range taskAnchorTokenRE.FindAllString(strings.ToLower(text), -1) {
		if _, stop := taskAnchorASCIIStopwords[match]; stop {
			continue
		}
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, match)
	}
	return out
}

func tokenSetsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	index := make(map[string]struct{}, len(a))
	for _, item := range a {
		index[item] = struct{}{}
	}
	for _, item := range b {
		if _, ok := index[item]; ok {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) rememberTaskAnchorDeliverable(userID string, snapshot *taskIdentityAnchor, toolName string, args map[string]interface{}) {
	if h == nil {
		return
	}
	path := taskAnchorDeliverablePath(toolName, args)
	ext := strings.ToLower(filepath.Ext(path))
	if path == "" || (ext != ".pptx" && ext != ".ppt") {
		return
	}
	clean := filepath.Clean(path)
	update := func(anchor taskIdentityAnchor) taskIdentityAnchor {
		if taskAnchorDeliverableContradicts(anchor, clean) {
			return anchor
		}
		anchor.PrimaryFiles = mergeTaskAnchorPaths(anchor.PrimaryFiles, []string{clean})
		anchor.UpdatedAt = time.Now()
		return anchor
	}
	if snapshot != nil {
		*snapshot = update(*snapshot)
	}
	if strings.TrimSpace(userID) == "" {
		return
	}
	current, ok := h.taskIdentityAnchorForUser(userID)
	if !ok {
		if snapshot != nil {
			current = *snapshot
		} else {
			return
		}
	}
	current = update(current)
	h.taskAnchors.Store(userID, current)
	log.Printf("[task-anchor] recorded primary deliverable user=%s file=%s", userID, clean)
}
