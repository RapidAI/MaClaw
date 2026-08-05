package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestUnzipToDirRejectsUnsafeEntries(t *testing.T) {
	for _, entries := range [][]string{{"config.json", "CONFIG.JSON"}, {"../escape.txt"}, {"assets/NUL.txt"}, {"assets/file. "}} {
		path := filepath.Join(t.TempDir(), "migration.zip")
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(out)
		for _, name := range entries {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("x"))
		}
		_ = zw.Close()
		_ = out.Close()
		if err := unzipToDir(path, t.TempDir()); err == nil {
			t.Fatalf("unsafe entries accepted: %v", entries)
		}
	}
}

func TestReadJSONFileLimitedRejectsOversizeAndUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(`{"version":"v1","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var manifest migrationManifest
	if err := readJSONFileLimited(path, &manifest, 1024); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field error = %v", err)
	}
	if err := readJSONFileLimited(path, &manifest, 8); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized JSON error = %v", err)
	}
}

func TestVerifyFileDigestsRejectsUndeclaredFiles(t *testing.T) {
	root := t.TempDir()
	declaredPath := filepath.Join(root, "memory_snapshot.json")
	if err := os.WriteFile(declaredPath, []byte("memory"), 0o600); err != nil {
		t.Fatal(err)
	}
	sha, size, err := fileSHA256(declaredPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "undeclared.txt"), []byte("extra"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = verifyFileDigests(root, []migrationFileDigest{{Path: "memory_snapshot.json", Bytes: size, SHA256: sha}})
	if err == nil || !strings.Contains(err.Error(), "file set") {
		t.Fatalf("undeclared file error = %v", err)
	}
}

func TestMigrationHubBytesLimitedRejectsOversizedResponse(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 9))
	}))
	defer hub.Close()
	_, err := (&HTTPServer{}).migrationHubBytesLimited(context.Background(), migrationClientConfig{HubURL: hub.URL, MachineID: "machine", ViewerToken: "token"}, http.MethodGet, "/chunk", nil, 8)
	if err == nil || !strings.Contains(err.Error(), "response exceeds") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestDecodeMigrationJSONRejectsTrailingValues(t *testing.T) {
	var out map[string]interface{}
	if err := decodeMigrationJSON([]byte(`{"ok":true}{"extra":true}`), &out); err == nil {
		t.Fatal("trailing Hub JSON value was accepted")
	}
}

func TestDecryptMigrationFileRejectsAmbiguousEncryptedHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ambiguous.enc")
	header := `{"version":"` + migrationPackageVersion + `","Version":"shadow","kdf":"argon2id","time":3,"memory_kb":65536,"threads":4,"salt":"AAAAAAAAAAAAAAAAAAAAAA==","nonce":"AAAAAAAAAAAAAAAA","plain_sha256":"` + strings.Repeat("a", 64) + `","plain_size":1}`
	var raw bytes.Buffer
	raw.WriteString(migrationMagic)
	if err := binary.Write(&raw, binary.BigEndian, uint32(len(header))); err != nil {
		t.Fatal(err)
	}
	raw.WriteString(header)
	if err := os.WriteFile(path, raw.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	err := decryptMigrationFile(path, filepath.Join(dir, "out.zip"), "password", "")
	if err == nil || !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("expected ambiguous encrypted header rejection, got %v", err)
	}
}

func TestDecryptMigrationFileRejectsTamperedPlainSize(t *testing.T) {
	dir := t.TempDir()
	plainPath := filepath.Join(dir, "plain.zip")
	plain := []byte("plain migration package")
	if err := os.WriteFile(plainPath, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(plain)
	plainHash := hex.EncodeToString(digest[:])
	encryptedPath := filepath.Join(dir, "package.enc")
	if err := encryptMigrationFile(plainPath, encryptedPath, "correct-password", plainHash); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatal(err)
	}
	headerOffset := len(migrationMagic)
	var headerLen uint32
	if err := binary.Read(bytes.NewReader(data[headerOffset:]), binary.BigEndian, &headerLen); err != nil {
		t.Fatal(err)
	}
	headerStart := headerOffset + 4
	headerEnd := headerStart + int(headerLen)
	var header encryptedMigrationHeader
	if err := json.Unmarshal(data[headerStart:headerEnd], &header); err != nil {
		t.Fatal(err)
	}
	header.PlainSize++
	mutatedHeader, err := json.Marshal(header)
	if err != nil || len(mutatedHeader) != int(headerLen) {
		t.Fatalf("mutate header err=%v old=%d new=%d", err, headerLen, len(mutatedHeader))
	}
	copy(data[headerStart:headerEnd], mutatedHeader)
	if err := os.WriteFile(encryptedPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = decryptMigrationFile(encryptedPath, filepath.Join(dir, "out.zip"), "correct-password", "")
	if err == nil {
		t.Fatal("tampered declared plain size was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out.zip")); !os.IsNotExist(statErr) {
		t.Fatalf("failed decryption left a plaintext file: %v", statErr)
	}
}

func TestReadJSONFileLimitedRejectsAmbiguousManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	data := []byte(`{"version":"` + migrationPackageVersion + `","Version":"shadow"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var manifest migrationManifest
	err := readJSONFileLimited(path, &manifest, migrationMaxManifest)
	if err == nil || !strings.Contains(err.Error(), "duplicate field") {
		t.Fatalf("expected ambiguous manifest rejection, got %v", err)
	}
}

func TestMigrationConfigFallsBackToHubCenterURL(t *testing.T) {
	hub := httptest.NewServer(http.NotFoundHandler())
	defer hub.Close()

	var center *httptest.Server
	center = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"routable":      true,
				"quality_score": 1000,
				"features": map[string]bool{
					"can_resolve": true,
				},
			})
		case "/api/client/hubcenters":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"urls": []string{center.URL},
			})
		case "/api/entry/resolve":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode resolve body: %v", err)
			}
			if req["email"] != "user@example.com" {
				t.Fatalf("resolve email = %q, want user@example.com", req["email"])
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"email":          "user@example.com",
				"mode":           "direct",
				"default_hub_id": "hub-1",
				"hubs": []map[string]string{
					{
						"hub_id":   "hub-1",
						"name":     "Hub 1",
						"base_url": hub.URL,
						"status":   "online",
					},
				},
			})
		default:
			t.Fatalf("unexpected hub center path: %s", r.URL.Path)
		}
	}))
	defer center.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{
		RemoteHubCenterURL: center.URL,
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "machine-1",
		RemoteMachineName:  "Machine 1",
		RemoteViewerToken:  "viewer-token",
		RemoteTenantID:     tenant.ID,
	}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	cfg, err := (&HTTPServer{svc: svc}).migrationConfig(context.Background(), principal)
	if err != nil {
		t.Fatalf("migrationConfig: %v", err)
	}
	if cfg.HubURL != hub.URL {
		t.Fatalf("HubURL = %q, want resolved Hub URL %q", cfg.HubURL, hub.URL)
	}
	if cfg.MachineID != "machine-1" || cfg.ViewerToken != "viewer-token" {
		t.Fatalf("unexpected migration config: %#v", cfg)
	}
}

