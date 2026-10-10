package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	transporthttp "github.com/RapidAI/CodeClaw/MaClawSrv/transport/http"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/botlog"
	"net/http"
	"strconv"
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
	releaseDesktopPerson(r.PathValue("instanceId"))
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

// instanceMessageTarget loads the instance for a message send. A missing or
// not-ready instance returns the error PostMessage would, before any caller
// opens the desktop. That open counts as a run, and the matching stop on the
// way out races the next send of the same command.
func instanceMessageTarget(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, instanceID string) (string, map[string]string, error) {
	instanceID = strings.TrimSpace(instanceID)
	if svc == nil {
		return instanceID, nil, nil
	}
	inst, err := svc.GetInstance(ctx, p, instanceID)
	if err != nil || inst == nil {
		return instanceID, nil, err
	}
	if id := strings.TrimSpace(inst.ID); id != "" {
		instanceID = id
	}
	if inst.Ready {
		return instanceID, inst.Metadata, nil
	}
	return instanceID, inst.Metadata, fmt.Errorf("instance is not ready: %s", inst.ReadyReason)
}

func (s *HTTPServer) messageTarget(r *http.Request, p agentservice.Principal) (string, map[string]string, error) {
	if r == nil {
		return "", nil, nil
	}
	svc := (*agentservice.Service)(nil)
	if s != nil {
		svc = s.svc
	}
	return instanceMessageTarget(r.Context(), svc, p, r.PathValue("instanceId"))
}

