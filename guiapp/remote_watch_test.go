package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestRemoteWatchParseSleepPoll(t *testing.T) {
	incident := `bash -lc 'sleep 300; echo "=== $(date "+%F %T") ==="; tail -n 5 /tmp/strata_watch.log'`
	wait, check, ok := parseRemoteSleepPoll(incident)
	if !ok || wait != 300*time.Second {
		t.Fatalf("incident wait=%s ok=%v", wait, ok)
	}
	wantCheck := `echo "=== $(date "+%F %T") ==="; tail -n 5 /tmp/strata_watch.log`
	if check != wantCheck {
		t.Fatalf("incident check = %q", check)
	}

	cases := []struct {
		name  string
		cmd   string
		ok    bool
		wait  time.Duration
		check string
	}{
		{name: "plain", cmd: "sleep 30; tail -n 5 /tmp/log", ok: true, wait: 30 * time.Second, check: "tail -n 5 /tmp/log"},
		{name: "and", cmd: "sleep 5 && ps aux | grep strata", ok: true, wait: 5 * time.Second, check: "ps aux | grep strata"},
		{name: "minutes", cmd: "sleep 5m; tail -n 1 /tmp/x", ok: true, wait: 5 * time.Minute, check: "tail -n 1 /tmp/x"},
		{name: "clamped", cmd: "sleep 7200; tail -n 1 /tmp/x", ok: true, wait: remoteWatchMaxInterval, check: "tail -n 1 /tmp/x"},
		{name: "double quotes", cmd: `bash -c "sleep 12; tail -n 3 /tmp/a.log"`, ok: true, wait: 12 * time.Second, check: "tail -n 3 /tmp/a.log"},
		{name: "stderr null", cmd: "sleep 1; tail -n 1 /tmp/x 2>/dev/null", ok: true, wait: time.Second, check: "tail -n 1 /tmp/x 2>/dev/null"},
		{name: "bare", cmd: "sleep 300", ok: false},
		{name: "not leading", cmd: "echo hi; sleep 300; tail -n 1 /tmp/x", ok: false},
		{name: "not a check", cmd: "sleep 300; rm -rf /tmp/x", ok: false},
		{name: "launch", cmd: "sleep 10; strata launch --family hy4", ok: false},
		{name: "redirect", cmd: "sleep 1; tail -n 1 /tmp/x > /tmp/out", ok: false},
		{name: "sed in place", cmd: "sleep 1; sed -i 's/a/b/' /tmp/x", ok: false},
		{name: "background", cmd: "sleep 1; tail -n 1 /tmp/x &", ok: false},
		{name: "follow", cmd: "sleep 1; tail -f /tmp/x", ok: false},
		{name: "follow long", cmd: "sleep 1; tail --follow=name /tmp/x", ok: false},
		{name: "follow zero cluster", cmd: "sleep 1; tail -0f /tmp/x", ok: false},
		{name: "tail n cluster", cmd: "sleep 1; tail -n5 /tmp/x", ok: true, wait: time.Second, check: "tail -n5 /tmp/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotWait, gotCheck, gotOK := parseRemoteSleepPoll(tc.cmd)
			if gotOK != tc.ok || (tc.ok && (gotWait != tc.wait || gotCheck != tc.check)) {
				t.Fatalf("parse(%q) = %s %q %v", tc.cmd, gotWait, gotCheck, gotOK)
			}
		})
	}
}

func TestRemoteWatchParentPrincipal(t *testing.T) {
	const project = `desktop-user:C:\work\proj`
	if got := chatTaskParentPrincipal(project + ":task:ct-1"); got != project {
		t.Fatalf("project parent = %q", got)
	}
	if got := chatTaskParentPrincipal("desktop-user:task:ct-1"); got != "desktop-user" {
		t.Fatalf("main chat parent = %q", got)
	}
	if got := chatTaskParentPrincipal(project); got != project {
		t.Fatalf("already parent = %q", got)
	}
}

