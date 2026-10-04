package guiapp

import (
	"context"
	"errors"
	"path/filepath"
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
	if decideSemanticResidueRelation(search, "revise it", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("a search label inherited the open document")
	}
	weather := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.95}
	if decideSemanticResidueRelation(weather, "Beijing weather", openOfficeResidue()) != semanticResidueNone {
		t.Fatal("a search label pulled office tools into the question")
	}
	office := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.92}
	if decideSemanticResidueRelation(office, "write a new deck", openOfficeResidue()) != semanticResidueUnclear {
		t.Fatal("an office label left the open document")
	}
	lookup := decideSemanticResidueRelation(weather, "check the weather", openOfficeResidue())
	if lookup != semanticResidueNone {
		t.Fatalf("evidence on an office residue relation=%s", lookup)
	}
	if _, ok := semanticClassificationWithOpenResidue(weather, officeResidueNeeds(), lookup); ok {
		t.Fatal("evidence on an office residue inherited the document")
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
	switched := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, "run it again", residue)
	if switched != semanticResidueSwitch {
		t.Fatalf("local label on a remote residue relation=%s", switched)
	}
	local := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.93}, "改到本机执行", residue)
	if local != semanticResidueSwitch {
		t.Fatalf("local switch relation=%s", local)
	}
	same := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.92}, "run it again", residue)
	if same != semanticResidueUnclear {
		t.Fatalf("matching remote label relation=%s", same)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.92}, residue.Needs, same)
	if !ok || !rewritten.HasLabel(intent.LabelSSH) || rewritten.HasLabel(intent.LabelShellCommand) {
		t.Fatalf("rewritten=%#v ok=%v", rewritten, ok)
	}
	kept := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "run it again", residue)
	if kept != semanticResidueContinue && kept != semanticResidueUnclear {
		t.Fatalf("continuation relation=%s", kept)
	}
	withBaseline := residue
	withBaseline.Needs = append(append([]tool.CapabilityNeed(nil), residue.Needs...),
		tool.CapabilityNeed{ID: "need:zz-baseline:shell.execute.local:bbb", Capability: tool.CapabilityShellExecuteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
	)
	held := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.92}, "run it again", withBaseline)
	if held != semanticResidueUnclear {
		t.Fatalf("baseline local shell cancelled the remote obligation, relation=%s", held)
	}
	left := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelShellCommand, Confidence: 0.92}, "改到本机执行", withBaseline)
	if left != semanticResidueSwitch {
		t.Fatalf("baseline local shell held a local command on the remote task, relation=%s", left)
	}
}

func TestShortDocumentEditStaysOnOpenOfficeSurface(t *testing.T) {
	residue := openOfficeResidue()
	current := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.91}
	relation := decideSemanticResidueRelation(current, "make it shorter", residue)
	if relation != semanticResidueSwitch {
		t.Fatalf("file-write on an office residue relation=%s", relation)
	}
	prefixed := decideSemanticResidueRelation(current, "and make it shorter", residue)
	if prefixed != semanticResidueSwitch {
		t.Fatalf("prefixed file-write relation=%s", prefixed)
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

func TestArchetypeSearchDoesNotMakeTheOfficeALookup(t *testing.T) {
	residue := openOfficeResidue()
	residue.Needs = append(residue.Needs,
		tool.CapabilityNeed{ID: "need:search:arch", Capability: "information.search.web", EvidenceIDs: []string{"intent:archetype_bundle"}},
		tool.CapabilityNeed{ID: "need:artifact.acquire.remote:ccc", Capability: tool.CapabilityArtifactAcquireRemote, EvidenceIDs: []string{"intent:archetype_bundle"}},
		tool.CapabilityNeed{ID: "need:zz-baseline:shell.execute.local:bbb", Capability: tool.CapabilityShellExecuteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
	)
	long := "请根据当前表格另存一份完整的PDF报告，封面用今天的日期"
	if got := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.93}, long, residue); got != semanticResidueSwitch {
		t.Fatalf("archetype search held a new PDF on the office task, relation=%s", got)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, residue.Needs, semanticResidueContinue)
	if !ok || rewritten.Primary != intent.LabelOffice {
		t.Fatalf("office rewrite=%#v ok=%v", rewritten, ok)
	}
	if rewritten.HasLabel(intent.LabelFileDownload) || rewritten.HasLabel(intent.LabelShellCommand) || rewritten.HasLabel(intent.LabelSearch) {
		t.Fatalf("companion labels joined the office task: %#v", rewritten)
	}
}

