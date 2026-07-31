package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
