package skill

import (
	"strings"
	"testing"
)

func TestPipelineRunStackRoundTripAndRecursionMessage(t *testing.T) {
	args := WithPipelineRunStack(map[string]interface{}{"input": "hello"}, "parent")
	args = WithPipelineRunStack(args, "child")

	stack := PipelineRunStackFromArgs(args)
	if len(stack) != 2 || stack[0] != "parent" || stack[1] != "child" {
		t.Fatalf("stack = %#v, want parent -> child", stack)
	}
	if !PipelineRunStackContains(stack, " CHILD ") {
		t.Fatalf("stack %#v should contain child case-insensitively", stack)
	}
	msg := FormatPipelineRecursionMessage("parent", stack)
	if !strings.Contains(msg, "parent -> child -> parent") {
		t.Fatalf("message = %q, want readable recursion chain", msg)
	}
}

func TestPipelineRunStackParsesStringForms(t *testing.T) {
	for _, raw := range []interface{}{
		`["one","two"]`,
		"one,two",
		"one -> two",
		"one-skill -> two-skill",
	} {
		stack := PipelineRunStackFromArgs(map[string]interface{}{PipelineRunStackArg: raw})
		want0, want1 := "one", "two"
		if raw == "one-skill -> two-skill" {
			want0, want1 = "one-skill", "two-skill"
		}
		if len(stack) != 2 || stack[0] != want0 || stack[1] != want1 {
			t.Fatalf("raw %#v parsed as %#v, want %s/%s", raw, stack, want0, want1)
		}
	}
}

func TestPipelineRunStackIgnoresPublicAlias(t *testing.T) {
	if stack := PipelineRunStackFromArgs(map[string]interface{}{"pipeline_stack": []string{"user"}}); len(stack) != 0 {
		t.Fatalf("stack = %#v, want public alias ignored", stack)
	}
	if stack := PipelineRunStackFromArgs(map[string]interface{}{"pipeline-stack": []string{"user"}}); len(stack) != 0 {
		t.Fatalf("stack = %#v, want public alias ignored", stack)
	}
}

func TestIsInternalPipelineRunArgsRequiresInternalMarker(t *testing.T) {
	if IsInternalPipelineRunArgs(map[string]interface{}{PipelineRunStackArg: []string{"parent"}}) {
		t.Fatal("private stack without internal marker should not count as internal run")
	}
	if stack := TrustedPipelineRunStackFromArgs(map[string]interface{}{PipelineRunStackArg: []string{"parent"}}); len(stack) != 0 {
		t.Fatalf("trusted stack = %#v, want unmarked private stack ignored", stack)
	}
	if IsInternalPipelineRunArgs(map[string]interface{}{PipelineRunStackArg: []string{"parent"}, PipelineInternalCallArg: "true"}) {
		t.Fatal("string marker should not count as internal run")
	}
	if IsInternalPipelineRunArgs(map[string]interface{}{PipelineRunStackArg: []string{"parent"}, PipelineInternalCallArg: true}) {
		t.Fatal("user-supplied bool marker should not count as internal run")
	}
	args := WithPipelineRunStack(map[string]interface{}{"input": "hello"}, "parent")
	if !IsInternalPipelineRunArgs(args) {
		t.Fatalf("WithPipelineRunStack args = %#v, want internal run marker", args)
	}
	if stack := TrustedPipelineRunStackFromArgs(args); len(stack) != 1 || stack[0] != "parent" {
		t.Fatalf("trusted stack = %#v, want parent", stack)
	}
}

func TestWithPipelineRunStackIgnoresUntrustedPrivateStack(t *testing.T) {
	args := WithPipelineRunStack(map[string]interface{}{
		PipelineRunStackArg:     []string{"forged-parent"},
		PipelineInternalCallArg: true,
	}, "real-parent")
	stack := TrustedPipelineRunStackFromArgs(args)
	if len(stack) != 1 || stack[0] != "real-parent" {
		t.Fatalf("trusted stack = %#v, want only real-parent", stack)
	}
}

