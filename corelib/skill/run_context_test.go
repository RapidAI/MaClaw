package skill

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNormalizeRunVarsParsesJSONInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"input": `{"city":"Chengdu","days":3}`})
	if got["city"] != "Chengdu" || got["days"] != "3" || got["input"] == "" {
		t.Fatalf("NormalizeRunVars() = %#v", got)
	}
}

func TestNormalizeRunVarsAcceptsJSONStringArgs(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"args": `{"city":"Chengdu","days":3}`})
	if got["city"] != "Chengdu" || got["days"] != "3" {
		t.Fatalf("NormalizeRunVars() = %#v, want JSON args unpacked", got)
	}
}

func TestNormalizeRunVarsCopiesArbitraryTopLevelParams(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"city": "Chengdu", "count": 2, "action": "run", "name": "weather", "operation": "safe"})
	if got["city"] != "Chengdu" || got["count"] != "2" {
		t.Fatalf("NormalizeRunVars() = %#v, want arbitrary top-level params", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("NormalizeRunVars() should not copy control keys: %#v", got)
	}
	if _, ok := got["operation"]; ok {
		t.Fatalf("NormalizeRunVars() should not copy operation control key: %#v", got)
	}
}

func TestNormalizeRunVarsTreatsExtraEnvAsControlInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"input":       "hello",
		"extra_env":   map[string]interface{}{"OPENAI_API_KEY": "sk-test"},
		"environment": "MODE=test",
	})
	if got["input"] != "hello" {
		t.Fatalf("NormalizeRunVars() = %#v, want input preserved", got)
	}
	if _, ok := got["extra_env"]; ok {
		t.Fatalf("NormalizeRunVars() = %#v, extra_env should not become a skill parameter", got)
	}
	if _, ok := got["environment"]; ok {
		t.Fatalf("NormalizeRunVars() = %#v, environment should not become a skill parameter", got)
	}
}

func TestNormalizeRunVarsTreatsNestedArgsControlKeysAsControlInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"args": map[string]interface{}{
			"city":                       "Chengdu",
			"extra_env":                  map[string]interface{}{"OPENAI_API_KEY": "sk-test"},
			"environment":                "MODE=test",
			"steps":                      []interface{}{"selected"},
			"name":                       "weather",
			"action":                     "run",
			PipelineRunStackArg:          []string{"forged"},
			PipelineInternalCallArg:      true,
			"pipeline_internal_call_alt": "kept",
			"operation":                  "business-op",
		},
	})
	if got["city"] != "Chengdu" || got["operation"] != "business-op" || got["pipeline_internal_call_alt"] != "kept" {
		t.Fatalf("NormalizeRunVars() = %#v, want normal nested args preserved", got)
	}
	for _, key := range []string{"extra_env", "environment", "steps", "name", "action", "pipeline_stack", "pipeline_internal_call"} {
		if _, ok := got[key]; ok {
			t.Fatalf("NormalizeRunVars() = %#v, nested control key %s should not become a skill parameter", got, key)
		}
	}
}

func TestNormalizeRunVarsTreatsNestedJSONArgsControlKeysAsControlInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"args": `{"city":"Chengdu","extra_env":{"TOKEN":"secret"},"name":"weather","operation":"business-op","pipeline_stack":["forged"]}`,
	})
	if got["city"] != "Chengdu" || got["operation"] != "business-op" {
		t.Fatalf("NormalizeRunVars() = %#v, want normal nested JSON args preserved", got)
	}
	for _, key := range []string{"extra_env", "name", "pipeline_stack"} {
		if _, ok := got[key]; ok {
			t.Fatalf("NormalizeRunVars() = %#v, nested JSON control key %s should not become a skill parameter", got, key)
		}
	}
}

func TestNormalizeRunVarsDropsManageSkillControlKeys(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"input":                    "weather in Chengdu",
		"query":                    "weather in Chengdu",
		"mode":                     "advanced",
		"_runtime_platform":        "desktop",
		"_runtime_policy_owner_id": "desktop-user",
		"wait_seconds":             30,
		"auto_run":                 true,
		"auto_fix":                 true,
		"force":                    true,
		"field":                    "command",
		"value":                    "patched",
	})
	if got["query"] != "weather in Chengdu" || got["mode"] != "advanced" {
		t.Fatalf("NormalizeRunVars() = %#v, want runtime selectors preserved", got)
	}
	for _, key := range []string{"runtime_platform", "runtime_policy_owner_id", "wait_seconds", "auto_run", "auto_fix", "force", "field", "value"} {
		if _, ok := got[key]; ok {
			t.Fatalf("NormalizeRunVars() = %#v, want %s control key dropped", got, key)
		}
	}
}

func TestNormalizeRunVarsAllowsOperationInsideArgs(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"args": map[string]interface{}{"operation": "business-op"},
	})
	if got["operation"] != "business-op" {
		t.Fatalf("NormalizeRunVars() = %#v, want operation preserved inside args object", got)
	}
}

func TestNormalizeRunVarsCanonicalizesKeyShape(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"User Prompt": "weather in Chengdu",
		"Args": map[string]interface{}{
			"Input-File":     "report.md",
			"Output.File":    "out.pdf",
			"targetLanguage": "English",
		},
	})
	if got["user_prompt"] != "weather in Chengdu" || got["input_file"] != "report.md" || got["output_file"] != "out.pdf" || got["target_language"] != "English" {
		t.Fatalf("NormalizeRunVars() = %#v, want canonical key shapes", got)
	}
}

func TestNormalizeRunVarsPreservesArgsOverJSONInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"args":  map[string]interface{}{"city": "Shanghai"},
		"input": `{"city":"Chengdu","days":3}`,
	})
	if got["city"] != "Shanghai" || got["days"] != "3" {
		t.Fatalf("NormalizeRunVars() = %#v, want args to win and input JSON to backfill", got)
	}
}

func TestNormalizeRunVarsNilSafe(t *testing.T) {
	if got := NormalizeRunVars(nil); got != nil {
		t.Fatalf("NormalizeRunVars(nil) = %#v, want nil", got)
	}
}

func TestNormalizeRunVarsExcludesContextValue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := NormalizeRunVars(map[string]interface{}{
		"input": "hello",
		"_ctx":  ctx,
	})
	if got["input"] != "hello" {
		t.Fatalf("NormalizeRunVars() = %#v, want input preserved", got)
	}
	// _ctx should not appear in any form (raw key "_ctx" or canonicalized "ctx").
	for key, val := range got {
		if strings.Contains(key, "ctx") || strings.Contains(val, "context") {
			t.Fatalf("NormalizeRunVars() = %#v, context.Context value leaked as key=%q val=%q", got, key, val)
		}
	}
}

func TestContractlessSkillHelpers(t *testing.T) {
	contractless := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "craft_tool",
			Params: map[string]interface{}{"task": "answer user request"},
		}},
	}
	if !IsContractlessSkill(contractless) {
		t.Fatal("skill without params, required args, or placeholders should be contractless")
	}

	withPlaceholder := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}
	if IsContractlessSkill(withPlaceholder) {
		t.Fatal("placeholder-bearing skill should have an implicit contract")
	}

	withFallbackPlaceholder := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo stable"},
			FallbackStep: &corelib.NLSkillStep{
				Action: "craft_tool",
				Params: map[string]interface{}{"task": "repair {{input}}"},
			},
		}},
	}
	if IsContractlessSkill(withFallbackPlaceholder) {
		t.Fatal("fallback placeholder should make skill contractual")
	}

	pipelineWithPlaceholder := &corelib.NLSkillEntry{
		Mode: "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill:             "summarize",
			Params:            map[string]string{"input": "{{input}}"},
			CheckpointMessage: "Review {{summarize.output}}",
		}},
	}
	if IsContractlessSkill(pipelineWithPlaceholder) {
		t.Fatal("pipeline placeholder should make skill contractual")
	}
}