func TestLookupCompanionDoesNotReopenAcquire(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
			{ID: "need:artifact.acquire.remote:ccc", Capability: tool.CapabilityArtifactAcquireRemote, EvidenceIDs: []string{"intent:archetype_bundle"}},
			{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
		},
		Remaining: map[string]int{
			"information.search.web":                     4,
			"document.generate.file":                     0,
			string(tool.CapabilityArtifactAcquireRemote): 0,
			string(tool.CapabilityFSWriteLocal):          0,
		},
		LookupFacts: true,
	}
	report := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}
	if got := decideSemanticResidueRelation(report, "生成pdf报告", residue); got != semanticResidueContinue {
		t.Fatalf("生成pdf报告 relation=%s", got)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(report, residue.Needs, semanticResidueContinue)
	if !ok || rewritten.Primary != intent.LabelDocumentGenerate || rewritten.HasLabel(intent.LabelFileDownload) || rewritten.HasLabel(intent.LabelFileWrite) {
		t.Fatalf("companion joined the lookup: %#v ok=%v", rewritten, ok)
	}
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if remaining[string(tool.CapabilityArtifactAcquireRemote)] != 0 || remaining[string(tool.CapabilityFSWriteLocal)] != 0 {
		t.Fatalf("companion reopened a ceiling: %v", remaining)
	}
	if _, spent := remaining["document.generate.file"]; spent {
		t.Fatalf("generate stayed closed: %v", remaining)
	}
	continued, ok := semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, residue.Needs, semanticResidueUnclear)
	if !ok || continued.Primary != intent.LabelSearch || continued.HasLabel(intent.LabelFileDownload) {
		t.Fatalf("lookup continuation=%#v ok=%v", continued, ok)
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
	if bare != semanticResidueSwitch {
		t.Fatalf("shell label on continue relation=%s", bare)
	}
	stay := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, "继续", openOfficeResidue())
	if stay != semanticResidueContinue && stay != semanticResidueUnclear {
		t.Fatalf("continuation relation=%s", stay)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, officeResidueNeeds(), stay)
	if !ok || !rewritten.HasLabel(intent.LabelOffice) {
		t.Fatalf("continuation rewritten=%#v ok=%v", rewritten, ok)
	}
	send := decideSemanticResidueRelation(current, "发给我", openOfficeResidue())
	if send != semanticResidueSwitch {
		t.Fatalf("shell label on handoff relation=%s", send)
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
	relation := decideSemanticResidueRelationWithBare(current, &intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.3}, "所以，多的脂肪去哪了？", residue)
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
	bareLookup := intent.ClassificationResult{Primary: intent.LabelLiveDataVisual, Confidence: 0.9}
	if decideSemanticResidueRelationWithBare(named, &bareLookup, "Chongzhou weather", residue) != semanticResidueNone {
		t.Fatal("a lookup the bare classification already had stayed on the previous card")
	}
}

func TestOpenTaskRestateDoesNotAbandonSpentShell(t *testing.T) {
	residue := semanticSessionResidue{
		Status:  semanticResidueOpen,
		Summary: "更新api2服务器上的omniroute，保存原始配置",
		Needs: []tool.CapabilityNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                        1,
			string(tool.CapabilityShellExecuteRemoteHost): 0,
		},
	}
	restated := intent.ClassificationResult{
		Primary:    intent.LabelFileWrite,
		Confidence: 0.92,
		Reason:     "tree-after-embedding: file_write (0.920); task-context merge; open-task restate",
	}
	if got := decideSemanticResidueRelation(restated, "已经解封", residue); got != semanticResidueUnclear {
		t.Fatalf("restate relation=%s", got)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(restated, residue.Needs, semanticResidueUnclear)
	if !ok || rewritten.Primary != intent.LabelSSH {
		t.Fatalf("restate dropped the shell obligation: %#v ok=%v", rewritten, ok)
	}
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, closed := remaining[string(tool.CapabilityShellExecuteRemoteHost)]; closed {
		t.Fatalf("restate left the shell ceiling closed: %v", remaining)
	}
	fresh := restated
	fresh.Reason = "tree-after-embedding: file_write (0.920); task-context merge"
	if got := decideSemanticResidueRelation(fresh, "已经解封", residue); got != semanticResidueNone {
		t.Fatalf("a file task the sentence's merge introduced stayed, relation=%s", got)
	}
	local := intent.ClassificationResult{
		Primary:    intent.LabelShellCommand,
		Confidence: 0.93,
		Reason:     "task-context merge; open-task restate",
	}
	open := residue
	open.Remaining = map[string]int{
		"knowledge.read.local":                        1,
		string(tool.CapabilityShellExecuteRemoteHost): 1,
	}
	if got := decideSemanticResidueRelation(local, "已经解封", open); got != semanticResidueUnclear {
		t.Fatalf("a restated local shell changed the remote surface, relation=%s", got)
	}
	long := "使用agnes ai的视频生成模型，生成一段猫和老鼠游戏的视频。"
	longRestate := intent.ClassificationResult{
		Primary:    intent.LabelKnowledgeWrite,
		Confidence: 0.90,
		Reason:     "task-context merge; open-task restate",
	}
	spentKnowledge := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs:  []tool.CapabilityNeed{{ID: "need:ingest", Capability: tool.CapabilityKnowledgeIngestLocal, Required: true}},
		Remaining: map[string]int{
			string(tool.CapabilityKnowledgeIngestLocal): 0,
		},
	}
	if got := decideSemanticResidueRelation(longRestate, long, spentKnowledge); got != semanticResidueNone {
		t.Fatalf("a long restated request stayed on the spent wave, relation=%s", got)
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
	if decideSemanticResidueRelation(current, "write this up as a report", residue) != semanticResidueNone {
		t.Fatal("a visual label stayed on the open card because of the wording")
	}
}

