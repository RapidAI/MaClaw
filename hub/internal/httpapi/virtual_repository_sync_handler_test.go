package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func doVirtualRepositorySyncRequest(t *testing.T, handler http.Handler, method, machineID, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/api/virtual-repositories/sync", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestVirtualRepositorySyncEncryptsAndIsolatesUsers(t *testing.T) {
	base := t.TempDir()
	authenticator := fakeVEMachineAuth{token: "secret", principals: map[string]*auth.MachinePrincipal{
		"machine-a": {TenantID: "tenant-a", UserID: "user-a", MachineID: "machine-a"},
		"machine-b": {TenantID: "tenant-a", UserID: "user-b", MachineID: "machine-b"},
	}}
	handler := VirtualRepositorySyncHandler(authenticator, base)
	payload := map[string]any{"version": 1, "credentials": map[string]any{"git": map[string]any{"secret": "top-secret"}}}
	put := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine-a", "secret", map[string]any{"payload": payload})
	if put.Code != http.StatusOK {
		t.Fatalf("put = %d: %s", put.Code, put.Body.String())
	}
	revision := strings.TrimSpace(put.Header().Get("X-Virtual-Repository-Sync-Revision"))
	if revision != "" {
		t.Fatalf("response should keep revision in JSON only, header=%q", revision)
	}
	get := doVirtualRepositorySyncRequest(t, handler, http.MethodGet, "machine-a", "secret", nil)
	if get.Code != http.StatusOK || !bytes.Contains(get.Body.Bytes(), []byte("top-secret")) {
		t.Fatalf("owner get = %d: %s", get.Code, get.Body.String())
	}
	other := doVirtualRepositorySyncRequest(t, handler, http.MethodGet, "machine-b", "secret", nil)
	if other.Code != http.StatusOK || bytes.Contains(other.Body.Bytes(), []byte("top-secret")) {
		t.Fatalf("other get = %d: %s", other.Code, other.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(base, "users"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("stored user documents = %d, err=%v", len(entries), err)
	}
	raw, err := os.ReadFile(filepath.Join(base, "users", entries[0].Name(), "record.json"))
	if err != nil || bytes.Contains(raw, []byte("top-secret")) {
		t.Fatalf("encrypted document leaked secret: %s err=%v", raw, err)
	}
}

func TestVirtualRepositorySyncRejectsStaleRevision(t *testing.T) {
	base := t.TempDir()
	handler := VirtualRepositorySyncHandler(fakeVEMachineAuth{token: "secret", principals: map[string]*auth.MachinePrincipal{"machine": {TenantID: "tenant", UserID: "user", MachineID: "machine"}}}, base)
	first := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1, "value": "one"}})
	if first.Code != http.StatusOK {
		t.Fatalf("first = %d: %s", first.Code, first.Body.String())
	}
	var view virtualRepositorySyncView
	if err := json.Unmarshal(first.Body.Bytes(), &view); err != nil || view.Revision == "" {
		t.Fatalf("view = %+v err=%v", view, err)
	}
	conflict := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1, "value": "two"}, "if_match_revision": "stale"})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "VREPO_SYNC_CONFLICT") {
		t.Fatalf("conflict = %d: %s", conflict.Code, conflict.Body.String())
	}
	ok := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1, "value": "two"}, "if_match_revision": view.Revision})
	if ok.Code != http.StatusOK {
		t.Fatalf("matched put = %d: %s", ok.Code, ok.Body.String())
	}
}

func TestVirtualRepositorySyncCreateOnlyPreconditionRejectsConcurrentFirstSync(t *testing.T) {
	base := t.TempDir()
	handler := VirtualRepositorySyncHandler(fakeVEMachineAuth{token: "secret", principals: map[string]*auth.MachinePrincipal{"machine": {TenantID: "tenant", UserID: "user", MachineID: "machine"}}}, base)
	first := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1, "value": "first"}, "if_match_revision": "*"})
	if first.Code != http.StatusOK {
		t.Fatalf("first create-only PUT = %d: %s", first.Code, first.Body.String())
	}
	second := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1, "value": "second"}, "if_match_revision": "*"})
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), "VREPO_SYNC_CONFLICT") {
		t.Fatalf("concurrent create-only PUT = %d: %s", second.Code, second.Body.String())
	}
	get := doVirtualRepositorySyncRequest(t, handler, http.MethodGet, "machine", "secret", nil)
	if get.Code != http.StatusOK || !bytes.Contains(get.Body.Bytes(), []byte(`"value":"first"`)) {
		t.Fatalf("winner was overwritten: %d: %s", get.Code, get.Body.String())
	}
}

