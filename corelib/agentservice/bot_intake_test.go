package agentservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestIntakeSupplementChangeAndNew(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	running := []BotIntakeRun{{RunID: "run_a", Task: "整理上周订单报表"}, {RunID: "run_b", Task: "给供应商写催货邮件"}}
	in := BotIntakeInput{BotID: "bot_p", Content: "只要前五条", Lang: "zh-Hans", Running: running}

	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, "1. 整理上周订单报表") || !strings.Contains(user, "2. 给供应商写催货邮件") {
			t.Fatalf("prompt missing runs: %s", user)
		}
		return `{"kind":"supplement","run":1,"instruction":"报表只要前五条","ack":"好，报表只留前五条。","pace":"direct","reason":"narrows the report"}`, nil
	})
	if err != nil || got.Kind != "supplement" || got.RunID != "run_a" || got.Instruction != "报表只要前五条" || got.Ack == "" {
		t.Fatalf("supplement = %+v %v", got, err)
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(context.Context, string, string) (string, error) {
		return `{"kind":"change","run":2,"instruction":"改发催货到飞书","ack":"改发飞书。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "change" || got.RunID != "run_b" || !strings.HasPrefix(got.Instruction, botIntakeChangeMark) {
		t.Fatalf("change = %+v %v", got, err)
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "查一下天气", Running: running}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"instruction":"查询今天的天气","ack":"我去查天气。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.RunID != "" || got.Instruction != "查询今天的天气" {
		t.Fatalf("new = %+v %v", got, err)
	}
}

func TestIntakeStopPauseContinueNeedALiveRun(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	one := []BotIntakeRun{{RunID: "run_a", Task: "导出订单"}}
	for _, kind := range []string{"stop", "pause", "continue"} {
		kind := kind
		got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "接着", Running: one}, func(context.Context, string, string) (string, error) {
			return `{"kind":"` + kind + `","run":0,"instruction":"","ack":"好。","pace":"direct"}`, nil
		})
		if err != nil || got.Kind != kind || got.RunID != "run_a" {
			t.Fatalf("%s with one run = %+v %v", kind, got, err)
		}
	}
	two := []BotIntakeRun{{RunID: "run_a", Task: "甲"}, {RunID: "run_b", Task: "乙"}}
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "停一下", Running: two}, func(context.Context, string, string) (string, error) {
		return `{"kind":"stop","run":0,"instruction":"","ack":"停。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Instruction != "停一下" {
		t.Fatalf("ambiguous stop fell through as %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "继续"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"continue","run":1,"instruction":"","ack":"接着做。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" {
		t.Fatalf("continue with nothing running = %+v %v", got, err)
	}
}

func TestIntakeReplyAskScheduleAndStatus(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "你好"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"reply","run":0,"instruction":"","ack":"在的。","told":"在的，说吧。","pace":"reply"}`, nil
	})
	if err != nil || got.Kind != "reply" || got.Told != "在的，说吧。" || got.Instruction != "" {
		t.Fatalf("reply = %+v %v", got, err)
	}
	hour, minute := 8, 30
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "每个工作日早上做简报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"instruction":"做一份简报","ack":"工作日早上的简报记下了。","pace":"schedule","schedule":{"mode":"create","name":"brief","hour":8,"minute":30,"day_of_week":1}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.Hour == nil || *got.Schedule.Hour != hour || *got.Schedule.Minute != minute {
		t.Fatalf("schedule = %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "以后再说"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"ack":"好。","pace":"schedule","schedule":{"mode":"create"}}`, nil
	})
	if err != nil || got.Kind != "new" || got.Reason != "bad schedule" {
		t.Fatalf("bad schedule = %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "现在怎么样"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"status","run":0,"instruction":"当前任务","ack":"should be dropped","pace":"status"}`, nil
	})
	if err != nil || got.Kind != "status" || got.Ack != "" || got.Instruction != "当前任务" {
		t.Fatalf("status = %+v %v", got, err)
	}
}

