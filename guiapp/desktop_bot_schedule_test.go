package guiapp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestArmDesktopBotScheduleRepeatsAndUpdatesTheSameAction(t *testing.T) {
	shareAccessRemoteUserOverride = "local"
	t.Cleanup(func() { shareAccessRemoteUserOverride = "" })
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{scheduledTaskManager: mgr}
	id, err := app.ArmDesktopBotSchedule("bot_1", "每分钟问好", "create", "向用户问好一次。", 1, -1, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := app.ArmDesktopBotSchedule("bot_1", "每分钟问好", "create", "向用户问好一次。", 5, 8, 30, -1)
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatalf("same action created another task: %s then %s", id, again)
	}
	tasks := mgr.List()
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	got := tasks[0]
	if got.DesktopBotID != "bot_1" || got.DesktopBotOwner != "local" || got.IntervalMinutes != 5 || got.Hour != 0 || got.Minute != 0 || got.DayOfWeek != -1 {
		t.Fatalf("updated task = %#v", got)
	}
	clock, err := app.ArmDesktopBotSchedule("bot_1", "早间简报", "create", "整理早间简报。", 0, 8, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	if clock == id {
		t.Fatal("a different action replaced the greeting")
	}
	if len(mgr.List()) != 2 {
		t.Fatalf("tasks=%d", len(mgr.List()))
	}
	n, err := app.ArmDesktopBotSchedule("bot_1", "每分钟问好", "cancel", "", 0, 0, 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if n != "1" {
		t.Fatalf("cancel count = %s", n)
	}
	left := mgr.List()
	if len(left) != 1 || left[0].Name != "早间简报" || left[0].Hour != 8 || left[0].Minute != 30 || left[0].DayOfWeek != 1 || left[0].IntervalMinutes != 0 {
		t.Fatalf("remaining = %#v", left)
	}
	if _, err := app.ArmDesktopBotSchedule("bot_1", "不存在", "cancel", "", 0, 0, 0, -1); err == nil || !strings.Contains(err.Error(), "no desktop bot schedule matched") {
		t.Fatalf("missing cancel = %v", err)
	}
}

func TestListDesktopBotSchedulesShowsOnlyThisBot(t *testing.T) {
	shareAccessRemoteUserOverride = "local"
	t.Cleanup(func() { shareAccessRemoteUserOverride = "" })
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{scheduledTaskManager: mgr}
	if _, err := app.ArmDesktopBotSchedule("bot_1", "每分钟问好", "create", "向用户问好一次。", 1, -1, -1, -1); err != nil {
		t.Fatal(err)
	}
	other, err := app.ArmDesktopBotSchedule("bot_2", "早间简报", "create", "整理早间简报。", 0, 8, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := app.ListDesktopBotSchedules("bot_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Name != "每分钟问好" || listed[0].IntervalMinutes != 1 || listed[0].ID == "" {
		t.Fatalf("list = %#v", listed)
	}
	if err := app.DeleteDesktopBotSchedule("bot_1", other); err == nil || !strings.Contains(err.Error(), "no desktop bot schedule matched") {
		t.Fatalf("cross-bot delete = %v", err)
	}
	if err := app.DeleteDesktopBotSchedule("bot_1", listed[0].ID); err != nil {
		t.Fatal(err)
	}
	left, err := app.ListDesktopBotSchedules("bot_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("bot_1 still has %#v", left)
	}
	kept, err := app.ListDesktopBotSchedules("bot_2")
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].ID != other || kept[0].Hour != 8 || kept[0].Minute != 30 || kept[0].DayOfWeek != 1 {
		t.Fatalf("other bot = %#v", kept)
	}
}

func TestExecuteDesktopBotScheduleReportsTheWorkerReply(t *testing.T) {
	var gotBot, gotText, gotPhase string
	desktopBotRelayOverride = func(_ context.Context, botID, text, phase string) (desktopBotRelayResult, error) {
		gotBot, gotText, gotPhase = botID, text, phase
		return desktopBotRelayResult{Text: "你好。"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	var payload string
	desktopBotReportSink = func(raw string) { payload = raw }
	t.Cleanup(func() { desktopBotReportSink = nil })
	app := &App{}
	text, err := app.executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-1",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "向用户问好一次。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "你好。" || gotBot != "bot_1" || gotPhase != "execute" || !strings.Contains(gotText, "向用户问好一次。") || !strings.Contains(gotText, "already set") {
		t.Fatalf("relay text=%q bot=%q phase=%q order=%q", text, gotBot, gotPhase, gotText)
	}
	if !strings.Contains(payload, `"schedule_report":true`) || !strings.Contains(payload, `"text":"你好。"`) || !strings.Contains(payload, `"bot_id":"bot_1"`) || !strings.Contains(payload, `"owner_id":"alice"`) || !strings.Contains(payload, "desktop-bot-sched-") {
		t.Fatalf("report = %s", payload)
	}
}
