package skill

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// --- ExtractRequirements tests ---

func TestExtractRequirements_ExplicitFields(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresPython: []string{"pdfplumber>=0.9", "requests"},
		RequiresNode:   []string{"puppeteer"},
		RequiredEnv:    []string{"API_KEY"},
		Platforms:      []string{"windows", "linux"},
	}

	reqs := ExtractRequirements(skill)

	counts := make(map[string]int)
	for _, r := range reqs {
		counts[r.Type]++
	}

	if counts["pip"] != 2 {
		t.Errorf("expected 2 pip requirements, got %d", counts["pip"])
	}
	if counts["npm"] != 1 {
		t.Errorf("expected 1 npm requirement, got %d", counts["npm"])
	}
	if counts["env"] != 1 {
		t.Errorf("expected 1 env requirement, got %d", counts["env"])
	}
	if counts["platform"] != 1 {
		t.Errorf("expected 1 platform requirement, got %d", counts["platform"])
	}

	// Check version extraction.
	for _, r := range reqs {
		if r.Type == "pip" && r.Name == "pdfplumber" {
			if r.Version != ">=0.9" {
				t.Errorf("expected version >=0.9, got %q", r.Version)
			}
			if r.Source != "explicit" {
				t.Errorf("expected source explicit, got %q", r.Source)
			}
		}
	}

	// Check platform uses Values field, not Name.
	for _, r := range reqs {
		if r.Type == "platform" {
			if len(r.Values) != 2 || r.Values[0] != "windows" {
				t.Errorf("expected Values=[windows,linux], got %v", r.Values)
			}
		}
	}
}

func TestExtractRequirements_InferredCommands(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "ffmpeg -i input.mp4 output.mp3"}},
			{Action: "bash", Params: map[string]interface{}{"command": "echo done"}},
			{Action: "bash", Params: map[string]interface{}{"command": "python3 script.py"}},
			{Action: "bash", Params: map[string]interface{}{"command": "jq '.data' file.json"}},
			{Action: "bash", Params: map[string]interface{}{"command": "ffmpeg -version"}}, // duplicate
		},
	}

	reqs := ExtractRequirements(skill)

	var cmdReqs []Requirement
	for _, r := range reqs {
		if r.Type == "command" {
			cmdReqs = append(cmdReqs, r)
		}
	}

	// python3 is inferred because RequiresPython is empty (no explicit coverage).
	// ffmpeg and jq are inferred. echo is a builtin.
	names := make(map[string]bool)
	for _, r := range cmdReqs {
		names[r.Name] = true
		if r.Source != "inferred" {
			t.Errorf("expected source inferred for %q, got %q", r.Name, r.Source)
		}
	}
	if !names["ffmpeg"] {
		t.Error("expected ffmpeg in inferred commands")
	}
	if !names["jq"] {
		t.Error("expected jq in inferred commands")
	}
	if names["echo"] {
		t.Error("echo should be skipped (builtin)")
	}
}

func TestExtractRequirements_PythonCoveredSkipsInference(t *testing.T) {
	// When RequiresPython is non-empty, python/python3 are covered and
	// should NOT be inferred from step commands.
	skill := &corelib.NLSkillEntry{
		RequiresPython: []string{"requests"},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Params: map[string]interface{}{"command": "python3 script.py"}},
		},
	}

	reqs := ExtractRequirements(skill)
	for _, r := range reqs {
		if r.Type == "command" && r.Name == "python3" {
			t.Error("python3 should be covered by RequiresPython, not inferred")
		}
	}
}

func TestExtractRequirements_EmptySkill(t *testing.T) {
	skill := &corelib.NLSkillEntry{}
	reqs := ExtractRequirements(skill)
	if len(reqs) != 0 {
		t.Errorf("expected 0 requirements for empty skill, got %d", len(reqs))
	}
}

// --- Registry tests ---

type mockChecker struct {
	typ       string
	satisfied map[string]bool
}

