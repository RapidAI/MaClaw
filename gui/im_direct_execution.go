package main

import (
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (h *IMMessageHandler) tryDirectExecutionProfile(msg IMUserMessage, loopCtx *LoopContext, history []agent.ConversationEntry) (*IMAgentResponse, bool) {
	if loopCtx == nil || !loopCtx.Runtime.Execution.IsDirect() {
		return nil, false
	}
	// Group turns with authorised knowledge must reach the normal agent loop so
	// its knowledge-first policy can inspect memory/recall before any direct
	// routing is allowed to invoke a network tool.
	if loopCtx.LansengerGroupPermissions != nil && loopCtx.LansengerGroupPermissions.allowsKnowledge() {
		return nil, false
	}
	startedAt := time.Now()
	requestID := strings.TrimSpace(loopCtx.Runtime.RequestID)
	profile := loopCtx.Runtime.Execution
	toolName := directExecutionToolName(profile)
	if toolName == "" {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s reason=no_tool", requestID, msg.UserID, profile.TaskType)
		return nil, false
	}
	if h == nil || h.registry == nil {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=no_registry", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	if _, ok := h.registry.Get(toolName); !ok {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=tool_missing", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	contract := h.executionContractForRegisteredToolName(toolName)
	if !contract.Explicit || !contract.SupportsDirect || !contract.Deterministic {
		log.Printf("[exec-direct] skip request_id=%q user=%q task=%s tool=%s reason=contract_not_direct", requestID, msg.UserID, profile.TaskType, toolName)
		return nil, false
	}
	result := h.executeToolDetailedWithRuntimeState(msg.UserID, strings.TrimSpace(msg.UserID) != "", msg.Platform, toolName, `{}`, msg.Text, nil)
	if result.Outcome != toolOutcomeSucceeded || strings.TrimSpace(result.Text) == "" {
		log.Printf("[exec-direct] fallback request_id=%q user=%q task=%s tool=%s outcome=%s failure=%s elapsed=%s",
			requestID, msg.UserID, profile.TaskType, toolName, result.Outcome.String(), string(result.FailureKind), time.Since(startedAt).Round(time.Millisecond))
		return nil, false
	}
	text := directExecutionFinalText(msg, profile, strings.TrimSpace(result.Text))
	resp := &IMAgentResponse{
		Text:           text,
		RequestID:      requestID,
		SessionKey:     loopCtx.Runtime.Conversation.SessionKey,
		ResponseSource: "direct_execution",
	}
	updated := append([]agent.ConversationEntry(nil), history...)
	updated = append(updated,
		agent.ConversationEntry{Role: "user", Content: msg.Text},
		agent.ConversationEntry{Role: "assistant", Content: text},
	)
	if h.memory != nil {
		h.saveConversationHistoryTimed(msg.UserID, updated, resp)
	}
	log.Printf("[exec-direct] done request_id=%q user=%q task=%s tool=%s elapsed=%s text_len=%d",
		requestID, msg.UserID, profile.TaskType, toolName, time.Since(startedAt).Round(time.Millisecond), len([]rune(text)))
	imPerfLog("direct_execution", startedAt, requestID, msg.UserID, "task", profile.TaskType, "tool", toolName, "text_len", len([]rune(text)))
	return resp, true
}

func (h *IMMessageHandler) tryImmediateCurrentTimeDirect(msg IMUserMessage, providedLoopCtx *LoopContext) (*IMAgentResponse, bool) {
	if h == nil || !isLocalCurrentTimeQuery(msg.Text) {
		return nil, false
	}
	if providedLoopCtx != nil {
		return nil, false
	}
	if _, forced := hardStructuralFullExecutionProfile(msg, false, false); forced {
		return nil, false
	}
	profile, ok := localCurrentTimeExecutionProfile(msg.Text, h.executionContractForRegisteredToolName)
	if !ok || !profile.IsDirect() {
		return nil, false
	}
	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Runtime = runtimeContextFromIMMessage(msg)
	loopCtx.Runtime.Execution = profile
	loopCtx.Platform = msg.Platform
	loopCtx.UserID = msg.UserID
	loopCtx.Lang = msg.Lang
	var history []agent.ConversationEntry
	if h.memory != nil && strings.TrimSpace(msg.UserID) != "" {
		history = h.memory.Load(msg.UserID)
	}
	return h.tryDirectExecutionProfile(msg, loopCtx, history)
}

// tryImmediateScheduleListDirect avoids asking the LLM to route an unambiguous
// read-only schedule query. The manage_schedule tool is still the single source
// of truth: this only supplies its deterministic list action directly.
func (h *IMMessageHandler) tryImmediateScheduleListDirect(msg IMUserMessage, providedLoopCtx *LoopContext) (*IMAgentResponse, bool) {
	if h == nil || !isExplicitScheduledTaskListQuery(msg.Text) || providedLoopCtx != nil {
		return nil, false
	}
	if _, forced := hardStructuralFullExecutionProfile(msg, false, false); forced {
		return nil, false
	}
	return h.tryImmediateScheduleActionDirect(msg, manageScheduleActionList, "schedule_list", `{"action":"list"}`)
}

// tryImmediateScheduleRunDirect immediately starts a task only when the user
// explicitly names a scheduler-generated ID. This prevents a name collision
// from causing an unintended task to run, while avoiding an LLM routing hop.
func (h *IMMessageHandler) tryImmediateScheduleRunDirect(msg IMUserMessage, providedLoopCtx *LoopContext) (*IMAgentResponse, bool) {
	id, ok := explicitScheduledTaskRunID(msg.Text)
	if h == nil || !ok || providedLoopCtx != nil {
		return nil, false
	}
	if _, forced := hardStructuralFullExecutionProfile(msg, false, false); forced {
		return nil, false
	}
	return h.tryImmediateScheduleActionDirect(msg, manageScheduleActionRun, "schedule_run", `{"action":"run","id":"`+id+`"}`)
}

// tryImmediateScheduleActionDirect executes an already validated, deterministic
// manage_schedule operation and records it like the other direct IM paths.
func (h *IMMessageHandler) tryImmediateScheduleActionDirect(msg IMUserMessage, action manageScheduleAction, taskType, argsJSON string) (*IMAgentResponse, bool) {
	if h == nil || h.registry == nil {
		return nil, false
	}
	if _, ok := h.registry.Get("manage_schedule"); !ok {
		return nil, false
	}

	startedAt := time.Now()
	result := h.executeToolDetailedWithRuntimeState(
		msg.UserID,
		strings.TrimSpace(msg.UserID) != "",
		msg.Platform,
		"manage_schedule",
		argsJSON,
		msg.Text,
		nil,
	)
	if result.Outcome != toolOutcomeSucceeded || strings.TrimSpace(result.Text) == "" {
		log.Printf("[exec-direct] fallback request_id=%q user=%q task=%s tool=manage_schedule action=%s outcome=%s failure=%s elapsed=%s",
			imRequestID(msg), msg.UserID, taskType, action, result.Outcome.String(), string(result.FailureKind), time.Since(startedAt).Round(time.Millisecond))
		return nil, false
	}

	runtime := runtimeContextFromIMMessage(msg)
	text := strings.TrimSpace(result.Text)
	resp := &IMAgentResponse{
		Text:           text,
		RequestID:      imRequestID(msg),
		SessionKey:     runtime.Conversation.SessionKey,
		ResponseSource: "direct_execution",
	}
	if h.memory != nil && strings.TrimSpace(msg.UserID) != "" {
		history := append([]agent.ConversationEntry(nil), h.memory.Load(msg.UserID)...)
		history = append(history,
			agent.ConversationEntry{Role: "user", Content: msg.Text},
			agent.ConversationEntry{Role: "assistant", Content: text},
		)
		h.saveConversationHistoryTimed(msg.UserID, history, resp)
	}
	log.Printf("[exec-direct] done request_id=%q user=%q task=%s tool=manage_schedule action=%s elapsed=%s text_len=%d",
		resp.RequestID, msg.UserID, taskType, action, time.Since(startedAt).Round(time.Millisecond), len([]rune(text)))
	imPerfLog("direct_execution", startedAt, resp.RequestID, msg.UserID, "task", taskType, "tool", "manage_schedule", "action", action, "text_len", len([]rune(text)))
	return resp, true
}

// isExplicitScheduledTaskListQuery is deliberately narrow: commands which
// change or run a task must continue through normal planning and confirmation.
func isExplicitScheduledTaskListQuery(text string) bool {
	query := strings.ToLower(strings.TrimSpace(text))
	if query == "" {
		return false
	}
	for _, verb := range []string{
		"创建", "新建", "添加", "执行", "运行", "触发", "暂停", "恢复", "删除", "移除", "修改", "更新", "设置",
		"create", "add", "run", "execute", "trigger", "pause", "resume", "delete", "remove", "update", "set",
	} {
		if strings.Contains(query, verb) {
			return false
		}
	}

	chineseSchedule := strings.Contains(query, "定时任务") || strings.Contains(query, "计划任务") || strings.Contains(query, "日程任务")
	if chineseSchedule {
		for _, verb := range []string{"查看", "查询", "列出", "显示", "有哪些", "有啥", "有何", "什么任务", "任务列表"} {
			if strings.Contains(query, verb) {
				return true
			}
		}
	}

	englishSchedule := strings.Contains(query, "scheduled task") || strings.Contains(query, "scheduled tasks") ||
		strings.Contains(query, "schedule task") || strings.Contains(query, "schedule tasks")
	if !englishSchedule {
		return false
	}
	for _, verb := range []string{"list", "show", "view", "query", "what scheduled", "which scheduled", "scheduled tasks do i have"} {
		if strings.Contains(query, verb) {
			return true
		}
	}
	return false
}

var scheduledTaskIDInTextPattern = regexp.MustCompile(`(?i)\b\d{12,20}-[0-9a-f]{4}\b`)

// explicitScheduledTaskRunID recognizes only a clear run/execute instruction
// paired with a native task ID. Scheduler IDs are timestamp plus four hex
// digits, so ordinary dates and message numbers cannot accidentally match.
func explicitScheduledTaskRunID(text string) (string, bool) {
	query := strings.ToLower(strings.TrimSpace(text))
	if query == "" || !hasScheduledTaskReference(query) {
		return "", false
	}
	hasRunVerb := false
	for _, verb := range []string{"执行", "运行", "立即运行", "马上执行", "立刻执行", "触发", "run", "execute", "trigger"} {
		if strings.Contains(query, verb) {
			hasRunVerb = true
			break
		}
	}
	if !hasRunVerb {
		return "", false
	}
	return scheduledTaskIDInTextPattern.FindString(query), scheduledTaskIDInTextPattern.MatchString(query)
}

func hasScheduledTaskReference(query string) bool {
	return strings.Contains(query, "定时任务") || strings.Contains(query, "计划任务") || strings.Contains(query, "日程任务") ||
		strings.Contains(query, "scheduled task") || strings.Contains(query, "scheduled tasks") ||
		strings.Contains(query, "schedule task") || strings.Contains(query, "schedule tasks")
}

func directExecutionToolName(profile ExecutionProfile) string {
	return strings.TrimSpace(profile.DirectToolName)
}

func directExecutionFinalText(msg IMUserMessage, profile ExecutionProfile, toolResult string) string {
	switch directExecutionToolName(profile) {
	case "current_datetime":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(msg.Lang)), "en") {
			return "Current date/time: " + toolResult
		}
		return "\u5f53\u524d\u65e5\u671f\u65f6\u95f4\uff1a" + toolResult
	default:
		return toolResult
	}
}
