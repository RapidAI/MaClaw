package guiapp

import (
	"context"
	"fmt"
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

func anaphoricClassifierHandler(classify func(string) (string, error)) *IMMessageHandler {
	return &IMMessageHandler{anaphoricDeliveryClassifier: classify}
}

func TestAnaphoricMailboxFollowUpUsesTheSemanticVerdict(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	var seen string
	h := anaphoricClassifierHandler(func(text string) (string, error) {
		seen = text
		return `{"deliver":"mailbox"}`, nil
	})
	out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", mailboxSendHistory())
	if out == nil {
		t.Fatal("mailbox verdict did not continue the channel")
	}
	if !out.HasLabel(intent.LabelMailboxSend) {
		t.Fatalf("labels = %v", out.Labels())
	}
	if seen != "把重庆的天气也发给他" {
		t.Fatalf("classifier saw %q", seen)
	}
	if current.HasLabel(intent.LabelMailboxSend) {
		t.Fatal("the input classification was mutated")
	}
}

func TestAnaphoricMailboxFollowUpKeepsCurrentChatWhenTheModelSaysSo(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	for _, verdict := range []string{
		`{"deliver":"current_chat"}`,
		`{"deliver":"none"}`,
		`current_chat`,
		`current_chat.`,
		"```json\n{\"deliver\":\"current_chat\"}\n```",
	} {
		h := anaphoricClassifierHandler(func(string) (string, error) { return verdict, nil })
		if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把报告发给我", mailboxSendHistory()); out != nil {
			t.Fatalf("%q was rewritten: %v", verdict, out.Labels())
		}
	}
}

func TestAnaphoricMailboxFollowUpFailsClosed(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	for _, classify := range []func(string) (string, error){
		func(string) (string, error) { return "", fmt.Errorf("timeout") },
		func(string) (string, error) { return "", nil },
		func(string) (string, error) { return `{"deliver":"maybe"}`, nil },
		func(string) (string, error) { return `not json`, nil },
	} {
		h := anaphoricClassifierHandler(classify)
		if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
			t.Fatalf("a failed verdict rewrote the turn: %v", out.Labels())
		}
	}
}

func TestAnaphoricMailboxFollowUpSkipsStructuralGuardsWithoutClassifying(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	unmapped := current
	unmapped.Secondary = []intent.IntentLabel{intent.LabelWorkflowTask}
	workflow := current
	workflow.WorkflowType = "presentation"
	controlPlane := current
	controlPlane.ControlPlaneFailure = true
	alreadyMail := current
	alreadyMail.Secondary = []intent.IntentLabel{intent.LabelMailboxSend}
	degraded := current
	degraded.Degraded = true
	low := current
	low.Confidence = 0.4

	addressOnly := []agent.ConversationEntry{{Role: "user", Content: "我的邮箱是 znsoft@163.com"}}
	sendNoAddress := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]string{{"name": "mailbox_send"}}},
	}
	webAddress := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]string{{"name": "mailbox_send"}}},
		{Role: "tool_result", Content: "contact: someone@example.com"},
	}

	tests := []struct {
		name    string
		current *intent.ClassificationResult
		text    string
		history []agent.ConversationEntry
	}{
		{"degraded", &degraded, "把重庆的天气也发给他", mailboxSendHistory()},
		{"low confidence", &low, "把重庆的天气也发给他", mailboxSendHistory()},
		{"unmapped companion", &unmapped, "把重庆的天气也发给他", mailboxSendHistory()},
		{"workflow phase", &workflow, "把重庆的天气也发给他", mailboxSendHistory()},
		{"control plane failure", &controlPlane, "把重庆的天气也发给他", mailboxSendHistory()},
		{"already mailbox", &alreadyMail, "把重庆的天气也发给他", mailboxSendHistory()},
		{"goal continuation", &current, "[系统续接][execute] 按步骤执行目标：把重庆的天气也发给他", mailboxSendHistory()},
		{"session round", &current, "[系统续轮] 继续完成同一任务。\n\n任务：\n把重庆的天气也发给他", mailboxSendHistory()},
		{"empty text", &current, "  ", mailboxSendHistory()},
		{"no history", &current, "把重庆的天气也发给他", nil},
		{"address without send", &current, "把重庆的天气也发给他", addressOnly},
		{"send without address", &current, "把重庆的天气也发给他", sendNoAddress},
		{"address only in web result", &current, "把重庆的天气也发给他", webAddress},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := anaphoricClassifierHandler(func(string) (string, error) {
				calls++
				return `{"deliver":"mailbox"}`, nil
			})
			if out := h.semanticAnaphoricMailboxFollowUp(tc.current, "user-1", tc.text, tc.history); out != nil {
				t.Fatalf("guard %s was rewritten: %v", tc.name, out.Labels())
			}
			if calls != 0 {
				t.Fatalf("classifier ran for a skipped turn: %d", calls)
			}
		})
	}
}

