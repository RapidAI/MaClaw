package agentservice

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	agent.ResetPromptPrefixPins()
	t.Cleanup(agent.ResetPromptPrefixPins)
	cb := &coreAgentCallbacks{
		botID:   "bot-silence",
		session: Session{ID: "conv-silence"},
	}
	first := cb.pinDesktopSystemPrompt(desktopbot.PinWorthSaying("系统规则"))
	if !strings.Contains(first, desktopbot.WorthSayingPrompt) || strings.Contains(first, "没有变化就结束") {
		t.Fatalf("system = %q", first)
	}
	second := cb.pinDesktopSystemPrompt(desktopbot.PinWorthSaying("系统规则\n中途改过"))
	if second != first {
		t.Fatalf("pin moved to %q", second)
	}
	if (&coreAgentCallbacks{}).promptPrefixActive() {
		t.Fatal("a host turn froze the interruption rule")
	}
}

func TestAskUserFillerDoesNotAddressThePerson(t *testing.T) {
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "无变化", InputType: "text"}) {
		t.Fatal("placeholder question was recorded as an ask")
	}
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "下一步？", InputType: "text"}) == false {
		t.Fatal("real question was dropped")
	}
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "无变化", InputType: "choice", Options: []string{"继续", "停下"}}) == false {
		t.Fatal("choices were dropped")
	}
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "无变化", InputType: "choice", Options: []string{"无变化", " ", ""}}) {
		t.Fatal("placeholder labels were recorded as choices")
	}
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "无变化", InputType: "choice", Options: []string{"继续", "无变化"}}) == false {
		t.Fatal("a real choice beside a placeholder label was dropped")
	}
	if askUserAddressesPerson(&agent.AskUserRequest{Question: "无变化", InputType: "secret", SecretName: "SITE_PASSWORD"}) == false {
		t.Fatal("secret was dropped")
	}
	if askUserAddressesPerson(nil) {
		t.Fatal("nil ask was recorded")
	}
}
