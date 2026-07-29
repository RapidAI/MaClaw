package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// Integration tests for GUI one-click publish:
// local durable queue → enterprise Hub pack submit → skill-market upload (materialize path).

type oneClickMockServer struct {
	t *testing.T

	packHits  int32
	skillHits int32

	// skillFailUntil: fail skill uploads while hit count is still <= this value (0 = never).
	skillFailUntil int32
	// skillFailAlways: every skill upload fails (overrides skillFailUntil).
	skillFailAlways bool
	packFail        bool

	mu             sync.Mutex
	packSourceID   string
	lastSkillEmail string
	lastSkillFiles map[string]string // zip entry name -> content (small text files only)
}

func startOneClickMockServer(t *testing.T, cfg *oneClickMockServer) *httptest.Server {
	t.Helper()
	if cfg == nil {
		cfg = &oneClickMockServer{}
	}
	cfg.t = t
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/capabilities/maclaw-apps/submit":
			atomic.AddInt32(&cfg.packHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("pack Authorization = %q", got)
			}
			if cfg.packFail {
				http.Error(w, "hub offline", http.StatusBadGateway)
				return
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode pack body: %v", err)
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			source, _ := payload["source_submission_id"].(string)
			cfg.mu.Lock()
			cfg.packSourceID = source
			cfg.mu.Unlock()
			pkg, _ := payload["package"].(map[string]any)
			if pkg == nil || pkg["schema"] != "maclaw.app.pack.v1" {
				t.Errorf("unexpected package: %#v", pkg)
			}
			appID := sourceAppIDFromPack(pkg)
			writeHubPackOK(w, appID, "hub-"+appID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/capabilities/skills/submit":
			n := atomic.AddInt32(&cfg.skillHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("skill Authorization = %q", got)
			}
			if cfg.skillFailAlways || (cfg.skillFailUntil > 0 && n <= cfg.skillFailUntil) {
				http.Error(w, "transient skill fail", http.StatusBadGateway)
				return
			}
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
				http.Error(w, "bad multipart", http.StatusBadRequest)
				return
			}
			email := strings.TrimSpace(r.FormValue("email"))
			file, _, err := r.FormFile("zip")
			if err != nil {
				t.Errorf("FormFile zip: %v", err)
				http.Error(w, "missing zip", http.StatusBadRequest)
				return
			}
			data, err := io.ReadAll(file)
			_ = file.Close()
			if err != nil {
				t.Errorf("read zip: %v", err)
				http.Error(w, "read zip", http.StatusBadRequest)
				return
			}
			files := readZipTextEntries(t, data)
			cfg.mu.Lock()
			cfg.lastSkillEmail = email
			cfg.lastSkillFiles = files
			cfg.mu.Unlock()
			if email == "" || !strings.Contains(email, "@") {
				t.Errorf("skill email = %q", email)
			}
			for _, name := range []string{"skill.yaml", "skill_package_manifest.json", "maclaw.app.json"} {
				if _, ok := files[name]; !ok {
					t.Errorf("materialized zip missing %s (have %#v)", name, keysOfMap(files))
				}
			}
			if man := files["skill_package_manifest.json"]; man != "" {
				if !strings.Contains(man, `"is_maclaw_app":true`) && !strings.Contains(man, `"is_maclaw_app": true`) {
					t.Errorf("manifest missing is_maclaw_app: %s", man)
				}
				if !strings.Contains(man, `"product_kind":"maclaw_app_skill"`) && !strings.Contains(man, `"product_kind": "maclaw_app_skill"`) {
					t.Errorf("manifest missing product_kind: %s", man)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "skill-market-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func writeHubPackOK(w http.ResponseWriter, appID, hubSubmissionID string) {
	if appID == "" {
		appID = "app"
	}
	if hubSubmissionID == "" {
		hubSubmissionID = "hub-" + appID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"schema":         "maclaw.app.hub_submission.v1",
		"status":         "pending_review",
		"package_sha256": "hub-pack-sha",
		"app_count":      1,
		"submissions": []map[string]any{{
			"submission_id": hubSubmissionID,
			"capability_id": appID,
			"app_id":        appID,
			"app_name":      "Hub Sync Ready Tool",
			"status":        "pending_review",
			"version_key":   hubSubmissionID,
		}},
	})
}

func sourceAppIDFromPack(pkg map[string]any) string {
	if pkg == nil {
		return "app"
	}
	apps := anySlice(pkg["apps"])
	if len(apps) == 0 {
		return "app"
	}
	entry := anyMap(apps[0])
	if entry == nil {
		return "app"
	}
	app := anyMap(entry["app"])
	if id := strings.TrimSpace(stringFromAnyMap(app, "id")); id != "" {
		return id
	}
	return "app"
}

func TestPublishMaclawAppOneClickIntegrationSuccess(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	clearDefaultHubCenters(t)

	mock := &oneClickMockServer{}
	server := startOneClickMockServer(t, mock)
	app := newOneClickIntegrationApp(t, tmpHome, server.URL, true)
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "one-click-app")

	result, err := app.PublishMaclawAppOneClick(pkg)
	if err != nil {
		t.Fatalf("PublishMaclawAppOneClick: %v", err)
	}
	assertOneClickOK(t, result, false)
	msg := stringFromAnyMap(result, "message")
	if !strings.Contains(msg, "enterprise hub pack submitted") || !strings.Contains(msg, "skill market upload ok") {
		t.Fatalf("message = %q", msg)
	}
	if atomic.LoadInt32(&mock.packHits) != 1 || atomic.LoadInt32(&mock.skillHits) != 1 {
		t.Fatalf("packHits=%d skillHits=%d", mock.packHits, mock.skillHits)
	}
	mock.mu.Lock()
	source := mock.packSourceID
	email := mock.lastSkillEmail
	files := mock.lastSkillFiles
	mock.mu.Unlock()
	if source == "" || !strings.HasPrefix(source, "local-review-") {
		t.Fatalf("source_submission_id = %q", source)
	}
	if email != "uploader@example.com" {
		t.Fatalf("skill email = %q", email)
	}
	if !strings.Contains(files["maclaw.app.json"], "one-click-app") {
		t.Fatalf("maclaw.app.json missing app id: %s", files["maclaw.app.json"])
	}
	if result["local_submission_id"] != "hub-one-click-app" {
		t.Fatalf("local_submission_id after hub rename = %#v", result["local_submission_id"])
	}
	targets, _ := result["targets"].(map[string]any)
	skill, _ := targets["skill_market"].(map[string]any)
	if skill["ok"] != true || !strings.Contains(stringFromAnyMap(skill, "submission_id"), "skill-market-1") {
		t.Fatalf("skill_market target = %#v", skill)
	}

	record, err := app.GetMaclawAppPackageSubmission("hub-one-click-app")
	if err != nil || record == nil {
		t.Fatalf("GetMaclawAppPackageSubmission: %v record=%v", err, record)
	}
	if record.Channel != "hub" || record.Status != "pending_review" {
		t.Fatalf("queue record channel/status = %s/%s", record.Channel, record.Status)
	}
	if !strings.Contains(record.Message, "skill market upload ok") {
		t.Fatalf("stamped message = %q", record.Message)
	}
	if schema := stringFromAnyMap(result, "schema"); schema != "maclaw.app.one_click_publish.v1" {
		t.Fatalf("schema = %q", schema)
	}
}

func TestPublishMaclawAppOneClickIntegrationPartialHubFailSkillOK(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	clearDefaultHubCenters(t)

	mock := &oneClickMockServer{packFail: true}
	server := startOneClickMockServer(t, mock)
	app := newOneClickIntegrationApp(t, tmpHome, server.URL, true)
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "partial-hub-fail")

	result, err := app.PublishMaclawAppOneClick(pkg)
	if err != nil {
		t.Fatalf("PublishMaclawAppOneClick: %v", err)
	}
	assertOneClickOK(t, result, true)
	msg := stringFromAnyMap(result, "message")
	if !strings.Contains(msg, "enterprise hub pack failed") || !strings.Contains(msg, "skill market upload ok") {
		t.Fatalf("message = %q", msg)
	}
	if atomic.LoadInt32(&mock.skillHits) != 1 {
		t.Fatalf("skillHits=%d", mock.skillHits)
	}
	localID := stringFromAnyMap(result, "local_submission_id")
	if !strings.HasPrefix(localID, "local-review-") {
		t.Fatalf("local_submission_id = %q", localID)
	}
	// Channel stays local when hub sync fails.
	record, err := app.GetMaclawAppPackageSubmission(localID)
	if err != nil || record == nil {
		t.Fatalf("Get local record: %v", err)
	}
	if record.Channel != "local" {
		t.Fatalf("channel after hub fail = %q", record.Channel)
	}
	if !strings.Contains(record.Message, "enterprise hub pack failed") {
		t.Fatalf("stamped partial message = %q", record.Message)
	}
}

