package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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
	if !strings.Contains(text, "steer_background_task") || !strings.Contains(text, "slides") {
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
	if !handled || resp == nil || resp.Text != "已停下。" || strings.Contains(resp.Text, first.ID) {
		t.Fatalf("stop = handled:%v resp:%+v", handled, resp)
	}
	if reg.activeCount(desktopUserID) != 0 {
		t.Fatalf("tasks still active: %d", reg.activeCount(desktopUserID))
	}
	again, handled := h.tryDesktopChatStopReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "停下，不干了。", Lang: "zh-Hans"})
	if !handled || again == nil || again.Text != "没有正在做的事。" {
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
	if got := h.rejectForegroundWorkTool(desktopUserID, "bash"); got == "" {
		t.Fatal("a third tool round stayed inline")
	}
	vision := NewLoopContext("chat-vision", desktopChatForegroundIterationBudget, nil)
	vision.OfferChatTaskDispatch = true
	h.sessionLoops.Store(desktopUserID, &sessionLoopState{loopCtx: vision})
	if got := h.rejectForegroundWorkTool(desktopUserID, "computer_observe"); got == "" {
		t.Fatal("computer use stayed inline")
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
	if result := chatTaskResultFromAgentResponse(&IMAgentResponse{}, nil); result.Err == nil {
		t.Fatal("empty result was treated as done")
	}
	withFile := chatTaskResultFromAgentResponse(&IMAgentResponse{LocalFilePaths: []string{"/tmp/out.pptx"}}, nil)
	if withFile.Err != nil || len(withFile.Artifacts) != 1 {
		t.Fatalf("file result = %+v", withFile)
	}
}
