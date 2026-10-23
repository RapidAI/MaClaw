package guiapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func testChatTaskHandler(t *testing.T) (*IMMessageHandler, *agent.ConversationMemory) {
	t.Helper()
	mem := agent.NewConversationMemory()
	// A bare handler is enough. newIMMessageHandler wires the database tool
	// through App.LoadConfig, which these isolation tests do not need.
	return &IMMessageHandler{memory: mem}, mem
}

func waitChatTaskStatus(t *testing.T, reg *chatTaskRegistry, id string, want chatTaskStatus) AIAssistantChatTask {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, task := range reg.List("") {
			if task.ID == id && task.Status == string(want) {
				return task
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach %s", id, want)
	return AIAssistantChatTask{}
}

func TestChatTaskDoesNotBlockChatOrClobberTranscript(t *testing.T) {
	h, mem := testChatTaskHandler(t)
	mem.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	reg := h.ensureChatTasks()
	started := make(chan struct{})
	release := make(chan struct{})
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		handler.memory.Save(task.MemoryKey, []agent.ConversationEntry{{Role: "user", Content: "task-only"}})
		close(started)
		<-release
		return chatTaskRunResult{Text: "deck is ready", Artifacts: []string{"/tmp/out.pptx"}}
	}

	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "make a slide deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}

	acquired := make(chan imMessageSerializationResult, 1)
	go func() {
		acquired <- h.enterIMMessageSerializationBoundary(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "still chatting"}, nil, nil, nil, explicitTaskSlotDecision{})
	}()
	select {
	case boundary := <-acquired:
		if boundary.Handled {
			t.Fatal("chat send was handled inside the serialization boundary")
		}
		if boundary.Unlock != nil {
			boundary.Unlock()
		}
	case <-time.After(2 * time.Second):
		t.Fatal("chat send blocked while a background task was running")
	}

	chat := mem.Load(desktopUserID)
	if len(chat) != 2 || chat[0].Content != "hello" || chat[1].Content != "hi" {
		t.Fatalf("desktop transcript changed while the task ran: %+v", chat)
	}
	forked := mem.Load(chatTaskMemoryOwner(desktopUserID, snap.ID))
	if len(forked) != 1 || forked[0].Content != "task-only" {
		t.Fatalf("task fork = %+v", forked)
	}

	close(release)
	done := waitChatTaskStatus(t, reg, snap.ID, chatTaskDone)
	if len(done.ArtifactPaths) != 1 || done.ArtifactPaths[0] != "/tmp/out.pptx" {
		t.Fatalf("artifacts = %+v", done.ArtifactPaths)
	}
	chat = mem.Load(desktopUserID)
	if len(chat) != 3 || chat[0].Content != "hello" || chat[1].Content != "hi" {
		t.Fatalf("result replaced the transcript: %+v", chat)
	}
	note, _ := chat[2].Content.(string)
	if !strings.Contains(note, "deck is ready") {
		t.Fatalf("result note = %q", note)
	}
	if mem.Load(desktopUserID)[0].Content == "task-only" {
		t.Fatal("task memory clobbered desktop-user")
	}
}

func TestChatTaskRestartDoesNotResume(t *testing.T) {
	dir := t.TempDir()
	h, _ := testChatTaskHandler(t)
	reg := newChatTaskRegistry(h, dir)
	called := 0
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		called++
		return chatTaskRunResult{Text: "should not run"}
	}
	reg.mu.Lock()
	reg.tasks["ct-old"] = &chatTaskRecord{
		ID:         "ct-old",
		SessionKey: desktopUserID,
		MemoryKey:  chatTaskMemoryOwner(desktopUserID, "ct-old"),
		Title:      "slides",
		Status:     chatTaskRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		answerCh:   make(chan string, 1),
	}
	reg.mu.Unlock()
	reg.persist()

	reloaded := newChatTaskRegistry(h, dir)
	tasks := reloaded.List(desktopUserID)
	if len(tasks) != 1 || tasks[0].Status != string(chatTaskInterrupted) || !tasks[0].Recovered {
		t.Fatalf("reloaded = %+v", tasks)
	}
	if called != 0 {
		t.Fatalf("restart resumed the task %d times", called)
	}
	if _, err := os.Stat(filepath.Join(dir, chatTaskStoreFile)); err != nil {
		t.Fatal(err)
	}
}

func TestChatTaskNeedsInputSteerAndSessionCap(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		answer, err := handler.ensureChatTasks().requestInput(task.ID, "which color?")
		if err != nil {
			return chatTaskRunResult{Err: err}
		}
		return chatTaskRunResult{Text: "color=" + answer}
	}
	first, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "paint the deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, first.ID, chatTaskNeedsInput)
	if err := reg.Steer(first.ID, "blue"); err != nil {
		t.Fatal(err)
	}
	done := waitChatTaskStatus(t, reg, first.ID, chatTaskDone)
	if done.ResultText != "color=blue" {
		t.Fatalf("result = %q", done.ResultText)
	}

	release := make(chan struct{})
	defer close(release)
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "ok"}
	}
	for i := 0; i < chatTaskPerSessionLimit; i++ {
		if _, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "another task", SessionKey: desktopUserID}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "one too many", SessionKey: desktopUserID}); err == nil {
		t.Fatal("expected the per-session cap to reject another task")
	}
	if _, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "mirror", SessionKey: "desktop-user:acp:mirror"}); err == nil {
		t.Fatal("expected ACP sessions to reject dispatch")
	}
}

func TestChatTaskSteerInjectsIntoTaskLoopOnly(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		loop := NewLoopContext("chat-task-"+task.ID, 4, nil)
		handler.bindChatTaskLoop(task.ID, loop)
		close(started)
		<-release
		return chatTaskRunResult{Text: "ok"}
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "write the notes", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}
	if err := reg.Steer(snap.ID, "keep it short"); err != nil {
		t.Fatal(err)
	}
	raw, ok := h.pendingInjection.Load(chatTaskMemoryOwner(desktopUserID, snap.ID))
	if !ok || !strings.Contains(raw.(string), "keep it short") {
		t.Fatalf("task injection = %#v ok=%v", raw, ok)
	}
	if _, ok := h.pendingInjection.Load(desktopUserID); ok {
		t.Fatal("steer landed on the desktop chat session")
	}
}

func TestChatTaskStatusReplyDoesNotRequireALoop(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks["ct-live"] = &chatTaskRecord{
		ID: "ct-live", SessionKey: desktopUserID, Title: "slides", Status: chatTaskRunning,
		Progress: "building page 3", CreatedAt: time.Now(), UpdatedAt: time.Now(), answerCh: make(chan string, 1),
	}
	reg.mu.Unlock()
	resp, handled := h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "进度怎么样了"})
	if !handled || resp == nil || strings.Contains(resp.Text, "ct-live") || !strings.Contains(resp.Text, "slides") || !strings.Contains(resp.Text, "building page 3") {
		t.Fatalf("status = handled:%v resp:%+v", handled, resp)
	}
	if _, handled := h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "please write a new essay about rivers"}); handled {
		t.Fatal("a new request was treated as a status question")
	}
	if _, handled := h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "git status"}); handled {
		t.Fatal("git status was treated as a background-task question")
	}
	resp, handled = h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "task status"})
	if !handled || resp == nil || strings.Contains(resp.Text, "ct-live") || !strings.Contains(resp.Text, "slides") {
		t.Fatalf("task status = handled:%v resp:%+v", handled, resp)
	}
}

