package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

type splitTaskStore struct {
	mu    sync.Mutex
	tasks []scheduler.ScheduledTask
}

func (s *splitTaskStore) LoadScheduledTasks() ([]scheduler.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]scheduler.ScheduledTask, len(s.tasks))
	copy(out, s.tasks)
	return out, nil
}

func (s *splitTaskStore) SaveScheduledTasks(tasks []scheduler.ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append([]scheduler.ScheduledTask(nil), tasks...)
	return nil
}

func TestDesktopBotSplitCases(t *testing.T) {
	t.Cleanup(resetDesktopToolJournal)

	t.Run("tool_rpc_has_no_transcript", func(t *testing.T) {
		resetDesktopToolJournal()
		prevSession := desktopRemoteSession
		prevFile := desktopRemoteFile
		prevBash := desktopRemoteBash
		prevShot := desktopRemoteScreenshot
		prevApp := desktopRemoteApp
		prevOpen := desktopRemoteOpen
		prevInstall := desktopRemoteInstall
		desktopRemoteSession = nil
		desktopRemoteFile = nil
		desktopRemoteBash = nil
		desktopRemoteScreenshot = nil
		desktopRemoteApp = nil
		desktopRemoteOpen = nil
		desktopRemoteInstall = nil
		t.Cleanup(func() {
			desktopRemoteSession = prevSession
			desktopRemoteFile = prevFile
			desktopRemoteBash = prevBash
			desktopRemoteScreenshot = prevShot
			desktopRemoteApp = prevApp
			desktopRemoteOpen = prevOpen
			desktopRemoteInstall = prevInstall
		})

		var mu sync.Mutex
		var bodies []map[string]json.RawMessage
		var paths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			mu.Lock()
			paths = append(paths, r.URL.Path)
			bodies = append(bodies, fields)
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":"ok","executor_id":"box-1","volumes":"desktops,home,opt,usr-local","replaced":false}`))
		}))
		defer srv.Close()
		t.Setenv(desktopHubURLEnv, srv.URL)
		t.Setenv(desktopHubTokenEnv, "hub-token")
		if !desktopHubExecutesTool() {
			t.Fatal("the hub client did not take the machine call")
		}
		desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
			t.Error("a test stub still sent the tool to Hub")
			return desktopEndpoint{}, nil
		}
		if desktopHubExecutesTool() {
			t.Fatal("a session stub left the hub short-circuit on")
		}
		desktopRemoteSession = nil

		const runID = "run-rpc"
		desktopRoundWakes.Store(runID, &desktopRoundWake{woken: true, have: true, endpoint: desktopEndpoint{Display: ":20"}})
		t.Cleanup(func() { finishDesktopRound("tenant-rpc", "alice", runID) })
		// Screen actions (click, type, app_open, computer...) run on the
		// guarded path; a command is the single machine call.
		ctx := agentruntime.WithToolCallID(context.Background(), "call-click")
		scope := agentruntime.Scope{TenantID: "tenant-rpc", UserID: "alice", InstanceID: "bot-rpc", RunID: runID}
		text, err := operateDesktop(ctx, scope, map[string]any{
			"action":       "bash",
			"command":      "ls /home/desktop",
			"messages":     []any{map[string]any{"role": "user", "content": "把对话带去"}},
			"memory":       "画像",
			"send_to_user": "告诉这个人",
			"conversation": "整段对话",
			"history":      "上一轮",
			"transcript":   "工具记录",
		})
		if err != nil || text != "ok" {
			t.Fatalf("text=%q err=%v", text, err)
		}
		mu.Lock()
		gotPaths := append([]string(nil), paths...)
		gotBodies := append([]map[string]json.RawMessage(nil), bodies...)
		mu.Unlock()
		if len(gotPaths) != 1 || gotPaths[0] != "/api/v1/desktop-services/tool" {
			t.Fatalf("paths = %v", gotPaths)
		}
		body := gotBodies[0]
		allowed := map[string]bool{"run_id": true, "tool_call_id": true, "tenant": true, "user": true, "action": true, "args": true, "display": true}
		for key := range body {
			if !allowed[key] {
				t.Fatalf("body key %s", key)
			}
		}
		for _, key := range []string{"run_id", "tool_call_id", "tenant", "user", "action", "args"} {
			if _, ok := body[key]; !ok {
				t.Fatalf("body missing %s: %s", key, body)
			}
		}
		if string(body["run_id"]) != `"run-rpc"` || string(body["tool_call_id"]) != `"call-click"` || string(body["action"]) != `"bash"` {
			t.Fatalf("body = %s", body)
		}
		var args map[string]any
		if err := json.Unmarshal(body["args"], &args); err != nil {
			t.Fatal(err)
		}
		if len(args) != 1 || args["command"] != "ls /home/desktop" {
			t.Fatalf("args = %#v", args)
		}
		raw, _ := json.Marshal(body)
		for _, word := range []string{"messages", "memory", "send_to_user", "conversation", "history", "transcript"} {
			if strings.Contains(string(raw), word) {
				t.Fatalf("body carried %s: %s", word, raw)
			}
		}

		// A file call omits display. The same tool id is not sent again.
		before := len(gotPaths)
		fileCtx := agentruntime.WithToolCallID(context.Background(), "call-file")
		fileText, err := operateDesktop(fileCtx, scope, map[string]any{
			"action":   "file_read",
			"path":     "/home/desktop/notes.txt",
			"messages": []any{"again"},
		})
		if err != nil || fileText != "ok" {
			t.Fatalf("file text=%q err=%v", fileText, err)
		}
		mu.Lock()
		gotPaths = append([]string(nil), paths...)
		gotBodies = append([]map[string]json.RawMessage(nil), bodies...)
		mu.Unlock()
		if len(gotPaths) != before+1 {
			t.Fatalf("file paths = %v", gotPaths)
		}
		if _, ok := gotBodies[len(gotBodies)-1]["display"]; ok {
			t.Fatalf("file body included a display: %s", gotBodies[len(gotBodies)-1])
		}
		again, err := operateDesktop(fileCtx, scope, map[string]any{"action": "file_read", "path": "/home/desktop/notes.txt"})
		if err != nil || again != "ok" {
			t.Fatalf("replay text=%q err=%v", again, err)
		}
		mu.Lock()
		if len(paths) != before+1 {
			t.Fatalf("a recorded tool_call_id was sent again: %v", paths)
		}
		mu.Unlock()

		loose := 0
		_, _ = dispatchDesktopTool(desktopToolCall{Action: "click", ToolCallID: "loose"}, desktopToolExec{}, func(desktopToolCall, desktopToolExec) (desktopToolOutcome, error) {
			loose++
			return desktopToolOutcome{Text: "once"}, nil
		})
		_, _ = dispatchDesktopTool(desktopToolCall{Action: "click", ToolCallID: "loose"}, desktopToolExec{}, func(desktopToolCall, desktopToolExec) (desktopToolOutcome, error) {
			loose++
			return desktopToolOutcome{Text: "twice"}, nil
		})
		if loose != 2 {
			t.Fatalf("an empty run id was cached, sends=%d", loose)
		}
	})

	t.Run("click_not_replayed_after_replace", func(t *testing.T) {
		resetDesktopToolJournal()
		const runID = "run-click"
		call := desktopToolCall{RunID: runID, ToolCallID: "call-click", Tenant: "tenant-click", User: "alice", Action: "click", Args: map[string]any{"text": "确定"}}
		sends := 0
		var volumes []string
		var ids []string
		send := func(got desktopToolCall, exec desktopToolExec) (desktopToolOutcome, error) {
			sends++
			volumes = append(volumes, exec.Volumes)
			ids = append(ids, got.ToolCallID+"/"+got.RunID)
			return desktopToolOutcome{Replaced: true, ExecutorID: "box-2"}, nil
		}
		outcome, err := dispatchDesktopTool(call, desktopToolExec{}, send)
		if sends != 1 || err == nil || err.Error() != desktopComputerReplaced || outcome.Volumes != desktopToolVolumes {
			t.Fatalf("sends=%d volumes=%q err=%v", sends, outcome.Volumes, err)
		}
		if _, err = dispatchDesktopTool(call, desktopToolExec{Replaced: true, ID: "box-3"}, send); sends != 1 || err == nil || err.Error() != desktopComputerReplaced {
			t.Fatalf("replay sends=%d err=%v", sends, err)
		}
		if len(volumes) != 1 || volumes[0] != desktopToolVolumes || len(ids) != 1 || ids[0] != "call-click/run-click" {
			t.Fatalf("volumes=%v ids=%v", volumes, ids)
		}
		finishDesktopRound("tenant-click", "alice", runID)
		if _, ok := desktopToolSlots.Load(runID + "\x00" + "call-click"); ok {
			t.Fatal("the round kept the tool result")
		}
	})

	t.Run("screenshot_retries_on_new_executor", func(t *testing.T) {
		resetDesktopToolJournal()
		const runID = "run-shot"
		call := desktopToolCall{RunID: runID, ToolCallID: "call-shot", Tenant: "tenant-shot", User: "alice", Action: "screenshot"}
		sends := 0
		send := func(got desktopToolCall, exec desktopToolExec) (desktopToolOutcome, error) {
			sends++
			if got.ToolCallID != "call-shot" || got.RunID != runID || exec.Volumes != desktopToolVolumes {
				t.Errorf("retry call=%+v volumes=%q", got, exec.Volumes)
			}
			if sends == 1 {
				return desktopToolOutcome{Replaced: true, ExecutorID: "box-2", Volumes: desktopToolVolumes}, nil
			}
			return desktopToolOutcome{Text: "shot", ExecutorID: "box-2", Volumes: desktopToolVolumes, AttachmentID: "shot-1"}, nil
		}
		outcome, err := dispatchDesktopTool(call, desktopToolExec{ID: "box-1"}, send)
		if err != nil || sends != 2 || outcome.Text != "shot" || outcome.AttachmentID != "shot-1" || outcome.Volumes != desktopToolVolumes || outcome.ExecutorID != "box-2" {
			t.Fatalf("sends=%d outcome=%+v err=%v", sends, outcome, err)
		}
		third, err := dispatchDesktopTool(call, desktopToolExec{Replaced: true}, send)
		if err != nil || sends != 2 || third.Text != "shot" || third.Volumes != desktopToolVolumes || third.AttachmentID != "shot-1" {
			t.Fatalf("third sends=%d outcome=%+v err=%v", sends, third, err)
		}
	})

	t.Run("schedule_fires_without_gui", func(t *testing.T) {
		t.Setenv("MACLAW_ENABLE_SCHEDULER", "")
		t.Setenv(desktopHubURLEnv, "")
		t.Setenv(desktopHubTokenEnv, "")
		enabled, err := getenvBoolStrict("MACLAW_ENABLE_SCHEDULER", false)
		if err != nil || enabled {
			t.Fatalf("scheduler flag enabled=%v err=%v", enabled, err)
		}
		if initScheduler(t.TempDir(), nil, nil) != nil {
			t.Fatal("the default flag started a manager")
		}
		sessionCalls := 0
		prev := desktopRemoteSession
		desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
			sessionCalls++
			return desktopEndpoint{}, nil
		}
		t.Cleanup(func() { desktopRemoteSession = prev })

		mgr, err := scheduler.NewManagerFromStore(&splitTaskStore{})
		if err != nil {
			t.Fatal(err)
		}
		id, err := mgr.Add(scheduler.ScheduledTask{
			Name: "morning", Action: "read memory",
			Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
			DesktopBotID: "bot_1", DesktopBotOwner: "alice",
		})
		if err != nil {
			t.Fatal(err)
		}
		task := mgr.Get(id)
		if task == nil || task.NextRunAt == nil || task.NextRunAt.Hour() != 8 || task.NextRunAt.Minute() != 0 {
			t.Fatalf("next = %+v", task)
		}
		due := *task.NextRunAt
		var mu sync.Mutex
		calls := 0
		mgr.SetExecutor(func(_ context.Context, got *scheduler.ScheduledTask) (string, error) {
			mu.Lock()
			calls++
			mu.Unlock()
			if got == nil || got.DesktopBotID != "bot_1" || got.Action != "read memory" {
				t.Errorf("task = %+v", got)
			}
			return "无变化", nil
		})
		mgr.FireDue(due)
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			n := calls
			mu.Unlock()
			got := mgr.Get(id)
			if n == 1 && mgr.Inflight() == 0 && got != nil && got.RunCount == 1 && got.LastResult == "无变化" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("calls=%d inflight=%d task=%+v session=%d", n, mgr.Inflight(), got, sessionCalls)
			}
			time.Sleep(5 * time.Millisecond)
		}
		mgr.FireDue(due)
		quiet := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(quiet) {
			mu.Lock()
			n := calls
			mu.Unlock()
			if n != 1 || mgr.Inflight() != 0 || sessionCalls != 0 {
				t.Fatalf("second fire calls=%d inflight=%d session=%d", n, mgr.Inflight(), sessionCalls)
			}
			time.Sleep(10 * time.Millisecond)
		}
		delivery := &scheduler.TaskDelivery{
			Enabled: true,
			Channel: scheduler.DeliveryChannelLansenger,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindGroup, GroupID: "g"}},
			On:      scheduler.DeliveryOnAlways,
		}
		if !delivery.Active() {
			t.Fatal("delivery was inactive")
		}
		if delivery.ShouldDeliver(nil, "无变化") {
			t.Fatal("无变化 was sent to the user")
		}
	})
}
