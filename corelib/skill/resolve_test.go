package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestResolveStep_BasicSubstitution(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{message}}",
		},
	}
	vars := map[string]string{"message": "hello world"}

	result, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "echo hello world" {
		t.Errorf("expected 'echo hello world', got %q", cmd)
	}
}

func TestResolveStep_AliasResolution(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "process --input {{source_file}}",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "source_file", Aliases: []string{"input", "file", "src"}},
	}
	// LLM passes "input" but skill expects "source_file"
	vars := map[string]string{"input": "/tmp/data.csv"}

	result, err := ResolveStep(step, vars, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "process --input /tmp/data.csv" {
		t.Errorf("expected alias resolution, got %q", cmd)
	}
}

func TestResolveStep_AliasParamCanSatisfyTemplatePlaceholder(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "process {{input}}",
		},
	}
	params := CompleteParamsForRunner([]corelib.NLSkillParam{
		{Name: "file", Aliases: []string{"input"}, CLIFlag: "--file"},
	}, []corelib.NLSkillStep{step}, []string{"input"})

	result, err := ResolveStep(step, map[string]string{"file": "report.md"}, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "process report.md" {
		t.Fatalf("command = %q, want alias value substituted without duplicate CLI flag", cmd)
	}
}

func TestResolveStep_CommonAliasCanSatisfyTemplatePlaceholder(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "process {{file}}",
		},
	}
	params := CompleteParamsForRunner([]corelib.NLSkillParam{
		{Name: "input"},
	}, []corelib.NLSkillStep{step}, nil)

	result, err := ResolveStep(step, map[string]string{"input": "report.md"}, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "process report.md" {
		t.Fatalf("command = %q, want common alias placeholder substituted", cmd)
	}
}

func TestResolveStep_ModeAliasSatisfiesActionPlaceholder(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "python weather.py {{action}} --lat {{lat}} --lng {{lng}}",
		},
	}
	params := CompleteParamsForRunner(nil, []corelib.NLSkillStep{step}, nil)
	vars := map[string]string{"mode": "weekly", "lat": "30.27", "lng": "120.15"}

	result, err := ResolveStep(step, vars, "", params, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "python weather.py weekly --lat 30.27 --lng 120.15" {
		t.Fatalf("command = %q, want mode alias resolved into action placeholder", cmd)
	}
}

func TestResolveStep_CommonAliasDoesNotCollapseDeclaredTemplateParams(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "emit {{text}} {{content}}",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "text", Required: true},
		{Name: "content", Required: true},
	}

	result, err := ResolveStep(step, map[string]string{"text": "alpha", "content": "beta"}, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "emit alpha beta" {
		t.Fatalf("command = %q, want independent declared param values", cmd)
	}
}

func TestResolveStep_RequiredParamMissing(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "gen.js --description {{content}}",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "content", Required: true},
	}
	vars := map[string]string{"unrelated": "some value"} // wrong name

	_, err := ResolveStep(step, vars, "", params, nil)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "content") {
		t.Errorf("error should mention 'content', got: %v", err)
	}
}

func TestResolveStep_CLIFlagAppending(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "gen.js",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format", Default: "png"},
		{Name: "output", CLIFlag: "--output"},
	}
	vars := map[string]string{"output": "/tmp/out.png"}

	result, err := ResolveStep(step, vars, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	// Should have --format png (default) and --output /tmp/out.png
	if !strings.Contains(cmd, "--format") || !strings.Contains(cmd, "png") {
		t.Errorf("expected --format png in command, got %q", cmd)
	}
	if !strings.Contains(cmd, "--output") || !strings.Contains(cmd, "/tmp/out.png") {
		t.Errorf("expected --output /tmp/out.png in command, got %q", cmd)
	}
}

