package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

func TestListSkillAppManifestsReadsPrivateExtension(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "redact",
			"name": "Document Redaction",
			"description": "Upload a document and return a redacted copy.",
			"category": "Document",
			"icon": "shield",
			"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo=",
			"input_mode": "file",
			"multiple_files": true,
			"output_modes": ["docx", "pdf"]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	if items[0].ID != "redact" || items[0].SkillID != "doc-tools" || items[0].Name != "Document Redaction" || items[0].InputMode != "file" || items[0].CustomIconDataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected manifest item: %#v", items[0])
	}
	if !items[0].MultipleFiles {
		t.Fatalf("expected multiple_files=true: %#v", items[0])
	}
	if len(items[0].OutputModes) != 2 || items[0].OutputModes[0] != "docx" || items[0].OutputModes[1] != "pdf" {
		t.Fatalf("unexpected output modes: %#v", items[0].OutputModes)
	}
}

func TestListNLSkillsMarksMaclawAppSkills(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review"
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.app.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.ensureRemoteInfra()

	defs := app.ListNLSkills()
	if len(defs) != 1 {
		t.Fatalf("defs len=%d want 1: %#v", len(defs), defs)
	}
	if !defs[0].IsMaclawApp || defs[0].MaclawAppCount != 1 || defs[0].MaclawAppEntry != "maclaw.app.json" {
		t.Fatalf("unexpected maclaw app metadata: %#v", defs[0])
	}
}

func TestListSkillAppManifestsReadsSingleMaclawAppDefinition(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: invoice-app\nmaclaw_app:\n  entry: maclaw.app.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review",
			"description": "Review invoice files.",
			"kind": "tool_app",
			"category": "Finance",
			"icon": "receipt",
			"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo=",
			"binding": {
				"skill": {
					"id": "invoice-app",
					"inputMode": "mixed",
					"multipleFiles": true,
					"outputModes": ["pdf", "json"],
					"fields": [
						{ "name": "strict", "type": "boolean", "default": true }
					]
				}
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.app.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	got := items[0]
	if got.ID != "invoice-review" || got.SkillID != "invoice-app" || got.Name != "Invoice Review" || got.Category != "Finance" || got.Icon != "receipt" || got.CustomIconDataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("unexpected app definition: %#v", got)
	}
	if got.InputMode != "mixed" || !got.MultipleFiles || len(got.OutputModes) != 2 || got.OutputModes[0] != "pdf" || got.OutputModes[1] != "json" {
		t.Fatalf("unexpected skill binding: %#v", got)
	}
	if got.AppDefinitionFile != "maclaw.app.json" {
		t.Fatalf("AppDefinitionFile = %q, want maclaw.app.json", got.AppDefinitionFile)
	}
	if len(got.Fields) != 1 || got.Fields[0].Name != "strict" || got.Fields[0].Type != "boolean" || got.Fields[0].Default != true {
		t.Fatalf("unexpected fields: %#v", got.Fields)
	}
}

func TestListSkillAppManifestsHonorsAddToAppPanelFalse(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: invoice-app\nmaclaw_app:\n  entry: maclaw.app.json\n  add_to_app_panel: false\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review",
			"binding": { "skill": { "id": "invoice-app" } }
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.app.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if items := app.ListSkillAppManifests(); len(items) != 0 {
		t.Fatalf("items len=%d want 0 when add_to_app_panel=false: %#v", len(items), items)
	}
	app.ensureRemoteInfra()
	defs := app.ListNLSkills()
	if len(defs) != 1 || !defs[0].IsMaclawApp || defs[0].MaclawAppCount != 1 {
		t.Fatalf("MaClaw App skill metadata should remain visible: %#v", defs)
	}
}

