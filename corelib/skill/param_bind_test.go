package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBindParams_AliasResolution(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "description", Aliases: []string{"content", "input", "text"}},
	}
	vars := map[string]string{"content": "北京5环图"}

	result := BindParams(params, vars)

	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if result.ResolvedVars["description"] != "北京5环图" {
		t.Errorf("expected alias resolution, got %q", result.ResolvedVars["description"])
	}
}

func TestBindParams_MirrorsResolvedValueToAliases(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "file", Aliases: []string{"input"}},
	}
	vars := map[string]string{"file": "report.md"}

	result := BindParams(params, vars)

	if result.ResolvedVars["file"] != "report.md" || result.ResolvedVars["input"] != "report.md" {
		t.Fatalf("ResolvedVars = %#v, want value mirrored to canonical name and alias", result.ResolvedVars)
	}
}

func TestBindParams_HonorsCommonAliases(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Required: true},
	}
	vars := map[string]string{"file": "report.md"}

	result := BindParams(params, vars)

	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if result.ResolvedVars["input"] != "report.md" || result.ResolvedVars["file"] != "report.md" {
		t.Fatalf("ResolvedVars = %#v, want common alias bound and mirrored", result.ResolvedVars)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("common alias should be consumed without warnings: %v", result.Warnings)
	}
}

func TestBindParams_CommonAliasesDoNotOverrideDeclaredParams(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "text", Required: true},
		{Name: "content", Required: true},
	}
	vars := map[string]string{"text": "from-text", "content": "from-content"}

	result := BindParams(params, vars)

	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if result.ResolvedVars["text"] != "from-text" || result.ResolvedVars["content"] != "from-content" {
		t.Fatalf("ResolvedVars = %#v, want declared params to keep independent values", result.ResolvedVars)
	}
}

func TestBindParams_DirectMatch(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", CLIFlag: "--input"},
	}
	vars := map[string]string{"input": "file.txt"}

	result := BindParams(params, vars)

	if result.ResolvedVars["input"] != "file.txt" {
		t.Errorf("expected direct match, got %q", result.ResolvedVars["input"])
	}
}

func TestBindParams_DefaultValue(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", Default: "png", CLIFlag: "--format"},
	}
	vars := map[string]string{} // no format provided

	result := BindParams(params, vars)

	if result.ResolvedVars["format"] != "png" {
		t.Errorf("expected default value 'png', got %q", result.ResolvedVars["format"])
	}
}

func TestBindParams_CLIArgs(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "format", CLIFlag: "--format"},
		{Name: "output", CLIFlag: "--output"},
	}
	vars := map[string]string{"format": "svg", "output": "out.svg"}

	result := BindParams(params, vars)

	if len(result.CLIArgs) != 4 {
		t.Fatalf("expected 4 CLI args, got %d: %v", len(result.CLIArgs), result.CLIArgs)
	}
	if result.CLIArgs[0] != "--format" || result.CLIArgs[2] != "--output" {
		t.Errorf("unexpected CLI args: %v", result.CLIArgs)
	}
}

func TestBindParams_SyntheticParamsNoCLI(t *testing.T) {
	// Synthetic params (from template) should NOT generate CLI args.
	params := []corelib.NLSkillParam{
		{Name: "content", Synthetic: true},
	}
	vars := map[string]string{"content": "hello"}

	result := BindParams(params, vars)

	if len(result.CLIArgs) != 0 {
		t.Errorf("synthetic params should not generate CLI args, got %v", result.CLIArgs)
	}
	if result.ResolvedVars["content"] != "hello" {
		t.Errorf("expected resolved var, got %q", result.ResolvedVars["content"])
	}
}

func TestBindParams_UndeclaredParameter(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input"},
	}
	vars := map[string]string{"input": "file.txt", "foo": "bar"}

	result := BindParams(params, vars)

	if len(result.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(result.Warnings), result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "foo") {
		t.Errorf("warning should mention 'foo', got %q", result.Warnings[0])
	}
}

