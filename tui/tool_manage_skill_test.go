package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
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
		if strings.Contains(got, "鏈煡 manage_skill action") {
			t.Errorf("TUI dispatcher has no handler for canonical action %q", action)
		}
	}
}

func TestManageSkillHandler_UploadAliases(t *testing.T) {
	app := &TUIApp{appConfig: corelib.AppConfig{}}
	handler := newManageSkillHandler(app)

	for _, action := range []string{"publish", "pub", "submit", "发布", "上架"} {
		got := handler(map[string]interface{}{"action": action})
		if strings.Contains(got, "未知 manage_skill action") || strings.Contains(got, "鏈煡 manage_skill action") {
			t.Fatalf("alias %q should route to upload, got %q", action, got)
		}
		if !strings.Contains(got, "name") {
			t.Fatalf("alias %q should reach upload handler and ask for name, got %q", action, got)
		}
	}
}

func TestManageSkillHandlerMaintenancePlanReturnsReadOnlyPlan(t *testing.T) {
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:         "fragile-skill",
		UsageCount:   3,
		FailureCount: 3,
		SuccessCount: 0,
	}}}}
	handler := newManageSkillHandler(app)
	raw := handler(map[string]interface{}{"action": "maintenance_plan", "min_failure_runs": 3})

	var payload struct {
		OK                    bool `json:"ok"`
		NonExecuting          bool `json:"non_executing"`
		Boundary              string
		MaintenancePlanStatus string `json:"maintenance_plan_status"`
		Plan                  struct {
			Actions []skill.SkillMaintenanceAction `json:"actions"`
		} `json:"plan"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal maintenance_plan result: %v\n%s", err, raw)
	}
	if !payload.OK || !payload.NonExecuting || payload.MaintenancePlanStatus != "local_skill_maintenance_plan_no_llm" || !strings.Contains(payload.Boundary, "read-only skill maintenance plan") {
		t.Fatalf("expected read-only maintenance plan payload: %#v", payload)
	}
	found := false
	for _, action := range payload.Plan.Actions {
		if action.Action == skill.MaintenanceActionMarkNeedsReview && action.Skill == "fragile-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fragile skill review action: %#v", payload.Plan.Actions)
	}
}

func TestManageSkillHandlerExecuteMaintenancePlanDryRun(t *testing.T) {
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:         "fragile-skill",
		Status:       "active",
		UsageCount:   3,
		FailureCount: 3,
		SuccessCount: 0,
	}}}}
	handler := newManageSkillHandler(app)
	raw := handler(map[string]interface{}{
		"action":           "execute_maintenance_plan",
		"min_failure_runs": 3,
		"approved_actions": []interface{}{skill.MaintenanceActionMarkNeedsReview},
	})

	var payload struct {
		OK     bool `json:"ok"`
		DryRun bool `json:"dry_run"`
		Result struct {
			ExecutedCount int `json:"executed_count"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal execute result: %v\n%s", err, raw)
	}
	if !payload.OK || !payload.DryRun || payload.Result.ExecutedCount != 1 {
		t.Fatalf("unexpected execute payload: %#v", payload)
	}
	if app.appConfig.NLSkills[0].Status != "active" {
		t.Fatalf("dry run mutated status to %q", app.appConfig.NLSkills[0].Status)
	}
}

func TestManageSkillHandlerExecuteMaintenancePlanRealRunRequiresApprovedActions(t *testing.T) {
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:         "fragile-skill",
		Status:       "active",
		UsageCount:   3,
		FailureCount: 3,
		SuccessCount: 0,
	}}}}
	handler := newManageSkillHandler(app)
	raw := handler(map[string]interface{}{
		"action":  "execute_maintenance_plan",
		"dry_run": false,
		"confirm": true,
	})
	if !strings.Contains(raw, "approved_actions is required") {
		t.Fatalf("expected approved action guard, got %s", raw)
	}
	if app.appConfig.NLSkills[0].Status != "active" {
		t.Fatalf("guard mutated status to %q", app.appConfig.NLSkills[0].Status)
	}
}

func TestTUIStringListArgTrimsApprovedActions(t *testing.T) {
	got := tuiStringListArg(map[string]interface{}{"approved_actions": []interface{}{" mark_needs_review ", "", "\tarchive_stale\t"}}, "approved_actions")
	if len(got) != 2 || got[0] != skill.MaintenanceActionMarkNeedsReview || got[1] != skill.MaintenanceActionArchiveStale {
		t.Fatalf("approved actions = %#v, want trimmed non-empty actions", got)
	}
}

func TestTUISkillHealthLabelsFlagPartialContract(t *testing.T) {
	labels := tuiSkillHealthLabels(corelib.NLSkillEntry{
		Name:   "partial-contract",
		Params: []corelib.NLSkillParam{{Name: "input"}},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
			"command": "convert {{input}} {{output}}",
		}}},
	})
	found := false
	for _, label := range labels {
		if label == "[missing_contract]" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("labels = %#v, want missing contract", labels)
	}
}

func TestTUISkillHealthLabelsFlagLegacyRequiredArgsContract(t *testing.T) {
	labels := tuiSkillHealthLabels(corelib.NLSkillEntry{
		Name:         "legacy-required-contract",
		RequiredArgs: []string{"input"},
		Steps: []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{
			"command": "cat {{input}}",
		}}},
	})
	found := false
	for _, label := range labels {
		if label == "[missing_contract]" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("labels = %#v, want missing contract", labels)
	}
}

