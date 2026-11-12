package guiapp

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The first reply to a new job is written by the model as a working partner
// who says back what it understood. It is not a stock line, a keyword
// template, or a copy of the message.
func TestDesktopBotFirstReplyPromptsAskForPartnerUnderstanding(t *testing.T) {
	for name, system := range map[string]string{"understand": desktopBotUnderstandSystem, "ack": desktopBotAckSystem} {
		if !strings.Contains(system, "working partner") || !strings.Contains(system, "show you understood it") {
			t.Fatalf("%s prompt does not ask for a partner's understanding", name)
		}
		if !strings.Contains(system, "do not claim any part is done") && !strings.Contains(system, "Do not claim any part is done") {
			t.Fatalf("%s prompt allows claiming work already done", name)
		}
		if !strings.Contains(system, "do not add goals") {
			t.Fatalf("%s prompt allows invented goals", name)
		}
	}
	if strings.Contains(desktopBotUnderstandSystem, "For direct, leave told empty") {
		t.Fatal("a direct reading still has no first reply")
	}
	if strings.Contains(desktopBotAckSystem, "do not say you are on it or restate the goal") {
		t.Fatal("the accept line is still told not to say back the job")
	}
	// No paper- or site-specific rule decides the sentence.
	if strings.Contains(desktopBotUnderstandSystem, "dailypapers 热门") || strings.Contains(desktopBotAckSystem, "dailypapers") {
		t.Fatal("first-reply prompt carries a task-specific template")
	}
}

func TestDesktopBotDirectReadingKeepsTheModelsUnderstanding(t *testing.T) {
	cases := []struct {
		name, text, told, order string
		wantTold                string
	}{
		{
			name:     "paper to csdn",
			text:     "搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn",
			told:     "我去 Hugging Face Daily Papers 挑一篇正热的论文，读完做成配图的摘要，再发到你的 CSDN，发好把链接给你。",
			order:    "目标：从 huggingface.co/papers 选一篇热门论文，写图文摘要并发布到 CSDN。",
			wantTold: "我去 Hugging Face Daily Papers 挑一篇正热的论文，读完做成配图的摘要，再发到你的 CSDN，发好把链接给你。",
		},
		{
			name:     "generic non-paper job",
			text:     "把桌面上的销售表整理成周报发给老王",
			told:     "我把销售表里的数据理成一份周报，整理好发给老王，发出去跟你说。",
			order:    "目标：用桌面上的销售表整理一份周报并发送给老王。",
			wantTold: "我把销售表里的数据理成一份周报，整理好发给老王，发出去跟你说。",
		},
		{
			// A copy of the message is not understanding; the GUI then asks
			// the ack writer for a line instead of showing the copy.
			name:     "parrot dropped",
			text:     "搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn",
			told:     "搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn。",
			order:    "目标：发布论文摘要。",
			wantTold: "",
		},
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	for _, c := range cases {
		desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
			return `{"told":"` + c.told + `","instruction":"` + c.order + `","pace":"direct","persona":""}`, nil
		}
		got, err := (&App{}).understandDesktopBotTask(c.text, "plan", "zh-Hans", nil, "", "")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Pace != "direct" || got.Told != c.wantTold || got.Instruction != c.order {
			t.Fatalf("%s: got %#v", c.name, got)
		}
	}
}

// A follow-up while a job runs is read with the earlier messages, and the
// ack writer is told which job is running, so the line names that job.
func TestDesktopBotFollowUpCarriesTheRunningJob(t *testing.T) {
	var user string
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		user = req.userPrompt
		return `{"told":"我会把配图做成三张，摘要发到 CSDN 后告诉你。","instruction":"目标：同上，配图三张。","pace":"direct","persona":""}`, nil
	}
	t.Cleanup(func() { desktopBotUnderstandLLM = callDesktopBotUnderstandLLM })
	earlier := []string{"搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn"}
	if _, err := (&App{}).understandDesktopBotTask("配图要三张", "plan", "zh-Hans", earlier, "", ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(user, earlier[0]) || !strings.Contains(user, "配图要三张") {
		t.Fatalf("follow-up read without the earlier job:\n%s", user)
	}
	if !strings.Contains(desktopBotUnderstandSystem, "stays on that job: it is not a new job") {
		t.Fatal("a change to the running job may be read as a new job")
	}
	prompt := desktopBotAckUser(DesktopBotAckRequest{Kind: "supplement", Message: "配图要三张", Lang: "zh-Hans", RunningTask: "热门论文图文摘要发 CSDN"})
	if !strings.Contains(prompt, "job already running: 热门论文图文摘要发 CSDN") {
		t.Fatalf("supplement line does not know its job:\n%s", prompt)
	}
}

