package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func decodeLeaseForce(r *http.Request) (bool, error) {
	var req struct {
		Force bool `json:"force"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	return req.Force, nil
}

func requireCloudWorkspacePathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
		return "", false
	}
	return id, true
}

// CloudWorkspaceAcquireLeaseHandler POST /api/v1/cloud-workspaces/{id}/leases
func CloudWorkspaceAcquireLeaseHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id, ok := requireCloudWorkspacePathID(w, r)
		if !ok {
			return
		}
		force, err := decodeLeaseForce(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace lease request")
			return
		}
		out, err := svc.AcquireLease(r.Context(), *principal, id, force)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceHeartbeatLeaseHandler POST /api/v1/cloud-workspaces/{id}/leases/{lease_id}/heartbeat
func CloudWorkspaceHeartbeatLeaseHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id, ok := requireCloudWorkspacePathID(w, r)
		if !ok {
			return
		}
		leaseID := strings.TrimSpace(r.PathValue("lease_id"))
		if leaseID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace lease not found")
			return
		}
		out, err := svc.HeartbeatLease(r.Context(), *principal, id, leaseID)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"lease_id":   out.LeaseID,
			"expires_at": out.ExpiresAt,
		})
	}
}

// CloudWorkspaceReleaseLeaseHandler DELETE /api/v1/cloud-workspaces/{id}/leases/{lease_id}
func CloudWorkspaceReleaseLeaseHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id, ok := requireCloudWorkspacePathID(w, r)
		if !ok {
			return
		}
		leaseID := strings.TrimSpace(r.PathValue("lease_id"))
		if leaseID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace lease not found")
			return
		}
		if err := svc.ReleaseLease(r.Context(), *principal, id, leaseID); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"released": true})
	}
}