func TestIntakeTimeoutAndGarbageBecomeANewTask(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	prev := BotIntakeBudget
	BotIntakeBudget = 80 * time.Millisecond
	defer func() { BotIntakeBudget = prev }()
	start := time.Now()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "打开报表"}, func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		time.Sleep(50 * time.Millisecond)
		return `{"kind":"stop","run":1,"ack":"停","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Reason != "timeout" || got.Instruction != "打开报表" || got.Ack != "" || time.Since(start) > time.Second {
		t.Fatalf("timeout = %+v %v after %v", got, err, time.Since(start))
	}
	for _, raw := range []string{`nope`, `{"kind":"stop"}`, `{"run":1}`} {
		got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "打开报表"}, func(context.Context, string, string) (string, error) {
			return raw, nil
		})
		if err != nil || got.Kind != "new" || got.Instruction != "打开报表" {
			t.Fatalf("%s -> %+v %v", raw, got, err)
		}
	}
	if _, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "telegram:1", Content: "hi"}, nil); err != ErrBotThreadsBadBot {
		t.Fatalf("bad bot err = %v", err)
	}
	if got.Phase != "execute" {
		t.Fatalf("unreadable phase = %q", got.Phase)
	}
}

func TestIntakePhaseComesFromTheModelAndTimeoutStaysExecute(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	// These sentences used to match a keyword list and enter plan. The
	// phase is whatever the model returns; a missing phase is execute.
	for _, text := range []string{"用支付宝查一下账单", "删除重复段落后发布", "format as a table", "please pay attention", "remove the watermark"} {
		got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: text}, func(context.Context, string, string) (string, error) {
			return `{"kind":"new","run":0,"instruction":"do it","ack":"好。","pace":"direct"}`, nil
		})
		if err != nil || got.Phase != "execute" {
			t.Fatalf("%s phase=%q err=%v", text, got.Phase, err)
		}
	}
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "删掉我电脑里的全部文件"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"instruction":"删除这个人自己的全部文件","ack":"这步要先安排。","pace":"direct","phase":"plan"}`, nil
	})
	if err != nil || got.Phase != "plan" || got.Kind != "new" {
		t.Fatalf("destructive = %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "确认，开始吧"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"continue","run":0,"instruction":"","ack":"开始。","pace":"direct","phase":"plan"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" {
		t.Fatalf("confirm follow-up = %+v %v", got, err)
	}
	prev := BotIntakeBudget
	BotIntakeBudget = 40 * time.Millisecond
	defer func() { BotIntakeBudget = prev }()
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "删除 /data 下所有文件"}, func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		return `{"kind":"new","run":0,"phase":"plan","ack":"安排","pace":"direct"}`, nil
	})
	if err != nil || got.Reason != "timeout" || got.Phase != "execute" {
		t.Fatalf("timeout must not fall back to keywords: %+v %v", got, err)
	}
}

func TestIntakeTimerFollowUpStaysASchedule(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	in := BotIntakeInput{
		BotID:   "bot_p",
		Content: "北京，下次再生成pdf文件",
		Earlier: []string{"用户: 每5分钟向我报告下天气情况"},
		Timers:  []BotIntakeTimer{{Name: "weather_report", Action: "每5分钟向我报告下天气情况", IntervalMinutes: 5}},
	}
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(_ context.Context, system, user string) (string, error) {
		if !strings.Contains(system, "timers already armed") || !strings.Contains(user, "weather_report every 5 minutes") || !strings.Contains(user, "每5分钟向我报告下天气情况") {
			t.Fatalf("prompt missing the timer:\n%s\n%s", system, user)
		}
		return `{"kind":"schedule","run":0,"instruction":"每5分钟报告北京的天气，并把这一轮做成 PDF","ack":"以后每5分钟只报北京，并生成 PDF。","pace":"schedule","schedule":{"name":"weather_report","mode":"create"}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.IntervalMinutes != 5 || got.Schedule.Name != "weather_report" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Instruction != "每5分钟报告北京的天气，并把这一轮做成 PDF" {
		t.Fatalf("instruction = %q", got.Instruction)
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"instruction":"每5分钟报告北京的天气，并把这一轮做成 PDF","ack":"以后只报北京，并生成 PDF。","pace":"schedule","schedule":{"mode":"create"}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.Name != "weather_report" || got.Schedule.IntervalMinutes != 5 {
		t.Fatalf("the only timer fills an omitted name: %+v %v", got, err)
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"instruction":"每5分钟打开百度","ack":"另开一个计时。","pace":"schedule","schedule":{"name":"baidu","mode":"create"}}`, nil
	})
	if err != nil || got.Kind != "new" || got.Reason != "bad schedule" {
		t.Fatalf("a different job must bring its own clock: %+v %v", got, err)
	}
}

