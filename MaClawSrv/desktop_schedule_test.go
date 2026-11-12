package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestArmDesktopBotScheduleUpdatesTheSameOrder(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	hour, minute, day := 8, 30, 1
	decision := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "做一份简报",
		Schedule: &agentservice.BotIntakeSchedule{
			Name: "brief", Mode: "create", Hour: &hour, Minute: &minute, DayOfWeek: &day,
		},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_1", "每个工作日早上做简报", decision); err != nil {
		t.Fatal(err)
	}
	minute = 0
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_1", "每个工作日早上做简报", decision); err != nil {
		t.Fatal(err)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].Minute != 0 || rows[0].Hour != 8 || rows[0].DayOfWeek != 1 || rows[0].Action != "做一份简报" {
		t.Fatalf("updated = %+v", rows)
	}
	if rows[0].OwnerTenantID != "tenant-a" || rows[0].OwnerUserID != "alice" || rows[0].InstanceID != "inst_1" || rows[0].TaskType != scheduler.TaskTypeReminder {
		t.Fatalf("identity = %+v", rows[0])
	}
	other := decision
	other.Instruction = "打开百度"
	other.Schedule = &agentservice.BotIntakeSchedule{Name: "baidu", Mode: "create", IntervalMinutes: 15}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_1", "每 15 分钟打开百度", other); err != nil {
		t.Fatal(err)
	}
	if rows = desktopScheduleRows(mgr, "inst_1", "bot_1"); len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if n := cancelDesktopBotSchedules(mgr, "inst_1", "bot_1", "brief"); n != 1 {
		t.Fatalf("cancel n=%d", n)
	}
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].Name != "baidu" || rows[0].IntervalMinutes != 15 {
		t.Fatalf("remaining = %+v", rows)
	}
	if n := cancelDesktopBotSchedules(mgr, "inst_1", "bot_1", ""); n != 1 || len(desktopScheduleRows(mgr, "inst_1", "bot_1")) != 0 {
		t.Fatal("cancel all left a row")
	}
}

func TestPersistDesktopBotScheduleUsesTheHostClock(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	prev := getSrvSchedulerManager()
	setSrvSchedulerManager(mgr)
	t.Cleanup(func() { setSrvSchedulerManager(prev) })
	hour, minute := 9, 0
	persistDesktopBotSchedule(agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}, "inst_1",
		agentservice.BotIntakeInput{BotID: "bot_1", Content: "每天早上打开首页"},
		agentservice.BotIntake{
			Kind:        "schedule",
			Instruction: "打开首页",
			Schedule:    &agentservice.BotIntakeSchedule{Name: "home", Mode: "create", Hour: &hour, Minute: &minute},
		})
	rows := desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].Action != "打开首页" || rows[0].DayOfWeek != -1 || rows[0].Hour != 9 {
		t.Fatalf("persisted = %+v", rows)
	}
	persistDesktopBotSchedule(agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}, "inst_1",
		agentservice.BotIntakeInput{BotID: "bot_1", Content: "现在就打开"},
		agentservice.BotIntake{Kind: "new", Instruction: "现在就打开"})
	if len(desktopScheduleRows(mgr, "inst_1", "bot_1")) != 1 {
		t.Fatal("a new task was stored as a timer")
	}
}

func TestDesktopBotScheduleMessageIsOneOccurrence(t *testing.T) {
	due := time.Date(2026, 11, 13, 8, 0, 0, 0, time.Local)
	task := &scheduler.ScheduledTask{
		ID: "sched-1", DesktopBotID: "bot_1", Action: "打开首页", NextRunAt: &due,
	}
	first := desktopBotScheduleMessage(task)
	again := desktopBotScheduleMessage(task)
	if first.ClientMessageID == "" || first.ClientMessageID != again.ClientMessageID {
		t.Fatalf("id = %q / %q", first.ClientMessageID, again.ClientMessageID)
	}
	if first.ClientSessionKey != "schedule:sched-1" {
		t.Fatalf("session = %q", first.ClientSessionKey)
	}
	if first.Metadata["bot_id"] != "bot_1" || first.Metadata["bot_phase"] != "execute" || first.Metadata["computer_helper"] != "1" || first.Metadata["bot_origin"] != "schedule" {
		t.Fatalf("meta = %#v", first.Metadata)
	}
	if !strings.HasPrefix(first.Content, desktopbot.RoutineQuietPrompt) || !strings.Contains(first.Content, "打开首页") || strings.Contains(first.Content, "请直接执行以下操作并返回结果") {
		t.Fatalf("content = %q", first.Content)
	}
	due = due.Add(24 * time.Hour)
	task.NextRunAt = &due
	next := desktopBotScheduleMessage(task)
	if next.ClientMessageID == first.ClientMessageID {
		t.Fatal("the next day reused the occurrence id")
	}
}

