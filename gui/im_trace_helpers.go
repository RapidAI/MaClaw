package main

import "strings"

func (h *IMMessageHandler) appendTraceEvent(ctx *LoopContext, kind, severity, title, summary, relatedFile, command string) {
	h.appendTraceEventWithToolOutcomes(ctx, TraceEvent{
		Kind:        kind,
		Severity:    severity,
		Title:       title,
		Summary:     summary,
		RelatedFile: relatedFile,
		Command:     command,
	})
}

func (h *IMMessageHandler) runtimeTraceSummary(ctx *LoopContext, text string) string {
	if ctx == nil || strings.TrimSpace(ctx.Runtime.RequestID) == "" {
		return truncateTraceText(text, 180)
	}
	rt := ctx.Runtime
	parts := []string{
		"request_id=" + rt.RequestID,
		"source=" + strings.TrimSpace(rt.Source.Channel) + "/" + strings.TrimSpace(rt.Source.Provider),
		"actor=" + strings.TrimSpace(rt.Actor.ActorID),
		"session_key=" + strings.TrimSpace(rt.Conversation.SessionKey),
		"lock_key=" + strings.TrimSpace(rt.LockKey),
	}
	if policyOwner := strings.TrimSpace(rt.PolicyOwnerID); policyOwner != "" {
		parts = append(parts, "policy_owner="+policyOwner)
	}
	if text = strings.TrimSpace(text); text != "" {
		parts = append(parts, "text="+truncateTraceText(text, 120))
	}
	return truncateTraceText(strings.Join(parts, " "), 400)
}

func (h *IMMessageHandler) appendTraceEventWithToolOutcomes(ctx *LoopContext, event TraceEvent) {
	if ctx == nil || h.traceService == nil || ctx.RunID == "" {
		return
	}
	event.Title = firstNonEmptyTraceText(event.Title, event.Kind)
	event.ProjectPath = h.traceProjectPath()
	event.CreatedAt = traceNowMillis()
	h.traceService.AppendEvent(ctx.RunID, event)
}

func (h *IMMessageHandler) appendTraceEvidence(ctx *LoopContext, sourceKind, category, summary, snippet, relatedFile, command string) {
	if ctx == nil || h.traceService == nil || ctx.RunID == "" {
		return
	}
	h.traceService.AppendEvidence(ctx.RunID, EvidenceRecord{
		SourceKind:     sourceKind,
		Category:       category,
		Summary:        summary,
		ContentSnippet: snippet,
		RelatedFile:    relatedFile,
		Command:        command,
		ProjectPath:    h.traceProjectPath(),
		CreatedAt:      traceNowMillis(),
	})
}

func shouldPersistTrialReflectSummary(summary *TrialReflectSummary) bool {
	if summary == nil {
		return false
	}
	if summary.FailureCount > 0 || summary.Recovered {
		return true
	}
	for _, category := range summary.FailureCategories {
		if strings.TrimSpace(category) != "" {
			return true
		}
	}
	return summary.RepeatGuard
}

func (h *IMMessageHandler) traceProjectPath() string {
	resolver := h.traceContextResolver()
	if resolver == nil {
		return ""
	}
	projectPath, _ := resolver.ResolveProject()
	return strings.TrimSpace(projectPath)
}

func (h *IMMessageHandler) buildTraceEvidencePrompt(userID, userMessage string) string {
	if h == nil || h.traceService == nil || h.memory == nil {
		return ""
	}
	if h.memory.ActiveUnfinishedSlot(userID) == nil {
		return ""
	}
	projectPath := h.traceProjectPath()
	evidence := h.traceService.RecallEvidence(projectPath, userMessage, 3)
	if len(evidence) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 最近执行证据\n")
	for _, item := range evidence {
		line := ""
		if normalizeTraceSourceKind(item.SourceKind) == traceSourceKindTrialReflectSummary {
			line = firstNonEmptyTraceText(item.ContentSnippet, item.Summary)
		} else {
			line = firstNonEmptyTraceText(item.Summary, item.ContentSnippet)
		}
		if line == "" {
			continue
		}
		if item.RelatedFile != "" {
			line += " [file=" + item.RelatedFile + "]"
		}
		if item.Command != "" {
			line += " [cmd=" + item.Command + "]"
		}
		b.WriteString("- ")
		b.WriteString(truncateTraceText(line, 220))
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) buildResumeTraceContext(userID, fallbackTask string) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	return h.buildResumeTraceContextWithLang(userID, fallbackTask, lang)
}

