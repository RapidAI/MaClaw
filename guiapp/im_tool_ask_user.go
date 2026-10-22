package guiapp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/mailbox"
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
	marker := fmt.Sprintf("__ASK_USER__%s", string(data))
	return marker
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
	// Confirmation gates stamp an unguessable nonce into Context. That token
	// binds the pending note; it is not part of the question. The same strip
	// the stored pending state uses keeps the card and the IM text free of it.
	if context := pendingAskUserContext(req.Context); context != "" {
		b.WriteString(context)
		b.WriteString("\n\n")
	}
	b.WriteString(req.Question)
	kind := normalizeAskUserInputKind(req.InputType)
	// Numbers are a selection. Print them only for the list a numbered reply
	// resolves: choice options. Confirm numbers are the action labels the
	// channel appends, so listing Options here would print a second list.
	// A text question keeps a typed "1", so its options stay unnumbered.
	if choices := askUserShownChoices(req.InputType, req.Options); len(choices) > 0 && !kind.IsConfirm() {
		b.WriteString("\n")
		for i, opt := range choices {
			b.WriteString(fmt.Sprintf("\n  %d. %s", i+1, opt))
		}
	} else if !kind.IsConfirm() && len(req.Options) > 0 {
		b.WriteString("\n")
		for _, opt := range req.Options {
			if strings.TrimSpace(opt) == "" {
				continue
			}
			b.WriteString("\n- " + opt)
		}
	}
	switch kind {
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
	// Workflow phases and canary-skipped turns still use the legacy loop.
	// Resolve here so that loop continues instead of parking on the card.
	// The shared loop rewrites the same marker before corelib can pause.
	if resolved, resolvedOK := unattendedResolvedAsk(h.resolveUnattendedAsk(userID, result)); resolvedOK {
		out.Result = resolved
		return out
	}
	if h != nil && strings.HasPrefix(result, "__ASK_USER__") && h.unattendedTurn(userID) {
		out.Result = "ask_user is unavailable in scheduled tasks; choose the next action directly and continue."
		return out
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
	toolResult := agent.FormatAskedUserHistoryResult(&agent.AskUserRequest{
		Question: askReq.Question,
		Context:  pendingAskUserContext(askReq.Context),
	})
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
		h.commitPendingAskUser(userID, askReq, out.History, nil)
	}
	resp := buildAskUserResponse(displayText, askReq)
	// Lansenger and the third-party gateway turn every action into another
	// numbered list. A choice question already numbers askUserShownChoices in
	// the text, so a second list can disagree after markdown is stripped.
	// Confirm questions do not number the body; those channels must keep the
	// actions and print them once.
	if resp != nil && normalizeAskUserInputKind(askReq.InputType).IsChoice() && channelAppendsAskUserActionsAsText(platform) {
		resp.Actions = nil
	}
	out.Response = resp
	return out
}

// channelAppendsAskUserActionsAsText reports channels that have no buttons
// and that append resp.Actions as a second numbered list under the message body.
func channelAppendsAskUserActionsAsText(platform string) bool {
	switch normalizeIMMessagePlatformKind(platform) {
	case imMessagePlatformLansenger, imMessagePlatformLansengerLocal:
		return true
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	return platform == "thirdparty" || strings.HasPrefix(platform, "thirdparty:")
}

// commitPendingAskUser publishes the question only after its paired tool result
// has reached durable conversation history. Shared-loop callers defer this
// until their history-and-checkpoint transition succeeds, so a failed atomic
// write cannot strand an interactive answer in process-local state.
func (h *IMMessageHandler) commitPendingAskUser(userID string, askReq *AskUserRequest, history []agent.ConversationEntry, workingState *agent.WorkingState) {
	if h == nil || askReq == nil {
		return
	}
	// The bar may already have deleted this turn's question. Publishing again
	// would capture the next message. The generation check and the store are
	// one critical section, so the delete cannot land between them.
	turnGen := h.pendingForegroundGen(userID)
	h.goalBarQuestionMu.Lock()
	defer h.goalBarQuestionMu.Unlock()
	if h.goalBarTurnMustNotAsk(userID, turnGen) {
		return
	}
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:       askReq.Question,
		Options:        askReq.Options,
		InputType:      askReq.InputType,
		Context:        pendingAskUserContext(askReq.Context),
		History:        cloneConversationEntries(history),
		Timestamp:      time.Now(),
		WorkingState:   agent.CloneWorkingState(workingState),
		MailboxDraftID: h.takeMailboxDraftNoteFor(userID, askReq),
		MCPServerWrite: h.takeMCPServerWriteNoteFor(userID, askReq),
	})
}

// newConfirmNonce mints the unguessable token a confirmation gate stamps
// into its ask_user card's context. The pending note records it, and only
// the committed question repeating it may consume the note — tool results
// that echo the sentinel cannot forge a card that binds.
func newConfirmNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// pendingAskUserContext strips the binding sentinels gates stamp onto a
// card's context — mailbox send confirmation, unreviewed MCP write
// confirmation. The segment right after a sentinel is the nonce that binds
// the pending note; what follows it is the real context the model-facing
// answer hint may repeat.
func pendingAskUserContext(context string) string {
	trimmed := strings.TrimSpace(context)
	for _, marker := range []string{mailboxDraftAskContext, mcpServerWriteAskContext} {
		if rest, ok := strings.CutPrefix(trimmed, marker); ok {
			// <marker><nonce>;<context>
			if _, after, found := strings.Cut(rest, ";"); found {
				return strings.TrimSpace(after)
			}
			return ""
		}
	}
	return trimmed
}

func buildAskUserResponse(displayText string, askReq *AskUserRequest) *IMAgentResponse {
	resp := &IMAgentResponse{Text: displayText, ResponseSource: imResponseSourceAskUser.String()}
	if askReq == nil {
		return resp
	}
	askInputKind := normalizeAskUserInputKind(askReq.InputType)
	if choices := askUserShownChoices(askReq.InputType, askReq.Options); askInputKind.IsChoice() && len(choices) > 0 {
		actions := make([]IMResponseAction, len(choices))
		for i, opt := range choices {
			actions[i] = IMResponseAction{Label: opt, Command: opt}
		}
		resp.Actions = actions
	} else if askInputKind.IsConfirm() {
		// Channels without buttons print these labels as the numbered reply.
		// The command stays confirm/cancel so a desktop click still settles.
		choices := askUserShownChoices(askReq.InputType, askReq.Options)
		resp.Actions = []IMResponseAction{
			{Label: choices[0], Command: "confirm"},
			{Label: choices[1], Command: "cancel"},
		}
	}
	return resp
}

// askUserShownChoices is the list a text channel numbers. A numbered reply
// selects from this same list. Choice questions print their options, including
// blanks, so the index stays aligned with the button. Confirm questions print
// the settler's approve/cancel pair when the question uses those words, and
// otherwise the English command labels. A captcha whose only option is 继续,
// and a confirm question that stored no options, both print Confirm/Cancel.
func askUserShownChoices(inputType string, options []string) []string {
	kind := normalizeAskUserInputKind(inputType)
	switch {
	case kind.IsChoice():
		return options
	case kind.IsConfirm():
		approve, cancel := "Confirm", "Cancel"
		if len(options) >= 2 && mailbox.IsApproveText(options[0]) && mailbox.IsCancelText(options[1]) {
			approve = strings.TrimSpace(options[0])
			cancel = strings.TrimSpace(options[1])
		}
		return []string{approve, cancel}
	default:
		return nil
	}
}
