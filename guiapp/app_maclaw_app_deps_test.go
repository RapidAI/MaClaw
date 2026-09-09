package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestDependencyVerificationReviewFlagsInstallTraceFailure(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID: "dependency-install-trace-failure",
		App: map[string]any{
			"id":   "dependency-install-trace-failure",
			"kind": "enterprise_normal_app",
			"binding": map[string]any{
				"appSkill": map[string]any{"id": "customer-renewal-skill", "source": "hub"},
			},
		},
	}
	governance := map[string]any{
		"dependencyVerification": map[string]any{
			"schema":                "maclaw.app.install_plan.v1",
			"hasBlockingDependency": false,
			"hasMissingRequired":    false,
			"dependencies": []any{
				map[string]any{
					"id":                  "customer-renewal-skill",
					"kind":                "runtime_skill",
					"required":            true,
					"installed":           true,
					"health":              "ready",
					"action":              "skip",
					"preflight_status":    "ready",
					"integrity_status":    "failed",
					"integrity_code":      "package_integrity_failed",
					"install_error_code":  "package_integrity_failed",
					"install_error_stage": "skillhub_download",
				},
			},
		},
	}

	issue := maclawAppDependencyVerificationReviewIssue(entry, governance, "apps[0].app")
	if issue == nil || issue.Path != "apps[0].app.governance.dependencyVerification" || !strings.Contains(issue.Message, "required dependency is missing or blocked") {
		t.Fatalf("expected dependency install trace failure to block review: %#v", issue)
	}
}

func TestMaclawAppPlanDependencyMatchesWrappedAppIDs(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{
		{ID: "market-skill", Required: true, Installed: true, Health: "ready", Action: "skip", AppIDs: []string{"market-customer-console"}},
		{ID: "datasrv-skill", Required: true, Installed: false, Health: "missing", Action: "blocked", AppIDs: []string{"datasrv-installed-expense-approval"}},
	}
	if got := cloneMaclawAppPlanDependenciesForApp(deps, "customer-console"); len(got) != 1 || got[0].ID != "market-skill" {
		t.Fatalf("expected market wrapped dependency to match canonical app id: %#v", got)
	}
	if !hasMissingMaclawAppRequiredDependencyForApp(deps, "expense-approval") {
		t.Fatalf("expected datasrv wrapped dependency to match canonical app id")
	}
	if !hasBlockingMaclawAppRequiredDependencyForApp(deps, "expense-approval") {
		t.Fatalf("expected datasrv wrapped dependency to count as blocking for canonical app id")
	}
	if got := cloneMaclawAppPlanDependenciesForApp(deps, "unrelated-app"); len(got) != 0 {
		t.Fatalf("expected unrelated app id to stay isolated: %#v", got)
	}
}