func TestChatTaskDispatchCapIsAtomic(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "ok"}
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, errN := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "parallel task", SessionKey: desktopUserID})
			mu.Lock()
			if err != nil {
				errN++
			} else {
				okN++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(release)
	if okN != chatTaskPerSessionLimit || errN != 8-chatTaskPerSessionLimit {
		t.Fatalf("accepted %d rejected %d", okN, errN)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reg.activeCount(desktopUserID) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("accepted tasks still active: %d", reg.activeCount(desktopUserID))
}

func TestChatTaskCancelUnblocksRequestInput(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	waiting := make(chan struct{})
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		close(waiting)
		_, err := handler.ensureChatTasks().requestInput(task.ID, "which color?")
		if err == nil {
			return chatTaskRunResult{Text: "should have been canceled"}
		}
		return chatTaskRunResult{Err: err}
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "paint the deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not wait for input")
	}
	waitChatTaskStatus(t, reg, snap.ID, chatTaskNeedsInput)
	if err := reg.Cancel(snap.ID); err != nil {
		t.Fatal(err)
	}
	got := waitChatTaskStatus(t, reg, snap.ID, chatTaskCanceled)
	if got.ResultText != "" {
		t.Fatalf("canceled task returned %q", got.ResultText)
	}
}

func TestChatTaskCancelReleasesQueuedSlot(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	mgr := NewBackgroundLoopManager(nil)
	mgr.SetSlotLimit(SlotKindChatTask, 1)
	h.bgManager = mgr
	reg := h.ensureChatTasks()
	entered := make(chan string, 4)
	release := make(chan struct{})
	reg.executor = func(_ *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		entered <- task.ID
		<-release
		return chatTaskRunResult{Text: "ok"}
	}
	first, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "first task", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-entered:
		if id != first.ID {
			t.Fatalf("first executor was %s", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first task did not start")
	}
	second, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "second task", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && mgr.QueueLength(SlotKindChatTask) != 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if mgr.QueueLength(SlotKindChatTask) != 1 {
		t.Fatalf("queue length = %d", mgr.QueueLength(SlotKindChatTask))
	}
	if err := reg.Cancel(second.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && mgr.QueueLength(SlotKindChatTask) != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if mgr.QueueLength(SlotKindChatTask) != 0 {
		t.Fatalf("canceled task stayed queued: %d", mgr.QueueLength(SlotKindChatTask))
	}
	select {
	case id := <-entered:
		t.Fatalf("queued task started: %s", id)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	waitChatTaskStatus(t, reg, first.ID, chatTaskDone)
	waitChatTaskStatus(t, reg, second.ID, chatTaskCanceled)
	select {
	case id := <-entered:
		t.Fatalf("queued task started after the slot freed: %s", id)
	case <-time.After(300 * time.Millisecond):
	}
	if mgr.RunningCount(SlotKindChatTask) != 0 || mgr.QueueLength(SlotKindChatTask) != 0 {
		t.Fatalf("slot leaked: running=%d queued=%d", mgr.RunningCount(SlotKindChatTask), mgr.QueueLength(SlotKindChatTask))
	}
}

func TestDesktopChatTurnBudgetDoesNotAutoExtend(t *testing.T) {
	ctx := NewLoopContext("chat", 30, nil)
	ctx.ForegroundChatBudget = desktopChatForegroundIterationBudget
	limits := computeAgentLoopIterationLimits(ctx, 30, 0)
	if limits.EffectiveMax != desktopChatForegroundIterationBudget || limits.ChatFinalizeGrace != 0 {
		t.Fatalf("limits = %+v", limits)
	}
	h := &IMMessageHandler{}
	got := h.refreshAgentLoopEffectiveMax(ctx, 3, limits.EffectiveMax, 0, nil, func(string) {})
	if got != desktopChatForegroundIterationBudget {
		t.Fatalf("auto-extend returned %d", got)
	}
}

func TestChatTaskLLMPriorityIsNotPreemptedByForeground(t *testing.T) {
	scheduler := newLLMConcurrencyScheduler()
	ctx := context.Background()
	taskLease, err := scheduler.Acquire(ctx, llm.RequestTrace{Caller: chatTaskLLMCaller, OwnerID: "desktop-user:task:1"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled := false
	taskLease.SetCancel(func() { cancelled = true })
	foreground, err := scheduler.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled {
		t.Fatal("foreground chat cancelled the running chat-task lease")
	}
	second, err := scheduler.Acquire(ctx, llm.RequestTrace{Caller: chatTaskLLMCaller, OwnerID: "desktop-user:task:2"})
	if err != nil {
		t.Fatal(err)
	}
	foreground.Release()
	taskLease.Release()
	second.Release()

	backgroundScheduler := newLLMConcurrencyScheduler()
	backgroundLease, err := backgroundScheduler.Acquire(ctx, llm.RequestTrace{Caller: "background_agent_loop"})
	if err != nil {
		t.Fatal(err)
	}
	backgroundCancelled := false
	backgroundLease.SetCancel(func() { backgroundCancelled = true })
	if _, err := backgroundScheduler.Acquire(ctx, llm.RequestTrace{Caller: "agent_loop", OwnerID: desktopUserID}); err != nil {
		t.Fatal(err)
	}
	if !backgroundCancelled {
		t.Fatal("foreground chat should still preempt ordinary background leases")
	}
}

func TestProjectPathIgnoresChatTaskMemoryKey(t *testing.T) {
	if got := projectPathFromSessionOwnerID("desktop-user:task:ct-1"); got != "" {
		t.Fatalf("task memory key resolved as a path: %q", got)
	}
	const project = `C:\Users\ma139\.maclaw\data\tasks\deck-1`
	got := projectPathFromSessionOwnerID("desktop-user:" + project + ":task:ct-9")
	if got != normalizeProjectSessionPath(project) {
		t.Fatalf("worker project = %q", got)
	}
}

func TestChatTaskGuidanceKeepsFollowUpsOnTheSameWork(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks["ct-live"] = &chatTaskRecord{
		ID: "ct-live", SessionKey: desktopUserID, Title: "slides", Status: chatTaskRunning,
		CreatedAt: time.Now(), UpdatedAt: time.Now(), answerCh: make(chan string, 1), cancelCh: make(chan struct{}),
	}
	reg.mu.Unlock()
	text := h.chatTaskDispatchGuidance(desktopUserID)
	if strings.Contains(text, "new chat turn") || strings.Contains(text, "ct-live") {
		t.Fatalf("guidance leaked the old follow-up rule or an id:\n%s", text)
	}
	if !strings.Contains(text, "steer_background_task") || !strings.Contains(text, "slides") || !strings.Contains(text, "stop_all_work") {
		t.Fatalf("guidance = %s", text)
	}
	if !strings.Contains(chatTaskWorkerGuidance(), "verified") {
		t.Fatal("worker guidance missing the verified-result rule")
	}
}

func TestChatTaskFollowUpSteersTheSameTask(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		loop := NewLoopContext("chat-task-"+task.ID, 4, nil)
		handler.bindChatTaskLoop(task.ID, loop)
		<-release
		return chatTaskRunResult{Text: "ok"}
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "write the notes", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, snap.ID, chatTaskRunning)
	resp, handled := h.tryChatTaskFollowUp(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "改成短一点", Lang: "zh-Hans"})
	if !handled || resp == nil || strings.Contains(resp.Text, snap.ID) {
		t.Fatalf("follow-up = handled:%v resp:%+v", handled, resp)
	}
	raw, ok := h.pendingInjection.Load(chatTaskMemoryOwner(desktopUserID, snap.ID))
	if !ok || !strings.Contains(raw.(string), "改成短一点") {
		t.Fatalf("injection = %#v ok=%v", raw, ok)
	}
	if _, handled := h.tryChatTaskFollowUp(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "please write a new essay about rivers"}); handled {
		t.Fatal("a new request was steered into the running work")
	}
}

func TestDesktopChatStopConfirmsOnlyAfterTasksAreIdle(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "should not be delivered"}
	}
	first, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "first", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "second", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, first.ID, chatTaskRunning)
	waitChatTaskStatus(t, reg, second.ID, chatTaskRunning)
	resp, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "都停下", Lang: "zh-Hans"})
	if !handled || resp == nil || resp.Text != "停了。" || strings.Contains(resp.Text, first.ID) {
		t.Fatalf("stop = handled:%v resp:%+v", handled, resp)
	}
	if reg.activeCount(desktopUserID) != 0 {
		t.Fatalf("tasks still active: %d", reg.activeCount(desktopUserID))
	}
	again, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "停下，不干了。", Lang: "zh-Hans"})
	if !handled || again == nil || again.Text != "没在做事。" {
		t.Fatalf("compound stop = handled:%v resp:%+v", handled, again)
	}
	idle, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "stop", Lang: "en"})
	if !handled || idle == nil || idle.Text != "Nothing is running." {
		t.Fatalf("idle stop = handled:%v resp:%+v", handled, idle)
	}
}

