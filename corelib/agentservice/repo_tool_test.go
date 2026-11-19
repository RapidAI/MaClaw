package agentservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestRepoToolIsOnADesktopBotAndNotOnAnEmployee(t *testing.T) {
	prev := DesktopRepo
	t.Cleanup(func() { DesktopRepo = prev })
	var got RepoCall
	DesktopRepo = func(_ context.Context, call RepoCall) (string, error) {
		got = call
		return "github acme/app 0 open issues", nil
	}
	meta := map[string]string{"hub_bot": "1", "hub_user_id": "alice", "hub_tenant_id": "ten"}
	bot := &coreAgentCallbacks{
		botID:          "bot_1",
		runID:          "run_1",
		userText:       "看看 issue",
		session:        Session{ID: "sess_1"},
		instance:       Instance{ID: "inst_1", Metadata: meta},
		runtimeRequest: ExecuteRequest{Instance: Instance{ID: "inst_1", Metadata: meta}},
	}
	listed := false
	for _, tool := range bot.finishToolSurface(nil) {
		if tooldef.Name(tool) == "repo" {
			listed = true
		}
	}
	if !listed {
		t.Fatal("desktop bot has no repo tool")
	}
	if !bot.IsToolAllowedForPromptProfile("repo", agent.PromptProfileLight) {
		t.Fatal("a short development turn hid repo")
	}
	out := bot.ExecuteToolStructured("repo", `{"action":"issues","host":"github","repo":"acme/app"}`)
	if out.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(out.Result, "acme/app") {
		t.Fatalf("call: %+v", out)
	}
	if got.UserID != "alice" || got.BotID != "bot_1" || got.TenantID != "ten" || got.SessionID != "sess_1" {
		t.Fatalf("call context: %+v", got)
	}

	employee := &coreAgentCallbacks{botID: "bot_1"}
	for _, tool := range employee.finishToolSurface(nil) {
		if tooldef.Name(tool) == "repo" {
			t.Fatal("a digital employee was offered repo")
		}
	}
	if employee.IsToolAllowedForPromptProfile("repo", agent.PromptProfileLight) {
		t.Fatal("a light employee turn gained repo")
	}
	missed := employee.ExecuteToolStructured("repo", `{"action":"issues"}`)
	if missed.Outcome == agent.ToolExecutionOutcomeOK {
		t.Fatalf("employee ran repo: %+v", missed)
	}
}
