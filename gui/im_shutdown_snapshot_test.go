package main
import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func newShutdownSnapshotHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)
	return &IMMessageHandler{memory: memory}
}

func TestShutdownSnapshotCreatesSlotForActiveLoop(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := desktopUserID
	h.memory.SetInFlightTask(userID, "stale marker", "")
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "整理下载目录里的文件"},
		{Role: "assistant", Content: "已开始整理"},
	})
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	h.getSessionLoop(userID).userText = "整理下载目录里的文件"

	h.snapshotInterruptedSessionsForShutdown()

	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil {
		t.Fatal("expected unfinished slot for active loop session")
	}
	if slot.Source != agent.UnfinishedTaskSlotSourceAppExit {
		t.Fatalf("slot source = %q, want %q", slot.Source, agent.UnfinishedTaskSlotSourceAppExit)
	}
	if slot.Status != agent.UnfinishedTaskSlotStatusInterrupted {
		t.Fatalf("slot status = %q, want interrupted", slot.Status)
	}
	if slot.LastTask != "整理下载目录里的文件" {
		t.Fatalf("slot LastTask = %q", slot.LastTask)
	}
	if !loop.IsCancelled() {
		t.Fatal("expected active loop to be cancelled")
	}
	if task, _ := h.memory.ConsumeInFlightTask(userID); task != "" {
		t.Fatalf("stale in-flight marker not cleared: %q", task)
	}
}

func TestShutdownSnapshotKeepsExistingPendingSlot(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := desktopUserID
	existing := &agent.UnfinishedTaskSlot{SlotID: "slot-existing", UserID: userID, LastTask: "old undecided task"}
	h.memory.UpsertUnfinishedSlot(userID, existing)
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	h.getSessionLoop(userID).userText = "new task"

	h.snapshotInterruptedSessionsForShutdown()

	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil || slot.SlotID != "slot-existing" {
		t.Fatalf("existing pending slot overwritten: %#v", slot)
	}
	if !loop.IsCancelled() {
		t.Fatal("expected active loop to be cancelled even when slot creation is skipped")
	}
}

func TestShutdownSnapshotIgnoresIdleAndEmptySessions(t *testing.T) {
	h := newShutdownSnapshotHandler(t)

	// Idle session: no loop at all.
	h.getSessionLoop("desktop-user:idle")

	// Active loop but no task text anywhere.
	emptyUser := "desktop-user:empty"
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(emptyUser, loop)

	h.snapshotInterruptedSessionsForShutdown()

	if slot := h.memory.GetUnfinishedSlot("desktop-user:idle"); slot != nil {
		t.Fatalf("idle session got slot: %#v", slot)
	}
	if slot := h.memory.GetUnfinishedSlot(emptyUser); slot != nil {
		t.Fatalf("textless session got slot: %#v", slot)
	}
	if !loop.IsCancelled() {
		t.Fatal("expected textless active loop to be cancelled")
	}
}

func TestShutdownSnapshotUsesBackgroundLoopDescription(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := desktopUserID
	loop := NewBackgroundLoopContext("bg-coding-1", SlotKindCoding, "后台重构核心模块", 5, nil, nil)
	h.setSessionLoopCtx(userID, loop)

	h.snapshotInterruptedSessionsForShutdown()

	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil || slot.LastTask != "后台重构核心模块" {
		t.Fatalf("slot = %#v, want background description as task", slot)
	}
}

func TestAppExitAutoResumeBindsWithoutAsking(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-1",
		Source: agent.UnfinishedTaskSlotSourceAppExit,
		Status: agent.UnfinishedTaskSlotStatusInterrupted,
	}
	decision := applyAppExitAutoResumeDecision(
		IMUserMessage{UserID: desktopUserID, Text: "把刚才那个文件再改一下"},
		"把刚才那个文件再改一下",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.ResumeSlotID != slot.SlotID {
		t.Fatalf("ResumeSlotID = %q, want auto-resume %q", decision.ResumeSlotID, slot.SlotID)
	}
	if decision.StartNewTask {
		t.Fatal("app-exit slot must not wipe context by starting a new task")
	}
}

func TestAppExitAutoResumeLeavesExplicitAndBackgroundUntouched(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "shutdown-2",
		Source: agent.UnfinishedTaskSlotSourceAppExit,
		Status: agent.UnfinishedTaskSlotStatusInterrupted,
	}

	// Explicit user decision wins.
	explicit := explicitTaskSlotDecision{StartNewTask: true}
	decision := applyAppExitAutoResumeDecision(IMUserMessage{UserID: desktopUserID, Text: "新任务"}, "新任务", slot, explicit)
	if decision.ResumeSlotID != "" || !decision.StartNewTask {
		t.Fatalf("explicit start-new overwritten: %#v", decision)
	}

	// Slot action commands pass through.
	decision = applyAppExitAutoResumeDecision(IMUserMessage{UserID: desktopUserID, Text: "__dismiss_unfinished__ shutdown-2"}, "__dismiss_unfinished__ shutdown-2", slot, explicitTaskSlotDecision{})
	if decision.ResumeSlotID != "" {
		t.Fatalf("slot action command turned into resume: %#v", decision)
	}

	// Background turns never auto-resume.
	decision = applyAppExitAutoResumeDecision(IMUserMessage{UserID: desktopUserID, Text: "continue", IsBackground: true}, "continue", slot, explicitTaskSlotDecision{})
	if decision.ResumeSlotID != "" {
		t.Fatalf("background turn auto-resumed: %#v", decision)
	}

	// Other sources keep the existing ask-first behavior.
	other := &agent.UnfinishedTaskSlot{SlotID: "s3", Source: agent.UnfinishedTaskSlotSourceMaxRounds}
	decision = applyAppExitAutoResumeDecision(IMUserMessage{UserID: desktopUserID, Text: "go on"}, "go on", other, explicitTaskSlotDecision{})
	if decision.ResumeSlotID != "" {
		t.Fatalf("non-app-exit slot auto-resumed: %#v", decision)
	}

	// Already-decided slots (resumed/completed) are not re-bound.
	resumed := &agent.UnfinishedTaskSlot{SlotID: "s4", Source: agent.UnfinishedTaskSlotSourceAppExit, Status: agent.UnfinishedTaskSlotStatusResumed}
	decision = applyAppExitAutoResumeDecision(IMUserMessage{UserID: desktopUserID, Text: "next step"}, "next step", resumed, explicitTaskSlotDecision{})
	if decision.ResumeSlotID != "" {
		t.Fatalf("resumed slot re-bound: %#v", decision)
	}
}

func TestAppExitSlotNeverShowsHintBanner(t *testing.T) {
	h := &IMMessageHandler{}
	slot := &agent.UnfinishedTaskSlot{
		SlotID:   "shutdown-3",
		Source:   agent.UnfinishedTaskSlotSourceAppExit,
		Status:   agent.UnfinishedTaskSlotStatusInterrupted,
		LastTask: "继续整理文件",
	}
	resp, handled := h.maybeReturnUnfinishedSlotHint(IMUserMessage{UserID: desktopUserID, Text: "你好"}, "你好", false, explicitTaskSlotDecision{}, slot)
	if handled || resp != nil {
		t.Fatalf("app-exit slot produced a hint banner: handled=%v resp=%#v", handled, resp)
	}
}
