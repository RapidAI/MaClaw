package skill

import (
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestIsPromotable_BelowThreshold(t *testing.T) {
	promoter := NewNudgePromoter(nil, nil, nil, "")
	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:    3, // below default 5
		SuccessRate: 0.95,
		Confidence:  0.80,
	}
	if promoter.IsPromotable(candidate) {
		t.Error("should not be promotable: evidence < 5")
	}
}

func TestIsPromotable_MeetsThreshold(t *testing.T) {
	promoter := NewNudgePromoter(nil, nil, nil, "")
	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:    6,
		SuccessRate: 0.92,
		Confidence:  0.80,
	}
	if !promoter.IsPromotable(candidate) {
		t.Error("should be promotable: all thresholds met")
	}
}

func TestIsPromotable_LowSuccessRate(t *testing.T) {
	promoter := NewNudgePromoter(nil, nil, nil, "")
	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:    10,
		SuccessRate: 0.85, // below 0.90
		Confidence:  0.80,
	}
	if promoter.IsPromotable(candidate) {
		t.Error("should not be promotable: success rate < 0.90")
	}
}

func TestIsPromotable_LowConfidence(t *testing.T) {
	promoter := NewNudgePromoter(nil, nil, nil, "")
	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:    10,
		SuccessRate: 0.95,
		Confidence:  0.60, // below 0.75
	}
	if promoter.IsPromotable(candidate) {
		t.Error("should not be promotable: confidence < 0.75")
	}
}

func TestFilterPromotable(t *testing.T) {
	candidates := []tool.ToolSkillNudgeCandidate{
		{Evidence: 2, SuccessRate: 0.95, Confidence: 0.80, SuggestedName: "low-evidence"},
		{Evidence: 6, SuccessRate: 0.95, Confidence: 0.80, SuggestedName: "good"},
		{Evidence: 10, SuccessRate: 0.70, Confidence: 0.90, SuggestedName: "low-success"},
		{Evidence: 8, SuccessRate: 0.92, Confidence: 0.85, SuggestedName: "also-good"},
	}
	result := FilterPromotable(candidates, PromotionThreshold{})
	if len(result) != 2 {
		t.Fatalf("expected 2 promotable, got %d", len(result))
	}
	if result[0].SuggestedName != "good" || result[1].SuggestedName != "also-good" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestTryPromote_BelowThreshold_NoLLMNeeded(t *testing.T) {
	promoter := NewNudgePromoter(nil, nil, nil, t.TempDir())
	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:    2,
		SuccessRate: 0.80,
		Confidence:  0.50,
	}
	result, err := promoter.TryPromote(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Promoted {
		t.Error("should not promote below threshold")
	}
}

func TestTryPromote_MeetsThreshold_LLMGeneratesSkill(t *testing.T) {
	llm := &mockLLMRepairer{
		response: `name: test-skill
description: A test skill
triggers: [test, example]
required_args: [input]
steps:
  - action: bash
    params:
      command: "echo {{input}}"`,
	}
	registrar := &mockSkillRegistrar{}
	promoter := NewNudgePromoter(llm, nil, registrar, t.TempDir())

	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:      6,
		SuccessRate:   0.95,
		Confidence:    0.80,
		SuggestedName: "test-promoted",
		ToolSequence:  []string{"bash", "write_file"},
		QueryTokens:   []string{"test", "example"},
		TaskType:      "testing",
		ContextKey:    "task:testing",
		Description:   "A test workflow",
	}

	result, err := promoter.TryPromote(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Promoted {
		t.Errorf("expected promotion, got: %s", result.Explanation)
	}
	if result.SkillName != "test-promoted" {
		t.Errorf("skill name = %q, want %q", result.SkillName, "test-promoted")
	}
	if registrar.registered == nil {
		t.Fatal("expected skill to be registered")
	}
	if registrar.registered.Source != "auto_discovered" {
		t.Errorf("source = %q, want %q", registrar.registered.Source, "auto_discovered")
	}
	if registrar.registered.Status != "active" {
		t.Errorf("status = %q, want %q", registrar.registered.Status, "active")
	}
}

func TestTryPromote_SecurityBlocked(t *testing.T) {
	llm := &mockLLMRepairer{response: "name: risky\nsteps:\n  - action: bash\n    params:\n      command: rm -rf /"}
	staging := &mockStagingValidator{blockErr: "contains dangerous command"}
	promoter := NewNudgePromoter(llm, staging, nil, t.TempDir())

	candidate := tool.ToolSkillNudgeCandidate{
		Evidence:      10,
		SuccessRate:   0.95,
		Confidence:    0.85,
		SuggestedName: "risky-skill",
		ToolSequence:  []string{"bash"},
	}

	result, err := promoter.TryPromote(candidate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Promoted {
		t.Error("should not promote: security blocked")
	}
	if !result.Blocked {
		t.Error("should be marked as blocked")
	}
}

func TestGeneratePromotedSkillName(t *testing.T) {
	tests := []struct {
		candidate tool.ToolSkillNudgeCandidate
		expected  string
	}{
		{
			candidate: tool.ToolSkillNudgeCandidate{TaskType: "pdf_conversion", ToolSequence: []string{"bash", "write_file"}},
			expected:  "pdf_conversion-bash-to-write_file",
		},
		{
			candidate: tool.ToolSkillNudgeCandidate{ToolSequence: []string{"web_fetch", "web_fetch"}},
			expected:  "web_fetch",
		},
		{
			candidate: tool.ToolSkillNudgeCandidate{},
			expected:  "auto-skill",
		},
	}
	for _, tt := range tests {
		got := generatePromotedSkillName(tt.candidate)
		if got != tt.expected {
			t.Errorf("generatePromotedSkillName(%+v) = %q, want %q", tt.candidate, got, tt.expected)
		}
	}
}

// --- Mocks ---

type mockLLMRepairer struct {
	response string
	err      error
}

func (m *mockLLMRepairer) ChatCall(messages []map[string]string) (string, error) {
	return m.response, m.err
}

func (m *mockLLMRepairer) IsConfigured() bool {
	return true
}

type mockStagingValidator struct {
	blockErr string
}

func (m *mockStagingValidator) ScanSkillDir(skillDir string) error {
	if m.blockErr != "" {
		return fmt.Errorf("%s", m.blockErr)
	}
	return nil
}

type mockSkillRegistrar struct {
	registered *corelib.NLSkillEntry
}

func (m *mockSkillRegistrar) RegisterSkill(entry *corelib.NLSkillEntry) error {
	m.registered = entry
	return nil
}
