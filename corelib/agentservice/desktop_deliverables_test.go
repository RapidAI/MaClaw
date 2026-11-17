package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
