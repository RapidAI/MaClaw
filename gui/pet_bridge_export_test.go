package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/gui/petpack"
)

func TestPetStoreRequestRejectsUnsafeHubURLAndPath(t *testing.T) {
	base := t.TempDir()
	app := &App{testHomeDir: base}
	// A bare local config is enough for these URL checks; the request must be
	// rejected before any network call is attempted.
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "file:///tmp/hub", SkillMarketSessionToken: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.petStoreRequest("GET", "/api/v1/pet-store/packs", nil, ""); err == nil {
		t.Fatal("non-HTTP HubCenter URL should be rejected")
	}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "https://hub.example.test", SkillMarketSessionToken: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.petStoreRequest("GET", "https://attacker.example.test/steal", nil, ""); err == nil {
		t.Fatal("absolute Pet Store path should be rejected")
	}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "https://token@hub.example.test", SkillMarketSessionToken: "test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.petStoreRequest("GET", "/api/v1/pet-store/packs", nil, ""); err == nil {
		t.Fatal("HubCenter URL with user info should be rejected")
	}
}

func TestExpertMarketRequestAllowsItsLargerPackageLimit(t *testing.T) {
	var received int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/expert-market/experts" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		received = len(body)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "expert-token"}); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), int(expertMarketMaxRequestBytes))
	if _, err := app.expertMarketRequest(http.MethodPost, "/api/v1/expert-market/experts", bytes.NewReader(payload), "application/octet-stream"); err != nil {
		t.Fatalf("expert market request: %v", err)
	}
	if received != len(payload) {
		t.Fatalf("received=%d, want %d", received, len(payload))
	}
}

func TestUninstallExpertMarketListingIsLocalOnly(t *testing.T) {
	previousStore := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	t.Cleanup(func() { defaultExpertStore = previousStore })

	const installedID = "pkgexp-reviewed-analyst"
	if err := defaultExpertStore.SaveMarketInstall(ExpertDefinition{ID: installedID, Name: "Reviewed analyst"}); err != nil {
		t.Fatal(err)
	}
	if err := defaultExpertStore.MarkPendingHubUpload(installedID); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).UninstallExpertMarketListing(installedID); err != nil {
		t.Fatalf("uninstall market expert: %v", err)
	}
	if _, found, err := defaultExpertStore.Get(installedID); err != nil || found {
		t.Fatalf("local expert remains after uninstall: found=%v err=%v", found, err)
	}
	_, tombstones, uploads, deletes, err := defaultExpertStore.ListForHubSync()
	if err != nil {
		t.Fatal(err)
	}
	if _, tombstoned := tombstones[installedID]; tombstoned {
		t.Fatalf("uninstall must not create a Hub delete tombstone: %#v", tombstones)
	}
	if uploads[installedID] || deletes[installedID] != "" {
		t.Fatalf("uninstall must not leave Hub sync work: uploads=%#v deletes=%#v", uploads, deletes)
	}
	if localOnly, err := defaultExpertStore.IsLocalOnly(installedID); err != nil || !localOnly {
		t.Fatalf("uninstall must retain the local-only marker: localOnly=%v err=%v", localOnly, err)
	}
}

func TestUninstallExpertMarketListingRejectsNonMarketID(t *testing.T) {
	if err := (&App{}).UninstallExpertMarketListing("expert-local-only"); err == nil {
		t.Fatal("non-market expert ID should be rejected")
	}
}

func TestUninstallExpertMarketListingRejectsPortablePackageImport(t *testing.T) {
	previousStore := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	t.Cleanup(func() { defaultExpertStore = previousStore })

	const packageID = "pkgexp-portable-import"
	if err := defaultExpertStore.Save(ExpertDefinition{ID: packageID, Name: "Portable import"}); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).UninstallExpertMarketListing(packageID); err == nil {
		t.Fatal("portable package imports must not be removable through the market uninstall API")
	}
	if _, found, err := defaultExpertStore.Get(packageID); err != nil || !found {
		t.Fatalf("portable package import must remain intact: found=%v err=%v", found, err)
	}
}

func TestListExpertMarketListingsMarksLocalMarketInstall(t *testing.T) {
	previousStore := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	t.Cleanup(func() { defaultExpertStore = previousStore })
	if err := defaultExpertStore.SaveMarketInstall(ExpertDefinition{ID: "pkgexp-reviewed-analyst", Name: "Reviewed analyst"}); err != nil {
		t.Fatal(err)
	}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/expert-market/experts" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"experts":[{"id":"listing-1","source_expert_id":"pkgexp-reviewed-analyst"}]}`))
	}))
	defer hub.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "market-token"}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	experts := result["experts"].([]interface{})
	listing := experts[0].(map[string]interface{})
	if listing["installed"] != true || listing["local_expert_id"] != "pkgexp-reviewed-analyst" {
		t.Fatalf("market listing missing local install state: %#v", listing)
	}
}

