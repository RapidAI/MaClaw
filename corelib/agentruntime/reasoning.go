package agentruntime

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// DisplayReasoning returns the display-safe reasoning summary for a completed
// loop.  Providers may put the summary on LoopResult or on the last assistant
// history entry; keeping the fallback here makes GUI and headless renderers
// agree without importing a transport package.
func DisplayReasoning(result agent.LoopResult) string {
	if reasoning := strings.TrimSpace(result.Reasoning); reasoning != "" {
		return reasoning
	}
	for i := len(result.HistoryDelta) - 1; i >= 0; i-- {
		entry := result.HistoryDelta[i]
		if entry.Role != "assistant" {
			continue
		}
		if reasoning := strings.TrimSpace(entry.ReasoningContent); reasoning != "" {
			return reasoning
		}
	}
	return ""
}
