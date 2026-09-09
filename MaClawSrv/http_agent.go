package main

import (
	"encoding/json"
	"errors"
	"fmt"
	transporthttp "github.com/RapidAI/CodeClaw/MaClawSrv/transport/http"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"net/http"
	"strings"
	"time"
)

func (s *HTTPServer) handleListInstances(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListInstances(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateInstances(sanitizeInstancesForAPI(s.svc.DataRoot(), out), page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateInstanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateInstance(r.Context(), p, in)
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			validation, vErr := s.svc.ValidateUserConfig(r.Context(), p)
			if vErr == nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error()), "config_validation": sanitizeConfigValidationPtrForAPI(s.svc.DataRoot(), validation)})
				return
			}
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleUpdateInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateInstanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateInstance(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleDeleteInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteInstance(r.Context(), p, r.PathValue("instanceId")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleGetInstanceCapabilities(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceCapabilities(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeAgentCapabilitiesForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetInstanceRuntimeCapabilities(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.DescribeRuntimeCapabilities(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleStopInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.StopInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleResumeInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ResumeInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) && out != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error()), "instance": sanitizeInstanceForAPI(s.svc.DataRoot(), out), "config_validation": sanitizeConfigValidationForAPI(s.svc.DataRoot(), out.ConfigValidation)})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleRefreshInstanceReadiness(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.RefreshInstanceReadiness(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetInstanceSummary(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceSummary(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceSummaryForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleGetInstanceBootstrap(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceBootstrap(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeInstanceBootstrapForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleSendMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SendMessageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		if in.ClientMessageID != "" && strings.TrimSpace(in.ClientMessageID) != key {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key does not match client_message_id"})
			return
		}
		in.ClientMessageID = key
	}
	if isReservedCodingRuntimeMetadata(in.Metadata) || isReservedCodingRuntimeMetadata(in.SessionMetadata) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "coding runtime metadata must be created by an explicit workflow runtime endpoint"})
		return
	}
	wantsAsync, asyncErr := transporthttp.WantsAsyncResponse(r)
	if asyncErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": asyncErr.Error()})
		return
	}
	if wantsAsync {
		sess, run, err := s.svc.SendMessageAsync(r.Context(), p, r.PathValue("instanceId"), in)
		if err != nil {
			if run != nil {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"async":      true,
					"session":    sess,
					"run":        sanitizeRunPtrForAPI(s.svc.DataRoot(), run),
					"status_url": fmt.Sprintf("/api/v1/instances/%s/runs/%s", r.PathValue("instanceId"), run.ID),
					"error":      redactSupportBundleText(s.svc.DataRoot(), err.Error()),
				})
				return
			}
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		statusURL := fmt.Sprintf("/api/v1/instances/%s/runs/%s", r.PathValue("instanceId"), run.ID)
		w.Header().Set("Preference-Applied", "respond-async")
		w.Header().Set("Location", statusURL)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"async":      true,
			"session":    sess,
			"run":        sanitizeRunPtrForAPI(s.svc.DataRoot(), run),
			"status_url": statusURL,
		})
		return
	}
	sess, run, msg, err := s.svc.SendMessage(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		if run != nil {
			status := http.StatusBadGateway
			if run.Status == agentservice.RunStatusCancelled {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg})
}

func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	includeArchived, err := parseOptionalBoolQuery(r, "include_archived")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListSessions(r.Context(), p, r.PathValue("instanceId"), agentservice.ListSessionsInput{
		IncludeArchived: includeArchived != nil && *includeArchived,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateSessions(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateSession(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDeleteSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId")); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleArchiveSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ArchiveSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleRestoreSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.RestoreSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleListMessages(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	role, ok := parseMessageRole(r.URL.Query().Get("role"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}
	out, err := s.svc.ListMessages(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), agentservice.ListMessagesInput{
		Role:  role,
		Since: since,
		Until: until,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateMessages(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handlePostMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.PostMessageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		if in.Metadata != nil && strings.TrimSpace(in.Metadata["client_message_id"]) != "" && strings.TrimSpace(in.Metadata["client_message_id"]) != key {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key does not match client_message_id"})
			return
		}
		if in.Metadata == nil {
			in.Metadata = map[string]string{}
		}
		in.Metadata["client_message_id"] = key
	}
	if isReservedCodingRuntimeMetadata(in.Metadata) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "coding runtime metadata must be created by an explicit workflow runtime endpoint"})
		return
	}
	wantsAsync, asyncErr := transporthttp.WantsAsyncResponse(r)
	if asyncErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": asyncErr.Error()})
		return
	}
	if wantsAsync {
		run, err := s.svc.PostMessageAsync(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), in)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		statusURL := fmt.Sprintf("/api/v1/instances/%s/runs/%s", r.PathValue("instanceId"), run.ID)
		w.Header().Set("Preference-Applied", "respond-async")
		w.Header().Set("Location", statusURL)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"async":      true,
			"run":        sanitizeRunPtrForAPI(s.svc.DataRoot(), run),
			"status_url": statusURL,
		})
		return
	}
	run, msg, err := s.svc.PostMessage(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), in)
	if err != nil {
		if run != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg})
}