func TestRemoteWatchClockLineDoesNotCountAsChange(t *testing.T) {
	a := "=== 2026-11-23 18:00:00 ===\nsize 10"
	b := "=== 2026-11-23 18:05:00 ===\nsize 10"
	c := "=== 2026-11-23 18:05:00 ===\nsize 11"
	if normalizeRemoteWatchOutput(a) != normalizeRemoteWatchOutput(b) {
		t.Fatalf("clock banner counted as a change: %q vs %q", normalizeRemoteWatchOutput(a), normalizeRemoteWatchOutput(b))
	}
	if normalizeRemoteWatchOutput(a) == normalizeRemoteWatchOutput(c) {
		t.Fatal("a new log tail was ignored")
	}
	timeoutA := "trusted_ssh_timeout: " + a
	timeoutB := "trusted_ssh_timeout: " + b
	timeoutC := "trusted_ssh_timeout: " + c
	if !remoteWatchSameFailure(timeoutA, timeoutB) {
		t.Fatal("a clock banner inside a timeout counted as a new failure")
	}
	if remoteWatchSameFailure(timeoutA, timeoutC) {
		t.Fatal("a new log tail inside a timeout was ignored")
	}
	if !remoteWatchSameFailure("trusted_ssh_timeout", "trusted_ssh_timeout") {
		t.Fatal("a bare timeout did not match itself")
	}
	if remoteWatchSameFailure("trusted_ssh_outcome_unobserved", "trusted_ssh_timeout") {
		t.Fatal("different failures matched")
	}
}

func TestRemoteWatchProgressLineIsAStep(t *testing.T) {
	line := remoteWatchProgressLine(5*time.Minute, "tail -n 5 /tmp/strata_watch.log")
	got, ok := chatTaskProgressMilestone(line)
	if !ok || strings.Contains(got, "sleep") || !strings.Contains(got, "再次查看") || !strings.Contains(got, "tail -n 5") {
		t.Fatalf("progress = %q ok=%v", got, ok)
	}
	if _, ok := chatTaskProgressMilestone(remoteWatchLivenessLine()); ok {
		t.Fatal("liveness line became a card step")
	}
}

func TestRemoteWatchChatTaskDoesNotWriteSleep(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const (
		taskID  = "ct-1"
		parent  = `desktop-user:C:\work\proj`
		memory  = parent + ":task:" + taskID
		command = `bash -lc 'sleep 300; echo "=== $(date "+%F %T") ==="; tail -n 5 /tmp/strata_watch.log'`
	)
	insertRemoteWatchTask(t, h, taskID, parent, memory)
	var principal, ran string
	var progress []string
	h.semanticTrustedSSH = func(user, command string) (string, error) {
		principal, ran = user, command
		return "size 10", nil
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("sleep poll waited inside the tool")
		return nil
	})
	defer restore()
	loop := &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})}
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "ignored",
		loopCtx: loop,
		onProgress: func(line string) {
			progress = append(progress, line)
		},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: memory}},
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, command))
	if strings.Contains(ran, "sleep") || ran != `echo "=== $(date "+%F %T") ==="; tail -n 5 /tmp/strata_watch.log` {
		t.Fatalf("remote command = %q", ran)
	}
	if principal != parent {
		t.Fatalf("principal = %q", principal)
	}
	if !strings.Contains(got, remoteWatchDoneToken) || !strings.Contains(got, "did not write sleep") || !strings.Contains(got, "size 10") {
		t.Fatalf("tool result = %q", got)
	}
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || watch.Interval != 300*time.Second || watch.Check != ran || watch.Principal != parent || strings.TrimSpace(watch.LastOutput) != "size 10" {
		t.Fatalf("watch = %+v armed=%v", watch, armed)
	}
	if len(progress) == 0 || strings.Contains(progress[0], "sleep") || !strings.Contains(progress[0], "再次查看") {
		t.Fatalf("card progress = %#v", progress)
	}
}

func TestRemoteWatchForegroundStillWritesSleep(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const command = "sleep 30; tail -n 1 /tmp/log"
	var ran string
	h.semanticTrustedSSH = func(user, command string) (string, error) {
		if user != "desktop-user" {
			t.Fatalf("principal = %q", user)
		}
		ran = command
		return "ok", nil
	}
	callbacks := &sharedAgentLoopCallbacks{handler: h, userID: "desktop-user"}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, command))
	if ran != command || strings.Contains(got, remoteWatchDoneToken) {
		t.Fatalf("foreground command = %q result = %q", ran, got)
	}
}

func TestRemoteWatchNonPollDisarms(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-arm"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "old")
	var ran, principal string
	h.semanticTrustedSSH = func(user, command string) (string, error) {
		principal, ran = user, command
		return "started", nil
	}
	loop := &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})}
	callbacks := &sharedAgentLoopCallbacks{
		handler:         h,
		userID:          "desktop-user:task:" + taskID,
		loopCtx:         loop,
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	_ = callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "mkdir -p /data/models"))
	if ran != "mkdir -p /data/models" || principal != "desktop-user" {
		t.Fatalf("command = %q principal = %q", ran, principal)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); armed {
		t.Fatal("a service command left the watch armed")
	}
}

