package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

const cloudWorkspaceInstanceSessionHeader = "X-Cloud-Workspace-Instance-Session"

func authenticateCloudWorkspaceMachine(w http.ResponseWriter, r *http.Request, svc *cloudworkspace.Service, authenticator veMachineAuthenticator) (*auth.MachinePrincipal, bool) {
	principal, ok := authenticateVEMachine(w, r, authenticator)
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(principal.UserID) == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
		return nil, false
	}
	if svc == nil {
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_FORBIDDEN", "cloud workspace is not enabled")
		return nil, false
	}
	protocol := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Protocol"))
	session, err := svc.AuthenticateInstanceSession(r.Context(), *principal, strings.TrimSpace(r.Header.Get(cloudWorkspaceInstanceSessionHeader)), protocol)
	if err != nil {
		writeCloudWorkspaceError(w, err)
		return nil, false
	}
	if claimed := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Instance")); claimed != "" && claimed != session.ClientInstanceID {
		writeCloudWorkspaceError(w, cloudworkspace.ErrInstanceSessionInvalid)
		return nil, false
	}
	principal.ClientInstanceID = session.ClientInstanceID
	// Fencing remains a lease epoch, not an identity assertion. Parse it only
	// after the Hub-issued instance session has established the caller.
	if raw := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Fencing")); raw != "" {
		if token, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && token > 0 {
			principal.FencingToken = token
		}
	}
	return principal, true
}

// CloudWorkspaceIssueInstanceSessionHandler POST /api/v1/cloud-workspace-sessions.
// Machine credentials bootstrap a short-lived, Hub-generated process identity.
func CloudWorkspaceIssueInstanceSessionHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if strings.TrimSpace(principal.UserID) == "" {
			writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		protocol := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Protocol"))
		if protocol != cloudworkspace.CloudWorkspaceProtocol {
			writeCloudWorkspaceError(w, cloudworkspace.ErrProtocolMismatch)
			return
		}
		session, err := svc.IssueInstanceSession(r.Context(), *principal, protocol)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, session)
	}
}

// CloudWorkspaceRevokeInstanceSessionHandler DELETE /api/v1/cloud-workspace-sessions/{session_id}.
func CloudWorkspaceRevokeInstanceSessionHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if strings.TrimSpace(principal.UserID) == "" {
			writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
			return
		}
		protocol := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Protocol"))
		if err := svc.RevokeInstanceSession(
			r.Context(), *principal, strings.TrimSpace(r.PathValue("session_id")),
			strings.TrimSpace(r.Header.Get(cloudWorkspaceInstanceSessionHeader)), protocol,
		); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
	}
}

func requireCloudWorkspaceGrant(w http.ResponseWriter, r *http.Request, svc *cloudworkspace.Service, principal *auth.MachinePrincipal) bool {
	if svc == nil {
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_FORBIDDEN", "cloud workspace is not enabled")
		return false
	}
	ok, err := svc.Granted(r.Context(), *principal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CLOUD_WORKSPACE_GRANT_FAILED", "cloud workspace grant could not be evaluated")
		return false
	}
	if !ok {
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_FORBIDDEN", "cloud workspace is not enabled for this user")
		return false
	}
	return true
}

// requireCloudWorkspaceProtocol rejects requests that explicitly negotiate a
// protocol other than the only writable protocol currently supported. The
// header remains optional for backwards-compatible read/probe requests; when
// supplied it must be an exact v1-sequential match. Lease heartbeats and
// releases use the same guard as manifest/object writes so an old client
// cannot accidentally keep a lease alive or release a newer protocol session.
func requireCloudWorkspaceProtocol(w http.ResponseWriter, r *http.Request) bool {
	if protocol := strings.TrimSpace(r.Header.Get("X-Cloud-Workspace-Protocol")); protocol != "" && protocol != cloudworkspace.CloudWorkspaceProtocol {
		writeCloudWorkspaceError(w, cloudworkspace.ErrProtocolMismatch)
		return false
	}
	return true
}

func writeCloudWorkspaceInUse(w http.ResponseWriter, err error) {
	payload := map[string]any{
		"error":               "CLOUD_WORKSPACE_IN_USE",
		"holder_machine_id":   "",
		"holder_machine_name": "",
		"expires_at":          "",
	}
	var inUse *cloudworkspace.InUseError
	if errors.As(err, &inUse) && inUse != nil {
		payload["holder_machine_id"] = inUse.HolderMachineID
		payload["holder_machine_name"] = inUse.HolderMachineName
		payload["expires_at"] = inUse.ExpiresAt
	}
	writeJSON(w, http.StatusConflict, payload)
}

