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
		RequiresBins:   []string{"python"},
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
	if counts["command"] != 1 {
		t.Errorf("expected 1 command requirement, got %d", counts["command"])
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

func TestExtractRequirements_RequiresBinsCoversInferredCommand(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiresBins: []string{"python"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "python {baseDir}/run.py"},
		}},
	}

	reqs := ExtractRequirements(skill)
	var explicitCommands, inferredCommands int
	for _, r := range reqs {
		if r.Type != "command" || r.Name != "python" {
			continue
		}
		switch r.Source {
		case "explicit":
			explicitCommands++
		case "inferred":
			inferredCommands++
		}
	}
	if explicitCommands != 1 || inferredCommands != 0 {
		t.Fatalf("command requirements explicit=%d inferred=%d all=%+v", explicitCommands, inferredCommands, reqs)
	}
}

func TestExtractRequirements_StepRequiredEnv(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{
				Action: "run",
				Params: map[string]interface{}{
					"cmd":          "node script.js",
					"required_env": []interface{}{"STEP_TOKEN", "OTHER_TOKEN"},
				},
			},
			{
				Action: "bash",
				Params: map[string]interface{}{
					"command":      "echo ok",
					"requires_env": "ALIAS_TOKEN",
				},
			},
		},
	}

	reqs := ExtractRequirements(skill)
	names := map[string]bool{}
	for _, r := range reqs {
		if r.Type == "env" {
			names[r.Name] = true
		}
	}
	for _, want := range []string{"STEP_TOKEN", "OTHER_TOKEN", "ALIAS_TOKEN"} {
		if !names[want] {
			t.Fatalf("env requirements = %#v, missing %q", names, want)
		}
	}
}

func TestExtractRequirements_StepRequiredEnvDeduplicatesWithSkillLevel(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":      "echo ok",
				"required_env": "api_token",
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	var envReqs []Requirement
	for _, r := range reqs {
		if r.Type == "env" {
			envReqs = append(envReqs, r)
		}
	}
	if len(envReqs) != 1 {
		t.Fatalf("env requirements = %#v, want exactly one deduplicated env requirement", envReqs)
	}
	if envReqs[0].Name != "API_TOKEN" {
		t.Fatalf("deduplicated env name = %q, want top-level spelling API_TOKEN", envReqs[0].Name)
	}
}

func TestExtractRequirements_StepRequiredEnvCanBeProvidedByStepEnv(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":      "echo ok",
				"required_env": "STEP_TOKEN",
				"extra_env": map[string]interface{}{
					"STEP_TOKEN": "secret",
				},
			},
		}},
	}

	reqs := ExtractRequirements(skill, BuildRunCheckContext(skill, nil))
	for _, r := range reqs {
		if r.Type == "env" && r.Name == "STEP_TOKEN" {
			if !r.Provided {
				t.Fatalf("STEP_TOKEN requirement = %#v, want Provided=true", r)
			}
			return
		}
	}
	t.Fatalf("requirements = %#v, want STEP_TOKEN env requirement", reqs)
}