func TestFoldUnconsumedArgsToInput(t *testing.T) {
	vars := map[string]string{
		"city":        "南京",
		"mode":        "weekly",
		"input":       "weather request",
		"user_prompt": "ignored carrier",
	}

	FoldUnconsumedArgsToInput(vars, nil)

	if got := vars["input"]; got != "weather request\ncity: 南京\nmode: weekly" {
		t.Fatalf("input = %q, want folded semantic args appended in stable order", got)
	}
}

func TestApplyRunInputInferenceFillsRequiredArgFromNamedPrompt(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": "city: Shanghai"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "city: Shanghai"})

	if vars["city"] != "Shanghai" {
		t.Fatalf("city = %q, want Shanghai", vars["city"])
	}
}

func TestApplyRunInputInferencePromotesRawKeyShapeAliases(t *testing.T) {
	vars := map[string]string{"User Prompt": "weather in Chengdu", "Source File": "report.md"}
	entry := &corelib.NLSkillEntry{
		RequiredArgs: []string{"city", "input_file"},
		Params: []corelib.NLSkillParam{{
			Name:    "Input-File",
			Aliases: []string{"Source File"},
		}},
	}

	ApplyRunInputInference(entry, vars, nil)

	if vars["city"] != "" || vars["input_file"] != "report.md" {
		t.Fatalf("vars = %#v, want raw aliases promoted without natural-language guessing", vars)
	}
}
func TestApplyRunInputInferenceDoesNotGuessNaturalCityPrompt(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": "weather in Chengdu"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "weather in Chengdu"})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceDoesNotGuessNaturalCityPromptWithoutPreposition(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": "weather Chengdu"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "weather Chengdu"})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceExtractsSingleCityFromNaturalPrompt(t *testing.T) {
	runArgs := map[string]interface{}{"user_prompt": "weather in Chengdu", "_skill_infer_natural_prompt": true}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["city"] != "Chengdu" {
		t.Fatalf("city = %q, want Chengdu from natural prompt", vars["city"])
	}
}

func TestApplyRunInputInferenceTrimsDateFromNaturalPromptCity(t *testing.T) {
	runArgs := map[string]interface{}{"user_prompt": "weather in Chengdu tomorrow", "_skill_infer_natural_prompt": true}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["city"] != "Chengdu" {
		t.Fatalf("city = %q, want Chengdu without date suffix", vars["city"])
	}
}

func TestApplyRunInputInferenceUsesDirectChineseNaturalPromptForSingleCity(t *testing.T) {
	runArgs := map[string]interface{}{"user_prompt": "\u6210\u90fd", "_skill_infer_natural_prompt": true}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["city"] != "\u6210\u90fd" {
		t.Fatalf("city = %q, want Chengdu in Chinese from natural prompt", vars["city"])
	}
}

func TestApplyRunInputInferenceDoesNotUseExplicitInputWhenMultipleParamsMissing(t *testing.T) {
	runArgs := map[string]interface{}{"input": "weather in Chengdu"}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city", "days"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["city"] != "" || vars["days"] != "" {
		t.Fatalf("vars = %#v, want no direct input inference with multiple missing params", vars)
	}
}

func TestApplyRunInputInferenceDoesNotTreatWeatherIntentAsCity(t *testing.T) {
	for _, prompt := range []string{"weather", "weather forecast", "\u5929\u6c14"} {
		vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
		entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

		ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

		if vars["city"] != "" {
			t.Fatalf("prompt %q inferred city = %q, want empty", prompt, vars["city"])
		}
	}
}

func TestApplyRunInputInferenceDoesNotGuessChineseCityPrompt(t *testing.T) {
	prompt := "\u8bf7\u67e5\u8be2\u6210\u90fd\u5929\u6c14"
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceDoesNotGuessChineseWeatherPrefixCity(t *testing.T) {
	prompt := "\u5929\u6c14 \u6210\u90fd"
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceDoesNotGuessChineseCityDateSuffix(t *testing.T) {
	prompt := "\u67e5\u4e00\u4e0b\u6210\u90fd\u660e\u5929\u5929\u6c14"
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceDoesNotGuessChineseCityDatePrefix(t *testing.T) {
	prompt := "\u67e5\u4e00\u4e0b\u660e\u5929\u6210\u90fd\u5929\u6c14"
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

	if vars["city"] != "" {
		t.Fatalf("city = %q, want empty without named argument", vars["city"])
	}
}

func TestApplyRunInputInferenceExtractsChineseNamedCity(t *testing.T) {
	prompt := "\u57ce\u5e02\uff1a\u4e0a\u6d77"
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": prompt})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": prompt})

	if vars["city"] != "\u4e0a\u6d77" {
		t.Fatalf("city = %q, want Shanghai in Chinese", vars["city"])
	}
}

func TestApplyRunInputInferenceUsesNamedParamAliases(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": "to_lang: English"})
	entry := &corelib.NLSkillEntry{
		Params: []corelib.NLSkillParam{{
			Name:        "target_language",
			Aliases:     []string{"to_lang"},
			Description: "target language",
			Required:    true,
		}},
	}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "to_lang: English"})

	if vars["target_language"] != "English" {
		t.Fatalf("target_language = %q, want English", vars["target_language"])
	}
}

func TestApplyRunInputInferencePromotesAliasBeforeRequiredValidation(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"file": "report.md"})
	entry := &corelib.NLSkillEntry{
		RequiredArgs: []string{"input"},
		Params:       []corelib.NLSkillParam{{Name: "input", Aliases: []string{"file"}, Required: true}},
	}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"file": "report.md"})

	if vars["input"] != "report.md" {
		t.Fatalf("input = %q, want alias promoted", vars["input"])
	}
}

func TestApplyRunInputInferencePromotesContentAliasToText(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"content": "hello"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"text"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"content": "hello"})

	if vars["text"] != "hello" {
		t.Fatalf("text = %q, want content alias promoted", vars["text"])
	}
}

func TestApplyRunInputInferenceFallsBackForTextParam(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"input": "translate this"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"text"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"input": "translate this"})

	if vars["text"] != "translate this" {
		t.Fatalf("text = %q, want inferred input", vars["text"])
	}
}

func TestApplyRunInputInferenceUsesScalarLegacyArgsAsCandidate(t *testing.T) {
	runArgs := map[string]interface{}{"args": "translate this text"}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"text"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["text"] != "translate this text" {
		t.Fatalf("text = %q, want inferred scalar args", vars["text"])
	}
	if vars["input"] != "translate this text" {
		t.Fatalf("input = %q, want scalar args preserved as input candidate", vars["input"])
	}
}

