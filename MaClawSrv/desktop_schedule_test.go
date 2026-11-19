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
	if n, left := cancelDesktopBotSchedules(mgr, "inst_1", "bot_1", "brief"); n != 1 || left != 0 {
		t.Fatalf("cancel n=%d left=%d", n, left)
	}
	rows = desktopScheduleRows(mgr, "inst_1", "bot_1")
	if len(rows) != 1 || rows[0].Name != "baidu" || rows[0].IntervalMinutes != 15 {
		t.Fatalf("remaining = %+v", rows)
	}
	if n, left := cancelDesktopBotSchedules(mgr, "inst_1", "bot_1", ""); n != 1 || left != 0 || len(desktopScheduleRows(mgr, "inst_1", "bot_1")) != 0 {
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
	if first.Metadata["bot_id"] != "bot_1" || first.Metadata["bot_phase"] != "execute" || first.Metadata["computer_helper"] != "1" || first.Metadata["bot_origin"] != "schedule" || first.Metadata["schedule_task"] != "sched-1" {
		t.Fatalf("meta = %#v", first.Metadata)
	}
	if !strings.HasPrefix(first.Content, desktopbot.RoutineQuietPrompt) || !strings.Contains(first.Content, "打开首页") || strings.Contains(first.Content, "请直接执行以下操作并返回结果") || !strings.Contains(first.Content, "上一轮留下的文件不能再发一次") {
		t.Fatalf("content = %q", first.Content)
	}
	due = due.Add(24 * time.Hour)
	task.NextRunAt = &due
	next := desktopBotScheduleMessage(task)
	if next.ClientMessageID == first.ClientMessageID {
		t.Fatal("the next day reused the occurrence id")
	}
}

