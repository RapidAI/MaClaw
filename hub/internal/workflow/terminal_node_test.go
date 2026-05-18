package workflow

import (
	"testing"
)

func TestValidateTerminalNodeConfig_NilConfig(t *testing.T) {
	result := ValidateTerminalNodeConfig(nil)
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error for nil config, got %d", len(result.Errors))
	}
	if result.Errors[0] != "terminal node config is nil" {
		t.Errorf("unexpected error message: %s", result.Errors[0])
	}
}

func TestValidateTerminalNodeConfig_ValidConfig(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "user1", TimeoutHours: 48, MaxReminders: 3, ReminderInterval: 24},
		},
		Notifiers: []NotifierConfig{
			{UserID: "user2", TimeoutHours: 72, MaxReminders: 2, ReminderInterval: 24},
		},
	}
	result := ValidateTerminalNodeConfig(config)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", result.Warnings)
	}
}

func TestValidateTerminalNodeConfig_TimeoutHoursRange(t *testing.T) {
	tests := []struct {
		name      string
		timeout   int
		wantError bool
	}{
		{"zero (default)", 0, false},
		{"min valid", 1, false},
		{"max valid", 720, false},
		{"mid range", 360, false},
		{"below min", -1, true},
		{"above max", 721, true},
		{"way above max", 9999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TerminalNodeConfig{
				ResultExecutors: []ExecutorConfig{
					{UserID: "user1", TimeoutHours: tt.timeout},
				},
			}
			result := ValidateTerminalNodeConfig(config)
			hasTimeoutError := false
			for _, e := range result.Errors {
				if contains(e, "timeout_hours") {
					hasTimeoutError = true
					break
				}
			}
			if tt.wantError && !hasTimeoutError {
				t.Errorf("expected timeout_hours error for value %d, got none", tt.timeout)
			}
			if !tt.wantError && hasTimeoutError {
				t.Errorf("unexpected timeout_hours error for value %d", tt.timeout)
			}
		})
	}
}

func TestValidateTerminalNodeConfig_MaxRemindersRange(t *testing.T) {
	tests := []struct {
		name      string
		reminders int
		wantError bool
	}{
		{"zero (default)", 0, false},
		{"min valid", 1, false},
		{"max valid", 10, false},
		{"mid range", 5, false},
		{"below min", -1, true},
		{"above max", 11, true},
		{"way above max", 100, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &TerminalNodeConfig{
				ResultExecutors: []ExecutorConfig{
					{UserID: "user1", MaxReminders: tt.reminders},
				},
			}
			result := ValidateTerminalNodeConfig(config)
			hasRemindersError := false
			for _, e := range result.Errors {
				if contains(e, "max_reminders") {
					hasRemindersError = true
					break
				}
			}
			if tt.wantError && !hasRemindersError {
				t.Errorf("expected max_reminders error for value %d, got none", tt.reminders)
			}
			if !tt.wantError && hasRemindersError {
				t.Errorf("unexpected max_reminders error for value %d", tt.reminders)
			}
		})
	}
}

func TestValidateTerminalNodeConfig_NoExecutorWarning(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{},
		Notifiers: []NotifierConfig{
			{UserID: "user1"},
		},
	}
	result := ValidateTerminalNodeConfig(config)
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors (warning only), got %v", result.Errors)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	if !contains(result.Warnings[0], "no result_executor") {
		t.Errorf("unexpected warning message: %s", result.Warnings[0])
	}
}

func TestValidateTerminalNodeConfig_EmptyUserID(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "", TimeoutHours: 48},
		},
		Notifiers: []NotifierConfig{
			{UserID: "", TimeoutHours: 72},
		},
	}
	result := ValidateTerminalNodeConfig(config)
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors (one per empty user_id), got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestValidateTerminalNodeConfig_NotifierValidation(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "user1"},
		},
		Notifiers: []NotifierConfig{
			{UserID: "user2", TimeoutHours: 800, MaxReminders: 15},
		},
	}
	result := ValidateTerminalNodeConfig(config)
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 errors (timeout + reminders), got %d: %v", len(result.Errors), result.Errors)
	}
}

func TestApplyTerminalNodeDefaults_ExecutorDefaults(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "user1"},
		},
	}
	ApplyTerminalNodeDefaults(config)

	exec := config.ResultExecutors[0]
	if exec.TimeoutHours != 48 {
		t.Errorf("expected TimeoutHours=48, got %d", exec.TimeoutHours)
	}
	if exec.MaxReminders != 3 {
		t.Errorf("expected MaxReminders=3, got %d", exec.MaxReminders)
	}
	if exec.ReminderInterval != 24 {
		t.Errorf("expected ReminderInterval=24, got %d", exec.ReminderInterval)
	}
}

func TestApplyTerminalNodeDefaults_NotifierDefaults(t *testing.T) {
	config := &TerminalNodeConfig{
		Notifiers: []NotifierConfig{
			{UserID: "user1"},
		},
	}
	ApplyTerminalNodeDefaults(config)

	notif := config.Notifiers[0]
	if notif.TimeoutHours != 72 {
		t.Errorf("expected TimeoutHours=72, got %d", notif.TimeoutHours)
	}
	if notif.MaxReminders != 2 {
		t.Errorf("expected MaxReminders=2, got %d", notif.MaxReminders)
	}
	if notif.ReminderInterval != 24 {
		t.Errorf("expected ReminderInterval=24, got %d", notif.ReminderInterval)
	}
}

func TestApplyTerminalNodeDefaults_DoesNotOverrideExplicitValues(t *testing.T) {
	config := &TerminalNodeConfig{
		ResultExecutors: []ExecutorConfig{
			{UserID: "user1", TimeoutHours: 100, MaxReminders: 5, ReminderInterval: 12},
		},
		Notifiers: []NotifierConfig{
			{UserID: "user2", TimeoutHours: 200, MaxReminders: 8, ReminderInterval: 6},
		},
	}
	ApplyTerminalNodeDefaults(config)

	exec := config.ResultExecutors[0]
	if exec.TimeoutHours != 100 {
		t.Errorf("expected TimeoutHours=100 (not overridden), got %d", exec.TimeoutHours)
	}
	if exec.MaxReminders != 5 {
		t.Errorf("expected MaxReminders=5 (not overridden), got %d", exec.MaxReminders)
	}
	if exec.ReminderInterval != 12 {
		t.Errorf("expected ReminderInterval=12 (not overridden), got %d", exec.ReminderInterval)
	}

	notif := config.Notifiers[0]
	if notif.TimeoutHours != 200 {
		t.Errorf("expected TimeoutHours=200 (not overridden), got %d", notif.TimeoutHours)
	}
	if notif.MaxReminders != 8 {
		t.Errorf("expected MaxReminders=8 (not overridden), got %d", notif.MaxReminders)
	}
	if notif.ReminderInterval != 6 {
		t.Errorf("expected ReminderInterval=6 (not overridden), got %d", notif.ReminderInterval)
	}
}

func TestApplyTerminalNodeDefaults_NilConfig(t *testing.T) {
	// Should not panic
	ApplyTerminalNodeDefaults(nil)
}

// contains checks if substr is in s.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
