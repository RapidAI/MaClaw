package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AskUserRequest represents a structured question from the agent to the user.
type AskUserRequest struct {
	Question  string   `json:"question"`
	Options   []string `json:"options,omitempty"`
	Context   string   `json:"context,omitempty"`
	InputType string   `json:"input_type,omitempty"` // "choice", "text", "confirm"
}

// ToolAskUser handles the ask_user tool call. It formats the question as a
// structured prompt and returns it as the tool result. The agent loop will
// detect this special result and pause for user input.
//
// In the GUI, this renders as a structured card with options/buttons.
// In IM gateways (Feishu/WeChat/QQ), this renders as an interactive card.
// The user's response is injected back as the tool_result for the next round.
func ToolAskUser(args map[string]interface{}) string {
	question, _ := args["question"].(string)
	if question == "" {
		return "错误: 缺少 question 参数"
	}

	inputType, _ := args["input_type"].(string)
	if inputType == "" {
		inputType = "text"
	}

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
	if inputType == "confirm" && len(options) == 0 {
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
	b.WriteString("")
	b.WriteString(req.Question)
	if len(req.Options) > 0 {
		b.WriteString("\n")
		for i, opt := range req.Options {
			b.WriteString(fmt.Sprintf("\n  %d. %s", i+1, opt))
		}
	}
	switch req.InputType {
	case "confirm":
		b.WriteString("\n\n请输入：确认 或 取消")
	case "choice":
		if len(req.Options) > 0 {
			b.WriteString("\n\n请输入：选项编号或内容")
		}
	default:
		// text type or unspecified — guide the user to reply directly.
		b.WriteString("\n\n请直接输入您的回复")
	}
	return b.String()
}
