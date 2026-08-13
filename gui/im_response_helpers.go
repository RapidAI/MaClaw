package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func hasVisibleIMResult(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	if strings.TrimSpace(resp.Text) != "" || strings.TrimSpace(resp.Error) != "" {
		return true
	}
	if len(resp.Fields) > 0 || len(resp.Actions) > 0 || strings.TrimSpace(resp.ImageKey) != "" {
		return true
	}
	if strings.TrimSpace(resp.FileData) != "" || strings.TrimSpace(resp.FileName) != "" || strings.TrimSpace(resp.FileMimeType) != "" {
		return true
	}
	return strings.TrimSpace(resp.LocalFilePath) != "" || hasNonEmptyString(resp.LocalFilePaths) || strings.TrimSpace(resp.ThumbnailBase64) != ""
}

var weixinQRCodeURLPattern = regexp.MustCompile("https://liteapp\\.weixin\\.qq\\.com/q/[^\\s<>\\\"']+")

func extractWeixinQRCodeURLFromToolResult(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	match := weixinQRCodeURLPattern.FindString(text)
	if match == "" || !strings.Contains(match, "qrcode=") {
		return ""
	}
	match = strings.TrimRight(match, ").,;:!?")
	if parsed, err := url.Parse(match); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return match
	}
	return ""
}

func attachLocalPreview(resp *IMAgentResponse, filePath, thumbnailBase64 string) {
	if resp == nil || strings.TrimSpace(filePath) == "" {
		return
	}
	if strings.TrimSpace(resp.ResponseSource) == "" {
		resp.ResponseSource = imResponseSourceScreenshot.String()
	}
	if strings.TrimSpace(resp.LocalFilePath) == "" {
		resp.LocalFilePath = filePath
	}
	seen := map[string]bool{}
	for _, p := range resp.LocalFilePaths {
		if strings.TrimSpace(p) != "" {
			seen[p] = true
		}
	}
	if !seen[filePath] {
		resp.LocalFilePaths = append(resp.LocalFilePaths, filePath)
	}
	if strings.TrimSpace(resp.ThumbnailBase64) == "" {
		resp.ThumbnailBase64 = thumbnailBase64
	}
}

func normalizeArtifactResponseSource(resp *IMAgentResponse) {
	if resp == nil {
		return
	}
	rawSource := strings.TrimSpace(resp.ResponseSource)
	source := canonicalIMResponseSourceKind(rawSource)
	switch source {
	case imResponseSourceFileDelivery, imResponseSourceScreenshot, imResponseSourceAskUser, imResponseSourceCancel, imResponseSourceAgentViewSubmit, imResponseSourceAgentViewDismiss:
		resp.ResponseSource = source.String()
		return
	case imResponseSourceUnknown:
	case imResponseSourceAgentLoop:
		resp.ResponseSource = source.String()
	default:
		resp.ResponseSource = rawSource
		return
	}
	if strings.TrimSpace(resp.ThumbnailBase64) != "" || strings.TrimSpace(resp.ImageKey) != "" {
		resp.ResponseSource = imResponseSourceScreenshot.String()
		return
	}
	if strings.TrimSpace(resp.LocalFilePath) != "" || hasNonEmptyString(resp.LocalFilePaths) ||
		strings.TrimSpace(resp.FileData) != "" || strings.TrimSpace(resp.FileName) != "" || strings.TrimSpace(resp.FileMimeType) != "" {
		resp.ResponseSource = imResponseSourceFileDelivery.String()
	}
}

func canonicalIMResponseSource(source string) string {
	return canonicalIMResponseSourceKind(source).String()
}