func TestBindParams_IgnoresUndeclaredRunInputCarriers(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "city"},
	}
	vars := map[string]string{
		"input":       "weather in Chengdu",
		"user_prompt": "weather in Chengdu",
		"city":        "Chengdu",
	}

	result := BindParams(params, vars)

	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none for generic run input carriers", result.Warnings)
	}
	if result.ResolvedVars["city"] != "Chengdu" {
		t.Fatalf("resolved city = %q, want Chengdu", result.ResolvedVars["city"])
	}
}

func TestBindParams_RequiredMissing(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Required: true},
	}
	vars := map[string]string{} // no input provided

	result := BindParams(params, vars)

	if !result.HasErrors() {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(result.ErrorString(), "input") {
		t.Errorf("error should mention 'input', got %q", result.ErrorString())
	}
}

func TestBindParams_NoSchema(t *testing.T) {
	// No params schema → pass through all vars unchanged.
	vars := map[string]string{"a": "1", "b": "2"}

	result := BindParams(nil, vars)

	if len(result.ResolvedVars) != 2 {
		t.Errorf("expected 2 vars passed through, got %d", len(result.ResolvedVars))
	}
	if result.ResolvedVars["a"] != "1" || result.ResolvedVars["b"] != "2" {
		t.Errorf("vars should be passed through unchanged")
	}
}

func TestBindParams_AliasFirstMatchWins(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "description", Aliases: []string{"content", "input"}},
	}
	// Both "content" and "input" provided — first alias match wins.
	vars := map[string]string{"content": "from-content", "input": "from-input"}

	result := BindParams(params, vars)

	if result.ResolvedVars["description"] != "from-content" {
		t.Errorf("expected first alias match 'from-content', got %q", result.ResolvedVars["description"])
	}
}

func TestBindParams_EmptyVarsIgnored(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "input", Required: true},
	}
	vars := map[string]string{"input": ""} // empty value

	result := BindParams(params, vars)

	if !result.HasErrors() {
		t.Fatal("empty value should not satisfy required param")
	}
}

func TestBindParams_AcceptsRawKeyShapes(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "Input-File", Aliases: []string{"Source File"}, Required: true},
	}
	vars := map[string]string{"Source File": "report.md"}

	result := BindParams(params, vars)

	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("unexpected warnings for consumed raw alias: %v", result.Warnings)
	}
	if result.ResolvedVars["input_file"] != "report.md" {
		t.Fatalf("ResolvedVars = %#v, want raw alias bound to canonical input_file", result.ResolvedVars)
	}
}

func TestBindParams_CanonicalizesParamAndAliasNames(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "Input.File", Aliases: []string{"Source File"}, Required: true},
		{Name: "targetLanguage", Required: true},
	}
	vars := map[string]string{"source_file": "report.md", "target_language": "English"}

	result := BindParams(params, vars)

	if result.HasErrors() {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if result.ResolvedVars["input_file"] != "report.md" || result.ResolvedVars["target_language"] != "English" {
		t.Fatalf("ResolvedVars = %#v, want canonical input_file and target_language", result.ResolvedVars)
	}
}

func TestFormatParamSchema_Explicit(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "description", Aliases: []string{"content"}, Description: "图表描述", CLIFlag: "--description"},
		{Name: "format", Default: "png", CLIFlag: "--format"},
	}
	schema := FormatParamSchema(params)
	if !strings.Contains(schema, "description") || !strings.Contains(schema, "content") {
		t.Errorf("schema should contain param name and alias, got:\n%s", schema)
	}
	if !strings.Contains(schema, "--description") {
		t.Errorf("schema should contain CLI flag, got:\n%s", schema)
	}
}

func TestFormatParamSchema_Synthetic(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "content", Required: true, Synthetic: true},
	}
	schema := FormatParamSchema(params)
	if !strings.Contains(schema, "从命令模板推断") {
		t.Errorf("synthetic schema should be annotated, got:\n%s", schema)
	}
	if !strings.Contains(schema, "从模板推断") {
		t.Errorf("synthetic param should be annotated, got:\n%s", schema)
	}
}