func TestForegroundChatHandsOffComputerUseAndLongToolRuns(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	h.sessionLoops.Store(desktopUserID, &sessionLoopState{loopCtx: loop})
	if got := h.rejectForegroundWorkTool(desktopUserID, "read_file"); got != "" {
		t.Fatal(got)
	}
	if got := h.rejectForegroundWorkTool(desktopUserID, "web_search"); got != "" {
		t.Fatal(got)
	}
	if got := h.rejectForegroundWorkTool(desktopUserID, "bash"); got != "" {
		t.Fatalf("a later tool call was refused: %s", got)
	}
	for _, name := range []string{"ssh", "write_file", "web_fetch", "download_file"} {
		if got := h.rejectForegroundWorkTool(desktopUserID, name); got != "" {
			t.Fatalf("%s was refused for a call count: %s", name, got)
		}
	}
	if loop.ForegroundHandoffNeeded {
		t.Fatal("a call count closed the chat turn")
	}
	vision := NewLoopContext("chat-vision", desktopChatForegroundIterationBudget, nil)
	vision.OfferChatTaskDispatch = true
	h.sessionLoops.Store(desktopUserID, &sessionLoopState{loopCtx: vision})
	if got := h.rejectForegroundWorkTool(desktopUserID, "computer_observe"); got != chatTaskComputerUseHandoffMessage {
		t.Fatalf("computer use = %q", got)
	}
	if vision.ForegroundToolRounds != 0 || vision.ForegroundHandoffNeeded {
		t.Fatalf("computer use closed the turn rounds=%d needed=%v", vision.ForegroundToolRounds, vision.ForegroundHandoffNeeded)
	}
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: vision,
		semanticSurface: &semanticCallSurface{
			registry: tool.NewCapabilityRegistry("test"),
			rendered: map[string]bool{},
		},
	}
	names := toolNames(cb.BuildToolsForModelRequest("看一下屏幕上的窗口", 1))
	if !names[semanticToolsSearchName] || !names[chatTaskToolDispatch] {
		t.Fatalf("computer-use refusal collapsed the turn: %v", names)
	}
	if _, ok := h.continueSpentForegroundChat(vision, desktopUserID, "看一下屏幕上的窗口", agent.LoopResult{}); ok {
		t.Fatal("a refused desktop call started another task")
	}
}

func toolNames(defs []map[string]interface{}) map[string]bool {
	names := map[string]bool{}
	for _, def := range defs {
		if name := extractToolName(def); name != "" {
			names[name] = true
		}
	}
	return names
}

func TestSemanticChatRequestListsBackgroundHandoff(t *testing.T) {
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{},
		loopCtx: loop,
		semanticSurface: &semanticCallSurface{
			registry: tool.NewCapabilityRegistry("test"),
			rendered: map[string]bool{},
		},
	}
	names := toolNames(cb.BuildToolsForModelRequest("需要相关图片展示", 0))
	if !names[chatTaskToolDispatch] || !names[chatTaskToolSteer] || !names[chatTaskToolStop] {
		t.Fatalf("handoff tools missing: %v", names)
	}
	if names[chatTaskToolRequestInput] {
		t.Fatal("foreground request listed the worker question tool")
	}
	if !names[semanticToolsSearchName] {
		t.Fatal("discovery meta-tool dropped")
	}
	if !cb.IsToolAllowed(chatTaskToolDispatch) {
		t.Fatal("dispatch was stripped by the authorizer")
	}

	loop.mu.Lock()
	loop.ForegroundToolRounds = desktopChatInlineToolRounds + 8
	loop.mu.Unlock()
	kept := toolNames(cb.BuildToolsForModelRequest("需要相关图片展示", 3))
	if !kept[semanticToolsSearchName] || !kept[chatTaskToolDispatch] {
		t.Fatalf("a call count removed tools: %v", kept)
	}
	loop.mu.Lock()
	loop.ForegroundHandoffNeeded = true
	loop.mu.Unlock()
	collapsed := toolNames(cb.BuildToolsForModelRequest("需要相关图片展示", 4))
	if !collapsed[chatTaskToolDispatch] || !collapsed[chatTaskToolSteer] || !collapsed[chatTaskToolStop] {
		t.Fatalf("handoff missing after computer use: %v", collapsed)
	}
	if collapsed[semanticToolsSearchName] || collapsed["bash"] || collapsed["write_file"] || len(collapsed) != 3 {
		t.Fatalf("work tools stayed listed after computer use: %v", collapsed)
	}
	if cb.IsToolAllowed("bash") || cb.IsToolAllowed("write_file") {
		t.Fatal("work tools stayed authorized after computer use")
	}
	allowed, reason := cb.IsToolCallAllowed("write_file", `{"path":"fetch_images.py"}`)
	if allowed || reason != chatTaskComputerUseHandoffMessage {
		t.Fatalf("write intake = %v %q", allowed, reason)
	}
	granted, message := cb.PetitionToolCall("bash")
	if granted || message != chatTaskComputerUseHandoffMessage {
		t.Fatalf("bash petition = %v %q", granted, message)
	}
	if denial := cb.ToolDenialMessage("bash"); !strings.Contains(denial, "Desktop control does not run") {
		t.Fatalf("authorizer denial = %q", denial)
	}
	// A grant refresh after the budget must not put work tools back on the
	// host record. Sync publishes the capability closure, not the request.
	cb.setVisibleToolDefinitions([]map[string]interface{}{
		toolDef("bash", "shell", map[string]interface{}{"command": map[string]string{"type": "string"}}, []string{"command"}),
	})
	if err := cb.syncSemanticToolSurface(); err != nil {
		t.Fatal(err)
	}
	refreshed := toolNames(cb.tools)
	if refreshed["bash"] || refreshed[semanticToolsSearchName] || len(refreshed) != 3 {
		t.Fatalf("refresh restored work tools: %v", refreshed)
	}
	if cb.semanticSurface.rendered["bash"] || !cb.semanticSurface.rendered[chatTaskToolDispatch] {
		t.Fatalf("rendered = %#v", cb.semanticSurface.rendered)
	}

	loop.markChatTaskHandoffSent()
	after := toolNames(cb.BuildToolsForModelRequest("需要相关图片展示", 4))
	if after[chatTaskToolDispatch] || after["bash"] || !after[chatTaskToolSteer] || !after[chatTaskToolStop] || len(after) != 2 {
		t.Fatalf("after dispatch = %v", after)
	}
	if denial := cb.ToolDenialMessage("write_file"); !strings.Contains(denial, "already handed the work off") {
		t.Fatalf("post-dispatch denial = %q", denial)
	}
	granted, message = cb.PetitionToolCall(chatTaskToolDispatch)
	if granted || message != chatTaskAlreadyHandedOffMessage {
		t.Fatalf("second dispatch petition = %v %q", granted, message)
	}
}