func TestPublishMaclawAppOneClickIntegrationPartialSkillFail(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	clearDefaultHubCenters(t)

	mock := &oneClickMockServer{skillFailAlways: true}
	server := startOneClickMockServer(t, mock)
	app := newOneClickIntegrationApp(t, tmpHome, server.URL, true)
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "skill-fail-app")

	result, err := app.PublishMaclawAppOneClick(pkg)
	if err != nil {
		t.Fatalf("PublishMaclawAppOneClick: %v", err)
	}
	assertOneClickOK(t, result, true)
	msg := stringFromAnyMap(result, "message")
	if !strings.Contains(msg, "enterprise hub pack submitted") || !strings.Contains(msg, "skill market failed") {
		t.Fatalf("message = %q", msg)
	}
	if atomic.LoadInt32(&mock.packHits) != 1 || atomic.LoadInt32(&mock.skillHits) != 1 {
		t.Fatalf("packHits=%d skillHits=%d", mock.packHits, mock.skillHits)
	}
	targets, _ := result["targets"].(map[string]any)
	hub, _ := targets["enterprise_hub_pack"].(map[string]any)
	skill, _ := targets["skill_market"].(map[string]any)
	if hub["ok"] != true || skill["ok"] != false {
		t.Fatalf("targets hub=%#v skill=%#v", hub, skill)
	}
	if result["local_submission_id"] != "hub-skill-fail-app" {
		t.Fatalf("expected hub-skill-fail-app, got %#v", result["local_submission_id"])
	}
	record, err := app.GetMaclawAppPackageSubmission("hub-skill-fail-app")
	if err != nil || record == nil {
		t.Fatalf("Get hub record: %v", err)
	}
	if record.Channel != "hub" {
		t.Fatalf("channel = %q", record.Channel)
	}
	if !strings.Contains(record.Message, "skill market failed") {
		t.Fatalf("stamped message = %q", record.Message)
	}
}