func TestVirtualRepositorySyncRejectsTrailingJSON(t *testing.T) {
	base := t.TempDir()
	handler := VirtualRepositorySyncHandler(fakeVEMachineAuth{token: "secret", principals: map[string]*auth.MachinePrincipal{"machine": {TenantID: "tenant", UserID: "user", MachineID: "machine"}}}, base)
	req := httptest.NewRequest(http.MethodPut, "/api/virtual-repositories/sync", strings.NewReader(`{"payload":{"version":1}} {"extra":true}`))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Machine-ID", "machine")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestVirtualRepositorySyncUsesAtomicRecordAndAcceptsLegacyDocuments(t *testing.T) {
	base := t.TempDir()
	principal := &auth.MachinePrincipal{TenantID: "tenant", UserID: "user", MachineID: "machine"}
	handler := VirtualRepositorySyncHandler(fakeVEMachineAuth{token: "secret", principals: map[string]*auth.MachinePrincipal{"machine": principal}}, base)
	put := doVirtualRepositorySyncRequest(t, handler, http.MethodPut, "machine", "secret", map[string]any{"payload": map[string]any{"version": 1}})
	if put.Code != http.StatusOK {
		t.Fatalf("put = %d: %s", put.Code, put.Body.String())
	}
	dir := virtualRepositorySyncDir(base, principal)
	if _, err := os.Stat(filepath.Join(dir, "record.json")); err != nil {
		t.Fatalf("atomic record missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); !os.IsNotExist(err) {
		t.Fatalf("new writes should not create legacy meta.json, err=%v", err)
	}

	plain := []byte(`{"version":1,"legacy":true}`)
	stored, err := encryptVirtualRepositorySync(base, principal, plain)
	if err != nil {
		t.Fatal(err)
	}
	meta := &virtualRepositorySyncMeta{TenantID: principal.TenantID, UserID: principal.UserID, Revision: virtualRepositorySyncRevision(plain), CreatedAt: "2026-07-23T00:00:00Z", UpdatedAt: "2026-07-23T00:00:00Z"}
	if err := os.Remove(filepath.Join(dir, "record.json")); err != nil {
		t.Fatal(err)
	}
	metaRaw, _ := json.Marshal(meta)
	storedRaw, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "document.json"), storedRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	get := doVirtualRepositorySyncRequest(t, handler, http.MethodGet, "machine", "secret", nil)
	if get.Code != http.StatusOK || !bytes.Contains(get.Body.Bytes(), []byte(`"legacy":true`)) {
		t.Fatalf("legacy get = %d: %s", get.Code, get.Body.String())
	}
}

func TestVirtualRepositorySyncRejectsRecordForAnotherUser(t *testing.T) {
	base := t.TempDir()
	owner := &auth.MachinePrincipal{TenantID: "tenant", UserID: "owner", MachineID: "machine"}
	other := &auth.MachinePrincipal{TenantID: "tenant", UserID: "other", MachineID: "other-machine"}
	plain := []byte(`{"version":1}`)
	stored, err := encryptVirtualRepositorySync(base, owner, plain)
	if err != nil {
		t.Fatal(err)
	}
	dir := virtualRepositorySyncDir(base, other)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := virtualRepositorySyncRecord{Meta: virtualRepositorySyncMeta{TenantID: owner.TenantID, UserID: owner.UserID, Revision: virtualRepositorySyncRevision(plain)}, Document: *stored}
	raw, _ := json.Marshal(record)
	if err := os.WriteFile(filepath.Join(dir, "record.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadVirtualRepositorySyncDocument(base, other); err == nil {
		t.Fatal("cross-user record was accepted")
	}
}