func TestApplyRunInputInferenceDoesNotGuessContentParamFromPrompt(t *testing.T) {
	runArgs := map[string]interface{}{"user_prompt": "draw a deployment flowchart"}
	vars := NormalizeRunVars(runArgs)
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"content"}}

	ApplyRunInputInference(entry, vars, runArgs)

	if vars["content"] != "" {
		t.Fatalf("content = %q, want empty without named argument", vars["content"])
	}
}
func TestApplyRunInputInferenceDoesNotWriteEmptyInferences(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"input": `{"city":"Chengdu"}`})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"unknown"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"input": `{"city":"Chengdu"}`})

	if _, ok := vars["unknown"]; ok {
		t.Fatalf("unknown inference should not be written: %#v", vars)
	}
}

func TestExtractRunExtraEnvAcceptsStringAssignments(t *testing.T) {
	got := ExtractRunExtraEnv("OPENAI_API_KEY=sk-test,HTTP_PROXY=http://127.0.0.1:7890")
	if got["OPENAI_API_KEY"] != "sk-test" || got["HTTP_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("ExtractRunExtraEnv() = %#v", got)
	}
}

func TestExtractRunExtraEnvFromArgsMergesAliases(t *testing.T) {
	got := ExtractRunExtraEnvFromArgs(map[string]interface{}{
		"env":         map[string]interface{}{"TOKEN": "from-env", "KEEP": "1"},
		"extra_env":   map[string]interface{}{"TOKEN": "from-extra"},
		"environment": []interface{}{"MODE=test"},
	})
	if got["TOKEN"] != "from-extra" || got["KEEP"] != "1" || got["MODE"] != "test" {
		t.Fatalf("ExtractRunExtraEnvFromArgs() = %#v", got)
	}
}

func TestExtractRunExtraEnvFromArgsReadsNestedArgsAliases(t *testing.T) {
	got := ExtractRunExtraEnvFromArgs(map[string]interface{}{
		"args": map[string]interface{}{
			"env":       map[string]interface{}{"TOKEN": "from-env"},
			"extra_env": map[string]interface{}{"TOKEN": "from-extra", "KEEP": "1"},
		},
	})
	if got["TOKEN"] != "from-extra" || got["KEEP"] != "1" {
		t.Fatalf("ExtractRunExtraEnvFromArgs() = %#v, want nested args env aliases merged", got)
	}
}

func TestExtractRunExtraEnvFromArgsReadsNestedJSONArgsAliases(t *testing.T) {
	got := ExtractRunExtraEnvFromArgs(map[string]interface{}{
		"args": `{"env":{"TOKEN":"from-env"},"extra_env":{"TOKEN":"from-extra","KEEP":"1"}}`,
	})
	if got["TOKEN"] != "from-extra" || got["KEEP"] != "1" {
		t.Fatalf("ExtractRunExtraEnvFromArgs() = %#v, want nested JSON args env aliases merged", got)
	}
}

func TestExtractRunExtraEnvAcceptsJSONStringAndNames(t *testing.T) {
	t.Setenv("PROCESS_TOKEN", "from-process")
	got := ExtractRunExtraEnv([]interface{}{
		`{"OPENAI_API_KEY":"sk-test"}`,
		"PROCESS_TOKEN",
		map[string]interface{}{"HTTP_PROXY": "http://127.0.0.1:7890"},
	})
	if got["OPENAI_API_KEY"] != "sk-test" || got["PROCESS_TOKEN"] != "from-process" || got["HTTP_PROXY"] != "http://127.0.0.1:7890" {
		t.Fatalf("ExtractRunExtraEnv() = %#v", got)
	}
}

func TestCollectSkillProvidedEnvReadsStepExtraEnv(t *testing.T) {
	entry := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"extra_env": map[string]interface{}{"API_TOKEN": "secret"}},
	}}}

	got := CollectSkillProvidedEnv(entry)

	if got["API_TOKEN"] != "secret" {
		t.Fatalf("CollectSkillProvidedEnv() = %#v", got)
	}
}

func TestCollectSkillProvidedEnvIgnoresPlaceholderValues(t *testing.T) {
	entry := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"extra_env": map[string]interface{}{
			"EMPTY_TOKEN": "",
			"SELF_TOKEN":  "${SELF_TOKEN}",
			"WIN_TOKEN":   "%WIN_TOKEN%",
			"REAL_TOKEN":  "secret",
		}},
	}}}

	got := CollectSkillProvidedEnv(entry)

	_, hasEmpty := got["EMPTY_TOKEN"]
	_, hasSelf := got["SELF_TOKEN"]
	_, hasWin := got["WIN_TOKEN"]
	if got["REAL_TOKEN"] != "secret" || hasEmpty || hasSelf || hasWin {
		t.Fatalf("CollectSkillProvidedEnv() = %#v", got)
	}
}

func TestBuildRunCheckContextMarksStepAndRunEnvProvided(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		SkillDir: `C:\skills\demo`,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"extra_env": map[string]interface{}{"STEP_TOKEN": "secret"}},
		}},
	}

	ctx := BuildRunCheckContext(entry, map[string]string{"RUN_TOKEN": "secret"})

	if ctx.SkillDir != entry.SkillDir || !ctx.ProvidedEnvVars["STEP_TOKEN"] || !ctx.ProvidedEnvVars["RUN_TOKEN"] || ctx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("BuildRunCheckContext() = %#v", ctx)
	}
}

func TestBuildRunCheckContextIgnoresEmptyProvidedEnv(t *testing.T) {
	ctx := BuildRunCheckContext(nil, map[string]string{
		"EMPTY_TOKEN": "",
		"SELF_TOKEN":  "${SELF_TOKEN}",
		"REAL_TOKEN":  "secret",
	})

	if !ctx.ProvidedEnvVars["REAL_TOKEN"] || ctx.ProvidedEnvVars["EMPTY_TOKEN"] || ctx.ProvidedEnvVars["SELF_TOKEN"] {
		t.Fatalf("BuildRunCheckContext() = %#v", ctx.ProvidedEnvVars)
	}
}

func TestBuildRunCheckContextForRunnerMarksGUIOpenAIProxyEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	entry := &corelib.NLSkillEntry{
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}

	guiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendTUI)

	if !guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || !guiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("GUI context should mark OpenAI proxy env provided: %#v", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || tuiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("TUI context should not mark GUI proxy env provided: %#v", tuiCtx.ProvidedEnvVars)
	}
}

func TestBuildRunCheckContextForRunnerBaseURLDoesNotSatisfyAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	entry := &corelib.NLSkillEntry{
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}

	guiCtx := BuildRunCheckContextForRunner(entry, map[string]string{
		"OPENAI_BASE_URL": "https://api.example.com/v1",
	}, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(entry, map[string]string{
		"OPENAI_BASE_URL": "https://api.example.com/v1",
	}, RunnerBackendTUI)

	if !guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("GUI context should still mark OPENAI_API_KEY provided by proxy: %#v", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("TUI context must not treat OPENAI_BASE_URL as OPENAI_API_KEY: %#v", tuiCtx.ProvidedEnvVars)
	}
}

