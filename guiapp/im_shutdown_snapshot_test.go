package guiapp

import (
	"path/filepath"
	"testing"
	"time"

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

func TestShutdownSnapshotQueuesExistingPendingSlot(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := desktopUserID
	existing := &agent.UnfinishedTaskSlot{SlotID: "slot-existing", UserID: userID, LastTask: "old undecided task"}
	h.memory.UpsertUnfinishedSlot(userID, existing)
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	h.getSessionLoop(userID).userText = "new task"

	h.snapshotInterruptedSessionsForShutdown()

	slot := h.memory.GetUnfinishedSlot(userID)
	if slot == nil || slot.LastTask != "new task" {
		t.Fatalf("new shutdown slot = %#v", slot)
	}
	h.memory.DismissUnfinishedSlot(userID, slot.SlotID)
	if slot = h.memory.GetUnfinishedSlot(userID); slot == nil || slot.SlotID != "slot-existing" {
		t.Fatalf("existing pending slot was lost: %#v", slot)
	}
	if !loop.IsCancelled() {
		t.Fatal("expected active loop to be cancelled")
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

func TestShutdownSnapshotDropsMarkerAfterUserStop(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := desktopUserID
	if err := h.memory.PersistInFlightCheckpoint(userID, []agent.ConversationEntry{{Role: "user", Content: "make the video"}}, "make the video", "/project", "chat", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "bash", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	h.getSessionLoop(userID).userText = "make the video"
	loop.MarkUserCancel()
	loop.Cancel()

	h.snapshotInterruptedSessionsForShutdown()

	if slot := h.memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("user stop must not become a shutdown recovery slot: %#v", slot)
	}
	if promoted := h.memory.PromoteRecoverableCheckpoints(time.Now()); promoted != 0 {
		t.Fatalf("user stop promoted %d unfinished slots on next launch", promoted)
	}
	if task, _ := h.memory.ConsumeInFlightTask(userID); task != "" {
		t.Fatalf("user stop left recovery marker %q", task)
	}
}

func TestShutdownSnapshotKeepsMarkerWhenCancelIsNotFromUser(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	userID := "desktop-user:parent-cancel"
	if err := h.memory.PersistInFlightCheckpoint(userID, []agent.ConversationEntry{{Role: "user", Content: "make the video"}}, "make the video", "/project", "chat", agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "bash", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	loop := NewLoopContext("chat", 3, nil)
	h.setSessionLoopCtx(userID, loop)
	h.getSessionLoop(userID).userText = "make the video"
	loop.Cancel()

	h.snapshotInterruptedSessionsForShutdown()

	if slot := h.memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("non-user cancel must not gain an app-exit slot: %#v", slot)
	}
	if promoted := h.memory.PromoteRecoverableCheckpoints(time.Now()); promoted != 1 {
		t.Fatalf("promoted = %d, want the non-user marker kept for recovery", promoted)
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

func TestShutdownSnapshotIncludesPreLoopTurn(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	loop := NewLoopContext("pre-loop", 3, nil)
	h.beginInFlightTurn(desktopUserID, "continue the pre-loop task", loop)

	h.snapshotInterruptedSessionsForShutdown()

	slot := h.memory.GetUnfinishedSlot(desktopUserID)
	if slot == nil || slot.LastTask != "continue the pre-loop task" {
		t.Fatalf("pre-loop recovery slot = %#v", slot)
	}
	if !loop.IsCancelled() {
		t.Fatal("pre-loop turn was not cancelled")
	}
	h.endInFlightTurn(desktopUserID, loop)
	h.waitForInterruptedSessionsForShutdown()
}

func TestShutdownWaitIncludesPreLoopCleanup(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	loop := NewLoopContext("pre-loop", 3, nil)
	h.beginInFlightTurn(desktopUserID, "save before exit", loop)
	cleaned := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		h.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "assistant", Content: "final progress"}})
		h.endInFlightTurn(desktopUserID, loop)
		close(cleaned)
	}()

	h.waitForInterruptedSessionsForShutdown()

	select {
	case <-cleaned:
	default:
		t.Fatal("shutdown returned before pre-loop cleanup completed")
	}
}

func TestShutdownFlushReloadKeepsFinalLoopProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	memory := agent.NewPersistentConversationMemory(path)
	h := &IMMessageHandler{memory: memory}
	loop := NewLoopContext("shutdown-run", 3, nil)
	h.setSessionLoopCtx(desktopUserID, loop)
	h.getSessionLoop(desktopUserID).userText = "finish the report"
	go func() {
		<-loop.CancelC
		memory.Save(desktopUserID, []agent.ConversationEntry{
			{Role: "user", Content: "finish the report"},
			{Role: "assistant", Content: "final progress before exit"},
		})
		loop.Done()
	}()

	h.snapshotInterruptedSessionsForShutdown()
	h.waitForInterruptedSessionsForShutdown()
	if err := memory.FlushNow(); err != nil {
		t.Fatal(err)
	}
	memory.Stop()

	reloaded := agent.NewPersistentConversationMemory(path)
	defer reloaded.Stop()
	if slot := reloaded.GetUnfinishedSlot(desktopUserID); slot == nil || slot.LastTask != "finish the report" {
		t.Fatalf("reloaded shutdown slot = %#v", slot)
	}
	entries := reloaded.Load(desktopUserID)
	if len(entries) != 2 || entries[1].Content != "final progress before exit" {
		t.Fatalf("final loop progress was not durable: %#v", entries)
	}
}

func TestShutdownWaitsForMultipleLoopsInParallel(t *testing.T) {
	h := newShutdownSnapshotHandler(t)
	const loops = 3
	for i := 0; i < loops; i++ {
		loop := NewLoopContext("parallel-shutdown", 3, nil)
		h.setSessionLoopCtx("desktop-user:parallel-"+string(rune('a'+i)), loop)
		go func(loop *LoopContext) {
			time.Sleep(100 * time.Millisecond)
			loop.Done()
		}(loop)
	}

	started := time.Now()
	h.waitForInterruptedSessionsForShutdown()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("parallel shutdown wait took %v", elapsed)
	}
}
