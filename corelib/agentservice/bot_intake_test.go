package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"
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
		return `{"kind":"continue","run":1,"instruction":"继续","ack":"没有可以继续的事。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "reply" || got.Instruction != "" || !intakeSaysNoWork(got.Ack) {
		t.Fatalf("continue with nothing must not start a run: %+v %v", got, err)
	}
}

func TestContinueRelaunchesACancelledRunWithItsTask(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	stopped := []BotIntakeRun{{RunID: "run_old", Task: "在京东下单", Status: "cancelled", Progress: "已打开商品页"}}
	got, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: "bot_p", Content: "继续", Earlier: []string{"在京东下单"}, Running: stopped,
	}, func(context.Context, string, string) (string, error) {
		return `{"kind":"new","run":0,"instruction":"继续","ack":"没有可以继续的事。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "new" || !strings.Contains(got.Instruction, "在京东下单") || strings.TrimSpace(got.Instruction) == "继续" {
		t.Fatalf("relaunch = %+v %v", got, err)
	}
	if !strings.Contains(got.Instruction, "已打开商品页") || intakeSaysNoWork(got.Ack) || !strings.Contains(got.Ack, "在京东下单") {
		t.Fatalf("ack and progress = %+v", got)
	}
	paused := []BotIntakeRun{{RunID: "run_hold", Task: "在京东下单", Status: "paused"}}
	got, err = svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{BotID: "bot_p", Content: "继续", Running: paused}, func(context.Context, string, string) (string, error) {
		return `{"kind":"continue","run":0,"instruction":"","ack":"没有可以继续的事。","pace":"direct"}`, nil
	})
	if err != nil || got.Kind != "continue" || got.RunID != "run_hold" || intakeSaysNoWork(got.Ack) {
		t.Fatalf("paused resume = %+v %v", got, err)
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
	if err != nil || got.Kind != "reply" || got.Instruction != "" || got.Reason != "nothing to continue" {
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
