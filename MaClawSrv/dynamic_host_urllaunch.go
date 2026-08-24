package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

func wireSrvReviewedHostURLLauncher(executor *agentservice.CoreAgentExecutor) {
	if executor == nil {
		return
	}
	agentservice.WireReviewedHostNativeURLLauncher(executor)
}