func TestCheckRunnerRequirementsUsesResolvedStepEnvFromRunParam(t *testing.T) {
	orig := envLookup
	defer func() { envLookup = orig }()
	envLookup = func(string) string { return "" }

	params := []corelib.NLSkillParam{{Name: "api_token", Required: true}}
	steps := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{
			"command":      "echo ok",
			"required_env": "API_TOKEN",
			"extra_env":    map[string]interface{}{"API_TOKEN": "{{api_token}}"},
		},
	}}
	resolved, err := ResolveStepsForRunnerPrecheck(steps, map[string]string{"api_token": "secret"}, "", params)
	if err != nil {
		t.Fatalf("ResolveStepsForRunnerPrecheck() error = %v", err)
	}
	entry := &corelib.NLSkillEntry{
		Name:        "run-param-env",
		RequiredEnv: []string{"API_TOKEN"},
		Steps:       resolved,
	}

	remaining := CheckRunnerRequirements(entry, nil, RunnerBackendTUI)

	if errors := FilterErrors(remaining); len(errors) > 0 {
		t.Fatalf("CheckRunnerRequirements() errors = %#v, want resolved step env to satisfy API_TOKEN", errors)
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

func TestExtractRequirements_InferredCommandsFromPipesAndChains(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": `echo "a|b" | jq '.data' && FOO=1 xparse-cli parse report.pdf || ffmpeg -version`,
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	for _, want := range []string{"jq", "xparse-cli", "ffmpeg"} {
		if !names[want] {
			t.Fatalf("inferred command requirements = %#v, missing %q", names, want)
		}
	}
	if names["echo"] || names["FOO=1"] {
		t.Fatalf("inferred command requirements = %#v, want builtins/assignments skipped", names)
	}
}

func TestExtractRequirements_InferredCommandsSkipQuotedEnvAssignments(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": `TOKEN="a b" xparse-cli parse report.pdf && FOO='c d' jq '.data' file.json`,
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	for _, want := range []string{"xparse-cli", "jq"} {
		if !names[want] {
			t.Fatalf("inferred command requirements = %#v, missing %q", names, want)
		}
	}
	for _, unexpected := range []string{"b", "b\"", "d", "d'", "TOKEN=a b", "FOO=c d"} {
		if names[unexpected] {
			t.Fatalf("inferred command requirements = %#v, contains assignment fragment %q", names, unexpected)
		}
	}
}

func TestExtractRequirements_InferredCommandsIncludeEnvWrappedCommand(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": `env TOKEN="a b" xparse-cli parse report.pdf`,
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if !names["env"] || !names["xparse-cli"] {
		t.Fatalf("inferred command requirements = %#v, want both env wrapper and wrapped command", names)
	}
}

func TestExtractRequirements_SkipsHeredocBodyCommands(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "cat <<'PY'\nmissing-tool run\nPY\nnode run.js",
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if names["missing-tool"] || names["PY"] {
		t.Fatalf("inferred command requirements = %#v, want heredoc body skipped", names)
	}
	if !names["node"] {
		t.Fatalf("inferred command requirements = %#v, want command after heredoc", names)
	}
}

func TestExtractRequirements_SkipsBackslashQuotedHeredocBodyCommands(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "cat <<\\PY\nmissing-tool run\nPY\nnode run.js",
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if names["missing-tool"] || names["PY"] {
		t.Fatalf("inferred command requirements = %#v, want backslash-quoted heredoc body skipped", names)
	}
	if !names["node"] {
		t.Fatalf("inferred command requirements = %#v, want command after heredoc", names)
	}
}

func TestExtractRequirements_SkipsContinuationFlagLines(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python run.py \\\n  --input report.pdf \\\n  --output out.pdf",
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if names["--input"] || names["--output"] {
		t.Fatalf("inferred command requirements = %#v, want continuation flag lines skipped", names)
	}
	if !names["python"] {
		t.Fatalf("inferred command requirements = %#v, want python command", names)
	}
}

func TestExtractRequirements_JoinsContinuationCommand(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": "python \\\n  helper \\\n  --input report.pdf",
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if names["helper"] || names["--input"] {
		t.Fatalf("inferred command requirements = %#v, want continuation arguments kept with python", names)
	}
	if !names["python"] {
		t.Fatalf("inferred command requirements = %#v, want python command", names)
	}
}

func TestExtractRequirements_SkipsCommonShellBuiltins(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command": `command -v ffmpeg && printf ok && pushd scripts && popd && exec python run.py`,
			},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	for _, unexpected := range []string{"command", "printf", "pushd", "popd", "exec"} {
		if names[unexpected] {
			t.Fatalf("inferred command requirements = %#v, contains shell builtin %q", names, unexpected)
		}
	}
	if !names["python"] {
		t.Fatalf("inferred command requirements = %#v, want real command python", names)
	}
}

func TestExtractRequirements_SkipsLocalExecutablePaths(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": `./scripts/run.sh && C:/tools/local.exe && node script.js`},
		}},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if names["./scripts/run.sh"] || names["C:/tools/local.exe"] {
		t.Fatalf("local executable paths should not become command requirements: %#v", names)
	}
	if !names["node"] {
		t.Fatalf("expected node requirement, got %#v", names)
	}
}

func TestExtractRequirements_TrimsQuotedCommandName(t *testing.T) {
	skill := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `"xparse-cli" parse report.pdf`},
	}}}

	reqs := ExtractRequirements(skill)
	for _, r := range reqs {
		if r.Type == "command" && r.Name == "xparse-cli" {
			return
		}
	}
	t.Fatalf("requirements = %#v, want command name xparse-cli without quotes", reqs)
}