func TestSaveMaclawAppDefinitionForSkillWritesSingleAppFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Invoice app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.SaveMaclawAppDefinitionForSkill("invoice-app", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review"
		}
	}`)
	if err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}
	if result["app_definition_file"] != "maclaw.app.json" || result["app_id"] != "invoice-review" {
		t.Fatalf("unexpected save result: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.app.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.app.json: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal saved maclaw.app.json: %v", err)
	}
	appObj := saved["app"].(map[string]any)
	binding := appObj["binding"].(map[string]any)
	skillBinding := binding["skill"].(map[string]any)
	if appObj["kind"] != "tool_app" || appObj["launchMode"] != "fixed_skill_ui" || skillBinding["id"] != "invoice-app" {
		t.Fatalf("saved definition did not get normalized: %s", string(data))
	}
	if result["skill_yaml_updated"] != false {
		t.Fatalf("skill_yaml_updated = %#v, want false for skill.md-only package", result["skill_yaml_updated"])
	}
}

func TestSaveMaclawAppDefinitionForSkillWritesEnterpriseAppFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval super skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-super-skill", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.SaveMaclawAppDefinitionForSkill("expense-super-skill", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "enterprise_app_pack",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": {"version": "1.0.0", "source": "local"},
				"dependencies": {"skills": [{"id": "expense-workflow", "kind": "workflow_skill", "version": "2.0.0", "required": true, "source": "hub"}]},
				"ui": {
					"schema": "maclaw.app.ui.v1",
					"entry": "approval_workspace",
					"layouts": {
						"approval_workspace": {
							"template": "classic_split",
							"density": "compact",
								"primaryRegion": "center",
								"outputRegion": "bottom",
								"navigation": ["my_requests", "pending_my_approval", "attention"],
								"list": {"columns": ["title", "applicant", "current_node", "status"]},
								"studio": {"generated": true, "lastEditedBy": "app_studio"},
								"regions": [
									{"id": "request_form", "role": "input", "placement": "center", "locked": true},
									{"id": "approval_inbox", "role": "instance_list", "placement": "left"},
									{"id": "result_panel", "role": "output", "placement": "bottom", "width": 420}
								]
							}
						}
				},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"}]}
			},
			"governance": {
				"workflowContract": {"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"},
				"resultContract": {"schema": "maclaw.app.result.v1", "primary": "approval_result", "types": ["approval_result", "business_status", "document"]},
				"testEvidence": {"runId": "run-expense-approval", "definitionHash": "hash-expense-1"}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}
	if result["app_definition_file"] != "maclaw.app.json" || result["app_id"] != "expense-approval" {
		t.Fatalf("unexpected save result: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.app.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.app.json: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal saved maclaw.app.json: %v", err)
	}
	appObj := saved["app"].(map[string]any)
	if appObj["kind"] != "enterprise_approval_app" || appObj["launchMode"] != "agent_dynamic_ui" {
		t.Fatalf("saved enterprise app definition was not normalized: %s", string(data))
	}
	binding := appObj["binding"].(map[string]any)
	appSkill := binding["appSkill"].(map[string]any)
	if appSkill["id"] != "expense-super-skill" || appSkill["version"] != "1.0.0" {
		t.Fatalf("enterprise app should bind the target super skill: %#v", appSkill)
	}
	dependencies := binding["dependencies"].(map[string]any)
	skills := dependencies["skills"].([]any)
	if len(skills) != 1 || skills[0].(map[string]any)["id"] != "expense-workflow" {
		t.Fatalf("enterprise app should preserve workflow skill dependency: %#v", dependencies)
	}
	ui := binding["ui"].(map[string]any)
	layout := ui["layouts"].(map[string]any)["approval_workspace"].(map[string]any)
	regions := layout["regions"].([]any)
	if layout["primaryRegion"] != "center" || layout["outputRegion"] != "bottom" || len(regions) != 3 {
		t.Fatalf("enterprise app should preserve dynamic workspace layout: %#v", layout)
	}
	navigation := layout["navigation"].([]any)
	if len(navigation) != 3 || navigation[0] != "my_requests" || navigation[1] != "pending_my_approval" || navigation[2] != "attention" {
		t.Fatalf("enterprise app should preserve workspace navigation: %#v", layout)
	}
	list := layout["list"].(map[string]any)
	columns := list["columns"].([]any)
	if len(columns) != 4 || columns[2] != "current_node" {
		t.Fatalf("enterprise app should preserve workspace list columns: %#v", layout)
	}
	studio := layout["studio"].(map[string]any)
	if studio["generated"] != true || studio["lastEditedBy"] != "app_studio" {
		t.Fatalf("enterprise app should preserve App Studio layout metadata: %#v", layout)
	}
	firstRegion := regions[0].(map[string]any)
	thirdRegion := regions[2].(map[string]any)
	if firstRegion["locked"] != true || thirdRegion["width"] != float64(420) {
		t.Fatalf("enterprise app should preserve user-adjusted region metadata: %#v", regions)
	}
	mis := binding["mis"].(map[string]any)
	approvalBindings := mis["approvalBindings"].([]any)
	if len(approvalBindings) != 1 || approvalBindings[0].(map[string]any)["workflowSkillId"] != "expense-workflow" {
		t.Fatalf("enterprise app should preserve approval workflow binding: %#v", mis)
	}
	governance := appObj["governance"].(map[string]any)
	if governance["workflowContract"].(map[string]any)["workflowSkillId"] != "expense-workflow" || governance["testEvidence"].(map[string]any)["definitionHash"] != "hash-expense-1" {
		t.Fatalf("enterprise app should preserve governance contracts and evidence: %#v", governance)
	}
}

func TestSaveMaclawAppDefinitionForSkillRejectsEnterpriseAppsManifestFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval super skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-super-skill", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err = app.SaveMaclawAppDefinitionForSkill("expense-super-skill", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"skill": {"id": "expense-super-skill", "appDefinitionFile": "maclaw.apps.json"},
				"appSkill": {"id": "expense-super-skill", "version": "1.0.0"}
			}
		}
	}`)
	if err == nil || !strings.Contains(err.Error(), "enterprise MaClaw App definitions must be saved as maclaw.app.json") {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v, want enterprise maclaw.app.json rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(skillDir, "maclaw.apps.json")); !os.IsNotExist(statErr) {
		t.Fatalf("enterprise app must not be written to maclaw.apps.json, stat err=%v", statErr)
	}
}

func TestListSkillAppManifestsReadsEnterpriseAppDefinition(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval super skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), []byte(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"installUnit": "enterprise_app_pack",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": {"id": "expense-super-skill", "version": "1.0.0"},
				"dependencies": {"skills": [{"id": "expense-workflow", "kind": "workflow_skill", "version": "2.0.0", "required": true, "source": "hub"}]},
				"ui": {"schema": "maclaw.app.ui.v1", "entry": "approval_workspace", "layouts": {"approval_workspace": {"template": "classic_split", "primaryRegion": "center", "outputRegion": "bottom", "regions": [{"id": "request_form", "role": "input", "placement": "center"}, {"id": "result_panel", "role": "output", "placement": "bottom"}]}}},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"}]}
			},
			"governance": {
				"testEvidence": {"runId": "run-expense-1", "definitionHash": "hash-expense-1", "approvalInstance": {"instanceId": "wf-expense-1", "status": "approved", "currentNode": "expense.result", "workflowSkillId": "expense-workflow", "businessStatus": "finance_approved", "resultStatus": "approved", "resultPayload": {"approval_result": "approved"}}}
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.app.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-super-skill", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	entries := app.ListSkillAppManifests()
	if len(entries) != 1 {
		t.Fatalf("ListSkillAppManifests len=%d want 1: %#v", len(entries), entries)
	}
	got := entries[0]
	if got.ID != "expense-approval" || got.SkillID != "expense-super-skill" || got.Kind != "enterprise_approval_app" || got.AppDefinitionFile != "maclaw.app.json" {
		t.Fatalf("unexpected enterprise app discovery entry: %#v", got)
	}
	if got.AppDefinition == nil {
		t.Fatalf("enterprise app discovery should include full app definition: %#v", got)
	}
	appObj := got.AppDefinition["app"].(map[string]interface{})
	binding := appObj["binding"].(map[string]interface{})
	ui := binding["ui"].(map[string]interface{})
	layout := ui["layouts"].(map[string]interface{})["approval_workspace"].(map[string]interface{})
	if layout["primaryRegion"] != "center" || layout["outputRegion"] != "bottom" {
		t.Fatalf("enterprise app discovery should preserve dynamic layout: %#v", layout)
	}
	governance := appObj["governance"].(map[string]interface{})
	evidence := governance["testEvidence"].(map[string]interface{})
	approvalInstance := evidence["approvalInstance"].(map[string]interface{})
	if approvalInstance["workflowSkillId"] != "expense-workflow" || approvalInstance["resultPayload"].(map[string]interface{})["approval_result"] != "approved" {
		t.Fatalf("enterprise app discovery should preserve approval evidence: %#v", evidence)
	}
}

