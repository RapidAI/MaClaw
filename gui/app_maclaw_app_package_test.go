package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMaclawAppDependencyIsBundled(t *testing.T) {
	dep := maclawAppInstallPlanDependency{ID: "paper_pdf_translator", Required: true, Source: "local"}
	bundled := maclawAppBundledDependencies{
		Schema: "maclaw.app.bundled_dependencies.v1",
		Skills: []maclawAppBundledSkillEntry{
			{
				StableID: "hub_skill:paper_pdf_translator",
				Name:     "paper_pdf_translator",
				Files:    map[string]string{"skill.md": "# paper_pdf_translator\n"},
			},
		},
	}
	if !maclawAppDependencyIsBundled(bundled, dep) {
		t.Fatal("expected matching bundled skill with files to satisfy dependency")
	}

	// Empty files must not count as bundled (receiver cannot install).
	emptyFiles := maclawAppBundledDependencies{
		Skills: []maclawAppBundledSkillEntry{{Name: "paper_pdf_translator"}},
	}
	if maclawAppDependencyIsBundled(emptyFiles, dep) {
		t.Fatal("empty files should not count as bundled")
	}

	// Unrelated skill name should not match.
	other := maclawAppBundledDependencies{
		Skills: []maclawAppBundledSkillEntry{
			{Name: "other_skill", Files: map[string]string{"skill.md": "# other\n"}},
		},
	}
	if maclawAppDependencyIsBundled(other, dep) {
		t.Fatal("unrelated skill should not match dependency")
	}
}

func TestMaclawAppDependencyHasPublishedHubSkillID(t *testing.T) {
	installed := map[string]NLSkillDefinition{
		"paper_pdf_translator": {Name: "paper_pdf_translator", HubSkillID: "hub-skill-abc"},
	}
	if !maclawAppDependencyHasPublishedHubSkillID(installed, maclawAppInstallPlanDependency{ID: "paper_pdf_translator"}) {
		t.Fatal("expected HubSkillID stamp to satisfy publish gate")
	}
	if maclawAppDependencyHasPublishedHubSkillID(installed, maclawAppInstallPlanDependency{ID: "missing-skill"}) {
		t.Fatal("missing skill should not satisfy gate")
	}
	noHub := map[string]NLSkillDefinition{
		"paper_pdf_translator": {Name: "paper_pdf_translator"},
	}
	if maclawAppDependencyHasPublishedHubSkillID(noHub, maclawAppInstallPlanDependency{ID: "paper_pdf_translator"}) {
		t.Fatal("installed local skill without HubSkillID should not satisfy gate")
	}
	// Match via InstalledName when ID differs from index key.
	if !maclawAppDependencyHasPublishedHubSkillID(installed, maclawAppInstallPlanDependency{
		ID:            "friendly-id",
		InstalledName: "paper_pdf_translator",
	}) {
		t.Fatal("InstalledName should resolve HubSkillID")
	}
}

func TestValidateAppDependenciesPublishedAcceptsBundled(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{ID: "paper_pdf_translator", Required: true, Source: "local"},
		},
	}
	pkg := map[string]any{
		"bundled_dependencies": map[string]any{
			"schema": "maclaw.app.bundled_dependencies.v1",
			"skills": []any{
				map[string]any{
					"stable_id": "hub_skill:paper_pdf_translator",
					"name":      "paper_pdf_translator",
					"files":     map[string]any{"skill.md": "# paper_pdf_translator\n"},
				},
			},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, pkg); err != nil {
		t.Fatalf("bundled required dependency should pass hub publish gate: %v", err)
	}
}

func TestValidateAppDependenciesPublishedAcceptsEntryScopedBundle(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{ID: "paper_pdf_translator", Required: true, Source: "local"},
		},
	}
	// Bundle may live on the app entry rather than pack root.
	pkg := map[string]any{
		"apps": []any{
			map[string]any{
				"bundled_dependencies": map[string]any{
					"skills": []any{
						map[string]any{
							"name":  "paper_pdf_translator",
							"files": map[string]any{"skill.md": "# paper\n"},
						},
					},
				},
			},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, pkg); err != nil {
		t.Fatalf("entry-scoped bundled dep should pass: %v", err)
	}
}

