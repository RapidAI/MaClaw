package guiapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func discoveryContentCallbacks(text string) *sharedAgentLoopCallbacks {
	return &sharedAgentLoopCallbacks{
		userText: text,
		userID:   desktopUserID,
		handler:  &IMMessageHandler{},
		semanticSurface: &semanticCallSurface{
			replan: &semanticReplanInput{Classification: intent.ModelDiscoveryClassification()},
		},
		loopCtx: &LoopContext{OfferChatTaskDispatch: true},
	}
}

func TestContentRetrievalUtterance(t *testing.T) {
	if !contentRetrievalUtterance("查询 api2服务器状态") {
		t.Fatal("server status is a content retrieval")
	}
	if !contentRetrievalUtterance("北京天气") {
		t.Fatal("weather is a content retrieval")
	}
	if contentRetrievalUtterance("生成一张图片") {
		t.Fatal("picture creation is not a content retrieval")
	}
	if contentRetrievalUtterance("api2服务器信息，将它存入知识库") {
		t.Fatal("saving into the knowledge base is not a lookup")
	}
	if contentRetrievalUtterance("查一下 https://example.com/status") {
		t.Fatal("a user-supplied URL is fetched directly")
	}
	if contentRetrievalUtterance("给张三发一封邮件") || contentRetrievalUtterance("看一下") {
		t.Fatal("mail and a bare look are not content retrieval")
	}
}

func TestContentRetrievalGateOrdersMemoryThenKnowledge(t *testing.T) {
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	for _, name := range []string{"craft_tool", "tools_search", "web_search", "web_fetch", "ssh", "bash", "knowledge_search", chatTaskToolDispatch} {
		handled, allowed, reason := cb.contentRetrievalGate(name)
		if !handled || allowed || !strings.Contains(reason, "memory_recall") {
			t.Fatalf("%s before memory: handled=%v allowed=%v reason=%q", name, handled, allowed, reason)
		}
	}
	for _, name := range []string{chatTaskToolSteer, chatTaskToolStop, chatTaskToolRequestInput, "current_datetime"} {
		if handled, _, _ := cb.contentRetrievalGate(name); handled {
			t.Fatalf("%s stays outside the ladder", name)
		}
	}
	if handled, allowed, reason := cb.contentRetrievalGate("memory_recall"); !handled || !allowed || reason != "" {
		t.Fatalf("memory: handled=%v allowed=%v reason=%q", handled, allowed, reason)
	}

	cb.contentRetrievalMemoryDone = true
	for _, name := range []string{"craft_tool", "web_search", "ssh", chatTaskToolDispatch} {
		handled, allowed, reason := cb.contentRetrievalGate(name)
		if !handled || allowed || !strings.Contains(reason, "knowledge_search") || !strings.Contains(reason, "以知识库为准") {
			t.Fatalf("%s before knowledge: handled=%v allowed=%v reason=%q", name, handled, allowed, reason)
		}
	}
	if handled, allowed, _ := cb.contentRetrievalGate("knowledge_search"); !handled || !allowed {
		t.Fatal("knowledge_search after memory must run")
	}

	cb.contentRetrievalKnowledgeDone = true
	for _, name := range []string{"ssh", "web_search", "craft_tool", "tools_search", chatTaskToolDispatch} {
		if handled, _, reason := cb.contentRetrievalGate(name); handled {
			t.Fatalf("%s still owned after both stores: %s", name, reason)
		}
	}
}

func TestContentRetrievalGateUsesHistoryAndSkipsOtherTurns(t *testing.T) {
	cb := discoveryContentCallbacks("查询另一台服务器状态")
	cb.checkpointHistory = []agent.ConversationEntry{
		{Role: "user", Content: "查询 api2服务器状态"},
		{Role: "tool", ToolName: "memory_recall", Content: "old memory" + contentRetrievalMemoryFooter},
		{Role: "tool", ToolName: "knowledge_search", Content: "old knowledge" + contentRetrievalKnowledgeFooter},
		{Role: "assistant", Content: "上一轮已经答完"},
		{Role: "user", Content: "查询另一台服务器状态"},
	}
	if handled, allowed, _ := cb.contentRetrievalGate("craft_tool"); !handled || allowed {
		t.Fatal("an earlier turn's retrieval must not open craft_tool")
	}
	cb.checkpointHistory = append(cb.checkpointHistory, agent.ConversationEntry{
		Role: "tool", ToolName: "memory_recall", Content: "[system rejected] memory_recall_query_required",
	})
	if handled, allowed, _ := cb.contentRetrievalGate("knowledge_search"); !handled || allowed {
		t.Fatal("a rejected memory call is not a consultation")
	}
	cb.checkpointHistory = append(cb.checkpointHistory, agent.ConversationEntry{
		Role: "tool", ToolName: "memory", Content: "No matching memory found." + contentRetrievalMemoryFooter,
	})
	cb.checkpointHistory = append(cb.checkpointHistory, agent.ConversationEntry{
		Role: "user", ToolName: "", Content: "继续", HostContinuation: true,
	})
	if handled, allowed, _ := cb.contentRetrievalGate("knowledge_search"); !handled || !allowed {
		t.Fatal("this turn's memory consultation opens knowledge_search")
	}

	classified := discoveryContentCallbacks("查询 api2服务器状态")
	classified.semanticSurface.replan.Classification = intent.ClassificationResult{Primary: intent.LabelSearch, Reason: "tree"}
	if handled, _, _ := classified.contentRetrievalGate("craft_tool"); handled {
		t.Fatal("a classified turn keeps its own grants")
	}

	direct := discoveryContentCallbacks("打开 https://example.com/status")
	if handled, _, _ := direct.contentRetrievalGate("web_fetch"); handled {
		t.Fatal("a supplied URL is not held behind the ladder")
	}
}