func TestRemoteWatchStatusCheckKeepsTheWatch(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-tail"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 5 /tmp/a.log", "old")
	h.semanticTrustedSSH = func(string, string) (string, error) { return "new tail", nil }
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		userID:  "desktop-user:task:" + taskID,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
	}
	_ = callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "tail -n 1 /tmp/b.log"))
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || watch.Check != "tail -n 1 /tmp/b.log" || strings.TrimSpace(watch.LastOutput) != "new tail" {
		t.Fatalf("watch = %+v armed=%v", watch, armed)
	}
}

func TestRemoteWatchQuietPollDoesNotFinish(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const (
		taskID = "ct-quiet"
		parent = "desktop-user"
		check  = "tail -n 1 /tmp/log"
	)
	task := insertRemoteWatchTask(t, h, taskID, parent, parent+":task:"+taskID)
	armTestRemoteWatch(h, taskID, parent, check, "downloading 10%")
	var commands []string
	var sshN int
	h.semanticTrustedSSH = func(user, command string) (string, error) {
		if user != parent || command != check || strings.Contains(command, "sleep") {
			t.Fatalf("recheck %s %q", user, command)
		}
		commands = append(commands, command)
		sshN++
		if sshN == 1 {
			return "downloading 10%", nil
		}
		return "downloading 100%", nil
	}
	waits := 0
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		waits++
		if waits > 4 {
			return context.Canceled
		}
		return nil
	})
	defer restore()
	calls := 0
	var prompts []string
	result := h.driveChatTaskWaves(task, func(_ *chatTaskRecord, _ int, text string) chatTaskWaveOutcome {
		calls++
		prompts = append(prompts, text)
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
		}
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n下载完成"}}
	})
	if result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("result = %+v", result)
	}
	if calls != 2 || waits != 2 || len(commands) != 2 {
		t.Fatalf("calls=%d waits=%d checks=%d prompts=%q", calls, waits, len(commands), prompts)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[1], "downloading 100%") || !strings.Contains(prompts[1], remoteWatchDoneToken) {
		t.Fatalf("wake prompt = %q", prompts)
	}
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	progress := reg.tasks[taskID].Progress
	reg.mu.Unlock()
	if strings.Contains(progress, "sleep") || !strings.Contains(progress, "再次查看") || !strings.Contains(progress, check) {
		t.Fatalf("card progress = %q", progress)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); armed {
		t.Fatal("finished watch stayed armed")
	}
}

func TestRemoteWatchDoneTokenFinishes(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-done"
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "done")
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("done token still parked")
		return nil
	})
	defer restore()
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n服务已启动"}}
	})
	if calls != 1 || result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); armed {
		t.Fatal("done token left the watch armed")
	}
}

func TestRemoteWatchSealedLoopKeepsTheSteer(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const (
		taskID  = "ct-steer-keep"
		session = "desktop-user"
		memory  = session + ":task:" + taskID
	)
	task := insertRemoteWatchTask(t, h, taskID, session, memory)
	reg := h.ensureChatTasks()
	loop := &LoopContext{CancelC: make(chan struct{})}
	if !loop.TrySealReplans(loop.ReplanRevision()) {
		t.Fatal("wave loop did not seal")
	}
	reg.mu.Lock()
	task.loop = loop
	reg.mu.Unlock()
	if err := reg.Steer(taskID, "too early"); err == nil {
		t.Fatal("a sealed loop without a watch accepted a steer")
	}
	armTestRemoteWatch(h, taskID, session, "tail -n 1 /tmp/log", "downloading 10%")
	kind, err := reg.continueOpenChatTask(session, "也看一下显存", "")
	if err != nil || kind != chatTaskOpenContinueSteered {
		t.Fatalf("continue = %v err=%v", kind, err)
	}
	raw, ok := h.pendingInjection.Load(memory)
	if !ok || !strings.Contains(raw.(string), "也看一下显存") {
		t.Fatalf("task injection = %#v ok=%v", raw, ok)
	}
	if _, ok := h.pendingInjection.Load(session); ok {
		t.Fatal("steer landed on the parent session")
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("a pending supplement still waited for the remote")
		return nil
	})
	defer restore()
	h.semanticTrustedSSH = func(string, string) (string, error) {
		t.Fatal("a pending supplement still opened SSH")
		return "", nil
	}
	calls := 0
	var prompts []string
	result := h.driveChatTaskWaves(task, func(_ *chatTaskRecord, _ int, text string) chatTaskWaveOutcome {
		calls++
		prompts = append(prompts, text)
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
		}
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n已纳入补充"}}
	})
	if calls != 2 || result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[1], "用户补充") || !strings.Contains(prompts[1], remoteWatchDoneToken) {
		t.Fatalf("steer prompt = %q", prompts)
	}
}

