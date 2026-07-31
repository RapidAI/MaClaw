package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corelib "github.com/RapidAI/CodeClaw/corelib"
)

// TestSubmitMaclawAppPackageBlocksUnbundlableRequiredDependency locks in the
// local-submit publish gate: a required dependency that is installed and ready
// (so it passes the readiness review) but cannot be embedded and has no
// Hub/SkillMarket coordinate must fail at submit time instead of reaching the
// receiver as a dangling external skill reference.
func TestSubmitMaclawAppPackageBlocksUnbundlableRequiredDependency(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "bloated-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# bloated-skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	// Exceed maxMaclawAppBundledSkillFiles so bundling fails deterministically.
	for i := 0; i < maxMaclawAppBundledSkillFiles+5; i++ {
		name := filepath.Join(skillDir, fmt.Sprintf("asset-%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile asset: %v", err)
		}
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "bloated-skill", SkillDir: skillDir, Status: "active", HubVersion: "1.0.0"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "bloated-dep-app",
				"name": "Bloated Dep App",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id": "bloated-skill", "version": "1.0.0"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"bloated-skill", "kind":"app_skill", "version":"1.0.0", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-bloated","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-bloated", "runId":"run-bloated", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	_, err = app.SubmitMaclawAppPackage(pkg)
	if err == nil {
		t.Fatalf("expected submit to reject a required dependency that is neither bundled nor published")
	}
	if !strings.Contains(err.Error(), "neither bundled") {
		t.Fatalf("expected publish gate error, got: %v", err)
	}
}

// TestMaclawAppBundleDependenciesForPlanDetailedReportsFailures ensures
// bundling no longer skips required dependencies silently: an installed
// dependency that cannot be matched to a local skill definition must be
// reported so preflight can surface it.
func TestMaclawAppBundleDependenciesForPlanDetailedReportsFailures(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "ok-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# ok-skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "ok-skill", SkillDir: skillDir, Status: "active", HubSkillID: "ok-skill", HubVersion: "1.0.0"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	deps := []maclawAppInstallPlanDependency{
		{ID: "ok-skill", Required: true, Installed: true, AppIDs: []string{"app-x"}},
		{ID: "ghost-skill", Required: true, Installed: true, InstalledName: "Ghost Skill", AppIDs: []string{"app-x"}},
		{ID: "optional-skill", Required: false, Installed: true, InstalledName: "Optional Ghost", AppIDs: []string{"app-x"}},
	}
	bundled, failures := app.maclawAppBundleDependenciesForPlanDetailed(deps)
	if len(bundled.Skills) != 1 {
		t.Fatalf("expected ok-skill to be bundled, got %#v", bundled.Skills)
	}
	if len(failures) != 1 || failures[0].ID != "ghost-skill" {
		t.Fatalf("expected ghost-skill bundling failure (optional deps must not be reported), got %#v", failures)
	}
}

