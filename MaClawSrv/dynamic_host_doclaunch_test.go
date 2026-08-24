package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWireSrvReviewedHostDocumentLauncher(t *testing.T) {
	wireSrvReviewedHostDocumentLauncher(&agentservice.CoreAgentExecutor{})
	wireSrvReviewedHostDocumentLauncher(nil)
}
