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

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func cloudWorkspacePayloadHash(v any) (string, []byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}

func replayCloudWorkspaceIdempotency(w http.ResponseWriter, rec *cloudworkspace.IdempotencyRecord) bool {
	if rec == nil || !rec.Completed {
		return false
	}
	w.Header().Set("Idempotency-Replayed", "true")
	if len(rec.Response) > 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(rec.StatusCode)
	_, _ = w.Write(rec.Response)
	return true
}

func prepareCloudWorkspaceAtomicRequest(r *http.Request, workspaces *cloudworkspace.Store, tenantID, userID, workspaceID, clientInstanceID, key, payloadHash string, encoder cloudworkspace.AtomicIdempotencyEncoder) (*http.Request, *cloudworkspace.IdempotencyRecord, error) {
	ctx, replay, err := workspaces.PrepareAtomicIdempotency(r.Context(), tenantID, userID, workspaceID, clientInstanceID, key, payloadHash, time.Now().UTC(), encoder)
	if ctx != r.Context() {
		r = r.WithContext(ctx)
	}
	return r, replay, err
}

func cloudWorkspaceAtomicJSON(statusCode int, project func(any) any) cloudworkspace.AtomicIdempotencyEncoder {
	return func(value any) (int, []byte, error) {
		if project != nil {
			value = project(value)
		}
		raw, err := json.Marshal(value)
		if err == nil {
			raw = append(raw, '\n')
		}
		return statusCode, raw, err
	}
}

func cloudWorkspaceAtomicStaticJSON(statusCode int, response any) cloudworkspace.AtomicIdempotencyEncoder {
	return cloudWorkspaceAtomicJSON(statusCode, func(any) any { return response })
}

func cloudWorkspaceAtomicEmpty(statusCode int) cloudworkspace.AtomicIdempotencyEncoder {
	return func(any) (int, []byte, error) { return statusCode, nil, nil }
}

const cloudWorkspaceManifestJSONLimit = 16 << 20

const cloudWorkspaceAuditJSONLimit = 64 << 10

func beginCloudWorkspaceSync(w http.ResponseWriter, r *http.Request, svc *cloudworkspace.Service, identity veMachineAuthenticator) (*auth.MachinePrincipal, string, bool) {
	principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
	if !ok {
		return nil, "", false
	}
	if !requireCloudWorkspaceGrant(w, r, svc, principal) {
		return nil, "", false
	}
	id, ok := requireCloudWorkspacePathID(w, r)
	if !ok {
		return nil, "", false
	}
	if !requireCloudWorkspaceProtocol(w, r) {
		return nil, "", false
	}
	return principal, id, true
}

func requireCloudWorkspaceSHA256(w http.ResponseWriter, r *http.Request) (string, bool) {
	sha := strings.TrimSpace(r.PathValue("sha256"))
	if !cloudworkspace.ValidSHA256Hex(sha) {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "sha256 must be 64 lowercase hex characters")
		return "", false
	}
	return sha, true
}

func readCloudWorkspacePlaintext(r *http.Request, max int64) ([]byte, error) {
	if r.ContentLength < 0 {
		return nil, cloudworkspace.ErrContentLength
	}
	if r.ContentLength > max {
		return nil, cloudworkspace.ErrBlobTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, r.ContentLength+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) != r.ContentLength {
		return nil, cloudworkspace.ErrContentLength
	}
	return body, nil
}

func writeCloudWorkspaceObjectMeta(w http.ResponseWriter, got cloudworkspace.PutResult) {
	writeJSON(w, http.StatusOK, cloudWorkspaceObjectResponse(got))
}

func cloudWorkspaceObjectResponse(value any) any {
	got, _ := value.(cloudworkspace.PutResult)
	return map[string]any{
		"sha256":  got.SHA256,
		"size":    got.SizeBytes,
		"existed": got.Existed,
	}
}