func TestRemoteWatchSteerWakesThePark(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const (
		taskID = "ct-steer"
		memory = "desktop-user:task:" + taskID
	)
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", memory)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "downloading 10%")
	reg := h.ensureChatTasks()
	loop := &LoopContext{CancelC: make(chan struct{})}
	if !loop.TrySealReplans(loop.ReplanRevision()) {
		t.Fatal("wave loop did not seal")
	}
	reg.mu.Lock()
	task.loop = loop
	reg.mu.Unlock()
	h.semanticTrustedSSH = func(string, string) (string, error) {
		t.Fatal("steer woke by opening SSH")
		return "", nil
	}
	waits := 0
	restore := swapRemoteWatchWait(func(ctx context.Context, _ time.Duration, _ func()) error {
		waits++
		if err := reg.Steer(taskID, "也看一下显存"); err != nil {
			t.Errorf("steer: %v", err)
			return err
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			t.Error("steer did not wake the park")
			return context.DeadlineExceeded
		}
	})
	defer restore()
	calls := 0
	var prompts []string
	result := h.driveChatTaskWaves(task, func(_ *chatTaskRecord, _ int, text string) chatTaskWaveOutcome {
		calls++
		prompts = append(prompts, text)
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
		}
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n已纳入补充"}}
	})
	if calls != 2 || waits != 1 || result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("calls=%d waits=%d result=%+v", calls, waits, result)
	}
	if len(prompts) < 2 || !strings.Contains(prompts[1], "用户补充") || !strings.Contains(prompts[1], remoteWatchDoneToken) {
		t.Fatalf("steer prompt = %q", prompts)
	}
	raw, ok := h.pendingInjection.Load(memory)
	if !ok || !strings.Contains(raw.(string), "也看一下显存") || strings.Contains(raw.(string), "sleep") {
		t.Fatalf("task injection = %#v ok=%v", raw, ok)
	}
}

func TestRemoteWatchCancelStopsThePark(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-cancel"
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "running")
	restore := swapRemoteWatchWait(func(ctx context.Context, _ time.Duration, _ func()) error {
		close(task.cancelCh)
		<-ctx.Done()
		return ctx.Err()
	})
	defer restore()
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
	})
	if calls != 1 || !isChatTaskCancelErr(result.Err) {
		t.Fatalf("calls=%d err=%v", calls, result.Err)
	}
}

func TestRemoteWatchCeilingFinishes(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-ceiling"
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "still")
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks[taskID].remoteWatch.ArmedAt = time.Now().Add(-remoteWatchParkCeiling - time.Hour)
	reg.mu.Unlock()
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("ceiling still waited")
		return nil
	})
	defer restore()
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
	})
	if calls != 1 || result.Err != nil || !strings.Contains(result.Text, remoteWatchCeilingNote()) {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestRemoteWatchSessionRoundStillContinues(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	task := insertRemoteWatchTask(t, h, "ct-round", desktopUserID, desktopUserID+":task:ct-round")
	task.Prompt = "finish the notes"
	calls := 0
	var second string
	result := h.driveChatTaskWaves(task, func(_ *chatTaskRecord, _ int, text string) chatTaskWaveOutcome {
		calls++
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "partial", sessionRoundContinue: true}}
		}
		second = text
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "all done"}}
	})
	if calls != 2 || result.Err != nil || result.Text != "all done" {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if second != sessionRoundContinuationPrompt(task.Prompt) {
		t.Fatalf("continuation prompt = %q", second)
	}
}

