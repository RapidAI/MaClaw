package agentservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostMessageSendProviderID     = "core-messagesend"
	reviewedHostMessageSendImplementation = "channel"
	reviewedHostMessageSendAdapterName    = "host_message_send_im"
)

type reviewedHostMessageSender interface {
	PrepareReviewedHostMessageSend(ctx context.Context, principal Principal, destinationID, text string) (string, error)
}

func reviewedHostMessageSendInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"text"},
		"additionalProperties": false,
	}
}

func reviewedHostMessageSendContractDigest() string {
	return coretool.SchemaDigest([]byte("message.send.im:v1:host-messagesend"))
}

// ProjectReviewedHostMessageSendProvider projects a quarantined IM text
// prepare. It is not a Skill/MCP discovery entry and must not import GUI
// send_to_im / im_message. The closed schema accepts text only. Channel,
// group_name, and destination are rejected. Destinations come only from
// inbound transport. Prepare is not a send.
func ProjectReviewedHostMessageSendProvider(sender reviewedHostMessageSender, destinationID string) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if sender == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host message sender is unavailable")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	parameters := reviewedHostMessageSendInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host message send schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostMessageSendContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-messagesend-text-v1", contractDigest, invocationDigest, destinationID,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostMessageSendAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostMessageSendProviderID,
			ImplementationID: reviewedHostMessageSendImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityMessageSend,
			Qualifiers: map[string]string{QualifierMessageFormat: MessageFormatText},
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostMessageSend(sender, destinationID)}, nil
}

func AttachReviewedHostMessageSendProvider(catalog DynamicSemanticCatalog, sender reviewedHostMessageSender, destinationID string) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostMessageSendProvider(sender, destinationID)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostMessageSend(sender reviewedHostMessageSender, destinationID string) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if sender == nil {
			return "", fmt.Errorf("host_message_send_unavailable")
		}
		text, err := reviewedHostMessageSendArgsAllowed(args)
		if err != nil {
			return "", err
		}
		if !reviewedHostTrustedDestination(destinationID) {
			return "", fmt.Errorf("trusted_delivery_target_missing")
		}
		return sender.PrepareReviewedHostMessageSend(ctx, principal, destinationID, text)
	}
}

func reviewedHostMessageSendArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("host_message_send_arguments_rejected")
	}
	text := ""
	hasText := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("host_message_send_arguments_rejected")
		}
		switch key {
		case "text":
			text, hasText = value, true
		default:
			return "", fmt.Errorf("host_message_send_arguments_rejected")
		}
	}
	text = strings.TrimSpace(text)
	if !hasText || text == "" {
		return "", fmt.Errorf("host_message_send_text_required")
	}
	return text, nil
}

func (c *coreAgentCallbacks) PrepareReviewedHostMessageSend(ctx context.Context, principal Principal, destinationID, text string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_message_send_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_message_send_principal_mismatch")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return "", fmt.Errorf("trusted_delivery_target_missing")
	}
	if destinationID != strings.TrimSpace(c.trustedDestinationID) {
		return "", fmt.Errorf("trusted_delivery_target_mismatch")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_message_send_text_required")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	if deps, ok := c.reviewedHostMessageSendFireDeps(destinationID, text); ok {
		if err := FireReviewedHostMessageSend(ctx, deps); err != nil {
			return "", err
		}
		return "", fmt.Errorf("host_message_send_unknown")
	}
	// Prepare only when the fire path is unarmed. Do not call
	// imMessageHandler: it accepts model channel / group_name soup and
	// would treat this as a send.
	return "IM message prepared for " + destinationID + ". This is not a send.", nil
}

func (c *coreAgentCallbacks) reviewedHostMessageSendFireDeps(destinationID, text string) (reviewedHostMessageSendFireDeps, bool) {
	if c == nil || c.executor == nil || c.executor.ScheduleDispatchSend == nil {
		return reviewedHostMessageSendFireDeps{}, false
	}
	coordinator := c.executor.reviewedHostScheduleDispatchCoordinator()
	if coordinator == nil {
		return reviewedHostMessageSendFireDeps{}, false
	}
	if _, ok := reviewedHostScheduleDispatchFireChannel(c.inboundChannelScope); !ok {
		return reviewedHostMessageSendFireDeps{}, false
	}
	principalID := strings.TrimSpace(c.principal.UserID)
	if principalID == "" {
		principalID = memoryOwnerIDForPrincipal(c.principal)
	}
	send := c.executor.ScheduleDispatchSend
	principal := c.principal
	return reviewedHostMessageSendFireDeps{
		Coordinator:   coordinator,
		ChannelScope:  c.inboundChannelScope,
		DestinationID: destinationID,
		PrincipalID:   principalID,
		Text:          text,
		Send: func(ctx context.Context, channel string, targets []scheduler.DeliveryTarget, body string) error {
			return send(ctx, principal, channel, targets, body)
		},
	}, true
}