func observeCloudWorkspaceError(err error) {
	switch {
	case errors.Is(err, cloudworkspace.ErrInUse):
		cloudworkspace.ObserveLeaseConflict()
	case errors.Is(err, cloudworkspace.ErrBandwidthLimit):
		cloudworkspace.ObserveBandwidthRejection()
	case errors.Is(err, cloudworkspace.ErrQuota),
		errors.Is(err, cloudworkspace.ErrWorkspaceSize),
		errors.Is(err, cloudworkspace.ErrTenantDisk),
		errors.Is(err, cloudworkspace.ErrVolumeFull),
		errors.Is(err, cloudworkspace.ErrDiskFull):
		cloudworkspace.ObserveQuotaRejection()
	}
}

// writeCloudWorkspaceBandwidthLimit answers an hourly-quota rejection with
// 429 plus a Retry-After contract so clients can schedule a single window
// reset retry instead of a hot loop.
func writeCloudWorkspaceBandwidthLimit(w http.ResponseWriter, err error) {
	var detail *cloudworkspace.BandwidthLimitError
	retryAfter := int64(0)
	if errors.As(err, &detail) && detail != nil {
		retryAfter = detail.RetryAfterSeconds
	}
	if retryAfter < 1 {
		retryAfter = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	writeErrorWithFields(w, http.StatusTooManyRequests, "CLOUD_WORKSPACE_BANDWIDTH", "cloud workspace hourly bandwidth limit exceeded", map[string]any{
		"retry_after_seconds": retryAfter,
	})
}

func cloudWorkspaceIsSyncFailure(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, cloudworkspace.ErrInUse),
		errors.Is(err, cloudworkspace.ErrNotFound),
		errors.Is(err, cloudworkspace.ErrRestoreWindow),
		errors.Is(err, cloudworkspace.ErrBlobNotFound),
		errors.Is(err, cloudworkspace.ErrQuota),
		errors.Is(err, cloudworkspace.ErrWorkspaceSize),
		errors.Is(err, cloudworkspace.ErrTenantDisk),
		errors.Is(err, cloudworkspace.ErrLeaseRequired),
		errors.Is(err, cloudworkspace.ErrFenced),
		errors.Is(err, cloudworkspace.ErrInstanceSessionRequired),
		errors.Is(err, cloudworkspace.ErrInstanceSessionInvalid),
		errors.Is(err, cloudworkspace.ErrIdempotencyKeyReused),
		errors.Is(err, cloudworkspace.ErrAuditEventIDReused),
		errors.Is(err, cloudworkspace.ErrIdempotencyInProgress),
		errors.Is(err, cloudworkspace.ErrNameTaken),
		errors.Is(err, cloudworkspace.ErrRevisionConflict),
		errors.Is(err, cloudworkspace.ErrVolumeFull),
		errors.Is(err, cloudworkspace.ErrDiskFull),
		errors.Is(err, cloudworkspace.ErrInvalidName),
		errors.Is(err, cloudworkspace.ErrInvalidPath),
		errors.Is(err, cloudworkspace.ErrInvalidBlobKey),
		errors.Is(err, cloudworkspace.ErrBlobHashMismatch),
		errors.Is(err, cloudworkspace.ErrObjectMissing),
		errors.Is(err, cloudworkspace.ErrObjectDeleting),
		errors.Is(err, cloudworkspace.ErrTooManyEntries),
		errors.Is(err, cloudworkspace.ErrIncompleteChunks),
		errors.Is(err, cloudworkspace.ErrInvalidChunkIndex),
		errors.Is(err, cloudworkspace.ErrContentLength),
		errors.Is(err, cloudworkspace.ErrBlobTooLarge),
		errors.Is(err, cloudworkspace.ErrBandwidthLimit),
		errors.Is(err, cloudworkspace.ErrProtocolMismatch):
		return false
	case errors.Is(err, cloudworkspace.ErrProvisionState):
		return false
	default:
		return true
	}
}

func writeCloudWorkspaceSyncError(w http.ResponseWriter, r *http.Request, svc *cloudworkspace.Service, tenantID, workspaceID string, err error) {
	writeCloudWorkspaceError(w, err)
	if svc != nil && cloudWorkspaceIsSyncFailure(err) {
		svc.RecordSyncFailed(r.Context(), tenantID, workspaceID, err.Error())
	}
}

