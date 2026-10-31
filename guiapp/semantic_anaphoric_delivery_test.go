package guiapp

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func mailboxSendHistory() []agent.ConversationEntry {
	return []agent.ConversationEntry{
		{Role: "user", Content: "北京天气，生成pdf，发送给znsoft@163.com"},
		{Role: "assistant", Content: "邮件已发送"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":      "mailbox_send",
					"arguments": `{"to":["znsoft@163.com"],"subject":"北京天气报告"}`,
				},
			},
		}},
	}
}

func TestAnaphoricMailboxFollowUpAddsMailboxToADeicticSend(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	for _, text := range []string{
		"把重庆的天气也发给他",
		"把北京和重庆的天气分别发给他",
		"也给他发一份",
		"再发一份给他",
		"send it to him",
	} {
		out := semanticAnaphoricMailboxFollowUp(&current, text, mailboxSendHistory())
		if out == nil {
			t.Fatalf("%q: deictic send did not continue the mailbox channel", text)
		}
		if !out.HasLabel(intent.LabelMailboxSend) {
			t.Fatalf("%q labels = %v", text, out.Labels())
		}
		if current.HasLabel(intent.LabelMailboxSend) {
			t.Fatal("the input classification was mutated")
		}
	}
}

func TestAnaphoricMailboxFollowUpNeedsARealMailboxHistory(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	addressOnly := []agent.ConversationEntry{
		{Role: "user", Content: "我的邮箱是 znsoft@163.com"},
	}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", addressOnly); out != nil {
		t.Fatalf("an address without a send became a delivery: %v", out.Labels())
	}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", nil); out != nil {
		t.Fatal("empty history became a delivery")
	}
}

func TestAnaphoricMailboxFollowUpLeavesAnExplicitChannelAlone(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	for _, text := range []string{
		"把重庆的天气发到他微信",
		"把重庆的天气发到聊天里",
		"email the Chongqing weather to him",
	} {
		if out := semanticAnaphoricMailboxFollowUp(&current, text, mailboxSendHistory()); out != nil {
			t.Fatalf("%q was rewritten: %v", text, out.Labels())
		}
	}
}

func TestAnaphoricMailboxFollowUpIgnoresOrdinaryLookups(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	for _, text := range []string{"查一下重庆天气", "重庆天气怎么样", "生成重庆天气pdf", "我也发现重庆天气不错"} {
		if out := semanticAnaphoricMailboxFollowUp(&current, text, mailboxSendHistory()); out != nil {
			t.Fatalf("%q was rewritten: %v", text, out.Labels())
		}
	}
}

func TestAnaphoricMailboxFollowUpReadsTypedAndReloadedCalls(t *testing.T) {
	typed := []agent.ConversationEntry{
		{Role: "user", Content: "发送给 znsoft@163.com"},
		{Role: "assistant", ToolCalls: []map[string]string{{"name": "mailbox_send"}}},
	}
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	if out := semanticAnaphoricMailboxFollowUp(&current, "再发一份给他", typed); out == nil {
		t.Fatal("map-shaped tool calls were not read")
	}
}

func TestAnaphoricMailboxFollowUpIgnoresNegatedAndStatusSends(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	for _, text := range []string{
		"别发给他",
		"先别发给他",
		"别给他发",
		"不用再发给他了",
		"don't send it to him",
		"把重庆天气发给他了吗",
		"发给他了吗",
		"给他发消息",
		"did you send it to him",
	} {
		if out := semanticAnaphoricMailboxFollowUp(&current, text, mailboxSendHistory()); out != nil {
			t.Fatalf("%q was rewritten: %v", text, out.Labels())
		}
	}
}

func TestAnaphoricMailboxFollowUpIgnoresUntrustedClassifications(t *testing.T) {
	degraded := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3, Degraded: true}
	if out := semanticAnaphoricMailboxFollowUp(&degraded, "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("degraded verdict was rewritten: %v", out.Labels())
	}
	low := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.4, Layer: 3}
	if out := semanticAnaphoricMailboxFollowUp(&low, "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("low-confidence verdict was rewritten: %v", out.Labels())
	}
}

func TestAnaphoricMailboxFollowUpIgnoresUnmappedCompanions(t *testing.T) {
	current := intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Secondary:  []intent.IntentLabel{intent.LabelWorkflowTask},
		Confidence: 0.95,
		Layer:      3,
	}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("unmapped companion was published as a managed plan: %v", out.Labels())
	}
}

func TestAnaphoricMailboxFollowUpIgnoresAddressesFromWebResults(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	history := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]interface{}{
			{"function": map[string]interface{}{"name": "mailbox_send", "arguments": `{}`}},
		}},
		{Role: "tool_result", Content: "contact: someone@example.com"},
	}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", history); out != nil {
		t.Fatalf("a web result address became the recipient: %v", out.Labels())
	}
}

