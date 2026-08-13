package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// AskUserRequest represents a structured question from the agent to the user.
type AskUserRequest struct {
	Question  string   `json:"question"`
	Options   []string `json:"options,omitempty"`
	Context   string   `json:"context,omitempty"`
	InputType string   `json:"input_type,omitempty"` // "choice", "text", "confirm"
}

// toolAskUser handles the ask_user tool call. It formats the question as a
// structured prompt and returns it as the tool result. The agent loop will
// detect this special result and pause for user input.
//
// In the GUI, this renders as a structured card with options/buttons.
// In IM gateways (Feishu/WeChat/QQ), this renders as an interactive card.
// The user's response is injected back as the tool_result for the next round.
func (h *IMMessageHandler) toolAskUser(args map[string]interface{}) string {
	question, _ := args["question"].(string)
	if question == "" {
		return "错误: 缺少 question 参数"
	}

	inputType, _ := args["input_type"].(string)
	if inputType == "" {
		inputType = "text"
	}
	inputKind := normalizeAskUserInputKind(inputType)
	inputType = inputKind.String()

	var options []string
	if rawOpts, ok := args["options"]; ok {
		switch v := rawOpts.(type) {
		case []interface{}:
			for _, opt := range v {
				if s, ok := opt.(string); ok {
					options = append(options, s)
				}
			}
		case string:
			// Try JSON array
			var parsed []string
			if json.Unmarshal([]byte(v), &parsed) == nil {
				options = parsed
			}
		}
	}

	ctx, _ := args["context"].(string)

	// For confirm type, ensure we have yes/no options
	if inputKind.IsConfirm() && len(options) == 0 {
		options = []string{"确认", "取消"}
	}

	// Build the structured response that the agent loop will detect
	// and render as an interactive UI element.
	req := AskUserRequest{
		Question:  question,
		Options:   options,
		Context:   ctx,
		InputType: inputType,
	}

	data, _ := json.Marshal(req)

	// Return a special marker that the agent loop can detect.
	// The loop will pause and wait for user input, then inject the
	// user's response as the tool_result.
	return fmt.Sprintf("__ASK_USER__%s", string(data))
}

// IsAskUserResult checks if a tool result is an ask_user structured question.
func IsAskUserResult(result string) bool {
	return strings.HasPrefix(result, "__ASK_USER__")
}

// ParseAskUserResult extracts the AskUserRequest from a tool result.
func ParseAskUserResult(result string) (*AskUserRequest, bool) {
	if !IsAskUserResult(result) {
		return nil, false
	}
	jsonStr := strings.TrimPrefix(result, "__ASK_USER__")
	var req AskUserRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		return nil, false
	}
	return &req, true
}

// FormatAskUserForDisplay formats an AskUserRequest for text-based display
// (used in TUI and IM gateways that don't support interactive cards).
func FormatAskUserForDisplay(req *AskUserRequest) string {
	var b strings.Builder
	if req.Context != "" {
		b.WriteString(req.Context)
		b.WriteString("\n\n")
	}
	b.WriteString(req.Question)
	if len(req.Options) > 0 {
		b.WriteString("\n")
		for i, opt := range req.Options {
			b.WriteString(fmt.Sprintf("\n  %d. %s", i+1, opt))
		}
	}
	switch normalizeAskUserInputKind(req.InputType) {
	case askUserInputConfirm:
		b.WriteString("\n\n请输入：确认 或 取消")
	case askUserInputChoice:
		if len(req.Options) > 0 {
			b.WriteString("\n\n请输入：选项编号或内容")
		}
	default:
		// text type or unspecified — guide the user to reply directly.
		b.WriteString("\n\n请直接输入您的回复")
	}
	return b.String()
}

type agentLoopAskUserToolResult struct {
	Result       string
	Response     *IMAgentResponse
	Conversation []interface{}
	History      []agent.ConversationEntry
	ToolResults  []string
}

func (h *IMMessageHandler) handleAgentLoopAskUserToolResult(userID, platform, msgContent, result string, gateActive bool, tcID string, conversation []interface{}, history []agent.ConversationEntry, toolResults []string, recordToolResult func(string, interface{}, string, string), persistHistory ...bool) agentLoopAskUserToolResult {
	out := agentLoopAskUserToolResult{
		Result:       result,
		Conversation: conversation,
		History:      history,
		ToolResults:  toolResults,
	}
	askReq, ok := ParseAskUserResult(result)
	if !ok {
		return out
	}
	if gateActive {
		plainText := askReq.Question
		for i, opt := range askReq.Options {
			plainText += fmt.Sprintf("\n%d. %s", i+1, opt)
		}
		out.Result = fmt.Sprintf("ask_user is blocked by the coding workflow confirmation gate. Present this question as normal assistant text and wait for user confirmation: %s", plainText)
		return out
	}

	displayText := FormatAskUserForDisplay(askReq)
	if !normalizeIMMessagePlatformKind(platform).IsDesktop() {
		if trimmedMsg := strings.TrimSpace(stripThinkingTags(msgContent)); trimmedMsg != "" {
			displayText = trimmedMsg + "\n\n---\n\n" + displayText
		}
	}
	toolResult := fmt.Sprintf("Asked user: %s", askReq.Question)
	out.ToolResults = append(out.ToolResults, toolResult)
	// "paused" matches shared RecordEarlyStopToolResult / interactive training labels.
	if recordToolResult != nil {
		recordToolResult(tcID, toolResult, "ask_user", "paused")
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
		ToolName:    "ask_user",
		ToolOutcome: "paused",
	})
	shouldPersistHistory := len(persistHistory) == 0 || persistHistory[0]
	if shouldPersistHistory {
		h.saveConversationHistoryTimed(userID, out.History, nil)
		h.commitPendingAskUser(userID, askReq, out.History)
	}
	resp := buildAskUserResponse(displayText, askReq)
	out.Response = resp
	return out
}

// commitPendingAskUser publishes the question only after its paired tool result
// has reached durable conversation history. Shared-loop callers defer this
// until their history-and-checkpoint transition succeeds, so a failed atomic
// write cannot strand an interactive answer in process-local state.
func (h *IMMessageHandler) commitPendingAskUser(userID string, askReq *AskUserRequest, history []agent.ConversationEntry) {
	if h == nil || askReq == nil {
		return
	}
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  askReq.Question,
		Options:   askReq.Options,
		InputType: askReq.InputType,
		History:   cloneConversationEntries(history),
		Timestamp: time.Now(),
	})
}

func buildAskUserResponse(displayText string, askReq *AskUserRequest) *IMAgentResponse {
	resp := &IMAgentResponse{Text: displayText, ResponseSource: imResponseSourceAskUser.String()}
	if askReq == nil {
		return resp
	}
	askInputKind := normalizeAskUserInputKind(askReq.InputType)
	if askInputKind.IsChoice() && len(askReq.Options) > 0 {
		actions := make([]IMResponseAction, len(askReq.Options))
		for i, opt := range askReq.Options {
			actions[i] = IMResponseAction{Label: opt, Command: opt}
		}
		resp.Actions = actions
	} else if askInputKind.IsConfirm() {
		resp.Actions = []IMResponseAction{
			{Label: "Confirm", Command: "confirm"},
			{Label: "Cancel", Command: "cancel"},
		}
	}
	return resp
}