func TestBuildRunCheckContextForRunnerAPIKeyDoesNotSatisfyBaseURL(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	entry := &corelib.NLSkillEntry{
		RequiredEnv: []string{"OPENAI_API_KEY", "OPENAI_BASE_URL"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}

	guiCtx := BuildRunCheckContextForRunner(entry, map[string]string{
		"OPENAI_API_KEY": "sk-user",
	}, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(entry, map[string]string{
		"OPENAI_API_KEY": "sk-user",
	}, RunnerBackendTUI)

	if !guiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("GUI context should mark OPENAI_BASE_URL provided by proxy: %#v", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("TUI context must not treat OPENAI_API_KEY as OPENAI_BASE_URL: %#v", tuiCtx.ProvidedEnvVars)
	}
}

func TestBuildRunCheckContextForRunnerMarksGUIOpenAIProxyEnvFromStepRequiredEnv(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	entry := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":      "echo ok",
				"required_env": []interface{}{"openai_api_key"},
			},
		}},
	}

	guiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendGUI)
	tuiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendTUI)

	if !guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || !guiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("GUI context should mark step-level OpenAI proxy env provided: %#v", guiCtx.ProvidedEnvVars)
	}
	if tuiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || tuiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("TUI context should not mark GUI proxy env provided: %#v", tuiCtx.ProvidedEnvVars)
	}
}

func TestRequiredArgsForRunnerPrecheckScopesLegacyArgsToActiveSteps(t *testing.T) {
	steps := []corelib.NLSkillStep{
		{Action: "bash", Params: map[string]interface{}{"command": "echo {{safe_input}}"}},
	}
	got := RequiredArgsForRunnerPrecheck([]string{"safe_input", "danger_input"}, steps)
	if len(got) != 1 || got[0] != "safe_input" {
		t.Fatalf("RequiredArgsForRunnerPrecheck() = %#v, want only active step placeholder", got)
	}

	if got := RequiredArgsForRunnerPrecheck([]string{"safe_input"}, nil); len(got) != 0 {
		t.Fatalf("RequiredArgsForRunnerPrecheck(nil steps) = %#v, want no blocking legacy args", got)
	}
}

func TestBuildRunCheckContextForRunnerSkipsInactiveOpenAIProxyProbe(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	entry := &corelib.NLSkillEntry{
		Steps: []corelib.NLSkillStep{
			{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo ok"},
			},
			{
				Action: "bash",
				When:   "{{mode}} == openai",
				Params: map[string]interface{}{
					"command":      "echo $OPENAI_API_KEY",
					"required_env": []interface{}{"OPENAI_API_KEY"},
				},
			},
		},
	}

	guiCtx := BuildRunCheckContextForRunner(entry, nil, RunnerBackendGUI)

	if guiCtx.ProvidedEnvVars["OPENAI_API_KEY"] || guiCtx.ProvidedEnvVars["OPENAI_BASE_URL"] {
		t.Fatalf("inactive OpenAI step should not mark proxy env provided: %#v", guiCtx.ProvidedEnvVars)
	}
}

func TestMissingRequiredArgsHonorsCanonicalKeysAndAliases(t *testing.T) {
	missing := MissingRequiredArgs([]string{"Input-File", "text", "output"}, map[string]string{
		"input_file": "report.md",
		"content":    "hello",
	})

	if len(missing) != 1 || missing[0] != "output" {
		t.Fatalf("MissingRequiredArgs() = %#v, want [output]", missing)
	}
}

func TestMissingRequiredParamsHonorsAliasesAndDefaults(t *testing.T) {
	missing := MissingRequiredParams([]corelib.NLSkillParam{
		{Name: "Input-File", Aliases: []string{"file"}, Required: true},
		{Name: "format", Required: true, Default: "pdf"},
		{Name: "target-lang", Required: true},
		{Name: "optional", Required: false},
	}, map[string]string{"file": "report.md"})

	if len(missing) != 1 || missing[0] != "target_lang" {
		t.Fatalf("MissingRequiredParams() = %#v, want [target_lang]", missing)
	}
}

func TestMissingRequiredParamsHonorsCommonAliases(t *testing.T) {
	missing := MissingRequiredParams([]corelib.NLSkillParam{
		{Name: "input", Required: true},
		{Name: "content", Required: true},
	}, map[string]string{
		"file":    "report.md",
		"message": "hello",
	})

	if len(missing) != 0 {
		t.Fatalf("MissingRequiredParams() = %#v, want common aliases to satisfy params", missing)
	}
}

func TestMissingRequiredParamsCommonAliasDoesNotHideDeclaredParam(t *testing.T) {
	missing := MissingRequiredParams([]corelib.NLSkillParam{
		{Name: "text", Required: true},
		{Name: "content", Required: true},
	}, map[string]string{"text": "hello"})

	if len(missing) != 1 || missing[0] != "content" {
		t.Fatalf("MissingRequiredParams() = %#v, want [content]", missing)
	}
}

func TestMissingRunRequiredArgsCombinesLegacyAndParamSchema(t *testing.T) {
	missing := MissingRunRequiredArgs(
		[]string{"input", "output"},
		[]corelib.NLSkillParam{
			{Name: "output", Required: true},
			{Name: "target_lang", Required: true},
		},
		map[string]string{"input": "in.md"},
	)

	if len(missing) != 2 || missing[0] != "output" || missing[1] != "target_lang" {
		t.Fatalf("MissingRunRequiredArgs() = %#v, want [output target_lang]", missing)
	}
}

func TestMissingRunRequiredArgsLetsParamSchemaSatisfyLegacyArgs(t *testing.T) {
	missing := MissingRunRequiredArgs(
		[]string{"Input-File", "format", "content"},
		[]corelib.NLSkillParam{
			{Name: "Input-File", Aliases: []string{"file"}, Required: true},
			{Name: "format", Required: true, Default: "pdf"},
			{Name: "text", Required: true, Default: "hello"},
		},
		map[string]string{"file": "in.md"},
	)

	if len(missing) != 0 {
		t.Fatalf("MissingRunRequiredArgs() = %#v, want aliases/defaults to satisfy legacy args", missing)
	}
}

func TestDetectImplicitRequiredArgsHonorsCommonAliases(t *testing.T) {
	missing := DetectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "cat {{input}}"},
	}}, map[string]string{"file": "report.md"})

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRequiredArgs() = %#v, want alias to satisfy input", missing)
	}
}

func TestDetectImplicitRequiredArgsHonorsTextAliases(t *testing.T) {
	missing := DetectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "translate {{text}}"},
	}}, map[string]string{"content": "hello"})

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRequiredArgs() = %#v, want content alias to satisfy text", missing)
	}
}

func TestDetectImplicitRunRequiredArgsCommonAliasDoesNotHideDeclaredParam(t *testing.T) {
	params := []corelib.NLSkillParam{
		{Name: "text", Required: true},
		{Name: "content", Required: true},
	}
	vars := map[string]string{"text": "hello"}

	implicit := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "emit {{text}} {{content}}"},
	}}, vars, nil, params)
	if len(implicit) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want schema-owned params skipped for required validation", implicit)
	}

	missing := MissingRunRequiredArgs(nil, params, vars)
	if len(missing) != 1 || missing[0] != "content" {
		t.Fatalf("MissingRunRequiredArgs() = %#v, want [content]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsSkipsSchemaOwnedAliasPlaceholder(t *testing.T) {
	params := []corelib.NLSkillParam{{Name: "text", Required: true}}
	steps := []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `python translate.py --content "{{content}}"`},
	}}

	implicit := DetectImplicitRunRequiredArgs(steps, nil, nil, params)
	if len(implicit) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want alias placeholder owned by schema", implicit)
	}

	missing := MissingRunRequiredArgs(nil, params, nil)
	if len(missing) != 1 || missing[0] != "text" {
		t.Fatalf("MissingRunRequiredArgs() = %#v, want [text]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsSkipsExplicitOptionalAliasPlaceholder(t *testing.T) {
	params := []corelib.NLSkillParam{{Name: "text"}}
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `python translate.py --content "{{content}}"`},
	}}, nil, nil, params)

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want explicit optional alias placeholder skipped", missing)
	}
}

