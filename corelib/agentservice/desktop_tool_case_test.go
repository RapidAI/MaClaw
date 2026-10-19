package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotToolCases(t *testing.T) {
	desktopbot.ResetToolSessionState()
	t.Cleanup(desktopbot.ResetToolSessionState)
	cb := &coreAgentCallbacks{
		botID:           "bot-case",
		session:         Session{ID: "conv-case"},
		messageMetadata: map[string]string{"bot_phase": "execute"},
		userText:        "打开页面",
	}
	failed := cb.finishBotToolResult("slack", `{"url":"https://slack.example/a"}`, agent.ToolExecutionResult{
		Result:  "Error: 401 unauthorized Bearer sk-live-secret-token-should-not-leak",
		Outcome: agent.ToolExecutionOutcomeError,
	})
	if failed.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("connector result: %+v", failed)
	}
	web := cb.finishBotToolResult("web_search", `{"query":"weather"}`, agent.ToolExecutionResult{
		Result:  "Error: 401 unauthorized",
		Outcome: agent.ToolExecutionOutcomeError,
	})
	if web.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatal(web.Result)
	}
	allowed, reason := cb.IsToolCallAllowed("desktop", `{"action":"navigate","url":"https://slack.example/inbox"}`)
	if allowed || !strings.Contains(reason, "slack") || strings.Contains(reason, "sk-") {
		t.Fatalf("navigate allowed=%v reason=%s", allowed, reason)
	}
	if cb.connectorAsk.Kind != "ask" || !strings.Contains(cb.connectorAsk.Text, "slack") || strings.Contains(cb.connectorAsk.Text, "sk-") {
		t.Fatalf("ask = %+v", cb.connectorAsk)
	}
	meta := map[string]string{}
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != cb.connectorAsk.Text || meta["send_to_user_kind"] != "ask" {
		t.Fatalf("metadata = %#v", meta)
	}
	meta["send_to_user"] = "已经有一句"
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != "已经有一句" {
		t.Fatal("an existing sentence was overwritten")
	}
	_, _ = cb.IsToolCallAllowed("desktop", `{"action":"probe"}`)
	if cb.connectorAsk.Text != meta["send_to_user"] && !strings.Contains(cb.connectorAsk.Text, "slack") {
		t.Fatal("a second refusal replaced the ask")
	}
	if ok, reason := cb.IsToolCallAllowed("web_search", `{"query":"weather"}`); !ok {
		t.Fatalf("web_search was blocked: %s", reason)
	}
	cb.userText = "改用浏览器"
	if ok, _ := cb.IsToolCallAllowed("desktop", `{"action":"navigate","url":"https://slack.example/inbox"}`); ok {
		t.Fatal("the phrase alone released the browser")
	}
	cb.userText = "继续"
	if ok, _ := cb.IsToolCallAllowed("desktop", `{"action":"navigate","url":"https://slack.example/inbox","user_authorized_bypass":true}`); ok {
		t.Fatal("the flag alone released the browser")
	}
	cb.userText = "请改用浏览器"
	if ok, reason := cb.IsToolCallAllowed("desktop", `{"action":"navigate","url":"https://slack.example/inbox","user_authorized_bypass":true}`); !ok {
		t.Fatalf("level 4 stayed blocked: %s", reason)
	}
	if ok, reason := cb.IsToolCallAllowed("desktop", `{"action":"computer","user_authorized_bypass":true}`); ok || !strings.Contains(reason, "computer") {
		t.Fatalf("level 5 allowed=%v reason=%s", ok, reason)
	}
	cb.userText = "继续"
	if ok, reason := cb.IsToolCallAllowed("desktop", `{"action":"navigate","url":"https://other.example/"}`); !ok {
		t.Fatalf("a different host was blocked: %s", reason)
	}
	long := strings.Repeat("乙", desktopbot.ToolResultSpillRunes+8)
	root := t.TempDir()
	desktopbot.SetSpillRoot(root)
	spilled := cb.finishBotToolResult("slack", `{}`, agent.ToolExecutionResult{Result: long, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.HasPrefix(spilled.Result, "/home/desktop/tasks/") || strings.Contains(spilled.Result, long) {
		t.Fatalf("spill = %q", spilled.Result)
	}
}
