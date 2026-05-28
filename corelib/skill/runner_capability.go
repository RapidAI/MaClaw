package skill

import (
	"fmt"
	"strings"
)

const (
	RunnerBackendGUI = "gui"
	RunnerBackendTUI = "tui"
)

// StepActionSupport describes whether a skill step action can run on a runner.
// Keeping this in corelib makes GUI/TUI capability boundaries explicit instead
// of letting each frontend fail with unrelated ad-hoc messages.
type StepActionSupport struct {
	Runner     string
	Action     string
	Supported  bool
	Reason     string
	ActionHint string
}

// UnsupportedStepActionError is returned when a runner cannot execute a step
// action. The machine prefix is intentionally stable for ClassifyStepError.
type UnsupportedStepActionError struct {
	Support StepActionSupport
}

func (e UnsupportedStepActionError) Error() string {
	msg := "unsupported_step_action: " + e.Support.Message()
	if strings.TrimSpace(e.Support.ActionHint) != "" {
		msg += " " + strings.TrimSpace(e.Support.ActionHint)
	}
	return msg
}

// Message returns a user-facing explanation without the classifier prefix.
func (s StepActionSupport) Message() string {
	action := s.Action
	if action == "" {
		action = "<empty>"
	}
	runner := s.Runner
	if runner == "" {
		runner = "unknown"
	}
	if s.Reason != "" {
		return s.Reason
	}
	return fmt.Sprintf("action %q is not supported by %s runner; supported actions: %s", action, runner, strings.Join(SupportedStepActions(runner), ", "))
}

// EnsureStepActionSupported returns nil when runner can execute action.
func EnsureStepActionSupported(runner, action string) error {
	support := CheckStepActionSupport(runner, action)
	if support.Supported {
		return nil
	}
	return UnsupportedStepActionError{Support: support}
}

// CheckStepActionSupport is the single source of truth for currently executable
// step actions per runner backend.
func CheckStepActionSupport(runner, action string) StepActionSupport {
	runner = normalizeRunnerBackend(runner)
	action = normalizeStepAction(action)
	support := StepActionSupport{
		Runner:     runner,
		Action:     action,
		Supported:  false,
		ActionHint: "[action: inspect] Check the skill step action and runner backend.",
	}
	for _, supported := range SupportedStepActions(runner) {
		if action == supported {
			support.Supported = true
			support.ActionHint = ""
			return support
		}
	}
	if runner == RunnerBackendTUI && action == "craft_tool" {
		support.Reason = "craft_tool requires GUI skill runner; TUI currently supports bash steps only."
		support.ActionHint = "[action: open_gui] Run this skill in the GUI runner, or add executable bash steps before using TUI."
		return support
	}
	if runner == RunnerBackendTUI && action == "poll" {
		support.Reason = "poll steps require GUI skill runner session support; TUI currently supports bash steps only."
		support.ActionHint = "[action: open_gui] Run this skill in the GUI runner, or replace the poll step with a bash-compatible check."
		return support
	}
	if runner == RunnerBackendGUI && isExternalCodingSessionAction(action) {
		support.Reason = fmt.Sprintf("action %q uses external coding sessions, which are disabled for the GUI skill runner; use the internal CodingSubAgent workflow for coding tasks.", action)
		support.ActionHint = "[action: edit_skill] Replace external session steps with bash/craft_tool steps, or let the agent route coding tasks to CodingSubAgent."
		return support
	}
	support.Reason = fmt.Sprintf("action %q is not supported by %s runner; supported actions: %s", action, runner, strings.Join(SupportedStepActions(runner), ", "))
	return support
}

func SupportedStepActions(runner string) []string {
	switch normalizeRunnerBackend(runner) {
	case RunnerBackendGUI:
		return []string{"bash", "call_mcp_tool", "craft_tool", "poll"}
	case RunnerBackendTUI:
		return []string{"bash"}
	default:
		return nil
	}
}

func isExternalCodingSessionAction(action string) bool {
	switch NormalizeStepActionName(action) {
	case "create_session", "send_input", "send_and_observe":
		return true
	default:
		return false
	}
}

func normalizeRunnerBackend(runner string) string {
	switch strings.ToLower(strings.TrimSpace(runner)) {
	case RunnerBackendGUI:
		return RunnerBackendGUI
	case RunnerBackendTUI:
		return RunnerBackendTUI
	default:
		return strings.ToLower(strings.TrimSpace(runner))
	}
}

// NormalizeStepActionName canonicalizes runner action spelling without changing
// semantics. Compatibility conversion (for example python -> bash) remains in
// NormalizeStepForRunner.
func NormalizeStepActionName(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "-", "_")
	action = strings.ReplaceAll(action, " ", "_")
	return action
}

func normalizeStepAction(action string) string {
	return NormalizeStepActionName(action)
}