func TestContentRetrievalCallAllowanceBlocksDispatchUntilKnowledge(t *testing.T) {
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	allowed, reason := cb.IsToolCallAllowed(chatTaskToolDispatch, `{"request":"check the server"}`)
	if allowed || !strings.Contains(reason, "memory_recall") {
		t.Fatalf("dispatch allowed=%v reason=%q", allowed, reason)
	}
	allowed, reason = cb.IsToolCallAllowed("craft_tool", `{"task":"find api2"}`)
	if allowed || !strings.Contains(reason, "memory_recall") {
		t.Fatalf("craft_tool allowed=%v reason=%q", allowed, reason)
	}
	if cb.IsToolAllowed("craft_tool") || cb.IsToolAllowed("knowledge_search") || !cb.IsToolAllowed("memory_recall") {
		t.Fatal("memory_recall is the only retrieval the first wave may run")
	}
	if msg := cb.ToolDenialMessage("craft_tool"); !strings.Contains(msg, "memory_recall") {
		t.Fatalf("denial=%q", msg)
	}
	if allowed, reason = cb.IsToolCallAllowed(chatTaskToolStop, `{}`); !allowed || reason != "" {
		t.Fatalf("stop stays available: allowed=%v reason=%q", allowed, reason)
	}

	cb.contentRetrievalMemoryDone = true
	cb.contentRetrievalKnowledgeDone = true
	if allowed, reason = cb.IsToolCallAllowed("craft_tool", `{"task":"find api2"}`); !allowed {
		t.Fatalf("craft_tool after both stores: %s", reason)
	}
	if _, reason = cb.IsToolCallAllowed("ssh", `{}`); strings.Contains(reason, "memory_recall") {
		t.Fatalf("ssh left the ladder: %s", reason)
	}
}

func TestContentRetrievalExecuteUsesHooks(t *testing.T) {
	h := &IMMessageHandler{}
	h.semanticTrustedMemoryRecall = func(userID, query string) (string, error) {
		if userID != desktopUserID || query != "查询 api2服务器状态" {
			t.Fatalf("memory principal=%q query=%q", userID, query)
		}
		return "memory says old host", nil
	}
	h.semanticTrustedKnowledgeRead = func(userID, query string) (string, error) {
		if userID != desktopUserID || query != "api2" {
			t.Fatalf("knowledge principal=%q query=%q", userID, query)
		}
		return "knowledge says api2.example.test", nil
	}
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	cb.handler = h

	text, handled := cb.executeContentRetrievalCall("memory_recall", `{}`)
	if !handled || !strings.Contains(text, "memory_recall_query_required") || cb.contentRetrievalMemoryDone {
		t.Fatalf("empty memory query handled=%v done=%v text=%q", handled, cb.contentRetrievalMemoryDone, text)
	}
	text, handled = cb.executeContentRetrievalCall("knowledge_search", `{"query":"api2"}`)
	if !handled || cb.contentRetrievalKnowledgeDone || !strings.Contains(text, "memory_recall") {
		t.Fatalf("knowledge before memory handled=%v done=%v text=%q", handled, cb.contentRetrievalKnowledgeDone, text)
	}

	text, handled = cb.executeContentRetrievalCall("memory_recall", `{"query":"查询 api2服务器状态"}`)
	if !handled || !cb.contentRetrievalMemoryDone || !strings.Contains(text, "old host") || !strings.Contains(text, "knowledge_search") || !strings.Contains(text, "以知识库为准") {
		t.Fatalf("memory result=%q done=%v", text, cb.contentRetrievalMemoryDone)
	}
	text, handled = cb.executeContentRetrievalCall("knowledge_search", `{"query":" "}`)
	if !handled || cb.contentRetrievalKnowledgeDone {
		t.Fatalf("empty knowledge query handled=%v done=%v text=%q", handled, cb.contentRetrievalKnowledgeDone, text)
	}
	text, handled = cb.executeContentRetrievalCall("knowledge_search", `{"query":"api2"}`)
	if !handled || !cb.contentRetrievalKnowledgeDone || !strings.Contains(text, "api2.example.test") || !strings.Contains(text, "以本条知识库结果为准") {
		t.Fatalf("knowledge result=%q done=%v", text, cb.contentRetrievalKnowledgeDone)
	}
	if _, handled = cb.executeContentRetrievalCall("ssh", `{}`); handled {
		t.Fatal("ssh is not a ladder call")
	}
}