func TestSaveMaclawAppDefinitionForSkillUpdatesMaclawAppsManifestEntry(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: doc-tools\nmaclaw_app:\n  entry: maclaw.apps.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [
			{
				"id": "redact",
				"skill_id": "doc-tools",
				"name": "Old Redact",
				"icon": "shield",
				"governance": {
					"testEvidence": {
						"runId": "run-old-1",
						"verifiedAt": "2026-06-17T10:00:00Z",
						"definitionHash": "oldhash",
						"artifactPresent": true,
						"artifactName": "old.pdf"
					}
				}
			}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.apps.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.SaveMaclawAppDefinitionForSkill("doc-tools", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "redact",
			"name": "Redact Plus",
			"description": "Redact files with a custom icon.",
			"category": "Document",
			"kind": "tool_app",
			"icon": "shield",
			"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo=",
			"governance": { "status": "draft" },
			"binding": {
				"skill": {
					"id": "doc-tools",
					"appDefinitionFile": "maclaw.apps.json",
					"inputMode": "mixed",
					"multipleFiles": true,
					"outputModes": ["pdf"]
				}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}
	if result["app_definition_file"] != "maclaw.apps.json" {
		t.Fatalf("app_definition_file = %#v, want maclaw.apps.json", result["app_definition_file"])
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.apps.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.apps.json: %v", err)
	}
	var saved skillAppManifestFile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal maclaw.apps.json: %v", err)
	}
	if len(saved.Apps) != 1 {
		t.Fatalf("apps len=%d want 1: %s", len(saved.Apps), string(data))
	}
	got := saved.Apps[0]
	if got.ID != "redact" || got.Name != "Redact Plus" || got.CustomIconDataURL != "data:image/png;base64,iVBORw0KGgo=" || got.InputMode != "mixed" || !got.MultipleFiles {
		t.Fatalf("unexpected saved app entry: %#v\n%s", got, string(data))
	}
	if got.Governance["status"] != "draft" {
		t.Fatalf("incoming governance status was not saved: %#v", got.Governance)
	}
	evidence := got.Governance["testEvidence"].(map[string]any)
	if evidence["runId"] != "run-old-1" || evidence["definitionHash"] != "oldhash" || evidence["artifactName"] != "old.pdf" {
		t.Fatalf("existing governance test evidence was not preserved: %#v", evidence)
	}
}

func TestSaveMaclawAppDefinitionForSkillUpdatesSkillYAMLMetadata(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: invoice-app\ndescription: Invoice app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.SaveMaclawAppDefinitionForSkill("invoice-app", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review"
		}
	}`)
	if err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}
	if result["skill_yaml_updated"] != true {
		t.Fatalf("skill_yaml_updated = %#v, want true", result["skill_yaml_updated"])
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "skill.yaml"))
	if err != nil {
		t.Fatalf("ReadFile skill.yaml: %v", err)
	}
	var saved map[string]any
	if err := yaml.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal skill.yaml: %v", err)
	}
	block := saved["maclaw_app"].(map[string]any)
	if block["entry"] != "maclaw.app.json" || block["status"] != "draft" || block["add_to_app_panel"] != true {
		t.Fatalf("unexpected maclaw_app metadata: %#v\n%s", block, string(data))
	}
}

func TestRecordMaclawAppRunEvidenceForSkillWritesGovernance(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: invoice-app\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := app.SaveMaclawAppDefinitionForSkill("invoice-app", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": { "id": "invoice-review", "name": "Invoice Review" }
	}`); err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}

	result, err := app.RecordMaclawAppRunEvidenceForSkill("invoice-app", "skill-app-invoice-app-invoice-review", "feedbeef", "run-ok-1", filepath.Join(tmpHome, "out", "invoice.pdf"), "2026-06-17T10:00:00Z")
	if err != nil {
		t.Fatalf("RecordMaclawAppRunEvidenceForSkill() error = %v", err)
	}
	if result["test_run_id"] != "run-ok-1" || result["test_definition_hash"] != "feedbeef" || result["test_artifact_name"] != "invoice.pdf" {
		t.Fatalf("unexpected record result: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.app.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.app.json: %v", err)
	}
	if strings.Contains(string(data), tmpHome) {
		t.Fatalf("evidence should not persist local artifact path: %s", string(data))
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal maclaw.app.json: %v", err)
	}
	appObj := doc["app"].(map[string]any)
	governance := appObj["governance"].(map[string]any)
	evidence := governance["testEvidence"].(map[string]any)
	if evidence["runId"] != "run-ok-1" || evidence["definitionHash"] != "feedbeef" || evidence["artifactName"] != "invoice.pdf" || evidence["artifactPresent"] != true {
		t.Fatalf("unexpected test evidence: %#v", evidence)
	}
}

