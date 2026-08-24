package httpthreat

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAPITenantFromIdentity(t *testing.T) {
	eng := NewEngine("enc1", func(string) ([]float32, error) { return oneHot(ClassBenign), nil })
	h := &Handler{Engine: eng, Who: func(r *http.Request) (NodeIdentity, string, error) {
		return NodeIdentity{TenantID: r.Header.Get("X-Tenant"), NodeID: "n1"}, RoleAdmin, nil
	}}
	mux := http.NewServeMux()
	h.Mount(mux, "/api/admin/httpthreat")

	body, _ := json.Marshal(Transaction{Method: "GET", Host: "h", Path: "/ok", TenantID: "other"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/httpthreat/detect", bytes.NewReader(body))
	req.Header.Set("X-Tenant", "alpha")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("claimed foreign tenant must 403, got %d %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(Transaction{Method: "GET", Host: "h", Path: "/ok"})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/httpthreat/detect", bytes.NewReader(body))
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detect %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/httpthreat/status", nil)
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var st StatusView
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Pipeline != PipelineOff {
		t.Fatalf("status body %+v %v", st, err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/httpthreat/export", nil)
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("export must stay disabled, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/httpthreat/queue", nil)
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue %d", rec.Code)
	}
	var cards []QueueItem
	if err := json.Unmarshal(rec.Body.Bytes(), &cards); err != nil {
		t.Fatalf("queue body %v %s", err, rec.Body.String())
	}
	if len(cards) == 0 || cards[0].WouldAction == "" {
		t.Fatalf("queue cards need would-action %+v", cards)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/httpthreat/audit", nil)
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit %d", rec.Code)
	}
	var audit []AuditRow
	if err := json.Unmarshal(rec.Body.Bytes(), &audit); err != nil || len(audit) == 0 {
		t.Fatalf("audit body %v %s", err, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/httpthreat/map", nil)
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("map %d", rec.Code)
	}

	body, _ = json.Marshal(Transaction{Method: "GET", Host: "h", Path: "/p", Query: "x=../etc/passwd"})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/httpthreat/detect", bytes.NewReader(body))
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("p0 detect %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/admin/httpthreat/train", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("async train %d %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(BatchLabelRequest{SampleIDs: []string{cards[0].ID}, GoldClass: ClassScan})
	req = httptest.NewRequest(http.MethodPost, "/api/admin/httpthreat/label/batch", bytes.NewReader(body))
	req.Header.Set("X-Tenant", "alpha")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch %d %s", rec.Code, rec.Body.String())
	}
}
