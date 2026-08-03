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
)

type hardwareMeetingOwnerStub struct {
	tenantID string
	userID   string
	clientID string
	ok       bool
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
