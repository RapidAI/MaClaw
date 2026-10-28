package guiapp

import (
	"strings"
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

func TestSchedulePayloadPlanningKeepsTheGoverningAct(t *testing.T) {
	incident := "创建定时任务，每小时清除一下rapidai邮箱。"
	settled := intent.ClassificationResult{Primary: intent.LabelScheduleManage, Confidence: 0.94, Layer: 3, Reason: "tree-after-embedding"}
	if got := semanticMailboxDeletePlanningRewrite(&settled, incident); got != nil {
		t.Fatalf("settled schedule was rewritten: %+v", got)
	}
	planned := semanticPlanningClassification(settled, incident)
	if planned.Primary != intent.LabelScheduleManage || strings.Contains(planned.Reason, "mailbox delete planning") {
		t.Fatalf("planned=%+v", planned)
	}
	for _, primary := range []intent.IntentLabel{intent.LabelMailboxRead, intent.LabelMailboxDelete} {
		raw := intent.ClassificationResult{Primary: primary, Confidence: 0.60, Layer: 3, Reason: "tree-after-embedding"}
		got := semanticPlanningClassification(raw, incident)
		if got.Primary != intent.LabelScheduleManage || got.Degraded || got.Confidence < imSemanticMinimumConfidence || !strings.Contains(got.Reason, "schedule payload planning") {
			t.Fatalf("from %s: %+v", primary, got)
		}
		if len(got.Secondary) != 0 || len(got.ToolNames) != 0 || strings.Contains(got.Reason, "mailbox delete planning") {
			t.Fatalf("companions %+v", got)
		}
	}
	hourly := "每小时清除一下rapidai邮箱"
	if got := semanticPlanningClassification(settled, hourly); got.Primary != intent.LabelScheduleManage {
		t.Fatalf("hourly settled=%+v", got)
	}
	if got := semanticMailboxDeletePlanningRewrite(&settled, hourly); got != nil {
		t.Fatalf("hourly settled rewrite=%+v", got)
	}
	dispatch := intent.ClassificationResult{Primary: intent.LabelScheduleDispatch, Confidence: 0.94, Layer: 2}
	if got := semanticMailboxDeletePlanningRewrite(&dispatch, hourly); got != nil {
		t.Fatalf("settled dispatch rewrite=%+v", got)
	}
	weak := intent.ClassificationResult{Primary: intent.LabelScheduleManage, Confidence: 0.60, Layer: 3, Degraded: true}
	if got := semanticMailboxDeletePlanningRewrite(&weak, hourly); got == nil || got.Primary != intent.LabelMailboxDelete {
		t.Fatalf("sub-floor schedule guess=%+v", got)
	}
	for _, text := range []string{"清空邮箱，另外创建一个定时任务提醒我", "把定时任务的通知邮件删了"} {
		got := semanticPlanningClassification(intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: 0.60, Layer: 3}, text)
		if got.Primary != intent.LabelMailboxDelete || !strings.Contains(got.Reason, "mailbox delete planning") {
			t.Fatalf("%q planned=%+v", text, got)
		}
	}
	file := semanticPlanningClassification(settled, "删除邮件.md")
	if file.Primary != intent.LabelFileDelete {
		t.Fatalf("file primary=%s", file.Primary)
	}
}

// A read-only mailbox turn that the user then authorizes for deletion reaches
// delete through a petition. Production 2026-10-27: the plan stayed on
// mailbox_query, the model called mailbox_delete, the unrendered fence asked
// for a petition, and the name was absent from the petition inventory — the
// turn answered that delete did not exist while the user was authorizing it.
func TestMailboxDeleteIsPetitionableFromAReadSurface(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-mail-del-petition", "授权删除", "desktop", "root-mail-del-petition", "turn-mail-del-petition",
		&intent.ClassificationResult{Primary: intent.LabelMailboxRead, Confidence: .92},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	closeSemanticSurface(t, surface)
	if semanticGrantNameForAdapter(surface, semanticTrustedMailboxDeleteAdapter) != "" {
		t.Fatal("fixture must start without a delete grant")
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface, platform: "desktop"}
	if got := semanticToolsSearchStatus(cb, "mailbox_delete"); got != "[可请愿：直接调用一次]" {
		t.Fatalf("discovery status=%s", got)
	}
	granted, message := cb.PetitionToolCall("mailbox_delete")
	if !granted || !strings.Contains(message, "mailbox_delete") {
		t.Fatalf("granted=%v message=%q", granted, message)
	}
	if cb.semanticSurface == surface {
		t.Fatal("petition must publish a child revision")
	}
	if !planHasCapabilities(cb.semanticSurface.plan, tool.CapabilityMessageReadMailbox, tool.CapabilityMessageDeleteMailbox) {
		t.Fatalf("child plan=%#v", cb.semanticSurface.plan.Selections)
	}
	if got := semanticGrantNameForAdapter(cb.semanticSurface, semanticTrustedMailboxDeleteAdapter); got != "mailbox_delete" {
		t.Fatalf("delete grant name=%q", got)
	}
	if cb.semanticSurface.executor == nil {
		t.Fatal("child surface has no executor")
	}
	if !cb.semanticEffectfulPetitionConsumed {
		t.Fatal("a delete petition must spend the effectful budget")
	}
	// The widened surface must be what the next request renders, not only a
	// plan entry: otherwise the grant message would send the model back to a
	// name that is still absent.
	listed := false
	for _, def := range cb.BuildToolsForModelRequest("授权删除", 1) {
		if extractToolName(def) == "mailbox_delete" {
			listed = true
		}
	}
	if !listed {
		t.Fatal("next request surface omitted mailbox_delete")
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