func TestIsPipelineBaseRunArgAllowedSharesContextCarriers(t *testing.T) {
	allowed := []string{
		PipelineRunStackArg,
		PipelineInternalCallArg,
		"env",
		"Extra Env",
		"environment",
		"user-prompt",
		"input",
		"output",
		"query",
		"text",
		"file",
		"path",
		"url",
		"format",
		"prompt",
		"content",
		"mode",
	}
	for _, key := range allowed {
		if !IsPipelineBaseRunArgAllowed(key) {
			t.Fatalf("%q should be allowed as pipeline base run arg", key)
		}
	}
	for _, key := range []string{"pipeline_stack", "steps", "operation", "name", "action", "city", "arbitrary"} {
		if IsPipelineBaseRunArgAllowed(key) {
			t.Fatalf("%q should not be allowed as pipeline base run arg", key)
		}
	}
}

func TestBuildPipelineSubSkillRunArgsCarriesNestedContextControls(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"args": map[string]interface{}{
			"input":     "weather in Chengdu",
			"query":     "weather query",
			"mode":      "advanced",
			"extra_env": map[string]interface{}{"API_TOKEN": "secret"},
			"name":      "forged-child",
			"action":    "run",
		},
	}, "parent")
	got := BuildPipelineSubSkillRunArgs(base, map[string]string{"city": "Chengdu"})

	if got["input"] != "weather in Chengdu" || got["query"] != "weather query" || got["mode"] != "advanced" || got["city"] != "Chengdu" {
		t.Fatalf("merged args = %#v, want nested context carriers and explicit params", got)
	}
	env, ok := got["extra_env"].(map[string]string)
	if !ok || env["API_TOKEN"] != "secret" {
		t.Fatalf("merged args extra_env = %#v, want nested env carried as run control input", got["extra_env"])
	}
	if _, ok := got["name"]; ok {
		t.Fatalf("merged args = %#v, nested manage_skill name must not leak", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("merged args = %#v, nested manage_skill action must not leak", got)
	}
}

func TestBuildPipelineSubSkillRunArgsCarriesNaturalInputAliases(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"text": "translate me",
		"url":  "https://example.test/input",
		"args": map[string]interface{}{
			"content": "nested content",
			"prompt":  "nested prompt",
			"file":    "report.md",
			"path":    "data/input.txt",
			"output":  "out.pdf",
			"format":  "pdf",
		},
	}, "parent")

	got := BuildPipelineSubSkillRunArgs(base, nil)

	if got["text"] != "translate me" || got["content"] != "nested content" || got["prompt"] != "nested prompt" || got["file"] != "report.md" || got["path"] != "data/input.txt" || got["output"] != "out.pdf" || got["url"] != "https://example.test/input" || got["format"] != "pdf" {
		t.Fatalf("merged args = %#v, want natural input aliases carried into sub-skill", got)
	}
}

func TestBuildPipelineSubSkillRunArgsCarriesPlainStringArgsAsInput(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"args": "translate this text",
	}, "parent")

	got := BuildPipelineSubSkillRunArgs(base, nil)

	if got["input"] != "translate this text" {
		t.Fatalf("merged args = %#v, want plain string args carried as legacy input", got)
	}
}

func TestBuildPipelineSubSkillRunArgsDoesNotTreatJSONArgsAsPlainInput(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"args": `{"input":"structured input"}`,
	}, "parent")

	got := BuildPipelineSubSkillRunArgs(base, nil)

	if got["input"] != "structured input" {
		t.Fatalf("merged args = %#v, want structured args input carried from JSON object", got)
	}
}

func TestBuildPipelineSubSkillRunArgsMergesStepEnvWithParentEnv(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"args": `{"extra_env":{"API_TOKEN":"parent-secret","SHARED":"parent"}}`,
	}, "parent")
	got := BuildPipelineSubSkillRunArgs(base, map[string]string{
		"extra_env": `{"SHARED":"step","STEP_ONLY":"1"}`,
		"input":     "hello",
	})

	env, ok := got["extra_env"].(map[string]string)
	if !ok {
		t.Fatalf("extra_env = %#v, want merged map[string]string", got["extra_env"])
	}
	if env["API_TOKEN"] != "parent-secret" || env["SHARED"] != "step" || env["STEP_ONLY"] != "1" {
		t.Fatalf("extra_env = %#v, want parent env retained and step env merged", env)
	}
	if got["input"] != "hello" {
		t.Fatalf("merged args = %#v, want normal params preserved", got)
	}
}

