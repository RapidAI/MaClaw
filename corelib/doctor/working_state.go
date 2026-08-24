package doctor

import (
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// WorkingStateCheck reports whether task-turn working state is enabled.
// Info when off (operator rollback); OK when on. Never a readiness blocker.
func WorkingStateCheck() Check {
	off := agent.WorkingStateDisabled()
	envRaw := strings.TrimSpace(os.Getenv(agent.WorkingStateEnvKey))
	detail := map[string]any{
		"env":     envRaw,
		"enabled": !off,
	}
	if off {
		return Check{
			ID:      "agent.working_state",
			Status:  StatusInfo,
			Message: "working state off (MACLAW_WORKING_STATE=off)",
			Hint:    "Unset MACLAW_WORKING_STATE to restore earned attach on full tool turns",
			Detail:  detail,
		}
	}
	return Check{
		ID:      "agent.working_state",
		Status:  StatusOK,
		Message: "working state on (earned attach on full tool turns)",
		Hint:    "Disable: MACLAW_WORKING_STATE=off",
		Detail:  detail,
	}
}
