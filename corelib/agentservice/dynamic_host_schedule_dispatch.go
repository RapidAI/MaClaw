package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostScheduleDispatchProviderID     = "core-schedule-dispatch"
	reviewedHostScheduleDispatchImplementation = "channel"
	reviewedHostScheduleDispatchAdapterName    = "host_schedule_dispatch_channel"
)

type reviewedHostScheduleDispatcher interface {
	DispatchReviewedHostSchedule(ctx context.Context, principal Principal, destinationID string) (string, error)
}

func reviewedHostTrustedDestination(destinationID string) bool {
	destinationID = strings.TrimSpace(destinationID)
	switch {
	case strings.HasPrefix(destinationID, "group:") && len(destinationID) > len("group:"):
		return true
	case strings.HasPrefix(destinationID, "user:") && len(destinationID) > len("user:"):
		return true
	default:
		return false
	}
}

// IsTrustedChannelDestination reports whether destinationID is already a
// transport-typed group: or user: identity. Bare names are never trusted.
func IsTrustedChannelDestination(destinationID string) bool {
	return reviewedHostTrustedDestination(destinationID)
}

// TrustedChannelDestinationID extracts a group: or user: destination from
// inbound message/session metadata only. It never reads user text, group_name,
// channel, or billing keys such as llm_service_group_id.
func TrustedChannelDestinationID(metas ...map[string]string) string {
	if dest := firstTrustedTypedDestination(metas, "destination_id", "destination"); dest != "" {
		return dest
	}
	if groupID := firstInboundMetaValue(metas, "im_group_id", "group_id"); groupID != "" {
		if reviewedHostTrustedDestination(groupID) {
			return groupID
		}
		if strings.Contains(groupID, ":") {
			return ""
		}
		return "group:" + groupID
	}
	if userID := firstInboundMetaValue(metas, "im_user_id"); userID != "" {
		if reviewedHostTrustedDestination(userID) {
			return userID
		}
		if strings.Contains(userID, ":") {
			return ""
		}
		return "user:" + userID
	}
	if contactID := firstInboundMetaValue(metas, "contact_id"); contactID != "" {
		if reviewedHostTrustedDestination(contactID) {
			return contactID
		}
		return "user:" + contactID
	}
	return ""
}

func firstTrustedTypedDestination(metas []map[string]string, keys ...string) string {
	for _, key := range keys {
		if dest := firstInboundMetaValue(metas, key); reviewedHostTrustedDestination(dest) {
			return dest
		}
	}
	return ""
}

func firstInboundMetaValue(metas []map[string]string, keys ...string) string {
	for _, meta := range metas {
		if meta == nil {
			continue
		}
		for _, key := range keys {
			if value := strings.TrimSpace(meta[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func reviewedHostScheduleDispatchInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostScheduleDispatchContractDigest() string {
	return coretool.SchemaDigest([]byte("schedule.dispatch.channel:v1:host-schedule-dispatch"))
}

// ProjectReviewedHostScheduleDispatchProvider projects due-time channel
// dispatch. The schema is empty: destinations come only from inbound
// transport. Missing destinations must not fall back to administer.
func ProjectReviewedHostScheduleDispatchProvider(dispatcher reviewedHostScheduleDispatcher, destinationID string) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if dispatcher == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host schedule dispatcher is unavailable")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("trusted_delivery_target_missing")
	}
	parameters := reviewedHostScheduleDispatchInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host schedule dispatch schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostScheduleDispatchContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-schedule-dispatch-empty-v1", contractDigest, invocationDigest, destinationID,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostScheduleDispatchAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostScheduleDispatchProviderID,
			ImplementationID: reviewedHostScheduleDispatchImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: coretool.CapabilityScheduleDispatchChannel,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostScheduleDispatch(dispatcher, destinationID)}, nil
}

func AttachReviewedHostScheduleDispatchProvider(catalog DynamicSemanticCatalog, dispatcher reviewedHostScheduleDispatcher, destinationID string) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostScheduleDispatchProvider(dispatcher, destinationID)
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

func executeReviewedHostScheduleDispatch(dispatcher reviewedHostScheduleDispatcher, destinationID string) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if dispatcher == nil {
			return "", fmt.Errorf("host_schedule_dispatch_unavailable")
		}
		if len(args) != 0 {
			return "", fmt.Errorf("host_schedule_dispatch_arguments_rejected")
		}
		if !reviewedHostTrustedDestination(destinationID) {
			return "", fmt.Errorf("trusted_delivery_target_missing")
		}
		return dispatcher.DispatchReviewedHostSchedule(ctx, principal, destinationID)
	}
}

func (c *coreAgentCallbacks) DispatchReviewedHostSchedule(ctx context.Context, principal Principal, destinationID string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_schedule_dispatch_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_schedule_dispatch_principal_mismatch")
	}
	destinationID = strings.TrimSpace(destinationID)
	if !reviewedHostTrustedDestination(destinationID) {
		return "", fmt.Errorf("trusted_delivery_target_missing")
	}
	if destinationID != strings.TrimSpace(c.trustedDestinationID) {
		return "", fmt.Errorf("trusted_delivery_target_mismatch")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	if taskID := c.takeAdministeredScheduleID(); taskID != "" {
		if _, ok := reviewedHostScheduleDispatchFireChannel(c.inboundChannelScope); !ok {
			return "", fmt.Errorf("trusted_dispatch_channel_unavailable")
		}
		if c.scheduleDispatchBindings == nil {
			return "", fmt.Errorf("schedule_dispatch_binding_store_unavailable")
		}
		principalID := strings.TrimSpace(c.principal.UserID)
		if principalID == "" {
			principalID = memoryOwnerIDForPrincipal(c.principal)
		}
		if err := c.scheduleDispatchBindings.Put(ScheduleDispatchBinding{
			TaskID: taskID, ChannelScope: c.inboundChannelScope, DestinationID: destinationID, PrincipalID: principalID, BoundAt: time.Now().UTC(),
		}); err != nil {
			return "", err
		}
		if c.executor != nil {
			c.executor.ensureReviewedHostScheduleDispatchFire(c.dataDir)
		}
	}
	// Prepare only. Do not call scheduleHandler or imMessageHandler: those
	// accept model group_name / channel soup and would treat this as a send.
	return "Schedule dispatch prepared for " + destinationID + ". This is not a send.", nil
}

func (c *coreAgentCallbacks) rememberAdministeredScheduleID(id string) {
	if c == nil {
		return
	}
	c.lastAdministeredScheduleID = strings.TrimSpace(id)
}

func (c *coreAgentCallbacks) takeAdministeredScheduleID() string {
	if c == nil {
		return ""
	}
	id := strings.TrimSpace(c.lastAdministeredScheduleID)
	c.lastAdministeredScheduleID = ""
	return id
}
