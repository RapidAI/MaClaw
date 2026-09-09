package agentruntime

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// RunAgentTurn is the single production entry into the Agent loop. Hosts may
// supply callbacks and HTTP clients; they must not call agent.RunLoop directly.
func RunAgentTurn(cb agent.LoopCallbacks, userText string, history []agent.ConversationEntry, httpClient *http.Client, hooks ...agent.LoopHooks) agent.LoopResult {
	return agent.RunLoop(cb, userText, history, httpClient, hooks...)
}

// RunAgentTurnWithUserContent is the multimodal variant used when attachment
// staging has already produced a provider-native user content payload.
func RunAgentTurnWithUserContent(cb agent.LoopCallbacks, userText string, userContent interface{}, history []agent.ConversationEntry, httpClient *http.Client, hooks ...agent.LoopHooks) agent.LoopResult {
	return agent.RunLoopWithUserContent(cb, userText, userContent, history, httpClient, hooks...)
}