func TestIntakeKeepsTheArmedWeekdayWhenTheClockMoves(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	in := BotIntakeInput{
		BotID:   "bot_p",
		Content: "改到九点",
		Timers:  []BotIntakeTimer{{Name: "brief", Action: "做一份简报", Hour: 8, Minute: 30, DayOfWeek: 1}},
	}
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, "brief at 8:30 day_of_week 1") {
			t.Fatalf("prompt = %s", user)
		}
		return `{"kind":"schedule","run":0,"instruction":"做一份简报","ack":"改到九点。","pace":"schedule","schedule":{"name":"brief","mode":"create","hour":9,"minute":0}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.Hour == nil || *got.Schedule.Hour != 9 || got.Schedule.Minute == nil || *got.Schedule.Minute != 0 {
		t.Fatalf("%+v %v", got, err)
	}
	if got.Schedule.DayOfWeek == nil || *got.Schedule.DayOfWeek != 1 || got.Schedule.IntervalMinutes != 0 {
		t.Fatalf("weekday = %+v", got.Schedule)
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, in, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"instruction":"做一份简报","ack":"改到九点。","pace":"schedule","schedule":{"name":"brief","mode":"create","hour":9}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.Hour == nil || *got.Schedule.Hour != 9 || got.Schedule.Minute == nil || *got.Schedule.Minute != 30 {
		t.Fatalf("omitted minute = %+v %v", got, err)
	}
	if got.Schedule.DayOfWeek == nil || *got.Schedule.DayOfWeek != 1 {
		t.Fatalf("omitted minute dropped the weekday: %+v", got.Schedule)
	}

	every := BotIntakeInput{
		BotID:   "bot_p",
		Content: "改成只报北京",
		Timers:  []BotIntakeTimer{{Name: "weather_report", Action: "报告天气", IntervalMinutes: 5}},
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, every, func(context.Context, string, string) (string, error) {
		return `{"kind":"schedule","run":0,"instruction":"每5分钟报告北京的天气","ack":"仍是每5分钟。","pace":"schedule","schedule":{"name":"weather_report","mode":"create","hour":9}}`, nil
	})
	if err != nil || got.Kind != "schedule" || got.Schedule == nil || got.Schedule.IntervalMinutes != 5 || got.Schedule.Hour != nil || got.Schedule.Minute != nil {
		t.Fatalf("a partial clock converted the interval: %+v %v", got, err)
	}
}

func TestIntakeSpecAsksUntilTheWorkIsSettled(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()

	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(_ context.Context, system, user string) (string, error) {
		if !strings.Contains(system, "规划模式") || !strings.Contains(system, "spec already open") {
			t.Fatalf("prompt does not describe hidden specifying:\n%s", system)
		}
		if strings.Contains(user, "spec already open") {
			t.Fatal("the first message has no spec yet")
		}
		return `{"kind":"new","run":0,"spec":true,"phase":"plan","instruction":"周报，收件人未定","ack":"我们进入规划模式，发给谁？","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || !got.Spec || got.Instruction != "周报，收件人未定" {
		t.Fatalf("open spec = %+v %v", got, err)
	}
	if strings.Contains(got.Ack, "规划") || strings.Contains(strings.ToLower(got.Ack), "plan mode") || !strings.Contains(got.Ack, "发给谁") {
		t.Fatalf("ack named a mode: %q", got.Ack)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "周报，收件人未定" {
		t.Fatal("spec was not kept")
	}

	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, "spec already open") || !strings.Contains(user, "周报，收件人未定") {
			t.Fatalf("follow-up lost the spec:\n%s", user)
		}
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"把表格做成周报发给老王","ack":"我做成周报发给老王。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || got.Instruction != "把表格做成周报发给老王" {
		t.Fatalf("settled = %+v %v", got, err)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("settled spec was kept")
	}
}

