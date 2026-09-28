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
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.9}, "把重庆天气发给我", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("把重庆天气发给我 pulled the open report into the weather delivery")
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

func TestContinueMoreImagesDropsSpentDownloadCeiling(t *testing.T) {
	in := map[string]int{
		string(tool.CapabilityArtifactAcquireRemote): 0,
		"document.generate.file":                     0,
	}
	got := semanticResidueRemainingForFollowUp(in, "继续补图")
	if _, ok := got[string(tool.CapabilityArtifactAcquireRemote)]; ok {
		t.Fatal("继续补图 kept the spent download ceiling")
	}
	if got["document.generate.file"] != 0 {
		t.Fatal("继续补图 cleared the generate ceiling")
	}
	for _, text := range []string{"继续下载剩下的", "再下几张"} {
		renewed := semanticResidueRemainingForFollowUp(in, text)
		if _, ok := renewed[string(tool.CapabilityArtifactAcquireRemote)]; ok {
			t.Fatalf("%s kept the spent download ceiling", text)
		}
	}
	for _, text := range []string{"改短一点", "继续下周的报告", "再下结论", "不要下载了"} {
		kept := semanticResidueRemainingForFollowUp(in, text)
		if kept[string(tool.CapabilityArtifactAcquireRemote)] != 0 {
			t.Fatalf("%s renewed the download ceiling", text)
		}
	}
}

func TestPPTToPDFDoesNotReloadOpenDownloadGrant(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:dl", Capability: "artifact.acquire.remote", Required: true},
			{ID: "need:office", Capability: "document.write.office", Required: true},
		},
	}
	current := intent.ClassificationResult{
		Primary:    intent.LabelOffice,
		Secondary:  []intent.IntentLabel{intent.LabelWebFetch},
		Confidence: 0.88,
		Reason:     "tree-after-embedding+synthesized composite: office(0.880)+web_fetch(0.600)",
	}
	if decideSemanticResidueRelation(current, "将ppt生成pdf文档", residue) != semanticResidueNone {
		t.Fatal("将ppt生成pdf文档 was replaced by the open download grant")
	}
	officeOnly := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.88}
	if decideSemanticResidueRelation(officeOnly, "将ppt生成pdf文档", residue) != semanticResidueNone {
		t.Fatal("a short ppt-to-pdf request was treated as an edit of the open deck")
	}
	if decideSemanticResidueRelation(officeOnly, "把幻灯片导成可打印文档", residue) != semanticResidueNone {
		t.Fatal("a paraphrased conversion was treated as an edit of the open deck")
	}
	if decideSemanticResidueRelation(officeOnly, "写一份新的ppt", residue) != semanticResidueNone {
		t.Fatal("写一份新的ppt stayed on the open grant because it contains 写")
	}
	if decideSemanticResidueRelation(officeOnly, "把ppt改成pdf", residue) != semanticResidueNone {
		t.Fatal("把ppt改成pdf stayed on the open grant because it contains 改")
	}
	if decideSemanticResidueRelation(officeOnly, "把标题改成红色", residue) != semanticResidueUnclear {
		t.Fatal("把标题改成红色 left the open deck")
	}
	if decideSemanticResidueRelation(officeOnly, "做研发我司的ppt", residue) != semanticResidueNone {
		t.Fatal("做研发我司的ppt stayed on the open grant because 研发我 contains 发我")
	}
	if decideSemanticResidueRelation(officeOnly, "发给我", residue) != semanticResidueUnclear {
		t.Fatal("发给我 left the open deck")
	}
	if decideSemanticResidueRelation(officeOnly, "把ppt发给老板", residue) != semanticResidueUnclear {
		t.Fatal("把ppt发给老板 left the open deck")
	}
	if decideSemanticResidueRelation(officeOnly, "做一份发给客户的ppt", residue) != semanticResidueNone {
		t.Fatal("做一份发给客户的ppt stayed on the open grant")
	}
}