func TestValidateAppDependenciesPublishedRejectsUnpublishedLocal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{ID: "paper_pdf_translator", Required: true, Source: "local"},
		},
	}
	err := app.validateAppDependenciesPublished(plan, map[string]any{})
	if err == nil {
		t.Fatal("expected gate error for unpublished local skill without bundle")
	}
	if !strings.Contains(err.Error(), "paper_pdf_translator") {
		t.Fatalf("error should name dependency: %v", err)
	}
	if !strings.Contains(err.Error(), "neither bundled") {
		t.Fatalf("error should explain bundle/publish options: %v", err)
	}
}

func TestValidateAppDependenciesPublishedAcceptsRemoteInstallRef(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{
				ID:         "rapidocr",
				Required:   true,
				Source:     "skillmarket",
				InstallRef: "skillmarket://skills/rapidocr",
			},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, nil); err != nil {
		t.Fatalf("remote install_ref should pass without local HubSkillID: %v", err)
	}
}

func TestMaclawAppDependencyHasRemoteInstallRef(t *testing.T) {
	if !maclawAppDependencyHasRemoteInstallRef(maclawAppInstallPlanDependency{
		InstallRef: "hub-id", Source: "hub",
	}) {
		t.Fatal("source=hub should be remote")
	}
	if !maclawAppDependencyHasRemoteInstallRef(maclawAppInstallPlanDependency{
		InstallRef: "uuid-1", Source: "local", InstallRefKind: "skillmarket",
	}) {
		t.Fatal("install_ref_kind=skillmarket should be remote even when source=local")
	}
	if !maclawAppDependencyHasRemoteInstallRef(maclawAppInstallPlanDependency{
		InstallRef: "enterprise_hub:skill:paper_pdf_translator@abc", Source: "local",
	}) {
		t.Fatal("enterprise_hub: scheme should be remote")
	}
	if maclawAppDependencyHasRemoteInstallRef(maclawAppInstallPlanDependency{
		InstallRef: "paper_pdf_translator", Source: "local",
	}) {
		t.Fatal("plain local id must not count as remote install_ref")
	}
	if maclawAppDependencyHasRemoteInstallRef(maclawAppInstallPlanDependency{Source: "hub"}) {
		t.Fatal("empty install_ref is not remote")
	}
}

func TestValidateAppDependenciesPublishedAcceptsLocalSourceWithRemoteKind(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{
				ID:             "paper_pdf_translator",
				Required:       true,
				Source:         "local",
				InstallRef:     "paper_pdf_translator",
				InstallRefKind: "skillmarket",
			},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, nil); err != nil {
		t.Fatalf("local source + skillmarket install_ref_kind should pass: %v", err)
	}
}

