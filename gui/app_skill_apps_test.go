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
	if items[0].ID != "redact" || items[0].SkillID != "doc-tools" || items[0].Name != "Document Redaction" || items[0].InputMode != "file" {
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
	if got.ID != "invoice-review" || got.SkillID != "invoice-app" || got.Name != "Invoice Review" || got.Category != "Finance" || got.Icon != "receipt" {
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
			"kind": "tool_app",
			"binding": { "skill": { "id": "invoice-app", "inputMode": "file", "outputModes": ["pdf", "docx"] } },
			"governance": { "testEvidence": { "runId": "run-ok-1", "verifiedAt": "2026-06-17T10:00:00Z", "definitionHash": "feedbeef", "artifactPresent": true, "artifactName": "invoice.pdf" } }
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
