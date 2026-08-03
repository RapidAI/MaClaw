package httpapi

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

const hardwareMeetingRecordingBasePath = "/api/device-gateway/v1/meeting-recordings"

type HardwareDeviceOwnerAuthenticator interface {
	AuthenticatedDeviceOwner(*http.Request) (tenantID, userID, clientID string, ok bool)
}

// HardwareMeetingRecordingWorkerAvailability is used by the device handshake
// so small clients never offer a processing mode the Hub cannot execute.
func HardwareMeetingRecordingWorkerAvailability() (transcript, minutes bool) {
	return mobileMeetingRecordingWorkerAvailability()
}

// HardwareMeetingRecordingsHandler exposes the same durable recording objects
// used by mobile. Hardware recordings therefore appear in the mobile/GUI
// library and share the completion, ASR, minutes and retention pipeline.
func HardwareMeetingRecordingsHandler(devices HardwareDeviceOwnerAuthenticator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if devices == nil {
			writeError(w, http.StatusServiceUnavailable, "MEETING_RECORDING_UNAVAILABLE", "hardware meeting recording is unavailable")
			return
		}
		tenantID, userID, _, ok := devices.AuthenticatedDeviceOwner(r)
		if !ok || strings.TrimSpace(userID) == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Device authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		principal := &auth.ViewerPrincipal{TenantID: tenantID, UserID: userID}
		ownerID := mobilePrincipalOwnerID(principal)
		relative := strings.Trim(strings.TrimPrefix(r.URL.Path, hardwareMeetingRecordingBasePath), "/")
		parts := []string{}
		if relative != "" {
			parts = strings.Split(relative, "/")
		}
		if len(parts) > 0 {
			r.SetPathValue("recordingId", strings.TrimSpace(parts[0]))
		}
		if len(parts) == 3 && parts[1] == "chunks" {
			r.SetPathValue("chunkIndex", strings.TrimSpace(parts[2]))
		}
		switch {
		case r.Method == http.MethodPost && len(parts) == 0:
			mobileMeetingRecordingCreate(w, r, principal, ownerID)
		case r.Method == http.MethodGet && len(parts) == 1:
			mobileMeetingRecordingGet(w, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "chunks":
			mobileMeetingRecordingPutChunk(w, r, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "complete":
			mobileMeetingRecordingComplete(w, r, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "process":
			mobileMeetingRecordingProcess(w, r, ownerID, tenantID, parts[0])
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported hardware meeting recording operation")
		}
	})
}