func TestRemoteWatchRearmKeepsTheCeiling(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const id = "ct-rearm"
	insertRemoteWatchTask(t, h, id, "desktop-user", "desktop-user:task:"+id)
	reg := h.ensureChatTasks()
	started := time.Now().Add(-2 * time.Hour)
	reg.armRemoteWatch(id, chatTaskRemoteWatch{Check: "tail -n 1 /tmp/log", Interval: 5 * time.Minute, LastOutput: "a"})
	reg.mu.Lock()
	reg.tasks[id].remoteWatch.ArmedAt = started
	reg.mu.Unlock()
	reg.armRemoteWatch(id, chatTaskRemoteWatch{Check: "tail -n 1 /tmp/log", Interval: 5 * time.Minute, LastOutput: "a"})
	watch, ok := reg.copyRemoteWatch(id)
	if !ok || !watch.ArmedAt.Equal(started) {
		t.Fatalf("same check reset the ceiling: %v", watch.ArmedAt)
	}
	reg.armRemoteWatch(id, chatTaskRemoteWatch{Check: "tail -n 1 /tmp/other", Interval: 5 * time.Minute, LastOutput: "b"})
	watch, ok = reg.copyRemoteWatch(id)
	if !ok || watch.ArmedAt.Equal(started) {
		t.Fatalf("a new check kept the old ceiling: %v", watch.ArmedAt)
	}
}

func TestRemoteWatchFreshPollSkipsSSH(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-fresh"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	sshN := 0
	h.semanticTrustedSSH = func(_, command string) (string, error) {
		sshN++
		if strings.Contains(command, "sleep") {
			t.Fatalf("wrote sleep: %q", command)
		}
		return "size 10", nil
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("fresh poll waited inside the tool")
		return nil
	})
	defer restore()
	var progress []string
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		onProgress: func(line string) {
			progress = append(progress, line)
		},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	const command = "sleep 30; tail -n 1 /tmp/log"
	first := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, command))
	second := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, command))
	if sshN != 1 || first != second || !strings.Contains(first, "size 10") || !strings.Contains(first, "did not write sleep") {
		t.Fatalf("ssh=%d first=%q second=%q", sshN, first, second)
	}
	if len(progress) < 2 || !strings.Contains(progress[1], "再次查看") || strings.Contains(progress[1], "sleep") {
		t.Fatalf("cached progress = %#v", progress)
	}
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks[taskID].remoteWatch.CheckedAt = time.Now().Add(-time.Minute)
	reg.mu.Unlock()
	_ = callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, command))
	if sshN != 2 {
		t.Fatalf("stale sample ssh calls = %d", sshN)
	}
}

func TestRemoteWatchUnstartedCommandKeepsTheWatch(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-idle"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "old")
	h.semanticTrustedSSH = func(string, string) (string, error) {
		return "", effectNotStarted(fmt.Errorf("trusted_ssh_session_unavailable"))
	}
	callbacks := &sharedAgentLoopCallbacks{
		handler:         h,
		loopCtx:         &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "mkdir -p /data/models"))
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) {
		t.Fatalf("unstarted command = %q", got)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); !armed {
		t.Fatal("a command that was not written dropped the watch")
	}
}

func TestRemoteWatchDownloadRefusalKeepsTheWatch(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-copy"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "old")
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
	}
	callbacks.disarmOwnRemoteWatchAfterEffect(semanticUnexecutedRejection(fmt.Errorf("trusted_ssh_remote_path_required")))
	if _, armed := h.copyChatTaskRemoteWatch(taskID); !armed {
		t.Fatal("a refused download dropped the watch")
	}
	callbacks.disarmOwnRemoteWatchAfterEffect("下载失败: boom\nremote_path=/a\nlocal_path=/b")
	if _, armed := h.copyChatTaskRemoteWatch(taskID); armed {
		t.Fatal("a started download left the watch armed")
	}
}

func TestRemoteWatchParkWaitsRemaining(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const (
		taskID = "ct-remain"
		check  = "tail -n 1 /tmp/log"
	)
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", check, "downloading 10%")
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks[taskID].remoteWatch.CheckedAt = time.Now().Add(-20 * time.Second)
	reg.tasks[taskID].remoteWatch.Shown = "downloading 10%"
	reg.mu.Unlock()
	var waited time.Duration
	waits := 0
	restore := swapRemoteWatchWait(func(_ context.Context, d time.Duration, _ func()) error {
		waits++
		waited = d
		return nil
	})
	defer restore()
	sshN := 0
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		return "downloading 100%", nil
	}
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载"}}
		}
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n好了"}}
	})
	if waits != 1 || waited < 30*time.Second || waited > 50*time.Second {
		t.Fatalf("waits=%d waited=%s", waits, waited)
	}
	if sshN != 1 || calls != 2 || result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("ssh=%d calls=%d result=%+v", sshN, calls, result)
	}
}

