package agent

import (
	"strings"
	"testing"
)

func TestShouldNudgeStoredRetrieval(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "ssh"}},
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0, "", "") {
		t.Fatal("a full turn that has not called a tool must look up stored facts")
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0, "帮我连上那台机器", "") {
		t.Fatal("an operational ask must still look up stored facts")
	}
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, "你好呀", "") {
		t.Fatal("a pure greeting must not be forced to search storage")
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0, "你好，帮我连上那台机器", "") {
		t.Fatal("a greeting plus a task must still look up stored facts")
	}
	if shouldNudgeStoredRetrieval(true, tools, nil, 0, "", "") {
		t.Fatal("a light turn must not be forced to search storage")
	}
	if shouldNudgeStoredRetrieval(false, tools, nil, 1, "", "") {
		t.Fatal("the retrieval nudge is one-shot")
	}
	used := []ConversationEntry{{Role: "tool", ToolName: "web_search"}}
	if shouldNudgeStoredRetrieval(false, tools, used, 0, "", "") {
		t.Fatal("a turn that already called a tool must not be nudged")
	}
	onlySSH := []map[string]interface{}{{"function": map[string]interface{}{"name": "ssh"}}}
	if shouldNudgeStoredRetrieval(false, onlySSH, nil, 0, "", "") {
		t.Fatal("nothing to force when memory and knowledge_search are absent")
	}
	lookupOnly := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
	}
	if shouldNudgeStoredRetrieval(false, lookupOnly, nil, 0, "", "") {
		t.Fatal("a lookup surface must not be forced to search storage before answering")
	}
	readOnly := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "read_file"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "knowledge_search"}},
	}
	if shouldNudgeStoredRetrieval(false, readOnly, nil, 0, "", "") {
		t.Fatal("a read-only file surface must not be forced to search storage")
	}
}

func TestShouldNotNudgeStoredRetrievalForFinishedTranslation(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "ssh"}},
	}
	user := "翻译以下摘要：Retrieval-augmented generation (RAG) improves large language models."
	reply := "## 术语表\n| 原文 | 译文 |\n| --- | --- |\n| reranking | 重排序 |\n\n## 译文\n检索增强生成通过引入外部知识来提升大语言模型。重排序已成为关键技术。"
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, user, reply) {
		t.Fatal("a finished translation must stay the visible answer")
	}
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, user, "请告诉我是否需要翻译下一段。") {
		t.Fatal("asking whether to continue is not a request for stored connection facts")
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0, user, "请提供 SSH 主机、端口和密码。") {
		t.Fatal("a reply that asks for connection facts must still look them up")
	}
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, user, "请提供一份 important report，说明 support 与 transport。") {
		t.Fatal("port inside important, report, support, or transport is not a connection port")
	}
	if !answerSolicitsMissingFacts("please provide the ssh host and port") {
		t.Fatal("a whole-word host and port request must still count")
	}
	quotedAsk := "前文请提供主机和密码，引用到此为止。" + strings.Repeat("检索增强生成通过外部知识提升模型。", 30)
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, user, quotedAsk) {
		t.Fatal("a quoted request inside a long translation must not force a lookup")
	}
	if !shouldNudgeStoredRetrieval(false, tools, nil, 0, "帮我连上那台机器", reply) {
		t.Fatal("an operational ask must still look up stored facts even after a long reply")
	}
	pasted := "翻译以下摘要：" + strings.Repeat("甲", 80) + "ssh 主机密码。"
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, pasted, reply) {
		t.Fatal("a host mention inside pasted source text must not force a lookup")
	}
	neurRank := "翻译以下摘要：Retrieval-augmented generation (RAG) improves large language models (LLMs) by incorporating external knowledge retrieved from document collections."
	if shouldNudgeStoredRetrieval(false, tools, nil, 0, neurRank, reply) {
		t.Fatal("the NeurRank abstract must stay a finished translation")
	}
}

