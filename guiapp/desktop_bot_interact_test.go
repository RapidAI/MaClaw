package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Rule 1: the first reply races a fast model next to the chosen one, so a
// slow chosen model (qwen4-max-preview at 11-40 s) cannot hold it. The fast
// racer answering first is the line the person sees, inside the budget.
func TestDesktopBotFirstReplyRacesAFastModel(t *testing.T) {
	prevLLM, prevModels := desktopBotUnderstandLLM, desktopBotAckModels
	t.Cleanup(func() { desktopBotUnderstandLLM, desktopBotAckModels = prevLLM, prevModels })
	fast := corelib.MaclawLLMConfig{URL: "https://hub.example/v1", Model: "deepseek-v4-flash"}
	desktopBotAckModels = func(*App) (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig) {
		return corelib.MaclawLLMConfig{URL: "https://hub.example/v1", Model: "qwen4-max-preview"}, &fast
	}
	var mu sync.Mutex
	var fastReq desktopBotUnderstandRequest
	desktopBotUnderstandLLM = func(ctx context.Context, _ *App, req desktopBotUnderstandRequest) (string, error) {
		if req.override != nil {
			mu.Lock()
			fastReq = req
			mu.Unlock()
			return "明白：把销售表整理成周报，我先看表里有哪些列。", nil
		}
		<-ctx.Done() // the chosen model and the Hub model are slow
		return "", ctx.Err()
	}
	start := time.Now()
	line, err := (&App{}).WriteDesktopBotAck(DesktopBotAckRequest{Kind: "accept", Message: "把桌面上的销售表整理成周报", Lang: "zh-Hans", Goal: "整理周报"})
	if err != nil || !strings.HasPrefix(line, "明白：") {
		t.Fatalf("line %q err %v", line, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the fast line waited for the slow models: %s", time.Since(start))
	}
	mu.Lock()
	defer mu.Unlock()
	if fastReq.override.Model != "deepseek-v4-flash" || fastReq.maxOut <= 0 || fastReq.maxOut > 200 {
		t.Fatalf("fast racer request: model=%q maxOut=%d", fastReq.override.Model, fastReq.maxOut)
	}
	// The brief for one kind is short: only that kind's rule line.
	if strings.Contains(fastReq.systemPrompt, "kind stop_failed:") || !strings.Contains(fastReq.systemPrompt, "kind accept:") {
		t.Fatal("the first-reply brief carries every kind's rules")
	}
	if got := classifyOverride(fastReq); got.Override == nil || got.HubOnly || got.MaxOutputTokens != desktopBotAckMaxOut || !got.NoThinking {
		t.Fatalf("fast racer classify request: %+v", got)
	}
}

func classifyOverride(req desktopBotUnderstandRequest) LLMClassifyRequest {
	return desktopBotUnderstandClassifyRequest(req)
}

// The fast pick prefers a measured fast model, then a fast-looking name; it
// never picks the chosen model itself or a large reasoning model.
func TestPickDesktopBotFastAck(t *testing.T) {
	desktopBotFastAckLatency.Lock()
	desktopBotFastAckLatency.ms = map[string]float64{}
	desktopBotFastAckLatency.Unlock()
	mat := func(p corelib.MaclawLLMProvider, model string) corelib.MaclawLLMConfig {
		return corelib.MaclawLLMConfig{URL: p.URL, Model: model}
	}
	hub := corelib.MaclawLLMProvider{Name: hubServiceProviderName, IsHubService: true, URL: "https://hub/v1", Model: "auto", Models: []string{"qwen4-max-preview", "deepseek-v4-flash"}}
	nokey := corelib.MaclawLLMProvider{Name: "x", URL: "https://x/v1", Model: "gpt-4o-mini"}
	skip := corelib.MaclawLLMConfig{URL: "https://hub/v1", Model: "qwen4-max-preview"}
	got := pickDesktopBotFastAck([]corelib.MaclawLLMProvider{hub, nokey}, skip, mat)
	if got == nil || got.Model != "deepseek-v4-flash" {
		t.Fatalf("pick = %+v", got)
	}
	// Measured latency wins over the name.
	noteDesktopBotAckLatency(corelib.MaclawLLMConfig{URL: "https://hub/v1", Model: "official-low"}, 900*time.Millisecond)
	noteDesktopBotAckLatency(corelib.MaclawLLMConfig{URL: "https://hub/v1", Model: "deepseek-v4-flash"}, 9*time.Second)
	got = pickDesktopBotFastAck([]corelib.MaclawLLMProvider{hub}, skip, mat)
	if got == nil || got.Model != "official-low" {
		t.Fatalf("measured pick = %+v", got)
	}
	// Only big models: no fast racer.
	big := corelib.MaclawLLMProvider{Name: "c", URL: "https://c/v1", Key: "k", Model: "qwen4-max-preview"}
	if got := pickDesktopBotFastAck([]corelib.MaclawLLMProvider{big}, corelib.MaclawLLMConfig{}, mat); got != nil {
		t.Fatalf("a large model was picked as the fast racer: %+v", got)
	}
}

// Rule 3: a line answering a mid-task message starts by saying how it was
// folded in, whatever the model wrote.
func TestDesktopBotFoldHead(t *testing.T) {
	cases := map[string]string{"supplement": "加进去了：", "change": "好，改一下：", "parallel": "这是另一件事，我另开一个任务：", "queued": "这是另一件事，等手上这件做完就开始："}
	for kind, head := range cases {
		got := withDesktopBotFoldHead("周报那件只放前5条。", DesktopBotAckRequest{Kind: kind, Lang: "zh-Hans"})
		if got != head+"周报那件只放前5条。" {
			t.Fatalf("%s: %q", kind, got)
		}
	}
	if got := withDesktopBotFoldHead("加进去了：周报只放前5条。", DesktopBotAckRequest{Kind: "supplement", Lang: "zh-Hans"}); got != "加进去了：周报只放前5条。" {
		t.Fatalf("head doubled: %q", got)
	}
	if got := withDesktopBotFoldHead("好，我先看表。", DesktopBotAckRequest{Kind: "accept", Lang: "zh-Hans"}); got != "好，我先看表。" {
		t.Fatalf("accept got a fold head: %q", got)
	}
	for _, kind := range []string{"supplement", "change", "parallel"} {
		if !desktopBotFirstReplyKind(kind) || !strings.Contains(desktopBotAckSystemFor(kind), "kind "+kind+":") {
			t.Fatalf("%s is not a first reply with its own brief", kind)
		}
	}
}

// Rule 3: the decision for a mid-task message is one of supplement, change,
// stop, new or chat, from the model; an old-shape answer still maps.
func TestEvaluateDesktopTaskSupplementDecisions(t *testing.T) {
	prevLLM, prevModels := desktopBotUnderstandLLM, desktopBotAckModels
	t.Cleanup(func() { desktopBotUnderstandLLM, desktopBotAckModels = prevLLM, prevModels })
	desktopBotAckModels = func(*App) (corelib.MaclawLLMConfig, *corelib.MaclawLLMConfig) { return corelib.MaclawLLMConfig{}, nil }
	cases := []struct {
		raw, decision string
		supplement    bool
		instruction   string
	}{
		{`{"decision":"supplement","is_supplement":true,"instruction":"只要前5条"}`, "supplement", true, "只要前5条"},
		{`{"decision":"change","instruction":"改发CSDN"}`, "change", true, "【改变做法】改发CSDN"},
		{`{"decision":"stop","is_supplement":false}`, "stop", false, ""},
		{`{"decision":"new","is_supplement":false}`, "new", false, ""},
		{`{"is_supplement":true,"instruction":""}`, "supplement", true, "顺便把作者也记下"},
		{`{"is_supplement":false}`, "new", false, ""},
	}
	for _, c := range cases {
		raw := c.raw
		desktopBotUnderstandLLM = func(context.Context, *App, desktopBotUnderstandRequest) (string, error) { return raw, nil }
		got, err := (&App{}).EvaluateDesktopTaskSupplement("整理周报", "顺便把作者也记下", "zh-Hans")
		if err != nil || got.Decision != c.decision || got.IsSupplement != c.supplement || got.Instruction != c.instruction {
			t.Fatalf("%s -> %+v %v", c.raw, got, err)
		}
	}
	// No model answers inside the budget: the keyword fallback decides.
	prevBudget := desktopTaskSupplementBudget
	desktopTaskSupplementBudget = 200 * time.Millisecond
	t.Cleanup(func() { desktopTaskSupplementBudget = prevBudget })
	desktopBotUnderstandLLM = func(ctx context.Context, _ *App, _ desktopBotUnderstandRequest) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	got, _ := (&App{}).EvaluateDesktopTaskSupplement("整理周报", "你好", "zh-Hans")
	if got.Decision != "chat" {
		t.Fatalf("small talk fallback: %+v", got)
	}
}

// TestWaitUntilDesktopFreeStartsBelowTheCap: a job Hub refused at the per-bot
// cap goes as soon as one of the bot's runs ends, not when all of them did.
func TestWaitUntilDesktopFreeStartsBelowTheCap(t *testing.T) {
	prev := desktopBotBusyPoll
	desktopBotBusyPoll = 2 * time.Millisecond
	t.Cleanup(func() {
		desktopBotBusyPoll = prev
		desktopBotExchangeOverride = nil
	})
	var mu sync.Mutex
	polls := 0
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method != http.MethodGet || !strings.Contains(path, "/tasks") {
			return 0, fmt.Errorf("unexpected %s %s", method, path)
		}
		mu.Lock()
		polls++
		n := polls
		mu.Unlock()
		items := []map[string]any{
			{"run_id": "run-1", "bot_id": "bot_1", "status": "running"},
			{"run_id": "run-2", "bot_id": "bot_1", "status": "running"},
			{"run_id": "run-3", "bot_id": "bot_1", "status": "running"},
		}
		if n >= 3 {
			items[0]["status"] = "succeeded" // one of three ended; two still run
		}
		raw, _ := json.Marshal(map[string]any{"items": items})
		return http.StatusOK, json.Unmarshal(raw, dest)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := (&App{}).waitUntilDesktopFree(ctx, "bot_1"); err != nil {
		t.Fatalf("wait err = %v (polls %d)", err, polls)
	}
	mu.Lock()
	defer mu.Unlock()
	if polls < 3 {
		t.Fatalf("returned before a run ended: polls=%d", polls)
	}
}
