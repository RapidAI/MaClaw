package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/nudge"
	"github.com/RapidAI/CodeClaw/corelib/progress"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/session"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// imHeartbeatMsg is the sentinel value sent as a progress update to keep the
// Hub-side response timer alive. It must never be delivered to the end user.
const imHeartbeatMsg = "__heartbeat__"

// ---------------------------------------------------------------------------
// IMMessageHandler — handles IM messages forwarded from Hub via WebSocket
// ---------------------------------------------------------------------------

// MessageAttachment represents a file/image/audio attachment from IM.
// MessageAttachment is an alias for the corelib type.
// See corelib/agent/message.go for the canonical definition.
type MessageAttachment = agent.MessageAttachment

// IMUserMessage is the payload of an "im.user_message" from Hub.
// Core fields are defined in agent.UserMessage; GUI-specific fields are added here.
type IMUserMessage = agent.UserMessage

// IMAgentResponse is the structured reply sent back to Hub.
type IMAgentResponse struct {
	Text                                string                        `json:"text"`
	Fields                              []IMResponseField             `json:"fields,omitempty"`
	Actions                             []IMResponseAction            `json:"actions,omitempty"`
	Confirmation                        *IMResponseConfirmation       `json:"confirmation,omitempty"`
	UnfinishedTask                      *IMResponseUnfinishedTask     `json:"unfinished_task,omitempty"`
	UnfinishedSlot                      *IMResponseUnfinishedTask     `json:"unfinished_slot,omitempty"`
	RecoverableSession                  *IMResponseRecoverableSession `json:"recoverable_session,omitempty"`
	ImageKey                            string                        `json:"image_key,omitempty"`
	FileData                            string                        `json:"file_data,omitempty"`
	FileName                            string                        `json:"file_name,omitempty"`
	FileMimeType                        string                        `json:"file_mime_type,omitempty"`
	LocalFilePath                       string                        `json:"local_file_path,omitempty"`
	LocalFilePaths                      []string                      `json:"local_file_paths,omitempty"`
	ThumbnailBase64                     string                        `json:"thumbnail_base64,omitempty"`
	Error                               string                        `json:"error,omitempty"`
	ResponseSource                      string                        `json:"response_source,omitempty"`
	Deferred                            bool                          `json:"deferred,omitempty"`
	ConfirmedResume                     bool                          `json:"confirmed_resume,omitempty"`
	HardExit                            bool                          `json:"-"` // set when agent loop exits due to consecutive empty responses; suppresses doc capture
	JobID                               string                        `json:"job_id,omitempty"`
	RunID                               string                        `json:"run_id,omitempty"`
	RequestID                           string                        `json:"request_id,omitempty"`
	TraceStatus                         string                        `json:"trace_status,omitempty"`
	TraceSummary                        string                        `json:"trace_summary,omitempty"`
	TraceEventCount                     int                           `json:"trace_event_count,omitempty"`
	EvidenceCount                       int                           `json:"evidence_count,omitempty"`
	TrialReflectSummary                 string                        `json:"trial_reflect_summary,omitempty"`
	TrialReflectStatus                  string                        `json:"trial_reflect_status,omitempty"`
	TrialReflectFailures                int                           `json:"trial_reflect_failures,omitempty"`
	InputTokens                         int                           `json:"input_tokens,omitempty"`
	OutputTokens                        int                           `json:"output_tokens,omitempty"`
	TotalTokens                         int                           `json:"total_tokens,omitempty"`
	HandlerTailNanos                    int64                         `json:"-"`
	HandlerBlackholeAfterUsageNanos     int64                         `json:"-"`
	HandlerBlackholeBeforeReturnNanos   int64                         `json:"-"`
	HandlerPostStreamUsageNanos         int64                         `json:"-"`
	HandlerPostStreamResponseNanos      int64                         `json:"-"`
	HandlerPostStreamToolExecNanos      int64                         `json:"-"`
	HandlerPostStreamChoiceNanos        int64                         `json:"-"`
	HandlerPostStreamAssistantMsgNanos  int64                         `json:"-"`
	HandlerPostStreamHistoryAppendNanos int64                         `json:"-"`
	HandlerPostStreamNoToolBranchNanos  int64                         `json:"-"`
	FinalizeTraceNanos                  int64                         `json:"-"`
	MemorySaveNanos                     int64                         `json:"-"`
	CapabilityGapNanos                  int64                         `json:"-"`
	FileMaterializeNanos                int64                         `json:"-"`
	PreLLMPrepNanos                     int64                         `json:"-"`
	PreLLMConfigNanos                   int64                         `json:"-"`
	PreLLMToolsNanos                    int64                         `json:"-"`
	PreLLMConversationNanos             int64                         `json:"-"`
	PreLLMIterationPrepNanos            int64                         `json:"-"`
	FirstTokenWaitNanos                 int64                         `json:"-"`
	LLMRequestBuildNanos                int64                         `json:"-"`
	LLMHTTPDoNanos                      int64                         `json:"-"`
	LLMFirstSSEWaitNanos                int64                         `json:"-"`
	LLMRetryWaitNanos                   int64                         `json:"-"`
	LLMStreamMaxTokenGapNanos           int64                         `json:"-"`
	LLMRetryCount                       int                           `json:"-"`
	LLMIdleTimeoutCount                 int                           `json:"-"`
	LLMIdleTimeoutAfterToken            bool                          `json:"-"`
}

const stalledNoToolRecoverThreshold = 2

// maxConsecutiveEmptyResponses is the hard limit for consecutive empty LLM
// responses. When the model returns empty content this many times in a row,
// the loop force-returns the best available result instead of injecting more
// Recover prompts (which inflate context and worsen the problem).
const maxConsecutiveEmptyResponses = 3

// maxTotalRecoverInjections caps the total number of Recover prompt injections
// per agent loop. This prevents context bloat when the model is stuck in a
// recover-empty-recover cycle.
const maxTotalRecoverInjections = 8

type agentLoopStage string

type skillPreferenceMode string

const (
	agentStageOrient   agentLoopStage = "orient"
	agentStageExecute  agentLoopStage = "execute"
	agentStageRecover  agentLoopStage = "recover"
	agentStageConverge agentLoopStage = "converge"
	agentStageFinalize agentLoopStage = "finalize"
)

const (
	skillPreferenceNone            skillPreferenceMode = "none"
	skillPreferenceLocalOnly       skillPreferenceMode = "local_only"
	skillPreferenceRemoteRequired  skillPreferenceMode = "remote_required"
	skillPreferenceFallbackAllowed skillPreferenceMode = "fallback_allowed"
)

type agentLoopPhase struct {
	Stage                     agentLoopStage
	ConsecutiveNoTool         int
	ConsecutiveEmptyResponses int // tracks consecutive empty LLM responses for hard exit
	TotalRecoverInjections    int // total recover prompt injections in this loop
	DeliverableRecoverCount   int
	ForceSkillPreference      bool
	SkillMode                 skillPreferenceMode
	PreferredSkillName        string
	PreferredSkillReason      string
	PreferredSkillRunID       string
	SkillAttempted            bool
	SkillFailed               bool
	RemoteSearchAttempted     bool
	RemoteSearchExhausted     bool
	RecoverReason             string
	RecoverPrompt             string

	// Workaround detection: when a skill execution fails, we record the
	// skill name and error. If the LLM subsequently resolves the task
	// through alternative tool calls (the loop ends successfully without
	// the skill), we classify the outcome as "workaround".
	FailedSkillName  string // set when a run_skill/manage_skill(action=run) call fails
	FailedSkillError string // the error message from the failed skill execution

	// ToolHallucinationCorrected is set to true after injecting a correction
	// for a tool availability hallucination (e.g. "我没有 bash 工具").
	// Only one correction per loop to avoid infinite cycles.
	ToolHallucinationCorrected bool

	// LengthContinuations tracks how many times the loop has injected a
	// continuation prompt after the LLM's text output was truncated
	// (finish_reason="length"). Capped at maxLengthContinuations to
	// prevent infinite loops.
	LengthContinuations int
}

// skillHintWordBoundaryRe matches a skill preference hint as a whole word,
// preventing false positives from substrings in domain names, file paths, or
// compound words (e.g. "paper" matching "mypapers.top").
var skillHintWordBoundaryRe = regexp.MustCompile(`(?i)\bpaper\b|\breport\b`)

func shouldPreferSkillForTask(text string) bool {
	result := classifyTaskIntent(text)
	if result.Intent == intentCoding || result.Intent == intentSSH {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	// Substring hints — safe for multi-char Chinese phrases and distinctive
	// English terms that rarely appear as substrings of other words.
	substringHints := []string{
		"pdf", "报告", "文档", "综述", "markdown", "导出", "转换",
		"生成文件", "发送文件", "daily papers",
	}
	for _, hint := range substringHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	// Word-boundary hints — common English words that frequently appear as
	// substrings in domain names / URLs (e.g. "mypapers.top", "report.csv").
	if skillHintWordBoundaryRe.MatchString(lower) {
		return true
	}
	return false
}

func matchPreferredLocalSkill(exec *SkillExecutor, userText string) (string, string) {
	if exec == nil {
		return "", ""
	}
	lower := strings.ToLower(strings.TrimSpace(userText))
	if lower == "" {
		return "", ""
	}
	bestName := ""
	bestReason := ""
	bestScore := 0
	for _, skill := range exec.List() {
		if strings.TrimSpace(skill.Name) == "" {
			continue
		}
		score := 0
		for _, trigger := range skill.Triggers {
			trigger = strings.ToLower(strings.TrimSpace(trigger))
			if trigger == "" {
				continue
			}
			if strings.Contains(lower, trigger) {
				score += 3
			}
		}
		desc := strings.ToLower(strings.TrimSpace(skill.Description))
		if desc != "" {
			for _, token := range []string{"pdf", "报告", "文档", "综述", "markdown", "daily papers"} {
				if strings.Contains(lower, token) && strings.Contains(desc, token) {
					score += 2
				}
			}
			// Word-boundary tokens: require whole-word match in user text
			// to avoid false positives from domain names / URLs.
			if skillHintWordBoundaryRe.MatchString(lower) {
				for _, token := range []string{"paper", "report"} {
					if strings.Contains(desc, token) {
						score += 2
					}
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestName = skill.Name
			bestReason = firstNonEmptyTraceText(skill.Description, strings.Join(skill.Triggers, ", "))
		}
	}
	if bestScore <= 0 {
		return "", ""
	}
	return bestName, bestReason
}

func shouldBypassSkillPreference(toolCalls []llm.ToolCall) bool {
	for _, tc := range toolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		switch name {
		case "run_skill", "get_skill_run", "list_skills", "search_skill_hub", "install_skill_hub", "search_and_install_skill":
			return true
		}
	}
	return false
}

func isSkillSearchToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "search_and_install_skill", "search_skill_hub", "install_skill_hub":
		return true
	default:
		return false
	}
}

func isSkillProgressToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case "run_skill", "get_skill_run", "list_skills":
		return true
	default:
		return false
	}
}

func shouldRestrictToSkillSearch(phase agentLoopPhase) bool {
	return phase.ForceSkillPreference && phase.SkillMode == skillPreferenceRemoteRequired && !phase.RemoteSearchExhausted
}

func filterToolsForSkillPreference(toolDefs []map[string]interface{}) []map[string]interface{} {
	if len(toolDefs) == 0 {
		return toolDefs
	}
	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, def := range toolDefs {
		name := extractToolName(def)
		switch name {
		case "craft_tool", "bash", "create_session":
			continue
		default:
			filtered = append(filtered, def)
		}
	}
	if len(filtered) == 0 {
		return toolDefs
	}
	return filtered
}

func filterToolsForRemoteSkillSearch(toolDefs []map[string]interface{}) []map[string]interface{} {
	if len(toolDefs) == 0 {
		return toolDefs
	}
	filtered := make([]map[string]interface{}, 0, len(toolDefs))
	for _, def := range toolDefs {
		name := extractToolName(def)
		if isSkillSearchToolName(name) || isSkillProgressToolName(name) {
			filtered = append(filtered, def)
		}
	}
	if len(filtered) == 0 {
		return filterToolsForSkillPreference(toolDefs)
	}
	return filtered
}

func enterRecoverPhase(phase *agentLoopPhase, reason, prompt string) {
	if phase == nil {
		return
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	phase.Stage = agentStageRecover
	phase.RecoverReason = strings.TrimSpace(reason)
	phase.RecoverPrompt = prompt
	phase.TotalRecoverInjections++
}

func buildSkillProgressGuidance(skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return fmt.Sprintf("若已拿到 run_id，请优先调用 get_skill_run(run_id=\"%s\") 继续观察状态；仅在明确失败后再切换到其他真实工具。", runID)
	}
	if skillName != "" {
		return fmt.Sprintf("若已拿到 run_id 且尚未见明确成功/失败，请优先调用 get_skill_run(run_id=...) 继续观察状态；否则先调用 manage_skill(action=\"run\", name=\"%s\") 开始执行。", skillName)
	}
	return "若已拿到 run_id 且尚未见明确成功/失败，请优先调用 get_skill_run(run_id=...) 继续观察状态。"
}

func extractSkillRunID(toolCalls []llm.ToolCall, toolResults []string) string {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return ""
	}
	for i := len(toolCalls) - 1; i >= 0; i-- {
		if strings.TrimSpace(toolCalls[i].Function.Name) != "run_skill" {
			continue
		}
		result := strings.TrimSpace(toolResults[i])
		if result == "" {
			continue
		}
		if matches := regexp.MustCompile(`run_id[:=]\s*([A-Za-z0-9._-]+)`).FindStringSubmatch(result); len(matches) == 2 {
			return strings.TrimSpace(matches[1])
		}
		if matches := regexp.MustCompile(`（run_id=([A-Za-z0-9._-]+)）`).FindStringSubmatch(result); len(matches) == 2 {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

func buildSkillRecoverPrompt(skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	guidance := buildSkillProgressGuidance(skillName, runID)
	if skillName == "" {
		return "[Recover 阶段]\n本地 Skill 已尝试且失败，当前进入 Recover 阶段。不要重复同一个失败 Skill。请基于已知失败原因重新规划，改用其他真实工具（如 send_file / craft_tool / bash）走最短交付路径，并继续完成任务。\n" + guidance + "\n[/Recover 阶段]"
	}
	return fmt.Sprintf("[Recover 阶段]\n本地 Skill「%s」已尝试且失败，当前进入 Recover 阶段。不要再次调用同一个 Skill。请基于失败原因重新规划，改用其他真实工具（如 send_file / craft_tool / bash）走最短交付路径，并继续完成任务。\n%s\n[/Recover 阶段]", skillName, guidance)
}

func buildDriftRecoverPrompt(drift DriftResult) string {
	detail := strings.TrimSpace(drift.ReplanPrompt)
	if detail == "" {
		detail = "检测到执行路径出现漂移或循环，请停止重复同一做法，回到原始目标并改用不同路径继续完成任务。"
	}
	toolWarning := ""
	if drift.DriftedTool != "" {
		toolWarning = fmt.Sprintf("\n禁止再次调用 %s（已连续失败）。如果没有替代方案，直接向用户说明限制。", drift.DriftedTool)
	}
	return "[Recover 阶段]\n检测到执行路径出现漂移或循环，当前进入 Recover 阶段。请先暂停重复操作，回到原始目标，基于已知结果改用不同路径继续完成任务。\n" + detail + toolWarning + "\n[/Recover 阶段]"
}

// truncateRunesForDrift truncates a string to maxRunes for use as a drift
// detector result hint. Prefers cutting at a newline boundary.
func truncateRunesForDrift(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	truncated := string(runes[:maxRunes])
	// Try to cut at last newline for readability, but keep at least half.
	if idx := strings.LastIndex(truncated, "\n"); idx > len(truncated)/2 {
		truncated = truncated[:idx]
	}
	return truncated + "…"
}

func buildDeliverableRecoverPrompt(skillName string, preferSkill bool, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return "[Recover 阶段]\n检测到上一轮只承诺将要生成、整理或发送结果，但还没有真正交付，当前进入 Recover 阶段。不要继续停留在承诺或解释上。" + buildSkillProgressGuidance(skillName, runID) + " 若仍无法完成，必须直接说明失败原因和当前可见结果。\n[/Recover 阶段]"
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[Recover 阶段]\n检测到上一轮只承诺将要生成、整理或发送结果，但还没有真正交付，当前进入 Recover 阶段。不要继续停留在承诺或解释上，优先直接调用 manage_skill(action=\"run\", name=\"%s\") 完成交付；若已拿到 run_id 且尚未见明确成功/失败，请优先调用 get_skill_run(run_id=...) 继续观察状态；若该 Skill 明确失败，再切换到其他真实工具。若仍无法完成，必须直接说明失败原因和当前可见结果。\n[/Recover 阶段]", skillName)
	}
	return "[Recover 阶段]\n检测到上一轮只承诺将要生成、整理或发送结果，但还没有真正交付，当前进入 Recover 阶段。不要继续停留在承诺或解释上，请立即调用真实工具完成交付；若目标是文档，优先选择当前可用的文档/文件交付工具。若仍无法完成，必须直接说明失败原因和当前可见结果。\n[/Recover 阶段]"
}

func buildEmptyResultRecoverPrompt() string {
	return "[Recover 阶段]\n检测到上一轮没有返回任何可展示结果，当前进入 Recover 阶段。不要空结束，也不要只重复解释。请立即二选一：1) 直接给出可展示结果；2) 明确说明失败原因和当前状态。若还需要继续执行，必须立即调用真实工具。\n[/Recover 阶段]"
}

// cancelledExitResponse saves accumulated history and returns a clean
// cancellation message. This is the single exit point for all cancellation
// paths inside runAgentLoop, structurally enforcing the invariant that the
// loop always saves (never clears) history.
func (h *IMMessageHandler) cancelledExitResponse(userID string, history []agent.ConversationEntry, userText string) *IMAgentResponse {
	h.saveConversationHistoryTimed(userID, history, nil)
	cancelMsg := "⏹️ 任务已取消。"
	if taskPreview := truncateRunes(userText, 30); taskPreview != "" {
		cancelMsg = fmt.Sprintf("⏹️ 已取消任务「%s」。", taskPreview)
	}
	return &IMAgentResponse{Text: cancelMsg}
}

// findLastAssistantContent scans conversation history backwards and returns
// the content of the last non-empty assistant message. This is used as a
// fallback when the loop must hard-exit due to consecutive empty responses.
func findLastAssistantContent(history []agent.ConversationEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Role == "assistant" {
			var content string
			switch v := entry.Content.(type) {
			case string:
				content = v
			default:
				continue
			}
			content = strings.TrimSpace(content)
			if content != "" && len([]rune(content)) > 10 {
				return content
			}
		}
	}
	return ""
}

func buildTrialFailureRecoverPrompt(observation string, repeatedFailures []string) string {
	var b strings.Builder
	b.WriteString("[Recover 阶段]\n上一轮真实工具执行已出现失败，当前进入 Recover 阶段。请先根据失败结果调整计划，不要原样重复已经失败的尝试。")
	if obs := strings.TrimSpace(observation); obs != "" {
		b.WriteString("\n失败观察: ")
		b.WriteString(obs)
	}
	if len(repeatedFailures) > 0 {
		items := append([]string(nil), repeatedFailures...)
		sort.Strings(items)
		b.WriteString("\n避免重复: ")
		b.WriteString(strings.Join(items, ", "))
	}
	b.WriteString("\n下一步：优先改用不同路径或修正参数后继续完成任务；若仍无法完成，直接说明失败原因和当前状态。\n[/Recover 阶段]")
	return b.String()
}

func buildRemoteSkillSearchPrompt() string {
	return "[执行要求]\n当前任务属于 Skill 优先路径，但本地未命中合适 Skill。不要继续解释、承诺或直接 craft_tool；请立即先调用 search_and_install_skill（或其他 skill 搜索/安装工具）查找并安装可复用 Skill。只有在确认远程 Skill 路径无解后，才切换到 craft_tool 或 bash。\n[/执行要求]"
}

func buildNoToolActionPrompt(preferSkill bool, skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return "[执行要求]\n当前任务需要真实执行，不要继续停留在解释、承诺或列步骤上。" + buildSkillProgressGuidance(skillName, runID) + "\n[/执行要求]"
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[执行要求]\n当前任务需要真实执行，不要继续停留在解释、承诺或列步骤上。优先立即调用 manage_skill(action=\"run\", name=\"%s\") 开始执行；若已拿到 run_id 且尚未见明确成功/失败，请优先调用 get_skill_run(run_id=...) 继续观察状态；若该 Skill 不适用或失败，再切换到其他真实工具。\n[/执行要求]", skillName)
	}
	return "[执行要求]\n当前任务需要真实执行，不要继续停留在解释、承诺或列步骤上。请立即选择一个最合适的真实工具开始执行；若目标是文档/文件交付，优先使用文件生成、编辑或发送相关工具。\n[/执行要求]"
}

func buildNoToolStallRecoverPrompt(consecutive int, preferSkill bool, skillName, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return fmt.Sprintf("[Recover 阶段]\n连续 %d 轮都没有真正调用工具，任务已进入停滞状态，当前进入 Recover 阶段。不要继续解释、承诺或空转。%s\n[/Recover 阶段]", consecutive, buildSkillProgressGuidance(skillName, runID))
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[Recover 阶段]\n连续 %d 轮都没有真正调用工具，任务已进入停滞状态，当前进入 Recover 阶段。不要继续解释、承诺或空转。优先立即调用 manage_skill(action=\"run\", name=\"%s\") 启动实际执行；若已拿到 run_id 且尚未见明确成功/失败，请优先调用 get_skill_run(run_id=...) 继续观察状态；若该 Skill 不适用或失败，再切换到其他真实工具完成任务。\n[/Recover 阶段]", consecutive, skillName)
	}
	return fmt.Sprintf("[Recover 阶段]\n连续 %d 轮都没有真正调用工具，任务已进入停滞状态，当前进入 Recover 阶段。不要继续解释、承诺或空转。请立即选择一个最合适的真实工具开始执行；若目标是文档/文件交付，优先使用文件生成或发送相关工具。\n[/Recover 阶段]", consecutive)
}

func didSkillToolFail(toolCalls []llm.ToolCall, toolResults []string) bool {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return false
	}
	for i, tc := range toolCalls {
		switch strings.TrimSpace(tc.Function.Name) {
		case "run_skill", "get_skill_run", "search_and_install_skill":
			if classifyToolOutcome(strings.TrimSpace(tc.Function.Name), toolResults[i]) == "failed" {
				return true
			}
		}
	}
	return false
}

// extractFailedSkillInfo extracts the skill name and error message from a
// failed run_skill or manage_skill(action=run) tool call. Returns ("", "")
// if no skill failure is found. This is used for workaround detection:
// when a skill fails but the LLM resolves the task through alternative
// tool calls, the outcome is classified as "workaround".
func extractFailedSkillInfo(toolCalls []llm.ToolCall, toolResults []string) (skillName, lastError string) {
	if len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return "", ""
	}
	for i, tc := range toolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name != "run_skill" && name != "manage_skill" {
			continue
		}
		// For manage_skill, only consider action=run
		if name == "manage_skill" {
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
				continue
			}
			action, _ := parsed["action"].(string)
			if action != "run" {
				continue
			}
		}
		if classifyToolOutcome(name, toolResults[i]) != "failed" {
			continue
		}
		// Extract skill name from the tool call arguments.
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsed); err != nil {
			continue
		}
		sn, _ := parsed["name"].(string)
		if sn == "" {
			sn, _ = parsed["skill_name"].(string)
		}
		if sn == "" {
			continue
		}
		// Use the tool result as the error message (truncated).
		errMsg := strings.TrimSpace(toolResults[i])
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		return sn, errMsg
	}
	return "", ""
}

func userFacingToolProgressText(toolName string) string {
	switch toolName {
	case "craft_tool":
		return "🛠️ 正在生成并执行脚本，准备继续完成交付..."
	case "bash":
		return "🖥️ 正在执行命令处理文件，请稍候..."
	case "run_skill":
		return "🚀 正在启动 Skill 并等待状态快照..."
	case "send_file":
		return "📤 正在整理并发送生成的文件..."
	case "generate_pdf":
		return "📄 正在生成 PDF 文件..."
	default:
		return "⚙️ 正在执行工具，请稍候..."
	}
}

func shouldExposeToolInternalProgress(toolName string) bool {
	switch toolName {
	case "craft_tool", "bash", "run_skill":
		return true
	default:
		return false
	}
}

func filterUserFacingToolProgress(toolName, msg string) string {
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return ""
	}
	if shouldExposeToolInternalProgress(toolName) {
		switch toolName {
		case "craft_tool":
			allowedPrefixes := []string{"🧠 ", "💾 ", "🚀 ", "📦 ", "⏳ "}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		case "bash":
			if strings.HasPrefix(trimmed, "⏳ ") {
				return trimmed
			}
			return ""
		case "run_skill":
			allowedPrefixes := []string{"🚀 ", "⏳ ", "✅ ", "❌ "}
			for _, prefix := range allowedPrefixes {
				if strings.HasPrefix(trimmed, prefix) {
					return trimmed
				}
			}
			return ""
		}
	}
	return ""
}

func shouldResumeIncompleteTask(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	resumePhrases := []string{
		"继续", "继续呀", "继续做", "继续完成", "继续上次", "接着做", "接着完成", "接着上次", "恢复上次", "做完上次",
		"continue", "continue it", "continue this", "resume", "resume it", "resume this", "pick up where you left off",
	}
	for _, phrase := range resumePhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func looksLikeFreshTaskRequest(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || shouldResumeIncompleteTask(trimmed) {
		return false
	}
	if countWords(trimmed) < 4 {
		return false
	}
	lower := strings.ToLower(trimmed)
	freshTaskHints := []string{
		"帮我", "请你", "请帮我", "现在去", "把", "整理", "分析", "搜索", "生成", "写", "修", "移动", "复制", "放入", "导入",
		"please", "help me", "can you", "now", "move", "copy", "write", "generate", "summarize", "analyze", "search", "import",
	}
	for _, hint := range freshTaskHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func shouldClearHistoryForIncompleteTask(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if shouldResumeIncompleteTask(trimmed) {
		return false
	}
	if looksLikeFreshTaskRequest(trimmed) {
		return true
	}
	if countWords(trimmed) < 4 {
		return false
	}
	return true
}

// pendingAskUserState tracks an ask_user question that is waiting for the
// user's response. Stored in IMMessageHandler.pendingAskUser keyed by userID.
type pendingAskUserState struct {
	Question  string
	Options   []string
	InputType string
	Timestamp time.Time
}

// pendingCapabilityGapResult stores the outcome of an async capability gap
// resolution that ran in the background after the response was returned.
// If a skill was found and installed, the result is injected into the next
// conversation turn's system prompt so the LLM knows a new capability is
// available.
type pendingCapabilityGapResult struct {
	SkillName string
	Result    string // install/execute result text
	Success   bool   // true if skill was installed and executed successfully
	Timestamp time.Time
}

func hasIncompleteTaskMarker(entries []agent.ConversationEntry) bool {
	for i := len(entries) - 1; i >= 0; i-- {
		text, ok := entries[i].Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。") {
			return true
		}
		if strings.Contains(trimmed, "(已达到最大推理轮次，请继续发送消息以完成任务)") {
			return true
		}
		if strings.Contains(trimmed, "已接近最大推理轮次") {
			return true
		}
	}
	return false
}

func shouldAutoClearIncompleteTaskContext(newMessage string, entries []agent.ConversationEntry) bool {
	if !hasIncompleteTaskMarker(entries) {
		return false
	}
	return shouldClearHistoryForIncompleteTask(newMessage)
}

// extractOriginalUserTask scans conversation history to find the first
// substantive user message (the original task request). This is used to
// populate the unfinished task slot when max rounds are reached.
func extractOriginalUserTask(history []agent.ConversationEntry) string {
	for _, e := range history {
		if e.Role != "user" {
			continue
		}
		text, ok := e.Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || shouldResumeIncompleteTask(trimmed) {
			continue
		}
		// Skip very short messages that are likely confirmations.
		if len([]rune(trimmed)) < 4 {
			continue
		}
		return truncateRunes(trimmed, 300)
	}
	return ""
}

// extractProgressSummary builds a brief summary of what the agent accomplished
// by scanning the last few assistant messages in the conversation history.
func extractProgressSummary(history []agent.ConversationEntry) string {
	var lastAssistantTexts []string
	for i := len(history) - 1; i >= 0 && len(lastAssistantTexts) < 3; i-- {
		if history[i].Role != "assistant" {
			continue
		}
		text, ok := history[i].Content.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			continue
		}
		// Skip the max-rounds marker itself.
		if strings.Contains(trimmed, "已达到最大推理轮次") || strings.Contains(trimmed, "已接近最大推理轮次") {
			continue
		}
		lastAssistantTexts = append(lastAssistantTexts, truncateRunes(trimmed, 150))
	}
	if len(lastAssistantTexts) == 0 {
		return ""
	}
	// Reverse to chronological order.
	for i, j := 0, len(lastAssistantTexts)-1; i < j; i, j = i+1, j-1 {
		lastAssistantTexts[i], lastAssistantTexts[j] = lastAssistantTexts[j], lastAssistantTexts[i]
	}
	return strings.Join(lastAssistantTexts, " → ")
}

type explicitTaskSlotDecision struct {
	ResumeSlotID                string
	StartNewTask                bool
	DismissSlotID               string
	ResumeRecoverableSessionID  string
	DismissRecoverableSessionID string
}

func resolveExplicitTaskSlotDecision(msg IMUserMessage, slot *agent.UnfinishedTaskSlot) explicitTaskSlotDecision {
	decision := explicitTaskSlotDecision{
		ResumeSlotID:                strings.TrimSpace(msg.ResumeSlotID),
		StartNewTask:                msg.StartNewTask,
		DismissSlotID:               strings.TrimSpace(msg.DismissSlotID),
		ResumeRecoverableSessionID:  strings.TrimSpace(msg.ResumeRecoverableSessionID),
		DismissRecoverableSessionID: strings.TrimSpace(msg.DismissRecoverableSessionID),
	}
	if decision.ResumeSlotID != "" && (slot == nil || slot.SlotID != decision.ResumeSlotID) {
		decision.ResumeSlotID = ""
	}
	return decision
}

func buildUnfinishedSlotResumeContext(slot *agent.UnfinishedTaskSlot) string {
	if slot == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## 显式恢复未完成任务\n")
	if slot.LastTask != "" {
		b.WriteString("- 任务: ")
		b.WriteString(slot.LastTask)
		b.WriteString("\n")
	}
	if slot.Summary != "" {
		b.WriteString("- 当前进度: ")
		b.WriteString(slot.Summary)
		b.WriteString("\n")
	}
	if slot.ResumePrompt != "" {
		b.WriteString(slot.ResumePrompt)
		if !strings.HasSuffix(slot.ResumePrompt, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("用户已显式选择继续这个未完成任务。请仅围绕该任务继续，不要混入其他旧任务。\n")
	return b.String()
}

func buildResumeSlotActions(slot *agent.UnfinishedTaskSlot) []IMResponseAction {
	if slot == nil || strings.TrimSpace(slot.SlotID) == "" {
		return nil
	}
	resumeLabel := "继续上次任务"
	if title := strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary)); title != "" {
		resumeLabel = "继续：" + truncateRunes(title, 20)
	}
	return []IMResponseAction{
		{Label: resumeLabel, Command: "__resume_unfinished__ " + slot.SlotID, Style: "default"},
		{Label: "开始新任务", Command: "__start_new_task__", Style: "default"},
		{Label: "放弃旧任务", Command: "__dismiss_unfinished__ " + slot.SlotID, Style: "danger"},
	}
}

func buildUnfinishedSlotHint(slot *agent.UnfinishedTaskSlot) string {
	if slot == nil {
		return ""
	}
	title := strings.TrimSpace(firstNonEmptyTraceText(slot.LastTask, slot.Summary, slot.ProjectPath))
	if title == "" {
		title = "上次未完成任务"
	}
	return "检测到一个未完成任务：" + truncateRunes(title, 60) + "。如需继续，请显式选择“继续上次任务”。"
}

func isSlotActionCommand(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "__resume_unfinished__ ") || trimmed == "__start_new_task__" || strings.HasPrefix(trimmed, "__dismiss_unfinished__ ")
}

