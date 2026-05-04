package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// TestManageSkillHandler_AllCanonicalActionsHandled verifies that the TUI
// dispatcher has a handler for every action in the canonical ManageSkillActions
// list. If a new action is added to the single source of truth but not to the
// TUI switch, this test fails.
func TestManageSkillHandler_AllCanonicalActionsHandled(t *testing.T) {
	app := &TUIApp{
		appConfig: corelib.AppConfig{},
	}
	handler := newManageSkillHandler(app)

	for _, action := range skill.ManageSkillActionNames() {
		got := handler(map[string]interface{}{"action": action})
		if strings.Contains(got, "未知 manage_skill action") {
			t.Errorf("TUI dispatcher has no handler for canonical action %q", action)
		}
	}
}

func TestFindSkillDefFileYAMLOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	path, format := findSkillDefFile(dir)
	if path != "" || format != "" {
		t.Fatalf("findSkillDefFile should ignore skill.json, got (%q, %q)", path, format)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yml"), []byte("name: yml-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, format = findSkillDefFile(dir)
	if filepath.Base(path) != "skill.yml" || format != "yaml" {
		t.Fatalf("findSkillDefFile(skill.yml) = (%q, %q), want skill.yml/yaml", path, format)
	}
}

func TestValidateSkillContentUsesSkillParser(t *testing.T) {
	if got := validateSkillContent([]byte("name: valid-skill\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n"), "yaml"); got != "" {
		t.Fatalf("valid skill yaml rejected: %s", got)
	}
	if got := validateSkillContent([]byte("name: [unterminated\n"), "yaml"); got == "" {
		t.Fatal("invalid YAML should be rejected")
	}
	if got := validateSkillContent([]byte(`{"name":"json"}`), "json"); got == "" {
		t.Fatal("json skill definitions should be rejected")
	}
}

func TestNormalizeTUIRunSkillVarsParsesJSONInput(t *testing.T) {
	got := normalizeTUIRunSkillVars(map[string]interface{}{"input": `{"city":"成都"}`})
	if got["city"] != "成都" || got["input"] == "" {
		t.Fatalf("normalizeTUIRunSkillVars() = %#v", got)
	}
}

func TestApplyTUIRunInputInferenceFillsRequiredCity(t *testing.T) {
	vars := normalizeTUIRunSkillVars(map[string]interface{}{"user_prompt": "查询 city: 上海 的天气"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}
	applyTUIRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "查询 city: 上海 的天气"})
	if vars["city"] != "上海" {
		t.Fatalf("city = %q, want 上海", vars["city"])
	}
}

func TestCollectTUISkillProvidedEnvReadsStepEnv(t *testing.T) {
	entry := &corelib.NLSkillEntry{Steps: []corelib.NLSkillStep{{
		Action: "bash",
		Params: map[string]interface{}{"extra_env": map[string]interface{}{"API_TOKEN": "secret"}},
	}}}
	got := collectTUISkillProvidedEnv(entry)
	if got["API_TOKEN"] != "secret" {
		t.Fatalf("provided env = %#v", got)
	}
}

func TestMergeTUIExtraEnvParamPreservesStepValues(t *testing.T) {
	params := map[string]interface{}{"extra_env": map[string]interface{}{"SHARED": "step"}}
	mergeTUIExtraEnvParam(params, map[string]string{"SHARED": "run", "RUN_ONLY": "1"})
	got := params["extra_env"].(map[string]interface{})
	if got["SHARED"] != "step" || got["RUN_ONLY"] != "1" {
		t.Fatalf("extra_env = %#v", got)
	}
}

func TestManageSkillRunHydratesMarkdownMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := filepath.Join(t.TempDir(), "weather")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: weather\nrequired_args: [city]\nproduces_artifact: false\n---\n\n# Weather\n\n```bash\necho weather {{city}}\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "weather", SkillDir: skillDir, Status: "active"}}}}

	got := skillRun(app, map[string]interface{}{"name": "weather", "input": "成都"})

	if !strings.Contains(got, "weather") || !strings.Contains(got, "成都") {
		t.Fatalf("skillRun() = %q", got)
	}
	if app.appConfig.NLSkills[0].ProducesArtifact {
		t.Fatalf("ProducesArtifact = true, want hydrated markdown false")
	}
}