func TestContinueMoreImagesDropsSpentDownloadCeiling(t *testing.T) {
	in := map[string]int{
		string(tool.CapabilityArtifactAcquireRemote): 0,
		"document.generate.file":                     0,
	}
	download := intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: 0.9}
	got := semanticResidueRemainingForFollowUp(in, download)
	if _, ok := got[string(tool.CapabilityArtifactAcquireRemote)]; ok {
		t.Fatal("a download label kept the spent download ceiling")
	}
	if got["document.generate.file"] != 0 {
		t.Fatal("a download label cleared the generate ceiling")
	}
	office := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.9}
	kept := semanticResidueRemainingForFollowUp(in, office)
	if kept[string(tool.CapabilityArtifactAcquireRemote)] != 0 {
		t.Fatal("继续补图 reopened the download ceiling without a download label")
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
	for _, text := range []string{"make a pdf from the deck", "write a new deck", "turn the deck into a pdf", "send it to me"} {
		if got := decideSemanticResidueRelation(officeOnly, text, residue); got != semanticResidueUnclear {
			t.Fatalf("%s relation=%s", text, got)
		}
	}
}

func TestBareDocumentDeliveryDoesNotReplayOpenGenerate(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
			{ID: "need:deliver", Capability: "artifact.deliver.current_channel", Required: true, Qualifiers: map[string]string{"format": "file"}},
		},
		Remaining: map[string]int{"information.search.web": 0, "document.generate.file": 0, "artifact.deliver.current_channel": 0},
	}
	bare := intent.ClassificationResult{Primary: intent.LabelDocumentDelivery, Confidence: 0.70, Layer: 3, Reason: "tree document_delivery"}
	restored, ok := semanticOpenResidueDelivery(bare, residue)
	if !ok || restored.Primary != intent.LabelDocumentDelivery || restored.HasLabel(intent.LabelDocumentGenerate) || restored.HasLabel(intent.LabelLiveData) {
		t.Fatalf("restored=%#v ok=%v", restored, ok)
	}
	if !strings.Contains(restored.Reason, "session residue delivery") {
		t.Fatalf("reason=%q", restored.Reason)
	}
	if semanticContinuesOpenDocument(restored, residue) {
		t.Fatal("delivery continued the open render")
	}
	merged := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.82, Reason: "tree; task-context merge"}
	relation := decideSemanticResidueRelationWithBare(merged, &bare, "send the file", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("merged relation=%s", relation)
	}
	rewritten, applied := semanticClassificationWithOpenResidue(merged, residue.Needs, relation)
	if !applied || !rewritten.HasLabel(intent.LabelDocumentGenerate) || rewritten.Primary == intent.LabelDocumentDelivery {
		t.Fatalf("unclear rewrite=%#v applied=%v", rewritten, applied)
	}
	renewed := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, clamped := renewed["document.generate.file"]; clamped {
		t.Fatalf("replaying generate left the PDF ceiling clamped: %v", renewed)
	}
	city := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate}, Confidence: 0.84,
	}
	if _, ok := semanticOpenResidueDelivery(city, residue); ok {
		t.Fatal("a new lookup document was kept as delivery of the previous file")
	}
	report := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}
	if _, ok := semanticOpenResidueDelivery(report, residue); ok {
		t.Fatal("a continued render was treated as delivery")
	}
	if got := decideSemanticResidueRelation(report, "生成pdf报告", residue); got != semanticResidueContinue {
		t.Fatalf("生成pdf报告 relation=%s", got)
	}
	attachment := intent.ClassificationResult{Primary: intent.LabelAttachmentDelivery, Confidence: 0.70, Layer: 3}
	if _, ok := semanticOpenResidueDelivery(attachment, residue); !ok {
		t.Fatal("attachment delivery of a produced document was not kept")
	}
	empty := residue
	empty.Needs = []tool.CapabilityNeed{{ID: "need:search", Capability: "information.search.web", Required: true}}
	if _, ok := semanticOpenResidueDelivery(bare, empty); ok {
		t.Fatal("delivery was restored when the task had not produced a document")
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
	if decideSemanticResidueRelation(current, "send me Chongqing weather as a pdf", residue) != semanticResidueNone {
		t.Fatal("a lookup plus a document continued the spent PDF grant")
	}
	lookupOnly := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.90}
	if got := decideSemanticResidueRelation(lookupOnly, "然后把重庆天气发给我", residue); got != semanticResidueContinue {
		t.Fatalf("lookup-only follow-up relation=%s", got)
	}
	modest := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.70}
	if got := decideSemanticResidueRelation(modest, "Lanzhou weather", residue); got != semanticResidueContinue {
		t.Fatalf("evidence-only relation=%s", got)
	}
	asPDF := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Secondary: []intent.IntentLabel{intent.LabelLiveData}, Confidence: 0.9}
	if decideSemanticResidueRelation(asPDF, "把重庆天气发给我", residue) != semanticResidueNone {
		t.Fatal("a PDF-labeled resend of Chongqing weather kept the spent grant")
	}
	if got := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}, "生成pdf报告", residue); got != semanticResidueContinue {
		t.Fatalf("生成pdf报告 relation=%s", got)
	}
	report := intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}
	rewritten, ok := semanticClassificationWithOpenResidue(report, residue.Needs, semanticResidueContinue)
	if !ok || rewritten.Primary != intent.LabelDocumentGenerate {
		t.Fatalf("生成pdf报告 rewritten=%#v ok=%v", rewritten, ok)
	}
	partial := residue
	partial.Remaining = map[string]int{
		"information.search.web":                     4,
		"document.generate.file":                     0,
		"artifact.deliver.current_channel":           0,
		string(tool.CapabilityArtifactAcquireRemote): 0,
		string(tool.CapabilityFSWriteLocal):          0,
	}
	remaining := semanticResidueRemainingForOpenTurn(partial, rewritten)
	if _, spent := remaining["document.generate.file"]; spent {
		t.Fatalf("partial search ceiling clamped the open PDF: %v", remaining)
	}
	if remaining["information.search.web"] != 4 {
		t.Fatalf("search allowance changed: %v", remaining)
	}
	if remaining[string(tool.CapabilityArtifactAcquireRemote)] != 0 || remaining[string(tool.CapabilityFSWriteLocal)] != 0 {
		t.Fatalf("render reopened an unrelated ceiling: %v", remaining)
	}
	if got := decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.92}, "generate the pdf report", residue); got != semanticResidueContinue {
		t.Fatalf("document render relation=%s", got)
	}
}

