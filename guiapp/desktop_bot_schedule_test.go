package guiapp

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{Text: "你好。"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	var payload string
	desktopBotReportSink = func(raw string) { payload = raw }
	t.Cleanup(func() { desktopBotReportSink = nil })
	text, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-1",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "向用户问好一次。",
	})
	if err != nil || text != "" || relays != 0 || payload != "" {
		t.Fatalf("GUI started the round text=%q relays=%d payload=%s err=%v", text, relays, payload, err)
	}
}

func TestExecuteDesktopBotScheduleReportsSendToUser(t *testing.T) {
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{Text: "无变化", SendToUser: "首页没打开。"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	reports := 0
	desktopBotReportSink = func(string) { reports++ }
	t.Cleanup(func() { desktopBotReportSink = nil })
	text, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-sentence",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "打开首页。",
	})
	if err != nil || text != "" || relays != 0 || reports != 0 {
		t.Fatalf("GUI started the round text=%q relays=%d reports=%d err=%v", text, relays, reports, err)
	}
}

func TestExecuteDesktopBotScheduleHoldsTheSharedDesktop(t *testing.T) {
	var marks []bool
	desktopBotBusySink = func(string, bool) { marks = append(marks, true) }
	t.Cleanup(func() { desktopBotBusySink = nil })
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{Text: "做好了。"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	_, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:           "sched-busy",
		DesktopBotID: "bot_1",
		Action:       "整理早间简报。",
	})
	if err != nil || relays != 0 || len(marks) != 0 {
		t.Fatalf("GUI held the desktop relays=%d marks=%v err=%v", relays, marks, err)
	}
}

func TestExecuteDesktopBotScheduleReleasesTheDesktopForALogin(t *testing.T) {
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{Text: "需要登录", UserControl: true}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	_, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-login",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "打开网站。",
	})
	if err != nil || relays != 0 {
		t.Fatalf("GUI started the login round relays=%d err=%v", relays, err)
	}
}

func TestExecuteDesktopBotScheduleSendsOnceWhenTheDesktopIsFree(t *testing.T) {
	prev := desktopBotBusyPoll
	desktopBotBusyPoll = 5 * time.Millisecond
	t.Cleanup(func() {
		desktopBotBusyPoll = prev
		desktopBotRelayOverride = nil
		desktopBotExchangeOverride = nil
		desktopBotReportSink = nil
	})
	relays := 0
	successes := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		if relays == 1 {
			return desktopBotRelayResult{}, fmt.Errorf("%s", desktopBotBusyMessage)
		}
		successes++
		return desktopBotRelayResult{Text: "做好了。", SendToUser: "做好了。"}, nil
	}
	polls := 0
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.Contains(path, "/tasks") {
			polls++
			payload := map[string]any{
				"items": []map[string]any{{"run_id": "run-1", "bot_id": "bot_1", "status": "running", "task": "早报"}},
			}
			if polls >= 2 {
				payload = map[string]any{"items": []any{}}
			}
			fillDesktopBotDest(t, dest, payload)
			return http.StatusOK, nil
		}
		return 0, fmt.Errorf("no bot list")
	}
	var payload string
	desktopBotReportSink = func(raw string) { payload = raw }
	text, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-wait",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "整理早间简报。",
	})
	if err != nil || text != "" || relays != 0 || successes != 0 || payload != "" {
		t.Fatalf("GUI started the round text=%q relays=%d successes=%d payload=%s err=%v", text, relays, successes, payload, err)
	}
}

func TestExecuteDesktopBotScheduleSkipsWhenTheDesktopStaysBusy(t *testing.T) {
	prev := desktopBotBusyPoll
	desktopBotBusyPoll = 5 * time.Millisecond
	t.Cleanup(func() {
		desktopBotBusyPoll = prev
		desktopBotRelayOverride = nil
		desktopBotExchangeOverride = nil
		desktopBotReportSink = nil
	})
	relays := 0
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		return desktopBotRelayResult{}, fmt.Errorf("%s", desktopBotBusyMessage)
	}
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.Contains(path, "/tasks") {
			fillDesktopBotDest(t, dest, map[string]any{
				"items": []map[string]any{{"run_id": "run-1", "bot_id": "bot_1", "status": "running", "task": "早报"}},
			})
			return http.StatusOK, nil
		}
		return 0, fmt.Errorf("no bot list")
	}
	reports := 0
	desktopBotReportSink = func(string) { reports++ }
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	text, err := (&App{}).executeDesktopBotScheduledTask(ctx, &scheduler.ScheduledTask{
		ID:           "sched-skip",
		DesktopBotID: "bot_1",
		Action:       "整理早间简报。",
	})
	if err != nil || text != "" || reports != 0 || relays != 0 {
		t.Fatalf("text=%q err=%v reports=%d relays=%d", text, err, reports, relays)
	}
}

func TestGetDesktopBotBusyRestoresHoldsForTheOpenAccount(t *testing.T) {
	desktopBotBusyMu.Lock()
	desktopBotBusyCount = map[string]int{}
	desktopBotBusyMu.Unlock()
	t.Cleanup(func() {
		desktopBotBusyMu.Lock()
		desktopBotBusyCount = map[string]int{}
		desktopBotBusyMu.Unlock()
	})
	app := &App{}
	app.noteDesktopBotBusy("bot_1", "alice", true)
	app.noteDesktopBotBusy("bot_1", "alice", true)
	app.noteDesktopBotBusy("bot_2", "alice", true)
	app.noteDesktopBotBusy("bot_9", "bob", true)
	got := app.GetDesktopBotBusy("alice")
	if len(got.Marks) != 2 || got.Marks[0].BotID != "bot_1" || got.Marks[0].Count != 2 || got.Marks[1].BotID != "bot_2" || got.Marks[1].Count != 1 {
		t.Fatalf("alice holds = %+v", got.Marks)
	}
	app.noteDesktopBotBusy("bot_1", "alice", false)
	got = app.GetDesktopBotBusy("alice")
	if len(got.Marks) != 2 || got.Marks[0].Count != 1 {
		t.Fatalf("alice holds after one release = %+v", got.Marks)
	}
	if bob := app.GetDesktopBotBusy("bob"); len(bob.Marks) != 1 || bob.Marks[0].BotID != "bot_9" {
		t.Fatalf("bob holds = %+v", bob.Marks)
	}
}

func TestGetDesktopBotBusyKeepsALiveRunAfterTheBubbleAgesOut(t *testing.T) {
	activeDesktopRunsMu.Lock()
	previous := activeDesktopRuns
	activeDesktopRuns = map[string]*ActiveDesktopRun{}
	activeDesktopRunsMu.Unlock()
	t.Cleanup(func() {
		activeDesktopRunsMu.Lock()
		activeDesktopRuns = previous
		activeDesktopRunsMu.Unlock()
	})
	app := &App{}
	app.registerActiveDesktopRun("bot_1", "req-1", "alice:bot_1", "打开网站。", "execute")
	got := app.GetDesktopBotBusy("alice")
	if len(got.Runs) != 1 || got.Runs[0] != "bot_1" {
		t.Fatalf("runs = %v", got.Runs)
	}
	if bob := app.GetDesktopBotBusy("bob"); len(bob.Runs) != 0 {
		t.Fatalf("bob runs = %v", bob.Runs)
	}
	app.markDesktopRunWaitingForUser("bot_1", "req-1")
	if waiting := app.GetDesktopBotBusy("alice"); len(waiting.Runs) != 0 {
		t.Fatalf("login handoff still driving: %v", waiting.Runs)
	}
}
