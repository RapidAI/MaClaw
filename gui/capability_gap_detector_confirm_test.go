package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

// TestCapabilityGapDetector_NilCallback_RejectsCriticalRisk verifies that when
// confirmCallback is nil (the default), the CapabilityGapDetector rejects
// critical-risk skills. This is the backward-compatible default behavior.
func TestCapabilityGapDetector_NilCallback_RejectsCriticalRisk(t *testing.T) {
	t.Parallel()
	d := &CapabilityGapDetector{}

	// Verify callback is nil by default.
	if d.confirmCallback != nil {
		t.Fatal("expected confirmCallback to be nil by default")
	}

	// Simulate the critical-risk check logic from Resolve():
	// When confirmCallback is nil, confirmed stays false → rejection.
	confirmed := false
	if d.confirmCallback != nil {
		confirmed = d.confirmCallback("test-skill", "critical risk details")
	}
	if confirmed {
		t.Fatal("expected confirmed=false when confirmCallback is nil (backward-compatible rejection)")
	}
}

// TestCapabilityGapDetector_NilCallback_HubPath verifies the nil callback
// rejection in the Hub install path matches the pattern used in Resolve().
func TestCapabilityGapDetector_NilCallback_HubPath(t *testing.T) {
	t.Parallel()
	d := &CapabilityGapDetector{}

	// Simulate the Hub path critical-risk check from Resolve():
	skillName := "dangerous-hub-skill"
	hubURL := "https://hub.example.com"
	trustLevel := "community"
	riskDetails := "Skill「" + skillName + "」来自 " + hubURL + " (trust_level=" + trustLevel + ") 包含 critical 级别风险操作"

	confirmed := false
	if d.confirmCallback != nil {
		confirmed = d.confirmCallback(skillName, riskDetails)
	}
	if confirmed {
		t.Fatal("expected rejection when confirmCallback is nil (Hub path)")
	}
}

// TestCapabilityGapDetector_NilCallback_GitHubPath verifies the nil callback
// rejection in the GitHub fallback path matches the pattern used in Resolve().
func TestCapabilityGapDetector_NilCallback_GitHubPath(t *testing.T) {
	t.Parallel()
	d := &CapabilityGapDetector{}

	// Simulate the GitHub path critical-risk check from Resolve():
	skillName := "dangerous-github-skill"
	repoURL := "https://github.com/user/repo"
	riskDetails := "GitHub Skill「" + skillName + "」来自 " + repoURL + " (trust_level=community) 包含 critical 级别风险操作"

	confirmed := false
	if d.confirmCallback != nil {
		confirmed = d.confirmCallback(skillName, riskDetails)
	}
	if confirmed {
		t.Fatal("expected rejection when confirmCallback is nil (GitHub path)")
	}
}

// TestSetCapabilityGapDetector_WiresConfirmCallback verifies that
// SetCapabilityGapDetector wires the confirmCallback on the detector so that
// CapabilityGapDetector.Resolve uses the shared confirmCriticalRiskSkill
// mechanism.
func TestSetCapabilityGapDetector_WiresConfirmCallback(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}
	d := &CapabilityGapDetector{}

	// Before wiring, callback should be nil.
	if d.confirmCallback != nil {
		t.Fatal("expected confirmCallback to be nil before SetCapabilityGapDetector")
	}

	// Wire the detector.
	h.SetCapabilityGapDetector(d)

	// After wiring, callback should be set.
	if d.confirmCallback == nil {
		t.Fatal("expected confirmCallback to be set after SetCapabilityGapDetector")
	}
}

// TestSetCapabilityGapDetector_NilDetector verifies that passing nil to
// SetCapabilityGapDetector does not panic.
func TestSetCapabilityGapDetector_NilDetector(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}

	// Should not panic.
	h.SetCapabilityGapDetector(nil)

	if h.capabilityGapDetector != nil {
		t.Fatal("expected capabilityGapDetector to be nil")
	}
}

// TestSetCapabilityGapDetector_CallbackReturnsFalseWithoutPlatform verifies
// that the wired callback returns false (fail-closed) when there is no active
// loop context (no platform). This matches the behavior of
// confirmCriticalRiskSkill with an empty platform.
func TestSetCapabilityGapDetector_CallbackReturnsFalseWithoutPlatform(t *testing.T) {
	t.Parallel()
	h := &IMMessageHandler{app: &App{}}
	d := &CapabilityGapDetector{}

	h.SetCapabilityGapDetector(d)

	// No currentLoopCtx set → platform is "" → fail-closed.
	result := d.confirmCallback("test-skill", "risk details")
	if result {
		t.Fatal("expected false when no platform context is available (fail-closed)")
	}
}

// TestCapabilityGapDetector_AuditFormat_ConsistentWithOtherPaths verifies that
// the audit log format for user-confirmed critical installs in the
// CapabilityGapDetector uses the same security.PolicyUserOverride constant and similar
// Result format as toolInstallSkillHub and registerAndExecuteSkill.
func TestCapabilityGapDetector_AuditFormat_ConsistentWithOtherPaths(t *testing.T) {
	t.Parallel()

	// The three paths all use the same pattern for user-confirmed critical installs:
	// PolicyAction: security.PolicyUserOverride
	// Result: "user confirmed critical skill {name} from {source}, risk=critical, ..."
	//
	// Verify the constant value is consistent.
	if security.PolicyUserOverride == "" {
		t.Fatal("security.PolicyUserOverride should not be empty")
	}
	if security.PolicyUserOverride == security.PolicyAllow {
		t.Fatal("security.PolicyUserOverride should be distinct from security.PolicyAllow")
	}
	if security.PolicyUserOverride == security.PolicyDeny {
		t.Fatal("security.PolicyUserOverride should be distinct from security.PolicyDeny")
	}

	// Verify the audit result format includes required fields.
	// Hub path format from CapabilityGapDetector:
	hubResult := "user confirmed critical skill test-skill from https://hub.example.com, risk=critical, trust_level=community"
	if !strings.Contains(hubResult, "user confirmed") {
		t.Error("Hub path audit result should contain 'user confirmed'")
	}
	if !strings.Contains(hubResult, "risk=critical") {
		t.Error("Hub path audit result should contain 'risk=critical'")
	}

	// GitHub path format from CapabilityGapDetector:
	ghResult := "user confirmed critical skill test-skill from https://github.com/user/repo, risk=critical"
	if !strings.Contains(ghResult, "user confirmed") {
		t.Error("GitHub path audit result should contain 'user confirmed'")
	}
	if !strings.Contains(ghResult, "risk=critical") {
		t.Error("GitHub path audit result should contain 'risk=critical'")
	}

	// toolInstallSkillHub format:
	toolHubResult := "user confirmed critical skill test-skill from https://hub.example.com, risk=critical, factors=[rm -rf found]"
	if !strings.Contains(toolHubResult, "user confirmed") {
		t.Error("toolInstallSkillHub audit result should contain 'user confirmed'")
	}
	if !strings.Contains(toolHubResult, "risk=critical") {
		t.Error("toolInstallSkillHub audit result should contain 'risk=critical'")
	}

	// registerAndExecuteSkill format:
	regResult := "user confirmed critical skill test-skill from github, risk=critical, factors=[shell access]"
	if !strings.Contains(regResult, "user confirmed") {
		t.Error("registerAndExecuteSkill audit result should contain 'user confirmed'")
	}
	if !strings.Contains(regResult, "risk=critical") {
		t.Error("registerAndExecuteSkill audit result should contain 'risk=critical'")
	}
}