func isConfirmationApproval(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	phrases := []string{
		"确认", "确认了", "可以", "可以开始", "开始吧", "继续", "继续吧", "没问题", "好的开始", "就这样", "按这个来",
		"ok", "okay", "confirmed", "confirm", "go ahead", "looks good", "start", "continue",
	}
	for _, phrase := range phrases {
		if lower == phrase || strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func isConfirmationCancel(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	phrases := []string{"取消", "先别做", "不用做了", "停止", "算了", "cancel", "stop", "never mind"}
	for _, phrase := range phrases {
		if lower == phrase || strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// classifyConfirmationIntent uses a lightweight LLM call to classify the
// user's response to an execution confirmation panel. Called when the user's
// message doesn't match the keyword-based isConfirmationApproval or
// isConfirmationCancel checks, to handle semantic confirmations like "开工".
//
// Returns "confirm", "cancel", "modify", or "" (empty on LLM failure,
// treated as modify by the caller).
func (h *IMMessageHandler) classifyConfirmationIntent(userID, text string, pending *pendingConfirmation) string {
	if pending == nil {
		return ""
	}

	// Build context from the pending confirmation.
	ctx := fmt.Sprintf("任务摘要：%s\n", truncateRunes(pending.Summary, 300))
	if len(pending.PlannedActions) > 0 {
		ctx += "计划动作：\n"
		for i, action := range pending.PlannedActions {
			if i >= 5 {
				break
			}
			ctx += fmt.Sprintf("  %d. %s\n", i+1, truncateRunes(action, 100))
		}
	}
	ctx += "系统提示用户：请确认方案或提出修改意见。"

	// Add the last assistant message for conversational context.
	if lastAssistant := h.getLastAssistantSnippet(userID, 300); lastAssistant != "" {
		ctx += fmt.Sprintf("\n助手最后一条消息：%s", lastAssistant)
	}

	userMessage := fmt.Sprintf("[上下文]\n%s\n\n[用户回复]\n%s", ctx, text)

	result, err := h.LLMClassify(context.Background(), LLMClassifyRequest{
		SystemPrompt: `You are a user intent classifier for a task execution confirmation dialog.

The user was shown a task plan and asked to confirm, cancel, or revise it. You will receive:
- The task summary and planned actions
- The assistant's last message (if available): this is what the user is directly responding to
- The user's response

IMPORTANT: Pay close attention to the assistant's last message. If the assistant asked the user to say a specific word/phrase to proceed, and the user replies with that word/phrase, it is a confirmation.

Classify the user's response into exactly one category. Reply with ONLY the category word:
- "confirm" — user approves the plan and wants to start execution. This includes any form of agreement, readiness signal, or go-ahead in the context of the pending task.
- "cancel" — user wants to abandon the task entirely.
- "modify" — user provides specific changes, corrections, or additional requirements for the plan.

When in doubt between "confirm" and "modify", prefer "confirm" if the response is short and doesn't contain specific change requests.`,
		UserMessage: userMessage,
		TimeoutSec:  8,
		Tag:         "confirmation-intent",
	})

	if err != nil {
		log.Printf("[confirmation-intent] LLM classify failed for user %s: %v", userID, err)
		return "" // caller treats as modify (safe fallback)
	}

	intent := strings.ToLower(strings.TrimSpace(result.Text))
	log.Printf("[confirmation-intent] user=%s text=%q → intent=%q (latency=%.1fs)",
		userID, truncateForLogGUI(text, 30), intent, result.Latency.Seconds())

	if strings.Contains(intent, "confirm") {
		return "confirm"
	}
	if strings.Contains(intent, "cancel") {
		return "cancel"
	}
	if strings.Contains(intent, "modify") {
		return "modify"
	}
	return "" // unrecognized → caller treats as modify
}

func isShortChitChatMessage(text string) bool {
	return normalizeShortChitChatToken(text) != ""
}

var shortChitChatEdgePunctuationPattern = regexp.MustCompile(`^[\s"'“”‘’` + "`" + `()（）\[\]【】<>《》,，.。!！?？~～…:：;；、\-—_]+|[\s"'“”‘’` + "`" + `()（）\[\]【】<>《》,，.。!！?？~～…:：;；、\-—_]+$`)
var shortChitChatChineseIdlePattern = regexp.MustCompile(`^(没事|没事了|没有)(啊|呀|啦|呢|吧|哦|喔|哈|哇|嘛|的)?$`)
var shortChitChatChineseThanksPattern = regexp.MustCompile(`^(谢谢)(啊|呀|啦|呢|吧|哦|喔|哈)?$`)
var shortChitChatChineseGreetingPattern = regexp.MustCompile(`^(你好|你好呀|你好啊|嗨|哈喽)(啊|呀|啦|呢|吧|哦|喔|哈)?$`)

func normalizeShortChitChatToken(text string) string {
	cleaned := strings.ToLower(strings.TrimSpace(text))
	if cleaned == "" {
		return ""
	}
	for {
		next := strings.TrimSpace(shortChitChatEdgePunctuationPattern.ReplaceAllString(cleaned, ""))
		if next == cleaned {
			break
		}
		cleaned = next
	}
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if cleaned == "" {
		return ""
	}
	switch {
	case shortChitChatChineseIdlePattern.MatchString(cleaned):
		return "没事"
	case shortChitChatChineseThanksPattern.MatchString(cleaned):
		return "谢谢"
	case shortChitChatChineseGreetingPattern.MatchString(cleaned):
		return "你好"
	}
	shortPhrases := map[string]struct{}{
		"hi":        {},
		"hello":     {},
		"hey":       {},
		"你好":        {},
		"没事":        {},
		"nothing":   {},
		"none":      {},
		"ok":        {},
		"okay":      {},
		"thanks":    {},
		"thank you": {},
		"谢谢":        {},
	}
	if _, ok := shortPhrases[cleaned]; ok {
		return cleaned
	}
	return ""
}

func buildShortChitChatResponse(text, lang string) string {
	normalized := normalizeShortChitChatToken(text)
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(text))
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		switch normalized {
		case "hi", "hello", "hey", "nothing", "none", "ok", "okay", "thanks", "thank you":
			lang = "en"
		default:
			lang = "zh"
		}
	}
	if lang == "en" {
		switch normalized {
		case "thanks", "thank you":
			return "You're welcome. I'm here if you want to continue."
		case "nothing", "none":
			return "No problem. I'm here if you need anything."
		case "ok", "okay":
			return "Okay. I'm here if you need anything."
		default:
			return "Hi! I'm here if you need anything."
		}
	}
	switch normalized {
	case "谢谢":
		return "不客气。我在这，有需要随时说。"
	case "没事", "nothing", "none":
		return "好，没问题。我在这，有需要随时叫我。"
	case "ok", "okay":
		return "好的，我在这。有需要随时说。"
	default:
		return "你好，我在这。有需要随时说。"
	}
}

func shouldRequireExecutionConfirmation(msg IMUserMessage, pending *pendingConfirmation) bool {
	return shouldRequireExecutionConfirmationForIntent(msg, pending, classifyTaskIntent(strings.TrimSpace(msg.Text)))
}

func confirmationTaskLabel(intent taskIntent) string {
	switch intent {
	case intentCoding:
		return "coding"
	case intentSSH:
		return "ssh"
	case intentAmbiguous:
		return "ambiguous"
	default:
		return string(intent)
	}
}

func confirmationPlannedActions(intent taskIntent) []string {
	switch intent {
	case intentCoding:
		return []string{"确认项目目录", "确认任务目标", "确认后开始修改代码"}
	case intentSSH:
		return []string{"确认目标服务器/目录", "确认排查目标", "确认后执行远程操作"}
	case intentAmbiguous:
		return []string{"确认这是改代码还是远程处理", "确认工作目录/目标环境", "确认后再执行"}
	default:
		return []string{"确认任务理解", "确认后开始执行"}
	}
}

func confirmationRiskFlags(intent taskIntent) []string {
	switch intent {
	case intentCoding:
		return []string{"未经确认直接执行可能在错误目录修改代码"}
	case intentSSH:
		return []string{"未经确认直接执行可能连错服务器或操作错环境"}
	case intentAmbiguous:
		return []string{"当前请求同时包含多种执行路径，直接执行容易跑偏"}
	default:
		return nil
	}
}

func confirmationRevisionHints(intent taskIntent) []string {
	switch intent {
	case intentAmbiguous:
		return []string{"补充这是改代码还是 SSH/服务器操作", "补充正确的项目目录或主机信息"}
	default:
		return []string{"如果目录不对，请直接回复正确目录", "如果任务理解不对，请直接回复要修正的点"}
	}
}

func buildPendingConfirmation(app *App, userID, text string, result taskIntentResult, understanding *taskUnderstandingResult) *pendingConfirmation {
	now := time.Now()
	// The confirmation panel's "默认工作目录" must reflect the agent's actual
	// default working directory — i.e. where bash, craft_tool, and other
	// general-purpose tools execute by default. This is the user-configured
	// working directory (AppConfig.WorkingDirectory) if set, otherwise
	// ~/.maclaw/workspace. Using EffectiveWorkspaceDir() aligns the
	// confirmation panel with the bash tool description and the actual
	// execution environment.
	projectPath := corelib.EffectiveWorkspaceDir()
	targetPaths := make([]string, 0, 1)
	if projectPath != "" {
		targetPaths = append(targetPaths, projectPath)
	}

	// --- Summary generation ---
	// If LLM understanding is available, use the structured summary.
	// Otherwise fall back to raw-text echo (previous behavior).
	var summary string
	var enhancedSummary string
	var enhancedInstruction string

	if understanding != nil && strings.TrimSpace(understanding.Summary) != "" {
		enhancedSummary = formatTaskUnderstandingSummary(understanding, projectPath)
		enhancedInstruction = formatEnhancedInstruction(understanding)
		summary = enhancedSummary
	} else {
		// Fallback: raw-text echo (previous behavior).
		summary = fmt.Sprintf("我理解你想让我处理这项任务：%s", strings.TrimSpace(text))
		if projectPath != "" {
			summary += fmt.Sprintf("\n默认工作目录：%s", projectPath)
		}
		if label := strings.TrimSpace(confirmationTaskLabel(result.Intent)); label != "" {
			summary += fmt.Sprintf("\n识别到的任务类型：%s", label)
		}
		if reason := strings.TrimSpace(result.Reason); reason != "" {
			summary += fmt.Sprintf("（原因：%s）", reason)
		} else if ev := strings.TrimSpace(formatIntentEvidence(result)); ev != "" && ev != "未命中特征词" {
			summary += fmt.Sprintf("（依据：%s）", ev)
		}
	}

	plannedActions := confirmationPlannedActions(result.Intent)
	if understanding != nil && len(understanding.ExecutionPlan) > 0 {
		plannedActions = understanding.ExecutionPlan
	}

	return &pendingConfirmation{
		ID:                  fmt.Sprintf("confirm-%d", now.UnixNano()),
		UserID:              userID,
		OriginalText:        strings.TrimSpace(text),
		ResumeText:          strings.TrimSpace(text),
		Summary:             summary,
		TaskType:            confirmationTaskLabel(result.Intent),
		TargetPaths:         targetPaths,
		PlannedActions:      plannedActions,
		RiskFlags:           confirmationRiskFlags(result.Intent),
		RevisionHints:       confirmationRevisionHints(result.Intent),
		Status:              "pending",
		CreatedAt:           now,
		UpdatedAt:           now,
		LastProjectPath:     projectPath,
		EnhancedSummary:     enhancedSummary,
		EnhancedInstruction: enhancedInstruction,
	}
}

func buildConfirmationPayload(item *pendingConfirmation) *IMResponseConfirmation {
	if item == nil {
		return nil
	}
	return &IMResponseConfirmation{
		ID:             item.ID,
		Summary:        item.Summary,
		TaskType:       item.TaskType,
		TargetPaths:    append([]string(nil), item.TargetPaths...),
		PlannedActions: append([]string(nil), item.PlannedActions...),
		RiskFlags:      append([]string(nil), item.RiskFlags...),
		RevisionHints:  append([]string(nil), item.RevisionHints...),
		Status:         item.Status,
	}
}

func buildConfirmationResponse(item *pendingConfirmation) *IMAgentResponse {
	if item == nil {
		return &IMAgentResponse{Text: "请确认后再继续。"}
	}
	// Summary is already shown inside the Confirmation card — only keep the
	// action prompt here to avoid repeating the same content twice.
	text := "请先确认我的理解是否正确。确认后我再开始执行；如果有偏差，直接回复要修改的目录、目标或前提。\n\n请输入：确认 或 修改意见"
	return &IMAgentResponse{
		Text:         text,
		Confirmation: buildConfirmationPayload(item),
		Actions: []IMResponseAction{
			{Label: "确认并开始", Command: "确认，按这个开始", Style: "primary"},
			{Label: "取消", Command: "取消这个任务", Style: "secondary"},
		},
	}
}

func applyConfirmationRevision(item *pendingConfirmation, revision string) *pendingConfirmation {
	if item == nil {
		return nil
	}
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return item
	}
	clone := *item
	clone.ResumeText = strings.TrimSpace(item.OriginalText + "\n\n用户补充/修正：" + revision)
	clone.Summary = item.Summary + "\n用户补充/修正：" + revision
	clone.RevisionHints = append([]string(nil), item.RevisionHints...)
	clone.UpdatedAt = time.Now()
	// Clear enhanced fields — the revision changes the task, so the LLM
	// understanding is stale. confirmationApprovedText will fall back to
	// ResumeText (which includes the revision).
	clone.EnhancedSummary = ""
	clone.EnhancedInstruction = ""
	return &clone
}

func confirmationApprovedText(item *pendingConfirmation) string {
	if item == nil {
		return ""
	}
	// Prefer the LLM-generated enhanced instruction over the raw user text.
	// The enhanced instruction is a structured, actionable rewrite that gives
	// the agent a clearer directive than the user's conversational input.
	// When using the enhanced instruction, append the original text as
	// reference so the agent can cross-check if the LLM missed any details.
	base := ""
	if ei := strings.TrimSpace(item.EnhancedInstruction); ei != "" {
		original := strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
		base = ei
		if original != "" && original != ei {
			base += "\n\n[用户原始请求]\n" + original
		}
	} else {
		base = strings.TrimSpace(firstNonEmptyTraceText(item.ResumeText, item.OriginalText))
	}
	if base == "" {
		return ""
	}

	// Extract "⚠️ 待确认" items from the confirmation summary/constraints.
	// When the LLM task understanding marked items as pending confirmation
	// (e.g. SSH credentials, deployment path), the user clicking "确认并开始"
	// confirms the PLAN but does NOT provide the missing information.
	// We must tell the agent to ask the user for these items before executing.
	pendingItems := extractPendingConfirmItems(item)
	if len(pendingItems) > 0 {
		var pendingSection strings.Builder
		pendingSection.WriteString("\n\n[执行上下文]\n用户已确认执行方案。但以下信息尚未提供，请在执行前先获取：\n")
		for _, pi := range pendingItems {
			pendingSection.WriteString("- " + pi + "\n")
		}
		pendingSection.WriteString("请先尝试通过 memory(action=recall) 从记忆中召回以上信息。如果记忆中没有，再向用户询问。获得全部信息后再开始执行。")
		return strings.TrimSpace(base + pendingSection.String())
	}

	return strings.TrimSpace(base + "\n\n[执行上下文]\n用户已确认当前方案，请直接开始执行，不要再次请求确认。\n如果暂时还没有最终交付，请先说明正在执行的动作或下一步。")
}

// extractPendingConfirmItems scans the confirmation's Summary and Constraints
// for "⚠️ 待确认" markers. These indicate information the LLM flagged as
// missing during task understanding. The user confirmed the plan but did NOT
// provide these values — the agent must ask for them before executing.
func extractPendingConfirmItems(item *pendingConfirmation) []string {
	if item == nil {
		return nil
	}
	var items []string
	seen := make(map[string]bool)

	// Scan all text sources for pending confirmation markers.
	// Only scan Summary and EnhancedSummary (user-facing display text).
	// EnhancedInstruction is the execution directive — scanning it would
	// cause false positives if it mentions "待确认" in a different context.
	sources := []string{item.Summary, item.EnhancedSummary}
	for _, c := range item.RiskFlags {
		sources = append(sources, c)
	}

	for _, src := range sources {
		for _, line := range strings.Split(src, "\n") {
			line = strings.TrimSpace(line)
			// Match "⚠️ 待确认：xxx" or "待确认：xxx" at meaningful positions.
			// Require "待确认" to be preceded by start-of-line, bullet, or ⚠️
			// to avoid matching "确认待确认项" or similar false positives.
			for _, sep := range []string{"⚠️ 待确认：", "⚠️ 待确认:", "待确认：", "待确认:"} {
				if pos := strings.Index(line, sep); pos >= 0 {
					extracted := strings.TrimSpace(line[pos+len(sep):])
					// Strip trailing parenthetical notes like "（建议...）"
					if extracted != "" && !seen[extracted] {
						seen[extracted] = true
						items = append(items, extracted)
					}
					break // only extract once per line
				}
			}
		}
	}
	return items
}

func looksLikeNoToolStallReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	stallHints := []string{
		"我先想想", "先想想", "再想想", "整理一下步骤", "整理步骤", "先整理", "先分析", "先看看", "先确认", "先梳理",
		"let me think", "i'll think", "think first", "organize the steps", "plan this out", "analyze first", "check first",
	}
	for _, hint := range stallHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	// Detect "blocked on one track but intending to continue another" pattern.
	// When the LLM reports a blocker (login required, waiting for approval, etc.)
	// AND also mentions continuing with other work, it should not finalize —
	// the agent loop should force another round so the LLM can proceed with
	// the unblocked subtask via tool calls.
	blockerHints := []string{
		"需要登录", "需要扫码", "需要验证", "需要授权", "需要审批", "等待登录", "等待扫码",
		"requires login", "needs login", "need to log in", "waiting for approval",
	}
	// These hints indicate the LLM intends to work on a different subtask.
	// Deliberately excludes bare "继续" — it often appears in blocker context
	// ("需要登录才能继续") rather than indicating a parallel work track.
	continueHints := []string{
		"同时开始", "同时准备", "同时处理", "先处理其他", "先做其他", "先准备", "先开始",
		"与此同时", "另一方面", "另外先",
		"meanwhile", "in the meantime", "continue with", "proceed with", "at the same time",
	}
	hasBlocker := false
	for _, hint := range blockerHints {
		if strings.Contains(lower, hint) {
			hasBlocker = true
			break
		}
	}
	if hasBlocker {
		for _, hint := range continueHints {
			if strings.Contains(lower, hint) {
				return true
			}
		}
	}
	return false
}

// Compiled regexes for isSubstantivePhaseDocument — package-level for performance.
var (
	substantiveHeadingRe    = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	substantiveNumberedRe   = regexp.MustCompile(`(?m)^(?:\d+[.、])\s*\S`)
	substantiveBulletLineRe = regexp.MustCompile(`(?m)^[-*]\s+\S`)
)

// isSubstantivePhaseDocument checks whether the LLM output constitutes a
// substantive phase document (vs a short transitional preamble).
// It returns true if ANY of the following conditions hold:
// 1. Text is 200+ runes long (sufficient length for a document)
// 2. Text contains Markdown heading markers (# , ## , ### , etc.)
// 3. Text contains numbered list patterns (1. , 2. , 1、, etc.)
// 4. Text contains 3+ bullet list lines (- item or * item)
func isSubstantivePhaseDocument(text string) bool {
	if len([]rune(text)) >= 200 {
		return true
	}
	if substantiveHeadingRe.MatchString(text) {
		return true
	}
	if substantiveNumberedRe.MatchString(text) {
		return true
	}
	if len(substantiveBulletLineRe.FindAllStringIndex(text, 3)) >= 3 {
		return true
	}
	return false
}

func hasFutureDeliveryPromise(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	futureDeliveryHints := []string{
		"马上发你", "稍后发送", "接下来发送", "继续发送", "继续发你", "继续生成", "继续整理",
		"will send", "send it to you shortly", "send you shortly", "about to send", "going to send",
	}
	for _, hint := range futureDeliveryHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func looksLikeCompletedOrSummaryDeliverableReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	completedHints := []string{
		"已完成", "已经完成", "完成了", "已整理", "已经整理", "整理好了", "已为你", "已经为你",
		"结果如下", "总结如下", "以下是", "结论如下", "报告如下", "文档如下", "文字版如下", "这里是",
		"completed", "done", "here is", "here's", "results below", "summary below", "below is",
	}
	hasCompletedHint := false
	for _, hint := range completedHints {
		if strings.Contains(lower, hint) {
			hasCompletedHint = true
			break
		}
	}
	if !hasCompletedHint {
		return false
	}
	if hasFutureDeliveryPromise(trimmed) {
		return false
	}
	return true
}

func looksLikePromiseOnlyDeliverableReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	// Negative patterns: self-introduction / capability-listing context.
	// When the model describes what it *can* do (e.g. "帮你写文档、做整理"),
	// the deliverable keywords appear in a descriptive context, not as an
	// actual promise to deliver a specific file. Skip these.
	selfIntroHints := []string{
		"我叫", "你好，我是", "我的名字", "平时我会", "我可以帮你", "我能帮你",
		"i'm ", "my name is", "i can help you", "nice to meet",
	}
	for _, hint := range selfIntroHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	// "我是" is very common in Chinese; only treat it as self-intro when it
	// appears at the very beginning of the response (first 10 chars).
	if len([]rune(lower)) >= 2 && strings.HasPrefix(lower, "我是") {
		return false
	}

	deliverableHints := []string{
		"pdf", "生成pdf", "生成 pdf", "报告", "文档", "文件", "综述", "发送给你", "发你",
		"report", "document", "file", "send you", "deliver", "summary",
	}
	hasDeliverableIntent := false
	for _, hint := range deliverableHints {
		if strings.Contains(lower, hint) {
			hasDeliverableIntent = true
			break
		}
	}
	if !hasDeliverableIntent {
		if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") {
			hasDeliverableIntent = true
		}
	}
	if !hasDeliverableIntent {
		return false
	}
	promiseHints := []string{
		"我来", "我会", "马上", "立刻", "直接", "继续", "执行", "生成", "发送", "整理", "添加", "补充", "写",
		"i will", "i'll", "let me", "going to", "about to", "right away", "prepare", "generate", "send", "continue", "append",
	}
	hasPromiseHint := false
	for _, hint := range promiseHints {
		if strings.Contains(lower, hint) {
			hasPromiseHint = true
			break
		}
	}
	if !hasPromiseHint {
		return false
	}
	if looksLikeCompletedOrSummaryDeliverableReply(trimmed) {
		return false
	}
	failureHints := []string{"失败", "无法", "出错", "报错", "error", "failed", "unable", "cannot"}
	for _, hint := range failureHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	futureDeliveryPromise := hasFutureDeliveryPromise(trimmed)
	completionHints := []string{
		"已生成", "已保存", "文件已保存", "将发送给用户", "已准备好，将发送给用户", "失败原因", "无法生成", "localfile", "[file_base64|",
		"已完成", "已经完成", "结果如下", "总结如下", "以下是", "here is", "here's", "results below", "summary below",
	}
	for _, hint := range completionHints {
		if strings.Contains(lower, hint) {
			if futureDeliveryPromise {
				break
			}
			return false
		}
	}
	if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") {
		return true
	}
	return true
}

func shouldRecoverForPendingSkillRunNoToolReply(text string, runID string) bool {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return false
	}
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return true
	}
	if looksLikeCompletedOrSummaryDeliverableReply(trimmed) {
		return false
	}
	lower := strings.ToLower(trimmed)
	failureHints := []string{"失败", "无法", "出错", "报错", "error", "failed", "unable", "cannot"}
	for _, hint := range failureHints {
		if strings.Contains(lower, hint) {
			return false
		}
	}
	if looksLikePromiseOnlyDeliverableReply(trimmed) || looksLikeNoToolStallReply(trimmed) {
		return true
	}
	pendingRunContinuationHints := []string{
		"继续添加", "继续补充", "继续整理", "继续写", "继续生成",
		"append more", "continue writing", "continue generating",
	}
	for _, hint := range pendingRunContinuationHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	if strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") {
		return true
	}
	return false
}

func looksLikePromiseOnlyPDFReply(text string) bool {
	trimmed := strings.TrimSpace(stripThinkingTags(text))
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	hasPDFIntent := strings.Contains(lower, "pdf") || strings.Contains(lower, "生成pdf") || strings.Contains(lower, "生成 pdf")
	if !hasPDFIntent {
		return false
	}
	return looksLikePromiseOnlyDeliverableReply(text)
}

func shouldForceAnotherRoundForDeliverable(text string, toolCalls int, pendingFiles int) bool {
	if toolCalls > 0 || pendingFiles > 0 {
		return false
	}
	return looksLikePromiseOnlyDeliverableReply(text)
}

func shouldForceAnotherRoundForPDF(text string, toolCalls int, pendingFiles int) bool {
	if toolCalls > 0 || pendingFiles > 0 {
		return false
	}
	return looksLikePromiseOnlyPDFReply(text)
}

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
	if strings.TrimSpace(resp.LocalFilePath) != "" || len(resp.LocalFilePaths) > 0 || strings.TrimSpace(resp.ThumbnailBase64) != "" {
		return true
	}
	return false
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
	normalizedEcho := normalizeShortChitChatToken(regexp.MustCompile(`^(summary|result|trace summary|结果|摘要)\s*[:：-]?\s*`).ReplaceAllString(lower, ""))
	if normalizedEcho != "" {
		return false
	}
	promptLikeMarkers := []string{
		"当前工作目录",
		"primary working directory",
		"current working directory",
		"project directory",
		"default directory",
		"continue the conversation",
		"resume directly",
		"user:",
		"assistant:",
		"task:",
		"任务：",
		"请帮我",
		"帮我",
		"请实现",
		"请修复",
		"请重建",
		"you are",
	}
	for _, marker := range promptLikeMarkers {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	executionSignals := []string{
		"failed",
		"failure",
		"error",
		"stopped",
		"timeout",
		"cancel",
		"killed",
		"retry",
		"recovered",
		"generated",
		"created",
		"saved",
		"wrote",
		"written",
		"exported",
		"uploaded",
		"downloaded",
		"prepared",
		"delivered",
		"found",
		"produced",
		"执行",
		"失败",
		"错误",
		"超时",
		"取消",
		"停止",
		"重试",
		"恢复",
		"生成",
		"创建",
		"保存",
		"写入",
		"导出",
		"上传",
		"下载",
		"准备",
		"找到",
		"文件",
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
		return "任务未完成可交付结果。可查看 Trace 了解失败位置。"
	case TraceRunStatusCompleted, TraceRunStatusExited:
		if summary != "" {
			return fmt.Sprintf("任务已结束，但没有生成可展示的结果。%s", summary)
		}
		return "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。"
	default:
		if summary != "" {
			return fmt.Sprintf("任务已停止，但没有返回可显示的结果。%s", summary)
		}
		return "任务已停止，但没有返回可显示的结果。可查看 Trace 了解详情。"
	}
}

func buildConfirmedResumeEmptyResultFallback(status TraceRunStatus, traceSummary string) string {
	summary := selectVisibleEmptyResultSummary(traceSummary)
	switch status {
	case TraceRunStatusFailed, TraceRunStatusTimeout, TraceRunStatusCancelled, TraceRunStatusStopped:
		if summary != "" {
			return fmt.Sprintf("任务已确认并开始执行，但未完成可交付结果。%s", summary)
		}
		return "任务已确认并开始执行，但未完成可交付结果。可查看 Trace 了解失败位置。"
	case TraceRunStatusCompleted, TraceRunStatusExited:
		if summary != "" {
			return fmt.Sprintf("已确认并开始执行任务。当前暂无可展示结果。%s", summary)
		}
		return "已确认并开始执行任务。当前暂无可展示结果，可查看 Trace 了解进展。"
	default:
		if summary != "" {
			return fmt.Sprintf("任务已确认并开始执行，但暂未返回可显示结果。%s", summary)
		}
		return "任务已确认并开始执行，但暂未返回可显示结果。可查看 Trace 了解进展。"
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
		{Label: "恢复会话", Command: "__resume_session__ " + sessionID, Style: "default"},
		{Label: "忽略此会话", Command: "__dismiss_recoverable_session__ " + sessionID, Style: "danger"},
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

// toolsCacheTTL is the maximum age of the cached tool definitions.
// When MCP_Registry changes, tools are regenerated within this window.
const toolsCacheTTL = 5 * time.Second

// IMMessageHandler processes IM messages using the local LLM Agent.
// It accesses mcpRegistry and skillExecutor via h.app at call time
// (not captured at construction) to handle late initialization.
//
// Direct fields (workflowEngine, unifiedClassifier, etc.) are extracted
// from App to enable standalone construction for TUI. GUI wires them
// from App at construction time; TUI wires them from its own components.
// See docs/agent-unification-design.md for the full plan.
type IMMessageHandler struct {
	app        *App
	manager    *RemoteSessionManager
	memory     *agent.ConversationMemory
	client     *http.Client // chat-priority HTTP client (optimised transport)
	taskClient *http.Client // background-task HTTP client (separate pool)

	// --- Extracted App dependencies (agent-unification Phase 1) ---
	// These fields are wired from App at construction time (GUI) or from
	// standalone components (TUI). Code should use h.getWorkflowEngine()
	// and h.getUnifiedClassifier() instead of h.app.XXX. The h.app field
	// is retained for not-yet-extracted deps.
	//
	// For GUI: these may be nil at construction (late-init goroutines) and
	// the accessor methods fall through to h.app.XXX as a bridge.
	// For TUI: h.app is nil, these fields are the sole source.

	workflowEngine    *workflow.WorkflowEngine
	unifiedClassifier *intent.UnifiedIntentClassifier
	steeringStore     *steering.Store

	// standaloneConfig holds the config from NewIMMessageHandlerStandalone.
	// nil when constructed via NewIMMessageHandler (GUI mode).
	standaloneConfig *StandaloneConfig

	// Unified tool registry and dynamic builder (Phase 1 upgrade).
	registry    *ToolRegistry
	toolBuilder *DynamicToolBuilder

	// Security firewall (Phase 2 upgrade).
	firewall *SecurityFirewall

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen     *ToolDefinitionGenerator
	toolRouter     *ToolRouter
	usageTracker   *tool.UsageTracker
	taskStore      *task.Store
	cachedTools    []map[string]interface{}
	toolsCacheTime time.Time
	toolsMu        sync.RWMutex

	// Capability gap detection (lazily initialized via setter).
	capabilityGapDetector *CapabilityGapDetector

	// Long-term memory store (lazily initialized via setter).
	memoryStore *memory.Store

	// Pending confirmation store for pre-execution confirmation gating.
	confirmationStore *aiConfirmationStore

	// Session template manager (lazily initialized via setter).
	templateManager *remote.SessionTemplateManager

	// Scheduled task manager (lazily initialized via setter).
	scheduledTaskManager *scheduler.Manager

	traceService *AITraceService

	// Smart session startup components (lazily initialized via setters).
	contextResolver *SessionContextResolver
	sessionPrecheck *SessionPrecheck
	startupFeedback *SessionStartupFeedback

	// Configuration manager (lazily initialized via setter).
	configManager *ConfigManager

	// Dynamic loop limit — set by the "set_max_iterations" tool during an
	// active agent loop. Reset to 0 at the start of each runAgentLoop call.
	// A positive value overrides the configured maxIter for the current loop.
	// NOTE: This field is kept as a legacy bridge alongside currentLoopCtx.
	// Both are kept in sync by toolSetMaxIterations. Will be fully replaced
	// by per-loop LoopContext.MaxIterations once Task 5 routes background
	// loops through bgManager (eliminating shared handler state).
	loopMaxOverride int

	// currentLoopCtx points to the LoopContext of the currently executing
	// runAgentLoop. Used by tools (e.g. set_max_iterations) to interact
	// with the active loop. Set at the start of runAgentLoop, cleared at end.
	currentLoopCtx *LoopContext
	chatLoopMu     sync.Mutex // serializes chat loop execution; prevents overlapping loops

	// Background loop manager and session monitor (lazily initialized via setters).
	bgManager      *BackgroundLoopManager
	sessionMonitor *SessionMonitor

	// SSH session manager (lazily initialized on first SSH tool call).
	sshMgr    *remote.SSHSessionManager
	bgTaskMgr *remote.SSHBackgroundTaskManager

	// Local background task manager for long-running local processes.
	// Mirrors the SSH BackgroundTaskManager pattern: Submit/Check/Wait/Kill.
	localBgTaskMgr *tool.LocalBackgroundTaskManager

	// lastUserText stores the most recent user message text for the current
	// agent loop. Used by toolCreateSession to detect non-coding tasks and
	// prevent unnecessary session creation.
	lastUserText string

	// lastUserID stores the user ID for the current agent loop. Used by
	// context-aware guards (e.g. conversationHasCodingContext) to load the
	// correct conversation history shard.
	lastUserID string

	// imFileSender is an optional callback that forwards a file to the user's
	// IM channels (Feishu/WeChat/etc.) via the Hub WebSocket. Set by the
	// desktop GUI after connecting to the Hub. When nil, IM forwarding is
	// silently skipped.
	imFileSender func(b64Data, fileName, mimeType, message string) error

	// agentActivity is a process-local shared store that lets the GUI AI
	// assistant and IM channels see each other's active tasks.
	agentActivity *AgentActivityStore

	// lastScreenshotAt records the time of the last successful screenshot
	// to enforce a cooldown period and prevent accidental rapid-fire captures.
	lastScreenshotAt time.Time

	// topicDetector automatically detects topic switches and clears stale
	// conversation context so users don't need to manually /new.
	topicDetector *topicSwitchDetector

	// --- First-layer Harness modules (lazily initialized via setters) ---

	// goalAnchor periodically re-injects the original user goal into the
	// LLM context to prevent drift during long-running agent loops.
	goalAnchor *GoalAnchor

	// driftDetector analyzes recent tool_call sequences to detect loop
	// patterns and trigger re-planning when the agent is stuck.
	driftDetector *DriftDetector

	// sessionDriftReplanCount tracks the cumulative drift replan count
	// across agent loops for each user. When a loop exits due to drift
	// (NeedHumanHelp), the replan count is saved here so the next loop
	// inherits it — preventing the detector from re-walking the full
	// "first drift → recover → second drift → human help" cycle.
	// Keyed by userID, value is int.
	sessionDriftReplanCount sync.Map

	// sessionDriftTool tracks the tool name that caused the last drift
	// exit for each user. Injected into the next loop's system prompt
	// so the LLM knows not to repeat the same tool.
	// Keyed by userID, value is string.
	sessionDriftTool sync.Map

	// harnessProgressTracker maintains a structured task checklist that is
	// injected into the LLM context before each iteration.
	harnessProgressTracker *HarnessProgressTracker

	// adaptiveRetry classifies tool_call failures and decides retry
	// strategy, supplementing the existing isRetryableLLMError logic.
	adaptiveRetry *AdaptiveRetry

	trajectoryRecorderFactory func() *TrajectoryRecorder

	// stashedPhasePrompt holds the custom PhasePrompt from HandleInput
	// (e.g. modify requests) so the system-prompt builder can use it
	// instead of rebuilding a generic one. Keyed by userID.
	stashedPhasePrompt sync.Map

	// workflowAgentLoopMarker is set by handleActiveWorkflow when the
	// workflow engine returns RunAgentLoop=true. Consumed (LoadAndDelete)
	// by handleIMMessageWithLoop to enable phase prompt injection and
	// doc capture when the agent loop is running on behalf of the workflow.
	//
	// This marker is set for ALL RunAgentLoop=true responses, including
	// DefaultInput=true (first phase execution). The phase prompt guides
	// the LLM to produce the phase deliverable, and doc capture saves it
	// so the workflow can advance via NeedsConfirm.
	workflowAgentLoopMarker sync.Map

	// workflowPendingConfirmOther is set by handlePendingConfirm when the
	// LLM classifies the user's message as "other" (unrelated to the active
	// workflow's pending confirmation). Consumed (LoadAndDelete) by the
	// agent loop to skip the NeedsConfirm gate — otherwise the unrelated
	// LLM output (e.g. weather query result) would be captured as a phase
	// document and emitted to the doc preview panel.
	workflowPendingConfirmOther sync.Map

	// pendingCriticalConfirm stores response channels for critical-risk
	// skill installation confirmations. Keyed by a unique confirmation ID
	// (string), value is chan criticalRiskConfirmResponse. Cleaned up after
	// use or timeout.
	pendingCriticalConfirm sync.Map

	// pendingCriticalConfirmIM maps "platform:userID" to the active
	// critical-risk confirmation ID. When an IM user responds with
	// "确认安装" or "拒绝安装", handleIMMessageWithLoop checks this map
	// to route the answer to ResolveCriticalConfirm.
	pendingCriticalConfirmIM sync.Map

	// pendingAskUser tracks ask_user questions that are waiting for user
	// responses. When the agent loop returns early due to ask_user, the
	// question is stored here so the next user message can be identified
	// as a response (not a new request) and the context is preserved.
	// Keyed by userID, value is *pendingAskUserState.
	pendingAskUser sync.Map

	// pendingCapabilityGap stores the result of an async capability gap
	// resolution (skill search + install) that completed after the response
	// was already returned to the user. The result is injected into the
	// system prompt of the next conversation turn.
	// Keyed by userID, value is *pendingCapabilityGapResult.
	pendingCapabilityGap sync.Map

	// frozenMemorySnapshots caches the memory section of the system prompt
	// per user. On the first message of a session, the memory section is
	// generated via appendMemorySection and cached. Subsequent system prompt
	// constructions reuse the cached snapshot instead of regenerating,
	// keeping the LLM's KV cache prefix stable.
	// Keyed by userID, value is string (the cached memory section text).
	frozenMemorySnapshots sync.Map

	// snapshotInitialized tracks whether a frozen memory snapshot has been
	// generated for a given user in the current session.
	// Keyed by userID, value is bool.
	snapshotInitialized sync.Map

	// taskOrchestrator manages per-task execution during the coding
	// workflow's Execution Phase. When active, it injects per-task system
	// messages and constructs focused prompts for send_and_observe instead
	// of letting the LLM dump the entire project description at once.
	// Uses a per-user registry to isolate concurrent workflows in maclawsrv.
	taskOrchestratorRegistry *TaskOrchestratorRegistry

	// nudgeTracker manages the post-use skill nudge system per session.
	// It tracks cooldown timing, deduplication, and iteration thresholds
	// to inject low-priority system messages encouraging skill creation
	// or improvement after complex tasks, skill failures, or user corrections.
	// Lazily initialized on first use via ensureNudgeTracker().
	nudgeTracker *nudge.NudgeTracker

	// steeringContextFiles accumulates file paths from tool calls
	// (read_file, write_file, edit_file, etc.) during the current
	// conversation. Used by fileMatch steering resolution.
	// Keyed by "userID\x00filePath" (string), value is bool.
	steeringContextFiles sync.Map

	// pendingInjection stores supplementary messages to inject into the
	// running agent loop. Set by the interrupt handler when a Merge decision
	// is made, consumed by the agent loop at the start of each iteration.
	// Keyed by userID, value is string.
	pendingInjection sync.Map

	// interruptHandler bridges IM gateways to the running agent loop's
	// cancel/merge/status mechanisms. Set during construction.
	interruptHandler *imInterruptHandler

	// activeBtwSubAgent holds the currently running /btw SubAgent (if any).
	// Used by /cancel to cancel a running side query. Stored/cleared
	// atomically by handleBtwCommand.
	activeBtwSubAgent atomic.Pointer[BtwSubAgent]
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	llmCfg := app.GetMaclawLLMConfig()
	responseHeaderTimeout := time.Duration(llmCfg.EffectiveTimeoutSec()) * time.Second
	// Optimised transport for interactive chat — larger connection pool
	// so concurrent requests don't queue behind each other.
	chatTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       20,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true, // 禁止自动 gzip，避免 SSE 流式被压缩缓冲
	}
	// Separate transport for background tasks (scheduled tasks, auto-picked
	// AgentNet tasks) so they never starve the chat connection pool.
	taskTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		MaxIdleConns:          50,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       10,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}

	chatClient := &http.Client{Transport: chatTransport}
	taskClient := &http.Client{Transport: taskTransport}

	h := &IMMessageHandler{
		app:               app,
		manager:           manager,
		memory:            app.ensureConversationMemory(),
		confirmationStore: app.ensureAIConfirmationStore(),
		client:            chatClient,
		taskClient:        taskClient,
		agentActivity:     NewAgentActivityStore(),
		workflowEngine:    app.workflowEngine,
		unifiedClassifier: app.unifiedClassifier,
		steeringStore:     app.steeringStore,
	}
	h.interruptHandler = newIMInterruptHandler(h)
	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Register non-code tools (Git, file search, health check).
	registerNonCodeTools(h.registry, app)
	// Register browser automation tools (CDP-based).
	registerBrowserTools(h.registry, app)
	h.toolBuilder = NewDynamicToolBuilder(h.registry)

	// Initialize automatic topic switch detector.
	h.topicDetector = newTopicSwitchDetector(func() (*http.Client, corelib.MaclawLLMConfig) {
		return h.client, h.getMaclawLLMConfig()
	})

	// Initialize task execution orchestrator for per-task coding workflow.
	h.taskOrchestratorRegistry = NewTaskOrchestratorRegistry()
	if h.sessionPrecheck != nil {
		h.taskOrchestratorRegistry.SetExternalChecker(&sessionPrecheckAdapter{precheck: h.sessionPrecheck})
	}

	// Initialize the nudge tracker for post-use skill nudge system.
	h.nudgeTracker = nudge.NewNudgeTracker()

	return h
}

