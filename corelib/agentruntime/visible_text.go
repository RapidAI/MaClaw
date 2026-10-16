package agentruntime

import "github.com/RapidAI/CodeClaw/corelib/agent"

// UserFacingText projects provider output into the text that a host may show
// to an end user. Working-state markers remain available to the Agent loop but
// are stripped consistently for GUI, headless HTTP and TUI responses.
func UserFacingText(text string) string {
	return agent.StripWorkingStateFromVisible(text)
}
