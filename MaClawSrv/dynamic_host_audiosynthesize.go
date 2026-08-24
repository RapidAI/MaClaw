package main

import "github.com/RapidAI/CodeClaw/corelib/agentservice"

func wireSrvReviewedHostSpeechPlayer(executor *agentservice.CoreAgentExecutor) {
	if executor == nil {
		return
	}
	agentservice.WireReviewedHostNativeSpeechPlayer(executor)
}
