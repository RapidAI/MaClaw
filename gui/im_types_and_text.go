package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

// imHeartbeatMsg is the sentinel value sent as a progress update to keep the
// Hub-side response timer alive. It must never be delivered to the end user.
const imHeartbeatMsg = "__heartbeat__"

// ---------------------------------------------------------------------------
// IMMessageHandler 鈥?handles IM messages forwarded from Hub via WebSocket
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
	ClearUI                             bool                          `json:"clear_ui,omitempty"`
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
	VoiceData                           string                        `json:"voice_data,omitempty"`      // Base64-encoded voice audio (OGG Opus or WAV)
	VoiceFileName                       string                        `json:"voice_file_name,omitempty"` // e.g. "voice.ogg"
	VoiceMimeType                       string                        `json:"voice_mime_type,omitempty"` // e.g. "audio/ogg"
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

	// Corrections provides one-click override options for the user when the
	// scheduler's automatic interrupt decision may not match their intent.
	// Populated only for interrupt responses (Merge/Queue). The Hub frontend
	// renders these as clickable buttons; IM gateways format them as text.
	Corrections []progress.CorrectionOption `json:"corrections,omitempty"`
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

const minHeuristicTextContinuationRunes = 1200

func shouldContinueTextOutput(finishReason, content string) (bool, string) {
	reason := normalizeLLMFinishReason(finishReason)
	if reason == llmFinishReasonLength {
		return true, "finish_reason=length"
	}
	if reason == llmFinishReasonUnknown && strings.TrimSpace(finishReason) != "" {
		return false, ""
	}
	if reason != llmFinishReasonUnknown && reason != llmFinishReasonStop {
		return false, ""
	}
	if !looksStructurallyTruncatedText(content) {
		return false, ""
	}
	return true, "structural_heuristic"
}

func looksStructurallyTruncatedText(content string) bool {
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed == "" {
		return false
	}
	runes := []rune(trimmed)
	if len(runes) < minHeuristicTextContinuationRunes {
		return false
	}
	if strings.Count(trimmed, "```")%2 == 1 {
		return true
	}
	switch runes[len(runes)-1] {
	case ',', '\uFF0C', ':', '\uFF1A', ';', '\uFF1B', '\u3001', '-', '\u2014', '(', '\uFF08', '[', '\u3010', '{', '\u300A':
		return true
	default:
		return false
	}
}

func isPureScreenshotAction(totalNonScreenshotToolCalls int) bool {
	return totalNonScreenshotToolCalls == 0
}

// sessionStartLLMCaller adapts the GUI's LLM calling to memory.LLMChatCaller
// for the SessionStartExtractor. Same pattern as archiverLLMCaller.
type sessionStartLLMCaller struct {
	app *App
}

func (c *sessionStartLLMCaller) ChatCall(messages []map[string]string) (string, error) {
	cfg := c.app.GetMaclawLLMConfig()
	iface := make([]interface{}, len(messages))
	for i, m := range messages {
		iface[i] = m
	}
	result, err := doSimpleLLMRequest(context.Background(), cfg, iface, &http.Client{Timeout: 30 * time.Second}, 30*time.Second)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c *sessionStartLLMCaller) IsConfigured() bool {
	return c.app.isMaclawLLMConfigured()
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