func TestTUIPersistableMaintenanceSkillsKeepsFileSkillsOverlayOnly(t *testing.T) {
	filtered := tuiPersistableMaintenanceSkills([]corelib.NLSkillEntry{{
		Name:         "file-skill",
		Source:       "file",
		SkillDir:     t.TempDir(),
		Status:       "needs_review",
		Description:  "loaded from yaml",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo hi"}}},
		UsageCount:   3,
		FailureCount: 3,
		LastError:    "review",
	}})
	if len(filtered) != 1 {
		t.Fatalf("filtered skills = %#v, want one overlay", filtered)
	}
	if filtered[0].Status != "needs_review" || filtered[0].LastError != "review" || filtered[0].UsageCount != 3 {
		t.Fatalf("overlay metadata = %#v", filtered[0])
	}
	if filtered[0].Description != "" || len(filtered[0].Steps) != 0 {
		t.Fatalf("file definition leaked into config overlay: %#v", filtered[0])
	}
}

func TestPersistStatsCreatesOverlayForScannedFileSkill(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	entry := &corelib.NLSkillEntry{
		Name:         "file-only",
		Source:       "file",
		SkillDir:     t.TempDir(),
		Description:  "loaded from yaml",
		Steps:        []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo ok"}}},
		UsageCount:   1,
		SuccessCount: 1,
		LastUsedAt:   "2026-05-22T12:00:00Z",
	}

	persistStats("file-only", entry)

	cfg, err := commands.NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 {
		t.Fatalf("NLSkills = %#v, want one file overlay", cfg.NLSkills)
	}
	got := cfg.NLSkills[0]
	if got.Name != "file-only" || got.Source != "file" || got.UsageCount != 1 || got.SuccessCount != 1 {
		t.Fatalf("overlay metadata = %#v", got)
	}
	if got.Description != "" || len(got.Steps) != 0 {
		t.Fatalf("file definition leaked into config overlay: %#v", got)
	}
}

func TestTUICollectMaintenanceSkillsHydratesFileOverlayFromExternalDir(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "file-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Join([]string{
		"name: file-skill",
		"description: loaded from yaml",
		"steps:",
		"  - action: bash",
		"    params:",
		"      command: echo {{input}}",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &TUIApp{appConfig: corelib.AppConfig{
		ExternalSkillDirs: []string{root},
		NLSkills: []corelib.NLSkillEntry{{
			Name:         "file-skill",
			Source:       "file",
			SkillDir:     skillDir,
			Status:       "needs_review",
			UsageCount:   3,
			FailureCount: 3,
			LastError:    "review",
		}},
	}}
	skills := tuiCollectMaintenanceSkills(app)
	var got *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].Name == "file-skill" {
			got = &skills[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("skills = %#v, want merged file skill", skills)
	}
	if got.Description != "loaded from yaml" || len(got.Steps) != 1 {
		t.Fatalf("file definition was not hydrated: %#v", got)
	}
	if got.Status != "needs_review" || got.LastError != "review" || got.UsageCount != 3 {
		t.Fatalf("runtime overlay was not applied: %#v", got)
	}
}