func TestDetectImplicitRequiredArgsNormalizesCommandActions(t *testing.T) {
	missing := DetectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "run",
		Params: map[string]interface{}{"command": "convert {{Input File}} --out {{output}}"},
	}}, map[string]string{"input_file": "report.md"})

	if len(missing) != 1 || missing[0] != "output" {
		t.Fatalf("DetectImplicitRequiredArgs() = %#v, want normalized command action to require output", missing)
	}
}

func TestDetectImplicitRunRequiredArgsScansWorkingDirPlaceholders(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{
			"command":     "echo ok",
			"working_dir": "{{project_dir}}",
		},
	}}, nil, nil, nil)

	if len(missing) != 1 || missing[0] != "project_dir" {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want [project_dir]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsDoesNotTreatWhenControlAsRequired(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		When:   "{{include_extra}}",
		Params: map[string]interface{}{"command": "echo ok"},
	}}, nil, nil, nil)

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want when control ignored", missing)
	}
}

func TestDetectImplicitRunRequiredArgsScansCraftToolTask(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{"task": "summarize {{topic}}"},
	}}, nil, nil, nil)

	if len(missing) != 1 || missing[0] != "topic" {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want [topic]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsIgnoresCraftToolInstructions(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "craft_tool",
		Params: map[string]interface{}{
			"instructions": "Use the examples literally: {{name}} and {{variable}}.",
		},
	}}, nil, nil, nil)

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want instructions ignored", missing)
	}
}

func TestDetectImplicitRequiredArgsDoesNotMutateStepParams(t *testing.T) {
	steps := []corelib.NLSkillStep{{
		Action: "run",
		Params: map[string]interface{}{"cmd": "convert {{input}} {{output}}"},
	}}

	missing := DetectImplicitRequiredArgs(steps, map[string]string{"input": "in.md"})

	if len(missing) != 1 || missing[0] != "output" {
		t.Fatalf("DetectImplicitRequiredArgs() = %#v, want output", missing)
	}
	if steps[0].Action != "run" {
		t.Fatalf("DetectImplicitRequiredArgs mutated action: %q", steps[0].Action)
	}
	if steps[0].Params["command"] != nil || steps[0].Params["cmd"] != "convert {{input}} {{output}}" {
		t.Fatalf("DetectImplicitRequiredArgs mutated params: %#v", steps[0].Params)
	}
}

func TestDetectImplicitRequiredArgsCanonicalizesPlaceholderNames(t *testing.T) {
	missing := DetectImplicitRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "tool {{Input-File}} ${Output File} {{input_file}}"},
	}}, map[string]string{"input_file": "report.md"})

	if len(missing) != 1 || missing[0] != "output_file" {
		t.Fatalf("DetectImplicitRequiredArgs() = %#v, want canonical output_file only", missing)
	}
}

func TestDetectImplicitRunRequiredArgsSkipsOptionalFlagOutsideRequiredArgs(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `python translate.py --text "{{text}}" --target_lang "{{target_lang}}"`},
	}}, map[string]string{"text": "hello"}, []string{"text"}, nil)

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want optional target_lang flag skipped", missing)
	}
}

func TestDetectImplicitRunRequiredArgsSkipsOptionalSlashFlagOutsideRequiredArgs(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `render.exe /input:"{{input}}" /out:{output}`},
	}}, map[string]string{"input": "in.drawio"}, []string{"input"}, nil)

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want optional slash-style output flag skipped", missing)
	}
}

func TestDetectImplicitRunRequiredArgsStillRequiresUndeclaredPositional(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": "convert {{input}} {{output}}"},
	}}, map[string]string{"input": "in.md"}, []string{"input"}, nil)

	if len(missing) != 1 || missing[0] != "output" {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want [output]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsStillRequiresFlagWhenNoContract(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `python weather.py --city "{{city}}"`},
	}}, nil, nil, nil)

	if len(missing) != 1 || missing[0] != "city" {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want [city]", missing)
	}
}

func TestDetectImplicitRunRequiredArgsSkipsExplicitOptionalParam(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `tool --target_lang "{{target_lang}}"`},
	}}, nil, nil, []corelib.NLSkillParam{{Name: "target_lang", Required: false}})

	if len(missing) != 0 {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want explicit optional param skipped", missing)
	}
}

func TestDetectImplicitRunRequiredArgsCatchesUndeclaredFlagWithExplicitSchema(t *testing.T) {
	missing := DetectImplicitRunRequiredArgs([]corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"command": `tool --input "{{input}}" --output "{{output}}"`},
	}}, map[string]string{"input": "in.md"}, nil, []corelib.NLSkillParam{{Name: "input", Required: true}})

	if len(missing) != 1 || missing[0] != "output" {
		t.Fatalf("DetectImplicitRunRequiredArgs() = %#v, want undeclared output flag required", missing)
	}
}

func TestHydrateRunMetadataFromDirLoadsStructuredYAML(t *testing.T) {
	skillDir := t.TempDir()
	content := []byte("name: yaml-tool\nrequired_args: [input]\nsteps:\n  - action: run\n    command: echo {{input}}\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), content, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "yaml-tool", SkillDir: skillDir}

	if err := HydrateRunMetadataFromDir(entry); err != nil {
		t.Fatalf("HydrateRunMetadataFromDir() error = %v", err)
	}

	if len(entry.Steps) != 1 || entry.Steps[0].Action != "bash" {
		t.Fatalf("hydrated steps = %#v, want normalized bash step", entry.Steps)
	}
	if cmd, _ := entry.Steps[0].Params["command"].(string); !strings.Contains(cmd, "echo {{input}}") {
		t.Fatalf("hydrated command = %q", cmd)
	}
	if len(entry.RequiredArgs) != 1 || entry.RequiredArgs[0] != "input" {
		t.Fatalf("hydrated required args = %#v", entry.RequiredArgs)
	}
}

func TestHydrateRunMetadataFromDirFallsBackToMarkdownWhenYAMLHasEmptySteps(t *testing.T) {
	skillDir := t.TempDir()
	yamlContent := []byte("name: xh-md-to-pdf\nrequired_args: [input]\nplatforms: [universal]\nsteps: []\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), yamlContent, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.yaml) error = %v", err)
	}
	markdownContent := []byte("# Markdown fallback\n\n```bash\necho {{input}}\n```\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), markdownContent, 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "xh-md-to-pdf", SkillDir: skillDir}

	if err := HydrateRunMetadataFromDir(entry); err != nil {
		t.Fatalf("HydrateRunMetadataFromDir() error = %v", err)
	}

	if len(entry.Steps) != 1 || entry.Steps[0].Action != "bash" {
		t.Fatalf("hydrated steps = %#v, want markdown bash step", entry.Steps)
	}
	if cmd, _ := entry.Steps[0].Params["command"].(string); !strings.Contains(cmd, "echo {{input}}") {
		t.Fatalf("hydrated command = %q", cmd)
	}
	if len(entry.RequiredArgs) != 1 || entry.RequiredArgs[0] != "input" {
		t.Fatalf("hydrated required args = %#v", entry.RequiredArgs)
	}
	if len(entry.Platforms) != 1 || entry.Platforms[0] != "universal" {
		t.Fatalf("hydrated platforms = %#v", entry.Platforms)
	}
}

func TestHydrateRunMetadataFromDirPreservesKnowledgeType(t *testing.T) {
	skillDir := t.TempDir()
	markdownContent := []byte("# Knowledge\n\n```bash\necho example-only\n```\n")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), markdownContent, 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	entry := &corelib.NLSkillEntry{Name: "knowledge", Type: "knowledge", SkillDir: skillDir}

	if err := HydrateRunMetadataFromDir(entry); err != nil {
		t.Fatalf("HydrateRunMetadataFromDir() error = %v", err)
	}

	if len(entry.Steps) != 0 {
		t.Fatalf("hydrated steps = %#v, want knowledge skill to stay non-executable", entry.Steps)
	}
	if entry.Type != "knowledge" {
		t.Fatalf("Type = %q, want knowledge", entry.Type)
	}
}

