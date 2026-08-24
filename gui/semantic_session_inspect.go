package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedSessionAdapter        = "semantic_inspect_trusted_session"
	semanticTrustedSessionImplementation = "trusted-session-inspect-v1"
	semanticTrustedSessionTimeout        = 10 * time.Second
)

func semanticUnpublishedLegacySessionProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilitySessionManageCoding {
			return true
		}
	}
	return false
}

func semanticTrustedSessionDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedSessionAdapter,
			"description": "Inspect the host-local coding sessions. Field presence decides list versus get. This does not send input, interrupt, or launch a session.",
			"parameters":  semanticTrustedSessionInvocationSchema(),
		},
	}
}

func semanticTrustedSessionInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedSessionArgsAllowed(args map[string]interface{}) (id string, err error) {
	if len(args) > 1 {
		return "", fmt.Errorf("trusted_session_arguments_rejected")
	}
	for key, raw := range args {
		if key != "id" {
			return "", fmt.Errorf("trusted_session_arguments_rejected")
		}
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_session_arguments_rejected")
		}
		id = strings.TrimSpace(value)
	}
	if _, ok := semanticTrustedSessionDispatch(id); !ok {
		return "", fmt.Errorf("trusted_session_field_presence_rejected")
	}
	return id, nil
}

func semanticTrustedSessionDispatch(id string) (string, bool) {
	if id == "" {
		return "list", true
	}
	return "get", true
}

func (h *IMMessageHandler) inspectTrustedSessions(principalID, id string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_session_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_session_principal_required")
	}
	if h.semanticTrustedSession != nil {
		return h.semanticTrustedSession(principalID, id)
	}
	id = strings.TrimSpace(id)
	if _, ok := semanticTrustedSessionDispatch(id); !ok {
		return "", fmt.Errorf("trusted_session_field_presence_rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedSessionTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if id == "" {
		if h.manager == nil {
			return "当前没有编码会话。", nil
		}
		listed := h.manager.List()
		if len(listed) == 0 {
			return "当前没有编码会话。", nil
		}
		sort.Slice(listed, func(i, j int) bool {
			return listed[i].ID < listed[j].ID
		})
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个编码会话:\n", len(listed))
		for _, session := range listed {
			fmt.Fprintf(&b, "- %s\n", semanticTrustedSessionLine(session))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
	if h.manager == nil {
		return "", fmt.Errorf("trusted_session_not_found")
	}
	session, ok := h.manager.Get(id)
	if !ok || session == nil {
		return "", fmt.Errorf("trusted_session_not_found")
	}
	return semanticTrustedSessionProjection("current", session), nil
}

func semanticTrustedSessionProjection(kind string, session *RemoteSession) string {
	line := semanticTrustedSessionLine(session)
	if kind == "current" {
		return "会话 [" + line + "]"
	}
	return line
}

func semanticTrustedSessionLine(session *RemoteSession) string {
	if session == nil {
		return ""
	}
	session.mu.RLock()
	status := session.Status
	session.mu.RUnlock()
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = session.ID
	}
	return fmt.Sprintf("%s [%s] 工具=%s 标题=%s", session.ID, status, session.Tool, title)
}

func semanticTrustedSessionResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_session_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_session_empty")
	}
	return text, nil
}