// ensureNudgeTracker returns the existing NudgeTracker or creates a new one
// if it hasn't been initialized yet (defensive lazy init).
func (h *IMMessageHandler) ensureNudgeTracker() *nudge.NudgeTracker {
	if h.nudgeTracker == nil {
		h.nudgeTracker = nudge.NewNudgeTracker()
	}
	return h.nudgeTracker
}

// isNudgeDisabled checks whether the nudge system is disabled via config.
// Returns true if nudges should be suppressed.
func (h *IMMessageHandler) isNudgeDisabled() bool {
	if h.app == nil {
		return false
	}
	cfg, err := h.loadConfig()
	if err != nil {
		return false
	}
	return cfg.NudgeDisabled
}

// userCorrectionKeywords are keywords that indicate the user is correcting
// the LLM's approach. Used for user correction nudge detection.
var userCorrectionKeywords = []string{
	// English
	"instead", "not like that", "wrong", "incorrect", "no,", "actually",
	"should be", "use this", "try this", "do it this way", "that's wrong",
	"fix", "correct",
	// Chinese
	"不对", "错了", "应该", "不是这样", "换一种", "改成", "用这个", "试试这个",
	"这样做", "纠正", "修正",
}

// containsCorrectionKeywords checks if the user message contains any
// correction keywords that suggest the user is correcting the LLM's approach.
func containsCorrectionKeywords(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, kw := range userCorrectionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasRecentFailedToolCall checks if the conversation history has a failed
// tool call in the recent entries (last 6 entries). A failed tool call is
// identified by error-like content in a tool result message.
func hasRecentFailedToolCall(history []agent.ConversationEntry) bool {
	// Scan the last 6 entries for a tool result that looks like a failure.
	start := len(history) - 6
	if start < 0 {
		start = 0
	}
	for i := start; i < len(history); i++ {
		entry := history[i]
		if entry.Role != "tool" {
			continue
		}
		content, ok := entry.Content.(string)
		if !ok {
			continue
		}
		lower := strings.ToLower(content)
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") ||
			strings.Contains(lower, "失败") || strings.Contains(lower, "错误") {
			return true
		}
	}
	return false
}

// wasSkillRecentlyRepaired checks if a skill was auto-repaired within the
// last 5 minutes by examining its persisted RepairHistory. This uses
// persisted data (not the one-shot ConsumeRepairNotifications) to avoid
// competing with appendSkillRepairNotifications for the same data.
func (h *IMMessageHandler) wasSkillRecentlyRepaired(skillName string) bool {
	if h.getSkillExecutor() == nil {
		return false
	}
	for _, s := range h.getSkillExecutor().loadSkills() {
		if s.Name == skillName && s.RepairAttemptCount > 0 && s.LastRepairAt != "" {
			if t, err := time.Parse(time.RFC3339, s.LastRepairAt); err == nil {
				return time.Since(t) < 5*time.Minute
			}
		}
	}
	return false
}

// injectNudgeMessages checks nudge conditions and appends appropriate system
// messages to the conversation history. Nudges are injected AFTER the current
// response is delivered — they go into the conversation history for the NEXT
// LLM call, not the current one.
//
// Parameters:
//   - history: the conversation history to append nudge messages to
//   - iteration: the current agent loop iteration count
//   - totalToolCallsInLoop: total number of tool calls across all iterations
//   - phase: the current agent loop phase (for workaround detection)
//   - userText: the current user message text (for correction detection)
//
// Returns the (possibly extended) history.
func (h *IMMessageHandler) injectNudgeMessages(
	history []agent.ConversationEntry,
	iteration int,
	totalToolCallsInLoop int,
	phase agentLoopPhase,
	userText string,
) []agent.ConversationEntry {
	if h.isNudgeDisabled() {
		return history
	}
	tracker := h.ensureNudgeTracker()

	// 1. Complex task nudge: ≥5 tool calls in this loop.
	if totalToolCallsInLoop >= 5 {
		event := nudge.NudgeEvent{
			Type:           nudge.ComplexTask,
			ToolCallCount:  totalToolCallsInLoop,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected ComplexTask nudge: toolCalls=%d iteration=%d", totalToolCallsInLoop, iteration)
			}
		}
	}

	// 2. Skill failure workaround nudge: skill failed + LLM resolved via alternative tools.
	//    Coordinates with self-repair: checks the skill's persisted repair history
	//    to determine if self-repair was recently attempted (within 5 minutes).
	//    This avoids competing with appendSkillRepairNotifications for the
	//    one-shot ConsumeRepairNotifications data.
	if phase.FailedSkillName != "" {
		selfRepairAttempted := h.wasSkillRecentlyRepaired(phase.FailedSkillName)
		event := nudge.NudgeEvent{
			Type:                nudge.SkillFailureWorkaround,
			SkillName:           phase.FailedSkillName,
			SelfRepairAttempted: selfRepairAttempted,
			IterationCount:      iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected SkillFailureWorkaround nudge: skill=%q selfRepairAttempted=%v iteration=%d",
					phase.FailedSkillName, selfRepairAttempted, iteration)
			}
		}
	}

	// 3. User correction nudge: user message following a failed tool call
	//    with correction keywords.
	if containsCorrectionKeywords(userText) && hasRecentFailedToolCall(history) {
		event := nudge.NudgeEvent{
			Type:           nudge.UserCorrection,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected UserCorrection nudge: iteration=%d", iteration)
			}
		}
	}

	return history
}

// SetToolRegistry replaces the tool registry (for testing or late reconfiguration).
func (h *IMMessageHandler) SetToolRegistry(r *ToolRegistry) {
	h.registry = r
	h.toolBuilder = NewDynamicToolBuilder(r)
}

// SetSecurityFirewall configures the security firewall for tool execution checks.
func (h *IMMessageHandler) SetSecurityFirewall(fw *SecurityFirewall) {
	h.firewall = fw
}

// SetToolDefGenerator configures the dynamic tool definition generator.
// When set, it replaces the hardcoded buildToolDefinitions() output.
func (h *IMMessageHandler) SetToolDefGenerator(gen *ToolDefinitionGenerator) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolDefGen = gen
	// Invalidate cache so next call regenerates.
	h.cachedTools = nil
	h.toolsCacheTime = time.Time{}
}

// SetCapabilityGapDetector configures the capability gap detector and wires
// the confirmCallback so that CapabilityGapDetector.Resolve uses the shared
// confirmCriticalRiskSkill mechanism for critical-risk user confirmation.
func (h *IMMessageHandler) SetCapabilityGapDetector(detector *CapabilityGapDetector) {
	h.capabilityGapDetector = detector
	if detector != nil {
		detector.SetConfirmCallback(func(skillName, riskDetails string) bool {
			// Determine the platform from the active loop context.
			platform := ""
			if h.currentLoopCtx != nil {
				platform = h.currentLoopCtx.Platform
			}
			// Extract factors from riskDetails for the shared confirmation function.
			// The riskDetails string is pre-formatted by the detector; pass it as a
			// single-element factors slice so buildCriticalRiskPrompt includes it.
			factors := []string{riskDetails}
			return h.confirmCriticalRiskSkill(
				context.Background(), skillName, "capability_gap_auto", factors, platform, h.lastUserID,
			)
		})
	}
}

// SetToolRouter configures the tool router for context-aware tool filtering.
func (h *IMMessageHandler) SetToolRouter(router *ToolRouter) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolRouter = router
	// Wire the registry into the router so it can dynamically resolve
	// builtin tool names and use tags for TF-IDF scoring.
	if router != nil && h.registry != nil {
		router.SetRegistry(h.registry)
	}
}

// SetContextResolver configures the session context resolver for auto-detecting
// project paths and recommending tools.
func (h *IMMessageHandler) SetContextResolver(resolver *SessionContextResolver) {
	h.contextResolver = resolver
}

// SetUsageTracker configures the tool usage tracker for outcome recording.
func (h *IMMessageHandler) SetUsageTracker(tracker *tool.UsageTracker) {
	h.usageTracker = tracker
}

// SetSessionPrecheck configures the session precheck for environment validation.
func (h *IMMessageHandler) SetSessionPrecheck(precheck *SessionPrecheck) {
	h.sessionPrecheck = precheck
	// Keep orchestrator's external tool checker in sync.
	if h.taskOrchestratorRegistry != nil && precheck != nil {
		h.taskOrchestratorRegistry.SetExternalChecker(&sessionPrecheckAdapter{precheck: precheck})
	}
}

// SetStartupFeedback configures the startup feedback monitor.
func (h *IMMessageHandler) SetStartupFeedback(feedback *SessionStartupFeedback) {
	h.startupFeedback = feedback
}

// SetConfigManager configures the configuration manager for config tools.
func (h *IMMessageHandler) SetConfigManager(cm *ConfigManager) {
	h.configManager = cm
}

// SetMemoryStore configures the long-term memory store.
func (h *IMMessageHandler) SetMemoryStore(ms *memory.Store) {
	h.memoryStore = ms
}

// SetConfirmationStore configures the pending confirmation store.
func (h *IMMessageHandler) SetConfirmationStore(store *aiConfirmationStore) {
	h.confirmationStore = store
}

func (h *IMMessageHandler) SetTraceService(trace *AITraceService) {
	h.traceService = trace
}

func (h *IMMessageHandler) traceContextResolver() *SessionContextResolver {
	if h.contextResolver != nil {
		return h.contextResolver
	}
	if h.app != nil {
		_ = h.getContextResolver() // ensure
		return h.getContextResolver()
	}
	return nil
}

// SetTemplateManager configures the session template manager.
func (h *IMMessageHandler) SetTemplateManager(tm *remote.SessionTemplateManager) {
	h.templateManager = tm
}

// SetScheduledTaskManager configures the scheduled task manager.
func (h *IMMessageHandler) SetScheduledTaskManager(stm *scheduler.Manager) {
	h.scheduledTaskManager = stm
}

// SetBackgroundLoopManager configures the background loop manager.
func (h *IMMessageHandler) SetBackgroundLoopManager(blm *BackgroundLoopManager) {
	h.bgManager = blm
}

// SetSessionMonitor configures the session monitor.
func (h *IMMessageHandler) SetSessionMonitor(sm *SessionMonitor) {
	h.sessionMonitor = sm
}

// SetIMFileSender configures the callback used to forward files to the user's
// IM channels (Feishu/WeChat/etc.) when the agent is running on the desktop.
func (h *IMMessageHandler) SetIMFileSender(fn func(b64Data, fileName, mimeType, message string) error) {
	h.imFileSender = fn
}

// SetGoalAnchor configures the goal anchoring module for the agent loop.
func (h *IMMessageHandler) SetGoalAnchor(ga *GoalAnchor) {
	h.goalAnchor = ga
}

// SetDriftDetector configures the drift detection module for the agent loop.
func (h *IMMessageHandler) SetDriftDetector(dd *DriftDetector) {
	h.driftDetector = dd
}

// SetHarnessProgressTracker configures the progress tracking module for the agent loop.
func (h *IMMessageHandler) SetHarnessProgressTracker(pt *HarnessProgressTracker) {
	h.harnessProgressTracker = pt
}

// SetAdaptiveRetry configures the adaptive retry module for the agent loop.
func (h *IMMessageHandler) SetAdaptiveRetry(ar *AdaptiveRetry) {
	h.adaptiveRetry = ar
}

func (h *IMMessageHandler) SetTrajectoryRecorderFactory(factory func() *TrajectoryRecorder) {
	h.trajectoryRecorderFactory = factory
}

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	var tools []map[string]interface{}

	// --- Phase 1 upgrade: prefer DynamicToolBuilder from ToolRegistry ---
	// Note: We use BuildAll() here intentionally — context-aware filtering
	// is handled downstream by routeTools() / ToolRouter which uses TF-IDF.
	// DynamicToolBuilder.Build(msg) is an alternative path for simpler setups
	// without ToolRouter.
	if h.toolBuilder != nil && h.registry != nil {
		h.toolsMu.RLock()
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			tools = cached
		} else {
			// Sync dynamic tools (AgentNet, SkillHub) only on cache rebuild, not every call.
			h.syncAgentNetTools()
			h.syncSkillHubTools()

			tools = h.toolBuilder.BuildAll()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	} else {
		// --- Legacy path: ToolDefinitionGenerator or hardcoded ---
		h.toolsMu.RLock()
		gen := h.toolDefGen
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		// Fallback: no generator configured — use hardcoded definitions.
		if gen == nil {
			tools = h.buildToolDefinitions()
		} else if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			// Return cached tools if still fresh (within 5 seconds).
			tools = cached
		} else {
			// Regenerate from the generator.
			tools = gen.Generate()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	}

	// In lite/simple mode (UIMode != "pro"), filter out coding session tools
	// since the user has not configured coding LLM providers. This removes
	// the tool definitions entirely so they are never sent to the LLM,
	// saving tokens and preventing the agent from attempting coding sessions.
	if !h.isProMode() {
		tools = filterCodingTools(tools)
	}

	return tools
}

// routeTools applies the ToolRouter to filter tools based on user message.
// If no router is configured, returns allTools unchanged.
//
// Tool selection (including conditional activation of ssh, browser, etc.)
// is fully handled by Route() via conditionalKeepRules + sessionTools.
// This function does not apply any additional per-tool filtering.
func (h *IMMessageHandler) routeTools(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	h.toolsMu.RLock()
	router := h.toolRouter
	h.toolsMu.RUnlock()

	if router == nil {
		return allTools
	}
	return router.Route(userMessage, allTools)
}

// syncSkillHubTools registers the search_and_install_skill tool when a
// SkillMarket (HubCenter) is reachable, giving the LLM the ability to
// proactively search the SkillMarket and install skills during a session.
func (h *IMMessageHandler) syncSkillHubTools() {
	if h.registry == nil {
		return
	}
	// The tool is available as long as we have an App (which provides HubCenter URL).
	hasApp := h.app != nil
	_, hasSearchTool := h.registry.Get("search_and_install_skill")

	if hasApp && !hasSearchTool {
		h.registry.Register(RegisteredTool{
			Name: "search_and_install_skill",
			Description: "在 SkillMarket 技能市场中搜索并安装技能。当你发现现有工具无法满足用户需求时，" +
				"主动调用此工具搜索可用的 Skill，找到后自动安装并执行。" +
				"Search and install a skill from SkillMarket when existing tools cannot fulfill the request.",
			Category: ToolCategoryBuiltin,
			Tags:     []string{"skill", "skillmarket", "install", "search", "capability"},
			Status:   RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词，描述你需要的能力（如 '生成PDF'、'发送邮件'、'图片处理'）"},
			},
			Required: []string{"query"},
			HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
				return h.toolSearchAndInstallSkill(args, onProgress)
			},
		})
	} else if !hasApp && hasSearchTool {
		h.registry.Unregister("search_and_install_skill")
	}
}

// toolSearchAndInstallSkill handles the search_and_install_skill tool call.
// Search order: SkillMarket (HubCenter) → ClawHub mirror → GitHub.
// If a match is found, it downloads and registers the skill locally.
func (h *IMMessageHandler) toolSearchAndInstallSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "错误: 缺少 query 参数"
	}

	sendStatus := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	ctx := context.Background()

	smClient := NewSkillMarketClient(h.app)
	searcher := NewSkillSearcher(smClient)

	sendStatus("🔍 正在搜索 SkillMarket...")
	best, err := searcher.SearchAndInstall(ctx, query)
	if err != nil {
		return fmt.Sprintf("搜索 SkillMarket 失败: %v", err)
	}
	if best == nil {
		return fmt.Sprintf("在 SkillMarket、ClawHub 和 GitHub 上均未找到与 %q 匹配的 Skill", query)
	}

	sendStatus(fmt.Sprintf("📦 找到 Skill: %s — %s (来源: %s)", best.Name, best.Description, best.Status))

	return h.installAndExecuteSkill(ctx, best, query, sendStatus)
}

// installAndExecuteSkill handles the download, security review, registration,
// and execution of a found skill. Shared by both active (tool call) and
// passive (capability gap) paths.
func (h *IMMessageHandler) installAndExecuteSkill(ctx context.Context, best *SkillSearchResult, query string, sendStatus func(string)) string {
	// GitHub result → import via a stable install ref when available.
	if best.Status == "github" {
		var imported *corelib.NLSkillEntry
		if strings.TrimSpace(best.InstallRef) != "" {
			var candidate cskill.GitHubSkillCandidate
			if err := json.Unmarshal([]byte(best.InstallRef), &candidate); err == nil && strings.TrimSpace(candidate.RawURL) != "" {
				imported, err = cskill.NewGitHubSearcher("").ImportFromCandidate(candidate)
			}
		}
		if imported == nil {
			gs := cskill.NewGitHubSearcher("")
			candidates, err := gs.SearchGitHub(best.ID)
			if err != nil || len(candidates) == 0 {
				return fmt.Sprintf("找到 GitHub Skill「%s」但导入失败", best.Name)
			}
			imported, err = gs.ImportFromCandidate(candidates[0])
			if err != nil {
				return fmt.Sprintf("GitHub Skill 导入失败: %v", err)
			}
		}
		imported.Source = "auto_github"
		return h.registerAndExecuteSkill(ctx, imported, best.Name, "auto_github", sendStatus)
	}

	// SkillMarket or ClawHub result → download and register locally.
	sendStatus(fmt.Sprintf("⬇️ 正在安装: %s ...", best.Name))

	if best.Status == "clawhub" {
		// ClawHub skills are SKILL.md-based knowledge guides, not step-based
		// automation. Download the SKILL.md content and register as a local skill.
		skill, dlErr := downloadClawHubSkill(ctx, best.ID)
		if dlErr != nil {
			return fmt.Sprintf("🔎 找到 ClawHub Skill「%s」但下载失败: %v", best.Name, dlErr)
		}
		skill.Source = "auto_clawhub"
		return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_clawhub", sendStatus)
	}

	// SkillMarket result: download through the HubCenter failover pool.
	skill, dlErr := downloadSkillJSONFromHubCenter(ctx, h.app, "/api/v1/skills/"+url.PathEscape(best.ID)+"/download")
	if dlErr != nil {
		return fmt.Sprintf("Found skill %s but download failed: %v", best.Name, dlErr)
	}
	skill.Source = "auto_hub"
	return h.registerAndExecuteSkill(ctx, skill, best.Name, "auto_hub", sendStatus)
}

// downloadClawHubSkill fetches a skill from the ClawHub mirror and converts
// the SKILL.md content into an NLSkillEntry with a single craft_tool step
// that uses the SKILL.md as instructions.
// Delegates to the shared corelib/skill.HubClient.
func downloadClawHubSkill(ctx context.Context, slug string) (*corelib.NLSkillEntry, error) {
	client := cskill.DefaultHubClient()
	return client.DownloadClawHub(ctx, slug)
}

// downloadSkillJSON fetches a skill definition from the given URL and
// converts it to an NLSkillEntry ready for local registration.
// Handles both step-based skills (steps array) and file-based skills
// (files map with SKILL.md in base64). All bundled files are extracted
// to ~/.maclaw/data/skills/<name>/.
func downloadSkillJSON(ctx context.Context, endpoint string) (*corelib.NLSkillEntry, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "MaClaw/1.0")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var full struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Triggers    []string          `json:"triggers"`
		TrustLevel  string            `json:"trust_level"`
		Version     string            `json:"version"`
		Steps       []json.RawMessage `json:"steps"`
		Files       map[string]string `json:"files"` // path → base64 content
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 5<<20)).Decode(&full); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	var steps []corelib.NLSkillStep

	if len(full.Steps) > 0 {
		for _, raw := range full.Steps {
			var s struct {
				Action  string                 `json:"action"`
				Params  map[string]interface{} `json:"params"`
				OnError string                 `json:"on_error"`
			}
			if err := json.Unmarshal(raw, &s); err == nil && s.Action != "" {
				steps = append(steps, corelib.NLSkillStep{Action: s.Action, Params: s.Params, OnError: s.OnError})
			}
		}
	}

	// Extract all bundled files to ~/.maclaw/data/skills/<name>/.
	installSkillDir := ""
	if len(full.Files) > 0 && full.Name != "" {
		extractSkillFiles(full.Name, full.Files)
		if skillsRoot, err := cskill.PrimarySkillsDir(); err == nil {
			installSkillDir = filepath.Join(skillsRoot, full.Name)
		}
	}

	if len(steps) == 0 {
		steps = craftToolStepsFromBundledSkillFiles(full.Files, installSkillDir)
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("skill %s has no steps and no SKILL.md", full.Name)
	}

	// Skills from the configured hub (official store) are treated as "trusted".
	trustLevel := full.TrustLevel
	if trustLevel == "" || trustLevel == "community" {
		trustLevel = "trusted"
	}

	return &corelib.NLSkillEntry{
		Name:        full.Name,
		Description: full.Description,
		Triggers:    full.Triggers,
		Steps:       steps,
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "hub",
		HubSkillID:  full.ID,
		HubVersion:  full.Version,
		TrustLevel:  trustLevel,
	}, nil
}

// extractSkillFiles decodes base64-encoded files and writes them to
// ~/.maclaw/data/skills/<skillName>/, preserving subdirectory structure.
func extractSkillFiles(skillName string, files map[string]string) {
	skillsRoot, err := cskill.PrimarySkillsDir()
	if err != nil {
		log.Printf("[skill-install] cannot determine skills dir: %v", err)
		return
	}
	skillDir := filepath.Join(skillsRoot, skillName)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		log.Printf("[skill-install] cannot create %s: %v", skillDir, err)
		return
	}

	for relPath, b64Content := range files {
		data, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			log.Printf("[skill-install] decode %s: %v", relPath, err)
			continue
		}

		// Sanitize path — prevent directory traversal.
		clean := filepath.ToSlash(filepath.Clean(relPath))
		if strings.Contains(clean, "..") || filepath.IsAbs(relPath) || strings.HasPrefix(clean, "/") {
			log.Printf("[skill-install] skipping unsafe path: %s", relPath)
			continue
		}

		dest := filepath.Join(skillDir, filepath.FromSlash(clean))
		if !strings.HasPrefix(dest, skillDir+string(filepath.Separator)) && dest != skillDir {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			continue
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			log.Printf("[skill-install] write %s: %v", dest, err)
		}
	}
	log.Printf("[skill-install] extracted %d files to %s", len(files), skillDir)
}

// registerAndExecuteSkill registers a skill locally, runs security review,
// executes it, and returns the result string.
func (h *IMMessageHandler) registerAndExecuteSkill(ctx context.Context, skill *corelib.NLSkillEntry, displayName, source string, sendStatus func(string)) string {
	if h.getSkillExecutor() == nil {
		return fmt.Sprintf("找到 Skill「%s」但 SkillExecutor 未初始化", displayName)
	}

	// Security review via RiskAssessor if available.
	if h.getRiskAssessor() != nil {
		sendStatus("🔒 正在进行安全审查...")
		assessment := h.getRiskAssessor().AssessSkill(skill, skill.TrustLevel)
		if assessment.Level == security.RiskCritical {
			// Determine the platform for the confirmation channel.
			platform := ""
			if h.currentLoopCtx != nil {
				platform = h.currentLoopCtx.Platform
			}

			confirmed := h.confirmCriticalRiskSkill(
				ctx, displayName, source, assessment.Factors, platform, h.lastUserID,
			)

			if !confirmed {
				// User rejected (or timeout / fail-closed).
				_ = h.getAuditLog() // ensure
				if h.getAuditLog() != nil {
					_ = h.getAuditLog().Log(security.AuditEntry{
						Timestamp:    time.Now(),
						Action:       security.AuditActionHubSkillReject,
						ToolName:     source + "_skill_install",
						RiskLevel:    security.RiskCritical,
						PolicyAction: security.PolicyDeny,
						Result:       fmt.Sprintf("user rejected critical skill %s: critical risk", displayName),
					})
				}
				return fmt.Sprintf("⚠️ Skill「%s」包含高风险操作，已拒绝自动安装。风险因素: %s",
					displayName, strings.Join(assessment.Factors, ", "))
			}

			// User confirmed — audit with PolicyUserOverride and continue to registration.
			_ = h.getAuditLog() // ensure
			if h.getAuditLog() != nil {
				_ = h.getAuditLog().Log(security.AuditEntry{
					Timestamp:    time.Now(),
					Action:       security.AuditActionHubSkillInstall,
					ToolName:     source + "_skill_install",
					RiskLevel:    security.RiskCritical,
					PolicyAction: security.PolicyUserOverride,
					Result: fmt.Sprintf("user confirmed critical skill %s from %s, risk=critical, factors=[%s]",
						displayName, source, strings.Join(assessment.Factors, ", ")),
				})
			}
		}
	}

	sendStatus(fmt.Sprintf("📝 正在注册 Skill: %s ...", skill.Name))
	if err := h.getSkillExecutor().Register(*skill); err != nil {
		return fmt.Sprintf("注册 Skill「%s」失败: %v", displayName, err)
	}

	// Refresh skill BM25 index so the router picks up the new skill.
	if h.getAppToolRouter() != nil {
		h.getAppToolRouter().RefreshSkillIndex()
	}

	// Audit log.
	_ = h.getAuditLog() // ensure
	if h.getAuditLog() != nil {
		_ = h.getAuditLog().Log(security.AuditEntry{
			Timestamp:    time.Now(),
			Action:       security.AuditActionHubSkillInstall,
			ToolName:     source + "_skill_install",
			RiskLevel:    security.RiskLow,
			PolicyAction: security.PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s from %s", displayName, source),
		})
	}

	sendStatus(fmt.Sprintf("▶️ 正在执行 Skill: %s ...", skill.Name))
	execResult, execErr := h.getSkillExecutor().Execute(skill.Name)
	if execErr != nil {
		log.Printf("[skill-auto] execute skill %s failed: %v", skill.Name, execErr)
		return fmt.Sprintf("⚠️ Skill「%s」已安装，但执行失败: %v", skill.Name, execErr)
	}
	return fmt.Sprintf("✅ 已自动安装并执行 Skill「%s」\n%s", skill.Name, execResult)
}

// syncAgentNetTools dynamically registers or unregisters AgentNet tools
// based on whether the AgentNet daemon is currently running.
func (h *IMMessageHandler) syncAgentNetTools() {
	if h.registry == nil {
		return
	}
	running := h.getAgentNetClient() != nil && h.getAgentNetClient().IsRunning()
	_, hasSearch := h.registry.Get("agentnet_search")

	if running && !hasSearch {
		h.registry.Register(RegisteredTool{
			Name:        "agentnet_search",
			Description: "在智网（AgentNet P2P 知识网络）中搜索知识条目。返回匹配的知识列表，包含标题、内容、作者等。",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"agentnet", "search", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词"},
			},
			Required: []string{"query"},
			Source:   "agentnet",
			Handler:  func(args map[string]interface{}) string { return h.toolAgentNetSearch(args) },
		})
		h.registry.Register(RegisteredTool{
			Name:        "agentnet_publish",
			Description: "向智网（AgentNet P2P 知识网络）发布一条知识条目。发布后其他节点可以搜索到。",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"agentnet", "publish", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"title": map[string]string{"type": "string", "description": "知识标题"},
				"body":  map[string]string{"type": "string", "description": "知识内容（Markdown 格式）"},
			},
			Required: []string{"title", "body"},
			Source:   "agentnet",
			Handler:  func(args map[string]interface{}) string { return h.toolAgentNetPublish(args) },
		})
	} else if !running && hasSearch {
		h.registry.Unregister("agentnet_search")
		h.registry.Unregister("agentnet_publish")
	}
}

// WarmupTools pre-builds and caches the tool definitions so the first user
// message does not pay the cost of syncAgentNetTools + BuildAll.
// Safe to call from a background goroutine.
func (h *IMMessageHandler) WarmupTools() {
	allTools := h.getTools()
	if h.toolRouter != nil {
		_ = h.routeTools("warmup", allTools)
		log.Println("[WarmupTools] tool routing cache pre-warmed")
	}
}