func TestHydrateRunMetadataCopiesRuntimeFields(t *testing.T) {
	dst := &corelib.NLSkillEntry{Name: "cached", ProducesArtifact: true, RequiresGUI: true}
	src := &corelib.NLSkillEntry{
		Description:      "full definition",
		Platforms:        []string{"windows"},
		Steps:            []corelib.NLSkillStep{{Action: "bash"}},
		Params:           []corelib.NLSkillParam{{Name: "city", Required: true}},
		RequiredArgs:     []string{"city"},
		RequiredEnv:      []string{"API_KEY"},
		RequiresPython:   []string{"requests"},
		Capabilities:     []string{"current_data"},
		RequiresTools:    []string{"browser"},
		FallbackForTools: []string{"web_fetch"},
		Pipeline:         []corelib.SkillPipelineStep{{Skill: "next"}},
		References:       []corelib.SkillReference{{Filename: "guide.md", Description: "guide"}},
		Mode:             "api_workflow",
		PreferredShell:   "powershell",
		SkillDir:         `C:\skills\cached`,
		ProducesArtifact: false,
		RequiresGUI:      false,
		Stateful:         true,
	}

	HydrateRunMetadata(dst, src)

	if len(dst.Steps) != 1 || len(dst.Params) != 1 || dst.RequiredArgs[0] != "city" || dst.RequiredEnv[0] != "API_KEY" || dst.Platforms[0] != "windows" || dst.Capabilities[0] != "current_data" || dst.RequiresTools[0] != "browser" || dst.FallbackForTools[0] != "web_fetch" || dst.Pipeline[0].Skill != "next" || dst.References[0].Filename != "guide.md" || dst.Mode != "api_workflow" || dst.PreferredShell != "powershell" || dst.ProducesArtifact || dst.RequiresGUI || !dst.Stateful {
		t.Fatalf("HydrateRunMetadata() = %#v", dst)
	}
}

func TestHydrateRunMetadataPreservesBoolTrueWhenDestinationHasRuntimeDefinition(t *testing.T) {
	dst := &corelib.NLSkillEntry{
		Name:             "existing",
		Steps:            []corelib.NLSkillStep{{Action: "bash"}},
		ProducesArtifact: true,
		RequiresGUI:      true,
	}
	src := &corelib.NLSkillEntry{ProducesArtifact: false, RequiresGUI: false}

	HydrateRunMetadata(dst, src)

	if !dst.ProducesArtifact || !dst.RequiresGUI {
		t.Fatalf("HydrateRunMetadata() should preserve existing true bools: %#v", dst)
	}
}

func TestHydrateRunMetadataLetsSourceControlBoolsWhenDestinationOnlyHasConstraints(t *testing.T) {
	dst := &corelib.NLSkillEntry{
		Name:             "cached-shell",
		Platforms:        []string{"windows"},
		RequiredArgs:     []string{"input"},
		ProducesArtifact: true,
		RequiresGUI:      true,
	}
	src := &corelib.NLSkillEntry{
		Steps:            []corelib.NLSkillStep{{Action: "bash"}},
		ProducesArtifact: false,
		RequiresGUI:      false,
	}

	HydrateRunMetadata(dst, src)

	if dst.ProducesArtifact || dst.RequiresGUI {
		t.Fatalf("HydrateRunMetadata() should let hydrated execution metadata control bools: %#v", dst)
	}
}

func TestMergeExtraEnvParamLetsRunEnvOverrideStepDefaults(t *testing.T) {
	params := map[string]interface{}{"extra_env": map[string]interface{}{"SHARED": "step"}}

	MergeExtraEnvParam(params, map[string]string{"SHARED": "run", "RUN_ONLY": "1"})

	got := params["extra_env"].(map[string]interface{})
	if got["SHARED"] != "run" || got["RUN_ONLY"] != "1" {
		t.Fatalf("extra_env = %#v", got)
	}
}

func TestPrepareResolvedStepEnvAppliesBashEnvContract(t *testing.T) {
	step := corelib.NLSkillStep{
		Action: "shell",
		Params: map[string]interface{}{
			"command":   "echo ok",
			"extra_env": map[string]interface{}{"SHARED": "step"},
		},
	}

	got := PrepareResolvedStepEnv(step, []string{"TOKEN"}, map[string]string{"SHARED": "run", "RUN_ONLY": "1"})

	if got.Params == nil {
		t.Fatal("PrepareResolvedStepEnv() dropped params")
	}
	required := stringListParam(got.Params["required_env"])
	if len(required) != 1 || required[0] != "TOKEN" {
		t.Fatalf("required_env = %#v", got.Params["required_env"])
	}
	extra := got.Params["extra_env"].(map[string]interface{})
	if extra["SHARED"] != "run" || extra["RUN_ONLY"] != "1" {
		t.Fatalf("extra_env = %#v", extra)
	}
}

func TestPrepareResolvedStepEnvIgnoresNonBashSteps(t *testing.T) {
	step := corelib.NLSkillStep{Action: "craft_tool"}

	got := PrepareResolvedStepEnv(step, []string{"TOKEN"}, map[string]string{"TOKEN": "run"})

	if got.Params != nil {
		t.Fatalf("PrepareResolvedStepEnv(non-bash) params = %#v", got.Params)
	}
}

func TestBuildCommandEnvInjectsRequiredAndExtraEnv(t *testing.T) {
	t.Setenv("REQUIRED_TOKEN", "from-process")
	got := BuildCommandEnv([]string{"PATH=base"}, map[string]interface{}{
		"requires_env": []interface{}{"REQUIRED_TOKEN", "MISSING_TOKEN"},
		"extra_env":    map[string]interface{}{"EXTRA_TOKEN": 123},
	})

	if !containsEnv(got, "REQUIRED_TOKEN=from-process") || !containsEnv(got, "EXTRA_TOKEN=123") || containsEnvPrefix(got, "MISSING_TOKEN=") {
		t.Fatalf("BuildCommandEnv() = %#v", got)
	}
}

