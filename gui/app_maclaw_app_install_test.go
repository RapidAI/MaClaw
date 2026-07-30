package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestPlanMaclawAppInstallReportsGovernanceReviewErrors(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "bad-layout-plan",
			"name": "Bad Layout Plan",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":1, "regions":[{"id":"file_queue", "role":"input", "placement":"left"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-layout", "runId":"run-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if !plan.HasGovernanceReviewIssue || len(plan.GovernanceReviewIssues) != 1 {
		t.Fatalf("expected governance review issue in install plan: %#v", plan)
	}
	issue := plan.GovernanceReviewIssues[0]
	if issue.Path != "apps[0].app.governance.workspaceLayout" || !strings.Contains(issue.Message, "workspace layout") {
		t.Fatalf("unexpected governance review issue: %#v", issue)
	}
}

func TestRecordMaclawAppInstallBlocksStaleRunEvidenceWorkspaceLayoutFingerprint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-stale-layout-evidence-fingerprint",
			"name": "Install Stale Layout Evidence Fingerprint",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":2, "regions":[{"id":"input","role":"input","placement":"left"},{"id":"output","role":"output","placement":"right"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "text"]},
				"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-install-layout-evidence", "sampleInput":{"sample":true}, "expectedOutput":{"content":"ok"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"},
				"testEvidence": {"runId":"run-install-stale-layout-fp", "testProtocolFingerprint":"proto-install-layout-evidence", "verifiedAt":"2026-07-01T01:00:00Z", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content", "text"], "missingTypes":[]}}
			}
		}
	}`)
	var doc map[string]any
	if err := json.Unmarshal([]byte(pkg), &doc); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	governance := anyMap(anyMap(doc["app"])["governance"])
	testEvidence := anyMap(governance["testEvidence"])
	testEvidence["workspaceLayoutFingerprint"] = "deadbeef"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode package: %v", err)
	}

	_, err = app.RecordMaclawAppInstall(string(mutated), "market")
	if err == nil {
		t.Fatalf("expected stale workspace layout run evidence to block install")
	}
	if !strings.Contains(err.Error(), "workspace layout fingerprint") {
		t.Fatalf("expected install error to mention workspace layout fingerprint, got %v", err)
	}
}

func TestRecordMaclawAppInstallBlocksDataSrvRegistrationFailure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := maclawAppPackageWithCurrentDefinitionHashes(t, `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"source": "market",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "datasrv-offline-app",
				"name": "DataSrv Offline App",
				"version": "1.0.0",
				"kind": "enterprise_normal_app",
				"binding": {
					"datasrv": {"domain":"ops", "datasetID":"ops.offline_records", "templateID":"ops.offline_template", "objectRole":"offline_record", "preferredAction":"ops.offline_upsert"}
				},
				"ui": {"schema":"maclaw.app.ui.v1", "entry":"normal_workspace", "layouts":{"normal_workspace":{"template":"classic_split", "density":"compact", "regions":[{"id":"input_form", "role":"input", "placement":"left"}, {"id":"record_grid", "role":"record_list", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"right"}]}}},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"normal_workspace", "template":"classic_split", "density":"compact", "regionCount":3, "regions":[{"id":"input_form", "role":"input", "placement":"left"}, {"id":"record_grid", "role":"record_list", "placement":"center"}, {"id":"result_panel", "role":"output", "placement":"right"}]},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-datasrv-offline", "sampleInput":{"id":"offline-1"}, "expectedOutput":{"content":"ok"}},
					"testEvidence": {"runId":"run-datasrv-offline", "testProtocolFingerprint":"proto-datasrv-offline", "resultPayload":{"content":"ok"}, "outputs":[{"kind":"content", "title":"Result", "text":"ok"}], "resultCoverage":{"ok":true, "primary":"content", "coveredTypes":["content"], "missingTypes":[]}}
				}
			}
		}]
	}`)
	dataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/v1/data/app-installations/datasrv-offline-app" {
			t.Fatalf("unexpected DataSrv request: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, "datasrv offline", http.StatusServiceUnavailable)
	}))
	defer dataSrv.Close()
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: dataSrv.URL, Token: "data-token", TenantID: "tenant", UserID: "alice", Role: "data_admin"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	_, err := app.RecordMaclawAppInstall(pkg, "market")
	if err == nil {
		t.Fatalf("RecordMaclawAppInstall should reject failed DataSrv registration")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "datasrv app installation registration failed") || !strings.Contains(errText, "status=failed") || !strings.Contains(errText, "datasrv-offline-app") || !strings.Contains(errText, "datasrv offline") {
		t.Fatalf("DataSrv registration failure should preserve app id and HTTP reason, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("failed DataSrv registration should not persist local install audit: %#v", records)
	}
}

func TestPlanMaclawAppInstallSingleAppChecksDependencies(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-approval-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-approval-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": { "appSkill": { "id": "expense-approval-app", "version": "1.0.0" } },
			"dependencies": {
				"skills": [
					{ "id": "expense-approval-workflow", "version": ">=1.0.0 <2.0.0", "kind": "workflow_skill", "required": true, "source": "hub" },
					{ "id": "expense-exporter", "kind": "runtime_skill", "required": false }
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if plan.Schema != "maclaw.app.install_plan.v1" || len(plan.Apps) != 1 || plan.Apps[0].Kind != "enterprise_approval_app" {
		t.Fatalf("unexpected apps plan: %#v", plan)
	}
	if !plan.HasMissingRequired || len(plan.Dependencies) != 3 {
		t.Fatalf("unexpected dependency summary: %#v", plan)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-approval-app"); dep == nil || !dep.Installed || dep.Action != "skip" || dep.Kind != "app_skill" {
		t.Fatalf("super app skill should be installed: %#v", dep)
	}
	if plan.HasBlockingDependency {
		t.Fatalf("missing installable workflow should not hard block the install plan: %#v", plan)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-approval-workflow"); dep == nil || dep.Installed || dep.Action != "install" || !dep.Required {
		t.Fatalf("required workflow should be queued for install: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-exporter"); dep == nil || dep.Action != "optional_missing" || dep.Required {
		t.Fatalf("optional dependency should not block: %#v", dep)
	}
}

func TestPlanMaclawAppInstallBlocksInstalledInactiveRequiredDependency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	requiredDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "disabled-workflow")
	optionalDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "optional-exporter")
	for _, dir := range []string{requiredDir, optionalDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll skill dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte("# Skill\n"), 0o644); err != nil {
			t.Fatalf("WriteFile skill.md: %v", err)
		}
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: "disabled-workflow", SkillDir: requiredDir, Status: "disabled"},
		{Name: "optional-exporter", SkillDir: optionalDir, Status: "needs_setup"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "inactive-deps-app",
			"name": "Inactive Deps App",
			"kind": "enterprise_approval_app",
			"dependencies": {
				"skills": [
					{ "id": "disabled-workflow", "kind": "workflow_skill", "required": true, "source": "hub" },
					{ "id": "optional-exporter", "kind": "runtime_skill", "required": false, "source": "hub" }
				]
			}
		}
	}`

	plan, err := app.PlanMaclawAppInstall(pkg)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("inactive required dependency should block without counting as missing: %#v", plan)
	}
	required := maclawAppPlanDepForTest(plan, "disabled-workflow")
	if required == nil || !required.Installed || required.Action != "blocked" || required.Health != "disabled" || required.InstalledStatus != "disabled" || !strings.Contains(required.Message, "not active") {
		t.Fatalf("disabled required dependency should block: %#v", required)
	}
	optional := maclawAppPlanDepForTest(plan, "optional-exporter")
	if optional == nil || !optional.Installed || optional.Action != "optional_unhealthy" || optional.Health != "needs_setup" || optional.Required {
		t.Fatalf("inactive optional dependency should degrade without blocking: %#v", optional)
	}

	installedPlan, err := app.InstallMaclawAppDependencies(pkg)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if installedPlan.HasMissingRequired || !installedPlan.HasBlockingDependency {
		t.Fatalf("install should preserve inactive required dependency block: %#v", installedPlan)
	}
	if dep := maclawAppPlanDepForTest(installedPlan, "disabled-workflow"); dep == nil || dep.Action != "blocked" || dep.Health != "disabled" {
		t.Fatalf("disabled required dependency should remain blocked after install attempt: %#v", dep)
	}

	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err == nil || !strings.Contains(err.Error(), "required Skill dependencies") {
		t.Fatalf("RecordMaclawAppInstall should reject blocking dependencies, got %v", err)
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("blocked install should not write install audit records: %#v", records)
	}
}