// TestNormalizeMaclawAppDefinitionForSkillAcceptsAutomationApp aligns the save
// whitelist with the package contract, which accepts automation_app.
func TestNormalizeMaclawAppDefinitionForSkillAcceptsAutomationApp(t *testing.T) {
	doc, appID, _, err := normalizeMaclawAppDefinitionForSkill(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {"id": "auto-1", "name": "Automation", "kind": "automation_app"}
	}`, "auto-skill")
	if err != nil {
		t.Fatalf("automation_app should be savable: %v", err)
	}
	if appID != "auto-1" {
		t.Fatalf("unexpected app id: %q", appID)
	}
	appDoc, _ := doc["app"].(map[string]any)
	if stringMapValue(appDoc, "kind") != "automation_app" {
		t.Fatalf("kind should be preserved, got %#v", appDoc)
	}
}

// TestMaclawAppDependencyWarningsForDoc covers create/edit-time advisory
// warnings: references to skills that are neither installed locally nor
// remotely installable must be reported without blocking the save.
func TestMaclawAppDependencyWarningsForDoc(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}

	ghostDoc := map[string]any{
		"app": map[string]any{
			"id":   "warn-app",
			"name": "Warn App",
			"kind": "tool_app",
			"binding": map[string]any{
				"skill": map[string]any{"id": "ghost-skill"},
			},
		},
	}
	warnings := app.maclawAppDependencyWarningsForDoc(ghostDoc)
	if len(warnings) != 1 || warnings[0]["id"] != "ghost-skill" || warnings[0]["reason"] != "not_installed_no_remote_ref" {
		t.Fatalf("expected ghost dependency warning, got %#v", warnings)
	}

	remoteDoc := map[string]any{
		"app": map[string]any{
			"id":   "warn-app",
			"name": "Warn App",
			"kind": "tool_app",
			"binding": map[string]any{
				"skill": map[string]any{"id": "ghost-skill", "source": "skillmarket", "install_ref": "https://market.example.com/skills/ghost-skill"},
			},
		},
	}
	if warnings := app.maclawAppDependencyWarningsForDoc(remoteDoc); len(warnings) != 0 {
		t.Fatalf("remote install ref should suppress the warning, got %#v", warnings)
	}

	// A stable publisher.skill-name canonical id is a publish-gate-accepted
	// coordinate and must not warn even when nothing is installed locally.
	stableIDDoc := map[string]any{
		"app": map[string]any{
			"id":   "warn-app",
			"name": "Warn App",
			"kind": "tool_app",
			"binding": map[string]any{
				"dependencies": map[string]any{
					"skills": []any{map[string]any{"id": "ghost-skill", "canonical_id": "lovstudio.ghost-skill"}},
				},
			},
		},
	}
	if warnings := app.maclawAppDependencyWarningsForDoc(stableIDDoc); len(warnings) != 0 {
		t.Fatalf("valid publisher.skill-name canonical id should suppress the warning, got %#v", warnings)
	}

	// A locally installed skill must not warn.
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "local-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# local-skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	installedApp := &App{testHomeDir: tmpHome}
	cfg, err := installedApp.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "local-skill", SkillDir: skillDir, Status: "active", HubVersion: "1.0.0"},
	}
	if err := installedApp.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	localDoc := map[string]any{
		"app": map[string]any{
			"id":   "warn-app",
			"name": "Warn App",
			"kind": "tool_app",
			"binding": map[string]any{
				"skill": map[string]any{"id": "local-skill"},
			},
		},
	}
	if warnings := installedApp.maclawAppDependencyWarningsForDoc(localDoc); len(warnings) != 0 {
		t.Fatalf("installed dependency should not warn, got %#v", warnings)
	}
}

// TestPreflightMaclawAppOneClickPublishFlagsBundleFailure ensures preflight
// surfaces an installed-but-unembeddable required dependency as a blocking
// dependency_bundle check instead of letting the publish proceed on the
// external skill install path.
func TestPreflightMaclawAppOneClickPublishFlagsBundleFailure(t *testing.T) {
	tmpHome := t.TempDir()
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "bloated-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# bloated-skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	for i := 0; i < maxMaclawAppBundledSkillFiles+5; i++ {
		name := filepath.Join(skillDir, fmt.Sprintf("asset-%03d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile asset: %v", err)
		}
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "bloated-skill", SkillDir: skillDir, Status: "active", HubVersion: "1.0.0"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "bloated-dep-app",
				"name": "Bloated Dep App",
				"kind": "enterprise_normal_app",
				"binding": {
					"appSkill": {"id": "bloated-skill", "version": "1.0.0"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"business_workspace", "template":"classic_split", "regionCount":4},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"bloated-skill", "kind":"app_skill", "version":"1.0.0", "required":true, "installed":true, "health":"ready", "action":"skip"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-bloated","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-bloated", "runId":"run-bloated", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`

	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	out, err := app.PreflightMaclawAppOneClickPublish(pkg)
	if err != nil {
		t.Fatalf("PreflightMaclawAppOneClickPublish() error = %v", err)
	}
	if ready, _ := out["ready_for_hub_pack"].(bool); ready {
		t.Fatalf("ready_for_hub_pack should be false when bundling fails: %#v", out["checks"])
	}
	var bundleCheck map[string]any
	for _, item := range anySlice(out["checks"]) {
		row := anyMap(item)
		if row == nil {
			continue
		}
		if maclawAppStringValue(row, "id") == "dependency_bundle" {
			bundleCheck = row
			break
		}
	}
	if bundleCheck == nil {
		t.Fatalf("expected dependency_bundle check, got %#v", out["checks"])
	}
	if ok, _ := bundleCheck["ok"].(bool); ok {
		t.Fatalf("dependency_bundle check should not be ok: %#v", bundleCheck)
	}
	if severity := maclawAppStringValue(bundleCheck, "severity"); severity != "error" {
		t.Fatalf("dependency_bundle severity = %q", severity)
	}
	failures := anySlice(bundleCheck["failures"])
	if len(failures) != 1 || maclawAppStringValue(anyMap(failures[0]), "id") != "bloated-skill" {
		t.Fatalf("expected bloated-skill in dependency_bundle failures: %#v", bundleCheck)
	}
}
