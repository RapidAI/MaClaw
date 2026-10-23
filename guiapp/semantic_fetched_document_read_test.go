package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestQueryWantsFetchedDocumentRead(t *testing.T) {
	positives := []string{
		"网上搜索一个dwg文档，理解其中的内容",
		"网上搜索一个ＤＷＧ文档，理解其中的内容",
		"search the web for a dwg and read its content",
		"下载 sample.dxf，读出内容",
		"搜索一份 pdf，看看内容",
		"不要读 pdf，还是网上搜索一个 dwg，理解其中的内容",
		"不要 读这个说明，网上搜索一个 dwg 文档，理解其中的内容",
	}
	for _, text := range positives {
		if !queryWantsFetchedDocumentRead(text) {
			t.Errorf("want read: %q", text)
		}
	}
	negatives := []string{
		"网上搜索 dwg 是什么",
		"搜索 pdf 转 word",
		"生成pdf",
		"真读内容",
		"不要读这个dwg的内容",
		"别读图纸内容",
		"不用读取内容，这是一个dwg",
		"网上搜索 pdfium 的文档，理解其中的内容",
		"网上搜索 docker 文档，理解其中的内容",
		"网上搜索 dwg，认真读书",
		"搜索一下今天的天气",
		"找一个 docx，文件内容是什么",
		"读一下 report.dwg 的图纸内容",
		"不要 读这个dwg的图纸内容",
		"不搜索 dwg，理解其中的内容",
		"理解其中的内容\n\n" + filePathPromptPrefix + "\nC:\\docs\\pump.dwg\n",
		"网上搜索，理解其中的内容\n\n" + filePathPromptPrefix + "\nC:\\docs\\pump.dwg\n",
	}
	for _, text := range negatives {
		if queryWantsFetchedDocumentRead(text) {
			t.Errorf("must not read: %q", text)
		}
	}
}

func TestFetchedDocumentReadRewriteKeepsSearchAndReader(t *testing.T) {
	in := intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.94,
		Secondary:  []intent.IntentLabel{intent.LabelWebFetch},
		Reason:     "semantic capability-managed lookup",
	}
	text := "网上搜索一个dwg文档，理解其中的内容"
	got := semanticFetchedDocumentReadRewrite(&in, text)
	if got == nil {
		t.Fatal("expected rewrite")
	}
	if len(in.Secondary) != 1 || in.Secondary[0] != intent.LabelWebFetch {
		t.Fatalf("rewrite mutated caller secondary: %#v", in.Secondary)
	}
	if got.Primary != intent.LabelSearch || got.Confidence != 0.94 || got.Degraded {
		t.Fatalf("primary=%s conf=%v degraded=%v", got.Primary, got.Confidence, got.Degraded)
	}
	if !got.HasLabel(intent.LabelFileDownload) || !got.HasLabel(intent.LabelFileRead) {
		t.Fatalf("labels=%v", got.Labels())
	}
	for _, banned := range []intent.IntentLabel{
		intent.LabelFileWrite, intent.LabelShellCommand, intent.LabelDocumentRead,
	} {
		if got.HasLabel(banned) {
			t.Fatalf("granted %s", banned)
		}
	}
	if semanticFetchedDocumentReadRewrite(got, text) != nil {
		t.Fatal("rewrite must be idempotent")
	}

	profile := executionProfileFromSemanticIntent(got, nil)
	if profile.IsLight() || profile.PromptIsLight() || profile.ToolBudget != 0 || profile.IterationBudget != 0 {
		t.Fatalf("profile=%+v", profile)
	}
	if profile.Reason != "semantic fetched document read" {
		t.Fatalf("reason=%q", profile.Reason)
	}
	if lookupContinuationMayNarrow(profile) {
		t.Fatal("project-task lookup continuation would pull this profile back to light")
	}
	kept := (&IMMessageHandler{}).continuationKeepsParentExecution(profile, "desktop-user:D:/workprj/aicoder", "真读内容", got)
	if kept.Reason != profile.Reason || kept.PromptIsLight() || kept.ToolBudget != 0 {
		t.Fatalf("continuation narrowed the reader: %+v", kept)
	}
	if semanticBaselineWorkspaceApplies(*got) {
		t.Fatal("search primary must not grow the bash baseline")
	}
	if semanticArchetypeBundleKey(*got) != intent.LabelSearch {
		t.Fatalf("bundle key=%s", semanticArchetypeBundleKey(*got))
	}

	needs, managed, err := semanticIntentNeedsFromClassification(newIMSemanticCapabilityRegistry(), *got)
	if err != nil || !managed {
		t.Fatalf("needs managed=%v err=%v", managed, err)
	}
	required := map[tool.CapabilityID]bool{}
	present := map[tool.CapabilityID]bool{}
	for _, need := range needs {
		present[need.Capability] = true
		if need.Required {
			required[need.Capability] = true
		}
	}
	for _, id := range []tool.CapabilityID{
		"information.search.web",
		tool.CapabilityArtifactAcquireRemote,
		tool.CapabilityFSReadLocal,
	} {
		if !required[id] {
			t.Errorf("missing required %s in %#v", id, needs)
		}
	}
	for _, id := range []tool.CapabilityID{
		tool.CapabilityFSWriteLocal,
		tool.CapabilityShellExecuteLocal,
		"document.read.local",
	} {
		if present[id] {
			t.Errorf("must not plan %s", id)
		}
	}
}

