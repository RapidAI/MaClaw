package agentservice

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// noteDesktopIdleSummary records a finished desktop turn for the idle
// summary. It does not send, and it does not touch a machine.
// The excerpt refresh runs only if that summary later commits.
func (c *coreAgentCallbacks) noteDesktopIdleSummary(result agent.LoopResult) {
	if !c.promptPrefixActive() {
		return
	}
	tokens := result.Usage.InputTokens
	estimated := false
	if tokens <= 0 {
		tokens = agent.EstimatePromptTokens(c.history, c.userText)
		estimated = true
	}
	owner := memoryOwnerIDForPrincipal(c.principal)
	if owner == "" {
		owner = strings.TrimSpace(c.user.ID)
	}
	if owner == "" {
		owner = "desktop-user"
	}
	botID := c.botID
	convID := c.session.ID
	agent.SetIdleSummaryMemoryRefresh(botID, convID, func() string {
		return memory.DesktopBook(owner).RefreshExcerpt()
	})
	agent.NoteIdleTurn(agent.IdleTurnNote{
		ConversationID:  convID,
		BotID:           botID,
		Provider:        c.llmCfg.ProviderName,
		Model:           c.llmCfg.Model,
		PromptTokens:    tokens,
		TokensEstimated: estimated,
		TurnEnded:       true,
		PendingAsk:      result.AskUser != nil,
		At:              time.Now(),
	})
}
