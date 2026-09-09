package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func decodeLeaseForce(r *http.Request) (bool, error) {
	var req struct {
		Force bool   `json:"force"`
		Mode  string `json:"mode"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(req.Mode)) {
	case "", "acquire":
		return req.Force, nil
	case "takeover":
		// `mode=takeover` is the explicit user-confirmed takeover form. Keep
		// accepting legacy force=true for older clients, but never let an
		// unrecognised mode silently become a force operation.
		return true, nil
	default:
		return false, errors.New("mode must be acquire or takeover")
	}
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
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
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
		if !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		force, err := decodeLeaseForce(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace lease request")
			return
		}
		payloadHash := sha256.Sum256([]byte(strconv.FormatBool(force)))
		payloadHex := hex.EncodeToString(payloadHash[:])
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "lease:acquire:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicJSON(http.StatusOK, nil))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		out, err := svc.AcquireLease(r.Context(), *principal, id, force)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(out); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceHandoffRequestHandler POST /api/v1/cloud-workspaces/{id}/leases/handoff-request
// Records a waiting device's request without granting it a writer lease.
func CloudWorkspaceHandoffRequestHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) || !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		id, ok := requireCloudWorkspacePathID(w, r)
		if !ok {
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		payloadHash, _, _ := cloudWorkspacePayloadHash(map[string]any{"workspace_id": id, "action": "handoff-request"})
		ledgerKey := "lease:handoff-request:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				lease, _ := value.(*cloudworkspace.Lease)
				if lease == nil {
					return map[string]any{"requested": true}
				}
				return map[string]any{"requested": true, "lease_id": lease.ID, "expires_at": lease.ExpiresAt, "handoff_requested_at": lease.HandoffRequestedAt}
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusOK, project))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		lease, err := svc.RequestLeaseHandoff(r.Context(), *principal, id)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := map[string]any{"requested": true, "lease_id": lease.ID, "expires_at": lease.ExpiresAt, "handoff_requested_at": lease.HandoffRequestedAt}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceHeartbeatLeaseHandler POST /api/v1/cloud-workspaces/{id}/leases/{lease_id}/heartbeat
func CloudWorkspaceHeartbeatLeaseHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
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
		if session := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Session")); session != "" && session != leaseID {
			writeCloudWorkspaceError(w, cloudworkspace.ErrFenced)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		payloadHash := sha256.Sum256([]byte(leaseID))
		payloadHex := hex.EncodeToString(payloadHash[:])
		ledgerKey := "lease:heartbeat:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				out, _ := value.(*cloudworkspace.AcquireOutcome)
				if out == nil {
					return map[string]any{}
				}
				return map[string]any{"lease_id": out.LeaseID, "expires_at": out.ExpiresAt}
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicJSON(http.StatusOK, project))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		out, err := svc.HeartbeatLease(r.Context(), *principal, id, leaseID)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := map[string]any{
			"lease_id":   out.LeaseID,
			"expires_at": out.ExpiresAt,
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceReleaseLeaseHandler DELETE /api/v1/cloud-workspaces/{id}/leases/{lease_id}
func CloudWorkspaceReleaseLeaseHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
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
		if session := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Session")); session != "" && session != leaseID {
			writeCloudWorkspaceError(w, cloudworkspace.ErrFenced)
			return
		}
		lastRevision := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Last-Revision"))
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		payloadHash := sha256.Sum256([]byte(leaseID + ":" + lastRevision))
		payloadHex := hex.EncodeToString(payloadHash[:])
		ledgerKey := "lease:release:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicStaticJSON(http.StatusOK, map[string]any{"released": true}))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		if err := svc.ReleaseLeaseWithRevision(r.Context(), *principal, id, leaseID, lastRevision); err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := map[string]any{"released": true}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}
