package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWireSrvReviewedHostSpeechPlayer(t *testing.T) {
	wireSrvReviewedHostSpeechPlayer(&agentservice.CoreAgentExecutor{})
	wireSrvReviewedHostSpeechPlayer(nil)
}
