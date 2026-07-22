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

func TestMigrationPublicManifestRejectsSecretBearingFields(t *testing.T) {
	for _, manifest := range []json.RawMessage{
		json.RawMessage(`{"version":"v2","api_key":"must-not-reach-hub"}`),
		json.RawMessage(`{"version":"v2","meta":{"secret_inventory":["provider.api_key"]}}`),
		json.RawMessage(`{"version":"v2","Version":"shadow"}`),
		json.RawMessage(`{"version":"v2","files":[],"Files":[]}`),
	} {
		if err := validateMigrationPublicManifest(manifest); err == nil {
			t.Fatalf("expected secret-bearing manifest to be rejected: %s", manifest)
		}
	}
	if err := validateMigrationPublicManifest(json.RawMessage(`{"version":"v2","secret_count":2,"files":[{"path":"config/app_config.json","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`)); err != nil {
		t.Fatalf("safe public manifest rejected: %v", err)
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
	base := `"compressed_size":1,"encrypted_size":1,"encrypted_sha256":"` + strings.Repeat("a", 64) + `","plain_sha256":"` + strings.Repeat("b", 64) + `","chunk_size":262144,"chunk_count":1,"manifest":{"version":"v2"}`
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

func TestMigrationCreateExportRejectsSpoofedManifestIdentity(t *testing.T) {
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()
	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-identity", TenantID: "tenant-identity", Email: "identity@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, user.TenantID, user.ID, "machine-identity", "token-identity", "Identity Mac")
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)
	payload := bytes.Repeat([]byte("x"), 1024)
	hash := migrationSHA256Hex(payload)
	for _, manifest := range []map[string]any{
		{"version": "v2", "tenant_id": "other-tenant"},
		{"version": "v2", "user_id": "other-user"},
		{"version": "v2", "machine_id": "other-machine"},
	} {
		resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-identity", "token-identity", map[string]any{
			"compressed_size": int64(len(payload)), "encrypted_size": int64(len(payload)),
			"encrypted_sha256": hash, "plain_sha256": hash,
			"chunk_size": migrationMinUploadChunkSize, "chunk_count": 1, "manifest": manifest,
		})
		if resp.Code != http.StatusBadRequest || !bytes.Contains(resp.Body.Bytes(), []byte("INVALID_MANIFEST_IDENTITY")) {
			t.Fatalf("spoofed manifest accepted: status=%d body=%s manifest=%v", resp.Code, resp.Body.String(), manifest)
		}
	}
}

