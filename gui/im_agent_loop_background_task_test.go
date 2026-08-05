package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestPendingBackgroundTaskHintFromStatusIncludesOnlyActiveLoopTasks(t *testing.T) {
	loopStart := time.Now().Add(-1 * time.Minute)
	rs := RuntimeStatus{Tasks: []RuntimeTaskInfo{
		{TaskID: "bg_active", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep 100", StartedAt: loopStart.Add(10 * time.Second)},
		{TaskID: "bg_active", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep 100", StartedAt: loopStart.Add(10 * time.Second)},
		{TaskID: "  bg_spaced  ", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep 75", StartedAt: loopStart.Add(15 * time.Second)},
		{TaskID: "local_active", Source: runtimeTaskSourceLocal, Status: runtimeTaskStatusPending, Command: "sleep 50", StartedAt: loopStart.Add(20 * time.Second)},
		{TaskID: "", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep invalid", StartedAt: loopStart.Add(30 * time.Second)},
		{TaskID: "bg_old", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep 100", StartedAt: loopStart.Add(-10 * time.Second)},
		{TaskID: "local_done", Source: runtimeTaskSourceLocal, Status: runtimeTaskStatusCompleted, Command: "echo done", StartedAt: loopStart.Add(10 * time.Second)},
		{TaskID: "watcher", Source: runtimeTaskSourceSSH, Status: runtimeTaskStatusRunning, Command: "sleep 60 && tail build.log", TaskRole: "poll", StartedAt: loopStart.Add(25 * time.Second)},
		{TaskID: "local_watcher", Source: runtimeTaskSourceLocal, Status: runtimeTaskStatusRunning, Command: "sleep 60 && tail build.log", TaskRole: "poll", StartedAt: loopStart.Add(25 * time.Second)},
	}}

	hint := pendingBackgroundTaskHintFromStatus(rs, loopStart)
	if !strings.Contains(hint, "bg_active") || !strings.Contains(hint, `ssh(action="check_task"`) {
		t.Fatalf("active loop SSH task missing from hint: %q", hint)
	}
	if !strings.Contains(hint, `ssh(action="wait_task"`) {
		t.Fatalf("active SSH task hint should advertise bounded wait_task path: %q", hint)
	}
	if strings.Contains(hint, "bg_old") || strings.Contains(hint, "local_done") || strings.Contains(hint, "sleep invalid") || strings.Contains(hint, "watcher") || strings.Contains(hint, "local_watcher") {
		t.Fatalf("hint should exclude stale/completed/non-command tasks: %q", hint)
	}
	if strings.Count(hint, "sleep 100") != 1 {
		t.Fatalf("hint should deduplicate repeated task ids, got: %q", hint)
	}
	if !strings.Contains(hint, "bg_spaced") || strings.Contains(hint, "  bg_spaced  ") {
		t.Fatalf("hint should trim task ids, got: %q", hint)
	}
	if strings.Index(hint, "local_active") > strings.Index(hint, "bg_active") {
		t.Fatalf("hint should be sorted deterministically by source and task id, got: %q", hint)
	}
	if !hasPendingBackgroundTaskFromStatus(rs, loopStart) {
		t.Fatal("expected active in-loop task to be detected")
	}
	if hasPendingBackgroundTaskFromStatus(RuntimeStatus{Tasks: []RuntimeTaskInfo{rs.Tasks[4], rs.Tasks[5], rs.Tasks[6], rs.Tasks[7], rs.Tasks[8]}}, loopStart) {
		t.Fatal("stale/completed/non-command tasks should not count as pending")
	}
	if got := pendingBackgroundTaskKeyFromStatus(rs, loopStart); got != "local:local_active,ssh:bg_active,ssh:bg_spaced" {
		t.Fatalf("pending background task key = %q", got)
	}
}

func TestPendingBackgroundTaskNoToolReplyForcesRecover(t *testing.T) {
	hint := `- SSH background task bg_123 is still running -> call ssh(action="check_task", task_id="bg_123")`

	if !shouldRecoverForPendingBackgroundTaskNoToolReply("\u6211\u7ee7\u7eed\u7b49\uff0c100\u79d2\u540e\u68c0\u67e5\u3002", hint) {
		t.Fatal("wait-only reply with active background task should recover")
	}
	if !shouldRecoverForPendingBackgroundTaskNoToolReply("I will keep waiting and check back soon.", hint) {
		t.Fatal("English wait-only reply with active background task should recover")
	}
	if !shouldRecoverForPendingBackgroundTaskNoToolReply("done", hint) {
		t.Fatal("final-looking reply without task handoff should recover while task is active")
	}
	if !shouldRecoverForPendingBackgroundTaskNoToolReply("SSH background task bg_123 is still running; ask me to check later.", hint) {
		t.Fatal("explicit background-task handoff should still recover; active tasks need a concrete status check")
	}
	if shouldRecoverForPendingBackgroundTaskNoToolReply("I will keep waiting.", "") {
		t.Fatal("without an active task hint, background-task recover should not trigger")
	}
	if shouldRecoverForPendingBackgroundTaskNoToolReply("", hint) {
		t.Fatal("empty assistant content should use existing empty-result recovery path")
	}
}

func TestEmptyResultRecoverPromptClosesRecoverTag(t *testing.T) {
	prompt := buildEmptyResultRecoverPromptWithTasks("pending task")
	if !strings.HasSuffix(prompt, "\n[/Recover]") {
		t.Fatalf("empty-result recover prompt has malformed closing tag: %q", prompt)
	}
	if strings.Contains(prompt, "[/Recover ") {
		t.Fatalf("empty-result recover prompt should not mix recover tag variants: %q", prompt)
	}
}

func TestPendingBackgroundTaskRecoverPromptRequiresConcreteStatusTool(t *testing.T) {
	prompt := buildPendingBackgroundTaskRecoverPrompt(`- SSH 后台任务 bg_123 仍在运行`)
	if !strings.Contains(prompt, `ssh(action="wait_task"`) || !strings.Contains(prompt, "Call the appropriate status tool") {
		t.Fatalf("pending background recover prompt should force concrete status tooling, got %q", prompt)
	}
	if !strings.HasSuffix(prompt, "\n[/Recover]") {
		t.Fatalf("pending background recover prompt has malformed closing tag: %q", prompt)
	}
}

func TestClassifySSHToolActionWaitTask(t *testing.T) {
	if got := classifySSHToolAction("wait_task"); got != sshToolActionWaitTask {
		t.Fatalf("classifySSHToolAction(wait_task) = %q, want %q", got, sshToolActionWaitTask)
	}
}

func TestSSHToolSchemaAdvertisesWaitTask(t *testing.T) {
	registry := NewToolRegistry()
	registerBuiltinTools(registry, &IMMessageHandler{})
	sshTool, ok := registry.Get("ssh")
	if !ok {
		t.Fatal("ssh tool not registered")
	}

	actionSchema, ok := sshTool.InputSchema["action"].(map[string]string)
	if !ok || !strings.Contains(actionSchema["description"], "wait_task") {
		t.Fatalf("ssh action schema should advertise wait_task, got %#v", sshTool.InputSchema["action"])
	}
	taskIDSchema, ok := sshTool.InputSchema["task_id"].(map[string]string)
	if !ok || !strings.Contains(taskIDSchema["description"], "wait_task") {
		t.Fatalf("ssh task_id schema should mention wait_task, got %#v", sshTool.InputSchema["task_id"])
	}
	if _, ok := sshTool.InputSchema["timeout"]; !ok {
		t.Fatalf("ssh schema should include wait_task timeout parameter")
	}
}

func TestWaitSSHBackgroundTaskTrimsTaskID(t *testing.T) {
	manager := remote.NewSSHBackgroundTaskManager(nil)
	got := waitSSHBackgroundTask(manager, map[string]interface{}{"task_id": "   "}, "no manager", "missing task", nil)
	if got != "missing task" {
		t.Fatalf("blank task_id should be rejected before manager lookup, got %q", got)
	}
}

func TestRegisterSSHBackgroundLoopIsIdempotent(t *testing.T) {
	mgr := NewBackgroundLoopManager(nil)
	h := &IMMessageHandler{bgManager: mgr}
	session := &remote.SSHManagedSession{ID: "ssh_root@example.com_1"}
	cfg := remote.SSHHostConfig{Host: "example.com", User: "root", Port: 22, Label: "prod"}

	h.registerSSHBackgroundLoop(session, cfg)
	h.registerSSHBackgroundLoop(session, cfg)

	loops := mgr.List()
	if len(loops) != 1 {
		t.Fatalf("registering the same SSH session should create one loop, got %d", len(loops))
	}
	if loops[0].SlotKind != SlotKindSSH || loops[0].SessionID != session.ID {
		t.Fatalf("unexpected SSH loop: kind=%s session=%q", loops[0].SlotKind, loops[0].SessionID)
	}
	if loops[0].LoopState() != LoopStateRunning {
		t.Fatalf("SSH loop state = %s, want running", loops[0].LoopState())
	}

	loops[0].SetLoopState(LoopStatePaused)
	h.registerSSHBackgroundLoop(session, cfg)
	if got := mgr.List()[0].LoopState(); got != LoopStateRunning {
		t.Fatalf("existing SSH loop should be refreshed to running, got %s", got)
	}
}

func TestFormatSSHBackgroundTaskStatusPreservesRunningState(t *testing.T) {
	status := &remote.BackgroundTaskStatus{
		TaskID:  "bg_1",
		Command: "sleep 30",
		Status:  remote.SSHBackgroundTaskStatusRunning,
		IsAlive: true,
		Elapsed: "5s",
	}

	formatted := formatSSHBackgroundTaskStatus(status)
	if !strings.Contains(formatted, "[running]") || !strings.Contains(formatted, "status: running") {
		t.Fatalf("formatted running status missing active markers: %q", formatted)
	}
}

func TestSSHBackgroundTaskSnapshotIncludesLocalMirror(t *testing.T) {
	mirrorFile := filepath.Join(t.TempDir(), "bg_1.log")
	if err := os.WriteFile(mirrorFile, []byte("status: completed\n--- latest log ---\ndone\nEXIT: 0\n"), 0o600); err != nil {
		t.Fatalf("write mirror: %v", err)
	}

	snapshot := sshBackgroundTaskMirrorSnapshot(mirrorFile)
	if !strings.Contains(snapshot, "local_mirror: "+mirrorFile) || !strings.Contains(snapshot, "EXIT: 0") {
		t.Fatalf("snapshot missing mirror content: %q", snapshot)
	}
}

func TestSSHBackgroundTaskSnapshotForOwnerRejectsDifferentOwner(t *testing.T) {
	dir := t.TempDir()
	mirrorFile := filepath.Join(dir, "mirror.log")
	if err := os.WriteFile(mirrorFile, []byte("secret build output"), 0o600); err != nil {
		t.Fatalf("write mirror: %v", err)
	}
	registry := map[string]interface{}{
		"updated_at": time.Now(),
		"tasks": []map[string]interface{}{
			{
				"task_id":     "bg_secret",
				"owner_id":    "owner-a",
				"session_id":  "ssh_1",
				"command":     "cat secret.txt",
				"log_file":    "/tmp/bg_secret.log",
				"pid_file":    "/tmp/bg_secret.pid",
				"mirror_file": mirrorFile,
				"status":      "running",
				"pid":         "123",
				"started_at":  time.Now(),
			},
		},
	}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ssh_bg_tasks.json"), data, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	mgr := remote.NewSSHBackgroundTaskManager(nil)
	mgr.SetPersistDir(dir)

	got := sshBackgroundTaskSnapshotForOwner(mgr, "bg_secret", "owner-b")
	if got != "" {
		t.Fatalf("cross-owner snapshot should be empty, got %q", got)
	}
	got = sshBackgroundTaskSnapshotForOwner(mgr, "bg_secret", "owner-a")
	if !strings.Contains(got, "cat secret.txt") || !strings.Contains(got, "secret build output") {
		t.Fatalf("same-owner snapshot should include evidence, got %q", got)
	}
}

func TestStartSSHBackgroundTaskMirrorWatcherDeduplicatesTaskID(t *testing.T) {
	h := &IMMessageHandler{bgTaskMgr: remote.NewSSHBackgroundTaskManager(nil)}

	if !h.registerSSHBackgroundTaskMirrorWatcher("bg_1") {
		t.Fatal("first watcher registration should succeed")
	}
	if h.registerSSHBackgroundTaskMirrorWatcher("bg_1") {
		t.Fatal("second watcher registration should be deduplicated")
	}

	h.sshMirrorWatchMu.Lock()
	defer h.sshMirrorWatchMu.Unlock()
	if len(h.sshMirrorWatch) != 1 {
		t.Fatalf("watcher map size = %d, want 1", len(h.sshMirrorWatch))
	}
	if _, ok := h.sshMirrorWatch["bg_1"]; !ok {
		t.Fatalf("watcher map missing bg_1: %#v", h.sshMirrorWatch)
	}
}

func TestAuthorizeLocalBackgroundTaskOwnerRejectsDifferentOwner(t *testing.T) {
	mgr := coretool.NewLocalBackgroundTaskManager(t.TempDir())
	command := "echo done"
	if runtime.GOOS == "windows" {
		command = "Write-Output done"
	}
	task, err := mgr.SubmitWithOwner(command, "", "command", "owner-a")
	if err != nil {
		t.Fatalf("SubmitWithOwner: %v", err)
	}

	if err := authorizeLocalBackgroundTaskOwner(mgr, task.TaskID, "owner-a"); err != nil {
		t.Fatalf("same owner should be allowed: %v", err)
	}
	if err := authorizeLocalBackgroundTaskOwner(mgr, task.TaskID, "owner-b"); err == nil || !strings.Contains(err.Error(), "another runtime owner") {
		t.Fatalf("different owner should be rejected, got %v", err)
	}
	if err := authorizeLocalBackgroundTaskOwner(mgr, task.TaskID, ""); err == nil || !strings.Contains(err.Error(), "another runtime owner") {
		t.Fatalf("blank owner must not access another owner's task, got %v", err)
	}
}

func TestAsyncWaitListCountsFilteredOwnerTasks(t *testing.T) {
	mgr := coretool.NewLocalBackgroundTaskManager(t.TempDir())
	command := "echo done"
	if runtime.GOOS == "windows" {
		command = "Write-Output done"
	}
	taskA, err := mgr.SubmitWithOwner(command, "", "command", "owner-a")
	if err != nil {
		t.Fatalf("SubmitWithOwner owner-a: %v", err)
	}
	taskB, err := mgr.SubmitWithOwner(command, "", "command", "owner-b")
	if err != nil {
		t.Fatalf("SubmitWithOwner owner-b: %v", err)
	}

	got := (&IMMessageHandler{}).asyncWaitList(mgr, "owner-a")
	if !strings.Contains(got, taskA.TaskID) {
		t.Fatalf("filtered list should include owner-a task, got %q", got)
	}
	if strings.Contains(got, taskB.TaskID) {
		t.Fatalf("filtered list leaked owner-b task: %q", got)
	}
	rowCount := strings.Count(got, "\n- ")
	if strings.HasPrefix(got, "- ") {
		rowCount++
	}
	if rowCount != 1 {
		t.Fatalf("filtered list should render one task row, rows=%d got %q", rowCount, got)
	}
}

func TestFormatSSHBackgroundTaskStatusTruncatesUTF8Safely(t *testing.T) {
	status := &remote.BackgroundTaskStatus{
		TaskID:  "bg_utf8",
		Command: "echo utf8",
		Status:  remote.SSHBackgroundTaskStatusCompleted,
		Elapsed: "1s",
		LogTail: strings.Repeat("\u754c", 7000),
	}

	formatted := formatSSHBackgroundTaskStatus(status)
	if !strings.Contains(formatted, "... (truncated) ...") {
		t.Fatalf("formatted output should be truncated: len=%d", len([]rune(formatted)))
	}
	if strings.ContainsRune(formatted, '\uFFFD') {
		t.Fatalf("formatted output contains replacement rune after UTF-8 truncation")
	}
}

func TestTruncateRunesMiddleKeepsUTF8SafeSSHExecBudget(t *testing.T) {
	output := strings.Repeat("界", 9000)

	truncated := truncateRunesMiddle(output, 4000, 4000)

	if !strings.Contains(truncated, "... (truncated) ...") {
		t.Fatalf("expected truncation marker, got len=%d", len([]rune(truncated)))
	}
	if strings.ContainsRune(truncated, '\uFFFD') {
		t.Fatalf("truncated output contains replacement rune")
	}
	if !strings.HasPrefix(truncated, strings.Repeat("界", 20)) || !strings.HasSuffix(truncated, strings.Repeat("界", 20)) {
		t.Fatalf("truncated output should preserve prefix and suffix")
	}
}

func TestBoundedIntArgAcceptsJSONAndNativeNumbers(t *testing.T) {
	args := map[string]interface{}{
		"json_float":  float64(42),
		"json_number": json.Number("43"),
		"native_int":  7,
		"string_int":  "8",
		"too_large":   700,
		"too_small":   1,
	}
	if got := boundedIntArg(args, "json_float", 60, 5, 600); got != 42 {
		t.Fatalf("float64 arg = %d, want 42", got)
	}
	if got := boundedIntArg(args, "json_number", 60, 5, 600); got != 43 {
		t.Fatalf("json.Number arg = %d, want 43", got)
	}
	if got := boundedIntArg(args, "native_int", 60, 5, 600); got != 7 {
		t.Fatalf("int arg = %d, want 7", got)
	}
	if got := boundedIntArg(args, "string_int", 60, 5, 600); got != 8 {
		t.Fatalf("string arg = %d, want 8", got)
	}
	if got := boundedIntArg(args, "too_large", 60, 5, 600); got != 600 {
		t.Fatalf("large arg = %d, want 600", got)
	}
	if got := boundedIntArg(args, "too_small", 60, 5, 600); got != 5 {
		t.Fatalf("small arg = %d, want 5", got)
	}
	if got := boundedIntArg(args, "missing", 60, 5, 600); got != 60 {
		t.Fatalf("missing arg = %d, want 60", got)
	}
}

func TestPendingBackgroundTaskRecoverPreemptsNoToolHardCap(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir())}
	ctx := NewLoopContext("pending-bg-hardcap", 10, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	phase := &agentLoopPhase{ConsecutiveNoTool: maxConsecutiveNoTool}
	result := h.handleAgentLoopNoToolPath(agentLoopNoToolPathOptions{
		Context:                  ctx,
		UserID:                   desktopUserID,
		UserText:                 "run long task",
		MessageContent:           "I will keep waiting and check back soon.",
		Choice:                   llm.Choice{Message: llm.Message{Role: "assistant", Content: "I will keep waiting and check back soon."}},
		Phase:                    phase,
		LengthContinuationBuffer: &strings.Builder{},
		AttachLLMTelemetry:       func(*IMAgentResponse) {},
		AttachVisibleArtifacts:   func(*IMAgentResponse) {},
	})

	if !result.ContinueLoop {
		t.Fatalf("expected pending background task recover to continue before no-tool hard cap; response=%#v", result.Response)
	}
	if result.Response != nil {
		t.Fatalf("expected no final response while background task is active, got %#v", result.Response)
	}
	if phase.RecoverReason != agentRecoverBackgroundTaskPending {
		t.Fatalf("recover reason = %q, want %q", phase.RecoverReason, agentRecoverBackgroundTaskPending)
	}
}

func TestNoToolHardCapIncludesPendingBackgroundTaskHint(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir()), memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	ctx := NewLoopContext("pending-bg-hardcap-hint", 10, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	phase := &agentLoopPhase{ConsecutiveNoTool: maxConsecutiveNoTool, TotalRecoverInjections: maxTotalRecoverInjections}
	result := h.handleAgentLoopNoToolPath(agentLoopNoToolPathOptions{
		Context:                  ctx,
		UserID:                   desktopUserID,
		UserText:                 "run long task",
		MessageContent:           "I will keep waiting and check back soon.",
		Choice:                   llm.Choice{Message: llm.Message{Role: "assistant", Content: "I will keep waiting and check back soon."}},
		Phase:                    phase,
		LengthContinuationBuffer: &strings.Builder{},
		AttachLLMTelemetry:       func(*IMAgentResponse) {},
		AttachVisibleArtifacts:   func(*IMAgentResponse) {},
	})

	if result.Response == nil {
		t.Fatal("expected hard-cap final response after recover cap")
	}
	if !strings.Contains(result.Response.Text, task.TaskID) || !strings.Contains(result.Response.Text, `async_wait(action="check"`) {
		t.Fatalf("hard-cap response should include pending task hint, got %q", result.Response.Text)
	}
}

func TestNoToolNormalFinalizeIncludesPendingBackgroundTaskHintAfterRecoverCap(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir()), memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	ctx := NewLoopContext("pending-bg-normal-finalize-hint", 10, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	phase := &agentLoopPhase{ConsecutiveNoTool: 1, TotalRecoverInjections: maxTotalRecoverInjections}
	result := h.handleAgentLoopNoToolPath(agentLoopNoToolPathOptions{
		Context:                  ctx,
		UserID:                   desktopUserID,
		UserText:                 "run long task",
		MessageContent:           "done",
		Choice:                   llm.Choice{Message: llm.Message{Role: "assistant", Content: "done"}},
		Phase:                    phase,
		LengthContinuationBuffer: &strings.Builder{},
		AttachLLMTelemetry:       func(*IMAgentResponse) {},
		AttachVisibleArtifacts:   func(*IMAgentResponse) {},
	})

	if result.Response == nil {
		t.Fatal("expected normal final response after recover cap")
	}
	if !strings.Contains(result.Response.Text, task.TaskID) || !strings.Contains(result.Response.Text, `async_wait(action="check"`) {
		t.Fatalf("normal final response should include pending task hint, got %q", result.Response.Text)
	}
}

func TestPendingBackgroundTaskExtendsFinalizationBoundary(t *testing.T) {
	if got := extendEffectiveMaxForPendingBackgroundTask(3, 3, true); got != 4 {
		t.Fatalf("extendEffectiveMaxForPendingBackgroundTask() = %d, want 4", got)
	}
	if got := extendEffectiveMaxForPendingBackgroundTask(4, 3, true); got != 3 {
		t.Fatalf("late pending task should not extend forever, got %d", got)
	}
	if got := extendEffectiveMaxForPendingBackgroundTask(2, 3, true); got != 3 {
		t.Fatalf("early pending task should not change effective max, got %d", got)
	}
	if got := extendEffectiveMaxForPendingBackgroundTask(3, 3, false); got != 3 {
		t.Fatalf("without pending task effective max changed to %d", got)
	}
}

func TestPendingBackgroundTaskBoundaryExtensionPersistsOnce(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir())}
	ctx := NewLoopContext("pending-bg-boundary", 3, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	if got := h.applyAgentLoopBoundaryExtensions(ctx, desktopUserID, 2, 3); got != 3 {
		t.Fatalf("early boundary extension = %d, want 3", got)
	}
	if got := h.applyAgentLoopBoundaryExtensions(ctx, desktopUserID, 3, 3); got != 4 {
		t.Fatalf("boundary extension = %d, want 4", got)
	}
	if got := ctx.MaxIterations(); got != 4 {
		t.Fatalf("ctx max after boundary extension = %d, want 4", got)
	}
	if got := h.applyAgentLoopBoundaryExtensions(ctx, desktopUserID, 4, 4); got != 4 {
		t.Fatalf("second boundary extension should not grow forever, got %d", got)
	}
}

func TestLoopContextBackgroundTaskBoundaryExtensionKey(t *testing.T) {
	ctx := NewLoopContext("pending-bg-key", 3, nil)
	if !ctx.MarkBackgroundTaskBoundaryExtended("local:a,ssh:b") {
		t.Fatal("first background task key should be accepted")
	}
	if ctx.MarkBackgroundTaskBoundaryExtended("ssh:b") {
		t.Fatal("remaining old background task should not extend twice after task set shrinks")
	}
	if ctx.MarkBackgroundTaskBoundaryExtended("local:a,ssh:b") {
		t.Fatal("same background task set should not extend twice")
	}
	if !ctx.MarkBackgroundTaskBoundaryExtended("ssh:b,local:c") {
		t.Fatal("new background task key should be able to extend once")
	}
}

func TestResponseNeutralToolDefersFinalizationForActiveBackgroundTask(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir()), memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	ctx := NewLoopContext("response-neutral-bg-defer", 10, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	phase := &agentLoopPhase{}

	// Simulate: LLM produced visible text + called compress_context (response-neutral tool)
	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                    ctx,
		UserID:                     desktopUserID,
		UserText:                   "update omniroute",
		Iteration:                  5,
		MessageContent:             "正在等待 Docker 构建完成。让我同时压缩一下上下文。",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "compress_context", Arguments: `{"summary":"building docker"}`}}},
		ToolResults:                []string{"上下文压缩已排队。"},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
		Phase:                      phase,
		History:                    []agent.ConversationEntry{{Role: "user", Content: "update omniroute"}},
		AttachLLMTelemetry:         func(*IMAgentResponse) {},
		AttachVisibleArtifacts:     func(*IMAgentResponse) {},
	})

	// compress_context never finalizes — loop always continues regardless of background tasks
	if result.Response != nil {
		t.Fatalf("expected loop to continue (response=nil) after compress_context, got text=%q", result.Response.Text)
	}
}

