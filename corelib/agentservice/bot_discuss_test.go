package agentservice

import (
	"context"
	"strings"
	"testing"
)

func TestDiscussionIsStoredAndDoesNotStartARun(t *testing.T) {
	exec := &captureExecutor{}
	svc, p, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()
	constraints := []string{"用现有的图"}
	open := []string{}
	first, err := svc.RecordBotDiscussion(context.Background(), p, inst.ID, BotDiscussInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "turn-1",
		UserText:         "把报表发到站上",
		AssistantText:    "先把计划对齐。",
		Plan:             &BotIntakePlan{Goal: "把报表发到站上", Constraints: &constraints, Open: &open},
	})
	if err != nil || first.State != "planned" || first.Plan.Goal != "把报表发到站上" {
		t.Fatalf("first = %+v %v", first, err)
	}
	if exec.req.Session.ID != "" {
		t.Fatalf("discussion started a run in %s", exec.req.Session.ID)
	}
	again, err := svc.RecordBotDiscussion(context.Background(), p, inst.ID, BotDiscussInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "turn-1",
		UserText:         "把报表发到站上",
		AssistantText:    "再写一遍",
	})
	if err != nil || len(again.Excerpts) != 2 {
		t.Fatalf("replay = %+v %v", again, err)
	}
	success := []string{"文章在列表里"}
	amended, err := svc.RecordBotDiscussion(context.Background(), p, inst.ID, BotDiscussInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "turn-2",
		UserText:         "标题用上周的数字",
		AssistantText:    "补上了。",
		Plan:             &BotIntakePlan{Success: &success},
	})
	if err != nil || amended.Plan.Goal != "把报表发到站上" || amended.Plan.Success == nil || (*amended.Plan.Success)[0] != "文章在列表里" {
		t.Fatalf("amend = %+v %v", amended, err)
	}
	text := amended.ExecutionText()
	if !strings.Contains(text, "目标：把报表发到站上") || !strings.Contains(text, "用现有的图") || !strings.Contains(text, "人: 把报表发到站上") || strings.Contains(text, "开始") {
		t.Fatalf("instruction = %s", text)
	}
	if err := svc.MarkBotDiscussionExecuting(context.Background(), p, inst.ID, "bot_p"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReadBotDiscussion(context.Background(), p, inst.ID, "bot_p")
	if err != nil || got.State != "executing" || got.Plan.Goal != "把报表发到站上" {
		t.Fatalf("read = %+v %v", got, err)
	}
}

func TestIntakeEarlierReachesTheClassifier(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	_, err := svc.IntakeBotMessage(context.Background(), p, inst.ID, BotIntakeInput{
		BotID: "bot_p", Content: "标题短一点", Persona: "值班",
		Earlier: []string{"人: 把报表发到站上"},
	}, func(_ context.Context, _, user string) (string, error) {
		if !strings.Contains(user, "人: 把报表发到站上") || !strings.Contains(user, "值班") {
			t.Fatalf("prompt = %s", user)
		}
		return `{"kind":"plan_amend","run":0,"ack":"标题改短。","pace":"discuss","plan":{"goal":"把报表发到站上"}}`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
