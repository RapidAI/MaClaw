package main

import (
	"fmt"
	"strings"
	"time"
)

func (h *IMMessageHandler) StartDesktopBackgroundTask(text, projectPath string) (*AIAssistantBackgroundTaskResult, error) {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return &AIAssistantBackgroundTaskResult{
			Accepted: false,
			Mode:     "background",
			Error:    "empty task text",
		}, nil
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	ownerID := projectSessionOwnerID(projectPath)
	if h != nil && h.app != nil {
		if err := h.app.ensureWorkflowAllowsRemoteToolCallForOwner(ownerID, "delegate_task", map[string]interface{}{"agent": "background", "request": trimmedText, "project_path": projectPath}); err != nil {
			return nil, err
		}
	}
	if h.manager == nil {
		return nil, fmt.Errorf("remote session manager not initialized")
	}
	loopCtx := NewLoopContext(fmt.Sprintf("ai-bg-%d", time.Now().UnixNano()), h.getMaclawAgentMaxIterations(), h.taskClient)
	loopCtx.Platform = "desktop"
	loopCtx.UserID = ownerID
	loopCtx.Runtime.PolicyOwnerID = ownerID
	if h.traceService != nil {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, trimmedText, desktopPlatform, ownerID, projectPath)
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI background task accepted", truncateTraceText(trimmedText, 180), "", "")
	}
	title := truncateRunes(trimmedText, 72)
	session := h.manager.CreateAIBackgroundSession(title, projectPath, loopCtx)
	if session == nil {
		return nil, fmt.Errorf("failed to create background AI session")
	}
	if h.traceService != nil && loopCtx.RunID != "" {
		h.traceService.SetRunSessionID(loopCtx.RunID, session.ID)
	}
	go h.runDesktopBackgroundTask(session.ID, loopCtx, ownerID, trimmedText)
	return &AIAssistantBackgroundTaskResult{
		Accepted:  true,
		Mode:      "background",
		SessionID: session.ID,
		JobID:     session.JobID,
		RunID:     session.RunID,
	}, nil
}

func (h *IMMessageHandler) runDesktopBackgroundTask(sessionID string, loopCtx *LoopContext, ownerID, text string) {
	msg := IMUserMessage{
		UserID:             ownerID,
		Platform:           desktopPlatform,
		Text:               text,
		IsBackground:       true,
		Lang:               "zh",
		MinIterations:      h.getMaclawAgentMaxIterations(),
		BackgroundSlotKind: "scheduled",
	}
	onProgress := func(progressText string) {
		if progressText == "" || progressText == imHeartbeatMsg || h.manager == nil {
			return
		}
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionBusy
			s.Summary.Status = SessionBusy.String()
			s.Summary.WaitingForUser = false
			s.Summary.ProgressSummary = progressText
			s.Summary.CurrentTask = firstNonEmptyTraceText(progressText, s.Title)
		})
		h.manager.AppendBackgroundAIOutput(sessionID, progressText)
	}
	resp := h.HandleIMMessageWithExistingLoop(msg, loopCtx, onProgress, nil, nil, nil)
	if h.manager == nil {
		return
	}
	if resp != nil && resp.Error != "" {
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionError
			s.Summary.Status = SessionError.String()
			s.Summary.Severity = "error"
			s.Summary.LastResult = resp.Error
			s.Summary.ProgressSummary = firstNonEmptyTraceText(resp.Error, s.Summary.ProgressSummary)
		})
		h.manager.AddBackgroundAIEvent(sessionID, ImportantEvent{
			Type:     "ai.background.error",
			Severity: "error",
			Title:    "AI background task failed",
			Summary:  truncateTraceText(resp.Error, 220),
		})
		return
	}
	if loopCtx.IsCancelled() {
		h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
			s.Status = SessionExited
			s.Summary.Status = SessionExited.String()
			s.Summary.Severity = "warn"
			s.Summary.LastResult = "Canceled"
			s.Summary.ProgressSummary = "Task canceled"
		})
		h.manager.AddBackgroundAIEvent(sessionID, ImportantEvent{
			Type:     "ai.background.canceled",
			Severity: "warn",
			Title:    "AI background task canceled",
			Summary:  truncateTraceText(text, 180),
		})
		return
	}
	resultText := ""
	if resp != nil {
		resultText = firstNonEmptyTraceText(resp.Text, resp.TraceSummary)
	}
	h.manager.UpdateBackgroundAISummary(sessionID, func(s *RemoteSession) {
		s.Status = SessionExited
		s.Summary.Status = SessionExited.String()
		s.Summary.Severity = "info"
		s.Summary.WaitingForUser = false
		s.Summary.ProgressSummary = firstNonEmptyTraceText(resultText, "Task completed")
		s.Summary.LastResult = resultText
	})
	if resultText != "" {
		h.manager.AppendBackgroundAIOutput(sessionID, resultText)
	}
	h.manager.AddBackgroundAIEvent(sessionID, ImportantEvent{
		Type:     "ai.background.completed",
		Severity: "info",
		Title:    "AI background task completed",
		Summary:  truncateTraceText(firstNonEmptyTraceText(resultText, text), 220),
	})
}
