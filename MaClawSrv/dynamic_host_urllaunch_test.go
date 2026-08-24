package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWireSrvReviewedHostURLLauncher(t *testing.T) {
	wireSrvReviewedHostURLLauncher(&agentservice.CoreAgentExecutor{})
	wireSrvReviewedHostURLLauncher(nil)
}
