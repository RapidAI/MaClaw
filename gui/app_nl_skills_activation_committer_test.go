package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func writeStagedActivationFixture(t *testing.T) (*App, string, string) {
	t.Helper()
	app := setupNLSkillTransactionApp(t)
	app.cachedSkillScanner = nil
	dir := filepath.Join(app.GetDataDir(), "skills", "activation-candidate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	yamlPath := filepath.Join(dir, "skill.yaml")
	data := "name: activation-candidate\nsource: auto_discovered\nstatus: staged\nsteps:\n  - action: bash\n    params:\n      command: echo {{input}}\n"
	if err := os.WriteFile(yamlPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "activation-candidate", Source: "auto_discovered", Status: "staged", SkillDir: dir,
		RequiredArgs: []string{"input"},
	}}}); err != nil {
		t.Fatal(err)
	}
	return app, dir, yamlPath
}

func TestVerifyAndActivateNLSkillUsesSharedCommitter(t *testing.T) {
	app, _, yamlPath := writeStagedActivationFixture(t)

	if err := app.VerifyAndActivateNLSkillWithArgs("activation-candidate", map[string]interface{}{"input": "verified"}); err != nil {
		t.Fatalf("VerifyAndActivateNLSkillWithArgs() error = %v", err)
	}
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"status: active", "verification_run_id:", "verification_digest:", "verification_gate_status: passed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("activated YAML missing %q: %s", want, text)
		}
	}
	loaded := app.skillExecutor.loadSkills()
	if len(loaded) != 1 || loaded[0].Status != "active" || loaded[0].VerificationGateStatus != "passed" {
		t.Fatalf("activated config = %#v", loaded)
	}
}

func TestVerifyAndActivateNLSkillRollsBackSharedCommitterOnIndexFailure(t *testing.T) {
	app, _, yamlPath := writeStagedActivationFixture(t)
	refreshCalls := 0
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}

	err := app.VerifyAndActivateNLSkillWithArgs("activation-candidate", map[string]interface{}{"input": "verified"})
	if err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("activation error = %v, want shared commit failure", err)
	}
	if refreshCalls < 2 {
		t.Fatalf("refresh calls = %d, want commit and rollback refresh", refreshCalls)
	}
	data, readErr := os.ReadFile(yamlPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "status: staged") || strings.Contains(string(data), "verification_gate_status: passed") {
		t.Fatalf("YAML was not restored to staged state: %s", data)
	}
	loaded := app.skillExecutor.loadSkills()
	if len(loaded) != 1 || loaded[0].Status != "staged" {
		t.Fatalf("config was not restored to staged state: %#v", loaded)
	}
}
