package guiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func officeResidueNeeds() []tool.CapabilityNeed {
	return []tool.CapabilityNeed{{
		ID:         "need:office",
		Capability: tool.CapabilityDocumentWriteOffice,
		Required:   true,
	}}
}

func openOfficeResidue() semanticSessionResidue {
	return semanticSessionResidue{
		Generation: 1,
		Status:     semanticResidueOpen,
		Needs:      officeResidueNeeds(),
		Summary:    "做一份项目周报",
	}
}

func TestSemanticSessionResidueFollowUpKeepsOffice(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	h.storeSemanticSessionResidue("desktop-user", openOfficeResidue())
	current := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	relation := decideSemanticResidueRelation(current, "继续", openOfficeResidue())
	if relation != semanticResidueContinue {
		t.Fatalf("继续 relation=%s", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, officeResidueNeeds(), relation)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) {
		t.Fatalf("继续 rewritten=%#v ok=%v", rewritten, ok)
	}
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.92, Reason: "bare search"}
	relation = decideSemanticResidueRelation(search, "再改一版", openOfficeResidue())
	if relation != semanticResidueUnclear {
		t.Fatalf("再改一版 relation=%s", relation)
	}
	rewritten, ok = semanticClassificationWithOpenResidue(search, officeResidueNeeds(), relation)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) || rewritten.HasLabel(intent.LabelSearch) {
		t.Fatalf("再改一版 rewritten=%#v ok=%v", rewritten, ok)
	}
	weather := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.95}
	if decideSemanticResidueRelation(weather, "然后北京天气怎么样", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("然后北京天气怎么样 pulled office tools into the weather question")
	}
	if decideSemanticResidueRelation(weather, "然后长沙天气怎么样", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("然后长沙天气怎么样 was treated as a document edit")
	}
	if decideSemanticResidueRelation(weather, "然后加拿大天气怎么样", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("然后加拿大天气怎么样 was treated as a document edit")
	}
	editAside := decideSemanticResidueRelation(weather, "然后改短一点", openOfficeResidue())
	if editAside != semanticResidueUnclear {
		t.Fatalf("然后改短一点 labeled search relation=%s", editAside)
	}
	if decideSemanticResidueRelation(weather, "然后长一点", openOfficeResidue()) != semanticResidueUnclear {
		t.Fatal("然后长一点 dropped the open document")
	}
	if decideSemanticResidueRelation(weather, "然后加上一段说明", openOfficeResidue()) != semanticResidueUnclear {
		t.Fatal("然后加上一段说明 dropped the open document")
	}
	again := decideSemanticResidueRelation(weather, "然后再改短一点", openOfficeResidue())
	if again != semanticResidueUnclear {
		t.Fatalf("然后再改短一点 relation=%s", again)
	}
	againRewrite, ok := semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), again)
	if !ok || !againRewrite.HasLabel(intent.LabelOffice) || againRewrite.HasLabel(intent.LabelSearch) {
		t.Fatalf("然后再改短一点 rewritten=%#v ok=%v", againRewrite, ok)
	}
	if decideSemanticResidueRelation(weather, "然后再北京天气怎么样", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("然后再北京天气怎么样 pulled office tools into the weather question")
	}
	sheet := decideSemanticResidueRelation(weather, "然后查一下这份表", openOfficeResidue())
	if sheet != semanticResidueUnclear {
		t.Fatalf("然后查一下这份表 relation=%s", sheet)
	}
	sheetRewrite, ok := semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), sheet)
	if !ok || !sheetRewrite.HasLabel(intent.LabelOffice) || sheetRewrite.HasLabel(intent.LabelSearch) {
		t.Fatalf("然后查一下这份表 rewritten=%#v ok=%v", sheetRewrite, ok)
	}
	if decideSemanticResidueRelation(weather, "然后查一下北京天气", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("然后查一下北京天气 kept the office tools")
	}
	about := decideSemanticResidueRelation(weather, "然后结论是什么", openOfficeResidue())
	if about != semanticResidueUnclear {
		t.Fatalf("然后结论是什么 relation=%s", about)
	}
	aboutRewrite, ok := semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), about)
	if !ok || !aboutRewrite.HasLabel(intent.LabelOffice) || aboutRewrite.HasLabel(intent.LabelSearch) {
		t.Fatalf("然后结论是什么 rewritten=%#v ok=%v", aboutRewrite, ok)
	}
	editRewrite, ok := semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), editAside)
	if !ok || !editRewrite.HasLabel(intent.LabelOffice) || editRewrite.HasLabel(intent.LabelSearch) {
		t.Fatalf("然后改短一点 rewritten=%#v ok=%v", editRewrite, ok)
	}
	lookup := decideSemanticResidueRelation(weather, "继续查一下天气", openOfficeResidue())
	if lookup != semanticResidueContinue {
		t.Fatalf("继续查一下天气 relation=%s", lookup)
	}
	rewritten, ok = semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), lookup)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) || !rewritten.HasLabel(intent.LabelSearch) {
		t.Fatalf("继续查一下天气 rewritten=%#v ok=%v", rewritten, ok)
	}
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || len(loaded.Needs) != 1 || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("stored residue=%#v ok=%v", loaded, ok)
	}
}

