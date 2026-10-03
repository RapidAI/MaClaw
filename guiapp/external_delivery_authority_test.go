package guiapp

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func externalDeliverySlot(toolName, sideEffect, lastTask string) *agent.UnfinishedTaskSlot {
	return &agent.UnfinishedTaskSlot{
		SlotID:          "slot-external",
		UserID:          "user",
		Status:          agent.UnfinishedTaskSlotStatusInterrupted,
		Source:          agent.UnfinishedTaskSlotSourceInFlightLeaseExpired,
		LastToolName:    toolName,
		SideEffectState: sideEffect,
		RecoveryMode:    "requires_review",
		LastTask:        lastTask,
	}
}

func TestExplicitResumeOfUnreviewedExternalDeliveryDoesNotBind(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", externalDeliverySlot("im_message", "external_uncertain", "发到微信"))
	h := &IMMessageHandler{memory: memory}
	trimmed := "继续上次未完成任务"
	entries := []agent.ConversationEntry(nil)
	slot := memory.GetUnfinishedSlot("user")
	msg := &IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: trimmed, ResumeSlotID: "slot-external"}
	handled, resp, stop := h.applyExplicitTaskSlotAction(msg, &trimmed, explicitTaskSlotDecision{ResumeSlotID: "slot-external"}, &entries, &slot)
	if !handled || !stop || resp == nil {
		t.Fatalf("handled=%v stop=%v resp=%#v", handled, stop, resp)
	}
	if resp.Error != "semantic_external_delivery_review_required" || resp.UnfinishedTask != nil {
		t.Fatalf("resp=%#v", resp)
	}
	requireDismissWithoutResume(t, resp)
	if strings.Contains(resp.Text, "当前能力目录未覆盖") || !strings.Contains(resp.Text, "weixin") || !strings.Contains(resp.Text, "im_message") {
		t.Fatalf("text=%q", resp.Text)
	}
	if active := memory.ActiveUnfinishedSlot("user"); active != nil {
		t.Fatalf("external delivery slot was bound: %#v", active)
	}
	persisted := memory.GetUnfinishedSlot("user")
	if persisted == nil || persisted.Status == agent.UnfinishedTaskSlotStatusResumed {
		t.Fatalf("slot=%#v", persisted)
	}
}

func TestExplicitResumeStillBindsLocalAndGenerateSlots(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		effect   string
	}{
		{name: "local write", toolName: "write_file", effect: "local_committed"},
		{name: "generate", toolName: "generate_pdf", effect: "external_uncertain"},
		{name: "search", toolName: "web_search", effect: "external_uncertain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			memory := agent.NewConversationMemory()
			slotIn := externalDeliverySlot(tc.toolName, tc.effect, "北京天气，生成pdf")
			slotIn.SlotID = "slot-other"
			memory.UpsertUnfinishedSlot("user", slotIn)
			h := &IMMessageHandler{memory: memory}
			trimmed := "继续上次未完成任务"
			entries := []agent.ConversationEntry(nil)
			slot := memory.GetUnfinishedSlot("user")
			msg := &IMUserMessage{UserID: "user", Platform: "desktop", Text: trimmed, ResumeSlotID: "slot-other"}
			handled, resp, stop := h.applyExplicitTaskSlotAction(msg, &trimmed, explicitTaskSlotDecision{ResumeSlotID: "slot-other"}, &entries, &slot)
			if handled || stop || resp != nil {
				t.Fatalf("handled=%v stop=%v resp=%#v", handled, stop, resp)
			}
			if active := memory.ActiveUnfinishedSlot("user"); active == nil || active.Status != agent.UnfinishedTaskSlotStatusResumed {
				t.Fatalf("slot was not bound: %#v", active)
			}
		})
	}
}

func TestSemanticContinuationDoesNotBindUnreviewedExternalDelivery(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", externalDeliverySlot("send_file", "unknown", "发到微信"))
	h := &IMMessageHandler{memory: memory}
	slot := memory.GetUnfinishedSlot("user")
	h.bindSemanticContinuationSlot("user", &slot)
	if slot == nil {
		t.Fatal("continuation cleared the slot pointer")
	}
	if active := memory.ActiveUnfinishedSlot("user"); active != nil {
		t.Fatalf("continuation bound the external send: %#v", active)
	}
}