func TestMigrationPublicManifestRejectsNonPortableFilePathsAndCounts(t *testing.T) {
	sha := strings.Repeat("a", 64)
	for _, manifest := range []json.RawMessage{
		json.RawMessage(`{"version":"v2","memory_entries":-1}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"config\\app.json","sha256":"` + sha + `"}]}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"manifest.json","sha256":"` + sha + `"}]}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"Config/app.json","sha256":"` + sha + `"},{"path":"config/app.json","sha256":"` + sha + `"}]}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"C:/config.json","sha256":"` + sha + `"}]}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"assets/NUL.txt","sha256":"` + sha + `"}]}`),
		json.RawMessage(`{"version":"v2","files":[{"path":"assets/file. ","sha256":"` + sha + `"}]}`),
	} {
		if err := validateMigrationPublicManifest(manifest); err == nil {
			t.Fatalf("expected unsafe public manifest to be rejected: %s", manifest)
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
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, claimed_by_machine_id, claimed_at, imported_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'imported', ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?, ?, ?)`,
		"mig-cleanup-retry", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), int64(1024), hash, hash, migrationMinUploadChunkSize, 1, "machine-target", now, now, now, now); err != nil {
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
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'ready', ?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		"mig-stale-claim", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), int64(1024), hash, hash, migrationMinUploadChunkSize, 1, now, now); err != nil {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO user_data_migration_exports
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, claimed_by_machine_id, claimed_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'importing', ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?, ?)`,
		"mig-cleanup-failure", "tenant-a", user.ID, "machine-source", "Old Mac", int64(1024), int64(1024), hash, hash, migrationMinUploadChunkSize, 1, "machine-target", now, now, now); err != nil {
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

func TestMigrationCreateExportAllowsStreamingEncryptionOverhead(t *testing.T) {
	ctx := context.Background()
	st, db, cleanup := newMigrationAPITestStore(t)
	defer cleanup()

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	user := &store.User{ID: "user-size", TenantID: "tenant-a", Email: "size@example.com", Status: "active", EnrollmentStatus: "approved", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.Users.Create(ctx, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	seedMigrationAPIMachine(t, st, "tenant-a", user.ID, "machine-source", "source-token", "Source")
	setting, _ := json.Marshal(map[string]int64{"value": migrationMaxCompressedBytes})
	if err := scopedSystemSettingsForTenant("tenant-a", st.System).Set(ctx, migrationSettingMaxCompressedBytes, string(setting)); err != nil {
		t.Fatalf("set migration limit: %v", err)
	}
	api := NewMigrationAPI(db, t.TempDir(), identity, identity.MachinesRepo(), st.System)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, nil)

	chunkSize := int64(4 * 1024 * 1024)
	hash := hex.EncodeToString(bytes.Repeat([]byte{0xab}, 32))
	resp := migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"compressed_size":  int64(2048),
		"encrypted_size":   int64(1024),
		"encrypted_sha256": hash,
		"plain_sha256":     hash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
		"manifest":         map[string]any{"version": "invalid-size"},
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("encrypted-smaller create status=%d body=%s", resp.Code, resp.Body.String())
	}

	encryptedSize := migrationMaxCompressedBytes + 6000
	chunkCount := int(encryptedSize / chunkSize)
	if encryptedSize%chunkSize != 0 {
		chunkCount++
	}
	resp = migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"compressed_size":  migrationMaxCompressedBytes,
		"encrypted_size":   encryptedSize,
		"encrypted_sha256": hash,
		"plain_sha256":     hash,
		"chunk_size":       chunkSize,
		"chunk_count":      chunkCount,
		"manifest":         map[string]any{"version": "large-boundary"},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("boundary create status=%d body=%s", resp.Code, resp.Body.String())
	}

	tooLargeEncrypted := migrationMaxCompressedBytes + migrationMaxEncryptedOverhead(chunkCount) + 1
	tooLargeChunkCount := int(tooLargeEncrypted / chunkSize)
	if tooLargeEncrypted%chunkSize != 0 {
		tooLargeChunkCount++
	}
	resp = migrationAPIRequest(t, mux, http.MethodPost, "/api/v1/migration/exports", "machine-source", "source-token", map[string]any{
		"compressed_size":  migrationMaxCompressedBytes,
		"encrypted_size":   tooLargeEncrypted,
		"encrypted_sha256": hash,
		"plain_sha256":     hash,
		"chunk_size":       chunkSize,
		"chunk_count":      tooLargeChunkCount,
		"manifest":         map[string]any{"version": "large-overhead"},
	})
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized encrypted create status=%d body=%s", resp.Code, resp.Body.String())
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
		"compressed_size":  int64(1024),
		"encrypted_size":   int64(1024),
		"encrypted_sha256": hash,
		"plain_sha256":     hash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
		"manifest":         map[string]any{"version": "first"},
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
		"compressed_size":  int64(2048),
		"encrypted_size":   int64(2048),
		"encrypted_sha256": hash,
		"plain_sha256":     hash,
		"chunk_size":       migrationMinUploadChunkSize,
		"chunk_count":      1,
		"manifest":         map[string]any{"version": "second"},
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
		(id, tenant_id, user_id, source_machine_id, source_machine_name, status, compressed_size, encrypted_size, encrypted_sha256, plain_sha256, chunk_size, chunk_count, manifest_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'uploading', ?, ?, ?, ?, ?, ?, '{}', ?, ?)`,
		"mig-over-limit", "tenant-a", user.ID, "machine-source", "Source", migrationMinCompressedBytes+1, migrationMinCompressedBytes+1, hash, hash, migrationMaxUploadChunkSize, 13, now, now); err != nil {
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
	if got := int64(defaults["max_compressed_bytes"].(float64)); got != migrationDefaultMaxCompressedBytes {
		t.Fatalf("default max bytes = %d, want %d", got, migrationDefaultMaxCompressedBytes)
	}

	below := request(http.MethodPut, "tenant-a", map[string]any{"max_compressed_bytes": int64(1)})
	if got := int64(below["max_compressed_bytes"].(float64)); got != migrationMinCompressedBytes {
		t.Fatalf("below-min max bytes = %d, want %d", got, migrationMinCompressedBytes)
	}

	tenantB := request(http.MethodGet, "tenant-b", nil)
	if got := int64(tenantB["max_compressed_bytes"].(float64)); got != migrationDefaultMaxCompressedBytes {
		t.Fatalf("tenant-b max bytes = %d, want default %d", got, migrationDefaultMaxCompressedBytes)
	}

	above := request(http.MethodPut, "tenant-a", map[string]any{"max_compressed_bytes": migrationMaxCompressedBytes + 1})
	if got := int64(above["max_compressed_bytes"].(float64)); got != migrationMaxCompressedBytes {
		t.Fatalf("above-max max bytes = %d, want %d", got, migrationMaxCompressedBytes)
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