func TestShortShellFollowUpStaysOnOpenSSHSurface(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs:  []tool.CapabilityNeed{{ID: "need:ssh", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true}},
	}
	relation := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, "再执行一下", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("relation=%s", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, residue.Needs, relation)
	if !ok || !rewritten.HasLabel(intent.LabelSSH) || rewritten.HasLabel(intent.LabelShellCommand) {
		t.Fatalf("rewritten=%#v ok=%v", rewritten, ok)
	}
	local := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.93}, "改到本机执行", residue)
	if local != semanticResidueSwitch {
		t.Fatalf("local switch relation=%s", local)
	}
	withCue := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, "然后再执行一下", residue)
	if withCue != semanticResidueUnclear {
		t.Fatalf("follow-up cue relation=%s", withCue)
	}
	rewritten, ok = semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, residue.Needs, withCue)
	if !ok || !rewritten.HasLabel(intent.LabelSSH) || rewritten.HasLabel(intent.LabelShellCommand) {
		t.Fatalf("cued rewritten=%#v ok=%v", rewritten, ok)
	}
}

func TestShortDocumentEditStaysOnOpenOfficeSurface(t *testing.T) {
	residue := openOfficeResidue()
	current := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.91}
	relation := decideSemanticResidueRelation(current, "改短一点", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("relation=%s", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, residue.Needs, relation)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) || rewritten.HasLabel(intent.LabelFileWrite) {
		t.Fatalf("rewritten=%#v ok=%v", rewritten, ok)
	}
	prefixed := decideSemanticResidueRelation(current, "然后改短一点", residue)
	if prefixed != semanticResidueUnclear {
		t.Fatalf("prefixed edit relation=%s", prefixed)
	}
	long := decideSemanticResidueRelation(
		intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.93},
		"请根据当前表格另存一份完整的PDF报告，封面用今天的日期",
		residue,
	)
	if long != semanticResidueSwitch {
		t.Fatalf("long document request relation=%s", long)
	}
}

func TestBrowserClickStaysOnOpenBrowserSurface(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs:  []tool.CapabilityNeed{{ID: "need:browser", Capability: tool.CapabilityBrowserControlWeb, Required: true}},
	}
	current := intent.ClassificationResult{Primary: intent.LabelComputerUse, Confidence: 0.92}
	relation := decideSemanticResidueRelation(current, "点一下登录", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("relation=%s", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, residue.Needs, relation)
	if !ok || !rewritten.HasLabel(intent.LabelBrowser) || rewritten.HasLabel(intent.LabelComputerUse) {
		t.Fatalf("rewritten=%#v ok=%v", rewritten, ok)
	}
	switched := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.93}, "改成在服务器上执行", residue)
	if switched != semanticResidueSwitch {
		t.Fatalf("shell relation=%s", switched)
	}
}

