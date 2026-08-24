package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestWireSrvReviewedHostDesktopCapturer(t *testing.T) {
	wireSrvReviewedHostDesktopCapturer(&agentservice.CoreAgentExecutor{})
	wireSrvReviewedHostDesktopCapturer(nil)
}