func TestListExpertMarketListingsDoesNotTreatPortablePackageAsMarketInstall(t *testing.T) {
	previousStore := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	t.Cleanup(func() { defaultExpertStore = previousStore })
	if err := defaultExpertStore.Save(ExpertDefinition{ID: "pkgexp-portable-import", Name: "Portable import"}); err != nil {
		t.Fatal(err)
	}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"experts":[{"id":"listing-1","source_expert_id":"pkgexp-portable-import"}]}`))
	}))
	defer hub.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "market-token"}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	listing := result["experts"].([]interface{})[0].(map[string]interface{})
	if listing["installed"] == true || listing["local_expert_id"] != nil {
		t.Fatalf("ordinary portable import must not be shown as market-installed: %#v", listing)
	}
}

func TestListExpertMarketListingsDoesNotTreatUninstalledMarketExpertAsInstalled(t *testing.T) {
	previousStore := defaultExpertStore
	defaultExpertStore = newExpertStore(filepath.Join(t.TempDir(), "experts.json"))
	t.Cleanup(func() { defaultExpertStore = previousStore })
	const installedID = "pkgexp-uninstalled-market-expert"
	if err := defaultExpertStore.SaveMarketInstall(ExpertDefinition{ID: installedID, Name: "Previously installed"}); err != nil {
		t.Fatal(err)
	}
	if err := (&App{}).UninstallExpertMarketListing(installedID); err != nil {
		t.Fatal(err)
	}

	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"experts":[{"id":"listing-1","source_expert_id":"pkgexp-uninstalled-market-expert"}]}`))
	}))
	defer hub.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "market-token"}); err != nil {
		t.Fatal(err)
	}
	result, err := app.ListExpertMarketListings("", "published", 1, 30)
	if err != nil {
		t.Fatal(err)
	}
	listing := result["experts"].([]interface{})[0].(map[string]interface{})
	if listing["installed"] == true || listing["local_expert_id"] != nil {
		t.Fatalf("uninstalled market expert must not be shown as installed: %#v", listing)
	}
}

func TestPetStoreRequestDoesNotFollowRedirects(t *testing.T) {
	redirectTargetCalls := 0
	var targetAuthorization string
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectTargetCalls++
		targetAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectTarget.Close()

	var hubAuthorization string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubAuthorization = r.Header.Get("Authorization")
		http.Redirect(w, r, redirectTarget.URL+"/unexpected", http.StatusFound)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "test-session"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.petStoreRequest(http.MethodGet, "/api/v1/pet-store/packs", nil, ""); err == nil {
		t.Fatal("redirect response should be surfaced as an error")
	}
	if redirectTargetCalls != 0 {
		t.Fatalf("redirect target was called %d times", redirectTargetCalls)
	}
	if hubAuthorization != "Bearer test-session" {
		t.Fatalf("HubCenter authorization = %q", hubAuthorization)
	}
	if targetAuthorization != "" {
		t.Fatalf("redirect target received authorization header: %q", targetAuthorization)
	}
}

func TestPetStoreRequestRefreshesExpiredSessionUsingHubEnrollment(t *testing.T) {
	var marketCalls int
	var machineLoginCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pet-store/account":
			marketCalls++
			if marketCalls == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer expired-token" {
					t.Fatalf("initial authorization=%q", got)
				}
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"session expired or invalid"}`))
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer refreshed-token" {
				t.Fatalf("retried authorization=%q", got)
			}
			_, _ = w.Write([]byte(`{"user":{"email":"user@example.test"}}`))
		case "/api/v1/auth/machine-login":
			machineLoginCalls++
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload["hub_id"] != "hub-test" || payload["account"] != "user-1" || payload["machine_id"] != "machine-1" || payload["viewer_token"] != strings.Repeat("a", 32) {
				t.Fatalf("unexpected machine login payload: %#v", payload)
			}
			_, _ = w.Write([]byte(`{"session_token":"refreshed-token"}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		RemoteHubID:             "hub-test",
		SkillMarketSessionToken: "expired-token",
		RemoteUserID:            "user-1",
		RemoteMachineID:         "machine-1",
		RemoteViewerToken:       strings.Repeat("a", 32),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetPetStoreAccount(); err != nil {
		t.Fatalf("GetPetStoreAccount: %v", err)
	}
	if marketCalls != 2 || machineLoginCalls != 1 {
		t.Fatalf("market=%d machine-login=%d, want 2/1", marketCalls, machineLoginCalls)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SkillMarketSessionToken != "refreshed-token" {
		t.Fatalf("session token=%q", cfg.SkillMarketSessionToken)
	}
}

