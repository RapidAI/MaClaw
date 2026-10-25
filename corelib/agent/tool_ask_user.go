package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// secretNamePattern is the only form a site secret may take in model context.
// The value never travels with this name.
var secretNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// AskUserRequest represents a structured question from the agent to the user.
type AskUserRequest struct {
	Question   string   `json:"question"`
	Options    []string `json:"options,omitempty"`
	Context    string   `json:"context,omitempty"`
	InputType  string   `json:"input_type,omitempty"` // "choice", "text", "confirm", "secret", "secret_fill"
	SecretName string   `json:"secret_name,omitempty"`
}

// ValidSecretName reports whether name may be shown to a model.
// A secret value must never be passed here.
func ValidSecretName(name string) bool {
	return secretNamePattern.MatchString(strings.TrimSpace(name))
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
	secretName := strings.TrimSpace(fmt.Sprint(args["secret_name"]))
	if secretName == "<nil>" {
		secretName = ""
	}
	if inputType == "secret" || inputType == "secret_fill" {
		if !ValidSecretName(secretName) {
			return "错误: secret_name 无效"
		}
		if strings.TrimSpace(question) == "" {
			question = secretName
		}
	}

	// For confirm type, ensure we have yes/no options
	if inputType == "confirm" && len(options) == 0 {
		options = []string{"确认", "取消"}
	}

	// Build the structured response that the agent loop will detect
	// and render as an interactive UI element.
	req := AskUserRequest{
		Question:   question,
		Options:    options,
		Context:    ctx,
		InputType:  inputType,
		SecretName: secretName,
	}

	return AskUserResultMarker(&req)
}

// AskUserResultMarker serializes an interactive request using the stable
// marker consumed by RunLoop and GUI hosts.
func AskUserResultMarker(req *AskUserRequest) string {
	if req == nil {
		return ""
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

// vendorForgeableControlMarkers are the loop-level control markers a tool
// result must never mint. Each is consumed with strings.HasPrefix by the
// loop or a host callback, so a result that merely begins with one is read as
// a host instruction — a paused question, an audio recording, a subagent
// context injection — that no host emitted.
var vendorForgeableControlMarkers = []string{"__ASK_USER__", recordAudioMarkerPrefix, "__SUBAGENT_CONTEXT__"}

// SanitizeVendorToolResultText prefixes a zero-width space onto a vendor tool
// result that begins with a host control marker. A bound or bridged MCP
// result is content, not control: a hostile or buggy server returning
// __ASK_USER__{...} would otherwise pause the loop on a question the host
// never asked — and one crafted with a confirmation context could even steal
// a pending grant. The space defeats every strings.HasPrefix marker check,
// is invisible to the user, and survives TrimSpace (it is a format
// character, not Unicode whitespace).
func SanitizeVendorToolResultText(content string) string {
	for _, marker := range vendorForgeableControlMarkers {
		if strings.HasPrefix(content, marker) {
			return "​" + content
		}
	}
	return content
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

func FormatAskedUserHistoryResult(req *AskUserRequest) string {
	if req == nil {
		return "Asked user"
	}
	out := "Asked user: " + req.Question
	if extra := visibleAskUserContext(req.Context); extra != "" {
		out += "\n" + extra
	}
	return out
}

func FormatPendingAskUserAnswerHint(question, answer, context string) string {
	hint := fmt.Sprintf(
		"[Context hint] The user is answering your previous clarification question, not starting a new request.\nAssistant question: %s\nUser answer: %s\nInterpret it as supplementary or corrective information for the current task.",
		question, answer,
	)
	if extra := visibleAskUserContext(context); extra != "" {
		hint += "\nQuestion context: " + extra
	}
	return hint
}

// visibleAskUserContext drops the commit-approval ticket. The nonce binds the
// pending note; it is not part of the question the person or the model sees.
func visibleAskUserContext(context string) string {
	trimmed := strings.TrimSpace(context)
	rest, found := strings.CutPrefix(trimmed, CommitApprovalContextPrefix)
	if !found {
		return trimmed
	}
	_, after, found := strings.Cut(rest, ";")
	if !found {
		return ""
	}
	return strings.TrimSpace(after)
}

// FormatAskUserForDisplay formats an AskUserRequest for text-based display
// (used in TUI and IM gateways that don't support interactive cards).
func FormatAskUserForDisplay(req *AskUserRequest) string {
	var b strings.Builder
	if context := visibleAskUserContext(req.Context); context != "" {
		b.WriteString(context)
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