func TestMaclawAppDependencyUpdatePreservesSkillMarketSource(t *testing.T) {
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

	const marketID = "market-dep-uuid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{}, "nodes": []any{}})
		case "/api/v1/skills/" + marketID + "/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"market_dep","name":"market_dep","description":"new market dep","version":"2.0.0","trust_level":"trusted","steps":[{"action":"bash","params":{"command":"echo market"},"on_error":"stop"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "market_dep")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# market_dep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  server.URL,
		RemoteHubCenterURLs: []string{server.URL},
		NLSkills: []corelib.NLSkillEntry{{
			Name:       "market_dep",
			SkillDir:   skillDir,
			Status:     "active",
			Source:     "skillmarket",
			HubSkillID: marketID,
			HubVersion: "1.0.0",
		}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	dep := maclawAppInstallPlanDependency{
		ID:               "market_dep",
		Source:           "hub",
		InstallRefKind:   "skillmarket",
		InstallRefTarget: marketID,
		InstalledName:    "market_dep",
		InstalledDir:     skillDir,
	}
	if err := os.MkdirAll(skillDir+".prev", 0o755); err != nil {
		t.Fatalf("create stale dependency backup: %v", err)
	}
	if updated, err := app.updateInstalledMaclawAppDependency(&dep); err == nil || !updated || !strings.Contains(err.Error(), "stale previous backup") {
		t.Fatalf("stale dependency backup should block update, updated=%v err=%v", updated, err)
	}
	if err := os.RemoveAll(skillDir + ".prev"); err != nil {
		t.Fatalf("remove stale dependency backup: %v", err)
	}
	updated, err := app.updateInstalledMaclawAppDependency(&dep)
	if err != nil || !updated {
		t.Fatalf("updateInstalledMaclawAppDependency() updated=%v err=%v", updated, err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].Source != "skillmarket" || cfg.NLSkills[0].HubSkillID != marketID || cfg.NLSkills[0].HubVersion != "2.0.0" {
		t.Fatalf("SkillMarket dependency update should preserve registered source and UUID: %#v", cfg.NLSkills)
	}
}

func TestMaclawAppDependencyUpdateClearsCompensationAfterRollback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	oldBase := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(filepath.Join(tmpHome, ".maclaw"))
	t.Cleanup(func() { corelib.SetMaclawBaseDir(oldBase) })
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	t.Cleanup(func() {
		defaultRemoteCenter := originalDefaultCenter
		defaultRemoteHubCenterURL = defaultRemoteCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
	})
	const marketID = "market-dep-rollback"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": []string{}, "nodes": []any{}})
		case "/api/v1/skills/" + marketID + "/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"rollback_dep","name":"rollback_dep","description":"new","version":"2.0.0","steps":[{"action":"bash","params":{"command":"echo rollback"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "rollback_dep")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# rollback_dep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: server.URL, RemoteHubCenterURLs: []string{server.URL},
		NLSkills: []corelib.NLSkillEntry{{Name: "rollback_dep", SkillDir: skillDir, Status: "active", Source: "skillmarket", HubSkillID: marketID, HubVersion: "1.0.0"}},
	}); err != nil {
		t.Fatal(err)
	}
	refreshCalls := 0
	app.toolRouter = NewToolRouter(nil)
	app.toolRouter.refreshSkillIndexOverride = func() error {
		refreshCalls++
		if refreshCalls == 1 {
			return os.ErrPermission
		}
		return nil
	}
	dep := maclawAppInstallPlanDependency{ID: "rollback_dep", Source: "hub", InstallRefKind: "skillmarket", InstallRefTarget: marketID, InstalledName: "rollback_dep", InstalledDir: skillDir}
	if updated, err := app.updateInstalledMaclawAppDependency(&dep); err == nil || !updated || !strings.Contains(err.Error(), "index_refresh_failed") {
		t.Fatalf("dependency update = updated:%v err:%v, want checked-index failure", updated, err)
	}
	if summaries, err := cskill.ListEvolutionCompensationSummaries(); err != nil {
		t.Fatal(err)
	} else if len(summaries) != 0 {
		t.Fatalf("successful rollback left compensation records: %#v", summaries)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("original dependency directory was not restored: %v", err)
	}
}

func TestMaclawAppDependencyUpdateBlocksNonCanonicalDirectory(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	customDir := filepath.Join(tmpHome, "custom-skills", "legacy-directory")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		t.Fatalf("create custom dependency dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "SKILL.md"), []byte("legacy-package"), 0o644); err != nil {
		t.Fatalf("write custom dependency package: %v", err)
	}
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{
		Name: "canonical-name", SkillDir: customDir, Status: "active",
		Source: "skillmarket", HubSkillID: "legacy-id", HubVersion: "1.0.0",
	}}}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	app.ensureInteractionInfra()
	dep := maclawAppInstallPlanDependency{
		ID: "canonical-name", Source: "hub", InstallRefKind: "skillmarket",
		InstallRefTarget: "legacy-id", InstalledName: "canonical-name", InstalledDir: customDir,
	}
	updated, err := app.updateInstalledMaclawAppDependency(&dep)
	if !updated || err == nil || !strings.Contains(err.Error(), "non-canonical installation directory") {
		t.Fatalf("dependency update = updated:%v err:%v, want non-canonical directory rejection", updated, err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.NLSkills) != 1 || cfg.NLSkills[0].HubVersion != "1.0.0" {
		t.Fatalf("blocked non-canonical update changed config: %#v", cfg.NLSkills)
	}
	data, err := os.ReadFile(filepath.Join(customDir, "SKILL.md"))
	if err != nil || string(data) != "legacy-package" {
		t.Fatalf("blocked non-canonical update changed package: data=%q err=%v", data, err)
	}
}

