package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockAuditStoreForAPI is a minimal mock for testing the audit API handler.
type mockAuditStoreForAPI struct {
	entries []AuditEntry
}

func (m *mockAuditStoreForAPI) Append(_ context.Context, entry *AuditEntry) error {
	m.entries = append(m.entries, *entry)
	return nil
}

func (m *mockAuditStoreForAPI) QueryByInstance(_ context.Context, instanceID string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	var result []AuditEntry
	for _, e := range m.entries {
		if e.InstanceID == instanceID {
			result = append(result, e)
		}
	}
	total := len(result)
	start := (page - 1) * pageSize
	if start >= total {
		return []AuditEntry{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return result[start:end], total, nil
}

func (m *mockAuditStoreForAPI) QueryByApprover(_ context.Context, approverID string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	var result []AuditEntry
	for _, e := range m.entries {
		if e.ActorID == approverID {
			result = append(result, e)
		}
	}
	total := len(result)
	start := (page - 1) * pageSize
	if start >= total {
		return []AuditEntry{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return result[start:end], total, nil
}

func (m *mockAuditStoreForAPI) QueryByTimeRange(_ context.Context, start, end time.Time, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	var result []AuditEntry
	for _, e := range m.entries {
		if !e.Timestamp.Before(start) && !e.Timestamp.After(end) {
			result = append(result, e)
		}
	}
	total := len(result)
	s := (page - 1) * pageSize
	if s >= total {
		return []AuditEntry{}, total, nil
	}
	e := s + pageSize
	if e > total {
		e = total
	}
	return result[s:e], total, nil
}

func (m *mockAuditStoreForAPI) QueryByDecision(_ context.Context, decision string, page, pageSize int) ([]AuditEntry, int, error) {
	pageSize = NormalizePageSize(pageSize)
	var result []AuditEntry
	for _, e := range m.entries {
		if e.Decision == decision {
			result = append(result, e)
		}
	}
	total := len(result)
	start := (page - 1) * pageSize
	if start >= total {
		return []AuditEntry{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return result[start:end], total, nil
}

func setupAuditAPITest() (*AuditAPI, *mockAuditStoreForAPI) {
	store := &mockAuditStoreForAPI{
		entries: []AuditEntry{
			{ID: "a1", InstanceID: "inst_1", ActorID: "ve_approver_1", Decision: "approve", Timestamp: time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)},
			{ID: "a2", InstanceID: "inst_1", ActorID: "ve_approver_2", Decision: "reject", Timestamp: time.Date(2024, 6, 1, 11, 0, 0, 0, time.UTC)},
			{ID: "a3", InstanceID: "inst_2", ActorID: "ve_approver_1", Decision: "approve", Timestamp: time.Date(2024, 6, 2, 9, 0, 0, 0, time.UTC)},
			{ID: "a4", InstanceID: "inst_3", ActorID: "requester_1", Decision: "escalate", Timestamp: time.Date(2024, 6, 3, 14, 0, 0, 0, time.UTC)},
		},
	}
	api := NewAuditAPI(store)
	return api, store
}

func noopAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Owner-ID", "test_user")
		h(w, r)
	}
}

func TestAuditAPI_QueryByInstanceID(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?instance_id=inst_1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for inst_1, got %d", len(entries))
	}
	if resp["total"].(float64) != 2 {
		t.Fatalf("expected total=2, got %v", resp["total"])
	}
	if resp["page"].(float64) != 1 {
		t.Fatalf("expected page=1, got %v", resp["page"])
	}
	if resp["page_size"].(float64) != 100 {
		t.Fatalf("expected page_size=100, got %v", resp["page_size"])
	}
}

func TestAuditAPI_QueryByApproverID(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?approver_id=ve_approver_1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries for ve_approver_1, got %d", len(entries))
	}
}

func TestAuditAPI_QueryByRequesterID(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?requester_id=requester_1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for requester_1, got %d", len(entries))
	}
}

func TestAuditAPI_QueryByDecision(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?decision=approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries with decision=approve, got %d", len(entries))
	}
}

func TestAuditAPI_QueryByTimeRange(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?start_time=2024-06-01T00:00:00Z&end_time=2024-06-01T23:59:59Z", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries in time range, got %d", len(entries))
	}
}

func TestAuditAPI_InvalidTimeRange(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?start_time=not-a-date", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["code"] != "INVALID_TIME_RANGE" {
		t.Fatalf("expected code=INVALID_TIME_RANGE, got %v", resp["code"])
	}
}

func TestAuditAPI_MissingFilter(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	if resp["code"] != "MISSING_FILTER" {
		t.Fatalf("expected code=MISSING_FILTER, got %v", resp["code"])
	}
}

func TestAuditAPI_Pagination(t *testing.T) {
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	// Page 2 with only 2 entries total for inst_1 — should return empty
	req := httptest.NewRequest("GET", "/api/v1/audit?instance_id=inst_1&page=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	entries := resp["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries on page 2, got %d", len(entries))
	}
	// Total should still reflect the full count
	if resp["total"].(float64) != 2 {
		t.Fatalf("expected total=2, got %v", resp["total"])
	}
}

func TestAuditAPI_FilterPriority(t *testing.T) {
	// When multiple filters are provided, instance_id takes priority
	api, _ := setupAuditAPITest()
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, noopAuth)

	req := httptest.NewRequest("GET", "/api/v1/audit?instance_id=inst_1&decision=reject", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	// instance_id filter takes priority, returns both entries for inst_1
	// (not just the reject one)
	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (instance_id priority), got %d", len(entries))
	}
}