func TestNextCityWeatherPDFDoesNotContinueSpentGenerate(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
			{ID: "need:deliver", Capability: "artifact.deliver.current_channel", Required: true},
		},
		Remaining:   map[string]int{"information.search.web": 0, "document.generate.file": 0},
		LookupFacts: true,
	}
	current := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.839,
		Reason:     "embedding declared composite: top=live_data (0.839), companion=document_generate (0.765)",
	}
	if decideSemanticResidueRelation(current, "重庆天气，生成格式化pdf", residue) != semanticResidueNone {
		t.Fatal("重庆天气，生成格式化pdf continued the spent Chongzhou PDF grant")
	}
	if decideSemanticResidueRelation(current, "然后重庆天气，生成格式化pdf", residue) != semanticResidueNone {
		t.Fatal("然后重庆天气，生成格式化pdf spent the previous city's PDF grant")
	}
	if decideSemanticResidueRelation(current, "然后把重庆天气发给我", residue) != semanticResidueNone {
		t.Fatal("然后把重庆天气发给我 was treated as an edit of the previous PDF")
	}
	lookupOnly := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.90}
	if decideSemanticResidueRelation(lookupOnly, "然后把重庆天气发给我", residue) != semanticResidueNone {
		t.Fatal("a lookup-only resend of Chongqing weather kept the spent grant")
	}
	modest := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.70}
	if decideSemanticResidueRelation(modest, "然后把重庆天气发给我", residue) != semanticResidueNone {
		t.Fatal("a modest lookup resend kept the spent weather grant")
	}
	asPDF := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: 0.9}
	if decideSemanticResidueRelation(asPDF, "把重庆天气发给我", residue) != semanticResidueNone {
		t.Fatal("a PDF-labeled resend of Chongqing weather kept the spent grant")
	}
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}, "生成pdf报告", residue) == semanticResidueNone {
		t.Fatal("生成pdf报告 was treated as a new city lookup")
	}
}

func TestStandaloneRequestDoesNotSpendOpenGrant(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
		},
		Remaining:   map[string]int{"information.search.web": 0, "document.generate.file": 0},
		LookupFacts: true,
	}
	current := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.70,
	}
	if decideSemanticResidueRelation(current, "整理杭州今天的情况，做成pdf", residue) != semanticResidueNone {
		t.Fatal("a new managed request spent the open PDF grant")
	}
	edit := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.80}
	if decideSemanticResidueRelation(edit, "改短一点", residue) != semanticResidueUnclear {
		t.Fatal("a short edit of the open delivery opened a new grant")
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
	chongqing := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.839,
	}
	if semanticReuseStoredLookupFacts("重庆天气，生成格式化pdf", chongqing) {
		t.Fatal("a new city PDF reused the previous city's lookup facts")
	}
	if semanticReuseStoredLookupFacts("再查一下最新天气", generate) {
		t.Fatal("explicit refresh must search again")
	}
	deck := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.90}
	if semanticReuseStoredLookupFacts("生成纪念小布生日PPT，网上搜索一张布偶照片", deck) {
		t.Fatal("a new deck must not reuse another topic's lookup facts")
	}
}

func TestOfficeDeckDoesNotContinueGenerateResidue(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
			{ID: "need:search", Capability: "information.search.web", Required: true},
		},
		LookupFacts: true,
		Summary:     "画出近一月股价趋势图",
	}
	current := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.90, Layer: 3}
	text := "生成纪念小布（布偶 猫）５岁生日的ｐｐｔ，网上搜索一张漂亮 布偶 照片作为它的照片。"
	if relation := decideSemanticResidueRelation(current, text, residue); relation != semanticResidueSwitch {
		t.Fatalf("relation=%s, want switch off the previous generate residue", relation)
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

func TestSpentKnowledgeWaveDoesNotSwallowANewRequest(t *testing.T) {
	residue := semanticSessionResidue{
		Status:  semanticResidueOpen,
		Needs:   []tool.CapabilityNeed{{ID: "need:ingest", Capability: tool.CapabilityKnowledgeIngestLocal, Required: true}},
		Summary: "将agnes视频生成模型信息保存到知识库",
		Remaining: map[string]int{
			string(tool.CapabilityKnowledgeIngestLocal): 0,
		},
	}
	video := "使用agnes ai的视频生成模型，生成一段猫和老鼠游戏的视频。"
	bare := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.80}
	if !semanticSpentWaveRelease(bare, residue, video) {
		t.Fatal("a new video request stayed on the spent knowledge wave")
	}
	if decideSemanticResidueRelation(bare, video, residue) != semanticResidueNone {
		t.Fatal("relation inherited the spent knowledge wave")
	}
	merged := intent.ClassificationResult{
		Primary: intent.LabelKnowledgeWrite, Confidence: 0.65,
		Reason: "tree-after-embedding: knowledge_write (0.650); task-context merge",
	}
	if decideSemanticResidueRelation(merged, video, residue) != semanticResidueNone {
		t.Fatal("a merged knowledge_write label kept the zero ceiling")
	}
	if semanticSpentWaveRelease(bare, residue, "可爱风") {
		t.Fatal("a short reply left the open task")
	}
	h := &IMMessageHandler{}
	h.storeSemanticSessionResidue("desktop-user", residue)
	h.completeSpentSemanticSessionResidue("desktop-user", residue)
	if _, open := h.loadOpenSemanticSessionResidue("desktop-user"); open {
		t.Fatal("a released wave stayed open")
	}
}

