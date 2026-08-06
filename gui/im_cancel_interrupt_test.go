package main

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

func TestCancelledLoopDoesNotTryInlineInterrupt(t *testing.T) {
	loopCtx := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{
		interruptHandler: &imInterruptHandler{},
		currentLoopCtx:   loopCtx,
	}
	msg := IMUserMessage{UserID: "desktop-user", Text: "new task after cancel"}
	h.setSessionLoopCtx(msg.UserID, loopCtx)

	if !h.shouldTryInlineInterrupt(msg) {
		t.Fatal("running loop should accept inline interrupt routing")
	}

	loopCtx.Cancel()

	if h.shouldTryInlineInterrupt(msg) {
		t.Fatal("cancelled loop must not route new messages as current-task supplements")
	}
}

func TestCancelledBoundaryDoesNotTryInlineInterrupt(t *testing.T) {
	loopCtx := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{
		interruptHandler: &imInterruptHandler{},
		currentLoopCtx:   loopCtx,
	}
	msg := IMUserMessage{UserID: "desktop-user", Text: "same task after cancel"}
	h.setSessionLoopCtx(msg.UserID, loopCtx)

	if !h.shouldTryInlineInterrupt(msg) {
		t.Fatal("running loop should accept inline interrupt routing before cancel boundary")
	}

	h.markTaskCancelledByUser(msg.UserID)

	if h.shouldTryInlineInterrupt(msg) {
		t.Fatal("cancel boundary must prevent merging the next user message into the cancelled task")
	}
}

func TestCancelCurrentSessionClearsPendingInjectionAndMarksBoundary(t *testing.T) {
	loopCtx := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{interruptHandler: &imInterruptHandler{}}
	userID := "desktop-user"
	h.setSessionLoopCtx(userID, loopCtx)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = loopCtx
	h.lastUserID = userID
	h.lastUserText = "old task"
	h.globalLoopMu.Unlock()
	h.pendingInjection.Store(userID, "[用户补充] stale")
	go func() {
		<-loopCtx.CancelC
		loopCtx.Done()
	}()

	if _, err := h.CancelCurrentSession(); err != nil {
		t.Fatalf("CancelCurrentSession() error = %v", err)
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("cancel must clear pending injections for the cancelled task")
	}
	if !h.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel must leave a task boundary for the next message")
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("loop context should be cancelled")
	}
}

func TestCancelledBoundaryForcesNextMessageToFreshTask(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	userID := "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "old task"},
		{Role: "assistant", Content: "partial answer"},
	}
	mem.Save(userID, entries)
	h.markTaskCancelledByUser(userID)

	askContext, freshTask, clearUI := h.applyUnifiedTaskContextDecision(
		IMUserMessage{UserID: userID, Text: "old task"},
		"old task",
		explicitTaskSlotDecision{},
		entries,
		nil,
		"stale ask context",
		false,
		false,
		false,
	)
	if !clearUI {
		t.Fatal("cancelled boundary should request clearing stale UI context")
	}
	if !freshTask {
		t.Fatal("first message after explicit cancel must start a fresh task")
	}
	if askContext != "" {
		t.Fatalf("ask context = %q, want cleared", askContext)
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel boundary should be consumed by the next message")
	}
}

func TestCancelledBoundaryIsConsumedEvenWhenAlreadyFreshTask(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	userID := "desktop-user"
	entries := []agent.ConversationEntry{
		{Role: "user", Content: "old task"},
		{Role: "assistant", Content: "partial answer"},
	}
	h.markTaskCancelledByUser(userID)

	askContext, freshTask, clearUI := h.applyUnifiedTaskContextDecision(
		IMUserMessage{UserID: userID, Text: "brand new task"},
		"brand new task",
		explicitTaskSlotDecision{},
		entries,
		nil,
		"stale ask context",
		false,
		true,
		false,
	)
	if !freshTask {
		t.Fatal("cancel boundary should preserve fresh-task routing")
	}
	if !clearUI {
		t.Fatal("cancel boundary should still clear stale UI context")
	}
	if askContext != "" {
		t.Fatalf("ask context = %q, want cleared", askContext)
	}
	if h.hasCancelledTaskBoundary(userID) {
		t.Fatal("cancel boundary should not leak into the next task")
	}
}

