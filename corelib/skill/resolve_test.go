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
	vars := map[string]string{"input": "some value"} // wrong name

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