func TestPreferPreNudgeAnswerWhenFollowUpEchoesInstruction(t *testing.T) {
	translation := "## 译文\n检索增强生成通过引入外部知识来提升大语言模型。"
	echo := "已收到该查询优先级指令（先查记忆 → 再查知识库 → 均无才向用户索要事实）。上一轮翻译已完成。"
	if got := preferPreNudgeAnswer(translation, echo); got != translation {
		t.Fatalf("echo of the lookup instruction must not replace the translation: %s", got)
	}
	continued := "已用记忆里的主机连上了 api2。"
	if got := preferPreNudgeAnswer("请提供主机。", continued); got != continued {
		t.Fatalf("a real follow-up must stand: %s", got)
	}
	quoted := "先查记忆（memory），再查知识库（knowledge_search）。两边都没有再问你。"
	if got := preferPreNudgeAnswer(translation, quoted); got != translation {
		t.Fatalf("a paraphrase of the injected instruction must not replace the translation: %s", got)
	}
	lookedUp := "已按查询优先级查过记忆和知识库，没有这台主机的密码，请提供密码。"
	if got := preferPreNudgeAnswer("请提供主机和密码。", lookedUp); got != lookedUp {
		t.Fatalf("a lookup follow-up that still needs a password must stand: %s", got)
	}
	carried := translation + "\n\n已收到该查询优先级指令。译文不依赖外部记忆或知识库，上面这一份就是完整译文，不需要再查，也不要另起一条说明。"
	if got := preferPreNudgeAnswer(translation, carried); got != carried {
		t.Fatalf("a follow-up that keeps the translation and adds to it must stand: %s", got)
	}
	mentioned := strings.Repeat(translation+"\n", 4) + "查询优先级按原文保留。"
	if got := preferPreNudgeAnswer(mentioned, echo); got != mentioned {
		t.Fatalf("a longer deliverable that mentions the lookup phrase must stay: %s", got)
	}
	shortEcho := "已收到查询优先级指令。"
	later := "已按查询优先级核对过，主机是 api2，端口 22，可以连上。"
	if got := preferPreNudgeAnswer(shortEcho, later); got != later {
		t.Fatalf("a longer later answer that also mentions the phrase must stand: %s", got)
	}
	// The 2026-10-08 论文翻译专家 receipt. It quotes the lookup rule and asks
	// what to translate next. 请告知 / 您提供 / 需要核验 are not a request for
	// a host, account, or password.
	receipt := "已收到该查询优先级指令（先查记忆 → 再查知识库 → 均无才向用户索要事实，单边有结果或两边一致则直接沿用）。\n\n上一轮的摘要翻译任务已基于您提供的原文独立完成，译文未依赖任何外部记忆或知识库事实，因此该流程当时未触发。\n\n若后续任务涉及需要核验的论文、术语或事实（例如确认 NeurRank 是否为某篇已发表工作的通行译名、或核对引文细节），我会严格按上述顺序执行。请告知下一步翻译任务。"
	if answerSolicitsMissingFacts(receipt) {
		t.Fatal("the paper-translation receipt must not count as a request for connection facts")
	}
	if got := preferPreNudgeAnswer(translation, receipt); got != translation {
		t.Fatalf("the paper-translation receipt must not replace the translation: %s", got)
	}
	shortDeliverable := "检索增强生成通过外部知识提升大语言模型。查询优先级按原文保留。"
	if got := preferPreNudgeAnswer(shortDeliverable, receipt); got != shortDeliverable {
		t.Fatalf("a finished sentence that mentions the lookup phrase must stay ahead of the longer receipt: %s", got)
	}
	if followReportsConnectionFact(receipt) {
		t.Fatal("the paper-translation receipt must not count as a reported host or password")
	}
	found := "已按查询优先级核对过，主机是 api2，端口 22，可以连上。"
	if got := preferPreNudgeAnswer(translation, found); got != found {
		t.Fatalf("a follow-up that reports the host must stand: %s", got)
	}
	if got := preferPreNudgeAnswer(mentioned, found); got != found {
		t.Fatalf("a host report must stand ahead of a deliverable that mentions the lookup phrase: %s", got)
	}
	note := "已按查询优先级核对过，论文里的服务器地址保持原样。"
	if got := preferPreNudgeAnswer(translation, note); got != translation {
		t.Fatalf("mentioning 服务器地址 in an acknowledgement must leave the translation in place: %s", got)
	}
	whether := "已按查询优先级确认主机是否需要保留。"
	if got := preferPreNudgeAnswer(translation, whether); got != translation {
		t.Fatalf("主机是否 is not a stated host: %s", got)
	}
	onlyPort := "已按查询优先级核对，端口 22。"
	if got := preferPreNudgeAnswer(translation, onlyPort); got != onlyPort {
		t.Fatalf("a stated port must stand: %s", got)
	}
	if got := preferPreNudgeAnswer("NeurRank", receipt); got != "NeurRank" {
		t.Fatalf("a short name that occurs inside the receipt must stay: %s", got)
	}
	if got := preferPreNudgeAnswer("独立完成", receipt); got != "独立完成" {
		t.Fatalf("a short phrase that occurs inside the receipt must stay: %s", got)
	}
	bareCopula := "已按查询优先级核对，主机是。"
	if got := preferPreNudgeAnswer(translation, bareCopula); got != translation {
		t.Fatalf("a copula with no value must leave the translation in place: %s", got)
	}
	emptyColon := "已按查询优先级核对，主机："
	if got := preferPreNudgeAnswer(translation, emptyColon); got != translation {
		t.Fatalf("a colon with no value must leave the translation in place: %s", got)
	}
	question := "已按查询优先级确认主机是不是需要保留。"
	if got := preferPreNudgeAnswer(translation, question); got != translation {
		t.Fatalf("主机是不是 is a question and must leave the translation in place: %s", got)
	}
	purpose := "已按查询优先级核对过，论文里的服务器为了保持术语一致未改。"
	if got := preferPreNudgeAnswer(translation, purpose); got != translation {
		t.Fatalf("服务器为了 must leave the translation in place: %s", got)
	}
	decoy := "已按查询优先级核对，important report 与 transport 都保留原文。"
	if got := preferPreNudgeAnswer(translation, decoy); got != translation {
		t.Fatalf("important and report must not count as a port: %s", got)
	}
	colonHost := "已按查询优先级核对，host: api2。"
	if got := preferPreNudgeAnswer(translation, colonHost); got != colonHost {
		t.Fatalf("host: api2 must stand: %s", got)
	}
	colonPort := "已按查询优先级核对，port: 22。"
	if got := preferPreNudgeAnswer(translation, colonPort); got != colonPort {
		t.Fatalf("port: 22 must stand: %s", got)
	}
	spacedPort := "已按查询优先级核对，port 22。"
	if got := preferPreNudgeAnswer(translation, spacedPort); got != spacedPort {
		t.Fatalf("port 22 must stand: %s", got)
	}
	hostIs := "已按查询优先级核对，host is api2。"
	if got := preferPreNudgeAnswer(translation, hostIs); got != hostIs {
		t.Fatalf("host is api2 must stand: %s", got)
	}
	emptyIs := "已按查询优先级核对，host is."
	if got := preferPreNudgeAnswer(translation, emptyIs); got != translation {
		t.Fatalf("host is with no value must leave the translation in place: %s", got)
	}
	fullwidth := "已按查询优先级核对，主机是：api2。"
	if got := preferPreNudgeAnswer(translation, fullwidth); got != fullwidth {
		t.Fatalf("主机是：api2 must stand: %s", got)
	}
	notHost := "已按查询优先级核对，主机不是 api2。"
	if got := preferPreNudgeAnswer(translation, notHost); got != notHost {
		t.Fatalf("主机不是 api2 must stand: %s", got)
	}
	preface := "核对后仍用这份译文。\n" + translation + "\n\n已收到该查询优先级指令。上面的译文保持不变，不需要再查记忆或知识库，这一份就是要给用户看的正文。"
	if got := preferPreNudgeAnswer(translation, preface); got != preface {
		t.Fatalf("a follow-up that quotes the translation after a short preface must stand: %s", got)
	}
	forUse := "已按查询优先级核对过，论文里的服务器为翻译用。"
	if got := preferPreNudgeAnswer(translation, forUse); got != translation {
		t.Fatalf("服务器为翻译用 must leave the translation in place: %s", got)
	}
	clause := "已按查询优先级确认，主机是论文中的原词。"
	if got := preferPreNudgeAnswer(translation, clause); got != translation {
		t.Fatalf("主机是论文中的原词 must leave the translation in place: %s", got)
	}
	figure := "已按查询优先级核对，论文里的地址 1 保持原样。"
	if got := preferPreNudgeAnswer(translation, figure); got != translation {
		t.Fatalf("a figure number after 地址 must leave the translation in place: %s", got)
	}
	local := "已按查询优先级核对，主机是本地。"
	if got := preferPreNudgeAnswer(translation, local); got != local {
		t.Fatalf("主机是本地 must stand: %s", got)
	}
	accountName := "已按查询优先级核对，用户名为张三。"
	if got := preferPreNudgeAnswer(translation, accountName); got != accountName {
		t.Fatalf("用户名为张三 must stand: %s", got)
	}
	ip := "已按查询优先级核对，地址 10.0.0.1。"
	if got := preferPreNudgeAnswer(translation, ip); got != ip {
		t.Fatalf("地址 10.0.0.1 must stand: %s", got)
	}
	hostIP := "已按查询优先级核对，host 10.0.0.1。"
	if got := preferPreNudgeAnswer(translation, hostIP); got != hostIP {
		t.Fatalf("host 10.0.0.1 must stand: %s", got)
	}
	quotedLate := strings.Repeat("已收到该查询优先级指令，此处先复述规则。", 4) + shortDeliverable
	if got := preferPreNudgeAnswer(shortDeliverable, quotedLate); got != shortDeliverable {
		t.Fatalf("a finished sentence quoted after a long acknowledgement must stay: %s", got)
	}
	taskWord := "已按查询优先级核对过，论文里的服务器为翻译。"
	if got := preferPreNudgeAnswer(translation, taskWord); got != translation {
		t.Fatalf("服务器为翻译 must leave the translation in place: %s", got)
	}
	glossWord := "已按查询优先级确认，主机是原词。"
	if got := preferPreNudgeAnswer(translation, glossWord); got != translation {
		t.Fatalf("主机是原词 must leave the translation in place: %s", got)
	}
	needKeep := "已按查询优先级确认，主机是需要保留。"
	if got := preferPreNudgeAnswer(translation, needKeep); got != translation {
		t.Fatalf("主机是需要保留 must leave the translation in place: %s", got)
	}
	blankPassword := "已按查询优先级核对，密码是空。"
	if got := preferPreNudgeAnswer(translation, blankPassword); got != blankPassword {
		t.Fatalf("密码是空 must stand: %s", got)
	}
	hostLocal := "已按查询优先级核对，host is 本地。"
	if got := preferPreNudgeAnswer(translation, hostLocal); got != hostLocal {
		t.Fatalf("host is 本地 must stand: %s", got)
	}
}

