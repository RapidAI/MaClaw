package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// toolRecordAudio opens an interactive long-form recording session.
// The agent loop detects the special result and pauses for the user to
// finish recording (waveform + pause/stop UI on desktop).
func (h *IMMessageHandler) toolRecordAudio(args map[string]interface{}) string {
	if recordDetailEnabled() {
		title, _ := args["title"].(string)
		purpose, _ := args["purpose"].(string)
		log.Printf("[record-audio] tool called title=%q purpose=%q", strings.TrimSpace(title), strings.TrimSpace(purpose))
	}
	return agent.ToolRecordAudio(args)
}

// Shared rejection copy for legacy + shared agent-loop paths (keep model guidance identical).
func recordAudioDesktopOnlyRejection() string {
	return "record_audio is desktop-only (long-form meeting recording with waveform UI). " +
		"This IM channel does not support meeting/long-form recording — native voice notes are too short. " +
		"Tell the user to open the desktop app and start meeting recording there. " +
		"Do not call record_audio again here, do not ask them to send a short voice note as a substitute, " +
		"and do not claim that recording has started."
}

func recordAudioConcurrentRejection(activeTitle string) string {
	return fmt.Sprintf(
		"录音会话已在进行中（%s）。请等待用户停止当前录音后再调用 record_audio。不要并行打开多个录音界面。",
		activeTitle,
	)
}

func recordAudioGateRejection(title string) string {
	return fmt.Sprintf(
		"record_audio is blocked by the coding workflow confirmation gate. Ask the user in plain text whether to start recording (%s) after the gate is cleared.",
		title,
	)
}

// recordAudioOpenedSessionResult is the tool-message content stored in history
// after a successful interactive open (legacy + shared paths must stay identical).
func recordAudioOpenedSessionResult(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "录音"
	}
	return fmt.Sprintf("Opened recording session: %s", title)
}

// recordAudioUserFacingRejectText maps model-facing rejection copy to a short
// user-visible message when the shared path cannot open the card.
func recordAudioUserFacingRejectText(modelFacing, fallbackDisplay string) string {
	modelFacing = strings.TrimSpace(modelFacing)
	fallbackDisplay = strings.TrimSpace(fallbackDisplay)
	switch {
	case strings.Contains(modelFacing, "已在进行中"):
		return modelFacing // already Chinese
	case strings.Contains(modelFacing, "desktop-only"):
		return "会议/长录音仅支持桌面端，请在桌面应用中开启录音。"
	case strings.HasPrefix(modelFacing, "record_audio "):
		return "无法打开录音会话。"
	case modelFacing != "":
		return modelFacing
	case fallbackDisplay != "":
		return fallbackDisplay
	default:
		return "无法打开录音会话。"
	}
}

type agentLoopRecordAudioToolResult struct {
	Result       string
	Response     *IMAgentResponse
	Conversation []interface{}
	History      []agent.ConversationEntry
	ToolResults  []string
}

func (h *IMMessageHandler) handleAgentLoopRecordAudioToolResult(
	userID, platform, _msgContent, result string,
	gateActive bool,
	tcID string,
	conversation []interface{},
	history []agent.ConversationEntry,
	toolResults []string,
	recordToolResult func(string, interface{}, string, string),
	persistHistory ...bool,
) agentLoopRecordAudioToolResult {
	out := agentLoopRecordAudioToolResult{
		Result:       result,
		Conversation: conversation,
		History:      history,
		ToolResults:  toolResults,
	}
	req, ok := agent.ParseRecordAudioResult(result)
	if !ok {
		if recordDetailEnabled() {
			log.Printf("[record-audio] skip non-marker tool result user=%s tc=%s result_prefix=%q", userID, tcID, truncateRecordLog(result, 80))
		}
		return out
	}
	// Product decision: meeting/long-form recording is desktop-only. IM native
	// voice is typically tens of seconds and is not a substitute for this feature.
	if !normalizeIMMessagePlatformKind(platform).IsDesktop() {
		log.Printf("[record-audio] rejected on non-desktop platform user=%s platform=%s title=%q", userID, platform, req.Title)
		out.Result = recordAudioDesktopOnlyRejection()
		return out
	}
	if gateActive {
		// Gate block is user-visible failure mode; always log.
		log.Printf("[record-audio] blocked by coding gate user=%s title=%q platform=%s", userID, req.Title, platform)
		out.Result = recordAudioGateRejection(req.Title)
		return out
	}
	// Reject overlapping interactive sessions for the same user — a second
	// record_audio would overwrite pending state and strand the first UI.
	if rawPending, loaded := h.pendingRecordAudio.Load(userID); loaded {
		if pending, fresh := pendingRecordAudioForCurrentHistory(rawPending, history); fresh && pending != nil {
			log.Printf("[record-audio] rejected concurrent session user=%s active_title=%q new_title=%q", userID, pending.Title, req.Title)
			out.Result = recordAudioConcurrentRejection(pending.Title)
			return out
		}
	}

	displayText := agent.FormatRecordAudioForDisplay(req)
	toolResult := recordAudioOpenedSessionResult(req.Title)
	out.Result = toolResult // replace marker so hosts/trajectory see the friendly tool result
	out.ToolResults = append(out.ToolResults, toolResult)
	// "paused" matches shared RecordEarlyStopToolResult (interactive host UI wait).
	if recordToolResult != nil {
		recordToolResult(tcID, toolResult, "record_audio", "paused")
	}
	out.Conversation = append(out.Conversation, map[string]interface{}{
		"role":         "tool",
		"tool_call_id": tcID,
		"content":      toolResult,
	})
	out.History = append(out.History, agent.ConversationEntry{
		Role:        "tool",
		Content:     toolResult,
		ToolCallID:  tcID,
		ToolName:    "record_audio",
		ToolOutcome: "paused",
	})
	shouldPersistHistory := len(persistHistory) == 0 || persistHistory[0]
	if shouldPersistHistory {
		h.saveConversationHistoryTimed(userID, out.History, nil)
		h.commitPendingRecordAudio(userID, req, out.History)
	}
	if recordDetailEnabled() {
		log.Printf("[record-audio] session opened user=%s platform=%s title=%q purpose=%q history_len=%d tc=%s",
			userID, platform, req.Title, req.Purpose, len(out.History), tcID)
	}

	resp := &IMAgentResponse{
		Text:           displayText,
		ResponseSource: imResponseSourceRecordAudio.String(),
		Fields: []IMResponseField{
			{Label: "recording_title", Value: req.Title},
		},
	}
	if req.Purpose != "" {
		resp.Fields = append(resp.Fields, IMResponseField{Label: "recording_purpose", Value: req.Purpose})
	}
	out.Response = resp
	return out
}

// commitPendingRecordAudio publishes the desktop-only interactive state only
// after the matching conversation history is durable. In the shared loop the
// caller invokes this after SaveAndCompleteInFlightCheckpointForRun succeeds;
// publishing it sooner would discard a prior post-recording choice even when
// the new recording card must be failed closed.
func (h *IMMessageHandler) commitPendingRecordAudio(userID string, req *agent.RecordAudioRequest, history []agent.ConversationEntry) {
	if h == nil || req == nil {
		return
	}
	// A successfully opened new recording supersedes any unfinished
	// post-recording choice (minutes/transcribe/keep) from a previous save.
	h.clearPendingPostRecording(userID)
	h.pendingRecordAudio.Store(userID, &pendingRecordAudioState{
		Title:     req.Title,
		Purpose:   req.Purpose,
		History:   cloneConversationEntries(history),
		Timestamp: time.Now(),
	})
}

func truncateRecordLog(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
