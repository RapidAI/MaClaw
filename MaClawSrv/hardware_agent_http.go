package main

import (
	"errors"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

func (s *HTTPServer) handleListHardwareDevices(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	fallback := ""
	if cfg, err := s.svc.GetUserConfig(r.Context(), p); err == nil && cfg != nil {
		fallback = cfg.AppConfig.TTSVoiceID
	}
	bindings := s.hardwareBindings.list(p)
	items := make([]srvHardwareDeviceView, 0, len(bindings))
	for _, binding := range bindings {
		items = append(items, s.hardwareBindings.view(p, binding, fallback))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleGetHardwareDevice(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	binding, ok := s.hardwareBindingForRequest(p, r.PathValue("deviceId"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "hardware device not found"})
		return
	}
	fallback := ""
	if cfg, err := s.svc.GetUserConfig(r.Context(), p); err == nil && cfg != nil {
		fallback = cfg.AppConfig.TTSVoiceID
	}
	writeJSON(w, http.StatusOK, s.hardwareBindings.view(p, binding, fallback))
}

func (s *HTTPServer) handleUpdateHardwareDeviceBinding(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	clientID := r.PathValue("deviceId")
	if _, ok := s.hardwareBindingForRequest(p, clientID); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "hardware device not found"})
		return
	}
	var in srvHardwareBindingUpdate
	if !decodeJSON(w, r, &in) {
		return
	}
	binding, err := s.hardwareBindings.update(p, clientID, in)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "another client") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	if err := s.hardwareBindings.syncBindingInstancePolicy(r.Context(), s.svc, p, binding); err != nil && !errors.Is(err, agentservice.ErrInstanceNotFound) {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	fallback := ""
	if cfg, err := s.svc.GetUserConfig(r.Context(), p); err == nil && cfg != nil {
		fallback = cfg.AppConfig.TTSVoiceID
	}
	writeJSON(w, http.StatusOK, s.hardwareBindings.view(p, binding, fallback))
}

func (s *HTTPServer) handleDeleteHardwareDevice(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	deviceID := r.PathValue("deviceId")
	binding, found, err := s.hardwareBindings.delete(p, deviceID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "hardware device not found"})
		return
	}
	if s.thirdPartyIM != nil {
		s.thirdPartyIM.stopDeviceClient(p, binding.ClientID)
	}
	if err := s.unbindHardwareDevice(p, deviceID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if strings.TrimSpace(binding.InstanceID) != "" {
		_, _ = s.svc.StopInstance(r.Context(), p, binding.InstanceID)
		if err := s.svc.DeleteInstance(r.Context(), p, binding.InstanceID); err != nil && !errors.Is(err, agentservice.ErrInstanceNotFound) {
			if errors.Is(err, agentservice.ErrInstanceBusy) {
				writeJSON(w, http.StatusAccepted, map[string]string{"status": "revoked_cleanup_pending"})
				return
			}
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleListHardwareTTSVoices(w http.ResponseWriter, r *http.Request, _ agentservice.Principal) {
	items := make([]map[string]string, 0, len(tts.SupportedTTSVoiceIDs))
	for _, id := range tts.SupportedTTSVoiceIDs {
		items = append(items, map[string]string{"id": id, "name": srvHardwareVoiceDisplayName(id)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "default": tts.DefaultTTSVoiceID})
}

func srvHardwareVoiceDisplayName(id string) string {
	switch id {
	case "zm_yunxi":
		return "云希"
	case "zm_yunyang":
		return "云扬"
	case "zf_xiaoxiao":
		return "晓晓"
	case "zf_xiaoyi":
		return "晓伊"
	case "am_adam":
		return "Adam"
	case "af_heart":
		return "Heart"
	default:
		return id
	}
}

func (s *HTTPServer) handleListHardwareExperts(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.hardwareBindings.listExperts(p)})
}

func (s *HTTPServer) handleUpsertHardwareExpert(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	var in srvHardwareExpert
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.hardwareBindings.upsertExpert(p, in)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDeleteHardwareExpert(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if s.hardwareBindings == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "hardware device management is unavailable"})
		return
	}
	found, err := s.hardwareBindings.deleteExpert(p, r.PathValue("expertId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "AI expert not found"})
		return
	}
	// Bound instances are intentionally not silently changed.  On the next
	// turn instanceMetadata cannot resolve this snapshot and the device enters
	// degraded state, rather than falling back to the general assistant.
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) hardwareBindingForRequest(p agentservice.Principal, deviceID string) (srvDeviceAgentBinding, bool) {
	if s == nil || s.hardwareBindings == nil {
		return srvDeviceAgentBinding{}, false
	}
	return s.hardwareBindings.get(p, strings.TrimSpace(deviceID))
}