func hasNonEmptyString(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func appendVisibleNote(resp *IMAgentResponse, note string) {
	if resp == nil {
		return
	}
	note = strings.TrimSpace(note)
	if note == "" || strings.Contains(resp.Text, note) {
		return
	}
	if strings.TrimSpace(resp.Text) == "" {
		resp.Text = note
		return
	}
	resp.Text = strings.TrimRight(resp.Text, " \t\r\n") + "\n\n" + note
}

func ensureTraceAction(resp *IMAgentResponse) {
	if resp == nil || strings.TrimSpace(resp.RunID) == "" {
		return
	}
	command := "__view_trace__ " + resp.RunID
	for _, action := range resp.Actions {
		if strings.TrimSpace(action.Command) == command {
			return
		}
	}
	resp.Actions = append(resp.Actions, IMResponseAction{
		Label:   "View trace",
		Command: command,
		Style:   "default",
	})
}

func selectVisibleEmptyResultSummary(traceSummary string) string {
	summary := strings.TrimSpace(traceSummary)
	if !isVisibleEmptyResultSummary(summary) {
		return ""
	}
	return summary
}

func isVisibleEmptyResultSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	lower := strings.ToLower(summary)
	normalizedEcho := normalizeShortChitChatToken(regexp.MustCompile(`^(summary|result|trace summary|结果|摘要)\s*[:：]?\s*`).ReplaceAllString(lower, ""))
	if normalizedEcho != "" {
		return false
	}
	promptLikeMarkers := []string{
		"primary working directory",
		"current working directory",
		"project directory",
		"default directory",
		"continue the conversation",
		"resume directly",
		"user:",
		"assistant:",
		"task:",
		"system prompt",
		"当前工作目录",
		"请帮我",
		"帮我",
		"请实现",
		"请修改",
		"请重构",
		"you are",
	}
	for _, marker := range promptLikeMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	executionSignals := []string{
		"failed", "failure", "error", "stopped", "timeout", "cancel", "killed",
		"retry", "recovered", "generated", "created", "saved", "wrote", "written",
		"exported", "uploaded", "downloaded", "prepared", "delivered", "found", "produced",
		"失败", "错误", "停止", "超时", "取消", "重试", "恢复", "生成", "创建",
		"保存", "写入", "导出", "上传", "下载", "准备", "交付", "找到", "完成", "文件",
	}
	for _, signal := range executionSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func buildEmptyResultFallback(status TraceRunStatus, traceSummary string) string {
	summary := selectVisibleEmptyResultSummary(traceSummary)
	switch status {
	case TraceRunStatusFailed, TraceRunStatusTimeout, TraceRunStatusCancelled, TraceRunStatusStopped:
		if summary != "" {
			return fmt.Sprintf("任务未完成可交付结果。%s", summary)
		}
		return "任务未完成可交付结果。可查看 Trace 定位失败点。"
	case TraceRunStatusCompleted, TraceRunStatusExited:
		if summary != "" {
			return fmt.Sprintf("任务已结束，但没有生成可展示的结果。%s", summary)
		}
		return "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。"
	default:
		if summary != "" {
			return fmt.Sprintf("任务已停止，但没有可展示结果。%s", summary)
		}
		return "任务已停止，但没有可展示结果。可查看 Trace 了解详情。"
	}
}

func buildConfirmedResumeEmptyResultFallback(status TraceRunStatus, traceSummary string) string {
	summary := selectVisibleEmptyResultSummary(traceSummary)
	switch status {
	case TraceRunStatusFailed, TraceRunStatusTimeout, TraceRunStatusCancelled, TraceRunStatusStopped:
		if summary != "" {
			return fmt.Sprintf("已确认并开始执行任务，但未完成可交付结果。%s", summary)
		}
		return "已确认并开始执行任务，但未完成可交付结果。可查看 Trace 定位失败点。"
	case TraceRunStatusCompleted, TraceRunStatusExited:
		if summary != "" {
			return fmt.Sprintf("已确认并开始执行任务。当前暂无可展示结果。%s", summary)
		}
		return "已确认并开始执行任务。当前暂无可展示结果，可查看 Trace 了解进展。"
	default:
		if summary != "" {
			return fmt.Sprintf("已确认并开始执行任务，但暂未返回可展示结果。%s", summary)
		}
		return "已确认并开始执行任务，但暂未返回可展示结果。可查看 Trace 了解进展。"
	}
}

// IMResponseField is a key-value field in the agent response.
type IMResponseField struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Internal bool   `json:"internal,omitempty"`
}