func TestBuildCommandEnvUpsertsRequiredAndExtraEnv(t *testing.T) {
	t.Setenv("TOKEN", "from-process")
	got := BuildCommandEnv([]string{"TOKEN=base", "KEEP=1"}, map[string]interface{}{
		"required_env": []interface{}{"TOKEN"},
		"extra_env":    map[string]interface{}{"TOKEN": "from-run"},
	})

	if !containsEnv(got, "TOKEN=from-run") || containsEnv(got, "TOKEN=base") || containsEnv(got, "TOKEN=from-process") || !containsEnv(got, "KEEP=1") {
		t.Fatalf("BuildCommandEnv() = %#v", got)
	}
}

func TestBuildCommandEnvDoesNotOverrideRequiredEnvWithPlaceholders(t *testing.T) {
	t.Setenv("EMPTY_TOKEN", "from-process-empty")
	t.Setenv("SELF_TOKEN", "from-process-self")
	got := BuildCommandEnv([]string{"KEEP=1"}, map[string]interface{}{
		"required_env": []interface{}{"EMPTY_TOKEN", "SELF_TOKEN"},
		"extra_env": map[string]interface{}{
			"EMPTY_TOKEN": "",
			"SELF_TOKEN":  "${SELF_TOKEN}",
		},
	})

	if !containsEnv(got, "EMPTY_TOKEN=from-process-empty") || !containsEnv(got, "SELF_TOKEN=from-process-self") || !containsEnv(got, "KEEP=1") {
		t.Fatalf("BuildCommandEnv() = %#v", got)
	}
}

func TestFoldUnconsumedArgsToInputSkipsAppFileCarriers(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{
		"input":              `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`,
		"app_id":             "skill-app-paper_pdf_translator-app-pdf",
		"app_kind":           "tool_app",
		"app_name":           "PDF翻译工具",
		"file":               map[string]interface{}{"name": "paper.pdf", "staged_path": `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`},
		"file_name":          "paper.pdf",
		"file_path":          `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`,
		"file_paths":         []interface{}{`C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`},
		"files":              []interface{}{map[string]interface{}{"name": "paper.pdf"}},
		"fields":             map[string]interface{}{},
		"file_text":          "",
		"input_file_path":    `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`,
		"input_mode":         "file",
		"local_file_path":    `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`,
		"_maclaw_app":        "true",
		"output":             `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper_output.pdf`,
		"output_mode":        "pdf",
		"params":             map[string]interface{}{},
		"prompt":             "Run MaClaw tool app: PDF translator",
		"uploaded_file_path": `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`,
	})

	FoldUnconsumedArgsToInput(vars, nil)

	if got, want := vars["input"], `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`; got != want {
		t.Fatalf("input = %q, want only uploaded file path %q", got, want)
	}
}

func TestResolveStepAppFileRunDoesNotAppendAppMetadata(t *testing.T) {
	pdfPath := `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper.pdf`
	vars := NormalizeRunVars(map[string]interface{}{
		"input":              pdfPath,
		"app_id":             "skill-app-paper_pdf_translator-app-pdf",
		"app_kind":           "tool_app",
		"app_name":           "PDF翻译工具",
		"file_path":          pdfPath,
		"input_file_path":    pdfPath,
		"input_mode":         "file",
		"output":             `C:\Users\me\.maclaw\temp\app-inputs\input-1\paper_output.pdf`,
		"output_mode":        "pdf",
		"params":             map[string]interface{}{},
		"prompt":             "Run MaClaw tool app: PDF翻译工具",
		"uploaded_file_path": pdfPath,
	})
	FoldUnconsumedArgsToInput(vars, nil)

	resolved, err := ResolveStep(corelib.NLSkillStep{
		Action: "bash",
		Params: map[string]interface{}{"command": `"C:/Python/python.exe" "C:/skill/run.py"`},
	}, vars, "", nil, func(s string) string { return `"` + s + `"` })
	if err != nil {
		t.Fatalf("ResolveStep() error = %v", err)
	}
	command, _ := resolved.Step.Params["command"].(string)
	for _, forbidden := range []string{"app_id:", "prompt:", "input_mode:", "params:", "\n"} {
		if strings.Contains(command, forbidden) {
			t.Fatalf("command contains folded app metadata %q: %q", forbidden, command)
		}
	}
	if !strings.Contains(command, `"`+pdfPath+`"`) {
		t.Fatalf("command = %q, want quoted pdf path", command)
	}
}

func TestBuildCommandEnvPrependsWindowsSystemDirs(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PATH repair is only active on Windows")
	}
	t.Setenv("SystemRoot", `C:\Windows`)

	got := BuildCommandEnv([]string{"Path=base"}, nil)
	pathValue := commandEnvValue(got, "PATH")

	for _, want := range []string{
		`C:\Windows\System32`,
		`C:\Windows`,
		`C:\Windows\System32\Wbem`,
		`C:\Windows\System32\WindowsPowerShell\v1.0`,
	} {
		if !pathHasDir(pathValue, want) {
			t.Fatalf("PATH = %q, missing %q", pathValue, want)
		}
	}
}

func TestBuildRunCheckContextPlansSharedPythonRuntime(t *testing.T) {
	dataDir := t.TempDir()
	entry := &corelib.NLSkillEntry{
		Name:           "paper_pdf_translator",
		RequiresPython: []string{"requests", "babeldoc==0.6.3"},
	}
	ctx := BuildRunCheckContextWithDataDir(dataDir, entry, nil)
	if ctx.PythonPath == "" || !strings.Contains(ctx.PythonPath, filepath.Join("runtimes", "python", "envs")) {
		t.Fatalf("PythonPath = %q, want shared runtime path", ctx.PythonPath)
	}
	if !strings.HasPrefix(ctx.PythonPath, dataDir) {
		t.Fatalf("PythonPath = %q, want under data dir %q", ctx.PythonPath, dataDir)
	}
	if ctx.PythonRuntimeDataDir != dataDir {
		t.Fatalf("PythonRuntimeDataDir = %q, want %q", ctx.PythonRuntimeDataDir, dataDir)
	}
	if len(ctx.PythonRuntimePackages) != 2 || ctx.PythonRuntimePackages[0] != "babeldoc==0.6.3" || ctx.PythonRuntimePackages[1] != "requests" {
		t.Fatalf("PythonRuntimePackages = %#v", ctx.PythonRuntimePackages)
	}
	reqs := ExtractRequirements(entry, ctx)
	if len(reqs) == 0 || reqs[0].Context["python_runtime_packages"] == "" || reqs[0].Context["python_path"] != ctx.PythonPath {
		t.Fatalf("pip requirement missing shared runtime context: %#v", reqs)
	}
	if reqs[0].Context["python_runtime_data_dir"] != dataDir {
		t.Fatalf("pip requirement data dir = %q, want %q", reqs[0].Context["python_runtime_data_dir"], dataDir)
	}
}

func TestExtractRequirementsCarriesSharedPythonRuntimeWithoutPythonPath(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		Name:           "pdf-word",
		RequiresPython: []string{"pymupdf", "python-docx"},
	}
	ctx := DefaultCheckContext()
	ctx.PythonRuntimePackages = []string{"pymupdf", "python-docx"}
	ctx.PythonRuntimeConstraint = ">=3.10,<3.14"
	ctx.PythonRuntimeManager = "uv"
	ctx.PythonRuntimeDataDir = t.TempDir()

	reqs := ExtractRequirements(entry, ctx)
	if len(reqs) == 0 {
		t.Fatal("expected pip requirements")
	}
	for _, req := range reqs {
		if req.Type != "pip" {
			continue
		}
		if req.Context["python_runtime_packages"] == "" {
			t.Fatalf("pip requirement missing shared runtime context: %#v", req)
		}
		if _, ok := req.Context["python_path"]; ok {
			t.Fatalf("python_path should not be synthesized when unresolved: %#v", req.Context)
		}
		if req.Context["python_runtime_data_dir"] != ctx.PythonRuntimeDataDir {
			t.Fatalf("python_runtime_data_dir = %q, want %q", req.Context["python_runtime_data_dir"], ctx.PythonRuntimeDataDir)
		}
	}
}

