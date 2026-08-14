package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		"encrypted_size":   int64(len(payload)),
		"encrypted_sha256": payloadHash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
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

func TestMigrationChunkUploadReplacesExistingChunk(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-replace", TenantID: "tenant-replace", Email: "replace@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-replace", "replace-token", "Replace Mac")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	firstPayload := bytes.Repeat([]byte("a"), 1024)
	secondPayload := bytes.Repeat([]byte("b"), len(firstPayload))
	createResp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-replace", "replace-token", map[string]any{
		"encrypted_size":   int64(len(firstPayload)),
		"encrypted_sha256": migrationSHA256Hex(secondPayload),
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
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

	upload := func(payload []byte) {
		t.Helper()
		hash := migrationSHA256Hex(payload)
		resp := migrationAPIRawRequest(t, mux, http.MethodPut, "/api/v1/migration/exports/"+created.ExportID+"/chunks/0?sha256="+hash, "machine-replace", "replace-token", payload)
		if resp.Code != http.StatusOK {
			t.Fatalf("upload status=%d body=%s", resp.Code, resp.Body.String())
		}
	}
	upload(firstPayload)
	upload(secondPayload)

	stored, err := os.ReadFile(api.chunkPath(user.TenantID, user.ID, created.ExportID, 0))
	if err != nil {
		t.Fatalf("read replaced chunk: %v", err)
	}
	if !bytes.Equal(stored, secondPayload) {
		t.Fatalf("stored chunk was not replaced")
	}
	var size int64
	var hash string
	if err := db.QueryRowContext(ctx, `SELECT size, sha256 FROM user_data_migration_chunks WHERE export_id = ? AND chunk_index = 0`, created.ExportID).Scan(&size, &hash); err != nil {
		t.Fatalf("read replaced chunk metadata: %v", err)
	}
	if size != int64(len(secondPayload)) || hash != migrationSHA256Hex(secondPayload) {
		t.Fatalf("replacement metadata size=%d hash=%q", size, hash)
	}
	entries, err := os.ReadDir(api.chunksDir(user.TenantID, user.ID, created.ExportID))
	if err != nil {
		t.Fatalf("read chunk directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "000000.part" {
		t.Fatalf("replacement left temporary files: %v", entries)
	}
}

func TestMigrationCreateExportTreatsEncryptedPackageAsOpaque(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-pet-pack", TenantID: "tenant-pet-pack", Email: "pet-pack@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-pet-pack", "token-pet-pack", "Pet Pack Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	payload := bytes.Repeat([]byte("x"), int(migrationMinUploadChunkSize))
	hash := migrationSHA256Hex(payload)
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-pet-pack", "token-pet-pack", map[string]any{
		"encrypted_size":   int64(len(payload)),
		"encrypted_sha256": hash,
		"chunk_size":       migrationMinUploadChunkSize, "chunk_count": 1,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("opaque package create export status=%d body=%s", resp.Code, resp.Body.String())
	}
	current := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/exports/current", "machine-pet-pack", "token-pet-pack", nil)
	if current.Code != http.StatusOK {
		t.Fatalf("current export status=%d body=%s", current.Code, current.Body.String())
	}
	if bytes.Contains(current.Body.Bytes(), []byte(`"manifest"`)) {
		t.Fatalf("Hub exposed client package schema: %s", current.Body.String())
	}
	instances := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/instances", "machine-pet-pack", "token-pet-pack", nil)
	if instances.Code != http.StatusOK {
		t.Fatalf("migration instances status=%d body=%s", instances.Code, instances.Body.String())
	}
	if bytes.Contains(instances.Body.Bytes(), []byte(`"export_manifest"`)) {
		t.Fatalf("Hub exposed client package schema in instances: %s", instances.Body.String())
	}
}