// WarmupHTTPConn sends a lightweight probe request to the configured LLM
// endpoint so the underlying TCP+TLS connection is established and pooled
// before the first real chat request.
func (h *IMMessageHandler) WarmupHTTPConn() {
	cfg := h.getMaclawLLMConfig()
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if baseURL == "" {
		return
	}
	key := strings.TrimSpace(cfg.Key)
	ua := cfg.UserAgent()
	endpoint := baseURL + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", ua)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := h.client.Do(req)
	if err != nil {
		log.Printf("[Warmup] HTTP connection warmup failed: %v", err)
		return
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	log.Printf("[Warmup] HTTP connection warmed up (status=%d)", resp.StatusCode)
}

func (h *IMMessageHandler) runTraceStatus(ctx *LoopContext, result *IMAgentResponse) TraceRunStatus {
	if ctx == nil {
		return TraceRunStatusRunning
	}
	if result != nil && result.Error != "" {
		return TraceRunStatusFailed
	}
	switch state := ctx.State(); state {
	case "completed":
		return TraceRunStatusCompleted
	case "failed":
		return TraceRunStatusFailed
	case "stopped":
		return TraceRunStatusCancelled
	case "timeout":
		return TraceRunStatusTimeout
	case "paused":
		return TraceRunStatusPaused
	default:
		return traceStatusFromLoopState(state)
	}
}

func (h *IMMessageHandler) finalizeTraceResult(ctx *LoopContext, resp *IMAgentResponse, summary, errText string) *IMAgentResponse {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
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
			h.appendTraceEvidence(ctx, "trial_reflect_summary", "decision", "trial-reflect summary", view.TrialReflectSummary.StrategyNote, "", "")
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

func extractBrowserRootCause(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "浏览器") && !strings.Contains(lower, "cdp") && !strings.Contains(lower, "debug") {
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

func (h *IMMessageHandler) saveConversationHistoryTimed(userID string, history []agent.ConversationEntry, resp *IMAgentResponse) {
	startedAt := time.Now()

	// Build optional callbacks only when trimming will actually occur.
	var summarizer func(string) string
	var memorySink func(string, []string)

	if len(history) > agent.MaxConversationTurns {
		// LLM summarizer for dropped entries (Phase 7).
		if h.app != nil {
			cfg := h.app.GetMaclawLLMConfig()
			if cfg.URL != "" && cfg.Model != "" {
				summarizer = makeSummarizer(cfg, &http.Client{Timeout: 15 * time.Second})
			}
		}
		// Memory sink for substantial dropped assistant messages (Phase 1 supplement).
		if h.memoryStore != nil {
			memorySink = func(content string, tags []string) {
				entry := memory.Entry{
					Content:  content,
					Category: memory.CategoryTaskArtifact,
					Tags:     tags,
					Scope:    memory.ScopeProject,
				}
				_ = h.memoryStore.Save(entry)
			}
		}
	}

	h.memory.Save(userID, trimHistoryWithSummary(history, summarizer, memorySink))
	if resp != nil {
		resp.MemorySaveNanos = time.Since(startedAt).Nanoseconds()
	}

	// Persist transcript to FTS5 session search store (non-blocking).
	h.persistSessionTranscriptAsync(userID, history)
}

// persistSessionTranscriptAsync converts the conversation history to a
// session.TranscriptEntry slice, serializes it, extracts a topic, and
// persists the document to the FTS5 session search store. Runs in a
// goroutine to avoid blocking the agent loop. Errors are logged but
// do not fail the main flow.
func (h *IMMessageHandler) persistSessionTranscriptAsync(userID string, history []agent.ConversationEntry) {
	if len(history) == 0 {
		return
	}

	// Copy history to avoid data races with the caller.
	historyCopy := make([]agent.ConversationEntry, len(history))
	copy(historyCopy, history)

	persist := func() {
		if h == nil || h.app == nil {
			return
		}
		store, err := session.NewStore(h.getSessionSearchDBPath())
		if err != nil {
			log.Printf("[session_search] failed to open store: %v", err)
			return
		}
		defer func() { _ = store.Close() }()

		entries := conversationToTranscriptEntries(historyCopy)
		if len(entries) == 0 {
			return
		}

		fullText := session.Serialize(entries)
		if strings.TrimSpace(fullText) == "" {
			return
		}

		topic := session.ExtractTopic(fullText)

		// Derive session ID from userID + current timestamp.
		sessionID := fmt.Sprintf("%s_%d", userID, time.Now().UnixNano())

		doc := session.SessionDocument{
			SessionID: sessionID,
			Timestamp: time.Now(),
			Platform:  "gui",
			Topic:     topic,
			FullText:  fullText,
		}

		if err := store.Persist(doc); err != nil {
			log.Printf("[session_search] persist failed: %v", err)
		}
	}

	if h != nil && h.app != nil && strings.TrimSpace(h.getTestHomeDir()) != "" {
		persist()
		return
	}

	go persist()
}

// conversationToTranscriptEntries converts GUI conversation entries to the
// corelib session.TranscriptEntry format for serialization and FTS5 indexing.
func conversationToTranscriptEntries(history []agent.ConversationEntry) []session.TranscriptEntry {
	var entries []session.TranscriptEntry
	for _, e := range history {
		te := session.TranscriptEntry{
			Role: e.Role,
		}

		// Extract content string.
		switch v := e.Content.(type) {
		case string:
			te.Content = v
		default:
			// For non-string content (e.g. multimodal arrays), marshal to JSON.
			if v != nil {
				b, err := json.Marshal(v)
				if err == nil {
					te.Content = string(b)
				}
			}
		}

		// Extract tool call metadata.
		if e.ToolCalls != nil {
			te.ToolCalls = extractToolCallMeta(e.ToolCalls)
		}

		// Set tool call ID for tool result entries.
		if e.ToolCallID != "" {
			te.ToolCallID = e.ToolCallID
		}

		entries = append(entries, te)
	}
	return entries
}

// extractToolCallMeta converts the raw ToolCalls interface (typically
// []interface{} from JSON) into []session.ToolCallMeta for serialization.
func extractToolCallMeta(raw interface{}) []session.ToolCallMeta {
	if raw == nil {
		return nil
	}

	// Try as []interface{} (common from JSON unmarshaling).
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	var metas []session.ToolCallMeta
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		tc := session.ToolCallMeta{}
		if id, ok := m["id"].(string); ok {
			tc.ID = id
		}
		if fn, ok := m["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"].(string); ok {
				tc.Name = name
			}
			if args, ok := fn["arguments"].(string); ok {
				tc.Args = args
			}
		}
		if tc.ID != "" || tc.Name != "" {
			metas = append(metas, tc)
		}
	}
	return metas
}

func (h *IMMessageHandler) appendTraceEvent(ctx *LoopContext, kind, severity, title, summary, relatedFile, command string) {
	if ctx == nil || h.traceService == nil || ctx.RunID == "" {
		return
	}
	h.traceService.AppendEvent(ctx.RunID, TraceEvent{
		Kind:        kind,
		Severity:    severity,
		Title:       firstNonEmptyTraceText(title, kind),
		Summary:     summary,
		RelatedFile: relatedFile,
		Command:     command,
		ProjectPath: h.traceProjectPath(),
		CreatedAt:   traceNowMillis(),
	})
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
	return strings.Contains(strings.ToLower(summary.StrategyNote), "repeat guard")
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
	if h.traceService == nil {
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
		if item.SourceKind == "trial_reflect_summary" {
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
	if activeSlot := h.memory.ActiveUnfinishedSlot(userID); activeSlot != nil {
		return buildUnfinishedSlotResumeContext(activeSlot) + h.buildTraceEvidencePrompt(userID, activeSlot.LastTask)
	}
	return h.buildTraceEvidencePrompt(userID, fallbackTask)
}

func traceCategoryForToolResult(toolName, result string) string {
	lower := strings.ToLower(result)
	switch {
	case strings.Contains(lower, "失败") || strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return "error"
	case toolName == "create_session":
		return "result"
	case strings.Contains(lower, "文件") || strings.Contains(lower, "saved"):
		return "file"
	default:
		return "event"
	}
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

// HandleIMMessage processes an IM user message and returns the Agent's response.
func (h *IMMessageHandler) HandleIMMessage(msg IMUserMessage) *IMAgentResponse {
	return h.HandleIMMessageWithProgress(msg, nil)
}

// HandleIMMessageWithProgress processes an IM message with an optional progress
// callback. When onProgress is non-nil, the agent loop sends intermediate status
// updates (e.g. "正在执行 bash 命令…") so the Hub can relay them to the user
// and reset the response timeout — preventing 504 on long-running tasks.
func (h *IMMessageHandler) HandleIMMessageWithProgress(msg IMUserMessage, onProgress tool.ProgressCallback) *IMAgentResponse {
	return h.HandleIMMessageWithProgressAndStream(msg, onProgress, nil, nil, nil)
}

// HandleIMMessageWithProgressAndStream extends HandleIMMessageWithProgress with
// streaming support for the desktop AI assistant. When onToken is non-nil, each
// LLM text delta is pushed in real-time. When onNewRound is non-nil, it is called
// at the start of each new agent loop iteration (after the first) so the frontend
// can create a new message bubble. IM platforms pass nil for both.
func (h *IMMessageHandler) StartDesktopBackgroundTask(text, projectPath string) (*AIAssistantBackgroundTaskResult, error) {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return &AIAssistantBackgroundTaskResult{
			Accepted: false,
			Mode:     "background",
			Error:    "empty task text",
		}, nil
	}
	if h.manager == nil {
		return nil, fmt.Errorf("remote session manager not initialized")
	}
	loopCtx := NewLoopContext(fmt.Sprintf("ai-bg-%d", time.Now().UnixNano()), h.getMaclawAgentMaxIterations(), h.taskClient)
	loopCtx.Platform = "desktop"
	if h.traceService != nil {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, trimmedText, "desktop", "desktop-user", strings.TrimSpace(projectPath))
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI 后台任务已接收", truncateTraceText(trimmedText, 180), "", "")
	}
	title := truncateRunes(trimmedText, 72)
	session := h.manager.CreateAIBackgroundSession(title, projectPath, loopCtx)
	if session == nil {
		return nil, fmt.Errorf("failed to create background AI session")
	}
	if h.traceService != nil && loopCtx.RunID != "" {
		h.traceService.SetRunSessionID(loopCtx.RunID, session.ID)
	}
	go h.runDesktopBackgroundTask(session.ID, loopCtx, trimmedText, strings.TrimSpace(projectPath))
	return &AIAssistantBackgroundTaskResult{
		Accepted:  true,
		Mode:      "background",
		SessionID: session.ID,
		JobID:     session.JobID,
		RunID:     session.RunID,
	}, nil
}

func (h *IMMessageHandler) runDesktopBackgroundTask(sessionID string, loopCtx *LoopContext, text, _ string) {
	msg := IMUserMessage{
		UserID:             "desktop-user",
		Platform:           "desktop",
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
			s.Summary.Status = string(SessionBusy)
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
			s.Summary.Status = string(SessionError)
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
			s.Summary.Status = string(SessionExited)
			s.Summary.Severity = "warn"
			s.Summary.LastResult = "Canceled"
			s.Summary.ProgressSummary = "任务已取消"
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
		s.Summary.Status = string(SessionExited)
		s.Summary.Severity = "info"
		s.Summary.WaitingForUser = false
		s.Summary.ProgressSummary = firstNonEmptyTraceText(resultText, "任务已完成")
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

func (h *IMMessageHandler) HandleIMMessageWithExistingLoop(msg IMUserMessage, loopCtx *LoopContext, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) *IMAgentResponse {
	return h.handleIMMessageWithLoop(msg, loopCtx, onProgress, onToken, onNewRound, onStreamDone)
}

func (h *IMMessageHandler) HandleIMMessageWithProgressAndStream(msg IMUserMessage, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) *IMAgentResponse {
	return h.handleIMMessageWithLoop(msg, nil, onProgress, onToken, onNewRound, onStreamDone)
}

func (h *IMMessageHandler) handleIMMessageWithLoop(msg IMUserMessage, providedLoopCtx *LoopContext, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) (result *IMAgentResponse) {
	trimmed := strings.TrimSpace(msg.Text)

	// --- IM Audit: deferred write covers ALL return paths ---
	// Record both the user message and the assistant response for IM platforms.
	// Uses named return `result` so the deferred closure captures the final value
	// regardless of which return path is taken.
	isIMAuditPlatform := h.app != nil && msg.Platform != "" && msg.Platform != "desktop" && msg.Platform != "tui"
	if isIMAuditPlatform && trimmed != "" {
		defer func() {
			store := h.app.getIMAuditStore()
			if store == nil {
				return
			}
			// Record user message.
			store.Write(IMAuditMessage{
				UserID:   msg.UserID,
				Platform: msg.Platform,
				Role:     "user",
				Content:  msg.Text,
			})
			// Record assistant response.
			if result != nil {
				content := result.Text
				if content == "" {
					content = result.Error
				}
				if content != "" {
					store.Write(IMAuditMessage{
						UserID:   msg.UserID,
						Platform: msg.Platform,
						Role:     "assistant",
						Content:  content,
					})
				}
			}
		}()
	}

	// Invalidate UIC cache after each message processing cycle completes,
	// ensuring all consumers within the same cycle share the same result
	// but the next message gets a fresh classification.
	if uic := h.getUnifiedClassifier(); uic != nil {
		defer uic.InvalidateCache()
	}

	// Slash commands are processed before the LLM config check — they don't
	// need LLM and must always work so users can manage state even when LLM
	// is misconfigured.
	if trimmed == "/new" || trimmed == "/reset" || trimmed == "/clear" {
		h.memory.Clear(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		if h.confirmationStore != nil {
			h.confirmationStore.clear(msg.UserID)
		}
		// Flush evidence batch and reset session on conversation reset.
		h.flushEvidenceOnSessionEnd(msg.UserID)
		// Clean up workflow and understanding state.
		h.cancelWorkflowForUser(msg.UserID)
		resp := &IMAgentResponse{Text: "对话已重置。"}
		if h.currentLoopCtx != nil {
			return h.finalizeTraceResult(h.currentLoopCtx, resp, resp.Text, "")
		}
		return resp
	}
	// Skip chit-chat interception when there's a pending ask_user question,
	// because short responses like "ok"/"好的" are valid answers to ask_user.
	hasPendingAskUser := false
	if _, loaded := h.pendingAskUser.Load(msg.UserID); loaded {
		hasPendingAskUser = true
	}

	// Intercept IM responses to critical-risk skill installation confirmations.
	// When a pending confirmation exists for this platform+user, match the user's
	// reply to resolve it and return immediately.
	if msg.Platform != "" && msg.Platform != "desktop" {
		imConfirmKey := msg.Platform + ":" + msg.UserID
		if v, ok := h.pendingCriticalConfirmIM.LoadAndDelete(imConfirmKey); ok {
			confirmID, _ := v.(string)
			if confirmID != "" {
				lower := strings.TrimSpace(strings.ToLower(trimmed))
				confirmed := lower == "确认安装" || lower == "确认" || lower == "1"
				h.ResolveCriticalConfirm(confirmID, confirmed)
				if confirmed {
					return &IMAgentResponse{Text: "✅ 已确认安装该 Critical 风险 Skill。"}
				}
				return &IMAgentResponse{Text: "❌ 已拒绝安装该 Critical 风险 Skill。"}
			}
		}
	}

	if !msg.IsBackground && len(msg.Attachments) == 0 && isShortChitChatMessage(trimmed) && !hasPendingAskUser {
		return &IMAgentResponse{Text: buildShortChitChatResponse(trimmed, msg.Lang)}
	}
	if trimmed == "/exit" || trimmed == "/quit" {
		return h.handleExitCommand(msg.UserID)
	}
	if trimmed == "/sessions" || trimmed == "/status" {
		return h.handleSessionsCommand()
	}
	if trimmed == "/compress" {
		return h.handleCompressCommand(msg.UserID)
	}
	if trimmed == "/help" {
		return &IMAgentResponse{Text: "📖 可用命令:\n" +
			"/new /reset — 重置对话\n" +
			"/btw <查询> — 侧查询（不打断当前任务上下文）\n" +
			"/compress — 压缩当前对话历史\n" +
			"/cancel /取消 — 取消当前正在执行的任务\n" +
			"/exit /quit — 终止所有会话，退出编程模式\n" +
			"/sessions — 查看当前会话状态\n" +
			"/help — 显示此帮助"}
	}
	// --- /btw side query: independent agent loop for quick lookups ---
	// Runs in a clean context (no workflow, no 40+ tools). Results are
	// displayed in the chat UI but not appended to the main history.
	// Desktop panel: reached via SendBtwQuery binding (bypasses buffer queue).
	// IM channels: reached via this code path in handleIMMessageWithLoop.
	if strings.HasPrefix(trimmed, "/btw ") || trimmed == "/btw" {
		btwQuery := ""
		if len(trimmed) > 5 {
			btwQuery = strings.TrimSpace(trimmed[5:])
		}
		if btwQuery == "" {
			return &IMAgentResponse{Text: "用法: /btw <查询内容>\n\n示例:\n  /btw 最新的 Go 1.23 有什么新特性\n  /btw React 19 的主要变化\n  /btw 这个项目用了什么框架"}
		}
		if !h.isMaclawLLMConfigured() {
			return &IMAgentResponse{Error: "LLM 未配置，无法执行 /btw 查询。"}
		}
		return h.handleBtwCommand(msg, btwQuery, onProgress, onToken)
	}
	if trimmed == "/cancel" || trimmed == "/取消" {
		// Cancel active workflow if any.
		h.cancelWorkflowForUser(msg.UserID)
		if h.confirmationStore != nil {
			if pending := h.confirmationStore.get(msg.UserID); pending != nil {
				h.confirmationStore.clear(msg.UserID)
				return &IMAgentResponse{Text: "⏹️ 已取消待确认任务。"}
			}
		}
		// Cancel active /btw side query if any.
		if btw := h.activeBtwSubAgent.Load(); btw != nil {
			btw.Cancel()
			return &IMAgentResponse{Text: "⏹️ 已取消 /btw 侧查询。"}
		}
		ctx := h.currentLoopCtx
		if ctx == nil {
			return &IMAgentResponse{Text: "ℹ️ 当前没有正在执行的任务。"}
		}
		taskText := h.lastUserText
		ctx.Cancel()
		// Don't wait for the loop to exit — the IM caller shouldn't block.
		// The loop will detect cancellation and clean up on its own.
		cancelMsg := "⏹️ 任务已取消。"
		if preview := truncateRunes(taskText, 30); preview != "" {
			cancelMsg = fmt.Sprintf("⏹️ 已取消任务「%s」。", preview)
		}
		return &IMAgentResponse{Text: cancelMsg}
	}

	// Select HTTP client: background tasks use a separate connection pool
	// so they never block interactive chat requests.
	httpClient := h.client
	if msg.IsBackground {
		httpClient = h.taskClient
	}
	if h.confirmationStore != nil {
		h.confirmationStore.clearExpired(time.Now())
	}

	EntriesBeforeClear := h.memory.Load(msg.UserID)
	unfinishedSlot := h.memory.GetUnfinishedSlot(msg.UserID)

	// --- Recover interrupted task from in-flight marker ---
	// If the previous agent loop was interrupted by a process kill (e.g.,
	// updater restart), the in-flight marker persists on disk. Consume it
	// and promote to an UnfinishedTaskSlot so the existing resume machinery
	// handles it — no keyword matching needed, works regardless of what the
	// user's next message says.
	if unfinishedSlot == nil && !msg.IsBackground {
		if interruptedTask := h.memory.ConsumeInFlightTask(msg.UserID); interruptedTask != "" {
			slotID := fmt.Sprintf("interrupted-%d", time.Now().UnixMilli())
			slot := &agent.UnfinishedTaskSlot{
				SlotID:       slotID,
				UserID:       msg.UserID,
				Status:       "interrupted",
				LastTask:     interruptedTask,
				Summary:      extractProgressSummary(EntriesBeforeClear),
				ResumePrompt: "上一次任务执行过程中应用被中断（可能因更新重启）。对话历史已恢复。请基于对话历史中已完成的工作继续执行，不要重复已完成的步骤。\n",
				Source:       "in_flight_recovery",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}
			h.memory.UpsertUnfinishedSlot(msg.UserID, slot)
			h.memory.BindUnfinishedSlot(msg.UserID, slotID)
			unfinishedSlot = slot
			log.Printf("[InFlightRecovery] recovered interrupted task for user %s: %q", msg.UserID, truncateRunes(interruptedTask, 80))
		}
	}

	decision := resolveExplicitTaskSlotDecision(msg, unfinishedSlot)
	freshTask := false
	if h.app != nil && h.getSessionStarter() == nil {
		h.ensureInteractionInfra()
	}
	if decision.DismissSlotID != "" {
		h.memory.DismissUnfinishedSlot(msg.UserID, decision.DismissSlotID)
		unfinishedSlot = nil
		decision.ResumeSlotID = ""
		freshTask = true
	}
	if decision.DismissRecoverableSessionID != "" && h.manager != nil {
		h.manager.SuppressResumeForSession(decision.DismissRecoverableSessionID)
		decision.ResumeRecoverableSessionID = ""
		freshTask = true
		return &IMAgentResponse{Text: "已忽略该恢复会话。"}
	}
	if decision.ResumeRecoverableSessionID != "" && h.manager != nil {
		session, ok := h.manager.Get(decision.ResumeRecoverableSessionID)
		if ok && session != nil {
			var resumeSessionID, projectPath, tool string
			session.mu.RLock()
			if session.ResumeContext != nil {
				resumeSessionID = strings.TrimSpace(session.ResumeContext.ResumeSessionID)
				projectPath = strings.TrimSpace(firstNonEmptyTraceText(session.ProjectPath, session.ResumeContext.ProjectPath))
				tool = strings.TrimSpace(firstNonEmptyTraceText(session.Tool, session.ResumeContext.Tool))
			}
			session.mu.RUnlock()
			if resumeSessionID != "" && h.app != nil {
				_, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
					Tool:               tool,
					ProjectPath:        projectPath,
					LaunchSource:       RemoteLaunchSourceAI,
					ResumeSessionID:    resumeSessionID,
					InjectResumePrompt: false,
				})
				if err != nil {
					return &IMAgentResponse{Error: fmt.Sprintf("恢复会话失败: %v", err)}
				}
				h.manager.SuppressResumeForSession(decision.ResumeRecoverableSessionID)
				return &IMAgentResponse{Text: "已启动恢复会话。请到远程会话列表继续查看执行状态。"}
			}
		}
		return &IMAgentResponse{Error: "当前没有可恢复的会话，或该会话不支持恢复。"}
	}

	if !h.isMaclawLLMConfigured() {
		return &IMAgentResponse{
			Error: "MaClaw LLM 未配置，无法处理请求。请在 MaClaw 客户端的设置中配置 LLM。",
		}
	}

	// --- Workflow engine interception ---
	// Route messages through the workflow engine before the main agent loop.
	// This handles active workflows, intent understanding, and complex task detection.
	//
	// workflowAgentLoop tracks whether the workflow engine explicitly routed
	// this message to the agent loop (RunAgentLoop=true). When true, the
	// agent loop is running ON BEHALF of the workflow and its output should
	// be captured as phase output. When false, the agent loop is running for
	// a normal (non-workflow) message and doc capture must be skipped — even
	// if a stale workflow happens to be active in memory.
	workflowAgentLoop := false
	skipWorkflowForAttachment := false
	if h.getWorkflowEngine() != nil && !msg.IsBackground {
		// Skip workflow interception when the user sends image attachments
		// with a short text prompt (likely an image recognition request, not
		// a workflow phase input). Without this, the workflow engine would
		// hijack the message and inject a PhasePrompt (e.g. "generate tech
		// design document"), causing the LLM to ignore the image entirely.
		hasImageAttachment := false
		for _, att := range msg.Attachments {
			if att.Type == "image" || att.Type == "file" {
				hasImageAttachment = true
				break
			}
		}
		skipWorkflowForAttachment = hasImageAttachment && len([]rune(trimmed)) < 50

		if !skipWorkflowForAttachment {
			if wfResp := h.handleWorkflowInterception(msg.UserID, trimmed); wfResp != nil {
				// Consume any pending ask_user state so it doesn't leak into
				// the next message when the workflow engine handles this one
				// (e.g., user clicks "确认" which matches confirmWords).
				h.pendingAskUser.Delete(msg.UserID)
				return wfResp
			}
			// handleWorkflowInterception returned nil — either the message is not
			// a workflow message, or the workflow engine set RunAgentLoop=true.
			// Check the marker set by handleActiveWorkflow.
			if _, ok := h.workflowAgentLoopMarker.LoadAndDelete(msg.UserID); ok {
				workflowAgentLoop = true
			}
		}
	}

	// Check if handlePendingConfirm classified this message as "other"
	// (unrelated to the active workflow). If so, the agent loop should
	// skip workflow-engine-specific gates (NeedsConfirm phase capture,
	// doc_only tool filtering) to prevent unrelated LLM output from
	// being captured as a workflow phase document.
	//
	// NOTE: This does NOT bypass the Coding Tool Gate — when the message
	// is a new coding task, gateConfig.active controls that independently.
	_, skipNeedsConfirmGate := h.workflowPendingConfirmOther.LoadAndDelete(msg.UserID)

	// Also skip workflow gates when the attachment bypass was triggered —
	// the message is unrelated to the workflow (image recognition, etc.)
	// and should not be subject to doc_only tool filtering.
	if skipWorkflowForAttachment {
		skipNeedsConfirmGate = true
	}

	if decision.StartNewTask {
		h.memory.ClearConversationAndDismissSlot(msg.UserID)
		h.clearPerUserSessionState(msg.UserID)
		EntriesBeforeClear = nil
		unfinishedSlot = nil
		freshTask = true
	} else if decision.ResumeSlotID != "" {
		if h.memory.BindUnfinishedSlot(msg.UserID, decision.ResumeSlotID) {
			unfinishedSlot = h.memory.ActiveUnfinishedSlot(msg.UserID)
		}
	} else {
		if !msg.IsBackground && shouldAutoClearIncompleteTaskContext(trimmed, EntriesBeforeClear) {
			h.memory.ClearConversationAndDismissSlot(msg.UserID)
			h.clearPerUserSessionState(msg.UserID)
			log.Printf("[IMMessageHandler] auto-cleared unfinished task context for user %s", msg.UserID)
			EntriesBeforeClear = nil
			unfinishedSlot = nil
			freshTask = true
		}
	}

	// --- Pending ask_user response detection ---
	// If the previous assistant message was an ask_user question, the current
	// user message is a response to that question (not a new request).
	// Consume the pending state and build context for the system prompt.
	var askUserContext string
	if raw, ok := h.pendingAskUser.LoadAndDelete(msg.UserID); ok {
		pending := raw.(*pendingAskUserState)
		// Expire after 30 minutes to avoid stale state.
		if time.Since(pending.Timestamp) < 30*time.Minute {
			askUserContext = fmt.Sprintf(
				"【上下文提示】用户正在回答你之前提出的确认问题，而非发起新请求。\n你的问题：%s\n用户回答：%s\n请基于当前任务上下文理解用户意图，将其视为补充或修改意见。",
				pending.Question, trimmed,
			)
			log.Printf("[AskUser] consumed pending ask_user for user %s, question=%q, answer=%q", msg.UserID, truncateRunes(pending.Question, 50), truncateRunes(trimmed, 50))
		}
	}

	// --- Pending capability gap result injection ---
	// If a background skill search/install completed since the last turn,
	// inject the result into the system prompt so the LLM knows about the
	// newly available capability.
	var capabilityGapContext string
	if raw, ok := h.pendingCapabilityGap.LoadAndDelete(msg.UserID); ok {
		pending := raw.(*pendingCapabilityGapResult)
		// Expire after 10 minutes to avoid stale state.
		if time.Since(pending.Timestamp) < 10*time.Minute {
			if pending.Success {
				capabilityGapContext = fmt.Sprintf(
					"[系统通知] 上一轮对话后，系统在后台自动搜索并安装了 Skill「%s」。你现在可以使用这个新能力来帮助用户。安装结果：%s",
					pending.SkillName, pending.Result,
				)
				log.Printf("[CapabilityGap] injecting async skill install result for user %s: skill=%s", msg.UserID, pending.SkillName)
			} else {
				log.Printf("[CapabilityGap] discarding failed async skill install for user %s: skill=%s result=%s", msg.UserID, pending.SkillName, truncateRunes(pending.Result, 100))
			}
		}
	}

	// --- Automatic topic switch detection ---
	// For interactive (non-background) messages, detect if the user has
	// switched topics and auto-clear stale conversation context.
	// Skip detection when the user is responding to an ask_user question.
	if !msg.IsBackground && h.topicDetector != nil && len(EntriesBeforeClear) > 0 && !hasIncompleteTaskMarker(EntriesBeforeClear) && decision.ResumeSlotID == "" && !decision.StartNewTask && askUserContext == "" {
		if h.topicDetector.detect(trimmed, msg.UserID, h.memory) == TopicNew {
			// Archive a one-line summary before clearing.
			if h.memoryStore != nil {
				entries := h.memory.Load(msg.UserID)
				if summary := buildQuickSummary(entries); summary != "" {
					_ = h.memoryStore.Save(memory.Entry{
						Content:  summary,
						Category: "conversation_summary",
					})
				}
			}
			h.memory.Clear(msg.UserID)
			h.clearPerUserSessionState(msg.UserID)
			log.Printf("[TopicDetector] auto-cleared context for user %s", msg.UserID)
		}
	}

	// --- Background routing: delegate to BackgroundLoopManager ---
	if msg.IsBackground && h.bgManager != nil && providedLoopCtx == nil {
		slotKind := parseSlotKind(msg.BackgroundSlotKind)
		maxIter := h.getMaclawAgentMaxIterations()
		if msg.MinIterations > maxIter {
			maxIter = msg.MinIterations
		}

		loopCtx, waitC := h.bgManager.SpawnOrQueue(slotKind, msg.UserID, msg.Text, maxIter)
		if loopCtx == nil && waitC != nil {
			// Slot full — block until a slot opens.
			loopCtx = <-waitC
		}
		if loopCtx == nil {
			return &IMAgentResponse{Error: "后台任务启动失败：无法获取执行槽位"}
		}
		loopCtx.HTTPClient = httpClient
		if h.traceService != nil && loopCtx.RunID == "" {
			job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
			loopCtx.JobID = job.JobID
			loopCtx.RunID = run.RunID
			h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
			h.appendTraceEvent(loopCtx, "request.accepted", "info", "后台任务已接收", truncateTraceText(msg.Text, 180), "", "")
		}

		var systemPrompt string
		history := h.memory.Load(msg.UserID)
		history = h.compactHistory(history, httpClient)
		if h.memoryStore != nil {
			systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0)
		} else {
			systemPrompt = h.buildSystemPrompt()
		}
		if activeSlot := h.memory.ActiveUnfinishedSlot(msg.UserID); activeSlot != nil {
			systemPrompt += buildUnfinishedSlotResumeContext(activeSlot)
			systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, activeSlot.LastTask)
		} else {
			systemPrompt += h.buildTraceEvidencePrompt(msg.UserID, msg.Text)
		}
		// Desktop AI assistant panel: override PDF instructions with Markdown preview.
		if msg.Platform == "desktop" {
			systemPrompt += desktopWorkflowDocOverride()
		} else if msg.Platform != "" {
			// IM channels: enforce PDF delivery for all workflow phase documents.
			systemPrompt += imWorkflowDocDeliveryRule()
		}

		result := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, nil, nil, nil, msg.MinIterations, msg.Platform)

		// --- Evidence collection hook (background loop path) ---
		h.runEvidenceCollection(msg.UserID, msg.Text)

		// Mark loop as completed/failed and dequeue next.
		if result != nil && result.Error != "" {
			loopCtx.SetState("failed")
		} else {
			loopCtx.SetState("completed")
		}
		summaryText := ""
		errText := ""
		if result != nil {
			summaryText = firstNonEmptyTraceText(result.Text, result.TraceSummary)
			errText = result.Error
		}
		result = h.finalizeTraceResult(loopCtx, result, summaryText, errText)
		h.bgManager.Complete(loopCtx.ID)
		return result
	}

	pending := (*pendingConfirmation)(nil)
	confirmedResume := false
	if h.confirmationStore != nil {
		pending = h.confirmationStore.get(msg.UserID)
	}
	if pending != nil {
		switch {
		case isConfirmationCancel(trimmed):
			// Preserve the cancelled task's original text as context so
			// subsequent messages don't lose the conversation thread.
			if pending.OriginalText != "" {
				entries := h.memory.Load(msg.UserID)
				cancelNote := fmt.Sprintf("（用户取消了该任务的执行确认，原始请求：%s）", truncateRunes(pending.OriginalText, 200))
				entries = append(entries,
					agent.ConversationEntry{Role: "user", Content: pending.OriginalText},
					agent.ConversationEntry{Role: "assistant", Content: cancelNote},
				)
				h.memory.Save(msg.UserID, entries)
			}
			h.confirmationStore.clear(msg.UserID)
			return &IMAgentResponse{Text: "⏹️ 已取消待确认任务。"}
		case isConfirmationApproval(trimmed):
			h.confirmationStore.clear(msg.UserID)
			msg.Text = confirmationApprovedText(pending)
			trimmed = strings.TrimSpace(msg.Text)
			confirmedResume = true
			// Re-route through workflow engine with the restored original text.
			// The first workflow interception (above) ran with the confirmation
			// message ("确认并开始"), not the original task text. Now that we've
			// restored the original text, give the workflow engine another chance
			// to classify and intercept it.
			if h.getWorkflowEngine() != nil && !msg.IsBackground {
				if wfResp := h.handleWorkflowInterception(msg.UserID, trimmed); wfResp != nil {
					h.pendingAskUser.Delete(msg.UserID)
					return wfResp
				}
				// Check marker from the re-routed workflow interception.
				if _, ok := h.workflowAgentLoopMarker.LoadAndDelete(msg.UserID); ok {
					workflowAgentLoop = true
				}
			}
		case !msg.IsBackground:
			// Neither explicit confirm nor cancel. Use a lightweight LLM call
			// to classify the user's intent with the confirmation context.
			// This handles cases like "开工" which is semantically a confirmation
			// but not in the keyword list.
			if llmIntent := h.classifyConfirmationIntent(msg.UserID, trimmed, pending); llmIntent == "confirm" {
				h.confirmationStore.clear(msg.UserID)
				msg.Text = confirmationApprovedText(pending)
				trimmed = strings.TrimSpace(msg.Text)
				confirmedResume = true
				if h.getWorkflowEngine() != nil {
					if wfResp := h.handleWorkflowInterception(msg.UserID, trimmed); wfResp != nil {
						h.pendingAskUser.Delete(msg.UserID)
						return wfResp
					}
					if _, ok := h.workflowAgentLoopMarker.LoadAndDelete(msg.UserID); ok {
						workflowAgentLoop = true
					}
				}
			} else if llmIntent == "cancel" {
				if pending.OriginalText != "" {
					entries := h.memory.Load(msg.UserID)
					cancelNote := fmt.Sprintf("（用户取消了该任务的执行确认，原始请求：%s）", truncateRunes(pending.OriginalText, 200))
					entries = append(entries,
						agent.ConversationEntry{Role: "user", Content: pending.OriginalText},
						agent.ConversationEntry{Role: "assistant", Content: cancelNote},
					)
					h.memory.Save(msg.UserID, entries)
				}
				h.confirmationStore.clear(msg.UserID)
				return &IMAgentResponse{Text: "⏹️ 已取消待确认任务。"}
			} else {
				// "modify" or LLM failed — treat as revision.
				updated := applyConfirmationRevision(pending, trimmed)
				h.confirmationStore.set(updated)
				return buildConfirmationResponse(updated)
			}
		}
	}
	if shouldRequireExecutionConfirmation(msg, pending) {
		intent := h.classifyTaskIntentForExecution(trimmed, msg.Attachments, httpClient)
		if shouldRequireExecutionConfirmationForIntent(msg, pending, intent) {
			// Attempt LLM-based task understanding for a structured summary.
			// On failure (timeout, LLM not configured, etc.), understanding
			// will be nil and buildPendingConfirmation falls back to raw-text echo.
			understanding := h.understandTaskWithLLM(msg.UserID, trimmed, intent)
			item := buildPendingConfirmation(h.app, msg.UserID, trimmed, intent, understanding)
			if h.confirmationStore != nil {
				h.confirmationStore.set(item)
			}
			return buildConfirmationResponse(item)
		}
	}
	if unfinishedSlot != nil && unfinishedSlot.Source != "session_exit" && !msg.IsBackground && !freshTask && !isSlotActionCommand(trimmed) && !decision.StartNewTask && decision.ResumeSlotID == "" {
		if hint := buildUnfinishedSlotHint(unfinishedSlot); hint != "" {
			unfinishedTask := buildUnfinishedTaskPayload(unfinishedSlot)
			recoverableSession := (*IMResponseRecoverableSession)(nil)
			if h.manager != nil {
				for _, session := range h.manager.List() {
					if strings.TrimSpace(session.ProjectPath) != strings.TrimSpace(unfinishedSlot.ProjectPath) {
						continue
					}
					recoverableSession = buildRecoverableSessionPayload(session)
					if recoverableSession != nil {
						break
					}
				}
			}
			resp := &IMAgentResponse{
				Text:               hint,
				UnfinishedTask:     unfinishedTask,
				UnfinishedSlot:     unfinishedTask,
				RecoverableSession: recoverableSession,
			}
			return resp
		}
	}

	history := h.memory.Load(msg.UserID)
	history = h.compactHistory(history, httpClient)
	var systemPrompt string
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0)
	} else {
		systemPrompt = h.buildSystemPrompt()
	}
	systemPrompt += h.buildResumeTraceContext(msg.UserID, msg.Text)

	// Inject workflow phase prompt when the agent loop is running on behalf
	// of the workflow engine (workflowAgentLoop=true). The stashed prompt
	// is set by handleActiveWorkflow (from engine.HandleInput) or by
	// advanceAndRespond (from engine.AdvancePhase). It contains the
	// phase-specific system instructions ("you are in the audience_goal
	// phase, generate...").
	//
	// Prefer the stashed prompt (includes modify context or the exact
	// prompt from HandleInput) over the generic BuildPhasePrompt fallback.
	if workflowAgentLoop && h.getWorkflowEngine() != nil {
		if stashed, ok := h.stashedPhasePrompt.LoadAndDelete(msg.UserID); ok {
			systemPrompt += "\n" + stashed.(string)
		} else if phasePrompt := h.getWorkflowEngine().BuildPhasePrompt(msg.UserID); phasePrompt != "" {
			systemPrompt += "\n" + phasePrompt
		}
	} else {
		// Not a workflow agent loop — clean up any stashed prompt to prevent
		// it from leaking into a future message.
		h.stashedPhasePrompt.Delete(msg.UserID)
	}

	// Inject ask_user response context so the LLM knows the user is
	// answering a previous confirmation question, not starting a new task.
	if askUserContext != "" {
		systemPrompt += "\n\n" + askUserContext
	}

	// Inject async capability gap result so the LLM knows about newly
	// installed skills from the background search.
	if capabilityGapContext != "" {
		systemPrompt += "\n\n" + capabilityGapContext
	}

	// Desktop AI assistant panel: override PDF instructions with Markdown preview.
	if msg.Platform == "desktop" {
		systemPrompt += desktopWorkflowDocOverride()
	} else if msg.Platform != "" {
		// IM channels: enforce PDF delivery for all workflow phase documents.
		systemPrompt += imWorkflowDocDeliveryRule()
	}

	// Create a LoopContext for this chat loop.
	loopCtx := providedLoopCtx
	if loopCtx == nil {
		loopCtx = NewLoopContext("chat", h.getMaclawAgentMaxIterations(), httpClient)
	}
	if loopCtx.HTTPClient == nil {
		loopCtx.HTTPClient = httpClient
	}
	if h.traceService != nil && loopCtx.RunID == "" {
		job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, msg.Text, msg.Platform, msg.UserID, h.traceProjectPath())
		loopCtx.JobID = job.JobID
		loopCtx.RunID = run.RunID
		h.traceService.SetRunLoopID(run.RunID, loopCtx.ID)
		h.appendTraceEvent(loopCtx, "request.accepted", "info", "AI 请求已接收", truncateTraceText(msg.Text, 180), "", "")
	}
	// Wire the bgManager's statusC so the chat loop can drain background events.
	if h.bgManager != nil && loopCtx.StatusC == nil {
		loopCtx.StatusC = h.bgManager.statusC
	}
	// Propagate the "pending confirm other" flag to the loop context so
	// runAgentLoop can skip the NeedsConfirm gate for unrelated messages.
	loopCtx.SkipNeedsConfirmGate = skipNeedsConfirmGate

	// Propagate the ask_user response flag so runAgentLoop skips task-level
	// routing decisions (e.g. Skill preference) that assume a fresh task.
	loopCtx.IsAskUserResponse = askUserContext != ""

	// Serialize chat loops: cancel any lingering previous loop and wait for
	// it to release the mutex before starting a new one.
	if providedLoopCtx == nil {
		// Interrupt check: if another loop is already running and this message
		// is a cancel/merge/status signal, handle it without waiting for the lock.
		// This is the unified interrupt point — works for all channels (local
		// gateways, Hub mode, desktop). Gateway-level interrupt is an optimization
		// that fires earlier, but this is the authoritative check.
		if h.interruptHandler != nil && msg.Text != "" && h.currentLoopCtx != nil {
			result := h.interruptHandler.TryInterrupt(msg.UserID, msg.Text)
			if result.Handled {
				// Fully handled (Replace/Merge/StatusQuery) — the message's
				// purpose was to control the running loop, not to start a new task.
				return &IMAgentResponse{Text: result.Reply}
			}
			// Queued (Insert/Enqueue) — fall through to Lock. The message
			// will be processed normally after the current loop finishes.
			// Desktop panel doesn't need instant feedback here because the
			// user can see the buffer queue UI.
		}
		h.chatLoopMu.Lock()
		defer h.chatLoopMu.Unlock()
	}

	// --- SubAgent interception for coding execution ---
	//
	// The orchestrator is ONLY activated when the workflow engine has
	// completed the three-phase flow (requirements → design → task breakdown)
	// ── SubAgent activation ──
	//
	// The orchestrator is activated by the workflow engine when advancePhase
	// enters an execution phase (ToolFilterFull && !NeedsConfirm). This
	// happens in handleActiveWorkflow when it processes the ActivateOrchestrator
	// signal from WorkflowResponse. By the time we reach here, the orchestrator
	// is already active if the workflow engine triggered it.
	//
	// Previously, a hardcoded "Path 1" check existed here:
	//   ws.CurrentPhase == PhaseCodingImplementation
	// This coupled SubAgent activation to the coding workflow's specific
	// phase ID. Now the engine declares execution phases via ToolFilterFull
	// && !NeedsConfirm, which works for all 19 workflow templates.

	// When the orchestrator is active and the task should use SubAgent (direct
	// mode, no external coding tool), run the SubAgent instead of the main loop.
	if ShouldUseSubAgent(h.getTaskOrchestratorReadOnly(msg.UserID)) {
		log.Printf("[subagent-intercept] routing to SubAgent for user=%s", msg.UserID)
		cfg := h.getMaclawLLMConfig()
		taskOrch := h.getTaskOrchestratorReadOnly(msg.UserID)

		if onProgress != nil {
			onProgress("🚀 启动编码 SubAgent（纯净上下文模式）")
		}

		runner := NewSubAgentTaskRunner(h, cfg, httpClient, taskOrch, loopCtx)
		report := runner.RunAllTasks(onToken, func(text string) {
			if onProgress != nil {
				onProgress(text)
			}
		})

		// Deactivate orchestrator after all tasks are done.
		// NOTE: Deactivate is called AFTER integration and AdvancePhase
		// because those operations need the orchestrator's task state
		// (HasPassedTasks, BuildIntegrationPrompt, ProjectPath, etc.).
		defer taskOrch.Deactivate()

		// --- Fix #1: Save implementation phase output and advance workflow ---
		// Previously, SubAgent completion only deactivated the orchestrator
		// without updating the workflow engine. This left the workflow stuck
		// in the "implementation" phase forever — the integration and review
		// phases were never reached.
		//
		// Now we:
		// 1. Save the execution report as the implementation phase output
		// 2. Run the integration phase (BuildIntegrationPrompt) if all tasks passed
		// 3. Advance the workflow to the review phase
		if engine := h.getWorkflowEngine(); engine != nil {
			// Save implementation phase output so the engine knows it's done.
			engine.SavePhaseOutput(msg.UserID, report)

			// Run integration phase if there are completed tasks to integrate.
			if taskOrch.HasPassedTasks() && !loopCtx.IsCancelled() {
				integrationPrompt := taskOrch.BuildIntegrationPrompt()
				if integrationPrompt != "" {
					if onProgress != nil {
						onProgress("🔗 启动集成联调阶段...")
					}
					integrationResult := RunTaskWithSubAgent(
						h, cfg, httpClient,
						&TaskItem{
							Title:       "集成联调",
							Description: integrationPrompt,
						},
						taskOrch.ProjectPath,
						taskOrch.RequirementsContext,
						taskOrch.DesignContext,
						runner.collectPreviousOutputs(),
						loopCtx, onToken,
						func(text string) {
							if onProgress != nil {
								onProgress(text)
							}
						},
					)
					report += "\n\n## 集成联调\n\n" + integrationResult.Summary
				}
			}

			// Advance from implementation to review phase.
			// Skip if the user cancelled — don't advance to review when
			// the implementation was interrupted.
			if !loopCtx.IsCancelled() {
				advResp, advErr := engine.AdvancePhase(msg.UserID)
				if advErr != nil {
					log.Printf("[subagent-intercept] AdvancePhase error after SubAgent: %v", advErr)
				} else if advResp != nil {
					if advResp.Text != "" {
						report += "\n\n---\n" + advResp.Text
					}
					// If the review phase needs the agent loop, stash the prompt
					// so the next user message triggers it.
					if advResp.RunAgentLoop && advResp.PhasePrompt != "" {
						h.stashedPhasePrompt.Store(msg.UserID, advResp.PhasePrompt)
						h.workflowAgentLoopMarker.Store(msg.UserID, true)
					}
					if advResp.Complete {
						if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
							adapter.ResetSuggestMaximize(msg.UserID)
						}
					}
				}
			}
		}

		// Preserve SubAgent execution context in conversation history so the
		// LLM has context for follow-up messages ("改一下 Player 的跳跃逻辑").
		history = append(history, agent.ConversationEntry{
			Role:    "assistant",
			Content: report,
		})

		resp := &IMAgentResponse{Text: report}
		h.saveConversationHistoryTimed(msg.UserID, history, resp)
		return resp
	}

	resp := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, onToken, onNewRound, onStreamDone, msg.MinIterations, msg.Platform)
	if resp == nil {
		resp = &IMAgentResponse{}
	}

	// --- Evidence collection hook: async user profile signal analysis ---
	// Must not block the agent loop response. All work runs in goroutines.
	h.runEvidenceCollection(msg.UserID, msg.Text)

	// --- Workflow doc capture: store phase output and emit to frontend ---
	// Only capture when the agent loop was explicitly triggered by the workflow
	// engine (RunAgentLoop=true). This prevents unrelated messages (weather
	// queries, Q&A, etc.) from being captured as phase output when a stale
	// workflow happens to be active in memory.
	// Use a lower threshold (50 chars) — the engine already confirmed this is
	// a workflow phase, so even shorter text is a valid deliverable. The old
	// 200-char threshold could miss cases where the LLM used tools (write_file)
	// to produce the document and only output a short confirmation message.
	if workflowAgentLoop && h.getWorkflowEngine() != nil && !msg.IsBackground && !resp.HardExit && len(resp.Text) > 50 {
		if phaseID := h.getWorkflowEngine().SavePhaseOutput(msg.UserID, resp.Text); phaseID != "" {
			if cb := h.getWorkflowEngine().GetCallbacks(); cb != nil {
				_ = cb.EmitDocUpdate(msg.UserID, phaseID, resp.Text)
				log.Printf("[WorkflowEngine] post-loop doc capture: emitted doc_update for user=%s phase=%s len=%d", msg.UserID, phaseID, len(resp.Text))
			}
		}
	}

	if confirmedResume {
		resp.ConfirmedResume = true
	}
	finalizeStartedAt := time.Now()
	resp = h.finalizeTraceResult(loopCtx, resp, firstNonEmptyTraceText(resp.Text, resp.TraceSummary), resp.Error)
	resp.FinalizeTraceNanos = time.Since(finalizeStartedAt).Nanoseconds()
	return resp
}

