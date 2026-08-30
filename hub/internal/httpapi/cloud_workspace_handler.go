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

func authenticateCloudWorkspaceMachine(w http.ResponseWriter, r *http.Request, authenticator veMachineAuthenticator) (*auth.MachinePrincipal, bool) {
	principal, ok := authenticateVEMachine(w, r, authenticator)
	if !ok {
		return nil, false
	}
	if strings.TrimSpace(principal.UserID) == "" {
		writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
		return nil, false
	}
	return principal, true
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
	case errors.Is(err, cloudworkspace.ErrQuota),
		errors.Is(err, cloudworkspace.ErrWorkspaceSize),
		errors.Is(err, cloudworkspace.ErrTenantDisk),
		errors.Is(err, cloudworkspace.ErrVolumeFull),
		errors.Is(err, cloudworkspace.ErrDiskFull):
		cloudworkspace.ObserveQuotaRejection()
	}
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
		errors.Is(err, cloudworkspace.ErrNameTaken),
		errors.Is(err, cloudworkspace.ErrRevisionConflict),
		errors.Is(err, cloudworkspace.ErrVolumeFull),
		errors.Is(err, cloudworkspace.ErrDiskFull),
		errors.Is(err, cloudworkspace.ErrInvalidName),
		errors.Is(err, cloudworkspace.ErrInvalidPath),
		errors.Is(err, cloudworkspace.ErrInvalidBlobKey),
		errors.Is(err, cloudworkspace.ErrBlobHashMismatch),
		errors.Is(err, cloudworkspace.ErrObjectMissing),
		errors.Is(err, cloudworkspace.ErrTooManyEntries),
		errors.Is(err, cloudworkspace.ErrIncompleteChunks),
		errors.Is(err, cloudworkspace.ErrInvalidChunkIndex),
		errors.Is(err, cloudworkspace.ErrContentLength),
		errors.Is(err, cloudworkspace.ErrBlobTooLarge):
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
	case errors.Is(err, cloudworkspace.ErrLeaseRequired):
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_LEASE_REQUIRED", "cloud workspace lease required")
	case errors.Is(err, cloudworkspace.ErrNameTaken):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_NAME_TAKEN", "cloud workspace name is already in use")
	case errors.Is(err, cloudworkspace.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "CLOUD_WORKSPACE_REVISION_CONFLICT", "cloud workspace revision conflict")
	case errors.Is(err, cloudworkspace.ErrVolumeFull), errors.Is(err, cloudworkspace.ErrDiskFull):
		writeError(w, http.StatusInsufficientStorage, "CLOUD_WORKSPACE_VOLUME_FULL", "cloud workspace volume is full")
	case errors.Is(err, cloudworkspace.ErrInvalidName),
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
	case errors.Is(err, cloudworkspace.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "cloud workspace store is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "CLOUD_WORKSPACE_FAILED", "cloud workspace operation failed")
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
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		name, err := decodeOptionalName(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace request")
			return
		}
		ws, err := svc.CreateWorkspace(r.Context(), *principal, name)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, workspaceJSON(ws))
	}
}

// CloudWorkspaceRenameHandler PATCH /api/v1/cloud-workspaces/{id}
func CloudWorkspaceRenameHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
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
		ws, err := svc.RenameWorkspace(r.Context(), *principal, id, req.Name)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, workspaceJSON(ws))
	}
}

// CloudWorkspaceDeleteHandler DELETE /api/v1/cloud-workspaces/{id}
func CloudWorkspaceDeleteHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		ws, err := svc.SoftDeleteWorkspace(r.Context(), *principal, id)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, workspaceJSON(ws))
	}
}

// CloudWorkspaceHardDeleteHandler DELETE /api/v1/cloud-workspaces/{id}/purge
func CloudWorkspaceHardDeleteHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		if err := svc.HardDeleteDeletedWorkspace(r.Context(), *principal, id); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// CloudWorkspaceRestoreHandler POST /api/v1/cloud-workspaces/{id}/restore
func CloudWorkspaceRestoreHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok {
			return
		}
		if !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		ws, err := svc.RestoreWorkspace(r.Context(), *principal, id)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, workspaceJSON(ws))
	}
}

// CloudWorkspaceEventsHandler returns ordered v2 file operations after a cursor.
func CloudWorkspaceEventsHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace not found")
			return
		}
		after, err := parseNonNegativeQueryInt(r.URL.Query().Get("after_seq"), 0)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "after_seq must be a non-negative integer")
			return
		}
		limit, err := parseNonNegativeQueryInt(r.URL.Query().Get("limit"), 500)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "limit must be a non-negative integer")
			return
		}
		if svc.Workspaces == nil {
			writeCloudWorkspaceError(w, cloudworkspace.ErrUnavailable)
			return
		}
		if _, err := svc.Workspaces.GetOwned(r.Context(), principal.TenantID, principal.UserID, id); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		events, err := svc.Workspaces.ListEvents(r.Context(), id, after, limit)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

func parseNonNegativeQueryInt(raw string, fallback int64) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, errors.New("invalid non-negative integer")
	}
	return n, nil
}

// CloudWorkspaceOperationHandler accepts an idempotent multi-writer file operation.
func CloudWorkspaceOperationHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		id := strings.TrimSpace(r.PathValue("id"))
		var op cloudworkspace.Operation
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&op); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace operation")
			return
		}
		if op.Kind == "put" {
			if svc.Blobs == nil {
				writeCloudWorkspaceError(w, cloudworkspace.ErrUnavailable)
				return
			}
			has, err := svc.Blobs.Has(r.Context(), principal.TenantID, principal.UserID, id, op.ObjectSHA256)
			if err != nil || !has {
				if err == nil {
					err = cloudworkspace.ErrObjectMissing
				}
				writeCloudWorkspaceError(w, err)
				return
			}
		}
		if svc.Workspaces == nil {
			writeCloudWorkspaceError(w, cloudworkspace.ErrUnavailable)
			return
		}
		out, err := svc.Workspaces.ApplyOperation(r.Context(), principal.TenantID, principal.UserID, id, op, time.Now().UTC())
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}