func TestTUICollectMaintenanceSkillsMatchesFileOverlayBySkillDir(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "renamed-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := strings.Join([]string{
		"name: renamed-from-yaml",
		"description: loaded from yaml",
		"steps:",
		"  - action: bash",
		"    params:",
		"      command: echo ok",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &TUIApp{appConfig: corelib.AppConfig{
		ExternalSkillDirs: []string{root},
		NLSkills: []corelib.NLSkillEntry{{
			Name:       "old-overlay-name",
			Source:     "file",
			SkillDir:   skillDir,
			Status:     "needs_review",
			LastError:  "review",
			UsageCount: 1,
		}},
	}}
	skills := tuiCollectMaintenanceSkills(app)
	var got *corelib.NLSkillEntry
	for i := range skills {
		if skills[i].SkillDir == skillDir {
			got = &skills[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("skills = %#v, want file skill matched by dir", skills)
	}
	if got.Name != "renamed-from-yaml" || got.Status != "needs_review" || got.LastError != "review" {
		t.Fatalf("file overlay not matched by dir: %#v", got)
	}
}

func TestTUIMaintenanceSkillKeyNormalizesWindowsDirCase(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path identity is case-insensitive")
	}
	left := tuiMaintenanceSkillKey(corelib.NLSkillEntry{SkillDir: `C:\Users\Me\Skills\Demo`})
	right := tuiMaintenanceSkillKey(corelib.NLSkillEntry{SkillDir: `c:\users\me\skills\demo`})
	if left != right {
		t.Fatalf("keys differ: %q != %q", left, right)
	}
}

func TestTUIMaintenanceSkillKeyNormalizesNameCase(t *testing.T) {
	left := tuiMaintenanceSkillKey(corelib.NLSkillEntry{Name: "File-Skill"})
	right := tuiMaintenanceSkillKey(corelib.NLSkillEntry{Name: " file-skill "})
	if left != right {
		t.Fatalf("keys differ: %q != %q", left, right)
	}
}

func TestFormatTUISkillRunLastErrorAddsClassAndAction(t *testing.T) {
	got := formatTUISkillRunLastError("[Step 1/1] failed: executable file not found")
	if !strings.Contains(got, "[class:") || !strings.Contains(got, "[action:") {
		t.Fatalf("formatTUISkillRunLastError() = %q, want classified action hint", got)
	}
}

func TestFormatTUISkillRunLastErrorClassifiesPrecheckCommandFailure(t *testing.T) {
	got := formatTUISkillRunLastError(`skill "xparse" runner requirements not satisfied: required command xparse-cli was not found on PATH [action: install_dependency]`)
	if !strings.Contains(got, "[class: command_not_found]") || !strings.Contains(got, "xparse-cli") {
		t.Fatalf("formatTUISkillRunLastError() = %q, want command_not_found for precheck failure", got)
	}
}

func TestFormatTUISkillRunLastErrorClassifiesPrecheckPackageFailure(t *testing.T) {
	got := formatTUISkillRunLastError(`skill "md2pdf" runner requirements not satisfied: required Python package weasyprint is not installed [action: install_dependency]`)
	if !strings.Contains(got, "[class: missing_dependency]") || !strings.Contains(got, "install_dependency") {
		t.Fatalf("formatTUISkillRunLastError() = %q, want missing_dependency for package precheck failure", got)
	}
}

func TestFormatTUISkillRunLastErrorTruncatesButKeepsActionHint(t *testing.T) {
	longTrace := strings.Repeat("very long traceback detail ", 80)
	got := formatTUISkillRunLastError("Error: API_KEY environment variable not set " + longTrace)
	if len(got) > 500 {
		t.Fatalf("formatTUISkillRunLastError() length = %d, want <= 500", len(got))
	}
	if !strings.Contains(got, "[class: missing_env_var]") || !strings.Contains(got, "[action: inform_user]") {
		t.Fatalf("formatTUISkillRunLastError() = %q, want class and action after truncation", got)
	}
	if !strings.Contains(got, "\n...\n") {
		t.Fatalf("formatTUISkillRunLastError() = %q, want truncation marker before action", got)
	}
}

func TestTruncateTUISkillRunLastErrorKeepsValidUTF8(t *testing.T) {
	formatted := "[class: unknown] " + strings.Repeat("缺少参数", 120) + "\n[action: inspect] Inspect the step output."
	got := skill.TruncateFormattedErrorForStorage(formatted, 500)
	if !utf8.ValidString(got) {
		t.Fatalf("TruncateFormattedErrorForStorage() returned invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "[action: inspect]") {
		t.Fatalf("TruncateFormattedErrorForStorage() = %q, want action preserved", got)
	}
}

func TestFormatTUISkillRunLastErrorIsIdempotentForFormattedError(t *testing.T) {
	formatted := "[class: missing_dependency] The skill is missing package dependency \"Pillow\".\n[action: install_dependency] Install Python package Pillow with pip, then retry the skill."
	got := formatTUISkillRunLastError(formatted)
	if got != formatted {
		t.Fatalf("formatTUISkillRunLastError() = %q, want already formatted error unchanged", got)
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
	got := normalizeTUIRunSkillVars(map[string]interface{}{"input": `{"city":"鎴愰兘"}`})
	if got["city"] != "鎴愰兘" || got["input"] == "" {
		t.Fatalf("normalizeTUIRunSkillVars() = %#v", got)
	}
}

func TestNormalizeTUIRunSkillVarsCanonicalizesKeyShape(t *testing.T) {
	got := normalizeTUIRunSkillVars(map[string]interface{}{
		"User Prompt": "weather in Chengdu",
		"Args":        map[string]interface{}{"Input-File": "report.md"},
	})
	if got["user_prompt"] != "weather in Chengdu" || got["input_file"] != "report.md" {
		t.Fatalf("normalizeTUIRunSkillVars() = %#v, want canonical key shapes", got)
	}
}

func TestApplyTUIRunInputInferenceFillsRequiredCity(t *testing.T) {
	vars := normalizeTUIRunSkillVars(map[string]interface{}{"user_prompt": "weather city: Shanghai"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"city"}}
	applyTUIRunInputInference(entry, vars, map[string]interface{}{"user_prompt": "weather city: Shanghai"})
	if vars["city"] != "Shanghai" {
		t.Fatalf("city = %q, want Shanghai", vars["city"])
	}
}

func TestApplyTUIRunInputInferencePromotesFileAliasToInput(t *testing.T) {
	vars := normalizeTUIRunSkillVars(map[string]interface{}{"file": "report.md"})
	entry := &corelib.NLSkillEntry{
		RequiredArgs: []string{"input"},
		Params:       []corelib.NLSkillParam{{Name: "input", Aliases: []string{"file"}, Required: true}},
	}
	applyTUIRunInputInference(entry, vars, map[string]interface{}{"file": "report.md"})
	if vars["input"] != "report.md" {
		t.Fatalf("input = %q, want file alias promoted", vars["input"])
	}
}

func TestApplyTUIRunInputInferencePromotesContentAliasToText(t *testing.T) {
	vars := normalizeTUIRunSkillVars(map[string]interface{}{"content": "hello"})
	entry := &corelib.NLSkillEntry{RequiredArgs: []string{"text"}}
	applyTUIRunInputInference(entry, vars, map[string]interface{}{"content": "hello"})
	if vars["text"] != "hello" {
		t.Fatalf("text = %q, want content alias promoted", vars["text"])
	}
}

func TestQuoteTUIRunValueForShellQuotesSpaces(t *testing.T) {
	got := quoteTUIRunValueForShell("hello world")
	if runtime.GOOS == "windows" {
		if got != "'hello world'" {
			t.Fatalf("quoteTUIRunValueForShell() = %q, want Windows PowerShell quotes", got)
		}
		return
	}
	if got != "'hello world'" {
		t.Fatalf("quoteTUIRunValueForShell() = %q, want POSIX single quotes", got)
	}
}

func TestQuoteTUIRunValueForPreferredShell(t *testing.T) {
	if got := quoteTUIRunValueForPreferredShell("a'b", "powershell"); got != "'a''b'" {
		t.Fatalf("powershell quote = %q, want doubled single quote", got)
	}
	if got := quoteTUIRunValueForPreferredShell("a b", "cmd"); got != `"a b"` {
		t.Fatalf("cmd quote = %q, want double quotes", got)
	}
	if got := quoteTUIRunValueForPreferredShell("a'b", "bash"); got != `'a'"'"'b'` {
		t.Fatalf("bash quote = %q, want POSIX quote", got)
	}
}

func TestTUIBaseCommandEnvForcesPythonUTF8(t *testing.T) {
	env := tuiBaseCommandEnvFrom([]string{"PATH=/bin"})

	if !envListContains(env, "PYTHONIOENCODING=utf-8") || !envListContains(env, "PYTHONUTF8=1") {
		t.Fatalf("tuiBaseCommandEnvFrom() = %#v, want Python UTF-8 env", env)
	}
}

func TestWithTUISkillPreferredShellCopiesParams(t *testing.T) {
	params := map[string]interface{}{"command": "echo ok"}
	step := withTUISkillPreferredShell(corelib.NLSkillStep{Action: "bash", Params: params}, "cmd")
	if step.Params["preferred_shell"] != "cmd" {
		t.Fatalf("preferred_shell = %#v, want cmd", step.Params["preferred_shell"])
	}
	if _, mutated := params["preferred_shell"]; mutated {
		t.Fatalf("withTUISkillPreferredShell mutated original params: %#v", params)
	}
}

func envListContains(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}

func TestManageSkillRunAcceptsCanonicalRequiredArgBeforePrecheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:         "cat-canonical-file",
		Status:       "active",
		RequiredArgs: []string{"Input-File"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{Input-File}}"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "cat-canonical-file", "input_file": "report.md"})

	if strings.Contains(got, "缂哄皯蹇呴渶鍙傛暟") || !strings.Contains(got, "report.md") {
		t.Fatalf("skillRun() = %q", got)
	}
}
func TestManageSkillRunAcceptsInputAliasBeforePrecheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:         "cat-file",
		Status:       "active",
		RequiredArgs: []string{"input"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo {{input}}"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "cat-file", "file": "report.md"})

	if strings.Contains(got, "缂哄皯蹇呴渶鍙傛暟") || !strings.Contains(got, "report.md") {
		t.Fatalf("skillRun() = %q", got)
	}
}

func TestManageSkillRunBlocksMissingInferredCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCommand := "definitely-missing-skill-runner-command-tui"
	entry := corelib.NLSkillEntry{
		Name:   "missing-command-skill",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": missingCommand + " --version"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "missing-command-skill"})

	if !strings.Contains(got, missingCommand) || strings.Contains(got, "[Step 1/1]") {
		t.Fatalf("skillRun() = %q, want precheck failure before step execution", got)
	}
}

func TestManageSkillRunBlocksMissingCredentialFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCredential := filepath.Join(home, "missing-credential.json")
	entry := corelib.NLSkillEntry{
		Name:                    "credential-skill",
		Status:                  "active",
		RequiredCredentialFiles: []string{missingCredential},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "credential-skill"})

	if !strings.Contains(got, "missing credential") || !strings.Contains(got, missingCredential) || strings.Contains(got, "[Step 1/1]") {
		t.Fatalf("skillRun() = %q, want credential precheck failure before execution", got)
	}
}