// handleExitCommand terminates all active sessions, resets conversation
// memory, and returns the user to normal chat mode.
func (h *IMMessageHandler) handleExitCommand(userID string) *IMAgentResponse {
	var killed []string
	var failCount int
	if h.manager != nil {
		for _, s := range h.manager.List() {
			s.mu.RLock()
			active := isActiveRemoteSessionStatus(s.Status)
			sid := s.ID
			tool := s.Tool
			s.mu.RUnlock()
			if active {
				if err := h.manager.Kill(sid); err == nil {
					killed = append(killed, fmt.Sprintf("%s(%s)", sid, tool))
				} else {
					failCount++
				}
			}
		}
	}
	h.memory.Clear(userID)
	h.clearPerUserSessionState(userID)
	// Flush evidence batch and reset session on exit.
	h.flushEvidenceOnSessionEnd(userID)
	// Reset workflow working directory and suggest_maximize dedup flag.
	if h.getWorkflowEngine() != nil {
		if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
			adapter.ResetWorkingDir()
			adapter.ResetSuggestMaximize(userID)
		}
	}

	var b strings.Builder
	if len(killed) > 0 {
		b.WriteString(fmt.Sprintf("已退出编程模式。终止了 %d 个会话: %s", len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString("已退出编程模式。")
	}
	if failCount > 0 {
		b.WriteString(fmt.Sprintf("\n⚠️ %d 个会话终止失败，可能需要手动处理。", failCount))
	}
	b.WriteString("\n对话已重置，后续消息将正常对话。")
	return &IMAgentResponse{Text: b.String()}
}

// handleBtwCommand runs a /btw side query in an independent agent loop.
// The query runs with a minimal tool set (web_search, web_fetch, read_file,
// memory) and does not pollute the main conversation with intermediate steps.
//
// Concurrency: /btw runs before chatLoopMu (by design — side queries should
// not block on the main loop). Results are NOT appended to the main history
// to avoid racing with a concurrent main loop's Save.
func (h *IMMessageHandler) handleBtwCommand(msg IMUserMessage, query string, onProgress tool.ProgressCallback, onToken llm.TokenCallback) *IMAgentResponse {
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client

	btw := NewBtwSubAgent(h, cfg, httpClient)
	btw.SetCallbacks(onToken, func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	})

	// Wire cancellation: store the SubAgent so /cancel can reach it.
	h.activeBtwSubAgent.Store(btw)
	defer h.activeBtwSubAgent.Store((*BtwSubAgent)(nil))

	result := btw.Execute(query)

	if result.Error != "" && result.Text == "" {
		return &IMAgentResponse{Error: fmt.Sprintf("/btw 查询失败: %s", result.Error)}
	}

	// NOTE: We intentionally do NOT append /btw results to the main
	// conversation history. Reasons:
	//
	// 1. If a main agent loop is running concurrently, its final Save()
	//    does a full replacement of the history — any Append we do here
	//    would be silently overwritten. Appending gives a false sense of
	//    persistence.
	//
	// 2. The desktop frontend manages its own message list (setMessages)
	//    and already displays the /btw result. The backend history is not
	//    the source of truth for the desktop panel's UI.
	//
	// 3. For IM channels, the /btw result is returned as IMAgentResponse.Text
	//    and delivered to the user. The next user message will trigger a
	//    fresh Load() that doesn't include the /btw exchange — this is
	//    acceptable because /btw is a side query, not part of the main task.
	//
	// If future requirements need /btw context in the main conversation,
	// the correct approach is to inject it as a system message at the start
	// of the next agent loop (similar to askUserContext), not to race with
	// the concurrent Save.

	log.Printf("[btw] completed query=%q iterations=%d tools=%d", truncateRunes(query, 50), result.Iterations, result.ToolCalls)

	return &IMAgentResponse{Text: result.Text}
}

// handleSessionsCommand returns a quick status summary of active sessions.
func (h *IMMessageHandler) handleSessionsCommand() *IMAgentResponse {
	if h.manager == nil {
		return &IMAgentResponse{Text: "会话管理器未初始化。"}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: "当前没有活跃会话。\n\n💡 提示: 发送 /exit 可退出编程模式回到普通对话。",
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 当前 %d 个会话:\n", len(sessions)))
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("• [%s] %s — %s", s.ID, s.Tool, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" | %s", task))
		}
		if waiting {
			b.WriteString(" ⏳等待输入")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n💡 发送 /exit 可终止所有会话并退出编程模式。")
	return &IMAgentResponse{Text: b.String()}
}

// compactHistory summarizes old conversation turns to stay within token limits.
//
// Mechanism: instead of serializing all entries as JSON (which produces
// unreadable input for the summarizer LLM), we extract turn-boundary
// texts — the user's requests and the LLM's first responses. These carry
// the task-level semantics. Tool call details are omitted from the summary
// input since they're execution noise.
func (h *IMMessageHandler) compactHistory(entries []agent.ConversationEntry, httpClient *http.Client) []agent.ConversationEntry {
	if estimateConversationEntryTokens(entries) < agent.MaxMemoryTokenEstimate {
		return entries
	}
	split := len(entries) / 2
	// Adjust split point forward so we don't cut in the middle of a
	// tool-call group (assistant+tool_calls followed by tool results).
	// The "recent" slice must not start with orphaned role:"tool" entries.
	for split < len(entries) && entries[split].Role == "tool" {
		split++
	}
	if split >= len(entries) {
		// Degenerate case: everything is tool messages — just return as-is.
		return entries
	}
	recent := entries[split:]

	// Extract turn-boundary texts from the old half for summarization.
	// This produces human-readable input instead of raw JSON.
	turnTexts := extractTurnBoundaryTexts(entries[:split], 20)
	var sb strings.Builder
	for _, text := range turnTexts {
		runes := []rune(text)
		if len(runes) > 500 {
			text = string(runes[:500]) + "..."
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}
	summaryText := sb.String()
	if len(summaryText) > 32000 {
		summaryText = summaryText[:32000] + "\n...(truncated)"
	}

	cfg := h.getMaclawLLMConfig()
	msgs := []map[string]string{
		{"role": "user", "content": "请简洁总结以下对话历史，保留关键事实、决策和待办事项：\n\n" + summaryText},
	}
	conv := make([]interface{}, len(msgs))
	for i, m := range msgs {
		conv[i] = m
	}
	resp, err := h.doLLMRequest(cfg, conv, nil, httpClient)
	if err != nil || len(resp.Choices) == 0 {
		return recent
	}

	compacted := []agent.ConversationEntry{
		{Role: "user", Content: "[对话历史摘要]\n" + resp.Choices[0].Message.Content},
		{Role: "assistant", Content: "好的，我已了解之前的对话上下文。"},
	}
	return append(compacted, recent...)
}

// ---------------------------------------------------------------------------
// LLM types and HTTP client
// ---------------------------------------------------------------------------

// CancelCurrentSession cancels the currently running chat session.
// It signals the loop to stop and waits (up to 10s) for it to exit so that
// a subsequent SendAIAssistantMessage call won't overlap with the old loop.
// Returns the cancelled task's user text (if any) for display purposes.
func (h *IMMessageHandler) CancelCurrentSession() (string, error) {
	ctx := h.currentLoopCtx
	if ctx == nil {
		return "", fmt.Errorf("no active session to cancel")
	}
	taskText := h.lastUserText
	ctx.Cancel()
	// Wait for the loop goroutine to finish so the chatLoopMu is released
	// before the caller sends a new message.
	select {
	case <-ctx.DoneC:
	case <-time.After(10 * time.Second):
		log.Printf("[CancelCurrentSession] timed out waiting for loop to exit")
	}
	return taskText, nil
}

// parseSlotKind converts a string slot kind to the SlotKind enum.
// Defaults to SlotKindScheduled for unknown values.
func parseSlotKind(s string) SlotKind {
	switch s {
	case "coding":
		return SlotKindCoding
	case "scheduled", "":
		return SlotKindScheduled
	case "auto":
		return SlotKindAuto
	default:
		return SlotKindScheduled
	}
}

// drainStatusEvents non-blockingly drains all pending StatusEvents from the
// LoopContext's StatusC channel, injecting each as a system message into the
// conversation and forwarding to the user via sendProgress.
func drainStatusEvents(ctx *LoopContext, conversation *[]interface{}, sendProgress func(string)) {
	for {
		select {
		case evt := <-ctx.StatusC:
			statusMsg := fmt.Sprintf("[后台事件] %s", evt.Message)
			*conversation = append(*conversation, map[string]string{
				"role": "system", "content": statusMsg,
			})
			sendProgress(fmt.Sprintf("📡 %s", evt.Message))
		default:
			return
		}
	}
}

type trialReflectState struct {
	enabled            bool
	pendingNote        string
	lastObservation    string
	failedActionCounts map[string]int
}

func newTrialReflectState(enabled bool) *trialReflectState {
	return &trialReflectState{
		enabled:            enabled,
		failedActionCounts: make(map[string]int),
	}
}

func classifyTrialOutcome(result string) string {
	lower := strings.ToLower(strings.TrimSpace(result))
	if lower == "" {
		return "uncertain"
	}
	failureHints := []string{
		"error", "failed", "not found", "timeout", "timed out", "denied", "invalid",
		"panic", "exception", "unable", "cannot", "can't", "permission", "no such file",
		"不存在", "失败", "错误", "超时", "拒绝", "无权限", "未找到", "异常",
	}
	for _, hint := range failureHints {
		if strings.Contains(lower, hint) {
			return "failed"
		}
	}
	successHints := []string{
		"success", "completed", "done", "saved", "created", "updated", "ok", "ready",
		"成功", "已完成", "完成", "已保存", "已创建", "已更新", "就绪",
	}
	for _, hint := range successHints {
		if strings.Contains(lower, hint) {
			return "succeeded"
		}
	}
	return "uncertain"
}

func classifySkillRunOutcome(result string) string {
	lower := strings.ToLower(strings.TrimSpace(result))
	if lower == "" {
		return "uncertain"
	}
	if strings.Contains(lower, "status: failed") || strings.Contains(lower, "status: cancelled") || strings.Contains(lower, "status: canceled") {
		return "failed"
	}
	if strings.Contains(lower, "status: success") {
		return "succeeded"
	}
	if strings.Contains(lower, "status: running") || strings.Contains(lower, "run_id:") || strings.Contains(lower, "session_id:") {
		return "uncertain"
	}
	return classifyTrialOutcome(result)
}

func classifyToolOutcome(toolName, result string) string {
	name := strings.TrimSpace(toolName)
	trimmed := strings.TrimSpace(result)
	lower := strings.ToLower(trimmed)
	if trimmed == "" {
		return "uncertain"
	}
	switch name {
	case "list_skills":
		if strings.Contains(trimmed, "=== 本地已注册 Skill ===") ||
			strings.Contains(trimmed, "本地没有已注册的 Skill") ||
			strings.Contains(trimmed, "推荐 Skill") ||
			strings.Contains(trimmed, "search_skill_hub") {
			return "succeeded"
		}
		return classifyTrialOutcome(result)
	case "run_skill", "get_skill_run":
		return classifySkillRunOutcome(result)
	case "search_and_install_skill":
		if strings.Contains(trimmed, "✅") || strings.Contains(trimmed, "已自动安装并执行 Skill") {
			return "succeeded"
		}
		if strings.Contains(trimmed, "均未找到") || strings.Contains(trimmed, "未找到") {
			return "uncertain"
		}
		if strings.Contains(lower, "搜索 skillmarket 失败") || strings.Contains(lower, "导入失败") || strings.Contains(lower, "下载失败") || strings.Contains(lower, "执行失败") || strings.Contains(lower, "已拒绝自动安装") {
			return "failed"
		}
		return classifyTrialOutcome(result)
	default:
		return classifyTrialOutcome(result)
	}
}

func trialActionSignature(name, args string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(args)))
	return fmt.Sprintf("%s#%x", strings.TrimSpace(name), hash[:4])
}

func buildTrialReflectNote(toolNames []string, observations []string, repeatedFailures []string) string {
	if len(toolNames) == 0 || len(observations) == 0 {
		return ""
	}
	toolSummary := strings.Join(toolNames, ", ")
	observation := strings.Join(observations, "；")
	var b strings.Builder
	b.WriteString("[试错反思]\n")
	b.WriteString("上一轮尝试：")
	b.WriteString(toolSummary)
	b.WriteString("\n")
	b.WriteString("观察结果：")
	b.WriteString(observation)
	b.WriteString("\n")
	b.WriteString("下一轮要求：先根据这些结果调整方法，再继续动作；不要原样重复已经失败的尝试。")
	if len(repeatedFailures) > 0 {
		sort.Strings(repeatedFailures)
		b.WriteString("\n避免重复：")
		b.WriteString(strings.Join(repeatedFailures, ", "))
	}
	return b.String()
}