func TestRefreshMaclawAppSubmissionPublishStampsAfterSkillUpload(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
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
		HubSkillID: "hub-paper-pdf-uuid",
		HubVersion: "1.0.0",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	pkg := map[string]any{
		"schema":        "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": []any{
			map[string]any{
				"schema":        "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": map[string]any{
					"id":   "app-pdf",
					"name": "PDF",
					"kind": "tool_app",
					"binding": map[string]any{
						"appSkill": map[string]any{"id": "paper_pdf_translator", "source": "local", "version": "1.0.0"},
					},
					"dependencies": map[string]any{
						"skills": []any{
							map[string]any{"id": "paper_pdf_translator", "version": "1.0.0", "kind": "app_skill", "required": true, "source": "local"},
						},
					},
				},
			},
		},
	}
	sha, size, err := maclawAppPackageFingerprint(pkg)
	if err != nil {
		t.Fatal(err)
	}
	const keepMessage = "queued locally; skill market upload ok (sub-1)"
	if err := app.appendMaclawAppSubmission(maclawAppSubmissionRecord{
		SubmissionID: "local-review-app-pdf-stamp",
		SubmittedAt:  "2026-07-20T00:00:00Z",
		Status:       "submitted",
		Channel:      "local",
		Package:      pkg,
		PackageSHA:   sha,
		PackageSize:  size,
		AppIDs:       []string{"app-pdf"},
		AppNames:     []string{"PDF"},
		Message:      keepMessage,
	}); err != nil {
		t.Fatalf("append submission: %v", err)
	}

	n, err := app.refreshMaclawAppSubmissionPublishStamps("local-review-app-pdf-stamp")
	if err != nil {
		t.Fatalf("refresh stamps: %v", err)
	}
	if n < 1 {
		t.Fatalf("expected at least 1 new stamp, got %d", n)
	}
	rec, err := app.GetMaclawAppPackageSubmission("local-review-app-pdf-stamp")
	if err != nil || rec == nil {
		t.Fatalf("get submission: %v %#v", err, rec)
	}
	if rec.Message != keepMessage {
		t.Fatalf("stamp must not clobber Message: got %q want %q", rec.Message, keepMessage)
	}
	if len(rec.Events) == 0 {
		t.Fatal("expected audit event for stamp")
	}
	resolved := anySlice(rec.Package["resolved_dependencies"])
	if len(resolved) == 0 {
		t.Fatalf("package missing resolved_dependencies: %#v", rec.Package)
	}
	dep := anyMap(resolved[0])
	if dep["install_ref"] != "hub-paper-pdf-uuid" {
		t.Fatalf("install_ref = %#v, want hub-paper-pdf-uuid", dep["install_ref"])
	}
	// Second call should be a no-op (no new stamps).
	n2, err := app.refreshMaclawAppSubmissionPublishStamps("local-review-app-pdf-stamp")
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second refresh stamped %d, want 0", n2)
	}
	rec2, _ := app.GetMaclawAppPackageSubmission("local-review-app-pdf-stamp")
	if rec2 == nil || rec2.Message != keepMessage {
		t.Fatalf("message should remain after no-op stamp: %#v", rec2)
	}
}

func TestValidateAppDependenciesPublishedSkipsOptionalDeps(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{ID: "optional-helper", Required: false, Source: "local"},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, map[string]any{}); err != nil {
		t.Fatalf("optional deps must not block hub pack: %v", err)
	}
}

func TestAppendMaclawAppSubmissionEventCapsHistory(t *testing.T) {
	var events []maclawAppSubmissionEvent
	for i := 0; i < maxMaclawAppSubmissionEvents+12; i++ {
		events = appendMaclawAppSubmissionEvent(events, maclawAppSubmissionEvent{
			At:      "t",
			Status:  "submitted",
			Message: fmt.Sprintf("evt-%d", i),
		})
	}
	if len(events) != maxMaclawAppSubmissionEvents {
		t.Fatalf("len(events)=%d want %d", len(events), maxMaclawAppSubmissionEvents)
	}
	if events[0].Message != fmt.Sprintf("evt-%d", 12) {
		t.Fatalf("oldest retained event = %q", events[0].Message)
	}
	if events[len(events)-1].Message != fmt.Sprintf("evt-%d", maxMaclawAppSubmissionEvents+11) {
		t.Fatalf("newest event = %q", events[len(events)-1].Message)
	}
}