func TestCancelCommandDoesNotCancelOtherUsersLoop(t *testing.T) {
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	otherLoop := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{memory: mem}
	h.setSessionLoopCtx("other-user", otherLoop)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = otherLoop
	h.lastUserID = "other-user"
	h.lastUserText = "other task"
	h.globalLoopMu.Unlock()

	resp, handled := h.handleImmediateIMCommand(IMUserMessage{UserID: "desktop-user", Text: "/cancel"}, "/cancel", nil, nil)
	if !handled || resp == nil {
		t.Fatal("cancel command should be handled")
	}
	if otherLoop.IsCancelled() {
		t.Fatal("cancel command must not cancel another user's active loop")
	}
	if h.hasCancelledTaskBoundary("desktop-user") {
		t.Fatal("no task boundary should be marked when this user had no active task")
	}
}

func TestCancelCommandDoesNotCancelOtherUsersBtwOrLoopCommand(t *testing.T) {
	btw := &BtwSubAgent{userID: "other-user"}
	loop := &guiLoopCommandCallbacks{userID: "other-user"}
	h := &IMMessageHandler{}
	h.activeBtwSubAgent.Store(btw)
	h.activeLoopCallbacks.Store(loop)

	resp, handled := h.handleImmediateIMCommand(IMUserMessage{UserID: "desktop-user", Text: "/cancel"}, "/cancel", nil, nil)
	if !handled || resp == nil {
		t.Fatal("cancel command should be handled")
	}
	if btw.cancelled.Load() != 0 {
		t.Fatal("cancel command must not cancel another user's /btw subagent")
	}
	if loop.IsCancelled() {
		t.Fatal("cancel command must not cancel another user's /loop command")
	}
}

func TestCancelCommandCancelsTargetUsersBtwAndLoopCommand(t *testing.T) {
	btw := &BtwSubAgent{userID: "desktop-user"}
	h := &IMMessageHandler{}
	h.activeBtwSubAgent.Store(btw)

	resp, handled := h.handleImmediateIMCommand(IMUserMessage{UserID: "desktop-user", Text: "/cancel"}, "/cancel", nil, nil)
	if !handled || resp == nil {
		t.Fatal("cancel command should be handled")
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("cancel command should cancel target user's /btw subagent")
	}

	loop := &guiLoopCommandCallbacks{userID: "desktop-user"}
	h.activeBtwSubAgent.Store((*BtwSubAgent)(nil))
	h.activeLoopCallbacks.Store(loop)
	resp, handled = h.handleImmediateIMCommand(IMUserMessage{UserID: "desktop-user", Text: "/cancel"}, "/cancel", nil, nil)
	if !handled || resp == nil {
		t.Fatal("cancel command should be handled")
	}
	if !loop.IsCancelled() {
		t.Fatal("cancel command should cancel target user's /loop command")
	}
}

func TestSideRunnerRegistriesKeepConcurrentOwnersIsolated(t *testing.T) {
	h := &IMMessageHandler{}
	btw1 := &BtwSubAgent{userID: "user-1"}
	btw2 := &BtwSubAgent{userID: "user-2"}
	cleanupBtw1 := h.storeActiveBtwSubAgent("user-1", btw1)
	cleanupBtw2 := h.storeActiveBtwSubAgent("user-2", btw2)

	if got := h.activeBtwSubAgentForOwner("user-1"); got != btw1 {
		t.Fatalf("activeBtwSubAgentForOwner(user-1) = %p, want %p", got, btw1)
	}
	if got := h.activeBtwSubAgentForOwner("user-2"); got != btw2 {
		t.Fatalf("activeBtwSubAgentForOwner(user-2) = %p, want %p", got, btw2)
	}
	cleanupBtw1()
	if got := h.activeBtwSubAgentForOwner("user-2"); got != btw2 {
		t.Fatalf("cleanup for user-1 removed user-2 /btw = %p, want %p", got, btw2)
	}
	cleanupBtw2()

	loop1 := &guiLoopCommandCallbacks{userID: "user-1"}
	loop2 := &guiLoopCommandCallbacks{userID: "user-2"}
	cleanupLoop1 := h.storeActiveLoopCallbacks("user-1", loop1)
	cleanupLoop2 := h.storeActiveLoopCallbacks("user-2", loop2)
	if got := h.activeLoopCallbacksForOwner("user-1"); got != loop1 {
		t.Fatalf("activeLoopCallbacksForOwner(user-1) = %p, want %p", got, loop1)
	}
	if got := h.activeLoopCallbacksForOwner("user-2"); got != loop2 {
		t.Fatalf("activeLoopCallbacksForOwner(user-2) = %p, want %p", got, loop2)
	}
	cleanupLoop1()
	if got := h.activeLoopCallbacksForOwner("user-2"); got != loop2 {
		t.Fatalf("cleanup for user-1 removed user-2 /loop = %p, want %p", got, loop2)
	}
	cleanupLoop2()
}

