package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

type hardwareMeetingOwnerStub struct {
	tenantID string
	userID   string
	clientID string
	ok       bool
}

type hardwareMeetingNotifierStub struct {
	clientID       string
	conversationID string
	reply          map[string]any
	count          int
}

func (s *hardwareMeetingNotifierStub) EnqueueReply(clientID, conversationID string, reply map[string]any) {
	s.clientID = clientID
	s.conversationID = conversationID
	s.reply = reply
	s.count++
}

func TestHardwareMeetingRetryCanNotifyReadyAfterFailed(t *testing.T) {
	notifier := &hardwareMeetingNotifierStub{}
	previous := hardwareMeetingResults
	SetHardwareMeetingResultNotifier(notifier)
	t.Cleanup(func() { SetHardwareMeetingResultNotifier(previous) })
	id := "meeting_hardware_retry_result"
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[id] = mobileMeetingRecording{
		ID: id, OwnerID: "user-hw", TenantID: "tenant-hw", Status: "failed",
		HardwareClientID: "pet-hw", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, id)
		mobileMeetingRecordings.Unlock()
	})
	mobileNotifyHardwareMeetingResult(id, mobileMeetingRecordings.items[id])
	mobileMeetingRecordingUpdate(id, func(rec *mobileMeetingRecording) {
		rec.Status = "processing"
		rec.HardwareNotified = false
	})
	mobileMeetingRecordingUpdate(id, func(rec *mobileMeetingRecording) {
		rec.Status = "ready"
		rec.Minutes = "重试后处理成功"
	})
	if notifier.count != 2 {
		t.Fatalf("notification count=%d, want failed and ready", notifier.count)
	}
	extra, _ := notifier.reply["extra"].(map[string]any)
	if extra["status"] != "ready" {
		t.Fatalf("last notification=%#v", notifier.reply)
	}
}

func TestHardwareMeetingStartupReplaysUnnotifiedTerminalResult(t *testing.T) {
	notifier := &hardwareMeetingNotifierStub{}
	previous := hardwareMeetingResults
	SetHardwareMeetingResultNotifier(notifier)
	t.Cleanup(func() { SetHardwareMeetingResultNotifier(previous) })
	id := "meeting_hardware_restart_result"
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[id] = mobileMeetingRecording{
		ID: id, OwnerID: "user-hw", TenantID: "tenant-hw", Status: "ready",
		HardwareClientID: "pet-hw", Minutes: "重启后补发", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, id)
		mobileMeetingRecordings.Unlock()
	})
	mobileResumeMeetingRecordingWorkers()
	if notifier.count != 1 || notifier.clientID != "pet-hw" {
		t.Fatalf("startup notification count=%d client=%q reply=%#v", notifier.count, notifier.clientID, notifier.reply)
	}
}

func (s hardwareMeetingOwnerStub) AuthenticatedDeviceOwner(*http.Request) (string, string, string, bool) {
	return s.tenantID, s.userID, s.clientID, s.ok
}

func TestHardwareMeetingRecordingUsesMobileLibraryPipeline(t *testing.T) {
	owner := hardwareMeetingOwnerStub{tenantID: "tenant-hw", userID: "user-hw", clientID: "pet-hw", ok: true}
	handler := HardwareMeetingRecordingsHandler(owner)
	create := httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath, bytes.NewBufferString(`{"title":"硬件周会","content_type":"audio/wav"}`))
	createResp := httptest.NewRecorder()
	handler.ServeHTTP(createResp, create)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["recording_id"].(string)
	if id == "" {
		t.Fatalf("missing recording id: %#v", created)
	}
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		rec := mobileMeetingRecordings.items[id]
		delete(mobileMeetingRecordings.items, id)
		mobileMeetingRecordings.Unlock()
		if rec.Dir != "" {
			_ = os.RemoveAll(rec.Dir)
		}
	})

	wav := make([]byte, 46)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVEfmt ")
	copy(wav[36:], "data")
	sum := sha256.Sum256(wav)
	put := httptest.NewRequest(http.MethodPut, hardwareMeetingRecordingBasePath+"/"+id+"/chunks/0", bytes.NewReader(wav))
	put.Header.Set("X-Chunk-SHA256", hex.EncodeToString(sum[:]))
	putResp := httptest.NewRecorder()
	handler.ServeHTTP(putResp, put)
	if putResp.Code != http.StatusNoContent {
		t.Fatalf("put=%d %s", putResp.Code, putResp.Body.String())
	}
	complete := httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath+"/"+id+"/complete", bytes.NewBufferString(`{"chunks":1,"duration_sec":0.001}`))
	completeResp := httptest.NewRecorder()
	handler.ServeHTTP(completeResp, complete)
	if completeResp.Code != http.StatusOK {
		t.Fatalf("complete=%d %s", completeResp.Code, completeResp.Body.String())
	}
	rec, ok := mobileMeetingRecordingOwnedForTenant(owner.userID, owner.tenantID, id)
	if !ok || rec.Status != "uploaded" || rec.ContentType != "audio/wav" {
		t.Fatalf("recording not visible in shared library: %#v", rec)
	}
}