func TestDesktopBotClockFiresOneRunWithoutTheGUI(t *testing.T) {
	t.Setenv("MACLAW_ENABLE_SCHEDULER", "")
	sessionCalls := 0
	prevSession := desktopRemoteSession
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		sessionCalls++
		return desktopEndpoint{}, nil
	}
	t.Cleanup(func() { desktopRemoteSession = prevSession })

	var mu sync.Mutex
	var fired []*scheduler.ScheduledTask
	prevRun := runDesktopBotSchedule
	runDesktopBotSchedule = func(_ context.Context, _ *agentservice.Service, task *scheduler.ScheduledTask) (string, error) {
		mu.Lock()
		fired = append(fired, task)
		mu.Unlock()
		return "打开了。", nil
	}
	t.Cleanup(func() { runDesktopBotSchedule = prevRun })

	prevMgr := getSrvSchedulerManager()
	mgr := initScheduler(t.TempDir(), nil, nil)
	if mgr == nil {
		t.Fatal("desktop-bot clock did not start")
	}
	t.Cleanup(func() {
		mgr.Stop()
		setSrvSchedulerManager(prevMgr)
	})

	if err := armDesktopBotSchedule(mgr, agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}, "inst_1", "bot_1", "打开首页", agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "打开首页",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "home", Mode: "create", Hour: intPtr(8), Minute: intPtr(0)},
	}); err != nil {
		t.Fatal(err)
	}
	plain, err := mgr.Add(scheduler.ScheduledTask{
		Name: "mail", Action: "读邮件", Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].NextRunAt == nil {
		t.Fatalf("row = %+v", rows)
	}
	due := *rows[0].NextRunAt
	plainBefore := mgr.Get(plain)
	mgr.FireDue(due)
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(fired)
		mu.Unlock()
		got := mgr.Get(rows[0].ID)
		if n == 1 && mgr.Inflight() == 0 && got != nil && got.RunCount == 1 && got.LastResult == "打开了。" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fired=%d inflight=%d task=%+v session=%d", n, mgr.Inflight(), got, sessionCalls)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	if fired[0].DesktopBotID != "bot_1" || fired[0].Action != "打开首页" || fired[0].InstanceID != "inst_1" {
		t.Fatalf("fired task = %+v", fired[0])
	}
	mu.Unlock()
	if sessionCalls != 0 {
		t.Fatalf("schedule opened the desktop %d times", sessionCalls)
	}
	plainAfter := mgr.Get(plain)
	if plainAfter == nil || plainBefore == nil || plainAfter.RunCount != 0 || plainAfter.NextRunAt == nil || !plainAfter.NextRunAt.Equal(*plainBefore.NextRunAt) {
		t.Fatalf("plain task fired: before=%+v after=%+v", plainBefore, plainAfter)
	}
	mgr.FireDue(due)
	quiet := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(quiet) {
		mu.Lock()
		n := len(fired)
		mu.Unlock()
		if n != 1 || mgr.Inflight() != 0 || sessionCalls != 0 {
			t.Fatalf("second fire n=%d inflight=%d session=%d", n, mgr.Inflight(), sessionCalls)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func newDesktopScheduleManager(t *testing.T) *scheduler.Manager {
	t.Helper()
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	return mgr
}

func desktopScheduleRows(mgr *scheduler.Manager, instanceID, botID string) []scheduler.ScheduledTask {
	var out []scheduler.ScheduledTask
	for _, task := range mgr.List() {
		if task.InstanceID == instanceID && task.DesktopBotID == botID {
			out = append(out, task)
		}
	}
	return out
}

func intPtr(n int) *int { return &n }
