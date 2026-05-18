package workflow

import "fmt"

// TerminalNodeConfig extends the existing WorkflowNode config for terminal nodes.
// It defines who receives results and who gets notified when the workflow completes.
type TerminalNodeConfig struct {
	ResultExecutors []ExecutorConfig `json:"result_executors"`
	Notifiers       []NotifierConfig `json:"notifiers"`
}

// ExecutorConfig defines a single result executor assignment.
type ExecutorConfig struct {
	UserID           string `json:"user_id"`
	TimeoutHours     int    `json:"timeout_hours"`           // 1-720, default 48
	MaxReminders     int    `json:"max_reminders"`           // 1-10, default 3
	ReminderInterval int    `json:"reminder_interval_hours"` // default 24
}

// NotifierConfig defines a single notifier assignment.
type NotifierConfig struct {
	UserID           string `json:"user_id"`
	TimeoutHours     int    `json:"timeout_hours"`           // 1-720, default 72
	MaxReminders     int    `json:"max_reminders"`           // 1-10, default 2
	ReminderInterval int    `json:"reminder_interval_hours"` // default 24
}

// Default values for ExecutorConfig.
const (
	DefaultExecutorTimeoutHours     = 48
	DefaultExecutorMaxReminders     = 3
	DefaultExecutorReminderInterval = 24
)

// Default values for NotifierConfig.
const (
	DefaultNotifierTimeoutHours     = 72
	DefaultNotifierMaxReminders     = 2
	DefaultNotifierReminderInterval = 24
)

// TerminalNodeValidationResult holds the validation outcome for a TerminalNodeConfig.
// Errors are issues that must be fixed before saving.
// Warnings are advisory messages that allow saving but suggest improvements.
type TerminalNodeValidationResult struct {
	Errors   []string
	Warnings []string
}

// ValidateTerminalNodeConfig validates a TerminalNodeConfig and returns errors and warnings.
// It checks:
//   - Each executor/notifier has a non-empty UserID
//   - TimeoutHours is in [1, 720] (if non-zero)
//   - MaxReminders is in [1, 10] (if non-zero)
//   - If no ResultExecutors are configured, a warning (not error) is added
func ValidateTerminalNodeConfig(config *TerminalNodeConfig) *TerminalNodeValidationResult {
	result := &TerminalNodeValidationResult{}

	if config == nil {
		result.Errors = append(result.Errors, "terminal node config is nil")
		return result
	}

	// Validate each executor
	for i, exec := range config.ResultExecutors {
		prefix := fmt.Sprintf("result_executor[%d]", i)

		if exec.UserID == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: user_id is required", prefix))
		}
		if exec.TimeoutHours != 0 && (exec.TimeoutHours < 1 || exec.TimeoutHours > 720) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: timeout_hours must be in range [1, 720], got %d", prefix, exec.TimeoutHours))
		}
		if exec.MaxReminders != 0 && (exec.MaxReminders < 1 || exec.MaxReminders > 10) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: max_reminders must be in range [1, 10], got %d", prefix, exec.MaxReminders))
		}
	}

	// Validate each notifier
	for i, notif := range config.Notifiers {
		prefix := fmt.Sprintf("notifier[%d]", i)

		if notif.UserID == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: user_id is required", prefix))
		}
		if notif.TimeoutHours != 0 && (notif.TimeoutHours < 1 || notif.TimeoutHours > 720) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: timeout_hours must be in range [1, 720], got %d", prefix, notif.TimeoutHours))
		}
		if notif.MaxReminders != 0 && (notif.MaxReminders < 1 || notif.MaxReminders > 10) {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: max_reminders must be in range [1, 10], got %d", prefix, notif.MaxReminders))
		}
	}

	// Warning (not error) if no executor is configured
	if len(config.ResultExecutors) == 0 {
		result.Warnings = append(result.Warnings, "no result_executor configured; workflow results will not be delivered to any executor")
	}

	return result
}

// ApplyTerminalNodeDefaults fills in default values for zero-valued fields in a TerminalNodeConfig.
// Executor defaults: TimeoutHours=48, MaxReminders=3, ReminderInterval=24
// Notifier defaults: TimeoutHours=72, MaxReminders=2, ReminderInterval=24
func ApplyTerminalNodeDefaults(config *TerminalNodeConfig) {
	if config == nil {
		return
	}

	for i := range config.ResultExecutors {
		if config.ResultExecutors[i].TimeoutHours == 0 {
			config.ResultExecutors[i].TimeoutHours = DefaultExecutorTimeoutHours
		}
		if config.ResultExecutors[i].MaxReminders == 0 {
			config.ResultExecutors[i].MaxReminders = DefaultExecutorMaxReminders
		}
		if config.ResultExecutors[i].ReminderInterval == 0 {
			config.ResultExecutors[i].ReminderInterval = DefaultExecutorReminderInterval
		}
	}

	for i := range config.Notifiers {
		if config.Notifiers[i].TimeoutHours == 0 {
			config.Notifiers[i].TimeoutHours = DefaultNotifierTimeoutHours
		}
		if config.Notifiers[i].MaxReminders == 0 {
			config.Notifiers[i].MaxReminders = DefaultNotifierMaxReminders
		}
		if config.Notifiers[i].ReminderInterval == 0 {
			config.Notifiers[i].ReminderInterval = DefaultNotifierReminderInterval
		}
	}
}