func TestPetStoreRequestObtainsMissingSessionUsingHubEnrollment(t *testing.T) {
	var marketAuthorization string
	var machineLoginCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/machine-login":
			machineLoginCalls++
			_, _ = w.Write([]byte(`{"session_token":"new-session"}`))
		case "/api/v1/pet-store/account":
			marketAuthorization = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"user":{"email":"user@example.test"}}`))
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: server.URL,
		RemoteHubID:        "hub-test",
		RemoteUserID:       "user-1",
		RemoteMachineID:    "machine-1",
		RemoteViewerToken:  strings.Repeat("a", 32),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetPetStoreAccount(); err != nil {
		t.Fatalf("GetPetStoreAccount: %v", err)
	}
	if machineLoginCalls != 1 || marketAuthorization != "Bearer new-session" {
		t.Fatalf("machine-login=%d authorization=%q", machineLoginCalls, marketAuthorization)
	}
}

func TestGetPetStoreAccountUsesPublicContactAndDropsInternalUserID(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pet-store/account" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"user":{"id":"u_1783155920697276064_9","email":"u_1783155920697276064_9"}}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      hub.URL,
		SkillMarketSessionToken: "account-token",
		RemoteMobile:            "+86 138 0013 8000",
	}); err != nil {
		t.Fatal(err)
	}
	account, err := app.GetPetStoreAccount()
	if err != nil {
		t.Fatalf("GetPetStoreAccount: %v", err)
	}
	user, ok := account["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("user = %#v", account["user"])
	}
	if got := user["phone_number"]; got != "+86 138 0013 8000" {
		t.Fatalf("phone_number = %#v", got)
	}
	if _, exists := user["id"]; exists {
		t.Fatalf("internal user ID leaked: %#v", user)
	}
	if _, exists := user["email"]; exists {
		t.Fatalf("invalid server email was retained: %#v", user)
	}
}

func TestGetPetStoreAccountPrefersServerEmailOverLocalContact(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"id":"u_internal","email":"creator@example.test","phone_number":"13800138000"}}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      hub.URL,
		SkillMarketSessionToken: "account-token",
		RemoteEmail:             "configured@example.test",
		RemoteMobile:            "13900139000",
	}); err != nil {
		t.Fatal(err)
	}
	account, err := app.GetPetStoreAccount()
	if err != nil {
		t.Fatalf("GetPetStoreAccount: %v", err)
	}
	user := account["user"].(map[string]interface{})
	if got := user["email"]; got != "creator@example.test" {
		t.Fatalf("email = %#v", got)
	}
	if _, exists := user["phone_number"]; exists {
		t.Fatalf("phone fallback should not accompany a valid email: %#v", user)
	}
}

func TestGetPetStoreAccountRejectsMalformedEmailConsistently(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"email":"creator@internal","phone_number":"13800138000"}}`))
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      hub.URL,
		SkillMarketSessionToken: "account-token",
	}); err != nil {
		t.Fatal(err)
	}
	account, err := app.GetPetStoreAccount()
	if err != nil {
		t.Fatalf("GetPetStoreAccount: %v", err)
	}
	user := account["user"].(map[string]interface{})
	if got := user["phone_number"]; got != "13800138000" {
		t.Fatalf("phone_number = %#v", got)
	}
	if _, exists := user["email"]; exists {
		t.Fatalf("malformed email was retained: %#v", user)
	}
}

