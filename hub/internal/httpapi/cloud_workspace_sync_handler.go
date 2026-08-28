package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

const cloudWorkspaceManifestJSONLimit = 16 << 20

func beginCloudWorkspaceSync(w http.ResponseWriter, r *http.Request, svc *cloudworkspace.Service, identity veMachineAuthenticator) (*auth.MachinePrincipal, string, bool) {
	principal, ok := authenticateCloudWorkspaceMachine(w, r, identity)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"sha256":  got.SHA256,
		"size":    got.SizeBytes,
		"existed": got.Existed,
	})
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
		out, err := svc.PutManifest(r.Context(), *principal, id, req.IfMatchRevision, req.Entries)
		if err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
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
		got, err := svc.PutObject(r.Context(), *principal, id, sha, body)
		if err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		cloudworkspace.ObserveSyncBytesUp(int64(len(body)))
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
		if err := svc.PutObjectChunk(r.Context(), *principal, id, sha, index, body); err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
		}
		cloudworkspace.ObserveSyncBytesUp(int64(len(body)))
		writeJSON(w, http.StatusOK, map[string]any{"sha256": sha, "index": index, "size": len(body)})
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
		got, err := svc.CompleteObject(r.Context(), *principal, id, sha)
		if err != nil {
			writeCloudWorkspaceSyncError(w, r, svc, principal.TenantID, id, err)
			return
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
		plain, err := svc.GetSidecar(r.Context(), *principal, id, name)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(plain)
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
		if err := svc.PutSidecar(r.Context(), *principal, id, name, body); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "size": len(body)})
	}
}
