package guiapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/mailbox"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedMailboxSendAdapter             = "semantic_send_trusted_mailbox"
	semanticTrustedMailboxSendImplementation      = "trusted-mailbox-send-v1"
	semanticTrustedMailboxConfigureAdapter        = "semantic_configure_trusted_mailbox"
	semanticTrustedMailboxConfigureImplementation = "trusted-mailbox-configure-v1"
)

func semanticUnpublishedLegacyMailboxSendProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageSendMailbox {
			return true
		}
	}
	return false
}

func semanticUnpublishedLegacyMailboxConfigureProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageConfigureMailbox {
			return true
		}
	}
	return false
}

func semanticTrustedMailboxSendDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMailboxSendAdapter,
			"description": mailbox.ToolDescriptionSend(),
			"parameters":  semanticTrustedMailboxSendInvocationSchema(),
		},
	}
}

func semanticTrustedMailboxSendInvocationSchema() map[string]interface{} {
	return mailbox.SendInvocationSchema()
}

func semanticTrustedMailboxConfigureDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMailboxConfigureAdapter,
			"description": mailbox.ToolDescriptionConfigure(),
			"parameters":  semanticTrustedMailboxConfigureInvocationSchema(),
		},
	}
}

func semanticTrustedMailboxConfigureInvocationSchema() map[string]interface{} {
	return mailbox.ConfigureInvocationSchema()
}

// semanticMailboxSendReceiptSelection is the host receipt for message.send.mailbox.
// The same process freezes the draft and later observes Approve. The prepare
// return is a confirmation, not the SMTP observation, so this is not the
// host-observed allow-list whose handler result is already the effect.
func semanticMailboxSendReceiptSelection(selection tool.PlannedSelection) bool {
	if !strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") {
		return false
	}
	if strings.TrimSpace(selection.AdapterName) != semanticTrustedMailboxSendAdapter {
		return false
	}
	for _, effect := range selection.Effects {
		if effect == tool.EffectExternalEffect {
			return true
		}
	}
	return false
}

func semanticMailboxConfigureSelection(selection tool.PlannedSelection) bool {
	return strings.EqualFold(strings.TrimSpace(selection.Provider.Kind), "builtin") &&
		strings.TrimSpace(selection.AdapterName) == semanticTrustedMailboxConfigureAdapter
}

func (c *sharedAgentLoopCallbacks) executeTrustedMailboxSend(ctx context.Context, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	if c == nil || c.handler == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", Succeeded: false, ReasonCode: "semantic_surface_unavailable"}
	}
	args := canonicalArgs.Values
	if args == nil {
		args = map[string]interface{}{}
	}
	text := c.handler.toolMailboxSend(withMailboxOwner(ctx, c.mailboxPrincipalID()), args)
	switch {
	case strings.HasPrefix(text, "__ASK_USER__"):
		// The draft is frozen. SMTP waits for Approve on the confirmation.
		// The selection is not complete, and the grant leaves the model list
		// so a second call cannot freeze another message.
		return tool.SelectionExecutionResult{Result: text, Succeeded: false, ReasonCode: mailboxSendAwaitingConfirmation}
	case text == mailbox.MsgUncertain || strings.HasPrefix(strings.TrimSpace(text), "[system unknown]"):
		return mailboxTypedFailure(text, "mailbox_send_uncertain", true)
	default:
		return mailboxTypedFailure(text, "mailbox_send_not_sent", false)
	}
}

const mailboxSendAwaitingConfirmation = "mailbox_send_awaiting_confirmation"

func mailboxTypedFailure(text, code string, unknown bool) tool.SelectionExecutionResult {
	text = strings.TrimSpace(text)
	if text == "" {
		text = code
	}
	prefix := "[system rejected]"
	if unknown {
		prefix = "[system unknown]"
	}
	if !strings.HasPrefix(text, prefix) {
		text = prefix + " " + text
	}
	return tool.SelectionExecutionResult{Result: text, Succeeded: false, Unknown: unknown, ReasonCode: code}
}

func (c *sharedAgentLoopCallbacks) executeTrustedMailboxConfigure(ctx context.Context, canonicalArgs tool.CanonicalRequest) tool.SelectionExecutionResult {
	if c == nil || c.handler == nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] semantic tool surface is unavailable", Succeeded: false, ReasonCode: "semantic_surface_unavailable"}
	}
	args := canonicalArgs.Values
	if args == nil {
		args = map[string]interface{}{}
	}
	if reason := mailbox.ConfigureShapeReason(args); reason != "" {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + reason, Succeeded: false, ReasonCode: reason}
	}
	text, err := c.handler.applyMailboxConfigure(withMailboxOwner(ctx, c.mailboxPrincipalID()), args)
	if err != nil {
		return tool.SelectionExecutionResult{Result: "[system rejected] " + configureMailboxToolErr(err, stringArg(args, "password")), Succeeded: false, ReasonCode: "mailbox_configure_rejected"}
	}
	return tool.SelectionExecutionResult{Result: text, Succeeded: true}
}

func (c *sharedAgentLoopCallbacks) mailboxPrincipalID() string {
	if c == nil {
		return ""
	}
	if id := strings.TrimSpace(c.userID); id != "" {
		return id
	}
	if c.semanticSurface != nil {
		return strings.TrimSpace(c.semanticSurface.scope.PrincipalID)
	}
	return ""
}

// mailboxIntakeRejectReason refuses a send or configure call before the grant
// is admitted. A shape the adapter cannot freeze, or a configure call with no
// server, must not retire the one-shot grant and must not write config.
func (c *sharedAgentLoopCallbacks) mailboxIntakeRejectReasonForName(name, argsJSON string) string {
	if c == nil || c.semanticSurface == nil {
		return ""
	}
	grant, ok := c.semanticSurface.grants[name]
	if !ok {
		return ""
	}
	selection, found := semanticSelectionByID(c.semanticSurface.plan, grant.SelectionID)
	if !found {
		return ""
	}
	return c.mailboxIntakeRejectReason(argsJSON, selection)
}

func (c *sharedAgentLoopCallbacks) mailboxIntakeRejectReason(argsJSON string, selection tool.PlannedSelection) string {
	isSend := semanticMailboxSendReceiptSelection(selection)
	if !isSend && !semanticMailboxConfigureSelection(selection) {
		return ""
	}
	canonical, err := c.semanticCanonicalArguments(selection, argsJSON)
	if err != nil {
		return err.Error()
	}
	args := canonical.Values
	if args == nil {
		var parsed map[string]interface{}
		if json.Unmarshal(canonical.CanonicalJSON, &parsed) != nil {
			return "parameter_schema_invalid"
		}
		args = parsed
	}
	if isSend {
		return mailboxSendIntakeReason(args)
	}
	return mailboxConfigureIntakeReason(args)
}

func mailboxSendIntakeReason(args map[string]interface{}) string {
	return mailbox.SendShapeReason(args)
}

func mailboxConfigureIntakeReason(args map[string]interface{}) string {
	return mailbox.ConfigureShapeReason(args)
}