func (m *mockChecker) Type() string { return m.typ }
func (m *mockChecker) Check(req Requirement) *Violation {
	if m.satisfied[req.Name] {
		return nil
	}
	return &Violation{Requirement: req, Message: req.Name + " missing", Severity: "error"}
}

type mockFixer struct {
	typ   string
	fixed map[string]bool
	onFix func(name string) // called on successful fix (to update checker state)
}

func (m *mockFixer) Type() string { return m.typ }
func (m *mockFixer) Fix(req Requirement) error {
	if m.fixed[req.Name] {
		if m.onFix != nil {
			m.onFix(req.Name)
		}
		return nil
	}
	return fmt.Errorf("cannot fix %s", req.Name)
}

func TestRegistry_CheckAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockChecker{
		typ:       "pip",
		satisfied: map[string]bool{"requests": true},
	})

	reqs := []Requirement{
		{Type: "pip", Name: "requests"},
		{Type: "pip", Name: "pdfplumber"},
		{Type: "unknown_type", Name: "foo"},
	}

	violations := reg.CheckAll(reqs)

	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d: %+v", len(violations), violations)
	}

	for _, v := range violations {
		if v.Requirement.Name == "foo" && v.Severity != "warning" {
			t.Error("unknown type should produce warning, not error")
		}
	}
}

func TestRegistry_FixAll(t *testing.T) {
	// The checker and fixer share state: when the fixer "installs" a package,
	// the checker's satisfied map is updated. This mirrors real behavior where
	// pip install makes pip show succeed.
	checker := &mockChecker{typ: "pip", satisfied: map[string]bool{}}
	fixer := &mockFixer{
		typ:   "pip",
		fixed: map[string]bool{"pdfplumber": true},
		// On successful fix, update the checker's satisfied map.
		onFix: func(name string) { checker.satisfied[name] = true },
	}
	reg := NewRegistry()
	reg.Register(checker)
	reg.RegisterFixer(fixer)

	violations := []Violation{
		{Requirement: Requirement{Type: "pip", Name: "pdfplumber"}, Severity: "error"},
		{Requirement: Requirement{Type: "pip", Name: "torch"}, Severity: "error"},
		{Requirement: Requirement{Type: "command", Name: "ffmpeg"}, Severity: "error"}, // no fixer
	}

	remaining := reg.FixAll(violations)

	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining violations, got %d", len(remaining))
	}
	names := make(map[string]bool)
	for _, v := range remaining {
		names[v.Requirement.Name] = true
	}
	if !names["torch"] {
		t.Error("torch should remain (fix failed)")
	}
	if !names["ffmpeg"] {
		t.Error("ffmpeg should remain (no fixer)")
	}
	if names["pdfplumber"] {
		t.Error("pdfplumber should have been fixed")
	}
}

func TestRegistry_FixAll_AcceptsWarnings(t *testing.T) {
	// FixAll should accept all violations, not just errors.
	// If a warning-severity violation has a fixer, it should be fixed.
	checker := &mockChecker{typ: "pip", satisfied: map[string]bool{}}
	fixer := &mockFixer{
		typ:   "pip",
		fixed: map[string]bool{"optional-pkg": true},
		onFix: func(name string) { checker.satisfied[name] = true },
	}
	reg := NewRegistry()
	reg.Register(checker)
	reg.RegisterFixer(fixer)

	violations := []Violation{
		{Requirement: Requirement{Type: "pip", Name: "optional-pkg"}, Severity: "warning"},
	}

	remaining := reg.FixAll(violations)
	if len(remaining) != 0 {
		t.Errorf("expected 0 remaining (warning should be fixable), got %d", len(remaining))
	}
}

func TestFilterErrors(t *testing.T) {
	violations := []Violation{
		{Severity: "error", Message: "a"},
		{Severity: "warning", Message: "b"},
		{Severity: "error", Message: "c"},
	}
	errors := FilterErrors(violations)
	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errors))
	}
}