func TestManageSkillRunDoesNotAssumeGUIOpenAIProxy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENAI_API_KEY", "")
	entry := corelib.NLSkillEntry{
		Name:        "openai-env-skill",
		Status:      "active",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "openai-env-skill"})

	if !strings.Contains(got, "OPENAI_API_KEY") || strings.Contains(got, "[Step 1/1]") {
		t.Fatalf("skillRun() = %q, want TUI env precheck failure before execution", got)
	}
}

func TestManageSkillRunAcceptsRunProvidedOpenAIEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("OPENAI_API_KEY", "")
	entry := corelib.NLSkillEntry{
		Name:        "openai-env-provided",
		Status:      "active",
		RequiredEnv: []string{"OPENAI_API_KEY"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{
		"name": "openai-env-provided",
		"env":  map[string]interface{}{"OPENAI_API_KEY": "sk-test"},
	})

	if !strings.Contains(got, "ok") {
		t.Fatalf("skillRun() = %q, want run-provided OPENAI_API_KEY to satisfy precheck", got)
	}
}

func TestManageSkillRunAcceptsRunProvidedExtraEnvAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	entry := corelib.NLSkillEntry{
		Name:        "extra-env-provided",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{
		"name":      "extra-env-provided",
		"extra_env": map[string]interface{}{"API_TOKEN": "secret"},
	})

	if !strings.Contains(got, "ok") {
		t.Fatalf("skillRun() = %q, want run-provided extra_env to satisfy precheck", got)
	}
}

func TestManageSkillRunAcceptsNestedArgsExtraEnvAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	command := "echo $API_TOKEN"
	params := map[string]interface{}{"command": command}
	if runtime.GOOS == "windows" {
		command = "echo $env:API_TOKEN"
		params["command"] = command
		params["preferred_shell"] = "powershell"
	}
	entry := corelib.NLSkillEntry{
		Name:        "nested-extra-env-provided",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: params,
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{
		"name": "nested-extra-env-provided",
		"args": map[string]interface{}{
			"extra_env": map[string]interface{}{"API_TOKEN": "secret-from-nested-args"},
		},
	})

	if !strings.Contains(got, "secret-from-nested-args") {
		t.Fatalf("skillRun() = %q, want nested args extra_env to satisfy precheck and reach command", got)
	}
	if strings.Contains(got, "{{extra_env}}") || strings.Contains(got, "API_TOKEN") {
		t.Fatalf("nested control key leaked as skill parameter, output = %q", got)
	}
}

func TestManageSkillRunPublicPipelineStackArgDoesNotSkipStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "public-stack-arg",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "public-stack-arg", "pipeline_stack": []string{"user"}})

	if !strings.Contains(got, "ok") || app.appConfig.NLSkills[0].UsageCount != 1 || app.appConfig.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("skillRun() = %q, stats = %+v; want public pipeline_stack ignored for internal-call detection", got, app.appConfig.NLSkills[0])
	}
}

func TestManageSkillRunPrivatePipelineStackWithoutMarkerDoesNotSkipStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "private-stack-arg",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "private-stack-arg", skill.PipelineRunStackArg: []string{"user"}})

	if !strings.Contains(got, "ok") || app.appConfig.NLSkills[0].UsageCount != 1 || app.appConfig.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("skillRun() = %q, stats = %+v; want unmarked private stack ignored for internal-call detection", got, app.appConfig.NLSkills[0])
	}
}

