package main

// Task identity anchors protect source-bound, multi-turn work from semantic
// drift. Conversation compaction intentionally removes large attachment bodies
// from historical turns; a compact follow-up must nevertheless retain which
// person/document it is about. This is an execution-context invariant, not an
// LLM instruction that can be lost during compaction.

import (
	"fmt"
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
	if subject == "" && len(paths) == 0 {
		return
	}
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
	if anchor.OriginalRequest == "" {
		anchor.OriginalRequest = truncateRunes(stripTaskAnchorDocumentBodies(userText), 500)
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
	if !ok || (anchor.Subject == "" && len(anchor.SourcePaths) == 0) {
		return taskIdentityAnchor{}, false
	}
	return anchor, true
}

func (h *IMMessageHandler) clearTaskIdentityAnchor(userID string) {
	if h != nil {
		h.taskAnchors.Delete(userID)
	}
}

func taskIdentityAnchorPrompt(anchor taskIdentityAnchor) string {
	if anchor.Subject == "" && len(anchor.SourcePaths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[任务身份与来源锚点 — 强制约束]\n")
	if anchor.Subject != "" {
		fmt.Fprintf(&b, "- 当前任务对象：%s。续写、浓缩、改写、翻译或生成资料时，必须保持该对象；不得替换为其他人。\n", anchor.Subject)
	}
	if len(anchor.SourcePaths) > 0 {
		b.WriteString("- 当前任务的优先事实来源：")
		b.WriteString(strings.Join(anchor.SourcePaths, "；"))
		b.WriteString("。\n")
		b.WriteString("- 若历史正文已被压缩，先重读上述来源；不得用长期记忆、历史会话、工作区中另一任务的材料补替。来源无法读取时，应说明缺少证据。\n")
	}
	b.WriteString("- 只有用户明确要求检索历史会话或切换对象时，才可跨任务检索或更换该锚点。\n")
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
	if len(anchor.SourcePaths) == 0 || taskAnchorAllowsLongTermMemoryRecall(userText) {
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