func TestMigrationCreateExportRejectsPackageSchemaFields(t *testing.T) {
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-opaque", TenantID: "tenant-opaque", Email: "opaque@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-opaque", "token-opaque", "Opaque Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	hash := strings.Repeat("a", 64)
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-opaque", "token-opaque", map[string]any{
		"encrypted_sha256": hash,
		"chunk_size":       migrationMinUploadChunkSize, "chunk_count": 1,
		"manifest": map[string]any{"version": "client-owned"},
	})
	if resp.Code != http.StatusBadRequest || !bytes.Contains(resp.Body.Bytes(), []byte("unknown field")) {
		t.Fatalf("package schema field accepted: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMigrationCreateExportRejectsAmbiguousOrUnknownEnvelopeFields(t *testing.T) {
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-envelope", TenantID: "tenant-envelope", Email: "envelope@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-envelope", "token-envelope", "Envelope Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	base := `"encrypted_size":1,"encrypted_sha256":"` + strings.Repeat("a", 64) + `","chunk_size":262144,"chunk_count":1`
	for _, body := range []string{
		`{` + base + `,"Compressed_Size":2}`,
		`{` + base + `,"api_key":"must-not-reach-hub"}`,
		`{` + base + `}{"shadow":true}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/migration/exports", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-envelope")
		req.Header.Set("X-Machine-ID", "machine-envelope")
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, req)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("ambiguous create envelope status=%d body=%s request=%s", resp.Code, resp.Body.String(), body)
		}
	}
}

func TestMigrationImportedExportRemainsVisibleForCleanupRetry(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-cleanup-retry", TenantID: "tenant-a", Email: "cleanup@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target", "target-token", "New Mac")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	hash := hex.EncodeToString(bytes.Repeat([]byte{0x7a}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, claimed_by_machine_id, claimed_at, imported_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'imported', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mig-cleanup-retry", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), hash, migrationMinUploadChunkSize, 1, "machine-target", now, now, now, now); err != nil {
		t.Fatalf("insert imported export: %v", err)
	}

	current := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/exports/current", "machine-target", "target-token", nil)
	if current.Code != http.StatusOK || !bytes.Contains(current.Body.Bytes(), []byte(`"status":"imported"`)) || !bytes.Contains(current.Body.Bytes(), []byte(`"claimed_by_machine_id":"machine-target"`)) {
		t.Fatalf("current imported status=%d body=%s", current.Code, current.Body.String())
	}

	instances := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/instances", "machine-target", "target-token", nil)
	if instances.Code != http.StatusOK || !bytes.Contains(instances.Body.Bytes(), []byte(`"export_status":"imported"`)) || !bytes.Contains(instances.Body.Bytes(), []byte(`"export_claimed_by_machine_id":"machine-target"`)) {
		t.Fatalf("instances imported status=%d body=%s", instances.Code, instances.Body.String())
	}

	complete := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/mig-cleanup-retry/complete", "machine-target", "target-token", map[string]any{})
	if complete.Code != http.StatusOK || !bytes.Contains(complete.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("complete cleanup retry status=%d body=%s", complete.Code, complete.Body.String())
	}
}

func TestMigrationStaleClaimCannotOverwriteWinner(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-stale-claim", TenantID: "tenant-a", Email: "claim@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target-a", "target-token-a", "New Mac A")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target-b", "target-token-b", "New Mac B")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	hash := hex.EncodeToString(bytes.Repeat([]byte{0x4c}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?, ?)`,
		"mig-stale-claim", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), hash, migrationMinUploadChunkSize, 1, now, now); err != nil {
		t.Fatalf("insert ready export: %v", err)
	}

	row, err := api.getExport(ctx, "tenant-a", user.ID, "mig-stale-claim")
	if err != nil {
		t.Fatalf("load ready export: %v", err)
	}
	firstSnapshot := *row
	secondSnapshot := *row
	first := &migrationPrincipal{TenantID: "tenant-a", UserID: user.ID, MachineID: "machine-target-a"}
	second := &migrationPrincipal{TenantID: "tenant-a", UserID: user.ID, MachineID: "machine-target-b"}

	if _, err := api.claimImportForMachine(ctx, first, &firstSnapshot, time.Now().UTC()); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := api.claimImportForMachine(ctx, second, &secondSnapshot, time.Now().UTC()); !errors.Is(err, errMigrationExportNotReady) {
		t.Fatalf("second stale claim err = %v, want not ready", err)
	}
	var status, claimedBy string
	if err := db.QueryRowContext(ctx, `SELECT status, claimed_by_machine_id FROM user_data_migration_exports WHERE id = ?`, "mig-stale-claim").Scan(&status, &claimedBy); err != nil {
		t.Fatalf("read claimed export: %v", err)
	}
	if status != "importing" || claimedBy != "machine-target-a" {
		t.Fatalf("unexpected claimed export status=%q claimed_by=%q", status, claimedBy)
	}
}

