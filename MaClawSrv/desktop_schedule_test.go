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

func TestArmDesktopBotScheduleKeepsTheIntervalAnchor(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name: "watch", Action: "看一眼", DesktopBotID: "bot_1", InstanceID: "inst_1",
		OwnerTenantID: "tenant-a", OwnerUserID: "alice",
		IntervalMinutes: 7, Hour: 8, Minute: 30, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	before := mgr.Get(id)
	if before == nil || before.NextRunAt == nil {
		t.Fatal("no next run")
	}
	due := *before.NextRunAt
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	follow := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "看一眼，有变化就说",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "watch", Mode: "create", IntervalMinutes: 7},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_1", "有变化就说", follow); err != nil {
		t.Fatal(err)
	}
	after := mgr.Get(id)
	if after == nil || after.Hour != 8 || after.Minute != 30 || after.IntervalMinutes != 7 || after.Action != "看一眼，有变化就说" || after.NextRunAt == nil || !after.NextRunAt.Equal(due) {
		t.Fatalf("anchor moved: %+v", after)
	}
}

func TestArmDesktopBotScheduleKeepsTheWeatherTimerWhenBeijingAndAPDFArrive(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	first := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟向我报告下天气情况",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 5},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "每5分钟向我报告下天气情况", first); err != nil {
		t.Fatal(err)
	}
	follow := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟报告北京的天气，并把这一轮做成 PDF",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 5},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "北京，下次再生成pdf文件", follow); err != nil {
		t.Fatal(err)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_5")
	if len(rows) != 1 || rows[0].Name != "weather_report" || rows[0].IntervalMinutes != 5 {
		t.Fatalf("timer = %+v", rows)
	}
	if rows[0].Action != "每5分钟报告北京的天气，并把这一轮做成 PDF" {
		t.Fatalf("action = %q", rows[0].Action)
	}
	other := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟打开百度",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "baidu", Mode: "create", IntervalMinutes: 15},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "每5分钟打开百度", other); err != nil {
		t.Fatal(err)
	}
	rows = desktopScheduleRows(mgr, "inst_1", "bot_5")
	if len(rows) != 2 {
		t.Fatalf("different job rows=%d", len(rows))
	}
	for _, row := range rows {
		if row.Name == "weather_report" && row.Action != "每5分钟报告北京的天气，并把这一轮做成 PDF" {
			t.Fatalf("weather order changed: %+v", row)
		}
	}
}

func TestArmDesktopBotScheduleKeepsTheOrderAndDoesNotGuessAName(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	first := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟向我报告下天气情况",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 5},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "每5分钟向我报告下天气情况", first); err != nil {
		t.Fatal(err)
	}
	clockOnly := agentservice.BotIntake{
		Kind:     "schedule",
		Schedule: &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 10},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "北京，下次再生成pdf文件", clockOnly); err != nil {
		t.Fatal(err)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_5")
	if len(rows) != 1 || rows[0].Action != "每5分钟向我报告下天气情况" || rows[0].IntervalMinutes != 10 {
		t.Fatalf("empty instruction = %+v", rows)
	}
	other := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟打开百度",
		Schedule:    &agentservice.BotIntakeSchedule{Mode: "create", IntervalMinutes: 15},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "每5分钟打开百度", other); err != nil {
		t.Fatal(err)
	}
	rows = desktopScheduleRows(mgr, "inst_1", "bot_5")
	if len(rows) != 2 {
		t.Fatalf("nameless order rows=%d", len(rows))
	}
	for _, row := range rows {
		if row.Name == "weather_report" && (row.Action != "每5分钟向我报告下天气情况" || row.IntervalMinutes != 10) {
			t.Fatalf("weather timer = %+v", row)
		}
	}
}

