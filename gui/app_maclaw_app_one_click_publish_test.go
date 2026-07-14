package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaterializeMaclawAppSkillZipForGUI(t *testing.T) {
	pkg := `{
  "schema": "maclaw.app.pack.v1",
  "privateMarker": "x_maclaw_apps",
  "apps": [{
    "schema": "maclaw.app.v1",
    "privateMarker": "x_maclaw_apps",
    "app": {
      "id": "invoice-one-click",
      "name": "Invoice One Click",
      "description": "One click publish package",
      "kind": "tool_app",
      "category": "finance",
      "icon": "receipt",
      "binding": {"skill": {"id": "Invoice.Skill_Name", "appDefinitionFile": "maclaw.app.json"}}
    }
  }]
}`
	zipPath, cleanup, err := materializeMaclawAppSkillZipForGUI(pkg)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer cleanup()

	files := readTestZip(t, zipPath)
	for _, name := range []string{"skill.yaml", "skill_package_manifest.json", "maclaw.app.json"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("missing %s in %#v", name, keysOfMap(files))
		}
	}
	// Skill name prefers bound skill id (sanitized), not only app id.
	if !strings.Contains(files["skill.yaml"], "invoice.skill_name") {
		t.Fatalf("skill.yaml missing bound skill name: %s", files["skill.yaml"])
	}
	if !strings.Contains(files["skill_package_manifest.json"], `"is_maclaw_app":true`) &&
		!strings.Contains(files["skill_package_manifest.json"], `"is_maclaw_app": true`) {
		t.Fatalf("manifest missing is_maclaw_app: %s", files["skill_package_manifest.json"])
	}
	if !strings.Contains(files["maclaw.app.json"], "invoice-one-click") {
		t.Fatalf("maclaw.app.json missing app id: %s", files["maclaw.app.json"])
	}
	if !strings.Contains(files["skill_package_manifest.json"], `"skill_name":"invoice.skill_name"`) &&
		!strings.Contains(files["skill_package_manifest.json"], `"skill_name": "invoice.skill_name"`) {
		t.Fatalf("manifest skill_name: %s", files["skill_package_manifest.json"])
	}
}

func TestMaterializeMaclawAppSkillZipPreservesEnrichedPackageFields(t *testing.T) {
	pkg := `{
  "schema": "maclaw.app.pack.v1",
  "privateMarker": "x_maclaw_apps",
  "resolved_dependencies": [{"id":"dep-1","source":"hub"}],
  "bundled_dependencies": {"skills":[{"id":"dep-1"}]},
  "apps": [{
    "schema": "maclaw.app.v1",
    "privateMarker": "x_maclaw_apps",
    "app": {
      "id": "enriched-app",
      "name": "Enriched",
      "kind": "tool_app",
      "binding": {"skill": {"id": "enriched-skill"}}
    }
  }]
}`
	zipPath, cleanup, err := materializeMaclawAppSkillZipForGUI(pkg)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	defer cleanup()
	files := readTestZip(t, zipPath)
	if !strings.Contains(files["maclaw.app.json"], "enriched-app") {
		t.Fatalf("maclaw.app.json missing app: %s", files["maclaw.app.json"])
	}
	if !strings.Contains(files["skill.yaml"], "enriched-skill") {
		t.Fatalf("skill.yaml missing bound skill: %s", files["skill.yaml"])
	}
	manifest := files["skill_package_manifest.json"]
	if !strings.Contains(manifest, "resolved_dependencies") || !strings.Contains(manifest, "dep-1") {
		t.Fatalf("manifest missing resolved_dependencies: %s", manifest)
	}
	if !strings.Contains(manifest, "bundled_dependencies") {
		t.Fatalf("manifest missing bundled_dependencies: %s", manifest)
	}
}

