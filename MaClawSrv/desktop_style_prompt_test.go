package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDesktopStylePromptCarriesOwnerStyle(t *testing.T) {
	meta := promptparts.Style{Soul: "名字叫阿龙", Memory: "主人周末不看消息", Parts: map[string]string{"quiet": "-"}}.Meta()
	req := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: meta}}}
	out := desktopStylePrompt(req, promptparts.Honesty, promptparts.Quiet)
	if !strings.Contains(out, "名字叫阿龙") || !strings.Contains(out, "主人周末不看消息") || !strings.Contains(out, promptparts.Default(promptparts.Honesty)) || strings.Contains(out, promptparts.Default(promptparts.Quiet)) {
		t.Fatal(out)
	}
	if out := desktopStylePrompt(agentruntime.TurnRequest{}, promptparts.Voice); !strings.Contains(out, promptparts.DefaultSoul) {
		t.Fatal(out)
	}
	t.Setenv("MACLAW_STYLE_LAYER", "off")
	if desktopStylePrompt(req, promptparts.Voice) != "" {
		t.Fatal("kill switch ignored")
	}
}
