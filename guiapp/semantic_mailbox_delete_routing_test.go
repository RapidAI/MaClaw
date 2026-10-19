package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestMailboxDeletePlanningRewriteLiftsAReadClassification(t *testing.T) {
	raw := &intent.ClassificationResult{
		Primary: intent.LabelMailboxRead, Confidence: 0.60, Layer: 3, Reason: "tree-after-embedding",
	}
	before := executionProfileFromSemanticIntent(raw, nil)
	if !before.IsLight() || !before.PromptIsLight() {
		t.Fatalf("mailbox_read at 0.60 profile=%+v", before)
	}
	got := semanticMailboxDeletePlanningRewrite(raw, "删除 邮箱中的所有邮件")
	if got == nil || got.Primary != intent.LabelMailboxDelete || got.Degraded || got.Confidence < imSemanticMinimumConfidence || got.Layer < 3 {
		t.Fatalf("rewrite=%+v", got)
	}
	if len(got.Secondary) != 0 || len(got.ToolNames) != 0 {
		t.Fatalf("rewrite kept companions: %+v", got)
	}
	profile := executionProfileFromSemanticIntent(got, nil)
	if profile.IsLight() || profile.PromptIsLight() || profile.Reason != "semantic capability-managed mutating intent" {
		t.Fatalf("profile=%+v", profile)
	}
	again := semanticMailboxDeletePlanningRewrite(got, "删除邮箱中的所有邮件")
	if again != nil {
		t.Fatalf("settled rewrite changed again: %+v", again)
	}
}

func TestMailboxDeletePlanningRewriteLeavesFileDeletesAndQuestions(t *testing.T) {
	raw := &intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.60, Layer: 3}
	for _, text := range []string{"删除邮件.md", "删除所有邮件吗", "删除邮箱", "was deleted from the inbox"} {
		if got := semanticMailboxDeletePlanningRewrite(raw, text); got != nil {
			t.Fatalf("%q rewrote to %+v", text, got)
		}
	}
	accountMail := semanticMailboxDeletePlanningRewrite(raw, "删除rapidai.tech中的所有邮件")
	if accountMail == nil || accountMail.Primary != intent.LabelMailboxDelete {
		t.Fatalf("domain delete=%+v", accountMail)
	}
}

func TestMailboxDeletePlanningRewriteBoostsOnlyAWeakTreePick(t *testing.T) {
	weak := &intent.ClassificationResult{Primary: intent.LabelMailboxDelete, Confidence: 0.65, Layer: 3}
	got := semanticMailboxDeletePlanningRewrite(weak, "你好")
	if got == nil || got.Confidence < imSemanticMinimumConfidence || got.Degraded || got.Layer != 3 {
		t.Fatalf("weak tree=%+v", got)
	}
	kept := &intent.ClassificationResult{Primary: intent.LabelMailboxDelete, Confidence: 0.72, Layer: 23}
	if semanticMailboxDeletePlanningRewrite(kept, "你好") != nil {
		t.Fatal("a tree score at the lookup floor was rewritten")
	}
	degraded := &intent.ClassificationResult{Primary: intent.LabelMailboxDelete, Confidence: 0.65, Layer: 2, Degraded: true}
	if semanticMailboxDeletePlanningRewrite(degraded, "你好") != nil {
		t.Fatal("a degraded L2 guess was boosted")
	}
	fromQuery := semanticMailboxDeletePlanningRewrite(degraded, "清空收件箱")
	if fromQuery == nil || fromQuery.Degraded || fromQuery.Primary != intent.LabelMailboxDelete || fromQuery.Layer != 3 {
		t.Fatalf("query rewrite=%+v", fromQuery)
	}
	layer23 := &intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.40, Layer: 23}
	keptLayer := semanticMailboxDeletePlanningRewrite(layer23, "清空邮箱")
	if keptLayer == nil || keptLayer.Layer != 23 {
		t.Fatalf("layer=%+v", keptLayer)
	}
}

func TestMailboxDeletePlanningOrderStaysBehindFileDelete(t *testing.T) {
	base := intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.60, Layer: 3}
	fileDelete := semanticPlanningClassification(base, "删除邮件.md")
	if fileDelete.Primary != intent.LabelFileDelete {
		t.Fatalf("file primary=%s", fileDelete.Primary)
	}
	mail := semanticPlanningClassification(base, "删除 邮箱中的所有邮件")
	if mail.Primary != intent.LabelMailboxDelete || mail.Confidence < imSemanticMinimumConfidence {
		t.Fatalf("mail=%+v", mail)
	}
	markdown := semanticPlanningClassification(intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.55}, "生成markdown")
	if markdown.Primary != intent.LabelFileWrite {
		t.Fatalf("markdown primary=%s", markdown.Primary)
	}
	worklogText := "查一下今天的工时记录"
	worklog := semanticPlanningClassification(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.2}, worklogText)
	direct := semanticWorklogPlanningClassification(intent.ClassificationResult{Primary: intent.LabelUnknown, Confidence: 0.2}, classifyWorklogRecord(worklogText))
	if worklog.Primary != direct.Primary || worklog.Primary == intent.LabelMailboxDelete {
		t.Fatalf("worklog=%s direct=%s", worklog.Primary, direct.Primary)
	}
}

func TestMailboxDeleteUtterancePlansFromASubFloorRead(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	prepared, handled, err := h.semanticPlanForTurnWithClassification(
		"user", "删除 邮箱中的所有邮件", "desktop", "root-mail-del", "turn-mail-del",
		&intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.60, Layer: 3, Reason: "tree-after-embedding"},
	)
	if err != nil || !handled || prepared == nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if !planHasCapabilities(prepared.plan, tool.CapabilityMessageDeleteMailbox) {
		t.Fatalf("selections=%#v unmet=%#v", prepared.plan.Selections, prepared.plan.Unmet)
	}
	if planHasCapabilities(prepared.plan, tool.CapabilityMessageSendMailbox) {
		t.Fatal("delete plan included send")
	}
}
