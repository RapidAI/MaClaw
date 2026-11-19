package desktopd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

func TestPanelResourcesOverrideTheRequestedSize(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	if _, err := SavePanelResources(dir, "8g", "2", "1g"); err != nil {
		t.Fatal(err)
	}
	spec, err := normalize(Spec{TenantID: "tenant-a", UserID: "alice", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Memory != "8g" || spec.CPUs != "2" || spec.ShmSize != "1g" {
		t.Fatalf("panel size = %+v", spec)
	}
	if err := ClearPanelResources(dir); err != nil {
		t.Fatal(err)
	}
	spec, err = normalize(Spec{TenantID: "tenant-a", UserID: "alice", Memory: "3g", CPUs: "1.5", ShmSize: "1g"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Memory != "3g" || spec.CPUs != "1.5" {
		t.Fatalf("cleared size = %+v", spec)
	}
}

func TestAdminResourceSaveRejectsAnEmptySize(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	admin := newAdminServer(&Service{}, "primary-token-value-0123456789", dir, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"memory":"","cpus":"","shm_size":""}`))
	rec := httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "desktop_resources.json")); !os.IsNotExist(err) {
		t.Fatalf("empty save wrote a file: %v", err)
	}
}

func TestAdminResourceSaveNamesTheBadField(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	admin := newAdminServer(&Service{}, "primary-token-value-0123456789", dir, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"memory":"4g","cpus":"0","shm_size":"1g"}`))
	rec := httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "CPU") {
		t.Fatalf("cpu status %d body %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"memory":"4","cpus":"2","shm_size":"1g"}`))
	rec = httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "4g") {
		t.Fatalf("memory status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestAdminRestoreKeepsTheLocalSizeWhenHubRejectsIt(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	if _, err := SavePanelResources(dir, "8g", "2", "1g"); err != nil {
		t.Fatal(err)
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"desktop size was not stored"}`))
	}))
	defer hub.Close()
	admin := newAdminServer(&Service{HubLink: &HubLink{creds: edgelink.Credentials{
		HubURL: hub.URL, NodeID: "node-1", NodeSecret: "node-secret",
	}}}, "primary-token-value-0123456789", dir, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"restore_auto":true}`))
	rec := httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	choice := panelResourceChoice()
	if choice == nil || choice.Memory != "8g" {
		t.Fatalf("local size dropped: %+v", choice)
	}
}

func TestAdminSaveTellsTheOperatorWhenHubHasNoSizeRoute(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer hub.Close()
	admin := newAdminServer(&Service{HubLink: &HubLink{creds: edgelink.Credentials{
		HubURL: hub.URL, NodeID: "node-1", NodeSecret: "node-secret",
	}}}, "primary-token-value-0123456789", dir, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"memory":"4","cpus":"2","shm_size":"1g"}`))
	rec := httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unitless status %d body %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"memory":"4g","cpus":"2","shm_size":"1g"}`))
	rec = httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"hub_saved":false`) {
		t.Fatalf("save status %d body %s", rec.Code, rec.Body.String())
	}
	if panelResourceChoice() == nil || panelResourceChoice().Memory != "4g" {
		t.Fatal("local size was not kept")
	}
	req = httptest.NewRequest(http.MethodPost, "/admin/api/resources", strings.NewReader(`{"restore_auto":true}`))
	rec = httptest.NewRecorder()
	admin.handleResources(rec, req)
	if rec.Code != http.StatusOK || panelResourceChoice() != nil {
		t.Fatalf("restore status %d choice %+v body %s", rec.Code, panelResourceChoice(), rec.Body.String())
	}
}

func TestSyncDesktopSizePinsTheSavedChoice(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() { panelResources.Store(nil) })
	if _, err := SavePanelResources(dir, "8g", "2", "1g"); err != nil {
		t.Fatal(err)
	}
	var gotPath, gotMemory string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Memory string `json:"memory"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotMemory = body.Memory
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hub.Close()
	link := &HubLink{creds: edgelink.Credentials{HubURL: hub.URL, NodeID: "node-1", NodeSecret: "node-secret"}}
	link.SyncDesktopSize(context.Background())
	if gotPath != "/api/v1/edge/resources" || gotMemory != "8g" {
		t.Fatalf("hub saw path %s memory %s", gotPath, gotMemory)
	}
}