// IMResponseAction is a suggested action in the agent response.
type IMResponseAction struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Style   string `json:"style"`
}

type IMResponseConfirmation struct {
	ID             string                   `json:"id"`
	Summary        string                   `json:"summary"`
	TaskType       string                   `json:"task_type,omitempty"`
	TargetPaths    []string                 `json:"target_paths,omitempty"`
	PlannedActions []string                 `json:"planned_actions,omitempty"`
	RiskFlags      []string                 `json:"risk_flags,omitempty"`
	RevisionHints  []string                 `json:"revision_hints,omitempty"`
	Status         string                   `json:"status,omitempty"`
	Labels         *IMResponseConfirmLabels `json:"labels,omitempty"`
}

// IMResponseConfirmLabels carries localized section titles for the
// confirmation card. The frontend renders these directly instead of
// hardcoding language-specific strings.
type IMResponseConfirmLabels struct {
	Title          string `json:"title"`
	Status         string `json:"status"`
	TargetPaths    string `json:"target_paths"`
	PlannedActions string `json:"planned_actions"`
	RiskFlags      string `json:"risk_flags"`
	RevisionHints  string `json:"revision_hints"`
}

type IMResponseUnfinishedTask struct {
	SlotID          string             `json:"slot_id,omitempty"`
	Title           string             `json:"title,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	ProjectPath     string             `json:"project_path,omitempty"`
	Status          string             `json:"status,omitempty"`
	LastToolName    string             `json:"last_tool_name,omitempty"`
	SideEffectState string             `json:"side_effect_state,omitempty"`
	RecoveryMode    string             `json:"recovery_mode,omitempty"`
	Actions         []IMResponseAction `json:"actions,omitempty"`
}

type IMResponseRecoverableSession struct {
	SessionID       string             `json:"session_id,omitempty"`
	Tool            string             `json:"tool,omitempty"`
	Title           string             `json:"title,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	ProjectPath     string             `json:"project_path,omitempty"`
	Status          string             `json:"status,omitempty"`
	ExitReason      string             `json:"exit_reason,omitempty"`
	ResumeSessionID string             `json:"resume_session_id,omitempty"`
	ResumeCount     int                `json:"resume_count,omitempty"`
	LastProgress    string             `json:"last_progress,omitempty"`
	Actions         []IMResponseAction `json:"actions,omitempty"`
}

func buildRecoverableSessionActions(sessionID string) []IMResponseAction {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildRecoverableSessionActionsWithLang(sessionID, lang)
}

func buildRecoverableSessionActionsWithLang(sessionID, lang string) []IMResponseAction {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return []IMResponseAction{
		{Label: unfinishedSlotText(lang, "Resume session", "恢复会话", "恢復會話"), Command: "__resume_session__ " + sessionID, Style: "default"},
		{Label: unfinishedSlotText(lang, "Dismiss session", "忽略会话", "忽略會話"), Command: "__dismiss_recoverable_session__ " + sessionID, Style: "danger"},
	}
}

func buildRecoverableSessionPayload(session *RemoteSession) *IMResponseRecoverableSession {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildRecoverableSessionPayloadWithLang(session, lang)
}

