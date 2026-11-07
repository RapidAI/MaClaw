package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// Content retrieval on a model-discovery turn is a fixed ladder: memory,
// then the knowledge base, then the network. A contradiction between the
// first two uses the knowledge base. The host lists those two reads and
// refuses the network, local scripts, and remote commands until both have run.

const contentRetrievalOrderMark = "【内容检索】"

const contentRetrievalOrderPrompt = "\n【内容检索】先调用 memory_recall，再调用 knowledge_search。两边都没有所需事实时才用网络。记忆与知识库不一致时，以知识库为准，不要问用户选哪一份。不要用 craft_tool、本机文件或 ssh 代替这两次检索。用户给出的 URL 直接抓取该地址。\n"

const contentRetrievalNeedMemory = "内容检索先调用 memory_recall，query 用用户的原话。顺序是先记忆，再知识库，后网络。记忆与知识库不一致时，以知识库为准。不要用 craft_tool、本机文件、ssh 或网络代替。"

const contentRetrievalNeedKnowledge = "记忆已经查过。先调用 knowledge_search，query 用用户的原话。与记忆不一致时以知识库为准，不要问用户选哪一份。两边都没有所需事实时再用网络。"

const contentRetrievalMemoryFooter = "\n下一步调用 knowledge_search，query 用用户的原话。与记忆不一致时以知识库为准，不要问用户选哪一份。这次知识库检索完成前，不要用网络、craft_tool、本机文件或 ssh。"

const contentRetrievalKnowledgeFooter = "\n若与记忆不一致，以本条知识库结果为准，不要问用户选哪一份。两边都没有所需事实时，再用网络。"

func contentRetrievalUtterance(text string) bool {
	text = strings.TrimSpace(text)
	// A URL the user already named is fetched directly. It is not a stored-fact lookup.
	if text == "" || discoveryPictureCreationUtterance(text) || contentRetrievalSuppliedURL(text) {
		return false
	}
	if contentRetrievalKnowledgeWrite(text) && !contentRetrievalLookupVerb(text) {
		return false
	}
	return contentRetrievalLookupVerb(text) || contentRetrievalStoredCue(text)
}

func contentRetrievalLookupVerb(text string) bool {
	for _, cue := range []string{"查询", "查一下", "查查", "查找", "检索", "是什么", "什么是", "状态", "天气"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	lower := strings.ToLower(text)
	return strings.Contains(lower, "look up") || strings.Contains(lower, "status")
}

func contentRetrievalStoredCue(text string) bool {
	for _, cue := range []string{"服务器", "主机"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(text), "ssh")
}

func contentRetrievalSuppliedURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "https://") || strings.Contains(lower, "http://")
}