func TestPublishMaclawAppSubmissionOneClickIntegrationRetryAfterHubSynced(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	clearDefaultHubCenters(t)

	mock := &oneClickMockServer{skillFailUntil: 1}
	server := startOneClickMockServer(t, mock)
	app := newOneClickIntegrationApp(t, tmpHome, server.URL, true)
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "retry-app")

	first, err := app.PublishMaclawAppOneClick(pkg)
	if err != nil {
		t.Fatalf("first one-click: %v", err)
	}
	assertOneClickOK(t, first, true)
	if atomic.LoadInt32(&mock.packHits) != 1 || atomic.LoadInt32(&mock.skillHits) != 1 {
		t.Fatalf("after first: pack=%d skill=%d", mock.packHits, mock.skillHits)
	}
	hubID := stringFromAnyMap(first, "local_submission_id")
	if hubID != "hub-retry-app" {
		t.Fatalf("hub id = %q", hubID)
	}

	// Retry same durable row: hub pack skipped, skill market retried.
	second, err := app.PublishMaclawAppSubmissionOneClick(hubID)
	if err != nil {
		t.Fatalf("retry one-click: %v", err)
	}
	assertOneClickOK(t, second, false)
	msg := stringFromAnyMap(second, "message")
	if !strings.Contains(msg, "local queue ready") || !strings.Contains(msg, "already synced") {
		t.Fatalf("retry message should reflect skipped hub: %q", msg)
	}
	if !strings.Contains(msg, "skill-market-1") {
		t.Fatalf("retry message missing skill id: %q", msg)
	}
	if atomic.LoadInt32(&mock.packHits) != 1 {
		t.Fatalf("hub pack should not re-submit, packHits=%d", mock.packHits)
	}
	if atomic.LoadInt32(&mock.skillHits) != 2 {
		t.Fatalf("skill should retry once more, skillHits=%d", mock.skillHits)
	}
	// Final stamp on durable queue.
	record, err := app.GetMaclawAppPackageSubmission(hubID)
	if err != nil || record == nil {
		t.Fatalf("Get after retry: %v", err)
	}
	if !strings.Contains(record.Message, "skill market upload ok") {
		t.Fatalf("final stamped message = %q", record.Message)
	}
}

