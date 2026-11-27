package agentservice

// Regression: a self-contained execution order that the LLM still labels
// spec:true must run when the spec carries no real open point (还缺). The
// spec premise is "a detail that would change the result is missing". When
// neither the written card nor this turn's order names a missing fact, asking
// would settle an explicit go-ahead command to a chat bubble with no run.

import (
	"context"
	"strings"
	"testing"
)

const probePostOrder = "将这篇文章改成小红书适合的形式，发在小红书上。"

// Loose spec: the model marks spec:true but writes no structured card and no
// 还缺 anywhere. Nothing is actually missing, so the order must run.
func TestSpecIntakeProbe_XiaohongshuLooseInstructionRuns(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	classify := func(ctx context.Context, system, user string) (string, error) {
		return `{"kind":"ask","spec":true,"phase":"plan","ack":"收到","told":"","instruction":"将这篇文章改成小红书适合的形式发在小红书上","pace":"detailed","confidence":0.9,"run":0,"reason":"post"}`, nil
	}
	out, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: probePostOrder}, classify)
	if err != nil {
		t.Fatalf("IntakeBotMessage: %v", err)
	}
	if out.Kind != "new" || out.Phase != "execute" {
		t.Fatalf("gapless spec order must run: kind=%q phase=%q instruction=%q", out.Kind, out.Phase, out.Instruction)
	}
	if strings.TrimSpace(out.Instruction) == "" {
		t.Fatal("run instruction must carry the order")
	}
}

// A written card whose only 还缺 is closed (无/empty) also names nothing to
// ask, so the order runs rather than settling.
func TestSpecIntakeProbe_WrittenCardClosedGapRuns(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	classify := func(ctx context.Context, system, user string) (string, error) {
		return `{"kind":"ask","spec":true,"phase":"plan","ack":"收到","told":"","instruction":"目标：将文章改成小红书适合的形式\n完成标准：发布到小红书\n还缺：无","pace":"detailed","confidence":0.9,"run":0,"reason":"post"}`, nil
	}
	out, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: probePostOrder}, classify)
	if err != nil {
		t.Fatalf("IntakeBotMessage: %v", err)
	}
	if out.Kind != "new" || out.Phase != "execute" {
		t.Fatalf("closed-gap spec must run: kind=%q phase=%q", out.Kind, out.Phase)
	}
}

// A written card with a REAL open 还缺 still asks: the model named a missing
// fact only the person knows, and the spec flow must wait for it. This locks
// the boundary so the fix only rescues gapless specs, not genuine questions.
func TestSpecIntakeProbe_WrittenCardRealGapStillAsks(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	classify := func(ctx context.Context, system, user string) (string, error) {
		return `{"kind":"ask","spec":true,"phase":"plan","ack":"还差发布账号","told":"","instruction":"目标：将文章改成小红书适合的形式\n完成标准：发布到小红书\n还缺：发布账号","pace":"detailed","confidence":0.9,"run":0,"reason":"post"}`, nil
	}
	out, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: probePostOrder}, classify)
	if err != nil {
		t.Fatalf("IntakeBotMessage: %v", err)
	}
	if out.Kind != "ask" || out.Phase != "plan" {
		t.Fatalf("real open gap must still ask: kind=%q phase=%q", out.Kind, out.Phase)
	}
	if !strings.Contains(out.Ack, "发布账号") && !strings.Contains(out.Ack, "？") {
		t.Fatalf("ask should carry the open point, got %q", out.Ack)
	}
}

// No written card, but the model's ack is itself a question it is waiting on
// (it asks for a fact only the person knows). The gapless-spec rescue must not
// fire: the turn stays an honest ask — HTTP 200, no run_id, no pretend start.
func TestSpecIntakeProbe_LooseSpecAckQuestionStillAsks(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	classify := func(ctx context.Context, system, user string) (string, error) {
		return `{"kind":"ask","spec":true,"phase":"plan","ack":"要发到哪个小红书账号？","told":"","instruction":"将这篇文章改成小红书适合的形式发在小红书上","pace":"detailed","confidence":0.9,"run":0,"reason":"post"}`, nil
	}
	out, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: probePostOrder}, classify)
	if err != nil {
		t.Fatalf("IntakeBotMessage: %v", err)
	}
	if out.Kind != "ask" || out.Phase != "plan" {
		t.Fatalf("a real follow-up question must stay an ask: kind=%q phase=%q", out.Kind, out.Phase)
	}
	if out.RunID != "" {
		t.Fatalf("an honest ask carries no run_id, got %q", out.RunID)
	}
	if !strings.Contains(out.Ack, "？") {
		t.Fatalf("the question the model asked must reach the person, got %q", out.Ack)
	}
}

// A settled spec (phase=execute, kind=new) is already an ordinary new task.
// preferOpenSpecCard must not demote it to an ask just because the ack carries
// a question mark — the model already judged this a go-ahead. The gapless
// rescue stays out of the way and the run stands.
func TestSpecIntakeProbe_SettledSpecStaysRunEvenWithQuestionAck(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	classify := func(ctx context.Context, system, user string) (string, error) {
		return `{"kind":"new","spec":true,"phase":"execute","ack":"要我直接发吗？","told":"","instruction":"将这篇文章改成小红书适合的形式发在小红书上","pace":"direct","confidence":0.9,"run":0,"reason":"post"}`, nil
	}
	out, err := svc.IntakeBotMessage(context.Background(), p, inst.ID,
		BotIntakeInput{BotID: "bot_p", Content: probePostOrder}, classify)
	if err != nil {
		t.Fatalf("IntakeBotMessage: %v", err)
	}
	if out.Kind != "new" || out.Phase != "execute" {
		t.Fatalf("a settled spec the model marked execute must run: kind=%q phase=%q", out.Kind, out.Phase)
	}
}