func TestMaclawAppPrimarySkillIDFromPackageJSON(t *testing.T) {
	pkg := `{
  "schema": "maclaw.app.pack.v1",
  "privateMarker": "x_maclaw_apps",
  "apps": [{
    "schema": "maclaw.app.v1",
    "privateMarker": "x_maclaw_apps",
    "app": {
      "id": "bound-app",
      "name": "Bound",
      "binding": {"skill": {"id": "invoice-skill", "appDefinitionFile": "maclaw.app.json"}}
    }
  }]
}`
	if got := maclawAppPrimarySkillIDFromPackageJSON(pkg); got != "invoice-skill" {
		t.Fatalf("skill id = %q", got)
	}
	appSkillPkg := `{
  "schema": "maclaw.app.v1",
  "app": {"id": "a1", "appSkill": {"id": "expense-app-skill"}}
}`
	if got := maclawAppPrimarySkillIDFromPackageJSON(appSkillPkg); got != "expense-app-skill" {
		t.Fatalf("appSkill id = %q", got)
	}
	if got := maclawAppPrimarySkillIDFromPackageJSON(`{"schema":"maclaw.app.v1"}`); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSummarizeMaclawAppOneClickPublish(t *testing.T) {
	msg := summarizeMaclawAppOneClickPublish(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": true},
		"skill_market":        map[string]any{"ok": true, "submission_id": "sub-1"},
	})
	if !strings.Contains(msg, "queued locally") || !strings.Contains(msg, "enterprise hub pack") || !strings.Contains(msg, "skill market") {
		t.Fatalf("unexpected summary: %s", msg)
	}
	partialMsg := summarizeMaclawAppOneClickPublish(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": false, "error": "hub offline"},
		"skill_market":        map[string]any{"ok": false, "error": "no remote_email"},
	})
	if !strings.Contains(partialMsg, "hub offline") || !strings.Contains(partialMsg, "no remote_email") {
		t.Fatalf("unexpected partial summary: %s", partialMsg)
	}
	if !maclawAppOneClickTargetsPartial(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": true},
		"skill_market":        map[string]any{"ok": false, "error": "x"},
	}) {
		t.Fatal("expected partial=true when skill market fails")
	}
	if maclawAppOneClickTargetsPartial(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": true, "skipped": true},
		"skill_market":        map[string]any{"ok": true, "submission_id": "s"},
	}) {
		t.Fatal("expected partial=false when all targets ok")
	}
	retryMsg := summarizeMaclawAppOneClickPublish(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": true, "skipped": true},
		"skill_market":        map[string]any{"ok": true, "submission_id": "s2"},
	})
	if !strings.Contains(retryMsg, "local queue ready") || strings.HasPrefix(retryMsg, "queued locally") {
		t.Fatalf("hub-skipped retry summary should say local queue ready: %s", retryMsg)
	}
}

func TestSanitizeGUIMaclawAppSkillName(t *testing.T) {
	if got := sanitizeGUIMaclawAppSkillName(" Invoice One Click "); got != "invoice-one-click" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeGUIMaclawAppSkillName("@@@"); got != "maclaw-app" {
		t.Fatalf("empty sanitize got %q", got)
	}
}

func TestResolveMaclawAppSubmissionIDAfterTargets(t *testing.T) {
	// Without a live queue, resolver falls back to the original id.
	var a *App
	got := a.resolveMaclawAppSubmissionIDAfterTargets("local-1", map[string]any{
		"enterprise_hub_pack": map[string]any{
			"ok":     true,
			"result": map[string]any{"submission_id": "hub-1"},
		},
	})
	if got != "local-1" {
		t.Fatalf("nil app should keep original id, got %q", got)
	}

	// When hub result lacks a findable record, keep original.
	app := &App{}
	got = app.resolveMaclawAppSubmissionIDAfterTargets("local-2", map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": false, "error": "offline"},
	})
	if got != "local-2" {
		t.Fatalf("failed hub should keep original id, got %q", got)
	}
}

func TestMaclawAppSubmissionLocalMap(t *testing.T) {
	if got := maclawAppSubmissionLocalMap(nil); len(got) != 0 {
		t.Fatalf("nil record map = %#v", got)
	}
	rec := &maclawAppSubmissionRecord{
		SubmissionID:    "sub-1",
		Status:          "submitted",
		Channel:         "local",
		SubmittedAt:     "2026-01-01T00:00:00Z",
		PackageSHA:      "abc",
		HubCapabilityID: "cap-1",
		Message:         "queued",
	}
	got := maclawAppSubmissionLocalMap(rec)
	if got["submission_id"] != "sub-1" || got["channel"] != "local" || got["package_sha256"] != "abc" {
		t.Fatalf("map = %#v", got)
	}
}

func TestFirstMaclawAppEntryMapsPackAndSingle(t *testing.T) {
	pack := map[string]any{
		"schema": "maclaw.app.pack.v1",
		"apps": []any{
			map[string]any{
				"schema": "maclaw.app.v1",
				"app":    map[string]any{"id": "a1", "name": "A"},
			},
		},
	}
	entry, app := firstMaclawAppEntryMaps(pack)
	if app == nil || stringFromAnyMap(app, "id") != "a1" {
		t.Fatalf("pack app = %#v entry=%#v", app, entry)
	}
	single := map[string]any{
		"schema": "maclaw.app.v1",
		"app":    map[string]any{"id": "s1", "name": "S"},
	}
	_, app2 := firstMaclawAppEntryMaps(single)
	if stringFromAnyMap(app2, "id") != "s1" {
		t.Fatalf("single app id = %#v", app2)
	}
}

func readTestZip(t *testing.T, path string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read %s: %v", f.Name, err)
		}
		out[filepath.ToSlash(f.Name)] = string(data)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("zip missing on disk: %v", err)
	}
	return out
}

func keysOfMap(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
