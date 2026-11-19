package main

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestScheduleTaskIDFromClientMessage(t *testing.T) {
	if got := scheduleTaskIDFromClientMessage("sched-1794664010883393478-359f-1710000000"); got != "1794664010883393478-359f" {
		t.Fatalf("got %q", got)
	}
	if got := scheduleTaskIDFromClientMessage("hello"); got != "" {
		t.Fatalf("plain %q", got)
	}
	if got := scheduleTaskIDFromClientMessage("sched-abc-x"); got != "" {
		t.Fatalf("due %q", got)
	}
}

func TestHoldResumedScheduleBlocksTheClock(t *testing.T) {
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mgr.Stop()
		setSrvSchedulerManager(nil)
	})
	weather, err := mgr.Add(scheduler.ScheduledTask{
		Name: "北京天气PDF", Action: "每五分钟查询北京的天气并生成PDF发给我",
		DesktopBotID: "bot_4", InstanceID: "inst_4",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := mgr.Add(scheduler.ScheduledTask{
		Name: "喝水", Action: "提醒喝水", DesktopBotID: "bot_4", InstanceID: "inst_4",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := mgr.Add(scheduler.ScheduledTask{
		Name: "别的机器人", Action: "做别的事", DesktopBotID: "bot_1", InstanceID: "inst_1",
		IntervalMinutes: 5, DayOfWeek: -1, DayOfMonth: -1, Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	setSrvSchedulerManager(mgr)
	release := holdResumedDesktopSchedule(nil, agentservice.Run{
		ID: "run_resume", InstanceID: "inst_4",
		Metadata: map[string]string{"bot_origin": "schedule", "bot_id": "bot_4"},
	})
	if release == nil || !mgr.Running(weather) || !mgr.Running(other) || mgr.Running(elsewhere) {
		t.Fatalf("hold weather=%v other=%v elsewhere=%v", mgr.Running(weather), mgr.Running(other), mgr.Running(elsewhere))
	}
	release()
	if mgr.Running(weather) || mgr.Running(other) {
		t.Fatal("release left a hold")
	}
	named := holdResumedDesktopSchedule(nil, agentservice.Run{
		ID: "run_named", InstanceID: "inst_4",
		Metadata: map[string]string{"bot_origin": "schedule", "bot_id": "bot_4", "schedule_task": weather},
	})
	if named == nil || !mgr.Running(weather) || mgr.Running(other) {
		t.Fatalf("named weather=%v other=%v", mgr.Running(weather), mgr.Running(other))
	}
	named()
}