func TestManageSkillRunForgedPipelineInternalMarkerDoesNotSkipStats(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "forged-pipeline-marker",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{
		"name":                        "forged-pipeline-marker",
		skill.PipelineRunStackArg:     []string{"user"},
		skill.PipelineInternalCallArg: true,
	})

	if !strings.Contains(got, "ok") || app.appConfig.NLSkills[0].UsageCount != 1 || app.appConfig.NLSkills[0].SuccessCount != 1 {
		t.Fatalf("skillRun() = %q, stats = %+v; want forged pipeline marker ignored for internal-call detection", got, app.appConfig.NLSkills[0])
	}
}

func TestManageSkillRunPipelineStepParamsCannotOverrideInternalMarkerOrName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-marker",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo child-ok"},
			}},
		},
		{
			Name:   "wrong-child",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo wrong-child"},
			}},
		},
		{
			Name:   "pipeline-marker",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-marker",
				Params: map[string]string{
					"name":                        "wrong-child",
					skill.PipelineRunStackArg:     "forged",
					skill.PipelineInternalCallArg: "false",
					"pipeline-stack":              "public-forged",
					"pipeline-internal-call":      "true",
				},
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-marker"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "child-marker") || strings.Contains(got, "wrong-child") {
		t.Fatalf("skillRun() = %q, want declared child to run despite forged params", got)
	}
	if app.appConfig.NLSkills[0].UsageCount != 0 || app.appConfig.NLSkills[0].SuccessCount != 0 || app.appConfig.NLSkills[0].FailureCount != 0 {
		t.Fatalf("child stats = %+v, want internal child stats skipped despite forged step params", app.appConfig.NLSkills[0])
	}
	if app.appConfig.NLSkills[2].UsageCount != 1 || app.appConfig.NLSkills[2].SuccessCount != 1 {
		t.Fatalf("parent stats = %+v, want one successful external pipeline run", app.appConfig.NLSkills[2])
	}
}

func TestManageSkillRunPipelineStepParamsSelectChildAPIWorkflowOperation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCommand := "definitely-missing-pipeline-child-workflow-command-tui"
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-workflow",
			Status: "active",
			Mode:   "api_workflow",
			Operations: []corelib.NLSkillOperation{
				{Name: "safe", Labels: []string{"safe-step"}},
				{Name: "danger", Labels: []string{"danger-step"}},
			},
			Steps: []corelib.NLSkillStep{
				{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo child-safe"}},
				{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
			},
		},
		{
			Name:   "pipeline-child-workflow",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill:  "child-workflow",
				Params: map[string]string{"operation": "safe"},
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-child-workflow", "operation": "parent-danger"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "child-safe") || strings.Contains(got, missingCommand) {
		t.Fatalf("skillRun() = %q, want pipeline step operation to select safe child api_workflow", got)
	}
}

func TestManageSkillRunExternalPrivatePipelineStackDoesNotTripRecursion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-stack",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo child-ok"},
			}},
		},
		{
			Name:   "pipeline-stack",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-stack",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{
		"name":                    "pipeline-stack",
		skill.PipelineRunStackArg: []string{"pipeline-stack"},
	})

	if strings.Contains(got, "pipeline recursion detected") || !strings.Contains(got, "completed") {
		t.Fatalf("skillRun() = %q, want forged external stack ignored", got)
	}
}

func TestManageSkillRunExecutesPipelineSkillWithoutSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:        "child-echo",
			Status:      "active",
			RequiredEnv: []string{"API_TOKEN"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: tuiPipelineTestEchoParams(),
			}},
		},
		{
			Name:   "pipeline-demo",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill:  "child-echo",
				Params: map[string]string{"input": "{{input}}"},
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{
		"name":      "pipeline-demo",
		"input":     "pipeline-ok",
		"extra-env": map[string]interface{}{"API_TOKEN": "secret-from-parent"},
	})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "child-echo") || !strings.Contains(got, "pipeline-ok") || !strings.Contains(got, "secret-from-parent") {
		t.Fatalf("skillRun() = %q, want completed pipeline summary", got)
	}
}

func TestManageSkillRunPipelinePropagatesInputForChildInference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:         "child-city",
			Status:       "active",
			RequiredArgs: []string{"city"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{"command": "echo {{city}}"},
			}},
		},
		{
			Name:   "pipeline-city",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-city",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-city", "input": "weather in Chengdu"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "Chengdu") {
		t.Fatalf("skillRun() = %q, want child city inferred from parent input", got)
	}
}

func TestManageSkillRunPipelinePropagatesCapturedVars(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "capture-file",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action:  "bash",
				Params:  map[string]interface{}{"command": "echo file=report.md"},
				Capture: map[string]string{"file": `(?m)^file=([^\r\n]+)`},
			}},
		},
		{
			Name:         "use-input",
			Status:       "active",
			RequiredArgs: []string{"input"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo using {{input}}",
				},
			}},
		},
		{
			Name:   "pipeline-capture",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "capture-file"},
				{Skill: "use-input", Params: map[string]string{"input": "{{capture-file.input}}"}},
			},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-capture"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "report.md") || strings.Contains(got, "{{capture-file.input}}") {
		t.Fatalf("skillRun() = %q, want downstream step to receive captured alias", got)
	}
}

func TestManageSkillRunPipelinePropagatesNestedArgsContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:         "child-nested-context",
			Status:       "active",
			RequiredArgs: []string{"input"},
			RequiredEnv:  []string{"API_TOKEN"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: tuiPipelineTestEchoParams(),
			}},
		},
		{
			Name:   "pipeline-nested-context",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-nested-context",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{
		"name": "pipeline-nested-context",
		"args": map[string]interface{}{
			"input":     "nested-pipeline-ok",
			"extra_env": map[string]interface{}{"API_TOKEN": "secret-from-nested-parent"},
		},
	})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "nested-pipeline-ok") || !strings.Contains(got, "secret-from-nested-parent") {
		t.Fatalf("skillRun() = %q, want nested args context propagated to child", got)
	}
}

