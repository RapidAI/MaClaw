package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBot4FollowUpResolvesToTheOriginalDrinkOrder(t *testing.T) {
	executor := &captureExecutor{}
	svc, p, inst := setupCaptureAgentService(t, executor)
	defer svc.Close()
	const (
		original = "我帮去京东下单买一件饮料，默认地址发货"
		explain  = "是别的任务，我说明一下"
		follow   = "京东下单啊"
	)
	session := "bot_4"
	if err := svc.NoteBotTask(context.Background(), p, inst.ID, session, BotContextTask{
		RunID: "run_jd", Instruction: original, Status: "cancelled", Progress: "已打开商品页",
	}); err != nil {
		t.Fatal(err)
	}
	explainOut, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: session, ClientSessionKey: session, Content: explain,
	}, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, original) {
			t.Fatalf("explain intake lost the cancelled order:\n%s", user)
		}
		return `{"kind":"reply","run":0,"instruction":"","ack":"你说。","told":"你说。","pace":"reply"}`, nil
	})
	if err != nil || explainOut.Kind != "reply" {
		t.Fatalf("explain = %+v %v", explainOut, err)
	}
	continued, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: session, ClientSessionKey: session, Content: "继续",
	}, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, original) {
			t.Fatalf("continue intake lost earlier turns:\n%s", user)
		}
		return `{"kind":"continue","run":0,"instruction":"继续","ack":"没有可以继续的事。","pace":"direct"}`, nil
	})
	if err != nil || continued.Kind != "ask" || continued.Phase != "plan" || intakeSaysNoWork(continued.Ack) {
		t.Fatalf("continue = %+v %v", continued, err)
	}
	if !strings.Contains(continued.Instruction, "饮料") || !strings.Contains(continued.Instruction, "默认地址") || len(continued.ConfirmBefore) == 0 {
		t.Fatalf("continue instruction = %+v", continued)
	}
	again, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: session, ClientSessionKey: session, Content: follow,
	}, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, original) || !strings.Contains(user, explain) {
			t.Fatalf("follow-up intake lost the conversation:\n%s", user)
		}
		return `{"kind":"new","run":0,"instruction":"京东下单啊","ack":"好，去下单。","pace":"direct"}`, nil
	})
	goal := ""
	if again.Plan != nil {
		goal = again.Plan.Goal
	}
	if err != nil || again.Kind != "discuss" || !strings.Contains(goal, "饮料") || !strings.Contains(goal, "默认地址") || !strings.Contains(goal, "京东下单") {
		t.Fatalf("follow-up = %+v %v", again, err)
	}
	if intakeSaysNoWork(again.Ack) {
		t.Fatalf("ack disagrees with starting the order: %q", again.Ack)
	}

	snap, err := svc.ReadBotContext(context.Background(), p, inst.ID, session)
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	for i, msg := range snap.Messages {
		if i > 0 && (msg.At.Before(snap.Messages[i-1].At) || (msg.At.Equal(snap.Messages[i-1].At) && msg.ID < snap.Messages[i-1].ID)) {
			t.Fatalf("messages out of order: %+v", snap.Messages)
		}
		if msg.Role == "user" {
			seen = append(seen, msg.Text)
		}
		if msg.ID == "" {
			t.Fatal("message without an id")
		}
	}
	if !contextHas(seen, explain, "继续", follow) {
		t.Fatalf("history = %#v", seen)
	}

	restarted, err := NewService(Config{DataRoot: svc.DataRoot(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, svc.store, executor)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	againSnap, err := restarted.ReadBotContext(context.Background(), p, inst.ID, session)
	if err != nil || againSnap.Revision != snap.Revision || len(againSnap.Messages) != len(snap.Messages) {
		t.Fatalf("restart = %+v %v want revision %d messages %d", againSnap.Revision, err, snap.Revision, len(snap.Messages))
	}
	second, err := restarted.ReadBotContext(context.Background(), p, inst.ID, session)
	if err != nil || second.Revision != snap.Revision || len(second.Messages) != len(snap.Messages) {
		t.Fatalf("second client = %+v %v", second.Revision, err)
	}
}

func TestStaleAskIsWithdrawnWhenANewerMessageArrives(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	session := "bot_4"
	if err := svc.NoteBotAsk(context.Background(), p, inst.ID, session, BotContextAsk{
		ID: "ask_old", RunID: "run_jd", Question: "买几件？",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: session, ClientSessionKey: session, Content: "就一件",
	}, func(context.Context, string, string) (string, error) {
		return `{"kind":"reply","run":0,"ack":"好。","told":"好。","pace":"reply"}`, nil
	}); err != nil {
		t.Fatal(err)
	}
	snap, err := svc.ReadBotContext(context.Background(), p, inst.ID, session)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ask := range snap.Asks {
		if ask.ID != "ask_old" {
			continue
		}
		found = true
		if !ask.Withdrawn || ask.Because != "newer_message" {
			t.Fatalf("stale ask still pending: %+v", ask)
		}
	}
	if !found {
		t.Fatal("withdrawn ask was dropped from the snapshot")
	}
}

func contextHas(lines []string, parts ...string) bool {
	blob := strings.Join(lines, "\n")
	for _, part := range parts {
		if !strings.Contains(blob, part) {
			return false
		}
	}
	return true
}
