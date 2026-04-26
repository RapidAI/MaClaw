package skill

import (
	"fmt"
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

// --- EnvVarChecker SkipNames test ---

func TestEnvVarChecker_SkipNames(t *testing.T) {
	// Override envLookup for test.
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(key string) string { return "" } // all vars unset

	checker := &EnvVarChecker{SkipNames: map[string]bool{"OPENAI_API_KEY": true}}

	// Skipped var should pass.
	if v := checker.Check(Requirement{Type: "env", Name: "OPENAI_API_KEY"}); v != nil {
		t.Error("OPENAI_API_KEY should be skipped")
	}

	// Non-skipped var should fail.
	if v := checker.Check(Requirement{Type: "env", Name: "OTHER_KEY"}); v == nil {
		t.Error("OTHER_KEY should fail (not set, not skipped)")
	}
}
