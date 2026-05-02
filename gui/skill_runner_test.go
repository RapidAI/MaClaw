package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestSkillRunnerExecuteStepWithContext_CallMCPToolResolvesName(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.LocalMCPServers = []corelib.LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	runner := NewSkillRunner(&SkillExecutor{app: app, mcpRegistry: app.mcpRegistry})
	result, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"server_id": "brave-search",
			"tool_name": "ping",
			"arguments": map[string]interface{}{},
		},
	}, "")
	if err != nil {
		t.Fatalf("executeStepWithContext() error = %v", err)
	}
	if strings.TrimSpace(result) != "{}" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestExpandPortableHomeVarsUsesSlashHome(t *testing.T) {
	home := `C:\Users\tester`
	cmd := `python "$HOME/scripts/run.py" --config=${HOME}\config\skill.json --root $HOME --name=$HOME_CONFIG`
	got := expandPortableHomeVars(cmd, home)
	wantHome := filepath.ToSlash(home)
	withoutUnrelatedVar := strings.ReplaceAll(got, "$HOME_CONFIG", "")
	if strings.Contains(withoutUnrelatedVar, "$HOME") || strings.Contains(withoutUnrelatedVar, "${HOME}") {
		t.Fatalf("portable HOME placeholders were not expanded: %q", got)
	}
	for _, want := range []string{wantHome + "/scripts/run.py", wantHome + "/config\\skill.json", "--root " + wantHome} {
		if !strings.Contains(got, want) {
			t.Fatalf("expanded command %q missing %q", got, want)
		}
	}
	if !strings.Contains(got, "$HOME_CONFIG") {
		t.Fatalf("unrelated HOME-prefixed variable should be preserved: %q", got)
	}
}

func TestMergeRequiredEnvParamPreservesStepEnv(t *testing.T) {
	params := map[string]interface{}{
		"required_env": []interface{}{"STEP_KEY", "SHARED_KEY"},
	}
	mergeRequiredEnvParam(params, []string{"SHARED_KEY", "SKILL_KEY"})

	got, ok := params["required_env"].([]interface{})
	if !ok {
		t.Fatalf("required_env type = %T, want []interface{}", params["required_env"])
	}
	want := []string{"STEP_KEY", "SHARED_KEY", "SKILL_KEY"}
	if len(got) != len(want) {
		t.Fatalf("required_env = %#v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("required_env[%d] = %#v, want %q; all=%#v", i, got[i], name, got)
		}
	}
}

func TestMergeExtraEnvParamPreservesStepValues(t *testing.T) {
	params := map[string]interface{}{
		"extra_env": map[string]interface{}{
			"STEP_ONLY": "1",
			"SHARED":    "from-step",
		},
	}
	mergeExtraEnvParam(params, map[string]string{"SHARED": "from-run", "RUN_ONLY": "2"})

	got, ok := params["extra_env"].(map[string]interface{})
	if !ok {
		t.Fatalf("extra_env type = %T, want map[string]interface{}", params["extra_env"])
	}
	if got["SHARED"] != "from-step" || got["STEP_ONLY"] != "1" || got["RUN_ONLY"] != "2" {
		t.Fatalf("extra_env merge = %#v", got)
	}
}

func TestSkillRunnerStartRun_RejectsSkillWithoutExecutableSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:   "doc-only",
		Status: "active",
		Steps:  nil,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	_, err = runner.StartRun("doc-only", nil)
	if err == nil || !strings.Contains(err.Error(), "has no executable steps") {
		t.Fatalf("expected no executable steps error, got %v", err)
	}
}

func TestSkillRunnerExecuteStepWithContext_CraftToolAcceptsLegacyInstructions(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "legacy task",
		},
	}, "")
	if err != nil && strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("craft_tool should not fall through unknown action: %v", err)
	}
}

func TestSkillRunnerExecuteStepWithContext_CraftToolMissingTask(t *testing.T) {
	runner := NewSkillRunner(&SkillExecutor{app: &App{}})
	_, err := runner.executeStepWithContext(context.Background(), "run-test", corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}

func TestIsInstructionOnlySkillEntry(t *testing.T) {
	if !isInstructionOnlySkillEntry(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"instructions": "# PPTX Generator\n\nGenerate slides."},
	}}}) {
		t.Fatal("expected craft_tool with instructions to require artifact verification")
	}
	if isInstructionOnlySkillEntry(&corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"task": "generate slides"},
	}}}) {
		t.Fatal("expected structured craft_tool task not to be treated as instruction-only")
	}
}