func buildRecoverableSessionPayloadWithLang(session *RemoteSession, lang string) *IMResponseRecoverableSession {
	if session == nil {
		return nil
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.ResumeContext == nil {
		return nil
	}
	rc := session.ResumeContext
	summary := strings.TrimSpace(firstNonEmptyTraceText(session.Summary.ProgressSummary, rc.LastProgress, session.Summary.LastResult, rc.LastOutput))
	lastProgress := strings.TrimSpace(rc.LastProgress)
	return &IMResponseRecoverableSession{
		SessionID:       strings.TrimSpace(session.ID),
		Tool:            strings.TrimSpace(session.Tool),
		Title:           strings.TrimSpace(firstNonEmptyTraceText(session.Title, session.Summary.CurrentTask, rc.OriginalTask)),
		Summary:         localizedUnfinishedSlotSummary(summary, lang),
		ProjectPath:     strings.TrimSpace(firstNonEmptyTraceText(session.ProjectPath, rc.ProjectPath)),
		Status:          strings.TrimSpace(session.Status.String()),
		ExitReason:      strings.TrimSpace(rc.ExitReason),
		ResumeSessionID: strings.TrimSpace(rc.ResumeSessionID),
		ResumeCount:     rc.ResumeCount,
		LastProgress:    localizedUnfinishedSlotSummary(lastProgress, lang),
		Actions:         buildRecoverableSessionActionsWithLang(session.ID, lang),
	}
}

func buildUnfinishedTaskPayload(slot *agent.UnfinishedTaskSlot) *IMResponseUnfinishedTask {
	lang, _ := agentViewCurrentLang.Load().(string)
	return buildUnfinishedTaskPayloadWithLang(slot, lang)
}

func buildUnfinishedTaskPayloadWithLang(slot *agent.UnfinishedTaskSlot, lang string) *IMResponseUnfinishedTask {
	if slot == nil {
		return nil
	}
	title := strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary))
	if strings.TrimSpace(slot.LastTask) == "" {
		title = localizedUnfinishedSlotSummary(title, lang)
	}
	return &IMResponseUnfinishedTask{
		SlotID:          slot.SlotID,
		Title:           title,
		Summary:         localizedUnfinishedSlotSummary(strings.TrimSpace(slot.Summary), lang),
		ProjectPath:     strings.TrimSpace(slot.ProjectPath),
		Status:          strings.TrimSpace(slot.Status.String()),
		LastToolName:    strings.TrimSpace(slot.LastToolName),
		SideEffectState: strings.TrimSpace(slot.SideEffectState),
		RecoveryMode:    strings.TrimSpace(slot.RecoveryMode),
		Actions:         buildResumeSlotActionsWithLang(slot, lang),
	}
}

func localizedUnfinishedSlotSummary(summary string, lang string) string {
	switch strings.TrimSpace(summary) {
	case "Previous task stopped making progress and was moved to recovery.":
		return unfinishedSlotText(lang,
			"Previous task stopped making progress and was moved to recovery.",
			"上次任务停止推进，已移入恢复状态。",
			"上次任務停止推進，已移入恢復狀態。")
	case "The previous task stopped before completion.":
		return unfinishedSlotText(lang,
			"The previous task stopped before completion.",
			"上次任务尚未完成就已停止。",
			"上次任務尚未完成就已停止。")
	default:
		return summary
	}
}

func tokenUsageResponseFields(input, output int) []IMResponseField {
	return tokenUsageResponseFieldsWithCache(input, output, 0, 0)
}

func tokenUsageResponseFieldsWithCache(input, output, cacheRead, cacheWrite int) []IMResponseField {
	if input <= 0 && output <= 0 && cacheRead <= 0 && cacheWrite <= 0 {
		return nil
	}
	fields := make([]IMResponseField, 0, 5)
	if input > 0 {
		fields = append(fields, IMResponseField{Label: "Input tokens", Value: strconv.Itoa(input), Internal: true})
	}
	if output > 0 {
		fields = append(fields, IMResponseField{Label: "Output tokens", Value: strconv.Itoa(output), Internal: true})
	}
	total := input + output
	if total > 0 {
		fields = append(fields, IMResponseField{Label: "Total tokens", Value: strconv.Itoa(total), Internal: true})
	}
	if cacheRead > 0 {
		fields = append(fields, IMResponseField{Label: "Cache read tokens", Value: strconv.Itoa(cacheRead), Internal: true})
	}
	if cacheWrite > 0 {
		fields = append(fields, IMResponseField{Label: "Cache write tokens", Value: strconv.Itoa(cacheWrite), Internal: true})
	}
	return fields
}

