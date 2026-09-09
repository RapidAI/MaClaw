package guiapp

import (
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// classifyAdaptiveRetryFailure delegates to the shared agentruntime failure
// classifier so every host applies the same keyword vocabulary.
func classifyAdaptiveRetryFailure(err error) FailureCategory {
	return agentruntime.ClassifyAdaptiveRetryFailure(err)
}
