package agentservice

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

const (
	sessionMetaPendingAskUser          = "pending_ask_user"
	sessionMetaPendingAskUserQuestion  = "pending_ask_user_question"
	sessionMetaPendingAskUserInputType = "pending_ask_user_input_type"
	sessionMetaPendingAskUserOptions   = "pending_ask_user_options_json"
)

func buildEffectiveUserContent(sess Session, raw string) (string, *agent.AskUserRequest) {
	if sess.Metadata == nil || sess.Metadata[sessionMetaPendingAskUser] != "true" {
		return raw, nil
	}
	question := strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserQuestion])
	inputType := strings.TrimSpace(sess.Metadata[sessionMetaPendingAskUserInputType])
	options := parsePendingAskUserOptions(sess.Metadata[sessionMetaPendingAskUserOptions])
	answer := resolveAskUserAnswer(raw, options)
	req := &agent.AskUserRequest{Question: question, InputType: inputType, Options: options}
	text := agent.FormatPendingAskUserAnswerHint(question, answer, "")
	if strings.Contains(question, "登录状态会留在这个浏览器里") {
		text += "\nContinue the original task in that same browser. If this site opens a window, continue there; the website login stays in this browser. Do not open another site, profile, or browser."
	}
	return text, req
}

func parsePendingAskUserOptions(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func resolveAskUserAnswer(raw string, options []string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(options) == 0 {
		return raw
	}
	for idx, option := range options {
		if trimmed == option || trimmed == fmt.Sprintf("%d", idx+1) {
			return option
		}
	}
	return raw
}

func clearPendingAskUserMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return metadata
	}
	next := cloneMap(metadata)
	delete(next, sessionMetaPendingAskUser)
	delete(next, sessionMetaPendingAskUserQuestion)
	delete(next, sessionMetaPendingAskUserInputType)
	delete(next, sessionMetaPendingAskUserOptions)
	return next
}

func setPendingAskUserMetadata(metadata map[string]string, values map[string]string) map[string]string {
	next := clearPendingAskUserMetadata(metadata)
	if next == nil {
		next = map[string]string{}
	}
	next[sessionMetaPendingAskUser] = "true"
	next[sessionMetaPendingAskUserQuestion] = values[metaAskUserQuestion]
	next[sessionMetaPendingAskUserInputType] = values[metaAskUserInputType]
	if options := values[metaAskUserOptionsJSON]; strings.TrimSpace(options) != "" {
		next[sessionMetaPendingAskUserOptions] = options
	}
	return next
}
