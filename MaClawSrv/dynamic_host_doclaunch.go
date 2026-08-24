package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

func wireSrvReviewedHostDocumentLauncher(executor *agentservice.CoreAgentExecutor) {
	if executor == nil {
		return
	}
	agentservice.WireReviewedHostNativeDocumentLauncher(executor)
}
