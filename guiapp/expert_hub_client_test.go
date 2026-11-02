package guiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

func signedInExpertHubConfig() corelib.AppConfig {
	return corelib.AppConfig{
		RemoteHubURL:           "https://hub.example.test/root/",
		RemoteMachineToken:     "token",
		RemoteMachineID:        "machine-1",
		LogDetailEnabled:       corelib.IsLogDetailEnabled(),
		MemoryRecallLogEnabled: memory.IsMemoryRecallLogEnabled(),
	}
}

func TestNewExpertHubClientRefusesRealProfileDuringTests(t *testing.T) {
	// Credentials already in memory must not count as opt-in. SaveExpert on
	// &App{} previously posted through whichever login LoadConfig returned.
	app := &App{configCacheValid: true, configCache: signedInExpertHubConfig()}
	client, err := app.newExpertHubClient()
	if client != nil || err == nil || !strings.Contains(err.Error(), "no isolated config") {
		t.Fatalf("in-memory credentials opened the hub: client=%v err=%v", client, err)
	}
	app.testHomeDir = "  "
	client, err = app.newExpertHubClient()
	if client != nil || err == nil || !strings.Contains(err.Error(), "no isolated config") {
		t.Fatalf("blank test home opened the hub: client=%v err=%v", client, err)
	}
}

func TestNewExpertHubClientUsesIsolatedConfig(t *testing.T) {
	app := &App{
		testHomeDir:      t.TempDir(),
		configCacheValid: true,
		configCache:      signedInExpertHubConfig(),
	}
	client, err := app.newExpertHubClient()
	if err != nil {
		t.Fatalf("isolated config: %v", err)
	}
	if client.baseURL != "https://hub.example.test/root" || client.token != "token" || client.machineID != "machine-1" {
		t.Fatalf("client = %+v", client)
	}
}

func TestUnisolatedListExpertsDoesNotCallHub(t *testing.T) {
	swapExpertStoreForTest(t)
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		t.Errorf("unisolated app called %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	cfg := signedInExpertHubConfig()
	cfg.RemoteHubURL = server.URL
	if err := defaultExpertStore.Save(ExpertDefinition{
		ID: "expert-pending", Name: "pending", SystemPrompt: "p",
		CreatedAt: "2026-08-01T00:00:00Z", UpdatedAt: "2026-08-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := defaultExpertStore.MarkPendingHubUpload("expert-pending"); err != nil {
		t.Fatal(err)
	}
	app := &App{configCacheValid: true, configCache: cfg}
	if _, err := app.ListExperts(); err != nil {
		t.Fatal(err)
	}
	if called.Load() {
		t.Fatal("list reached the hub")
	}
	if !defaultExpertStore.PendingHubUploadIsCurrent("expert-pending", "2026-08-01T00:00:00Z") {
		t.Fatal("list cleared a pending upload without a hub round trip")
	}
}

func TestIsolatedListExpertsUsesItsOwnHub(t *testing.T) {
	swapExpertStoreForTest(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/experts" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"experts":[]}`))
	}))
	t.Cleanup(server.Close)
	cfg := signedInExpertHubConfig()
	cfg.RemoteHubURL = server.URL
	app := &App{testHomeDir: t.TempDir(), configCacheValid: true, configCache: cfg}
	if _, err := app.ListExperts(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("hub calls = %d, want 1", calls.Load())
	}
}

func TestSyncSkipsSignedInExpertProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "experts.json")
	ambientExpertProfileTestPath.Store(path)
	t.Cleanup(func() { ambientExpertProfileTestPath.Store("") })
	previous := defaultExpertStore
	defaultExpertStore = newExpertStore(path)
	t.Cleanup(func() { defaultExpertStore = previous })
	raw, err := json.Marshal(expertStoreFile{
		Experts:           []ExpertDefinition{{ID: "expert-pending", Name: "pending", SystemPrompt: "secret", CreatedAt: "2026-08-01T00:00:00Z", UpdatedAt: "2026-08-01T00:00:00Z"}},
		PendingHubUploads: map[string]bool{"expert-pending": true},
		PendingHubDeletes: map[string]string{"expert-gone": "2026-08-01T00:00:00Z"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Errorf("signed-in profile sync called %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(server.Close)
	client := &expertHubClient{
		baseURL: server.URL, machineID: "machine-1", token: "token", httpClient: server.Client(),
	}
	(&App{testHomeDir: t.TempDir()}).syncPendingExpertsToHubLocked(client, nil)
	if calls.Load() != 0 {
		t.Fatalf("hub calls = %d", calls.Load())
	}
}

func TestUnisolatedExpertSaveDoesNotArmHubRetry(t *testing.T) {
	swapExpertStoreForTest(t)
	app := &App{}
	raw, err := app.SaveExpert(`{"name":"local only","system_prompt":"p"}`)
	if err != nil {
		t.Fatal(err)
	}
	var saved ExpertDefinition
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if _, _, uploads, deletes, err := defaultExpertStore.ListForHubSync(); err != nil || len(uploads) != 0 || len(deletes) != 0 {
		t.Fatalf("save armed a hub retry: uploads=%v deletes=%v err=%v", uploads, deletes, err)
	}
	if err := app.DeleteExpert(saved.ID); err != nil {
		t.Fatal(err)
	}
	_, tombstones, uploads, deletes, err := defaultExpertStore.ListForHubSync()
	if err != nil {
		t.Fatal(err)
	}
	if tombstones[saved.ID] == "" {
		t.Fatal("local delete did not keep its tombstone")
	}
	if len(uploads) != 0 || len(deletes) != 0 {
		t.Fatalf("delete armed a hub retry: uploads=%v deletes=%v", uploads, deletes)
	}
}