func TestContinueNamedLookupKeepsOpenDeliverable(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:search", Capability: "information.search.web", Required: true},
			{ID: "need:gen", Capability: "document.generate.file", Required: true},
			{ID: "need:deliver", Capability: "artifact.deliver.current_channel", Required: true, Qualifiers: map[string]string{"format": "file"}},
		},
		Summary:     "成都天气生成pdf",
		LookupFacts: true,
		Remaining:   map[string]int{"information.search.web": 0, "document.generate.file": 0, "artifact.deliver.current_channel": 0},
	}
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.90}
	relation := decideSemanticResidueRelation(current, "继续完成 兰州天气", residue)
	if relation != semanticResidueContinue {
		t.Fatalf("继续完成 兰州天气 relation=%s", relation)
	}
	if got := decideSemanticResidueRelation(current, "Lanzhou weather", residue); got != semanticResidueContinue {
		t.Fatalf("Lanzhou weather relation=%s", got)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, residue.Needs, relation)
	if !ok || rewritten.Primary != intent.LabelLiveData || !rewritten.HasLabel(intent.LabelDocumentGenerate) {
		t.Fatalf("rewritten=%#v ok=%v", rewritten, ok)
	}
	if semanticReuseStoredLookupFacts("继续完成 兰州天气", current) {
		t.Fatal("the open deliverable reused the previous city's facts")
	}
	if semanticReuseStoredLookupFacts("继续完成 兰州天气", rewritten) {
		t.Fatal("the continued lookup reused facts after the residue rewrite")
	}
	residue.Needs = append(residue.Needs, tool.CapabilityNeed{
		ID:          "need:zz-baseline:fs.write.local:extra",
		Capability:  tool.CapabilityFSWriteLocal,
		EvidenceIDs: []string{"intent:baseline_workspace"},
	})
	residue.Remaining[string(tool.CapabilityArtifactAcquireRemote)] = 0
	residue.Remaining[string(tool.CapabilityFSWriteLocal)] = 0
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, ok := remaining["information.search.web"]; ok {
		t.Fatalf("spent search ceiling still closes the new subject: %v", remaining)
	}
	if _, ok := remaining["document.generate.file"]; ok {
		t.Fatalf("spent generate ceiling still closes the open deliverable: %v", remaining)
	}
	if _, ok := remaining["artifact.deliver.current_channel"]; ok {
		t.Fatalf("spent deliver ceiling still closes the open deliverable: %v", remaining)
	}
	if remaining[string(tool.CapabilityArtifactAcquireRemote)] != 0 {
		t.Fatalf("unrelated download ceiling was reopened: %v", remaining)
	}
	if remaining[string(tool.CapabilityFSWriteLocal)] != 0 {
		t.Fatalf("stale file-write ceiling was reopened: %v", remaining)
	}
	kept := decideSemanticResidueRelation(current, "继续查一下天气", residue)
	if kept != semanticResidueContinue {
		t.Fatalf("继续查一下天气 relation=%s", kept)
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
	if decideSemanticResidueRelation(edit, "make it shorter", residue) != semanticResidueNone {
		t.Fatal("a file-write label stayed on the open PDF")
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
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}, "你好", residue) != semanticResidueContinue {
		t.Fatal("an unknown label continues the open task")
	}
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}, "你好啊", residue) != semanticResidueContinue {
		t.Fatal("the same unknown label continues, whatever the greeting")
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
}

func TestSocialAckDoesNotReopenOpenTaskTools(t *testing.T) {
	residue := openOfficeResidue()
	unknown := intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.4}
	for _, text := range []string{"谢谢", "thanks", "再见", "你好，帮我改周报"} {
		if decideSemanticResidueRelation(unknown, text, residue) != semanticResidueContinue {
			t.Fatalf("%s with an unknown label left the open task", text)
		}
	}
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.92}, "谢谢", residue) != semanticResidueUnclear {
		t.Fatal("an office label left the open document")
	}
}