func TestSemanticDispatchClosesTheRestOfTheTurn(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "deck is ready"}
	}
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.UserID = desktopUserID
	h.sessionLoops.Store(desktopUserID, &sessionLoopState{loopCtx: loop})
	cb := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  desktopUserID,
		loopCtx: loop,
		semanticSurface: &semanticCallSurface{
			registry: tool.NewCapabilityRegistry("test"),
			rendered: map[string]bool{},
		},
	}
	first := cb.executeSemanticTool(chatTaskToolDispatch, `{"request":"download licensed photos and rebuild the deck"}`)
	if !strings.Contains(first, "Started:") {
		t.Fatalf("first dispatch = %q", first)
	}
	second := cb.executeSemanticTool(chatTaskToolDispatch, `{"request":"do it again"}`)
	if strings.Contains(second, "Started:") || !strings.Contains(second, "already handed the work off") {
		t.Fatalf("second dispatch = %q", second)
	}
	if got := h.rejectForegroundWorkTool(desktopUserID, "bash"); got != chatTaskAlreadyHandedOffMessage {
		t.Fatalf("bash after dispatch = %q", got)
	}
	if h.rejectForegroundWorkTool(desktopUserID, "read_file") == "" {
		t.Fatal("a lookup after dispatch stayed inline")
	}
}

func TestChatTaskTurnEnvelopeResetsOnPrepare(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	loop := NewLoopContext("chat", 30, nil)
	loop.OfferChatTaskDispatch = true
	loop.ForegroundChatBudget = desktopChatForegroundIterationBudget
	loop.ForegroundToolRounds = desktopChatInlineToolRounds + 1
	loop.ForegroundHandoffNeeded = true
	loop.markChatTaskHandoffSent()
	got := h.prepareIMLoopContext(loop, IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "继续"}, nil, false, false)
	if got != loop {
		t.Fatal("prepare replaced the reused loop")
	}
	if loop.OfferChatTaskDispatch || loop.ForegroundChatBudget != 0 || loop.ForegroundToolRounds != 0 || loop.chatTaskHandoffSent() || loop.ForegroundHandoffNeeded {
		t.Fatalf("turn envelope survived prepare: offer=%v budget=%d rounds=%d sent=%v needed=%v", loop.OfferChatTaskDispatch, loop.ForegroundChatBudget, loop.ForegroundToolRounds, loop.chatTaskHandoffSent(), loop.ForegroundHandoffNeeded)
	}
}

func TestSemanticWorkerListsOnlyTheQuestionTool(t *testing.T) {
	loop := NewLoopContext("chat-task", 30, nil)
	loop.ChatTaskID = "ct-1"
	loop.OfferChatTaskDispatch = true
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{},
		loopCtx: loop,
		semanticSurface: &semanticCallSurface{
			registry: tool.NewCapabilityRegistry("test"),
			rendered: map[string]bool{},
		},
	}
	names := toolNames(cb.BuildToolsForModelRequest("rebuild the deck", 0))
	if !names[chatTaskToolRequestInput] {
		t.Fatalf("worker question missing: %v", names)
	}
	if names[chatTaskToolDispatch] || names[chatTaskToolSteer] || names[chatTaskToolStop] {
		t.Fatalf("worker listed foreground handoff tools: %v", names)
	}
	if cb.foregroundHandoffOnly() {
		t.Fatal("a background chat task was treated as the foreground handoff")
	}
}

func TestSemanticDispatchRunsWithoutCapabilityGrant(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "deck is ready"}
	}
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.UserID = desktopUserID
	h.sessionLoops.Store(desktopUserID, &sessionLoopState{loopCtx: loop})
	cb := &sharedAgentLoopCallbacks{
		handler:         h,
		userID:          desktopUserID,
		loopCtx:         loop,
		semanticSurface: &semanticCallSurface{},
	}
	text := cb.executeSemanticTool(chatTaskToolDispatch, `{"request":"download licensed photos and rebuild the deck with them"}`)
	if !strings.Contains(text, "Started:") {
		t.Fatalf("dispatch = %q", text)
	}
	if strings.Contains(text, "selection_not_authorized") {
		t.Fatalf("handoff was treated as a capability grant: %q", text)
	}
	if loop.MaxIterations() != desktopChatForegroundIterationBudget {
		t.Fatalf("dispatch changed the turn ceiling to %d", loop.MaxIterations())
	}
}

func TestSpentForegroundChatContinuesWithoutAnotherUserMessage(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "container is healthy"}
	}
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.UserID = desktopUserID
	loop.ForegroundHandoffNeeded = true
	note, ok := h.continueSpentForegroundChat(loop, desktopUserID, "继续", agent.LoopResult{
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "ssh", ToolOutcome: "succeeded", Content: "service name is omniroute-host; image tag is 3.8.52; container still 3.8.51"},
			{Role: "tool", ToolName: "ssh", ToolOutcome: "failed", Content: "[system rejected] " + chatTaskHandoffRequiredMessage},
		},
	})
	if !ok || note != "剩下的接着做，做完发这里。" {
		t.Fatalf("note = %q ok=%v", note, ok)
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	if !strings.Contains(tasks[0].Prompt, "继续") || !strings.Contains(tasks[0].Prompt, "omniroute-host") {
		t.Fatalf("prompt = %s", tasks[0].Prompt)
	}
	if tasks[0].Title != "继续" {
		t.Fatalf("title = %q", tasks[0].Title)
	}
	if strings.Contains(tasks[0].Prompt, chatTaskHandoffRequiredMessage) {
		t.Fatal("worker prompt repeated the lookup denial")
	}
	if _, again := h.continueSpentForegroundChat(loop, desktopUserID, "继续", agent.LoopResult{}); again {
		t.Fatal("the same turn handed the work off twice")
	}
	under := NewLoopContext("chat-under", desktopChatForegroundIterationBudget, nil)
	under.OfferChatTaskDispatch = true
	under.ForegroundToolRounds = desktopChatInlineToolRounds + 8
	if _, ok := h.continueSpentForegroundChat(under, desktopUserID, "继续", agent.LoopResult{}); ok {
		t.Fatal("a call count was treated as unfinished work")
	}
	capped := NewLoopContext("chat-capped", desktopChatForegroundIterationBudget, nil)
	capped.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(capped, desktopUserID, "继续", agent.LoopResult{Error: "max iterations reached"}); !ok {
		t.Fatal("an iteration ceiling left the task for the user")
	}
	handed := NewLoopContext("chat-handed", desktopChatForegroundIterationBudget, nil)
	handed.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(handed, desktopUserID, "继续", agent.LoopResult{Text: "本轮操作额度已用完，请再发一条消息。"}); !ok {
		t.Fatal("a reply that asks for another message was the final answer")
	}
	finished := NewLoopContext("chat-finished", desktopChatForegroundIterationBudget, nil)
	finished.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(finished, desktopUserID, "继续", agent.LoopResult{Text: "升级已经完成，服务已是最新版本。"}); ok {
		t.Fatal("a finished report started another task")
	}
	worker := NewLoopContext("chat-worker", 30, nil)
	worker.OfferChatTaskDispatch = true
	worker.ChatTaskID = "ct-1"
	if _, ok := h.continueSpentForegroundChat(worker, desktopUserID, "继续", agent.LoopResult{Error: "max iterations reached"}); ok {
		t.Fatal("a background task started another one")
	}
	stopped := NewLoopContext("chat-dead", desktopChatForegroundIterationBudget, nil)
	stopped.OfferChatTaskDispatch = true
	stopped.ForegroundHandoffNeeded = true
	if _, ok := h.continueSpentForegroundChat(stopped, desktopUserID, "继续", agent.LoopResult{HardExit: true}); ok {
		t.Fatal("a dead-loop stop was handed off")
	}
}

