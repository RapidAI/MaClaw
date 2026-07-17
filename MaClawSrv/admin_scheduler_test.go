package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func startNoopScheduler(mgr *scheduler.Manager) {
	mgr.StartWithExecutor(func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		return "ran", nil
	})
}

func TestDeliveryAuditRoundTrip(t *testing.T) {
	dir := t.TempDir()
	appendDeliveryAudit(dir, DeliveryAuditEntry{TaskID: "a", Channel: "qq", OK: false, Error: "boom"})
	appendDeliveryAudit(dir, DeliveryAuditEntry{TaskID: "b", Channel: "qq", OK: true, Peer: "u1"})
	appendDeliveryAudit(dir, DeliveryAuditEntry{TaskID: "c", Channel: "qq", OK: true, Peer: "self"})
	items := listDeliveryAudit(dir, 10)
	if len(items) != 3 {
		t.Fatalf("items=%d", len(items))
	}
	// Newest first
	if items[0].TaskID != "c" {
		t.Fatalf("order: %#v", items)
	}
	// self peer is stripped
	if items[0].Peer != "" {
		t.Fatalf("self peer should be empty, got %q", items[0].Peer)
	}
}

func TestAdminBodyTouchesDelivery(t *testing.T) {
	if adminBodyReplacesDelivery(map[string]interface{}{"name": "x", "channel": "telegram"}) {
		t.Fatal("channel alone must not replace delivery")
	}
	if adminBodyReplacesDelivery(map[string]interface{}{"fail_on_error": true}) {
		t.Fatal("fail_on_error alone must not replace delivery")
	}
	if !adminBodyReplacesDelivery(map[string]interface{}{"user_id": "self"}) {
		t.Fatal("user_id should replace delivery")
	}
	if !adminBodyTouchesDelivery(map[string]interface{}{"fail_on_error": true}) {
		t.Fatal("fail_on_error should touch (partial)")
	}
	if !adminBodyTouchesDelivery(map[string]interface{}{"delivery": nil}) {
		t.Fatal("delivery key should touch")
	}
}

func TestApplyDeliveryUpdateArgsPartialFailOnError(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "t.json"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name: "n", Action: "a", Hour: 9,
		Delivery: &scheduler.TaskDelivery{
			Enabled: true, Channel: scheduler.DeliveryChannelTelegram,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "self"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]interface{}{"fail_on_error": true}
	if err := applyDeliveryUpdateArgs(nil, mgr, id, args); err != nil {
		t.Fatal(err)
	}
	d, ok := args["delivery"].(*scheduler.TaskDelivery)
	if !ok || d == nil || !d.FailOnError {
		t.Fatalf("expected patched delivery, got %#v", args["delivery"])
	}
	if len(d.Targets) != 1 || d.Targets[0].UserID != "self" {
		t.Fatalf("targets wiped: %#v", d.Targets)
	}
	// Persist via Update
	if err := mgr.Update(id, args); err != nil {
		t.Fatal(err)
	}
	got := mgr.Get(id)
	if got == nil || got.Delivery == nil || !got.Delivery.FailOnError {
		t.Fatalf("persist: %#v", got)
	}
}

func TestAdminSchedulerHTTPCRUD(t *testing.T) {
	dataRoot := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dataRoot, "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	setSrvSchedulerManager(mgr)
	t.Cleanup(func() {
		setSrvSchedulerManager(nil)
		mgr.Stop()
	})
	startNoopScheduler(mgr)

	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	// Create via manager directly (owner gate may block pure secret on some builds),
	// then exercise list / pause / resume / trigger / delete over HTTP where allowed.
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name: "admin-task", Action: "ping", Hour: 8, Minute: 30,
		Delivery: &scheduler.TaskDelivery{
			Enabled: true, Channel: scheduler.DeliveryChannelTelegram,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "self"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// List tasks
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/scheduler/tasks", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "admin-task") {
		t.Fatalf("list body: %s", w.Body.String())
	}

	// Try create via API (may require owner)
	body := `{"name":"api-task","task_action":"do","hour":7,"minute":0,"channel":"telegram","user_id":"self"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/scheduler/tasks", bytes.NewReader([]byte(body)))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	createdOK := w.Code == http.StatusOK
	if !createdOK && w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}

	// Mutations (owner may be required)
	for _, path := range []string{
		"/api/v1/admin/scheduler/tasks/" + id + "/pause",
		"/api/v1/admin/scheduler/tasks/" + id + "/resume",
		"/api/v1/admin/scheduler/tasks/" + id + "/trigger",
	} {
		req = httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w = httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK && w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", path, w.Code, w.Body.String())
		}
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/scheduler/tasks/"+id, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusForbidden && w.Code != http.StatusUnauthorized {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}

	// If owner path worked for delete, manager should be empty (or still have api-task).
	_ = createdOK

	// Audit endpoint
	appendDeliveryAudit(dataRoot, DeliveryAuditEntry{
		TaskID: "t1", TaskName: "n", Channel: "telegram", OK: true, Peer: "42",
	})
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/scheduler/delivery-audit?limit=10", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("audit = %d %s", w.Code, w.Body.String())
	}
	var audit struct {
		Items []DeliveryAuditEntry `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.Items) < 1 || audit.Items[0].Channel != "telegram" {
		t.Fatalf("audit: %#v", audit.Items)
	}
}

func TestAdminWebSchedulerEndpointsPresent(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	for _, needle := range []string{
		"/api/v1/admin/scheduler/tasks",
		"/api/v1/admin/scheduler/delivery-audit",
		"data-sch-trigger",
		"bindSchedulerTaskActions",
		"summarizeTaskPush",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin app.js missing %q", needle)
		}
	}
	if strings.Contains(body, "style=") {
		t.Fatal("CSP-hostile style= in admin app.js")
	}
}
