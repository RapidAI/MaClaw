package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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
		"enterprise_hub_pack": map[string]any{"ok": false, "error": "hub offline", "error_code": "network"},
		"skill_market":        map[string]any{"ok": false, "error": "remote_email is not configured", "error_code": "email_not_configured"},
	})
	if !strings.Contains(partialMsg, "network") && !strings.Contains(partialMsg, "hub offline") {
		t.Fatalf("unexpected hub partial summary: %s", partialMsg)
	}
	if !strings.Contains(partialMsg, "remote_email") && !strings.Contains(partialMsg, "enrollment") {
		t.Fatalf("unexpected skill market partial summary: %s", partialMsg)
	}
	gateMsg := summarizeMaclawAppOneClickPublish(map[string]any{
		"enterprise_hub_pack": map[string]any{
			"ok": false, "error_code": "dep_not_published",
			"error": `cannot upload App to Hub: skill dependency "x" is neither bundled`,
		},
		"skill_market": map[string]any{"ok": true, "submission_id": "s", "stamped_deps": 1},
	})
	if !strings.Contains(gateMsg, "bundle") || !strings.Contains(gateMsg, "stamped") {
		t.Fatalf("gate/stamp summary: %s", gateMsg)
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

func TestApplyMaclawAppResolvedDepsRefreshesDependencyVerificationInstallRefs(t *testing.T) {
	pkg := map[string]any{
		"schema": "maclaw.app.pack.v1",
		"apps": []any{map[string]any{
			"app": map[string]any{
				"id": "pdf-translator",
				"governance": map[string]any{
					"dependencyVerification": map[string]any{
						"skills": []any{map[string]any{"id": "paper_pdf_translator", "required": true}},
					},
				},
			},
		}},
	}
	updated := applyMaclawAppResolvedDepsToPackage(pkg, []map[string]any{{
		"id":                 "paper_pdf_translator",
		"install_ref":        "hub-skill-123",
		"source":             "skillmarket",
		"install_ref_kind":   "skillmarket",
		"install_ref_target": "hub-skill-123",
		"app_ids":            []string{"pdf-translator"},
	}})
	entry := anyMap(anySlice(updated["apps"])[0])
	app := anyMap(entry["app"])
	verification := anyMap(anyMap(app["governance"])["dependencyVerification"])
	skill := anyMap(anySlice(verification["skills"])[0])
	if got := stringFromAny(skill["install_ref"]); got != "hub-skill-123" {
		t.Fatalf("dependency verification install_ref = %q, want hub-skill-123; payload=%#v", got, updated)
	}
	if got := stringFromAny(skill["install_ref_kind"]); got != "skillmarket" {
		t.Fatalf("dependency verification install_ref_kind = %q, want skillmarket", got)
	}
}

func TestMaclawAppOneClickErrorDetailFormatsHubReviewIssue(t *testing.T) {
	err := fmt.Errorf(`submit failed (400): {"code":"MACLAW_APP_PACKAGE_NOT_READY","error":{"code":"MACLAW_APP_PACKAGE_NOT_READY","message":"package not ready","issues":[{"path":"apps[0].app.governance.dependencyVerification.skills[0].install_ref","message":"verified required dependency must include install_ref","suggestion":"publish the dependency Skill to Hub or SkillMarket and regenerate dependency verification"}]}}`)
	target := maclawAppOneClickTargetFail(err)
	detail := anyMap(target["error_detail"])
	if got := stringFromAny(detail["code"]); got != "MACLAW_APP_PACKAGE_NOT_READY" {
		t.Fatalf("error code = %q", got)
	}
	hint := maclawAppOneClickHumanizeError(target)
	if !strings.Contains(hint, "missing its install reference") || !strings.Contains(hint, "regenerate dependency verification") {
		t.Fatalf("humanized hint = %q", hint)
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

func TestSummarizeMaclawAppOneClickPublishSkillMarketSkipped(t *testing.T) {
	msg := summarizeMaclawAppOneClickPublish(map[string]any{
		"enterprise_hub_pack": map[string]any{"ok": true, "retried_after_stamp": true},
		"skill_market": map[string]any{
			"ok": true, "skipped": true, "reason": "primary_skill_already_published", "stamped_deps": 2,
		},
	})
	if !strings.Contains(msg, "skill market skipped") || !strings.Contains(msg, "primary_skill_already_published") {
		t.Fatalf("skip summary: %s", msg)
	}
	if !strings.Contains(msg, "after dep stamp retry") {
		t.Fatalf("hub retry summary: %s", msg)
	}
	if !strings.Contains(msg, "stamped 2") {
		t.Fatalf("stamped summary: %s", msg)
	}
}

func TestMaclawAppOneClickCanSkipSkillMarket(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{testHomeDir: tmpHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:       "paper_pdf_translator",
		SkillDir:   skillDir,
		Status:     "active",
		Source:     "skillmarket",
		HubSkillID: "hub-already",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	pkg := `{"schema":"maclaw.app.pack.v1","apps":[{"app":{"id":"a1","binding":{"appSkill":{"id":"paper_pdf_translator"}}}}]}`
	if !app.maclawAppOneClickCanSkipSkillMarket(pkg) {
		t.Fatal("expected skip when primary skill has HubSkillID")
	}

	cfg.NLSkills[0].HubSkillID = ""
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// ListNLSkills may cache; clear executor cache.
	if app.skillExecutor != nil {
		app.skillExecutor.clearSkillListCache()
	}
	if app.maclawAppOneClickCanSkipSkillMarket(pkg) {
		t.Fatal("expected no skip without HubSkillID")
	}
	// No bound skill id → materialize path still runs (do not skip).
	if app.maclawAppOneClickCanSkipSkillMarket(`{"schema":"maclaw.app.pack.v1","apps":[{"app":{"id":"no-skill"}}]}`) {
		t.Fatal("empty primary skill must not skip skill market (materialize upload)")
	}
}

func TestPreflightMaclawAppOneClickPublishReportsConfigAndDeps(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	// No email / hub market → warnings, but local still ready for a minimal pack
	// that may fail readiness review — use a pack with governance like other tests.
	pkg := `{
  "schema": "maclaw.app.pack.v1",
  "privateMarker": "x_maclaw_apps",
  "apps": [{
    "schema": "maclaw.app.v1",
    "privateMarker": "x_maclaw_apps",
    "app": {
      "id": "preflight-app",
      "name": "Preflight",
      "kind": "tool_app",
      "binding": {
        "appSkill": {"id": "missing-skill", "source": "local", "version": "1.0.0"}
      },
      "dependencies": {"skills": [
        {"id": "missing-skill", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "local"}
      ]}
    }
  }]
}`
	out, err := app.PreflightMaclawAppOneClickPublish(pkg)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if out["schema"] != "maclaw.app.one_click_preflight.v1" {
		t.Fatalf("schema = %#v", out["schema"])
	}
	// missing skill + no config should not ready_for_hub_pack
	if out["ready_for_hub_pack"] == true {
		t.Fatalf("expected hub pack not ready: %#v", out)
	}
	// skill market email warning expected
	foundEmail := false
	for _, c := range preflightCheckMaps(out["checks"]) {
		if c["id"] == "skill_market_email" && c["ok"] == false {
			foundEmail = true
		}
	}
	if !foundEmail {
		t.Fatalf("expected skill_market_email warning in checks: %#v", out["checks"])
	}

	// With email + hub market + published skill, hub pack becomes ready when dep is published.
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "missing-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# missing-skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RemoteEmail = "user@example.com"
	cfg.RemoteMachineID = "m1"
	cfg.RemoteViewerToken = "viewer-token-16chars"
	cfg.RemoteHubURL = "https://hub.example.com"
	cfg.RemoteHubCenterURL = "https://hubs.example.com"
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name: "missing-skill", SkillDir: skillDir, Status: "active",
		Source: "skillmarket", HubSkillID: "hub-missing",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// Bundle dep so readiness/gate can pass without full governance evidence.
	pkgBundled := `{
  "schema": "maclaw.app.pack.v1",
  "privateMarker": "x_maclaw_apps",
  "bundled_dependencies": {"skills":[{"name":"missing-skill","files":{"skill.md":"# missing-skill\n"}}]},
  "apps": [{
    "schema": "maclaw.app.v1",
    "privateMarker": "x_maclaw_apps",
    "app": {
      "id": "preflight-app",
      "name": "Preflight",
      "kind": "tool_app",
      "binding": {"appSkill": {"id": "missing-skill", "source": "local", "version": "1.0.0"}},
      "dependencies": {"skills": [
        {"id": "missing-skill", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "local"}
      ]}
    }
  }]
}`
	out2, err := app.PreflightMaclawAppOneClickPublish(pkgBundled)
	if err != nil {
		t.Fatalf("Preflight bundled: %v", err)
	}
	// dependencies check should be ok when bundled
	depOK := false
	for _, c := range preflightCheckMaps(out2["checks"]) {
		if c["id"] == "dependencies" && c["ok"] == true {
			depOK = true
		}
	}
	if !depOK {
		t.Fatalf("bundled deps should pass dependencies check: %#v", out2["checks"])
	}
	// Config checks should be green even if package_ready is still blocked by missing testEvidence.
	emailOK, enrollOK := false, false
	for _, c := range preflightCheckMaps(out2["checks"]) {
		if c["id"] == "skill_market_email" && c["ok"] == true {
			emailOK = true
		}
		if c["id"] == "hub_enrollment" && c["ok"] == true {
			enrollOK = true
		}
	}
	if !emailOK || !enrollOK {
		t.Fatalf("expected email+enrollment ok after SaveConfig: %#v", out2["checks"])
	}
}

func preflightCheckMaps(raw any) []map[string]any {
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func TestPreflightMaclawAppOneClickPublishEmptyPackage(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, err := app.PreflightMaclawAppOneClickPublish("  ")
	if err == nil {
		t.Fatal("expected error for empty package")
	}
}

func TestMaclawAppHubTLSPreflightCachedSharesShortLivedResult(t *testing.T) {
	maclawAppHubTLSPreflightMu.Lock()
	oldCheck := maclawAppHubTLSCheck
	oldCache := maclawAppHubTLSPreflightCache
	maclawAppHubTLSPreflightCache = map[string]*maclawAppHubTLSPreflightCacheEntry{}
	maclawAppHubTLSPreflightMu.Unlock()
	t.Cleanup(func() {
		maclawAppHubTLSPreflightMu.Lock()
		maclawAppHubTLSCheck = oldCheck
		maclawAppHubTLSPreflightCache = oldCache
		maclawAppHubTLSPreflightMu.Unlock()
	})

	calls := 0
	maclawAppHubTLSCheck = func(baseURL string) error {
		calls++
		if baseURL != "https://hub.example.test" {
			t.Fatalf("TLS check URL = %q", baseURL)
		}
		return fmt.Errorf("x509: certificate has expired")
	}

	for i := 0; i < 2; i++ {
		if err := maclawAppHubTLSPreflightCached("https://hub.example.test"); err == nil {
			t.Fatal("expected cached TLS check error")
		}
	}
	if calls != 1 {
		t.Fatalf("TLS check calls = %d, want 1", calls)
	}
}

func TestClassifyMaclawAppOneClickError(t *testing.T) {
	cases := []struct {
		err  string
		code string
	}{
		{`cannot upload App to Hub: skill dependency "x" is neither bundled in the package nor published`, "dep_not_published"},
		{"SkillMarket 认证失败或已过期，请重新登录", "auth_expired"},
		{"enterprise Hub marketplace URL or auth token is not configured", "hub_not_configured"},
		{"remote_email is not configured (required for SkillMarket upload)", "email_not_configured"},
		{"no reachable hubcenter", "network"},
		{"Post \"https://hub.example.com/api/submit\": tls: failed to verify certificate: x509: certificate has expired or is not yet valid", "hub_tls_certificate_invalid"},
		{"package fingerprint mismatch", "fingerprint_mismatch"},
		{"package payload is empty", "empty_package"},
		{"something else", "target_failed"},
	}
	for _, tc := range cases {
		got := classifyMaclawAppOneClickError(fmt.Errorf("%s", tc.err))
		if got != tc.code {
			t.Fatalf("classify(%q) = %q, want %q", tc.err, got, tc.code)
		}
	}
	if code := classifyMaclawAppOneClickError(nil); code != "" {
		t.Fatalf("nil err code = %q", code)
	}
	fail := maclawAppOneClickTargetFail(fmt.Errorf("no reachable hubcenter"))
	if fail["ok"] != false || fail["error_code"] != "network" {
		t.Fatalf("target fail map = %#v", fail)
	}
}

func TestMaclawAppOneClickHumanizeTLSCertificateError(t *testing.T) {
	target := maclawAppOneClickTargetFail(fmt.Errorf("tls: failed to verify certificate: x509: certificate has expired or is not yet valid"))
	if got := target["error_code"]; got != "hub_tls_certificate_invalid" {
		t.Fatalf("error code = %#v", got)
	}
	hint := maclawAppOneClickHumanizeError(target)
	if !strings.Contains(hint, "TLS certificate") || !strings.Contains(hint, "server clock") {
		t.Fatalf("TLS hint = %q", hint)
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
	// In-memory stamp path may store apps as []map[string]any.
	packMaps := map[string]any{
		"schema": "maclaw.app.pack.v1",
		"apps": []map[string]any{
			{
				"schema": "maclaw.app.v1",
				"app":    map[string]any{"id": "a2", "name": "B"},
			},
		},
	}
	_, appMapSlice := firstMaclawAppEntryMaps(packMaps)
	if appMapSlice == nil || stringFromAnyMap(appMapSlice, "id") != "a2" {
		t.Fatalf("[]map apps app = %#v", appMapSlice)
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