func contentRetrievalKnowledgeWrite(text string) bool {
	for _, cue := range []string{"存入知识库", "保存到知识库", "记入知识库", "写入知识库", "加入知识库"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func contentRetrievalActive(surface *semanticCallSurface, userText string) bool {
	return surface != nil && surface.replan != nil && intent.ClassificationIsModelDiscovery(surface.replan.Classification) && contentRetrievalUtterance(userText)
}

func contentRetrievalExempt(name string) bool {
	switch strings.TrimSpace(name) {
	case chatTaskToolSteer, chatTaskToolStop, chatTaskToolRequestInput, "current_datetime":
		return true
	default:
		return false
	}
}

func contentRetrievalKind(name string) string {
	switch strings.TrimSpace(name) {
	case "memory_recall", "memory":
		return "memory"
	case "knowledge_search":
		return "knowledge"
	default:
		return ""
	}
}

// contentRetrievalOwned is the desktop discovery ladder. A group policy has
// its own allow list; running the reads here would skip that check, and
// waiting on a forbidden read would close every other tool.
func (c *sharedAgentLoopCallbacks) contentRetrievalOwned() bool {
	if c == nil || (c.loopCtx != nil && c.loopCtx.LansengerGroupPermissions != nil) {
		return false
	}
	return contentRetrievalActive(c.semanticSurface, c.userText)
}

// contentRetrievalGate reports whether this discovery content turn still owns
// the call. handled is false once memory and knowledge have both run, so the
// ordinary grant rules decide ssh and the network. reason is empty when the
// call may run.
func (c *sharedAgentLoopCallbacks) contentRetrievalGate(name string) (handled bool, allowed bool, reason string) {
	if !c.contentRetrievalOwned() || contentRetrievalExempt(name) {
		return false, false, ""
	}
	memory, knowledge := c.contentRetrievalProgress()
	switch contentRetrievalKind(name) {
	case "memory":
		return true, true, ""
	case "knowledge":
		if !memory {
			return true, false, contentRetrievalNeedMemory
		}
		return true, true, ""
	default:
		if !memory {
			return true, false, contentRetrievalNeedMemory
		}
		if !knowledge {
			return true, false, contentRetrievalNeedKnowledge
		}
		return false, false, ""
	}
}

func (c *sharedAgentLoopCallbacks) contentRetrievalProgress() (memory bool, knowledge bool) {
	if c == nil {
		return false, false
	}
	memory = c.contentRetrievalMemoryDone
	knowledge = c.contentRetrievalKnowledgeDone
	for _, entry := range c.contentRetrievalTurnTools() {
		text := conversationEntryText(entry.Content)
		switch strings.TrimSpace(entry.ToolName) {
		case "memory", "memory_recall":
			if strings.Contains(text, contentRetrievalMemoryFooter) {
				memory = true
			}
		case "knowledge_search":
			if strings.Contains(text, contentRetrievalKnowledgeFooter) {
				knowledge = true
			}
		}
	}
	return memory, knowledge
}

// contentRetrievalTurnTools is this turn's tool rows. Checkpoint history also
// holds earlier turns, and a tool name alone is not a consultation: an empty
// query is rejected without the footer the completed call appends.
func (c *sharedAgentLoopCallbacks) contentRetrievalTurnTools() []agent.ConversationEntry {
	if c == nil || len(c.checkpointHistory) == 0 {
		return nil
	}
	start := 0
	for i := len(c.checkpointHistory) - 1; i >= 0; i-- {
		entry := c.checkpointHistory[i]
		if strings.EqualFold(strings.TrimSpace(entry.Role), "user") && !entry.HostContinuation {
			start = i + 1
			break
		}
	}
	out := make([]agent.ConversationEntry, 0)
	for _, entry := range c.checkpointHistory[start:] {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "tool") {
			out = append(out, entry)
		}
	}
	return out
}

func appendContentRetrievalTools(userText string, tools []map[string]interface{}) []map[string]interface{} {
	if !contentRetrievalUtterance(userText) {
		return tools
	}
	return appendUniqueToolDefs(tools, contentRetrievalToolDefs())
}

func contentRetrievalToolDefs() []map[string]interface{} {
	query := map[string]interface{}{
		"type":        "string",
		"description": "用户的原话，或从中抽出的检索词。",
	}
	schema := func() map[string]interface{} {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": query,
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		}
	}
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "memory_recall",
				"description": "先查记忆。只读。内容检索的第一步，完成后再查知识库。",
				"parameters":  schema(),
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        "knowledge_search",
				"description": "再查知识库。只读。与记忆不一致时以这里的结果为准。两边都没有再用网络。",
				"parameters":  schema(),
			},
		},
	}
}

func ensureContentRetrievalOrderPrompt(prompt, userText string, discovery bool) string {
	if !discovery || !contentRetrievalUtterance(userText) || strings.Contains(prompt, contentRetrievalOrderMark) {
		return prompt
	}
	return prompt + contentRetrievalOrderPrompt
}

func (c *sharedAgentLoopCallbacks) executeContentRetrievalCall(name, argsJSON string) (string, bool) {
	if !c.contentRetrievalOwned() {
		return "", false
	}
	switch contentRetrievalKind(name) {
	case "memory":
		return c.runContentMemoryRecall(argsJSON), true
	case "knowledge":
		return c.runContentKnowledgeSearch(argsJSON), true
	default:
		return "", false
	}
}

