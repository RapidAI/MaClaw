package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestAssessSkill_EmptySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name:  "empty-skill",
		Steps: []NLSkillStep{},
	}
	// agent-created: standard assessment, no modification
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow for empty steps with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_ReadOnlySteps(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "read-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// agent-created: standard assessment, read-only stays low
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow for read-only steps with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_WriteStep_Medium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, write step = medium
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium for write step with agent-created trust, got %s", result.Level)
	}
}

func TestAssessSkill_DangerousKeyword_Critical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "dangerous-skill",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "rm -rf /"}},
		},
	}
	// community escalates critical → critical (already max)
	result := ra.AssessSkill(skill, "community")
	if result.Level != RiskCritical {
		t.Errorf("expected RiskCritical for dangerous keyword, got %s", result.Level)
	}
}

// --- Backward compatibility: "official" maps to "trusted" ---

func TestAssessSkill_OfficialTrust_DowngradesMediumToLow(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "official-write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// "official" → "trusted", trusted caps at medium; write step = medium, stays medium
	result := ra.AssessSkill(skill, "official")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (official/trusted caps at medium), got %s", result.Level)
	}
}

func TestAssessSkill_OfficialDoesNotDowngradeCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "critical-official",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// "official" → "trusted", trusted caps at medium; critical → medium
	result := ra.AssessSkill(skill, "official")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (official/trusted caps critical to medium), got %s", result.Level)
	}
}

// --- Backward compatibility: "unknown" maps to "community" ---

func TestAssessSkill_UnknownTrust_UpgradesLowToMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "unknown-read-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// "unknown" → "community", community escalates low → medium
	result := ra.AssessSkill(skill, "unknown")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (unknown/community escalates low to medium), got %s", result.Level)
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
	skill := &NLSkillEntry{
		Name: "unknown-write",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// "unknown" → "community", community escalates medium → high
	result := ra.AssessSkill(skill, "unknown")
	if result.Level != RiskHigh {
		t.Errorf("expected RiskHigh (unknown/community escalates medium to high), got %s", result.Level)
	}
}

func TestAssessSkill_TakesHighestRisk(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "mixed-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, highest = medium (write step)
	result := ra.AssessSkill(skill, "agent-created")
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (highest of read=low, write=medium), got %s", result.Level)
	}
}

// --- 4-tier trust level hierarchy tests ---

func TestAssessSkill_BuiltinTrust_CapsAtLow(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "builtin-write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// builtin caps at low regardless of pattern matches
	result := ra.AssessSkill(skill, security.TrustLevelBuiltin)
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow (builtin caps at low), got %s", result.Level)
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
	skill := &NLSkillEntry{
		Name: "builtin-dangerous",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// builtin caps at low even for critical risk
	result := ra.AssessSkill(skill, security.TrustLevelBuiltin)
	if result.Level != RiskLow {
		t.Errorf("expected RiskLow (builtin caps critical to low), got %s", result.Level)
	}
}

func TestAssessSkill_TrustedTrust_CapsAtMedium(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "trusted-dangerous",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// trusted caps at medium
	result := ra.AssessSkill(skill, security.TrustLevelTrusted)
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (trusted caps critical to medium), got %s", result.Level)
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
	skill := &NLSkillEntry{
		Name: "agent-write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// agent-created: standard assessment, no modification
	result := ra.AssessSkill(skill, security.TrustLevelAgentCreated)
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (agent-created, no modification), got %s", result.Level)
	}
}

func TestAssessSkill_CommunityTrust_EscalatesRisk(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "community-write-skill",
		Steps: []NLSkillStep{
			{Action: "Write", Params: map[string]interface{}{"path": "/tmp/out.txt"}},
		},
	}
	// community escalates medium → high
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != RiskHigh {
		t.Errorf("expected RiskHigh (community escalates medium to high), got %s", result.Level)
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
	skill := &NLSkillEntry{
		Name: "community-read-skill",
		Steps: []NLSkillStep{
			{Action: "Read", Params: map[string]interface{}{"path": "/tmp/file.txt"}},
		},
	}
	// community escalates low → medium
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != RiskMedium {
		t.Errorf("expected RiskMedium (community escalates low to medium), got %s", result.Level)
	}
}

func TestAssessSkill_CommunityTrust_CriticalStaysCritical(t *testing.T) {
	ra := &RiskAssessor{}
	skill := &NLSkillEntry{
		Name: "community-dangerous",
		Steps: []NLSkillStep{
			{Action: "Bash", Params: map[string]interface{}{"command": "sudo rm -rf /"}},
		},
	}
	// community: critical stays critical (already max)
	result := ra.AssessSkill(skill, security.TrustLevelCommunity)
	if result.Level != RiskCritical {
		t.Errorf("expected RiskCritical (community, critical stays critical), got %s", result.Level)
	}
}

// --- NormalizeTrustLevel tests ---

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