func TestMaclawAppHubSubmitHTTPTimeout(t *testing.T) {
	got, err := maclawAppHubSubmitHTTPTimeout(context.Background(), 30*time.Second)
	if err != nil || got != 30*time.Second {
		t.Fatalf("background timeout = %v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err = maclawAppHubSubmitHTTPTimeout(ctx, 60*time.Second)
	if err != nil {
		t.Fatalf("live ctx: %v", err)
	}
	if got > 5*time.Second || got < 4*time.Second {
		t.Fatalf("expected ~5s remaining, got %v", got)
	}
	expired, cancel2 := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel2()
	time.Sleep(5 * time.Millisecond)
	if _, err := maclawAppHubSubmitHTTPTimeout(expired, 60*time.Second); err == nil {
		t.Fatal("expected error for expired context")
	}
}

func TestApplyResolvedDependenciesToPlanAcceptsMapSlice(t *testing.T) {
	// In-memory stamp path stores []map[string]any (not []interface{}).
	deps := []maclawAppInstallPlanDependency{
		{ID: "paper_pdf_translator", Required: true, Source: "local"},
	}
	applyResolvedDependenciesToPlan(deps, map[string]any{
		"resolved_dependencies": []map[string]any{
			{
				"id":               "paper_pdf_translator",
				"install_ref":      "hub-uuid-1",
				"source":           "skillmarket",
				"install_ref_kind": "skillmarket",
			},
		},
	})
	if deps[0].InstallRef != "hub-uuid-1" {
		t.Fatalf("InstallRef = %q", deps[0].InstallRef)
	}
	if deps[0].Source != "skillmarket" {
		t.Fatalf("Source = %q", deps[0].Source)
	}
	if deps[0].InstallRefKind != "skillmarket" {
		t.Fatalf("InstallRefKind = %q", deps[0].InstallRefKind)
	}
}

func TestValidateAppDependenciesPublishedAcceptsInstalledHubSkillID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillDir := filepath.Join(tmpHome, ".maclaw", "data", "skills", "paper_pdf_translator")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll skillDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# paper_pdf_translator\n"), 0o644); err != nil {
		t.Fatalf("WriteFile skill.md: %v", err)
	}

	app := &App{testHomeDir: tmpHome}
	app.skillExecutor = NewSkillExecutor(app, nil, nil)
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:       "paper_pdf_translator",
		SkillDir:   skillDir,
		Status:     "active",
		Source:     "enterprise_hub",
		HubSkillID: "hub-paper-pdf-1",
		HubVersion: "1.0.0",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	plan := maclawAppInstallPlan{
		Dependencies: []maclawAppInstallPlanDependency{
			{ID: "paper_pdf_translator", Required: true, Source: "local"},
		},
	}
	if err := app.validateAppDependenciesPublished(plan, map[string]any{}); err != nil {
		t.Fatalf("installed skill with HubSkillID should pass without bundle: %v", err)
	}
}

func TestMaclawAppWorkspaceLayoutMetadataFallsBackToGovernanceLayout(t *testing.T) {
	entry := parsedMaclawAppEntry{
		ID:   "layout-governance-only",
		Name: "Governance Layout Only",
		Kind: "enterprise_normal_app",
		App: map[string]any{
			"id":   "layout-governance-only",
			"name": "Governance Layout Only",
			"kind": "enterprise_normal_app",
			"governance": map[string]any{
				"workspaceLayout": map[string]any{
					"schema":        "maclaw.app.ui.v1",
					"entry":         "business_workspace",
					"template":      "dashboard",
					"density":       "spacious",
					"primaryRegion": "right",
					"outputRegion":  "modal",
					"regions": []any{
						map[string]any{"id": "operation_form", "role": "input", "placement": "right"},
						map[string]any{"id": "record_list", "role": "record_list", "placement": "center"},
						map[string]any{"id": "result_panel", "role": "output", "placement": "modal"},
					},
				},
			},
		},
	}

	layout := maclawAppWorkspaceLayoutMetadataForEntry(entry)
	if layout == nil {
		t.Fatal("expected governance workspace layout fallback")
	}
	if layout["entry"] != "business_workspace" || layout["template"] != "dashboard" || layout["density"] != "spacious" {
		t.Fatalf("unexpected governance workspace layout identity: %#v", layout)
	}
	if layout["primaryRegion"] != "right" || layout["primary_region"] != "right" || layout["outputRegion"] != "modal" || layout["output_region"] != "modal" {
		t.Fatalf("workspace layout should expose camel and snake region aliases: %#v", layout)
	}
	if layout["regionCount"] != 3 || layout["region_count"] != 3 {
		t.Fatalf("workspace layout should expose camel and snake region counts: %#v", layout)
	}
	regions, ok := layout["regions"].([]any)
	if !ok || len(regions) != 3 {
		t.Fatalf("workspace layout should preserve regions: %#v", layout["regions"])
	}
}
