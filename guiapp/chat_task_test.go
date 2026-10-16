package guiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if !handled || resp == nil || !strings.Contains(resp.Text, "ct-live") || !strings.Contains(resp.Text, "building page 3") {
		t.Fatalf("status = handled:%v resp:%+v", handled, resp)
	}
	if _, handled := h.tryChatTaskStatusReply(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "please write a new essay about rivers"}); handled {
		t.Fatal("a new request was treated as a status question")
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