func (s *HTTPServer) handleGetRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRunPtrForAPI(s.svc.DataRoot(), out))
}

func (s *HTTPServer) handleStreamRunEvents(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	instanceID := r.PathValue("instanceId")
	runID := r.PathValue("runId")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastPayload := ""
	sendSnapshot := func(eventType string, snap *runStreamSnapshot) bool {
		payload, err := json.Marshal(runStreamEnvelope{Type: eventType, Snapshot: snap})
		if err != nil {
			return false
		}
		if eventType == "snapshot" && string(payload) == lastPayload {
			return true
		}
		if eventType == "snapshot" {
			lastPayload = string(payload)
		}
		if _, err := w.Write([]byte("event: " + eventType + "\n")); err != nil {
			return false
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	snapshot, err := s.loadRunStreamSnapshot(r.Context(), p, instanceID, runID)
	if err != nil {
		writeSSEError(w, flusher, err, s.svc.DataRoot())
		return
	}
	if !sendSnapshot("snapshot", snapshot) {
		return
	}
	lastEventSequence := uint64(0)
	if rawLastID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); rawLastID != "" {
		// Numeric ids are accepted for stores that expose sequence ids directly;
		// normal event ids are resolved through the optional durable-store index.
		parsed, opaqueID := transporthttp.ParseLastEventID(rawLastID)
		if opaqueID == "" {
			lastEventSequence = parsed
		} else if resolved, found, resolveErr := s.svc.RunEventSequenceForID(r.Context(), p, runID, opaqueID); resolveErr == nil && found {
			lastEventSequence = resolved
		}
	}
	sendRunEvents := func() bool {
		events, err := s.svc.ListRunEventsForInstance(r.Context(), p, instanceID, runID, lastEventSequence, 100)
		if err != nil {
			return true // event outbox is optional for legacy/custom stores
		}
		for i := range events {
			event := sanitizeRunEventForAPI(s.svc.DataRoot(), events[i])
			if event.Sequence > lastEventSequence {
				lastEventSequence = event.Sequence
			}
			payload, marshalErr := json.Marshal(runStreamEnvelope{Type: event.Type, Event: &event})
			if marshalErr != nil {
				continue
			}
			if writeErr := transporthttp.WriteSSEEvent(w, flusher, event.ID, event.Type, payload); writeErr != nil {
				return false
			}
		}
		return true
	}
	if !sendRunEvents() {
		return
	}
	if snapshot.Run != nil && snapshot.Run.Status != agentservice.RunStatusRunning {
		_ = sendSnapshot("done", snapshot)
		return
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !sendRunEvents() {
				return
			}
			snapshot, err := s.loadRunStreamSnapshot(r.Context(), p, instanceID, runID)
			if err != nil {
				writeSSEError(w, flusher, err, s.svc.DataRoot())
				return
			}
			if !sendSnapshot("snapshot", snapshot) {
				return
			}
			if snapshot.Run != nil && snapshot.Run.Status != agentservice.RunStatusRunning {
				_ = sendSnapshot("done", snapshot)
				return
			}
		}
	}
}

func (s *HTTPServer) handleListRuns(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	waitingForUser, err := parseOptionalBoolQuery(r, "waiting_for_user")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status, ok := parseRunStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	responseSource, ok := parseRunResponseSource(r.URL.Query().Get("response_source"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid response_source"})
		return
	}
	out, err := s.svc.ListRuns(r.Context(), p, r.PathValue("instanceId"), agentservice.ListRunsInput{
		Status:         status,
		SessionID:      strings.TrimSpace(r.URL.Query().Get("session_id")),
		ResponseSource: responseSource,
		WaitingForUser: waitingForUser,
	})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateRuns(out, page)
	writeJSON(w, http.StatusOK, listResponse(sanitizeRunsForAPI(s.svc.DataRoot(), items), meta))
}

func (s *HTTPServer) handleCancelRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.CancelRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, sanitizeRunPtrForAPI(s.svc.DataRoot(), out))
}