func TestFetchedDocumentReadPromotesAmbiguousLookup(t *testing.T) {
	in := intent.ClassificationResult{
		Primary:    intent.LabelAmbiguous,
		Confidence: 0.55,
		Degraded:   true,
	}
	got := semanticFetchedDocumentReadRewrite(&in, "网上搜索一个dwg文档，理解其中的内容")
	if got == nil {
		t.Fatal("expected promotion")
	}
	if got.Primary != intent.LabelSearch || got.Degraded || got.Confidence < imSemanticMinimumConfidence {
		t.Fatalf("primary=%s conf=%v degraded=%v", got.Primary, got.Confidence, got.Degraded)
	}
	if got.Confidence != imSemanticMinimumConfidence {
		t.Fatalf("confidence=%v, want the resolver floor", got.Confidence)
	}
	profile := executionProfileFromSemanticIntent(got, nil)
	if profile.Reason != "semantic fetched document read" || profile.PromptIsLight() {
		t.Fatalf("profile=%+v", profile)
	}
}

func TestFetchedDocumentReadFollowUpUsesHistory(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "网上搜索一个dwg文档，理解其中的内容"},
		{Role: "assistant", Content: "本轮只有网页搜索"},
		{Role: "user", Content: "你自己搜索"},
		{Role: "assistant", Content: "列出了样例"},
	}
	stored := &intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.90}
	got := semanticFetchedDocumentReadPlanning(stored, "真读内容", history)
	if got == nil || got.Primary != intent.LabelSearch || !got.HasLabel(intent.LabelFileDownload) || !got.HasLabel(intent.LabelFileRead) {
		t.Fatalf("follow-up=%v", got)
	}
	if got.Confidence != 0.90 {
		t.Fatalf("confidence=%v", got.Confidence)
	}
	if semanticFetchedDocumentReadPlanning(&intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.90}, "真读内容", nil) != nil {
		t.Fatal("bare 真读内容 must not mint a reader")
	}

	planned := semanticPlanningClassification(*got, "真读内容")
	if planned.Primary != intent.LabelSearch || !planned.HasLabel(intent.LabelFileDownload) || !planned.HasLabel(intent.LabelFileRead) {
		t.Fatalf("planning stripped labels: %+v", planned)
	}
	added := semanticPlanningClassification(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.94}, "网上搜索一个dwg文档，理解其中的内容")
	if !added.HasLabel(intent.LabelFileDownload) || !added.HasLabel(intent.LabelFileRead) || added.Primary != intent.LabelSearch {
		t.Fatalf("planning=%+v", added)
	}
	left := semanticPlanningClassification(intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.94}, "网上搜索 dwg 是什么")
	if left.HasLabel(intent.LabelFileDownload) || left.HasLabel(intent.LabelFileRead) {
		t.Fatalf("format question minted a reader: %+v", left)
	}
}

func TestFetchedDocumentReadDoesNotStealOtherPrimaries(t *testing.T) {
	text := "网上搜索一个dwg文档，理解其中的内容"
	for _, primary := range []intent.IntentLabel{intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelCoding, intent.LabelShellCommand} {
		got := semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{Primary: primary, Confidence: 0.92}, text)
		if got != nil {
			t.Fatalf("stole %s -> %+v", primary, got)
		}
	}
	for _, text := range []string{
		"读一下 report.dwg 的图纸内容",
		"理解其中的内容\n\n" + filePathPromptPrefix + "\nC:\\docs\\pump.dwg\n",
		"网上搜索，理解其中的内容\n\n" + filePathPromptPrefix + "\nC:\\docs\\pump.dwg\n",
	} {
		for _, primary := range []intent.IntentLabel{intent.LabelUnknown, intent.LabelSearch} {
			got := semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{Primary: primary, Confidence: 0.94}, text)
			if got != nil {
				t.Fatalf("primary %s minted a download for %q: %+v", primary, text, got)
			}
		}
	}
	wrapped := acpProgrammingUserText(`D:\workprj\aicoder`, "网上搜索一个dwg文档，理解其中的内容")
	if got := semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.94}, wrapped); got == nil || !got.HasLabel(intent.LabelFileRead) {
		t.Fatalf("acp wrapper hid the request: %+v", got)
	}
}