func TestCompressContextContinuesLoopWhenNoBackgroundTask(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir()), memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	ctx := NewLoopContext("response-neutral-no-bg", 10, nil)

	phase := &agentLoopPhase{}

	// No background task running — compress_context should still NOT finalize.
	// The LLM compresses context to free space for upcoming work.
	// If it's truly done, the next iteration will produce no tool calls and
	// the no-tool branch will finalize naturally.
	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                    ctx,
		UserID:                     desktopUserID,
		UserText:                   "summarize work",
		Iteration:                  5,
		MessageContent:             "已完成所有工作。",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "compress_context", Arguments: `{"summary":"all done"}`}}},
		ToolResults:                []string{"上下文压缩已排队。"},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
		Phase:                      phase,
		History:                    []agent.ConversationEntry{{Role: "user", Content: "summarize work"}},
		AttachLLMTelemetry:         func(*IMAgentResponse) {},
		AttachVisibleArtifacts:     func(*IMAgentResponse) {},
	})

	// Must NOT finalize — loop continues so LLM can deliver or confirm completion
	if result.Response != nil {
		t.Fatalf("compress_context must not finalize loop even without background tasks, got response %q", result.Response.Text)
	}
}

func TestCompressContextContinuesLoopEvenWhenRecoverCapReached(t *testing.T) {
	h := &IMMessageHandler{localBgTaskMgr: coretool.NewLocalBackgroundTaskManager(t.TempDir()), memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	ctx := NewLoopContext("response-neutral-bg-cap", 10, nil)

	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "Start-Sleep -Seconds 30"
	}
	task, err := h.localBgTaskMgr.Submit(command, "")
	if err != nil {
		t.Fatalf("submit background task: %v", err)
	}
	defer func() { _ = h.localBgTaskMgr.Kill(task.TaskID) }()

	// Simulate: recover cap already reached
	phase := &agentLoopPhase{TotalRecoverInjections: maxTotalRecoverInjections}

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		Context:                    ctx,
		UserID:                     desktopUserID,
		UserText:                   "continue waiting",
		Iteration:                  12,
		MessageContent:             "Still waiting for build.",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call_1", Type: "function", Function: llm.ToolCallFunction{Name: "compress_context", Arguments: `{"summary":"waiting"}`}}},
		ToolResults:                []string{"上下文压缩已排队。"},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
		Phase:                      phase,
		History:                    []agent.ConversationEntry{{Role: "user", Content: "continue waiting"}},
		AttachLLMTelemetry:         func(*IMAgentResponse) {},
		AttachVisibleArtifacts:     func(*IMAgentResponse) {},
	})

	// compress_context never finalizes — loop continues regardless of recover cap or background tasks
	if result.Response != nil {
		t.Fatalf("compress_context must not finalize loop even with active bg task + recover cap, got response %q", result.Response.Text)
	}
	_ = task // referenced above in defer
}