func TestContentRetrievalKnowledgeErrorDoesNotOpenSSH(t *testing.T) {
	h := &IMMessageHandler{}
	h.semanticTrustedMemoryRecall = func(string, string) (string, error) {
		return "", errors.New("memory store down")
	}
	h.semanticTrustedKnowledgeRead = func(string, string) (string, error) {
		return "", errors.New("database is locked")
	}
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	cb.handler = h
	text, handled := cb.executeContentRetrievalCall("memory_recall", `{"query":"查询 api2服务器状态"}`)
	if !handled || !cb.contentRetrievalMemoryDone || !strings.Contains(text, "knowledge_search") {
		t.Fatalf("memory error: handled=%v done=%v text=%q", handled, cb.contentRetrievalMemoryDone, text)
	}
	if handled, allowed, _ := cb.contentRetrievalGate("knowledge_search"); !handled || !allowed {
		t.Fatal("knowledge stayed closed after memory failed")
	}
	if handled, allowed, _ := cb.contentRetrievalGate("ssh"); !handled || allowed {
		t.Fatal("ssh opened before knowledge")
	}

	text, handled = cb.executeContentRetrievalCall("knowledge_search", `{"query":"api2"}`)
	if !handled || cb.contentRetrievalKnowledgeDone || strings.Contains(text, contentRetrievalKnowledgeFooter) || !strings.Contains(text, "database is locked") {
		t.Fatalf("knowledge error: handled=%v done=%v text=%q", handled, cb.contentRetrievalKnowledgeDone, text)
	}
	if handled, allowed, _ := cb.contentRetrievalGate("ssh"); !handled || allowed {
		t.Fatal("ssh opened after a failed knowledge read")
	}
	cb.checkpointHistory = []agent.ConversationEntry{
		{Role: "user", Content: "查询 api2服务器状态"},
		{Role: "tool", ToolName: "memory_recall", Content: "memory failed" + contentRetrievalMemoryFooter},
		{Role: "tool", ToolName: "knowledge_search", Content: text},
	}
	cb.contentRetrievalMemoryDone = false
	cb.contentRetrievalKnowledgeDone = false
	if handled, allowed, _ := cb.contentRetrievalGate("ssh"); !handled || allowed {
		t.Fatal("a recorded knowledge error opened ssh")
	}

	empty := discoveryContentCallbacks("查询 api2服务器状态")
	empty.handler = &IMMessageHandler{
		semanticTrustedKnowledgeRead: func(string, string) (string, error) {
			return "No matching knowledge found.", nil
		},
	}
	empty.contentRetrievalMemoryDone = true
	got, handled := empty.executeContentRetrievalCall("knowledge_search", `{"query":"api2"}`)
	if !handled || !empty.contentRetrievalKnowledgeDone || !strings.Contains(got, contentRetrievalKnowledgeFooter) {
		t.Fatalf("empty knowledge: handled=%v done=%v text=%q", handled, empty.contentRetrievalKnowledgeDone, got)
	}
	if handled, _, _ := empty.contentRetrievalGate("ssh"); handled {
		t.Fatal("an empty knowledge result still held ssh")
	}
}

func TestContentRetrievalStopNudgeAndGroupBoundary(t *testing.T) {
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	if nudge := cb.ContentRetrievalStopNudge(); !strings.Contains(nudge, "memory_recall") {
		t.Fatalf("nudge=%q", nudge)
	}
	cb.contentRetrievalMemoryDone = true
	if nudge := cb.ContentRetrievalStopNudge(); !strings.Contains(nudge, "knowledge_search") || !strings.Contains(nudge, "以知识库为准") {
		t.Fatalf("nudge=%q", nudge)
	}
	if msg := cb.ToolDenialMessage("ssh"); !strings.Contains(msg, "knowledge_search") {
		t.Fatalf("denial=%q", msg)
	}
	cb.contentRetrievalKnowledgeDone = true
	if cb.ContentRetrievalStopNudge() != "" {
		t.Fatal("a finished ladder still nudged")
	}
	if discoveryContentCallbacks("生成一张图片").ContentRetrievalStopNudge() != "" {
		t.Fatal("picture creation was nudged onto the ladder")
	}

	group := discoveryContentCallbacks("查询 api2服务器状态")
	group.loopCtx.LansengerGroupPermissions = &lansengerGroupPermissionPolicy{}
	if handled, _, _ := group.contentRetrievalGate("ssh"); handled {
		t.Fatal("a group turn entered the desktop ladder")
	}
	if _, handled := group.executeContentRetrievalCall("memory_recall", `{"query":"查询 api2服务器状态"}`); handled {
		t.Fatal("a group turn read memory outside allowsTool")
	}
	if group.ContentRetrievalStopNudge() != "" {
		t.Fatal("a group turn was nudged onto the desktop ladder")
	}
	if group.IsToolAllowed("memory_recall") {
		t.Fatal("group policy listed memory_recall without a grant")
	}
}

