package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

// CloudWorkspaceGetTaskBindingHandler returns the server-owned task binding.
func CloudWorkspaceGetTaskBindingHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		binding, err := svc.GetTaskBinding(r.Context(), *principal, id)
		if err != nil {
			if err == cloudworkspace.ErrNotFound {
				writeJSON(w, http.StatusOK, map[string]any{"workspace_id": id, "bound": false})
				return
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, binding)
	}
}

// CloudWorkspacePutTaskBindingHandler creates or updates the unique binding.
func CloudWorkspacePutTaskBindingHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		var req struct {
			CloudTaskID     string `json:"cloud_task_id,omitempty"`
			DeviceTaskID    string `json:"device_task_id,omitempty"`
			Name            string `json:"name,omitempty"`
			Mode            string `json:"mode,omitempty"`
			Tag             string `json:"tag,omitempty"`
			ExpectedVersion int64  `json:"expected_version,omitempty"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace task binding")
			return
		}
		payloadHash, _, err := cloudWorkspacePayloadHash(req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace task binding")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "task-binding:put:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusOK, nil))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		binding, err := svc.UpsertTaskBinding(r.Context(), *principal, id, req.CloudTaskID, req.DeviceTaskID, req.Name, req.Mode, req.Tag, req.ExpectedVersion)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(binding); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, binding)
	}
}

// CloudWorkspaceDeleteTaskBindingHandler unbinds a task projection.
func CloudWorkspaceDeleteTaskBindingHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		rawExpected := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Binding-Version"))
		expected := int64(0)
		if rawExpected != "" {
			parsed, parseErr := strconv.ParseInt(rawExpected, 10, 64)
			if parseErr != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "binding version must be a non-negative integer")
				return
			}
			expected = parsed
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(struct {
			WorkspaceID     string `json:"workspace_id"`
			ExpectedVersion int64  `json:"expected_version,omitempty"`
		}{WorkspaceID: id, ExpectedVersion: expected})
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace task binding")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "task-binding:delete:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicStaticJSON(http.StatusOK, map[string]any{"deleted": true}))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		if err := svc.DeleteTaskBinding(r.Context(), *principal, id, expected); err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := map[string]any{"deleted": true}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}
