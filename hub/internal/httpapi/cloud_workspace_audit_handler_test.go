package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func TestCloudWorkspaceAuditHTTPRoundTripAndValidation(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "audit-http"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/cloud-workspaces/" + ws.ID + "/audit"
	first := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{
		"event_id": "http-event-1", "operation": "push", "outcome": "ok", "files": 1, "bytes": 7,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("record=%d %s", first.Code, first.Body.String())
	}
	replay := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{
		"event_id": "http-event-1", "operation": "push", "outcome": "ok", "files": 1, "bytes": 7,
	})
	if replay.Code != http.StatusOK {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	list := doCloudWorkspaceRequest(t, h, http.MethodGet, path+"?limit=10", "m1", "secret", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list=%d %s", list.Code, list.Body.String())
	}
	var payload struct {
		Events []cloudworkspace.AuditEvent `json:"events"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 1 || payload.Events[0].EventID != "http-event-1" || payload.Events[0].MachineID != "m1" {
		t.Fatalf("events=%+v", payload.Events)
	}
	bad := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{
		"operation": "push", "outcome": "failed", "detail": "path=/secret.txt",
	})
	if bad.Code != http.StatusBadRequest || cloudWorkspaceErrCode(t, bad) != "INVALID_INPUT" {
		t.Fatalf("bad=%d %s", bad.Code, bad.Body.String())
	}
	deleted := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+ws.ID, "m1", "secret", nil)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", deleted.Code, deleted.Body.String())
	}
	purged := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+ws.ID+"/purge", "m1", "secret", nil)
	if purged.Code != http.StatusNoContent {
		t.Fatalf("purge=%d %s", purged.Code, purged.Body.String())
	}
	late := doCloudWorkspaceRequest(t, h, http.MethodPost, path, "m1", "secret", map[string]any{
		"event_id": "http-purge-after-delete", "operation": "remote_purge", "outcome": "ok",
	})
	// After a hard purge the workspace row is gone; late audit uploads are
	// rejected and the device-local JSONL log remains the evidence of record.
	if late.Code != http.StatusNotFound || cloudWorkspaceErrCode(t, late) != "NOT_FOUND" {
		t.Fatalf("post-purge audit=%d %s, want 404 NOT_FOUND", late.Code, late.Body.String())
	}
}