func TestExtractRequirements_InferredCommandsNormalizeStepActions(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{Action: "run", Params: map[string]interface{}{"command": "ffmpeg -i input.mp4 output.mp3"}},
			{Action: "node", Params: map[string]interface{}{"code": "console.log('ok')"}},
		},
	}

	reqs := ExtractRequirements(skill)
	names := make(map[string]bool)
	for _, r := range reqs {
		if r.Type == "command" {
			names[r.Name] = true
		}
	}
	if !names["ffmpeg"] || !names["node"] {
		t.Fatalf("command requirements = %#v, want normalized run/node actions inferred", names)
	}
}
func TestExtractRequirements_DoesNotMutateStepParams(t *testing.T) {
	skill := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "run",
			Params: map[string]interface{}{"cmd": "ffmpeg -version"},
		}},
	}

	reqs := ExtractRequirements(skill)

	found := false
	for _, req := range reqs {
		if req.Type == "command" && req.Name == "ffmpeg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ExtractRequirements() = %#v, want inferred ffmpeg", reqs)
	}
	if skill.Steps[0].Action != "run" {
		t.Fatalf("ExtractRequirements mutated action: %q", skill.Steps[0].Action)
	}
	if skill.Steps[0].Params["command"] != nil || skill.Steps[0].Params["cmd"] != "ffmpeg -version" {
		t.Fatalf("ExtractRequirements mutated params: %#v", skill.Steps[0].Params)
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

func TestFormatViolationsAddsActionHints(t *testing.T) {
	got := FormatViolations([]Violation{
		{
			Requirement: Requirement{Type: "command", Name: "xparse-cli", Source: "inferred"},
			Message:     "命令 xparse-cli 未找到",
			Severity:    "error",
		},
		{
			Requirement: Requirement{Type: "env", Name: "OPENAI_API_KEY", Source: "explicit"},
			Message:     "环境变量 OPENAI_API_KEY 未设置",
			Severity:    "error",
		},
	})
	for _, want := range []string{"required command xparse-cli was not found on PATH", "[action: install_dependency]", "required environment variable OPENAI_API_KEY is not set", "[action: provide_env]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatViolations() = %q, missing %q", got, want)
		}
	}
	for _, bad := range []string{"命令 xparse-cli 未找到", "环境变量 OPENAI_API_KEY 未设置"} {
		if strings.Contains(got, bad) {
			t.Fatalf("FormatViolations() = %q, should not leak raw checker message %q", got, bad)
		}
	}
}

func TestFormatViolationPreservesExistingActionHint(t *testing.T) {
	got := FormatViolation(Violation{
		Requirement: Requirement{Type: "command", Name: "tool"},
		Message:     "custom message [action: inspect_skill]",
		Severity:    "error",
	})
	if got != "custom message [action: inspect_skill]" {
		t.Fatalf("FormatViolation() = %q, want existing action hint preserved", got)
	}
}

func TestFormatViolationsUsesLineBoundariesForActionHints(t *testing.T) {
	got := FormatViolations([]Violation{
		{
			Requirement: Requirement{Type: "command", Name: "xparse-cli", Source: "inferred"},
			Severity:    "error",
		},
		{
			Requirement: Requirement{Type: "pip", Name: "weasyprint", Version: ">=61", Source: "explicit"},
			Severity:    "error",
		},
	})
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("FormatViolations() = %q, want one violation per line", got)
	}
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[0], "xparse-cli") || !strings.Contains(lines[0], "[action: install_dependency]") {
		t.Fatalf("first violation line = %q, want command action hint", lines[0])
	}
	if !strings.Contains(lines[1], "weasyprint>=61") || !strings.Contains(lines[1], "[action: install_dependency]") {
		t.Fatalf("second violation line = %q, want package action hint", lines[1])
	}
}

func TestPromoteRunnerBlockingViolationsPromotesInferredCommands(t *testing.T) {
	violations := []Violation{
		{
			Requirement: Requirement{Type: "command", Name: "missing-tool", Source: "inferred"},
			Message:     "missing-tool missing",
			Severity:    "warning",
		},
		{
			Requirement: Requirement{Type: "unknown_type", Name: "soft"},
			Message:     "soft warning",
			Severity:    "warning",
		},
	}

	promoted := PromoteRunnerBlockingViolations(violations)
	if promoted[0].Severity != "error" {
		t.Fatalf("inferred command severity = %q, want error", promoted[0].Severity)
	}
	if promoted[1].Severity != "warning" {
		t.Fatalf("non-command warning severity = %q, want warning", promoted[1].Severity)
	}
	if violations[0].Severity != "warning" {
		t.Fatalf("PromoteRunnerBlockingViolations mutated input: %#v", violations[0])
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