func TestSpentForegroundChatLeavesAnUnobservedCommandAlone(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	unobserved := []agent.ConversationEntry{
		{Role: "tool", ToolName: "tools_search", Content: "ssh"},
		{Role: "tool", ToolName: "ssh", ToolOutcome: "error", Content: "[system unknown] trusted_ssh_outcome_unobserved"},
	}
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, "继续", agent.LoopResult{
		Text:         "本轮操作额度已用完，请再发一条消息。",
		HistoryDelta: unobserved,
	}); ok {
		t.Fatal("an unobserved command was started again")
	}
	capped := NewLoopContext("chat-capped", desktopChatForegroundIterationBudget, nil)
	capped.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(capped, desktopUserID, "继续", agent.LoopResult{
		Error: "max iterations reached",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "ssh", Content: "[system unknown] still running"},
		},
	}); ok {
		t.Fatal("an iteration ceiling reissued an unobserved command")
	}
	waiting := NewLoopContext("chat-waiting", desktopChatForegroundIterationBudget, nil)
	waiting.OfferChatTaskDispatch = true
	waiting.ForegroundHandoffNeeded = true
	if _, ok := h.continueSpentForegroundChat(waiting, desktopUserID, "继续", agent.LoopResult{
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "send_file", Content: "[system rejected] selection_awaiting_receipt"},
		},
	}); ok {
		t.Fatal("an explicit close reissued an unobserved command")
	}
	if len(reg.List(desktopUserID)) != 0 {
		t.Fatalf("tasks = %d", len(reg.List(desktopUserID)))
	}
	settled := NewLoopContext("chat-settled", desktopChatForegroundIterationBudget, nil)
	settled.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(settled, desktopUserID, "继续", agent.LoopResult{
		Text: "本轮操作额度已用完，请再发一条消息。",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "ssh", ToolOutcome: "error", Content: "[system unknown] trusted_ssh_outcome_unobserved"},
			{Role: "tool", ToolName: "ssh", ToolOutcome: "succeeded", Content: "EXIT=0"},
		},
	}); !ok {
		t.Fatal("a later settled command was left for the user")
	}
	quoted := NewLoopContext("chat-quoted", desktopChatForegroundIterationBudget, nil)
	quoted.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(quoted, desktopUserID, "继续", agent.LoopResult{
		Text: "本轮操作额度已用完，请再发一条消息。",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "read_file", ToolOutcome: "ok", Content: "the plan stays awaiting_receipt\nreturn \"[system unknown] lost\""},
		},
	}); !ok {
		t.Fatal("a settled read that quotes the marker was treated as in flight")
	}
	notAwaiting := NewLoopContext("chat-not-awaiting", desktopChatForegroundIterationBudget, nil)
	notAwaiting.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(notAwaiting, desktopUserID, "继续", agent.LoopResult{
		Text: "本轮操作额度已用完，请再发一条消息。",
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "ssh", ToolOutcome: "error", Content: "[system rejected] selection_execution_not_awaiting_receipt"},
		},
	}); !ok {
		t.Fatal("a settled rejection was treated as in flight")
	}
}

func TestSpentForegroundChatSteersTheSingleOpenTask(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "done"}
	}
	first, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "make the deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, first.ID, chatTaskRunning)

	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.UserID = desktopUserID
	loop.ForegroundHandoffNeeded = true
	note, ok := h.continueSpentForegroundChat(loop, desktopUserID, "需要相关图片", agent.LoopResult{
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "web_search", ToolOutcome: "succeeded", Content: "licensed photo list"},
		},
	})
	if !ok || note != "剩下的接着做，做完发这里。" {
		t.Fatalf("note = %q ok=%v", note, ok)
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Fatalf("started another task: %+v", tasks)
	}
	if tasks[0].Prompt != first.Prompt {
		t.Fatalf("prompt replaced: %s", tasks[0].Prompt)
	}
	raw, loaded := h.pendingInjection.Load(chatTaskMemoryOwner(desktopUserID, first.ID))
	injected, _ := raw.(string)
	if !loaded || !strings.Contains(injected, "[用户补充]") || !strings.Contains(injected, "需要相关图片") || !strings.Contains(injected, "licensed photo list") {
		t.Fatalf("injection = %q loaded=%v", injected, loaded)
	}
}

func TestSpentForegroundChatStartsFreshWhenTheOpenTaskCannotTakeASteer(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "done"}
	}
	first, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "make the deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, first.ID, chatTaskRunning)
	worker := NewLoopContext("chat-task-"+first.ID, 30, nil)
	if !worker.TrySealReplans(worker.ReplanRevision()) {
		t.Fatal("worker replans did not seal")
	}
	h.bindChatTaskLoop(first.ID, worker)

	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.UserID = desktopUserID
	loop.ForegroundHandoffNeeded = true
	note, ok := h.continueSpentForegroundChat(loop, desktopUserID, "需要相关图片", agent.LoopResult{
		HistoryDelta: []agent.ConversationEntry{
			{Role: "tool", ToolName: "web_search", ToolOutcome: "succeeded", Content: "licensed photo list"},
		},
	})
	if !ok || note != "剩下的接着做，做完发这里。" {
		t.Fatalf("note = %q ok=%v", note, ok)
	}
	var fresh AIAssistantChatTask
	for _, task := range reg.List(desktopUserID) {
		if task.ID != first.ID {
			fresh = task
		}
	}
	if fresh.ID == "" || !strings.Contains(fresh.Prompt, "需要相关图片") || !strings.Contains(fresh.Prompt, "licensed photo list") {
		t.Fatalf("fresh = %+v", fresh)
	}
	if raw, loaded := h.pendingInjection.Load(chatTaskMemoryOwner(desktopUserID, first.ID)); loaded {
		t.Fatalf("sealed task kept the note: %#v", raw)
	}
}

func TestSpentForegroundChatLeavesSeveralOpenTasks(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	release := make(chan struct{})
	defer close(release)
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		<-release
		return chatTaskRunResult{Text: "done"}
	}
	for _, text := range []string{"first deck", "second deck"} {
		snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: text, SessionKey: desktopUserID})
		if err != nil {
			t.Fatal(err)
		}
		waitChatTaskStatus(t, reg, snap.ID, chatTaskRunning)
	}
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.ForegroundHandoffNeeded = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, "需要相关图片", agent.LoopResult{}); ok {
		t.Fatal("several open tasks were handed a new one")
	}
	if len(reg.List(desktopUserID)) != 2 {
		t.Fatalf("tasks = %d", len(reg.List(desktopUserID)))
	}
	if loop.chatTaskHandoffSent() {
		t.Fatal("an ambiguous handoff closed the turn")
	}
}

