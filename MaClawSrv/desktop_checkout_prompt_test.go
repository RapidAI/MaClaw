package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestDesktopPromptStopsBeforeOrderOrPayment(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	execute := agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), execute)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), execute)
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools %v %v", tools, err)
	}
	for name, text := range map[string]string{"prompt": prompt, "tool": tools[0].Description} {
		if !strings.Contains(text, "except before submitting an order or paying") || !strings.Contains(text, "确认支付") {
			t.Fatalf("%s lacks the checkout exception: %s", name, text)
		}
	}
}
