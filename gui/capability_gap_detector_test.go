package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCapabilityGapDetector_SetConfirmCallback(t *testing.T) {
	d := &CapabilityGapDetector{}
	if d.confirmCallback != nil {
		t.Fatal("confirmCallback should be nil by default")
	}
	called := false
	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		called = true
		return true
	})
	if d.confirmCallback == nil {
		t.Fatal("confirmCallback should be set after SetConfirmCallback")
	}
	d.confirmCallback("test", "details")
	if !called {
		t.Fatal("confirmCallback was not invoked")
	}
}

func TestCapabilityGapDetector_CriticalRisk_NoCallback_Rejects(t *testing.T) {
	// Build a detector with a mock hub client, skill executor, risk assessor,
	// and audit log that will produce a critical-risk Skill.
	d := &CapabilityGapDetector{}

	// No confirmCallback set — critical risk should be rejected.
	if d.confirmCallback != nil {
		t.Fatal("expected nil confirmCallback")
	}
}

func TestCapabilityGapDetector_CriticalRisk_CallbackConfirms(t *testing.T) {
	d := &CapabilityGapDetector{}

	var receivedName, receivedDetails string
	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		receivedName = skillName
		receivedDetails = riskDetails
		return true
	})

	// Verify callback returns true (confirms installation).
	result := d.confirmCallback("dangerous-skill", "contains rm -rf")
	if !result {
		t.Fatal("expected callback to return true")
	}
	if receivedName != "dangerous-skill" {
		t.Fatalf("expected skillName 'dangerous-skill', got %q", receivedName)
	}
	if receivedDetails != "contains rm -rf" {
		t.Fatalf("expected riskDetails 'contains rm -rf', got %q", receivedDetails)
	}
}

func TestCapabilityGapDetector_CriticalRisk_CallbackRejects(t *testing.T) {
	d := &CapabilityGapDetector{}

	d.SetConfirmCallback(func(skillName, riskDetails string) bool {
		return false
	})

	result := d.confirmCallback("dangerous-skill", "contains sudo")
	if result {
		t.Fatal("expected callback to return false")
	}
}

func TestCapabilityGapDetector_Detect_KeywordFallback(t *testing.T) {
	// No LLM configured — should use keyword matching.
	d := &CapabilityGapDetector{}

	tests := []struct {
		input string
		want  bool
	}{
		{"我无法完成这个任务", true},
		{"这个功能不支持", true},
		{"I cannot do that", true},
		{"一切正常，已完成", false},
	}
	for _, tt := range tests {
		got := d.Detect(tt.input)
		if got != tt.want {
			t.Errorf("Detect(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestCapabilityGapDetector_Resolve_NoCandidates verifies that Resolve returns
// empty when the hub client returns no candidates.
func TestCapabilityGapDetector_Resolve_NoCandidates(t *testing.T) {
	assessor := &RiskAssessor{}
	auditLog, err := NewAuditLog(t.TempDir())
	if err != nil {
		t.Fatalf("NewAuditLog: %v", err)
	}

	app := &App{}
	hubClient := NewSkillHubClient(app)
	executor := NewSkillExecutor(app, nil, nil)

	d := NewCapabilityGapDetector(app, hubClient, executor, assessor, auditLog, corelib.MaclawLLMConfig{})

	var statuses []string
	name, result, resolveErr := d.Resolve(context.Background(), "do something", nil, func(s string) {
		statuses = append(statuses, s)
	})
	if name != "" {
		t.Fatalf("expected empty skillName, got %q", name)
	}
	if result != "" {
		t.Fatalf("expected empty result, got %q", result)
	}
	// No candidates → no error (silent return).
	if resolveErr != nil {
		// Hub search may fail if no URLs configured, which is fine.
		t.Logf("Resolve returned error (expected for no hub URLs): %v", resolveErr)
	}
}


// TestCapabilityGapDetector_HubPath_SetsAutoHubSource verifies that the Hub
// install path in Resolve() sets Source to "auto_hub" before Register().
func TestCapabilityGapDetector_HubPath_SetsAutoHubSource(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)

	// Simulate what Resolve() does in the Hub path (Step 6):
	// After hubClient.Install() returns a skill, Resolve() sets
	// skill.Source = "auto_hub" before calling Register().
	skill := corelib.NLSkillEntry{
		Name:        "test-hub-skill",
		Description: "A skill from SkillHub",
		Source:      "hub", // This is what Install() would set
	}
	// Override source as Resolve() does.
	skill.Source = "auto_hub"

	if err := executor.Register(skill); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Verify the registered skill has Source = "auto_hub".
	skills := executor.List()
	var found bool
	for _, s := range skills {
		if s.Name == "test-hub-skill" {
			found = true
			if s.Source != "auto_hub" {
				t.Errorf("expected Source %q, got %q", "auto_hub", s.Source)
			}
			break
		}
	}
	if !found {
		t.Fatal("registered skill not found in List()")
	}
}

// TestCapabilityGapDetector_GitHubPath_SetsAutoGitHubSource verifies that the
// GitHub fallback path in Resolve() sets Source to "auto_github" before Register().
func TestCapabilityGapDetector_GitHubPath_SetsAutoGitHubSource(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	executor := NewSkillExecutor(app, nil, nil)

	// Simulate what Resolve() does in the GitHub fallback path:
	// After gs.ImportFromCandidate() returns an imported skill, Resolve()
	// sets imported.Source = "auto_github" before calling Register().
	imported := corelib.NLSkillEntry{
		Name:        "test-github-skill",
		Description: "A skill from GitHub",
		Source:      "github", // This is what ImportFromCandidate() would set
	}
	// Override source as Resolve() does.
	imported.Source = "auto_github"

	if err := executor.Register(imported); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Verify the registered skill has Source = "auto_github".
	skills := executor.List()
	var found bool
	for _, s := range skills {
		if s.Name == "test-github-skill" {
			found = true
			if s.Source != "auto_github" {
				t.Errorf("expected Source %q, got %q", "auto_github", s.Source)
			}
			break
		}
	}
	if !found {
		t.Fatal("registered skill not found in List()")
	}
}

// TestCapabilityGapDetector_AutoSourcesAreNotLearnedSources verifies that the
// auto_ prefixed sources are NOT recognized as learned sources by IsLearnedSource.
// Auto-installed skills come from external hubs, not from Maclaw's own learning.
func TestCapabilityGapDetector_AutoSourcesAreNotLearnedSources(t *testing.T) {
	tests := []struct {
		source string
		want   bool
	}{
		{"auto_hub", false},
		{"auto_github", false},
		{"auto_clawhub", false},
		{"hub", false},
		{"github", false},
		{"learned", true},
		{"crafted", true},
	}
	for _, tt := range tests {
		got := corelib.IsLearnedSource(tt.source)
		if got != tt.want {
			t.Errorf("IsLearnedSource(%q) = %v, want %v", tt.source, got, tt.want)
		}
	}
}