func TestSpentForegroundChatDoesNotAnswerAWaitingTask(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	reg := h.ensureChatTasks()
	reg.executor = func(handler *IMMessageHandler, task *chatTaskRecord) chatTaskRunResult {
		_, _ = handler.ensureChatTasks().requestInput(task.ID, "which file?")
		return chatTaskRunResult{Text: "done"}
	}
	snap, err := reg.Dispatch(AIAssistantChatTaskDispatchRequest{Text: "edit the deck", SessionKey: desktopUserID})
	if err != nil {
		t.Fatal(err)
	}
	waitChatTaskStatus(t, reg, snap.ID, chatTaskNeedsInput)
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.ForegroundHandoffNeeded = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, "需要相关图片", agent.LoopResult{}); ok {
		t.Fatal("a waiting question was answered by the host")
	}
	got := waitChatTaskStatus(t, reg, snap.ID, chatTaskNeedsInput)
	if got.Question == "" {
		t.Fatal("the question was cleared")
	}
	if len(reg.List(desktopUserID)) != 1 {
		t.Fatalf("tasks = %d", len(reg.List(desktopUserID)))
	}
	if err := reg.Cancel(snap.ID); err != nil {
		t.Fatal(err)
	}
}

func TestApplyForegroundHandoffNoteReplacesTheClosingSentence(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "user", Content: "需要相关图片"},
		{Role: "assistant", Content: "", ToolCalls: []any{map[string]any{"id": "1"}}},
		{Role: "tool", ToolName: "web_search", Content: "photo"},
		{Role: "assistant", Content: "后台执行通道没有启动成功"},
	}
	got := applyForegroundHandoffNote(history, 0, "剩余工作已继续处理，完成后会发到这个对话。")
	if len(got) != 4 {
		t.Fatalf("len = %d", len(got))
	}
	if got[1].Content != "" || got[1].ToolCalls == nil {
		t.Fatalf("tool call = %+v", got[1])
	}
	if got[3].Content != "剩余工作已继续处理，完成后会发到这个对话。" || got[3].FinishReason != "stop" {
		t.Fatalf("closing = %+v", got[3])
	}
	if history[3].Content != "后台执行通道没有启动成功" {
		t.Fatal("input history was mutated")
	}
	typedNil := []agent.ConversationEntry{
		{Role: "assistant", Content: "再发一句继续", ToolCalls: []llm.ToolCall(nil)},
	}
	replaced := applyForegroundHandoffNote(typedNil, 0, "剩余工作已继续处理，完成后会发到这个对话。")
	if len(replaced) != 1 || replaced[0].Content != "剩余工作已继续处理，完成后会发到这个对话。" || replaced[0].ToolCalls != nil {
		t.Fatalf("empty tool calls were kept: %+v", replaced)
	}
	emptySlice := []agent.ConversationEntry{
		{Role: "assistant", Content: "我下一步核实服务名", ToolCalls: []llm.ToolCall{}},
	}
	replaced = applyForegroundHandoffNote(emptySlice, 0, "剩余工作已继续处理，完成后会发到这个对话。")
	if len(replaced) != 1 || replaced[0].Content != "剩余工作已继续处理，完成后会发到这个对话。" {
		t.Fatalf("empty slice was kept: %+v", replaced)
	}
	endedOnTools := []agent.ConversationEntry{
		{Role: "assistant", Content: "looking", ToolCalls: []any{"call"}},
		{Role: "tool", Content: "result"},
	}
	appended := applyForegroundHandoffNote(endedOnTools, 0, "note")
	if len(appended) != 3 || appended[0].Content != "looking" || appended[2].Role != "assistant" || appended[2].Content != "note" {
		t.Fatalf("appended = %+v", appended)
	}
	withPrior := []agent.ConversationEntry{
		{Role: "assistant", Content: "上一轮的正文"},
		{Role: "user", Content: "需要相关图片"},
		{Role: "assistant", Content: "looking", ToolCalls: []any{"call"}},
		{Role: "tool", Content: "result"},
	}
	kept := applyForegroundHandoffNote(withPrior, 1, "剩余工作已继续处理，完成后会发到这个对话。")
	if len(kept) != 5 || kept[0].Content != "上一轮的正文" || kept[4].Content != "剩余工作已继续处理，完成后会发到这个对话。" {
		t.Fatalf("prior turn = %+v", kept)
	}
}

func TestLightLookupCeilingDoesNotStartATask(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	h.ensureChatTasks().executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	light := NewLoopContext("chat-light", 3, nil)
	light.OfferChatTaskDispatch = true
	light.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", IterationBudget: 3}
	if _, ok := h.continueSpentForegroundChat(light, desktopUserID, "北京天气", agent.LoopResult{Error: "max iterations reached"}); ok {
		t.Fatal("a light lookup ceiling started a background task")
	}
	if _, ok := h.continueSpentForegroundChat(light, desktopUserID, "北京天气", agent.LoopResult{Text: "本轮操作额度已用完，请再发一条消息。"}); !ok {
		t.Fatal("a light lookup that handed the task back was left with the user")
	}
}

func TestForegroundContinuationPromptKeepsTheEarlierRequest(t *testing.T) {
	h, mem := testChatTaskHandler(t)
	mem.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: "把配图版讲稿做完"},
		{Role: "assistant", Content: "先写大纲"},
		{Role: "user", Content: "[系统] 不要停"},
		{Role: "user", Content: "继续"},
	})
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	loop := NewLoopContext("chat", 30, nil)
	loop.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, "继续", agent.LoopResult{Text: "本轮操作额度已用完，请再发一条消息。"}); !ok {
		t.Fatal("short follow-up did not continue")
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 || !strings.Contains(tasks[0].Prompt, "把配图版讲稿做完") || !strings.Contains(tasks[0].Prompt, "User request:\n继续") {
		t.Fatalf("prompt = %s", tasks[0].Prompt)
	}
	if strings.Contains(tasks[0].Prompt, "不要停") || strings.Contains(tasks[0].Prompt, "先写大纲") {
		t.Fatal("a system line or an assistant reply was copied")
	}
	long := strings.Repeat("配", 41)
	if got := h.chatTaskEarlierUserRequest(desktopUserID, long); got != "" {
		t.Fatalf("long request kept an earlier line: %q", got)
	}
}

func TestForegroundContinuationPromptReadsTextBesideImages(t *testing.T) {
	h, mem := testChatTaskHandler(t)
	const imagePayload = "data:image/png;base64,SHOULD-NOT-COPY"
	mem.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "把配图版讲稿做完\n\n" + agent.AutoExtractNotice + "\n\n" + agent.AttachedImageReadHint + "\n\n这份 PDF 没有文字层。页面图片已附在本条消息里，请直接看图回答。不要再调用 office、read_document 或 OCR。\n\n## Page 1\n[image attached]\n\n" + visionFirstInstruction},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": imagePayload}},
		}},
		{Role: "user", Content: []interface{}{
			map[string]interface{}{"type": "text", "text": visionFirstInstruction},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": imagePayload}},
		}},
	})
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	loop := NewLoopContext("chat", 30, nil)
	loop.OfferChatTaskDispatch = true
	delta := []agent.ConversationEntry{{
		Role: "tool", ToolName: "write_file", ToolOutcome: "succeeded",
		Content: []interface{}{
			map[string]interface{}{"type": "text", "text": "wrote slide 3"},
			map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": imagePayload}},
		},
	}}
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, "继续", agent.LoopResult{
		Text:         "请再发一条消息。",
		HistoryDelta: delta,
	}); !ok {
		t.Fatal("short follow-up after an image message did not continue")
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	prompt := tasks[0].Prompt
	if !strings.Contains(prompt, "Earlier request:\n把配图版讲稿做完") || !strings.Contains(prompt, "wrote slide 3") {
		t.Fatalf("prompt = %s", prompt)
	}
	if strings.Contains(prompt, "SHOULD-NOT-COPY") || strings.Contains(prompt, "base64") || strings.Contains(prompt, "vision input") || strings.Contains(prompt, "不要再调用") || strings.Contains(prompt, "请直接看图") || strings.Contains(prompt, "优先基于下列内容") || strings.Contains(prompt, "Analyze attached images first") {
		t.Fatal("image payload or an attached-image instruction was copied into the worker prompt")
	}
}