func TestFollowUpCueReadOnlySideQuestionLeavesOfficeToolsOff(t *testing.T) {
	residue := openOfficeResidue()
	clock := intent.ClassificationResult{Primary: intent.LabelCurrentTime, Confidence: 0.95}
	if decideSemanticResidueRelation(clock, "hello", residue) != semanticResidueNone {
		t.Fatal("a clock label inherited the open office tools")
	}
	if semanticFollowUpAllowsTaskMerge(&clock, "然后现在几点") {
		t.Fatal("a confident clock question was rewritten with the office summary")
	}
	edit := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.92}
	if semanticFollowUpAllowsTaskMerge(&edit, "再改一版") {
		t.Fatal("a confident search label must not be rewritten by the office summary")
	}
	if decideSemanticResidueRelation(edit, "再改一版", residue) != semanticResidueNone {
		t.Fatal("a search label stayed on the office task")
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

func TestGrantedArchetypeCompanionStaysACompanion(t *testing.T) {
	plan := tool.ToolPlan{Selections: []tool.PlannedSelection{
		{ID: "selection:need:office", NeedID: "need:document.write.office:aaa", FitProof: tool.FitProof{MatchedCapability: tool.CapabilityDocumentWriteOffice}},
		{ID: "selection:need:acquire", NeedID: "need:artifact.acquire.remote:ccc", EvidenceIDs: []string{"intent:archetype_bundle"}, FitProof: tool.FitProof{MatchedCapability: tool.CapabilityArtifactAcquireRemote}},
		{ID: "selection:need:search", NeedID: "need:information.search.web:ddd", EvidenceIDs: []string{"intent:archetype_bundle"}, FitProof: tool.FitProof{MatchedCapability: "information.search.web"}},
	}}
	needs := semanticResidueWithoutAmbient(withoutLookupCarryNeeds(grantedNeedsFromPlan(plan)))
	obligation := semanticResidueObligationNeeds(needs)
	if len(obligation) != 1 || obligation[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("archetype companions became the obligation: %#v", obligation)
	}
	if semanticResidueIsLookupVisual(needs) {
		t.Fatal("archetype search made the office plan a lookup")
	}
	reloaded := semanticResidueFromPersisted(semanticResidueToPersisted(semanticSessionResidue{Status: semanticResidueOpen, Needs: needs}))
	if semanticResidueIsLookupVisual(reloaded.Needs) || len(semanticResidueObligationNeeds(reloaded.Needs)) != 1 {
		t.Fatalf("restart lost the companion marker: %#v", reloaded.Needs)
	}
}

func TestBaselineCompanionDoesNotReplaceOpenTask(t *testing.T) {
	h := &IMMessageHandler{}
	h.storeSemanticSessionResidue("desktop-user", openOfficeResidue())
	side := &LoopContext{semanticResidueCandidateNeeds: []tool.CapabilityNeed{
		{ID: "need:search", Capability: "information.search.web", Required: true},
		{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
		{ID: "need:zz-baseline:shell.execute.local:ccc", Capability: tool.CapabilityShellExecuteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
	}, semanticResidueCandidateText: "现在几点"}
	h.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "现在几点"}, side, &IMAgentResponse{})
	loaded, ok := h.loadOpenSemanticSessionResidue("desktop-user")
	if !ok || loaded.Summary != "做一份项目周报" || loaded.Needs[0].Capability != tool.CapabilityDocumentWriteOffice {
		t.Fatalf("baseline companion replaced the open task: %#v ok=%v", loaded, ok)
	}
	fresh := &IMMessageHandler{}
	fresh.settleSemanticSessionResidue(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "现在几点"}, side, &IMAgentResponse{})
	if _, open := fresh.loadOpenSemanticSessionResidue("desktop-user"); open {
		t.Fatal("a baseline companion stayed open")
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
	if semanticReuseStoredLookupFacts("继续完成 兰州天气", fresh) {
		t.Fatal("a follow-up lookup reused the previous subject's facts")
	}
	chongqing := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.839,
	}
	if semanticReuseStoredLookupFacts("重庆天气，生成格式化pdf", chongqing) {
		t.Fatal("a new city PDF reused the previous city's lookup facts")
	}
	if semanticReuseStoredLookupFacts("再查一下最新天气", generate) != true {
		t.Fatal("a generate label reused or dropped facts based on refresh wording")
	}
	if semanticReuseStoredLookupFacts("latest weather", fresh) {
		t.Fatal("a lookup label reused stored facts")
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
	if semanticReuseStoredLookupFacts("再查一下最新天气", intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.93}) {
		t.Fatal("a lookup label reused stored facts because the wording asked to refresh")
	}
}

func TestReadCompanionDoesNotCloseSpentRemoteShell(t *testing.T) {
	residue := semanticSessionResidue{
		Status:  semanticResidueOpen,
		Summary: "更新api2服务器上的omniroute，保存原始配置",
		Needs: []tool.CapabilityNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
			{ID: "need:~ambient:memory.recall.agent", Capability: tool.CapabilityMemoryRecallAgent, Required: true},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                        1,
			string(tool.CapabilityShellExecuteRemoteHost): 0,
			string(tool.CapabilityMemoryRecallAgent):      1,
		},
	}
	if !semanticResidueWaveSpent(residue) {
		t.Fatal("unspent knowledge and memory kept a spent remote shell wave open")
	}
	merged := intent.ClassificationResult{
		Primary:    intent.LabelSSH,
		Confidence: 0.97,
		Reason:     "tree-after-embedding: ssh (0.970); task-context merge",
	}
	if decideSemanticResidueRelation(merged, "已经解封", residue) != semanticResidueUnclear {
		t.Fatal("a remote-shell continuation left the open server task")
	}
	rewritten, ok := semanticClassificationWithOpenResidue(merged, residue.Needs, semanticResidueUnclear)
	if !ok || rewritten.Primary != intent.LabelSSH || !rewritten.HasLabel(intent.LabelKnowledgeRead) {
		t.Fatalf("rewrite lost the shell obligation: %#v ok=%v", rewritten, ok)
	}
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, closed := remaining[string(tool.CapabilityShellExecuteRemoteHost)]; closed {
		t.Fatalf("spent shell stayed closed beside the unused knowledge read: %v", remaining)
	}
	if remaining["knowledge.read.local"] != 1 {
		t.Fatalf("knowledge grant changed: %v", remaining)
	}
	reloaded := semanticResidueFromPersisted(agent.SemanticSessionResidue{
		Status:  string(semanticResidueOpen),
		Summary: residue.Summary,
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(tool.CapabilityShellExecuteRemoteHost), Required: true},
			{ID: "need:~ambient:memory.recall.agent", Capability: string(tool.CapabilityMemoryRecallAgent), Required: true},
		},
		Remaining: residue.Remaining,
	})
	if len(reloaded.Needs) != 2 {
		t.Fatalf("reloaded needs=%#v", reloaded.Needs)
	}
	for _, need := range reloaded.Needs {
		if semanticResidueAmbientNeed(need) {
			t.Fatalf("reloaded needs kept ambient retrieval: %#v", reloaded.Needs)
		}
	}
	if _, kept := reloaded.Remaining[string(tool.CapabilityMemoryRecallAgent)]; kept {
		t.Fatalf("ambient counter survived reload: %v", reloaded.Remaining)
	}
	video := "使用agnes ai的视频生成模型，生成一段猫和老鼠游戏的视频。"
	bare := intent.ClassificationResult{Primary: intent.LabelCoding, Confidence: 0.80}
	if !semanticSpentWaveRelease(bare, residue, video) {
		t.Fatal("a new video request stayed on the spent shell because knowledge was unused")
	}
	if semanticSpentWaveRelease(merged, residue, "已经解封") {
		t.Fatal("the shell continuation itself was released")
	}
}