func (s *trialReflectState) observeIteration(toolCalls []llm.ToolCall, toolResults []string) (string, string, []string) {
	if s == nil || !s.enabled || len(toolCalls) == 0 || len(toolCalls) != len(toolResults) {
		return "", "", nil
	}
	toolNames := make([]string, 0, len(toolCalls))
	observations := make([]string, 0, len(toolCalls))
	repeatedFailures := make([]string, 0)
	overall := "succeeded"
	for i, tc := range toolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		toolNames = append(toolNames, name)
		outcome := classifyToolOutcome(name, toolResults[i])
		summary := truncateTraceText(strings.TrimSpace(toolResults[i]), 120)
		if summary == "" {
			summary = "无明确输出"
		}
		observations = append(observations, fmt.Sprintf("%s=%s（%s）", name, outcome, summary))
		sig := trialActionSignature(name, tc.Function.Arguments)
		switch outcome {
		case "failed":
			s.failedActionCounts[sig]++
			if s.failedActionCounts[sig] >= 1 {
				repeatedFailures = append(repeatedFailures, name)
			}
			overall = "failed"
		case "uncertain":
			if overall == "succeeded" {
				overall = "uncertain"
			}
		default:
			delete(s.failedActionCounts, sig)
		}
	}
	note := buildTrialReflectNote(toolNames, observations, repeatedFailures)
	s.pendingNote = note
	s.lastObservation = strings.Join(observations, "；")
	return overall, s.lastObservation, repeatedFailures
}

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []agent.ConversationEntry, userText string, attachments []MessageAttachment, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback, minIterations int, platform string) (result *IMAgentResponse) {
	// panic recovery — 防止工具执行异常导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 内部错误: %v", r)}
		}
	}()

	// Wire the loop context so tools can access it.
	loopStartedAt := time.Now()
	var preLLMConfigElapsed time.Duration
	var preLLMToolsElapsed time.Duration
	var preLLMConversationElapsed time.Duration
	var preLLMIterationPrepElapsed time.Duration
	var firstLLMRequestStartedAt time.Time
	var firstLLMResponseAt time.Time
	var firstLLMRequestBuildElapsed time.Duration
	var firstLLMHTTPDoElapsed time.Duration
	var firstLLMFirstSSEWaitElapsed time.Duration
	var firstLLMRetryWaitElapsed time.Duration
	var firstLLMStreamMaxTokenGapElapsed time.Duration
	var firstLLMRetryCount int
	var firstLLMIdleTimeoutCount int
	var firstLLMIdleTimeoutAfterToken bool
	var firstLLMRequestMarked bool
	var streamDoneAt time.Time
	var postStreamUsageDoneAt time.Time
	var postStreamLastReturnPrepAt time.Time
	var handlerPostStreamUsageElapsed time.Duration
	var handlerPostStreamResponseElapsed time.Duration
	var handlerPostStreamToolExecElapsed time.Duration
	var handlerPostStreamChoiceElapsed time.Duration
	var handlerPostStreamAssistantMsgElapsed time.Duration
	var handlerPostStreamHistoryAppendElapsed time.Duration
	var handlerPostStreamNoToolBranchElapsed time.Duration
	var lastLLMInputTokens int
	var lastLLMOutputTokens int
	attachLLMTelemetry := func(resp *IMAgentResponse) {
		if resp == nil {
			return
		}
		if !firstLLMRequestStartedAt.IsZero() {
			resp.PreLLMPrepNanos = firstLLMRequestStartedAt.Sub(loopStartedAt).Nanoseconds()
			resp.PreLLMConfigNanos = preLLMConfigElapsed.Nanoseconds()
			resp.PreLLMToolsNanos = preLLMToolsElapsed.Nanoseconds()
			resp.PreLLMConversationNanos = preLLMConversationElapsed.Nanoseconds()
			resp.PreLLMIterationPrepNanos = preLLMIterationPrepElapsed.Nanoseconds()
		}
		if !firstLLMRequestStartedAt.IsZero() && !firstLLMResponseAt.IsZero() {
			resp.FirstTokenWaitNanos = firstLLMResponseAt.Sub(firstLLMRequestStartedAt).Nanoseconds()
			resp.LLMRequestBuildNanos = firstLLMRequestBuildElapsed.Nanoseconds()
			resp.LLMHTTPDoNanos = firstLLMHTTPDoElapsed.Nanoseconds()
			resp.LLMFirstSSEWaitNanos = firstLLMFirstSSEWaitElapsed.Nanoseconds()
			resp.LLMRetryWaitNanos = firstLLMRetryWaitElapsed.Nanoseconds()
			resp.LLMStreamMaxTokenGapNanos = firstLLMStreamMaxTokenGapElapsed.Nanoseconds()
			resp.LLMRetryCount = firstLLMRetryCount
			resp.LLMIdleTimeoutCount = firstLLMIdleTimeoutCount
			resp.LLMIdleTimeoutAfterToken = firstLLMIdleTimeoutAfterToken
		}
		if !streamDoneAt.IsZero() && resp.HandlerTailNanos == 0 {
			resp.HandlerTailNanos = time.Since(streamDoneAt).Nanoseconds()
		}
		if lastLLMInputTokens > 0 || lastLLMOutputTokens > 0 {
			resp.Fields = mergeIMResponseFields(resp.Fields, tokenUsageResponseFields(lastLLMInputTokens, lastLLMOutputTokens))
			resp.InputTokens = lastLLMInputTokens
			resp.OutputTokens = lastLLMOutputTokens
			resp.TotalTokens = lastLLMInputTokens + lastLLMOutputTokens
		}
		resp.HandlerPostStreamUsageNanos = handlerPostStreamUsageElapsed.Nanoseconds()
		resp.HandlerPostStreamResponseNanos = handlerPostStreamResponseElapsed.Nanoseconds()
		resp.HandlerPostStreamToolExecNanos = handlerPostStreamToolExecElapsed.Nanoseconds()
		resp.HandlerPostStreamChoiceNanos = handlerPostStreamChoiceElapsed.Nanoseconds()
		resp.HandlerPostStreamAssistantMsgNanos = handlerPostStreamAssistantMsgElapsed.Nanoseconds()
		resp.HandlerPostStreamHistoryAppendNanos = handlerPostStreamHistoryAppendElapsed.Nanoseconds()
		resp.HandlerPostStreamNoToolBranchNanos = handlerPostStreamNoToolBranchElapsed.Nanoseconds()
		if !streamDoneAt.IsZero() && !postStreamUsageDoneAt.IsZero() && postStreamUsageDoneAt.After(streamDoneAt) {
			resp.HandlerBlackholeAfterUsageNanos = time.Since(postStreamUsageDoneAt).Nanoseconds() - resp.HandlerPostStreamResponseNanos - resp.MemorySaveNanos - resp.CapabilityGapNanos - resp.FileMaterializeNanos
			if resp.HandlerBlackholeAfterUsageNanos < 0 {
				resp.HandlerBlackholeAfterUsageNanos = 0
			}
		}
		if !streamDoneAt.IsZero() && !postStreamLastReturnPrepAt.IsZero() && postStreamLastReturnPrepAt.After(streamDoneAt) {
			resp.HandlerBlackholeBeforeReturnNanos = time.Since(postStreamLastReturnPrepAt).Nanoseconds()
		}
	}
	h.currentLoopCtx = ctx
	h.lastUserText = userText
	h.lastUserID = userID
	ctx.Platform = platform
	if h.traceService != nil && ctx.RunID != "" {
		h.traceService.SetRunLoopID(ctx.RunID, ctx.ID)
		h.appendTraceEvent(ctx, "loop.started", "info", "Agent loop started", truncateTraceText(userText, 180), "", "")
	}
	defer func() { h.currentLoopCtx = nil; h.lastUserText = ""; h.lastUserID = ""; ctx.Done() }()

	// Derive a context.Context from the LoopContext's CancelC so that
	// in-flight HTTP requests (LLM streaming) are aborted on cancel.
	loopCtx, loopCtxCancel := ctx.Context()
	defer loopCtxCancel()

	// --- Initialize first-layer Harness modules (optional, nil-safe) ---
	var loopGoalAnchor *GoalAnchor
	if h.goalAnchor != nil {
		loopGoalAnchor = h.goalAnchor
	} else {
		loopGoalAnchor = NewGoalAnchor(userText, 5)
	}

	var loopDriftDetector *DriftDetector
	// Inherit the session-level replan count so that after a drift exit +
	// user confirmation, the new loop immediately escalates to NeedHumanHelp
	// on the very first repeated drift instead of re-walking the full cycle.
	priorReplanCount := 0
	if v, ok := h.sessionDriftReplanCount.Load(userID); ok {
		priorReplanCount = v.(int)
	}
	if h.driftDetector != nil {
		loopDriftDetector = NewDriftDetectorWithHistory(h.driftDetector.windowSize, h.driftDetector.similarityThresh, priorReplanCount)
	} else {
		loopDriftDetector = NewDriftDetectorWithHistory(0, 0, priorReplanCount)
	}

	var loopProgressTracker *HarnessProgressTracker
	if h.harnessProgressTracker != nil {
		loopProgressTracker = h.harnessProgressTracker
	}

	var loopAdaptiveRetry *AdaptiveRetry
	if h.adaptiveRetry != nil {
		loopAdaptiveRetry = h.adaptiveRetry
	}

	// Helper to send progress if callback is set.
	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}
	streamDoneCallback := onStreamDone
	if onStreamDone != nil {
		streamDoneCallback = func() {
			if streamDoneAt.IsZero() {
				streamDoneAt = time.Now()
			}
			onStreamDone()
		}
	}

	// isDebug reads the debug toggle live from config so changes take effect
	// immediately — even mid-loop when the user flips the switch.
	// Cached for up to 2 seconds to avoid excessive disk reads in the hot loop.
	var cachedDebug bool
	var cachedDebugTime time.Time
	isDebug := func() bool {
		if now := time.Now(); now.Sub(cachedDebugTime) > 2*time.Second {
			c, err := h.loadConfig()
			if err != nil {
				cachedDebug = false
			} else {
				cachedDebug = c.MaclawDebugToolCalls
			}
			cachedDebugTime = now
		}
		return cachedDebug
	}

	// sendToolProgress sends user-visible tool stage progress. Detailed tool
	// internals remain gated by debug mode via toolOnProgress below, but users
	// should still see that the assistant is actively executing a tool.
	sendToolProgress := func(text string) {
		sendProgress(text)
	}

	// --- Event-driven progress tracker ---
	// Replaces the old ack timer + "任务较复杂" patience hints with
	// milestone-based progress that only sends messages when there's new info.
	//
	// Compute task embedding for the interrupt handler's relevance scoring.
	// When a new message arrives during this loop, TryInterrupt computes
	// cosine(taskEmbed, msgEmbed) to decide merge/replace/insert.
	var taskEmbed []float32
	if h.interruptHandler != nil {
		taskEmbed = h.interruptHandler.EmbedText(userText)
	}
	milestoneTracker := progress.NewAgentProgressTracker(
		func(text string) { sendProgress(text) },
		userText, "", taskEmbed,
	)
	defer milestoneTracker.Stop()
	// Register tracker for interrupt handler access.
	if h.interruptHandler != nil {
		h.interruptHandler.SetTracker(userID, milestoneTracker)
		defer h.interruptHandler.ClearTracker(userID)
	}

	// Hub heartbeat: silent keepalive every 60s so the Hub-side response
	// timer (180s) is continuously reset. Separate from user-facing progress.
	const hubHeartbeatInterval = 60 * time.Second
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(hubHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendProgress(imHeartbeatMsg)
			case <-heartbeatDone:
				return
			}
		}
	}()
	defer close(heartbeatDone)

	configStartedAt := time.Now()
	// Ensure OAuth token is fresh before reading config — the token may have
	// expired since the last request. ensureOAuthToken refreshes and persists
	// the new token so GetMaclawLLMConfig picks up the updated Key.
	if err := h.ensureOAuthToken(); err != nil {
		log.Printf("[LLM] OAuth token refresh failed: %v", err)
	}
	cfg := h.getMaclawLLMConfig()
	if cfg.IsResponsesAPI() {
		keyPrefix := cfg.Key
		if len(keyPrefix) > 20 {
			keyPrefix = keyPrefix[:20] + "..."
		}
		log.Printf("[LLM] WARNING Responses API config: wire_api=%s key_prefix=%q key_len=%d url=%s", cfg.WireAPI, keyPrefix, len(cfg.Key), cfg.URL)
	}
	trialReflectEnabled := false
	if appCfg, err := h.loadConfig(); err == nil {
		trialReflectEnabled = appCfg.UIMode == "pro" && appCfg.TrialReflectEnabled
	}
	preLLMConfigElapsed = time.Since(configStartedAt)
	trialState := newTrialReflectState(trialReflectEnabled)
	maxIter := h.getMaclawAgentMaxIterations()
	h.loopMaxOverride = 0 // reset dynamic override for this loop
	// Sync initial maxIter into the LoopContext so ctx is the source of truth.
	if ctx.MaxIterations() <= 0 {
		ctx.SetMaxIterations(maxIter)
	}
	phase := agentLoopPhase{Stage: agentStageOrient, SkillMode: skillPreferenceNone}
	// Skip Skill preference evaluation when the user is answering a previous
	// ask_user question. The task context is already established — re-evaluating
	// the answer text as a new task leads to false positives (e.g. a domain name
	// like "mypapers.top" matching the "paper" hint and triggering Skill search).
	if !ctx.IsAskUserResponse && shouldPreferSkillForTask(userText) {
		phase.ForceSkillPreference = true
		phase.SkillMode = skillPreferenceRemoteRequired
		if skillName, skillReason := matchPreferredLocalSkill(h.getSkillExecutor(), userText); skillName != "" {
			phase.SkillMode = skillPreferenceLocalOnly
			phase.PreferredSkillName = skillName
			phase.PreferredSkillReason = skillReason
		}
	}

	toolsStartedAt := time.Now()
	allTools := h.getTools()
	baseTools := h.routeTools(userText, allTools)
	tools := baseTools
	if phase.ForceSkillPreference {
		if shouldRestrictToSkillSearch(phase) {
			tools = filterToolsForRemoteSkillSearch(baseTools)
		} else {
			tools = filterToolsForSkillPreference(baseTools)
		}
	}
	// Workflow tool filtering: restrict tools during doc_only phases.
	// Skip when the workflow-confirm classifier returned "other" (user's
	// message is unrelated to the active workflow), so that tools like ssh
	// are not stripped by the doc_only whitelist.
	if h.getWorkflowEngine() != nil && !ctx.SkipNeedsConfirmGate {
		tools = h.applyWorkflowToolFilter(userID, tools)
	}
	toolsTokenBudget := estimateToolsTokens(tools)
	preLLMToolsElapsed = time.Since(toolsStartedAt)
	httpClient := ctx.HTTPClient

	var recorder *TrajectoryRecorder
	if h.trajectoryRecorderFactory != nil {
		recorder = h.trajectoryRecorderFactory()
	}
	if recorder != nil {
		defer recorder.Flush()
		if loopAdaptiveRetry == nil {
			loopAdaptiveRetry = NewAdaptiveRetry(recorder)
		}
	}
	recordSystemMessages := func(start int, items []interface{}) {
		if recorder == nil {
			return
		}
		for i := start; i < len(items); i++ {
			msg, ok := items[i].(map[string]string)
			if !ok {
				continue
			}
			if msg["role"] != "system" {
				continue
			}
			recorder.Record("system", msg["content"], nil, "", "")
		}
	}
	recordToolCall := func(id, name, args string) {
		if recorder == nil {
			return
		}
		recorder.Record("tool", map[string]interface{}{
			"name":      name,
			"arguments": args,
		}, nil, id, "")
	}
	recordToolResult := func(id string, content interface{}) {
		if recorder == nil {
			return
		}
		recorder.Record("tool_result", content, nil, id, "")
	}

	// Cross-channel activity: report this loop so the other channel can see it.
	activitySource := "im"
	if platform == "desktop" {
		activitySource = "gui"
	}
	reportActivity := func(iter, maxI int, summary string) {
		task := userText
		if len(task) > 100 {
			task = task[:100]
		}
		if len(summary) > 120 {
			summary = summary[:120]
		}
		h.agentActivity.Update(&AgentActivity{
			Source:      activitySource,
			Task:        task,
			Iteration:   iter,
			MaxIter:     maxI,
			LastSummary: summary,
		})
	}
	reportActivity(0, maxIter, "")
	defer h.agentActivity.Clear(activitySource)

	// Inject cross-channel activity awareness into the system prompt.
	if extra := h.agentActivity.FormatForPrompt(activitySource); extra != "" {
		systemPrompt += extra
	}

	conversationStartedAt := time.Now()
	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		// Strip base64 image data and annotate file/attachment sections from
		// previous user messages so the LLM does not confuse earlier
		// uploads with the current one. See bugfix #image-history-leak.
		conversation = append(conversation, stripHistoryAttachments(entry.ToMessage()))
	}

	// Build user message — multimodal if attachments contain images.
	userContent := buildUserContent(userText, attachments, cfg.Protocol, cfg.SupportsVision)
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})

	history = append(history, agent.ConversationEntry{Role: "user", Content: userContent})
	preLLMConversationElapsed = time.Since(conversationStartedAt)

	// --- Inject drift context from previous loop ---
	// When the previous agent loop exited due to drift (NeedHumanHelp),
	// inject a system message warning the LLM not to repeat the same tool.
	// Also clear the session drift state so it doesn't leak into unrelated
	// future conversations.
	if driftTool, ok := h.sessionDriftTool.LoadAndDelete(userID); ok {
		toolName := driftTool.(string)
		driftCtx := fmt.Sprintf(
			"[系统提示] 上一轮对话因反复调用 %s 失败而停止。"+
				"禁止再次使用相同的方法。"+
				"如果没有其他可行方案，直接告诉用户当前的限制和建议。",
			toolName,
		)
		conversation = append(conversation, map[string]string{
			"role": "system", "content": driftCtx,
		})
		log.Printf("[DriftContext] injected drift warning for user=%s tool=%s priorReplanCount=%d", userID, toolName, priorReplanCount)
	}

	if recorder != nil {
		recorder.StartSession(ctx.ID, h.getMaclawLLMProviders().Current, cfg.Model, cfg.Protocol, userID, platform, tools)
		recorder.Record("system", systemPrompt, nil, "", "")
		recorder.Record("user", userContent, nil, "", "")
	}

	// maxIter defaults to 300 (MaxAgentIterationsCap).
	// Use the single source of truth for configured → effective conversion.
	effectiveMax := config.EffectiveMaxIterations(maxIter)
	chatFinalizeGrace := 0
	if ctx.Kind == LoopKindChat {
		chatFinalizeGrace = 2
	}
	// Apply minimum iterations floor (e.g. scheduled tasks need more rounds).
	// This is an additional business rule on top of EffectiveMaxIterations.
	if minIterations > 0 && effectiveMax < minIterations {
		effectiveMax = minIterations
		if effectiveMax > config.MaxAgentIterationsCap {
			effectiveMax = config.MaxAgentIterationsCap
		}
	}
	log.Printf("[AgentLoop] start loop=%s kind=%d maxIter=%d effectiveMax=%d minIterations=%d configCap=%d grace=%d user=%q task=%q",
		ctx.ID, ctx.Kind, maxIter, effectiveMax, minIterations, config.MaxAgentIterationsCap, chatFinalizeGrace, userID, truncateRunes(userText, 80))

	// --- Coding Tool Gate: pre-compute gate decision before iteration loop ---
	var gic *GateIntentClassifier
	if h.app != nil {
		gic = h.getGateIntentClassifier()
	}
	gateConfig := newCodingToolGateConfigWithClassifier(userText, ctx.Kind, gic, h.lastUserID)

	// Refine milestone tracker complexity now that intent is known.
	if gateConfig.intent != "" {
		milestoneTracker.RefineIntent(string(gateConfig.intent))
	}

	// --- Coding Tool Gate: filter blocked tool DEFINITIONS from the tool list ---
	// When the gate is active, remove browser/coding tool definitions from the
	// list sent to the LLM. This prevents the LLM from seeing 25+ browser tool
	// definitions during the three-phase coding workflow, which wastes context
	// tokens and causes hallucinated "Browser:" role prefixes in output.
	//
	// NOTE: We do NOT skip this filter when SkipNeedsConfirmGate is set.
	// SkipNeedsConfirmGate means handlePendingConfirm classified the message
	// as "other" (unrelated to the active workflow), but gateConfig.active is
	// independently computed from the message's coding intent. When the
	// "unrelated" message is itself a new coding task (e.g. "开发超级玛利游戏"
	// during an active PPT workflow), gateConfig.active=true and the gate
	// must strip coding tools to enforce the three-phase flow.
	// Non-coding "other" messages (e.g. "查天气") have gateConfig.active=false,
	// so the gate naturally does not fire and the full tool set is preserved.
	//
	// Also skip when the TaskExecutionOrchestrator is active — the orchestrator
	// manages tool availability itself (direct mode keeps bash/write_file/edit_file,
	// external mode keeps session tools). Applying the gate on top would strip
	// tools that the orchestrator intentionally preserved.
	//
	// NOTE: orchestratorActive is a function, not a cached bool, because the
	// orchestrator may be activated mid-loop (when the user confirms the task list).
	orchestratorActive := func() bool {
		if h.taskOrchestratorRegistry == nil {
			return false
		}
		o := h.taskOrchestratorRegistry.Get(userID) // read-only: don't create empty entries
		return o != nil && o.IsActive()
	}
	if gateConfig.active && !orchestratorActive() {
		filtered := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			name := tool.ExtractToolName(t)
			if !codingToolBlocklist[name] || deliveryToolAllowlist[name] {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) < len(tools) {
			log.Printf("[coding-gate] filtered %d blocked tool definitions from tool list", len(tools)-len(filtered))
			tools = filtered
			toolsTokenBudget = estimateToolsTokens(tools)
		}
	}

	// --- Steering Workflow Detector: detect coding workflows driven by
	// steering rules (coding-workflow.md) and emit frontend events that
	// would normally come from the WorkflowEngine path. Only activate when:
	// 1. Not a background task (ctx.Kind != LoopKindBackground)
	// 2. The GateIntentClassifier confirms coding intent, or conversation
	//    history has coding context (for follow-up messages like "确认")
	// 3. No active workflow engine workflow exists for this user
	//
	// IMPORTANT: We rely on gateConfig (from the three-layer GateIntentClassifier)
	// as the authoritative signal for whether the CURRENT message is a coding
	// task. The old isCodingTask() keyword list was redundant and less accurate.
	// conversationHasCodingContext() is only used for pre-activation (preparing
	// the detector to intercept coding tools), NOT for emitting the fullscreen
	// suggestion banner. The banner is only emitted when gateConfig.active=true,
	// meaning the classifier is confident this is a new_project coding task.
	// This prevents non-coding messages (skill invocations, weather queries, etc.)
	// from triggering the "编程" banner just because the conversation history
	// contains coding context.
	var steeringDetector *SteeringWorkflowDetector
	if ctx.Kind != LoopKindBackground {
		detector := NewSteeringWorkflowDetector(userID)
		// Two-tier activation:
		// Tier 1 (strong): gateConfig.active — classifier confirmed new_project
		//   → activate detector + emit fullscreen banner immediately
		// Tier 2 (weak): conversation history has coding context
		//   → activate detector (prepare to intercept coding tools) but
		//     do NOT emit fullscreen banner yet; defer to first actual
		//     coding tool interception
		shouldActivate := gateConfig.active || h.conversationHasCodingContext()
		if shouldActivate {
			hasEngineWorkflow := false
			if h.getWorkflowEngine() != nil {
				hasEngineWorkflow = h.getWorkflowEngine().HasActiveWorkflow(userID)
			}
			if !hasEngineWorkflow {
				steeringDetector = detector
				log.Printf("[SteeringWorkflow] detector activated for user=%s task=%q gateActive=%v", userID, truncateRunes(userText, 60), gateConfig.active)
				// Only emit suggest_maximize immediately when the classifier
				// is confident this is a new coding project (gateConfig.active).
				// When activation is only from conversation history context,
				// defer the banner to the first actual coding tool interception
				// (handled in the iteration loop below).
				if gateConfig.active && platform == "desktop" && h.getWorkflowEngine() != nil {
					if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
						// Auto-set working directory to the current project path.
						// The frontend will show a banner allowing the user to
						// confirm or change it before documents are generated.
						if adapter.GetWorkingDir() == "" {
							projectPath := strings.TrimSpace(h.getCurrentProjectPath())
							if projectPath != "" {
								adapter.SetWorkingDir(userID, projectPath)
							}
						}
						adapter.EmitSuggestMaximize(userID, "coding")
						steeringDetector.suggestMaximizeEmitted = true
						log.Printf("[SteeringWorkflow] emitted early suggest_maximize for desktop user=%s (classifier confirmed new_project)", userID)
					}
				}
			} else {
				log.Printf("[SteeringWorkflow] detector NOT activated: engine has active workflow for user=%s gateActive=%v", userID, gateConfig.active)
			}
		} else {
			log.Printf("[SteeringWorkflow] detector NOT activated: shouldActivate=false gateActive=%v gateReason=%q user=%s", gateConfig.active, gateConfig.reason, userID)
		}
	}

	// consecutiveJSONTruncations tracks consecutive write_file calls that fail
	// with "unexpected end of JSON input" (truncated output from the model).
	// After 2 consecutive failures, a system message is injected to guide the
	// model to use mode=append for chunked writing.
	consecutiveJSONTruncations := 0

	// directModeToolsFiltered tracks whether session tools have already been
	// stripped from the tools list for direct coding mode. Reset when tools
	// are restored (e.g. recover path's tools = baseTools).
	directModeToolsFiltered := false

	// totalToolCallsInLoop tracks the cumulative number of tool calls across
	// all iterations in this agent loop. Used by the nudge system to detect
	// complex tasks (≥5 tool calls).
	totalToolCallsInLoop := 0

	// --- Coding iteration budget ---
	// When the main agent loop is executing coding tasks (write_file, edit_file,
	// bash), it can run for 90+ iterations and blow up the context. This counter
	// tracks consecutive iterations with coding tool calls. When it exceeds the
	// soft limit, a progress reminder is injected. At the hard limit, the loop
	// force-returns with a progress summary.
	//
	// This is a safety net for when the main agent loop handles coding directly
	// (e.g. workflow engine unavailable, or coding gate enforcing three-phase).
	const codingIterBudgetSoft = 50  // inject progress reminder
	const codingIterBudgetHard = 65  // force return
	codingIterCount := 0             // consecutive iterations with coding tools

	// --- In-flight task marker ---
	// Mark that an agent loop is now executing. If the process is killed
	// before the loop completes, this marker persists on disk and tells
	// the next app session that the previous task was interrupted.
	h.memory.SetInFlightTask(userID, truncateRunes(userText, 200))
	if err := h.memory.FlushNow(); err != nil {
		log.Printf("[InFlightTask] flush failed: %v", err)
	}
	// Ensure the marker is cleared on every exit path (normal, cancel,
	// panic, max-rounds, drift, etc.). The defer runs after the named
	// return in runAgentLoop, so it covers all paths.
	defer func() {
		h.memory.ClearInFlightTask(userID)
		// Best-effort flush; if it fails the marker will be cleared on
		// the next successful persist cycle.
		_ = h.memory.FlushNow()
	}()

	// effectiveTokenLimit is the calibrated context token budget for
	// trimConversation. Updated each iteration based on API-reported
	// actual token counts. Also used by the bonus round after the loop.
	effectiveTokenLimit := cfg.EffectiveContextTokens()

	// lengthContinuationBuf accumulates text across finish_reason=length
	// continuations. When the LLM's output is truncated by the output token
	// limit, the continuation mechanism injects a "please continue" prompt
	// and loops. Each iteration's msgContent only contains that iteration's
	// chunk. Without accumulation, resp.Text (and therefore post-loop doc
	// capture / SavePhaseOutput) would only contain the LAST chunk, losing
	// all preceding content. This is critical for workflow phases where the
	// full document must be saved as the phase output.
	var lengthContinuationBuf strings.Builder

	for iteration := 0; ; iteration++ {
		ctx.SetIteration(iteration)

		// --- Check dynamic override from set_max_iterations tool ---
		// Both loopMaxOverride (legacy) and ctx.MaxIterations() are kept in
		// sync by toolSetMaxIterations. Read from ctx as source of truth.
		if h.loopMaxOverride > 0 {
			override := h.loopMaxOverride
			if minIterations > 0 && override < minIterations {
				override = minIterations
			}
			effectiveMax = override
			// Keep ctx in sync.
			ctx.SetMaxIterations(effectiveMax)
		} else {
			// ctx may have been updated externally (e.g. by ContinueC).
			if cm := ctx.MaxIterations(); cm > 0 && cm != effectiveMax {
				effectiveMax = cm
			}
		}
		if ctx.Kind == LoopKindChat && effectiveMax > 0 {
			remaining := effectiveMax - iteration
			if remaining <= 2 {
				driftPreview := loopDriftDetector.PreviewDrift()
				if !driftPreview.Drifted || !driftPreview.NeedHumanHelp {
					// Auto-extend cap is 2x the config cap (e.g. 600 when cap=300).
					// This ensures auto-extension works even when effectiveMax
					// already equals maxAgentIterationsCap.
					autoExtendCap := config.MaxAgentIterationsCap * 2
					autoExtended := effectiveMax + 30
					if autoExtended > autoExtendCap {
						autoExtended = autoExtendCap
					}
					if autoExtended > effectiveMax {
						effectiveMax = autoExtended
						ctx.SetMaxIterations(effectiveMax)
						sendProgress(fmt.Sprintf("⏳ 当前任务较长，已自动扩展推理轮次到 %d 轮，继续完成最终结果…", effectiveMax))
						log.Printf("[AgentLoop] auto-extended: iteration=%d new_max=%d cap=%d loop=%s", iteration, effectiveMax, autoExtendCap, ctx.ID)
						if h.traceService != nil && ctx.RunID != "" {
							h.appendTraceEvent(ctx, "loop.extended", "info", "Auto-extended iteration limit", truncateTraceText(fmt.Sprintf("remaining=%d new_max=%d", remaining, effectiveMax), 220), "", "")
						}
					}
				}
			}
		}

		// --- Background loop: pause near limit, wait for 续命 ---
		// Only pause if: (a) background loop, (b) effectiveMax > 4 to ensure
		// meaningful work before first pause, (c) iteration is at the pause
		// threshold. The threshold is effectiveMax-2 to give 2 remaining rounds
		// for graceful wrap-up after resume.
		if ctx.Kind == LoopKindBackground && effectiveMax > 4 && iteration == effectiveMax-2 {
			ctx.SetState("paused")
			// Notify via StatusC that we're approaching the limit.
			if ctx.StatusC != nil {
				select {
				case ctx.StatusC <- StatusEvent{
					Type:      StatusEventApproachingLimit,
					LoopID:    ctx.ID,
					SessionID: ctx.SessionID,
					Message:   fmt.Sprintf("后台任务 %s 即将达到最大轮数 (%d/%d)", ctx.ID, iteration, effectiveMax),
					Remaining: effectiveMax - iteration,
				}:
				default:
				}
			}
			// Wait for continue signal, cancel, or timeout (5 min).
			select {
			case extra := <-ctx.ContinueC:
				ctx.AddMaxIterations(extra)
				effectiveMax = ctx.MaxIterations()
				ctx.SetState("running")
			case <-ctx.CancelC:
				ctx.SetState("stopped")
				return &IMAgentResponse{Text: fmt.Sprintf("后台任务 %s 已被停止。", ctx.ID)}
			case <-time.After(5 * time.Minute):
				ctx.SetState("timeout")
				return &IMAgentResponse{Text: fmt.Sprintf("后台任务 %s 等待续命超时，已自动结束。", ctx.ID)}
			}
		}

		// --- Normal iteration limit check ---
		if iteration >= effectiveMax+chatFinalizeGrace {
			log.Printf("[AgentLoop] iteration limit reached: iteration=%d effectiveMax=%d grace=%d loop=%s", iteration, effectiveMax, chatFinalizeGrace, ctx.ID)
			break
		}

		// --- Chat loop: drain StatusC events before LLM call ---
		if ctx.Kind == LoopKindChat && ctx.StatusC != nil {
			drainStatusEvents(ctx, &conversation, sendProgress)
		}

		// --- Check cancellation ---
		if ctx.IsCancelled() {
			ctx.SetState("stopped")
			break
		}

		if h.traceService != nil && ctx.RunID != "" {
			h.traceService.UpdateRun(ctx.RunID, TraceRunStatusRunning, firstNonEmptyTraceText(ctx.Description, userText), "")
		}

		if iteration > 0 {
			if iteration >= effectiveMax && ctx.Kind == LoopKindChat {
				sendProgress("⏳ 已接近最大推理轮次，正在基于现有信息收尾并生成最终结果…")
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": "[收尾要求]\n你已接近最大推理轮次。禁止继续扩展搜索范围；优先基于当前已有信息直接收尾并交付最终结果。若已有文档内容，请立即生成并发送 PDF；若仍无法生成 PDF，请明确说明当前已完成部分、缺少什么，并给出可见终态。[/收尾要求]",
				})
			}
			if isDebug() {
				if maxIter > 0 || h.loopMaxOverride > 0 {
					sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d/%d 轮）…", iteration+1, effectiveMax))
				} else {
					sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d 轮）…", iteration+1))
				}
			} else {
				// Event-driven progress: milestone tracker handles merge
				// window expiry and heartbeat. No more timer-based "任务较复杂".
				milestoneTracker.Tick()
			}
		}

		// --- Consume pending injection (Merge from interrupt handler) ---
		if injected, ok := h.pendingInjection.LoadAndDelete(userID); ok {
			injectedText, _ := injected.(string)
			if injectedText != "" {
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": "[用户补充] " + injectedText,
				})
				log.Printf("[injection] user=%s injected supplementary message: %s", userID, truncateForLog(injectedText, 50))
			}
		}
		iterationPrepStartedAt := time.Now()
		conversation = autoCompressConversation(conversation, cfg, httpClient)

		// --- Actual-token calibration ---
		// estimateConversationTokens may underestimate the real token count
		// (especially for mixed CJK/code content). When the API reports
		// actual input tokens exceeding the effective context budget, the
		// estimate is too optimistic and trimConversation won't trigger.
		//
		// Fix: use the API-reported token count from the previous iteration
		// to compute a calibration ratio. If the ratio > 1.0, reduce the
		// token limit passed to trimConversation so it trims more aggressively.
		// effectiveTokenLimit is updated each iteration and also used by the
		// bonus round after the loop exits.
		effectiveTokenLimit = cfg.EffectiveContextTokens()
		if lastLLMInputTokens > 0 {
			estimated := estimateConversationTokens(conversation)
			if estimated > 0 {
				ratio := float64(lastLLMInputTokens) / float64(estimated)
				if ratio > 1.15 { // >15% underestimate — apply calibration
					calibrated := int(float64(effectiveTokenLimit) / ratio)
					if calibrated < 4000 {
						calibrated = 4000
					}
					log.Printf("[trim-calibration] API reported %d tokens, estimated %d (ratio=%.2f), reducing limit from %d to %d",
						lastLLMInputTokens, estimated, ratio, effectiveTokenLimit, calibrated)
					effectiveTokenLimit = calibrated
				}
			}

			// --- API-reported token hard ceiling ---
			// The configured ContextLength may exceed the model's actual
			// effective capacity (e.g. glm-5.1 configured at 180K but
			// returns empty responses above ~120K). When the API reports
			// input tokens exceeding 85% of the ORIGINAL effective budget
			// (before calibration), force trim to 65%. This catches the
			// case where the estimate is accurate but the budget itself
			// is too generous for the model's real capacity.
			//
			// Use cfg.EffectiveContextTokens() (not effectiveTokenLimit)
			// because effectiveTokenLimit may already be reduced by the
			// ratio calibration above. The hard ceiling should compare
			// against the original budget to avoid double-reduction.
			originalEffective := cfg.EffectiveContextTokens()
			hardCeiling := originalEffective * 85 / 100
			if lastLLMInputTokens > hardCeiling {
				forcedLimit := originalEffective * 65 / 100
				if forcedLimit < 4000 {
					forcedLimit = 4000
				}
				if forcedLimit < effectiveTokenLimit {
					log.Printf("[trim-hardlimit] API reported %d tokens > 85%% ceiling %d (effective=%d), forcing limit from %d to %d",
						lastLLMInputTokens, hardCeiling, originalEffective, effectiveTokenLimit, forcedLimit)
					effectiveTokenLimit = forcedLimit
				}
			}

			// --- Empty response proactive trim ---
			// When the previous LLM call returned an empty response
			// (output=0), the model is likely struggling with context
			// size. Proactively trim to 60% of effective before the
			// next call, instead of only injecting a Recover prompt
			// (which further inflates context).
			if lastLLMOutputTokens == 0 {
				emptyTrimLimit := cfg.EffectiveContextTokens() * 60 / 100
				if emptyTrimLimit < 4000 {
					emptyTrimLimit = 4000
				}
				if emptyTrimLimit < effectiveTokenLimit {
					log.Printf("[trim-empty-response] previous response was empty (input=%d), forcing aggressive trim from %d to %d",
						lastLLMInputTokens, effectiveTokenLimit, emptyTrimLimit)
					effectiveTokenLimit = emptyTrimLimit
				}
			}
		}
		conversation = trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, makeSummarizer(cfg, httpClient))

		// --- Harness: inject GoalAnchor content ---
		systemMessagesStart := len(conversation)
		if loopGoalAnchor != nil && loopGoalAnchor.ShouldAnchor(iteration) {
			var progressSummary string
			if loopProgressTracker != nil {
				progressSummary = loopProgressTracker.Summary()
			} else {
				progressSummary = fmt.Sprintf("迭代 %d/%d", iteration, effectiveMax)
			}
			anchorContent := loopGoalAnchor.BuildAnchorContent(progressSummary)
			conversation = append(conversation, map[string]string{
				"role": "system", "content": anchorContent,
			})
		}

		// --- Harness: inject ProgressTracker checklist ---
		if loopProgressTracker != nil {
			if checklist := loopProgressTracker.BuildChecklistContent(); checklist != "" {
				conversation = append(conversation, map[string]string{
					"role": "system", "content": "[📋 任务清单]\n" + checklist + "\n[/任务清单]",
				})
			}
		}
		if trialState.enabled && strings.TrimSpace(trialState.pendingNote) != "" {
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": trialState.pendingNote,
			})
			if h.traceService != nil && ctx.RunID != "" {
				h.appendTraceEvent(ctx, "trial.adjusted", "info", "Injected reflection note", truncateTraceText(trialState.pendingNote, 220), "", "")
			}
			trialState.pendingNote = ""
		}
		if phase.Stage == agentStageRecover && strings.TrimSpace(phase.RecoverPrompt) != "" {
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": phase.RecoverPrompt,
			})
			if phase.RecoverReason == "skill_failed" {
				phase.ForceSkillPreference = false
				phase.SkillMode = skillPreferenceFallbackAllowed
				phase.RemoteSearchExhausted = true
				tools = baseTools
				directModeToolsFiltered = false // reset: baseTools includes session tools
				// Re-apply coding gate definition filter after restoring baseTools.
				if gateConfig.active && !orchestratorActive() {
					gateFiltered := make([]map[string]interface{}, 0, len(tools))
					for _, t := range tools {
						name := tool.ExtractToolName(t)
						if !codingToolBlocklist[name] || deliveryToolAllowlist[name] {
							gateFiltered = append(gateFiltered, t)
						}
					}
					tools = gateFiltered
				}
				// Re-apply direct-mode session tool filter after restoring baseTools.
				if orchestratorActive() {
					orchInst := h.taskOrchestratorRegistry.Get(userID)
					if orchInst != nil && orchInst.CurrentExecutionMode() == TaskExecModeDirect {
						var directFiltered []map[string]interface{}
						for _, t := range tools {
							name := tool.ExtractToolName(t)
							if !isDirectModeBlockedTool(name) {
								directFiltered = append(directFiltered, t)
							}
						}
						tools = directFiltered
					}
				}
				toolsTokenBudget = estimateToolsTokens(tools)
			}
			recoverReason := firstNonEmptyTraceText(phase.RecoverReason, "recover")
			if h.traceService != nil && ctx.RunID != "" {
				h.appendTraceEvent(ctx, "loop.recover_entered", "warn", "Entered Recover stage", truncateTraceText(recoverReason, 220), "", "")
			}
			phase.RecoverPrompt = ""
			phase.RecoverReason = ""
			phase.Stage = agentStageConverge
		}

		// --- Task Execution Orchestrator: inject per-task guidance ---
		if h.taskOrchestratorRegistry != nil {
			orchInst := h.taskOrchestratorRegistry.Get(userID)
			if orchInst != nil && orchInst.IsActive() {
				// Resolve execution mode for the current task at runtime.
				execMode := orchInst.ResolveExecutionMode()

				// In direct mode, strip session management tools from the tool
				// list so the LLM codes directly instead of trying to delegate.
				// Only filter once per mode resolution — after the first pass,
				// session tools are already gone from the tools slice.
				if execMode == TaskExecModeDirect && !directModeToolsFiltered {
					var directFiltered []map[string]interface{}
					for _, t := range tools {
						name := tool.ExtractToolName(t)
						if !isDirectModeBlockedTool(name) {
							directFiltered = append(directFiltered, t)
						}
					}
					if len(directFiltered) < len(tools) {
						log.Printf("[agent-loop] direct-mode: stripped %d session tools from tool list", len(tools)-len(directFiltered))
						tools = directFiltered
					}
					directModeToolsFiltered = true
				}

				if taskInjection := orchInst.BuildSystemInjection(); taskInjection != "" {
					conversation = append(conversation, map[string]string{
						"role":    "system",
						"content": taskInjection,
					})
					if h.traceService != nil && ctx.RunID != "" {
						h.appendTraceEvent(ctx, "task_orchestrator.injection", "info",
							"Injected per-task guidance", truncateTraceText(taskInjection, 220), "", "")
					}
				}
			}
		}

		recordSystemMessages(systemMessagesStart, conversation)
		if phase.Stage == agentStageOrient && phase.ForceSkillPreference {
			convergePrompt := ""
			if shouldRestrictToSkillSearch(phase) {
				convergePrompt = "[Skill 优先要求]\n当前任务属于 Skill 优先路径，但本地未命中合适 Skill。本轮必须先调用 search_and_install_skill（或其他 skill 搜索/安装工具）查找可复用 Skill；在确认远程 Skill 路径无解之前，不要直接使用 craft_tool 或 bash。\n[/Skill 优先要求]"
			} else if phase.PreferredSkillName != "" {
				guidance := buildSkillProgressGuidance(phase.PreferredSkillName, phase.PreferredSkillRunID)
				convergePrompt = fmt.Sprintf("[Skill 优先要求]\n检测到本地已有可复用 Skill「%s」。本轮优先调用 manage_skill(action=\"run\", name=\"%s\") 完成任务，不要先使用 craft_tool 或 bash 自建脚本。%s 若该 Skill 失败，再基于失败原因切换到其他工具路径。", phase.PreferredSkillName, phase.PreferredSkillName, guidance)
				if phase.PreferredSkillReason != "" {
					convergePrompt += fmt.Sprintf("\n匹配依据: %s", truncateTraceText(phase.PreferredSkillReason, 160))
				}
				convergePrompt += "\n[/Skill 优先要求]"
			}
			if convergePrompt != "" {
				conversation = append(conversation, map[string]string{"role": "system", "content": convergePrompt})
				recordSystemMessages(len(conversation)-1, conversation)
			}
		}
		if !firstLLMRequestMarked {
			preLLMIterationPrepElapsed += time.Since(iterationPrepStartedAt)
		}

		// Notify frontend of new round (for streaming UI) — skip first iteration
		// since the frontend already created a placeholder message.
		if onNewRound != nil && iteration > 0 {
			onNewRound()
		}
		llmCallStartedAt := time.Now()
		if !firstLLMRequestMarked {
			firstLLMRequestMarked = true
			firstLLMRequestStartedAt = llmCallStartedAt
		}
		streamMetrics := &llmStreamMetrics{}
		resp, err := h.doLLMRequestStream(loopCtx, cfg, conversation, tools, httpClient, onToken, streamMetrics)
		if !firstLLMRequestMarked || firstLLMRequestBuildElapsed == 0 {
			firstLLMRequestBuildElapsed += time.Duration(streamMetrics.RequestBuildNanos)
			firstLLMHTTPDoElapsed += time.Duration(streamMetrics.HTTPDoNanos)
			firstLLMFirstSSEWaitElapsed += time.Duration(streamMetrics.FirstSSEWaitNanos)
			firstLLMStreamMaxTokenGapElapsed += time.Duration(streamMetrics.MaxTokenGapNanos)
			firstLLMIdleTimeoutCount += streamMetrics.IdleTimeoutCount
			firstLLMIdleTimeoutAfterToken = firstLLMIdleTimeoutAfterToken || streamMetrics.IdleTimeoutAfterToken
		}
		if err == nil && firstLLMResponseAt.IsZero() {
			if !streamMetrics.FirstTokenAt.IsZero() {
				firstLLMResponseAt = streamMetrics.FirstTokenAt
			} else {
				firstLLMResponseAt = time.Now()
			}
		}
		if err == nil && streamDoneCallback != nil {
			streamDoneCallback()
		}
		// Retry on timeout / temporary network errors.
		// When AdaptiveRetry is available, use it for smarter classification;
		// otherwise fall back to the existing isRetryableLLMError logic.
		if err != nil {
			// If cancelled, don't retry — exit immediately.
			if ctx.IsCancelled() {
				ctx.SetState("stopped")
				break
			}
			if loopAdaptiveRetry != nil {
				category := loopAdaptiveRetry.Classify("llm_request", err)
				decision := loopAdaptiveRetry.Decide("llm_request", category, 0)
				loopAdaptiveRetry.RecordFailure("llm_request", category, decision)
				h.appendTraceEvent(ctx, "trial.retry_decided", "warn", "Adaptive retry decision", truncateTraceText(fmt.Sprintf("llm_request category=%s action=%s attempt=%d", category, decision.Action, decision.Attempt), 220), "", "")
				h.appendTraceEvidence(ctx, "adaptive_retry", string(category), "retry decision", truncateTraceText(firstNonEmptyTraceText(decision.ErrorContext, err.Error()), 400), "", "llm_request")
				if decision.Action == "retry" && !ctx.IsCancelled() {
					log.Printf("[LLM] AdaptiveRetry: %s 错误，%v 后重试: %v", string(category), decision.Delay, err)
					firstLLMRetryWaitElapsed += decision.Delay
					firstLLMRetryCount++
					// Cancellation-aware sleep: abort wait if user cancels.
					select {
					case <-time.After(decision.Delay):
					case <-ctx.CancelC:
					}
					if !ctx.IsCancelled() {
						llmCallStartedAt = time.Now()
						retryMetrics := &llmStreamMetrics{}
						resp, err = h.doLLMRequestStream(loopCtx, cfg, conversation, tools, httpClient, onToken, retryMetrics)
						if err == nil && firstLLMResponseAt.IsZero() {
							if !retryMetrics.FirstTokenAt.IsZero() {
								firstLLMResponseAt = retryMetrics.FirstTokenAt
							} else {
								firstLLMResponseAt = time.Now()
							}
						}
						if !firstLLMRequestMarked || firstLLMRequestBuildElapsed == 0 {
							firstLLMRequestBuildElapsed += time.Duration(retryMetrics.RequestBuildNanos)
							firstLLMHTTPDoElapsed += time.Duration(retryMetrics.HTTPDoNanos)
							firstLLMFirstSSEWaitElapsed += time.Duration(retryMetrics.FirstSSEWaitNanos)
							firstLLMStreamMaxTokenGapElapsed += time.Duration(retryMetrics.MaxTokenGapNanos)
						}
						firstLLMIdleTimeoutCount += retryMetrics.IdleTimeoutCount
						firstLLMIdleTimeoutAfterToken = firstLLMIdleTimeoutAfterToken || retryMetrics.IdleTimeoutAfterToken
						if err == nil && streamDoneCallback != nil {
							streamDoneCallback()
						}
					}
				}
			} else if isRetryableLLMError(err) && !ctx.IsCancelled() {
				log.Printf("[LLM] 首次请求超时/网络错误，2s 后重试: %v", err)
				firstLLMRetryWaitElapsed += 2 * time.Second
				firstLLMRetryCount++
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.CancelC:
				}
				if !ctx.IsCancelled() {
					llmCallStartedAt = time.Now()
					retryMetrics := &llmStreamMetrics{}
					resp, err = h.doLLMRequestStream(loopCtx, cfg, conversation, tools, httpClient, onToken, retryMetrics)
					if err == nil && firstLLMResponseAt.IsZero() {
						if !retryMetrics.FirstTokenAt.IsZero() {
							firstLLMResponseAt = retryMetrics.FirstTokenAt
						} else {
							firstLLMResponseAt = time.Now()
						}
					}
					firstLLMRequestBuildElapsed += time.Duration(retryMetrics.RequestBuildNanos)
					firstLLMHTTPDoElapsed += time.Duration(retryMetrics.HTTPDoNanos)
					firstLLMFirstSSEWaitElapsed += time.Duration(retryMetrics.FirstSSEWaitNanos)
					firstLLMStreamMaxTokenGapElapsed += time.Duration(retryMetrics.MaxTokenGapNanos)
					firstLLMIdleTimeoutCount += retryMetrics.IdleTimeoutCount
					firstLLMIdleTimeoutAfterToken = firstLLMIdleTimeoutAfterToken || retryMetrics.IdleTimeoutAfterToken
					if err == nil && streamDoneCallback != nil {
						streamDoneCallback()
					}
				}
			}
		}
		// Accumulate token usage stats
		if resp != nil {
			usageStartedAt := time.Now()
			input, output := deriveLLMTokenUsage(resp, conversation)
			providerName := h.getMaclawLLMProviders().Current
			log.Printf("[LLM] usage main_round provider=%q input=%d output=%d usage_nil=%t choices=%d", providerName, input, output, resp.Usage == nil, len(resp.Choices))
			h.accumulateLLMTokenUsage(providerName, input, output)
			lastLLMInputTokens = input
			lastLLMOutputTokens = output
			if !streamDoneAt.IsZero() {
				handlerPostStreamUsageElapsed += time.Since(usageStartedAt)
				postStreamUsageDoneAt = time.Now()
			}
		}
		if err != nil {
			// If the error is due to cancellation, save history and return
			// a clean message instead of an LLM error. The agent loop must
			// never call memory.Clear — history lifecycle is managed by the
			// caller (handleIMMessageWithLoop), not the loop itself.
			if ctx.IsCancelled() {
				ctx.SetState("stopped")
				return h.cancelledExitResponse(userID, history, userText)
			}
			return &IMAgentResponse{Error: fmt.Sprintf("LLM 调用失败: %s [url=%s model=%s protocol=%s]", err.Error(), cfg.URL, cfg.Model, cfg.Protocol)}
		}
		if len(resp.Choices) == 0 {
			return &IMAgentResponse{Error: "LLM 未返回有效回复"}
		}

		choiceStartedAt := time.Now()
		choice := resp.Choices[0]
		if !streamDoneAt.IsZero() {
			handlerPostStreamChoiceElapsed += time.Since(choiceStartedAt)
		}

		// Kimi's kimi-for-coding puts all output in reasoning_content with empty content.
		// Promote reasoning to content so the assistant message is never empty.
		assistantMsgStartedAt := time.Now()
		msgContent := choice.Message.Content
		msgReasoning := choice.Message.ReasoningContent
		if msgContent == "" && msgReasoning != "" {
			msgContent = msgReasoning
		}

		// Strip hallucinated role prefix (e.g. "Browser: ...") that some LLMs
		// produce when browser tool definitions are in context or when the
		// output text mentions browser-related terms (like "Chrome 浏览器进程").
		msgContent = stripRolePrefixHallucination(msgContent)

		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": msgContent,
		}
		if h.traceService != nil && ctx.RunID != "" {
			h.appendTraceEvent(ctx, "assistant.response", "info", "Assistant response", truncateTraceText(msgContent, 220), "", "")
		}
		if msgReasoning != "" {
			assistantMsg["reasoning_content"] = msgReasoning
		} else if len(choice.Message.ToolCalls) > 0 {
			// DeepSeek thinking mode: reasoning_content field must exist on
			// assistant messages with tool_calls. Missing field → HTTP 400.
			assistantMsg["reasoning_content"] = ""
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)
		if recorder != nil {
			recorder.Record("assistant", msgContent, choice.Message.ToolCalls, "", msgReasoning)
		}
		if !streamDoneAt.IsZero() {
			handlerPostStreamAssistantMsgElapsed += time.Since(assistantMsgStartedAt)
		}

		// Update cross-channel activity every 5 iterations.
		if iteration%5 == 0 {
			reportActivity(iteration, effectiveMax, msgContent)
		}

		historyAppendStartedAt := time.Now()
		historyEntry := agent.ConversationEntry{Role: "assistant", Content: msgContent, ReasoningContent: msgReasoning}
		if len(choice.Message.ToolCalls) > 0 {
			historyEntry.ToolCalls = choice.Message.ToolCalls
		}
		history = append(history, historyEntry)
		if !streamDoneAt.IsZero() {
			handlerPostStreamHistoryAppendElapsed += time.Since(historyAppendStartedAt)
		}

		// --- Coding Tool Gate: strip coding tools on ALL iterations ---
		// The gate must remain active for the entire agent loop (not just
		// iteration 0). Within a single user message → agent loop, if the
		// intent is coding and no skip signal, coding tools should never be
		// allowed. The user's confirmation ("确认") arrives as a separate
		// message triggering a new loop where the gate is re-evaluated.
		// Previously this only fired on iteration == 0, so the LLM could
		// output the requirements doc on iteration 1 and then immediately
		// call create_session on iteration 2+ without any enforcement.
		if gateConfig.active && !orchestratorActive() && len(choice.Message.ToolCalls) > 0 {
			gateResult := applyCodingToolGate(choice.Message.ToolCalls)
			if gateResult.applied {
				// Log stripped and preserved tool names.
				strippedNames := make([]string, 0, len(gateResult.stripped))
				for _, tc := range gateResult.stripped {
					strippedNames = append(strippedNames, tc.Function.Name)
				}
				preservedNames := make([]string, 0, len(gateResult.remaining))
				for _, tc := range gateResult.remaining {
					preservedNames = append(preservedNames, tc.Function.Name)
				}
				log.Printf("[coding-gate] activated (iter=%d): stripped=%v preserved=%v reason=%s", iteration, strippedNames, preservedNames, gateConfig.reason)

				// Trace event.
				if h.traceService != nil && ctx.RunID != "" {
					h.appendTraceEvent(ctx, "gate.coding_tool_stripped", "warn",
						"Coding tool gate stripped tools",
						fmt.Sprintf("iteration=%d stripped=%v preserved=%v", iteration, strippedNames, preservedNames), "", "")
				}

				// Update tool calls on the choice and assistantMsg.
				choice.Message.ToolCalls = gateResult.remaining
				if len(gateResult.remaining) == 0 {
					delete(assistantMsg, "tool_calls")
				} else {
					assistantMsg["tool_calls"] = gateResult.remaining
				}
				// Also update the already-appended conversation and history entries.
				if len(gateResult.remaining) == 0 {
					if m, ok := conversation[len(conversation)-1].(map[string]interface{}); ok {
						delete(m, "tool_calls")
					}
					history[len(history)-1] = agent.ConversationEntry{Role: "assistant", Content: msgContent, ReasoningContent: msgReasoning}
				} else {
					if m, ok := conversation[len(conversation)-1].(map[string]interface{}); ok {
						m["tool_calls"] = gateResult.remaining
					}
					entry := history[len(history)-1]
					entry.ToolCalls = gateResult.remaining
					history[len(history)-1] = entry
				}

				// If all tools stripped, inject system message and force return.
				// On iteration 0: inject prompt to generate requirements doc.
				// On iteration 1+: the LLM has already produced the doc, so
				// force-return the accumulated text to the user for confirmation
				// instead of continuing the loop (which would let the LLM
				// attempt coding tools again on the next iteration).
				if len(gateResult.remaining) == 0 {
					if iteration == 0 && strings.TrimSpace(msgContent) == "" {
						// First iteration, no text yet — prompt for requirements doc.
						systemMessagesStart := len(conversation)
						conversation = append(conversation, map[string]string{
							"role":    "system",
							"content": "[编程工作流] 你刚才尝试直接调用编码工具，但当前处于需求确认阶段。请先生成需求文档（包含功能需求、非功能需求、边界情况、验收标准），等待用户确认后再开始编码。",
						})
						recordSystemMessages(systemMessagesStart, conversation)
						continue
					}
					// Iteration 1+: LLM already produced text (requirements doc)
					// but is now trying to call coding tools. Force-return the
					// response so the user can review and confirm.
					if strings.TrimSpace(msgContent) != "" {
						log.Printf("[coding-gate] iter=%d: coding tools stripped after doc output, force-returning for user confirmation", iteration)
						phase.Stage = agentStageFinalize
						strippedContent := stripThinkingTags(msgContent)
						finalResp := &IMAgentResponse{Text: strippedContent}

						// Desktop: intercept text output for the doc preview panel.
						if platform == "desktop" && h.getWorkflowEngine() != nil {
							trimmedStripped := strings.TrimSpace(strippedContent)
							emitted := false
							if steeringDetector != nil {
								steeringDetector.interceptTextOutput(trimmedStripped, func(phaseID, content string) {
									if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
										_ = adapter.EmitDocUpdate(userID, phaseID, content)
										log.Printf("[SteeringWorkflow] emitted doc_update from gate force-return for user=%s phase=%s len=%d", userID, phaseID, len(content))
										emitted = true
									}
								})
							}
							// Fallback: WorkflowEngine active but steering detector not activated.
							if !emitted && len(trimmedStripped) >= 50 {
								if ws := h.getWorkflowEngine().GetActiveWorkflow(userID); ws != nil {
									if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
										_ = adapter.EmitDocUpdate(userID, ws.CurrentPhase, trimmedStripped)
										log.Printf("[WorkflowEngine] emitted doc_update from gate force-return for user=%s phase=%s len=%d", userID, ws.CurrentPhase, len(trimmedStripped))
									}
								}
							}
						}
						attachLLMTelemetry(finalResp)
						h.saveConversationHistoryTimed(userID, history, finalResp)
						return finalResp
					}
					// No text and not iteration 0 — inject reminder and continue.
					systemMessagesStart := len(conversation)
					conversation = append(conversation, map[string]string{
						"role":    "system",
						"content": "[编程工作流] 编码工具已被拦截。请先完成需求文档并等待用户确认，不要尝试调用编码工具。",
					})
					recordSystemMessages(systemMessagesStart, conversation)
					continue
				}
			}
		} else if !gateConfig.active && iteration == 0 && len(choice.Message.ToolCalls) > 0 {
			log.Printf("[coding-gate] DEBUG: gate inactive: %s", gateConfig.reason)
		}

		if !streamDoneAt.IsZero() {
			noToolBranchStartedAt := time.Now()
			defer func() {
				handlerPostStreamNoToolBranchElapsed += time.Since(noToolBranchStartedAt)
			}()
		}
		if len(choice.Message.ToolCalls) == 0 {
			phase.Stage = agentStageConverge
			phase.ConsecutiveNoTool++

			// --- Tool availability hallucination correction ---
			// When the LLM falsely claims a tool is unavailable but the tool
			// IS in the current tools list sent to the LLM, inject a correction.
			// This is a mechanism-level check: we compare the LLM's claim
			// against the actual tool list, not a hardcoded set.
			// One-shot per loop to avoid infinite correction cycles.
			if !phase.ToolHallucinationCorrected {
				if correction := detectToolAvailabilityHallucination(msgContent, tools); correction != "" {
					phase.ToolHallucinationCorrected = true
					phase.ConsecutiveNoTool = 0
					log.Printf("[agent-loop] tool availability hallucination detected, injecting correction (iter=%d)", iteration)
					conversation = append(conversation, map[string]interface{}{
						"role":    "user",
						"content": correction,
					})
					continue
				}
			}

			// --- finish_reason=length continuation for text-only responses ---
			// When the LLM's text output is truncated (finish_reason="length")
			// and there are no tool calls, the model hit its output token limit
			// mid-sentence. Instead of returning the truncated text to the user,
			// inject a continuation prompt so the LLM can finish its output.
			//
			// This is especially important when the coding tool gate is active:
			// the LLM is forced to output text-only (coding tools are stripped),
			// and may produce very long narrations that get truncated. Without
			// this handling, the user sees a confusing wall of text ending
			// mid-sentence.
			//
			// Cap at 3 continuations to prevent infinite loops when the model
			// keeps producing max-length outputs.
			const maxLengthContinuations = 3
			if choice.FinishReason == "length" && phase.LengthContinuations < maxLengthContinuations {
				phase.LengthContinuations++
				phase.ConsecutiveNoTool = 0 // reset — this is not a stall
				// Accumulate the truncated chunk so the final response contains
				// the full document, not just the last continuation's output.
				lengthContinuationBuf.WriteString(msgContent)
				log.Printf("[agent-loop] finish_reason=length on text-only response (continuation %d/%d, iter=%d, textLen=%d, accumulated=%d), injecting continuation prompt",
					phase.LengthContinuations, maxLengthContinuations, iteration, len(msgContent), lengthContinuationBuf.Len())
				systemMessagesStart := len(conversation)
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": "[系统提示] 你的输出因长度限制被截断。请从截断处继续输出，不要重复已输出的内容。",
				})
				recordSystemMessages(systemMessagesStart, conversation)
				continue
			}

			// --- Workflow NeedsConfirm hard gate ---
			// When the current workflow phase requires user confirmation
			// (e.g. requirements, tech design, task breakdown), and the LLM
			// has produced a substantive deliverable (not a stall/thinking
			// reply), return immediately. This prevents stall/deliverable
			// heuristics (which match keywords like "文档", "写", "生成")
			// from misclassifying the confirmation prompt as a "promise to
			// deliver" and forcing another round that re-outputs the content.
			needsConfirmFromEngine := false
			if h.getWorkflowEngine() != nil {
				// When the semantic classifier says the user's message is
				// maintenance/bug_fix (not a workflow phase request), skip
				// the engine's NeedsConfirm gate. This prevents maintenance
				// tasks from being force-returned as phase documents.
				semanticBypass := false
				if !gateConfig.active && gateConfig.intent == intentCoding {
					semanticBypass = true // maintenance or bug_fix
				}
				if gateConfig.bugFix {
					semanticBypass = true
				}
				// When handlePendingConfirm classified the message as "other"
				// (unrelated to the workflow), skip the NeedsConfirm gate so
				// the unrelated output (e.g. weather info) is not captured as
				// a phase document.
				//
				// EXCEPTION: When gateConfig.active is true, the current
				// message is a NEW coding task that needs the three-phase
				// flow (requirements → design → task breakdown). In this
				// case, do NOT bypass — let the Coding Tool Gate enforce
				// the three-phase flow even though the message came through
				// the "other" path of handlePendingConfirm.
				if ctx.SkipNeedsConfirmGate && !gateConfig.active {
					semanticBypass = true
				}
				if !semanticBypass {
					needsConfirmFromEngine = h.getWorkflowEngine().IsPhaseNeedsConfirm(userID)
				} else {
					log.Printf("[workflow-gate] NeedsConfirm no-tool engine bypassed: semantic intent=%v active=%v bugFix=%v skipConfirmOther=%v",
						gateConfig.intent, gateConfig.active, gateConfig.bugFix, ctx.SkipNeedsConfirmGate)
				}
			}
			// Steering-based coding workflow: when the coding tool gate is
			// active (intentCoding, no skip signal) and the LLM has produced
			// substantive text (likely the requirements doc), treat it as a
			// NeedsConfirm phase. This covers the case where no workflow
			// engine workflow is active but the steering rules enforce the
			// three-phase flow.
			//
			// IMPORTANT: When the WorkflowEngine has an active workflow, we
			// defer to IsPhaseNeedsConfirm() instead of blindly using
			// gateConfig.active. The implementation phase has NeedsConfirm=false,
			// so the steering gate must NOT trigger during code execution.
			// Only fall back to gateConfig.active when there is NO engine
			// workflow (pure steering-driven flow).
			needsConfirmFromSteering := false
			if gateConfig.active && iteration > 0 {
				if h.getWorkflowEngine() != nil && h.getWorkflowEngine().GetActiveWorkflow(userID) != nil {
					// Engine owns the workflow — delegate to phase-aware check.
					// IsPhaseNeedsConfirm returns false for implementation/execution phases.
					needsConfirmFromSteering = h.getWorkflowEngine().IsPhaseNeedsConfirm(userID)
					if !needsConfirmFromSteering && platform == "desktop" {
						log.Printf("[agent-loop] NeedsConfirm steering bypassed: engine workflow active, phase NeedsConfirm=false (iter=%d user=%s)", iteration, userID)
					}
				} else {
					// No engine workflow — pure steering-driven flow, use gate as before.
					needsConfirmFromSteering = true
				}
			}
			engineGateActive := needsConfirmFromEngine
			if engineGateActive && h.getWorkflowEngine() != nil {
				if !h.getWorkflowEngine().HasPhaseOutput(userID) {
					engineGateActive = false
					log.Printf("[agent-loop] NeedsConfirm gate: first execution (hasOutput=false), allowing loop to continue (iter=%d)", iteration)
				}
			}
			if platform == "desktop" && (engineGateActive || needsConfirmFromSteering) {
				log.Printf("[agent-loop] NeedsConfirm check: engine=%v steering=%v iteration=%d msgLen=%d steeringDetector=%v user=%s",
					needsConfirmFromEngine, needsConfirmFromSteering, iteration, len(strings.TrimSpace(stripThinkingTags(msgContent))), steeringDetector != nil, userID)
			}
			if engineGateActive || needsConfirmFromSteering {
				trimmedForGate := strings.TrimSpace(stripThinkingTags(msgContent))

				// Self-confirmation detection: detect when the LLM both requests
				// confirmation AND self-answers it in the same response.
				// Truncate at the confirmation request boundary to prevent the
				// self-answer from being returned to the user.
				if containsSelfConfirmationPattern(trimmedForGate) {
					originalLen := len(trimmedForGate)
					trimmedForGate = truncateAtConfirmationBoundary(trimmedForGate)
					msgContent = trimmedForGate
					log.Printf("NeedsConfirm gate: detected self-confirmation pattern, truncated at confirmation boundary (originalLen=%d truncatedLen=%d)", originalLen, len(trimmedForGate))
					if h.traceService != nil && ctx.RunID != "" {
						h.appendTraceEvent(ctx, "gate.self_confirm_truncated", "warn",
							fmt.Sprintf("Self-confirmation detected and truncated (originalLen=%d truncatedLen=%d)", originalLen, len(trimmedForGate)),
							truncateTraceText(trimmedForGate, 220), "", "")
					}
				}

				if trimmedForGate != "" &&
					!looksLikeNoToolStallReply(msgContent) &&
					isSubstantivePhaseDocument(trimmedForGate) {
					gateSource := "workflow"
					if needsConfirmFromSteering && !engineGateActive {
						gateSource = "steering"
					}
					log.Printf("[agent-loop] NeedsConfirm gate (%s): returning response for user confirmation (iteration=%d len=%d)", gateSource, iteration, len(trimmedForGate))
					if h.traceService != nil && ctx.RunID != "" {
						h.appendTraceEvent(ctx, "gate.needs_confirm", "info",
							fmt.Sprintf("NeedsConfirm phase gate (%s) — pausing for user confirmation", gateSource),
							truncateTraceText(trimmedForGate, 220), "", "")
					}
					phase.Stage = agentStageFinalize
					// Use accumulated continuation text if available.
					gateText := msgContent
					if lengthContinuationBuf.Len() > 0 {
						gateText = lengthContinuationBuf.String() + msgContent
					}
					finalResp := &IMAgentResponse{Text: stripThinkingTags(gateText)}
					if !streamDoneAt.IsZero() {
						postStreamLastReturnPrepAt = time.Now()
					}

					// Desktop: intercept text output for the doc preview panel.
					// Use the full accumulated text (gateText) for doc preview,
					// not just the current chunk (trimmedForGate).
					docPreviewText := strings.TrimSpace(stripThinkingTags(gateText))
					if platform == "desktop" && h.getWorkflowEngine() != nil {
						if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
							emitted := false
							// Path 1: steering detector (coding workflow without engine)
							if steeringDetector != nil {
								steeringDetector.interceptTextOutput(docPreviewText, func(phaseID, content string) {
									_ = adapter.EmitDocUpdate(userID, phaseID, content)
									log.Printf("[SteeringWorkflow] emitted doc_update from text output for user=%s phase=%s len=%d", userID, phaseID, len(content))
									emitted = true
								})
							}
							// Path 2: WorkflowEngine active — use the engine's current phase directly.
							// This covers all workflow templates (coding, product_design, innovation, etc.)
							// where the steering detector is not activated (because the engine owns the workflow).
							// Use a lower threshold (50 chars) than the old 200 — the engine already confirmed
							// this is a NeedsConfirm phase, so even shorter text is a valid deliverable.
							// Also emit suggest_maximize in case it wasn't emitted yet (e.g. dedup cleared).
							if !emitted && engineGateActive && len(docPreviewText) >= 50 {
								if ws := h.getWorkflowEngine().GetActiveWorkflow(userID); ws != nil {
									_ = adapter.EmitDocUpdate(userID, ws.CurrentPhase, docPreviewText)
									adapter.EmitSuggestMaximize(userID, string(ws.Type))
									log.Printf("[WorkflowEngine] emitted doc_update for user=%s phase=%s type=%s len=%d", userID, ws.CurrentPhase, ws.Type, len(docPreviewText))
								} else {
									log.Printf("[WorkflowEngine] WARNING: engineGateActive=true but GetActiveWorkflow returned nil for user=%s", userID)
								}
							}
						}
					} else if platform == "desktop" {
						log.Printf("[agent-loop] NeedsConfirm gate: skipped doc preview emission (workflowEngine=%v)", h.getWorkflowEngine() != nil)
					}
					attachLLMTelemetry(finalResp)
					h.saveConversationHistoryTimed(userID, history, finalResp)
					return finalResp
				} else if trimmedForGate != "" && !looksLikeNoToolStallReply(msgContent) {
					// isSubstantivePhaseDocument was false — short preamble, let the loop continue.
					log.Printf("[agent-loop] NeedsConfirm gate: skipping non-substantive preamble (len=%d), allowing loop to continue", len([]rune(trimmedForGate)))
				}
			}

			// Hard cap: if the model has returned text without tool calls for
			// too many consecutive iterations, force-return the latest response.
			// This prevents infinite loops caused by false-positive stall/deliverable
			// detection (e.g. self-introduction text matching "文档"/"写" keywords).
			const maxConsecutiveNoTool = 5
			trimmedVisibleContent := strings.TrimSpace(stripThinkingTags(msgContent))
			if phase.ConsecutiveNoTool > maxConsecutiveNoTool && trimmedVisibleContent != "" {
				log.Printf("[agent-loop] hard cap: %d consecutive no-tool iterations, force-returning response", phase.ConsecutiveNoTool)
				phase.Stage = agentStageFinalize
				hardCapText := msgContent
				if lengthContinuationBuf.Len() > 0 {
					hardCapText = lengthContinuationBuf.String() + msgContent
				}
				finalResp := &IMAgentResponse{Text: stripThinkingTags(hardCapText)}
				if !streamDoneAt.IsZero() {
					postStreamLastReturnPrepAt = time.Now()
				}
				attachLLMTelemetry(finalResp)
				h.saveConversationHistoryTimed(userID, history, finalResp)
				return finalResp
			}

			emptyVisibleResult := trimmedVisibleContent == ""
			promiseOnlyDeliverable := shouldForceAnotherRoundForDeliverable(msgContent, len(choice.Message.ToolCalls), 0)
			noToolStall := looksLikeNoToolStallReply(msgContent) || emptyVisibleResult || promiseOnlyDeliverable
			hasPendingSkillRun := strings.TrimSpace(phase.PreferredSkillRunID) != ""
			preferSkill := phase.ForceSkillPreference && phase.PreferredSkillName != ""
			effectiveNoToolRecoverThreshold := stalledNoToolRecoverThreshold
			if hasPendingSkillRun || phase.SkillFailed || phase.Stage == agentStageRecover {
				effectiveNoToolRecoverThreshold = 1
			}
			pendingSkillRunNoToolRecover := shouldRecoverForPendingSkillRunNoToolReply(msgContent, phase.PreferredSkillRunID)
			noToolPrompt := buildNoToolActionPrompt(preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID)
			if shouldRestrictToSkillSearch(phase) {
				noToolPrompt = buildRemoteSkillSearchPrompt()
			}
			if pendingSkillRunNoToolRecover {
				enterRecoverPhase(&phase, "pending_skill_run_no_tool", buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
				continue
			}
			if emptyVisibleResult && phase.SkillFailed {
				enterRecoverPhase(&phase, "no_tool_stall", buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
				continue
			}
			if emptyVisibleResult {
				phase.ConsecutiveEmptyResponses++

				// Hard exit: if the model has returned empty content too many
				// times in a row, or total recover injections exceeded the cap,
				// stop the loop. Continuing only inflates context and worsens
				// the empty-response problem (common with glm-5.1 at >100K tokens).
				if phase.ConsecutiveEmptyResponses >= maxConsecutiveEmptyResponses ||
					phase.TotalRecoverInjections >= maxTotalRecoverInjections {
					log.Printf("[agent-loop] hard exit: %d consecutive empty responses, %d total recovers — returning best available result",
						phase.ConsecutiveEmptyResponses, phase.TotalRecoverInjections)
					phase.Stage = agentStageFinalize
					// Try to return the last non-empty assistant message from history.
					fallbackText := findLastAssistantContent(history)
					if fallbackText == "" {
						fallbackText = "抱歉，模型多次返回空响应，无法继续处理。请尝试简化请求或重新发送。"
					}
					finalResp := &IMAgentResponse{Text: fallbackText, HardExit: true}
					attachLLMTelemetry(finalResp)
					h.saveConversationHistoryTimed(userID, history, finalResp)
					return finalResp
				}

				enterRecoverPhase(&phase, "empty_final_response", buildEmptyResultRecoverPrompt())
				continue
			}
			// Reset consecutive empty counter on non-empty response.
			phase.ConsecutiveEmptyResponses = 0
			if promiseOnlyDeliverable {
				phase.DeliverableRecoverCount++
				if phase.DeliverableRecoverCount >= effectiveNoToolRecoverThreshold {
					enterRecoverPhase(&phase, "no_tool_stall", buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
					continue
				}
				enterRecoverPhase(&phase, "deliverable_pending", buildDeliverableRecoverPrompt(phase.PreferredSkillName, preferSkill, phase.PreferredSkillRunID))
				if shouldBypassSkillPreference(choice.Message.ToolCalls) {
					phase.ForceSkillPreference = false
				}
				if h.traceService != nil && ctx.RunID != "" {
					h.appendTraceEvent(ctx, "delivery.nudged", "warn", "Forced deliverable follow-up", truncateTraceText(msgContent, 220), "", "")
				}
				continue
			}
			phase.DeliverableRecoverCount = 0
			if phase.ConsecutiveNoTool == 1 && (looksLikeNoToolStallReply(msgContent) || hasPendingSkillRun || (phase.ForceSkillPreference && !phase.SkillAttempted)) {
				systemMessagesStart := len(conversation)
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": noToolPrompt,
				})
				recordSystemMessages(systemMessagesStart, conversation)
				if phase.ForceSkillPreference {
					phase.SkillAttempted = true
				}
				continue
			}
			if noToolStall && (phase.ConsecutiveNoTool >= effectiveNoToolRecoverThreshold || phase.DeliverableRecoverCount >= effectiveNoToolRecoverThreshold || (phase.SkillFailed && phase.ConsecutiveNoTool >= 1)) {
				enterRecoverPhase(&phase, "no_tool_stall", buildNoToolStallRecoverPrompt(phase.ConsecutiveNoTool, preferSkill, phase.PreferredSkillName, phase.PreferredSkillRunID))
				continue
			}
			// Check for capability gap in the background (async).
			// Uses SkillSearcher (SkillMarket → ClawHub mirror → GitHub) for
			// unified search order, then installAndExecuteSkill for the install
			// path — no duplicate searches.
			//
			// IMPORTANT: This runs as a goroutine AFTER the response is returned
			// to the user, so the input box is unlocked immediately. If a skill
			// is found and installed, the result is stored in pendingCapabilityGap
			// and injected into the next conversation turn's system prompt.
			//
			// Skip when the LLM has been actively working (many tool-call
			// iterations) or the response is long — these are summaries/reports,
			// not "I can't do this" signals.
			skipCapabilityGap := iteration >= 3 || len(trimmedVisibleContent) > 500
			if !skipCapabilityGap && h.capabilityGapDetector != nil && h.capabilityGapDetector.Detect(msgContent) {
				// Capture values for the goroutine closure.
				capturedUserText := userText
				capturedUserID := userID
				go func() {
					goCtx, goCancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer goCancel()
					smClient := NewSkillMarketClient(h.app)
					searcher := NewSkillSearcher(smClient)
					best, searchErr := searcher.SearchAndInstall(goCtx, capturedUserText)
					if searchErr != nil || best == nil {
						return
					}
					log.Printf("[skill-auto-async] found skill: %s (%s)", best.Name, best.Status)
					installResult := h.installAndExecuteSkill(goCtx, best, capturedUserText,
						func(status string) {
							log.Printf("[skill-auto-async] %s", status)
						})
					success := strings.HasPrefix(installResult, "✅")
					h.pendingCapabilityGap.Store(capturedUserID, &pendingCapabilityGapResult{
						SkillName: best.Name,
						Result:    installResult,
						Success:   success,
						Timestamp: time.Now(),
					})
					if success {
						log.Printf("[skill-auto-async] skill %q installed successfully, result pending for next turn", best.Name)
						// Notify frontend so user sees a toast notification.
						if h.app != nil {
							h.emitAppEvent("skill-auto-installed", map[string]string{
								"name":   best.Name,
								"result": installResult,
							})
						}
					} else {
						log.Printf("[skill-auto-async] skill install/execute finished without success: %s", installResult)
					}
				}()
			}
			phase.Stage = agentStageFinalize
			// When finish_reason=length continuations occurred, the accumulated
			// buffer contains all chunks. Append the final chunk and use the
			// full text as the response. This ensures post-loop doc capture
			// (SavePhaseOutput) saves the complete document, not just the last chunk.
			finalText := msgContent
			if lengthContinuationBuf.Len() > 0 {
				finalText = lengthContinuationBuf.String() + msgContent
				log.Printf("[agent-loop] assembled %d continuation chunks into final response (totalLen=%d)", phase.LengthContinuations+1, len(finalText))
			}
			finalResp := &IMAgentResponse{Text: stripThinkingTags(finalText)}
			if !streamDoneAt.IsZero() {
				postStreamLastReturnPrepAt = time.Now()
				handlerPostStreamResponseStartedAt := time.Now()
				defer func() {
					handlerPostStreamResponseElapsed += time.Since(handlerPostStreamResponseStartedAt)
				}()
			}

			// Workaround detection: if a skill failed earlier in this loop
			// and the LLM has now produced a final response (resolved the
			// task through alternative tool calls), classify the original
			// skill failure as a "workaround" outcome.
			if phase.FailedSkillName != "" && h.app != nil && h.getSkillRunner() != nil {
				h.getSkillRunner().RecordWorkaround(phase.FailedSkillName, phase.FailedSkillError)
				log.Printf("[skill-workaround] skill %q failure classified as workaround — LLM resolved task via alternative tools", phase.FailedSkillName)
			}

			// Nudge injection: append nudge system messages to history
			// AFTER the current response. They will be visible to the LLM
			// in the next conversation turn, not the current one.
			history = h.injectNudgeMessages(history, iteration, totalToolCallsInLoop, phase, userText)

			attachLLMTelemetry(finalResp)
			h.saveConversationHistoryTimed(userID, history, finalResp)
			return finalResp
		}

		phase.Stage = agentStageExecute
		phase.ConsecutiveNoTool = 0

		// --- Steering Workflow: emit suggest_maximize on first tool call ---
		// Only emit when the gate actually intercepted coding tools in this
		// iteration. If the detector was activated only from conversation
		// history context (Tier 2), we should NOT emit the banner just
		// because the LLM called a non-coding tool (e.g. run_skill for
		// weather). The banner should only appear when the LLM genuinely
		// attempts coding work.
		if steeringDetector != nil && !steeringDetector.suggestMaximizeEmitted && gateConfig.active {
			steeringDetector.suggestMaximizeEmitted = true
			if h.getWorkflowEngine() != nil {
				if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
					adapter.EmitSuggestMaximize(userID, "coding")
					log.Printf("[SteeringWorkflow] emitted suggest_maximize for user=%s (gate active, first tool call)", userID)
				}
			}
		}

		// Execute tool calls and feed results back.
		if trialState.enabled && h.traceService != nil && ctx.RunID != "" {
			h.appendTraceEvent(ctx, "trial.started", "info", "Trial iteration started", fmt.Sprintf("iteration=%d tool_calls=%d", iteration+1, len(choice.Message.ToolCalls)), "", "")
		}
		var pendingImageKey string
		type pendingFile struct {
			name, mimeType, data string
			forwardIM            bool
			message              string // IM delivery prompt
		}
		var pendingFiles []pendingFile
		screenshotAlreadySent := false
		toolResults := make([]string, 0, len(choice.Message.ToolCalls))
		totalToolCallsInLoop += len(choice.Message.ToolCalls)
		for _, tc := range choice.Message.ToolCalls {
			// Check cancellation between tool calls so we don't keep
			// executing tools after the user clicked cancel. Save history
			// so the next message retains full context.
			if ctx.IsCancelled() {
				ctx.SetState("stopped")
				return h.cancelledExitResponse(userID, history, userText)
			}
			toolLabel := userFacingToolProgressText(tc.Function.Name)
			// Record "starting" milestone — pusher decides whether to show it.
			milestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
			if isDebug() {
				sendToolProgress(toolLabel)
			}
			// When debug is off, suppress intermediate progress from tool execution too.
			toolOnProgress := onProgress
			if !isDebug() {
				toolOnProgress = nil
				if onProgress != nil && shouldExposeToolInternalProgress(tc.Function.Name) {
					toolName := tc.Function.Name
					toolOnProgress = func(msg string) {
						if filtered := filterUserFacingToolProgress(toolName, msg); filtered != "" {
							onProgress(filtered)
						}
					}
				}
			}
			toolExecStartedAt := time.Now()
			recordToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments, toolOnProgress)

			// Record "completed" milestone for event-driven progress.
			milestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)

			// Handle ask_user: pause the loop and present the question to the user.
			// The question text becomes the visible response; the agent loop will
			// resume when the user replies in the next message.
			//
			// HARD GUARD: When the coding tool gate is active (three-phase workflow),
			// ask_user is NOT allowed for phase confirmations. Convert it to a plain
			// text tool result so the LLM's question text flows into msgContent
			// naturally, and the loop can force-return for user confirmation via the
			// NeedsConfirm gate instead of popping a button UI.
			if askReq, ok := ParseAskUserResult(result); ok {
				if gateConfig.active {
					// Coding workflow active: convert ask_user to plain tool result.
					// The LLM's question becomes part of the normal text flow.
					plainText := askReq.Question
					if len(askReq.Options) > 0 {
						plainText += "\n"
						for i, opt := range askReq.Options {
							plainText += fmt.Sprintf("\n%d. %s", i+1, opt)
						}
					}
					result = fmt.Sprintf("编码工作流中不使用 ask_user 弹窗确认。请将确认提示直接写在回复文本中，用户会直接在输入框回复。你的问题是: %s", plainText)
					log.Printf("[coding-gate] ask_user intercepted: converted to plain text (question=%q)", askReq.Question)
					// Fall through to normal tool result handling below.
				} else {
					displayText := FormatAskUserForDisplay(askReq)
					// Prepend any LLM text output from this iteration (e.g. a
					// requirements document) that would otherwise be lost when
					// we return early for ask_user. This is critical for IM
					// channels like Lanxin (蓝信) that don't support interactive
					// cards — the document content must be in resp.Text.
					// Skip for desktop: the user already sees msgContent via
					// streaming tokens, and the frontend's SPECIAL_RESPONSE_SOURCES
					// logic would replace the streamed content with resp.Text,
					// causing a duplicate display.
					if platform != "desktop" {
						if trimmedMsg := strings.TrimSpace(stripThinkingTags(msgContent)); trimmedMsg != "" {
							displayText = trimmedMsg + "\n\n---\n\n" + displayText
						}
					}
					toolResults = append(toolResults, fmt.Sprintf("用户被提问: %s（等待回答）", askReq.Question))
					recordToolResult(tc.ID, toolResults[len(toolResults)-1])

					conversation = append(conversation, map[string]interface{}{
						"role":         "tool",
						"tool_call_id": tc.ID,
						"content":      toolResults[len(toolResults)-1],
					})
					history = append(history, agent.ConversationEntry{
						Role: "tool", Content: toolResults[len(toolResults)-1], ToolCallID: tc.ID,
					})
					h.saveConversationHistoryTimed(userID, history, nil)

					h.pendingAskUser.Store(userID, &pendingAskUserState{
						Question:  askReq.Question,
						Options:   askReq.Options,
						InputType: askReq.InputType,
						Timestamp: time.Now(),
					})

					resp := &IMAgentResponse{
						Text:           displayText,
						ResponseSource: "ask_user",
					}
					if askReq.InputType == "choice" && len(askReq.Options) > 0 {
						actions := make([]IMResponseAction, len(askReq.Options))
						for i, opt := range askReq.Options {
							actions[i] = IMResponseAction{Label: opt, Command: opt}
						}
						resp.Actions = actions
					} else if askReq.InputType == "confirm" {
						resp.Actions = []IMResponseAction{
							{Label: "✅ 确认", Command: "确认"},
							{Label: "❌ 取消", Command: "取消"},
						}
					}
					return resp
				}
			}

			// Handle delegate_task: inject sub-agent context into the tool result.
			// The sub-agent's specialized prompt becomes part of the conversation,
			// guiding the main agent's behavior for the delegated task.
			if IsSubAgentContext(result) {
				result = ExtractSubAgentContext(result)
			}

			// Pin conditional tools (e.g. ssh, web_search) to the session
			// after first successful use so they remain available for
			// follow-up messages that may not contain the trigger keywords.
			// Some tools (e.g. generate_pdf) are excluded from pinning
			// because they should only appear in specific contexts.
			if h.toolRouter != nil && tool.ShouldPinConditionalTool(tc.Function.Name) &&
				!strings.HasPrefix(result, "未知工具") && !strings.HasPrefix(result, "工具执行异常") {
				h.toolRouter.ActivateSessionTool(tc.Function.Name)
				log.Printf("[ToolPin] session-pinned conditional tool %q", tc.Function.Name)
			}

			traceResult := result
			toolContent := result
			if strings.HasPrefix(result, "[screenshot_base64]") {
				traceResult = "截图已成功捕获，将作为图片发送给用户。"
				toolContent = traceResult
			}
			if result == "[screenshot_sent]" {
				traceResult = "截图已成功捕获并发送给用户。"
				toolContent = traceResult
			}
			if strings.HasPrefix(result, "[file_base64|") {
				traceResult = "文件已生成，等待解析发送结果。"
			}
			if !streamDoneAt.IsZero() {
				handlerPostStreamToolExecElapsed += time.Since(toolExecStartedAt)
			}
			toolResults = append(toolResults, traceResult)

			// Intercept direct screenshot results: extract base64 image data
			// so it can be delivered via IM image channel instead of text.
			if strings.HasPrefix(result, "[screenshot_base64]") {
				pendingImageKey = strings.TrimPrefix(result, "[screenshot_base64]")
			}

			// Intercept session-based screenshot: image was already pushed
			// via session.image WebSocket channel, so we just need to stop
			// the agent loop — no image data to carry in the response.
			if result == "[screenshot_sent]" {
				screenshotAlreadySent = true
			}

			// Intercept file send results: collect ALL files (not just the last one).
			// Format: [file_base64|filename|mimetype]data
			//     or: [file_base64|filename|mimetype|im]data  (forward to IM)
			//     or: [file_base64|filename|mimetype|im|msg:提示信息]data
			if strings.HasPrefix(result, "[file_base64|") {
				rest := strings.TrimPrefix(result, "[file_base64|")
				if closeBracket := strings.Index(rest, "]"); closeBracket > 0 {
					meta := rest[:closeBracket]
					parts := strings.Split(meta, "|")
					if len(parts) >= 2 {
						fwd := false
						mType := parts[1]
						var fileMsg string
						// Scan remaining segments for flags.
						for i := 2; i < len(parts); i++ {
							seg := parts[i]
							if seg == "im" {
								fwd = true
							} else if strings.HasPrefix(seg, "msg:") {
								fileMsg = strings.TrimPrefix(seg, "msg:")
							} else {
								// Unknown segment — append to mimeType for safety.
								mType += "|" + seg
							}
						}
						// Fallback: auto-generate prompt based on filename if none provided.
						if fwd && fileMsg == "" {
							fileMsg = inferFileDeliveryMessage(parts[0])
						}
						pendingFiles = append(pendingFiles, pendingFile{
							name:      parts[0],
							mimeType:  mType,
							data:      rest[closeBracket+1:],
							forwardIM: fwd,
							message:   fileMsg,
						})
						if fwd {
							toolContent = fmt.Sprintf("文件 %s 已准备好，将通过 IM 通道发送给用户。", parts[0])
						} else {
							toolContent = fmt.Sprintf("文件 %s 已准备好，将发送给用户。", parts[0])
						}
						traceResult = toolContent
					}
				}
			}
			if h.traceService != nil && ctx.RunID != "" {
				h.appendTraceEvent(ctx, "tool.executed", "info", tc.Function.Name, truncateTraceText(traceResult, 220), "", tc.Function.Name)
				h.appendTraceEvidence(ctx, "ai_tool", traceCategoryForToolResult(tc.Function.Name, traceResult), tc.Function.Name, truncateTraceText(traceResult, 400), "", tc.Function.Name)
				if tc.Function.Name == "create_session" && h.manager != nil {
					if linkedRunID := h.linkTraceToLatestAISession(ctx, result); linkedRunID != "" {
						h.appendTraceEvent(ctx, "session.linked", "info", "Linked remote session", linkedRunID, "", "")
					}
				}
			}

			// --- Steering Workflow: intercept write_file and generate_pdf ---
			// Detect workflow phase documents and emit doc_update events so
			// the frontend doc preview panel works for steering-driven workflows.
			if steeringDetector != nil {
				steeringDetector.interceptToolCall(tc.Function.Name, tc.Function.Arguments, func(phaseID, content string) {
					if h.getWorkflowEngine() != nil {
						if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
							_ = adapter.EmitDocUpdate(userID, phaseID, content)
							log.Printf("[SteeringWorkflow] emitted doc_update for user=%s phase=%s len=%d", userID, phaseID, len(content))
						}
					}
				})
			}

			truncated := truncateToolResultForTool(tc.Function.Name, toolContent)
			recordToolResult(tc.ID, truncated)
			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      truncated,
			})
			history = append(history, agent.ConversationEntry{
				Role: "tool", Content: truncated, ToolCallID: tc.ID,
			})

			// --- Detect consecutive JSON truncation failures for write_file ---
			// When the model generates content too long for a single JSON argument,
			// the output gets truncated and parsing fails. After 2 consecutive
			// failures, inject a system message guiding the model to chunk writes.
			if strings.Contains(truncated, "参数解析失败") && strings.Contains(truncated, "unexpected end of JSON input") {
				consecutiveJSONTruncations++
				if consecutiveJSONTruncations >= 2 {
					jsonHint := "[系统提示] 连续 " + fmt.Sprintf("%d", consecutiveJSONTruncations) + " 次 write_file 因内容过长导致 JSON 截断失败。" +
						"请将文件内容拆分为多次写入：第一次用 write_file 写入前半部分（不超过 5000 字符），后续用 write_file(mode=\"append\") 逐块追加。" +
						"或者改用 bash 工具通过 Python 脚本写入大文件。"
					conversation = append(conversation, map[string]string{
						"role":    "system",
						"content": jsonHint,
					})
					log.Printf("[agent-loop] injected JSON truncation hint after %d consecutive failures", consecutiveJSONTruncations)
				}
			} else {
				consecutiveJSONTruncations = 0
			}

			// --- Harness: DriftDetector — record tool_call and check for drift ---
			if loopDriftDetector != nil {
				argsHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Function.Arguments)))
				resultHash := fmt.Sprintf("%x", sha256.Sum256([]byte(truncated)))
				loopDriftDetector.Record(ToolCallRecord{
					ToolName:   tc.Function.Name,
					ArgsHash:   argsHash,
					Timestamp:  time.Now(),
					ResultHint: truncateRunesForDrift(truncated, 200),
					ResultHash: resultHash,
				})
				driftResult := loopDriftDetector.DetectDrift()
				if driftResult.Drifted {
					log.Printf("[Harness] 漂移检测触发: pattern=%s needHuman=%v replanCount=%d tool=%s", driftResult.Pattern, driftResult.NeedHumanHelp, loopDriftDetector.ReplanCount(), driftResult.DriftedTool)
					conversation = append(conversation, map[string]string{
						"role": "system", "content": driftResult.ReplanPrompt,
					})
					recordSystemMessages(len(conversation)-1, conversation)
					loopDriftDetector.ResetWindow()
					if driftResult.NeedHumanHelp {
						// Persist replan count and drifted tool to session level
						// so the next loop (after user responds) inherits the state.
						h.sessionDriftReplanCount.Store(userID, loopDriftDetector.ReplanCount())
						h.sessionDriftTool.Store(userID, driftResult.DriftedTool)

						resp := &IMAgentResponse{
							Text: fmt.Sprintf("⚠️ Agent 在执行过程中反复调用 %s 未能成功，已停止尝试。请检查任务要求或提供新的指示。", driftResult.DriftedTool),
						}
						h.saveConversationHistoryTimed(userID, history, resp)
						return resp
					}
					enterRecoverPhase(&phase, "drift_detected", buildDriftRecoverPrompt(driftResult))
				}
			}
		}
		skillToolAttempted := shouldBypassSkillPreference(choice.Message.ToolCalls)
		if skillToolAttempted {
			phase.SkillAttempted = true
			if runID := extractSkillRunID(choice.Message.ToolCalls, toolResults); strings.TrimSpace(runID) != "" {
				phase.PreferredSkillRunID = strings.TrimSpace(runID)
			}
			if phase.SkillMode == skillPreferenceRemoteRequired {
				phase.RemoteSearchAttempted = true
			}
			hasSearchTool := false
			for _, tc := range choice.Message.ToolCalls {
				if isSkillSearchToolName(tc.Function.Name) {
					hasSearchTool = true
					break
				}
			}
			if didSkillToolFail(choice.Message.ToolCalls, toolResults) {
				phase.SkillFailed = true
				phase.RemoteSearchExhausted = true
				phase.ForceSkillPreference = false
				phase.SkillMode = skillPreferenceFallbackAllowed

				// Workaround detection: record the failed skill name and error
				// so that if the LLM resolves the task through alternative
				// tool calls later in this loop, we classify as "workaround".
				if sn, se := extractFailedSkillInfo(choice.Message.ToolCalls, toolResults); sn != "" {
					phase.FailedSkillName = sn
					phase.FailedSkillError = se
					log.Printf("[skill-workaround] skill %q failed, marking as pending workaround: %s", sn, truncateRunes(se, 120))
					// Note: failure stats (UsageCount, FailureCount) are already
					// recorded by SkillRunner.updateUsageStats() inside executeAsync().
					// We do NOT call RecordSkillOutcome("failure") here to avoid
					// double-counting.
				}

				enterRecoverPhase(&phase, "skill_failed", buildSkillRecoverPrompt(phase.PreferredSkillName, phase.PreferredSkillRunID))
			} else if shouldRestrictToSkillSearch(phase) && hasSearchTool {
				phase.ForceSkillPreference = false
				phase.SkillMode = skillPreferenceFallbackAllowed
			}
		}
		if trialState.enabled {
			outcome, observation, repeatedFailures := trialState.observeIteration(choice.Message.ToolCalls, toolResults)

			// Record tool outcomes to UsageTracker for routing feedback.
			if h.usageTracker != nil {
				// Tokenize user message once for all tool outcome records.
				var msgTokens []string
				if userText != "" {
					msgTokens = bm25.Tokenize(userText)
					if len(msgTokens) > 5 {
						msgTokens = msgTokens[:5]
					}
				}
				for i, tc := range choice.Message.ToolCalls {
					name := strings.TrimSpace(tc.Function.Name)
					toolOutcome := classifyToolOutcome(name, toolResults[i])
					success := toolOutcome == "succeeded"
					followUp := "continue"
					if toolOutcome == "failed" {
						// Check if this tool was retried (same name appears again in this batch)
						for j := i + 1; j < len(choice.Message.ToolCalls); j++ {
							if strings.TrimSpace(choice.Message.ToolCalls[j].Function.Name) == name {
								followUp = "retry"
								break
							}
						}
						if followUp == "continue" && outcome == "failed" {
							followUp = "abandon"
						}
					}
					h.usageTracker.RecordOutcome(name, msgTokens, success, followUp)
				}
			}

			if outcome == "failed" && phase.Stage != agentStageRecover {
				enterRecoverPhase(&phase, "trial_failed", buildTrialFailureRecoverPrompt(observation, repeatedFailures))
			}
			if phase.Stage != agentStageRecover {
				phase.Stage = agentStageConverge
			}
			if observation != "" && h.traceService != nil && ctx.RunID != "" {
				h.appendTraceEvent(ctx, "trial.observed", "info", "Trial outcome", truncateTraceText(observation, 220), "", "")
				h.appendTraceEvidence(ctx, "trial_reflect", outcome, "trial observation", truncateTraceText(observation, 400), "", "")
			}
			if strings.TrimSpace(trialState.pendingNote) != "" && h.traceService != nil && ctx.RunID != "" {
				severity := "info"
				if outcome == "failed" {
					severity = "warn"
				}
				h.appendTraceEvent(ctx, "trial.reflected", severity, "Trial reflection", truncateTraceText(trialState.pendingNote, 220), "", "")
				if len(repeatedFailures) > 0 {
					h.appendTraceEvidence(ctx, "trial_reflect", "repeat_guard", "avoid repeating failed actions", strings.Join(repeatedFailures, ", "), "", "")
				}
			}
		}

		// --- Coding iteration budget enforcement ---
		// Track consecutive iterations where the LLM is primarily calling
		// coding tools (write_file, edit_file, bash). When the budget is
		// exceeded, force-return to prevent context blowup.
		{
			codingToolsThisIter := 0
			for _, tc := range choice.Message.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
				switch name {
				case "write_file", "edit_file", "bash":
					codingToolsThisIter++
				}
			}
			if len(choice.Message.ToolCalls) > 0 && codingToolsThisIter*100/len(choice.Message.ToolCalls) >= 80 {
				codingIterCount++
			} else if len(choice.Message.ToolCalls) == 0 {
				// No tool calls — don't reset, the LLM might be thinking
				// between coding bursts.
			} else {
				codingIterCount = 0 // non-coding tools, reset
			}

			if codingIterCount >= codingIterBudgetHard {
				log.Printf("[coding-budget] hard limit reached: %d consecutive coding iterations, force-returning (iter=%d)", codingIterCount, iteration)
				phase.Stage = agentStageFinalize
				summaryText := fmt.Sprintf("⏸️ 编码执行已达到 %d 轮迭代上限。已完成的工作已保存。\n\n如需继续，请发送「继续」。", codingIterCount)
				finalResp := &IMAgentResponse{Text: summaryText}
				attachLLMTelemetry(finalResp)
				h.saveConversationHistoryTimed(userID, history, finalResp)
				return finalResp
			}
			if codingIterCount == codingIterBudgetSoft {
				log.Printf("[coding-budget] soft limit reached: %d consecutive coding iterations, injecting progress reminder (iter=%d)", codingIterCount, iteration)
				systemMessagesStart := len(conversation)
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": fmt.Sprintf("[系统提示] 你已连续执行了 %d 轮编码操作。请在完成当前文件后暂停，向用户汇报进度（已完成哪些文件、还剩哪些），然后等待用户确认是否继续。", codingIterCount),
				})
				recordSystemMessages(systemMessagesStart, conversation)
			}
		}

		// If a direct screenshot was captured, return it immediately as an image response.
		// However, if the screenshot was part of a multi-tool call (LLM called screenshot
		// alongside other tools) or the agent has been actively working (prior tool calls
		// in earlier iterations), the screenshot is an intermediate step — let the loop
		// continue so the LLM can proceed with the remaining task. The screenshot result
		// is already in the conversation as a tool_result ("截图已成功捕获...").
		//
		// We use totalToolCallsInLoop <= 1 instead of iteration == 0 because the LLM
		// might output a preamble ("好的，我来截屏") in iteration 0 and call screenshot
		// in iteration 1. As long as screenshot is the ONLY tool call so far, it's a
		// pure screenshot request and should return immediately.
		screenshotIsOnlyAction := len(choice.Message.ToolCalls) <= 1 && totalToolCallsInLoop <= 1
		if pendingImageKey != "" && screenshotIsOnlyAction {
			resp := &IMAgentResponse{}
			if !streamDoneAt.IsZero() {
				postStreamLastReturnPrepAt = time.Now()
			}
			attachLLMTelemetry(resp)
			h.saveConversationHistoryTimed(userID, history, resp)
			// Desktop platform: save to local file and return path + thumbnail
			if platform == "desktop" {
				filePath, err := h.saveScreenshotToFile(pendingImageKey)
				if err != nil {
					return &IMAgentResponse{Text: fmt.Sprintf("📷 截图已捕获，但保存文件失败: %s", err.Error())}
				}
				// Generate a small thumbnail (reuse the base64 data, frontend will size it)
				thumb := pendingImageKey
				// Cap thumbnail data to keep the JSON response lean
				if len(thumb) > 50000 {
					if downsized, err := remote.DownsizeScreenshotBase64(thumb, 10000); err == nil {
						thumb = downsized
					}
				}
				resp.Text = "📷 截图已保存"
				resp.LocalFilePath = filePath
				resp.ThumbnailBase64 = thumb
				return resp
			}
			resp.Text = ""
			resp.ImageKey = pendingImageKey
			return resp
		} else if pendingImageKey != "" {
			// Screenshot is an intermediate step — save the file but don't
			// stop the loop. The LLM will continue with the next step.
			if platform == "desktop" {
				if filePath, err := h.saveScreenshotToFile(pendingImageKey); err == nil {
					log.Printf("[screenshot] intermediate screenshot saved to %s, loop continues (iteration=%d toolCalls=%d)", filePath, iteration, len(choice.Message.ToolCalls))
				}
			} else {
				// Non-desktop (IM): save to file so the path is available in
				// the tool result text. The LLM already received "截图已成功捕获"
				// as tool_result and can reference the screenshot in its response.
				if filePath, err := h.saveScreenshotToFile(pendingImageKey); err == nil {
					log.Printf("[screenshot] intermediate screenshot (IM) saved to %s, loop continues (iteration=%d toolCalls=%d)", filePath, iteration, len(choice.Message.ToolCalls))
				}
			}
		}

		// If screenshot was already delivered via session.image channel,
		// stop the loop immediately — UNLESS the screenshot is an intermediate
		// step in a larger task. When the LLM called multiple tools in this
		// iteration or has been actively working (iteration > 0), the screenshot
		// is just a "check the screen" step and the LLM should continue.
		if screenshotAlreadySent && screenshotIsOnlyAction {
			resp := &IMAgentResponse{Text: "📷 截图已发送"}
			attachLLMTelemetry(resp)
			h.saveConversationHistoryTimed(userID, history, resp)
			return resp
		} else if screenshotAlreadySent {
			log.Printf("[screenshot] intermediate screenshot via session.image, loop continues (iteration=%d totalToolCalls=%d)", iteration, totalToolCallsInLoop)
		}

		// If file(s) were prepared, return them for delivery.
		if len(pendingFiles) > 0 {
			resp := &IMAgentResponse{}
			if !streamDoneAt.IsZero() {
				postStreamLastReturnPrepAt = time.Now()
			}
			attachLLMTelemetry(resp)
			h.saveConversationHistoryTimed(userID, history, resp)
			if platform == "desktop" {
				fileMaterializeStartedAt := time.Now()
				var savedPaths []string
				var failLines []string
				var imForwardedCount int
				for _, pf := range pendingFiles {
					filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
					if err != nil {
						failLines = append(failLines, fmt.Sprintf("📄 %s 保存失败: %s", pf.name, err.Error()))
						continue
					}
					savedPaths = append(savedPaths, filePath)

					// Forward to IM channels if requested and sender is configured.
					if pf.forwardIM {
						if h.imFileSender == nil {
							failLines = append(failLines, fmt.Sprintf("📄 %s 已保存到本地，但未连接到 Hub，无法转发到 IM", pf.name))
						} else if err := h.imFileSender(pf.data, pf.name, pf.mimeType, pf.message); err != nil {
							log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, err)
							failLines = append(failLines, fmt.Sprintf("📄 %s 已保存到本地，但发送到 IM 失败: %s", pf.name, err.Error()))
						} else {
							imForwardedCount++
						}
					}
				}
				// Text only contains failure messages (if any); paths are in LocalFilePaths
				// so the frontend can render clickable links without duplication.
				text := strings.Join(failLines, "\n")
				if imForwardedCount > 0 {
					imNote := fmt.Sprintf("📨 已将 %d 个文件发送到 IM 通道", imForwardedCount)
					if text != "" {
						text = imNote + "\n" + text
					} else {
						text = imNote
					}
				}
				resp.Text = text
				resp.LocalFilePaths = savedPaths
				resp.FileMaterializeNanos = time.Since(fileMaterializeStartedAt).Nanoseconds()
				// Keep backward compat: also set singular field to first path
				if len(savedPaths) > 0 {
					resp.LocalFilePath = savedPaths[0]
				}
				return resp
			}
			// IM platforms: send the last file (IM channels support one attachment per message)
			last := pendingFiles[len(pendingFiles)-1]
			resp.Text = ""
			resp.FileData = last.data
			resp.FileName = last.name
			resp.FileMimeType = last.mimeType
			return resp
		}

		// --- NeedsConfirm gate (tool branch) ---
		// When the current workflow phase requires user confirmation and the
		// LLM has already produced substantive text (the requirements/design
		// doc), force-return after executing delivery tools (open, memory,
		// task, etc.) instead of continuing to the next iteration.
		//
		// IMPORTANT: Same phase-awareness as the no-tool branch — when the
		// WorkflowEngine has an active workflow, defer to IsPhaseNeedsConfirm()
		// instead of blindly using gateConfig.active. This prevents premature
		// exit during the implementation phase where NeedsConfirm=false.
		needsConfirmToolBranch := false
		if gateConfig.active && iteration > 0 {
			if h.getWorkflowEngine() != nil && h.getWorkflowEngine().GetActiveWorkflow(userID) != nil {
				// Engine owns the workflow — delegate to phase-aware check.
				needsConfirmToolBranch = h.getWorkflowEngine().IsPhaseNeedsConfirm(userID)
				if !needsConfirmToolBranch {
					log.Printf("[workflow-gate] NeedsConfirm tool-branch bypassed: engine workflow active, phase NeedsConfirm=false (iter=%d user=%s)", iteration, userID)
				}
			} else {
				// No engine workflow — pure steering-driven flow.
				needsConfirmToolBranch = true
			}
		}
		// Fallback for non-coding workflows: when gateConfig is not active
		// (no coding intent) but the engine has a NeedsConfirm phase, still
		// activate the gate. IsPhaseNeedsConfirm internally checks ws != nil
		// && ws.Status == WorkflowActive, so if it returns true here,
		// GetActiveWorkflow below will also return non-nil.
		//
		// EXCEPTION: When the semantic intent classifier has determined the
		// user's message is maintenance/bug_fix/continuation (not a workflow
		// phase document), skip the engine gate. This prevents "改进优化下这个
		// 技能？" from being force-returned as a phase document when a
		// presentation_design workflow happens to be active.
		if !needsConfirmToolBranch && iteration > 0 && h.getWorkflowEngine() != nil {
			semanticBypass := false
			switch gateConfig.intent {
			case intentCoding:
				// maintenance or bug_fix — gateConfig.active is false but intent is coding.
				// The user is doing maintenance work, not generating a workflow phase doc.
				if !gateConfig.active {
					semanticBypass = true
				}
			}
			if gateConfig.bugFix {
				semanticBypass = true
			}
			if !semanticBypass {
				needsConfirmToolBranch = h.getWorkflowEngine().IsPhaseNeedsConfirm(userID)
			} else {
				log.Printf("[workflow-gate] NeedsConfirm tool-branch fallback bypassed: semantic intent=%v active=%v bugFix=%v reason=%q",
					gateConfig.intent, gateConfig.active, gateConfig.bugFix, gateConfig.reason)
			}
		}
		// Engine-State-Aware Gate: when the engine path sets needsConfirmToolBranch=true,
		// additionally check HasPhaseOutput. On first execution (hasOutput=false), skip
		// the engine gate so the agent loop continues generating the full phase document.
		// The steering-driven path (gateConfig.active without engine workflow) is unchanged.
		if needsConfirmToolBranch && h.getWorkflowEngine() != nil &&
			h.getWorkflowEngine().GetActiveWorkflow(userID) != nil {
			if !h.getWorkflowEngine().HasPhaseOutput(userID) {
				needsConfirmToolBranch = false
				log.Printf("[agent-loop] NeedsConfirm tool branch: first execution (hasOutput=false), skipping engine gate (iter=%d)", iteration)
			}
		}
		// When handlePendingConfirm classified the message as "other"
		// (unrelated to the workflow), skip the NeedsConfirm gate so
		// the unrelated tool output is not captured as a phase document.
		//
		// EXCEPTION: When gateConfig.active is true, the current message
		// is a NEW coding task that needs the three-phase flow. Do NOT
		// bypass — let the Coding Tool Gate enforce the three-phase flow.
		if needsConfirmToolBranch && ctx.SkipNeedsConfirmGate && !gateConfig.active {
			needsConfirmToolBranch = false
			log.Printf("[workflow-gate] NeedsConfirm tool-branch bypassed: pending confirm classified as 'other' (iter=%d user=%s)", iteration, userID)
		}
		if needsConfirmToolBranch {
			trimmedAfterTools := strings.TrimSpace(stripThinkingTags(msgContent))

			// Self-confirmation detection (tool branch): same logic as no-tool branch.
			// Detect when the LLM both requests confirmation AND self-answers it in the
			// same response, then truncate at the confirmation request boundary.
			if containsSelfConfirmationPattern(trimmedAfterTools) {
				originalLen := len(trimmedAfterTools)
				trimmedAfterTools = truncateAtConfirmationBoundary(trimmedAfterTools)
				msgContent = trimmedAfterTools
				log.Printf("NeedsConfirm gate (tool branch): detected self-confirmation pattern, truncated at confirmation boundary (originalLen=%d truncatedLen=%d)", originalLen, len(trimmedAfterTools))
				if h.traceService != nil && ctx.RunID != "" {
					h.appendTraceEvent(ctx, "gate.self_confirm_truncated", "warn",
						fmt.Sprintf("Self-confirmation detected and truncated in tool branch (originalLen=%d truncatedLen=%d)", originalLen, len(trimmedAfterTools)),
						truncateTraceText(trimmedAfterTools, 220), "", "")
				}
			}

			if trimmedAfterTools != "" && !looksLikeNoToolStallReply(msgContent) && isSubstantivePhaseDocument(trimmedAfterTools) {
				log.Printf("[workflow-gate] NeedsConfirm (tool branch): force-returning after tool execution for user confirmation (iteration=%d len=%d)", iteration, len(trimmedAfterTools))
				phase.Stage = agentStageFinalize
				toolBranchText := msgContent
				if lengthContinuationBuf.Len() > 0 {
					toolBranchText = lengthContinuationBuf.String() + msgContent
				}
				finalResp := &IMAgentResponse{Text: stripThinkingTags(toolBranchText)}
				// Desktop: intercept text output for the doc preview panel.
				if platform == "desktop" && h.getWorkflowEngine() != nil {
					if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
						emitted := false
						if steeringDetector != nil {
							steeringDetector.interceptTextOutput(trimmedAfterTools, func(phaseID, content string) {
								_ = adapter.EmitDocUpdate(userID, phaseID, content)
								log.Printf("[SteeringWorkflow] emitted doc_update from tool-branch gate for user=%s phase=%s len=%d", userID, phaseID, len(content))
								emitted = true
							})
						}
						if !emitted && len(trimmedAfterTools) >= 50 {
							if ws := h.getWorkflowEngine().GetActiveWorkflow(userID); ws != nil {
								_ = adapter.EmitDocUpdate(userID, ws.CurrentPhase, trimmedAfterTools)
								adapter.EmitSuggestMaximize(userID, string(ws.Type))
								log.Printf("[WorkflowEngine] emitted doc_update from tool-branch for user=%s phase=%s type=%s len=%d", userID, ws.CurrentPhase, ws.Type, len(trimmedAfterTools))
							} else {
								log.Printf("[WorkflowEngine] WARNING: NeedsConfirm tool-branch but GetActiveWorkflow returned nil for user=%s", userID)
							}
						}
					}
				}
				attachLLMTelemetry(finalResp)
				h.saveConversationHistoryTimed(userID, history, finalResp)
				return finalResp
			} else if trimmedAfterTools != "" && !looksLikeNoToolStallReply(msgContent) {
				// isSubstantivePhaseDocument was false — short preamble after tool execution, let the loop continue.
				log.Printf("[workflow-gate] NeedsConfirm (tool branch): skipping non-substantive preamble (len=%d), allowing loop to continue", len([]rune(trimmedAfterTools)))
			}
		}
	}

	// When the loop exits due to cancellation, save history and return a
	// clean message. The agent loop must never call memory.Clear — history
	// lifecycle is managed by the caller, not the loop itself.
	if ctx.IsCancelled() {
		return h.cancelledExitResponse(userID, history, userText)
	}

	// When rounds are exhausted but coding sessions are still active,
	// auto-continue one extra round so the agent can check session status,
	// then ask the user whether to keep watching.
	if h.manager != nil && h.manager.HasActiveSessions() {
		sendProgress("⏳ 推理轮次已用完，但编程会话仍在运行，正在检查状态…")

		// Run one bonus iteration to let the agent observe current session state.
		conversation = autoCompressConversation(conversation, cfg, httpClient)
		conversation = trimConversation(conversation, effectiveTokenLimit, toolsTokenBudget, makeSummarizer(cfg, httpClient))
		if onNewRound != nil {
			onNewRound()
		}
		bonusMetrics := &llmStreamMetrics{}
		bonusResp, err := h.doLLMRequestStream(loopCtx, cfg, conversation, tools, httpClient, onToken, bonusMetrics)
		firstLLMRequestBuildElapsed += time.Duration(bonusMetrics.RequestBuildNanos)
		firstLLMHTTPDoElapsed += time.Duration(bonusMetrics.HTTPDoNanos)
		firstLLMFirstSSEWaitElapsed += time.Duration(bonusMetrics.FirstSSEWaitNanos)
		firstLLMStreamMaxTokenGapElapsed += time.Duration(bonusMetrics.MaxTokenGapNanos)
		firstLLMIdleTimeoutCount += bonusMetrics.IdleTimeoutCount
		firstLLMIdleTimeoutAfterToken = firstLLMIdleTimeoutAfterToken || bonusMetrics.IdleTimeoutAfterToken
		if err == nil && streamDoneCallback != nil {
			streamDoneCallback()
		}
		// Accumulate token usage stats for bonus round
		if bonusResp != nil {
			usageStartedAt := time.Now()
			input, output := deriveLLMTokenUsage(bonusResp, conversation)
			providerName := h.getMaclawLLMProviders().Current
			log.Printf("[LLM] usage bonus_round provider=%q input=%d output=%d usage_nil=%t choices=%d", providerName, input, output, bonusResp.Usage == nil, len(bonusResp.Choices))
			h.accumulateLLMTokenUsage(providerName, input, output)
			lastLLMInputTokens = input
			lastLLMOutputTokens = output
			if !streamDoneAt.IsZero() {
				handlerPostStreamUsageElapsed += time.Since(usageStartedAt)
				postStreamUsageDoneAt = time.Now()
			}
		}
		if err == nil && len(bonusResp.Choices) > 0 {
			bc := bonusResp.Choices[0]
			bcContent := bc.Message.Content
			bcReasoning := bc.Message.ReasoningContent
			if bcContent == "" && bcReasoning != "" {
				bcContent = bcReasoning
			}
			assistantMsg := map[string]interface{}{
				"role":    "assistant",
				"content": bcContent,
			}
			if bcReasoning != "" {
				assistantMsg["reasoning_content"] = bcReasoning
			} else if len(bc.Message.ToolCalls) > 0 {
				assistantMsg["reasoning_content"] = ""
			}
			if len(bc.Message.ToolCalls) > 0 {
				assistantMsg["tool_calls"] = bc.Message.ToolCalls
			}
			conversation = append(conversation, assistantMsg)
			if recorder != nil {
				recorder.Record("assistant", bcContent, bc.Message.ToolCalls, "", bcReasoning)
			}
			history = append(history, agent.ConversationEntry{
				Role: "assistant", Content: bcContent, ReasoningContent: bcReasoning, ToolCalls: bc.Message.ToolCalls,
			})

			// Execute any tool calls from the bonus round.
			for _, tc := range bc.Message.ToolCalls {
				milestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, false)
				if isDebug() {
					sendToolProgress(userFacingToolProgressText(tc.Function.Name))
				}
				toolOnProgress := onProgress
				if !isDebug() {
					toolOnProgress = nil
					if onProgress != nil && shouldExposeToolInternalProgress(tc.Function.Name) {
						toolName := tc.Function.Name
						toolOnProgress = func(msg string) {
							if filtered := filterUserFacingToolProgress(toolName, msg); filtered != "" {
								onProgress(filtered)
							}
						}
					}
				}
				toolExecStartedAt := time.Now()
				recordToolCall(tc.ID, tc.Function.Name, tc.Function.Arguments)
				toolResult := h.executeTool(tc.Function.Name, tc.Function.Arguments, toolOnProgress)

				// Record completed milestone.
				milestoneTracker.RecordToolCall(tc.Function.Name, tc.Function.Arguments, true)

				// Handle ask_user in background loop: skip (background tasks shouldn't ask questions).
				if IsAskUserResult(toolResult) {
					toolResult = "ask_user 在后台任务中不可用，请直接做出决定。"
				}

				// Handle delegate_task: inject sub-agent context.
				if IsSubAgentContext(toolResult) {
					toolResult = ExtractSubAgentContext(toolResult)
				}

				// Pin conditional tools to session (same as main loop).
				if h.toolRouter != nil && tool.ShouldPinConditionalTool(tc.Function.Name) &&
					!strings.HasPrefix(toolResult, "未知工具") && !strings.HasPrefix(toolResult, "工具执行异常") {
					h.toolRouter.ActivateSessionTool(tc.Function.Name)
					log.Printf("[ToolPin] session-pinned conditional tool %q", tc.Function.Name)
				}

				if !streamDoneAt.IsZero() {
					handlerPostStreamToolExecElapsed += time.Since(toolExecStartedAt)
				}
				truncated := truncateToolResultForTool(tc.Function.Name, toolResult)
				recordToolResult(tc.ID, truncated)
				conversation = append(conversation, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tc.ID,
					"content":      truncated,
				})
				history = append(history, agent.ConversationEntry{
					Role: "tool", Content: truncated, ToolCallID: tc.ID,
				})
			}
		}

		h.saveConversationHistoryTimed(userID, history, &IMAgentResponse{})
		resp := &IMAgentResponse{Text: "🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。", Deferred: true}
		attachLLMTelemetry(resp)
		return resp
	}

	// --- Create unfinished task slot so "继续" can resume with context ---
	finalIteration := ctx.Iteration()
	log.Printf("[AgentLoop] ⚠️ MAX ROUNDS EXHAUSTED loop=%s iteration=%d effectiveMax=%d configMax=%d loopOverride=%d grace=%d kind=%d user=%q task=%q elapsed=%s",
		ctx.ID, finalIteration, effectiveMax, maxIter, h.loopMaxOverride, chatFinalizeGrace, ctx.Kind, userID, truncateRunes(userText, 80), time.Since(conversationStartedAt))
	originalTask := extractOriginalUserTask(history)
	progressSummary := extractProgressSummary(history)
	if originalTask != "" {
		slotID := fmt.Sprintf("maxround-%d", time.Now().UnixMilli())
		h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
			SlotID:       slotID,
			UserID:       userID,
			Status:       "max_rounds_reached",
			LastTask:     originalTask,
			Summary:      progressSummary,
			ResumePrompt: "用户发送「继续」以恢复此任务。请基于对话历史中已完成的工作，继续完成剩余部分。不要重复已完成的步骤。\n",
			Source:       "max_rounds",
		})
		h.memory.BindUnfinishedSlot(userID, slotID)
		log.Printf("[MaxRounds] created unfinished slot %s for user %s, task=%q", slotID, userID, truncateRunes(originalTask, 80))
	}

	resp := &IMAgentResponse{Text: "(已达到最大推理轮次，请继续发送消息以完成任务)"}
	attachLLMTelemetry(resp)
	// Nudge injection at max-rounds exit.
	history = h.injectNudgeMessages(history, finalIteration, totalToolCallsInLoop, phase, userText)
	h.saveConversationHistoryTimed(userID, history, resp)
	return resp
}