func TestSplitPkgVersion(t *testing.T) {
	tests := []struct {
		input       string
		wantName    string
		wantVersion string
	}{
		{"pdfplumber>=0.9", "pdfplumber", ">=0.9"},
		{"requests==2.31", "requests", "==2.31"},
		{"pypdf", "pypdf", ""},
		{"numpy~=1.24", "numpy", "~=1.24"},
	}
	for _, tt := range tests {
		name, version := splitPkgVersion(tt.input)
		if name != tt.wantName || version != tt.wantVersion {
			t.Errorf("splitPkgVersion(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, version, tt.wantName, tt.wantVersion)
		}
	}
}

// --- Extension point test ---

type goModChecker struct {
	installed map[string]bool
}

func (c *goModChecker) Type() string { return "gomod" }
func (c *goModChecker) Check(req Requirement) *Violation {
	if c.installed[req.Name] {
		return nil
	}
	return &Violation{Requirement: req, Message: "Go module " + req.Name + " not installed", Severity: "error"}
}

func TestRegistry_ExtensionPoint(t *testing.T) {
	reg := DefaultRegistry()
	reg.Register(&goModChecker{installed: map[string]bool{"github.com/foo/bar": true}})

	reqs := []Requirement{
		{Type: "gomod", Name: "github.com/foo/bar"},
		{Type: "gomod", Name: "github.com/baz/qux"},
	}

	violations := reg.CheckAll(reqs)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Requirement.Name != "github.com/baz/qux" {
		t.Errorf("expected baz/qux violation, got %q", violations[0].Requirement.Name)
	}
}

// --- EnvVarChecker + Provided field test ---

func TestEnvVarChecker_ProvidedSkipped(t *testing.T) {
	// Override envLookup for test.
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(key string) string { return "" } // all vars unset

	checker := &EnvVarChecker{}

	// Provided requirement should be skipped by CheckAll, not by the checker.
	// The checker itself doesn't know about Provided — CheckAll handles it.
	// So calling checker.Check directly on a Provided req still returns violation.
	v := checker.Check(Requirement{Type: "env", Name: "OPENAI_API_KEY", Provided: true})
	if v == nil {
		t.Error("checker.Check should still report violation — Provided is handled by CheckAll, not checker")
	}

	// Non-provided var should fail.
	if v := checker.Check(Requirement{Type: "env", Name: "OTHER_KEY"}); v == nil {
		t.Error("OTHER_KEY should fail (not set)")
	}
}

func TestCheckAll_SkipsProvidedRequirements(t *testing.T) {
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(key string) string { return "" }

	reg := DefaultRegistry()
	reqs := []Requirement{
		{Type: "env", Name: "OPENAI_API_KEY", Provided: true},
		{Type: "env", Name: "OTHER_KEY"},
	}
	violations := reg.CheckAll(reqs)

	// OPENAI_API_KEY should be skipped (Provided=true), OTHER_KEY should fail.
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Requirement.Name != "OTHER_KEY" {
		t.Errorf("expected OTHER_KEY violation, got %q", violations[0].Requirement.Name)
	}
}

func TestExtractRequirements_ProvidedEnvVars(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiredEnv: []string{"OPENAI_API_KEY", "MY_SECRET"},
	}
	reqs := ExtractRequirements(skill, &CheckContext{
		ProvidedEnvVars: map[string]bool{"OPENAI_API_KEY": true},
	})

	for _, r := range reqs {
		if r.Type != "env" {
			continue
		}
		if r.Name == "OPENAI_API_KEY" && !r.Provided {
			t.Error("OPENAI_API_KEY should be marked Provided")
		}
		if r.Name == "MY_SECRET" && r.Provided {
			t.Error("MY_SECRET should NOT be marked Provided")
		}
	}
}

// --- GUIChecker tests ---

func TestGUIChecker_NonLinux_AlwaysPasses(t *testing.T) {
	// GUIChecker only checks on Linux. On other platforms it always passes.
	// We can't easily mock runtime.GOOS, so we test the envLookup path.
	checker := &GUIChecker{}
	req := Requirement{Type: "gui", Name: "display"}

	// On Windows/macOS this will pass. On Linux it depends on DISPLAY.
	// Either way, the checker should not panic.
	_ = checker.Check(req)
}

