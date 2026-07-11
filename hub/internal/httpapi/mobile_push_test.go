package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMobileIsTerminalStatus(t *testing.T) {
	if !mobileIsTerminalStatus("completed") || !mobileIsTerminalStatus("FAILED") {
		t.Fatal("terminal")
	}
	if mobileIsTerminalStatus("running") || mobileIsTerminalStatus("hub_streaming") {
		t.Fatal("non-terminal")
	}
	if !mobileIsTerminalStatus("cancelled") {
		t.Fatal("cancelled")
	}
}

func TestMobilePushPendingEnqueueAndAck(t *testing.T) {
	tenant, user := "t_push", "u_push_"+time.Now().Format("150405.000")
	// clean
	mobilePushState.Lock()
	delete(mobilePushState.pending, mobilePushUserKey(tenant, user))
	delete(mobilePushState.devices, mobilePushUserKey(tenant, user))
	mobilePushState.Unlock()

	item := mobilePushEnqueue(tenant, user, mobilePushPendingItem{
		Type: "ssh_task", Title: "SSH 任务完成", Body: "ls", Status: "completed",
		TaskID: "task-1", DedupeKey: "ssh_task:task-1:completed",
	})
	if item.ID == "" {
		t.Fatal("id")
	}
	// dedupe replace
	item2 := mobilePushEnqueue(tenant, user, mobilePushPendingItem{
		Type: "ssh_task", Title: "SSH 任务完成", Body: "ls again", Status: "completed",
		TaskID: "task-1", DedupeKey: "ssh_task:task-1:completed",
	})
	list := mobilePushListPending(tenant, user)
	if len(list) != 1 || list[0].ID != item2.ID {
		t.Fatalf("list=%#v", list)
	}
	if n := mobilePushAck(tenant, user, []string{item2.ID}); n != 1 {
		t.Fatalf("acked=%d", n)
	}
	if len(mobilePushListPending(tenant, user)) != 0 {
		t.Fatal("want empty after ack")
	}
}

func TestMobilePushDeviceUpsert(t *testing.T) {
	tenant, user := "t_dev", "u_dev"
	mobilePushState.Lock()
	delete(mobilePushState.devices, mobilePushUserKey(tenant, user))
	mobilePushState.Unlock()

	mobilePushUpsertDevice(tenant, user, mobilePushDevice{Platform: "fcm", Token: "tok-a", DeviceID: "d1"})
	mobilePushUpsertDevice(tenant, user, mobilePushDevice{Platform: "android", Token: "tok-b", DeviceID: "d1"}) // same device replaces
	devs := mobilePushListDevices(tenant, user)
	if len(devs) != 1 || devs[0].Token != "tok-b" || devs[0].Platform != "fcm" {
		t.Fatalf("devs=%#v", devs)
	}
	if !mobilePushRemoveDevice(tenant, user, "tok-b") {
		t.Fatal("remove")
	}
	if len(mobilePushListDevices(tenant, user)) != 0 {
		t.Fatal("want empty")
	}
}

func TestMobilePushOnRealtimeEventEnqueuesWhenOffline(t *testing.T) {
	tenant, user := "t_rt", "u_rt"
	mobilePushState.Lock()
	delete(mobilePushState.pending, mobilePushUserKey(tenant, user))
	mobilePushState.Unlock()

	mobilePushOnRealtimeEvent(tenant, user, map[string]any{
		"type":   "ssh_task",
		"status": "completed",
		"task_id": "t1",
		"task": map[string]any{
			"task_id": "t1",
			"status":  "completed",
			"command": "echo hi",
			"message": "done",
		},
	}, 0)
	list := mobilePushListPending(tenant, user)
	if len(list) != 1 || list[0].Title == "" {
		t.Fatalf("pending=%#v", list)
	}
	// non-terminal ignored
	mobilePushOnRealtimeEvent(tenant, user, map[string]any{
		"type": "ssh_task", "status": "running", "task_id": "t2",
		"task": map[string]any{"task_id": "t2", "status": "running"},
	}, 0)
	if len(mobilePushListPending(tenant, user)) != 1 {
		t.Fatal("running should not enqueue")
	}
}

func TestMobilePushDevicesHandlerAuth(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	// unauth
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/push/devices", nil)
	rec := httptest.NewRecorder()
	MobilePushDevicesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
	token, _ := issueViewerToken(t, identity, "mobile-push@example.com")
	req2 := httptest.NewRequest(http.MethodPost, "/api/mobile/push/devices",
		strings.NewReader(`{"platform":"device","token":"dev-token-1","device_id":"phone-1"}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	MobilePushDevicesHandler(identity).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	req3 := httptest.NewRequest(http.MethodGet, "/api/mobile/push/devices", nil)
	req3.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	MobilePushDevicesHandler(identity).ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("status=%d", rec3.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec3.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	devs, _ := body["devices"].([]any)
	if len(devs) < 1 {
		t.Fatalf("body=%#v", body)
	}
}

func TestMobilePushStatePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mobile-state.json")
	t.Setenv(mobileStatePathEnv, path)
	mobileResetStatePersistenceForTest()
	// load empty path as loaded
	mobileEnsureStateLoaded()

	tenant, user := "t_persist", "u_persist"
	mobilePushUpsertDevice(tenant, user, mobilePushDevice{
		Platform: "device", Token: "tok-persist", DeviceID: "d1",
	})
	mobilePushEnqueue(tenant, user, mobilePushPendingItem{
		Type: "ssh_task", Title: "SSH 任务完成", Body: "ls",
		Status: "completed", TaskID: "task-p1",
		DedupeKey: "ssh_task:task-p1:completed",
	})
	// Allow async schedulePersist to flush.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Force sync write in case go routine raced.
	mobilePersistState()

	// Clear memory and reload from disk.
	mobilePushResetForTest()
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()

	devs := mobilePushListDevices(tenant, user)
	if len(devs) != 1 || devs[0].Token != "tok-persist" {
		t.Fatalf("devices after reload=%#v", devs)
	}
	pending := mobilePushListPending(tenant, user)
	if len(pending) != 1 || pending[0].TaskID != "task-p1" {
		t.Fatalf("pending after reload=%#v", pending)
	}
}

func TestMobileBootstrapPushPaths(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-boot-push@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBootstrapHandler(identity, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	features, _ := body["features"].(map[string]any)
	if features["push_pending_sync"] != true {
		t.Fatalf("features=%#v", features)
	}
	services, _ := body["services"].(map[string]any)
	if services["push_pending_path"] == nil || services["push_devices_path"] == nil {
		t.Fatalf("services=%#v", services)
	}
	if body["push"] == nil {
		t.Fatal("want push transport summary")
	}
}