func writeCloudWorkspaceError(w http.ResponseWriter, err error) {
	if replay, ok := cloudworkspace.IdempotencyReplay(err); ok {
		replayCloudWorkspaceIdempotency(w, replay)
		return
	}
	observeCloudWorkspaceError(err)
	switch {
	case errors.Is(err, cloudworkspace.ErrInUse):
		writeCloudWorkspaceInUse(w, err)
	case errors.Is(err, cloudworkspace.ErrNotFound), errors.Is(err, cloudworkspace.ErrRestoreWindow):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
	case errors.Is(err, cloudworkspace.ErrBlobNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace object not found")
	case errors.Is(err, cloudworkspace.ErrQuota):
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_QUOTA", "cloud workspace quota exceeded")
	case errors.Is(err, cloudworkspace.ErrWorkspaceSize):
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_SIZE", "cloud workspace size exceeded")
	case errors.Is(err, cloudworkspace.ErrTenantDisk):
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_TENANT_DISK", "tenant cloud workspace disk quota exceeded")
	case errors.Is(err, cloudworkspace.ErrBandwidthLimit):
		writeCloudWorkspaceBandwidthLimit(w, err)
	case errors.Is(err, cloudworkspace.ErrLeaseRequired):
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_LEASE_REQUIRED", "cloud workspace lease required")
	case errors.Is(err, cloudworkspace.ErrInstanceSessionRequired):
		writeError(w, http.StatusUnauthorized, "CLOUD_WORKSPACE_SESSION_REQUIRED", "cloud workspace instance session required")
	case errors.Is(err, cloudworkspace.ErrInstanceSessionInvalid):
		writeError(w, http.StatusUnauthorized, "CLOUD_WORKSPACE_SESSION_INVALID", "cloud workspace instance session is invalid or expired")
	case errors.Is(err, cloudworkspace.ErrFenced):
		writeError(w, http.StatusConflict, "FENCED", "cloud workspace writer session is no longer current")
	case errors.Is(err, cloudworkspace.ErrIdempotencyKeyReused):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was already used with a different payload")
	case errors.Is(err, cloudworkspace.ErrAuditEventIDReused):
		writeError(w, http.StatusConflict, "AUDIT_EVENT_ID_REUSED", "audit event id was already used with a different payload")
	case errors.Is(err, cloudworkspace.ErrIdempotencyInProgress):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_IN_PROGRESS", "idempotent request is still in progress")
	case errors.Is(err, cloudworkspace.ErrNameTaken):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_NAME_TAKEN", "cloud workspace name is already in use")
	case errors.Is(err, cloudworkspace.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_REVISION_CONFLICT", "cloud workspace revision conflict")
	case errors.Is(err, cloudworkspace.ErrVolumeFull), errors.Is(err, cloudworkspace.ErrDiskFull):
		writeError(w, http.StatusInsufficientStorage, "CLOUD_WORKSPACE_VOLUME_FULL", "cloud workspace volume is full")
	case errors.Is(err, cloudworkspace.ErrInvalidName),
		errors.Is(err, cloudworkspace.ErrInvalidInput),
		errors.Is(err, cloudworkspace.ErrInvalidPath),
		errors.Is(err, cloudworkspace.ErrInvalidBlobKey),
		errors.Is(err, cloudworkspace.ErrBlobHashMismatch),
		errors.Is(err, cloudworkspace.ErrObjectMissing),
		errors.Is(err, cloudworkspace.ErrTooManyEntries),
		errors.Is(err, cloudworkspace.ErrIncompleteChunks),
		errors.Is(err, cloudworkspace.ErrInvalidChunkIndex),
		errors.Is(err, cloudworkspace.ErrContentLength),
		errors.Is(err, cloudworkspace.ErrBlobTooLarge),
		errors.Is(err, cloudworkspace.ErrInvalidSidecarName):
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, cloudworkspace.ErrObjectDeleting):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_OBJECT_DELETING", "cloud workspace object cleanup is in progress; retry later")
	case errors.Is(err, cloudworkspace.ErrProtocolMismatch):
		writeError(w, http.StatusConflict, "PROTOCOL_MISMATCH", "cloud workspace uses v1-sequential")
	case errors.Is(err, cloudworkspace.ErrProvisionState):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_PROVISION_STATE", "cloud workspace task provisioning state does not allow this transition")
	case errors.Is(err, cloudworkspace.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "cloud workspace store is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "CLOUD_WORKSPACE_FAILED", "cloud workspace operation failed")
	}
}

func workspaceTaskProvisionJSON(op *cloudworkspace.WorkspaceTaskProvision) map[string]any {
	if op == nil {
		return map[string]any{}
	}
	return map[string]any{
		"operation_id":   op.OperationID,
		"workspace_id":   op.WorkspaceID,
		"cloud_task_id":  op.CloudTaskID,
		"device_task_id": op.DeviceTaskID,
		"name":           op.Name,
		"mode":           op.Mode,
		"tag":            op.Tag,
		"state":          op.State,
		"last_error":     op.LastError,
		"created_at":     op.CreatedAt,
		"updated_at":     op.UpdatedAt,
	}
}

// CloudWorkspaceTaskProvisionHandler POST /api/v1/cloud-workspace-tasks.
// It commits the Hub-side workspace and unique task binding as one durable
// provisioning operation. The local task is acknowledged separately through
// the /complete endpoint once its sidecar is safely written.
func CloudWorkspaceTaskProvisionHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		var req struct {
			Name         string `json:"name"`
			CloudTaskID  string `json:"cloud_task_id"`
			DeviceTaskID string `json:"device_task_id"`
			Mode         string `json:"mode"`
			Tag          string `json:"tag"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace task request")
			return
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(req)
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace task request")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace-task:provision:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				op, _ := value.(*cloudworkspace.WorkspaceTaskProvision)
				return workspaceTaskProvisionJSON(op)
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusAccepted, project))
			r = atomicRequest
			if beginErr != nil {
				// The workspace transaction and the generic ledger finalize are
				// intentionally separate. If Hub crashed after creating the
				// durable operation but before FinishIdempotency, recover that
				// operation instead of executing a second create.
				if errors.Is(beginErr, cloudworkspace.ErrIdempotencyInProgress) {
					if existing, findErr := svc.FindWorkspaceTaskProvisionByIdempotencyKey(r.Context(), *principal, key, payloadHash); findErr == nil {
						response := workspaceTaskProvisionJSON(existing)
						if raw, marshalErr := json.Marshal(response); marshalErr == nil {
							_ = svc.Workspaces.FinishIdempotency(r.Context(), principal.TenantID, principal.UserID, "", ledgerKey, payloadHash, http.StatusAccepted, raw, time.Now().UTC())
						}
						writeJSON(w, http.StatusAccepted, response)
						return
					} else if errors.Is(findErr, cloudworkspace.ErrIdempotencyKeyReused) {
						writeCloudWorkspaceError(w, findErr)
						return
					}
				}
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		op, err := svc.BeginWorkspaceTaskProvision(r.Context(), *principal, cloudworkspace.WorkspaceTaskProvisionParams{
			Name: req.Name, CloudTaskID: req.CloudTaskID, DeviceTaskID: req.DeviceTaskID, Mode: req.Mode, Tag: req.Tag,
			IdempotencyKey: key, IdempotencyPayloadHash: payloadHash,
		})
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := workspaceTaskProvisionJSON(op)
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusAccepted, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusAccepted, response)
	}
}

// CloudWorkspaceTaskProvisionStatusHandler GET /api/v1/cloud-workspace-tasks/{operation_id}.
func CloudWorkspaceTaskProvisionStatusHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		op, err := svc.GetWorkspaceTaskProvision(r.Context(), *principal, strings.TrimSpace(r.PathValue("operation_id")))
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, workspaceTaskProvisionJSON(op))
	}
}

func cloudWorkspaceTaskProvisionTransitionHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator, abort bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		opID := strings.TrimSpace(r.PathValue("operation_id"))
		if opID == "" {
			writeCloudWorkspaceError(w, cloudworkspace.ErrNotFound)
			return
		}
		reason := ""
		if abort {
			var req struct {
				Reason string `json:"reason"`
			}
			if r.Body != nil {
				_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
			}
			reason = req.Reason
		}
		payloadHash, _, _ := cloudWorkspacePayloadHash(map[string]any{"operation_id": opID, "action": map[bool]string{true: "abort", false: "complete"}[abort], "reason": reason})
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace-task:" + map[bool]string{true: "abort", false: "complete"}[abort] + ":" + opID + ":" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				op, _ := value.(*cloudworkspace.WorkspaceTaskProvision)
				return workspaceTaskProvisionJSON(op)
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusOK, project))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		var op *cloudworkspace.WorkspaceTaskProvision
		var err error
		if abort {
			op, err = svc.AbortWorkspaceTaskProvision(r.Context(), *principal, opID, reason)
		} else {
			op, err = svc.CompleteWorkspaceTaskProvision(r.Context(), *principal, opID)
		}
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := workspaceTaskProvisionJSON(op)
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func workspaceJSON(ws *cloudworkspace.Workspace) map[string]any {
	if ws == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"id":         ws.ID,
		"name":       ws.Name,
		"status":     ws.Status,
		"used_bytes": ws.UsedBytes,
		"created_at": ws.CreatedAt,
		"updated_at": ws.UpdatedAt,
	}
	if ws.DeletedAt != "" {
		out["deleted_at"] = ws.DeletedAt
	}
	return out
}

func decodeOptionalName(r *http.Request) (string, error) {
	var req struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return req.Name, nil
}

// CloudWorkspaceEntitlementHandler GET /api/v1/cloud-workspaces/entitlement.
// Machine auth is enough: an unbound machine is a 200 enabled=false probe, not 401.
// Mutations still require authenticateCloudWorkspaceMachine (UserID present).
func CloudWorkspaceEntitlementHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if svc == nil {
			writeJSON(w, http.StatusOK, cloudworkspace.Entitlement{
				Workspaces: []cloudworkspace.EntitlementWorkspace{},
				Deleted:    []cloudworkspace.EntitlementDeletedWorkspace{},
			})
			return
		}
		ent, err := svc.EntitlementFor(r.Context(), *principal)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CLOUD_WORKSPACE_FAILED", "cloud workspace entitlement could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, ent)
	}
}

// CloudWorkspaceCreateHandler POST /api/v1/cloud-workspaces
func CloudWorkspaceCreateHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
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
		name, err := decodeOptionalName(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace request")
			return
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(struct {
			Name string `json:"name"`
		}{Name: strings.TrimSpace(name)})
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace request")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace:create:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				workspace, _ := value.(*cloudworkspace.Workspace)
				return workspaceJSON(workspace)
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusCreated, project))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		ws, err := svc.CreateWorkspace(r.Context(), *principal, name)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response, _ := json.Marshal(workspaceJSON(ws))
		if key != "" && svc.Workspaces != nil {
			_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, "", principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusCreated, response, time.Now().UTC())
		}
		writeJSON(w, http.StatusCreated, workspaceJSON(ws))
	}
}

// CloudWorkspaceRenameHandler PATCH /api/v1/cloud-workspaces/{id}
func CloudWorkspaceRenameHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
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
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace request")
			return
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(struct {
			WorkspaceID string `json:"workspace_id"`
			Name        string `json:"name"`
		}{WorkspaceID: id, Name: strings.TrimSpace(req.Name)})
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace request")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace:rename:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				workspace, _ := value.(*cloudworkspace.Workspace)
				return workspaceJSON(workspace)
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
		ws, err := svc.RenameWorkspace(r.Context(), *principal, id, req.Name)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := workspaceJSON(ws)
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceDeleteHandler DELETE /api/v1/cloud-workspaces/{id}
func CloudWorkspaceDeleteHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
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
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		payloadHash, _, _ := cloudWorkspacePayloadHash(map[string]any{"workspace_id": id, "action": "delete"})
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace:delete:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				workspace, _ := value.(*cloudworkspace.Workspace)
				return workspaceJSON(workspace)
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
		ws, err := svc.SoftDeleteWorkspace(r.Context(), *principal, id)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := workspaceJSON(ws)
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceHardDeleteHandler DELETE /api/v1/cloud-workspaces/{id}/purge
func CloudWorkspaceHardDeleteHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if !requireCloudWorkspaceProtocol(w, r) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		payloadHash, _, _ := cloudWorkspacePayloadHash(map[string]any{"workspace_id": id, "action": "purge"})
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace:purge:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicEmpty(http.StatusNoContent))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		if err := svc.HardDeleteDeletedWorkspace(r.Context(), *principal, id); err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusNoContent, nil, time.Now().UTC())
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// CloudWorkspaceRestoreHandler POST /api/v1/cloud-workspaces/{id}/restore
func CloudWorkspaceRestoreHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
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
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		payloadHash, _, _ := cloudWorkspacePayloadHash(map[string]any{"workspace_id": id, "action": "restore"})
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "workspace:restore:" + key
		if key != "" && svc.Workspaces != nil {
			project := func(value any) any {
				workspace, _ := value.(*cloudworkspace.Workspace)
				return workspaceJSON(workspace)
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
		ws, err := svc.RestoreWorkspace(r.Context(), *principal, id)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		response := workspaceJSON(ws)
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceEventsHandler returns ordered v2 file operations after a cursor.
func CloudWorkspaceEventsHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		writeError(w, http.StatusConflict, "PROTOCOL_MISMATCH", "cloud workspace uses v1-sequential; event replay is disabled")
	}
}

// CloudWorkspaceOperationHandler accepts an idempotent multi-writer file operation.
func CloudWorkspaceOperationHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		writeError(w, http.StatusConflict, "PROTOCOL_MISMATCH", "cloud workspace uses v1-sequential; per-file operations are disabled")
	}
}