func TestOpenShellObligationRestoresAfterProcessRestart(t *testing.T) {
	const userID = `desktop-user:C:\Users\ma139\.maclaw\data\tasks\保存原始配置`
	store := filepath.Join(t.TempDir(), "conversation.json")
	mem := agent.NewPersistentConversationMemory(store)
	mem.SetSemanticSessionResidue(userID, agent.SemanticSessionResidue{
		Generation: 3,
		Status:     string(semanticResidueOpen),
		Summary:    "更新api2服务器上的omniroute 到官方 3.8.51版本，保存原始配置",
		Needs: []agent.SemanticSessionResidueNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: string(tool.CapabilityShellExecuteRemoteHost), Required: true},
			{ID: "need:~ambient:memory.recall.agent", Capability: string(tool.CapabilityMemoryRecallAgent), Required: true},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                        1,
			string(tool.CapabilityShellExecuteRemoteHost): 0,
			string(tool.CapabilityMemoryRecallAgent):      1,
		},
	})
	mem.Stop()

	reloaded := agent.NewPersistentConversationMemory(store)
	defer reloaded.Stop()
	h := &IMMessageHandler{memory: reloaded}
	residue, open, factsOnly := h.loadDesktopTurnResidue(IMUserMessage{
		UserID: userID, Platform: "desktop", Text: "已经解封",
	}, false, nil)
	if !open || factsOnly {
		t.Fatalf("restart residue open=%v factsOnly=%v residue=%#v", open, factsOnly, residue)
	}
	if residue.Summary == "" || len(residue.Needs) != 2 {
		t.Fatalf("restart dropped the obligation: %#v", residue)
	}
	for _, need := range residue.Needs {
		if semanticResidueAmbientNeed(need) {
			t.Fatalf("restart kept ambient retrieval: %#v", residue.Needs)
		}
	}
	merged := intent.ClassificationResult{
		Primary:    intent.LabelSSH,
		Confidence: 0.97,
		Reason:     "tree-after-embedding: ssh (0.970); task-context merge",
	}
	relation := decideSemanticResidueRelation(merged, "已经解封", residue)
	if relation != semanticResidueUnclear {
		t.Fatalf("restart relation=%s", relation)
	}
	rewritten, ok := semanticClassificationWithOpenResidue(merged, residue.Needs, relation)
	if !ok || rewritten.Primary != intent.LabelSSH || !rewritten.HasLabel(intent.LabelKnowledgeRead) {
		t.Fatalf("restart rewrite=%#v ok=%v", rewritten, ok)
	}
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, closed := remaining[string(tool.CapabilityShellExecuteRemoteHost)]; closed {
		t.Fatalf("restart left the shell ceiling closed: %v", remaining)
	}
	if remaining["knowledge.read.local"] != 1 {
		t.Fatalf("restart knowledge grant=%v", remaining)
	}
	if _, kept := remaining[string(tool.CapabilityMemoryRecallAgent)]; kept {
		t.Fatalf("restart kept the ambient ceiling: %v", remaining)
	}
}

func TestBaselineWriteDoesNotHoldAnotherFileTaskOnTheShell(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                        1,
			string(tool.CapabilityFSWriteLocal):           0,
			string(tool.CapabilityShellExecuteRemoteHost): 1,
		},
	}
	file := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.92}
	if semanticSpentWaveRelease(file, residue, "把笔记存成本地文件") {
		t.Fatal("an in-progress shell released for a different file task")
	}
	if got := decideSemanticResidueRelation(file, "把笔记存成本地文件", residue); got != semanticResidueSwitch {
		t.Fatalf("baseline write held the file task on the shell, relation=%s", got)
	}
	residue.Remaining[string(tool.CapabilityShellExecuteRemoteHost)] = 0
	if !semanticSpentWaveRelease(file, residue, "把笔记存成本地文件") {
		t.Fatal("a spent shell kept a different file task")
	}
	if got := decideSemanticResidueRelation(file, "把笔记存成本地文件", residue); got != semanticResidueNone {
		t.Fatalf("spent shell relation=%s", got)
	}
	own := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:fs.write.local:real", Capability: tool.CapabilityFSWriteLocal, Required: true},
		},
		Remaining: map[string]int{string(tool.CapabilityFSWriteLocal): 0},
	}
	if got := decideSemanticResidueRelation(file, "把笔记存成本地文件", own); got != semanticResidueUnclear {
		t.Fatalf("a real file write left its own task, relation=%s", got)
	}
}