func TestRunMigrationImportRequiresPasswordForReadyExport(t *testing.T) {
	var aborted int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/migration/imports/export-1/claim":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"export":{"export_id":"export-1","status":"ready","chunk_count":1,"chunk_size":8,"encrypted_size":8,"encrypted_sha256":"abc"}}`))
		case "/api/v1/migration/imports/export-1/abort":
			atomic.AddInt32(&aborted, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	srv := &HTTPServer{svc: svc}
	_, err = srv.runMigrationImport(context.Background(), agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"}, migrationClientConfig{
		HubURL:      hub.URL,
		ViewerToken: "viewer-token",
		MachineID:   "machine-1",
	}, "export-1", "", func(float64, string) {})
	if err == nil || !strings.Contains(err.Error(), "migration password is required") {
		t.Fatalf("expected password required error, got %v", err)
	}
	if atomic.LoadInt32(&aborted) != 1 {
		t.Fatalf("expected claimed export to be aborted once, got %d", aborted)
	}
}

func TestHandleMigrationImportRejectsMissingPasswordBeforeHubAccess(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/migration/import", strings.NewReader(`{"export_id":"export-1"}`))
	rec := httptest.NewRecorder()

	(&HTTPServer{}).handleMigrationImport(rec, req, agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !strings.Contains(body["error"], "migration password is required") {
		t.Fatalf("error = %q", body["error"])
	}
}

func TestRunMigrationImportCleanupRetryDoesNotRequirePassword(t *testing.T) {
	var completed int32
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/migration/imports/export-1/claim":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"export":{"export_id":"export-1","status":"imported","claimed_by_machine_id":"machine-1"}}`))
		case "/api/v1/migration/imports/export-1/complete":
			atomic.AddInt32(&completed, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected hub path: %s", r.URL.Path)
		}
	}))
	defer hub.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	result, err := (&HTTPServer{svc: svc}).runMigrationImport(context.Background(), agentservice.Principal{TenantID: "tenant-1", UserID: "user-1"}, migrationClientConfig{
		HubURL:      hub.URL,
		ViewerToken: "viewer-token",
		MachineID:   "machine-1",
	}, "export-1", "", func(float64, string) {})
	if err != nil {
		t.Fatalf("runMigrationImport cleanup retry: %v", err)
	}
	if result["cleanup_retried"] != true {
		t.Fatalf("cleanup_retried = %#v, want true; result=%#v", result["cleanup_retried"], result)
	}
	if atomic.LoadInt32(&completed) != 1 {
		t.Fatalf("expected complete to be called once, got %d", completed)
	}
}
