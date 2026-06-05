package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestImplicitInFlightRecoveryDecisionStartsNewForOrdinaryInput(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_recovery",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "check whether omniroute supports websocket"},
		"check whether omniroute supports websocket",
		slot,
		explicitTaskSlotDecision{},
	)
	if !decision.StartNewTask {
		t.Fatal("expected ordinary input to start a new task instead of binding recovery slot")
	}
	if decision.DismissSlotID != slot.SlotID {
		t.Fatalf("DismissSlotID = %q, want %q", decision.DismissSlotID, slot.SlotID)
	}
	if decision.ResumeSlotID != "" {
		t.Fatalf("ResumeSlotID = %q, want empty", decision.ResumeSlotID)
	}
}

func TestImplicitInFlightRecoveryDecisionResumesOnlyForExplicitResume(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "continue this"},
		"continue this",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.ResumeSlotID != slot.SlotID {
		t.Fatalf("ResumeSlotID = %q, want %q", decision.ResumeSlotID, slot.SlotID)
	}
	if decision.StartNewTask {
		t.Fatal("did not expect explicit resume input to start a new task")
	}
}

func TestBuildUnfinishedSlotHintUsesConfiguredChineseLanguage(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	setAgentViewLang("zh-Hans")
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	slot := &agent.UnfinishedTaskSlot{SlotID: "slot-zh", LastTask: "继续优化系统性能"}
	got := buildUnfinishedSlotHint(slot)

	if strings.Contains(got, "Detected an unfinished task") || strings.Contains(got, "Choose resume") {
		t.Fatalf("hint leaked English text: %q", got)
	}
	if !strings.Contains(got, "检测到未完成任务") || !strings.Contains(got, "继续上次任务") {
		t.Fatalf("hint not localized to Chinese: %q", got)
	}
}

func TestBuildResumeSlotActionsUseConfiguredChineseLanguage(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	setAgentViewLang("zh-Hans")
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	actions := buildResumeSlotActions(&agent.UnfinishedTaskSlot{SlotID: "slot-zh"})
	if len(actions) != 2 {
		t.Fatalf("actions len = %d, want 2", len(actions))
	}
	if actions[0].Label != "继续上次任务" || actions[1].Label != "开始新任务" {
		t.Fatalf("actions not localized: %#v", actions)
	}
}

func TestUnfinishedSlotPayloadUsesMessageLanguage(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{SlotID: "slot-hant", LastTask: "繼續整理報告", Summary: "Previous task stopped making progress and was moved to recovery."}

	hint := buildUnfinishedSlotHintWithLang(slot, "zh-Hant")
	if strings.Contains(hint, "Detected an unfinished task") || !strings.Contains(hint, "偵測到未完成任務") {
		t.Fatalf("hint not localized with message language: %q", hint)
	}
	payload := buildUnfinishedTaskPayloadWithLang(slot, "zh-Hant")
	if payload == nil || len(payload.Actions) != 2 {
		t.Fatalf("payload/actions missing: %#v", payload)
	}
	if payload.Actions[0].Label != "繼續上次任務" || payload.Actions[1].Label != "開始新任務" {
		t.Fatalf("actions not localized with message language: %#v", payload.Actions)
	}
	if payload.Summary != "上次任務停止推進，已移入恢復狀態。" {
		t.Fatalf("summary not localized with message language: %q", payload.Summary)
	}
}

func TestUnfinishedSlotFallbackTitleLocalizesKnownSummary(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{SlotID: "slot-summary", Summary: "Previous task stopped making progress and was moved to recovery."}

	hint := buildUnfinishedSlotHintWithLang(slot, "zh-Hans")
	if strings.Contains(hint, "Previous task stopped") || strings.Contains(hint, "Detected an unfinished task") {
		t.Fatalf("hint leaked English fallback summary: %q", hint)
	}
	if !strings.Contains(hint, "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb") {
		t.Fatalf("hint did not localize fallback title: %q", hint)
	}

	payload := buildUnfinishedTaskPayloadWithLang(slot, "zh-Hans")
	if payload == nil || strings.Contains(payload.Title, "Previous task stopped") {
		t.Fatalf("payload title leaked English fallback summary: %#v", payload)
	}
	if payload.Title != "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb\uff0c\u5df2\u79fb\u5165\u6062\u590d\u72b6\u6001\u3002" {
		t.Fatalf("payload title = %q", payload.Title)
	}
}

func TestUnfinishedSlotResumeContextUsesMessageLanguage(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		LastTask: "resume docs",
		Summary:  "Previous task stopped making progress and was moved to recovery.",
	}

	zh := buildUnfinishedSlotResumeContextWithLang(slot, "zh-Hans")
	if strings.Contains(zh, "Explicit unfinished task resume") || strings.Contains(zh, "Current progress") {
		t.Fatalf("resume context leaked English text: %q", zh)
	}
	if !strings.Contains(zh, "\u663e\u5f0f\u6062\u590d\u672a\u5b8c\u6210\u4efb\u52a1") || !strings.Contains(zh, "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb") {
		t.Fatalf("resume context not localized to Chinese: %q", zh)
	}

	en := buildUnfinishedSlotResumeContextWithLang(slot, "en")
	if !strings.Contains(en, "Explicit unfinished task resume") || !strings.Contains(en, "Previous task stopped making progress") {
		t.Fatalf("resume context not localized to English: %q", en)
	}
}