func TestRecordMaclawAppRunEvidenceForSkillPreservesEnterpriseEvidence(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "expense-super-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Expense approval super skill\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "expense-super-skill", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := app.SaveMaclawAppDefinitionForSkill("expense-super-skill", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": {
				"appSkill": {"id": "expense-super-skill", "version": "1.0.0"},
				"dependencies": {"skills": [{"id": "expense-workflow", "kind": "workflow_skill", "version": "2.0.0", "required": true, "source": "hub"}]},
				"mis": {"approvalBindings": [{"event": "expense.submitted", "workflowSkillId": "expense-workflow", "workflowVersion": "2.0.0", "objectRole": "expense_report"}]}
			},
			"governance": {
				"testEvidence": {
					"runId": "run-old",
					"definitionHash": "old-hash",
					"resultPayload": {"approval_result": "approved", "business_status": "finance_approved"},
					"outputs": [{"kind": "approval_result", "title": "Approval", "text": "approved", "status": "ready"}],
					"artifacts": [{"id": "artifact-expense", "uri": "artifact://expense/evidence.zip", "name": "evidence.zip"}],
					"approvalInstance": {
						"instanceId": "wf-expense-1",
						"approvalID": "approval-expense-1",
						"recordID": "expense-1",
						"status": "approved",
						"currentNode": "expense.result",
						"workflowSkillId": "expense-workflow",
						"workflowVersion": "2.0.0",
						"businessStatus": "finance_approved",
						"resultStatus": "approved",
						"resultPayload": {"approval_result": "approved", "business_status": "finance_approved"},
						"outputs": [{"kind": "approval_result", "title": "Approval", "text": "approved", "status": "ready"}],
						"artifacts": [{"id": "artifact-expense", "uri": "artifact://expense/evidence.zip", "name": "evidence.zip"}],
						"viewVerified": true
					},
					"dependencyVerification": {"schema": "maclaw.app.install_plan.v1", "dependencyCount": 2, "hasMissingRequired": false, "hasBlockingDependency": false}
				}
			}
		}
	}`); err != nil {
		t.Fatalf("SaveMaclawAppDefinitionForSkill() error = %v", err)
	}

	result, err := app.RecordMaclawAppRunEvidenceForSkill("expense-super-skill", "expense-approval", "fresh-hash", "run-fresh", filepath.Join(tmpHome, "out", "approval.zip"), "2026-06-19T10:00:00Z")
	if err != nil {
		t.Fatalf("RecordMaclawAppRunEvidenceForSkill() error = %v", err)
	}
	if result["test_run_id"] != "run-fresh" || result["test_definition_hash"] != "fresh-hash" || result["test_artifact_name"] != "approval.zip" {
		t.Fatalf("unexpected record result: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.app.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.app.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("Unmarshal maclaw.app.json: %v", err)
	}
	appObj := doc["app"].(map[string]any)
	governance := appObj["governance"].(map[string]any)
	evidence := governance["testEvidence"].(map[string]any)
	if evidence["runId"] != "run-fresh" || evidence["definitionHash"] != "fresh-hash" || evidence["artifactName"] != "approval.zip" {
		t.Fatalf("run evidence should update freshness fields: %#v", evidence)
	}
	approvalInstance, ok := evidence["approvalInstance"].(map[string]any)
	if !ok || approvalInstance["approvalID"] != "approval-expense-1" || approvalInstance["currentNode"] != "expense.result" || approvalInstance["workflowSkillId"] != "expense-workflow" || approvalInstance["businessStatus"] != "finance_approved" || approvalInstance["resultStatus"] != "approved" {
		t.Fatalf("run evidence should preserve approval instance core fields: %#v", evidence)
	}
	if payload, ok := approvalInstance["resultPayload"].(map[string]any); !ok || payload["approval_result"] != "approved" {
		t.Fatalf("run evidence should preserve approval instance result payload: %#v", approvalInstance)
	}
	if outputs, ok := approvalInstance["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("run evidence should preserve approval instance outputs: %#v", approvalInstance)
	}
	if artifacts, ok := approvalInstance["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("run evidence should preserve approval instance artifacts: %#v", approvalInstance)
	}
	if payload, ok := evidence["resultPayload"].(map[string]any); !ok || payload["business_status"] != "finance_approved" {
		t.Fatalf("run evidence should preserve top-level result payload: %#v", evidence)
	}
	if verification, ok := evidence["dependencyVerification"].(map[string]any); !ok || verification["schema"] != "maclaw.app.install_plan.v1" {
		t.Fatalf("run evidence should preserve dependency verification: %#v", evidence)
	}
}

func TestMergeMaclawAppRunEvidenceDeepPreservesEnterpriseEvidence(t *testing.T) {
	merged := mergeMaclawAppRunEvidence(map[string]any{
		"runId": "run-old",
		"resultPayload": map[string]any{
			"approval_result": "approved",
			"business_status": "finance_ready",
		},
		"outputs":   []any{map[string]any{"kind": "approval_result", "title": "Decision", "text": "approved"}},
		"artifacts": []any{map[string]any{"id": "artifact-old", "name": "approval.pdf"}},
		"approvalInstance": map[string]any{
			"instanceId":      "wf-expense-1",
			"approvalID":      "approval-expense-1",
			"currentNode":     "finance.archive",
			"workflowSkillId": "expense-workflow",
			"businessStatus":  "finance_ready",
			"resultStatus":    "approved",
			"resultPayload":   map[string]any{"approval_result": "approved", "amount": float64(1280)},
			"outputs":         []any{map[string]any{"kind": "text", "text": "ready"}},
			"artifacts":       []any{map[string]any{"id": "artifact-approval", "name": "approval.pdf"}},
		},
		"dependencyVerification": map[string]any{
			"schema":                "maclaw.app.install_plan.v1",
			"dependencyCount":       float64(1),
			"hasBlockingDependency": false,
			"dependencies":          []any{map[string]any{"id": "expense-workflow", "installed": true, "health": "ready"}},
		},
	}, map[string]any{
		"runId":          "run-fresh",
		"verifiedAt":     "2026-06-27T11:00:00Z",
		"definitionHash": "fresh-hash",
		"resultPayload": map[string]any{
			"business_status": "payment_ready",
		},
		"outputs":   []any{},
		"artifacts": []any{},
		"approvalInstance": map[string]any{
			"currentNode":   "payment.queue",
			"resultPayload": map[string]any{"business_status": "payment_ready"},
			"outputs":       []any{},
			"artifacts":     []any{},
		},
		"dependencyVerification": map[string]any{
			"verifiedAt": "2026-06-27T10:59:00Z",
		},
	})

	if merged["runId"] != "run-fresh" || merged["definitionHash"] != "fresh-hash" || merged["verifiedAt"] != "2026-06-27T11:00:00Z" {
		t.Fatalf("freshness fields should update: %#v", merged)
	}
	payload, ok := merged["resultPayload"].(map[string]any)
	if !ok || payload["approval_result"] != "approved" || payload["business_status"] != "payment_ready" {
		t.Fatalf("top-level result payload should deep merge: %#v", merged["resultPayload"])
	}
	if outputs, ok := merged["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("empty incoming outputs should not clear existing outputs: %#v", merged["outputs"])
	}
	if artifacts, ok := merged["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("empty incoming artifacts should not clear existing artifacts: %#v", merged["artifacts"])
	}
	approval, ok := merged["approvalInstance"].(map[string]any)
	if !ok || approval["approvalID"] != "approval-expense-1" || approval["workflowSkillId"] != "expense-workflow" || approval["currentNode"] != "payment.queue" {
		t.Fatalf("approval instance should deep merge identity and node fields: %#v", merged["approvalInstance"])
	}
	approvalPayload, ok := approval["resultPayload"].(map[string]any)
	if !ok || approvalPayload["approval_result"] != "approved" || approvalPayload["business_status"] != "payment_ready" {
		t.Fatalf("approval instance result payload should deep merge: %#v", approval["resultPayload"])
	}
	if outputs, ok := approval["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("empty incoming approval outputs should not clear existing outputs: %#v", approval["outputs"])
	}
	if artifacts, ok := approval["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("empty incoming approval artifacts should not clear existing artifacts: %#v", approval["artifacts"])
	}
	verification, ok := merged["dependencyVerification"].(map[string]any)
	if !ok || verification["schema"] != "maclaw.app.install_plan.v1" || verification["verifiedAt"] != "2026-06-27T10:59:00Z" {
		t.Fatalf("dependency verification should deep merge: %#v", merged["dependencyVerification"])
	}
	if dependencies, ok := verification["dependencies"].([]any); !ok || len(dependencies) != 1 {
		t.Fatalf("dependency verification dependencies should be preserved: %#v", verification)
	}
}

func TestRecordMaclawAppRunEvidenceForSkillUpdatesMaclawAppsManifest(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "doc-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: doc-tools\nmaclaw_app:\n  entry: maclaw.apps.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [
			{
				"id": "redact",
				"skill_id": "doc-tools",
				"name": "Document Redaction",
				"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo="
			},
			{
				"id": "archive",
				"skill_id": "doc-tools",
				"name": "Archive"
			}
		]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.apps.json: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "doc-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	result, err := app.RecordMaclawAppRunEvidenceForSkill("doc-tools", "skill-app-doc-tools-redact", "c0ffee", "run-redact-1", filepath.Join(tmpHome, "out", "redacted.pdf"), "2026-06-18T10:00:00Z")
	if err != nil {
		t.Fatalf("RecordMaclawAppRunEvidenceForSkill() error = %v", err)
	}
	if result["app_definition_file"] != "maclaw.apps.json" || result["test_run_id"] != "run-redact-1" || result["test_definition_hash"] != "c0ffee" || result["test_artifact_name"] != "redacted.pdf" {
		t.Fatalf("unexpected record result: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(skillDir, "maclaw.apps.json"))
	if err != nil {
		t.Fatalf("ReadFile maclaw.apps.json: %v", err)
	}
	if strings.Contains(string(data), tmpHome) {
		t.Fatalf("evidence should not persist local artifact path: %s", string(data))
	}
	var manifest skillAppManifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("Unmarshal maclaw.apps.json: %v", err)
	}
	if len(manifest.Apps) != 2 {
		t.Fatalf("apps len=%d want 2: %#v", len(manifest.Apps), manifest.Apps)
	}
	if manifest.Apps[0].CustomIconDataURL != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("custom icon was not preserved: %#v", manifest.Apps[0])
	}
	evidence := manifest.Apps[0].Governance["testEvidence"].(map[string]any)
	if evidence["runId"] != "run-redact-1" || evidence["definitionHash"] != "c0ffee" || evidence["artifactName"] != "redacted.pdf" || evidence["artifactPresent"] != true {
		t.Fatalf("unexpected test evidence: %#v", evidence)
	}
	if manifest.Apps[1].Governance != nil {
		t.Fatalf("non-target app should not receive governance evidence: %#v", manifest.Apps[1].Governance)
	}
}

func TestPackageSkillForMarketPreservesMaclawAppDefinition(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: invoice-app\ndescription: A portable invoice app skill for upload testing.\ntriggers:\n  - invoice app\nplatforms:\n  - universal\nsteps:\n  - action: bash\n    params:\n      command: echo ok\nmaclaw_app:\n  entry: maclaw.app.json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.yaml: %v", err)
	}
	appDefinitionData := []byte(`{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review",
			"description": "Review invoices with a guided panel",
			"category": "finance",
			"icon": "receipt",
			"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo=",
			"kind": "tool_app",
			"binding": { "skill": { "id": "invoice-app", "inputMode": "file", "outputModes": ["pdf", "docx"] } },
			"governance": { "testEvidence": { "runId": "run-ok-1", "verifiedAt": "2026-06-17T10:00:00Z", "definitionHash": "feedbeef", "artifactPresent": true, "artifactName": "invoice.pdf", "outputCount": 1, "primaryResult": "invoice_ready", "resultPayload": { "business_status": "invoice_ready", "business_record": { "id": "INV-1", "status": "invoice_ready" } } } }
		}
	}`)
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.app.json"), appDefinitionData, 0o644); err != nil {
		t.Fatalf("WriteFile maclaw.app.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillDir, "credentials"), 0o755); err != nil {
		t.Fatalf("MkdirAll credentials: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "credentials", "invoice.json"), []byte(`{"token":"test"}`), 0o644); err != nil {
		t.Fatalf("WriteFile credential fixture: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:                    "invoice-app",
		Description:             "A portable invoice app skill for upload testing.",
		Triggers:                []string{"invoice app"},
		Status:                  "active",
		SkillDir:                skillDir,
		Platforms:               []string{"universal"},
		RequiresGUI:             true,
		RequiredEnv:             []string{"INVOICE_API_KEY"},
		RequiredCredentialFiles: []string{"credentials/invoice.json"},
		RequiresTools:           []string{"browser"},
		RequiresToolsets:        []string{"files"},
		Capabilities:            []string{"document_review"},
		Steps:                   []corelib.NLSkillStep{{Action: "bash", Params: map[string]any{"command": "echo ok"}}},
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)

	zipPath, packageDir, err := app.packageSkillForMarketWithDir("invoice-app")
	if err != nil {
		t.Fatalf("packageSkillForMarketWithDir() error = %v", err)
	}
	defer os.RemoveAll(packageDir)
	defer os.Remove(zipPath)

	if _, err := os.Stat(filepath.Join(packageDir, "maclaw.app.json")); err != nil {
		t.Fatalf("package missing maclaw.app.json: %v", err)
	}
	packagedAppData, err := os.ReadFile(filepath.Join(packageDir, "maclaw.app.json"))
	if err != nil {
		t.Fatalf("ReadFile packaged maclaw.app.json: %v", err)
	}
	if !strings.Contains(string(packagedAppData), `"customIconDataUrl": "data:image/png;base64,iVBORw0KGgo="`) {
		t.Fatalf("packaged maclaw.app.json lost custom icon: %s", string(packagedAppData))
	}
	yamlData, err := os.ReadFile(filepath.Join(packageDir, "skill.yaml"))
	if err != nil {
		t.Fatalf("ReadFile packaged skill.yaml: %v", err)
	}
	var saved map[string]any
	if err := yaml.Unmarshal(yamlData, &saved); err != nil {
		t.Fatalf("Unmarshal packaged skill.yaml: %v", err)
	}
	block := saved["maclaw_app"].(map[string]any)
	if block["entry"] != "maclaw.app.json" {
		t.Fatalf("packaged skill.yaml lost maclaw_app metadata: %#v\n%s", block, string(yamlData))
	}
	manifestData, err := os.ReadFile(filepath.Join(packageDir, "skill_package_manifest.json"))
	if err != nil {
		t.Fatalf("ReadFile skill_package_manifest.json: %v", err)
	}
	var manifest skillPackageManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("Unmarshal skill_package_manifest.json: %v", err)
	}
	if manifest.ProductKind != "maclaw_app_skill" || !manifest.IsMaclawApp || manifest.MaclawAppEntry != "maclaw.app.json" || manifest.MaclawAppCount != 1 {
		t.Fatalf("package manifest missing maclaw app product metadata: %#v", manifest)
	}
	if manifest.MaclawAppID != "invoice-review" || manifest.MaclawAppName != "Invoice Review" || manifest.MaclawAppDescription != "Review invoices with a guided panel" || manifest.MaclawAppCategory != "finance" || manifest.MaclawAppIcon != "receipt" {
		t.Fatalf("package manifest missing maclaw app preview metadata: %#v", manifest)
	}
	if manifest.MaclawAppInputMode != "file" || strings.Join(manifest.MaclawAppOutputModes, ",") != "pdf,docx" {
		t.Fatalf("package manifest missing maclaw app IO metadata: %#v", manifest)
	}
	if !manifest.ArtifactContractRequired || strings.Join(manifest.ArtifactContractOutputModes, ",") != "pdf,docx" || manifest.ArtifactContractPresentation != "preview_or_file" {
		t.Fatalf("package manifest missing artifact contract: %#v", manifest)
	}
	if !manifest.DeclaredRequiresGUI || strings.Join(manifest.DeclaredRequiredEnv, ",") != "INVOICE_API_KEY" {
		t.Fatalf("package manifest missing declared env/gui: %#v", manifest)
	}
	if strings.Join(manifest.DeclaredPermissions, ",") != "gui,env:INVOICE_API_KEY,credential_file,tool:browser,toolset:files,capability:document_review" {
		t.Fatalf("package manifest missing declared permissions: %#v", manifest)
	}
	if manifest.MaclawAppTestEvidence == nil || manifest.MaclawAppTestEvidence.RunID != "run-ok-1" || manifest.MaclawAppTestEvidence.VerifiedAt != "2026-06-17T10:00:00Z" || manifest.MaclawAppTestEvidence.DefinitionFingerprint != "feedbeef" || !manifest.MaclawAppTestEvidence.ArtifactPresent || manifest.MaclawAppTestEvidence.ArtifactName != "invoice.pdf" {
		t.Fatalf("package manifest missing app test evidence: %#v", manifest.MaclawAppTestEvidence)
	}
	if manifest.MaclawAppTestEvidence.OutputCount != 1 || manifest.MaclawAppTestEvidence.PrimaryResult != "invoice_ready" {
		t.Fatalf("package manifest missing structured result evidence: %#v", manifest.MaclawAppTestEvidence)
	}
	if record, ok := manifest.MaclawAppTestEvidence.ResultPayload["business_record"].(map[string]any); !ok || record["id"] != "INV-1" {
		t.Fatalf("package manifest missing result payload record: %#v", manifest.MaclawAppTestEvidence.ResultPayload)
	}
	appDefinitionSum := sha256.Sum256(appDefinitionData)
	if manifest.MaclawAppDefinitionSHA256 != hex.EncodeToString(appDefinitionSum[:]) {
		t.Fatalf("package manifest missing maclaw app definition hash: %#v", manifest)
	}
	seen := map[string]bool{}
	for _, file := range manifest.Files {
		seen[file.Path] = true
	}
	if !seen["maclaw.app.json"] {
		t.Fatalf("package manifest missing maclaw.app.json: %+v", manifest.Files)
	}
}

func TestSaveMaclawAppDefinitionForSkillRejectsMismatchedBinding(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "invoice-app")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "invoice-app", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err = app.SaveMaclawAppDefinitionForSkill("invoice-app", `{
		"schema": "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": {
			"id": "invoice-review",
			"name": "Invoice Review",
			"binding": { "skill": { "id": "other-skill" } }
		}
	}`)
	if err == nil || !strings.Contains(err.Error(), "does not match target skill") {
		t.Fatalf("expected mismatched binding error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(skillDir, "maclaw.app.json")); !os.IsNotExist(statErr) {
		t.Fatalf("maclaw.app.json should not be written, stat err=%v", statErr)
	}
}

func TestStageSkillAppInputFileWritesTempFile(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}

	ref, err := app.StageSkillAppInputFile("../demo.pdf", "application/pdf", 1234, base64.StdEncoding.EncodeToString([]byte("pdf data")))
	if err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}
	if ref.Name != "demo.pdf" || ref.Type != "application/pdf" || ref.Size != int64(len("pdf data")) || ref.LastModified != 1234 || ref.Transfer != "staged_file" {
		t.Fatalf("unexpected file ref: %#v", ref)
	}
	if !strings.Contains(filepath.Clean(ref.StagedPath), filepath.Clean(filepath.Join(tmpHome, ".maclaw", "temp", "app-inputs"))) {
		t.Fatalf("staged path outside app temp: %s", ref.StagedPath)
	}
	got, err := os.ReadFile(ref.StagedPath)
	if err != nil {
		t.Fatalf("ReadFile staged path: %v", err)
	}
	if string(got) != "pdf data" {
		t.Fatalf("staged content = %q, want pdf data", string(got))
	}
}

func TestStageSkillAppInputFileRejectsInvalidPayload(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.StageSkillAppInputFile("demo.pdf", "application/pdf", 0, "not base64"); err == nil {
		t.Fatal("expected invalid base64 error")
	}
	tooLarge := base64.StdEncoding.EncodeToString(make([]byte, maxSkillAppInputFileBytes+1))
	if _, err := app.StageSkillAppInputFile("big.bin", "application/octet-stream", 0, tooLarge); err == nil {
		t.Fatal("expected size limit error")
	}
}

func TestStageSkillAppInputFileCleansStaleInputDirs(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	root := filepath.Join(app.GetTempDir(), "app-inputs")
	oldDir := filepath.Join(root, "input-old")
	freshDir := filepath.Join(root, "input-fresh")
	foreignDir := filepath.Join(root, "keep-old")
	for _, dir := range []string{oldDir, freshDir, foreignDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("MkdirAll %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "payload.txt"), []byte("data"), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", dir, err)
		}
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	for _, path := range []string{oldDir, filepath.Join(oldDir, "payload.txt"), foreignDir, filepath.Join(foreignDir, "payload.txt")} {
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes %s: %v", path, err)
		}
	}

	if _, err := app.StageSkillAppInputFile("demo.txt", "text/plain", 0, base64.StdEncoding.EncodeToString([]byte("demo"))); err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("expected stale input dir removed, stat err=%v", err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		t.Fatalf("fresh input dir should remain: %v", err)
	}
	if _, err := os.Stat(foreignDir); err != nil {
		t.Fatalf("non input-* dir should remain: %v", err)
	}
}

func TestRunNLSkillAsyncCleansStagedInputWhenStartFails(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	app.skillRunner = NewSkillRunner(&SkillExecutor{app: app})
	ref, err := app.StageSkillAppInputFile("demo.txt", "text/plain", 0, base64.StdEncoding.EncodeToString([]byte("demo")))
	if err != nil {
		t.Fatalf("StageSkillAppInputFile() error = %v", err)
	}

	_, err = app.RunNLSkillAsync("missing-skill", map[string]interface{}{
		"file": map[string]interface{}{"staged_path": ref.StagedPath},
	})

	if err == nil {
		t.Fatal("expected missing skill error")
	}
	if _, statErr := os.Stat(ref.StagedPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected staged input cleanup after start failure, stat err=%v", statErr)
	}
}

func TestMarkMaclawAppSkillRunArgsSetsAppMarker(t *testing.T) {
	got := markMaclawAppSkillRunArgs(map[string]interface{}{"input": "demo.pdf"})
	if got["_maclaw_app"] != true {
		t.Fatalf("_maclaw_app = %#v, want true", got["_maclaw_app"])
	}

	got = markMaclawAppSkillRunArgs(nil)
	if got == nil || got["_maclaw_app"] != true {
		t.Fatalf("nil args marker = %#v, want _maclaw_app true", got)
	}
}

func TestWithSkillAppInputFileAliasesAddsTemplateFriendlyPaths(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"file": map[string]interface{}{
			"name":        "demo.pdf",
			"staged_path": "/tmp/maclaw/demo.pdf",
		},
		"files": []map[string]interface{}{
			{"staged_path": "/tmp/maclaw/demo.pdf"},
			{"staged_path": "/tmp/maclaw/extra.pdf"},
		},
	})

	for _, key := range []string{"file_path", "input_file_path", "local_file_path", "uploaded_file_path"} {
		if got[key] != "/tmp/maclaw/demo.pdf" {
			t.Fatalf("%s = %#v, want staged path in %#v", key, got[key], got)
		}
	}
	if got["file_name"] != "demo.pdf" {
		t.Fatalf("file_name = %#v, want demo.pdf", got["file_name"])
	}
	paths, ok := got["file_paths"].([]string)
	if !ok || len(paths) != 2 || paths[0] != "/tmp/maclaw/demo.pdf" || paths[1] != "/tmp/maclaw/extra.pdf" {
		t.Fatalf("file_paths = %#v, want both staged paths", got["file_paths"])
	}
}

func TestWithSkillAppInputFileAliasesDoesNotOverrideExplicitValues(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"file_path": "explicit.txt",
		"file_name": "explicit-name.txt",
		"file": map[string]interface{}{
			"name":        "demo.pdf",
			"staged_path": "/tmp/maclaw/demo.pdf",
		},
	})

	if got["file_path"] != "explicit.txt" || got["file_name"] != "explicit-name.txt" {
		t.Fatalf("explicit aliases should win: %#v", got)
	}
	if got["input_file_path"] != "/tmp/maclaw/demo.pdf" {
		t.Fatalf("missing non-conflicting alias: %#v", got)
	}
}

func TestWithSkillAppInputFileAliasesSetsInputAlias(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"file": map[string]interface{}{
			"name":        "report.pdf",
			"staged_path": "/tmp/maclaw/report.pdf",
		},
	})
	if got["input"] != "/tmp/maclaw/report.pdf" {
		t.Fatalf("input = %#v, want staged path for {{input}} template", got["input"])
	}
}

func TestWithSkillAppInputFileAliasesSynthesizesOutputPath(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"output_mode": "docx",
		"file": map[string]interface{}{
			"name":        "report.pdf",
			"staged_path": "/tmp/maclaw/report.pdf",
		},
	})
	want := filepath.Join("/tmp/maclaw", "report.docx")
	if got["output"] != want {
		t.Fatalf("output = %#v, want %q (synthesized from input + output_mode)", got["output"], want)
	}
}

func TestWithSkillAppInputFileAliasesSynthesizesPersistentOutputForStagedInput(t *testing.T) {
	oldBaseDir := corelib.MaclawBaseDir()
	baseDir := t.TempDir()
	corelib.SetMaclawBaseDir(baseDir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBaseDir) })

	stagedPath := filepath.Join(baseDir, "temp", "app-inputs", "input-123", "report.pdf")
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"output_mode": "txt",
		"file": map[string]interface{}{
			"name":        "report.pdf",
			"staged_path": stagedPath,
		},
	})

	want := filepath.Join(baseDir, "data", "app-outputs", "input-123", "report.txt")
	if got["output"] != want {
		t.Fatalf("output = %#v, want persistent path %q", got["output"], want)
	}
	if info, err := os.Stat(filepath.Dir(want)); err != nil || !info.IsDir() {
		t.Fatalf("persistent output dir was not created: info=%v err=%v", info, err)
	}
}

func TestWithSkillAppInputFileAliasesSynthesizesPersistentPDFOutputForStagedPDF(t *testing.T) {
	oldBaseDir := corelib.MaclawBaseDir()
	baseDir := t.TempDir()
	corelib.SetMaclawBaseDir(baseDir)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBaseDir) })

	stagedPath := filepath.Join(baseDir, "temp", "app-inputs", "input-456", "paper.pdf")
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"output_mode": "pdf",
		"file": map[string]interface{}{
			"name":        "paper.pdf",
			"staged_path": stagedPath,
		},
	})

	want := filepath.Join(baseDir, "data", "app-outputs", "input-456", "paper.pdf")
	if got["output"] != want {
		t.Fatalf("output = %#v, want persistent PDF path %q", got["output"], want)
	}
}

func TestWithSkillAppInputFileAliasesDoesNotOverrideExplicitOutput(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"output_mode": "docx",
		"output":      "/explicit/output.docx",
		"file": map[string]interface{}{
			"name":        "report.pdf",
			"staged_path": "/tmp/maclaw/report.pdf",
		},
	})
	if got["output"] != "/explicit/output.docx" {
		t.Fatalf("explicit output should not be overridden: %#v", got["output"])
	}
}

func TestWithSkillAppInputFileAliasesOutputAvoidsOverwriteInput(t *testing.T) {
	got := withSkillAppInputFileAliases(map[string]interface{}{
		"output_mode": "pdf",
		"file": map[string]interface{}{
			"name":        "report.pdf",
			"staged_path": "/tmp/maclaw/report.pdf",
		},
	})
	want := filepath.Join("/tmp/maclaw", "report_output.pdf")
	if got["output"] != want {
		t.Fatalf("output = %#v, want %q (should avoid overwriting input)", got["output"], want)
	}
}

func TestSynthesizeSkillAppOutputPathVariousFormats(t *testing.T) {
	cases := []struct {
		input      string
		format     string
		wantSuffix string
	}{
		{"/tmp/demo.pdf", "docx", "demo.docx"},
		{"/tmp/demo.pdf", "txt", "demo.txt"},
		{"/tmp/demo.xlsx", "json", "demo.json"},
		{"/tmp/a b c.PDF", "docx", "a b c.docx"},
		{"", "docx", ""},
		{"/tmp/demo.pdf", "", ""},
	}
	for _, tc := range cases {
		got := synthesizeSkillAppOutputPath(tc.input, tc.format)
		if tc.wantSuffix == "" {
			if got != "" {
				t.Fatalf("synthesizeSkillAppOutputPath(%q, %q) = %q, want empty", tc.input, tc.format, got)
			}
			continue
		}
		if !strings.HasSuffix(got, tc.wantSuffix) {
			t.Fatalf("synthesizeSkillAppOutputPath(%q, %q) = %q, want suffix %q", tc.input, tc.format, got, tc.wantSuffix)
		}
	}
}

func TestListSkillAppManifestsNormalizesPrivateExtension(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "sheet-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "clean",
			"name": "Sheet Clean",
			"category": "",
			"icon": "unknown",
			"input_mode": "weird",
			"output_modes": ["xlsx", "bad", "xlsx", "JSON"]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "sheet-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	if items[0].Category != "Skill" || items[0].Icon != "contract" || items[0].InputMode != "file" {
		t.Fatalf("unexpected normalized fields: %#v", items[0])
	}
	if len(items[0].OutputModes) != 2 || items[0].OutputModes[0] != "xlsx" || items[0].OutputModes[1] != "json" {
		t.Fatalf("unexpected normalized output modes: %#v", items[0].OutputModes)
	}
}

func TestListSkillAppManifestsNormalizesFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "field-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "v1",
		"apps": [{
			"id": "fields",
			"name": "Field Tool",
			"input_mode": "form",
			"fields": [
				{ "name": "", "label": "Skip me" },
				{ "name": " title ", "type": "unknown", "required": true, "default": 42 },
				{ "name": "format", "type": "SELECT", "default": "Summary", "options": ["Detailed", "Summary", "Detailed", 2] },
				{ "name": "include_refs", "type": "boolean", "required": true, "default": "true", "options": ["ignored"] }
			]
		}]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "field-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	items := app.ListSkillAppManifests()
	if len(items) != 1 {
		t.Fatalf("items len=%d want 1: %#v", len(items), items)
	}
	fields := items[0].Fields
	if len(fields) != 3 {
		t.Fatalf("fields len=%d want 3: %#v", len(fields), fields)
	}
	if fields[0].Name != "title" || fields[0].Label != "title" || fields[0].Type != "text" || !fields[0].Required || fields[0].Default != "42" {
		t.Fatalf("unexpected text field: %#v", fields[0])
	}
	if fields[1].Type != "select" || len(fields[1].Options) != 3 || fields[1].Options[0] != "Summary" || fields[1].Options[1] != "Detailed" || fields[1].Options[2] != "2" {
		t.Fatalf("unexpected select field: %#v", fields[1])
	}
	if fields[2].Type != "boolean" || fields[2].Default != true || len(fields[2].Options) != 0 {
		t.Fatalf("unexpected boolean field: %#v", fields[2])
	}
}

func TestNormalizeSkillAppIconAndInputModeLowercase(t *testing.T) {
	if got := normalizeSkillAppIconName(" PDF "); got != "pdf" {
		t.Fatalf("normalizeSkillAppIconName()=%q want pdf", got)
	}
	if got := normalizeSkillAppInputMode(" Mixed "); got != "mixed" {
		t.Fatalf("normalizeSkillAppInputMode()=%q want mixed", got)
	}
}

func TestListSkillAppManifestsRequiresV1PrivateMarker(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "bad-tools")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "maclaw.apps.json"), []byte(`{
		"x_maclaw_apps": "not-v1",
		"apps": [{ "id": "bad", "name": "Bad App" }]
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile manifest: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{Name: "bad-tools", SkillDir: skillDir, Status: "active"}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	if items := app.ListSkillAppManifests(); len(items) != 0 {
		t.Fatalf("items len=%d want 0: %#v", len(items), items)
	}
}