func TestContentRetrievalStopNudgeKeepsTheKnowledgeReminder(t *testing.T) {
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	if cb.ContentRetrievalStopNudge() == "" || cb.ContentRetrievalStopNudge() == "" {
		t.Fatal("memory step allows two reminders")
	}
	if cb.ContentRetrievalStopNudge() != "" {
		t.Fatal("memory step nudged a third time")
	}
	cb.contentRetrievalMemoryDone = true
	nudge := cb.ContentRetrievalStopNudge()
	if !strings.Contains(nudge, "knowledge_search") {
		t.Fatalf("knowledge reminder was spent on memory: %q", nudge)
	}
}

func TestContentRetrievalJunkQuerySearchesTheUserText(t *testing.T) {
	h := &IMMessageHandler{}
	var memoryQuery, knowledgeQuery string
	h.semanticTrustedMemoryRecall = func(_, query string) (string, error) {
		memoryQuery = query
		return "", nil
	}
	h.semanticTrustedKnowledgeRead = func(_, query string) (string, error) {
		knowledgeQuery = query
		return "api2.example.test", nil
	}
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	cb.handler = h
	if _, handled := cb.executeContentRetrievalCall("memory_recall", `{"query":"."}`); !handled {
		t.Fatal("memory call was not handled")
	}
	if memoryQuery != "查询 api2服务器状态" {
		t.Fatalf("memory query=%q", memoryQuery)
	}
	if _, handled := cb.executeContentRetrievalCall("knowledge_search", `{"query":"a"}`); !handled {
		t.Fatal("knowledge call was not handled")
	}
	if knowledgeQuery != "查询 api2服务器状态" {
		t.Fatalf("knowledge query=%q", knowledgeQuery)
	}
	focused := discoveryContentCallbacks("查询 api2服务器状态")
	focused.handler = &IMMessageHandler{
		semanticTrustedKnowledgeRead: func(_, query string) (string, error) {
			knowledgeQuery = query
			return "api2.example.test", nil
		},
	}
	focused.contentRetrievalMemoryDone = true
	if _, handled := focused.executeContentRetrievalCall("knowledge_search", `{"query":"api2"}`); !handled {
		t.Fatal("focused knowledge call was not handled")
	}
	if knowledgeQuery != "api2" {
		t.Fatalf("focused query=%q", knowledgeQuery)
	}
}

func TestContentRetrievalOrderPromptAndTools(t *testing.T) {
	got := ensureContentRetrievalOrderPrompt("base", "查询 api2服务器状态", true)
	if !strings.Contains(got, contentRetrievalOrderMark) || !strings.Contains(got, "以知识库为准") || !strings.Contains(got, "memory_recall") {
		t.Fatalf("prompt=%q", got)
	}
	if again := ensureContentRetrievalOrderPrompt(got, "查询 api2服务器状态", true); again != got {
		t.Fatal("content retrieval prompt was appended twice")
	}
	if ensureContentRetrievalOrderPrompt("base", "查询 api2服务器状态", false) != "base" {
		t.Fatal("classified turns do not get the discovery sentence")
	}
	if ensureContentRetrievalOrderPrompt("base", "生成一张图片", true) != "base" {
		t.Fatal("picture turns do not get the discovery sentence")
	}
	if ensureContentRetrievalOrderPrompt("base", "查一下 https://example.com/a", true) != "base" {
		t.Fatal("a supplied URL does not get the discovery sentence")
	}

	tools := appendContentRetrievalTools("查询 api2服务器状态", nil)
	have := map[string]bool{}
	for _, def := range tools {
		have[extractToolName(def)] = true
	}
	if !have["memory_recall"] || !have["knowledge_search"] {
		t.Fatalf("tools=%v", have)
	}
	if again := appendContentRetrievalTools("查询 api2服务器状态", tools); len(again) != len(tools) {
		t.Fatalf("tools duplicated: %d", len(again))
	}
	if len(appendContentRetrievalTools("给张三发一封邮件", nil)) != 0 {
		t.Fatal("a non-lookup lists no retrieval tools")
	}
}
