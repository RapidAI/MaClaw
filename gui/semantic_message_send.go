package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedMessageSendAdapter        = "semantic_send_trusted_im"
	semanticTrustedMessageSendImplementation = "trusted-message-send-v1"
)

func semanticUnpublishedLegacyMessageSendProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityMessageSendIM {
			return true
		}
	}
	return false
}

func semanticTrustedMessageSendPublished(channel, destination string) bool {
	return semanticFileDeliveryPublished(channel) && semanticTrustedDispatchDestination(destination)
}

func semanticTrustedMessageSendDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedMessageSendAdapter,
			"description": "Send text to the host-authenticated IM destination. Channel and group fields are rejected.",
			"parameters":  semanticTrustedMessageSendInvocationSchema(),
		},
	}
}

func semanticTrustedMessageSendInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}
}

func semanticTrustedMessageSendArgsAllowed(args map[string]interface{}) (text string, err error) {
	if len(args) > 1 {
		return "", fmt.Errorf("trusted_message_send_arguments_rejected")
	}
	hasText := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_message_send_arguments_rejected")
		}
		switch key {
		case "text":
			text, hasText = value, true
		default:
			return "", fmt.Errorf("trusted_message_send_arguments_rejected")
		}
	}
	text = strings.TrimSpace(text)
	if !hasText || text == "" {
		return "", fmt.Errorf("trusted_message_send_text_required")
	}
	return text, nil
}

func semanticMessageSendIM(selection tool.PlannedSelection) bool {
	if selection.Provider.Kind != "channel" {
		return false
	}
	return selection.FitProof.MatchedCapability == tool.CapabilityMessageSendIM
}
