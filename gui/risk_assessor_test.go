package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib"
)

func TestAssessSkill_EmptySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name:  "empty-skill",
		Steps: []corelib.NLSkillStep{},
	}
	// agent-created: standard assessment, no modification
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != security.RiskLow {
		t.Errorf("expected security.RiskLow for empty steps with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_ReadOnlySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "read-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// agent-created: standard assessment, read-only stays low
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != security.RiskLow {
		t.Errorf("expected security.RiskLow for read-only steps with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_WriteStep_Medium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "write-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, write step = medium
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium for write step with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_DangerousKeyword_Critical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "dangerous-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "rm -rf /"}},
		},
	}
	// community escalates critical → critical (already max)
	result := ra.AssessSkill(skill, "community")
	if result.Level != security.RiskCritical {
		t.Errorf("expected security.RiskCritical for dangerous keyword, got %s", result.Level)
	}
}

// --- Backward compatibility: "official" maps to "trusted" ---

func TestAssessSkill_OfficialTrust_DowngradesMediumToLow(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "official-write-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// "official" → "trusted", trusted caps at medium; write step = medium, stays medium
	result := ra.AssessSkill(skill, "official")
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (official/trusted caps at medium), got %s", result.Level)
	}
}

func TestAssessSkill_OfficialDoesNotDowngradeCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "critical-official",
		Steps: []corelib.NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// "official" → "trusted", trusted caps at medium; critical → medium
	result := ra.AssessSkill(skill, "official")
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (official/trusted caps critical to medium), got %s", result.Level)
	}
}

// --- Backward compatibility: "unknown" maps to "community" ---

func TestAssessSkill_UnknownTrust_UpgradesLowToMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "unknown-read-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// "unknown" → "community", community escalates low → medium
	result := ra.AssessSkill(skill, "unknown")
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (unknown/community escalates low to medium), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "community trust level: low escalated to medium" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected community escalation factor in factors list")
	}
}

func TestAssessSkill_UnknownDoesNotUpgradeMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "unknown-write",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// "unknown" → "community", community escalates medium → high
	result := ra.AssessSkill(skill, "unknown")
	if result.Level != security.RiskHigh {
		t.Errorf("expected security.RiskHigh (unknown/community escalates medium to high), got %s", result.Level)
	}
}

func TestAssessSkill_TakesHighestRisk(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "mixed-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, highest = medium (write step)
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (highest of read=low, write=medium), got %s", result.Level)
	}
}

// --- 4-tier trust level hierarchy tests ---

func TestAssessSkill_BuiltinTrust_CapsAtLow(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "builtin-write-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// builtin caps at low regardless of pattern matches
	result := ra.AssessSkill(skill, security.TrustLevelBuiltin)
	if result.Level != security.RiskLow {
		t.Errorf("expected security.RiskLow (builtin caps at low), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "builtin trust level: medium capped to low" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected builtin cap factor in factors list")
	}
}

func TestAssessSkill_BuiltinTrust_CapsEvenCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "builtin-dangerous",
		Steps: []corelib.NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// builtin caps at low even for critical risk
	result := ra.AssessSkill(skill, security.TrustLevelBuiltin)
	if result.Level != security.RiskLow {
		t.Errorf("expected security.RiskLow (builtin caps critical to low), got %s", result.Level)
	}
}

func TestAssessSkill_TrustedTrust_CapsAtMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "trusted-dangerous",
		Steps: []corelib.NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// trusted caps at medium
	result := ra.AssessSkill(skill, security.TrustLevelTrusted)
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (trusted caps critical to medium), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "trusted trust level: critical capped to medium" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected trusted cap factor in factors list")
	}
}

func TestAssessSkill_AgentCreatedTrust_NoModification(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "agent-write-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, no modification
	result := ra.AssessSkill(skill, security.TrustLevelAgentCreated)
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (agent-created, no modification), got %s", result.Level)
	}
}

func TestAssessSkill_CommunityTrust_EscalatesRisk(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "community-write-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// community escalates medium → high
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != security.RiskHigh {
		t.Errorf("expected security.RiskHigh (community escalates medium to high), got %s", result.Level)
	}
	found := false
	for _, f := range result.Factors {
		if f == "community trust level: medium escalated to high" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected community escalation factor in factors list")
	}
}

func TestAssessSkill_CommunityTrust_EscalatesLowToMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "community-read-skill",
		Steps: []corelib.NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// community escalates low → medium
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (community escalates low to medium), got %s", result.Level)
	}
}

func TestAssessSkill_CommunityTrust_CriticalStaysCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "community-dangerous",
		Steps: []corelib.NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// community: critical stays critical (already max)
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != security.RiskCritical {
		t.Errorf("expected security.RiskCritical (community, critical stays critical), got %s", result.Level)
	}
}

// --- NormalizeTrustLevel tests ---

// --- Regression: safe-tool category + community trust interaction ---
// This test covers the exact bug that blocked weather-query in standard mode:
// bash action (medium) + community trust escalation (medium→high) must be
// capped back to medium by the safe-tool category check.
func TestAssessSkill_SafeToolCategory_CommunityTrust_CappedAtMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "weather-query",
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python weather.py weekly --lat 39.9 --lng 116.4"}},
		},
	}
	// community trust would escalate bash's medium→high, but safe-tool "weather"
	// must cap it back to medium.
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != security.RiskMedium {
		t.Errorf("expected security.RiskMedium (safe-tool 'weather' caps community-escalated high to medium), got %s", result.Level)
	}
}

func TestAssessSkill_SafeToolCategory_CommunityTrust_NonSafeStillHigh(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &corelib.NLSkillEntry{
		Name: "my-custom-tool",
		Steps: []corelib.NLSkillStep{
			// Use capitalized "Bash" to match GUI's isWriteOrExecuteTool switch.
			// (The corelib version uses substring matching on lowercase, so "bash" works there.)
			{Action: "Bash", Params: map[string]interface{}{"command": "python script.py"}},
		},
	}
	// Non-safe skill: community escalates medium→high, no safe-tool cap.
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != security.RiskHigh {
		t.Errorf("expected security.RiskHigh (non-safe skill, community escalates medium to high), got %s", result.Level)
	}
}

func TestNormalizeTrustLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"official", security.TrustLevelTrusted},
		{"unknown", security.TrustLevelCommunity},
		{security.TrustLevelBuiltin, security.TrustLevelBuiltin},
		{security.TrustLevelTrusted, security.TrustLevelTrusted},
		{security.TrustLevelAgentCreated, security.TrustLevelAgentCreated},
		{security.TrustLevelCommunity, security.TrustLevelCommunity},
		{"something-else", "something-else"},
		{"", ""},
	}
	for _, tt := range tests {
		got := security.NormalizeTrustLevel(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeTrustLevel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