func TestUnfinishedHintForExternalDeliveryDoesNotOfferResume(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", externalDeliverySlot("send_to_im", "", "发到微信"))
	h := &IMMessageHandler{memory: memory}
	slot := memory.GetUnfinishedSlot("user")
	resp, handled := h.maybeReturnUnfinishedSlotHint(
		IMUserMessage{UserID: "user", Platform: "desktop", Text: "继续"},
		"继续",
		false,
		explicitTaskSlotDecision{},
		slot,
	)
	if !handled || resp == nil || resp.Error != "semantic_external_delivery_review_required" || resp.UnfinishedTask != nil {
		t.Fatalf("handled=%v resp=%#v", handled, resp)
	}
	requireDismissWithoutResume(t, resp)
	if strings.Contains(resp.Text, "选择") {
		t.Fatalf("review offered a resume choice: %q", resp.Text)
	}
}

func TestUnfinishedHintReviewsExternalDeliveryAcrossProjects(t *testing.T) {
	memory := agent.NewConversationMemory()
	slotIn := externalDeliverySlot("im_message", "external_uncertain", "发到微信")
	h := &IMMessageHandler{memory: memory}
	current := h.effectiveWorkingDirForUser("user")
	if strings.TrimSpace(current) == "" {
		t.Fatal("working directory is empty, so a project mismatch cannot be distinguished")
	}
	slotIn.ProjectPath = current + "-other-project"
	memory.UpsertUnfinishedSlot("user", slotIn)
	slot := memory.GetUnfinishedSlot("user")
	resp, handled := h.maybeReturnUnfinishedSlotHint(
		IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: "继续"},
		"继续",
		false,
		explicitTaskSlotDecision{},
		slot,
	)
	if !handled || resp == nil || resp.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v resp=%#v", handled, resp)
	}
	requireDismissWithoutResume(t, resp)
}

func TestSameProcessRecoveryKeepsExternalSendEvidence(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	if err := memory.PersistInFlightCheckpoint("user", []agent.ConversationEntry{{Role: "user", Content: "发到微信"}}, "发到微信", "", "run-external", agent.InFlightCheckpoint{
		Sequence:        1,
		LastToolName:    "im_message",
		SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	slot := h.recoverInterruptedTaskSlot("user", nil)
	if slot == nil || slot.LastToolName != "im_message" || slot.SideEffectState != "external_uncertain" || slot.RecoveryMode != "requires_review" {
		t.Fatalf("recovered slot dropped the send evidence: %#v", slot)
	}
	if slot.EvidenceScopeKey == "" {
		t.Fatal("recovered slot dropped the run scope")
	}
	trimmed := "继续上次未完成任务"
	msg := &IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: trimmed}
	result := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            msg,
		Trimmed:            &trimmed,
		UnfinishedSlot:     memory.GetUnfinishedSlot("user"),
		EntriesBeforeClear: memory.Load("user"),
	})
	if !result.Handled || result.Response == nil || result.Response.Error != "semantic_external_delivery_review_required" || result.Response.UnfinishedTask != nil {
		t.Fatalf("handled=%v resp=%#v", result.Handled, result.Response)
	}
	if len(result.Response.Actions) != 1 || result.Response.Actions[0].Command != "__dismiss_unfinished__ "+slot.SlotID {
		t.Fatalf("actions=%#v", result.Response.Actions)
	}
	if got := memory.GetUnfinishedSlot("user"); got == nil || got.LastToolName != "im_message" {
		t.Fatalf("slot=%#v", got)
	}
	if task, _ := memory.ConsumeInFlightTask("user"); task != "" {
		t.Fatalf("marker remained after recovery: %q", task)
	}
}

func TestLaterCheckpointKeepsExternalSendBeforePlanning(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	if err := memory.PersistInFlightCheckpoint("user", []agent.ConversationEntry{{Role: "user", Content: "发到微信"}}, "发到微信", "", "run-external", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "im_message", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.PersistInFlightCheckpoint("user", []agent.ConversationEntry{
		{Role: "user", Content: "发到微信"},
		{Role: "tool", Content: "searched", ToolName: "bash"},
	}, "发到微信", "", "run-external", agent.InFlightCheckpoint{
		Sequence: 2, LastToolName: "bash", SideEffectState: "local_committed",
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{memory: memory}
	slot := h.recoverInterruptedTaskSlot("user", nil)
	if slot == nil || slot.LastToolName != "im_message" || slot.SideEffectState != "external_uncertain" {
		t.Fatalf("recovered slot = %#v", slot)
	}
	trimmed := "继续上次未完成任务"
	msg := &IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: trimmed}
	result := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            msg,
		Trimmed:            &trimmed,
		UnfinishedSlot:     memory.GetUnfinishedSlot("user"),
		EntriesBeforeClear: memory.Load("user"),
	})
	if !result.Handled || result.Response == nil || result.Response.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v resp=%#v", result.Handled, result.Response)
	}
}

