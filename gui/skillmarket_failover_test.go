package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestSkillSearcherSearch_FailsOverAndPersistsHubCenterList(t *testing.T) {
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

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL, "https://skillmarket-backup.example"}})
		case "/api/v1/skillmarket/search":
			_ = json.NewEncoder(w).Encode(struct {
				Results []SkillSearchResult `json:"results"`
			}{Results: []SkillSearchResult{{ID: "m1", Name: "Market Skill", Price: 0}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	searcher := NewSkillSearcher(NewSkillMarketClient(app))
	results, err := searcher.Search(context.Background(), "market", nil, 5)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].ID != "m1" {
		t.Fatalf("Search() results = %+v", results)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
	if !containsString(saved.RemoteHubCenterURLs, "https://skillmarket-backup.example") {
		t.Fatalf("RemoteHubCenterURLs = %#v", saved.RemoteHubCenterURLs)
	}
}

func TestDownloadSkillJSONFromHubCenter_FailsOver(t *testing.T) {
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

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/demo/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "demo",
				"name":        "Demo Skill",
				"description": "demo",
				"version":     "1.0.0",
				"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "do it"}}},
				"files": map[string]string{
					"assets/logo.png": base64.StdEncoding.EncodeToString([]byte("png")),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	skill, err := downloadSkillJSONFromHubCenter(context.Background(), app, "/api/v1/skills/demo/download")
	if err != nil {
		t.Fatalf("downloadSkillJSONFromHubCenter() error = %v", err)
	}
	if skill.Name != "Demo Skill" || skill.HubSkillID != "demo" {
		t.Fatalf("skill = %+v", skill)
	}
	if skill.SkillDir == "" {
		t.Fatalf("SkillDir is empty for file-backed skill")
	}
	if _, err := os.Stat(filepath.Join(skill.SkillDir, "assets", "logo.png")); err != nil {
		t.Fatalf("expected failover download to extract bundled file: %v", err)
	}
}

func TestDownloadSkillJSONFromHubCenterVerifiesPackageSHA256(t *testing.T) {
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

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/demo/download":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "demo",
				"name":        "Demo Skill",
				"description": "demo",
				"version":     "1.0.0",
				"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "do it"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	_, err := downloadSkillJSONFromHubCenterToDirWithIntegrity(context.Background(), app, "/api/v1/skills/demo/download", "", strings.Repeat("0", 64), "")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}
func TestVerifyDownloadedSkillPackageSignatureEd25519(t *testing.T) {
	data := []byte(`{"id":"demo","name":"Demo Skill"}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	signature := ed25519.Sign(privateKey, data)
	value := "ed25519:" + base64.StdEncoding.EncodeToString(publicKey) + ":" + base64.StdEncoding.EncodeToString(signature)
	if err := verifyDownloadedSkillPackageSignature(data, value); err != nil {
		t.Fatalf("verifyDownloadedSkillPackageSignature() error = %v", err)
	}
	if err := verifyDownloadedSkillPackageSignature([]byte(`{"id":"tampered"}`), value); err == nil || !strings.Contains(strings.ToLower(err.Error()), "signature verification failed") {
		t.Fatalf("expected signature verification failure, got %v", err)
	}
	if err := verifyDownloadedSkillPackageSignature(data, "sig-ready"); err != nil {
		t.Fatalf("unsupported legacy signature metadata should be ignored, got %v", err)
	}
}

func TestVerifyDownloadedSkillPackageSignatureChecksPublicKeyFingerprint(t *testing.T) {
	data := []byte(`{"id":"demo","name":"Demo Skill"}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	signatureValue := map[string]any{
		"algorithm":              "ed25519",
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data)),
		"public_key_fingerprint": fingerprint,
	}
	signatureJSON, err := json.Marshal(signatureValue)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := verifyDownloadedSkillPackageSignature(data, string(signatureJSON)); err != nil {
		t.Fatalf("signature with matching public key fingerprint should pass: %v", err)
	}

	signatureValue["public_key_fingerprint"] = "sha256:" + strings.Repeat("0", 64)
	signatureJSON, err = json.Marshal(signatureValue)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := verifyDownloadedSkillPackageSignature(data, string(signatureJSON)); err == nil || !strings.Contains(strings.ToLower(err.Error()), "fingerprint mismatch") {
		t.Fatalf("expected fingerprint mismatch, got %v", err)
	}
}

func TestVerifyDownloadedSkillPackageSignatureRequiresTrustedFingerprintWhenConfigured(t *testing.T) {
	data := []byte(`{"id":"demo","name":"Demo Skill"}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	signatureValue := map[string]any{
		"algorithm":         "ed25519",
		"public_key_base64": base64.StdEncoding.EncodeToString(publicKey),
		"signature_base64":  base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data)),
	}
	signatureJSON, err := json.Marshal(signatureValue)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, string(signatureJSON), []string{strings.TrimPrefix(fingerprint, "sha256:")}); err != nil {
		t.Fatalf("signature with trusted public key fingerprint should pass: %v", err)
	}
	if err := verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, string(signatureJSON), []string{"sha256:" + strings.Repeat("1", 64)}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not trusted") {
		t.Fatalf("expected untrusted public key failure, got %v", err)
	}
}

func TestDownloadSkillJSONFromHubCenterRequiresConfiguredTrustedPackageKeyFingerprint(t *testing.T) {
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

	skillBody := []byte(`{"id":"demo","name":"Demo Skill","description":"demo","version":"1.0.0","steps":[{"action":"craft_tool","params":{"instructions":"do it"}}]}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	signatureValue := map[string]any{
		"algorithm":              "ed25519",
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, skillBody)),
		"public_key_fingerprint": fingerprint,
	}
	signatureJSON, err := json.Marshal(signatureValue)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/demo/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(skillBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}, TrustedSkillPackageKeyFingerprints: []string{strings.TrimPrefix(fingerprint, "sha256:")}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := downloadSkillJSONFromHubCenterToDirWithIntegrity(context.Background(), app, "/api/v1/skills/demo/download", "", "", string(signatureJSON)); err != nil {
		t.Fatalf("trusted package key fingerprint should pass: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.TrustedSkillPackageKeyFingerprints = []string{"sha256:" + strings.Repeat("2", 64)}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	_, err = downloadSkillJSONFromHubCenterToDirWithIntegrity(context.Background(), app, "/api/v1/skills/demo/download", "", "", string(signatureJSON))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not trusted") {
		t.Fatalf("expected untrusted package key failure, got %v", err)
	}
}

func TestSkillHubClientInstallToDirRequiresConfiguredTrustedPackageKeyFingerprint(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	skillBody := []byte(`{"id":"demo","name":"Demo Skill","description":"demo","version":"1.0.0","steps":[{"action":"craft_tool","params":{"instructions":"do it"}}]}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	signatureValue := map[string]any{
		"algorithm":              "ed25519",
		"public_key_base64":      base64.StdEncoding.EncodeToString(publicKey),
		"signature_base64":       base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, skillBody)),
		"public_key_fingerprint": fingerprint,
	}
	signatureJSON, err := json.Marshal(signatureValue)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/demo/download" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(skillBody)
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, TrustedSkillPackageKeyFingerprints: []string{"sha256:" + strings.Repeat("3", 64)}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	client := NewSkillHubClient(app)
	_, err = client.InstallToDirWithIntegrity(context.Background(), "demo", server.URL, "", "", string(signatureJSON))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not trusted") {
		t.Fatalf("expected untrusted package key failure, got %v", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	cfg.TrustedSkillPackageKeyFingerprints = []string{fingerprint}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := client.InstallToDirWithIntegrity(context.Background(), "demo", server.URL, "", "", string(signatureJSON)); err != nil {
		t.Fatalf("trusted package key fingerprint should pass: %v", err)
	}
}

func TestDownloadSkillJSONFromHubCenterVerifiesPackageSignature(t *testing.T) {
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

	skillBody := []byte(`{"id":"demo","name":"Demo Skill","description":"demo","version":"1.0.0","steps":[{"action":"craft_tool","params":{"instructions":"do it"}}]}`)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	validSignature := "ed25519:" + base64.StdEncoding.EncodeToString(publicKey) + ":" + base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, skillBody))
	badSignature := "ed25519:" + base64.StdEncoding.EncodeToString(publicKey) + ":" + base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte("tampered")))

	var backup *httptest.Server
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{backup.URL}})
		case "/api/v1/skills/demo/download":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(skillBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: "http://127.0.0.1:1", RemoteHubCenterURLs: []string{backup.URL}}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if _, err := downloadSkillJSONFromHubCenterToDirWithIntegrity(context.Background(), app, "/api/v1/skills/demo/download", "", "", validSignature); err != nil {
		t.Fatalf("valid package signature should pass: %v", err)
	}
	_, err = downloadSkillJSONFromHubCenterToDirWithIntegrity(context.Background(), app, "/api/v1/skills/demo/download", "", "", badSignature)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "signature verification failed") {
		t.Fatalf("expected signature verification failure, got %v", err)
	}
}

func TestDownloadSkillJSONSetsSkillDirForBundledFiles(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skill.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "direct-demo",
			"name":        "Direct Demo",
			"description": "demo",
			"version":     "1.0.0",
			"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "do it"}}},
			"files": map[string]string{
				"assets/logo.png": base64.StdEncoding.EncodeToString([]byte("png")),
			},
		})
	}))
	defer server.Close()

	skill, err := downloadSkillJSON(context.Background(), server.URL+"/skill.json")
	if err != nil {
		t.Fatalf("downloadSkillJSON: %v", err)
	}
	if skill.SkillDir == "" {
		t.Fatal("SkillDir is empty for direct bundled skill download")
	}
	if _, err := os.Stat(filepath.Join(skill.SkillDir, "assets", "logo.png")); err != nil {
		t.Fatalf("expected direct download to extract bundled file: %v", err)
	}
}

func TestDownloadSkillJSONUsesIDForBundledDirWhenNameMissing(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skill.json" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "id-only-demo",
			"description": "demo",
			"version":     "1.0.0",
			"steps":       []map[string]any{{"action": "craft_tool", "params": map[string]any{"instructions": "do it"}}},
			"files": map[string]string{
				"assets/logo.png": base64.StdEncoding.EncodeToString([]byte("png")),
			},
		})
	}))
	defer server.Close()

	skill, err := downloadSkillJSON(context.Background(), server.URL+"/skill.json")
	if err != nil {
		t.Fatalf("downloadSkillJSON: %v", err)
	}
	wantDir := filepath.Join(homeDir, ".maclaw", "data", "skills", "id-only-demo")
	if skill.SkillDir != wantDir {
		t.Fatalf("SkillDir = %q, want %q", skill.SkillDir, wantDir)
	}
	if _, err := os.Stat(filepath.Join(skill.SkillDir, "assets", "logo.png")); err != nil {
		t.Fatalf("expected id-only direct download to extract bundled file: %v", err)
	}
}

func TestSubmitSkill_FailsOverWhenSessionExpiredOnSelectedHub(t *testing.T) {
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

	var primaryHits int32
	var backupHits int32
	var primary *httptest.Server
	var backup *httptest.Server
	discovery := func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(struct {
			OK   bool     `json:"ok"`
			URLs []string `json:"urls"`
		}{OK: true, URLs: []string{primary.URL, backup.URL}})
	}
	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			discovery(w)
		case "/api/v1/skills/submit":
			atomic.AddInt32(&primaryHits, 1)
			http.Error(w, `{"error":"session expired or invalid"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer primary.Close()
	backup = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			discovery(w)
		case "/api/v1/skills/submit":
			atomic.AddInt32(&backupHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
				t.Errorf("Authorization = %q, want bearer token", got)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm() error = %v", err)
			}
			if got := r.FormValue("email"); got != "uploader@example.com" {
				t.Errorf("email = %q", got)
			}
			if _, _, err := r.FormFile("zip"); err != nil {
				t.Errorf("zip form file missing: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "sub-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer backup.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:  primary.URL,
		RemoteHubCenterURLs: []string{backup.URL},
		RemoteViewerToken:   "session-token",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkill() error = %v", err)
	}
	if id != "sub-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&primaryHits) == 0 || atomic.LoadInt32(&backupHits) == 0 {
		t.Fatalf("primary hits = %d, backup hits = %d", primaryHits, backupHits)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.RemoteHubCenterURL != backup.URL {
		t.Fatalf("RemoteHubCenterURL = %q, want %q", saved.RemoteHubCenterURL, backup.URL)
	}
}

func TestSubmitSkill_PrefersSkillMarketSessionToken(t *testing.T) {
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

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			if got := r.Header.Get("Authorization"); got != "Bearer skillmarket-session" {
				t.Errorf("Authorization = %q, want SkillMarket session token", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "sub-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		RemoteViewerToken:       "viewer-token",
		SkillMarketSessionToken: "skillmarket-session",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkill() error = %v", err)
	}
	if id != "sub-ok" {
		t.Fatalf("submission id = %q", id)
	}
}

func TestSubmitSkillToConfiguredTargetsEnterpriseOnlySkipsHubCenter(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubCenterHits int32
	var enterpriseHits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters", "/api/v1/skills/submit":
			atomic.AddInt32(&hubCenterHits, 1)
			http.Error(w, "hubcenter should not be used", http.StatusInternalServerError)
		case "/api/capabilities/skills/submit":
			atomic.AddInt32(&enterpriseHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("Authorization = %q, want viewer token", got)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: server.URL,
		RemoteHubURL:       server.URL,
		RemoteViewerToken:  "viewer-token",
		CapabilityMarketPolicy: corelib.CapabilityMarketPolicy{
			PreferredUploadTarget: corelib.CapabilitySourceEnterpriseHub,
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkillToConfiguredTargets() error = %v", err)
	}
	if id != "enterprise_hub=enterprise-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&enterpriseHits) != 1 || atomic.LoadInt32(&hubCenterHits) != 0 {
		t.Fatalf("enterprise hits = %d, hubcenter hits = %d", enterpriseHits, hubCenterHits)
	}
}

func TestSubmitSkillToConfiguredTargetsEnterpriseUsesSessionTokenFallback(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capabilities/skills/submit":
			capturedAuth = r.Header.Get("Authorization")
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm() error = %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-session-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: server.URL,
		CapabilityMarketPolicy: corelib.CapabilityMarketPolicy{
			PreferredUploadTarget: corelib.CapabilitySourceEnterpriseHub,
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = "skillmarket-session"
	}); err != nil {
		t.Fatalf("PatchConfig(SkillMarketSessionToken) error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkillToConfiguredTargets() error = %v", err)
	}
	if id != "enterprise_hub=enterprise-session-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if capturedAuth != "Bearer skillmarket-session" {
		t.Fatalf("Authorization = %q, want session token", capturedAuth)
	}
}
func TestSubmitSkillToConfiguredTargetsDefaultUploadsBothTargets(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubCenterHits int32
	var enterpriseHits int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			atomic.AddInt32(&hubCenterHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "hubcenter-ok"})
		case "/api/capabilities/skills/submit":
			atomic.AddInt32(&enterpriseHits, 1)
			if got := r.Header.Get("Authorization"); got != "Bearer viewer-token" {
				t.Errorf("Authorization = %q, want viewer token", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("SubmitSkillToConfiguredTargets() error = %v", err)
	}
	if id != "hubcenter-ok;enterprise_hub=enterprise-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&hubCenterHits) != 1 || atomic.LoadInt32(&enterpriseHits) != 1 {
		t.Fatalf("hubcenter hits = %d, enterprise hits = %d", hubCenterHits, enterpriseHits)
	}
}

func TestSubmitSkillRejectsEmptyHubCenterSubmissionID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": ""})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	bodyBytes, contentType, err := buildSkillSubmitMultipart(zipPath, "uploader@example.com")
	if err != nil {
		t.Fatalf("buildSkillSubmitMultipart() error = %v", err)
	}
	_, err = NewSkillMarketClient(app).submitSkillToHubCenter(context.Background(), []string{server.URL}, bodyBytes, contentType)
	if err == nil || !strings.Contains(err.Error(), "missing submission_id") {
		t.Fatalf("submitSkillToHubCenter() err = %v", err)
	}
}

func TestSubmitSkillToConfiguredTargetsRejectsEmptyEnterpriseSubmissionID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capabilities/skills/submit":
			_ = json.NewEncoder(w).Encode(map[string]string{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      server.URL,
		RemoteViewerToken: "viewer-token",
		CapabilityMarketPolicy: corelib.CapabilityMarketPolicy{
			PreferredUploadTarget: corelib.CapabilitySourceEnterpriseHub,
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewSkillMarketClient(app).SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	if err == nil || !strings.Contains(err.Error(), "missing submission_id") {
		t.Fatalf("SubmitSkillToConfiguredTargets() err = %v", err)
	}
}

func TestSubmitSkillToConfiguredTargetsPartialRetrySkipsCompletedTarget(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubCenterHits int32
	var enterpriseHits int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			atomic.AddInt32(&hubCenterHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "hubcenter-ok"})
		case "/api/capabilities/skills/submit":
			hit := atomic.AddInt32(&enterpriseHits, 1)
			if hit == 1 {
				http.Error(w, "temporary enterprise failure", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := NewSkillMarketClient(app)

	_, err := client.SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	var partial *skillSubmitPartialError
	if !errors.As(err, &partial) || partial.Completed[corelib.CapabilitySourceHubCenter] != "hubcenter-ok" {
		t.Fatalf("first submit err=%v partial=%+v", err, partial)
	}
	id, err := client.SubmitSkillToConfiguredTargetsWithCompleted(context.Background(), zipPath, "uploader@example.com", partial.Completed)
	if err != nil {
		t.Fatalf("retry submit error = %v", err)
	}
	if id != "hubcenter-ok;enterprise_hub=enterprise-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&hubCenterHits) != 1 || atomic.LoadInt32(&enterpriseHits) != 2 {
		t.Fatalf("hubcenter hits = %d, enterprise hits = %d", hubCenterHits, enterpriseHits)
	}
}

func TestSubmitSkillToConfiguredTargetsPartialRetrySkipsCompletedEnterpriseTarget(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubCenterHits int32
	var enterpriseHits int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			hit := atomic.AddInt32(&hubCenterHits, 1)
			if hit == 1 {
				http.Error(w, "temporary hubcenter failure", http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "hubcenter-ok"})
		case "/api/capabilities/skills/submit":
			atomic.AddInt32(&enterpriseHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubCenterURL: server.URL, RemoteHubURL: server.URL, RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := NewSkillMarketClient(app)

	_, err := client.SubmitSkillToConfiguredTargets(context.Background(), zipPath, "uploader@example.com")
	var partial *skillSubmitPartialError
	if !errors.As(err, &partial) || partial.Completed[corelib.CapabilitySourceEnterpriseHub] != "enterprise-ok" {
		t.Fatalf("first submit err=%v partial=%+v", err, partial)
	}
	id, err := client.SubmitSkillToConfiguredTargetsWithCompleted(context.Background(), zipPath, "uploader@example.com", partial.Completed)
	if err != nil {
		t.Fatalf("retry submit error = %v", err)
	}
	if id != "hubcenter-ok;enterprise_hub=enterprise-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&hubCenterHits) != 2 || atomic.LoadInt32(&enterpriseHits) != 1 {
		t.Fatalf("hubcenter hits = %d, enterprise hits = %d", hubCenterHits, enterpriseHits)
	}
}

func TestSubmitSkillToConfiguredTargetsDropsCompletedTargetsOutsideCurrentPolicy(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	var hubCenterHits int32
	var enterpriseHits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters", "/api/v1/skills/submit":
			atomic.AddInt32(&hubCenterHits, 1)
			http.Error(w, "hubcenter should not be used", http.StatusInternalServerError)
		case "/api/capabilities/skills/submit":
			atomic.AddInt32(&enterpriseHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{"submission_id": "enterprise-ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL: server.URL,
		RemoteHubURL:       server.URL,
		RemoteViewerToken:  "viewer-token",
		CapabilityMarketPolicy: corelib.CapabilityMarketPolicy{
			PreferredUploadTarget: corelib.CapabilitySourceEnterpriseHub,
		},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	id, err := NewSkillMarketClient(app).SubmitSkillToConfiguredTargetsWithCompleted(context.Background(), zipPath, "uploader@example.com", map[string]string{corelib.CapabilitySourceHubCenter: "old-hubcenter"})
	if err != nil {
		t.Fatalf("SubmitSkillToConfiguredTargetsWithCompleted() error = %v", err)
	}
	if id != "enterprise_hub=enterprise-ok" {
		t.Fatalf("submission id = %q", id)
	}
	if atomic.LoadInt32(&hubCenterHits) != 0 || atomic.LoadInt32(&enterpriseHits) != 1 {
		t.Fatalf("hubcenter hits = %d, enterprise hits = %d", hubCenterHits, enterpriseHits)
	}
}

func TestSubmitSkill_AllAuthFailuresReturnAuthExpiredMessage(t *testing.T) {
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

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(struct {
				OK   bool     `json:"ok"`
				URLs []string `json:"urls"`
			}{OK: true, URLs: []string{server.URL}})
		case "/api/v1/skills/submit":
			http.Error(w, `{"error":"session expired or invalid"}`, http.StatusUnauthorized)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubCenterURL:      server.URL,
		SkillMarketSessionToken: "expired-session",
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	zipPath := tmpHome + "/skill.zip"
	if err := os.WriteFile(zipPath, []byte("zip bytes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewSkillMarketClient(app).SubmitSkill(context.Background(), zipPath, "uploader@example.com")
	if err == nil {
		t.Fatal("SubmitSkill() succeeded with expired auth")
	}
	if !strings.Contains(err.Error(), "SkillMarket 认证失败或已过期") {
		t.Fatalf("SubmitSkill() error = %v, want auth expired message", err)
	}
}