func (c *sharedAgentLoopCallbacks) runContentMemoryRecall(argsJSON string) string {
	query, err := contentRetrievalQuery(argsJSON)
	if err != nil {
		return "[system rejected] memory_recall_query_required"
	}
	if c.handler == nil {
		return "[system rejected] trusted_memory_recall_unavailable"
	}
	principal := strings.TrimSpace(c.userID)
	if principal == "" {
		principal = desktopUserID
	}
	query = contentRetrievalConsultQuery(c.userText, query)
	var text string
	if c.handler.semanticTrustedMemoryRecall != nil {
		text, err = c.handler.semanticTrustedMemoryRecall(principal, query)
	} else {
		text, err = c.handler.recallTrustedMemory(principal, query)
	}
	c.contentRetrievalMemoryDone = true
	if err != nil {
		return "[system rejected] " + err.Error() + contentRetrievalMemoryFooter
	}
	if strings.TrimSpace(text) == "" {
		text = "No matching memory found."
	}
	return text + contentRetrievalMemoryFooter
}

func (c *sharedAgentLoopCallbacks) runContentKnowledgeSearch(argsJSON string) string {
	if memory, _ := c.contentRetrievalProgress(); !memory {
		return "[system rejected] " + contentRetrievalNeedMemory
	}
	query, err := contentRetrievalQuery(argsJSON)
	if err != nil {
		return "[system rejected] memory_recall 已经完成。knowledge_search 需要 query。"
	}
	if c.handler == nil {
		return "[system rejected] trusted_knowledge_read_unavailable"
	}
	principal := strings.TrimSpace(c.userID)
	if principal == "" {
		principal = desktopUserID
	}
	query = contentRetrievalConsultQuery(c.userText, query)
	text, err := c.handler.readTrustedKnowledge(principal, query)
	if err != nil {
		// An error is not an empty library. Recording it as consulted used to
		// open ssh and the network, which is the skip this ladder exists to stop.
		// The no-progress stop ends a store that keeps failing.
		return "[system rejected] " + err.Error() + "\n知识库这一步没有完成。请重试 knowledge_search。完成前不要用网络、craft_tool、本机文件或 ssh。"
	}
	c.contentRetrievalKnowledgeDone = true
	if strings.TrimSpace(text) == "" {
		text = "No matching knowledge found."
	}
	return text + contentRetrievalKnowledgeFooter
}

// ContentRetrievalStopNudge implements agent.ContentRetrievalStopGuard.
// Hiding craft_tool and ssh until the two reads finish also removes the
// side-effecting tool the stored-fact nudge requires, so a prose answer
// would skip the stores. Each missing step is asked twice. A third prose
// stop on the same step ends that reminder, and finishing memory still
// leaves the knowledge reminder unused.
func (c *sharedAgentLoopCallbacks) ContentRetrievalStopNudge() string {
	if !c.contentRetrievalOwned() {
		return ""
	}
	memory, knowledge := c.contentRetrievalProgress()
	if !memory {
		if c.contentRetrievalMemoryNudges >= 2 {
			return ""
		}
		c.contentRetrievalMemoryNudges++
		return "[系统] " + contentRetrievalNeedMemory
	}
	if !knowledge {
		if c.contentRetrievalKnowledgeNudges >= 2 {
			return ""
		}
		c.contentRetrievalKnowledgeNudges++
		return "[系统] " + contentRetrievalNeedKnowledge
	}
	return ""
}

// contentRetrievalConsultQuery is the text actually searched. A focused
// extract of the user's words is kept. Any other model query, including a
// one-character token, is replaced with the user text. Searching that token
// and treating an empty hit as "the library has no fact" used to open ssh.
func contentRetrievalConsultQuery(userText, modelQuery string) string {
	userText = strings.TrimSpace(userText)
	modelQuery = strings.TrimSpace(modelQuery)
	if userText == "" {
		return modelQuery
	}
	if utf8.RuneCountInString(modelQuery) >= 2 && strings.Contains(strings.ToLower(userText), strings.ToLower(modelQuery)) {
		return modelQuery
	}
	return userText
}

func contentRetrievalQuery(argsJSON string) (string, error) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil || parsed == nil {
		return "", fmt.Errorf("query required")
	}
	query, _ := parsed["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query required")
	}
	return query, nil
}
