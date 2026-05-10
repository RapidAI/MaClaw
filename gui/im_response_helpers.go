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
	Label string `json:"label"`
	Value string `json:"value"`
}

// IMResponseAction is a suggested action in the agent response.
type IMResponseAction struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Style   string `json:"style"`
}

type IMResponseConfirmation struct {
	ID             string   `json:"id"`
	Summary        string   `json:"summary"`
	TaskType       string   `json:"task_type,omitempty"`
	TargetPaths    []string `json:"target_paths,omitempty"`
	PlannedActions []string `json:"planned_actions,omitempty"`
	RiskFlags      []string `json:"risk_flags,omitempty"`
	RevisionHints  []string `json:"revision_hints,omitempty"`
	Status         string   `json:"status,omitempty"`
}

type IMResponseUnfinishedTask struct {
	SlotID      string             `json:"slot_id,omitempty"`
	Title       string             `json:"title,omitempty"`
	Summary     string             `json:"summary,omitempty"`
	ProjectPath string             `json:"project_path,omitempty"`
	Status      string             `json:"status,omitempty"`
	Actions     []IMResponseAction `json:"actions,omitempty"`
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
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return []IMResponseAction{
		{Label: "Resume session", Command: "__resume_session__ " + sessionID, Style: "default"},
		{Label: "Dismiss session", Command: "__dismiss_recoverable_session__ " + sessionID, Style: "danger"},
	}
}

func buildRecoverableSessionPayload(session *RemoteSession) *IMResponseRecoverableSession {
	if session == nil {
		return nil
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.ResumeContext == nil {
		return nil
	}
	rc := session.ResumeContext
	return &IMResponseRecoverableSession{
		SessionID:       strings.TrimSpace(session.ID),
		Tool:            strings.TrimSpace(session.Tool),
		Title:           strings.TrimSpace(firstNonEmptyTraceText(session.Title, session.Summary.CurrentTask, rc.OriginalTask)),
		Summary:         strings.TrimSpace(firstNonEmptyTraceText(session.Summary.ProgressSummary, rc.LastProgress, session.Summary.LastResult, rc.LastOutput)),
		ProjectPath:     strings.TrimSpace(firstNonEmptyTraceText(session.ProjectPath, rc.ProjectPath)),
		Status:          strings.TrimSpace(string(session.Status)),
		ExitReason:      strings.TrimSpace(rc.ExitReason),
		ResumeSessionID: strings.TrimSpace(rc.ResumeSessionID),
		ResumeCount:     rc.ResumeCount,
		LastProgress:    strings.TrimSpace(rc.LastProgress),
		Actions:         buildRecoverableSessionActions(session.ID),
	}
}

func buildUnfinishedTaskPayload(slot *agent.UnfinishedTaskSlot) *IMResponseUnfinishedTask {
	if slot == nil {
		return nil
	}
	return &IMResponseUnfinishedTask{
		SlotID:      slot.SlotID,
		Title:       strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary)),
		Summary:     strings.TrimSpace(slot.Summary),
		ProjectPath: strings.TrimSpace(slot.ProjectPath),
		Status:      strings.TrimSpace(slot.Status),
		Actions:     buildResumeSlotActions(slot),
	}
}

func tokenUsageResponseFields(input, output int) []IMResponseField {
	if input <= 0 && output <= 0 {
		return nil
	}
	fields := make([]IMResponseField, 0, 3)
	if input > 0 {
		fields = append(fields, IMResponseField{Label: "Input tokens", Value: strconv.Itoa(input)})
	}
	if output > 0 {
		fields = append(fields, IMResponseField{Label: "Output tokens", Value: strconv.Itoa(output)})
	}
	total := input + output
	if total > 0 {
		fields = append(fields, IMResponseField{Label: "Total tokens", Value: strconv.Itoa(total)})
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