func TestPublishMaclawAppSubmissionOneClickIntegrationRejectsPublished(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "published-app")
	queued, err := app.SubmitMaclawAppPackage(pkg)
	if err != nil {
		t.Fatalf("SubmitMaclawAppPackage: %v", err)
	}
	localID := stringFromAnyMap(queued, "submission_id")
	if localID == "" {
		t.Fatalf("missing submission_id: %#v", queued)
	}
	ok, err := app.UpdateMaclawAppPackageSubmissionStatus(localID, maclawAppSubmissionStatusUpdate{
		Status:  "published",
		Channel: "hub",
		Message: "already published",
	})
	if err != nil || !ok {
		t.Fatalf("UpdateMaclawAppPackageSubmissionStatus: ok=%v err=%v", ok, err)
	}
	_, err = app.PublishMaclawAppSubmissionOneClick(localID)
	if err == nil || !strings.Contains(err.Error(), "published") {
		t.Fatalf("expected published rejection, got %v", err)
	}
}

func TestPublishMaclawAppOneClickIntegrationSkillFailWithoutEmail(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	clearDefaultHubCenters(t)

	// Pack-only server (skill never reached because remote_email is missing).
	var packHits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/capabilities/maclaw-apps/submit" {
			atomic.AddInt32(&packHits, 1)
			writeHubPackOK(w, "no-email-app", "hub-no-email-app")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	app := newOneClickIntegrationApp(t, tmpHome, server.URL, false)
	pkg := maclawAppReadyToolPackageForHubSyncTest(t, "no-email-app")

	result, err := app.PublishMaclawAppOneClick(pkg)
	if err != nil {
		t.Fatalf("PublishMaclawAppOneClick: %v", err)
	}
	assertOneClickOK(t, result, true)
	msg := stringFromAnyMap(result, "message")
	if !strings.Contains(msg, "remote_email") && !strings.Contains(msg, "skill market failed") {
		t.Fatalf("message = %q", msg)
	}
	if atomic.LoadInt32(&packHits) != 1 {
		t.Fatalf("packHits=%d", packHits)
	}
	if result["local_submission_id"] != "hub-no-email-app" {
		t.Fatalf("local_submission_id = %#v", result["local_submission_id"])
	}
}

