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

type HardwareMeetingResultNotifier interface {
	EnqueueReply(clientID, conversationID string, reply map[string]any)
}

var hardwareMeetingResults HardwareMeetingResultNotifier

// SetHardwareMeetingResultNotifier connects terminal Mobile-library processing
// state to the originating hardware queue. It is optional for mobile-only Hub
// deployments and is wired to DeviceGateway during bootstrap.
func SetHardwareMeetingResultNotifier(notifier HardwareMeetingResultNotifier) {
	hardwareMeetingResults = notifier
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
		tenantID, userID, clientID, ok := devices.AuthenticatedDeviceOwner(r)
		if !ok || strings.TrimSpace(userID) == "" || strings.TrimSpace(clientID) == "" {
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
			mobileMeetingRecordingCreateForHardware(w, r, principal, ownerID, clientID)
		case r.Method == http.MethodGet && len(parts) == 1:
			if !hardwareMeetingRecordingOwned(w, ownerID, tenantID, clientID, parts[0]) {
				return
			}
			mobileMeetingRecordingGet(w, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPut && len(parts) == 3 && parts[1] == "chunks":
			if !hardwareMeetingRecordingOwned(w, ownerID, tenantID, clientID, parts[0]) {
				return
			}
			mobileMeetingRecordingPutChunk(w, r, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "complete":
			if !hardwareMeetingRecordingOwned(w, ownerID, tenantID, clientID, parts[0]) {
				return
			}
			mobileMeetingRecordingComplete(w, r, ownerID, tenantID, parts[0])
		case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "process":
			if !hardwareMeetingRecordingOwned(w, ownerID, tenantID, clientID, parts[0]) {
				return
			}
			mobileMeetingRecordingProcess(w, r, principal, parts[0])
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported hardware meeting recording operation")
		}
	})
}

// hardwareMeetingRecordingOwned keeps recordings isolated between multiple
// paired devices belonging to the same account. Mobile ownership alone checks
// tenant and user, while a hardware credential must also match the client that
// originally created the recording. Return 404 so recording IDs cannot be used
// to probe another device's library objects.
func hardwareMeetingRecordingOwned(w http.ResponseWriter, ownerID, tenantID, clientID, recordingID string) bool {
	rec, ok := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, strings.TrimSpace(recordingID))
	if !ok || strings.TrimSpace(rec.HardwareClientID) == "" || strings.TrimSpace(rec.HardwareClientID) != strings.TrimSpace(clientID) {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return false
	}
	return true
}
