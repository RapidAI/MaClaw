package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

func wireSrvReviewedHostDesktopCapturer(executor *agentservice.CoreAgentExecutor) {
	if executor == nil {
		return
	}
	agentservice.WireReviewedHostNativeDesktopCapturer(executor)
}