// CloudWorkspaceGetManifestHandler GET /api/v1/cloud-workspaces/{id}/manifest
func CloudWorkspaceGetManifestHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		out, err := svc.GetManifest(r.Context(), *principal, id)
		if err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		if out == nil {
			out = &cloudworkspace.Manifest{}
		}
		if out.Entries == nil {
			out.Entries = []cloudworkspace.ManifestEntry{}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceRecordAuditHandler POST /api/v1/cloud-workspaces/{id}/audit
//
// Audit writes intentionally do not require an active lease.  A fenced writer
// or failed release must still be able to report what happened after the
// lease has moved to another device.  Authentication, tenant ownership and
// protocol negotiation remain mandatory.
func CloudWorkspaceRecordAuditHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		var req cloudworkspace.AuditEvent
		dec := json.NewDecoder(io.LimitReader(r.Body, cloudWorkspaceAuditJSONLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace audit event")
			return
		}
		out, err := svc.RecordAudit(r.Context(), *principal, id, req)
		if err != nil {
			if errors.Is(err, cloudworkspace.ErrInvalidAuditEvent) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceListAuditHandler GET /api/v1/cloud-workspaces/{id}/audit
func CloudWorkspaceListAuditHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		afterID := int64(0)
		if raw := strings.TrimSpace(r.URL.Query().Get("after_id")); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "after_id must be a non-negative integer")
				return
			}
			afterID = parsed
		}
		limit := 100
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 || parsed > 500 {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "limit must be between 1 and 500")
				return
			}
			limit = parsed
		}
		events, err := svc.ListAudit(r.Context(), *principal, id, afterID, limit)
		if err != nil {
			if errors.Is(err, cloudworkspace.ErrInvalidAuditEvent) {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
				return
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		if events == nil {
			events = []cloudworkspace.AuditEvent{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}

// CloudWorkspacePutManifestHandler PUT /api/v1/cloud-workspaces/{id}/manifest
func CloudWorkspacePutManifestHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		var req struct {
			IfMatchRevision string                         `json:"if_match_revision"`
			Entries         []cloudworkspace.ManifestEntry `json:"entries"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, cloudWorkspaceManifestJSONLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace manifest")
			return
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(req)
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace manifest")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "manifest:put:" + key
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
		out, err := svc.PutManifest(r.Context(), *principal, id, req.IfMatchRevision, req.Entries)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			response, marshalErr := json.Marshal(out)
			if marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, response, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceManifestDeltaHandler applies a batch of puts/deletes against
// the current manifest revision. It is an optimization for large workspaces,
// not a v2 event stream.
func CloudWorkspaceManifestDeltaHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		var req cloudworkspace.ManifestDelta
		dec := json.NewDecoder(io.LimitReader(r.Body, cloudWorkspaceManifestJSONLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace manifest delta")
			return
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(req)
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace manifest delta")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "manifest:delta:" + key
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
		out, err := svc.ApplyManifestDelta(r.Context(), *principal, id, req)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(out); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceRestoreSnapshotHandler POST /api/v1/cloud-workspaces/{id}/snapshots/{snapshot_id}/restore
func CloudWorkspaceRestoreSnapshotHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		snapshotID := strings.TrimSpace(r.PathValue("snapshot_id"))
		if snapshotID == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "cloud workspace snapshot not found")
			return
		}
		var req struct {
			IfMatchRevision string `json:"if_match_revision,omitempty"`
		}
		if r.Body != nil {
			dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&req); err != nil && err != io.EOF {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace snapshot restore request")
				return
			}
		}
		payloadHash, _, hashErr := cloudWorkspacePayloadHash(struct {
			SnapshotID string `json:"snapshot_id"`
			Request    any    `json:"request"`
		}{SnapshotID: snapshotID, Request: req})
		if hashErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace snapshot restore request")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "snapshot:restore:" + key
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
		out, err := svc.RestoreSnapshot(r.Context(), *principal, id, snapshotID, req.IfMatchRevision)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(out); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// CloudWorkspaceGetObjectHandler GET /api/v1/cloud-workspaces/{id}/objects/{sha256}
func CloudWorkspaceGetObjectHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		sha, ok := requireCloudWorkspaceSHA256(w, r)
		if !ok {
			return
		}
		plain, err := svc.GetObject(r.Context(), *principal, id, sha)
		if err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		cloudworkspace.ObserveSyncBytesDown(int64(len(plain)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(plain)
	}
}

// CloudWorkspacePutObjectHandler PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}
func CloudWorkspacePutObjectHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		sha, ok := requireCloudWorkspaceSHA256(w, r)
		if !ok {
			return
		}
		body, err := readCloudWorkspacePlaintext(r, cloudworkspace.MaxObjectBytes)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		payloadHash := sha256.Sum256(append([]byte(sha+":"), body...))
		payloadHex := hex.EncodeToString(payloadHash[:])
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "object:put:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicJSON(http.StatusOK, cloudWorkspaceObjectResponse))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		got, err := svc.PutObject(r.Context(), *principal, id, sha, body)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		cloudworkspace.ObserveSyncBytesUp(int64(len(body)))
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(got); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeCloudWorkspaceObjectMeta(w, got)
	}
}

// CloudWorkspacePutObjectChunkHandler PUT /api/v1/cloud-workspaces/{id}/objects/{sha256}/chunks/{index}
func CloudWorkspacePutObjectChunkHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		sha, ok := requireCloudWorkspaceSHA256(w, r)
		if !ok {
			return
		}
		rawIndex := strings.TrimSpace(r.PathValue("index"))
		index, err := strconv.Atoi(rawIndex)
		if err != nil || index < 0 || strconv.Itoa(index) != rawIndex {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid chunk index")
			return
		}
		body, err := readCloudWorkspacePlaintext(r, cloudworkspace.MaxChunkBytes)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		payloadHash := sha256.Sum256(append([]byte(strconv.Itoa(index)+":"), body...))
		payloadHex := hex.EncodeToString(payloadHash[:])
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "object:chunk:" + key
		if key != "" && svc.Workspaces != nil {
			response := map[string]any{"sha256": sha, "index": index, "size": len(body)}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicStaticJSON(http.StatusOK, response))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		if err := svc.PutObjectChunk(r.Context(), *principal, id, sha, index, body); err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		cloudworkspace.ObserveSyncBytesUp(int64(len(body)))
		response := map[string]any{"sha256": sha, "index": index, "size": len(body)}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// CloudWorkspaceCompleteObjectHandler POST /api/v1/cloud-workspaces/{id}/objects/{sha256}/complete
func CloudWorkspaceCompleteObjectHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		sha, ok := requireCloudWorkspaceSHA256(w, r)
		if !ok {
			return
		}
		payloadHash := sha256.Sum256([]byte(sha))
		payloadHex := hex.EncodeToString(payloadHash[:])
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "object:complete:" + key
		if key != "" && svc.Workspaces != nil {
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, cloudWorkspaceAtomicJSON(http.StatusOK, cloudWorkspaceObjectResponse))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		got, err := svc.CompleteObject(r.Context(), *principal, id, sha)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex)
			}
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(got); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHex, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeCloudWorkspaceObjectMeta(w, got)
	}
}

func requireCloudWorkspaceSidecarName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name, err := cloudworkspace.ValidateSidecarName(strings.TrimSpace(r.PathValue("name")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return "", false
	}
	return name, true
}

// CloudWorkspaceGetSidecarHandler GET /api/v1/cloud-workspaces/{id}/sidecars/{name}
func CloudWorkspaceGetSidecarHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		name, ok := requireCloudWorkspaceSidecarName(w, r)
		if !ok {
			return
		}
		out, err := svc.GetSidecarWithRevision(r.Context(), *principal, id, name)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		if out == nil {
			out = &cloudworkspace.Sidecar{}
		}
		if out.Revision != "" {
			w.Header().Set("ETag", `"`+out.Revision+`"`)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out.Data)
	}
}

// CloudWorkspacePutSidecarHandler PUT /api/v1/cloud-workspaces/{id}/sidecars/{name}
func CloudWorkspacePutSidecarHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, id, ok := beginCloudWorkspaceSync(w, r, svc, identity)
		if !ok {
			return
		}
		name, ok := requireCloudWorkspaceSidecarName(w, r)
		if !ok {
			return
		}
		body, err := readCloudWorkspacePlaintext(r, cloudworkspace.MaxSidecarBytes)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		ifMatch := strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		ledgerKey := "sidecar:put:" + name + ":" + key
		payloadHash := ""
		if key != "" && svc.Workspaces != nil {
			sum := sha256.Sum256(append([]byte(ifMatch+":"), body...))
			payloadHash = hex.EncodeToString(sum[:])
			project := func(value any) any {
				sidecar, _ := value.(*cloudworkspace.Sidecar)
				revision := ""
				if sidecar != nil {
					revision = sidecar.Revision
				}
				return map[string]any{"name": name, "size": len(body), "revision": revision, "committed": true}
			}
			atomicRequest, replay, beginErr := prepareCloudWorkspaceAtomicRequest(r, svc.Workspaces, principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, cloudWorkspaceAtomicJSON(http.StatusOK, project))
			r = atomicRequest
			if beginErr != nil {
				writeCloudWorkspaceError(w, beginErr)
				return
			}
			if replay != nil && replay.Completed {
				if name == cloudworkspace.SidecarSession {
					w.Header().Set("Cache-Control", "no-store")
				}
				var replayResponse struct {
					Revision string `json:"revision"`
				}
				if json.Unmarshal(replay.Response, &replayResponse) == nil && replayResponse.Revision != "" {
					w.Header().Set("ETag", `"`+replayResponse.Revision+`"`)
				}
			}
			if replayCloudWorkspaceIdempotency(w, replay) {
				return
			}
		}
		out, err := svc.PutSidecarWithRevision(r.Context(), *principal, id, name, ifMatch, body)
		if err != nil {
			if key != "" && svc.Workspaces != nil {
				_ = svc.Workspaces.CancelIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash)
			}
			writeCloudWorkspaceError(w, err)
			return
		}
		if name == cloudworkspace.SidecarSession {
			w.Header().Set("Cache-Control", "no-store")
		}
		if out != nil && out.Revision != "" {
			w.Header().Set("ETag", `"`+out.Revision+`"`)
		}
		response := map[string]any{"name": name, "size": len(body), "revision": out.Revision, "committed": true}
		if key != "" && svc.Workspaces != nil {
			if raw, marshalErr := json.Marshal(response); marshalErr == nil {
				_ = svc.Workspaces.FinishIdempotencyForClient(r.Context(), principal.TenantID, principal.UserID, id, principal.ClientInstanceID, ledgerKey, payloadHash, http.StatusOK, raw, time.Now().UTC())
			}
		}
		writeJSON(w, http.StatusOK, response)
	}
}