func TestCompanionDoesNotLeadOrReopenSpentShell(t *testing.T) {
	residue := semanticSessionResidue{
		Status:  semanticResidueOpen,
		Summary: "更新api2服务器上的omniroute，保存原始配置",
		Needs: []tool.CapabilityNeed{
			{ID: "need:knowledge.read.local:20dd5f458c29", Capability: "knowledge.read.local", Required: true},
			{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
			{ID: "need:shell.execute.remote_host:a08acf449f05", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
			{ID: "need:artifact.acquire.remote:ccc", Capability: tool.CapabilityArtifactAcquireRemote, EvidenceIDs: []string{"intent:archetype_bundle"}},
		},
		Remaining: map[string]int{
			"knowledge.read.local":                        1,
			string(tool.CapabilityFSWriteLocal):           0,
			string(tool.CapabilityShellExecuteRemoteHost): 0,
			string(tool.CapabilityArtifactAcquireRemote):  0,
		},
	}
	if !semanticResidueWaveSpent(residue) {
		t.Fatal("a baseline write and an archetype download kept the spent shell closed")
	}
	merged := intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.97, Reason: "task-context merge"}
	rewritten, ok := semanticClassificationWithOpenResidue(merged, residue.Needs, semanticResidueUnclear)
	if !ok || rewritten.Primary != intent.LabelSSH {
		t.Fatalf("companion led the restored task: %#v ok=%v", rewritten, ok)
	}
	if !rewritten.HasLabel(intent.LabelKnowledgeRead) {
		t.Fatalf("knowledge companion dropped: %#v", rewritten)
	}
	remaining := semanticResidueRemainingForOpenTurn(residue, rewritten)
	if _, closed := remaining[string(tool.CapabilityShellExecuteRemoteHost)]; closed {
		t.Fatalf("shell stayed closed: %v", remaining)
	}
	if left, ok := remaining[string(tool.CapabilityFSWriteLocal)]; !ok || left != 0 {
		t.Fatalf("baseline write ceiling = %v", remaining)
	}
	if left, ok := remaining[string(tool.CapabilityArtifactAcquireRemote)]; !ok || left != 0 {
		t.Fatalf("archetype download ceiling = %v", remaining)
	}
	if remaining["knowledge.read.local"] != 1 {
		t.Fatalf("knowledge grant=%v", remaining)
	}
	reloaded := semanticResidueFromPersisted(semanticResidueToPersisted(residue))
	sawCompanion := false
	for _, need := range reloaded.Needs {
		if need.Capability == tool.CapabilityArtifactAcquireRemote && semanticPlanCompanionNeed(need) {
			sawCompanion = true
		}
	}
	if !sawCompanion {
		t.Fatal("restart dropped the archetype companion marker")
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
	if !semanticReleasedRequestPlansShell(edit, "改一下这个函数", history) {
		t.Fatal("wording kept a weak coding label off shell when history already has an endpoint")
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
	if !semanticReleasedRequestPlansShell(knowledge, save, history) {
		t.Fatal("wording kept a knowledge label off shell when history already has an endpoint")
	}
	if semanticReleasedRequestPlansShell(knowledge, save, nil) {
		t.Fatal("a knowledge label with no endpoint was planned as shell")
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
	yes := intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}
	if semanticConsentLeavesSpentWave(yes, blocked, false) {
		t.Fatal("assistant wording was treated as a closed plan")
	}
	shell := semanticConsentShellClassification(yes, blocked, true)
	if shell == nil || shell.Primary != intent.LabelShellCommand {
		t.Fatalf("consent plan=%v", shell)
	}
	answer := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.8}
	if semanticConsentLeavesSpentWave(answer, blocked, true) {
		t.Fatal("a content label was treated as shell consent")
	}
	revision := []agent.ConversationEntry{{Role: "assistant", Content: "要不要再改一版周报？"}}
	if semanticConsentLeavesSpentWave(yes, revision, false) || semanticConsentShellClassification(yes, revision, false) != nil {
		t.Fatal("a continuation without a closed plan left the document")
	}
	if assistantBlockedOnShell("不要把密码写进 bash，不允许在命令里带密钥。") {
		t.Fatal("a bash warning was treated as a blocked shell")
	}
	if assistantBlockedOnShell("请允许我使用 bash 工具，我就可以直接调用。") {
		t.Fatal("a permission sentence was treated as a blocked shell")
	}
}

func TestAssistantQuotaClaimIsNotShownAgain(t *testing.T) {
	if _, claimed := neutralizeAssistantQuotaClaim(agent.ConversationEntry{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽，无法再执行 bash。请输入 `/new` 开启新对话。",
	}); claimed {
		t.Fatal("assistant wording was rewritten without the host ceiling sentence")
	}
	closed := rewriteClosedPlanAssistant(agent.ConversationEntry{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽，无法再执行 bash。请输入 `/new` 开启新对话。",
	})
	text := closed.Content.(string)
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
	}); claimed {
		t.Fatal("a tool-call sentence was rewritten without the host ceiling sentence")
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
		remaining := semanticResidueRemainingForOpenTurn(residue, merged)
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
	blocked := []agent.ConversationEntry{{
		Role:    "assistant",
		Content: "当前对话的工具调用额度已耗尽。请输入 `/new` 开启新对话。",
	}}
	if shortConsentContinuesHere(&intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, blocked, false) != "" {
		t.Fatal("quota wording injected a consent note")
	}
	note := shortConsentContinuesHere(&intent.ClassificationResult{Primary: intent.LabelContinuation, Confidence: 0.9}, blocked, true)
	if !strings.Contains(note, "不是同意开启新对话") || !strings.Contains(note, "不要让用户另开对话") || strings.Contains(note, "/new") {
		t.Fatalf("consent note=%q", note)
	}
	if shortConsentContinuesHere(&intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.9}, blocked, true) != "" {
		t.Fatal("an office label was treated as consent to stay in this chat")
	}
}

