package agentservice

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestDesktopDeliverablesNoteShownOnceWhenReady(t *testing.T) {
	st := &desktopDeliverablesState{done: make(chan struct{})}
	c := &coreAgentCallbacks{deliverables: st}
	r := c.withDeliverablesNote("desktop", agent.ToolExecutionResult{Result: "Delivered a.png (3 bytes)"})
	if r.Result != "Delivered a.png (3 bytes)" {
		t.Fatalf("note before ready: %q", r.Result)
	}
	st.items = []string{"运行截图，发到聊天"}
	close(st.done)
	r = c.withDeliverablesNote("desktop", agent.ToolExecutionResult{Result: "Delivered a.png (3 bytes)"})
	if !strings.HasPrefix(r.Result, "Delivered a.png") || !strings.Contains(r.Result, "1. 运行截图，发到聊天") {
		t.Fatalf("note: %q", r.Result)
	}
	if r = c.withDeliverablesNote("desktop", agent.ToolExecutionResult{Result: "x"}); r.Result != "x" {
		t.Fatalf("note twice: %q", r.Result)
	}
	if got := c.desktopDeliverableItems(); len(got) != 1 {
		t.Fatalf("items %v", got)
	}
	if got := (&coreAgentCallbacks{}).desktopDeliverableItems(); got != nil {
		t.Fatalf("no checklist %v", got)
	}
}

func TestScheduleTurnDoesNotAskTheChecklistToProveTheInterval(t *testing.T) {
	c := &coreAgentCallbacks{
		messageMetadata: map[string]string{"bot_origin": "schedule", "bot_phase": "execute"},
		userText:        "每五分钟查询北京的天气并生成PDF发给我",
	}
	got := desktopDeliverableRequestText(c)
	if !strings.Contains(got, "不要把「每几分钟」") || !strings.Contains(got, "生成PDF发给我") {
		t.Fatalf("text = %q", got)
	}
	resumed := desktopDeliverableRequestText(&coreAgentCallbacks{
		messageMetadata: map[string]string{
			"bot_origin":     "schedule",
			"bot_phase":      "execute",
			"restart_resume": "1",
			"task_request":   "每五分钟查询北京的天气并生成PDF发给我",
		},
		userText: restartNoticeContinue,
	})
	if !strings.Contains(resumed, "生成PDF发给我") || strings.Contains(resumed, restartNoticeContinue) {
		t.Fatalf("resumed = %q", resumed)
	}
	plain := desktopDeliverableRequestText(&coreAgentCallbacks{userText: "生成PDF发给我"})
	if strings.Contains(plain, "定时器") {
		t.Fatalf("plain = %q", plain)
	}
}

func TestDesktopDeliverablesOnlyForExecuteDesktopTurns(t *testing.T) {
	plan := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "plan"}, userText: "截屏发我"}
	if plan.desktopDeliverablesApply() {
		t.Fatal("plan phase reads deliverables")
	}
	none := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "execute", "bot_desktop": "none"}, userText: "做一份PDF"}
	if !none.desktopDeliverablesApply() {
		t.Fatal("execute without a computer skipped the checklist")
	}
	wake := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "execute", "bot_wake": "1"}, userText: "继续"}
	if !wake.desktopDeliverablesApply() {
		t.Fatal("execute wake skipped the checklist")
	}
}

// runDeliverableExtraction drives startDesktopDeliverables with a stubbed
// model and returns what RecordDesktopDeliverableExtraction stored.
func runDeliverableExtraction(t *testing.T, runID string, complete func(context.Context, corelib.MaclawLLMConfig, *http.Client, []interface{}) (*llm.Response, error)) (items []agent.DesktopDeliverable, failed bool, cause string) {
	t.Helper()
	prevComplete := agent.CompleteDesktopOutcome
	agent.CompleteDesktopOutcome = complete
	t.Cleanup(func() { agent.CompleteDesktopOutcome = prevComplete })

	prev := RememberDesktopOutputContract
	RememberDesktopOutputContract = func(r string, it []agent.DesktopDeliverable, f bool, c string) {
		if r == runID {
			items, failed, cause = it, f, c
		}
	}
	t.Cleanup(func() { RememberDesktopOutputContract = prev })

	c := &coreAgentCallbacks{
		runID:           runID,
		messageMetadata: map[string]string{"bot_phase": "execute"},
		userText:        "生成报告发给我",
	}
	c.startDesktopDeliverables()
	// The goroutine owns st.done; wait on the checklist channel directly.
	select {
	case <-c.deliverables.done:
	case <-time.After(5 * time.Second):
		t.Fatal("extraction never finished")
	}
	return items, failed, cause
}

func TestDeliverableExtractionRetriesOnceThenSucceeds(t *testing.T) {
	var calls int32
	items, failed, cause := runDeliverableExtraction(t, "run-retry-ok", func(context.Context, corelib.MaclawLLMConfig, *http.Client, []interface{}) (*llm.Response, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			return nil, errors.New("transient model read error")
		}
		return &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: `{"items":[{"kind":"file","text":"报告"}]}`}}}}, nil
	})
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("extraction attempts = %d, want 2 (one bounded retry)", got)
	}
	if failed {
		t.Fatalf("retry recovered but recorded failed; cause=%q", cause)
	}
	if len(items) != 1 || items[0].Kind != "file" {
		t.Fatalf("items = %#v", items)
	}
}

func TestDeliverableExtractionRepairReasksOnMalformedOutput(t *testing.T) {
	var calls int32
	var retryUser string
	items, failed, cause := runDeliverableExtraction(t, "run-repair", func(_ context.Context, _ corelib.MaclawLLMConfig, _ *http.Client, messages []interface{}) (*llm.Response, error) {
		n := atomic.AddInt32(&calls, 1)
		user := ""
		if len(messages) > 1 {
			if m, ok := messages[1].(map[string]interface{}); ok {
				user, _ = m["content"].(string)
			}
		}
		if n == 1 {
			// First read produces output the parser rejects (non-JSON).
			return &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: "sure, here is the list"}}}}, nil
		}
		retryUser = user
		return &llm.Response{Choices: []llm.Choice{{Message: llm.Message{Content: `{"items":[{"kind":"file","text":"报告"}]}`}}}}, nil
	})
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("attempts = %d, want 2 (one bounded repair)", got)
	}
	if failed {
		t.Fatalf("grounded repair recovered but recorded failed; cause=%q", cause)
	}
	if len(items) != 1 || items[0].Kind != "file" {
		t.Fatalf("items = %#v", items)
	}
	// The retry must be a corrective prompt grounded on the malformed output,
	// not the identical request re-sent.
	if !strings.Contains(retryUser, "sure, here is the list") {
		t.Fatalf("repair prompt did not carry prior malformed output: %q", retryUser)
	}
	if !strings.Contains(retryUser, "重新输出") {
		t.Fatalf("repair prompt is not corrective: %q", retryUser)
	}
}

func TestDeliverableExtractionStillFailedKeepsConcreteCause(t *testing.T) {
	var calls int32
	_, failed, cause := runDeliverableExtraction(t, "run-retry-fail", func(context.Context, corelib.MaclawLLMConfig, *http.Client, []interface{}) (*llm.Response, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("deliverables extraction was not json")
	})
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("extraction attempts = %d, want 2 (bounded retry)", got)
	}
	if !failed {
		t.Fatal("still-failed extraction must stay failed, not succeed")
	}
	if !strings.Contains(cause, "not json") {
		t.Fatalf("concrete cause lost: %q", cause)
	}
}