func TestNoneDesktopScheduleIsNotAComputerHelper(t *testing.T) {
	due := time.Date(2026, 11, 13, 8, 0, 0, 0, time.Local)
	task := &scheduler.ScheduledTask{
		ID: "sched-none", DesktopBotID: "bot_none", Action: "看看有没有新消息", NextRunAt: &due,
	}
	msg := desktopBotScheduleMessageMode(task, true)
	if msg.Metadata["computer_helper"] != "" || msg.Metadata["bot_desktop"] != "none" || msg.Metadata["bot_id"] != "bot_none" || msg.Metadata["bot_origin"] != "schedule" {
		t.Fatalf("meta = %#v", msg.Metadata)
	}
	if msg.ClientMessageID == "" || !strings.Contains(msg.Content, "看看有没有新消息") {
		t.Fatalf("message = %+v", msg)
	}

	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}
	if err := store.SaveInstance(agentservice.Instance{
		ID: "inst_none", TenantID: "tenant-a", UserID: "alice",
		Metadata: map[string]string{"hub_bot": "1", "hub_bot_desktop": "none"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInstance(agentservice.Instance{
		ID: "inst_desk", TenantID: "tenant-a", UserID: "alice",
		Metadata: map[string]string{"hub_bot": "1"},
	}); err != nil {
		t.Fatal(err)
	}
	if !scheduleTurnHasNoDesktop(context.Background(), svc, principal, "inst_none") {
		t.Fatal("none instance was treated as a computer")
	}
	if scheduleTurnHasNoDesktop(context.Background(), svc, principal, "inst_desk") {
		t.Fatal("desktop instance was treated as having no computer")
	}
	if scheduleTurnHasNoDesktop(context.Background(), svc, principal, "missing") {
		t.Fatal("a missing instance dropped the computer helper")
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

func TestDesktopBotScheduleListAndDeleteStayOnThisBot(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	add := func(botID, name string) string {
		t.Helper()
		id, err := mgr.Add(scheduler.ScheduledTask{
			Name: name, Action: name, DesktopBotID: botID, DesktopBotOwner: "alice",
			InstanceID: "inst_4", OwnerTenantID: "tenant-a", OwnerUserID: "alice",
			TaskType: scheduler.TaskTypeReminder, IntervalMinutes: 5,
			DayOfWeek: -1, DayOfMonth: -1, Status: "active",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine := add("bot_4", "天气")
	other := add("bot_1", "别的")
	items := listDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice")
	if len(items) != 1 || items[0].ID != mine || items[0].Name != "天气" || items[0].IntervalMinutes != 5 {
		t.Fatalf("list = %+v", items)
	}
	if listDesktopBotSchedules(mgr, "inst_4", "bot_4", "bob") != nil && len(listDesktopBotSchedules(mgr, "inst_4", "bot_4", "bob")) != 0 {
		t.Fatal("another user saw the timer")
	}
	if err := deleteDesktopBotSchedule(mgr, "inst_4", "bot_4", "alice", other); err == nil {
		t.Fatal("deleted another bot's timer")
	}
	if err := deleteDesktopBotSchedule(mgr, "inst_4", "bot_4", "alice", mine); err != nil {
		t.Fatal(err)
	}
	if left := listDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice"); len(left) != 0 {
		t.Fatalf("still listed %+v", left)
	}
	if rows := desktopScheduleRows(mgr, "inst_4", "bot_1"); len(rows) != 1 || rows[0].ID != other {
		t.Fatalf("other bot = %+v", rows)
	}
	if n := deleteDesktopBotSchedules(mgr, "inst_4", "bot_1", "alice"); n != 1 {
		t.Fatalf("deleted %d", n)
	}
}

func TestRetainDesktopBotSchedulesDropsOnlyGoneBots(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	add := func(instanceID, botID, name string) {
		t.Helper()
		if _, err := mgr.Add(scheduler.ScheduledTask{
			Name: name, Action: name, DesktopBotID: botID, DesktopBotOwner: "alice",
			InstanceID: instanceID, OwnerTenantID: "tenant-a", OwnerUserID: "alice",
			TaskType: scheduler.TaskTypeReminder, IntervalMinutes: 5,
			DayOfWeek: -1, DayOfMonth: -1, Status: "active",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("inst_4", "bot_4", "天气")
	add("inst_4", "bot_gone", "旧的")
	add("inst_other", "bot_other", "别人的机器")
	if _, err := mgr.Add(scheduler.ScheduledTask{
		Name: "主机任务", Action: "不要动", InstanceID: "inst_4", OwnerUserID: "alice",
		TaskType: scheduler.TaskTypeReminder, Hour: 8, Minute: 0,
		DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	if n := retainDesktopBotSchedules(mgr, "inst_4", "alice", nil); n != 0 {
		t.Fatalf("empty live set deleted %d", n)
	}
	if n := retainDesktopBotSchedules(mgr, "inst_4", "alice", []string{"bot_4"}); n != 1 {
		t.Fatalf("dropped %d", n)
	}
	if left := listDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice"); len(left) != 1 || left[0].Name != "天气" {
		t.Fatalf("kept = %+v", left)
	}
	if left := listDesktopBotSchedules(mgr, "inst_4", "bot_gone", "alice"); len(left) != 0 {
		t.Fatalf("gone bot still listed %+v", left)
	}
	if left := listDesktopBotSchedules(mgr, "inst_other", "bot_other", "alice"); len(left) != 1 {
		t.Fatalf("other instance = %+v", left)
	}
	host := 0
	for _, task := range mgr.List() {
		if task.Name == "主机任务" {
			host++
		}
	}
	if host != 1 {
		t.Fatalf("host timers = %d", host)
	}
}

func TestManageScheduleCreateKeepsTheDesktopBot(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	got := srvToolCreateScheduledTask(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice", DesktopBotID: "bot_4",
	}, map[string]interface{}{
		"name": "北京天气PDF", "task_action": "查询北京天气并生成PDF", "interval_minutes": 5,
	})
	if !strings.Contains(got, "定时任务已创建") {
		t.Fatal(got)
	}
	var found *scheduler.ScheduledTask
	for _, task := range mgr.List() {
		if task.Name == "北京天气PDF" {
			copy := task
			found = &copy
		}
	}
	if found == nil || found.DesktopBotID != "bot_4" || found.TaskType != scheduler.TaskTypeReminder || found.InstanceID != "inst_4" {
		t.Fatalf("row = %+v", found)
	}
	plain := srvToolCreateScheduledTask(nil, mgr, srvScheduleCaller{InstanceID: "inst_4", UserID: "alice"}, map[string]interface{}{
		"name": "主机任务", "task_action": "不要动", "hour": 8, "minute": 0,
	})
	if !strings.Contains(plain, "定时任务已创建") {
		t.Fatal(plain)
	}
	for _, task := range mgr.List() {
		if task.Name == "主机任务" && task.DesktopBotID != "" {
			t.Fatalf("host row tagged %q", task.DesktopBotID)
		}
	}
	pushed := srvToolCreateScheduledTask(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice", DesktopBotID: "bot_4",
	}, map[string]interface{}{
		"name": "北京天气推送", "task_action": "查询北京天气并生成PDF", "interval_minutes": 5,
		"channel": "weixin", "user_id": "self",
	})
	if !strings.Contains(pushed, "已创建") || !strings.Contains(pushed, "不另建推送") {
		t.Fatal(pushed)
	}
	for _, task := range mgr.List() {
		if task.Name == "北京天气推送" && (task.DesktopBotID != "bot_4" || task.Delivery != nil || task.IntervalMinutes != 5) {
			t.Fatalf("push row = %+v", task)
		}
	}
	closed := srvToolCreateScheduledTask(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice", DesktopBotID: "bot_4",
	}, map[string]interface{}{
		"name": "限期天气", "task_action": "查询北京天气", "interval_minutes": 5,
		"start_date": "2026-11-14", "end_date": "2026-11-14", "day_of_month": 14,
	})
	if !strings.Contains(closed, "已创建") || !strings.Contains(closed, "一直重复") || !strings.Contains(closed, "每 5 分钟") {
		t.Fatal(closed)
	}
	for _, task := range mgr.List() {
		if task.Name == "限期天气" && (task.EndDate != "" || task.StartDate != "" || task.DayOfMonth > 0 || task.IntervalMinutes != 5) {
			t.Fatalf("window row = %+v", task)
		}
	}
}

func TestScheduleCatalogHidesLongJobPatrols(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	if _, err := mgr.Add(scheduler.ScheduledTask{
		Name: "天气", Action: "报告天气", DesktopBotID: "bot_4", DesktopBotOwner: "alice",
		InstanceID: "inst_4", OwnerTenantID: "tenant-a", OwnerUserID: "alice",
		TaskType: scheduler.TaskTypeReminder, IntervalMinutes: 5,
		DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	patrol, err := mgr.Add(scheduler.ScheduledTask{
		Name: desktopLongJobTaskPrefix + "job_1", Action: "巡查", DesktopBotID: "bot_4",
		InstanceID: "inst_4", OwnerUserID: "alice",
		TaskType: scheduler.TaskTypeReminder, IntervalMinutes: 1,
		DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	items := listDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice")
	if len(items) != 1 || items[0].Name != "天气" {
		t.Fatalf("catalog = %+v", items)
	}
	if !desktopBotScheduleIsPatrol(mgr, patrol) {
		t.Fatal("patrol was not recognized")
	}
	if n := deleteDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice"); n != 2 {
		t.Fatalf("bot delete removed %d", n)
	}
}

func TestScheduleClockTextUsesTheLocalOffset(t *testing.T) {
	utc := time.Date(2026, 11, 14, 14, 31, 0, 0, time.UTC)
	got := scheduleClockText(&utc)
	local := utc.In(time.Local)
	if !strings.Contains(got, local.Format("15:04")) || !strings.Contains(got, local.Format("-07:00")) {
		t.Fatalf("clock = %s local = %s", got, local.Format("2006-01-02 15:04 -07:00"))
	}
	if scheduleClockText(nil) != "-" {
		t.Fatal("nil clock")
	}
}

func TestDesktopBotListDropsAStalePushWarning(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name: "北京天气PDF", Action: "查询北京天气并生成PDF", DesktopBotID: "bot_4",
		InstanceID: "inst_4", OwnerTenantID: "tenant-a", OwnerUserID: "alice",
		TaskType: scheduler.TaskTypeReminder, IntervalMinutes: 5,
		DayOfWeek: -1, DayOfMonth: 14, StartDate: "2026-11-14", EndDate: "2099-01-01", Status: "active",
		Delivery: &scheduler.TaskDelivery{
			Enabled: true, Channel: scheduler.DeliveryChannelWeixin,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "self"}},
		},
		LastResult: "已生成 PDF\n\n[投递警告] no active weixin session",
	})
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := mgr.Add(scheduler.ScheduledTask{
		Name: "主机窗口", Action: "不要动", InstanceID: "inst_4", OwnerUserID: "alice",
		Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: 3, EndDate: "2099-01-01", Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newSrvManageScheduleHandlerForCaller(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice",
	})
	listed := h(map[string]interface{}{"action": "list", "desktop_bot_id": "bot_4"})
	if strings.Contains(listed, "投递") || strings.Contains(listed, "推送") || !strings.Contains(listed, "每 5 分钟") {
		t.Fatalf("list: %s", listed)
	}
	got := mgr.Get(id)
	if got == nil || got.Delivery != nil || got.IntervalMinutes != 5 || got.EndDate != "" || got.StartDate != "" || got.DayOfMonth > 0 || strings.Contains(got.LastResult, "投递") || !strings.Contains(got.LastResult, "已生成 PDF") {
		t.Fatalf("row = %+v", got)
	}
	host := mgr.Get(hostID)
	if host == nil || host.EndDate != "2099-01-01" || host.DayOfMonth != 3 || host.DesktopBotID != "" {
		t.Fatalf("host window = %+v", host)
	}
	made := h(map[string]interface{}{
		"action": "create", "desktop_bot_id": "bot_4", "name": "过程任务",
		"task_action": "查询", "interval_minutes": 5, "task_type": "process",
	})
	if !strings.Contains(made, "已创建") {
		t.Fatal(made)
	}
	for _, task := range mgr.List() {
		if task.Name == "过程任务" && task.TaskType != scheduler.TaskTypeReminder {
			t.Fatalf("task type = %s", task.TaskType)
		}
	}
}

func TestManageScheduleEditKeepsTheIntervalAndTheOrder(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	h := newSrvManageScheduleHandlerForCaller(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice",
	})
	created := h(map[string]interface{}{
		"action": "create", "desktop_bot_id": "bot_4", "name": "北京天气PDF",
		"task_action": "查询北京天气", "interval_minutes": 5,
	})
	if !strings.Contains(created, "已创建") || !strings.Contains(created, "每 5 分钟") {
		t.Fatal(created)
	}
	edited := h(map[string]interface{}{
		"action": "create", "desktop_bot_id": "bot_4", "name": "北京天气PDF",
		"task_action": "查询北京天气并生成PDF",
	})
	if !strings.Contains(edited, "已更新") || !strings.Contains(edited, "每 5 分钟") || strings.Contains(edited, "hour 必须") {
		t.Fatalf("edit: %s", edited)
	}
	var id string
	for _, task := range mgr.List() {
		if task.Name != "北京天气PDF" {
			continue
		}
		id = task.ID
		if task.IntervalMinutes != 5 || task.Action != "查询北京天气并生成PDF" || task.DesktopBotID != "bot_4" {
			t.Fatalf("row = %+v", task)
		}
	}
	if id == "" || len(mgr.List()) != 1 {
		t.Fatalf("rows = %+v", mgr.List())
	}
	kept := h(map[string]interface{}{
		"action": "create", "desktop_bot_id": "bot_4", "name": "北京天气PDF",
		"task_action": "查询北京天气并生成PDF", "interval_minutes": 0,
	})
	if !strings.Contains(kept, "每 5 分钟") {
		t.Fatalf("zero interval: %s", kept)
	}
	updated := h(map[string]interface{}{
		"action": "update", "desktop_bot_id": "bot_4", "id": id,
		"interval_minutes": 0, "channel": "weixin", "user_id": "self",
	})
	if !strings.Contains(updated, "查询北京天气并生成PDF") || strings.Contains(updated, "操作: update") || !strings.Contains(updated, "不另建推送") || !strings.Contains(updated, "每 5 分钟") {
		t.Fatalf("update: %s", updated)
	}
	if got := mgr.Get(id); got == nil || got.IntervalMinutes != 5 || got.Delivery != nil {
		t.Fatalf("push update = %+v", got)
	}
	if err := mgr.Update(id, map[string]interface{}{"delivery": &scheduler.TaskDelivery{
		Enabled: true, Channel: scheduler.DeliveryChannelWeixin,
		Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "self"}},
	}}); err != nil {
		t.Fatal(err)
	}
	listed := h(map[string]interface{}{"action": "list", "desktop_bot_id": "bot_4"})
	if !strings.Contains(listed, "每 5 分钟") || strings.Contains(listed, "推送") {
		t.Fatalf("list: %s", listed)
	}
	if got := mgr.Get(id); got == nil || got.Delivery != nil || got.IntervalMinutes != 5 {
		t.Fatalf("list kept a push: %+v", got)
	}
}

func TestManageScheduleDoesNotCancelOrForkARunningBotTimer(t *testing.T) {
	mgr := newDesktopScheduleManager(t)
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name: "北京天气PDF", Action: "每五分钟查询北京的天气并生成PDF发给我",
		InstanceID: "inst_4", OwnerTenantID: "tenant-a", OwnerUserID: "alice",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Add(scheduler.ScheduledTask{
		Name: "主机任务", Action: "不要动", InstanceID: "inst_4", OwnerUserID: "alice",
		Hour: 8, Minute: 0, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	mgr.SetExecutor(func(ctx context.Context, _ *scheduler.ScheduledTask) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		cancelled <- struct{}{}
		return "", ctx.Err()
	})
	if err := mgr.TriggerNow(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timer did not start")
	}
	h := newSrvManageScheduleHandlerForCaller(nil, mgr, srvScheduleCaller{
		InstanceID: "inst_4", TenantID: "tenant-a", UserID: "alice",
	})
	again := h(map[string]interface{}{
		"action": "create", "desktop_bot_id": "bot_4", "name": "北京天气PDF",
		"task_action": "每五分钟查询北京的天气并生成PDF发给我", "interval_minutes": float64(5),
	})
	if !strings.Contains(again, "已更新") || !strings.Contains(again, id) || !strings.Contains(again, "每 5 分钟") {
		t.Fatalf("create: %s", again)
	}
	if rows := mgr.List(); len(rows) != 2 {
		t.Fatalf("create forked the timer: %d", len(rows))
	}
	select {
	case <-cancelled:
		t.Fatal("create stopped the running timer")
	default:
	}
	del := h(map[string]interface{}{"action": "delete", "desktop_bot_id": "bot_4", "id": id})
	if !strings.Contains(del, "没有取消") {
		t.Fatalf("delete: %s", del)
	}
	botCancel := desktopBotScheduleTool(mgr, agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}, "inst_4", "bot_4", map[string]interface{}{
		"action": "cancel", "name": "北京天气PDF",
	})
	if !strings.Contains(botCancel, "没有取消") {
		t.Fatalf("bot_schedule cancel: %s", botCancel)
	}
	if err := armDesktopBotSchedule(mgr, agentservice.Principal{TenantID: "tenant-a", UserID: "alice"}, "inst_4", "bot_4", "", agentservice.BotIntake{
		Kind:     "schedule",
		Schedule: &agentservice.BotIntakeSchedule{Name: "北京天气PDF", Mode: "cancel"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("cancel stopped the running timer")
	default:
	}
	n := 0
	for _, task := range mgr.List() {
		switch task.Name {
		case "北京天气PDF":
			n++
			if task.ID != id || task.DesktopBotID != "bot_4" || task.IntervalMinutes != 5 {
				t.Fatalf("timer = %+v", task)
			}
		case "主机任务":
			if task.DesktopBotID != "" {
				t.Fatalf("host row tagged %q", task.DesktopBotID)
			}
		default:
			t.Fatalf("extra row %s", task.Name)
		}
	}
	if n != 1 {
		t.Fatalf("weather rows = %d", n)
	}
	listed := listDesktopBotSchedules(mgr, "inst_4", "bot_4", "alice")
	if len(listed) != 1 || listed[0].ID != id {
		t.Fatalf("panel = %+v", listed)
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