func TestManageSkillRunPipelinePropagatesTextAliasContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:         "child-text",
			Status:       "active",
			RequiredArgs: []string{"text"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo text={{text}}",
				},
			}},
		},
		{
			Name:   "pipeline-text",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-text",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-text", "text": "translate me"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "translate me") {
		t.Fatalf("skillRun() = %q, want text alias propagated to child", got)
	}
}

func TestManageSkillRunPipelinePropagatesPlainArgsToTextSubSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:         "child-text",
			Status:       "active",
			RequiredArgs: []string{"text"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo text={{text}}",
				},
			}},
		},
		{
			Name:   "pipeline-plain-args",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-text",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-plain-args", "args": "translate me"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "translate me") {
		t.Fatalf("skillRun() = %q, want plain args propagated to child text", got)
	}
}

func TestManageSkillRunPipelineChecksParentRequiredEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-never",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo should-not-run",
				},
			}},
		},
		{
			Name:        "pipeline-env",
			Status:      "active",
			Mode:        "pipeline",
			RequiredEnv: []string{"API_TOKEN"},
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-never",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-env"})
	if !strings.Contains(got, "API_TOKEN") || strings.Contains(got, "should-not-run") {
		t.Fatalf("skillRun() = %q, want parent required_env precheck before child execution", got)
	}

	got = skillRun(app, map[string]interface{}{"name": "pipeline-env", "env": map[string]interface{}{"API_TOKEN": "secret"}})
	if !strings.Contains(got, "completed") || !strings.Contains(got, "should-not-run") {
		t.Fatalf("skillRun() = %q, want run-provided env to satisfy parent pipeline", got)
	}
}

func TestManageSkillRunPipelineContinueOnFailCompletes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-fail",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo diagnostic && exit 7",
				},
			}},
		},
		{
			Name:   "child-ok",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo recovered",
				},
			}},
		},
		{
			Name:   "pipeline-continue",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-fail", ContinueOnFail: true},
				{Skill: "child-ok"},
			},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-continue"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "diagnostic") || !strings.Contains(got, "recovered") {
		t.Fatalf("skillRun() = %q, want completed pipeline with failed-step output retained", got)
	}
	for _, entry := range app.appConfig.NLSkills {
		switch entry.Name {
		case "pipeline-continue":
			if entry.UsageCount != 1 || entry.SuccessCount != 1 || entry.FailureCount != 0 {
				t.Fatalf("parent stats = %+v, want one visible success", entry)
			}
		case "child-fail", "child-ok":
			if entry.UsageCount != 0 || entry.SuccessCount != 0 || entry.FailureCount != 0 {
				t.Fatalf("child stats = %+v, want internal pipeline calls not counted", entry)
			}
		}
	}
}

func TestManageSkillRunPipelineContinueOnFailPropagatesFailedCapture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-fail-capture",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action:  "bash",
				Params:  tuiPipelineTestFailCapturedParams(),
				Capture: map[string]string{"file": `(?m)^file=([^\r\n]+)`},
			}},
		},
		{
			Name:         "child-use-input",
			Status:       "active",
			RequiredArgs: []string{"input"},
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo using {{input}}",
				},
			}},
		},
		{
			Name:   "pipeline-continue-capture",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-fail-capture", ContinueOnFail: true},
				{Skill: "child-use-input", Params: map[string]string{"input": "{{child-fail-capture.input}}"}},
			},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-continue-capture"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "report.md") || strings.Contains(got, "{{child-fail-capture.input}}") {
		t.Fatalf("skillRun() = %q, want failed-step capture to feed second pipeline step", got)
	}
}

func TestManageSkillRunPipelineDoesNotFailOnOutputText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-report",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo error budget is healthy",
				},
			}},
		},
		{
			Name:   "pipeline-report",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "child-report",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-report"})

	if !strings.Contains(got, "completed") || !strings.Contains(got, "error") || !strings.Contains(got, "budget") || !strings.Contains(got, "healthy") {
		t.Fatalf("skillRun() = %q, want successful pipeline despite ordinary output text", got)
	}
}

func TestManageSkillRunPipelineHonorsGlobalTimeout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "child-slow",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: tuiPipelineTestSlowParams(),
			}},
		},
		{
			Name:   "child-never",
			Status: "active",
			Steps: []corelib.NLSkillStep{{
				Action: "bash",
				Params: map[string]interface{}{
					"command": "echo should-not-run",
				},
			}},
		},
		{
			Name:          "pipeline-timeout",
			Status:        "active",
			Mode:          "pipeline",
			GlobalTimeout: 1,
			Pipeline: []corelib.SkillPipelineStep{
				{Skill: "child-slow"},
				{Skill: "child-never"},
			},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-timeout"})

	if !strings.Contains(got, "cancelled") || strings.Contains(got, "should-not-run") {
		t.Fatalf("skillRun() = %q, want timeout cancellation before second child", got)
	}
}

func TestManageSkillRunPipelineRejectsRecursion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:   "pipeline-self",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self",
		}},
	}}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-self"})

	if !strings.Contains(got, "pipeline recursion detected") || !strings.Contains(got, "pipeline-self -> pipeline-self") {
		t.Fatalf("skillRun() = %q, want recursion chain", got)
	}
}