func TestSpentWaveVideoRequestPlansShellNotCoding(t *testing.T) {
	history := []agent.ConversationEntry{{
		Role:    "assistant",
		Content: "下次可以用 bash/curl 调 https://api.example.com/v1。",
	}}
	video := "使用agnes ai的视频生成模型，生成一段猫和老鼠游戏的视频。"
	coding := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.80}
	if !semanticReleasedRequestPlansShell(coding, video, history) {
		t.Fatal("a video request on a finished wave stayed on coding")
	}
	edit := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.80}
	if semanticReleasedRequestPlansShell(edit, "改一下这个函数", history) {
		t.Fatal("a source edit was planned as shell")
	}
	sure := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.90}
	if semanticReleasedRequestPlansShell(sure, video, history) {
		t.Fatal("a confident coding verdict was replaced")
	}
	if semanticReleasedRequestPlansShell(coding, video, nil) {
		t.Fatal("a video request with no endpoint history was planned as shell")
	}
	knowledge := intent.ClassificationResult{Primary: intent.LabelKnowledgeWrite, Confidence: 0.65}
	if !semanticReleasedRequestPlansShell(knowledge, video, history) {
		t.Fatal("a knowledge label kept a video request that needs a remote call")
	}
	save := "把agnes视频生成模型信息保存到知识库"
	if semanticReleasedRequestPlansShell(knowledge, save, history) {
		t.Fatal("a knowledge save was planned as shell")
	}
	onlyBash := []agent.ConversationEntry{{Role: "assistant", Content: "下次可以用 bash 看一下日志。"}}
	if semanticReleasedRequestPlansShell(coding, video, onlyBash) {
		t.Fatal("a bash mention without a call target planned shell")
	}
	withURL := "用 https://api.example.com/v1 生成一段视频"
	if !semanticReleasedRequestPlansShell(coding, withURL, nil) {
		t.Fatal("a video request that already names the endpoint stayed on coding")
	}
}

func TestShortConsentOnSpentWavePlansShellNotKnowledge(t *testing.T) {
	blocked := []agent.ConversationEntry{{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽，我无法再执行 bash/curl。请输入 `/new` 开启新对话。",
	}}
	if !semanticConsentLeavesSpentWave("要", blocked) || !semanticConsentLeavesSpentWave("允许 使用", blocked) {
		t.Fatal("a yes after a blocked shell stayed on the spent wave")
	}
	shell := semanticConsentShellClassification("要", blocked)
	if shell == nil || shell.Primary != intent.LabelShellCommand {
		t.Fatalf("consent plan=%v", shell)
	}
	if semanticConsentLeavesSpentWave("可爱风", blocked) {
		t.Fatal("a short answer was treated as shell consent")
	}
	revision := []agent.ConversationEntry{{Role: "assistant", Content: "要不要再改一版周报？"}}
	if semanticConsentLeavesSpentWave("要", revision) || semanticConsentShellClassification("要", revision) != nil {
		t.Fatal("要 after a revision question left the document")
	}
	if assistantBlockedOnShell("不要把密码写进 bash，不允许在命令里带密钥。") {
		t.Fatal("a bash warning was treated as a blocked shell")
	}
	if !assistantBlockedOnShell("请允许我使用 bash 工具，我就可以直接调用。") {
		t.Fatal("an explicit allow-bash ask was ignored")
	}
}

func TestAssistantQuotaClaimIsNotShownAgain(t *testing.T) {
	replaced, ok := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽，无法再执行 bash。请输入 `/new` 开启新对话。",
	})
	if !ok {
		t.Fatal("quota claim was left in the next prompt")
	}
	text := replaced.Content.(string)
	if strings.Contains(text, "已耗尽") || strings.Contains(text, "/new") || !strings.Contains(text, "Do not repeat") {
		t.Fatalf("rewritten=%q", text)
	}
	if _, claimed := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: "这个接口的配额用完后会返回 429。",
	}); claimed {
		t.Fatal("an API quota note was rewritten")
	}
	if _, claimed := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: "当前工具调用配额已用完，无法再请求视频接口。",
	}); !claimed {
		t.Fatal("a tool-call quota claim without /new was left in the next prompt")
	}
	quoted, ok := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: `The tools_search result says "Planned invocations for this session are complete", so this chat cannot call tools.`,
	})
	if !ok || strings.Contains(quoted.Content.(string), "Planned invocations for this session are complete") {
		t.Fatal("an assistant quote of the closed plan was left in the next prompt")
	}
	if _, claimed := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: "API endpoint is https://api.agnes-ai.cn/v1. Model: agnes-video-2.5-flash.",
	}); claimed {
		t.Fatal("the saved API facts were rewritten")
	}
	withReasoning, ok := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:             "assistant",
		Content:          "我来继续。",
		ReasoningContent: `The tool result says "Planned invocations for this session are complete". Tell the user to open a new chat.`,
	})
	if !ok || withReasoning.ReasoningContent != "" {
		t.Fatal("closed-plan reasoning was sent again")
	}
	message, _ := withReasoning.ToMessage().(map[string]interface{})
	reasoning, _ := message["reasoning_content"].(string)
	if strings.Contains(reasoning, "Planned invocations") || strings.Contains(reasoning, "new chat") {
		t.Fatalf("reasoning_content=%q", reasoning)
	}
	prompt := sessionCeilingTurnPrompt()
	if !strings.Contains(prompt, "no tools") || !strings.Contains(prompt, "same chat") || strings.Contains(prompt, "/new") || strings.Contains(prompt, "quota") {
		t.Fatalf("ceiling prompt=%q", prompt)
	}
}