func (h *IMMessageHandler) buildResumeTraceContextWithLang(userID, fallbackTask, lang string) string {
	if h == nil || h.memory == nil {
		return ""
	}
	if activeSlot := h.memory.ActiveUnfinishedSlot(userID); activeSlot != nil {
		return buildUnfinishedSlotResumeContextWithLang(activeSlot, lang) + h.buildTraceEvidencePrompt(userID, activeSlot.LastTask)
	}
	return h.buildTraceEvidencePrompt(userID, fallbackTask)
}

func traceCategoryForToolExecution(execResult toolExecutionResult) traceEvidenceCategory {
	return execResult.ToolKind.TraceCategory(execResult)
}

func (h *IMMessageHandler) linkTraceToLatestAISession(ctx *LoopContext, result string) string {
	if ctx == nil || h.traceService == nil || h.manager == nil {
		return ""
	}
	sessionID := ""
	if start := strings.Index(result, "["); start >= 0 {
		if end := strings.Index(result[start:], "]"); end > 1 {
			sessionID = strings.TrimSpace(result[start+1 : start+end])
		}
	}
	if sessionID == "" {
		return ""
	}
	session, ok := h.manager.Get(sessionID)
	if !ok || session == nil || session.RunID == "" {
		return ""
	}
	h.traceService.LinkRuns(ctx.RunID, session.RunID)
	return session.RunID
}

func (h *IMMessageHandler) runTraceStatus(ctx *LoopContext, result *IMAgentResponse) TraceRunStatus {
	if ctx == nil {
		return TraceRunStatusRunning
	}
	if result != nil && result.Error != "" {
		return TraceRunStatusFailed
	}
	status := traceStatusFromLoopStateKind(ctx.LoopState())
	switch status {
	case TraceRunStatusCompleted:
		return TraceRunStatusCompleted
	case TraceRunStatusFailed:
		return TraceRunStatusFailed
	case TraceRunStatusStopped:
		return TraceRunStatusCancelled
	case TraceRunStatusTimeout:
		return TraceRunStatusTimeout
	case TraceRunStatusPaused:
		return TraceRunStatusPaused
	default:
		return status
	}
}

func (h *IMMessageHandler) finalizeTraceResult(ctx *LoopContext, resp *IMAgentResponse, summary, errText string) *IMAgentResponse {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	attachRuntimeResponseFields(ctx, resp)
	if ctx == nil || h.traceService == nil || ctx.RunID == "" {
		return resp
	}
	status := h.runTraceStatus(ctx, resp)
	h.traceService.UpdateRun(ctx.RunID, status, summary, errText)
	resp.JobID = ctx.JobID
	resp.RunID = ctx.RunID
	resp.TraceStatus = string(status)
	resp.TraceSummary = h.traceService.TraceSummary(ctx.RunID)
	if view, ok := h.traceService.GetTrace(ctx.RunID); ok && view.TrialReflectSummary != nil {
		resp.TrialReflectSummary = view.TrialReflectSummary.StrategyNote
		resp.TrialReflectStatus = view.TrialReflectSummary.FinalOutcome
		resp.TrialReflectFailures = view.TrialReflectSummary.FailureCount
		if shouldPersistTrialReflectSummary(view.TrialReflectSummary) {
			h.appendTraceEvidence(ctx, traceSourceKindTrialReflectSummary.String(), traceEvidenceCategoryDecision.String(), "trial-reflect summary", view.TrialReflectSummary.StrategyNote, "", "")
			resp.TraceSummary = h.traceService.TraceSummary(ctx.RunID)
		}
	}
	resp.TraceEventCount, resp.EvidenceCount = h.traceService.TraceCounts(ctx.RunID)
	if !hasVisibleIMResult(resp) {
		if resp.ConfirmedResume {
			resp.Text = buildConfirmedResumeEmptyResultFallback(status, resp.TraceSummary)
		} else {
			resp.Text = buildEmptyResultFallback(status, resp.TraceSummary)
		}
		ensureTraceAction(resp)
	}
	if browserRootCause := extractBrowserRootCause(firstNonEmptyTraceText(resp.Error, resp.Text)); browserRootCause != "" {
		resp.Fields = append(resp.Fields, IMResponseField{Label: "Browser", Value: browserRootCause})
	}
	return resp
}

func attachRuntimeResponseFields(ctx *LoopContext, resp *IMAgentResponse) {
	if ctx == nil || resp == nil {
		return
	}
	if resp.RequestID == "" {
		resp.RequestID = ctx.Runtime.RequestID
	}
	if resp.SessionKey == "" {
		resp.SessionKey = ctx.Runtime.Conversation.SessionKey
	}
}

func extractBrowserRootCause(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "browser") && !strings.Contains(lower, "cdp") && !strings.Contains(lower, "debug") {
		return ""
	}
	lines := strings.Split(text, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		filtered = append(filtered, line)
		if len(filtered) >= 4 {
			break
		}
	}
	return strings.Join(filtered, "\n")
}