func TestSpentOfficeRevisionRenewsInsteadOfClosing(t *testing.T) {
	residue := openOfficeResidue()
	residue.Remaining = map[string]int{string(tool.CapabilityDocumentWriteOffice): 0}
	current := intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.92}
	if semanticSpentWaveRelease(current, residue, "revise the deck") {
		t.Fatal("an office label released a spent office wave")
	}
	if decideSemanticResidueRelation(current, "revise the deck", residue) != semanticResidueUnclear {
		t.Fatal("an office label left the open document")
	}
	if decideSemanticResidueRelation(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.92}, "再改一版", residue) != semanticResidueNone {
		t.Fatal("a search label stayed on the spent office document")
	}
	rewritten, ok := semanticClassificationWithOpenResidue(current, residue.Needs, semanticResidueUnclear)
	if !ok || semanticClassificationRequestsLookupEvidence(rewritten) || !semanticReuseStoredLookupFacts("再改一版", rewritten) {
		t.Fatalf("revision rewrite=%#v ok=%v", rewritten, ok)
	}
	if renewed := semanticResidueRemainingForOpenTurn(residue, rewritten); len(renewed) != 0 {
		t.Fatalf("spent office revision stayed closed: %v", renewed)
	}
	if renewed := semanticResidueDropSpentCounts(residue.Remaining); len(renewed) != 0 {
		t.Fatalf("renewed ceiling=%v", renewed)
	}
	kept := semanticResidueRemainingForFollowUp(map[string]int{
		string(tool.CapabilityArtifactAcquireRemote): 0,
		"document.generate.file":                     0,
	}, intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: 0.9})
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

func TestOpenFileWriteDropsLeftoverWriteCeiling(t *testing.T) {
	residue := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:fs.write.local:real", Capability: tool.CapabilityFSWriteLocal, Required: true},
			{ID: "need:fs.read.local:real", Capability: tool.CapabilityFSReadLocal, Required: true},
		},
		Remaining: map[string]int{
			string(tool.CapabilityFSWriteLocal): 1,
			string(tool.CapabilityFSReadLocal):  6,
		},
	}
	planned := intent.ClassificationResult{Primary: intent.LabelFileWrite, Confidence: 0.91, Reason: "session residue continuation"}
	remaining := semanticResidueRemainingForOpenTurn(residue, planned)
	if _, ok := remaining[string(tool.CapabilityFSWriteLocal)]; ok {
		t.Fatalf("leftover file-write ceiling stayed on the continued edit: %v", remaining)
	}
	if remaining[string(tool.CapabilityFSReadLocal)] != 6 {
		t.Fatalf("read ceiling changed: %v", remaining)
	}
	companion := semanticSessionResidue{
		Status: semanticResidueOpen,
		Needs: []tool.CapabilityNeed{
			{ID: "need:shell.execute.remote_host:abc", Capability: tool.CapabilityShellExecuteRemoteHost, Required: true},
			{ID: "need:zz-baseline:fs.write.local:bbb", Capability: tool.CapabilityFSWriteLocal, EvidenceIDs: []string{"intent:baseline_workspace"}},
		},
		Remaining: map[string]int{
			string(tool.CapabilityShellExecuteRemoteHost): 1,
			string(tool.CapabilityFSWriteLocal):           0,
		},
	}
	kept := semanticResidueRemainingForOpenTurn(companion, intent.ClassificationResult{Primary: intent.LabelSSH, Confidence: 0.97})
	if kept[string(tool.CapabilityFSWriteLocal)] != 0 {
		t.Fatalf("baseline write on another task was reopened: %v", kept)
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
	if got := semanticResidueSummary("继续", "做一份项目周报"); got != "继续" {
		t.Fatalf("summary=%q", got)
	}
	if got := semanticResidueSummary("", "做一份项目周报"); got != "做一份项目周报" {
		t.Fatalf("empty summary=%q", got)
	}
	kept := &LoopContext{Runtime: RuntimeContext{SemanticIntent: &intent.ClassificationResult{
		Primary: intent.LabelOffice, Reason: "session residue continuation",
	}}}
	if !semanticClassificationKeepsPriorTask(kept) {
		t.Fatal("a residue rewrite must keep the prior task")
	}
	fresh := &LoopContext{Runtime: RuntimeContext{SemanticIntent: &intent.ClassificationResult{
		Primary: intent.LabelOffice, Confidence: 0.9, Reason: "tree",
	}}}
	if semanticClassificationKeepsPriorTask(fresh) {
		t.Fatal("a fresh office label kept the prior task")
	}
}
