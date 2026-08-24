package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

func TestStopCommandIsCancelAlias(t *testing.T) {
	if got := classifyImmediateIMCommand(" /stop "); got != imCommandCancel {
		t.Fatalf("/stop classification = %v, want cancel", got)
	}
}

func TestStopCommandInterruptsActiveLoopWithoutWaiting(t *testing.T) {
	const userID = "im:stop"
	loopCtx := NewLoopContext("long task", 3, nil)
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h

	result := h.interruptHandler.TryInterrupt(userID, "/stop")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/stop result = %+v, want handled replace", result)
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("/stop must cancel the active loop")
	}
	if !h.hasCancelledTaskBoundary(userID) {
		t.Fatal("/stop must establish a new-task boundary")
	}
	if !strings.Contains(result.Reply, "取消") && !strings.Contains(strings.ToLower(result.Reply), "cancel") {
		t.Fatalf("/stop reply should confirm cancellation, got %q", result.Reply)
	}
}

func TestStopInterruptAppliesFullCancellationPolicy(t *testing.T) {
	const userID = "im:stop-policy"
	loopCtx := NewLoopContext("long task", 3, nil)
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	result := h.interruptHandler.TryInterrupt(userID, "/stop")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/stop result = %+v, want handled replace", result)
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("/stop interrupt must cancel the active side-runner")
	}
	// The current foreground loop must be stopped too, even if a side-runner is
	// present for the same IM owner.
	if !loopCtx.IsCancelled() {
		t.Fatal("/stop interrupt must cancel the foreground loop")
	}
}

func TestStopInterruptCancelsSideRunnerWithoutActiveLoop(t *testing.T) {
	const userID = "im:stop-side-runner"
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	result := h.interruptHandler.TryInterrupt(userID, "/stop")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/stop result = %+v, want handled replace", result)
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("/stop must cancel a side-runner even without an active LoopContext")
	}
}

func TestResetInterruptCancelsSideRunnerWithoutActiveLoop(t *testing.T) {
	const userID = "im:reset-side-runner"
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	result := h.interruptHandler.TryInterrupt(userID, "/new")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/new result = %+v, want handled replace", result)
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("/new must cancel a side-runner even without an active LoopContext")
	}
}

func TestExitInterruptCancelsActiveLoopInsteadOfQueueing(t *testing.T) {
	const userID = "im:exit-interrupt"
	loopCtx := NewLoopContext("long task", 3, nil)
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem, interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h

	result := h.interruptHandler.TryInterrupt(userID, "/exit")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/exit result = %+v, want handled replace", result)
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("/exit must cancel the active loop")
	}
}

func TestExitInterruptCancelsSideRunnerWithoutActiveLoop(t *testing.T) {
	const userID = "im:exit-side-runner"
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	result := h.interruptHandler.TryInterrupt(userID, "/exit")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/exit result = %+v, want handled replace", result)
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("/exit must cancel a side-runner even without an active LoopContext")
	}
}

func TestCorrectionReplaceUsesImmediateFullCancellationPolicy(t *testing.T) {
	const userID = "im:correction-stop"
	loopCtx := NewLoopContext("long task", 3, nil)
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	resultCh := make(chan progress.InterruptResult, 1)
	go func() {
		resultCh <- h.interruptHandler.HandleCorrection(userID, "replacement", progress.ActionMerge, progress.ActionReplace)
	}()

	select {
	case result := <-resultCh:
		if !result.Handled || result.Action != progress.ActionReplace {
			t.Fatalf("correction result = %+v, want handled replace", result)
		}
	case <-time.After(time.Second):
		t.Fatal("correction replace must not wait for the active loop to exit")
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("correction replace must cancel the foreground loop")
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("correction replace must cancel active side-runners")
	}
}

func TestAutomaticReplaceUsesImmediateFullCancellationPolicy(t *testing.T) {
	const userID = "im:automatic-stop"
	loopCtx := NewLoopContext("long task", 3, nil)
	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h
	btw := &BtwSubAgent{userID: userID}
	h.activeBtwSubAgents.Store(userID, btw)

	resultCh := make(chan progress.InterruptResult, 1)
	go func() {
		resultCh <- h.interruptHandler.TryInterrupt(userID, "停止当前任务，改为处理新任务")
	}()

	select {
	case result := <-resultCh:
		if !result.Handled || result.Action != progress.ActionReplace {
			t.Fatalf("automatic replace result = %+v, want handled replace", result)
		}
	case <-time.After(time.Second):
		t.Fatal("automatic replace must not wait for the active loop to exit")
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("automatic replace must cancel the foreground loop")
	}
	if btw.cancelled.Load() == 0 {
		t.Fatal("automatic replace must cancel active side-runners")
	}
}

func TestResetCommandInterruptsActiveLoop(t *testing.T) {
	const userID = "im:reset"
	loopCtx := NewLoopContext("long task", 3, nil)
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	h := &IMMessageHandler{memory: mem}
	h.setSessionLoopCtx(userID, loopCtx)

	resp, handled := h.handleImmediateIMCommand(IMUserMessage{UserID: userID, Text: "/new"}, "/new", nil, nil)
	if !handled || resp == nil {
		t.Fatal("/new should be handled")
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("/new must cancel the active loop before reset")
	}
}

func TestNewInterruptResetsActiveLoopInsteadOfQueueing(t *testing.T) {
	const userID = "im:new-interrupt"
	loopCtx := NewLoopContext("long task", 3, nil)
	mem := agent.NewConversationMemory()
	defer mem.Stop()
	mem.Save(userID, []agent.ConversationEntry{{Role: "user", Content: "old task"}})
	h := &IMMessageHandler{memory: mem, interruptHandler: newIMInterruptHandler(nil)}
	h.setSessionLoopCtx(userID, loopCtx)
	h.interruptHandler.handler = h

	result := h.interruptHandler.TryInterrupt(userID, "/new")
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/new result = %+v, want handled replace", result)
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("/new must cancel the active loop")
	}
	if got := mem.Load(userID); len(got) != 0 {
		t.Fatalf("/new must clear conversation memory, got %#v", got)
	}
}

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

func TestHubCancelSessionDoesNotWaitForLoopExit(t *testing.T) {
	const userID = "im:hub-stop"
	loopCtx := NewLoopContext("long task", 3, nil)
	h := &IMMessageHandler{}
	h.setSessionLoopCtx(userID, loopCtx)
	payload, err := json.Marshal(map[string]string{"user_id": userID})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	client := &RemoteHubClient{imHandler: h}

	done := make(chan struct{})
	go func() {
		client.handleIMCancelSession(inboundHubEnvelope{Payload: payload})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("hub cancellation must not wait for the target loop to exit")
	}
	if !loopCtx.IsCancelled() {
		t.Fatal("hub cancellation must signal the target loop")
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