// saveScreenshotToFile saves base64-encoded PNG data to a local file under
// ~/.maclaw/data/screenshots/ and returns the absolute file path.
func (h *IMMessageHandler) saveScreenshotToFile(base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".maclaw", "data", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create screenshots directory: %w", err)
	}
	fileName := fmt.Sprintf("screenshot_%s_%d.png", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	filePath := filepath.Join(dir, fileName)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// saveFileDataToLocal saves base64-encoded file data to ~/.maclaw/data/files/
// and returns the absolute file path.
func (h *IMMessageHandler) saveFileDataToLocal(name, base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".maclaw", "data", "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create files directory: %w", err)
	}
	if name == "" {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	// Sanitize: use only the base name to prevent path traversal (e.g. "../../etc/passwd")
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	filePath := filepath.Join(dir, name)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// ---------------------------------------------------------------------------
// Attachment → LLM Content Builder
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// SteeringWorkflowDetector — lightweight detector for steering-driven coding
// workflows. When the workflow engine has no active workflow for the user but
// the LLM is executing a coding task via steering rules (coding-workflow.md),
// this detector identifies phase documents from tool calls and emits the same
// frontend events (workflow:suggest_maximize, workflow:doc_update) that the
// workflow engine path would emit.
// ---------------------------------------------------------------------------

// SteeringWorkflowDetector tracks steering-driven coding workflow state
// within a single agent loop invocation.
type SteeringWorkflowDetector struct {
	detected               bool              // whether a coding workflow has been detected
	suggestMaximizeEmitted bool              // whether suggest_maximize event has been emitted
	phaseDocuments         map[string]string // detected phase documents (phaseID → content)
	userID                 string            // current user ID
}

// NewSteeringWorkflowDetector creates a new detector for the given user.
func NewSteeringWorkflowDetector(userID string) *SteeringWorkflowDetector {
	return &SteeringWorkflowDetector{
		detected:       true,
		phaseDocuments: make(map[string]string),
		userID:         userID,
	}
}

// isCodingTask checks whether the message text matches coding task keywords
// from the steering workflow rules. This uses a focused keyword list aligned
// with the coding-workflow.md steering file.
func (d *SteeringWorkflowDetector) isCodingTask(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	// Keywords aligned with coding-workflow.md steering rules.
	keywords := []string{
		"开发", "编写", "实现", "创建", "修改代码", "重构",
		"修 bug", "设计架构", "添加功能", "新增功能",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// matchPhaseID extracts a workflow phase ID from a file name by matching
// known patterns for requirements, design, and tasks documents.
// Returns empty string if the file name does not match any workflow phase.
func (d *SteeringWorkflowDetector) matchPhaseID(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" {
		return ""
	}
	// Check tasks BEFORE design — task documents often reference "技术设计"
	// in their body, which would cause a false match on the design case.
	switch {
	case strings.Contains(lower, "任务拆分") ||
		strings.Contains(lower, "任务列表") ||
		strings.Contains(lower, "任务分解") ||
		strings.Contains(lower, "开发任务") ||
		strings.Contains(lower, "tasks") ||
		strings.Contains(lower, "task_breakdown") ||
		strings.Contains(lower, "task breakdown"):
		return "tasks"
	case strings.Contains(lower, "需求文档") ||
		strings.Contains(lower, "需求分析") ||
		strings.Contains(lower, "功能需求") ||
		strings.Contains(lower, "项目需求") ||
		strings.Contains(lower, "需求背景") ||
		strings.Contains(lower, "需求概述") ||
		strings.Contains(lower, "requirements"):
		return "requirements"
	case strings.Contains(lower, "技术设计") ||
		strings.Contains(lower, "设计文档") ||
		strings.Contains(lower, "架构设计") ||
		strings.Contains(lower, "接口设计") ||
		strings.Contains(lower, "技术方案") ||
		strings.Contains(lower, "design"):
		return "design"
	default:
		return ""
	}
}

// interceptToolCall checks a tool call for workflow phase documents and
// invokes the emit callback with (phaseID, content) when a match is found.
// Supports write_file (path + content args) and generate_pdf (markdown_content arg).
func (d *SteeringWorkflowDetector) interceptToolCall(toolName, argsJSON string, emit func(phaseID, content string)) {
	if !d.detected || emit == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	switch toolName {
	case "write_file":
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		phaseID := d.matchPhaseID(args.Path)
		if phaseID == "" || args.Content == "" {
			return
		}
		d.phaseDocuments[phaseID] = args.Content
		emit(phaseID, args.Content)

	case "generate_pdf":
		var args struct {
			MarkdownContent string `json:"markdown_content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		if args.MarkdownContent == "" {
			return
		}
		// Infer phase from the markdown content itself since generate_pdf
		// doesn't have a file path argument.
		phaseID := d.matchPhaseID(args.MarkdownContent)
		if phaseID == "" {
			return
		}
		d.phaseDocuments[phaseID] = args.MarkdownContent
		emit(phaseID, args.MarkdownContent)

	case "office":
		var args struct {
			Action          string `json:"action"`
			MarkdownContent string `json:"markdown_content"`
			Content         string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		if args.Action != "generate_pdf" {
			return
		}
		content := args.MarkdownContent
		if content == "" {
			content = args.Content
		}
		if content == "" {
			return
		}
		phaseID := d.matchPhaseID(content)
		if phaseID == "" {
			return
		}
		d.phaseDocuments[phaseID] = content
		emit(phaseID, content)
	}
}

// extractFencedDocument extracts the document content between `---`
// delimiters in the text. LLM typically outputs:
//
//	Here's the requirements document:
//	---
//	# Requirements
//	...
//	---
//	Please review and confirm.
//
// Only the content between the delimiters should be shown in the preview.
// Returns the extracted content, or the original text if no delimiters found.
//
// To avoid false positives with Markdown heading underlines (e.g.
// "# Title\n---"), the first `---` must appear within the first 5 lines
// of the text and must NOT be immediately preceded by a heading line.
func extractFencedDocument(text string) string {
	lines := strings.Split(text, "\n")
	firstIdx := -1
	secondIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 || strings.Trim(trimmed, "-") != "" {
			continue
		}
		// This line is a `---` separator.
		if firstIdx < 0 {
			// Only accept the opening fence within the first 5 lines.
			if i > 4 {
				break
			}
			// Skip if preceded by a Markdown heading (e.g. "# Title\n---").
			if i > 0 {
				prev := strings.TrimSpace(lines[i-1])
				if strings.HasPrefix(prev, "#") {
					continue
				}
			}
			firstIdx = i
		} else {
			secondIdx = i
			break
		}
	}
	if firstIdx >= 0 && secondIdx > firstIdx+1 {
		inner := strings.Join(lines[firstIdx+1:secondIdx], "\n")
		inner = strings.TrimSpace(inner)
		if len(inner) > 100 {
			return inner
		}
	}
	// No valid --- fences found. Strip any conversational preamble before
	// the first Markdown heading so the preview only shows the document.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			stripped := strings.Join(lines[i:], "\n")
			stripped = strings.TrimSpace(stripped)
			if len(stripped) > 100 {
				return stripped
			}
			break
		}
	}
	return text
}

// interceptTextOutput checks the LLM's plain text output for workflow phase
// documents. This is used in desktop mode where the LLM outputs Markdown
// directly instead of calling generate_pdf. The heading area (first ~500
// chars) is matched against phase keywords to determine the document type.
// Content between `---` delimiters is extracted so the preview panel only
// shows the document body, not the surrounding conversational text.
func (d *SteeringWorkflowDetector) interceptTextOutput(text string, emit func(phaseID, content string)) {
	if !d.detected || emit == nil || strings.TrimSpace(text) == "" {
		return
	}
	// Only match substantive text (likely a document, not a short reply).
	if len(text) < 200 {
		return
	}
	// Match against the heading area to avoid false positives from keywords
	// appearing in the body (e.g., "design" mentioned in a requirements doc).
	headingArea := text
	if len(headingArea) > 500 {
		headingArea = headingArea[:500]
	}
	phaseID := d.matchPhaseID(headingArea)
	if phaseID == "" {
		// Fallback: if no phase keyword matched but this is the first
		// document in the workflow, assume it's the requirements doc
		// (the three-phase flow always starts with requirements).
		if len(d.phaseDocuments) == 0 {
			phaseID = "requirements"
		} else {
			return
		}
	}
	// Extract only the fenced document content (between --- delimiters).
	docContent := extractFencedDocument(text)
	// Avoid re-emitting the same phase document.
	if existing, ok := d.phaseDocuments[phaseID]; ok && existing == docContent {
		return
	}
	d.phaseDocuments[phaseID] = docContent
	emit(phaseID, docContent)
}