func TestPreviousTaskDismissedMessageUsesMessageLanguage(t *testing.T) {
	if got := localizedPreviousTaskDismissedMessage("zh-Hans"); got != "已忽略上次未完成任务。请告诉我新的任务。" {
		t.Fatalf("zh-Hans message = %q", got)
	}
	if got := localizedPreviousTaskDismissedMessage("zh-Hant"); got != "已忽略上次未完成任務。請告訴我新的任務。" {
		t.Fatalf("zh-Hant message = %q", got)
	}
	if got := localizedPreviousTaskDismissedMessage("en-US"); got != "Previous task dismissed. Tell me the new task." {
		t.Fatalf("en-US message = %q", got)
	}
}

func TestStartNewTaskUIActionReplaysSavedOriginalTask(t *testing.T) {
	userID := "desktop-user"
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	h.pendingSlotUserText.Store(userID, &pendingSlotText{Text: "成都天气", Timestamp: time.Now()})

	msg := &IMUserMessage{
		UserID:        userID,
		Platform:      "desktop",
		Text:          "开始新任务",
		StartNewTask:  true,
		DismissSlotID: "slot-1",
		UIAction:      true,
	}
	trimmed := strings.TrimSpace(msg.Text)
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "old task"},
		{Role: "assistant", Content: "old progress"},
	}
	unfinishedSlot := &agent.UnfinishedTaskSlot{SlotID: "slot-1", UserID: userID, LastTask: "old task"}
	h.memory.UpsertUnfinishedSlot(userID, unfinishedSlot)
	h.memory.BindUnfinishedSlot(userID, "slot-1")

	freshTask, resp, handled := h.applyExplicitTaskSlotAction(
		msg,
		&trimmed,
		explicitTaskSlotDecision{StartNewTask: true, DismissSlotID: "slot-1"},
		&entries,
		&unfinishedSlot,
	)
	if handled {
		t.Fatalf("handled = true with response %#v, want replay to continue into new task flow", resp)
	}
	if !freshTask {
		t.Fatal("freshTask = false, want true")
	}
	if msg.Text != "成都天气" || trimmed != "成都天气" {
		t.Fatalf("replayed text = msg %q trimmed %q, want 成都天气", msg.Text, trimmed)
	}
	if msg.UIAction {
		t.Fatal("UIAction remained true, want false after replay")
	}
	if entries != nil || unfinishedSlot != nil {
		t.Fatalf("old context not cleared: entries=%#v slot=%#v", entries, unfinishedSlot)
	}
	if slot := h.memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("memory unfinished slot = %#v, want nil", slot)
	}
}

func TestStartNewTaskUIActionFallsBackWhenSavedTextUnavailable(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "expired", value: &pendingSlotText{Text: "成都天气", Timestamp: time.Now().Add(-11 * time.Minute)}},
		{name: "invalid type", value: "成都天气"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			userID := "desktop-user-" + tc.name
			h := &IMMessageHandler{memory: agent.NewConversationMemory()}
			h.pendingSlotUserText.Store(userID, tc.value)

			msg := &IMUserMessage{
				UserID:        userID,
				Platform:      "desktop",
				Text:          "开始新任务",
				StartNewTask:  true,
				DismissSlotID: "slot-1",
				UIAction:      true,
				Lang:          "zh-Hans",
			}
			trimmed := strings.TrimSpace(msg.Text)
			entries := []agent.ConversationEntry{{Role: "user", Content: "old task"}, {Role: "assistant", Content: "old progress"}}
			unfinishedSlot := &agent.UnfinishedTaskSlot{SlotID: "slot-1", UserID: userID, LastTask: "old task"}

			_, resp, handled := h.applyExplicitTaskSlotAction(
				msg,
				&trimmed,
				explicitTaskSlotDecision{StartNewTask: true, DismissSlotID: "slot-1"},
				&entries,
				&unfinishedSlot,
			)
			if !handled || resp == nil {
				t.Fatalf("handled=%v resp=%#v, want fallback response", handled, resp)
			}
			if resp.Text != "已忽略上次未完成任务。请告诉我新的任务。" {
				t.Fatalf("resp.Text = %q", resp.Text)
			}
			if msg.Text != "开始新任务" || trimmed != "开始新任务" {
				t.Fatalf("unexpected replay: msg=%q trimmed=%q", msg.Text, trimmed)
			}
		})
	}
}

func TestRecoverableSessionMessagesUseMessageLanguage(t *testing.T) {
	if got := localizedRecoverableSessionDismissedMessage("zh-Hant"); got != "已忽略可恢復會話。" {
		t.Fatalf("zh-Hant dismiss message = %q", got)
	}
	if got := localizedRecoverableSessionResumeDisabledMessage("zh-Hans"); !strings.Contains(got, "已禁用外部编码会话恢复") || !strings.Contains(got, "CodingSubAgent") {
		t.Fatalf("zh-Hans disabled message = %q", got)
	}
	if got := localizedRecoverableSessionUnavailableMessage("en-US"); got != "There is no recoverable session available, or the session does not support resume." {
		t.Fatalf("en-US unavailable message = %q", got)
	}
}

