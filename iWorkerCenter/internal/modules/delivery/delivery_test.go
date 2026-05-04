package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/db"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

const testTID = "test-tenant"

func setupTestDB(t *testing.T) *db.Provider {
	t.Helper()
	provider, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(provider.Write); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

func TestCreateAndPublishBundle(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)

	now := time.Now()
	bundle := &Bundle{
		ID: idgen.New("cfgb"), Version: 1, ContentType: "full",
		Payload: `{"colleagues":[],"memories":[]}`, Status: "draft", Note: "初始配置", CreatedAt: now,
	}
	if err := repo.Insert(bundle, testTID); err != nil {
		t.Fatalf("insert: %v", err)
	}
	_, err := repo.GetLatestPublished(testTID)
	if err == nil {
		t.Error("expected no published bundle")
	}
	if err := repo.Publish(bundle.ID, testTID); err != nil {
		t.Fatalf("publish: %v", err)
	}
	latest, err := repo.GetLatestPublished(testTID)
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest.Version != 1 || latest.Status != "published" {
		t.Errorf("unexpected: version=%d status=%s", latest.Version, latest.Status)
	}
}

func TestGetLatestVersion(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	v, _ := repo.GetLatestVersion(testTID)
	if v != 0 {
		t.Errorf("expected 0, got %d", v)
	}
	now := time.Now()
	repo.Insert(&Bundle{ID: idgen.New("cfgb"), Version: 1, CreatedAt: now, Payload: "{}"}, testTID)
	repo.Insert(&Bundle{ID: idgen.New("cfgb"), Version: 2, CreatedAt: now, Payload: "{}"}, testTID)
	v, _ = repo.GetLatestVersion(testTID)
	if v != 2 {
		t.Errorf("expected 2, got %d", v)
	}
}