func TestAnaphoricMailboxFollowUpReadsTrustedAndReloadedSendNames(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	h := anaphoricClassifierHandler(func(string) (string, error) { return `{"deliver":"mailbox"}`, nil })
	histories := [][]agent.ConversationEntry{
		{
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
		},
		{
			{Role: "user", Content: "发送给 znsoft@163.com"},
			{Role: "assistant", ToolCalls: []map[string]string{{"name": "mailbox_send"}}},
		},
		{
			{Role: "user", Content: "发送给 znsoft@163.com"},
			{Role: "tool", Content: map[string]interface{}{"name": "mailbox_send", "arguments": `{"to":["znsoft@163.com"]}`}},
		},
	}
	for i, history := range histories {
		if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "再发一份给他", history); out == nil {
			t.Fatalf("history shape %d did not read as a mailbox send", i)
		}
	}
}

func TestAnaphoricMailboxFollowUpReachesThePlan(t *testing.T) {
	current := *liveDataVisualClassification()
	h := &IMMessageHandler{
		registry:                    NewToolRegistry(),
		anaphoricDeliveryClassifier: func(string) (string, error) { return `{"deliver":"mailbox"}`, nil },
	}
	registerBuiltinTools(h.registry, h)
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Chongqing: light rain, 18C", nil }
	out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", mailboxSendHistory())
	if out == nil {
		t.Fatal("rewrite returned nil")
	}
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

	current := *liveDataVisualClassification()
	h := &IMMessageHandler{
		registry:                    NewToolRegistry(),
		anaphoricDeliveryClassifier: func(string) (string, error) { return `{"deliver":"mailbox"}`, nil },
	}
	registerBuiltinTools(h.registry, h)
	h.semanticTrustedWebSearch = func(_, _ string) (string, error) { return "Chongqing: light rain, 18C", nil }
	rewritten := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", mailboxSendHistory())
	if rewritten == nil {
		t.Fatal("rewrite returned nil")
	}
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

func TestAnaphoricMailboxHistorySurvivesToolHeavyTurns(t *testing.T) {
	// One turn can append dozens of tool rows after the send. The window is
	// counted in user turns, so the send is still visible to the follow-up.
	history := mailboxSendHistory()
	for i := 0; i < 40; i++ {
		history = append(history, agent.ConversationEntry{Role: "tool_result", Content: "tool output"})
	}
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	h := anaphoricClassifierHandler(func(string) (string, error) { return `{"deliver":"mailbox"}`, nil })
	if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", history); out == nil {
		t.Fatal("a tool-heavy previous turn hid the mailbox send")
	}

	// Six user turns later the send no longer arms the rewrite.
	stale := mailboxSendHistory()
	for i := 0; i < 7; i++ {
		stale = append(stale, agent.ConversationEntry{Role: "user", Content: "继续"}, agent.ConversationEntry{Role: "assistant", Content: "好"})
	}
	if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", stale); out != nil {
		t.Fatalf("an old send re-armed the rewrite: %v", out.Labels())
	}
}

func TestAnaphoricMailboxFollowUpLeavesAPendingDraftAlone(t *testing.T) {
	current := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Confidence: 0.95, Layer: 3,
		Secondary: []intent.IntentLabel{intent.LabelLiveDataVisual},
	}
	calls := 0
	h := anaphoricClassifierHandler(func(string) (string, error) {
		calls++
		return `{"deliver":"mailbox"}`, nil
	})
	h.pendingAskUser.Store("user-1", &pendingAskUserState{MailboxDraftID: "draft-1"})
	if out := h.semanticAnaphoricMailboxFollowUp(&current, "user-1", "把重庆的天气也发给他", mailboxSendHistory()); out != nil {
		t.Fatalf("a pending draft was re-armed: %v", out.Labels())
	}
	if calls != 0 {
		t.Fatal("classifier ran while a mailbox draft was pending")
	}
}

func TestMailboxSendAllowedByChannelPolicy(t *testing.T) {
	var nilLoop *LoopContext
	if !nilLoop.mailboxSendAllowed() {
		t.Fatal("a nil loop has no group policy")
	}
	if !(&LoopContext{}).mailboxSendAllowed() {
		t.Fatal("a direct chat allows the mail tool")
	}
	group := &LoopContext{LansengerGroupPermissions: &lansengerGroupPermissionPolicy{}}
	if group.mailboxSendAllowed() {
		t.Fatal("a group chat without an explicit mailbox grant must fail closed")
	}
}
