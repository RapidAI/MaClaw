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

func TestExecuteDesktopBotScheduleHoldsTheSharedDesktop(t *testing.T) {
	var marks []bool
	desktopBotBusySink = func(botID string, busy bool) {
		if botID != "bot_1" {
			t.Errorf("busy bot = %q", botID)
		}
		marks = append(marks, busy)
	}
	t.Cleanup(func() { desktopBotBusySink = nil })
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		if len(marks) != 1 || !marks[0] {
			t.Errorf("desktop busy before the run = %v", marks)
		}
		return desktopBotRelayResult{Text: "做好了。"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	_, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:           "sched-busy",
		DesktopBotID: "bot_1",
		Action:       "整理早间简报。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 2 || !marks[0] || marks[1] {
		t.Fatalf("busy marks = %v", marks)
	}
}

func TestExecuteDesktopBotScheduleReleasesTheDesktopForALogin(t *testing.T) {
	released := false
	desktopBotBusySink = func(botID string, busy bool) {
		if botID != "bot_1" {
			t.Errorf("busy bot = %q", botID)
		}
		if !busy {
			released = true
		}
	}
	t.Cleanup(func() { desktopBotBusySink = nil })
	desktopBotReportSink = func(string) {
		if !released {
			t.Error("login handoff was reported while the desktop was still held")
		}
	}
	t.Cleanup(func() { desktopBotReportSink = nil })
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		if released {
			t.Error("desktop was released before the run finished")
		}
		return desktopBotRelayResult{Text: "需要登录", UserControl: true}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	_, err := (&App{}).executeDesktopBotScheduledTask(context.Background(), &scheduler.ScheduledTask{
		ID:              "sched-login",
		DesktopBotID:    "bot_1",
		DesktopBotOwner: "alice",
		Action:          "打开网站。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("desktop was still held after a login handoff")
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
		return desktopBotRelayResult{Text: "做好了。"}, nil
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
	if err != nil {
		t.Fatal(err)
	}
	if text != "做好了。" || successes != 1 || relays != 2 {
		t.Fatalf("text=%q relays=%d successes=%d polls=%d", text, relays, successes, polls)
	}
	if strings.Contains(payload, desktopBotBusyMessage) || !strings.Contains(payload, `"text":"做好了。"`) {
		t.Fatalf("report = %s", payload)
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
	if err != nil || text != "" || reports != 0 || relays != 1 {
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