func TestSemanticSessionResidueHighConfidenceMutatingSwitchLeavesOffice(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.9}
	relation := decideSemanticResidueRelation(current, "现在连上服务器跑一遍检查", openOfficeResidue())
	if relation != semanticResidueSwitch {
		t.Fatalf("relation=%s", relation)
	}
	if _, ok := semanticClassificationWithOpenResidue(current, officeResidueNeeds(), relation); ok {
		t.Fatal("switch must not inherit office")
	}
	cued := decideSemanticResidueRelation(current, "然后连上服务器跑一遍检查", openOfficeResidue())
	if cued != semanticResidueSwitch {
		t.Fatalf("cued switch relation=%s", cued)
	}
	if _, ok := semanticClassificationWithOpenResidue(current, officeResidueNeeds(), cued); ok {
		t.Fatal("cued switch must not inherit office")
	}
	bare := decideSemanticResidueRelation(current, "继续", openOfficeResidue())
	if bare != semanticResidueUnclear {
		t.Fatalf("bare continue relation=%s", bare)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, officeResidueNeeds(), bare)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) || rewritten.HasLabel(intent.LabelShellCommand) {
		t.Fatalf("bare continue rewritten=%#v ok=%v", rewritten, ok)
	}
	send := decideSemanticResidueRelation(current, "发给我", openOfficeResidue())
	if send != semanticResidueUnclear {
		t.Fatalf("发给我 relation=%s", send)
	}
	sendRewrite, ok := semanticClassificationWithOpenResidue(current, officeResidueNeeds(), send)
	if !ok || !sendRewrite.HasLabel(intent.LabelOffice) || sendRewrite.HasLabel(intent.LabelShellCommand) {
		t.Fatalf("发给我 rewritten=%#v ok=%v", sendRewrite, ok)
	}
}

func TestTaskContextMergeStaysOnTheOpenLookup(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true, Qualifiers: map[string]string{"freshness": "current"}},
			{ID: "need:render", Capability: "visual.render.live_data", Required: true},
			{ID: "need:deliver", Capability: "artifact.deliver.current_channel", Required: true, Qualifiers: map[string]string{"format": "image"}},
		},
	}
	current := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelSearch},
		Confidence: 0.85,
		Reason:     "tree-after-embedding: live_data (0.850); task-context merge",
	}
	relation := decideSemanticResidueRelation(current, "所以，多的脂肪去哪了？", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("relation=%s, want the open lookup kept", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, residue.Needs, relation)
	if !ok || !rewritten.HasLabel(intent.LabelLiveDataVisual) || rewritten.HasLabel(intent.LabelSearch) {
		t.Fatalf("rewritten=%+v ok=%v", rewritten, ok)
	}
	switched := intent.ClassificationResult{
		Primary: intent.LabelSSH, Confidence: 0.9,
		Reason: "tree-after-embedding: ssh (0.900); task-context merge",
	}
	if decideSemanticResidueRelation(switched, "然后连上服务器跑一遍检查", openOfficeResidue()) != semanticResidueSwitch {
		t.Fatal("a merged remote-shell request stayed on the open document")
	}
	named := intent.ClassificationResult{
		Primary: intent.LabelLiveDataVisual, Confidence: 0.9,
		Reason: "tree-after-embedding: live_data_visual (0.900); task-context merge",
	}
	if decideSemanticResidueRelation(named, "崇州天气", residue) != semanticResidueNone {
		t.Fatal("崇州天气 stayed on the previous card because the summary was merged")
	}
}

func TestNextCityWeatherDoesNotContinueAsScreenshot(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:render", Capability: "visual.render.live_data", Required: true},
			{ID: "need:deliver", Capability: "artifact.deliver.current_channel", Required: true, Qualifiers: map[string]string{"format": "image"}},
		},
		Remaining: map[string]int{"information.search.web": 0, "visual.render.live_data": 0},
	}
	current := intent.ClassificationResult{Primary: intent.LabelLiveDataVisual, Confidence: 0.93}
	if decideSemanticResidueRelation(current, "崇州天气", residue) != semanticResidueNone {
		t.Fatal("崇州天气 continued the spent Beijing weather grant")
	}
	if decideSemanticResidueRelation(current, "然后写成周报", residue) == semanticResidueNone {
		t.Fatal("然后写成周报 was treated as another city lookup")
	}
}

func TestSemanticSessionResidueReadOnlySideQuestionDoesNotInherit(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelCurrentTime, Confidence: 0.95}
	relation := decideSemanticResidueRelation(current, "现在几点", openOfficeResidue())
	if relation != semanticResidueNone {
		t.Fatalf("relation=%s", relation)
	}
}