func TestArmDesktopBotScheduleKeepsTheWeekdayWhenTheOrderChanges(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	day := 1
	first := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟向我报告下天气情况",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 5, DayOfWeek: &day},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "每5分钟向我报告下天气情况", first); err != nil {
		t.Fatal(err)
	}
	follow := agentservice.BotIntake{
		Kind:        "schedule",
		Instruction: "每5分钟报告北京的天气，并把这一轮做成 PDF",
		Schedule:    &agentservice.BotIntakeSchedule{Name: "weather_report", Mode: "create", IntervalMinutes: 5},
	}
	if err := armDesktopBotSchedule(mgr, p, "inst_1", "bot_5", "北京，下次再生成pdf文件", follow); err != nil {
		t.Fatal(err)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_5")
	if len(rows) != 1 || rows[0].DayOfWeek != 1 || rows[0].DayOfMonth != -1 || rows[0].IntervalMinutes != 5 {
		t.Fatalf("weekday = %+v", rows)
	}
	if rows[0].Action != follow.Instruction {
		t.Fatalf("action = %q", rows[0].Action)
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

func TestDesktopBotScheduleToolKeepsTheTimerItNamed(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	p := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	got := desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "instruction": "搜索免费资源，有新的就告诉我", "interval_minutes": float64(60),
	})
	if !strings.Contains(got, "已登记 free_resources") || !strings.Contains(got, "每 60 分钟") {
		t.Fatalf("create = %q", got)
	}
	rows := desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].IntervalMinutes != 60 || rows[0].Action != "搜索免费资源，有新的就告诉我" || rows[0].DesktopBotID != "bot_1" {
		t.Fatalf("row = %+v", rows)
	}
	next := rows[0].NextRunAt
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "instruction": "搜索免费资源，有新的就告诉我", "interval_minutes": float64(60),
	})
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if !strings.Contains(got, "已是 free_resources") || len(rows) != 1 || rows[0].NextRunAt == nil || next == nil || !rows[0].NextRunAt.Equal(*next) {
		t.Fatalf("repeat moved the timer: %q next=%v", got, rows[0].NextRunAt)
	}
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "instruction": "搜索免费资源，有新的就告诉我",
		"interval_minutes": float64(60), "hour": float64(9), "minute": float64(15),
	})
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].IntervalMinutes != 60 || rows[0].Hour != 9 || rows[0].Minute != 15 || !strings.Contains(got, "从 09:15 起") {
		t.Fatalf("anchor = %q row=%+v", got, rows)
	}
	anchored := rows[0].NextRunAt
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "instruction": "搜索免费资源，有新的就告诉我",
		"interval_minutes": float64(60), "hour": float64(9), "minute": float64(15),
	})
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if !strings.Contains(got, "已是") || rows[0].NextRunAt == nil || anchored == nil || !rows[0].NextRunAt.Equal(*anchored) {
		t.Fatalf("same anchor moved: %q", got)
	}
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "instruction": "搜索免费数据集和模型，有新的就告诉我",
	})
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].IntervalMinutes != 60 || rows[0].Action != "搜索免费数据集和模型，有新的就告诉我" || !strings.Contains(got, "已更新") {
		t.Fatalf("update = %q row=%+v", got, rows)
	}
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "free_resources", "hour": float64(9),
	})
	if !strings.Contains(got, "一起写") || desktopScheduleRows(mgr, "inst_1", "bot_1")[0].IntervalMinutes != 60 {
		t.Fatalf("partial clock = %q", got)
	}
	got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "morning", "instruction": "整理早间简报", "hour": float64(8), "minute": float64(30),
	})
	if rows = desktopScheduleRows(mgr, "inst_1", "bot_1"); len(rows) != 2 || !strings.Contains(got, "已登记 morning") {
		t.Fatalf("second = %q rows=%d", got, len(rows))
	}
	if got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{
		"name": "other", "instruction": "做一次",
	}); !strings.Contains(got, "新建") || len(desktopScheduleRows(mgr, "inst_1", "bot_1")) != 2 {
		t.Fatalf("missing clock = %q", got)
	}
	if got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_2", map[string]interface{}{"action": "list"}); got != "当前没有定时任务" {
		t.Fatalf("other bot = %q", got)
	}
	if got = desktopBotScheduleTool(mgr, p, "inst_1", "bot_1", map[string]interface{}{"action": "cancel", "name": "free_resources"}); !strings.Contains(got, "已取消") {
		t.Fatal(got)
	}
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].Name != "morning" {
		t.Fatalf("after cancel = %+v", rows)
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
