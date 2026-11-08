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
	if contentRetrievalUtterance("查一下 https://example.com/status") {
		t.Fatal("a user-supplied URL is fetched directly")
	}
	if contentRetrievalUtterance("给张三发一封邮件") || contentRetrievalUtterance("看一下") {
		t.Fatal("mail and a bare look are not content retrieval")
	}
	base := "api2服务器信息：api2.maclaw.top root [REDACTED]"
	if contentRetrievalUtterance(base+"，保存进知识库") != contentRetrievalUtterance(base) {
		t.Fatal("save wording must not change retrieval activation")
	}
}

func TestContentRetrievalGateOrdersMemoryThenKnowledge(t *testing.T) {
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	for _, name := range []string{"craft_tool", "web_search", "web_fetch", "ssh", "bash", "knowledge_search", chatTaskToolDispatch} {
		handled, allowed, reason := cb.contentRetrievalGate(name)
		if !handled || allowed || !strings.Contains(reason, "memory_recall") {
			t.Fatalf("%s before memory: handled=%v allowed=%v reason=%q", name, handled, allowed, reason)
		}
	}
	if handled, _, _ := cb.contentRetrievalGate("tools_search"); handled {
		t.Fatal("tools_search is decided from its arguments")
	}
	if !cb.IsToolAllowed("tools_search") {
		t.Fatal("tools_search was removed before its arguments could be read")
	}
	if handled, allowed, reason := cb.contentRetrievalGateCall("tools_search", `{"needs":["ssh"]}`); !handled || allowed || !strings.Contains(reason, "memory_recall") {
		t.Fatalf("ssh discovery: handled=%v allowed=%v reason=%q", handled, allowed, reason)
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

func TestContentRetrievalKnowledgeWriteDiscoverySkipsReads(t *testing.T) {
	for _, userText := range []string{
		"api2服务器信息：api2.maclaw.top root [REDACTED]，保存进知识库",
		"save the api2 server status into the knowledge base",
	} {
		cb := discoveryContentCallbacks(userText)
		allowed, reason := cb.IsToolCallAllowed(semanticToolsSearchName, `{"needs":["knowledge_save_text"]}`)
		if !allowed || reason != "" {
			t.Fatalf("write discovery remained behind retrieval for %q: allowed=%v reason=%q", userText, allowed, reason)
		}
		for _, name := range []string{"knowledge_save_text", semanticTrustedKnowledgeIngestAdapter} {
			if handled, _, reason := cb.contentRetrievalGateCall(name, `{"text":"note"}`); handled {
				t.Fatalf("%s was held behind retrieval for %q: %q", name, userText, reason)
			}
		}
	}
	cb := discoveryContentCallbacks("查询 api2服务器状态")
	for _, args := range []string{
		`{"needs":["web_search"]}`,
		`{"needs":["knowledge_save_text","web_search"]}`,
		`{"needs":["knowledge_save_url"]}`,
		`{"needs":["semantic_ingest_trusted_knowledge"]}`,
		`{"needs":"knowledge_save_text"}`,
		`{}`,
	} {
		if handled, allowed, reason := cb.contentRetrievalGateCall(semanticToolsSearchName, args); !handled || allowed || !strings.Contains(reason, "memory_recall") {
			t.Fatalf("non-write discovery escaped retrieval: args=%s handled=%v allowed=%v reason=%q", args, handled, allowed, reason)
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
		if userID != desktopUserID || query != "api2" {
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
	if memoryQuery != "api2" {
		t.Fatalf("memory query=%q", memoryQuery)
	}
	for _, cue := range []string{"a", "查询", "状态", "服务器状态"} {
		if _, handled := cb.executeContentRetrievalCall("knowledge_search", `{"query":"`+cue+`"}`); !handled {
			t.Fatalf("knowledge call %q was not handled", cue)
		}
		if knowledgeQuery != "api2" {
			t.Fatalf("knowledge query for %q=%q", cue, knowledgeQuery)
		}
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
	if _, handled := focused.executeContentRetrievalCall("knowledge_search", `{"query":"api2服务器"}`); !handled {
		t.Fatal("wider extract was not handled")
	}
	if knowledgeQuery != "api2" {
		t.Fatalf("wider extract=%q", knowledgeQuery)
	}
}

func TestContentRetrievalTwoCitiesAreBothSearched(t *testing.T) {
	var got []string
	cb := discoveryContentCallbacks("查一下北京，上海的天气")
	cb.contentRetrievalMemoryDone = true
	cb.handler = &IMMessageHandler{
		semanticTrustedKnowledgeRead: func(_, query string) (string, error) {
			got = append(got, query)
			if query == "上海" {
				return "上海晴", nil
			}
			return "No matching knowledge found.", nil
		},
	}
	text, handled := cb.executeContentRetrievalCall("knowledge_search", `{"query":"天气"}`)
	if !handled || !cb.contentRetrievalKnowledgeDone || strings.Join(got, ",") != "北京,上海" || !strings.Contains(text, "上海晴") || strings.Contains(text, "No matching knowledge found.") {
		t.Fatalf("handled=%v done=%v queries=%v text=%q", handled, cb.contentRetrievalKnowledgeDone, got, text)
	}
}

func TestContentRetrievalConsultQueryUsesTheSubject(t *testing.T) {
	weather := "查一下北京天气"
	for _, query := range []string{"天气", "查一下", "一下", "下北", "北京天气", weather} {
		if got := contentRetrievalConsultQuery(weather, query); got != "北京" {
			t.Fatalf("weather query %q searched %q", query, got)
		}
	}
	if got := contentRetrievalConsultQuery(weather, "北京"); got != "北京" {
		t.Fatalf("weather subject=%q", got)
	}
	if got := contentRetrievalConsultQuery("查一下北京和上海的天气", "北京"); got != "北京" {
		t.Fatalf("one city=%q", got)
	}
	if got := contentRetrievalConsultQuery("查询 api2服务器状态", "查询 api2服务器状态"); got != "api2" {
		t.Fatalf("full status utterance=%q", got)
	}
	if got := contentRetrievalConsultQuery("ssh到那台主机", "主机"); got != "ssh到那台主机" {
		t.Fatalf("host cue query=%q", got)
	}
	if got := contentRetrievalConsultQuery("ssh到那台主机", "ssh到那台主机"); got != "ssh到那台主机" {
		t.Fatalf("host utterance=%q", got)
	}
	if got := contentRetrievalConsultQuery("查询状态", "状态"); got != "查询状态" {
		t.Fatalf("cue-only utterance=%q", got)
	}
	if got := contentRetrievalConsultQuery("查询 maclaw 状态", "状态"); got != "maclaw" {
		t.Fatalf("name query=%q", got)
	}
	both := "查询 api2 与 web1 的状态"
	if got := contentRetrievalConsultQuery(both, "状态"); got != both {
		t.Fatalf("two hosts were ANDed: %q", got)
	}
	if got := contentRetrievalConsultQuery(both, "web1"); got != "web1" {
		t.Fatalf("one of two hosts=%q", got)
	}
	if got := contentRetrievalConsultQuery("check the status of the backup", "status"); got != "backup" {
		t.Fatalf("english status query=%q", got)
	}
	for _, query := range []string{"of", "the", "of the", "the backup"} {
		if got := contentRetrievalConsultQuery("check the status of the backup", query); got != "backup" {
			t.Fatalf("english function word %q searched %q", query, got)
		}
	}
	if got := contentRetrievalConsultQuery("look up api2.maclaw.top status", "api2.maclaw.top"); got != "api2" {
		t.Fatalf("repeated hostname=%q", got)
	}
	commaCities := "查一下北京，上海的天气"
	if got := strings.Join(contentRetrievalConsultQueries(commaCities, "天气"), ","); got != "北京,上海" {
		t.Fatalf("two cities=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries(commaCities, commaCities), ","); got != "北京,上海" {
		t.Fatalf("original two-city sentence=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries(commaCities, "北京"), ","); got != "北京" {
		t.Fatalf("one city of two=%q", got)
	}
	twoNames := "check the status of api2 and web1"
	if got := strings.Join(contentRetrievalConsultQueries(twoNames, twoNames), ","); got != "api2,web1" {
		t.Fatalf("two identifiers=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries(twoNames, "web1"), ","); got != "web1" {
		t.Fatalf("one identifier=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries(both, both), ","); got != "api2,web1" {
		t.Fatalf("two host sentence=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries("查一下北京和上海的天气", "和上"), ","); got != "北京,上海" {
		t.Fatalf("interior slice=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries("查一下北京和上海的天气", "上海的"), ","); got != "上海" {
		t.Fatalf("particle glued to the city=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries("查一下北京天气怎么样", "天气"), ","); got != "北京" {
		t.Fatalf("question tail=%q", got)
	}
	if got := contentRetrievalConsultQuery("查一下北京天气怎么样", "怎么样"); got != "北京" {
		t.Fatalf("question word completed the step: %q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries("查一下北京天气怎么样", "怎么样"), ","); got != "北京" {
		t.Fatalf("question word as query=%q", got)
	}
	if got := strings.Join(contentRetrievalConsultQueries("ssh到那台主机", "主机"), ","); got != "ssh到那台主机" {
		t.Fatalf("host sentence=%q", got)
	}
	if got := contentRetrievalConsultQuery("look up api2.maclaw.top status", "status"); got != "api2" {
		t.Fatalf("domain label searched instead of the host id: %q", got)
	}
	for _, query := range []string{"状态", "查询 api2.maclaw.top 状态"} {
		if got := contentRetrievalConsultQuery("查询 api2.maclaw.top 状态", query); got != "api2" {
			t.Fatalf("chinese host query %q searched %q", query, got)
		}
	}
	if got := contentRetrievalConsultQuery("look up files.maclaw.top status", "status"); got != "files.maclaw.top" {
		t.Fatalf("letter host=%q", got)
	}
	if got := contentRetrievalConsultQuery("查询 192.168.0.1 状态", "状态"); got != "192.168.0.1" {
		t.Fatalf("address=%q", got)
	}
	twoDotted := "查询 api2.maclaw.top 与 web1.maclaw.top 的状态"
	if got := contentRetrievalConsultQuery(twoDotted, "状态"); got != twoDotted {
		t.Fatalf("two dotted hosts=%q", got)
	}
	ported := "查询 api2.maclaw.top:22 状态"
	for _, query := range []string{"状态", "22", "api2.maclaw.top:22", ported} {
		if got := contentRetrievalConsultQuery(ported, query); got != "api2" {
			t.Fatalf("ported host query %q searched %q", query, got)
		}
	}
	if got := contentRetrievalConsultQuery("look up api2.maclaw.top:22 status", "22"); got != "api2" {
		t.Fatalf("english port=%q", got)
	}
	address := "查询 192.168.0.1 状态"
	if got := contentRetrievalConsultQuery(address, "192"); got != "192.168.0.1" {
		t.Fatalf("octet=%q", got)
	}
	if got := contentRetrievalConsultQuery("查询 192.168.0.1:22 状态", "22"); got != "192.168.0.1" {
		t.Fatalf("address port=%q", got)
	}
	twoPorts := "查询 api2.maclaw.top:22 与 web1.maclaw.top:443 的状态"
	if got := contentRetrievalConsultQuery(twoPorts, "22"); got != twoPorts {
		t.Fatalf("two ported hosts=%q", got)
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
	if !strings.Contains(got, "保存新内容") || !strings.Contains(got, semanticToolsSearchName) {
		t.Fatal("the retrieval prompt does not offer the tool-driven write path")
	}
}