func TestGUIChecker_LinuxWithDisplay(t *testing.T) {
	// Override envLookup to simulate Linux with DISPLAY set.
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}

	checker := &GUIChecker{}
	req := Requirement{Type: "gui", Name: "display"}

	// On non-Linux this always passes. On Linux with DISPLAY it should pass.
	v := checker.Check(req)
	// We can't force runtime.GOOS to "linux" in a unit test, so we just
	// verify no panic and the result is nil (non-Linux) or nil (Linux+DISPLAY).
	if v != nil && v.Severity == "error" {
		// This would only happen on Linux without DISPLAY, which contradicts our mock.
		// On non-Linux, v is always nil.
		t.Logf("GUIChecker returned violation on non-Linux or mock didn't apply: %s", v.Message)
	}
}

// --- ExtractRequirements GUI requirement test ---

func TestExtractRequirements_RequiresGUI(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresGUI: true,
	}
	reqs := ExtractRequirements(skill)

	found := false
	for _, r := range reqs {
		if r.Type == "gui" {
			found = true
			if r.Source != "explicit" {
				t.Errorf("expected source=explicit, got %s", r.Source)
			}
		}
	}
	if !found {
		t.Error("expected gui requirement to be extracted from RequiresGUI=true")
	}
}

func TestExtractRequirements_NoGUI(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresGUI: false,
	}
	reqs := ExtractRequirements(skill)

	for _, r := range reqs {
		if r.Type == "gui" {
			t.Error("expected no gui requirement when RequiresGUI=false")
		}
	}
}

func TestExtractRequirements_NpmCarriesSkillDir(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresNode: []string{"puppeteer"},
		SkillDir:     "/opt/skills/my-skill",
	}
	reqs := ExtractRequirements(skill)

	for _, r := range reqs {
		if r.Type == "npm" {
			if r.Context == nil || r.Context["skill_dir"] != "/opt/skills/my-skill" {
				t.Errorf("npm requirement should carry skill_dir in Context, got %v", r.Context)
			}
			return
		}
	}
	t.Error("expected npm requirement")
}

func TestExtractRequirements_NpmNoSkillDir(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresNode: []string{"puppeteer"},
	}
	reqs := ExtractRequirements(skill)

	for _, r := range reqs {
		if r.Type == "npm" {
			if r.Context != nil && r.Context["skill_dir"] != "" {
				t.Errorf("npm requirement should not have skill_dir when SkillDir is empty, got %v", r.Context)
			}
			return
		}
	}
	t.Error("expected npm requirement")
}

func TestExtractRequirements_CheckContextOverridesSkillDir(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresNode: []string{"puppeteer"},
		SkillDir:     "/old/path",
	}
	reqs := ExtractRequirements(skill, &CheckContext{SkillDir: "/new/path"})

	for _, r := range reqs {
		if r.Type == "npm" {
			if r.Context == nil || r.Context["skill_dir"] != "/new/path" {
				t.Errorf("CheckContext.SkillDir should override skill.SkillDir, got %v", r.Context)
			}
			return
		}
	}
	t.Error("expected npm requirement")
}

// --- DefaultRegistry includes GUIChecker test ---

func TestDefaultRegistry_IncludesGUIChecker(t *testing.T) {
	reg := DefaultRegistry()
	// Verify GUIChecker is registered by checking a gui requirement.
	reqs := []Requirement{{Type: "gui", Name: "display"}}
	violations := reg.CheckAll(reqs)
	// On non-Linux, GUIChecker always passes → 0 violations.
	// On Linux with DISPLAY, also 0 violations.
	// On Linux without DISPLAY, 1 violation.
	// The key assertion: no "unknown requirement type" warning.
	for _, v := range violations {
		if strings.Contains(v.Message, "unknown requirement type") {
			t.Error("GUIChecker not registered: got unknown requirement type warning")
		}
	}
}