func TestForegroundContinuationPromptKeepsTheGoalBesideAFile(t *testing.T) {
	h, mem := testChatTaskHandler(t)
	mem.Save(desktopUserID, []agent.ConversationEntry{
		{Role: "user", Content: "把配图版讲稿做完"},
	})
	fileOnly := agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png\n" + agent.AttachedImageReadHint
	if got := h.chatTaskEarlierUserRequest(desktopUserID, fileOnly); got != "" {
		t.Fatalf("file-only message kept an earlier line: %q", got)
	}
	long := strings.Repeat("配", 41) + "\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png"
	if got := h.chatTaskEarlierUserRequest(desktopUserID, long); got != "" {
		t.Fatalf("long request kept an earlier line: %q", got)
	}
	current := "继续\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png\n" + agent.AttachedImageReadHint
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	loop := NewLoopContext("chat", 30, nil)
	loop.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, current, agent.LoopResult{Text: "请再发一条消息。"}); !ok {
		t.Fatal("short follow-up with a file did not continue")
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	prompt := tasks[0].Prompt
	if !strings.Contains(prompt, "Earlier request:\n把配图版讲稿做完") || !strings.Contains(prompt, `C:\slides\pic.png`) {
		t.Fatalf("prompt = %s", prompt)
	}
	if strings.Contains(prompt, "Analyze attached images first") {
		t.Fatal("the picker image note was copied into the worker prompt")
	}
}

func TestForegroundContinuationPromptMarksTheEarlierFileAsHistory(t *testing.T) {
	h, mem := testChatTaskHandler(t)
	const oldPath = `C:\slides\deck.pdf`
	const secret = "SECRET-BODY-SHOULD-DROP"
	earlier := "把配图版讲稿做完\n\n" + agent.FilePathPromptPrefix + "\n" + oldPath + "\n\n" +
		agent.AutoExtractNotice + "\n\n" +
		agent.AutoExtractBeginMarker + fmt.Sprintf("path=%q format=%q total_chars=40 injected_chars=40 injected_tokens=10 truncated=false ---\n", oldPath, "pdf") +
		"# text_source: vision\n# vision_bytes: 12\n" +
		"这份 PDF 没有文字层。页面图片已附在本条消息里，请直接看图回答。不要再调用 office、read_document 或 OCR。\n\n" +
		"## Page 1\n[image attached]\n" + secret + "\n" +
		agent.AutoExtractEndMarker + fmt.Sprintf("path=%q ---", oldPath)
	mem.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: earlier}})
	current := "继续\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\pic.png\n" + agent.AttachedImageReadHint
	reg := h.ensureChatTasks()
	reg.executor = func(*IMMessageHandler, *chatTaskRecord) chatTaskRunResult {
		return chatTaskRunResult{Text: "done"}
	}
	loop := NewLoopContext("chat", 30, nil)
	loop.OfferChatTaskDispatch = true
	if _, ok := h.continueSpentForegroundChat(loop, desktopUserID, current, agent.LoopResult{Text: "请再发一条消息。"}); !ok {
		t.Fatal("short follow-up with a new file did not continue")
	}
	tasks := reg.List(desktopUserID)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	prompt := tasks[0].Prompt
	if !strings.Contains(prompt, "Earlier request:\n把配图版讲稿做完") || !strings.Contains(prompt, agent.FilePathPromptPrefixHistorical) || !strings.Contains(prompt, oldPath) {
		t.Fatalf("prompt = %s", prompt)
	}
	if strings.Count(prompt, agent.FilePathPromptPrefix) != 1 || !strings.Contains(prompt, `C:\slides\pic.png`) {
		t.Fatalf("current upload marker = %s", prompt)
	}
	if strings.Index(prompt, agent.FilePathPromptPrefix) < strings.Index(prompt, "Tool results from this turn:") {
		t.Fatal("file grant precedes the tool results")
	}
	if got := agent.SelectedLocalFilePathsFromPrompt(prompt); len(got) != 1 || got[0] != `C:\slides\pic.png` {
		t.Fatalf("grant = %#v", got)
	}
	if strings.Contains(prompt, secret) || strings.Contains(prompt, "# text_source: vision") || strings.Contains(prompt, "[image attached]") || strings.Contains(prompt, "不要再调用") || strings.Contains(prompt, "优先基于下列内容") || strings.Contains(prompt, "Analyze attached images first") {
		t.Fatal("the earlier extract or an attached-image instruction was copied")
	}
	if !strings.Contains(prompt, fmt.Sprintf("之前已自动解析文档，正文已省略 path=%q", oldPath)) {
		t.Fatalf("omitted-body summary missing: %s", prompt)
	}

	const kept = "KEEP-CURRENT-BODY"
	long := strings.Repeat("配", 41) + "\n\n" + agent.FilePathPromptPrefix + "\nC:\\slides\\now.pdf\n\n" +
		agent.AutoExtractNotice + "\n\n" +
		agent.AutoExtractBeginMarker + `path="C:\slides\now.pdf" format="pdf" total_chars=18 injected_chars=18 injected_tokens=4 truncated=false ---` + "\n" +
		kept + "\n" + agent.AutoExtractEndMarker + `path="C:\slides\now.pdf" ---`
	own := foregroundChatContinuationPrompt("", long, "", nil)
	if !strings.Contains(own, kept) || !strings.Contains(own, agent.AutoExtractNotice) || strings.Contains(own, agent.FilePathPromptPrefixHistorical) {
		t.Fatalf("current upload was rewritten: %s", own)
	}

	bare := h.chatTaskEarlierUserRequest(desktopUserID, "继续")
	withOutcome := foregroundChatContinuationPrompt("", "继续", bare, []agent.ConversationEntry{{
		Role: "tool", ToolName: "office", ToolOutcome: "succeeded",
		Content: `office(action="read_document") kept-outcome`,
	}})
	if strings.Count(withOutcome, agent.FilePathPromptPrefix) != 1 || strings.Index(withOutcome, agent.FilePathPromptPrefix) < strings.Index(withOutcome, "kept-outcome") {
		t.Fatalf("earlier file grant = %s", withOutcome)
	}
	if got := agent.SelectedLocalFilePathsFromPrompt(withOutcome); len(got) != 1 || got[0] != oldPath {
		t.Fatalf("earlier grant = %#v", got)
	}
	expanded := agent.ExpandUserSelectedFilePaths(withOutcome)
	if !strings.Contains(expanded, "kept-outcome") || !strings.Contains(expanded, "把配图版讲稿做完") || !strings.Contains(expanded, "Do not repeat a send") {
		t.Fatalf("expansion dropped the briefing: %s", expanded)
	}
}

func TestForegroundContinuationPromptUsesManagedTaskWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	taskRoot := filepath.Join(app.GetDataDir(), "tasks", "deck-1")
	workspace := filepath.Join(taskRoot, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	session := "desktop-user:" + taskRoot
	got := h.foregroundChatContinuationWorkspace(session)
	want := normalizeProjectSessionPath(workspace)
	if normalizeProjectSessionPath(got) != want {
		t.Fatalf("workspace = %q, want %q", got, want)
	}
	worker := h.projectTabWorkDirForOwner(chatTaskMemoryOwner(session, "ct-1"))
	if normalizeProjectSessionPath(worker) != want {
		t.Fatalf("worker dir = %q, want %q", worker, want)
	}
	prompt := foregroundChatContinuationPrompt(got, "需要相关图片展示，更正式一些。", "", nil)
	if !strings.Contains(prompt, "Workspace:\n"+want+"\n") {
		t.Fatalf("prompt workspace = %s", prompt)
	}
	if strings.Contains(prompt, "Workspace:\n"+normalizeProjectSessionPath(taskRoot)+"\n") {
		t.Fatalf("prompt names the task metadata root: %s", prompt)
	}
	if desktop := h.foregroundChatContinuationWorkspace(desktopUserID); desktop != "" {
		t.Fatalf("plain desktop chat workspace = %q", desktop)
	}
}