func TestResolveStep_CLIFlagAppendJoinStyles(t *testing.T) {
	quoteFunc := func(s string) string { return `"` + s + `"` }
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "gen.js",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format="},
		{Name: "output", CLIFlag: "/out:"},
	}
	vars := map[string]string{"format": "svg", "output": "out file.svg"}

	result, err := ResolveStep(step, vars, "", params, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if !strings.Contains(cmd, `--format="svg"`) {
		t.Fatalf("command = %q, want equals-style CLI flag rendered as one token", cmd)
	}
	if !strings.Contains(cmd, `/out:"out file.svg"`) {
		t.Fatalf("command = %q, want slash-colon CLI flag rendered as one token", cmd)
	}
	if strings.Contains(cmd, `--format= "svg"`) || strings.Contains(cmd, `/out: "out file.svg"`) {
		t.Fatalf("command = %q, contains invalid space after joined CLI flag", cmd)
	}
}

func TestResolveStep_CLIFlagAndTemplatePlaceholder_NoDoubleApply(t *testing.T) {
	// When a param has both CLIFlag and a template placeholder, the template
	// substitution handles it. The CLI flag should NOT be appended again.
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "gen.js --format {{format}}",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
	}
	vars := map[string]string{"format": "svg"}

	result, err := ResolveStep(step, vars, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	// Should have exactly one --format, not two
	count := strings.Count(cmd, "--format")
	if count != 1 {
		t.Errorf("expected exactly 1 --format occurrence, got %d in %q", count, cmd)
	}
}

func TestResolveStep_DefaultValue(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "convert --format {{format}}",
		},
	}
	params := []corelib.NLSkillParam{
		{Name: "format", Default: "png"},
	}
	vars := map[string]string{} // no format provided

	result, err := ResolveStep(step, vars, "", params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if !strings.Contains(cmd, "png") {
		t.Errorf("expected default 'png' in command, got %q", cmd)
	}
}

func TestResolveStep_AppendsInputForCommandWithoutParamContract(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "command",
		Params: map[string]interface{}{
			"command": "node run.js",
		},
	}
	quoteFunc := func(s string) string { return `"` + s + `"` }

	result, err := ResolveStep(step, map[string]string{"input": "翻译 thesis.pdf"}, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != `node run.js "翻译 thesis.pdf"` {
		t.Fatalf("command = %q, want input appended as shell arg", cmd)
	}
}

func TestResolveStep_AppendsInputForEmptyActionCommandStep(t *testing.T) {
	step := corelib.NLSkillStep{
		Params: map[string]interface{}{
			"command": "python scripts/run.py",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "python scripts/run.py thesis.pdf" {
		t.Fatalf("command = %q, want input appended for empty-action command step", cmd)
	}
}

func TestResolveStep_DoesNotAppendInputWhenCommandConsumesPlaceholder(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "command",
		Params: map[string]interface{}{
			"command": "node run.js {input}",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "node run.js thesis.pdf" {
		t.Fatalf("command = %q, want placeholder substitution without duplicate arg", cmd)
	}
}

func TestResolveStep_DoesNotAppendInputToNonScriptSetupCommand(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "command",
		Params: map[string]interface{}{
			"command": "npm install",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "npm install" {
		t.Fatalf("command = %q, want setup command unchanged", cmd)
	}
}

func TestResolveStep_DoesNotAppendInputToCompoundShellCommand(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "command",
		Params: map[string]interface{}{
			"command": "node run.js && echo done",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "node run.js && echo done" {
		t.Fatalf("command = %q, want compound command unchanged", cmd)
	}
}

func TestResolveStep_DoesNotAppendInputToExplicitBashStep(t *testing.T) {
	// Bash steps that reference a script file (node/python + .js/.py extension)
	// DO get implicit input appended when no placeholder is present.
	// This enables SKILL.md-parsed skills (which always use action:"bash")
	// to receive user-provided input as a trailing CLI argument.
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "node run.js",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if !strings.Contains(cmd, "thesis.pdf") {
		t.Fatalf("command = %q, want input appended for script-accepting bash step", cmd)
	}
}

func TestResolveStep_DoesNotAppendInputToExplicitShellStep(t *testing.T) {
	// Same as bash: shell steps with script commands get implicit input.
	step := corelib.NLSkillStep{
		Action: "shell",
		Params: map[string]interface{}{
			"command": "node run.js",
		},
	}

	result, err := ResolveStep(step, map[string]string{"input": "thesis.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if !strings.Contains(cmd, "thesis.pdf") {
		t.Fatalf("command = %q, want input appended for script-accepting shell step", cmd)
	}
}

func TestResolveStep_CraftToolInjection(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "do something",
		},
	}
	vars := map[string]string{"input": "/tmp/file.txt", "output": "/tmp/out.txt"}

	result, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, _ := result.Step.Params["input"].(string); v != "/tmp/file.txt" {
		t.Errorf("expected craft_tool input injection, got %q", v)
	}
	if v, _ := result.Step.Params["output"].(string); v != "/tmp/out.txt" {
		t.Errorf("expected craft_tool output injection, got %q", v)
	}
}

func TestResolveStep_CraftToolInjectsUserPromptFromInput(t *testing.T) {
	result, err := ResolveStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "Use python-docx for Word files.",
		},
	}, map[string]string{"input": "create a test docx"}, "", nil, nil)
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	if got := result.Step.Params["user_prompt"]; got != "create a test docx" {
		t.Fatalf("user_prompt = %#v, want input value", got)
	}
}

func TestResolveStep_CraftToolInjectionHonorsAliases(t *testing.T) {
	result, err := ResolveStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{"task": "convert file"},
	}, map[string]string{"file": "in.md", "destination": "out.pdf"}, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Step.Params["input"] != "in.md" || result.Step.Params["output"] != "out.pdf" {
		t.Fatalf("craft_tool injected params = %#v, want aliases promoted", result.Step.Params)
	}
}