func TestIsInstructionOnlySkillStatus(t *testing.T) {
	status := &SkillRunStatus{Steps: []StepResult{{
		Action: "craft_tool",
		Status: "success",
		Output: "📝 脚本语言: python\n📁 脚本路径: /tmp/tool.py\n\n✅ 脚本执行成功",
	}}}
	if !isInstructionOnlySkillStatus(status) {
		t.Fatal("expected craft_tool output with script metadata to require artifact verification")
	}
}

func TestNormalizeSkillRunVars_ArgsOverrideLegacy(t *testing.T) {
	got := normalizeSkillRunVars(map[string]interface{}{
		"args":   map[string]interface{}{"input": "new-in", "output": "new-out"},
		"input":  "old-in",
		"output": "old-out",
	})
	if got["input"] != "new-in" || got["output"] != "new-out" {
		t.Fatalf("normalizeSkillRunVars() = %#v, want args values to win", got)
	}
}

func TestNormalizeSkillRunVars_CoercesNonStringArgs(t *testing.T) {
	got := normalizeSkillRunVars(map[string]interface{}{
		"args": map[string]interface{}{"count": 3, "enabled": true, "format": "pdf"},
	})
	// Non-string values are coerced via fmt.Sprintf (aligned with TUI behavior).
	if len(got) != 3 || got["format"] != "pdf" || got["count"] != "3" || got["enabled"] != "true" {
		t.Fatalf("normalizeSkillRunVars() = %#v, want all args coerced to strings", got)
	}
}

func TestResolveSkillStep_ReplacesNestedPlaceholders(t *testing.T) {
	skillDir := filepath.Join("base", "skill")
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":     "printf '%s %s' {{input}} ${output}",
			"working_dir": "nested",
			"arguments": map[string]interface{}{
				"path": "{{input}}",
			},
			"items": []interface{}{"${output}", 7},
		},
	}, map[string]string{"input": "report.md", "output": "out.pdf"}, skillDir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "report.md") || !strings.Contains(command, "out.pdf") {
		t.Fatalf("command = %q, want placeholders replaced", command)
	}
	workDir, _ := resolved.Params["working_dir"].(string)
	wantDir := filepath.Clean(filepath.Join(skillDir, "nested"))
	if workDir != wantDir {
		t.Fatalf("working_dir = %q, want %q", workDir, wantDir)
	}
	args, _ := resolved.Params["arguments"].(map[string]interface{})
	if path, _ := args["path"].(string); !strings.Contains(path, "report.md") {
		t.Fatalf("nested argument path = %q, want replaced input", path)
	}
	items, _ := resolved.Params["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	if item0, _ := items[0].(string); !strings.Contains(item0, "out.pdf") {
		t.Fatalf("items[0] = %q, want replaced output", item0)
	}
}

func TestResolveSkillStep_CraftToolInheritsRunArgs(t *testing.T) {
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "Generate slides.",
		},
	}, map[string]string{
		"output": "out/live_test_deck.pptx",
		"topic":  "Quarterly product review",
	}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, _ := resolved.Params["output"].(string); got != "out/live_test_deck.pptx" {
		t.Fatalf("output = %q, want propagated run arg", got)
	}
	if got, _ := resolved.Params["topic"].(string); got != "Quarterly product review" {
		t.Fatalf("topic = %q, want propagated run arg", got)
	}
}

func TestResolveSkillStep_StripsMissingOptionalPlaceholder(t *testing.T) {
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node ./tool.mjs {{input}} {{output}}",
		},
	}, map[string]string{"input": "report.md"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if strings.Contains(command, "{{output}}") || strings.Contains(command, "${output}") {
		t.Fatalf("command = %q, want unresolved output placeholder stripped", command)
	}
	if !strings.Contains(command, "report.md") {
		t.Fatalf("command = %q, want input retained", command)
	}
}

func TestSubstituteSkillVariables_LeavesUnknownPlaceholderUntouched(t *testing.T) {
	got := substituteSkillVariables("echo {{missing}}", map[string]string{"input": "ignored"})
	if got != "echo " {
		t.Fatalf("substituteSkillVariables() = %q, want unresolved placeholder removed", got)
	}
}