func TestRemoteWatchMaxIterationsKeepsTheWatch(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-iter"
	task := insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "downloading 10%")
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error { return nil })
	defer restore()
	h.semanticTrustedSSH = func(string, string) (string, error) { return "downloading 100%", nil }
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		if calls == 1 {
			return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "仍在下载", Error: "max iterations reached"}}
		}
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "远端任务已结束\n好了"}}
	})
	if calls != 2 || result.Err != nil || !strings.HasPrefix(result.Text, remoteWatchDoneToken) {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestRemoteWatchMaxIterationsWithoutWatchFinishes(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	task := insertRemoteWatchTask(t, h, "ct-iter-plain", "desktop-user", "desktop-user:task:ct-iter-plain")
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("an unarmed task parked")
		return nil
	})
	defer restore()
	calls := 0
	result := h.driveChatTaskWaves(task, func(*chatTaskRecord, int, string) chatTaskWaveOutcome {
		calls++
		return chatTaskWaveOutcome{resp: &IMAgentResponse{Text: "stopped", Error: "max iterations reached"}}
	})
	if calls != 1 || result.Err == nil || !strings.Contains(result.Err.Error(), "max iterations reached") {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}

func TestRemoteWatchEmptyCheckIsASample(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-empty"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	h.semanticTrustedSSH = func(_, command string) (string, error) {
		if strings.Contains(command, "sleep") {
			t.Fatalf("wrote sleep: %q", command)
		}
		return "", fmt.Errorf("trusted_ssh_empty")
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("empty check waited inside the tool")
		return nil
	})
	defer restore()
	callbacks := &sharedAgentLoopCallbacks{
		handler:         h,
		loopCtx:         &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 30; tail -n 1 /tmp/log"))
	if strings.HasPrefix(got, "[system rejected]") || !strings.Contains(got, remoteWatchEmptyOutput) || !strings.Contains(got, remoteWatchDoneToken) {
		t.Fatalf("empty check = %q", got)
	}
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || watch.LastError != "" || strings.TrimSpace(watch.LastOutput) != remoteWatchEmptyOutput {
		t.Fatalf("watch = %+v armed=%v", watch, armed)
	}
}

func TestRemoteWatchBareSleepKeepsTheWatch(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-bare"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "size 10")
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks[taskID].remoteWatch.Shown = "size 10"
	reg.tasks[taskID].remoteWatch.CheckedAt = time.Now()
	reg.mu.Unlock()
	h.semanticTrustedSSH = func(string, string) (string, error) {
		t.Fatal("bare sleep reached SSH")
		return "", nil
	}
	var progress []string
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		onProgress: func(line string) {
			progress = append(progress, line)
		},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 300"))
	if !strings.Contains(got, "size 10") || !strings.Contains(got, "did not write sleep") || strings.Contains(got, "sleep 300") {
		t.Fatalf("bare sleep result = %q", got)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); !armed {
		t.Fatal("bare sleep dropped the watch")
	}
	if len(progress) == 0 || strings.Contains(progress[0], "sleep") || !strings.Contains(progress[0], "再次查看") {
		t.Fatalf("card progress = %#v", progress)
	}
}