func TestOpenTaskGreetingAnswersWithoutLegacyTools(t *testing.T) {
	residue := openOfficeResidue()
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}, "你好", residue) != semanticResidueNone {
		t.Fatal("a greeting must not inherit or replace the open task")
	}
	if isTaskAnchorGreetingText("现在几点") || isTaskAnchorGreetingText("你好，帮我改周报") || !isTaskAnchorGreetingText("你好") || !isTaskAnchorGreetingText("Hello!") || !isTaskAnchorGreetingText("你好啊") || !isTaskAnchorGreetingText("hi there") || !isTaskAnchorGreetingText("早上好") {
		t.Fatal("greeting detection drifted")
	}
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}, "你好啊", residue) != semanticResidueNone {
		t.Fatal("你好啊 must not inherit the open task")
	}
	ctx := &LoopContext{
		semanticTurnAnswerOnly: true,
		Runtime:                RuntimeContext{RoutingMissFallback: true},
	}
	if ctx.semanticSessionCeilingSpent {
		t.Fatal("a greeting must not spend the session ceiling")
	}
	if !loopContextBlocksLegacyToolRouter(ctx) {
		t.Fatal("greeting must block the legacy tool router")
	}
	miss := &LoopContext{Runtime: RuntimeContext{RoutingMissFallback: true}}
	if loopContextBlocksLegacyToolRouter(miss) {
		t.Fatal("a leftover miss without a greeting still uses the legacy router")
	}
	set := (&IMMessageHandler{}).prepareAgentLoopTools("desktop-user", "你好", ctx, agentLoopPhase{})
	if len(set.Tools) != 0 || len(set.BaseTools) != 0 {
		t.Fatalf("tools=%d base=%d", len(set.Tools), len(set.BaseTools))
	}
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, loopCtx: ctx}
	if cb.IsToolAllowed("bash") || cb.IsToolAllowedForPromptProfile("bash", agent.PromptProfileFull) {
		t.Fatal("greeting must deny every legacy tool name")
	}
	if allowed, reason := cb.IsToolCallAllowed("bash", `{}`); allowed || !strings.Contains(reason, "does not use tools") || strings.Contains(reason, "complete") {
		t.Fatalf("allowed=%v reason=%q", allowed, reason)
	}
	got := cb.ExecuteTool("bash", `{}`)
	if !strings.Contains(got, "does not use tools") || strings.Contains(got, "complete") {
		t.Fatalf("execute = %q", got)
	}
	called := cb.ExecuteToolCall("web_search", `{"query":"x"}`, "call-1")
	if called.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(called.Result, "does not use tools") {
		t.Fatalf("call = %#v", called)
	}
	if granted, message := cb.PetitionToolCall("ssh"); granted || !strings.Contains(message, "does not use tools") {
		t.Fatalf("petition granted=%v message=%q", granted, message)
	}
	h := &IMMessageHandler{}
	h.storeSemanticSessionResidue("desktop-user", residue)
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "你好"}, ctx, &IMAgentResponse{})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Summary != "做一份项目周报" || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("greeting replaced the open task: %#v ok=%v", loaded, ok)
	}
}

func TestPrepareIMLoopContextDropsTurnLocalResidue(t *testing.T) {
	h := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	provided.semanticTurnAnswerOnly = true
	provided.semanticSessionCeilingSpent = true
	provided.semanticResidueLookupFacts = true
	provided.semanticResidueLookupUsed = true
	provided.semanticResidueRemaining = map[string]int{string(tool.CapabilityDocumentWriteOffice): 1}
	provided.semanticResidueCandidateNeeds = officeResidueNeeds()
	provided.semanticResidueCandidateText = "做一份项目周报"
	provided.noteSemanticResidueUse(tool.CapabilityDocumentWriteOffice)
	got := h.prepareIMLoopContext(provided, IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "继续"}, nil, false, false)
	if got.semanticTurnAnswerOnly || got.semanticSessionCeilingSpent || got.semanticResidueLookupFacts || got.semanticResidueLookupUsed {
		t.Fatal("reused loop kept the previous turn's closure")
	}
	if len(got.semanticResidueRemaining) != 0 || len(got.semanticResidueCandidateNeeds) != 0 || got.semanticResidueCandidateText != "" {
		t.Fatalf("remaining=%v needs=%v text=%q", got.semanticResidueRemaining, got.semanticResidueCandidateNeeds, got.semanticResidueCandidateText)
	}
	used, lookupUsed := got.semanticResidueUsage()
	if len(used) != 0 || lookupUsed {
		t.Fatalf("used=%v lookup=%v", used, lookupUsed)
	}
	if !markOpenTaskAnswerOnly(got, true, "你好啊") || !got.semanticTurnAnswerOnly {
		t.Fatal("an open-task greeting must close only this turn")
	}
	if markOpenTaskAnswerOnly(got, false, "你好") || markOpenTaskAnswerOnly(got, true, "现在几点") {
		t.Fatal("a greeting without an open task, or a clock question, must stay open")
	}
}