func TestMigrationExpiredClaimCannotDownloadCompleteOrAbort(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-expired-claim", TenantID: "tenant-a", Email: "expired@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target", "target-token", "New Mac")

	root := t.TempDir()
	api := NewMigrationAPI(db, root, identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	hash := hex.EncodeToString(bytes.Repeat([]byte{0x5d}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, claimed_by_machine_id, claimed_at, claim_expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'importing', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mig-expired-claim", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1), hash, migrationMinUploadChunkSize, 1, "machine-target", now, past, now, now); err != nil {
		t.Fatalf("insert importing export: %v", err)
	}
	if err := os.MkdirAll(api.chunksDir("tenant-a", user.ID, "mig-expired-claim"), 0o700); err != nil {
		t.Fatalf("make chunk dir: %v", err)
	}
	if err := os.WriteFile(api.chunkPath("tenant-a", user.ID, "mig-expired-claim", 0), []byte("x"), 0o600); err != nil {
		t.Fatalf("write chunk: %v", err)
	}

	for _, request := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/migration/imports/mig-expired-claim/chunks/0"},
		{http.MethodPost, "/api/v1/migration/imports/mig-expired-claim/complete"},
		{http.MethodPost, "/api/v1/migration/imports/mig-expired-claim/abort"},
	} {
		resp := migrationAPIRequest(t, mux, request.method, request.path, "machine-target", "target-token", map[string]any{})
		if resp.Code != http.StatusConflict || !bytes.Contains(resp.Body.Bytes(), []byte("CLAIM_EXPIRED")) {
			t.Fatalf("expired claim %s %s status=%d body=%s", request.method, request.path, resp.Code, resp.Body.String())
		}
	}
}

func TestMigrationStaleClaimHolderCannotCompleteOrAbortAfterReclaim(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-reclaimed", TenantID: "tenant-a", Email: "reclaimed@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-old", "old-token", "Old Target")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-winner", "winner-token", "Winning Target")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	hash := hex.EncodeToString(bytes.Repeat([]byte{0x6e}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, claimed_by_machine_id, claimed_at, claim_expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'importing', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mig-reclaimed", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1), hash, migrationMinUploadChunkSize, 1, "machine-old", now, past, now, now); err != nil {
		t.Fatalf("insert importing export: %v", err)
	}

	winnerClaim := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/mig-reclaimed/claim", "machine-winner", "winner-token", map[string]any{})
	if winnerClaim.Code != http.StatusOK {
		t.Fatalf("winner claim status=%d body=%s", winnerClaim.Code, winnerClaim.Body.String())
	}
	for _, path := range []string{
		"/api/v1/migration/imports/mig-reclaimed/complete",
		"/api/v1/migration/imports/mig-reclaimed/abort",
	} {
		resp := migrationAPIRequest(t, mux, http.MethodPost, path, "machine-old", "old-token", map[string]any{})
		if resp.Code != http.StatusConflict {
			t.Fatalf("stale holder POST %s status=%d body=%s", path, resp.Code, resp.Body.String())
		}
	}
	var status, claimedBy string
	if err := db.QueryRowContext(ctx, `SELECT status, claimed_by_machine_id FROM user_data_migration_exports WHERE id = ?`, "mig-reclaimed").Scan(&status, &claimedBy); err != nil {
		t.Fatalf("read reclaimed export: %v", err)
	}
	if status != "importing" || claimedBy != "machine-winner" {
		t.Fatalf("stale holder changed winner status=%q claimed_by=%q", status, claimedBy)
	}
}

func TestMigrationCleanupFailureKeepsExportVisibleForRetry(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-cleanup-failure", TenantID: "tenant-a", Email: "cleanup-failure@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Old Mac")
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-target", "target-token", "New Mac")

	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	hash := hex.EncodeToString(bytes.Repeat([]byte{0x6b}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	claimExpires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, claimed_by_machine_id, claimed_at, claim_expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'importing', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"mig-cleanup-failure", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), hash, migrationMinUploadChunkSize, 1, "machine-target", now, claimExpires, now, now); err != nil {
		t.Fatalf("insert importing export: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_chunks (export_id, tenant_id, user_id, chunk_index, size, sha256, uploaded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "mig-cleanup-failure", "tenant-a", user.ID, 0, int64(1024), hash, now); err != nil {
		t.Fatalf("insert chunk row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TRIGGER fail_migration_chunk_delete
		BEFORE DELETE ON user_data_migration_chunks
		BEGIN
			SELECT RAISE(ABORT, 'chunk delete blocked');
		END`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}

	complete := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/mig-cleanup-failure/complete", "machine-target", "target-token", map[string]any{})
	if complete.Code != http.StatusInternalServerError || !bytes.Contains(complete.Body.Bytes(), []byte("chunk delete blocked")) {
		t.Fatalf("complete cleanup failure status=%d body=%s", complete.Code, complete.Body.String())
	}

	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM user_data_migration_exports WHERE id = ?`, "mig-cleanup-failure").Scan(&status); err != nil {
		t.Fatalf("read export status: %v", err)
	}
	if status != "deleting" {
		t.Fatalf("status after cleanup failure = %q, want deleting", status)
	}
	current := migrationAPIRequest(t, mux, http.MethodGet, "/api/v1/migration/exports/current", "machine-target", "target-token", nil)
	if current.Code != http.StatusOK || !bytes.Contains(current.Body.Bytes(), []byte(`"status":"deleting"`)) || !bytes.Contains(current.Body.Bytes(), []byte(`"claimed_by_machine_id":"machine-target"`)) {
		t.Fatalf("current deleting status=%d body=%s", current.Code, current.Body.String())
	}

	if _, err := db.ExecContext(ctx, `DROP TRIGGER fail_migration_chunk_delete`); err != nil {
		t.Fatalf("drop delete trigger: %v", err)
	}
	retry := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/imports/mig-cleanup-failure/complete", "machine-target", "target-token", map[string]any{})
	if retry.Code != http.StatusOK || !bytes.Contains(retry.Body.Bytes(), []byte(`"status":"deleted"`)) {
		t.Fatalf("retry cleanup status=%d body=%s", retry.Code, retry.Body.String())
	}
}

func TestMigrationCreateExportEnforcesConfiguredAndAbsolutePackageLimits(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-size", TenantID: "tenant-a", Email: "size@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Source")
	configuredLimit := migrationMinPackageBytes
	setting, _ := json.Marshal(map[string]int64{"value": configuredLimit})
	if err := scopedSystemSettingsForTenant("tenant-a", st.System).Set(ctx, migrationSettingMaxPackageBytes, string(setting)); err != nil {
		t.Fatalf("set migration limit: %v", err)
	}
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	chunkSize := int64(4 * 1024 * 1024)
	hash := hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))

	atLimit := configuredLimit
	chunkCount := int((atLimit + chunkSize - 1) / chunkSize)
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"encrypted_size":   atLimit,
		"encrypted_sha256": hash,
		"chunk_size":       chunkSize,
		"chunk_count":      chunkCount,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("at-limit create status=%d body=%s", resp.Code, resp.Body.String())
	}

	overLimit := configuredLimit + 1
	overCount := int((overLimit + chunkSize - 1) / chunkSize)
	resp = migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"encrypted_size":   overLimit,
		"encrypted_sha256": hash,
		"chunk_size":       chunkSize,
		"chunk_count":      overCount,
	})
	if resp.Code != http.StatusRequestEntityTooLarge || !bytes.Contains(resp.Body.Bytes(), []byte("MIGRATION_TOO_LARGE")) {
		t.Fatalf("configured-limit create status=%d body=%s", resp.Code, resp.Body.String())
	}
	var tooLarge map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &tooLarge); err != nil {
		t.Fatalf("decode configured-limit response: %v", err)
	}
	if got := int64(tooLarge["encrypted_size"].(float64)); got != overLimit {
		t.Fatalf("encrypted_size = %d, want %d", got, overLimit)
	}
	if got := int64(tooLarge["max_package_bytes"].(float64)); got != configuredLimit {
		t.Fatalf("max_package_bytes = %d, want %d", got, configuredLimit)
	}

	absurdSize := migrationMaxPackageBytes + 1
	absurdCount := int((absurdSize + chunkSize - 1) / chunkSize)
	resp = migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"encrypted_size":   absurdSize,
		"encrypted_sha256": hash,
		"chunk_size":       chunkSize,
		"chunk_count":      absurdCount,
	})
	if resp.Code != http.StatusBadRequest || !bytes.Contains(resp.Body.Bytes(), []byte("INVALID_INPUT")) {
		t.Fatalf("absolute-limit create status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMigrationCompleteUploadMissingChunkReturnsToUploading(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-finalizing", TenantID: "tenant-finalizing", Email: "finalizing@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-finalizing", "token-finalizing", "Finalizing Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	hash := strings.Repeat("a", 64)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?)`,
		"mig-finalizing", user.TenantID, user.ID, "machine-finalizing", "Finalizing Mac", int64(1), hash, migrationMinUploadChunkSize, 1, now, now); err != nil {
		t.Fatalf("insert export: %v", err)
	}
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports/mig-finalizing/complete-upload", "machine-finalizing", "token-finalizing", map[string]any{})
	if resp.Code != http.StatusConflict || !bytes.Contains(resp.Body.Bytes(), []byte("MISSING_CHUNK")) {
		t.Fatalf("missing chunk status=%d body=%s", resp.Code, resp.Body.String())
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM user_data_migration_exports WHERE id = ?`, "mig-finalizing").Scan(&status); err != nil {
		t.Fatalf("read export: %v", err)
	}
	if status != "uploading" {
		t.Fatalf("status=%q, want uploading", status)
	}
}

func TestMigrationCompleteUploadHashMismatchReturnsToUploading(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-hash-retry", TenantID: "tenant-hash-retry", Email: "hash-retry@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-hash-retry", "token-hash-retry", "Hash Retry Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	payload := []byte("x")
	wrongHash := migrationSHA256Hex([]byte("y"))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?)`,
		"mig-hash-retry", user.TenantID, user.ID, "machine-hash-retry", "Hash Retry Mac", int64(len(payload)), wrongHash, migrationMinUploadChunkSize, 1, now, now); err != nil {
		t.Fatalf("insert export: %v", err)
	}
	if err := os.MkdirAll(api.chunksDir(user.TenantID, user.ID, "mig-hash-retry"), 0o700); err != nil {
		t.Fatalf("make chunk dir: %v", err)
	}
	if err := os.WriteFile(api.chunkPath(user.TenantID, user.ID, "mig-hash-retry", 0), payload, 0o600); err != nil {
		t.Fatalf("write chunk: %v", err)
	}

	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports/mig-hash-retry/complete-upload", "machine-hash-retry", "token-hash-retry", map[string]any{})
	if resp.Code != http.StatusBadRequest || !bytes.Contains(resp.Body.Bytes(), []byte("HASH_MISMATCH")) {
		t.Fatalf("hash mismatch status=%d body=%s", resp.Code, resp.Body.String())
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM user_data_migration_exports WHERE id = ?`, "mig-hash-retry").Scan(&status); err != nil {
		t.Fatalf("read export: %v", err)
	}
	if status != "uploading" {
		t.Fatalf("status=%q, want uploading", status)
	}
}

func TestMigrationCompleteUploadIsIdempotentAfterReady(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-ready-retry", TenantID: "tenant-ready-retry", Email: "ready-retry@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-ready-retry", "token-ready-retry", "Ready Retry Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?, ?)`,
		"mig-ready-retry", user.TenantID, user.ID, "machine-ready-retry", "Ready Retry Mac", int64(1), strings.Repeat("a", 64), migrationMinUploadChunkSize, 1, now, now); err != nil {
		t.Fatalf("insert export: %v", err)
	}

	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports/mig-ready-retry/complete-upload", "machine-ready-retry", "token-ready-retry", map[string]any{})
	if resp.Code != http.StatusOK || !bytes.Contains(resp.Body.Bytes(), []byte(`"status":"ready"`)) {
		t.Fatalf("ready retry status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMigrationStorageSegmentsDoNotCollideOrExposeIDs(t *testing.T) {
	api := &migrationAPI{rootDir: t.TempDir()}
	first := api.exportDir("tenant/a", "user:one", "mig..one")
	second := api.exportDir("tenant_a", "user_one", "mig_one")
	if first == second {
		t.Fatalf("distinct migration identities share storage path: %q", first)
	}
	for _, sensitive := range []string{"tenant/a", "tenant_a", "user:one", "user_one", "mig..one", "mig_one"} {
		if strings.Contains(first, sensitive) || strings.Contains(second, sensitive) {
			t.Fatalf("storage path exposes raw identifier %q: %q / %q", sensitive, first, second)
		}
	}
	for _, segment := range []string{safeMigrationSegment("CON"), safeMigrationSegment(".."), safeMigrationSegment("")} {
		if len(segment) != 64 || !validSHA256(segment) {
			t.Fatalf("unsafe storage segment %q", segment)
		}
	}
}

func TestMigrationCreateExportReplacesExpiredImportingExport(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-expired", TenantID: "tenant-a", Email: "expired@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Source")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	hash := hex.EncodeToString(bytes.Repeat([]byte{0xcd}, 32))
	first := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"encrypted_size":   int64(1024),
		"encrypted_sha256": hash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first create status=%d body=%s", first.Code, first.Body.String())
	}
	var created struct {
		ExportID string `json:"export_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil || created.ExportID == "" {
		t.Fatalf("decode first response err=%v body=%s", err, first.Body.String())
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `UPDATE user_data_migration_exports SET status = 'importing', claimed_by_machine_id = 'machine-target', claim_expires_at = ? WHERE id = ?`, past, created.ExportID); err != nil {
		t.Fatalf("expire importing export: %v", err)
	}

	second := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"encrypted_size":   int64(2048),
		"encrypted_sha256": hash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second create status=%d body=%s", second.Code, second.Body.String())
	}
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM user_data_migration_exports WHERE id = ?`, created.ExportID).Scan(&status); err != nil {
		t.Fatalf("load old status: %v", err)
	}
	if status != "replaced" {
		t.Fatalf("expired importing export status = %q, want replaced", status)
	}
}

func TestMigrationCompleteUploadRechecksTenantLimit(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-complete-limit", TenantID: "tenant-a", Email: "complete-limit@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Source")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	hash := hex.EncodeToString(bytes.Repeat([]byte{0xef}, 32))
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, encrypted_size, encrypted_sha256, chunk_size, chunk_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?)`,
		"mig-over-limit", "tenant-a", user.ID, "machine-source", "Source", migrationMinPackageBytes+1, hash, migrationMaxUploadChunkSize, 13, now, now); err != nil {
		t.Fatalf("insert export: %v", err)
	}
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports/mig-over-limit/complete-upload", "machine-source", "source-token", map[string]any{"encrypted_sha256": hash})
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("complete upload over limit status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestMigrationAdminSettingsClampAndTenantScope(t *testing.T) {
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	api := NewMigrationAPI(db, t.TempDir(), nil, nil, st.System)

	request := func(method, tenantID string, body any) map[string]any {
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
		req := httptest.NewRequest(method, "/api/admin/migration/settings", reader)
		req = req.WithContext(WithRequestTenant(req.Context(), tenantID))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		api.handleAdminSettings(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s settings status=%d body=%s", method, rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return out
	}

	defaults := request(http.MethodGet, "tenant-a", nil)
	if got := int64(defaults["max_package_bytes"].(float64)); got != migrationDefaultMaxPackageBytes {
		t.Fatalf("default max bytes = %d, want %d", got, migrationDefaultMaxPackageBytes)
	}
	if _, found := defaults["max_compressed_bytes"]; found {
		t.Fatalf("settings response must expose max_package_bytes, not obsolete max_compressed_bytes: %#v", defaults)
	}

	below := request(http.MethodPut, "tenant-a", map[string]any{"max_package_bytes": int64(1)})
	if got := int64(below["max_package_bytes"].(float64)); got != migrationMinPackageBytes {
		t.Fatalf("below-min max bytes = %d, want %d", got, migrationMinPackageBytes)
	}

	tenantB := request(http.MethodGet, "tenant-b", nil)
	if got := int64(tenantB["max_package_bytes"].(float64)); got != migrationDefaultMaxPackageBytes {
		t.Fatalf("tenant-b max bytes = %d, want default %d", got, migrationDefaultMaxPackageBytes)
	}

	above := request(http.MethodPut, "tenant-a", map[string]any{"max_package_bytes": migrationMaxPackageBytes + 1})
	if got := int64(above["max_package_bytes"].(float64)); got != migrationMaxPackageBytes {
		t.Fatalf("above-max max bytes = %d, want %d", got, migrationMaxPackageBytes)
	}

	for _, body := range []string{
		`{"max_package_bytes":104857600,"unknown":true}`,
		`{"max_compressed_bytes":104857600}`,
		`{"max_package_bytes":104857600}{"max_package_bytes":209715200}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/migration/settings", strings.NewReader(body))
		req = req.WithContext(WithRequestTenant(req.Context(), "tenant-a"))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		api.handleAdminSettings(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid settings body accepted: status=%d body=%s input=%s", rec.Code, rec.Body.String(), body)
		}
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
