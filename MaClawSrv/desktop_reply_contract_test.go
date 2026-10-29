package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestHubBotReplyIsPersonFacingNotHelperCard(t *testing.T) {
	hub := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{Instance: agentservice.Instance{Metadata: map[string]string{"hub_bot": "1"}}}}
	if strings.Contains(desktopReplyLead(hub)+desktopReplyCard(hub), "three conclusion lines") {
		t.Fatal("hub bot turn got the GUI helper card format")
	}
	gui := agentruntime.TurnRequest{Input: agentservice.ExecuteRequest{}}
	if !strings.Contains(desktopReplyLead(gui), "three conclusion lines") {
		t.Fatal("GUI helper lost its card format")
	}
}