func deriveLLMTokenUsage(resp *llm.Response, conversation []interface{}) (int, int) {
	if resp == nil {
		return 0, 0
	}
	input := 0
	output := 0
	if resp.Usage != nil {
		u := resp.Usage
		input = u.PromptTokens
		output = u.CompletionTokens
		if input == 0 && u.InputTokens > 0 {
			input = u.InputTokens
		}
		if output == 0 && u.OutputTokens > 0 {
			output = u.OutputTokens
		}
	}
	if input == 0 {
		input = estimateConversationTokens(conversation)
	}
	if output == 0 && len(resp.Choices) > 0 {
		output = estimateBytesToTokens([]byte(resp.Choices[0].Message.Content))
	}
	return input, output
}

func mergeIMResponseFields(base []IMResponseField, extra []IMResponseField) []IMResponseField {
	if len(extra) == 0 {
		return base
	}
	merged := append([]IMResponseField{}, base...)
	merged = append(merged, extra...)
	return merged
}

func modelRouteResponseFields(d modelRouteDecision) []IMResponseField {
	if d.Task == "" && d.Model == "" {
		return nil
	}
	fields := []IMResponseField{
		{Label: "Route task", Value: firstNonEmpty(d.Task, "-"), Internal: true},
		{Label: "Route source", Value: firstNonEmpty(d.Source, "-"), Internal: true},
		{Label: "Route model", Value: firstNonEmpty(d.Model, "-"), Internal: true},
	}
	if d.CostTier != "" && (d.CostRouteMode == "shadow" || d.CostRouteMode == "on") {
		val := d.CostTier
		if !d.CostRouteApplied {
			val = d.CostTier + " (shadow)"
		}
		fields = append(fields, IMResponseField{Label: "Cost tier", Value: val, Internal: true})
	}
	if d.ThinkingPolicy != "" && (d.CostRouteMode == "shadow" || d.CostRouteMode == "on") {
		val := d.ThinkingPolicy
		if !d.CostRouteApplied {
			val = d.ThinkingPolicy + " (shadow)"
		}
		fields = append(fields, IMResponseField{Label: "Thinking", Value: val, Internal: true})
	}
	if d.Escalated {
		fields = append(fields, IMResponseField{Label: "Route escalated", Value: "yes", Internal: true})
	}
	if strings.TrimSpace(d.Reason) != "" {
		fields = append(fields, IMResponseField{Label: "Route reason", Value: d.Reason, Internal: true})
	}
	return fields
}

// turnMetaResponseField returns a single always-on "Turn" chip for chat UI:
// route tier + model + compact tokens + estimated cost + optional prompt profile/savings.
func turnMetaResponseField(d modelRouteDecision, input, output, cacheRead int, estCostRMB float64, promptProfile string, promptSavedTokens int, promptUpgraded bool, promptABSample bool, promptSoftFull bool) []IMResponseField {
	usage := agent.TurnUsage{
		Model:        d.Model,
		InputTokens:  input,
		OutputTokens: output,
		CachedTokens: cacheRead,
		EstCostRMB:   estCostRMB,
	}
	route := agent.RouteDecision{
		TaskType:         d.Task,
		Source:           d.Source,
		Model:            d.Model,
		Provider:         d.Provider,
		Reason:           d.Reason,
		Applied:          true,
		CostTier:         d.CostTier,
		CostRouteMode:    d.CostRouteMode,
		CostRouteApplied: d.CostRouteApplied,
		ThinkingPolicy:   d.ThinkingPolicy,
	}
	if d.Escalated && route.Source == "" {
		route.Source = "escalate"
	}
	meta := agent.FormatTurnMetaOpts(agent.TurnMetaOptions{
		Route:             route,
		Usage:             usage,
		PromptProfile:     promptProfile,
		PromptSavedTokens: promptSavedTokens,
		PromptUpgraded:    promptUpgraded,
		PromptABSample:    promptABSample,
		PromptSoftFull:    promptSoftFull,
	})
	if meta == "" {
		return nil
	}
	return []IMResponseField{{Label: "Turn", Value: meta, Internal: true}}
}