func TestBuildPipelineSubSkillRunArgsProtectsControlKeys(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"input": "weather in Chengdu",
		"env":   map[string]interface{}{"API_TOKEN": "secret"},
	}, "parent")
	got := BuildPipelineSubSkillRunArgs(base, map[string]string{
		"city":                       "Shanghai",
		"name":                       "other-skill",
		"action":                     "run",
		PipelineRunStackArg:          "forged",
		PipelineInternalCallArg:      "false",
		"pipeline-stack":             "public-forged",
		"pipeline-internal-call":     "true",
		"operation":                  "business-operation",
		"steps":                      "selected-step",
		"dry_run":                    "true",
		"qualified_name":             "other-qualified",
		"pipeline_internal_call_alt": "kept",
	})

	if got["city"] != "Shanghai" || got["input"] != "weather in Chengdu" {
		t.Fatalf("merged args = %#v, want normal params and parent context", got)
	}
	if got["operation"] != "business-operation" || got["steps"] != "selected-step" || got["dry_run"] != "true" {
		t.Fatalf("merged args = %#v, want workflow selectors preserved", got)
	}
	if _, ok := got["name"]; ok {
		t.Fatalf("merged args = %#v, name must not override selected sub-skill", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("merged args = %#v, manage_skill action must not leak into sub-skill params", got)
	}
	if got[PipelineRunStackArg] == "forged" || got[PipelineInternalCallArg] == "false" {
		t.Fatalf("merged args = %#v, private pipeline controls were overwritten", got)
	}
	if !IsInternalPipelineRunArgs(got) {
		t.Fatalf("merged args = %#v, want original internal marker preserved", got)
	}
	if got["pipeline_internal_call_alt"] != "kept" {
		t.Fatalf("merged args = %#v, unrelated similarly named param should remain", got)
	}
}

func TestBuildPipelineSubSkillRunArgsCanonicalizesControlKeyShapes(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"Operation":              "parent-op",
		"Selected Steps":         "parent-step",
		"Pipeline Internal Call": true,
		"input":                  "hello",
	}, "parent")

	got := BuildPipelineSubSkillRunArgs(base, map[string]string{
		"Operation":              "child-op",
		"Selected-Steps":         "child-step",
		"Skill Name":             "forged-child",
		"Qualified.Name":         "forged-qualified",
		"Pipeline Internal Call": "false",
		"dry-run":                "true",
	})

	if got["Operation"] != "child-op" || got["Selected-Steps"] != "child-step" || got["dry-run"] != "true" {
		t.Fatalf("merged args = %#v, want child workflow selectors with original key shapes preserved", got)
	}
	for _, key := range []string{"Skill Name", "Qualified.Name", "Pipeline Internal Call"} {
		if _, ok := got[key]; ok {
			t.Fatalf("merged args = %#v, control key %q must not leak", got, key)
		}
	}
	if got["input"] != "hello" {
		t.Fatalf("merged args = %#v, want parent input carrier preserved", got)
	}
}

func TestBuildPipelineSubSkillRunArgsDoesNotLeakParentWorkflowSelector(t *testing.T) {
	base := WithPipelineRunStack(map[string]interface{}{
		"operation": "parent-op",
		"steps":     "parent-step",
		"input":     "hello",
	}, "parent")

	got := BuildPipelineSubSkillRunArgs(base, nil)

	if got["operation"] != nil || got["steps"] != nil || got["input"] != "hello" {
		t.Fatalf("merged args = %#v, want parent workflow selectors isolated", got)
	}
}

func TestPipelineRunStackDepthMessage(t *testing.T) {
	stack := make([]string, MaxPipelineRunStackDepth)
	for i := range stack {
		stack[i] = "skill"
	}
	if !PipelineRunStackDepthExceeded(stack) {
		t.Fatal("expected max-depth stack to be rejected before appending another skill")
	}
	msg := FormatPipelineStackDepthMessage("next", stack)
	if !strings.Contains(msg, "pipeline nesting depth exceeded") || !strings.Contains(msg, "next") {
		t.Fatalf("message = %q, want depth error with next skill", msg)
	}
}
