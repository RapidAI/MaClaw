package skill

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNormalizeRunVarsParsesJSONInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"input": `{"city":"成都","days":3}`})
	if got["city"] != "成都" || got["days"] != "3" || got["input"] == "" {
		t.Fatalf("NormalizeRunVars() = %#v", got)
	}
}

func TestNormalizeRunVarsAcceptsJSONStringArgs(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"args": `{"city":"成都","days":3}`})
	if got["city"] != "成都" || got["days"] != "3" {
		t.Fatalf("NormalizeRunVars() = %#v, want JSON args unpacked", got)
	}
}

func TestNormalizeRunVarsCopiesArbitraryTopLevelParams(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{"city": "成都", "count": 2, "action": "run", "name": "weather"})
	if got["city"] != "成都" || got["count"] != "2" {
		t.Fatalf("NormalizeRunVars() = %#v, want arbitrary top-level params", got)
	}
	if _, ok := got["action"]; ok {
		t.Fatalf("NormalizeRunVars() should not copy control keys: %#v", got)
	}
}

func TestNormalizeRunVarsPreservesArgsOverJSONInput(t *testing.T) {
	got := NormalizeRunVars(map[string]interface{}{
		"args":  map[string]interface{}{"city": "上海"},
		"input": `{"city":"成都","days":3}`,
	})
	if got["city"] != "上海" || got["days"] != "3" {
		t.Fatalf("NormalizeRunVars() = %#v, want args to win and input JSON to backfill", got)
	}
}

func TestNormalizeRunVarsNilSafe(t *testing.T) {
	if got := NormalizeRunVars(nil); got != nil {
		t.Fatalf("NormalizeRunVars(nil) = %#v, want nil", got)
	}
}

func TestApplyRunInputInferenceFillsRequiredArgFromNamedPrompt(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"user_prompt": "请查询 city: 上海 的天气"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "请查询 city: 上海 的天气"})

	if vars["city"] != "上海" {
		t.Fatalf("city = %q, want 上海", vars["city"])
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

func TestApplyRunInputInferenceDoesNotWriteEmptyInferences(t *testing.T) {
	vars := NormalizeRunVars(map[string]interface{}{"input": `{"city":"成都"}`})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"unknown"}}

	ApplyRunInputInference(entry, vars, map[string]interface{}{"input": `{"city":"成都"}`})

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

func TestBuildRunCheckContextMarksStepAndRunEnvProvided(t *testing.T) {
	entry := &corelib.NLSkillEntry{
		SkillDir: `C:\skills\demo`,
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"extra_env": map[string]interface{}{"STEP_TOKEN": "secret"}},
		}},
	}

	ctx := BuildRunCheckContext(entry, map[string]string{"RUN_TOKEN": "secret"})

	if ctx.SkillDir != entry.SkillDir || !ctx.ProvidedEnvVars["STEP_TOKEN"] || !ctx.ProvidedEnvVars["RUN_TOKEN"] || !ctx.ProvidedEnvVars["OPENAI_API_KEY"] {
		t.Fatalf("BuildRunCheckContext() = %#v", ctx)
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

	if len(dst.Steps) != 1 || len(dst.Params) != 1 || dst.RequiredArgs[0] != "city" || dst.RequiredEnv[0] != "API_KEY" || dst.Platforms[0] != "windows" || dst.RequiresTools[0] != "browser" || dst.FallbackForTools[0] != "web_fetch" || dst.Pipeline[0].Skill != "next" || dst.References[0].Filename != "guide.md" || dst.Mode != "api_workflow" || dst.PreferredShell != "powershell" || dst.ProducesArtifact || dst.RequiresGUI || !dst.Stateful {
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

func TestMergeExtraEnvParamPreservesStepValues(t *testing.T) {
	params := map[string]interface{}{"extra_env": map[string]interface{}{"SHARED": "step"}}

	MergeExtraEnvParam(params, map[string]string{"SHARED": "run", "RUN_ONLY": "1"})

	got := params["extra_env"].(map[string]interface{})
	if got["SHARED"] != "step" || got["RUN_ONLY"] != "1" {
		t.Fatalf("extra_env = %#v", got)
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