func TestDropStoredFactNudgeSuffix(t *testing.T) {
	nudge := StoredOperationalFactNudge()
	history := []ConversationEntry{
		{Role: "assistant", Content: "## 译文\n检索增强生成。"},
		{Role: "user", Content: nudge},
		{Role: "assistant", Content: "已收到该查询优先级指令。"},
	}
	got := dropStoredFactNudgeSuffix(history)
	if len(got) != 1 || got[0].Content != history[0].Content {
		t.Fatalf("nudge suffix must be dropped, got %#v", got)
	}
	if len(dropStoredFactNudgeSuffix([]ConversationEntry{history[0]})) != 1 {
		t.Fatal("history without the nudge must stay")
	}
	withTool := []ConversationEntry{
		history[0],
		{Role: "user", Content: nudge},
		{Role: "tool", ToolName: "memory", Content: "none"},
		{Role: "assistant", Content: "已按查询优先级查过，没有记录。"},
	}
	if !historyHasToolAfterStoredFactNudge(withTool) {
		t.Fatal("a memory call after the nudge must be visible")
	}
	kept := dropStoredFactNudgeSuffix(withTool)
	if len(kept) != len(withTool) {
		t.Fatalf("tool results after the nudge must stay, got %d entries", len(kept))
	}
}

func TestIsPureSocialGreeting(t *testing.T) {
	for _, text := range []string{"你好呀", "你好！", "您好", "hello", "thanks", "谢谢", "在吗", "ok", "收到", "好的"} {
		if !IsPureSocialGreeting(text) {
			t.Fatalf("%q must be a pure social greeting", text)
		}
	}
	for _, text := range []string{"", "北京天气", "继续", "截图", "中国人，奇强", "你好，帮我连上那台机器", "帮我连上那台机器"} {
		if IsPureSocialGreeting(text) {
			t.Fatalf("%q must not be a pure social greeting", text)
		}
	}
	for _, text := range []string{"你好呀", "谢谢", "hello", "在吗"} {
		if !IsAnswerOnlySocialTurn(text) {
			t.Fatalf("%q must answer in place", text)
		}
	}
	for _, text := range []string{"好的", "好的呀", "好呀", "ok", "收到", "嗯", "北京天气", "继续"} {
		if IsAnswerOnlySocialTurn(text) {
			t.Fatalf("%q must stay on the open task", text)
		}
	}
}