func TestResolveStep_WorkingDirResolution(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command":     "echo test",
			"working_dir": "scripts",
		},
	}

	result, err := ResolveStep(step, nil, "/home/user/skills/my-skill", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wd, _ := result.Step.Params["working_dir"].(string)
	if !strings.Contains(wd, "my-skill") || !strings.Contains(wd, "scripts") {
		t.Errorf("expected resolved working_dir, got %q", wd)
	}
}

func TestResolveStep_NoParams_PassThrough(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{msg}}",
		},
	}
	// No params schema — vars pass through directly
	vars := map[string]string{"msg": "hello"}

	result, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", cmd)
	}
}

func TestResolveStep_WithQuoteFunc(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo {{msg}}",
		},
	}
	vars := map[string]string{"msg": "hello world"}
	quoteFunc := func(s string) string {
		return `"` + s + `"`
	}

	result, err := ResolveStep(step, vars, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != `echo "hello world"` {
		t.Errorf("expected quoted value, got %q", cmd)
	}
}

func TestResolveStep_QuotesOnlyShellCommandParam(t *testing.T) {
	quoteFunc := func(s string) string { return `"` + s + `"` }
	result, err := ResolveStep(corelib.NLSkillStep{
		Action: "call_mcp_tool",
		Params: map[string]interface{}{
			"server_id": "search",
			"tool_name": "query",
			"arguments": map[string]interface{}{
				"query": "{{input}}",
			},
		},
	}, map[string]string{"input": "hello world"}, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	args, ok := result.Step.Params["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("arguments type = %T", result.Step.Params["arguments"])
	}
	if args["query"] != "hello world" {
		t.Fatalf("arguments.query = %#v, want unquoted structured value", args["query"])
	}
}

func TestResolveStep_DoesNotShellQuoteCraftTask(t *testing.T) {
	quoteFunc := func(s string) string { return `"` + s + `"` }
	result, err := ResolveStep(corelib.NLSkillStep{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"task": "summarize {{input}}",
		},
	}, map[string]string{"input": "hello world"}, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Step.Params["task"] != "summarize hello world" {
		t.Fatalf("task = %#v, want unquoted craft task", result.Step.Params["task"])
	}
}

func TestResolveStep_ReplacesCanonicalizedPlaceholders(t *testing.T) {
	resolved, err := ResolveStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "tool {{Input-File}} ${Output File} {{targetLanguage}}",
		},
	}, map[string]string{"input_file": "report.md", "output_file": "out.pdf", "target_language": "English"}, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := resolved.Step.Params["command"].(string)
	if cmd != "tool report.md out.pdf English" {
		t.Fatalf("command = %q, want canonical placeholder replacement", cmd)
	}
}