func TestRemoteWatchBareSleepWithoutWatchIsWritten(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-bare-new"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	var ran string
	h.semanticTrustedSSH = func(_, command string) (string, error) {
		ran = command
		return "ok", nil
	}
	callbacks := &sharedAgentLoopCallbacks{
		handler:         h,
		loopCtx:         &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	_ = callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 30"))
	if ran != "sleep 30" {
		t.Fatalf("bare sleep without a watch = %q", ran)
	}
}

func TestRemoteWatchFollowIsNotWritten(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-follow"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	armTestRemoteWatch(h, taskID, "desktop-user", "tail -n 1 /tmp/log", "size 10")
	h.semanticTrustedSSH = func(string, string) (string, error) {
		t.Fatal("tail -f reached SSH")
		return "", nil
	}
	callbacks := &sharedAgentLoopCallbacks{
		handler:         h,
		loopCtx:         &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		semanticSurface: &semanticCallSurface{scope: tool.InvocationScope{PrincipalID: "desktop-user:task:" + taskID}},
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -f /tmp/x"))
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || !strings.Contains(got, "remote_watch_check_must_exit") || !strings.Contains(got, remoteWatchDoneToken) {
		t.Fatalf("follow result = %q", got)
	}
	got = callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 1; tail -0f /tmp/x"))
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || !strings.Contains(got, "remote_watch_check_must_exit") {
		t.Fatalf("tail -0f result = %q", got)
	}
	if _, armed := h.copyChatTaskRemoteWatch(taskID); !armed {
		t.Fatal("a refused follow dropped the watch")
	}
	if _, ok := parseBareSleep("bash -lc 'sleep 300'"); !ok {
		t.Fatal("wrapped bare sleep was not recognized")
	}
	if _, ok := parseBareSleep("sleep 300; tail -n 1 /tmp/x"); ok {
		t.Fatal("a sleep poll was treated as a bare sleep")
	}
	msg := remoteWatchErrorForPrompt(fmt.Errorf("trusted_ssh_outcome_unobserved\n不要直接重发同一条命令"))
	if strings.Contains(msg, "不要直接重发同一条命令") || !strings.Contains(msg, "宿主会再次执行同一条检查") {
		t.Fatalf("prompt error = %q", msg)
	}
}

func TestRemoteWatchCommandResultClasses(t *testing.T) {
	idle := trustedSSHCommandResult("", effectNotStarted(fmt.Errorf("trusted_ssh_session_unavailable")), time.Second)
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: idle}) || strings.HasPrefix(idle, "[system unknown]") {
		t.Fatalf("unstarted command = %q", idle)
	}
	timeout := trustedSSHCommandResult("", fmt.Errorf("trusted_ssh_timeout"), 15*time.Second)
	if !strings.HasPrefix(timeout, "[system rejected]") || strings.HasPrefix(timeout, "[system unknown]") || !strings.Contains(timeout, "timeout_seconds") {
		t.Fatalf("timeout = %q", timeout)
	}
	lost := trustedSSHCommandResult("", fmt.Errorf("trusted_ssh_outcome_unobserved"), time.Minute)
	if !strings.HasPrefix(lost, "[system unknown]") {
		t.Fatalf("lost session = %q", lost)
	}
	if got := trustedSSHCommandResult("size 10", nil, time.Second); got != "size 10" {
		t.Fatalf("check output = %q", got)
	}
}

func TestRemoteWatchCheckFailureStaysSettled(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-fail"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	var sshN int
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		return "", fmt.Errorf("trusted_ssh_timeout")
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("sleep poll waited inside the tool")
		return nil
	})
	defer restore()
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		userID:  "desktop-user:task:" + taskID,
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if !strings.HasPrefix(got, "[system rejected]") || !strings.Contains(got, "trusted_ssh_timeout") || strings.Contains(got, "sleep 5") || strings.Contains(got, "timeout_seconds") || !strings.Contains(got, remoteWatchDoneToken) {
		t.Fatalf("timeout result = %q", got)
	}
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || !watch.LastStarted || !strings.Contains(watch.LastError, "trusted_ssh_timeout") {
		t.Fatalf("watch = %+v armed=%v", watch, armed)
	}
	// The command was written. Repeating it, or sending a bare sleep, must
	// not open SSH again inside the interval.
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		return "size 10", nil
	}
	retried := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if sshN != 1 || retried != got {
		t.Fatalf("started timeout was rerun: ssh=%d result=%q", sshN, retried)
	}
	bare := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 300"))
	if sshN != 1 || strings.Contains(bare, "sleep 300") || !strings.Contains(bare, "trusted_ssh_timeout") || strings.Contains(bare, "timeout_seconds") {
		t.Fatalf("bare sleep reran a started timeout: ssh=%d result=%q", sshN, bare)
	}
}