func TestManageSkillRunPipelineRecursionPrecedesRequirementChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:        "pipeline-self-env",
		Status:      "active",
		Mode:        "pipeline",
		RequiredEnv: []string{"API_TOKEN"},
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "pipeline-self-env",
		}},
	}}}}
	args := skill.WithPipelineRunStack(map[string]interface{}{"name": "pipeline-self-env"}, "pipeline-self-env")

	got := skillRun(app, args)

	if !strings.Contains(got, "pipeline recursion detected") || strings.Contains(got, "API_TOKEN") {
		t.Fatalf("skillRun() = %q, want recursion to short-circuit requirement checks", got)
	}
}

func TestManageSkillRunPipelineRejectsMutualRecursion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{
		{
			Name:   "pipeline-a",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "pipeline-b",
			}},
		},
		{
			Name:   "pipeline-b",
			Status: "active",
			Mode:   "pipeline",
			Pipeline: []corelib.SkillPipelineStep{{
				Skill: "pipeline-a",
			}},
		},
	}}}

	got := skillRun(app, map[string]interface{}{"name": "pipeline-a"})

	if !strings.Contains(got, "pipeline recursion detected") || !strings.Contains(got, "pipeline-a -> pipeline-b -> pipeline-a") {
		t.Fatalf("skillRun() = %q, want mutual recursion chain", got)
	}
}

func TestManageSkillRunPipelineRejectsExcessiveDepth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:   "pipeline-too-deep",
		Status: "active",
		Mode:   "pipeline",
		Pipeline: []corelib.SkillPipelineStep{{
			Skill: "child",
		}},
	}}}}
	stack := make([]string, skill.MaxPipelineRunStackDepth)
	args := map[string]interface{}{}
	for range stack {
		args = skill.WithPipelineRunStack(args, "existing")
	}
	args["name"] = "pipeline-too-deep"

	got := skillRun(app, args)

	if !strings.Contains(got, "pipeline nesting depth exceeded") || !strings.Contains(got, "pipeline-too-deep") {
		t.Fatalf("skillRun() = %q, want depth error", got)
	}
}

func tuiPipelineTestEchoParams() map[string]interface{} {
	if runtime.GOOS == "windows" {
		return map[string]interface{}{
			"command":         "echo $env:API_TOKEN {{input}}",
			"preferred_shell": "powershell",
		}
	}
	return map[string]interface{}{"command": "echo $API_TOKEN {{input}}"}
}

func tuiPipelineTestSlowParams() map[string]interface{} {
	return map[string]interface{}{"command": `python -c "import time; time.sleep(1.2); print('slow-done')"`}
}

func tuiPipelineTestFailCapturedParams() map[string]interface{} {
	if runtime.GOOS == "windows" {
		return map[string]interface{}{
			"command":         "echo file=report.md & exit /b 7",
			"preferred_shell": "cmd",
		}
	}
	return map[string]interface{}{"command": "echo file=report.md; exit 7"}
}

func TestManageSkillRunAcceptsEnvDerivedFromRunParam(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("API_TOKEN", "")
	entry := corelib.NLSkillEntry{
		Name:        "env-from-param",
		Status:      "active",
		RequiredEnv: []string{"API_TOKEN"},
		Params:      []corelib.NLSkillParam{{Name: "api_token", Required: true}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{
				"command":   "echo ok",
				"extra_env": map[string]interface{}{"API_TOKEN": "{{api_token}}"},
			},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "env-from-param", "api_token": "secret"})

	if !strings.Contains(got, "ok") {
		t.Fatalf("skillRun() = %q, want api_token-derived env to satisfy precheck", got)
	}
}

func TestManageSkillRunPrechecksOnlySelectedAPIWorkflowSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCommand := "definitely-missing-skill-runner-command-unselected-tui"
	entry := corelib.NLSkillEntry{
		Name:         "workflow-selected",
		Status:       "active",
		Mode:         "api_workflow",
		RequiredArgs: []string{"danger_input"},
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": missingCommand + " --version {{danger_input}}"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-selected", "operation": "safe"})

	if !strings.Contains(got, "ok") || strings.Contains(got, missingCommand) {
		t.Fatalf("skillRun() = %q, want selected safe step only", got)
	}
}

func TestManageSkillRunReadsOperationFromNestedArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCommand := "definitely-missing-skill-runner-command-nested-op-tui"
	entry := corelib.NLSkillEntry{
		Name:   "workflow-nested-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{
		"name": "workflow-nested-op",
		"args": map[string]interface{}{"operation": "safe"},
	})

	if !strings.Contains(got, "ok") || strings.Contains(got, missingCommand) {
		t.Fatalf("skillRun() = %q, want nested operation to select safe step only", got)
	}
}

func TestManageSkillRunDefaultsSingleAPIWorkflowOperation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	missingCommand := "definitely-missing-skill-runner-command-default-op-tui"
	entry := corelib.NLSkillEntry{
		Name:   "workflow-default-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": missingCommand + " --version"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-default-op"})

	if !strings.Contains(got, "ok") || strings.Contains(got, missingCommand) {
		t.Fatalf("skillRun() = %q, want single operation to default to safe step", got)
	}
}

func TestManageSkillRunRequiresOperationForMultipleAPIWorkflowOperations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "workflow-choose-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{
			{Name: "safe", Labels: []string{"safe-step"}},
			{Name: "danger", Labels: []string{"danger-step"}},
		},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "danger-step", Params: map[string]interface{}{"command": "echo danger"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-choose-op"})

	if !strings.Contains(got, "requires an operation") || !strings.Contains(got, "safe") || !strings.Contains(got, "[action: choose_operation]") {
		t.Fatalf("skillRun() = %q, want choose operation error", got)
	}
}

func TestManageSkillRunPrechecksOnlySelectedFileReferences(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := t.TempDir()
	entry := corelib.NLSkillEntry{
		Name:     "workflow-selected-files",
		Status:   "active",
		Mode:     "api_workflow",
		SkillDir: skillDir,
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": "python missing.py"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-selected-files", "operation": "safe"})

	if !strings.Contains(got, "ok") || strings.Contains(got, "missing.py") {
		t.Fatalf("skillRun() = %q, want selected safe step to ignore unselected missing file", got)
	}
}