func TestHardwareMeetingRecordingRejectsInvalidDevice(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath, bytes.NewBufferString(`{"title":"denied"}`))
	resp := httptest.NewRecorder()
	HardwareMeetingRecordingsHandler(hardwareMeetingOwnerStub{}).ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHardwareMeetingRecordingIsIsolatedByClientID(t *testing.T) {
	creator := hardwareMeetingOwnerStub{tenantID: "tenant-hw", userID: "user-hw", clientID: "pet-a", ok: true}
	create := httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath, bytes.NewBufferString(`{"title":"private device recording","content_type":"audio/wav"}`))
	createResp := httptest.NewRecorder()
	HardwareMeetingRecordingsHandler(creator).ServeHTTP(createResp, create)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["recording_id"].(string)
	if id == "" {
		t.Fatalf("missing recording id: %#v", created)
	}
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		rec := mobileMeetingRecordings.items[id]
		delete(mobileMeetingRecordings.items, id)
		mobileMeetingRecordings.Unlock()
		if rec.Dir != "" {
			_ = os.RemoveAll(rec.Dir)
		}
	})

	otherDevice := hardwareMeetingOwnerStub{tenantID: creator.tenantID, userID: creator.userID, clientID: "pet-b", ok: true}
	handler := HardwareMeetingRecordingsHandler(otherDevice)
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, hardwareMeetingRecordingBasePath+"/"+id, nil),
		httptest.NewRequest(http.MethodPut, hardwareMeetingRecordingBasePath+"/"+id+"/chunks/0", bytes.NewReader([]byte("not visible"))),
		httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath+"/"+id+"/complete", bytes.NewBufferString(`{"chunks":1}`)),
		httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath+"/"+id+"/process", bytes.NewBufferString(`{"mode":"keep"}`)),
	}
	for _, req := range requests {
		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d body=%s", req.Method, req.URL.Path, resp.Code, resp.Body.String())
		}
	}

	ownerGet := httptest.NewRequest(http.MethodGet, hardwareMeetingRecordingBasePath+"/"+id, nil)
	ownerResp := httptest.NewRecorder()
	HardwareMeetingRecordingsHandler(creator).ServeHTTP(ownerResp, ownerGet)
	if ownerResp.Code != http.StatusOK {
		t.Fatalf("originating client get=%d %s", ownerResp.Code, ownerResp.Body.String())
	}
}

func TestHardwareMeetingRecordingRequiresClientIdentity(t *testing.T) {
	owner := hardwareMeetingOwnerStub{tenantID: "tenant-hw", userID: "user-hw", ok: true}
	req := httptest.NewRequest(http.MethodPost, hardwareMeetingRecordingBasePath, bytes.NewBufferString(`{"title":"unbound"}`))
	resp := httptest.NewRecorder()
	HardwareMeetingRecordingsHandler(owner).ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestHardwareMeetingResultReturnsToOriginatingDeviceOnce(t *testing.T) {
	notifier := &hardwareMeetingNotifierStub{}
	previous := hardwareMeetingResults
	SetHardwareMeetingResultNotifier(notifier)
	t.Cleanup(func() { SetHardwareMeetingResultNotifier(previous) })

	id := "meeting_hardware_result"
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[id] = mobileMeetingRecording{
		ID: id, OwnerID: "user-hw", TenantID: "tenant-hw", ConversationID: "meeting-room",
		Status: "processing", HardwareClientID: "pet-hw", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, id)
		mobileMeetingRecordings.Unlock()
	})

	mobileMeetingRecordingUpdate(id, func(rec *mobileMeetingRecording) {
		rec.Status = "ready"
		rec.Message = "meeting minutes ready"
		rec.Minutes = "决定：周五发布。"
	})
	if notifier.clientID != "pet-hw" || notifier.conversationID != "meeting-room" || notifier.reply["type"] != "meeting_result" {
		t.Fatalf("hardware notification=%#v client=%q conversation=%q", notifier.reply, notifier.clientID, notifier.conversationID)
	}
	notifier.reply = nil
	mobileMeetingRecordingUpdate(id, func(rec *mobileMeetingRecording) { rec.Message = "duplicate update" })
	if notifier.reply != nil {
		t.Fatalf("terminal hardware result was delivered twice: %#v", notifier.reply)
	}
}