func TestPublishMaclawAppOneClickIntegrationRejectsUnreadyPackage(t *testing.T) {
	tmpHome := t.TempDir()
	app := newOneClickIntegrationApp(t, tmpHome, "http://127.0.0.1:1", true)
	// Missing test evidence / layout → hard error before remote calls.
	pkg := `{
		"schema": "maclaw.app.pack.v1",
		"privateMarker": "x_maclaw_apps",
		"apps": [{
			"schema": "maclaw.app.v1",
			"privateMarker": "x_maclaw_apps",
			"app": {"id": "unready", "name": "Unready", "kind": "tool_app"}
		}]
	}`
	_, err := app.PublishMaclawAppOneClick(pkg)
	if err == nil {
		t.Fatal("expected unready package to fail")
	}
	if !strings.Contains(err.Error(), "not ready") && !strings.Contains(err.Error(), "testEvidence") && !strings.Contains(err.Error(), "governance") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishMaclawAppSubmissionOneClickIntegrationNotFound(t *testing.T) {
	tmpHome := t.TempDir()
	app := &App{testHomeDir: tmpHome}
	_, err := app.PublishMaclawAppSubmissionOneClick("does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestPublishMaclawAppSubmissionOneClickIntegrationEmptyID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, err := app.PublishMaclawAppSubmissionOneClick("  ")
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required id error, got %v", err)
	}
}

func newOneClickIntegrationApp(t *testing.T, tmpHome, hubURL string, withEmail bool) *App {
	t.Helper()
	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		RemoteHubURL:      hubURL,
		RemoteViewerToken: "viewer-token",
		// Prefer enterprise skill upload only so pack+skill share one mock server.
		CapabilityMarketPolicy: corelib.CapabilityMarketPolicy{
			PreferredUploadTarget: "enterprise",
		},
	}
	if withEmail {
		cfg.RemoteEmail = "uploader@example.com"
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return app
}

// Serializes tests that mutate package-level HubCenter defaults.
var oneClickHubCenterTestMu sync.Mutex

func clearDefaultHubCenters(t *testing.T) {
	t.Helper()
	oneClickHubCenterTestMu.Lock()
	originalDefaultCenter := defaultRemoteHubCenterURL
	originalDefaultCenters := remote.DefaultRemoteHubCenterURLs
	defaultRemoteHubCenterURL = ""
	remote.DefaultRemoteHubCenterURLs = nil
	t.Cleanup(func() {
		defaultRemoteHubCenterURL = originalDefaultCenter
		remote.DefaultRemoteHubCenterURLs = originalDefaultCenters
		oneClickHubCenterTestMu.Unlock()
	})
}

func assertOneClickOK(t *testing.T, result map[string]any, wantPartial bool) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if result["ok"] != true {
		t.Fatalf("ok = %#v result=%#v", result["ok"], result)
	}
	partial, _ := result["partial"].(bool)
	if partial != wantPartial {
		t.Fatalf("partial = %v want %v message=%v targets=%#v", partial, wantPartial, result["message"], result["targets"])
	}
	if schema := stringFromAnyMap(result, "schema"); schema != "" && schema != "maclaw.app.one_click_publish.v1" {
		t.Fatalf("schema = %q", schema)
	}
	if targets, ok := result["targets"].(map[string]any); !ok || targets == nil {
		t.Fatalf("missing targets: %#v", result["targets"])
	} else {
		if _, ok := targets["enterprise_hub_pack"]; !ok {
			t.Fatalf("missing enterprise_hub_pack in targets: %#v", targets)
		}
		if _, ok := targets["skill_market"]; !ok {
			t.Fatalf("missing skill_market in targets: %#v", targets)
		}
	}
}

func readZipTextEntries(t *testing.T, zipBytes []byte) map[string]string {
	t.Helper()
	r, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Errorf("open skill zip: %v", err)
		return nil
	}
	out := map[string]string{}
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Errorf("open %s: %v", f.Name, err)
			continue
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Errorf("read %s: %v", f.Name, err)
			continue
		}
		out[f.Name] = string(data)
	}
	return out
}