func (s *HTTPServer) handleSendMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SendMessageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	botID := strings.TrimSpace(in.ClientSessionKey)
	phase := ""
	if in.Metadata != nil {
		phase = strings.TrimSpace(in.Metadata["bot_phase"])
		if botID == "" {
			botID = strings.TrimSpace(in.Metadata["bot_id"])
		}
	}
	started := time.Now()
	status := http.StatusOK
	var callErr error
	var detail []string
	if botID != "" {
		botlog.Write(botID, "srv.http_begin", nil,
			"instance", r.PathValue("instanceId"),
			"phase", phase,
			"content_len", strconv.Itoa(len(in.Content)),
		)
		defer func() {
			fields := []string{
				"instance", r.PathValue("instanceId"),
				"phase", phase,
				"http_status", strconv.Itoa(status),
				"dur_ms", strconv.FormatInt(time.Since(started).Milliseconds(), 10),
			}
			fields = append(fields, detail...)
			botlog.Write(botID, "srv.http_end", callErr, fields...)
		}()
	}
	if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
		if in.ClientMessageID != "" && strings.TrimSpace(in.ClientMessageID) != key {
			status = http.StatusBadRequest
			callErr = errors.New("Idempotency-Key does not match client_message_id")
			writeJSON(w, status, map[string]string{"error": callErr.Error()})
			return
		}
		in.ClientMessageID = key
	}
	if isReservedCodingRuntimeMetadata(in.Metadata) || isReservedCodingRuntimeMetadata(in.SessionMetadata) {
		status = http.StatusBadRequest
		callErr = errors.New("coding runtime metadata must be created by an explicit workflow runtime endpoint")
		writeJSON(w, status, map[string]string{"error": callErr.Error()})
		return
	}
	wantsAsync, asyncErr := transporthttp.WantsAsyncResponse(r)
	if asyncErr != nil {
		status = http.StatusBadRequest
		callErr = asyncErr
		writeJSON(w, status, map[string]string{"error": asyncErr.Error()})
		return
	}
	instanceID, metadata, notReady := s.messageTarget(r, p)
	if notReady != nil {
		callErr = notReady
		status = errorStatusCode(notReady)
		if strings.Contains(notReady.Error(), "instance is not ready:") {
			detail = []string{"ready", "no"}
		}
		writeRedactedError(w, callErr, s.svc.DataRoot())
		return
	}
	desktopUserID, desktopTenantID := rememberDesktopOwner(instanceID, metadata, p.UserID, p.TenantID)
	shotID := strings.TrimSpace(r.PathValue("instanceId"))
	if wantsAsync {
		// Occupy, the screenshot, and the handoff live on the detached run.
		// Returning 202 from this request must not release the desktop.
		ownsRun := false
		in.ExecutionOwner = &ownsRun
		in.PrepareExecution, in.FinishExecution = desktopAsyncHooks(desktopTenantID, desktopUserID, shotID, botID, &ownsRun)
		sess, run, err := s.svc.SendMessageAsync(r.Context(), p, r.PathValue("instanceId"), in)
		if err != nil {
			callErr = err
			if run != nil {
				status = http.StatusAccepted
				detail = []string{"run", run.ID, "run_status", string(run.Status), "async", "true"}
				writeJSON(w, status, map[string]any{
					"async":      true,
					"session":    sess,
					"run":        sanitizeRunPtrForAPI(s.svc.DataRoot(), run),
					"status_url": fmt.Sprintf("/api/v1/instances/%s/runs/%s", r.PathValue("instanceId"), run.ID),
					"error":      redactSupportBundleText(s.svc.DataRoot(), err.Error()),
				})
				return
			}
			status = http.StatusBadGateway
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		status = http.StatusAccepted
		detail = []string{"run", run.ID, "run_status", string(run.Status), "async", "true"}
		statusURL := fmt.Sprintf("/api/v1/instances/%s/runs/%s", r.PathValue("instanceId"), run.ID)
		w.Header().Set("Preference-Applied", "respond-async")
		w.Header().Set("Location", statusURL)
		writeJSON(w, status, map[string]any{
			"async":      true,
			"session":    sess,
			"run":        sanitizeRunPtrForAPI(s.svc.DataRoot(), run),
			"status_url": statusURL,
		})
		return
	}
	releaseDesktop := occupyUserDesktop(r.Context(), desktopTenantID, desktopUserID, shotID)
	defer releaseDesktop()
	allowMessageResponseWrite(w)
	shotCtx, shot := withDesktopShotSlot(r.Context())
	sess, run, msg, err := s.svc.SendMessage(shotCtx, p, shotID, in)
	imageCount := 0
	fileCount := 0
	if err == nil {
		imageCount = attachDesktopShot(msg, shot)
		fileCount = attachDesktopFile(msg, shot)
	}
	handoff, attention := takeDesktopTurn(desktopTenantID, desktopUserID, shotID)
	if err != nil {
		callErr = err
		textLen := 0
		if msg != nil {
			textLen = len(msg.Content)
		}
		if run != nil {
			status = http.StatusBadGateway
			if run.Status == agentservice.RunStatusCancelled {
				status = http.StatusConflict
			}
			detail = []string{"run", run.ID, "run_status", string(run.Status), "text_len", strconv.Itoa(textLen), "images", strconv.Itoa(imageCount), "files", strconv.Itoa(fileCount), "handoff", strconv.FormatBool(handoff), "attention", attention}
			writeJSON(w, status, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "error": redactSupportBundleText(s.svc.DataRoot(), err.Error()), "desktop_handoff": handoff, "attention_reason": attention})
			return
		}
		status = http.StatusBadGateway
		detail = []string{"handoff", strconv.FormatBool(handoff), "attention", attention}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	textLen := 0
	if msg != nil {
		textLen = len(msg.Content)
	}
	runID := ""
	runStatus := ""
	if run != nil {
		runID = run.ID
		runStatus = string(run.Status)
	}
	detail = []string{"run", runID, "run_status", runStatus, "text_len", strconv.Itoa(textLen), "images", strconv.Itoa(imageCount), "files", strconv.Itoa(fileCount), "handoff", strconv.FormatBool(handoff), "attention", attention}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "desktop_handoff": handoff, "attention_reason": attention})
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
	instanceID, metadata, notReady := s.messageTarget(r, p)
	if notReady != nil {
		writeRedactedError(w, notReady, s.svc.DataRoot())
		return
	}
	desktopUserID, desktopTenantID := rememberDesktopOwner(instanceID, metadata, p.UserID, p.TenantID)
	shotID := strings.TrimSpace(r.PathValue("instanceId"))
	botID := ""
	if in.Metadata != nil {
		botID = strings.TrimSpace(in.Metadata["bot_id"])
	}
	if wantsAsync {
		ownsRun := false
		in.ExecutionOwner = &ownsRun
		in.PrepareExecution, in.FinishExecution = desktopAsyncHooks(desktopTenantID, desktopUserID, shotID, botID, &ownsRun)
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
	releaseDesktop := occupyUserDesktop(r.Context(), desktopTenantID, desktopUserID, shotID)
	defer releaseDesktop()
	allowMessageResponseWrite(w)
	shotCtx, shot := withDesktopShotSlot(r.Context())
	run, msg, err := s.svc.PostMessage(shotCtx, p, shotID, r.PathValue("sessionId"), in)
	if err == nil {
		publishDesktopReply(msg, shot)
	} else if msg != nil {
		msg.Content = correctDesktopPathClaim(msg.Content, desktopSavedPath(shot))
	}
	handoff, attention := takeDesktopTurn(desktopTenantID, desktopUserID, shotID)
	if err != nil {
		if run != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "error": redactSupportBundleText(s.svc.DataRoot(), err.Error()), "desktop_handoff": handoff, "attention_reason": attention})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "desktop_handoff": handoff, "attention_reason": attention})
}