// A model that never answers cannot hold the first reply open. This runs on
// the production default, not a test-only budget: every attempt together
// stops at desktopBotFirstReplyBudget (a few seconds), and no attempt asks
// the client for more than what is left.
func TestWriteDesktopBotAckFirstReplyIsBounded(t *testing.T) {
	if desktopBotFirstReplyBudget <= 0 || desktopBotFirstReplyBudget > 5*time.Second {
		t.Fatalf("first reply default budget %s is not immediate feedback", desktopBotFirstReplyBudget)
	}
	prev := desktopBotUnderstandLLM
	t.Cleanup(func() { desktopBotUnderstandLLM = prev })
	var calls int32
	var maxTimeout int32
	desktopBotUnderstandLLM = func(ctx context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		atomic.AddInt32(&calls, 1)
		if int32(req.timeoutSec) > atomic.LoadInt32(&maxTimeout) {
			atomic.StoreInt32(&maxTimeout, int32(req.timeoutSec))
		}
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	line, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: "把销售表整理成周报", Lang: "zh-Hans", Goal: "整理周报"})
	if err == nil || line != "" {
		t.Fatalf("a hung model produced a line: %q %v", line, err)
	}
	if elapsed := time.Since(start); elapsed > desktopBotFirstReplyBudget+500*time.Millisecond {
		t.Fatalf("first reply waited %s past its %s budget", elapsed, desktopBotFirstReplyBudget)
	}
	if n := atomic.LoadInt32(&calls); n < 1 || n > int32(len(desktopBotAckAttempts)) {
		t.Fatalf("attempts = %d", n)
	}
	if got := time.Duration(atomic.LoadInt32(&maxTimeout)) * time.Second; got > desktopBotFirstReplyBudget {
		t.Fatalf("an attempt asked for %s, more than the %s budget", got, desktopBotFirstReplyBudget)
	}
	// Other kinds keep their own per-attempt timeouts only.
	if desktopBotFirstReplyKind("stopping") || !desktopBotFirstReplyKind("accept") {
		t.Fatal("first-reply budget applies to the wrong kinds")
	}
}

// The reading already spent part of the end-to-end budget. The GUI sends what
// is left, and the writer stops there instead of starting a fresh budget.
func TestWriteDesktopBotAckHonoursTheRemainingBudget(t *testing.T) {
	prev := desktopBotUnderstandLLM
	t.Cleanup(func() { desktopBotUnderstandLLM = prev })
	desktopBotUnderstandLLM = func(ctx context.Context, _ *App, _ desktopBotUnderstandRequest) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	start := time.Now()
	if _, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: "整理周报", Lang: "zh-Hans", BudgetMs: 300}); err == nil {
		t.Fatal("hung model returned no error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("remaining 300ms budget waited %s", elapsed)
	}
	if got := desktopBotFirstReplyBudgetFor(DesktopBotAckRequest{Kind: "accept", BudgetMs: 60_000}); got != desktopBotFirstReplyBudget {
		t.Fatalf("a larger remainder must not extend the default: %s", got)
	}
	if got := desktopBotFirstReplyBudgetFor(DesktopBotAckRequest{Kind: "stopping", BudgetMs: 300}); got != 0 {
		t.Fatalf("non-first-reply kinds have no first-reply budget: %s", got)
	}
}

// One message gets one written line: the writer answers once and the
// result is the model's own sentence, not a stock line.
func TestWriteDesktopBotAckAcceptUsesThePartnerLine(t *testing.T) {
	prev := desktopBotUnderstandLLM
	t.Cleanup(func() { desktopBotUnderstandLLM = prev })
	var calls int32
	var mu sync.Mutex
	var system, user string
	desktopBotUnderstandLLM = func(_ context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		atomic.AddInt32(&calls, 1)
		mu.Lock()
		system, user = req.systemPrompt, req.userPrompt
		mu.Unlock()
		return "我去挑一篇正热的论文，做成图文摘要发到 CSDN，发好告诉你。", nil
	}
	line, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: "搜索一篇dailypapers中的热门论文，做图文摘要后发到csdn", Lang: "zh-Hans", Goal: "热门论文图文摘要发CSDN", Instruction: "目标：从 huggingface.co/papers 选一篇热门论文，写图文摘要并发布到 CSDN。"})
	mu.Lock()
	defer mu.Unlock()
	// The first reply races the models it can reach (own, fast, Hub); one
	// line comes back whichever answered first.
	if n := atomic.LoadInt32(&calls); err != nil || line != "我去挑一篇正热的论文，做成图文摘要发到 CSDN，发好告诉你。" || n < 1 || n > 3 {
		t.Fatalf("line %q err %v calls %d", line, err, calls)
	}
	if !strings.Contains(system, "working partner") || !strings.Contains(user, "goal: 热门论文图文摘要发CSDN") {
		t.Fatalf("accept prompt lost the partner role or the goal:\n%s", user)
	}
}