func TestSocialAckDoesNotReopenOpenTaskTools(t *testing.T) {
	residue := openOfficeResidue()
	for _, text := range []string{"谢谢", "谢谢啊", "thanks", "再见"} {
		if !semanticSocialNoToolText(text) {
			t.Fatalf("%s should be a no-tool social turn", text)
		}
		if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}, text, residue) != semanticResidueNone {
			t.Fatalf("%s inherited the open task", text)
		}
	}
	if semanticSocialNoToolText("谢谢，帮我改周报") || semanticSocialNoToolText("好的") || semanticSocialNoToolText("继续") {
		t.Fatal("a task, a go-ahead, or a continuation was treated as thanks")
	}
	ctx := &LoopContext{}
	if !markOpenTaskAnswerOnly(ctx, true, "谢谢") || !ctx.semanticTurnAnswerOnly {
		t.Fatal("thanks while a task is open must answer without tools")
	}
}

func TestFollowUpCueReadOnlySideQuestionLeavesOfficeToolsOff(t *testing.T) {
	residue := openOfficeResidue()
	clock := intent.ClassificationResult{Primary: intent.LabelCurrentTime, Confidence: 0.95}
	if decideSemanticResidueRelation(clock, "然后现在几点", residue) != semanticResidueNone {
		t.Fatal("然后现在几点 pulled the open office tools")
	}
	if semanticFollowUpAllowsTaskMerge(&clock, "然后现在几点") {
		t.Fatal("a confident clock question was rewritten with the office summary")
	}
	edit := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.92}
	if semanticFollowUpAllowsTaskMerge(&edit, "再改一版") {
		t.Fatal("a confident search label must not be rewritten by the office summary")
	}
	if decideSemanticResidueRelation(edit, "再改一版", residue) != semanticResidueUnclear {
		t.Fatal("再改一版 must keep the office task without adding search")
	}
	shell := intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.93}
	if semanticFollowUpAllowsTaskMerge(&shell, "然后连上服务器跑一遍检查") {
		t.Fatal("a confident shell switch must not be rewritten back to office")
	}
	weather := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.95}
	if semanticFollowUpAllowsTaskMerge(&weather, "然后北京天气怎么样") {
		t.Fatal("a weather aside must not be rewritten by the office summary")
	}
	weak := intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}
	if !semanticFollowUpAllowsTaskMerge(&weak, "继续") {
		t.Fatal("a weak follow-up must still merge the open task")
	}
	h := &IMMessageHandler{}
	h.storeSemanticSessionResidue("desktop-user", residue)
	searchCtx := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{{
		ID: "need:search", Capability: "information.search.web", Required: true,
	}}}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "然后再查一下最新天气"}, searchCtx, &IMAgentResponse{})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("read-only follow-up closed office: %#v ok=%v", loaded, ok)
	}
}

func TestSemanticSessionResidueIsDesktopSessionScoped(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	h.storeSemanticSessionResidue("desktop-user:proj-a", openOfficeResidue())
	if _, ok := h.loadOpenSemanticSessionResidue("desktop-user:proj-b"); ok {
		t.Fatal("another desktop session saw the residue")
	}
	if _, ok := semanticResidueSessionKey(IMUserMessage{UserID: "desktop-user", Platform: "lansenger"}); ok {
		t.Fatal("group channel must not own a desktop residue key")
	}
	h.clearPerUserSessionState("desktop-user:proj-a")
	if _, ok := h.loadOpenSemanticSessionResidue("desktop-user:proj-a"); ok {
		t.Fatal("session reset kept the residue")
	}
}