func TestIntakeSpecTimeoutDoesNotStartTheJob(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "周报，收件人未定")
	prev := BotIntakeBudget
	BotIntakeBudget = 40 * time.Millisecond
	defer func() { BotIntakeBudget = prev }()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		return `{"kind":"new","run":0,"phase":"execute","instruction":"发给老王","ack":"开始","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Reason != "timeout" {
		t.Fatalf("timeout = %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "周报，收件人未定") || !strings.Contains(stored, "这次补充：发给老王") || got.Instruction != stored {
		t.Fatalf("timeout dropped the sentence: ack=%q stored=%q", got.Instruction, stored)
	}
}

func TestRefineOpenSpecWritesTheCardAndDropsWhenTheTurnMoves(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	gen := desktopbot.NoteSpec("t", "u", "bot", "周报，收件人未定")
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, "周报，收件人未定", "做一份周报", nil, false, func(_ context.Context, system, user string) (string, error) {
		if !strings.Contains(system, "完成标准") || !strings.Contains(user, "周报，收件人未定") {
			t.Fatalf("refine prompt missing the draft:\n%s\n%s", system, user)
		}
		return `{"instruction":"进入规划模式\n目标：一份周报\n约束：不另发平台\n完成标准：指定的人收到文件\n还缺：发给谁"}`, nil
	})
	card := desktopbot.SpecOf("t", "u", "bot")
	if !strings.Contains(card, "完成标准") || !strings.Contains(card, "\n") || strings.Contains(card, "规划") {
		t.Fatalf("card = %q", card)
	}
	desktopbot.NoteSpec("t", "u", "bot", "周报发给老王")
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, "周报，收件人未定", "做一份周报", nil, false, func(context.Context, string, string) (string, error) {
		t.Fatal("a moved turn was refined again")
		return "", nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != "周报发给老王" {
		t.Fatalf("newer spec = %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

// A bot created without a computer gets the capability clause, so a
// rewritten instruction or spec cannot order a desktop it cannot reach.
func TestIntakeNoDesktopBotGetsTheSurfaceClause(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	var system string
	classify := func(_ context.Context, s, _ string) (string, error) {
		system = s
		return `{"kind":"new","instruction":"查北京天气","ack":"我去查。","pace":"direct"}`, nil
	}
	in := BotIntakeInput{BotID: "bot_p", Content: "北京天气", NoDesktop: true}
	if _, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, in, classify); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "has no computer and no desktop") {
		t.Fatalf("no-desktop prompt lacks the clause: %s", system)
	}
	system = ""
	in.NoDesktop = false
	if _, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, in, classify); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(system, "has no computer and no desktop") {
		t.Fatalf("desktop prompt picked up the clause: %s", system)
	}
}

// The instance record is authoritative: even when the request body forgets
// no_desktop, a bot marked hub_bot_desktop=none still gets the clause.
func TestIntakeNoDesktopInstanceMarkBeatsTheRequest(t *testing.T) {
	svc, p, _ := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	inst, err := svc.CreateInstance(context.Background(), p, CreateInstanceInput{
		Name:     "NoDesktop",
		Metadata: map[string]string{"hub_bot_desktop": "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var system string
	if _, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: "北京天气"},
		func(_ context.Context, s, _ string) (string, error) {
			system = s
			return `{"kind":"new","instruction":"查北京天气","ack":"我去查。","pace":"direct"}`, nil
		}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(system, "has no computer and no desktop") {
		t.Fatalf("marked bot lost the clause: %s", system)
	}
}

func TestRefineOpenSpecNoDesktopCarriesTheSameClause(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	gen := desktopbot.NoteSpec("t", "u", "bot", "周报，收件人未定")
	var system string
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, "周报，收件人未定", "做一份周报", nil, true, func(_ context.Context, s, _ string) (string, error) {
		system = s
		return `{"instruction":"目标：一份周报\n完成标准：指定的人收到文件\n还缺：发给谁"}`, nil
	})
	if !strings.Contains(system, "has no computer and no desktop") {
		t.Fatalf("no-desktop refine lacks the clause: %s", system)
	}
}

func TestSettledRunCarriesTheBackgroundSpec(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n约束：不另发平台\n完成标准：老王收到文件\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"发给老王","ack":"我做成周报发给老王。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" {
		t.Fatalf("settled = %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "完成标准") || !strings.Contains(got.Instruction, "发给老王") || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("instruction = %q", got.Instruction)
	}
	if !strings.Contains(got.Instruction, specDoneCheckPrefix) {
		t.Fatalf("done check missing: %q", got.Instruction)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("the spec stayed open after it was sent")
	}
}

func TestFollowUpKeepsTheBackgroundCard(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	const card = "目标：一份周报\n约束：不另发平台\n完成标准：老王收到文件\n还缺：发给谁"
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", card)
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "先做成 PDF"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问形式","ack":"做成哪种文件？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "做成哪种文件？" {
		t.Fatalf("follow-up = %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "完成标准") || !strings.Contains(stored, "不另发平台") || !strings.Contains(stored, "先做成 PDF") {
		t.Fatalf("stored = %q", stored)
	}
	gen, draft := SpecDraftForRefine(p.TenantID, p.UserID, "bot_p", got.Instruction)
	if gen == 0 || !strings.Contains(draft, "完成标准") || !strings.Contains(draft, "先做成 PDF") {
		t.Fatalf("refine draft = %q gen %d", draft, gen)
	}
}

func TestOpenGapHoldsTheRun(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"做一份周报","ack":"我开始做周报。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("held = %+v %v", got, err)
	}
	if strings.Contains(got.Ack, "规划") {
		t.Fatalf("ack named a mode: %q", got.Ack)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺") || !strings.Contains(stored, "完成标准") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestOpenGapQuestionIsWhatTheyHear(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "用宋体"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"还要问字体","ack":"好的","told":"目标：做一份周报\n还缺：发给谁","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "发给谁？" || got.Told != "发给谁？" {
		t.Fatalf("the open point was not the sentence they hear: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "这次补充：用宋体") || !strings.Contains(stored, "还缺：发给谁") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestOpenGapKeepsAQuestionThatAlreadyAsksIt(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问收件人","ack":"这周报要发给谁？","told":"目标：做一份周报","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "这周报要发给谁？" || got.Told != "这周报要发给谁？" {
		t.Fatalf("a question that already asks the point was replaced: %+v %v", got, err)
	}
}

func TestOpenGapOnTheFirstCardDoesNotStart(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁","ack":"我开始做。","told":"目标：做一份周报\n还缺：发给谁","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" || got.Told != "发给谁？" {
		t.Fatalf("first card started: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺：发给谁") || !strings.Contains(stored, "完成标准") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestOpenGapQuestionIsNotTheDraft(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报，收件人未定","ack":"这周报要发给谁？","told":"目标：一份周报\n还缺：发给谁","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "这周报要发给谁？" || got.Told != "这周报要发给谁？" || got.Instruction != "周报，收件人未定" {
		t.Fatalf("the draft was the sentence: %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_q", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报，收件人未定","ack":"目标：一份周报\n还缺：发给谁？","told":"目标：一份周报\n还缺：发给谁","pace":"ask"}`, nil
	})
	if err != nil || got.Ack != "发给谁？" || got.Told != "发给谁？" || strings.Contains(got.Ack, "目标") {
		t.Fatalf("a card was spoken: %+v %v", got, err)
	}
}