func TestManageSkillRunPrechecksOnlyWhenActiveSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := t.TempDir()
	entry := corelib.NLSkillEntry{
		Name:         "conditional-precheck",
		Status:       "active",
		SkillDir:     skillDir,
		RequiredArgs: []string{"advanced_input"},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			When:   "{{mode}} == advanced",
			Params: map[string]interface{}{"command": "python missing.py {{advanced_input}}"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "conditional-precheck", "mode": "basic"})

	if strings.Contains(got, "missing.py") || strings.Contains(got, "advanced_input") || !strings.Contains(got, "skipped") {
		t.Fatalf("skillRun() = %q, want when=false step skipped without precheck failure", got)
	}
}

func TestManageSkillRunForwardsQueryToWhenCondition(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "query-when",
		Status: "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			When:   "{{query}} contains Chengdu",
			Params: map[string]interface{}{"command": "echo query-city"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "query-when", "query": "weather in Chengdu"})

	if !strings.Contains(got, "query-city") {
		t.Fatalf("skillRun() = %q, want query forwarded into when-conditioned step", got)
	}
}

func TestManageSkillRunPrechecksResolvedWorkingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := t.TempDir()
	missingDir := filepath.Join(skillDir, "missing-project")
	entry := corelib.NLSkillEntry{
		Name:     "dynamic-working-dir",
		Status:   "active",
		SkillDir: skillDir,
		Params:   []corelib.NLSkillParam{{Name: "project_dir", Required: true}},
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"working_dir": "{{project_dir}}", "command": "echo ok"},
		}},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "dynamic-working-dir", "project_dir": missingDir})

	if !strings.Contains(got, missingDir) || !strings.Contains(got, "working_dir") || strings.Contains(got, "[Step 1/1]") {
		t.Fatalf("skillRun() = %q, want resolved missing working_dir precheck before execution", got)
	}
}

func TestManageSkillRunBlocksSelectedMissingFileReference(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := t.TempDir()
	entry := corelib.NLSkillEntry{
		Name:     "workflow-selected-missing-file",
		Status:   "active",
		Mode:     "api_workflow",
		SkillDir: skillDir,
		Operations: []corelib.NLSkillOperation{{
			Name:   "bad",
			Labels: []string{"bad-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "bad-step", Params: map[string]interface{}{"command": "python missing.py"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-selected-missing-file", "operation": "bad"})

	if !strings.Contains(got, "missing.py") || strings.Contains(got, "[Step 1/1]") {
		t.Fatalf("skillRun() = %q, want missing file precheck before execution", got)
	}
}

func TestManageSkillRunRejectsUnknownAPIWorkflowOperation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	entry := corelib.NLSkillEntry{
		Name:   "workflow-unknown-op",
		Status: "active",
		Mode:   "api_workflow",
		Operations: []corelib.NLSkillOperation{{
			Name:   "safe",
			Labels: []string{"safe-step"},
		}},
		Steps: []corelib.NLSkillStep{
			{Action: "bash", Label: "safe-step", Params: map[string]interface{}{"command": "echo ok"}},
		},
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{entry}}}

	got := skillRun(app, map[string]interface{}{"name": "workflow-unknown-op", "operation": "missing"})

	if !strings.Contains(got, `operation "missing" not found`) || !strings.Contains(got, "safe") {
		t.Fatalf("skillRun() = %q, want unknown operation error with available operations", got)
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

func TestMergeTUIExtraEnvParamLetsRunEnvOverrideStepDefaults(t *testing.T) {
	params := map[string]interface{}{"extra_env": map[string]interface{}{"SHARED": "step"}}
	mergeTUIExtraEnvParam(params, map[string]string{"SHARED": "run", "RUN_ONLY": "1"})
	got := params["extra_env"].(map[string]interface{})
	if got["SHARED"] != "run" || got["RUN_ONLY"] != "1" {
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

	got := skillRun(app, map[string]interface{}{"name": "weather", "input": "鎴愰兘"})

	if !strings.Contains(got, "weather") || !strings.Contains(got, "鎴愰兘") {
		t.Fatalf("skillRun() = %q", got)
	}
	if app.appConfig.NLSkills[0].ProducesArtifact {
		t.Fatalf("ProducesArtifact = true, want hydrated markdown false")
	}
}

func TestManageSkillRunReportsCraftToolCapabilityGap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := filepath.Join(t.TempDir(), "doc-only")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: doc-only
description: Documentation-only skill.
produces_artifact: false
---

# Doc Only

Use these instructions to produce the requested answer. There is no executable bash block.
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "doc-only", SkillDir: skillDir, Status: "active"}}}}

	got := skillRun(app, map[string]interface{}{"name": "doc-only", "input": "summarize"})

	if !strings.Contains(got, "craft_tool requires GUI skill runner") || !strings.Contains(got, "open_gui") {
		t.Fatalf("skillRun() = %q", got)
	}
}

func TestManageSkillRunReportsKnowledgeSkillNotExecutable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	skillDir := filepath.Join(t.TempDir(), "knowledge")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# Knowledge\n\n```bash\necho example-only\n```\n"
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &TUIApp{appConfig: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name:     "knowledge",
		Type:     "documentation",
		SkillDir: skillDir,
		Status:   "active",
		Steps: []corelib.NLSkillStep{{
			Action: "bash",
			Params: map[string]interface{}{"command": "echo stale-cache"},
		}},
	}}}}

	got := skillRun(app, map[string]interface{}{"name": "knowledge", "input": "use docs"})

	if !strings.Contains(got, "knowledge skill") || !strings.Contains(got, "not directly executable") {
		t.Fatalf("skillRun() = %q", got)
	}
}
