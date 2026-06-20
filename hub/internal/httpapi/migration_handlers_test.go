package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestMigrationAPILifecycleAndCleanupIdempotency(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-a", TenantID: "tenant-a", Email: "user@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target", "target-token", "New Mac")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	payload := bytes.Repeat([]byte("migration-payload"), 2048)
	payloadHash := migrationSHA256Hex(payload)
	createResp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"compressed_size":  int64(len(payload)),
		"encrypted_size":   int64(len(payload)),
		"encrypted_sha256": payloadHash,
		"plain_sha256":     payloadHash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
		"manifest":         map[string]any{"version": "test"},
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create export status=%d body=%s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		ExportID string `json:"export_id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil || created.ExportID == "" {
		t.Fatalf("decode create response err=%v body=%s", err, createResp.Body.String())
	}

	targetUpload := migrationAPIRawRequest(t, mux, http.MethodPut, "/api/v1/migration/exports/"+created.ExportID+"/chunks/0?sha256="+payloadHash, "machine-target", "target-token", payload)
	if targetUpload.Code != http.StatusForbidden {
		t.Fatalf("target upload status=%d body=%s", targetUpload.Code, targetUpload.Body.String())
	}

	sourceUpload := migrationAPIRawRequest(t, mux, http.MethodPut, "/api/v1/migration/exports/"+created.ExportID+"/chunks/0?sha256="+payloadHash, "machine-source", "source-token", payload)
	if sourceUpload.Code != http.StatusOK {
		t.Fatalf("source upload status=%d body=%s", sourceUpload.Code, sourceUpload.Body.String())
	}
	completeUpload := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports/"+created.ExportID+"/complete-upload", "machine-source", "source-token", map[string]any{"encrypted_sha256": payloadHash})
	if completeUpload.Code != http.StatusOK {
		t.Fatalf("complete upload status=%d body=%s", completeUpload.Code, completeUpload.Body.String())
	}

	instances := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/instances", "machine-target", "target-token", nil)
	if instances.Code != http.StatusOK || !bytes.Contains(instances.Body.Bytes(), []byte("Old Mac")) || !bytes.Contains(instances.Body.Bytes(), []byte(`"has_export":true`)) {
		t.Fatalf("instances status=%d body=%s", instances.Code, instances.Body.String())
	}

	claim := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/"+created.ExportID+"/claim", "machine-target", "target-token", map[string]any{})
	if claim.Code != http.StatusOK || !bytes.Contains(claim.Body.Bytes(), []byte(`"status":"importing"`)) {
		t.Fatalf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	reclaim := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/"+created.ExportID+"/claim", "machine-target", "target-token", map[string]any{})
	if reclaim.Code != http.StatusOK {
		t.Fatalf("reclaim same machine status=%d body=%s", reclaim.Code, reclaim.Body.String())
	}
	download := migrationAPIRawRequest(t, mux, http.MethodGet, "/api/v1/migration/imports/"+created.ExportID+"/chunks/0", "machine-target", "target-token", nil)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), payload) {
		t.Fatalf("download status=%d len=%d", download.Code, download.Body.Len())
	}

	completeImport := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/"+created.ExportID+"/complete", "machine-target", "target-token", map[string]any{})
	if completeImport.Code != http.StatusOK || !bytes.Contains(completeImport.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("complete import status=%d body=%s", completeImport.Code, completeImport.Body.String())
	}
	reclaimDeleted := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/"+created.ExportID+"/claim", "machine-target", "target-token", map[string]any{})
	if reclaimDeleted.Code != http.StatusOK || !bytes.Contains(reclaimDeleted.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("reclaim deleted status=%d body=%s", reclaimDeleted.Code, reclaimDeleted.Body.String())
	}
	completeAgain := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/"+created.ExportID+"/complete", "machine-target", "target-token", map[string]any{})
	if completeAgain.Code != http.StatusOK || !bytes.Contains(completeAgain.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("complete again status=%d body=%s", completeAgain.Code, completeAgain.Body.String())
	}

	var chunks int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_data_migration_chunks WHERE export_id = ?`, created.ExportID).Scan(&chunks); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunks != 0 {
		t.Fatalf("expected chunks removed, got %d", chunks)
	}
	if _, err := os.Stat(api.exportDir("tenant-a", user.ID, created.ExportID)); !os.IsNotExist(err) {
		t.Fatalf("expected export dir removed, stat err=%v", err)
	}
}

func newMigrationAPITestStore(t *testing.T) (*store.Store, *sql.DB, func()) {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "migration-api.db")})
	if err != nil {
		t.Fatalf("new sqlite provider: %v", err)
	}
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		_ = provider.Close()
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	return st, provider.Write, func() { _ = provider.Close() }
}

func seedMigrationAPIMachine(t *testing.T, st *store.Store, tenantID, userID, machineID, token, name string) {
	t.Helper()
	now := time.Now().UTC()
	if err := st.Machines.Create(context.Background(), &store.Machine{
		ID:               machineID,
		TenantID:         tenantID,
		UserID:           userID,
		ClientID:         machineID + "-client",
		Name:             name,
		Platform:         "darwin",
		MachineTokenHash: migrationAPITestTokenHash(token),
		Status:           "offline",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("seed machine %s: %v", machineID, err)
	}
}

func migrationAPIRequest(t *testing.T, handler http.Handler, method, path, machineID, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func migrationAPIRawRequest(t *testing.T, handler http.Handler, method, path, machineID, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func migrationAPITestTokenHash(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