func TestQuoteSkillInputForShell_EscapesQuotes(t *testing.T) {
	input := "a'b"
	got := quoteSkillInputForShell(input)
	if runtime.GOOS == "windows" {
		// On Windows, quoteSkillInputForShell uses double-quotes for cmd.exe.
		if got != `"a'b"` {
			t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `"a'b"`)
		}
		return
	}
	if got != `'a'"'"'b'` {
		t.Fatalf("quoteSkillInputForShell() = %q, want %q", got, `'a'"'"'b'`)
	}
}

// TestSubstituteSkillVariables_DedupsQuotedPlaceholder verifies that when a
// placeholder is already wrapped in quotes in the template (e.g. "{{text}}"),
// substituteSkillVariables does not produce double-quoting.
func TestSubstituteSkillVariables_DedupsQuotedPlaceholder(t *testing.T) {
	vars := map[string]string{"text": "Hello, how are you today?"}
	quoted := quoteSkillInputForShell("Hello, how are you today?")

	// Template with placeholder already in double quotes — common in SKILL.md
	got := substituteSkillVariables(`python translate.py --text "{{text}}"`, vars)
	want := `python translate.py --text ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() double-quoted dedup:\n  got  = %q\n  want = %q", got, want)
	}

	// Template with placeholder NOT in quotes — should still get quoted
	got2 := substituteSkillVariables(`python translate.py --text {{text}}`, vars)
	want2 := `python translate.py --text ` + quoted
	if got2 != want2 {
		t.Fatalf("substituteSkillVariables() bare placeholder:\n  got  = %q\n  want = %q", got2, want2)
	}

	// Template with placeholder in single quotes
	got3 := substituteSkillVariables(`python translate.py --text '{{text}}'`, vars)
	want3 := `python translate.py --text ` + quoted
	if got3 != want3 {
		t.Fatalf("substituteSkillVariables() single-quoted dedup:\n  got  = %q\n  want = %q", got3, want3)
	}
}

// TestSubstituteSkillVariables_DollarBraceDedup verifies dedup also works
// for ${key} style placeholders.
func TestSubstituteSkillVariables_DollarBraceDedup(t *testing.T) {
	vars := map[string]string{"city": "New York"}
	quoted := quoteSkillInputForShell("New York")

	got := substituteSkillVariables(`curl --data "${city}"`, vars)
	want := `curl --data ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() ${} dedup:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_MixedQuotedAndBare verifies that a command
// containing both quoted and bare occurrences of the same placeholder handles
// each occurrence correctly.
func TestSubstituteSkillVariables_MixedQuotedAndBare(t *testing.T) {
	vars := map[string]string{"text": "hello world"}
	quoted := quoteSkillInputForShell("hello world")

	got := substituteSkillVariables(`echo "{{text}}" && log {{text}}`, vars)
	want := `echo ` + quoted + ` && log ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() mixed quoted+bare:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBracePlaceholder verifies that {key}
// single-brace placeholders are also replaced (bug fix for manage_skill path).
func TestSubstituteSkillVariables_SingleBracePlaceholder(t *testing.T) {
	vars := map[string]string{"input": "/path/to/image.png", "output": "/path/to/out.pdf"}
	got := substituteSkillVariables("python convert.py {input} {output}", vars)
	quoted1 := quoteSkillInputForShell(vars["input"])
	quoted2 := quoteSkillInputForShell(vars["output"])
	want := "python convert.py " + quoted1 + " " + quoted2
	if got != want {
		t.Fatalf("substituteSkillVariables() single-brace:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBraceDoesNotBreakDoubleBrace verifies
// that {{key}} is still correctly replaced when {key} support is present.
func TestSubstituteSkillVariables_SingleBraceDoesNotBreakDoubleBrace(t *testing.T) {
	vars := map[string]string{"input": "test.png"}
	got := substituteSkillVariables("echo {{input}} && echo {input}", vars)
	quoted := quoteSkillInputForShell("test.png")
	want := "echo " + quoted + " && echo " + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() mixed double+single brace:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestSubstituteSkillVariables_SingleBraceQuotedDedup verifies that quoted
// single-brace placeholders like "{input}" are handled without double-quoting.
func TestSubstituteSkillVariables_SingleBraceQuotedDedup(t *testing.T) {
	vars := map[string]string{"input": "/tmp/my file.png"}
	quoted := quoteSkillInputForShell(vars["input"])

	got := substituteSkillVariables(`python convert.py "{input}"`, vars)
	want := `python convert.py ` + quoted
	if got != want {
		t.Fatalf("substituteSkillVariables() single-brace quoted dedup:\n  got  = %q\n  want = %q", got, want)
	}
}

// TestUnresolvedSkillPlaceholderPattern_MatchesSingleBrace verifies that the
// unresolved placeholder regex also catches single-brace patterns.
func TestUnresolvedSkillPlaceholderPattern_MatchesSingleBrace(t *testing.T) {
	// Known vars don't include "missing", so it should be detected as unresolved.
	got := substituteSkillVariables("echo {missing}", map[string]string{"input": "ignored"})
	// GUI version strips unresolved placeholders.
	if got != "echo " {
		t.Fatalf("substituteSkillVariables() unresolved single-brace:\n  got  = %q\n  want = %q", got, "echo ")
	}
}

// TestDetectImplicitRequiredArgs_SingleBrace verifies that {input} single-brace
// placeholders are detected as missing required args.
func TestDetectImplicitRequiredArgs_SingleBrace(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "python ocr.py {input}"}},
	}
	// No vars provided — {input} should be detected as missing.
	missing := detectImplicitRequiredArgs(steps, nil)
	if len(missing) != 1 || missing[0] != "input" {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [input]", missing)
	}
}

// TestDetectImplicitRequiredArgs_SingleBraceProvided verifies that {input}
// is NOT reported as missing when the var is provided.
func TestDetectImplicitRequiredArgs_SingleBraceProvided(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "python ocr.py {input}"}},
	}
	vars := map[string]string{"input": "/path/to/image.png"}
	missing := detectImplicitRequiredArgs(steps, vars)
	if len(missing) != 0 {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want []", missing)
	}
}

// TestDetectImplicitRequiredArgs_MixedBraceStyles verifies detection works
// with mixed {{key}}, ${key}, and {key} in the same command.
func TestDetectImplicitRequiredArgs_MixedBraceStyles(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "python convert.py {{input}} --format {format} --out ${output}",
		}},
	}
	vars := map[string]string{"input": "in.png"} // format and output missing
	missing := detectImplicitRequiredArgs(steps, vars)
	if len(missing) != 2 {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [format, output]", missing)
	}
	// Should be sorted
	if missing[0] != "format" || missing[1] != "output" {
		t.Fatalf("detectImplicitRequiredArgs() = %v, want [format, output]", missing)
	}
}

// ── Parameter Contract Integration Tests ──

func TestResolveSkillStep_WithParamBinding_AliasResolution(t *testing.T) {
	// Skill declares "description" with alias "content".
	// LLM passes "content" → BindParams resolves to "description".
	params := []corelib.NLSkillParam{
		{Name: "description", Aliases: []string{"content", "input"}},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js --desc {{description}}",
		},
	}, map[string]string{"content": "北京5环图"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "北京5环图") {
		t.Fatalf("command = %q, want alias-resolved value", command)
	}
}

func TestResolveSkillStep_WithParamBinding_RequiredMissing(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Required: true},
	}
	_, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "tool {{input}}",
		},
	}, map[string]string{}, "", params)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "input") {
		t.Fatalf("error should mention 'input', got %q", err.Error())
	}
}

func TestResolveSkillStep_WithParamBinding_DefaultValue(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", Default: "png"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "convert --format {{format}}",
		},
	}, map[string]string{}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "png") {
		t.Fatalf("command = %q, want default value applied", command)
	}
}

func TestResolveSkillStep_WithParamBinding_CLIFlagAppend(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js",
		},
	}, map[string]string{"format": "svg"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "--format") || !strings.Contains(command, "svg") {
		t.Fatalf("command = %q, want CLI flag appended", command)
	}
}

func TestResolveSkillStep_WithParamBinding_CLIFlagNotDuplicated(t *testing.T) {
	// When a param has both CLIFlag and a template placeholder, the CLI flag
	// should NOT be appended (template substitution handles it).
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
	}
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node gen.js --format {{format}}",
		},
	}, map[string]string{"format": "svg"}, "", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	// Should contain --format exactly once (from template), not twice.
	count := strings.Count(command, "--format")
	if count != 1 {
		t.Fatalf("command = %q, want --format exactly once (got %d)", command, count)
	}
}

func TestResolveSkillStep_NilParams_BackwardCompatible(t *testing.T) {
	// When params is nil, resolveSkillStep should work exactly as before.
	resolved, err := resolveSkillStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{name}}",
		},
	}, map[string]string{"name": "world"}, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	command, _ := resolved.Params["command"].(string)
	if !strings.Contains(command, "world") {
		t.Fatalf("command = %q, want substituted value", command)
	}
}

func TestSkillRunnerPersistRepairResultWritesFileBackedYAML(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "repair-file-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data := []byte("name: repair-file-skill\ndescription: A file backed skill that should persist repairs.\ntriggers:\n  - repair-file-skill\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: echo old\n")
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "repair-file-skill", Source: "file", SkillDir: dir, FailureCount: 1}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	runner := NewSkillRunner(app.skillExecutor)

	runner.persistRepairResult(&corelib.NLSkillEntry{
		Name:               "repair-file-skill",
		Description:        "A file backed skill that should persist repairs.",
		Triggers:           []string{"repair-file-skill"},
		Source:             "file",
		SkillDir:           dir,
		Status:             "active",
		Platforms:          []string{"universal"},
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo repaired"}}},
		FailureCount:       1,
		RepairAttemptCount: 1,
	})

	reloaded, err := loadImportedSkillEntry(dir)
	if err != nil {
		t.Fatalf("loadImportedSkillEntry() error = %v", err)
	}
	if len(reloaded.Steps) != 1 {
		t.Fatalf("reloaded steps = %+v", reloaded.Steps)
	}
	cmd, _ := reloaded.Steps[0].Params["command"].(string)
	if cmd != "echo repaired" {
		t.Fatalf("command = %q, want repaired yaml", cmd)
	}
	if _, err := os.Stat(filepath.Join(dir, "skill.yaml.bak")); err != nil {
		t.Fatalf("expected skill.yaml.bak, stat err = %v", err)
	}
}

func TestSkillRunnerTryAutoUploadUsesQualityGateAfterSingleSuccess(t *testing.T) {
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	var submitCount int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "urls": []string{server.URL}})
		case "/api/v1/skills/submit":
			submitCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"submission_id": "sub-quality"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "quality-upload-skill")
	writeLifecycleTestSkill(t, dir, "quality-upload-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Quality upload skill\n\nA verified portable skill.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteViewerToken = "test-token"
	cfg.RemoteHubCenterURL = server.URL
	cfg.RemoteHubCenterURLs = []string{server.URL}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:         "quality-upload-skill",
		SkillDir:     dir,
		Source:       "file",
		Status:       "active",
		SuccessCount: 1,
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	app.autoUploadTrigger = NewAutoUploadTrigger(app.skillMarketClient, func() string { return "user@example.com" })
	app.skillLifecycle = NewSkillLifecycleManager(app)
	runner := NewSkillRunner(app.skillExecutor)
	runner.uploadTrigger = app.autoUploadTrigger

	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-upload-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-upload-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})
	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-upload-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-upload-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})

	items, err := app.skillLifecycle.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusUploaded || items[0].SubmissionID != "sub-quality" {
		t.Fatalf("upload queue = %+v", items)
	}
	if submitCount != 1 {
		t.Fatalf("submitCount = %d, want exactly one upload for unchanged hash", submitCount)
	}
}

func TestSkillRunnerTryAutoUploadBlocksWhenQualityGateFails(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	app := &App{testHomeDir: tempHome}
	dir := filepath.Join(tempHome, "skills", "quality-blocked-skill")
	writeLifecycleTestSkill(t, dir, "quality-blocked-skill")
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# Quality blocked skill\n\nA portable skill that still needs runtime proof.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:     "quality-blocked-skill",
		SkillDir: dir,
		Source:   "file",
		Status:   "active",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.skillMarketClient = NewSkillMarketClient(app)
	app.autoUploadTrigger = NewAutoUploadTrigger(app.skillMarketClient, func() string { return "user@example.com" })
	app.skillLifecycle = NewSkillLifecycleManager(app)
	runner := NewSkillRunner(app.skillExecutor)
	runner.uploadTrigger = app.autoUploadTrigger

	runner.tryAutoUpload(&corelib.NLSkillEntry{Name: "quality-blocked-skill", SkillDir: dir}, &skillRun{status: SkillRunStatus{
		Skill:  "quality-blocked-skill",
		Status: "success",
		Steps:  []StepResult{{Status: "success"}},
	}})

	items, err := app.skillLifecycle.ListUploadQueue()
	if err != nil {
		t.Fatalf("ListUploadQueue() error = %v", err)
	}
	if len(items) != 1 || items[0].Status != skillUploadStatusBlocked || items[0].QualityScore >= 70 {
		t.Fatalf("upload queue = %+v", items)
	}
	if !strings.Contains(items[0].LastError, "successful verification run") {
		t.Fatalf("LastError = %q, want verification quality reason", items[0].LastError)
	}
}