func TestSemanticSessionResidueSettleCompletesReadOnlyAndKeepsOffice(t *testing.T) {
	h := &IMMessageHandler{}
	officeCtx := &LoopContext{semanticResidueCandidateNeeds: officeResidueNeeds(), semanticResidueCandidateText: "做一份项目周报"}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "做一份项目周报"}, officeCtx, &IMAgentResponse{})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Summary != "做一份项目周报" {
		t.Fatalf("office residue=%#v ok=%v", loaded, ok)
	}

	searchCtx := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{{
		ID: "need:search", Capability: "information.search.web", Required: true,
	}}, semanticResidueCandidateText: "查一下航班"}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "查一下航班"}, searchCtx, &IMAgentResponse{})
	if loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user"); !ok || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("side question closed office residue=%#v ok=%v", loaded, ok)
	}

	fresh := &IMMessageHandler{}
	fresh.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "查一下航班"}, searchCtx, &IMAgentResponse{})
	if _, ok := fresh.loadOpenSemanticSessionResidue("desktop-user"); ok {
		t.Fatal("read-only success stayed open")
	}
	settled, ok := fresh.loadSemanticSessionResidue("desktop-user")
	if !ok || settled.Status != semanticResidueCompleted {
		t.Fatalf("settled=%#v ok=%v", settled, ok)
	}
}

func TestSemanticSessionResidueErrorDoesNotReplaceOpenTask(t *testing.T) {
	h := &IMMessageHandler{}
	h.storeSemanticSessionResidue("desktop-user", openOfficeResidue())
	ctx := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{{
		ID: "need:shell", Capability: tool.CapabilityShellExecuteLocal, Required: true,
	}}}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "跑一下"}, ctx, &IMAgentResponse{Error: "semantic_capability_unmet"})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("error replaced residue=%#v ok=%v", loaded, ok)
	}
}

func TestStoredLookupFactsServeGenerateButNotNewLookup(t *testing.T) {
	generate := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}
	if !semanticReuseStoredLookupFacts("生成pdf报告", generate) {
		t.Fatal("a later generate should reuse the stored lookup")
	}
	if !semanticReuseStoredLookupFacts("再出一版", intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}) {
		t.Fatal("short follow-up should reuse the stored lookup")
	}
	fresh := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.93}
	if semanticReuseStoredLookupFacts("上海天气", fresh) {
		t.Fatal("a new high-confidence lookup must search again")
	}
	if semanticReuseStoredLookupFacts("再查一下最新天气", generate) {
		t.Fatal("explicit refresh must search again")
	}
}

func TestSemanticSessionResidueReloadsFromConversationMemory(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	first := &IMMessageHandler{memory: mem}
	loop := &LoopContext{semanticResidueCandidateNeeds: officeResidueNeeds(), semanticResidueCandidateText: "做一份项目周报"}
	first.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "做一份项目周报"}, loop, &IMAgentResponse{})

	second := &IMMessageHandler{memory: mem}
	residue, open, factsOnly := second.loadDesktopTurnResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "继续"}, false, nil)
	if !open || factsOnly || residue.Summary != "做一份项目周报" || len(residue.Needs) != 1 {
		t.Fatalf("reloaded residue=%#v open=%v factsOnly=%v", residue, open, factsOnly)
	}
}

func TestCompletedLookupFactsFollowWithoutReopeningTools(t *testing.T) {
	h := &IMMessageHandler{}
	loop := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{{
		ID: "need:search", Capability: "information.search.web", Required: true,
	}}, semanticResidueCandidateText: "北京天气"}
	loop.noteSemanticResidueUse("information.search.web")
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "北京天气"}, loop, &IMAgentResponse{})

	residue, open, factsOnly := h.loadDesktopTurnResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "再出一版"}, false, nil)
	if open || !factsOnly || !residue.LookupFacts {
		t.Fatalf("residue=%#v open=%v factsOnly=%v", residue, open, factsOnly)
	}
	if _, stillOpen := h.loadOpenSemanticSessionResidue("desktop-user"); stillOpen {
		t.Fatal("completed search must not stay an open tool task")
	}
	refresh, _, refreshFacts := h.loadDesktopTurnResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "再查一下最新天气"}, false, nil)
	if !refreshFacts || !refresh.LookupFacts {
		t.Fatal("refresh wording still has the stored fact; the caller must decline to reuse it")
	}
	if lexicalFreshLookupRequest("再查一下最新天气") != true {
		t.Fatal("再查 must stay a fresh-lookup request")
	}
}

func TestFailedAttemptSpendsSessionInvocation(t *testing.T) {
	loop := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "office", Capability: tool.CapabilityDocumentWriteOffice, Required: true},
		{ID: "office#02", Capability: tool.CapabilityDocumentWriteOffice},
	}}
	loop.noteSemanticResidueUse(tool.CapabilityDocumentWriteOffice)
	loop.noteSemanticResidueUse(tool.CapabilityDocumentWriteOffice)
	h := &IMMessageHandler{}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "做周报"}, loop, &IMAgentResponse{})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Remaining[string(tool.CapabilityDocumentWriteOffice)] != 0 {
		t.Fatalf("remaining=%v ok=%v", loaded.Remaining, ok)
	}
}