func TestRemoteWatchNotStartedFailureIsNotCached(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-idle"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	var sshN int
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		if sshN == 1 {
			return "", effectNotStarted(fmt.Errorf("trusted_ssh_session_unavailable"))
		}
		return "size 10", nil
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("unstarted check waited inside the tool")
		return nil
	})
	defer restore()
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		userID:  "desktop-user:task:" + taskID,
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if !tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: got}) || !strings.Contains(got, "trusted_ssh_session_unavailable") {
		t.Fatalf("unstarted result = %q", got)
	}
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || watch.LastStarted || !strings.Contains(watch.LastError, "trusted_ssh_session_unavailable") {
		t.Fatalf("watch = %+v armed=%v", watch, armed)
	}
	retried := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if sshN != 2 || !strings.Contains(retried, "size 10") || strings.Contains(retried, "trusted_ssh_session_unavailable") || !strings.Contains(retried, remoteWatchDoneToken) {
		t.Fatalf("unstarted retry: ssh=%d result=%q", sshN, retried)
	}
	reg := h.ensureChatTasks()
	reg.mu.Lock()
	reg.tasks[taskID].remoteWatch.LastError = "trusted_ssh_session_unavailable"
	reg.tasks[taskID].remoteWatch.LastStarted = false
	reg.tasks[taskID].remoteWatch.Shown = "[system rejected] arguments_rejected: trusted_ssh_session_unavailable"
	reg.tasks[taskID].remoteWatch.CheckedAt = time.Now()
	reg.mu.Unlock()
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		return "size 11", nil
	}
	bare := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 300"))
	if sshN != 3 || strings.Contains(bare, "sleep 300") || !strings.Contains(bare, "size 11") || strings.Contains(bare, "trusted_ssh_session_unavailable") {
		t.Fatalf("bare sleep after an unstarted check: ssh=%d result=%q", sshN, bare)
	}
	if watch, armed = h.copyChatTaskRemoteWatch(taskID); !armed || !watch.LastStarted {
		t.Fatalf("check after reconnect = %+v armed=%v", watch, armed)
	}
}

func TestRemoteWatchUnobservedTellsTheHostToRecheck(t *testing.T) {
	h, _ := testChatTaskHandler(t)
	const taskID = "ct-unobserved"
	insertRemoteWatchTask(t, h, taskID, "desktop-user", "desktop-user:task:"+taskID)
	var sshN int
	h.semanticTrustedSSH = func(string, string) (string, error) {
		sshN++
		return "", fmt.Errorf("trusted_ssh_outcome_unobserved\n不要直接重发同一条命令")
	}
	restore := swapRemoteWatchWait(func(context.Context, time.Duration, func()) error {
		t.Fatal("unobserved check waited inside the tool")
		return nil
	})
	defer restore()
	callbacks := &sharedAgentLoopCallbacks{
		handler: h,
		loopCtx: &LoopContext{ChatTaskID: taskID, CancelC: make(chan struct{})},
		userID:  "desktop-user:task:" + taskID,
	}
	got := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if !strings.HasPrefix(got, "[system unknown]") || strings.Contains(got, "不要直接重发同一条命令") || !strings.Contains(got, "宿主会再次执行同一条检查") || !strings.Contains(got, remoteWatchDoneToken) {
		t.Fatalf("unobserved result = %q", got)
	}
	watch, armed := h.copyChatTaskRemoteWatch(taskID)
	if !armed || !watch.LastStarted || !strings.Contains(watch.LastError, "不要直接重发同一条命令") {
		t.Fatalf("stored error = %+v armed=%v", watch, armed)
	}
	again := callbacks.executeTrustedSSH(tool.PlannedSelection{}, sshCommandRequest(t, "sleep 5; tail -n 1 /tmp/log"))
	if sshN != 1 || again != got {
		t.Fatalf("unobserved retry opened SSH: ssh=%d result=%q", sshN, again)
	}
}

func insertRemoteWatchTask(t *testing.T, h *IMMessageHandler, id, session, memory string) *chatTaskRecord {
	t.Helper()
	reg := h.ensureChatTasks()
	task := &chatTaskRecord{
		ID: id, SessionKey: session, MemoryKey: memory, Prompt: "watch the download",
		Status: chatTaskRunning, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		answerCh: make(chan string, 1), cancelCh: make(chan struct{}),
	}
	reg.mu.Lock()
	reg.tasks[id] = task
	reg.mu.Unlock()
	return task
}

func armTestRemoteWatch(h *IMMessageHandler, id, principal, check, output string) {
	h.ensureChatTasks().armRemoteWatch(id, chatTaskRemoteWatch{
		Principal:  principal,
		Check:      check,
		Interval:   time.Minute,
		LastOutput: output,
	})
}

func sshCommandRequest(t *testing.T, command string) tool.CanonicalRequest {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return tool.CanonicalRequest{CanonicalJSON: raw}
}

func swapRemoteWatchWait(fn remoteWatchWaitFunc) func() {
	remoteWatchWaitMu.Lock()
	prev := remoteWatchWaitHook
	remoteWatchWaitHook = fn
	remoteWatchWaitMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			remoteWatchWaitMu.Lock()
			remoteWatchWaitHook = prev
			remoteWatchWaitMu.Unlock()
		})
	}
}