func (s *HTTPServer) handleGetRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, desktopRunAPIBody(s.svc.DataRoot(), out))
}

// desktopAsyncHooks hold the desktop for the detached run. The HTTP handler
// returns as soon as the run exists. Finish stores the screenshot and the
// handoff so a later read of the run can deliver them.
func desktopAsyncHooks(tenantID, userID, instanceID, botID string, owns *bool) (
	func(context.Context) context.Context,
	func(context.Context, *agentservice.Run, *agentservice.Message, error),
) {
	var release func()
	var occupyOwned *bool
	var shot *desktopShotSlot
	prepare := func(ctx context.Context) context.Context {
		release, occupyOwned = occupyUserDesktopRun(ctx, tenantID, userID, instanceID)
		next, slot := withDesktopShotSlot(ctx)
		shot = slot
		botlog.Write(botID, "srv.desktop_prepare", nil, "instance", instanceID)
		return next
	}
	finish := func(_ context.Context, run *agentservice.Run, msg *agentservice.Message, err error) {
		owner := owns == nil || *owns
		if occupyOwned != nil {
			*occupyOwned = owner
		}
		defer func() {
			if release != nil {
				release()
			}
		}()
		// A repeated Idempotency-Key still occupies, then returns the run
		// that is already executing. Release that extra count here. Taking
		// the handoff or storing a result would publish the other run early.
		if !owner {
			runID := ""
			runStatus := ""
			if run != nil {
				runID = run.ID
				runStatus = string(run.Status)
			}
			botlog.Write(botID, "srv.desktop_finish", err,
				"instance", instanceID,
				"run", runID,
				"status", runStatus,
				"replay", "yes",
			)
			return
		}
		images := 0
		files := 0
		if err == nil {
			images, files = publishDesktopReply(msg, shot)
		} else if msg != nil {
			msg.Content = correctDesktopPathClaim(msg.Content, desktopSavedPath(shot))
		}
		handoff, attention := takeDesktopTurn(tenantID, userID, instanceID)
		storeDesktopTurnResult(run, msg, handoff, attention)
		runID := ""
		runStatus := ""
		if run != nil {
			runID = run.ID
			runStatus = string(run.Status)
		}
		textLen := 0
		if msg != nil {
			textLen = len(msg.Content)
		}
		botlog.Write(botID, "srv.desktop_finish", err,
			"instance", instanceID,
			"run", runID,
			"status", runStatus,
			"text_len", strconv.Itoa(textLen),
			"images", strconv.Itoa(images),
			"files", strconv.Itoa(files),
			"handoff", strconv.FormatBool(handoff),
			"attention", attention,
		)
	}
	return prepare, finish
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
