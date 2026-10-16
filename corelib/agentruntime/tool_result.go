package agentruntime

import "github.com/RapidAI/CodeClaw/corelib/agent"

// SemanticToolExecutionResult projects a semantic adapter's text result into
// the legacy agent outcome enum. The enum predates the explicit unknown and
// awaiting-receipt states, so both rejection and unobserved-effect markers
// must remain non-successful from the loop's perspective. Keeping this
// projection in Runtime prevents GUI and headless adapters from independently
// deciding whether a marker invites a retry.
func SemanticToolExecutionResult(text string) agent.ToolExecutionResult {
	result := agent.ToolExecutionResult{Result: text, Outcome: agent.ToolExecutionOutcomeOK}
	if SelectionFailed(text) || SelectionOutcomeUnknown(text) {
		result.Outcome = agent.ToolExecutionOutcomeError
	}
	return result
}