func TestOpenGapKeepsAQuestionFromTheOtherField(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问收件人","ack":"好的","told":"这周报要发给谁？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "这周报要发给谁？" || got.Told != "这周报要发给谁？" {
		t.Fatalf("the question in the other field was dropped: %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_q", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报，收件人未定","ack":"好的","told":"这周报要发给谁？","pace":"ask"}`, nil
	})
	if err != nil || got.Ack != "这周报要发给谁？" || got.Told != "这周报要发给谁？" || got.Instruction != "周报，收件人未定" {
		t.Fatalf("an unwritten spec kept the receipt: %+v %v", got, err)
	}
}

func TestOpenGapStartSentenceStaysWhenTheOtherFieldAsks(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"把周报发给老王","ack":"我做成周报发给老王。","told":"还有别的吗？","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || got.Ack != "我做成周报发给老王" || got.Told != "我做成周报发给老王" {
		t.Fatalf("a question replaced the start sentence: %+v %v", got, err)
	}
}

func TestOpenGapIsTheQuestion(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问收件人","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("question = %+v %v", got, err)
	}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "先做成 PDF"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问形式","ack":"做成哪种文件？","pace":"ask"}`, nil
	})
	if err != nil || got.Ack != "做成哪种文件？" {
		t.Fatalf("new fact kept its own question: %+v %v", got, err)
	}
}