func TestMaclawAppResolvedDependenciesPreserveCanonicalMetadata(t *testing.T) {
	enriched := []maclawAppInstallPlanDependency{{
		ID:                 "Approval Workflow",
		InstallRef:         "approval-flow",
		Source:             "hub",
		Kind:               "workflow_skill",
		Required:           true,
		Version:            "2.0.0",
		CanonicalID:        "approval-flow",
		Aliases:            []string{"ApprovalFlow", "approval-flow-local"},
		InstallRefKind:     "skillmarket",
		InstallRefTarget:   "approval-flow",
		InstallRefVersion:  "2.0.0",
		PackageSHA256:      "sha256-approval-flow",
		PackageSignature:   "sig-approval-flow",
		PackageDownloadURL: "https://hub.example/approval-flow/download",
	}}
	serialized := maclawAppSerializableResolvedDeps(enriched)
	if len(serialized) != 1 || serialized[0]["canonical_id"] != "approval-flow" {
		t.Fatalf("resolved dependency should serialize canonical_id: %#v", serialized)
	}
	aliases, ok := serialized[0]["aliases"].([]string)
	if !ok || len(aliases) != 2 || aliases[0] != "ApprovalFlow" || aliases[1] != "approval-flow-local" {
		t.Fatalf("resolved dependency should serialize aliases: %#v", serialized)
	}
	if serialized[0]["install_ref_kind"] != "skillmarket" || serialized[0]["install_ref_target"] != "approval-flow" || serialized[0]["install_ref_version"] != "2.0.0" {
		t.Fatalf("resolved dependency should serialize install_ref metadata: %#v", serialized[0])
	}
	if serialized[0]["package_sha256"] != "sha256-approval-flow" || serialized[0]["package_signature"] != "sig-approval-flow" || serialized[0]["package_download_url"] != "https://hub.example/approval-flow/download" {
		t.Fatalf("resolved dependency should serialize package integrity metadata: %#v", serialized[0])
	}

	deps := []maclawAppInstallPlanDependency{{
		ID:       "Approval Workflow",
		Kind:     "workflow_skill",
		Required: true,
		Source:   "local",
	}}
	applyResolvedDependenciesToPlan(deps, map[string]any{"resolved_dependencies": []interface{}{
		map[string]interface{}{
			"id":                   "Approval Workflow",
			"install_ref":          "approval-flow",
			"source":               "hub",
			"version":              "2.0.0",
			"canonical_id":         "approval-flow",
			"aliases":              []interface{}{"ApprovalFlow", "approval-flow-local"},
			"install_ref_kind":     "skillmarket",
			"install_ref_target":   "approval-flow",
			"install_ref_version":  "2.0.0",
			"package_sha256":       "sha256-approval-flow",
			"package_signature":    "sig-approval-flow",
			"package_download_url": "https://hub.example/approval-flow/download",
		},
	}})
	if deps[0].InstallRef != "approval-flow" || deps[0].Source != "hub" || deps[0].Version != "2.0.0" || deps[0].CanonicalID != "approval-flow" {
		t.Fatalf("resolved dependency should apply install and canonical metadata: %#v", deps[0])
	}
	if len(deps[0].Aliases) != 2 || deps[0].Aliases[0] != "ApprovalFlow" || deps[0].Aliases[1] != "approval-flow-local" {
		t.Fatalf("resolved dependency should apply aliases: %#v", deps[0])
	}
	if deps[0].InstallRefKind != "skillmarket" || deps[0].InstallRefTarget != "approval-flow" || deps[0].InstallRefVersion != "2.0.0" {
		t.Fatalf("resolved dependency should apply install_ref metadata: %#v", deps[0])
	}
	if deps[0].PackageSHA256 != "sha256-approval-flow" || deps[0].PackageSignature != "sig-approval-flow" || deps[0].PackageDownloadURL != "https://hub.example/approval-flow/download" {
		t.Fatalf("resolved dependency should apply package integrity metadata: %#v", deps[0])
	}
}