func TestAnaphoricMailboxFollowUpDoesNotAliasTheInputSlice(t *testing.T) {
	secondary := make([]intent.IntentLabel, 1, 4)
	secondary[0] = intent.LabelDocumentGenerate
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Secondary: secondary, Confidence: 0.95, Layer: 3}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", mailboxSendHistory()); out == nil {
		t.Fatal("rewrite returned nil")
	}
	if spare := current.Secondary[:2][1]; spare != "" {
		t.Fatalf("rewrite wrote into the input backing array: %v", spare)
	}
}

func TestAnaphoricMailboxFollowUpReachesThePlan(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", mailboxSendHistory())
	if out == nil {
		t.Fatal("rewrite returned nil")
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Chongqing: light rain, 18C", nil }
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassificationAndAttachments(
		"user-1", "把重庆的天气也发给他", "desktop", "root-anaphoric-mail", "turn-anaphoric-mail", out, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v surface=%v", handled, err, surface)
	}
	if !planHasCapabilities(surface.plan, "message.send.mailbox") {
		t.Fatalf("mailbox missing from plan: %#v", surface.plan.Selections)
	}
}

func TestAnaphoricMailboxSurvivesTheCarriedResidueCeiling(t *testing.T) {
	// The open task spent its mailbox grant last turn. The host drops that
	// carried count when it adds the fresh send, so the planner keeps the
	// mailbox need instead of clamping it away.
	loop := &LoopContext{semanticResidueRemaining: map[string]int{
		"information.search.web":                  2,
		string(tool.CapabilityMessageSendMailbox): 0,
	}}
	loop.dropSemanticResidueCeiling(string(tool.CapabilityMessageSendMailbox))
	requestCtx := withSemanticResidueRemaining(context.Background(), loop.semanticResidueRemaining)

	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	rewritten := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", mailboxSendHistory())
	if rewritten == nil {
		t.Fatal("rewrite returned nil")
	}
	h := &IMMessageHandler{registry: NewToolRegistry()}
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Chongqing: light rain, 18C", nil }
	_, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(
		requestCtx, "user-1", "把重庆的天气也发给他", "desktop", "root-anaphoric-ceiling", "turn-anaphoric-ceiling", "session-anaphoric-ceiling", rewritten, nil,
	)
	if err != nil || !handled || surface == nil {
		t.Fatalf("surface handled=%v err=%v surface=%v", handled, err, surface)
	}
	if !planHasCapabilities(surface.plan, "message.send.mailbox") {
		t.Fatalf("mailbox was clamped off the plan: %#v", surface.plan.Selections)
	}

	// Control: the same turn with the spent count still carried must not
	// publish the mailbox capability, which is what the drop prevents.
	spentCtx := withSemanticResidueRemaining(context.Background(), map[string]int{
		"information.search.web":                  2,
		string(tool.CapabilityMessageSendMailbox): 0,
	})
	_, spentSurface, spentHandled, spentErr := h.semanticCallSurfaceForSharedTurnWithContextAndIdentityAndClassificationAndAttachmentsWithSession(
		spentCtx, "user-1", "把重庆的天气也发给他", "desktop", "root-anaphoric-ceiling", "turn-anaphoric-ceiling", "session-anaphoric-ceiling", rewritten, nil,
	)
	if spentErr == nil && spentHandled && spentSurface != nil && planHasCapabilities(spentSurface.plan, "message.send.mailbox") {
		t.Fatal("the carried spent count did not clamp the fresh send")
	}
}

func TestAnaphoricMailboxFollowUpLeavesWorkflowAndControlPlaneTurnsAlone(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	workflow := current
	workflow.WorkflowType = "presentation"
	if out := semanticAnaphoricMailboxFollowUp(&workflow, "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("workflow phase was rewritten: %v", out.Labels())
	}
	failed := current
	failed.ControlPlaneFailure = true
	if out := semanticAnaphoricMailboxFollowUp(&failed, "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("control-plane failure was rewritten: %v", out.Labels())
	}
}

func TestAnaphoricMailboxFollowUpReadsTheTrustedSendName(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "发送给 znsoft@163.com"},
		{Role: "assistant", ToolCalls: []interface{}{
			map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":      "semantic_send_trusted_mailbox",
					"arguments": `{"to":["znsoft@163.com"]}`,
				},
			},
		}},
	}
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	if out := semanticAnaphoricMailboxFollowUp(&current, "把重庆的天气也发给他", history); out == nil {
		t.Fatal("the trusted adapter name was not read as a mailbox send")
	}
}

func TestAnaphoricMailboxFollowUpIgnoresHostContinuations(t *testing.T) {
	current := intent.ClassificationResult{Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3}
	goal := "[系统续接][execute] 按步骤执行目标：把重庆的天气也发给他\n\n当前步骤：发送报告"
	if out := semanticAnaphoricMailboxFollowUp(&current, goal, mailboxSendHistory()); out != nil {
		t.Fatalf("goal continuation re-armed the send: %v", out.Labels())
	}
	round := "[系统续轮] 继续完成同一任务。从对话记录里已经完成的部分接着做。\n\n任务：\n把重庆的天气也发给他"
	if out := semanticAnaphoricMailboxFollowUp(&current, round, mailboxSendHistory()); out != nil {
		t.Fatalf("session round continuation re-armed the send: %v", out.Labels())
	}
}
