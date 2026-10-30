package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestMailboxSendPlanningLiftsGenerateThenSendOffTheSkillMarket(t *testing.T) {
	raw := &intent.ClassificationResult{
		Primary: intent.LabelDocumentGenerate, Confidence: 0.88, Layer: 3, Reason: "tree",
	}
	got := semanticMailboxSendPlanningRewrite(raw, "生成pdf后再次发送")
	if got == nil || got.Primary != intent.LabelMailboxSend || !got.HasLabel(intent.LabelDocumentGenerate) {
		t.Fatalf("rewrite=%+v", got)
	}
	if got.HasLabel(intent.LabelDocumentDelivery) || skillTaskLabel(got.Primary) {
		t.Fatalf("skill market would close the native tools: %+v", got)
	}
	if !strings.Contains(got.Reason, "mailbox send planning") {
		t.Fatalf("reason=%q", got.Reason)
	}
	again := semanticMailboxSendPlanningRewrite(got, "生成pdf后再次发送")
	if again != nil {
		t.Fatalf("settled rewrite changed again: %+v", again)
	}
	ctx := &LoopContext{}
	ctx.Runtime.SemanticIntent = got
	ctx.Runtime.Execution = executionProfileFromSemanticIntent(got, nil)
	if complexSkillTask(ctx, "生成pdf后再次发送") {
		t.Fatal("generate-then-send opened the skill market")
	}
}

func TestMailboxSendPlanningLiftsAnAddressedPDFMailOffDocumentDelivery(t *testing.T) {
	raw := &intent.ClassificationResult{
		Primary: intent.LabelDocumentDelivery, Confidence: 0.99, Layer: 3,
	}
	text := "向 znsoft@163.com 发送一封带PDF附件的邮件，报告北京天气。"
	got := semanticGovernedEffectPlanningRewrite(raw, text)
	if got == nil || got.Primary != intent.LabelMailboxSend {
		t.Fatalf("rewrite=%+v", got)
	}
	if got.HasLabel(intent.LabelDocumentGenerate) || got.HasLabel(intent.LabelDocumentDelivery) {
		t.Fatalf("existing attachment must not regenerate or stay on IM delivery: %+v", got)
	}
}

func TestMailboxSendPlanningLeavesPlainPDFAndIMDelivery(t *testing.T) {
	raw := &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.90, Layer: 3}
	for _, text := range []string{"生成一份北京天气的pdf", "把这个文件发送给我", "把pdf发到微信"} {
		if got := semanticMailboxSendPlanningRewrite(raw, text); got != nil {
			t.Fatalf("%q rewrote to %+v", text, got)
		}
	}
}

func TestMailboxSendPlanningKeepsDisplacedLookupAndDoesNotRegenerateAFinishedPDF(t *testing.T) {
	raw := &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelDocumentDelivery, intent.LabelLiveData},
		Confidence: 0.88, Layer: 3,
	}
	got := semanticMailboxSendPlanningRewrite(raw, "生成pdf后再次发送")
	if got == nil || got.Primary != intent.LabelMailboxSend {
		t.Fatalf("rewrite=%+v", got)
	}
	if len(got.Secondary) != 2 || got.Secondary[0] != intent.LabelLiveData || got.Secondary[1] != intent.LabelDocumentGenerate {
		t.Fatalf("secondary=%v", got.Secondary)
	}
	if got.HasLabel(intent.LabelDocumentDelivery) || skillTaskLabel(got.Primary) {
		t.Fatalf("delivery or skill market stayed: %+v", got)
	}
	finished := "向 znsoft@163.com 发送一封带PDF附件的邮件，并附加刚生成的 PDF"
	again := semanticMailboxSendPlanningRewrite(&intent.ClassificationResult{
		Primary: intent.LabelDocumentDelivery, Confidence: 0.99, Layer: 3,
	}, finished)
	if again == nil || again.Primary != intent.LabelMailboxSend || again.HasLabel(intent.LabelDocumentGenerate) || again.HasLabel(intent.LabelDocumentDelivery) {
		t.Fatalf("finished pdf=%+v", again)
	}
}

func TestMailboxSendPlanningDropsAnUnmappedCompanion(t *testing.T) {
	raw := &intent.ClassificationResult{
		Primary:    intent.LabelWorkflowTask,
		Secondary:  []intent.IntentLabel{intent.LabelLiveData, intent.LabelWorkflowTask},
		Confidence: 0.90, Layer: 3,
	}
	got := semanticMailboxSendPlanningRewrite(raw, "向znsoft@163.com发邮件，报告北京天气。")
	if got == nil || got.Primary != intent.LabelMailboxSend || !got.HasLabel(intent.LabelLiveData) {
		t.Fatalf("rewrite=%+v", got)
	}
	if got.HasLabel(intent.LabelWorkflowTask) {
		t.Fatalf("workflow_task stayed on the send: %+v", got)
	}
	if _, unmapped := imSemanticIntentCoverage(*got); unmapped != "" {
		t.Fatalf("send plan fail-closes on %s", unmapped)
	}
}

func TestMailboxDeleteStillWinsOverSendPlanning(t *testing.T) {
	raw := &intent.ClassificationResult{Primary: intent.LabelDocumentGenerate, Confidence: 0.50, Layer: 2, Degraded: true}
	got := semanticGovernedEffectPlanningRewrite(raw, "删除邮箱中的所有邮件")
	if got == nil || got.Primary != intent.LabelMailboxDelete {
		t.Fatalf("rewrite=%+v", got)
	}
}