func TestListBundles(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	now := time.Now()
	repo.Insert(&Bundle{ID: "b1", Version: 1, Status: "published", CreatedAt: now, Payload: "{}"}, testTID)
	repo.Insert(&Bundle{ID: "b2", Version: 2, Status: "draft", CreatedAt: now, Payload: "{}"}, testTID)
	bundles, err := repo.List(testTID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(bundles) != 2 {
		t.Errorf("expected 2, got %d", len(bundles))
	}
	if bundles[0].Version != 2 {
		t.Errorf("expected first version 2, got %d", bundles[0].Version)
	}
}

func TestPublishNonExistent(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	if err := repo.Publish("nonexistent", testTID); err == nil {
		t.Error("expected error")
	}
}

func TestRecordApplyResult(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-1", Version: 3, ContentType: "full", Payload: "{}", Status: "published", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, testTID); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}
	rec := ApplyRecord{TenantID: testTID, BundleID: "cfgb-1", Version: 3, WorkerID: "worker-a", DepartmentID: "ops", Status: "success", Message: "cached"}
	if err := repo.RecordApply(rec); err != nil {
		t.Fatalf("record apply: %v", err)
	}
	records, err := repo.ListApplyRecords(testTID, 10)
	if err != nil {
		t.Fatalf("list apply records: %v", err)
	}
	if len(records) != 1 || records[0].BundleID != "cfgb-1" || records[0].WorkerID != "worker-a" {
		t.Fatalf("unexpected records: %+v", records)
	}
	rec.Status = "failed"
	rec.Message = "retry later"
	if err := repo.RecordApply(rec); err != nil {
		t.Fatalf("update apply: %v", err)
	}
	records, err = repo.ListApplyRecords(testTID, 10)
	if err != nil {
		t.Fatalf("list apply records after update: %v", err)
	}
	if len(records) != 1 || records[0].Status != "failed" || records[0].Message != "retry later" {
		t.Fatalf("unexpected updated records: %+v", records)
	}
}

func TestRecordApplyRejectsUnknownBundle(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	err := repo.RecordApply(ApplyRecord{TenantID: testTID, BundleID: "missing", WorkerID: "worker-a", Status: "success"})
	if err == nil {
		t.Fatal("expected unknown bundle apply result to be rejected")
	}
	records, listErr := repo.ListApplyRecords(testTID, 10)
	if listErr != nil {
		t.Fatalf("list records: %v", listErr)
	}
	if len(records) != 0 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestRecordApplyNormalizesStatusAndVersion(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-2", Version: 9, ContentType: "full", Payload: "{}", Status: "published", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, testTID); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}
	if err := repo.RecordApply(ApplyRecord{TenantID: testTID, BundleID: "cfgb-2", Version: 123, WorkerID: "worker-a", Status: "weird"}); err != nil {
		t.Fatalf("record apply: %v", err)
	}
	records, err := repo.ListApplyRecords(testTID, 10)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 1 || records[0].Version != 9 || records[0].Status != "failed" {
		t.Fatalf("unexpected normalized record: %+v", records)
	}
}

func TestRecordApplyRejectsDraftBundle(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-draft", Version: 1, ContentType: "full", Payload: "{}", Status: "draft", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, testTID); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}
	err := repo.RecordApply(ApplyRecord{TenantID: testTID, BundleID: "cfgb-draft", WorkerID: "worker-a", Status: "success"})
	if !errors.Is(err, ErrBundleNotPublished) {
		t.Fatalf("expected ErrBundleNotPublished, got %v", err)
	}
	records, listErr := repo.ListApplyRecords(testTID, 10)
	if listErr != nil {
		t.Fatalf("list records: %v", listErr)
	}
	if len(records) != 0 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestClientApplyResultHTTPRecordsPublishedBundle(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-http", Version: 7, ContentType: "full", Payload: "{}", Status: "published", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, "tenant-http"); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(p.Write, p.Read).RegisterClientRoutes(mux)
	body := map[string]any{"bundle_id": "cfgb-http", "version": 99, "worker_id": "worker-http", "department_id": "ops", "status": "ok", "message": "cached"}
	rec := postClientApplyResult(t, mux, "tenant-http", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	records, err := repo.ListApplyRecords("tenant-http", 10)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 1 || records[0].BundleID != "cfgb-http" || records[0].WorkerID != "worker-http" {
		t.Fatalf("unexpected records: %+v", records)
	}
	if records[0].Version != 7 || records[0].Status != "success" || records[0].DepartmentID != "ops" {
		t.Fatalf("unexpected normalized record: %+v", records[0])
	}
}

func TestClientApplyResultHTTPRejectsUnknownBundle(t *testing.T) {
	p := setupTestDB(t)
	mux := http.NewServeMux()
	NewHandler(p.Write, p.Read).RegisterClientRoutes(mux)

	rec := postClientApplyResult(t, mux, testTID, map[string]any{"bundle_id": "missing", "worker_id": "worker-a", "status": "success"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), "UNKNOWN_BUNDLE")

	records, err := NewRepo(p.Write, p.Read).ListApplyRecords(testTID, 10)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestClientApplyResultHTTPRejectsDraftBundle(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-http-draft", Version: 1, ContentType: "full", Payload: "{}", Status: "draft", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, testTID); err != nil {
		t.Fatalf("insert bundle: %v", err)
	}
	mux := http.NewServeMux()
	NewHandler(p.Write, p.Read).RegisterClientRoutes(mux)

	rec := postClientApplyResult(t, mux, testTID, map[string]any{"bundle_id": "cfgb-http-draft", "worker_id": "worker-a", "status": "success"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec.Body.Bytes(), "BUNDLE_NOT_PUBLISHED")

	records, err := repo.ListApplyRecords(testTID, 10)
	if err != nil {
		t.Fatalf("list records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestClientApplyResultHTTPRespectsTenantHeader(t *testing.T) {
	p := setupTestDB(t)
	repo := NewRepo(p.Write, p.Read)
	bundle := &Bundle{ID: "cfgb-shared", Version: 5, ContentType: "full", Payload: "{}", Status: "published", CreatedAt: time.Now()}
	if err := repo.Insert(bundle, "tenant-a"); err != nil {
		t.Fatalf("insert tenant-a bundle: %v", err)
	}
	if err := repo.Insert(&Bundle{ID: "cfgb-other", Version: 6, ContentType: "full", Payload: "{}", Status: "published", CreatedAt: time.Now()}, "tenant-b"); err != nil {
		t.Fatalf("insert tenant-b bundle: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(p.Write, p.Read).RegisterClientRoutes(mux)
	rec := postClientApplyResult(t, mux, "tenant-a", map[string]any{"bundle_id": "cfgb-shared", "worker_id": "worker-a", "status": "success"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	tenantARecords, err := repo.ListApplyRecords("tenant-a", 10)
	if err != nil {
		t.Fatalf("list tenant-a records: %v", err)
	}
	tenantBRecords, err := repo.ListApplyRecords("tenant-b", 10)
	if err != nil {
		t.Fatalf("list tenant-b records: %v", err)
	}
	if len(tenantARecords) != 1 || tenantARecords[0].TenantID != "tenant-a" {
		t.Fatalf("unexpected tenant-a records: %+v", tenantARecords)
	}
	if len(tenantBRecords) != 0 {
		t.Fatalf("unexpected tenant-b records: %+v", tenantBRecords)
	}
}

func postClientApplyResult(t *testing.T, handler http.Handler, tenantID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/client/config/apply-result", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set("X-Tenant-ID", tenantID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertErrorCode(t *testing.T, body []byte, want string) {
	t.Helper()
	var got struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, string(body))
	}
	if got.Error.Code != want {
		t.Fatalf("expected error code %s, got %s body=%s", want, got.Error.Code, string(body))
	}
}