func TestForegroundContinuationPromptKeepsTheRecentOutcome(t *testing.T) {
	prefix := strings.Repeat("log line before the outcome ", 80)
	body := prefix + "MIDDLEMARKER-should-drop " + strings.Repeat("more log ", 80) + "no such service: omniroute-host"
	excerpt := chatTaskResultExcerpt(body, 480)
	if !strings.Contains(excerpt, "no such service: omniroute-host") || !strings.HasPrefix(excerpt, "log line") {
		t.Fatalf("excerpt = %s", excerpt)
	}
	if strings.Contains(excerpt, "MIDDLEMARKER-should-drop") {
		t.Fatalf("excerpt kept the middle: %s", excerpt)
	}
	if n := utf8.RuneCountInString(excerpt); n > 480 {
		t.Fatalf("excerpt runes = %d", n)
	}

	delta := make([]agent.ConversationEntry, 0, 10)
	for i := 1; i <= 8; i++ {
		delta = append(delta, agent.ConversationEntry{
			Role: "tool", ToolName: "ssh", ToolOutcome: "succeeded",
			Content: fmt.Sprintf("fact-%d", i),
		})
	}
	delta = append(delta,
		agent.ConversationEntry{Role: "tool", ToolName: "ssh", ToolOutcome: "failed", Content: body},
		agent.ConversationEntry{Role: "tool", ToolName: "ssh", ToolOutcome: "failed", Content: "[system rejected] " + chatTaskHandoffRequiredMessage},
	)
	project := `C:\Users\ma139\.maclaw\data\tasks\omni-1`
	prompt := foregroundChatContinuationPrompt(normalizeProjectSessionPath(project), "继续", "", delta)
	if !strings.Contains(prompt, normalizeProjectSessionPath(project)) {
		t.Fatalf("workspace missing: %s", prompt)
	}
	for _, early := range []string{"fact-1", "fact-2", "fact-3", "MIDDLEMARKER-should-drop", chatTaskHandoffRequiredMessage} {
		if strings.Contains(prompt, early) {
			t.Fatalf("prompt kept %q: %s", early, prompt)
		}
	}
	if !strings.Contains(prompt, "fact-4") || !strings.Contains(prompt, "fact-8") || !strings.Contains(prompt, "no such service: omniroute-host") {
		t.Fatalf("recent outcome missing: %s", prompt)
	}
	keptTools := chatTaskPromptToolNames(prompt)
	if len(keptTools) != 1 || keptTools[0] != "ssh" {
		t.Fatalf("kept tools = %v", keptTools)
	}
}

func TestChatTaskWorkerIsNotAnotherQuickLookup(t *testing.T) {
	search := intent.ClassificationResult{Primary: intent.LabelSearch, Confidence: 0.96}
	plain := NewLoopContext("chat", 30, nil)
	plain.Runtime.SemanticIntent = &search
	plain.Runtime.Execution = ExecutionProfile{Layer: string(executionLayerLight), PromptProfile: "light", ToolBudget: 1, IterationBudget: 3}
	if !loopContextBlocksLegacyToolRouter(plain) {
		t.Fatal("managed lookup still used the name router")
	}
	worker := NewLoopContext("chat-task", 30, nil)
	worker.ChatTaskID = "ct-1"
	worker.Runtime.SemanticIntent = &search
	worker.Runtime.Execution = plain.Runtime.Execution
	worker.semanticTurnAnswerOnly = true
	if loopContextBlocksLegacyToolRouter(worker) {
		t.Fatal("background chat task skipped the tool router")
	}
	applyChatTaskWorkerExecutionProfile(worker)
	if worker.Runtime.Execution.IsLight() || worker.Runtime.Execution.IterationBudget != 0 || worker.Runtime.Execution.Reason != "background chat task" || worker.semanticTurnAnswerOnly {
		t.Fatalf("profile = %+v answerOnly=%v", worker.Runtime.Execution, worker.semanticTurnAnswerOnly)
	}
	h, _ := testChatTaskHandler(t)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithContext(worker, desktopUserID, "Continue the user's task", desktopPlatform)
	if err != nil || handled || len(defs) != 0 || surface != nil {
		t.Fatalf("surface handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	project := `C:\Users\ma139\.maclaw\data\tasks\omni-update-1792449160929319200`
	if got := foregroundChatContinuationTitle("desktop-user:"+project, "继续"); got != "omni-update" {
		t.Fatalf("task title = %q", got)
	}
	if got := foregroundChatContinuationTitle(desktopUserID, "继续做配图版"); got != "继续做配图版" {
		t.Fatalf("chat title = %q", got)
	}
}

func TestOpenNextRepeatWaveStaysClosedAfterInlineHandoff(t *testing.T) {
	loop := NewLoopContext("chat", desktopChatForegroundIterationBudget, nil)
	loop.OfferChatTaskDispatch = true
	loop.ForegroundHandoffNeeded = true
	cb := &sharedAgentLoopCallbacks{
		loopCtx: loop,
		semanticSurface: &semanticCallSurface{
			grants: map[string]tool.InvocationGrant{"ssh": {Token: "live"}},
			plan: tool.ToolPlan{Selections: []tool.PlannedSelection{{
				ID: "selection:need:shell.execute.remote_host:abc", NeedID: "need:shell.execute.remote_host:abc", AdapterName: "ssh",
			}}},
		},
	}
	if cb.OpenNextRepeatWave("ssh") {
		t.Fatal("a spent chat turn reopened ssh")
	}
	if len(cb.semanticSurface.plan.Selections) != 1 {
		t.Fatalf("selections = %d", len(cb.semanticSurface.plan.Selections))
	}
}

func TestChatTaskProgressAndEmptyResult(t *testing.T) {
	if _, ok := chatTaskProgressMilestone("[Status] 已接收任务"); ok {
		t.Fatal("status prep was kept")
	}
	if _, ok := chatTaskProgressMilestone("Agent reasoning (2/30)..."); ok {
		t.Fatal("reasoning counter was kept")
	}
	if _, ok := chatTaskProgressMilestone("$ ls -la"); ok {
		t.Fatal("command narration was kept")
	}
	got, ok := chatTaskProgressMilestone("第 3 页已经写好")
	if !ok || got == "" {
		t.Fatalf("milestone = %q ok=%v", got, ok)
	}
	retry, ok := chatTaskProgressMilestone("[LiveStatus:llm-retry] 重试 2/5：API 暂时不可用，4 秒后重试")
	if !ok || retry != "重试 2/5：API 暂时不可用，4 秒后重试" {
		t.Fatalf("retry milestone = %q ok=%v", retry, ok)
	}
	if result := chatTaskResultFromAgentResponse(&IMAgentResponse{}, nil); result.Err == nil {
		t.Fatal("empty result was treated as done")
	}
	withFile := chatTaskResultFromAgentResponse(&IMAgentResponse{LocalFilePaths: []string{"/tmp/out.pptx"}}, nil)
	if withFile.Err != nil || len(withFile.Artifacts) != 1 {
		t.Fatalf("file result = %+v", withFile)
	}
}
