package skill

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSynthesizeParams_BasicCommand(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "node gen.js --description {{content}} --name diagram",
		}},
	}
	params := SynthesizeParams(steps, []string{"content"})

	if len(params) != 1 {
		t.Fatalf("expected 1 param, got %d: %v", len(params), paramNames(params))
	}
	if params[0].Name != "content" {
		t.Errorf("expected param name 'content', got %q", params[0].Name)
	}
	if !params[0].Required {
		t.Error("content should be required (in requiredArgs)")
	}
	if !params[0].Synthetic {
		t.Error("param should be marked synthetic")
	}
}

func TestSynthesizeParams_MultipleSteps(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "python create.py {{input}}",
		}},
		{Action: "bash", Params: map[string]interface{}{
			"command": "python query.py {{session_id}}",
		}},
	}
	params := SynthesizeParams(steps, nil)

	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d: %v", len(params), paramNames(params))
	}
	nameSet := make(map[string]bool)
	for _, p := range params {
		nameSet[p.Name] = true
	}
	if !nameSet["input"] || !nameSet["session_id"] {
		t.Errorf("expected input and session_id, got %v", nameSet)
	}
}

func TestSynthesizeParams_SkipsBaseDir(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "node {baseDir}/gen.js {{input}}",
		}},
	}
	params := SynthesizeParams(steps, nil)

	if len(params) != 1 || params[0].Name != "input" {
		t.Errorf("expected only 'input' (baseDir skipped), got %v", paramNames(params))
	}
}

func TestSynthesizeParams_Deduplicates(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "echo {{name}} and {{name}}",
		}},
	}
	params := SynthesizeParams(steps, nil)

	if len(params) != 1 {
		t.Errorf("expected 1 unique param, got %d", len(params))
	}
}

func TestSynthesizeParams_NoPlaceholders(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "echo hello world",
		}},
	}
	params := SynthesizeParams(steps, nil)

	if len(params) != 0 {
		t.Errorf("expected 0 params, got %d", len(params))
	}
}

func TestSynthesizeParams_NestedParams(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command":     "echo {{input}}",
			"working_dir": "/tmp/{{project}}",
		}},
	}
	params := SynthesizeParams(steps, nil)

	nameSet := make(map[string]bool)
	for _, p := range params {
		nameSet[p.Name] = true
	}
	if !nameSet["input"] || !nameSet["project"] {
		t.Errorf("expected input and project from nested params, got %v", nameSet)
	}
}

func TestSynthesizeParams_RequiredArgs(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "tool {{input}} {{output}}",
		}},
	}
	params := SynthesizeParams(steps, []string{"input"})

	for _, p := range params {
		if p.Name == "input" && !p.Required {
			t.Error("input should be required")
		}
		if p.Name == "output" && p.Required {
			t.Error("output should not be required")
		}
	}
}

func TestSynthesizeParams_CanonicalizesPlaceholderNames(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "tool {{Input-File}} ${Output File}",
		}},
	}
	params := SynthesizeParams(steps, []string{"input_file"})

	nameSet := make(map[string]corelib.NLSkillParam)
	for _, p := range params {
		nameSet[p.Name] = p
	}
	if _, ok := nameSet["input_file"]; !ok || !nameSet["input_file"].Required {
		t.Fatalf("input_file param not synthesized as required: %#v", params)
	}
	if _, ok := nameSet["output_file"]; !ok {
		t.Fatalf("output_file param not synthesized: %#v", params)
	}
}

func TestCompleteParamsForRunnerMergesMissingTemplateParams(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "tool {{input}} --format {{format}}",
		}},
	}
	explicit := []corelib.NLSkillParam{
		{Name: "format", Description: "Output format", Default: "pdf", CLIFlag: "--format"},
	}

	params := CompleteParamsForRunner(explicit, steps, []string{"input"})

	if len(params) != 2 {
		t.Fatalf("expected explicit format plus synthesized input, got %d: %#v", len(params), params)
	}
	if params[0].Name != "format" || params[0].Description != "Output format" || params[0].Default != "pdf" || params[0].CLIFlag != "--format" {
		t.Fatalf("explicit param was not preserved: %#v", params[0])
	}
	if params[1].Name != "input" || !params[1].Required || !params[1].Synthetic {
		t.Fatalf("expected required synthetic input, got %#v", params[1])
	}
}

func TestCompleteParamsForRunnerDoesNotDuplicateExplicitAlias(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "tool {{input}}",
		}},
	}
	explicit := []corelib.NLSkillParam{
		{Name: "file", Aliases: []string{"input"}, Description: "Input file"},
	}

	params := CompleteParamsForRunner(explicit, steps, []string{"input"})

	if len(params) != 1 {
		t.Fatalf("expected alias to cover synthesized input, got %d: %#v", len(params), params)
	}
	if params[0].Name != "file" || !params[0].Required || params[0].Description != "Input file" {
		t.Fatalf("explicit alias param not preserved/upgraded: %#v", params[0])
	}
}

func TestCompleteParamsForRunnerDoesNotDuplicateCommonAlias(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{
			"command": "tool {{input}}",
		}},
	}
	explicit := []corelib.NLSkillParam{
		{Name: "input", Aliases: []string{"file"}, Description: "Input"},
	}

	params := CompleteParamsForRunner(explicit, steps, nil)

	if len(params) != 1 {
		t.Fatalf("expected explicit input to cover synthesized input, got %d: %#v", len(params), params)
	}
	if params[0].Aliases[0] != "file" || params[0].Synthetic {
		t.Fatalf("explicit param fields changed unexpectedly: %#v", params[0])
	}
}

func paramNames(params []corelib.NLSkillParam) []string {
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = p.Name
	}
	return out
}