func TestMaclawAppResolvedDependenciesRespectAppIDScopeForSameDependencyID(t *testing.T) {
	deps := []maclawAppInstallPlanDependency{
		{
			ID:               "RapidOCR",
			Kind:             "runtime_skill",
			Required:         true,
			Source:           "hub",
			InstallRef:       "rapidocr",
			InstallRefKind:   "hub",
			InstallRefTarget: "rapidocr",
			AppIDs:           []string{"public-ocr-app"},
		},
		{
			ID:               "RapidOCR",
			Kind:             "runtime_skill",
			Required:         true,
			Source:           "hub",
			InstallRef:       "rapidocr",
			InstallRefKind:   "hub",
			InstallRefTarget: "rapidocr",
			AppIDs:           []string{"private-ocr-app"},
		},
	}
	applyResolvedDependenciesToPlan(deps, map[string]any{"resolved_dependencies": []interface{}{
		map[string]interface{}{
			"id":                  "RapidOCR",
			"app_ids":             []interface{}{"public-ocr-app"},
			"install_ref":         "public-market-uuid",
			"source":              "skillmarket",
			"install_ref_kind":    "skillmarket",
			"install_ref_target":  "public-market-uuid",
			"install_ref_version": "10",
		},
		map[string]interface{}{
			"id":                  "RapidOCR",
			"app_ids":             []interface{}{"private-ocr-app"},
			"install_ref":         "private-hub-uuid",
			"source":              "hub",
			"install_ref_kind":    "hub",
			"install_ref_target":  "private-hub-uuid",
			"install_ref_version": "10",
		},
	}})
	if deps[0].InstallRef != "public-market-uuid" || deps[0].Source != "skillmarket" || deps[0].InstallRefKind != "skillmarket" {
		t.Fatalf("public app dependency should use public scoped resolved ref: %#v", deps[0])
	}
	if deps[1].InstallRef != "private-hub-uuid" || deps[1].Source != "hub" || deps[1].InstallRefKind != "hub" {
		t.Fatalf("private app dependency should use private scoped resolved ref: %#v", deps[1])
	}
}

func TestEnrichDependenciesWithHubSkillIDMatchesAlias(t *testing.T) {
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
		HubSkillID: "hub-uuid-alias",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// Dep id is an alias form; enrich must still find HubSkillID via candidate keys.
	deps := []maclawAppInstallPlanDependency{{
		ID:       "Paper PDF Translator",
		Required: true,
		Source:   "local",
		Aliases:  []string{"paper_pdf_translator"},
	}}
	app.enrichDependenciesWithHubSkillID(deps)
	if deps[0].InstallRef != "hub-uuid-alias" {
		t.Fatalf("InstallRef = %q want hub-uuid-alias (alias match)", deps[0].InstallRef)
	}
	if deps[0].Source != "skillmarket" {
		t.Fatalf("Source = %q want skillmarket", deps[0].Source)
	}
	if deps[0].InstallRefKind != "skillmarket" {
		t.Fatalf("InstallRefKind = %q want skillmarket", deps[0].InstallRefKind)
	}
}

func TestMaclawAppHubCenterDependencySourceUsesSkillMarketInstaller(t *testing.T) {
	source, ok := maclawAppInstallSkillSource("hubcenter")
	if !ok || source != "skillmarket" {
		t.Fatalf("hubcenter dependency source should use SkillMarket installer, got source=%q ok=%v", source, ok)
	}
	source, ok = maclawAppDependencyInstallerSource(maclawAppInstallPlanDependency{ID: "rapidocr", Source: "hub", InstallRefKind: "skillmarket"})
	if !ok || source != "skillmarket" {
		t.Fatalf("SkillMarket install_ref should override declared hub source for installer, got source=%q ok=%v", source, ok)
	}
	if stage := maclawAppDependencyInstallStage("hubcenter"); stage != "skillmarket_download" {
		t.Fatalf("hubcenter dependency install stage should be SkillMarket, got %q", stage)
	}
	dep := maclawAppInstallPlanDependency{
		ID:         "hubcenter-paper",
		Source:     "hubcenter",
		InstallRef: "hubcenter://skills/hubcenter-paper@v2",
	}
	kind, target, version, status, message := maclawAppParseDependencyInstallRef(dep)
	if kind != "hubcenter" || target != "hubcenter-paper" || version != "v2" || status != "ok" {
		t.Fatalf("hubcenter install_ref should resolve, got kind=%q target=%q version=%q status=%q message=%q", kind, target, version, status, message)
	}
	dep.InstallRefKind = kind
	if !maclawAppDependencySupportsPublicMarketPreflight(dep) {
		t.Fatalf("hubcenter dependency should use public SkillMarket preflight: %#v", dep)
	}
	if !maclawAppDependencySupportsPublicMarketPreflight(maclawAppInstallPlanDependency{ID: "hubcenter-paper", InstallRefKind: "hubcenter"}) {
		t.Fatalf("hubcenter install_ref kind should use public SkillMarket preflight")
	}
}