func TestBuildRunCheckContextUsesDefaultDataDirForSharedPythonRuntime(t *testing.T) {
	oldBaseDir := corelib.MaclawBaseDir()
	baseDir := t.TempDir()
	corelib.SetMaclawBaseDir(baseDir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBaseDir) })

	entry := &corelib.NLSkillEntry{
		Name:           "ocr_runtime",
		RequiresPython: []string{"rapidocr-onnxruntime==1.4.4"},
	}
	ctx := BuildRunCheckContext(entry, nil)
	wantDataDir := filepath.Join(baseDir, "data")
	if ctx.PythonRuntimeDataDir != wantDataDir {
		t.Fatalf("PythonRuntimeDataDir = %q, want %q", ctx.PythonRuntimeDataDir, wantDataDir)
	}
	if !strings.HasPrefix(ctx.PythonPath, wantDataDir) {
		t.Fatalf("PythonPath = %q, want under default data dir %q", ctx.PythonPath, wantDataDir)
	}
}

func TestSharedPythonRuntimeExtraEnvPrependsRuntimePath(t *testing.T) {
	dataDir := t.TempDir()
	entry := &corelib.NLSkillEntry{Name: "demo", RequiresPython: []string{"requests"}}
	env := SharedPythonRuntimeExtraEnvWithDataDir(dataDir, entry, []string{"PATH=base"})
	if env["MACLAW_PYTHON"] == "" || env["MACLAW_PYTHON_RUNTIME_REF"] == "" || env["VIRTUAL_ENV"] == "" {
		t.Fatalf("runtime env missing metadata: %#v", env)
	}
	if !strings.HasPrefix(env["MACLAW_PYTHON"], dataDir) || !strings.HasPrefix(env["VIRTUAL_ENV"], dataDir) {
		t.Fatalf("runtime env should use data dir %q: %#v", dataDir, env)
	}
	pythonDir := filepath.Dir(env["MACLAW_PYTHON"])
	if !strings.HasPrefix(env["PATH"], pythonDir+string(os.PathListSeparator)) {
		t.Fatalf("PATH = %q, want prefix %q", env["PATH"], pythonDir)
	}
}

func TestSharedPythonRuntimeExtraEnvUsesDefaultDataDir(t *testing.T) {
	oldBaseDir := corelib.MaclawBaseDir()
	baseDir := t.TempDir()
	corelib.SetMaclawBaseDir(baseDir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBaseDir) })

	entry := &corelib.NLSkillEntry{Name: "ocr-runtime", RequiresPython: []string{"rapidocr-onnxruntime==1.4.4"}}
	env := SharedPythonRuntimeExtraEnv(entry, []string{"PATH=base"})
	wantDataDir := filepath.Join(baseDir, "data")
	if env["MACLAW_PYTHON"] == "" || env["VIRTUAL_ENV"] == "" || env["MACLAW_PYTHON_RUNTIME_REF"] == "" {
		t.Fatalf("runtime env missing shared Python metadata: %#v", env)
	}
	if !strings.HasPrefix(env["MACLAW_PYTHON"], wantDataDir) || !strings.HasPrefix(env["VIRTUAL_ENV"], wantDataDir) {
		t.Fatalf("runtime env should use default data dir %q: %#v", wantDataDir, env)
	}
}

func TestRunnerStepTimeoutSecondsParsesAndCapsTimeout(t *testing.T) {
	got := RunnerStepTimeoutSeconds(map[string]interface{}{"timeout": "1200"}, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)
	if got != 900 {
		t.Fatalf("RunnerStepTimeoutSeconds() = %d, want capped 900", got)
	}
}

func TestRunnerStepTimeoutSecondsIgnoresInvalidStringTimeout(t *testing.T) {
	got := RunnerStepTimeoutSeconds(map[string]interface{}{"timeout": "30s"}, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)
	if got != corelib.DefaultAgentTimeoutSec {
		t.Fatalf("RunnerStepTimeoutSeconds() = %d, want default %d", got, corelib.DefaultAgentTimeoutSec)
	}
}

func TestRunnerStepTimeoutSecondsClampsBelowDefaultTimeout(t *testing.T) {
	got := RunnerStepTimeoutSeconds(map[string]interface{}{"timeout": "120"}, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)
	if got != corelib.DefaultAgentTimeoutSec {
		t.Fatalf("RunnerStepTimeoutSeconds() = %d, want minimum %d", got, corelib.DefaultAgentTimeoutSec)
	}
}

func TestRunnerStepTimeoutSecondsCapsGlobalTimeout(t *testing.T) {
	got := RunnerStepTimeoutSeconds(map[string]interface{}{
		"timeout":        float64(700),
		"global_timeout": "1200",
	}, corelib.DefaultAgentTimeoutSec, corelib.MaxAgentTimeoutSec)
	if got != corelib.MaxAgentTimeoutSec {
		t.Fatalf("RunnerStepTimeoutSeconds() = %d, want capped %d", got, corelib.MaxAgentTimeoutSec)
	}
}

func TestRunnerStepTimeoutSecondsWithMinKeepsExplicitTimeoutBelowDefault(t *testing.T) {
	got := RunnerStepTimeoutSecondsWithMin(map[string]interface{}{"timeout": "700"}, 3600, corelib.MinSkillRunnerTimeoutSec, corelib.MaxSkillRunnerTimeoutSec)
	if got != 700 {
		t.Fatalf("RunnerStepTimeoutSecondsWithMin() = %d, want explicit timeout 700", got)
	}
}

func containsEnv(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsEnvPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func pathHasDir(pathValue, want string) bool {
	want = strings.TrimRight(filepath.Clean(want), `\/`)
	if runtime.GOOS == "windows" {
		want = strings.ToLower(want)
	}
	for _, part := range strings.Split(pathValue, string(os.PathListSeparator)) {
		part = strings.TrimRight(filepath.Clean(part), `\/`)
		if runtime.GOOS == "windows" {
			part = strings.ToLower(part)
		}
		if part == want {
			return true
		}
	}
	return false
}

func TestIsManageSkillRunnerControlKeyKeepsRuntimeSelectors(t *testing.T) {
	for _, key := range []string{"query", "mode", "user_prompt", "target_lang"} {
		if IsManageSkillRunnerControlKey(key) {
			t.Fatalf("%s should be forwarded to runner", key)
		}
	}
	for _, key := range []string{"action", "name", "wait_seconds", "run_id", "skill_id", "auto_run", "auto_fix", "force", "step_index", "field", "value", "find", "replace", "reason", "_runtime_platform", "_runtime_policy_owner_id"} {
		if !IsManageSkillRunnerControlKey(key) {
			t.Fatalf("%s should be consumed by manage_skill", key)
		}
	}
}