func TestSupplementsStayOnOneLine(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：先做成 PDF")
	_, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "用宋体"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"还要问字体","ack":"用哪种字体？","pace":"ask"}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if strings.Count(stored, "这次补充：") != 1 || !strings.Contains(stored, "先做成 PDF") || !strings.Contains(stored, "用宋体") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestClosedSpecStartsWhenTheModelKeepsAsking(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n完成标准：老王收到文件\n还缺：无")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "好"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Ack, "？") {
		t.Fatalf("closed spec stayed a question: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "完成标准") || !strings.Contains(got.Instruction, specDoneCheckPrefix) || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("instruction = %q", got.Instruction)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("the settled spec stayed open")
	}
}

func TestClosedSpecStartsWhenTheMarkIsAQuestion(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n完成标准：老王收到文件\n还缺：无？")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "好"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Ack, "？") {
		t.Fatalf("a closed mark stayed a question: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "完成标准") || !strings.Contains(got.Instruction, specDoneCheckPrefix) || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("instruction = %q", got.Instruction)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("the settled spec stayed open")
	}
}

func TestNewGapOnAClosedSpecStaysAQuestion(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n完成标准：老王收到文件\n还缺：无")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "先等一下"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"目标：一份周报\n完成标准：老王收到文件\n还缺：用什么格式","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "用什么格式？" {
		t.Fatalf("new gap started the run: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺：用什么格式") || !strings.Contains(stored, "完成标准") || strings.Contains(stored, "还缺：无") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestAnsweredGapStartsEvenOnAPlanPhase(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"发给老王","ack":"发给老王。","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("answered gap stayed a question: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "发给老王") || !strings.Contains(got.Instruction, specDoneCheckPrefix) {
		t.Fatalf("instruction = %q", got.Instruction)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("the settled spec stayed open")
	}
}

func TestDelegatedAnswerClosesTheGap(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：图文综述发到 CSDN\n完成标准：帖子发布成功\n还缺：篇幅与目标读者；发布方式")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "你自己决定长度，公开给网友，直接发布。"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"目标：图文综述发到 CSDN\n完成标准：帖子发布成功\n还缺：篇幅与目标读者；发布方式","ack":"综述的篇幅与目标读者未定？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" {
		t.Fatalf("delegated answer stayed a question: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "你自己决定长度") {
		t.Fatalf("the delegation did not reach the run order: %q", got.Instruction)
	}
	if desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p") != "" {
		t.Fatal("the settled spec stayed open")
	}
}
func TestPlanOrderThatStillHasAGapDoesNotStart(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：发给老王","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" {
		t.Fatalf("open point in the order started the run: %+v %v", got, err)
	}
	if !strings.Contains(desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p"), "还缺：发给谁") {
		t.Fatal("the open point was cleared")
	}
}

func TestRepeatedGoalWithPunctuationDoesNotStart(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报。"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"做一份周报。","ack":"我开始做周报。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("punctuated goal started the run: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺：发给谁") || strings.Contains(stored, "这次补充") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestRefineKeepsTheDoneCheck(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：老王收到文件\n还缺：发给谁"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "随便", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"周报"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite dropped the card: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestUnrelatedLineDoesNotCloseTheGap(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "用宋体"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"做一份周报","ack":"我开始做周报。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("unrelated line started the run: %+v %v", got, err)
	}
	if !strings.Contains(desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p"), "还缺：发给谁") {
		t.Fatal("the open point was cleared")
	}
}