func TestShortYesOnSpentWaveDoesNotCloseTools(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs:  []tool.CapabilityNeed{{ID: "need:ingest", Capability: tool.CapabilityKnowledgeIngestLocal, Required: true}},
		Remaining: map[string]int{
			string(tool.CapabilityKnowledgeIngestLocal): 0,
		},
	}
	merged := intent.ClassificationResult{
		Primary: intent.LabelKnowledgeWrite, Confidence: 0.65,
		Reason: "tree-after-embedding: knowledge_write (0.650); task-context merge",
	}
	for _, text := range []string{"要", "允许 使用"} {
		if semanticSpentWaveRelease(merged, residue, text) {
			t.Fatalf("%q released the open task", text)
		}
		if decideSemanticResidueRelation(merged, text, residue) != semanticResidueUnclear {
			t.Fatalf("%q left the open task", text)
		}
		remaining := semanticResidueRemainingForFollowUp(residue.Remaining, text)
		if semanticResidueWaveSpent(residue) && !semanticFollowUpRenewsDownloads(text) {
			remaining = semanticResidueDropSpentCounts(remaining)
		}
		if len(remaining) != 0 {
			t.Fatalf("%q kept a zero ceiling: %v", text, remaining)
		}
	}
	replaced, ok := neutralizeHistoricalSessionCeiling(agent.ConversationEntry{
		Role:    "tool",
		Content: "[system] Planned invocations for this session are complete. Answer from the results you already have. Do not call tools.",
	})
	if !ok || strings.Contains(replaced.Content.(string), "Planned invocations for this session are complete") || strings.Contains(replaced.Content.(string), "/new") || !strings.Contains(replaced.Content.(string), "does not apply to this turn") {
		t.Fatalf("historical ceiling=%q ok=%v", replaced.Content, ok)
	}
	note := shortConsentContinuesHere("要", []agent.ConversationEntry{{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽。请输入 `/new` 开启新对话。",
	}})
	if !strings.Contains(note, "不是同意开启新对话") || !strings.Contains(note, "不要让用户另开对话") || strings.Contains(note, "/new") {
		t.Fatalf("consent note=%q", note)
	}
}

func TestSpentOfficeRevisionRenewsInsteadOfClosing(t *testing.T) {
	residue := openOfficeResidue()
	residue.Remaining = map[string]int{string(tool.CapabilityDocumentWriteOffice): 0}
	current := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.92}
	if semanticSpentWaveRelease(current, residue, "再改一版") {
		t.Fatal("再改一版 released a spent office wave")
	}
	if decideSemanticResidueRelation(current, "再改一版", residue) != semanticResidueUnclear {
		t.Fatal("再改一版 left the open document")
	}
	if renewed := semanticResidueDropSpentCounts(residue.Remaining); len(renewed) != 0 {
		t.Fatalf("renewed ceiling=%v", renewed)
	}
	kept := semanticResidueRemainingForFollowUp(map[string]int{
		string(tool.CapabilityArtifactAcquireRemote): 0,
		"document.generate.file":                     0,
	}, "继续补图")
	if _, ok := kept[string(tool.CapabilityArtifactAcquireRemote)]; ok || kept["document.generate.file"] != 0 {
		t.Fatalf("继续补图 remaining=%v", kept)
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
	if !strings.Contains(got, "did not run") || !strings.Contains(got, "same chat") || strings.Contains(got, "quota") {
		t.Fatalf("execute = %q", got)
	}
	called := cb.ExecuteToolCall("web_search", `{"query":"x"}`, "call-1")
	if called.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(called.Result, "did not run") {
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
