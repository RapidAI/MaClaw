package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestApplyRepairRecordsSuccessfulAttemptMetadata(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	entry := &corelib.NLSkillEntry{Name: "repairable", LastError: formatted}

	applied := ApplyRepair(entry, &RepairResult{
		Repaired:    true,
		Explanation: "fixed command",
		NewSteps:    []SkillYAMLStep{{Action: "bash", Params: map[string]interface{}{"command": "echo fixed"}}},
	})

	if !applied {
		t.Fatal("ApplyRepair() = false, want true")
	}
	if entry.RepairAttemptCount != 1 || entry.LastRepairAt == "" || len(entry.RepairHistory) != 1 {
		t.Fatalf("repair metadata = count %d at %q history %#v", entry.RepairAttemptCount, entry.LastRepairAt, entry.RepairHistory)
	}
	if entry.RepairHistory[0].ErrorClass != string(ErrCommandNotFound) || entry.RepairHistory[0].Success {
		t.Fatalf("repair history = %#v, want command_not_found unverified", entry.RepairHistory[0])
	}
	if !strings.Contains(entry.LastError, "auto-repaired") {
		t.Fatalf("LastError = %q, want auto-repaired", entry.LastError)
	}
}

func TestShouldAttemptRepairRecognizesHubSources(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	for _, source := range []string{"hub", "skillhub", "clawhub", "github", "auto_hub", "auto_github", " GitHub "} {
		source := source
		t.Run(source, func(t *testing.T) {
			entry := &corelib.NLSkillEntry{Name: "repairable", Source: source, UsageCount: 1, FailureCount: 1, LastError: formatted}
			if !ShouldAttemptRepair(entry) {
				t.Fatalf("ShouldAttemptRepair(source=%q) = false, want true", source)
			}
		})
	}
}

func TestShouldAttemptRepairSkipsNonRepairableErrors(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrRateLimit, UserMessage: "rate limited", Repairable: false})
	entry := &corelib.NLSkillEntry{
		Name:         "rate-limited",
		Source:       "github",
		UsageCount:   5,
		SuccessCount: 0,
		FailureCount: 5,
		LastError:    formatted,
	}

	if ShouldAttemptRepair(entry) {
		t.Fatal("ShouldAttemptRepair() = true for rate_limit, want false")
	}
}

func TestShouldAttemptRepairSkipsFileBackedSkills(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	entry := &corelib.NLSkillEntry{
		Name:         "file-backed",
		Source:       " file ",
		SkillDir:     t.TempDir(),
		UsageCount:   5,
		SuccessCount: 0,
		FailureCount: 5,
		LastError:    formatted,
	}

	if ShouldAttemptRepair(entry) {
		t.Fatal("ShouldAttemptRepair() = true for file-backed skill, want false")
	}
}

func TestShouldAttemptRepairSkipsInactiveStatuses(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	for _, status := range []string{"needs_review", "disabled", "archived"} {
		status := status
		t.Run(status, func(t *testing.T) {
			entry := &corelib.NLSkillEntry{
				Name:         "inactive",
				Source:       "github",
				Status:       status,
				UsageCount:   5,
				SuccessCount: 0,
				FailureCount: 5,
				LastError:    formatted,
			}
			if ShouldAttemptRepair(entry) {
				t.Fatalf("ShouldAttemptRepair(status=%q) = true, want false", status)
			}
		})
	}
}

func TestApplyRepairIgnoresNilInputs(t *testing.T) {
	if ApplyRepair(nil, &RepairResult{ShouldDisable: true}) {
		t.Fatal("ApplyRepair(nil skill) = true, want false")
	}
	entry := &corelib.NLSkillEntry{Name: "repairable"}
	if ApplyRepair(entry, nil) {
		t.Fatal("ApplyRepair(nil result) = true, want false")
	}
	if entry.Status != "" || entry.RepairAttemptCount != 0 || len(entry.RepairHistory) != 0 {
		t.Fatalf("entry mutated on nil result: %#v", entry)
	}
}

func TestApplyRepairRecordsDisableAttemptMetadata(t *testing.T) {
	formatted := FormatErrorForLLM(ClassifiedError{Class: ErrMissingParam, UserMessage: "bad args", Repairable: true})
	entry := &corelib.NLSkillEntry{Name: "bad-skill", Status: "active", LastError: formatted}

	applied := ApplyRepair(entry, &RepairResult{ShouldDisable: true, Explanation: "impossible task"})

	if applied {
		t.Fatal("ApplyRepair() = true, want false for disable")
	}
	if entry.Status != "needs_review" || !strings.Contains(entry.LastError, "auto-disabled") {
		t.Fatalf("disabled entry = %#v", entry)
	}
	if entry.RepairAttemptCount != 1 || entry.LastRepairAt == "" || len(entry.RepairHistory) != 1 {
		t.Fatalf("disable metadata = count %d at %q history %#v", entry.RepairAttemptCount, entry.LastRepairAt, entry.RepairHistory)
	}
	if entry.RepairHistory[0].ErrorClass != string(ErrMissingParam) || entry.RepairHistory[0].Success {
		t.Fatalf("disable history = %#v, want missing_param unverified", entry.RepairHistory[0])
	}
}

func TestApplyRepairKeepsLastFiveRepairHistoryItems(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "repairable", LastError: FormatErrorForLLM(ClassifiedError{Class: ErrUnknown})}
	for i := 0; i < 5; i++ {
		entry.RepairHistory = append(entry.RepairHistory, corelib.SkillRepairRecord{Explanation: "old"})
	}

	ApplyRepair(entry, &RepairResult{
		Repaired:    true,
		Explanation: "new",
		NewSteps:    []SkillYAMLStep{{Action: "bash"}},
	})

	if len(entry.RepairHistory) != 5 {
		t.Fatalf("history len = %d, want 5", len(entry.RepairHistory))
	}
	if entry.RepairHistory[4].Explanation != "new" {
		t.Fatalf("last history = %#v, want newest repair", entry.RepairHistory[4])
	}
}

func TestRepairMetadataHelpersIgnoreNilSkill(t *testing.T) {
	MarkRepairVerified(nil)
	ResetRepairCount(nil)
}
