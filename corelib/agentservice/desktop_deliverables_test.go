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

func TestDesktopDeliverablesOnlyForExecuteDesktopTurns(t *testing.T) {
	c := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "plan"}, userText: "截屏发我"}
	c.startDesktopDeliverables()
	if c.deliverables != nil {
		t.Fatal("plan phase got a checklist")
	}
}