func TestRecoverableSessionPayloadLocalizesKnownProgress(t *testing.T) {
	session := &RemoteSession{
		ID:     "sess-progress",
		Tool:   "codex",
		Status: SessionExited,
		ResumeContext: &SessionResumeContext{
			OriginalTask:    "continue work",
			LastProgress:    "Previous task stopped making progress and was moved to recovery.",
			ResumeSessionID: "resume-1",
		},
	}

	payload := buildRecoverableSessionPayloadWithLang(session, "zh-Hans")
	if payload == nil {
		t.Fatal("payload nil")
	}
	if strings.Contains(payload.Summary, "Previous task stopped") || strings.Contains(payload.LastProgress, "Previous task stopped") {
		t.Fatalf("payload leaked English progress: %#v", payload)
	}
	if !strings.Contains(payload.Summary, "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb") || !strings.Contains(payload.LastProgress, "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb") {
		t.Fatalf("payload did not localize progress: %#v", payload)
	}
	if len(payload.Actions) != 2 || payload.Actions[0].Label != "\u6062\u590d\u4f1a\u8bdd" {
		t.Fatalf("actions not localized: %#v", payload.Actions)
	}
}

func TestImplicitInFlightRecoveryDecisionResumesForChineseContinue(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "\u7ee7\u7eed"},
		"\u7ee7\u7eed",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.ResumeSlotID != slot.SlotID {
		t.Fatalf("ResumeSlotID = %q, want %q", decision.ResumeSlotID, slot.SlotID)
	}
	if decision.StartNewTask {
		t.Fatal("did not expect Chinese resume input to start a new task")
	}
}

func TestImplicitInFlightRecoveryDecisionDoesNotOverrideExplicitAction(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_recovery",
	}
	original := explicitTaskSlotDecision{ResumeSlotID: "slot-explicit"}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "new request"},
		"new request",
		slot,
		original,
	)
	if decision.ResumeSlotID != original.ResumeSlotID || decision.StartNewTask {
		t.Fatalf("decision = %#v, want explicit resume preserved", decision)
	}
}

func TestImplicitInFlightRecoveryDecisionIgnoresOtherSlotSources(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-max-rounds",
		Source: "max_rounds",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "check whether omniroute supports websocket"},
		"check whether omniroute supports websocket",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.StartNewTask || decision.DismissSlotID != "" || decision.ResumeSlotID != "" {
		t.Fatalf("decision = %#v, want ordinary unfinished slot untouched", decision)
	}
}

func TestImplicitInFlightRecoveryDecisionKeepsEmptyInputNeutral(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user"},
		"",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.StartNewTask || decision.DismissSlotID != "" || decision.ResumeSlotID != "" {
		t.Fatalf("decision = %#v, want empty input to leave recovery hint available", decision)
	}
}

func TestShouldRecoverInFlightMarkerWaitsForActiveLoop(t *testing.T) {
	msg := IMUserMessage{UserID: "desktop-user", Text: "new request"}
	if shouldRecoverInFlightMarker(msg, nil, &LoopContext{ID: "active-loop"}) {
		t.Fatal("did not expect in-flight marker recovery while an agent loop is still active")
	}
}

func TestShouldRecoverInFlightMarkerOnlyForForegroundWithoutSlot(t *testing.T) {
	msg := IMUserMessage{UserID: "desktop-user", Text: "new request"}
	if !shouldRecoverInFlightMarker(msg, nil, nil) {
		t.Fatal("expected foreground message with no active slot or loop to recover marker")
	}
	if shouldRecoverInFlightMarker(IMUserMessage{UserID: "desktop-user", IsBackground: true}, nil, nil) {
		t.Fatal("did not expect background message to recover foreground marker")
	}
	if shouldRecoverInFlightMarker(msg, &agent.UnfinishedTaskSlot{SlotID: "existing"}, nil) {
		t.Fatal("did not expect recovery when an unfinished slot already exists")
	}
}

func TestPrepareIMMessagePreflightDoesNotRecoverWhileSameSessionLoopActive(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	memory.SetInFlightTask(desktopUserID, "old interrupted task", `D:\old-project`)

	h := &IMMessageHandler{memory: memory}
	active := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(desktopUserID, active)

	msg := IMUserMessage{UserID: desktopUserID, Text: "北京天气"}
	trimmed := msg.Text
	result := h.prepareIMMessagePreflight(&msg, &trimmed)
	if result.UnfinishedSlot != nil {
		t.Fatalf("unfinished slot = %#v, want nil while same session loop active", result.UnfinishedSlot)
	}
	task, project := memory.ConsumeInFlightTask(desktopUserID)
	if task != "old interrupted task" || project != `D:\old-project` {
		t.Fatalf("in-flight marker consumed unexpectedly: task=%q project=%q", task, project)
	}
}