func TestUnreviewedExternalDeliveryStopsBeforeTaskContext(t *testing.T) {
	for _, history := range [][]agent.ConversationEntry{
		nil,
		{{Role: "user", Content: "发到微信"}},
	} {
		memory := agent.NewConversationMemory()
		memory.UpsertUnfinishedSlot("user", externalDeliverySlot("im_message", "external_uncertain", "发到微信"))
		h := &IMMessageHandler{memory: memory}
		trimmed := "继续上次未完成任务"
		msg := &IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: trimmed}
		slot := memory.GetUnfinishedSlot("user")
		result := h.resolveIMEntryContext(imEntryContextOptions{
			Message:            msg,
			Trimmed:            &trimmed,
			UnfinishedSlot:     slot,
			EntriesBeforeClear: history,
		})
		if !result.Handled || result.Response == nil || result.Response.Error != "semantic_external_delivery_review_required" || result.FreshTask {
			t.Fatalf("history=%d handled=%v fresh=%v resp=%#v", len(history), result.Handled, result.FreshTask, result.Response)
		}
		requireDismissWithoutResume(t, result.Response)
		if memory.GetUnfinishedSlot("user") == nil {
			t.Fatal("task context dismissed the unreviewed send")
		}
	}

	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user-bound", externalDeliverySlot("im_message", "external_uncertain", "发到微信"))
	if !memory.BindUnfinishedSlot("user-bound", "slot-external") {
		t.Fatal("bind failed")
	}
	h := &IMMessageHandler{memory: memory}
	trimmed := "北京天气"
	msg := &IMUserMessage{UserID: "user-bound", Platform: "desktop", Lang: "zh", Text: trimmed}
	result := h.resolveIMEntryContext(imEntryContextOptions{
		Message:            msg,
		Trimmed:            &trimmed,
		UnfinishedSlot:     memory.GetUnfinishedSlot("user-bound"),
		EntriesBeforeClear: []agent.ConversationEntry{{Role: "user", Content: "发到微信"}},
	})
	if !result.Handled || result.Response == nil || result.Response.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v resp=%#v", result.Handled, result.Response)
	}
	if memory.GetUnfinishedSlot("user-bound") == nil {
		t.Fatal("task context dismissed the resumed external send")
	}
}

func TestExternalDeliveryReviewDismissDoesNotReplayPendingText(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", externalDeliverySlot("im_message", "external_uncertain", "发到微信"))
	h := &IMMessageHandler{memory: memory}
	h.pendingSlotUserText.Store("user", &pendingSlotText{Text: "发到微信", Timestamp: time.Now()})
	slot := memory.GetUnfinishedSlot("user")
	resp, handled := h.maybeReturnUnfinishedSlotHint(
		IMUserMessage{UserID: "user", Platform: "desktop", Lang: "zh", Text: "继续"},
		"继续",
		false,
		explicitTaskSlotDecision{},
		slot,
	)
	if !handled || resp == nil || resp.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("handled=%v resp=%#v", handled, resp)
	}
	if _, ok := h.pendingSlotUserText.Load("user"); ok {
		t.Fatal("review left an utterance that dismiss would replay")
	}
	trimmed := "忽略上次未完成任务"
	entries := []agent.ConversationEntry{{Role: "user", Content: "发到微信"}, {Role: "assistant", Content: "old"}}
	msg := &IMUserMessage{
		UserID:        "user",
		Platform:      "desktop",
		Lang:          "zh",
		Text:          trimmed,
		StartNewTask:  true,
		UIAction:      true,
		DismissSlotID: "slot-external",
	}
	handled, resp, stop := h.applyExplicitTaskSlotAction(msg, &trimmed, explicitTaskSlotDecision{StartNewTask: true, DismissSlotID: "slot-external"}, &entries, &slot)
	if !handled || !stop || resp == nil {
		t.Fatalf("handled=%v stop=%v resp=%#v", handled, stop, resp)
	}
	if msg.Text != "忽略上次未完成任务" || !strings.Contains(resp.Text, "已忽略") {
		t.Fatalf("msg=%q text=%q", msg.Text, resp.Text)
	}
	if memory.GetUnfinishedSlot("user") != nil {
		t.Fatal("dismiss left the external delivery slot")
	}
}