func TestInterruptCorrectionCancelsTargetUserLoop(t *testing.T) {
	targetLoop := NewLoopContext("chat", 3, nil)
	otherLoop := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{}
	h.setSessionLoopCtx("desktop-user", targetLoop)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = otherLoop
	h.lastUserID = "other-user"
	h.lastUserText = "other task"
	h.globalLoopMu.Unlock()
	go func() {
		<-targetLoop.CancelC
		targetLoop.Done()
	}()
	go func() {
		<-otherLoop.CancelC
		otherLoop.Done()
	}()

	ih := &imInterruptHandler{handler: h}
	result := ih.HandleCorrection("desktop-user", "interrupt me", progress.ActionQueue, progress.ActionReplace)
	if !result.Handled {
		t.Fatal("correction replace should be handled")
	}
	if !targetLoop.IsCancelled() {
		t.Fatal("interrupt correction must cancel the target user's loop")
	}
	if otherLoop.IsCancelled() {
		t.Fatal("interrupt correction must not cancel another user's global loop")
	}
	if !h.hasCancelledTaskBoundary("desktop-user") {
		t.Fatal("interrupt correction should mark a cancel boundary for the target user")
	}
}

func TestHubCancelSessionTargetsPayloadUser(t *testing.T) {
	targetLoop := NewLoopContext("chat", 3, nil)
	otherLoop := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{}
	h.setSessionLoopCtx("desktop-user", targetLoop)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = otherLoop
	h.lastUserID = "other-user"
	h.lastUserText = "other task"
	h.globalLoopMu.Unlock()
	go func() {
		<-targetLoop.CancelC
		targetLoop.Done()
	}()
	go func() {
		<-otherLoop.CancelC
		otherLoop.Done()
	}()
	payload, err := json.Marshal(map[string]string{"user_id": "desktop-user"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	client := &RemoteHubClient{imHandler: h}

	client.handleIMCancelSession(inboundHubEnvelope{Payload: payload})

	if !targetLoop.IsCancelled() {
		t.Fatal("hub cancel must cancel payload user loop")
	}
	if otherLoop.IsCancelled() {
		t.Fatal("hub cancel must not cancel the global last-user loop when payload has user_id")
	}
	if !h.hasCancelledTaskBoundary("desktop-user") {
		t.Fatal("hub cancel should mark a cancel boundary for the payload user")
	}
}

func TestHubCancelSessionTargetsOnlyOwningHardwareRuntime(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	runtimes := newHardwareAgentRuntimeRegistry(app, nil, nil)
	t.Cleanup(runtimes.stopAll)
	hardware, err := runtimes.handler("pet-alpha")
	if err != nil {
		t.Fatalf("create hardware runtime: %v", err)
	}
	hardwareUserID := thirdPartySessionUserID("pet-alpha", "default")
	hardwareLoop := NewLoopContext("hardware task", 3, nil)
	desktopLoop := NewLoopContext("desktop task", 3, nil)
	hardware.setSessionLoopCtx(hardwareUserID, hardwareLoop)
	desktop := &IMMessageHandler{}
	desktop.setSessionLoopCtx(hardwareUserID, desktopLoop)
	go func() { <-hardwareLoop.CancelC; hardwareLoop.Done() }()
	go func() { <-desktopLoop.CancelC; desktopLoop.Done() }()

	payload, err := json.Marshal(map[string]string{"user_id": hardwareUserID})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	client := &RemoteHubClient{imHandler: desktop, hardwareAgents: runtimes}
	client.handleIMCancelSession(inboundHubEnvelope{Payload: payload})

	if !hardwareLoop.IsCancelled() {
		t.Fatal("hub cancel must cancel the owning hardware runtime")
	}
	if desktopLoop.IsCancelled() {
		t.Fatal("hardware cancel must not fall through to the desktop Agent")
	}
}

func TestHubCancelSessionWithoutPayloadDoesNotCancelGlobalLoop(t *testing.T) {
	otherLoop := NewLoopContext("chat", 3, nil)
	h := &IMMessageHandler{}
	h.setSessionLoopCtx("weixin:user", otherLoop)
	h.globalLoopMu.Lock()
	h.currentLoopCtx = otherLoop
	h.lastUserID = "weixin:user"
	h.lastUserText = "im task"
	h.globalLoopMu.Unlock()
	go func() {
		<-otherLoop.CancelC
		otherLoop.Done()
	}()
	client := &RemoteHubClient{imHandler: h}

	client.handleIMCancelSession(inboundHubEnvelope{})

	if otherLoop.IsCancelled() {
		t.Fatal("hub cancel without user_id must not cancel legacy global loop")
	}
	if h.hasCancelledTaskBoundary("weixin:user") {
		t.Fatal("hub cancel without user_id must not mark a cancel boundary")
	}
}