func TestMaclawAppInstallEvidenceGeneratesDependencyVerification(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID:   "expense-approval",
		Name: "Expense Approval",
		Kind: "enterprise_approval_app",
		Entry: map[string]any{
			"schema": "maclaw.app.v1",
		},
		App: map[string]any{
			"id":   "expense-approval",
			"name": "Expense Approval",
			"kind": "enterprise_approval_app",
			"binding": map[string]any{
				"workflow": map[string]any{
					"schema":        "maclaw.app.workflow.v1",
					"submitNode":    "expense.submit",
					"approvalNode":  "expense.manager_review",
					"resultNode":    "expense.result",
					"attentionNode": "expense.attention",
					"statusMapping": map[string]any{"pending": "approval_pending", "approved": "approved", "rejected": "rejected", "attention": "attention"},
				},
			},
		},
	}
	dependencies := []maclawAppInstallPlanDependency{
		{ID: "expense-workflow", Kind: "workflow_skill", Required: true, AppIDs: []string{"expense-approval"}, Installed: true, Health: "ready", Action: "skip"},
		{ID: "expense-export", Kind: "skill", Required: false, AppIDs: []string{"expense-approval"}, Installed: false, Health: "missing", Action: "optional_missing"},
	}

	evidence := maclawAppInstallEvidenceByApp([]parsedMaclawAppEntry{entry}, dependencies, nil)
	appEvidence, ok := evidence["expense-approval"].(map[string]interface{})
	if !ok {
		t.Fatalf("install evidence should include app id: %#v", evidence)
	}
	verification := anyMap(appEvidence["dependency_verification"])
	if verification == nil || maclawAppStringValue(verification, "schema") != "maclaw.app.install_plan.v1" || verification["app_count"] != 1 || verification["dependency_count"] != 2 || verification["has_missing_required"] != false || verification["has_blocking_dependency"] != false {
		t.Fatalf("install evidence should generate dependency verification: %#v", appEvidence["dependency_verification"])
	}
	if _, err := time.Parse(time.RFC3339, maclawAppStringValue(verification, "verified_at")); err != nil {
		t.Fatalf("generated dependency verification should include install-time verified_at: %#v", verification)
	}
	verifiedDependencies, ok := verification["dependencies"].([]maclawAppInstallPlanDependency)
	if !ok || len(verifiedDependencies) != 2 || verifiedDependencies[0].ID != "expense-workflow" || !verifiedDependencies[0].Installed || verifiedDependencies[1].Action != "optional_missing" {
		t.Fatalf("generated dependency verification should carry per-app dependencies: %#v", verification["dependencies"])
	}
	workflowMapping := anyMap(appEvidence["workflow_mapping"])
	if workflowMapping == nil || workflowMapping["schema"] != "maclaw.app.workflow.v1" || workflowMapping["approvalNode"] != "expense.manager_review" || workflowMapping["resultNode"] != "expense.result" {
		t.Fatalf("install evidence should preserve approval workflow mapping: %#v", appEvidence["workflow_mapping"])
	}
	statusMapping := anyMap(workflowMapping["statusMapping"])
	if statusMapping == nil || statusMapping["pending"] != "approval_pending" || statusMapping["attention"] != "attention" {
		t.Fatalf("install evidence should preserve workflow status mapping: %#v", workflowMapping)
	}
}