func TestUnfinishedHintStillSuppressesMismatchedLocalSlot(t *testing.T) {
	memory := agent.NewConversationMemory()
	slotIn := externalDeliverySlot("write_file", "local_committed", "编辑文件")
	h := &IMMessageHandler{memory: memory}
	current := h.effectiveWorkingDirForUser("user")
	if strings.TrimSpace(current) == "" {
		t.Fatal("working directory is empty, so a project mismatch cannot be distinguished")
	}
	slotIn.ProjectPath = current + "-other-project"
	memory.UpsertUnfinishedSlot("user", slotIn)
	slot := memory.GetUnfinishedSlot("user")
	resp, handled := h.maybeReturnUnfinishedSlotHint(
		IMUserMessage{UserID: "user", Platform: "desktop", Text: "继续"},
		"继续",
		false,
		explicitTaskSlotDecision{},
		slot,
	)
	if handled || resp != nil {
		t.Fatalf("mismatched local slot was not suppressed: handled=%v resp=%#v", handled, resp)
	}
}

func TestBoundExternalDeliveryStopsBeforePlanning(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user-bound", externalDeliverySlot("im_message", "external_uncertain", "发到微信"))
	if !memory.BindUnfinishedSlot("user-bound", "slot-external") {
		t.Fatal("bind failed")
	}
	h := &IMMessageHandler{memory: memory}
	resp := h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message: IMUserMessage{UserID: "user-bound", Platform: "desktop", Text: "继续上次未完成任务"},
		Trimmed: "继续上次未完成任务",
	})
	if resp == nil || resp.Error != "semantic_external_delivery_review_required" {
		t.Fatalf("resp=%#v", resp)
	}
	requireDismissWithoutResume(t, resp)
	if strings.Contains(resp.Text, "当前能力目录未覆盖") {
		t.Fatalf("text=%q", resp.Text)
	}
}

func TestDesktopWeixinDestinationRefusesBeforePlanning(t *testing.T) {
	h := &IMMessageHandler{}
	resp := h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message: IMUserMessage{UserID: "user-dest", Platform: "desktop", Lang: "zh", Text: "发到微信"},
		Trimmed: "发到微信",
	})
	if resp == nil || resp.Error != "semantic_delivery_channel_unsupported" {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Text, "weixin") || !strings.Contains(resp.Text, "im_message") || !strings.Contains(resp.Text, "生成") {
		t.Fatalf("text=%q", resp.Text)
	}
}

func TestFileDestinationGateLeavesSameChannelAndLansenger(t *testing.T) {
	if _, handled := refuseUnpublishedFileDestination(IMUserMessage{Platform: "weixin", Text: "发到微信"}); handled {
		t.Fatal("a weixin turn saying 发到微信 must stay on the own-channel path")
	}
	if _, handled := refuseUnpublishedFileDestination(IMUserMessage{Platform: "desktop", Text: "发到蓝信"}); handled {
		t.Fatal("desktop to lansenger stays on the existing file sender")
	}
	for _, text := range []string{"继续上次未完成任务", "北京天气，生成pdf", "把崇州pdf发到微信"} {
		if _, handled := refuseUnpublishedFileDestination(IMUserMessage{Platform: "desktop", Text: text}); handled {
			t.Fatalf("%q was treated as a closed destination", text)
		}
	}
}

func requireDismissWithoutResume(t *testing.T, resp *IMAgentResponse) {
	t.Helper()
	if resp == nil || resp.UnfinishedTask != nil || resp.UnfinishedSlot != nil {
		t.Fatalf("review projected a slot card: %#v", resp)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Command != "__dismiss_unfinished__ slot-external" {
		t.Fatalf("actions=%#v", resp.Actions)
	}
	if strings.Contains(resp.Actions[0].Command, "__resume_unfinished__") {
		t.Fatalf("resume command leaked: %#v", resp.Actions[0])
	}
}

func TestArtifactAndProducedDocumentKeepTheirHostOutcomes(t *testing.T) {
	missing := semanticHostRejectResponseForManagedSurfaceFailure(semanticUnmetNeedsError{
		Unmet: []tool.UnmetNeed{{NeedID: "need:artifact.deliver.specified_target:e7", ReasonCode: "artifact_dependency_missing"}},
	})
	if missing == nil || missing.Error != "semantic_artifact_dependency_missing" || strings.Contains(missing.Text, "当前能力目录未覆盖") {
		t.Fatalf("missing=%#v", missing)
	}
	stale := semanticHostRejectResponseForManagedSurfaceFailure(fmt.Errorf("produced_document_stale"))
	if stale == nil || stale.Error != "semantic_produced_document_stale" || strings.Contains(stale.Text, "受管工具面暂时不可用") || strings.Contains(stale.Text, "当前能力目录未覆盖") {
		t.Fatalf("stale=%#v", stale)
	}
}