func TestFetchedDocumentReadRetargetsFetchPrimaries(t *testing.T) {
	download := intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: 0.92}
	got := semanticFetchedDocumentReadRewrite(&download, "下载 sample.dxf，读出内容")
	if got == nil || got.Primary != intent.LabelSearch || download.Primary != intent.LabelFileDownload {
		t.Fatalf("download primary=%v caller=%s", got, download.Primary)
	}
	if !got.HasLabel(intent.LabelFileDownload) || !got.HasLabel(intent.LabelFileRead) || got.HasLabel(intent.LabelDocumentRead) {
		t.Fatalf("labels=%v", got.Labels())
	}
	if semanticBaselineWorkspaceApplies(*got) || semanticArchetypeBundleKey(*got) != intent.LabelSearch {
		t.Fatalf("baseline or bundle changed: key=%s", semanticArchetypeBundleKey(*got))
	}
	assertFetchedDocumentReadNeeds(t, *got)
	profile := executionProfileFromSemanticIntent(got, nil)
	if profile.Reason != "semantic fetched document read" || profile.PromptIsLight() || profile.ToolBudget != 0 {
		t.Fatalf("profile=%+v", profile)
	}
	if semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{Primary: intent.LabelFileDownload, Confidence: 0.92}, "下载 sample.dxf") != nil {
		t.Fatal("a download without a body read minted a reader")
	}

	// Residue can union file_download onto a file_read primary before this
	// rewrite. Returning nil there kept the file_write archetype.
	held := intent.ClassificationResult{
		Primary:    intent.LabelFileRead,
		Confidence: 0.94,
		Secondary:  []intent.IntentLabel{intent.LabelFileDownload},
	}
	retargeted := semanticFetchedDocumentReadRewrite(&held, "网上搜索一个dwg文档，理解其中的内容")
	if retargeted == nil || retargeted.Primary != intent.LabelSearch || held.Primary != intent.LabelFileRead {
		t.Fatalf("held file_read=%v caller=%s", retargeted, held.Primary)
	}
	if len(held.Secondary) != 1 || held.Secondary[0] != intent.LabelFileDownload {
		t.Fatalf("retarget mutated caller secondary: %#v", held.Secondary)
	}
	if semanticBaselineWorkspaceApplies(*retargeted) || semanticArchetypeBundleKey(*retargeted) != intent.LabelSearch {
		t.Fatalf("file_read primary kept the write bundle: key=%s", semanticArchetypeBundleKey(*retargeted))
	}
	assertFetchedDocumentReadNeeds(t, *retargeted)

	attachment := semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{
		Primary: intent.LabelDocumentRead, Confidence: 0.90,
	}, "网上搜索一个dwg文档，理解其中的内容")
	if attachment == nil || attachment.Primary != intent.LabelSearch || attachment.HasLabel(intent.LabelDocumentRead) {
		t.Fatalf("attachment read stayed: %+v", attachment)
	}
	assertFetchedDocumentReadNeeds(t, *attachment)

	withAttachment := semanticFetchedDocumentReadRewrite(&intent.ClassificationResult{
		Primary:    intent.LabelSearch,
		Confidence: 0.94,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentRead, intent.LabelFileDownload, intent.LabelFileRead},
	}, "网上搜索一个dwg文档，理解其中的内容")
	if withAttachment == nil || withAttachment.HasLabel(intent.LabelDocumentRead) || !withAttachment.HasLabel(intent.LabelFileRead) {
		t.Fatalf("secondary attachment read stayed: %+v", withAttachment)
	}
	if semanticFetchedDocumentReadRewrite(withAttachment, "网上搜索一个dwg文档，理解其中的内容") != nil {
		t.Fatal("stripped attachment read must stay idempotent")
	}
}

func assertFetchedDocumentReadNeeds(t *testing.T, got intent.ClassificationResult) {
	t.Helper()
	needs, managed, err := semanticIntentNeedsFromClassification(newIMSemanticCapabilityRegistry(), got)
	if err != nil || !managed {
		t.Fatalf("needs managed=%v err=%v", managed, err)
	}
	required := map[tool.CapabilityID]bool{}
	present := map[tool.CapabilityID]bool{}
	for _, need := range needs {
		present[need.Capability] = true
		if need.Required {
			required[need.Capability] = true
		}
	}
	for _, id := range []tool.CapabilityID{
		"information.search.web",
		tool.CapabilityArtifactAcquireRemote,
		tool.CapabilityFSReadLocal,
	} {
		if !required[id] {
			t.Errorf("missing required %s in %#v", id, needs)
		}
	}
	for _, id := range []tool.CapabilityID{
		tool.CapabilityFSWriteLocal,
		tool.CapabilityShellExecuteLocal,
		"document.read.local",
	} {
		if present[id] {
			t.Errorf("must not plan %s", id)
		}
	}
}