func TestPetStoreRequestFailsFastWhenHubCenterURLIsMissing(t *testing.T) {
	var serverCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		_, _ = w.Write([]byte(`{"user":{"email":"user@example.test"}}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	// Only RemoteHubURL is set. The hub serves no pet-store routes, so the
	// bridge must fail fast with the HubCenter-missing error instead of
	// falling back to a guaranteed 404 (and must not retry a session refresh).
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      server.URL,
		RemoteUserID:      "user-1",
		RemoteMachineID:   "machine-1",
		RemoteViewerToken: strings.Repeat("a", 32),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.GetPetStoreAccount(); err == nil || err.Error() != errPetStoreHubCenterMissing.Error() {
		t.Fatalf("GetPetStoreAccount err = %v, want %q", err, errPetStoreHubCenterMissing)
	}
	if serverCalls != 0 {
		t.Fatalf("hub fallback issued %d requests, want 0", serverCalls)
	}
}

func TestRefreshPetStoreSessionReusesConcurrentBootstrap(t *testing.T) {
	var machineLoginCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/machine-login" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		machineLoginCalls++
		_, _ = w.Write([]byte(`{"session_token":"new-session"}`))
	}))
	defer server.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: server.URL,
		RemoteUserID:       "user-1",
		RemoteMachineID:    "machine-1",
		RemoteViewerToken:  strings.Repeat("a", 32),
	}); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			errs <- app.refreshPetStoreSession("")
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("refresh session: %v", err)
		}
	}
	if machineLoginCalls != 1 {
		t.Fatalf("machine-login calls=%d, want 1", machineLoginCalls)
	}
}

func TestSubmitPetStorePackAllowsAnyLocalZipButRejectsMarketPack(t *testing.T) {
	previousRegistry := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(previousRegistry) })
	userRoot := t.TempDir()
	writeCreatedPetPack(t, userRoot, "created-pet")
	reg := petpack.NewRegistry(userRoot, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	zipPath := filepath.Join(t.TempDir(), "created-pet.zip")
	if err := writePetPackZip(filepath.Join(userRoot, "created-pet"), zipPath); err != nil {
		t.Fatal(err)
	}

	var receivedSourceID string
	var requestFailure string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/pet-store/packs" {
			requestFailure = "unexpected request " + r.Method + " " + r.URL.Path
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer publish-token" {
			requestFailure = "authorization = " + got
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			requestFailure = err.Error()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		receivedSourceID = r.FormValue("source_pack_id")
		file, _, err := r.FormFile("zip")
		if err != nil {
			requestFailure = err.Error()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		if body, err := io.ReadAll(file); err != nil || len(body) == 0 {
			requestFailure = "uploaded archive missing"
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "listing-1"})
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: hub.URL, SkillMarketSessionToken: "publish-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitPetStorePack(zipPath, "Created pet", "", "1.0.0", 0, "created-pet"); err != nil {
		t.Fatal(err)
	}
	if receivedSourceID != "created-pet" {
		t.Fatalf("source_pack_id = %q", receivedSourceID)
	}
	if requestFailure != "" {
		t.Fatal(requestFailure)
	}

	if err := reg.SetPackSource("created-pet", petpack.SourceImported); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitPetStorePack(zipPath, "Created pet", "", "1.0.0", 0, "created-pet"); err != nil {
		t.Fatalf("locally imported Zip should be publishable, got %v", err)
	}
	if err := reg.SetPackSource("created-pet", petpack.SourceMarket); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SubmitPetStorePack(zipPath, "Created pet", "", "1.0.0", 0, "created-pet"); err == nil {
		t.Fatal("market-installed pack should be rejected")
	}
	if _, err := app.SubmitPetStorePack(zipPath, "Created pet", "", "1.0.0", 0, "not a pack id"); err == nil {
		t.Fatal("invalid source pack ID should be rejected")
	}
}

func TestExportPetPackZipStagesArchiveWithoutDesktopDialog(t *testing.T) {
	previousRegistry := petpack.EnsureGlobal()
	t.Cleanup(func() { petpack.SetGlobalForTest(previousRegistry) })
	userRoot := t.TempDir()
	writeCreatedPetPack(t, userRoot, "created-pet")
	reg := petpack.NewRegistry(userRoot, nil)
	if err := reg.Scan(); err != nil {
		t.Fatal(err)
	}
	petpack.SetGlobalForTest(reg)

	app := &App{testHomeDir: t.TempDir()}
	archivePath, err := app.ExportPetPackZip("created-pet")
	if err != nil {
		t.Fatalf("stage archive: %v", err)
	}
	if filepath.Ext(archivePath) != ".zip" {
		t.Fatalf("archive path %q is not a zip", archivePath)
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("staged archive does not exist: %v", err)
	}
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatalf("open staged archive: %v", err)
	}
	defer zr.Close()
	if len(zr.File) == 0 || zr.File[0].Name != "pet-pack.yaml" {
		t.Fatalf("unexpected staged archive contents: %+v", zr.File)
	}
}

func writeCreatedPetPack(t *testing.T, userRoot, id string) {
	t.Helper()
	dir := filepath.Join(userRoot, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "schema_version: 1\nid: " + id + "\nname: Created pet\nrenderer: procedural-fallback\n"
	if err := os.WriteFile(filepath.Join(dir, "pet-pack.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWritePetPackZipExcludesLocalSourceMarker(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "pet-pack.yaml"), []byte("schema_version: 1\nid: creator-pet\nname: Creator pet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".maclaw-pet-source"), []byte("market\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "creator-pet.zip")
	if err := writePetPackZip(source, destination); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == ".maclaw-pet-source" {
			t.Fatal("local source marker must not be exported")
		}
	}
}