func TestResolveStep_QuotedCanonicalPlaceholderDedup(t *testing.T) {
	quoteFunc := func(s string) string { return `"` + s + `"` }
	resolved, err := ResolveStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": `tool --input "{{Input-File}}" --output '${Output File}'`,
		},
	}, map[string]string{"input_file": "report path.md", "output_file": "out path.pdf"}, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := resolved.Step.Params["command"].(string)
	if strings.Contains(cmd, `""`) || strings.Contains(cmd, `''`) {
		t.Fatalf("placeholder replacement double-quoted command: %q", cmd)
	}
	if !strings.Contains(cmd, `--input "report path.md"`) || !strings.Contains(cmd, `--output "out path.pdf"`) {
		t.Fatalf("command = %q, want quoted canonical replacements", cmd)
	}
}

func TestFilterConsumedCLIArgs_NoOverlap(t *testing.T) {
	args := []string{"--format", "png", "--output", "file.png"}
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
		{Name: "output", CLIFlag: "--output"},
	}
	// Command has no placeholders
	filtered := FilterConsumedCLIArgs(args, params, "gen.js")
	if len(filtered) != 4 {
		t.Errorf("expected all 4 args preserved, got %d", len(filtered))
	}
}

func TestFilterConsumedCLIArgs_WithOverlap(t *testing.T) {
	args := []string{"--format", "png", "--output", "file.png"}
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
		{Name: "output", CLIFlag: "--output"},
	}
	// Command has {{format}} placeholder — --format should be filtered
	filtered := FilterConsumedCLIArgs(args, params, "gen.js --format {{format}}")
	if len(filtered) != 2 {
		t.Errorf("expected 2 args (--output file.png), got %d: %v", len(filtered), filtered)
	}
	if filtered[0] != "--output" {
		t.Errorf("expected --output, got %s", filtered[0])
	}
}

func TestResolveStep_NilVarsNilParams(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "echo hello",
		},
	}
	result, err := ResolveStep(step, nil, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", cmd)
	}
}

func TestResolveStep_UnresolvedPlaceholderStripped(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": "tool {{input}} {{optional_flag}}",
		},
	}
	vars := map[string]string{"input": "file.txt"}

	result, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if strings.Contains(cmd, "{{optional_flag}}") {
		t.Errorf("unresolved placeholder should be stripped, got %q", cmd)
	}
	if !strings.Contains(cmd, "file.txt") {
		t.Errorf("resolved placeholder should be preserved, got %q", cmd)
	}
}

func TestResolveStep_UnresolvedOptionalFlagRemoved(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": `python translate.py --text "{{text}}" --target_lang "{{target_lang}}"`,
		},
	}
	vars := map[string]string{"text": "hello"}

	result, err := ResolveStep(step, vars, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if strings.Contains(cmd, "--target_lang") || strings.Contains(cmd, "target_lang") {
		t.Fatalf("optional unresolved target_lang flag was not removed: %q", cmd)
	}
	if !strings.Contains(cmd, `--text "hello"`) {
		t.Fatalf("resolved text flag was not preserved: %q", cmd)
	}
}

func TestResolveStep_PreservesAuthorQuotesWithoutQuoteFunc(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": `tool --text "{{text}}"`,
		},
	}

	result, err := ResolveStep(step, map[string]string{"text": "hello world"}, "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	if cmd != `tool --text "hello world"` {
		t.Fatalf("command = %q, want author quotes preserved", cmd)
	}
}

func TestResolveStep_QuotedPlaceholderDedup(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{
			"command": `tool --text "{{text}}"`,
		},
	}
	vars := map[string]string{"text": "hello world"}
	quoteFunc := func(s string) string {
		return `"` + s + `"`
	}

	result, err := ResolveStep(step, vars, "", nil, quoteFunc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cmd, _ := result.Step.Params["command"].(string)
	// The "{{text}}" pattern should be replaced with the quoted value,
	// not produce double quotes like ""hello world""
	if strings.Contains(cmd, `""hello`) {
		t.Errorf("double-quoting detected, got %q", cmd)
	}
	if !strings.Contains(cmd, `"hello world"`) {
		t.Errorf("expected quoted value, got %q", cmd)
	}
}
