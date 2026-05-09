package agentruntime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHeartbeatHandlerNotifiesObserver(t *testing.T) {
	svc := NewService(newTestRepo(t))
	handler := NewHandler(svc)
	seen := make(chan struct{}, 1)
	handler.SetHeartbeatObserver(func() { seen <- struct{}{} })
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/runtime/iworker/instances/heartbeat", bytes.NewBufferString(`{"worker_id":"worker-a","instance_id":"worker-a:executor","role":"executor","status":"online"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("heartbeat observer was not called")
	}
}

func TestHeartbeatHandlerRejectsOversizedBody(t *testing.T) {
	svc := NewService(newTestRepo(t))
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)

	body := `{"worker_id":"worker-a","instance_id":"worker-a:executor","role":"executor","status":"online","host_id":"` + strings.Repeat("x", maxHeartbeatBodyBytes+1024)
	req := httptest.NewRequest(http.MethodPost, "/runtime/iworker/instances/heartbeat", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/client/iworker/instances?worker_id=worker-a", nil)
	listRec := httptest.NewRecorder()
	handler.RegisterClientRoutes(mux)
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var bodyResp struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &bodyResp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(bodyResp.Instances) != 0 {
		t.Fatalf("unexpected instances after oversized heartbeat: %+v", bodyResp.Instances)
	}
}

func TestHeartbeatHandlerRejectsTrailingJSONWithoutRecordingInstance(t *testing.T) {
	svc := NewService(newTestRepo(t))
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)
	handler.RegisterClientRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/runtime/iworker/instances/heartbeat", bytes.NewBufferString(`{"worker_id":"worker-a","instance_id":"worker-a:executor","role":"executor","status":"online"} {"worker_id":"worker-b"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/client/iworker/instances?worker_id=worker-a", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var bodyResp struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &bodyResp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(bodyResp.Instances) != 0 {
		t.Fatalf("unexpected instances after trailing heartbeat: %+v", bodyResp.Instances)
	}
}

func TestAdminListHandlerReturnsRuntimeWorkStatus(t *testing.T) {
	svc := NewService(newTestRepo(t))
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)
	handler.RegisterAdminRoutes(mux)

	heartbeatBody := bytes.NewBufferString(`{"worker_id":"worker-a","instance_id":"worker-a:executor","role":"executor","status":"busy","work_status":{"current_task":"Review invoice exception","current_detail":"Waiting for human approval","active_count":1,"completed_count":3,"review_count":1,"blocked_count":0,"updated_at":"2026-04-28T10:00:00Z"}}`)
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/runtime/iworker/instances/heartbeat", heartbeatBody)
	heartbeatReq.Header.Set("X-Tenant-ID", "tenant-a")
	heartbeatRec := httptest.NewRecorder()
	mux.ServeHTTP(heartbeatRec, heartbeatReq)
	if heartbeatRec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", heartbeatRec.Code, heartbeatRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/iworker/instances?worker_id=worker-a&offline_after_seconds=90", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Instances) != 1 {
		t.Fatalf("instances len = %d, want 1", len(body.Instances))
	}
	item := body.Instances[0]
	if item.WorkStatus == nil || item.WorkStatus.CurrentTask != "Review invoice exception" || item.WorkStatus.ReviewCount != 1 {
		t.Fatalf("work status = %+v", item.WorkStatus)
	}
	if item.EffectiveStatus != "busy" || item.HeartbeatAgeSeconds < 0 {
		t.Fatalf("unexpected health fields: %+v", item)
	}
}

func TestClientListHandlerReturnsTenantScopedWorkerRuntime(t *testing.T) {
	svc := NewService(newTestRepo(t))
	handler := NewHandler(svc)
	mux := http.NewServeMux()
	handler.RegisterRuntimeRoutes(mux)
	handler.RegisterClientRoutes(mux)

	postHeartbeat := func(tenantID, workerID, instanceID, currentTask string) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"worker_id":   workerID,
			"instance_id": instanceID,
			"role":        "executor",
			"status":      "online",
			"work_status": map[string]any{
				"current_task":    currentTask,
				"active_count":    1,
				"completed_count": 0,
				"review_count":    0,
				"blocked_count":   0,
			},
		})
		if err != nil {
			t.Fatalf("marshal heartbeat: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/runtime/iworker/instances/heartbeat", bytes.NewReader(payload))
		req.Header.Set("X-Tenant-ID", tenantID)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("heartbeat status = %d body=%s", rec.Code, rec.Body.String())
		}
	}

	postHeartbeat("tenant-a", "worker-a", "worker-a:executor", "Tenant A task")
	postHeartbeat("tenant-b", "worker-a", "worker-a:executor", "Tenant B task")
	postHeartbeat("tenant-a", "worker-b", "worker-b:executor", "Other worker task")

	req := httptest.NewRequest(http.MethodGet, "/client/iworker/instances?worker_id=worker-a&offline_after_seconds=90", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Instances []Instance `json:"instances"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Instances) != 1 {
		t.Fatalf("instances len = %d, want 1: %+v", len(body.Instances), body.Instances)
	}
	item := body.Instances[0]
	if item.TenantID != "tenant-a" || item.WorkerID != "worker-a" || item.InstanceID != "worker-a:executor" {
		t.Fatalf("wrong instance returned: %+v", item)
	}
	if item.WorkStatus == nil || item.WorkStatus.CurrentTask != "Tenant A task" {
		t.Fatalf("work status = %+v", item.WorkStatus)
	}
}