func TestSessionCeilingDeniesLegacyToolExecution(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{},
		loopCtx: &LoopContext{semanticSessionCeilingSpent: true},
	}
	if cb.IsToolAllowed("bash") || cb.IsToolAllowed("web_search") {
		t.Fatal("spent ceiling must deny every legacy tool name")
	}
	got := cb.ExecuteTool("bash", `{}`)
	if !strings.Contains(got, "complete") {
		t.Fatalf("execute = %q", got)
	}
	called := cb.ExecuteToolCall("web_search", `{"query":"x"}`, "call-1")
	if called.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(called.Result, "complete") {
		t.Fatalf("call = %#v", called)
	}
}

func TestSpentSessionCeilingStaysClosed(t *testing.T) {
	h := registerDocumentGenerateAndSearch(t)
	ctx := withSemanticResidueRemaining(context.Background(), map[string]int{"information.search.web": 0})
	prepared, handled, err := h.semanticPlanForTurnWithContextAndClassificationAndAttachments(
		ctx, "user", "再看一眼", "desktop", "root-ceiling", "turn-ceiling",
		&intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: .98}, nil,
	)
	if !handled || prepared != nil || !errors.Is(err, errSemanticSessionCeilingSpent) {
		t.Fatalf("handled=%v prepared=%v err=%v", handled, prepared != nil, err)
	}
	if !semanticPlanErrorBlocksSession(err) {
		t.Fatal("spent ceiling must not reopen the legacy tool catalog")
	}
}

func TestDropUnusedSessionCompanionsKeepsUsedWrite(t *testing.T) {
	needs := []tool.CapabilityNeed{
		{ID: "need:document.write.office:aaa", Capability: tool.CapabilityDocumentWriteOffice, Required: true},
		{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
		{ID: "need:zz-baseline:fs.write.local:bbb#02", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
		{ID: "need:artifact.acquire.remote:ccc", Capability: tool.CapabilityArtifactAcquireRemote, EvidenceIDs: []string{"intent:archetype_bundle"}},
	}
	unused := dropUnusedSessionCompanions(needs, map[string]int{
		string(tool.CapabilityDocumentWriteOffice):   1,
		string(tool.CapabilityFSWriteLocal):          2,
		string(tool.CapabilityArtifactAcquireRemote): 1,
	})
	if len(unused) != 1 || unused[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("unused companions stayed: %#v", unused)
	}
	used := dropUnusedSessionCompanions(needs, map[string]int{
		string(tool.CapabilityFSWriteLocal): 1,
	})
	foundWrite := false
	for _, need := range used {
		if need.Capability == tool.CapabilityFSWriteLocal {
			foundWrite = true
		}
	}
	if !foundWrite {
		t.Fatalf("a used write companion was dropped: %#v", used)
	}
}

func TestClampNeedsToResidueRemainingDropsSpentFamily(t *testing.T) {
	needs := []tool.CapabilityNeed{
		{ID: "office", Capability: tool.CapabilityDocumentWriteOffice},
		{ID: "office#02", Capability: tool.CapabilityDocumentWriteOffice},
		{ID: "office#03", Capability: tool.CapabilityDocumentWriteOffice},
		{ID: "read", Capability: tool.CapabilityFSReadLocal},
	}
	got := clampNeedsToResidueRemaining(needs, map[string]int{string(tool.CapabilityDocumentWriteOffice): 1})
	if len(got) != 2 || got[0].ID != "office" || got[1].ID != "read" {
		t.Fatalf("clamped=%#v", got)
	}
	none := clampNeedsToResidueRemaining(needs, map[string]int{string(tool.CapabilityDocumentWriteOffice): 0})
	if len(none) != 1 || none[0].ID != "read" {
		t.Fatalf("zero remaining=%#v", none)
	}
}

func TestSemanticSessionResidueFollowUpKeepsOriginalSummary(t *testing.T) {
	if got := semanticResidueSummary("继续", "做一份项目周报"); got != "做一份项目周报" {
		t.Fatalf("summary=%q", got)
	}
}