func TestRetrievalToolsStillPending(t *testing.T) {
	tools := []map[string]interface{}{
		{"function": map[string]interface{}{"name": "knowledge_search"}},
		{"function": map[string]interface{}{"name": "memory"}},
		{"function": map[string]interface{}{"name": "web_search"}},
	}
	if !retrievalToolsStillPending(tools, nil) {
		t.Fatal("uncalled memory and knowledge_search are pending")
	}
	called := []ConversationEntry{{Role: "tool", ToolName: "memory"}, {Role: "tool", ToolName: "knowledge_search"}}
	if retrievalToolsStillPending(tools, called) {
		t.Fatal("both retrieval tools already ran")
	}
	onlyWeb := []map[string]interface{}{{"function": map[string]interface{}{"name": "web_search"}}}
	if retrievalToolsStillPending(onlyWeb, nil) {
		t.Fatal("no retrieval tool on the surface means nothing to force")
	}
}

func TestStoredOperationalFactNudgeOrdersMemoryThenKnowledge(t *testing.T) {
	nudge := StoredOperationalFactNudge()
	mem := strings.Index(nudge, "memory")
	kb := strings.Index(nudge, "knowledge_search")
	if mem < 0 || kb < 0 || mem > kb {
		t.Fatalf("nudge must name memory before knowledge_search: %s", nudge)
	}
	if !strings.Contains(nudge, "冲突") || !strings.Contains(nudge, "直接用") {
		t.Fatalf("nudge must use agreement and ask only on conflict: %s", nudge)
	}
}