func TestAnswerKeptOnTheSupplementStillCloses(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：发给老王")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"发给老王","ack":"发给老王。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("answer already on the card did not start: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "发给老王") || !strings.Contains(got.Instruction, specDoneCheckPrefix) {
		t.Fatalf("instruction = %q", got.Instruction)
	}
}

func TestAnswerInsideTheGoalStillCloses(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：把周报发给老王\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"发给老王","ack":"发给老王。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Instruction, "还缺") {
		t.Fatalf("answer inside the goal did not start: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "发给老王") {
		t.Fatalf("instruction = %q", got.Instruction)
	}
}

func TestAnswerInsideTheGoalKeepsTheQuestion(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：把周报发给老王\n完成标准：文件送到指定的人\n还缺：用什么格式")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问格式","ack":"做成 PDF 可以吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "做成 PDF 可以吗？" {
		t.Fatalf("a fact already in the goal replaced the question: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺：用什么格式") || strings.Contains(stored, "这次补充") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestAnswerInsideTheOpenPointStaysAQuestion(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李","ack":"做成 PDF 可以吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "做成 PDF 可以吗？" {
		t.Fatalf("a choice inside the open point replaced the question: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "这次补充：发给老王") || !strings.Contains(stored, "还缺：发给老王还是老李") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestOpenGapAsksAgainWhenTheSupplementIsRepeated(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：先做成 PDF")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "先做成 PDF"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"ask","run":0,"spec":true,"phase":"plan","instruction":"周报还要问收件人","ack":"还有别的吗？","pace":"ask"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Ack != "发给谁？" {
		t.Fatalf("a repeated supplement kept a new question: %+v %v", got, err)
	}
}

func TestAnswerInsideTheOpenPointStillCloses(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给老王"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"发给老王","ack":"发给老王。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || got.Phase != "execute" || strings.Contains(got.Instruction, "还缺") || strings.Contains(got.Instruction, "老李") {
		t.Fatalf("answer inside the open point did not start cleanly: %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "发给老王") || !strings.Contains(got.Instruction, specDoneCheckPrefix) {
		t.Fatalf("instruction = %q", got.Instruction)
	}
}

func TestRepeatingTheOpenPointDoesNotStart(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "发给谁。"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"发给谁。","ack":"我开始做周报。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("the open point started the run: %+v %v", got, err)
	}
	stored := desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p")
	if !strings.Contains(stored, "还缺：发给谁") || strings.Contains(stored, "这次补充") {
		t.Fatalf("stored = %q", stored)
	}
}

func TestRefineLeavesTheAnswerOnTheSupplement(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给谁"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：把周报发给老王\n完成标准：文件送到指定的人\n还缺：发给谁"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite moved the answer into the goal: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：发给老王"}`, nil
	})
	if !strings.Contains(desktopbot.SpecOf("t", "u", "bot"), "这次补充：发给老王") || !strings.Contains(desktopbot.SpecOf("t", "u", "bot"), "还缺：发给谁") {
		t.Fatalf("supplement was not kept: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestRefineKeepsTheChoiceInsideTheOpenPoint(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：把周报发给老王\n完成标准：文件送到指定的人\n还缺：发给老王还是老李"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite lifted the choice into the goal: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李\n这次补充：发给老王"}`, nil
	})
	if !strings.Contains(desktopbot.SpecOf("t", "u", "bot"), "这次补充：发给老王") || !strings.Contains(desktopbot.SpecOf("t", "u", "bot"), "还缺：发给老王还是老李") {
		t.Fatalf("choice was not kept on the supplement: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestRefineKeepsTheChoiceWithPunctuation(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给老王还是老李"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王。", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：把周报发给老王\n完成标准：文件送到指定的人\n还缺：发给老王还是老李"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite lifted the punctuated choice into the goal: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestRefineKeepsTheWrittenFacts(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n约束：不另发平台\n完成标准：老王收到文件\n还缺：发给谁"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	keep := func(rewritten string) {
		t.Helper()
		body, err := json.Marshal(map[string]string{"instruction": rewritten})
		if err != nil {
			t.Fatal(err)
		}
		RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "随便", nil, false, func(context.Context, string, string) (string, error) {
			return string(body), nil
		})
		if desktopbot.SpecOf("t", "u", "bot") != card {
			t.Fatalf("rewrite replaced a written fact: %q", desktopbot.SpecOf("t", "u", "bot"))
		}
	}
	keep("目标：一份月报\n约束：不另发平台\n完成标准：老王收到文件\n还缺：发给谁")
	keep("目标：一份周报\n约束：不另发平台\n完成标准：老王收到文件\n还缺：用什么格式")
	keep("目标：一份周报\n完成标准：老王收到文件\n还缺：发给谁")
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "随便", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报，限一页\n约束：不另发平台\n完成标准：老王收到文件\n还缺：发给谁"}`, nil
	})
	got := desktopbot.SpecOf("t", "u", "bot")
	if !strings.Contains(got, "一份周报") || !strings.Contains(got, "限一页") || !strings.Contains(got, "不另发平台") || !strings.Contains(got, "还缺：发给谁") {
		t.Fatalf("a longer line was dropped: %q", got)
	}
	const ascii = "目标：一份周报\n完成标准：老王收到文件\n还缺：发给谁\n这次补充:发给老王"
	gen = desktopbot.NoteSpec("t", "u", "bot", ascii)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, ascii, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报\n完成标准：老王收到文件\n还缺：发给谁"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != ascii {
		t.Fatalf("ascii supplement was dropped: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestRefineKeepsTheOpenPoint(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：老王收到文件\n还缺：发给谁"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "一份周报", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报\n完成标准：老王收到文件"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite cleared the open point: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestRefineKeepsTheSupplementLine(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	const card = "目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：发给老王"
	gen := desktopbot.NoteSpec("t", "u", "bot", card)
	RefineOpenSpec(context.Background(), "t", "u", "bot", gen, card, "发给老王", nil, false, func(context.Context, string, string) (string, error) {
		return `{"instruction":"目标：一份周报\n完成标准：文件送到指定的人\n还缺：发给谁"}`, nil
	})
	if desktopbot.SpecOf("t", "u", "bot") != card {
		t.Fatalf("rewrite dropped the supplement: %q", desktopbot.SpecOf("t", "u", "bot"))
	}
}

