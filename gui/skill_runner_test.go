package main

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	cfg.LocalMCPServers = []LocalMCPServerEntry{newHelperLocalMCPServerEntry("enabled-no-autostart", false, false)}
	cfg.LocalMCPServers[0].Name = "brave-search"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.mcpRegistry = NewMCPRegistry(app)
	app.localMCPManager = NewLocalMCPManager(app.mcpRegistry)
	defer app.localMCPManager.StopAll()
	app.localMCPManager.SyncFromConfig()

	runner := NewSkillRunner(&SkillExecutor{app: app, mcpRegistry: app.mcpRegistry})
	result, err := runner.executeStepWithContext(context.Background(), "run-test", NLSkillStep{
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

func TestSkillRunnerStartRun_RejectsSkillWithoutExecutableSteps(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []NLSkillEntry{{
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
	_, err := runner.executeStepWithContext(context.Background(), "run-test", NLSkillStep{
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
	_, err := runner.executeStepWithContext(context.Background(), "run-test", NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{},
	}, "")
	if err == nil || !strings.Contains(err.Error(), "missing task parameter") {
		t.Fatalf("expected missing task error, got: %v", err)
	}
}

func TestIsInstructionOnlySkillEntry(t *testing.T) {
	if !isInstructionOnlySkillEntry(&NLSkillEntry{Steps: []NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"instructions": "# PPTX Generator\n\nGenerate slides."},
	}}}) {
		t.Fatal("expected craft_tool with instructions to require artifact verification")
	}
	if isInstructionOnlySkillEntry(&NLSkillEntry{Steps: []NLSkillStep{{
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
	resolved := resolveSkillStep(NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":     "printf '%s %s' {{input}} ${output}",
			"working_dir": "nested",
			"arguments": map[string]interface{}{
				"path": "{{input}}",
			},
			"items": []interface{}{"${output}", 7},
		},
	}, map[string]string{"input": "report.md", "output": "out.pdf"}, skillDir)

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
	resolved := resolveSkillStep(NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "Generate slides.",
		},
	}, map[string]string{
		"output": "out/live_test_deck.pptx",
		"topic":  "Quarterly product review",
	}, "")
	if got, _ := resolved.Params["output"].(string); got != "out/live_test_deck.pptx" {
		t.Fatalf("output = %q, want propagated run arg", got)
	}
	if got, _ := resolved.Params["topic"].(string); got != "Quarterly product review" {
		t.Fatalf("topic = %q, want propagated run arg", got)
	}
}

func TestResolveSkillStep_StripsMissingOptionalPlaceholder(t *testing.T) {
	resolved := resolveSkillStep(NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node ./tool.mjs {{input}} {{output}}",
		},
	}, map[string]string{"input": "report.md"}, "")
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
