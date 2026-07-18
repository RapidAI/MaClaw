package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEnrichRepairParamContract_DetectsMissingAndUnknown(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name: "convert",
		Params: []corelib.NLSkillParam{
			{Name: "input", Required: true, Aliases: []string{"file", "path"}},
			{Name: "format", Required: false, Default: "pdf"},
		},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "convert {{input}} --format {{format}}"},
		}},
	}
	ctx := NewRepairContext(entry, map[string]string{
		"file":   "a.md", // alias of input
		"extra":  "x",    // unknown
		"format": "pdf",
	})
	if len(ctx.DeclaredParams) == 0 {
		t.Fatal("expected declared params")
	}
	if len(ctx.MissingRequired) != 0 {
		t.Fatalf("missing=%v want empty (file aliases to input)", ctx.MissingRequired)
	}
	if len(ctx.UnknownArgs) != 1 || ctx.UnknownArgs[0] != "extra" {
		t.Fatalf("unknown=%v", ctx.UnknownArgs)
	}
	if ctx.ResolvedByAlias["file"] != "input" {
		t.Fatalf("aliasHits=%v", ctx.ResolvedByAlias)
	}
	if !strings.Contains(ctx.ParamContractNote, "alias") && !strings.Contains(ctx.ParamContractNote, "schema") {
		t.Fatalf("note=%q", ctx.ParamContractNote)
	}

	ctx2 := NewRepairContext(entry, map[string]string{"format": "html"})
	if len(ctx2.MissingRequired) != 1 || ctx2.MissingRequired[0] != "input" {
		t.Fatalf("missing=%v want [input]", ctx2.MissingRequired)
	}
}

func TestAttemptRepairWithContext_PromptIncludesParamContract(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:         "x",
		Description:  "test",
		UsageCount:   5,
		SuccessCount: 1,
		LastError:    FormatErrorForLLM(ClassifiedError{Class: ErrMissingParam, UserMessage: "missing input", Repairable: true}),
		Params:       []corelib.NLSkillParam{{Name: "input", Required: true}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}
	var captured []map[string]string
	llm := &stubRepairLLM{respond: `{"repaired":false,"explanation":"param mismatch","should_disable":false}`, onCall: func(msgs []map[string]string) {
		captured = msgs
	}}
	ctx := NewRepairContext(entry, map[string]string{"file": "a.txt"})
	_, err := AttemptRepairWithContext(llm, entry, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(captured) < 2 {
		t.Fatalf("expected chat messages, got %d", len(captured))
	}
	user := captured[1]["content"]
	if !strings.Contains(user, "Parameter contract") || !strings.Contains(user, "input") {
		t.Fatalf("user prompt missing contract:\n%s", user)
	}
	if !strings.Contains(captured[0]["content"], "PARAMETER CONTRACT") {
		t.Fatalf("system prompt missing contract rules:\n%s", captured[0]["content"])
	}
}

type stubRepairLLM struct {
	respond string
	onCall  func([]map[string]string)
}

func (s *stubRepairLLM) IsConfigured() bool { return true }
func (s *stubRepairLLM) ChatCall(messages []map[string]string) (string, error) {
	if s.onCall != nil {
		s.onCall(messages)
	}
	return s.respond, nil
}

func TestSanitizeRepairResult_NormalizesShellTool(t *testing.T) {
	entry := &corelib.NLSkillEntry{Name: "wget"}
	result := &RepairResult{
		Repaired:    true,
		Explanation: "use shell_tool",
		NewSteps: []SkillYAMLStep{{
			Action: "shell_tool",
			Params: map[string]interface{}{"command": "wget -O out.pdf https://example.com/a.pdf"},
		}},
	}
	if err := SanitizeRepairResult(entry, result); err != nil {
		t.Fatalf("SanitizeRepairResult: %v", err)
	}
	if len(result.NewSteps) != 1 || result.NewSteps[0].Action != "bash" {
		t.Fatalf("NewSteps = %#v, want bash", result.NewSteps)
	}
}

func TestApplyRepair_RejectsUnsupportedAction(t *testing.T) {
	// After normalize, a step with no command cannot become bash and is rejected.
	entry := &corelib.NLSkillEntry{Name: "bad"}
	applied := ApplyRepair(entry, &RepairResult{
		Repaired:    true,
		Explanation: "invent action",
		NewSteps:    []SkillYAMLStep{{Action: "totally_unknown_action", Params: map[string]interface{}{}}},
	})
	if applied {
		t.Fatal("ApplyRepair should reject unsupported actions")
	}
}

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

func TestCanForceAttemptRepair_SkipsUsageThreshold(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:        "x",
		Status:      "active",
		Source:      "manual",
		UsageCount:  1, // below SelfRepairThreshold
		SuccessCount: 1,
		LastError:   "[class: command_not_found] missing foo",
	}
	if ShouldAttemptRepair(entry) {
		t.Fatal("ShouldAttemptRepair should be false with low usage and non-hub source")
	}
	if !CanForceAttemptRepair(entry) {
		t.Fatal("CanForceAttemptRepair should allow when base safety gates pass")
	}
	entry.LastError = ""
	if CanForceAttemptRepair(entry) {
		t.Fatal("empty LastError should block force repair")
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