func TestEarlierSupplementDoesNotCloseTheGap(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁\n这次补充：先做成 PDF")
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"spec":true,"phase":"execute","instruction":"做一份周报","ack":"我开始做周报。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "ask" || got.Phase != "plan" || got.Ack != "发给谁？" {
		t.Fatalf("supplement closed the gap: %+v %v", got, err)
	}
	if !strings.Contains(desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p"), "还缺") {
		t.Fatal("the open point was dropped")
	}
}

func TestTimeoutAsksTheOpenGap(t *testing.T) {
	desktopbot.ResetSpecMemory()
	t.Cleanup(desktopbot.ResetSpecMemory)
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	gen := desktopbot.NoteSpec(p.TenantID, p.UserID, "bot_p", "目标：做一份周报\n完成标准：文件送到指定的人\n还缺：发给谁")
	prev := BotIntakeBudget
	BotIntakeBudget = 40 * time.Millisecond
	defer func() { BotIntakeBudget = prev }()
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "做一份周报"}, func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		return "", nil
	})
	if err != nil || got.Kind != "ask" || got.Reason != "timeout" || got.Ack != "发给谁？" {
		t.Fatalf("timeout = %+v %v", got, err)
	}
	if desktopbot.SpecGen(p.TenantID, p.UserID, "bot_p") == gen || desktopbot.ReplaceSpec(p.TenantID, p.UserID, "bot_p", gen, "过期的规范") {
		t.Fatal("a background write from before the timeout can still land")
	}
	if !strings.Contains(desktopbot.SpecOf(p.TenantID, p.UserID, "bot_p"), "还缺：发给谁") {
		t.Fatal("timeout replaced the spec")
	}
}