func TestPlanMaclawAppInstallPackDedupesDependenciesAndNormalizesLegacyKind(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "legacy-enterprise",
				"name": "Legacy Enterprise",
				"kind": "enterprise_app",
				"binding": { "appSkill": { "id": "shared-workflow" } }
			}
		}, {
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {
				"id": "normal-enterprise",
				"name": "Normal Enterprise",
				"kind": "enterprise_normal_app",
				"dependencies": { "skills": [{ "id": "shared-workflow", "required": true }] }
			}
		}]
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if len(plan.Apps) != 2 || plan.Apps[0].Kind != "enterprise_normal_app" {
		t.Fatalf("legacy kind was not normalized: %#v", plan.Apps)
	}
	dep := maclawAppPlanDepForTest(plan, "shared-workflow")
	if dep == nil || dep.Action != "install" || len(dep.AppIDs) != 2 || plan.HasBlockingDependency {
		t.Fatalf("shared dependency should be deduped across apps: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesSkipsInstalledAndBlocksUnsupportedSource(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "installed-app-skill")
	workflowDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "dependency-install-workflow")
	for _, dir := range []string{skillDir, workflowDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll skill dir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Installed app skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "skill.md"), []byte("# Dependency install workflow\n"), 0o644); err != nil {
		t.Fatalf("WriteFile workflow skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "installed-app-skill", SkillDir: skillDir, Status: "active"}, {Name: "dependency-install-workflow", SkillDir: workflowDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "dependency-install-app",
			"name": "Dependency Install App",
			"kind": "enterprise_approval_app",
			"binding": { "appSkill": { "id": "installed-app-skill" } },
			"dependencies": { "skills": [{ "id": "dependency-install-workflow", "kind": "workflow_skill", "required": true, "source": "local" }, { "id": "manual-only-skill", "required": true, "source": "builtin" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if !plan.HasMissingRequired {
		t.Fatalf("manual-only required dependency should remain missing: %#v", plan)
	}
	if dep := maclawAppPlanDepForTest(plan, "installed-app-skill"); dep == nil || dep.Action != "skip" || !dep.Installed {
		t.Fatalf("installed app skill should be skipped: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "manual-only-skill"); dep == nil || dep.Action != "blocked" || !strings.Contains(dep.Message, "cannot be installed automatically") {
		t.Fatalf("unsupported required dependency should be blocked: %#v", dep)
	}
}

func TestMaclawAppInstallSkillSourceNormalizesHubAndMarket(t *testing.T) {
	cases := map[string]string{
		"":               "skillhub",
		"local":          "skillhub",
		"hub":            "skillhub",
		"skillhub":       "skillhub",
		"market":         "skillmarket",
		"skillmarket":    "skillmarket",
		"enterprise_hub": "enterprise_hub",
		"github":         "github",
	}
	for input, want := range cases {
		got, ok := maclawAppInstallSkillSource(input)
		if !ok || got != want {
			t.Fatalf("maclawAppInstallSkillSource(%q) = %q,%v want %q,true", input, got, ok, want)
		}
	}
	if got, ok := maclawAppInstallSkillSource("builtin"); ok || got != "" {
		t.Fatalf("builtin source should not auto-install, got %q,%v", got, ok)
	}
}

func TestInstallMaclawAppDependenciesInstallsHubBackedSources(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "dependency-source-app",
			"name": "Dependency Source App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "market-workflow", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-enterprise-workflow" },
				{ "id": "github-workflow", "kind": "workflow_skill", "required": true, "source": "github", "install_ref": "{\"repo_full_name\":\"acme/github-workflow\",\"raw_url\":\"https://raw.githubusercontent.com/acme/github-workflow/main/SKILL.md\"}" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("expected three dependency install calls, got %#v", calls)
	}
	if calls[0] != (installCall{source: "skillmarket", id: "market-workflow"}) || calls[1] != (installCall{source: "enterprise_hub", id: "enterprise-workflow", installRef: "cap-enterprise-workflow"}) || calls[2].source != "github" || calls[2].id != "github-workflow" || !strings.Contains(calls[2].installRef, "raw.githubusercontent.com/acme/github-workflow") {
		t.Fatalf("unexpected dependency install calls: %#v", calls)
	}
	for _, id := range []string{"market-workflow", "enterprise-workflow", "github-workflow"} {
		dep := maclawAppPlanDepForTest(plan, id)
		if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" {
			t.Fatalf("dependency %s should be installed and ready: %#v", id, dep)
		}
	}
	if dep := maclawAppPlanDepForTest(plan, "enterprise-workflow"); dep == nil || dep.InstallRef != "cap-enterprise-workflow" {
		t.Fatalf("enterprise dependency should preserve capability install ref: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "github-workflow"); dep == nil || !strings.Contains(dep.InstallRef, "raw.githubusercontent.com/acme/github-workflow") {
		t.Fatalf("github dependency should preserve github install ref: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("installed hub-backed dependencies should clear blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesMatchesInstalledSkillByInstallRefTarget(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "RapidOCR" || installRef != "rapidocr" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "rapidocr-runtime")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "rapidocr-runtime",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillhub",
			HubSkillID: "rapidocr",
			HubVersion: "v1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "RapidOCR", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub", "install_ref": "hub://skills/rapidocr@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "RapidOCR")
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "rapidocr-runtime" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should be matched by install_ref target after install: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("install_ref target match should clear blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesResolvesKnownRuntimeDependencyAlias(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "RapidOCR" || installRef != "rapidocr" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "rapidocr-runtime")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "rapidocr-runtime",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillhub",
			HubSkillID: "rapidocr",
			HubVersion: "v1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "RapidOCR", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "RapidOCR")
	if dep == nil || dep.InstallRefKind != "hub" || dep.InstallRefTarget != "rapidocr" || dep.InstallRefStatus != "ok" {
		t.Fatalf("dependency should resolve RapidOCR alias to rapidocr: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "rapidocr-runtime" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should be installed through known runtime alias: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("known runtime alias install should clear blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesResolvesHubAliasThroughSkillMarketSearch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const rapidOCRMarketID = "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			if query := r.URL.Query().Get("q"); query != "rapidocr" {
				t.Fatalf("unexpected search query %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{{
				ID: rapidOCRMarketID, Name: "RapidOCR", Version: "10",
			}}})
		case "/api/v1/skills/" + rapidOCRMarketID + "/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          rapidOCRMarketID,
				"name":        "rapidocr-runtime",
				"description": "RapidOCR runtime",
				"version":     "1.0.0",
				"trust_level": "trusted",
				"triggers":    []string{"rapidocr"},
				"steps": []map[string]any{{
					"action": "bash",
					"params": map[string]any{"command": "echo rapidocr"},
				}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "RapidOCR", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "RapidOCR")
	if dep == nil || dep.InstallRefTarget != rapidOCRMarketID || dep.PreflightStatus != "ready" || dep.PreflightCode != "skillmarket_target_ready" {
		t.Fatalf("dependency should resolve RapidOCR alias through SkillMarket search: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "rapidocr-runtime" {
		t.Fatalf("dependency should install and match local runtime: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("resolved hub alias install should clear blocking flags: %#v", plan)
	}
}

func TestMaclawAppHubCenterLookupPreflightChoosesExactAliasMatch(t *testing.T) {
	dep := maclawAppInstallPlanDependency{
		ID:               "RapidOCR",
		Version:          "10",
		Kind:             "runtime_skill",
		Required:         true,
		Source:           "hub",
		InstallRefTarget: "rapidocr",
		InstallRefStatus: "ok",
	}
	matched := maclawAppApplyHubCenterLookupPreflight(&dep, []SkillSearchResult{
		{ID: "unrelated-id", Name: "rapidocr-tools", InstallRef: "unrelated-id", Version: "10"},
		{ID: "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b", Name: "RapidOCR", InstallRef: "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b", Version: "10"},
	})
	if !matched || dep.Source != "skillmarket" || dep.PreflightStatus != "ready" || dep.InstallRefTarget != "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b" {
		t.Fatalf("HubCenter lookup should choose the exact RapidOCR alias match: matched=%v dep=%#v", matched, dep)
	}
}

func TestMaclawAppSkillMarketPreflightMatchesDeclaredAliases(t *testing.T) {
	dep := maclawAppInstallPlanDependency{
		ID:          "OCR Runtime",
		Version:     "10",
		Kind:        "runtime_skill",
		Required:    true,
		Source:      "skillmarket",
		CanonicalID: "rapidocr-runtime",
		Aliases:     []string{"RapidOCR", "rapidocr"},
	}
	maclawAppApplyPublicSkillMarketPreflight(&dep, []SkillSearchResult{{
		ID:         "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b",
		Name:       "RapidOCR",
		InstallRef: "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b",
		Version:    "10",
	}})
	if dep.PreflightStatus != "ready" || dep.InstallRefTarget != "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b" {
		t.Fatalf("SkillMarket preflight should match declared aliases and persist UUID install_ref: %#v", dep)
	}
}

func TestMaclawAppSkillMarketPreflightMatchesInstallRefURIWithoutAliases(t *testing.T) {
	dep := maclawAppInstallPlanDependency{
		ID:         "Friendly OCR Name",
		Version:    "1.0.0",
		Kind:       "runtime_skill",
		Required:   true,
		Source:     "hub",
		InstallRef: "skillmarket://skills/exact-market-name@1.0.0",
	}
	maclawAppApplyPublicSkillMarketPreflight(&dep, []SkillSearchResult{{
		ID:         "alias-market-id",
		Name:       "exact-market-name",
		InstallRef: "alias-market-id",
		Version:    "1.0.0",
	}})
	if dep.Source != "skillmarket" || dep.PreflightStatus != "ready" || dep.InstallRefTarget != "alias-market-id" {
		t.Fatalf("SkillMarket preflight should match URI target and normalize source: %#v", dep)
	}
}

func TestPlanMaclawAppInstallSearchesSkillMarketByDeclaredAlias(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const marketID = "alias-market-id"
	var queries []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			query := r.URL.Query().Get("q")
			queries = append(queries, query)
			if query == "exact-market-name" {
				_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{{
					ID: marketID, Name: "exact-market-name", InstallRef: marketID, Version: "1.0.0",
				}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "alias-search-app",
			"name": "Alias Search App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "Friendly OCR Name", "canonical_id": "missing-canonical-name", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "skillmarket", "aliases": ["exact-market-name"] }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "Friendly OCR Name")
	if dep == nil || dep.PreflightStatus != "ready" || dep.InstallRefTarget != marketID {
		t.Fatalf("dependency should search by declared alias and persist market UUID: queries=%v dep=%#v", queries, dep)
	}
	joinedQueries := "," + strings.Join(queries, ",") + ","
	if !strings.Contains(joinedQueries, ",missing-canonical-name,") || !strings.Contains(joinedQueries, ",exact-market-name,") {
		t.Fatalf("expected SkillMarket search to try canonical and declared alias, got %v", queries)
	}
	if !plan.HasMissingRequired || plan.HasBlockingDependency || dep.Action != "install" {
		t.Fatalf("resolved missing SkillMarket dependency should be installable without blocking: %#v", plan)
	}
}

func TestPlanMaclawAppInstallKeepsAliasPreflightErrorPending(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var queries []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			query := r.URL.Query().Get("q")
			queries = append(queries, query)
			if query == "exact-market-name" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "alias-search-app",
			"name": "Alias Search App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "Friendly OCR Name", "canonical_id": "missing-canonical-name", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "skillmarket", "aliases": ["exact-market-name"] }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "Friendly OCR Name")
	if dep == nil || dep.PreflightStatus != "pending" || dep.PreflightCode != "remote_preflight_unavailable" || dep.Action != "install" {
		t.Fatalf("partial SkillMarket search failure should stay pending and installable: queries=%v dep=%#v", queries, dep)
	}
	joinedQueries := "," + strings.Join(queries, ",") + ","
	if !strings.Contains(joinedQueries, ",missing-canonical-name,") || !strings.Contains(joinedQueries, ",exact-market-name,") {
		t.Fatalf("expected SkillMarket search to try canonical and declared alias, got %v", queries)
	}
	if !plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("partial SkillMarket search failure should not hard-block install plan: %#v", plan)
	}
}

func TestPlanMaclawAppInstallMarksNameBasedRuntimeDependencyInstallable(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "RapidOCR", "version": "10", "kind": "runtime_skill", "required": true, "source": "local" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "RapidOCR")
	if dep == nil || dep.PreflightStatus != "pending" || dep.PreflightCode != "target_resolved" || dep.Action != "install" || dep.Health != "missing" {
		t.Fatalf("name-based runtime dependency with alias should resolve target and be installable: %#v", dep)
	}
	if !plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("name-based runtime dependency should remain missing but not blocking: %#v", plan)
	}
}

func TestPlanMaclawAppInstallDoesNotBlockSkillMarketTargetWithoutChecksum(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const marketID = "8d597bd7-c33f-44e8-bd70-21d28805770b"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []SkillSearchResult{{
				ID:         marketID,
				Name:       "paper_pdf_translator",
				InstallRef: marketID,
				Version:    "1.0.0",
			}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome, hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "pdf-translator-app",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "skillmarket" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.PreflightStatus != "ready" || dep.PreflightCode != "skillmarket_target_ready" || dep.IntegrityStatus != "missing" || dep.IntegrityCode != "checksum_unavailable" || dep.Action != "install" || dep.InstallRefTarget != marketID {
		t.Fatalf("SkillMarket target without checksum should be installable with integrity warning: %#v", dep)
	}
	if !plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("missing checksum metadata should not make the install plan blocking: %#v", plan)
	}
}

func TestMaclawAppDependencySkillMarketSearchQueriesNormalizeInstallRefURI(t *testing.T) {
	queries := maclawAppDependencySkillMarketSearchQueries(maclawAppInstallPlanDependency{
		ID:          "Friendly OCR Name",
		Source:      "hub",
		InstallRef:  "skillmarket://skills/exact-market-name@1.0.0",
		CanonicalID: "exact-market-name",
		Aliases:     []string{"Friendly OCR Name"},
	})
	if got, want := strings.Join(queries, ","), "exact-market-name,Friendly OCR Name"; got != want {
		t.Fatalf("SkillMarket search queries should normalize URI refs and dedupe aliases, got %q want %q", got, want)
	}
}

func TestPlanMaclawAppInstallAcceptsEnterpriseHubVersionKeyForSemverAppDependency(t *testing.T) {
	// Regression: PDF翻译工具 declares appSkill.version "1.0.0", but the skill
	// installed from enterprise hub records hub_version as a content key
	// (enterprise_hub:skill:paper_pdf_translator@…). Local preflight must not
	// block with version_mismatch / dependency_update_failed.
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "Paper PDF Translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	versionKey := "enterprise_hub:skill:paper_pdf_translator@d774c84f9b53"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:       "Paper PDF Translator",
		SkillDir:   skillDir,
		Status:     "active",
		Source:     "hub",
		HubSkillID: "paper_pdf_translator",
		HubVersion: versionKey,
		Triggers:   []string{"paper_pdf_translator"},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "pdf-translator-app",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"appSkill": {"id": "paper_pdf_translator", "version": "1.0.0"},
			"dependencies": { "skills": [
				{ "id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || !dep.Installed || dep.Health != "ready" || dep.Action != "skip" || dep.VersionStatus != "matched" {
		t.Fatalf("installed enterprise-hub skill should satisfy app semver dependency: %#v", dep)
	}
	if dep.PreflightStatus != "ready" || dep.PreflightCode != "installed_ready" || dep.PreflightStage != "local_dependency_scan" {
		t.Fatalf("local preflight should be installed_ready, got %#v", dep)
	}
	if dep.InstalledVersion != versionKey || dep.RequiredVersion != "1.0.0" {
		t.Fatalf("version evidence should keep both coordinate systems: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("cross-format version pair must not block install plan: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesResolvesPaperPDFTranslatorAlias(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "pdf-paper-translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "paper_pdf_translator",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillhub",
			HubSkillID: "paper_pdf_translator",
			HubVersion: "v1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "pdf-translator-app",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "pdf-paper-translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "pdf-paper-translator")
	if dep == nil || dep.InstallRefKind != "hub" || dep.InstallRefTarget != "paper_pdf_translator" || dep.InstallRefStatus != "ok" {
		t.Fatalf("dependency should resolve PDF translator alias to paper_pdf_translator: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "paper_pdf_translator" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should be installed through PDF translator alias: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("PDF translator alias install should clear blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesUsesSkillMarketForMarketAppDependency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillmarket" || id != "pdf-paper-translator" || strings.TrimSpace(installRef) == "" {
			t.Fatalf("market app dependency should install from SkillMarket target, got source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "paper_pdf_translator",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillmarket",
			HubSkillID: "paper_pdf_translator",
			HubVersion: "v1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "market-pdf-translator-app",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"source": "market",
			"dependencies": { "skills": [
				{ "id": "pdf-paper-translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "pdf-paper-translator")
	if dep == nil || dep.Source != "skillmarket" || strings.TrimSpace(dep.InstallRefTarget) == "" || !dep.Installed || dep.Health != "ready" {
		t.Fatalf("market app dependency should resolve and install through SkillMarket: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesUpgradesKnownLegacyLocalDependencyForMarketApp(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillmarket" || id != "paper_pdf_translator" || strings.TrimSpace(installRef) == "" {
			t.Fatalf("legacy market dependency should install from SkillMarket, got source=%q id=%q ref=%q", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "paper_pdf_translator",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillmarket",
			HubSkillID: "paper_pdf_translator",
			HubVersion: "1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{
			"id":"market-pdf-translator-app",
			"name":"PDF translator",
			"kind":"tool_app",
			"source":"market",
			"dependencies":{"skills":[{
				"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local"
			}]}
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "skillmarket" || dep.InstallRefTarget != "paper_pdf_translator" || !dep.Installed || dep.Health != "ready" {
		t.Fatalf("known legacy local market dependency should be upgraded and installed: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDerivesSkillMarketProvenanceFromInstalledWrapper(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "pdf-translator-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "skill.md"), []byte("# PDF translator\n"), 0o644); err != nil {
		t.Fatalf("write wrapper skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"market-pdf-translator-app","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: "pdf-translator-wrapper", SkillDir: wrapperDir, Status: "active",
		Source: "skillmarket", HubSkillID: "market-pdf-translator-app",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillmarket" || id != "paper_pdf_translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("dependency should inherit the wrapper's SkillMarket provenance: source=%q id=%q ref=%q", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name: id, SkillDir: skillDir, Status: "active", Source: "skillmarket", HubSkillID: id, HubVersion: "1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	// Deliberately omit app.source. The only trusted source is the installed
	// wrapper's persisted registration, as it would be after an app restart.
	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-pdf-translator-wrapper-market-pdf-translator-app","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "skillmarket" || !dep.Installed || dep.Health != "ready" || plan.HasBlockingDependency {
		t.Fatalf("known legacy dependency should install from trusted wrapper provenance: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDerivesProvenanceFromLegacyStableWrapperPanelID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const legacyPanelID = "skill-app-paper_pdf_translator-app-pdf"
	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", legacyPanelID)
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-paper_pdf_translator-app-pdf","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","version":"1.0.0","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: legacyPanelID, SkillDir: wrapperDir, Status: "active",
		Source: "skillmarket", HubSkillID: "8d597bd7-c33f-44e8-bd70-21d28805770b",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillmarket" || id != "paper_pdf_translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("legacy stable wrapper must retain SkillMarket provenance: source=%q id=%q ref=%q", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: "skillmarket", HubSkillID: id, HubVersion: "1.0.0"})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-paper_pdf_translator-app-pdf","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"forged-local-dependency","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(plan.Dependencies) != 1 {
		t.Fatalf("legacy wrapper declaration must replace caller-provided dependencies: %#v", plan.Dependencies)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "skillmarket" || !dep.Installed || dep.Health != "ready" || plan.HasBlockingDependency {
		t.Fatalf("legacy stable wrapper should install its declared market dependency: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesRestoresBundledDependencyFromInstalledWrapper(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const panelID = "skill-app-paper_pdf_translator-app-pdf"
	sourceDir := filepath.Join(t.TempDir(), "paper_pdf_translator")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("write bundled skill: %v", err)
	}
	dep := maclawAppInstallPlanDependency{
		ID: "paper_pdf_translator", Version: "1.0.0", Kind: "runtime_skill", Required: true,
		Source: "skillmarket", CanonicalID: "paper_pdf_translator", InstallRefTarget: "paper_pdf_translator", AppIDs: []string{panelID},
	}
	bundled, err := maclawAppBundleInstalledSkill(NLSkillDefinition{
		Name: "paper_pdf_translator", SkillDir: sourceDir, Source: "skillmarket", HubSkillID: "paper_pdf_translator", HubVersion: "1.0.0",
	}, dep)
	if err != nil {
		t.Fatalf("maclawAppBundleInstalledSkill() error = %v", err)
	}
	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", panelID)
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	wrapperDoc := map[string]any{
		"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps",
		"app": map[string]any{
			"id": panelID, "name": "PDF translator", "kind": "tool_app",
			"dependencies": map[string]any{"skills": []any{map[string]any{
				"id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "local",
			}}},
		},
		"bundled_dependencies": maclawAppBundledDependencies{Schema: "maclaw.app.bundled_dependencies.v1", Skills: []maclawAppBundledSkillEntry{bundled}},
	}
	wrapperJSON, err := json.Marshal(wrapperDoc)
	if err != nil {
		t.Fatalf("Marshal wrapper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), wrapperJSON, 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: panelID, SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{
		skills:    []corelib.NLSkillEntry{{Name: panelID, SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator"}},
		createdAt: time.Now(),
	})
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		t.Fatalf("remote install must not run when the trusted wrapper embeds %q", id)
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-paper_pdf_translator-app-pdf","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"forged-local-dependency","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	installed := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if installed == nil || !installed.Installed || installed.Health != "ready" || installed.Action != "installed_from_bundle" || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("trusted wrapper bundle should restore the missing dependency: %#v", plan)
	}
	// The install result is not enough: the next authoritative publish plan must
	// see the dependency immediately even while the disk scanner is refreshing.
	replanned, err := app.PlanMaclawAppInstall(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-paper_pdf_translator-app-pdf","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"forged-local-dependency","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() after restore error = %v", err)
	}
	replannedDep := maclawAppPlanDepForTest(replanned, "paper_pdf_translator")
	if replannedDep == nil || !replannedDep.Installed || replannedDep.Health != "ready" || replanned.HasMissingRequired || replanned.HasBlockingDependency {
		t.Fatalf("restored dependency must be immediately publish-ready: %#v", replanned)
	}
	registered := app.skillExecutor.loadSkills()
	var restored *corelib.NLSkillEntry
	for i := range registered {
		if registered[i].Name == "paper_pdf_translator" {
			restored = &registered[i]
			break
		}
	}
	if restored == nil || restored.SkillDir != filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator") {
		t.Fatalf("restored runtime skill has wrong durable directory: %#v", restored)
	}
	for _, step := range restored.Steps {
		for _, key := range []string{"command", "working_dir"} {
			if value, _ := step.Params[key].(string); strings.Contains(value, "maclaw-app-bundled-skill-") {
				t.Fatalf("restored runtime step still references deleted temp directory: %s=%q", key, value)
			}
		}
	}
}

func TestInstallMaclawAppDependenciesRestoresWrapperBundleScopedToDefinitionID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const wrapperSkillID = "pdf-translator-wrapper"
	const wrapperAppID = "market-pdf-translator-app"
	const panelID = "skill-app-pdf-translator-wrapper-market-pdf-translator-app"
	sourceDir := filepath.Join(t.TempDir(), "paper_pdf_translator")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("write bundled skill: %v", err)
	}
	bundled, err := maclawAppBundleInstalledSkill(NLSkillDefinition{
		Name: "paper_pdf_translator", SkillDir: sourceDir, Source: "skillmarket", HubSkillID: "paper_pdf_translator", HubVersion: "1.0.0",
	}, maclawAppInstallPlanDependency{ID: "paper_pdf_translator", AppIDs: []string{wrapperAppID}})
	if err != nil {
		t.Fatalf("maclawAppBundleInstalledSkill() error = %v", err)
	}
	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", wrapperSkillID)
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	wrapperDoc := map[string]any{
		"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps",
		"app": map[string]any{
			"id": wrapperAppID, "name": "PDF translator", "kind": "tool_app",
			"dependencies": map[string]any{"skills": []any{map[string]any{
				"id": "paper_pdf_translator", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "local",
			}}},
		},
		"bundled_dependencies": maclawAppBundledDependencies{Schema: "maclaw.app.bundled_dependencies.v1", Skills: []maclawAppBundledSkillEntry{bundled}},
	}
	wrapperJSON, err := json.Marshal(wrapperDoc)
	if err != nil {
		t.Fatalf("Marshal wrapper: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), wrapperJSON, 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: wrapperSkillID, SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	app.cachedSkillScanner = &CachedSkillScanner{}
	app.cachedSkillScanner.cache.Store(&skillCacheEntry{skills: []corelib.NLSkillEntry{{Name: wrapperSkillID, SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator"}}, createdAt: time.Now()})
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		t.Fatalf("remote install must not run when wrapper bundle matches definition ID: %s %s %s", source, id, installRef)
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-pdf-translator-wrapper-market-pdf-translator-app","name":"PDF translator","kind":"tool_app"}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	installed := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if installed == nil || !installed.Installed || installed.Health != "ready" || installed.Action != "installed_from_bundle" || !containsMaclawAppStringFold(installed.AppIDs, panelID) || plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("definition-scoped trusted wrapper bundle should restore the panel dependency: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesRejectsConflictingTrustedWrapperBundles(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	makeBundle := func(label string, content string, appID string) maclawAppBundledSkillEntry {
		t.Helper()
		dir := filepath.Join(t.TempDir(), label)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", label, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skill.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s bundle: %v", label, err)
		}
		bundle, err := maclawAppBundleInstalledSkill(NLSkillDefinition{Name: "paper_pdf_translator", SkillDir: dir, Source: "skillmarket", HubSkillID: "paper_pdf_translator", HubVersion: "1.0.0"}, maclawAppInstallPlanDependency{ID: "paper_pdf_translator", AppIDs: []string{appID}})
		if err != nil {
			t.Fatalf("bundle %s: %v", label, err)
		}
		return bundle
	}
	writeWrapper := func(skillID, appID string, bundle maclawAppBundledSkillEntry) {
		t.Helper()
		dir := filepath.Join(tmpHome, ".maclaw", "data", "skills", skillID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll wrapper %s: %v", skillID, err)
		}
		doc := map[string]any{
			"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps",
			"app": map[string]any{
				"id": appID, "name": skillID, "kind": "tool_app",
				"dependencies": map[string]any{"skills": []any{map[string]any{"id": "paper_pdf_translator", "kind": "runtime_skill", "required": true, "source": "local"}}},
			},
			"bundled_dependencies": maclawAppBundledDependencies{Schema: "maclaw.app.bundled_dependencies.v1", Skills: []maclawAppBundledSkillEntry{bundle}},
		}
		payload, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal wrapper %s: %v", skillID, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "maclaw.app.json"), payload, 0o644); err != nil {
			t.Fatalf("write wrapper %s: %v", skillID, err)
		}
	}

	const (
		firstSkill  = "market-wrapper-one"
		firstApp    = "market-app-one"
		secondSkill = "market-wrapper-two"
		secondApp   = "market-app-two"
	)
	writeWrapper(firstSkill, firstApp, makeBundle("first", "# first payload\n", firstApp))
	writeWrapper(secondSkill, secondApp, makeBundle("second", "# second payload\n", secondApp))

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{
		{Name: firstSkill, SkillDir: filepath.Join(tmpHome, ".maclaw", "data", "skills", firstSkill), Status: "active", Source: "skillmarket", HubSkillID: "market-one"},
		{Name: secondSkill, SkillDir: filepath.Join(tmpHome, ".maclaw", "data", "skills", secondSkill), Status: "active", Source: "skillmarket", HubSkillID: "market-two"},
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		t.Fatalf("ambiguous trusted bundles must not fall back to remote: %s %s %s", source, id, installRef)
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.pack.v1", "privateMarker":"x_maclaw_apps", "apps":[
			{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"id":"skill-app-market-wrapper-one-market-app-one","name":"One","kind":"tool_app"}},
			{"schema":"maclaw.app.v1","privateMarker":"x_maclaw_apps","app":{"id":"skill-app-market-wrapper-two-market-app-two","name":"Two","kind":"tool_app"}}
		]
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Action != "failed" || dep.InstallErrorCode != "bundled_dependency_ambiguous" || dep.Installed || !plan.HasBlockingDependency {
		t.Fatalf("conflicting trusted wrapper bundles must fail closed: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesDoesNotAcceptResolvedMetadataForInstalledWrapper(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "pdf-translator-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"market-pdf-translator-app","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "pdf-translator-wrapper", SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator-app"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillmarket" || id != "paper_pdf_translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("resolved metadata must not replace wrapper authority: source=%q id=%q ref=%q", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: "skillmarket", HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-pdf-translator-wrapper-market-pdf-translator-app","name":"forged name","kind":"tool_app","dependencies":{"skills":[
			{"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"github"},
			{"id":"untrusted-extra","kind":"runtime_skill","required":true,"source":"skillmarket"}
		]}},
		"resolved_dependencies":[{"id":"paper_pdf_translator","install_ref":"evil-target","source":"github"}]
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(plan.Dependencies) != 1 {
		t.Fatalf("only the wrapper's dependency declaration may be installed: %#v", plan.Dependencies)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "skillmarket" || dep.InstallRef != "paper_pdf_translator" || !dep.Installed {
		t.Fatalf("wrapper-derived dependency should remain authoritative: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDoesNotAcceptBundleForInstalledWrapper(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "pdf-translator-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"market-pdf-translator-app","name":"PDF translator","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "pdf-translator-wrapper", SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-pdf-translator-app"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	evilDir := filepath.Join(t.TempDir(), "evil-dependency")
	if err := os.MkdirAll(evilDir, 0o755); err != nil {
		t.Fatalf("MkdirAll evilDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(evilDir, "skill.yaml"), []byte("name: paper_pdf_translator\nsteps:\n  - command: evil\n"), 0o644); err != nil {
		t.Fatalf("write forged bundle: %v", err)
	}
	forgedBundle, err := maclawAppBundleInstalledSkill(NLSkillDefinition{Name: "paper_pdf_translator", SkillDir: evilDir, HubSkillID: "paper_pdf_translator"}, maclawAppInstallPlanDependency{ID: "paper_pdf_translator", AppIDs: []string{"skill-app-pdf-translator-wrapper-market-pdf-translator-app"}})
	if err != nil {
		t.Fatalf("bundle forged dependency: %v", err)
	}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		if source != "skillmarket" || id != "paper_pdf_translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("wrapper should retain the approved market target: %q %q %q", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: paper_pdf_translator\nsteps: []\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: "skillmarket", HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	pkg, err := json.Marshal(map[string]any{
		"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps",
		"bundled_dependencies": maclawAppBundledDependencies{Skills: []maclawAppBundledSkillEntry{forgedBundle}},
		"app":                  map[string]any{"id": "skill-app-pdf-translator-wrapper-market-pdf-translator-app", "name": "PDF translator", "kind": "tool_app"},
	})
	if err != nil {
		t.Fatalf("marshal install package: %v", err)
	}
	plan, err := app.InstallMaclawAppDependencies(string(pkg))
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 1 {
		t.Fatalf("forged bundle must not bypass the trusted market installer, calls=%d", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Source != "skillmarket" {
		t.Fatalf("market wrapper dependency should be installed from its trusted provenance: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesKeepsUnknownLocalDependencyLocalWithSkillMarketWrapper(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "market-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"market-local-contract-app","name":"Market local contract","kind":"tool_app","dependencies":{"skills":[{
			"id":"private-local-runtime","version":"skillmarket:skill:forged-private-runtime@1.0.0","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "market-wrapper", SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "market-local-contract-app"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-market-wrapper-market-local-contract-app","name":"Market local contract","kind":"tool_app","dependencies":{"skills":[{
			"id":"private-local-runtime","version":"skillmarket:skill:forged-private-runtime@1.0.0","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("unknown local dependency must not gain remote authority, got %d calls", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "private-local-runtime")
	if dep == nil || dep.Source != "local" || dep.Action != "blocked" || dep.InstallErrorCode != "local_dependency_missing" {
		t.Fatalf("unknown local dependency must retain its local-only contract: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDoesNotGrantMarketAuthorityToBareAppIDCollision(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "market-wrapper")
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"collision-app","name":"Marketplace app","kind":"tool_app"}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "market-wrapper", SkillDir: wrapperDir, Status: "active", Source: "skillmarket", HubSkillID: "collision-app"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"collision-app","name":"Local collision","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("bare app ID collision must not gain marketplace authority, got %d calls", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "local" || dep.InstallErrorCode != "local_dependency_missing" {
		t.Fatalf("bare app ID collision must retain the local-only contract: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDoesNotTrustMarketLabelWithoutHubSkillID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	const wrapperSkillID = "locally-forged-market-wrapper"
	const wrapperAppID = "forged-market-app"
	wrapperDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", wrapperSkillID)
	if err := os.MkdirAll(wrapperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll wrapperDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wrapperDir, "maclaw.app.json"), []byte(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"forged-market-app","name":"Forged market wrapper","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`), 0o644); err != nil {
		t.Fatalf("write wrapper app definition: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	// A user can write Source into local config; without a HubSkillID it must
	// not become remote download authority.
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: wrapperSkillID, SkillDir: wrapperDir, Status: "active", Source: "skillmarket"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1", "privateMarker":"x_maclaw_apps",
		"app":{"id":"skill-app-locally-forged-market-wrapper-forged-market-app","name":"Forged market wrapper","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("market label without HubSkillID must not grant remote authority, got %d calls", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "local" || dep.InstallErrorCode != "local_dependency_missing" || dep.Installed {
		t.Fatalf("untrusted market label must keep caller dependency local-only: %#v", dep)
	}
}

func TestPlanMaclawAppInstallKeepsExplicitHubDependencyForMarketApp(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "market-with-hub-runtime",
			"name": "Market With Hub Runtime",
			"kind": "tool_app",
			"source": "market",
			"dependencies": { "skills": [
				{ "id": "legacy-hub-runtime", "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "legacy-hub-runtime")
	if dep == nil || dep.Source != "hub" {
		t.Fatalf("explicit non-alias Hub dependency should stay Hub even for market app: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesInfersEnterpriseHubRefFromVersionKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "enterprise_hub" || id != "paper_pdf_translator" || installRef != "paper_pdf_translator" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "paper_pdf_translator",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "enterprise_hub",
			HubSkillID: "paper_pdf_translator",
			HubVersion: "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "paper_pdf_translator",
			"name": "PDF翻译工具",
			"kind": "tool_app",
			"binding": {
				"appSkill": {
					"id": "paper_pdf_translator",
					"version": "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151"
				}
			},
			"dependencies": { "skills": [
				{ "id": "paper_pdf_translator", "version": "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151", "kind": "app_skill", "required": true, "source": "local" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Source != "enterprise_hub" || dep.InstallRefKind != "enterprise_hub" || dep.InstallRefTarget != "paper_pdf_translator" || dep.InstallRefVersion != "d1cb0335a151" {
		t.Fatalf("dependency should infer enterprise Hub install ref from version key: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should install through inferred enterprise Hub ref: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesPrefersBundledSkill(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	sourceDir := filepath.Join(t.TempDir(), "bundled_dep")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "skill.yaml"), []byte("name: bundled_dep\ndescription: bundled fallback\nsteps:\n  - action: bash\n    params:\n      command: echo bundled\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "SKILL.md"), []byte("# bundled_dep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
	dep := maclawAppInstallPlanDependency{
		ID:               "bundled_dep",
		Version:          "1.0.0",
		Kind:             "runtime_skill",
		Required:         true,
		Source:           "hub",
		CanonicalID:      "bundled_dep",
		InstallRefTarget: "bundled_dep",
		AppIDs:           []string{"bundle-app"},
	}
	bundled, err := maclawAppBundleInstalledSkill(NLSkillDefinition{
		Name:       "bundled_dep",
		SkillDir:   sourceDir,
		Source:     "local",
		HubSkillID: "bundled_dep",
		HubVersion: "1.0.0",
	}, dep)
	if err != nil {
		t.Fatalf("maclawAppBundleInstalledSkill() error = %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		t.Fatalf("remote install must not run when the package embeds %q", id)
		return nil
	}
	pkgDoc := map[string]any{
		"schema":        "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": map[string]any{
			"id":   "bundle-app",
			"name": "Bundled App",
			"kind": "tool_app",
			"dependencies": map[string]any{"skills": []any{map[string]any{
				"id":          "bundled_dep",
				"version":     "1.0.0",
				"kind":        "runtime_skill",
				"required":    true,
				"source":      "hub",
				"install_ref": "bundled_dep",
			}}},
		},
		"bundled_dependencies": maclawAppBundledDependencies{
			Schema: "maclaw.app.bundled_dependencies.v1",
			Skills: []maclawAppBundledSkillEntry{bundled},
		},
	}
	pkgBytes, err := json.Marshal(pkgDoc)
	if err != nil {
		t.Fatalf("Marshal package: %v", err)
	}

	plan, err := app.InstallMaclawAppDependencies(string(pkgBytes))
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	installed := maclawAppPlanDepForTest(plan, "bundled_dep")
	if installed == nil || !installed.Installed || installed.Health != "ready" || installed.InstalledName != "bundled_dep" {
		t.Fatalf("dependency should install from the bundled package: %#v", installed)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	registered := app.ListNLSkills()
	if len(registered) != 1 || registered[0].Name != "bundled_dep" || registered[0].Source != "hub" || registered[0].HubSkillID != "bundled_dep" {
		t.Fatalf("bundled skill should be registered with stable identity metadata: cfg=%#v registered=%#v", cfg.NLSkills, registered)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".maclaw", "data", "skills", "bundled_dep", "skill.yaml")); err != nil {
		t.Fatalf("bundled skill files should be installed: %v", err)
	}
}

func TestInstallMaclawAppDependenciesDoesNotFallBackToRemoteAfterBundledFailure(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		return nil
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"bundled_dependencies":{"schema":"maclaw.app.bundled_dependencies.v1","skills":[{
			"id":"broken-bundle",
			"name":"broken-bundle",
			"sha256":"not-the-payload-hash",
			"files":{"skill.yaml":"bmFtZTogYnJva2VuLWJ1bmRsZQpzdGVwczogW10K"}
		}]},
		"app":{"id":"broken-bundle-app","name":"Broken bundle","kind":"tool_app","dependencies":{"skills":[{
			"id":"broken-bundle","kind":"runtime_skill","required":true,"source":"hub","install_ref":"broken-bundle"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("remote installer called %d times after bundled package verification failed", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "broken-bundle")
	if dep == nil || dep.Action != "failed" || dep.InstallErrorCode != "bundled_dependency_failed" || dep.InstallErrorStage != "bundled_dependency_install" {
		t.Fatalf("bundled verification failure should be terminal: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesDoesNotDownloadMissingLocalDependency(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remoteCalls := 0
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		remoteCalls++
		return nil
	}
	plan, err := app.InstallMaclawAppDependencies(`{
		"schema":"maclaw.app.v1",
		"privateMarker":"x_maclaw_apps",
		"app":{"id":"local-dependency-app","name":"Local dependency app","kind":"tool_app","dependencies":{"skills":[{
			"id":"paper_pdf_translator","kind":"runtime_skill","required":true,"source":"local","version":"1.0.0"
		}]}}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("missing local dependency must not trigger a remote download, got %d calls", remoteCalls)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || dep.Action != "blocked" || dep.InstallErrorCode != "local_dependency_missing" || dep.InstallErrorStage != "local_dependency_scan" {
		t.Fatalf("missing local dependency should explain the local recovery path: %#v", dep)
	}
}

func TestInstallMaclawAppDependenciesUsesBundledDefinitionNameForDependencyMatch(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	sourceDir := filepath.Join(t.TempDir(), "Paper PDF Translator")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sourceDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "skill.yaml"), []byte("name: Paper PDF Translator\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	bundled, err := maclawAppBundleInstalledSkill(NLSkillDefinition{
		Name:       "Paper PDF Translator",
		SkillDir:   sourceDir,
		HubSkillID: "paper_pdf_translator",
		HubVersion: "1.0.0",
	}, maclawAppInstallPlanDependency{ID: "paper_pdf_translator", AppIDs: []string{"pdf-translator"}})
	if err != nil {
		t.Fatalf("bundle skill files: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		t.Fatalf("remote install must not run for a bundled dependency: %s %s %s", source, id, installRef)
		return nil
	}
	pkg, err := json.Marshal(map[string]any{
		"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps",
		"bundled_dependencies": maclawAppBundledDependencies{Skills: []maclawAppBundledSkillEntry{bundled}},
		"app": map[string]any{
			"id": "pdf-translator", "name": "PDF translator", "kind": "tool_app",
			"dependencies": map[string]any{"skills": []map[string]any{{"id": "paper_pdf_translator", "kind": "runtime_skill", "required": true, "source": "hub"}}},
		},
	})
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	plan, err := app.InstallMaclawAppDependencies(string(pkg))
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "paper_pdf_translator")
	if dep == nil || !dep.Installed || dep.Action != "installed_from_bundle" || dep.InstalledName != "Paper PDF Translator" || dep.RuntimeSkillRef != "paper_pdf_translator" {
		t.Fatalf("bundled dependency should resolve by package ID and retain runtime ID: %#v", dep)
	}
}

func TestMaclawAppBundledDependenciesForPlanMatchesDisplayNameByStableID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "Paper PDF Translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: Paper PDF Translator\nsteps: []\n"), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: "Paper PDF Translator", SkillDir: skillDir, Status: "active", Source: "hub", HubSkillID: "paper_pdf_translator", HubVersion: "1.0.0",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	bundled := app.maclawAppBundledDependenciesForPlan([]maclawAppInstallPlanDependency{{
		ID: "paper_pdf_translator", CanonicalID: "paper_pdf_translator", Installed: true, InstalledName: "Paper PDF Translator", AppIDs: []string{"pdf-app"},
	}})
	if len(bundled.Skills) != 1 || bundled.Skills[0].Name != "Paper PDF Translator" || bundled.Skills[0].HubSkillID != "paper_pdf_translator" || len(bundled.Skills[0].Files) == 0 {
		t.Fatalf("stable dependency id should bundle the installed display-name skill: %#v", bundled)
	}
}

func TestMaclawAppBundleInstalledSkillChecksumIsDeterministic(t *testing.T) {
	makeSkill := func(files []string) string {
		dir := t.TempDir()
		for _, rel := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("MkdirAll %s: %v", rel, err)
			}
			if err := os.WriteFile(path, []byte("content:"+rel+"\n"), 0o644); err != nil {
				t.Fatalf("WriteFile %s: %v", rel, err)
			}
		}
		return dir
	}
	dep := maclawAppInstallPlanDependency{ID: "stable_dep", Version: "1.0.0", InstalledVersion: "1.0.0"}
	first, err := maclawAppBundleInstalledSkill(NLSkillDefinition{Name: "stable_dep", SkillDir: makeSkill([]string{"skill.yaml", "nested/tool.py", "SKILL.md"})}, dep)
	if err != nil {
		t.Fatalf("maclawAppBundleInstalledSkill(first) error = %v", err)
	}
	second, err := maclawAppBundleInstalledSkill(NLSkillDefinition{Name: "stable_dep", SkillDir: makeSkill([]string{"nested/tool.py", "SKILL.md", "skill.yaml"})}, dep)
	if err != nil {
		t.Fatalf("maclawAppBundleInstalledSkill(second) error = %v", err)
	}
	if first.SHA256 != second.SHA256 {
		t.Fatalf("bundle checksum should not depend on file creation order: first=%s second=%s", first.SHA256, second.SHA256)
	}
}

func TestInstallMaclawAppDependenciesUpdatesInstalledHubSkillWhenRemoteIsNewer(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	defer func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	}()

	var metadataHits, downloadHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"urls":[],"nodes":[]}`))
		case "/api/v1/skills/remote_dep":
			metadataHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"remote_dep","name":"remote_dep","version":"2.0.0","trust_level":"trusted"}`))
		case "/api/v1/skills/remote_dep/download":
			downloadHits++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"remote_dep","name":"remote_dep","description":"new remote dep","version":"2.0.0","trust_level":"trusted","triggers":["remote"],"steps":[{"action":"bash","params":{"command":"echo remote"},"on_error":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "remote_dep")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# remote_dep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "stale.txt"), []byte("old file should be removed\n"), 0o644); err != nil {
		t.Fatalf("WriteFile stale.txt: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  server.URL,
		RemoteHubCenterURLs: []string{server.URL},
		NLSkills: []corelib.NLSkillEntry{{
			Name:       "remote_dep",
			SkillDir:   skillDir,
			Steps:      []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo old"}}},
			Status:     "active",
			Source:     "hub",
			HubSkillID: "remote_dep",
			HubVersion: "1.0.0",
		}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	packageJSON := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "remote-update-app",
			"name": "Remote Update App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "remote_dep", "version": "2.0.0", "kind": "runtime_skill", "required": true, "source": "hub", "install_ref": "remote_dep" }
			] }
		}
	}`
	plan, err := app.InstallMaclawAppDependencies(packageJSON)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "remote_dep")
	if dep == nil || !dep.Installed || dep.Health != "ready" || dep.InstalledVersion != "2.0.0" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should update installed Hub skill to remote version: metadata=%d download=%d dep=%#v", metadataHits, downloadHits, dep)
	}
	if downloadHits == 0 {
		t.Fatalf("expected update download call, metadata=%d download=%d", metadataHits, downloadHits)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].HubVersion != "2.0.0" || cfg.NLSkills[0].Description != "new remote dep" {
		t.Fatalf("installed skill should be updated from remote: %#v", cfg.NLSkills)
	}
	if cfg.NLSkills[0].SkillDir != skillDir {
		t.Fatalf("updated skill should stay registered at final target dir: got %q want %q", cfg.NLSkills[0].SkillDir, skillDir)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("remote update should replace old skill dir and remove stale files, stat err=%v", err)
	}
	if _, err := readSkillScanCache(skillDir, "remote_dep"); err != nil {
		t.Fatalf("remote update should persist scan cache in final skill dir: %v", err)
	}
}

func TestInstallMaclawAppDependenciesUsesDeclaredCanonicalDependencyTarget(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		if source != "skillhub" || id != "OCR Runtime" || installRef != "rapidocr" {
			t.Fatalf("unexpected dependency install call: source=%s id=%s installRef=%s", source, id, installRef)
		}
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "rapidocr-runtime")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
			Name:       "rapidocr-runtime",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillhub",
			HubSkillID: "rapidocr",
			HubVersion: "v1.0.0",
		})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "OCR Runtime", "canonical_id": "rapidocr", "aliases": ["RapidOCR", "rapidocr-runtime"], "version": "1.0.0", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "OCR Runtime")
	if dep == nil || dep.CanonicalID != "rapidocr" || dep.InstallRefTarget != "rapidocr" || dep.InstallRefStatus != "ok" {
		t.Fatalf("dependency should use declared canonical target: %#v", dep)
	}
	if dep == nil || !dep.Installed || dep.Action != "installed" || dep.Health != "ready" || dep.InstalledName != "rapidocr-runtime" || dep.VersionStatus != "matched" {
		t.Fatalf("dependency should install through declared canonical target: %#v", dep)
	}
}

func TestMaclawAppResolvedDependenciesOverrideDeclaredAliasInstallRef(t *testing.T) {
	const marketID = "5ce9973a-a8cd-465a-a3a3-a8d95d2eb69b"
	deps := []maclawAppInstallPlanDependency{{
		ID:                "RapidOCR",
		Kind:              "runtime_skill",
		Required:          true,
		Source:            "hub",
		InstallRef:        "rapidocr",
		InstallRefKind:    "hub",
		InstallRefTarget:  "rapidocr",
		InstallRefVersion: "10",
	}}
	applyResolvedDependenciesToPlan(deps, map[string]any{"resolved_dependencies": []interface{}{
		map[string]interface{}{
			"id":                  "RapidOCR",
			"install_ref":         marketID,
			"source":              "skillmarket",
			"version":             "10",
			"install_ref_kind":    "skillmarket",
			"install_ref_target":  marketID,
			"install_ref_version": "10",
		},
	}})
	if deps[0].Source != "skillmarket" || deps[0].InstallRef != marketID || deps[0].InstallRefKind != "skillmarket" || deps[0].InstallRefTarget != marketID {
		t.Fatalf("resolved dependency should override declared alias ref with concrete SkillMarket ref: %#v", deps[0])
	}
	maclawAppValidateDependencyInstallRefs(deps)
	if deps[0].InstallRefKind != "skillmarket" || deps[0].InstallRefTarget != marketID || deps[0].InstallRefStatus != "ok" {
		t.Fatalf("bare resolved SkillMarket UUID should retain SkillMarket install_ref kind after validation: %#v", deps[0])
	}
}

func TestMaclawAppInjectsEntryScopedResolvedAndBundledDependencies(t *testing.T) {
	pkg := map[string]any{
		"apps": []any{
			map[string]any{"app": map[string]any{"id": "public-ocr-app"}},
			map[string]any{"app": map[string]any{"id": "private-ocr-app"}},
		},
	}
	resolved := []map[string]any{
		{"id": "RapidOCR", "install_ref": "public-market-uuid", "app_ids": []string{"public-ocr-app"}},
		{"id": "RapidOCR", "install_ref": "private-hub-uuid", "app_ids": []string{"private-ocr-app"}},
		{"id": "SharedRuntime", "install_ref": "shared-runtime-uuid"},
	}
	injectResolvedDepsIntoAppEntries(pkg, resolved)

	data, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal scoped resolved package: %v", err)
	}
	var resolvedRoundTrip map[string]any
	if err := json.Unmarshal(data, &resolvedRoundTrip); err != nil {
		t.Fatalf("unmarshal scoped resolved package: %v", err)
	}
	apps := anySlice(resolvedRoundTrip["apps"])
	publicResolved := anySlice(anyMap(apps[0])["resolved_dependencies"])
	privateResolved := anySlice(anyMap(apps[1])["resolved_dependencies"])
	if len(publicResolved) != 2 || anyMap(publicResolved[0])["install_ref"] != "public-market-uuid" || anyMap(publicResolved[1])["install_ref"] != "shared-runtime-uuid" {
		t.Fatalf("public app entry should only carry scoped and global resolved deps: %#v", publicResolved)
	}
	if len(privateResolved) != 2 || anyMap(privateResolved[0])["install_ref"] != "private-hub-uuid" || anyMap(privateResolved[1])["install_ref"] != "shared-runtime-uuid" {
		t.Fatalf("private app entry should only carry scoped and global resolved deps: %#v", privateResolved)
	}

	injectBundledDepsIntoAppEntries(pkg, maclawAppBundledDependencies{
		Schema: "maclaw.app.bundled_dependencies.v1",
		Skills: []maclawAppBundledSkillEntry{
			{StableID: "public", Name: "RapidOCR", SHA256: "public-sha", Files: map[string]string{"skill.md": "public"}, AppIDs: []string{"public-ocr-app"}},
			{StableID: "private", Name: "RapidOCR", SHA256: "private-sha", Files: map[string]string{"skill.md": "private"}, AppIDs: []string{"private-ocr-app"}},
			{StableID: "shared", Name: "SharedRuntime", SHA256: "shared-sha", Files: map[string]string{"skill.md": "shared"}},
		},
	})
	data, err = json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal scoped package: %v", err)
	}
	var roundTrip map[string]any
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal scoped package: %v", err)
	}
	apps = anySlice(roundTrip["apps"])
	publicBundled := anyMap(anyMap(apps[0])["bundled_dependencies"])
	privateBundled := anyMap(anyMap(apps[1])["bundled_dependencies"])
	publicSkills := anySlice(publicBundled["skills"])
	privateSkills := anySlice(privateBundled["skills"])
	if len(publicSkills) != 2 || anyMap(publicSkills[0])["stable_id"] != "public" || anyMap(publicSkills[1])["stable_id"] != "shared" {
		t.Fatalf("public app entry should only carry scoped and global bundled deps: %#v", publicBundled)
	}
	if len(privateSkills) != 2 || anyMap(privateSkills[0])["stable_id"] != "private" || anyMap(privateSkills[1])["stable_id"] != "shared" {
		t.Fatalf("private app entry should only carry scoped and global bundled deps: %#v", privateBundled)
	}
}

func TestMaclawAppSourceVersionKeyDependenciesSerializeInstallRefs(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{
		{
			ID:       "paper_pdf_translator",
			Version:  "enterprise_hub:skill:paper_pdf_translator@d1cb0335a151",
			Kind:     "app_skill",
			Required: true,
			Source:   "local",
		},
		{
			ID:       "hubcenter-paper",
			Version:  "hubcenter:skill:hubcenter-paper@v2",
			Kind:     "runtime_skill",
			Required: true,
			Source:   "local",
		},
	}
	maclawAppApplySourceVersionKeyDependencyRefs(deps)
	serialized := maclawAppSerializableResolvedDeps(deps)
	if len(serialized) != 2 {
		t.Fatalf("source version keys should serialize resolved dependencies: %#v", serialized)
	}
	if serialized[0]["source"] != "enterprise_hub" || serialized[0]["install_ref"] != "enterprise_hub://capabilities/paper_pdf_translator@d1cb0335a151" || serialized[0]["canonical_id"] != "paper_pdf_translator" {
		t.Fatalf("enterprise dependency should carry installable resolved ref: %#v", serialized[0])
	}
	if serialized[1]["source"] != "skillmarket" || serialized[1]["install_ref"] != "skillmarket://skills/hubcenter-paper@v2" || serialized[1]["canonical_id"] != "hubcenter-paper" {
		t.Fatalf("hubcenter dependency should carry SkillMarket resolved ref: %#v", serialized[1])
	}
}

func TestMaclawAppSkillMarketPreflightPersistsInstallRef(t *testing.T) {
	dep := maclawAppInstallPlanDependency{
		ID:               "paper_pdf_translator",
		Kind:             "runtime_skill",
		Required:         true,
		Source:           "skillmarket",
		InstallRefKind:   "skillmarket",
		InstallRefTarget: "paper_pdf_translator",
		InstallRefStatus: "ok",
		Version:          "1.0.0",
	}
	maclawAppApplyPublicSkillMarketPreflight(&dep, []SkillSearchResult{{
		ID:         "8d597bd7-c33f-44e8-bd70-21d28805770b",
		Name:       "paper_pdf_translator",
		InstallRef: "8d597bd7-c33f-44e8-bd70-21d28805770b",
		Version:    "1.0.0",
	}})
	if dep.PreflightStatus != "ready" || dep.InstallRef != "8d597bd7-c33f-44e8-bd70-21d28805770b" || dep.InstallRefTarget != "8d597bd7-c33f-44e8-bd70-21d28805770b" {
		t.Fatalf("SkillMarket preflight should persist concrete install_ref for download: %#v", dep)
	}
}

func TestPlanMaclawAppInstallMatchesKnownRuntimeDependencyAliasByLocalName(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "rapidocr-runtime")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{
		Name:     "rapidocr-runtime",
		SkillDir: skillDir,
		Status:   "active",
		Source:   "skillhub",
	})
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "ocr-app",
			"name": "OCR App",
			"kind": "tool_app",
			"dependencies": { "skills": [
				{ "id": "RapidOCR", "kind": "runtime_skill", "required": true, "source": "hub" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "RapidOCR")
	if dep == nil || !dep.Installed || dep.Health != "ready" || dep.Action != "skip" || dep.InstalledName != "rapidocr-runtime" {
		t.Fatalf("dependency should match local rapidocr-runtime alias: %#v", dep)
	}
	if dep.InstallRefTarget != "rapidocr" || dep.PreflightCode != "installed_ready" {
		t.Fatalf("dependency should resolve alias and report installed_ready: %#v", dep)
	}
	if plan.HasMissingRequired || plan.HasBlockingDependency {
		t.Fatalf("local runtime alias should clear blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesClassifiesInstallFailures(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		switch id {
		case "missing-market-workflow":
			return fmt.Errorf("404 skill not found")
		case "policy-enterprise-workflow":
			return fmt.Errorf("blocked by capability market policy: enterprise-only installs required")
		case "checksum-hub-workflow":
			return fmt.Errorf("package checksum mismatch for %s", installRef)
		default:
			return fmt.Errorf("unexpected install %s/%s/%s", source, id, installRef)
		}
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-failure-app",
			"name": "Install Failure App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "missing-market-workflow", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "policy-enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/policy-enterprise-workflow@1.0.0" },
				{ "id": "checksum-hub-workflow", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/checksum-hub-workflow@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	cases := map[string]struct {
		code  string
		stage string
	}{
		"missing-market-workflow":    {code: "not_found", stage: "skillmarket_download"},
		"policy-enterprise-workflow": {code: "policy_rejected", stage: "enterprise_hub_install"},
		"checksum-hub-workflow":      {code: "package_integrity_failed", stage: "skillhub_download"},
	}
	for id, want := range cases {
		dep := maclawAppPlanDepForTest(plan, id)
		if dep == nil || dep.Action != "failed" || dep.Health != "missing" || dep.InstallErrorCode != want.code || dep.InstallErrorStage != want.stage || dep.InstallErrorDetail == "" {
			t.Fatalf("dependency %s failure diagnostics mismatch: got %#v want code=%s stage=%s", id, dep, want.code, want.stage)
		}
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("failed required installs should set blocking flags: %#v", plan)
	}
}

func TestInstallMaclawAppDependenciesParsesInstallRefs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: installRef})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "install-ref-app",
			"name": "Install Ref App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "uri-workflow", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/uri-workflow@2.1.0" },
				{ "id": "enterprise-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-enterprise-workflow@1.4.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected two dependency install calls, got %#v", calls)
	}
	if calls[0] != (installCall{source: "skillhub", id: "uri-workflow", installRef: "uri-workflow"}) {
		t.Fatalf("hub URI should install by parsed target, got %#v", calls[0])
	}
	if calls[1] != (installCall{source: "enterprise_hub", id: "enterprise-workflow", installRef: "cap-enterprise-workflow"}) {
		t.Fatalf("enterprise URI should install by parsed capability target, got %#v", calls[1])
	}
	dep := maclawAppPlanDepForTest(plan, "uri-workflow")
	if dep == nil || dep.InstallRefKind != "hub" || dep.InstallRefTarget != "uri-workflow" || dep.InstallRefVersion != "2.1.0" || dep.InstallRefStatus != "ok" || dep.PreflightStatus != "pending" || dep.PreflightCode != "target_resolved" {
		t.Fatalf("hub dependency install_ref diagnostics missing: %#v", dep)
	}
	dep = maclawAppPlanDepForTest(plan, "enterprise-workflow")
	if dep == nil || dep.InstallRefKind != "enterprise_hub" || dep.InstallRefTarget != "cap-enterprise-workflow" || dep.InstallRefVersion != "1.4.0" || dep.InstallRefStatus != "ok" || dep.PreflightStatus != "pending" || dep.PreflightCode != "remote_preflight_unavailable" || dep.PreflightStage != "enterprise_hub_preflight" {
		t.Fatalf("enterprise dependency install_ref diagnostics missing: %#v", dep)
	}
}

func TestPlanMaclawAppInstallPreflightsSkillMarketDependency(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{server.URL}, "ttl_seconds": 60})
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{"quality_score": 99, "routable": true})
		case "/api/v1/skillmarket/search":
			query := r.URL.Query().Get("q")
			var results []SkillSearchResult
			switch query {
			case "market-ready-workflow":
				results = []SkillSearchResult{{ID: "market-ready-workflow", Name: "Market Ready Workflow", Version: "v1.2.0", PackageSHA256: "sha-ready", PackageSignature: "sig-ready", PackageDownloadURL: "https://skillmarket.example/download/market-ready-workflow"}}
			case "market-newer-workflow":
				results = []SkillSearchResult{{ID: "market-newer-workflow", Name: "Market Newer Workflow", Version: "2.5.0", PackageSHA256: "sha-newer", PackageSignature: "sig-newer", PackageDownloadURL: "https://skillmarket.example/download/market-newer-workflow"}}
			case "market-old-workflow":
				results = []SkillSearchResult{{ID: "market-old-workflow", Name: "Market Old Workflow", Version: "1.0.0", PackageSHA256: "sha-old"}}
			case "market-missing-workflow":
				results = []SkillSearchResult{}
			default:
				t.Fatalf("unexpected search query %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir(), hubCenterCache: remote.NewHubCenterSelectionCache(time.Minute)}
	app.hubCenterCache.Set(server.URL, []string{server.URL})
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "skillmarket-preflight-app",
			"name": "SkillMarket Preflight App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "market-ready-workflow", "version": "1.2.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "market-newer-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "market-old-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" },
				{ "id": "market-missing-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "skillmarket" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	ready := maclawAppPlanDepForTest(plan, "market-ready-workflow")
	if ready == nil || ready.PreflightStatus != "ready" || ready.PreflightCode != "skillmarket_target_ready" || ready.PreflightStage != "skillmarket_preflight" || ready.IntegrityStatus != "ready" || ready.IntegrityCode != "package_integrity_metadata_ready" || ready.PackageSHA256 != "sha-ready" || ready.PackageSignature != "sig-ready" || ready.PackageDownloadURL == "" {
		t.Fatalf("ready SkillMarket dependency preflight mismatch: %#v", ready)
	}
	newer := maclawAppPlanDepForTest(plan, "market-newer-workflow")
	if newer == nil || newer.PreflightStatus != "ready" || newer.PreflightCode != "skillmarket_target_ready" || newer.PackageSHA256 != "sha-newer" {
		t.Fatalf("newer SkillMarket dependency should satisfy minimum required version: %#v", newer)
	}
	old := maclawAppPlanDepForTest(plan, "market-old-workflow")
	if old == nil || old.PreflightStatus != "blocked" || old.PreflightCode != "version_mismatch" || old.Action != "blocked" || old.IntegrityStatus != "partial" || old.IntegrityCode != "signature_unavailable" || old.PackageSHA256 != "sha-old" || !strings.Contains(old.PreflightMessage, "version 1.0.0") {
		t.Fatalf("version mismatch SkillMarket dependency should be blocked: %#v", old)
	}
	missing := maclawAppPlanDepForTest(plan, "market-missing-workflow")
	if missing == nil || missing.PreflightStatus != "blocked" || missing.PreflightCode != "not_found" || missing.Action != "blocked" {
		t.Fatalf("missing SkillMarket dependency should be blocked: %#v", missing)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("blocked SkillMarket preflight should set blocking flags: %#v", plan)
	}
}

func TestPlanMaclawAppInstallPreflightsEnterpriseHubCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		switch r.URL.Path {
		case "/api/capabilities/cap-ready-workflow":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{ID: "cap-ready-workflow", CapabilityID: "ready-workflow", CapabilityType: "skill", Status: "published", CurrentVersionKey: "v1.2.0", MetadataJSON: `{"package_sha256":"enterprise-sha-ready","package_signature":"enterprise-sig-ready","package_download_url":"https://hub.example/packages/cap-ready-workflow"}`})
		case "/api/capabilities/cap-newer-workflow":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{ID: "cap-newer-workflow", CapabilityID: "newer-workflow", CapabilityType: "skill", Status: "published", CurrentVersionKey: "2.5.0", PackageSHA256: "enterprise-sha-newer", PackageSignature: "enterprise-sig-newer"})
		case "/api/capabilities/cap-old-workflow":
			_ = json.NewEncoder(w).Encode(HubCapabilitySummary{ID: "cap-old-workflow", CapabilityID: "old-workflow", CapabilityType: "skill", Status: "published", CurrentVersionKey: "1.0.0", PackageSHA256: "enterprise-sha-old"})
		case "/api/capabilities/cap-missing-workflow":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "enterprise-preflight-app",
			"name": "Enterprise Preflight App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [
				{ "id": "ready-workflow", "version": "1.2.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-ready-workflow@1.2.0" },
				{ "id": "newer-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-newer-workflow@2.0.0" },
				{ "id": "old-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-old-workflow@2.0.0" },
				{ "id": "missing-workflow", "version": "1.0.0", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "install_ref": "enterprise_hub://capabilities/cap-missing-workflow@1.0.0" }
			] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	ready := maclawAppPlanDepForTest(plan, "ready-workflow")
	if ready == nil || ready.PreflightStatus != "ready" || ready.PreflightCode != "enterprise_hub_target_ready" || ready.PreflightStage != "enterprise_hub_preflight" || ready.IntegrityStatus != "ready" || ready.PackageSHA256 != "enterprise-sha-ready" || ready.PackageSignature != "enterprise-sig-ready" || ready.PackageDownloadURL == "" {
		t.Fatalf("ready enterprise dependency preflight mismatch: %#v", ready)
	}
	newer := maclawAppPlanDepForTest(plan, "newer-workflow")
	if newer == nil || newer.PreflightStatus != "ready" || newer.PreflightCode != "enterprise_hub_target_ready" || newer.PackageSHA256 != "enterprise-sha-newer" || newer.PackageSignature != "enterprise-sig-newer" {
		t.Fatalf("newer enterprise dependency should satisfy minimum required version: %#v", newer)
	}
	old := maclawAppPlanDepForTest(plan, "old-workflow")
	if old == nil || old.PreflightStatus != "blocked" || old.PreflightCode != "version_mismatch" || old.Action != "blocked" || old.IntegrityStatus != "partial" || old.IntegrityCode != "signature_unavailable" || old.PackageSHA256 != "enterprise-sha-old" || !strings.Contains(old.PreflightMessage, "version 1.0.0") {
		t.Fatalf("version mismatch enterprise dependency should be blocked: %#v", old)
	}
	missing := maclawAppPlanDepForTest(plan, "missing-workflow")
	if missing == nil || missing.PreflightStatus != "blocked" || missing.PreflightCode != "not_found" || missing.Action != "blocked" {
		t.Fatalf("missing enterprise dependency should be blocked: %#v", missing)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("blocked enterprise preflight should set blocking flags: %#v", plan)
	}
}

func TestPlanMaclawAppInstallBlocksInstallRefVersionMismatch(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "version-mismatch-app",
			"name": "Version Mismatch App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "approval-workflow", "version": "2.0.0", "kind": "workflow_skill", "required": true, "source": "hub", "install_ref": "hub://skills/approval-workflow@1.0.0" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "approval-workflow")
	if dep == nil || dep.PreflightStatus != "blocked" || dep.PreflightCode != "version_mismatch" || dep.PreflightStage != "install_ref" || dep.Action != "blocked" || !strings.Contains(dep.PreflightMessage, "install_ref version 1.0.0") {
		t.Fatalf("install_ref version mismatch should be blocked by preflight: %#v", dep)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("install_ref version mismatch should set blocking flags: %#v", plan)
	}
}

func TestPlanMaclawAppInstallBlocksEnterpriseDependencyWithoutInstallRef(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "missing-install-ref-app",
			"name": "Missing Install Ref App",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "enterprise-only-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub" }] }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "enterprise-only-workflow")
	if dep == nil || dep.InstallRefStatus != "missing" || dep.PreflightStatus != "blocked" || dep.PreflightCode != "missing_install_ref" || dep.Action != "blocked" || !strings.Contains(dep.Message, "must include install_ref") {
		t.Fatalf("enterprise dependency should be blocked with install_ref diagnostic: %#v", dep)
	}
	if !plan.HasMissingRequired || !plan.HasBlockingDependency {
		t.Fatalf("missing enterprise install_ref should set blocking flags: %#v", plan)
	}
}

func TestPlanMaclawAppInstallNormalizesWorkspaceLayout(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "layout-approval",
			"name": "Layout Approval",
			"kind": "enterprise_approval_app",
			"dependencies": { "skills": [{ "id": "layout-approval-workflow", "kind": "workflow_skill", "required": true, "source": "hub" }] },
			"ui": {
				"entry": "approval_workspace",
				"layouts": {
					"approval_workspace": {
						"template": "left_nav",
						"density": "compact",
						"regions": [
							{"id":"request_form","role":"input","placement":"left"},
							{"id":"approval_detail","role":"detail","placement":"center"}
						]
					}
				}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if len(plan.Apps) != 1 || plan.Apps[0].ID != "layout-approval" {
		t.Fatalf("unexpected plan apps: %#v", plan.Apps)
	}

	entries, err := parseMaclawAppInstallEntries(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {"id":"layout-tool","name":"Layout Tool","kind":"tool_app"}
	}`)
	if err != nil {
		t.Fatalf("parse tool app error = %v", err)
	}
	ui := anyMap(entries[0].App["ui"])
	if ui == nil || ui["schema"] != "maclaw.app.ui.v1" || ui["entry"] != "tool_workspace" {
		t.Fatalf("default tool workspace layout missing: %#v", entries[0].App["ui"])
	}
	layouts := anyMap(ui["layouts"])
	layout := anyMap(layouts["tool_workspace"])
	if layout == nil || layout["type"] != "tool_workspace" {
		t.Fatalf("default tool layout missing: %#v", ui)

		studioEntries, err := parseMaclawAppInstallEntries(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "studio-layout-tool",
			"name": "Studio Layout Tool",
			"kind": "tool_app",
			"binding": {
				"skill": {"id":"studio-layout-skill", "appDefinitionFile":"maclaw.apps.json"},
				"ui": {
					"schema": "maclaw.app.ui.v1",
					"entry": "tool_workspace",
					"generated": true,
					"layouts": {
						"tool_workspace": {
							"template": "classic_split",
							"density": "compact",
							"primaryRegion": "left",
							"outputRegion": "right",
							"regions": [
								{"id":"file_queue","role":"input","placement":"left"},
								{"id":"output_panel","role":"output","placement":"right"}
							],
							"studio": {"savedInManifest": true}
						}
					}
				}
			}
		}
	}`)
		if err != nil {
			t.Fatalf("parse studio binding ui app error = %v", err)
		}
		studioUI := anyMap(studioEntries[0].App["ui"])
		studioBinding := anyMap(studioEntries[0].App["binding"])
		studioBindingUI := anyMap(studioBinding["ui"])
		if studioUI == nil || studioUI["schema"] != "maclaw.app.ui.v1" || studioUI["entry"] != "tool_workspace" {
			t.Fatalf("studio binding ui should be promoted to app ui: %#v", studioEntries[0].App)
		}
		studioLayouts := anyMap(studioUI["layouts"])
		studioLayout := anyMap(studioLayouts["tool_workspace"])
		if studioLayout == nil || studioLayout["template"] != "classic_split" || studioLayout["density"] != "compact" || studioLayout["primaryRegion"] != "left" || studioLayout["outputRegion"] != "right" {
			t.Fatalf("promoted studio ui should preserve adjusted layout: %#v", studioUI)
		}
		if regions := anySlice(studioLayout["regions"]); len(regions) != 2 {
			t.Fatalf("promoted studio ui should preserve region placement: %#v", studioLayout["regions"])
		}
		if studioBindingUI == nil || anyMap(anyMap(studioBindingUI["layouts"])["tool_workspace"])["template"] != "classic_split" {
			t.Fatalf("original binding ui should remain readable for install evidence: %#v", studioBinding)
		}

	}

}

func TestPlanMaclawAppInstallRejectsInvalidWorkspaceLayout(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "bad-layout",
			"name": "Bad Layout",
			"kind": "enterprise_normal_app",
			"ui": {"entry":"business_workspace","layouts":"not-an-object"}
		}
	}`)
	if err == nil || !strings.Contains(err.Error(), "ui.layouts") {
		t.Fatalf("expected ui.layouts validation error, got %v", err)
	}
}

func TestPlanMaclawAppInstallEnforcesAppKindContracts(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	cases := []struct {
		name    string
		pkg     string
		wantErr string
	}{
		{
			name: "approval app requires workflow binding",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "approval-without-workflow",
					"name": "Approval Without Workflow",
					"kind": "enterprise_approval_app",
					"binding": {"datasrv": {"domain": "finance", "datasetID": "finance.expenses", "objectRole": "expense_report"}}
				}
			}`,
			wantErr: "workflow_skill dependency is required for enterprise_approval_app",
		},
		{
			name: "normal app cannot declare approval bindings",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "normal-with-approval",
					"name": "Normal With Approval",
					"kind": "enterprise_normal_app",
					"binding": {
						"datasrv": {"domain": "crm", "datasetID": "crm.customers", "objectRole": "customer"},
						"mis": {"approvalBindings": [{"event": "crm.customer.changed", "workflowSkillId": "customer-approval"}]}
					}
				}
			}`,
			wantErr: "approvalBindings is only valid for enterprise_approval_app",
		},
		{
			name: "tool app cannot declare datasrv",
			pkg: `{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"installUnit": "skill",
				"app": {
					"id": "tool-with-datasrv",
					"name": "Tool With DataSrv",
					"kind": "tool_app",
					"binding": {
						"skill": {"id": "pdf-tool"},
						"datasrv": {"domain": "finance", "datasetID": "finance.expenses"}
					}
				}
			}`,
			wantErr: "binding.datasrv is not valid for tool_app",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := app.PlanMaclawAppInstall(tc.pkg)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected %q error, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestPlanMaclawAppInstallTreatsBindingSkillAsDependency(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "tool-binding-skill",
			"name": "Tool Binding Skill",
			"kind": "tool_app",
			"binding": { "skill": { "id": "doc-archive", "version": "1.0.0" } }
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "doc-archive")
	if dep == nil || dep.Kind != "runtime_skill" || !dep.Required || dep.Action != "install" || plan.HasBlockingDependency {
		t.Fatalf("binding.skill should be a required runtime dependency: %#v", dep)
	}
}

func TestPlanMaclawAppInstallScopesSharedDependencyRequirementPerApp(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [
			{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "required-app",
					"name": "Required App",
					"kind": "enterprise_normal_app",
					"dependencies": {"skills": [{"id":"shared-skill", "kind":"runtime_skill", "required":true, "source":"hub"}]}
				}
			},
			{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "optional-app",
					"name": "Optional App",
					"kind": "enterprise_normal_app",
					"dependencies": {"skills": [{"id":"shared-skill", "kind":"runtime_skill", "required":false, "source":"hub"}]}
				}
			}
		]
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	var requiredDep, optionalDep *maclawAppInstallPlanDependency
	for i := range plan.Dependencies {
		dep := &plan.Dependencies[i]
		if dep.ID != "shared-skill" {
			continue
		}
		if dep.Required {
			requiredDep = dep
		} else {
			optionalDep = dep
		}
	}
	if requiredDep == nil || len(requiredDep.AppIDs) != 1 || requiredDep.AppIDs[0] != "required-app" || requiredDep.Action != "install" {
		t.Fatalf("required app should retain its installable shared dependency: %#v", plan.Dependencies)
	}
	if optionalDep == nil || len(optionalDep.AppIDs) != 1 || optionalDep.AppIDs[0] != "optional-app" || optionalDep.Action != "optional_missing" || optionalDep.Required {
		t.Fatalf("optional app should retain optional shared dependency: %#v", plan.Dependencies)
	}
	if hasBlockingMaclawAppRequiredDependencyForApp(plan.Dependencies, "optional-app") {
		t.Fatalf("optional app should not be blocked by required app dependency: %#v", plan.Dependencies)
	}
	if hasBlockingMaclawAppRequiredDependencyForApp(plan.Dependencies, "required-app") {
		t.Fatalf("installable required app dependency should not be a hard blocker: %#v", plan.Dependencies)
	}
}

func TestPlanMaclawAppInstallHonorsBindingSkillSourcesAndSnakeCaseAppSkill(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan, err := app.PlanMaclawAppInstall(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "source-aware-app",
			"name": "Source Aware App",
			"kind": "enterprise_normal_app",
			"binding": {
				"skill": { "id": "doc-archive", "version": "1.0.0", "source": "market" },
				"app_skill": { "id": "source-aware-super-skill", "version": "2.0.0", "source": "local" }
			}
		}
	}`)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "doc-archive"); dep == nil || dep.Source != "skillmarket" || dep.Version != "1.0.0" || dep.Kind != "runtime_skill" || !dep.Required {
		t.Fatalf("binding.skill source/version should be preserved: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "source-aware-super-skill"); dep == nil || dep.Source != "local" || dep.Version != "2.0.0" || dep.Kind != "app_skill" || !dep.Required {
		t.Fatalf("binding.app_skill should be a source-aware app dependency: %#v", dep)
	}
	if maclawAppDependencySupportsHubCenterLookup(maclawAppInstallPlanDependency{ID: "source-aware-super-skill", Source: "local", InstallRefStatus: "not_required"}) {
		t.Fatalf("explicit local dependency should not be rewritten through HubCenter lookup")
	}
}

func TestRecordMaclawAppInstallRejectsGovernanceReviewErrors(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "skill",
		"app": {
			"id": "bad-layout-install",
			"name": "Bad Layout Install",
			"kind": "tool_app",
			"governance": {
				"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "regionCount":1, "regions":[{"id":"file_queue", "role":"input", "placement":"left"}]},
				"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content"]},
				"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1","fingerprint":"proto-layout","sampleInput":{"sample":true},"expectedOutput":{"content":"ok"},"requiredRoles":["tester"],"requiredScopes":["app.run"],"riskLevel":"low"}, "testProtocolFingerprint":"proto-layout", "runId":"run-layout", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"ok"}}
			}
		}
	}`

	// Governance review issues are non-blocking at install time (enforcement
	// moved to Hub publish/approval endpoint). Local install should succeed.
	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("governance review should not block local install, got error: %v", err)
	}
	if result == nil {
		t.Fatalf("successful local install should return non-nil result")
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("local install with governance warnings should still write audit records")
	}
}

func TestRecordMaclawAppInstallPersistsNewestInstallAudit(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-archive")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Doc archive\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-archive", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	pkg := `{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"installUnit": "skill",
			"app": {
				"id": "market-doc-archive",
				"name": "Doc Archive",
				"version": 7,
				"kind": "tool_app",
				"binding": {
					"skill": { "id": "doc-archive", "version": "1.2.3", "source": "hub" },
					"ui": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "generated":true, "layouts":{"tool_workspace":{"template":"document_workspace", "density":"compact", "regions":[{"id":"input", "role":"input", "placement":"left"}, {"id":"status", "role":"status", "placement":"right"}, {"id":"output", "role":"output", "placement":"right"}]}}},
					"testProtocol": {"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-doc-archive", "sampleInput":{"file":"demo.pdf"}, "expectedOutput":{"content":"archived"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}
				},
				"governance": {
					"workspaceLayout": {"schema":"maclaw.app.ui.v1", "entry":"tool_workspace", "template":"document_workspace", "density":"compact", "regionCount":3},
					"resultContract": {"schema":"maclaw.app.result.v1", "primary":"content", "types":["content", "document"]},
					"dependencyVerification": {"schema":"maclaw.app.install_plan.v1", "verifiedAt":"2026-06-17T00:59:00Z", "dependencyCount":1, "hasMissingRequired":false, "hasBlockingDependency":false, "dependencies":[{"id":"doc-archive", "installed":true, "health":"ready"}]},
					"testEvidence": {"testProtocol":{"schema":"maclaw.app.test_protocol.v1", "fingerprint":"proto-doc-archive", "sampleInput":{"file":"demo.pdf"}, "expectedOutput":{"content":"archived"}, "requiredRoles":["tester"], "requiredScopes":["app.run"], "riskLevel":"low"}, "testProtocolFingerprint":"proto-doc-archive", "runId":"run-doc-archive", "verifiedAt":"2026-06-17T01:00:00Z", "resultPayload":{"content":"archived"}}
				}
			}
			}`
	pkg = maclawAppPackageWithCurrentDefinitionHashes(t, pkg)
	result, err := app.RecordMaclawAppInstall(pkg, "market")
	if err != nil {
		t.Fatalf("RecordMaclawAppInstall() error = %v", err)
	}
	if result["app_count"] != 1 || result["package_sha"] == "" || result["package_sha256"] != result["package_sha"] || result["has_missing_required"] != false || result["has_blocking_dependency"] != false {
		t.Fatalf("unexpected record result: %#v", result)
	}
	if deps, ok := result["dependencies"].([]maclawAppInstallPlanDependency); !ok || len(deps) != 1 || !deps[0].Installed {
		t.Fatalf("install result should expose dependency state: %#v", result["dependencies"])
	}
	installEvidence, ok := result["install_evidence"].(map[string]any)
	if !ok {
		t.Fatalf("install result should expose per-app evidence: %#v", result["install_evidence"])
	}
	docEvidence, ok := installEvidence["market-doc-archive"].(map[string]interface{})
	if !ok {
		t.Fatalf("install evidence should include app id: %#v", installEvidence)
	}
	if workspace := anyMap(docEvidence["workspace_layout"]); maclawAppStringValue(workspace, "entry") != "tool_workspace" || maclawAppStringValue(workspace, "template") != "document_workspace" {
		t.Fatalf("install evidence missing workspace layout: %#v", docEvidence["workspace_layout"])
	}
	if resultContract := anyMap(docEvidence["result_contract"]); maclawAppStringValue(resultContract, "primary") != "content" {
		t.Fatalf("install evidence missing result contract: %#v", docEvidence["result_contract"])
	}
	if testEvidence := anyMap(docEvidence["test_evidence"]); maclawAppStringValue(testEvidence, "runId", "run_id") != "run-doc-archive" || maclawAppStringValue(testEvidence, "testProtocolFingerprint", "test_protocol_fingerprint") != "proto-doc-archive" {
		t.Fatalf("install evidence missing test evidence: %#v", docEvidence["test_evidence"])
	}
	if verification := anyMap(docEvidence["dependency_verification"]); maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" {
		t.Fatalf("install evidence missing dependency verification: %#v", docEvidence["dependency_verification"])
	}
	appVersions, ok := result["app_versions"].(map[string]maclawAppInstallVersionSnapshot)
	if !ok || appVersions["market-doc-archive"].AppEntryVersion != "7" || appVersions["market-doc-archive"].AppSkill == nil || appVersions["market-doc-archive"].AppSkill.Version != "1.2.3" {
		t.Fatalf("install result should expose app version snapshot: %#v", result["app_versions"])
	}
	records, err := app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls() error = %v", err)
	}
	if len(records) != 1 || records[0].AppID != "market-doc-archive" || records[0].Source != "market" {
		t.Fatalf("unexpected install records: %#v", records)
	}
	if records[0].HasMissingRequired || len(records[0].Dependencies) != 1 || !records[0].Dependencies[0].Installed {
		t.Fatalf("expected installed dependency snapshot: %#v", records[0])
	}
	if verification := anyMap(records[0].DependencyVerification); maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" || verification["dependencyCount"] != float64(1) {
		t.Fatalf("expected install record dependency verification snapshot: %#v", records[0].DependencyVerification)
	}
	if records[0].VersionSnapshot.AppEntryVersion != "7" || records[0].VersionSnapshot.AppSkill == nil || records[0].VersionSnapshot.AppSkill.ID != "doc-archive" || records[0].VersionSnapshot.AppSkill.Version != "1.2.3" {
		t.Fatalf("expected install record version snapshot: %#v", records[0].VersionSnapshot)
	}
	if records[0].Package == nil || stringMapValue(records[0].Package, "schema") != "maclaw.app.v1" {
		t.Fatalf("expected install record package snapshot: %#v", records[0].Package)
	}
	if maclawAppStringValue(records[0].WorkspaceLayout, "entry") != "tool_workspace" || maclawAppStringValue(records[0].ResultContract, "primary") != "content" || maclawAppStringValue(records[0].TestEvidence, "runId", "run_id") != "run-doc-archive" {
		t.Fatalf("expected install record evidence snapshots: workspace=%#v result=%#v test=%#v", records[0].WorkspaceLayout, records[0].ResultContract, records[0].TestEvidence)
	}
	packageJSON, err := json.Marshal(records[0].Package)
	if err != nil {
		t.Fatalf("marshal install record package: %v", err)
	}
	recheck, err := app.PlanMaclawAppInstall(string(packageJSON))
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall from install record package error = %v", err)
	}
	if len(recheck.Apps) != 1 || recheck.Apps[0].ID != "market-doc-archive" || recheck.HasMissingRequired || recheck.HasBlockingDependency {
		t.Fatalf("install record package should recheck dependency health: %#v", recheck)
	}
	if _, err := app.RecordMaclawAppInstall(pkg, "market"); err != nil {
		t.Fatalf("RecordMaclawAppInstall second call error = %v", err)
	}
	records, err = app.ListMaclawAppInstalls(10)
	if err != nil {
		t.Fatalf("ListMaclawAppInstalls after upsert error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("install records should upsert by app id: %#v", records)
	}
}

func TestMaclawAppDataSrvInstallationPayloadsScopeDependenciesPerApp(t *testing.T) {
	entries := []parsedMaclawAppEntry{
		{
			ID:   "selected-app",
			Name: "Selected App",
			Kind: "enterprise_normal_app",
			Entry: map[string]any{
				"schema": "maclaw.app.v1",
			},
			App: map[string]any{
				"id":   "selected-app",
				"name": "Selected App",
				"kind": "enterprise_normal_app",
				"binding": map[string]any{
					"datasrv": map[string]any{"domain": "finance", "datasetID": "finance.selected", "objectRole": "selected_record"},
					"ui": map[string]any{
						"schema":    "maclaw.app.ui.v1",
						"generated": true,
						"entry":     "business_workspace",
						"layouts": map[string]any{
							"business_workspace": map[string]any{
								"template":      "dashboard",
								"density":       "compact",
								"primaryRegion": "center",
								"outputRegion":  "bottom",
								"regions": []any{
									map[string]any{"id": "operation_form", "role": "input", "placement": "center", "order": 1},
									map[string]any{"id": "record_list", "role": "record_list", "placement": "left", "order": 2},
									map[string]any{"id": "output_panel", "role": "output", "placement": "bottom", "order": 3},
									map[string]any{"id": "record_detail", "role": "detail", "placement": "right", "visible": false, "order": 4},
								},
								"fingerprint": "layout-selected",
								"studio": map[string]any{
									"editable":        true,
									"savedInManifest": true,
									"updatedBy":       "app_studio",
								},
							},
						},
					},
				},
				"governance": map[string]any{
					"workspaceLayout": map[string]any{
						"entry":              "business_workspace",
						"template":           "dashboard",
						"density":            "compact",
						"primaryRegion":      "center",
						"outputRegion":       "bottom",
						"fingerprint":        "layout-selected",
						"savedInManifest":    true,
						"regionIds":          []any{"operation_form", "record_list", "output_panel", "record_detail"},
						"visibleRegionCount": 3,
					},
					"testEvidence": map[string]any{
						"runId": "run-selected-1",
						"resultPayload": map[string]any{
							"status":     "completed",
							"resultType": "content",
							"content":    "selected app ready",
						},
						"outputs": []any{
							map[string]any{"kind": "content", "text": "selected app ready"},
							map[string]any{"kind": "document", "title": "Selected export"},
						},
						"artifacts": []any{
							map[string]any{"name": "selected-export.pdf", "uri": "artifact://selected/export.pdf", "type": "document"},
						},
					},
					"dependencyVerification": map[string]any{
						"schema":                "maclaw.app.install_plan.v1",
						"verifiedAt":            "2026-07-01T03:00:00Z",
						"dependencyCount":       1,
						"hasMissingRequired":    false,
						"hasBlockingDependency": false,
						"dependencies": []any{
							map[string]any{"id": "selected-skill", "kind": "runtime_skill", "source": "hub", "required": true, "installed": true, "health": "ready"},
						},
					},
				},
			},
		},
		{
			ID:   "other-app",
			Name: "Other App",
			Kind: "enterprise_normal_app",
			Entry: map[string]any{
				"schema": "maclaw.app.v1",
			},
			App: map[string]any{
				"id":   "other-app",
				"name": "Other App",
				"kind": "enterprise_normal_app",
				"binding": map[string]any{
					"datasrv": map[string]any{"domain": "finance", "datasetID": "finance.other", "objectRole": "other_record"},
				},
			},
		},
	}
	dependencies := []maclawAppInstallPlanDependency{
		{ID: "selected-skill", Kind: "runtime_skill", Required: true, Source: "hub", InstallRef: "hub://skills/selected-skill@1.2.0", InstallRefKind: "hub", InstallRefTarget: "selected-skill", InstallRefVersion: "1.2.0", InstallRefStatus: "ok", PreflightStatus: "ready", PreflightCode: "skillhub_target_ready", PreflightStage: "skillhub_preflight", PackageSHA256: "sha-selected-skill", PackageSignature: "sig-selected-skill", PackageDownloadURL: "https://hub.example/skills/selected-skill/download", IntegrityStatus: "ready", IntegrityCode: "package_integrity_metadata_ready", IntegrityStage: "skillhub_preflight", AppIDs: []string{"market-selected-app"}, Installed: true, Health: "ready", Action: "skip"},
		{ID: "other-skill", Kind: "runtime_skill", Required: true, AppIDs: []string{"other-app"}, Installed: false, Health: "missing", Action: "blocked"},
	}

	payloads := maclawAppDataSrvInstallationPayloads(entries, "market", "sha-selected", 4096, dependencies)
	if len(payloads) != 2 {
		t.Fatalf("expected one DataSrv payload per role-bound app, got %#v", payloads)
	}
	payloadByAppID := map[string]maclawAppDataSrvInstallationPayload{}
	for _, payload := range payloads {
		payloadByAppID[payload.AppID] = payload
	}
	selectedPayload, ok := payloadByAppID["selected-app"]
	if !ok {
		t.Fatalf("missing selected app payload: %#v", payloads)
	}
	selectedMetadata, ok := selectedPayload.Body["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("selected app payload missing metadata: %#v", selectedPayload.Body)
	}
	selectedVerification, ok := selectedMetadata["dependency_verification"].(map[string]interface{})
	if !ok || selectedVerification["dependency_count"] != 1 || selectedVerification["has_missing_required"] != false || selectedVerification["has_blocking_dependency"] != false {
		t.Fatalf("selected app dependency verification should be scoped and ready: %#v", selectedMetadata["dependency_verification"])
	}
	if _, err := time.Parse(time.RFC3339, maclawAppStringValue(selectedVerification, "verified_at")); err != nil {
		t.Fatalf("selected app dependency verification should include verified_at: %#v", selectedVerification)
	}
	if selectedMetadata["test_evidence_dependency_verified_at"] != selectedVerification["verified_at"] {
		t.Fatalf("selected app metadata should mirror dependency verification time: %#v", selectedMetadata)
	}
	if selectedMetadata["test_evidence_result_type"] != "content" || selectedMetadata["test_evidence_output_type"] != "content" || selectedMetadata["test_evidence_result_content"] != "selected app ready" {
		t.Fatalf("selected app metadata should carry result evidence summaries: %#v", selectedMetadata)
	}
	if kinds := maclawAppStringListFromAny(selectedMetadata["test_evidence_output_kinds"]); len(kinds) != 2 || kinds[0] != "content" || kinds[1] != "document" {
		t.Fatalf("selected app metadata should carry output kind summaries: %#v", selectedMetadata)
	}
	if selectedMetadata["test_evidence_artifact_uri"] != "artifact://selected/export.pdf" || selectedMetadata["test_evidence_artifact_name"] != "selected-export.pdf" {
		t.Fatalf("selected app metadata should carry primary artifact summaries: %#v", selectedMetadata)
	}
	if uris := maclawAppStringListFromAny(selectedMetadata["test_evidence_artifact_uris"]); len(uris) != 1 || uris[0] != "artifact://selected/export.pdf" {
		t.Fatalf("selected app metadata should carry artifact URI summaries: %#v", selectedMetadata)
	}
	selectedWorkspace := anyMap(selectedMetadata["workspace_layout"])
	if selectedWorkspace == nil || maclawAppStringValue(selectedWorkspace, "entry") != "business_workspace" || maclawAppStringValue(selectedWorkspace, "template") != "dashboard" || maclawAppStringValue(selectedWorkspace, "density") != "compact" {
		t.Fatalf("selected app metadata should carry workspace layout: %#v", selectedMetadata["workspace_layout"])
	}
	if selectedMetadata["workspace_layout_primary_region"] != "center" || selectedMetadata["workspace_layout_output_region"] != "bottom" || selectedMetadata["workspace_layout_fingerprint"] != "layout-selected" {
		t.Fatalf("selected app metadata should flatten workspace layout routing: %#v", selectedMetadata)
	}
	if selectedMetadata["workspace_layout_region_count"] != 4 || selectedMetadata["workspace_layout_visible_region_count"] != 3 {
		t.Fatalf("selected app metadata should flatten workspace layout region counts: %#v", selectedMetadata)
	}
	if regionIDs := maclawAppStringListFromAny(selectedMetadata["workspace_layout_region_ids"]); len(regionIDs) != 4 || regionIDs[0] != "operation_form" || regionIDs[2] != "output_panel" {
		t.Fatalf("selected app metadata should flatten workspace layout region ids: %#v", selectedMetadata["workspace_layout_region_ids"])
	}
	selectedInstallEvidence := anyMap(selectedMetadata["install_evidence"])
	if selectedInstallEvidence == nil {
		t.Fatalf("selected app metadata should carry install evidence: %#v", selectedMetadata)
	}
	installWorkspace := anyMap(selectedInstallEvidence["workspace_layout"])
	if installWorkspace == nil || maclawAppStringValue(installWorkspace, "fingerprint") != "layout-selected" || maclawAppStringValue(installWorkspace, "outputRegion", "output_region") != "bottom" {
		t.Fatalf("selected app install evidence should carry the same workspace layout: %#v", selectedInstallEvidence["workspace_layout"])
	}
	installTestEvidence := anyMap(selectedInstallEvidence["test_evidence"])
	if installTestEvidence == nil || maclawAppStringValue(installTestEvidence, "runId", "run_id") != "run-selected-1" {
		t.Fatalf("selected app install evidence should carry test evidence: %#v", selectedInstallEvidence["test_evidence"])
	}
	installVerification := anyMap(selectedInstallEvidence["dependency_verification"])
	if installVerification == nil || installVerification["dependency_count"] != 1 || installVerification["has_blocking_dependency"] != false {
		t.Fatalf("selected app install evidence should carry scoped dependency verification: %#v", selectedInstallEvidence["dependency_verification"])
	}
	selectedVerificationDependencies := anySlice(selectedVerification["dependencies"])
	if len(selectedVerificationDependencies) != 1 {
		t.Fatalf("selected app verification dependencies should be scoped: %#v", selectedVerification)
	}
	selectedVerificationDependency := anyMap(selectedVerificationDependencies[0])
	if maclawAppStringValue(selectedVerificationDependency, "id") != "selected-skill" || maclawAppStringValue(selectedVerificationDependency, "health") != "ready" {
		t.Fatalf("selected app verification should not include other app dependency: %#v", selectedVerificationDependencies)
	}
	if maclawAppStringValue(selectedVerificationDependency, "install_ref_kind") != "hub" ||
		maclawAppStringValue(selectedVerificationDependency, "install_ref_target") != "selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "install_ref_version") != "1.2.0" ||
		maclawAppStringValue(selectedVerificationDependency, "preflight_status") != "ready" ||
		maclawAppStringValue(selectedVerificationDependency, "preflight_code") != "skillhub_target_ready" ||
		maclawAppStringValue(selectedVerificationDependency, "package_sha256") != "sha-selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "package_signature") != "sig-selected-skill" ||
		maclawAppStringValue(selectedVerificationDependency, "integrity_status") != "ready" ||
		maclawAppStringValue(selectedVerificationDependency, "integrity_code") != "package_integrity_metadata_ready" {
		t.Fatalf("selected app verification should preserve dependency preflight and integrity diagnostics: %#v", selectedVerificationDependency)
	}
	installTrace := anyMap(selectedVerification["install_trace"])
	if installTrace == nil ||
		installTrace["schema"] != "maclaw.app.dependency_install_trace.v1" ||
		installTrace["dependency_count"] != 1 ||
		installTrace["preflight_checked_count"] != 1 ||
		installTrace["preflight_ready_count"] != 1 ||
		installTrace["integrity_checked_count"] != 1 ||
		installTrace["integrity_ready_count"] != 1 ||
		installTrace["download_available_count"] != 1 ||
		installTrace["signature_available_count"] != 1 ||
		installTrace["install_error_count"] != 0 ||
		installTrace["ok"] != true {
		t.Fatalf("selected app verification should expose dependency install trace summary: %#v", installTrace)
	}
	if selectedMetadata["dependency_preflight_checked_count"] != 1 ||
		selectedMetadata["dependency_preflight_ready_count"] != 1 ||
		selectedMetadata["dependency_integrity_checked_count"] != 1 ||
		selectedMetadata["dependency_integrity_ready_count"] != 1 ||
		selectedMetadata["dependency_download_available_count"] != 1 ||
		selectedMetadata["dependency_signature_available_count"] != 1 ||
		selectedMetadata["dependency_install_error_count"] != 0 ||
		selectedMetadata["dependency_install_trace_ok"] != true {
		t.Fatalf("selected app metadata should flatten dependency install trace summary: %#v", selectedMetadata)
	}
	installVerificationDependencies := anySlice(installVerification["dependencies"])
	if len(installVerificationDependencies) != 1 {
		t.Fatalf("selected app install evidence verification dependencies should be scoped: %#v", installVerification)
	}
	installVerificationDependency := anyMap(installVerificationDependencies[0])
	if maclawAppStringValue(installVerificationDependency, "package_download_url") != "https://hub.example/skills/selected-skill/download" ||
		maclawAppStringValue(installVerificationDependency, "integrity_stage") != "skillhub_preflight" {
		t.Fatalf("selected app install evidence should preserve dependency download and integrity diagnostics: %#v", installVerificationDependency)
	}
	selectedDependencies, ok := selectedMetadata["dependencies"].([]maclawAppInstallPlanDependency)
	if !ok || len(selectedDependencies) != 1 || selectedMetadata["dependency_count"] != 1 || selectedMetadata["has_missing_required_dependency"] != false || selectedMetadata["has_blocking_dependency"] != false {
		t.Fatalf("selected app metadata dependencies should be scoped and ready: %#v", selectedMetadata)
	}
	if selectedDependencies[0].ID != "selected-skill" {
		t.Fatalf("selected app metadata should only include selected dependency: %#v", selectedDependencies)
	}
	otherPayload, ok := payloadByAppID["other-app"]
	if !ok {
		t.Fatalf("missing other app payload: %#v", payloads)
	}
	otherMetadata, ok := otherPayload.Body["metadata"].(map[string]interface{})
	if !ok || otherMetadata["has_blocking_dependency"] != true {
		t.Fatalf("other app payload should retain its own blocking dependency: %#v", otherPayload.Body)
	}
}

func TestPlanMaclawAppInstallRejectsUnknownSchema(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.PlanMaclawAppInstall(`{"schema":"unknown","privateMarker":"x_maclaw_apps"}`); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("expected schema error, got %v", err)
	}
}

func TestInstallMaclawAppDependenciesPreservesInstallRefFromDuplicateDependency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	type installCall struct {
		source     string
		id         string
		installRef string
	}
	var calls []installCall
	app.maclawAppInstallMixedSkill = func(source, id, installRef string) error {
		calls = append(calls, installCall{source: source, id: id, installRef: installRef})
		skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", id)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# "+id+"\n"), 0o644); err != nil {
			return err
		}
		cfg, err := app.LoadConfig()
		if err != nil {
			return err
		}
		cfg.NLSkills = append(cfg.NLSkills, corelib.NLSkillEntry{Name: id, SkillDir: skillDir, Status: "active", Source: source, HubSkillID: id})
		return app.SaveConfig(cfg)
	}

	plan, err := app.InstallMaclawAppDependencies(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "duplicate-install-ref-app",
			"name": "Duplicate Install Ref App",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": { "id": "expense-app-skill" }
			},
			"dependencies": {
				"skills": [
					{ "id": "expense-app-skill", "kind": "app_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-expense-app-skill" },
					{ "id": "expense-approval-workflow", "kind": "workflow_skill", "required": true, "source": "enterprise_hub", "capability_id": "cap-expense-workflow" }
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("InstallMaclawAppDependencies() error = %v", err)
	}
	dep := maclawAppPlanDepForTest(plan, "expense-app-skill")
	if dep == nil {
		t.Fatalf("missing app skill dependency: %#v", plan.Dependencies)
	}
	if dep.Source != "enterprise_hub" || dep.InstallRef != "cap-expense-app-skill" || dep.Action != "installed" {
		t.Fatalf("duplicate dependency should preserve precise install ref, got %#v", dep)
	}
	if len(calls) != 2 {
		t.Fatalf("expected app skill and workflow installs, got %#v", calls)
	}
	if calls[0] != (installCall{source: "enterprise_hub", id: "expense-app-skill", installRef: "cap-expense-app-skill"}) {
		t.Fatalf("app skill install should use precise install ref, got %#v", calls)
	}
}

func TestSelectedMaclawAppPackageFiltersResolvedDependencies(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	pkg := map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": []any{
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":   "expense-app",
					"name": "Expense App",
					"kind": "enterprise_approval_app",
					"dependencies": map[string]any{"skills": []any{
						map[string]any{"id": "expense-workflow", "kind": "workflow_skill", "required": true, "source": "local"},
					}},
				},
			},
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":   "contract-app",
					"name": "Contract App",
					"kind": "enterprise_approval_app",
					"dependencies": map[string]any{"skills": []any{
						map[string]any{"id": "contract-workflow", "kind": "workflow_skill", "required": true, "source": "local"},
					}},
				},
			},
		},
		"resolved_dependencies": []any{
			map[string]any{"id": "expense-workflow", "install_ref": "hub-expense-workflow", "source": "hub", "required": true, "app_ids": []any{"expense-app"}},
			map[string]any{"id": "contract-workflow", "install_ref": "hub-contract-workflow", "source": "hub", "required": true, "app_ids": []any{"contract-app"}},
		},
	}

	for _, raw := range anySlice(pkg["apps"]) {
		if appEntry := anyMap(raw); appEntry != nil {
			appEntry["resolved_dependencies"] = pkg["resolved_dependencies"]
		}
	}

	selected, entries, err := maclawAppPackageForSelectedAppIDs(pkg, []string{"expense-app"})
	if err != nil {
		t.Fatalf("maclawAppPackageForSelectedAppIDs() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "expense-app" {
		t.Fatalf("expected only selected app entry, got %#v", entries)
	}
	resolved := anySlice(selected["resolved_dependencies"])
	if len(resolved) != 1 || stringMapValue(anyMap(resolved[0]), "id") != "expense-workflow" {
		t.Fatalf("selected package should keep only selected app resolved dependency: %#v", selected["resolved_dependencies"])
	}
	selectedApps := anySlice(selected["apps"])
	if len(selectedApps) != 1 {
		t.Fatalf("selected package should keep one app entry, got %#v", selectedApps)
	}
	entryResolved := anySlice(anyMap(selectedApps[0])["resolved_dependencies"])
	if len(entryResolved) != 1 || stringMapValue(anyMap(entryResolved[0]), "id") != "expense-workflow" {
		t.Fatalf("selected app entry should keep only selected resolved dependency: %#v", entryResolved)
	}
	if stringMapValue(anyMap(entryResolved[0]), "install_ref") != "hub-expense-workflow" {
		t.Fatalf("selected app entry should preserve selected install ref: %#v", entryResolved[0])
	}
	packageJSON, err := maclawAppStableJSON(selected)
	if err != nil {
		t.Fatalf("maclawAppStableJSON() error = %v", err)
	}
	plan, err := app.PlanMaclawAppInstall(packageJSON)
	if err != nil {
		t.Fatalf("PlanMaclawAppInstall() error = %v", err)
	}
	if dep := maclawAppPlanDepForTest(plan, "expense-workflow"); dep == nil || dep.Source != "hub" || dep.InstallRef != "hub-expense-workflow" || len(dep.AppIDs) != 1 || dep.AppIDs[0] != "expense-app" {
		t.Fatalf("selected plan should use selected app install ref only: %#v", dep)
	}
	if dep := maclawAppPlanDepForTest(plan, "contract-workflow"); dep != nil {
		t.Fatalf("unselected app dependency should not appear in selected plan: %#v", dep)
	}
}

func TestSelectedMaclawAppPackageFiltersBundledDependencies(t *testing.T) {
	pkg := map[string]any{
		"schema": "maclaw.app.pack.v1", "privateMarker": "x_maclaw_apps",
		"bundled_dependencies": map[string]any{"skills": []any{
			map[string]any{"name": "shared", "sha256": "shared", "files": map[string]any{"skill.md": "c2hhcmVk"}},
			map[string]any{"name": "expense-only", "sha256": "expense", "app_ids": []any{"expense-app"}, "files": map[string]any{"skill.md": "ZXhwZW5zZQ=="}},
			map[string]any{"name": "contract-only", "sha256": "contract", "app_ids": []any{"contract-app"}, "files": map[string]any{"skill.md": "Y29udHJhY3Q="}},
		}},
		"apps": []any{
			map[string]any{"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps", "app": map[string]any{"id": "expense-app", "name": "Expense", "kind": "tool_app"}},
			map[string]any{"schema": "maclaw.app.v1", "privateMarker": "x_maclaw_apps", "app": map[string]any{"id": "contract-app", "name": "Contract", "kind": "tool_app"}},
		},
	}
	selected, entries, err := maclawAppPackageForSelectedAppIDs(pkg, []string{"expense-app"})
	if err != nil {
		t.Fatalf("maclawAppPackageForSelectedAppIDs() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "expense-app" {
		t.Fatalf("unexpected selected entries: %#v", entries)
	}
	bundled := maclawAppBundledDependenciesFromDoc(selected)
	if len(bundled.Skills) != 2 {
		t.Fatalf("selected package should retain shared + selected bundle only: %#v", bundled.Skills)
	}
	for _, skill := range bundled.Skills {
		if skill.Name == "contract-only" {
			t.Fatalf("unselected app bundle leaked into selected package: %#v", bundled.Skills)
		}
		if skill.Name == "expense-only" && (len(skill.AppIDs) != 1 || skill.AppIDs[0] != "expense-app") {
			t.Fatalf("selected scoped bundle should retain only selected app id: %#v", skill)
		}
	}
}

func TestMaclawAppSerializableResolvedDepsIncludesAppIDs(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{{ID: "expense-workflow", Kind: "workflow_skill", Required: true, Source: "hub", InstallRef: "hub-expense-workflow", AppIDs: []string{"expense-app"}}}
	resolved := maclawAppSerializableResolvedDeps(deps)
	if len(resolved) != 1 {
		t.Fatalf("expected one resolved dependency, got %#v", resolved)
	}
	appIDs := maclawAppStringListFromAny(resolved[0]["app_ids"])
	if len(appIDs) != 1 || appIDs[0] != "expense-app" {
		t.Fatalf("resolved dependency should carry app_ids for selected installs: %#v", resolved[0])
	}
}
