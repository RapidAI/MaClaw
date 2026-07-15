package httpapi

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// workflowStatusWireType is Hub → machine push for approval/workflow lifecycle
// events (blocked, escalation, etc.). Desktop routes ve:* to handleVEEvent.
const workflowStatusWireType = "ve:workflow_status"

// HubWorkflowParticipantNotifier implements workflow.WorkflowNotifier by pushing
// structured status events to the initiator's machines (and best-effort
// current assignee machines when present on instance data).
type HubWorkflowParticipantNotifier struct {
	sender   machineCommandSender
	store    workflow.InstanceStore
	devices  *device.Service
	identity *auth.IdentityService
}

// Compile-time check.
var _ workflow.WorkflowNotifier = (*HubWorkflowParticipantNotifier)(nil)

// NewHubWorkflowParticipantNotifier constructs a real WorkflowNotifier.
func NewHubWorkflowParticipantNotifier(sender machineCommandSender, store workflow.InstanceStore, devices *device.Service, identity *auth.IdentityService) *HubWorkflowParticipantNotifier {
	return &HubWorkflowParticipantNotifier{sender: sender, store: store, devices: devices, identity: identity}
}

// NotifyInitiator delivers a blocked/attention-style status event to the
// workflow initiator's machines so the desktop can update local projections
// without waiting for the next ops-panel reconcile.
func (n *HubWorkflowParticipantNotifier) NotifyInitiator(ctx context.Context, instanceID string, reason string, details string) error {
	if n == nil || n.sender == nil {
		return fmt.Errorf("workflow participant notifier has no machine sender")
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return fmt.Errorf("instance id is required")
	}
	var inst *workflow.WorkflowInstance
	if n.store != nil {
		var err error
		inst, err = n.store.Get(ctx, instanceID)
		if err != nil {
			return fmt.Errorf("get instance: %w", err)
		}
	}
	recipients := n.resolveRecipientMachines(ctx, inst)
	if len(recipients) == 0 {
		log.Printf("[workflow-notifier] no recipient machines for instance=%s reason=%s", instanceID, reason)
		return nil
	}
	event, status, urgency := classifyWorkflowStatusEvent(reason, details, inst)
	payload := map[string]any{
		"event":       event,
		"status":      status,
		"urgency":     urgency,
		"instance_id": instanceID,
		"reason":      strings.TrimSpace(reason),
		"details":     strings.TrimSpace(details),
		"ts":          time.Now().UTC().Format(time.RFC3339),
	}
	if inst != nil {
		payload["current_node"] = inst.CurrentNodeID
		payload["workflow_id"] = inst.WorkflowID
		if name, ok := inst.InstanceData["workflow_name"].(string); ok {
			payload["workflow_name"] = name
		}
		if blockedReason, ok := inst.InstanceData["blocked_reason"].(string); ok && blockedReason != "" {
			payload["blocked_reason"] = blockedReason
		}
		if blockedDetails, ok := inst.InstanceData["blocked_details"].(string); ok && blockedDetails != "" {
			payload["blocked_details"] = blockedDetails
		}
		if pending, ok := inst.InstanceData["escalation_pending"].(bool); ok && pending {
			payload["escalation_pending"] = true
		}
		if approvers := stringSliceFromPayloadAny(inst.InstanceData["escalation_approvers"]); len(approvers) > 0 {
			payload["escalation_approvers"] = approvers
			payload["escalation_pending"] = true
		} else if s, ok := inst.InstanceData["escalation_approver"].(string); ok && strings.TrimSpace(s) != "" {
			payload["escalation_approver"] = strings.TrimSpace(s)
			payload["escalation_approvers"] = []string{strings.TrimSpace(s)}
			payload["escalation_pending"] = true
		}
	}
	var lastErr error
	for _, machineID := range recipients {
		if err := n.sender.SendToMachine(machineID, map[string]any{
			"type":    workflowStatusWireType,
			"ts":      time.Now().Unix(),
			"payload": payload,
		}); err != nil {
			log.Printf("[workflow-notifier] deliver status to %s failed: %v", machineID, err)
			lastErr = err
			continue
		}
		log.Printf("[workflow-notifier] delivered status instance=%s machine=%s event=%s", instanceID, machineID, event)
	}
	return lastErr
}

// stringSliceFromPayloadAny coerces instance-data list fields for push payloads.
func stringSliceFromPayloadAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					out = append(out, t)
				}
			}
		}
		return out
	default:
		return nil
	}
}

// classifyWorkflowStatusEvent maps blocked/timeout reasons into desktop-friendly
// event/status/urgency so MaClaw can surface attention without string-matching alone.
func classifyWorkflowStatusEvent(reason, details string, inst *workflow.WorkflowInstance) (event, status, urgency string) {
	event = "blocked"
	status = "blocked"
	urgency = "attention"
	blob := strings.ToLower(strings.TrimSpace(reason) + " " + strings.TrimSpace(details))
	if inst != nil && inst.InstanceData != nil {
		if br, ok := inst.InstanceData["blocked_reason"].(string); ok {
			blob += " " + strings.ToLower(strings.TrimSpace(br))
		}
	}
	switch {
	case strings.Contains(blob, "timeout"), strings.Contains(blob, "overdue"), strings.Contains(blob, "escalat"):
		event = "escalation"
		status = "blocked"
		urgency = "overdue"
	case strings.Contains(blob, "unavailable"), strings.Contains(blob, "no fallback"), strings.Contains(blob, "queue_full"):
		event = "blocked"
		status = "blocked"
		urgency = "critical"
	}
	return event, status, urgency
}

func (n *HubWorkflowParticipantNotifier) resolveRecipientMachines(ctx context.Context, inst *workflow.WorkflowInstance) []string {
	if inst == nil {
		return nil
	}
	ids := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		ids[id] = struct{}{}
	}
	// Direct machine id fields when present.
	if inst.InstanceData != nil {
		for _, key := range []string{"requester_machine_id", "initiator_machine_id", "machine_id"} {
			if v, ok := inst.InstanceData[key].(string); ok {
				add(v)
			}
		}
	}
	// Resolve user emails / ids to online (or any) machines.
	candidates := []string{}
	if inst.InstanceData != nil {
		for _, key := range []string{"requester_id", "initiator_id", "applicant", "owner", "submitted_by"} {
			if v, ok := inst.InstanceData[key].(string); ok && strings.TrimSpace(v) != "" {
				candidates = append(candidates, strings.TrimSpace(v))
			}
		}
	}
	for _, candidate := range candidates {
		for _, mid := range n.machinesForIdentity(ctx, candidate) {
			add(mid)
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out
}

func (n *HubWorkflowParticipantNotifier) machinesForIdentity(ctx context.Context, identity string) []string {
	identity = strings.TrimSpace(identity)
	if identity == "" || n.devices == nil {
		return nil
	}
	// If it already looks like a machine id (no @, no uuid-only assumption), try as-is via online check.
	if !strings.Contains(identity, "@") && n.devices.IsMachineOnline(identity) {
		return []string{identity}
	}
	userID := ""
	if n.identity != nil && strings.Contains(identity, "@") {
		// List users is heavy; use email→user via ListUsersForTenant when tenant on context.
		// Best-effort: iterate is ok for small tenants; production may optimize later.
		// Tenant may be in context from store.WithTenant.
		// Fallback: treat identity as user id for ListMachines.
	}
	// Prefer ListMachines by user id when identity is not email.
	if !strings.Contains(identity, "@") {
		userID = identity
	} else if n.identity != nil {
		// Resolve email to user id.
		// Use devices.ListMachines requires userID — try identity package helper if available.
		// Lightweight path: scan is avoided; devices may accept email in some deployments — not here.
		// Look up via identity service ListUsers is expensive; use GetByEmail if exists.
		if u, err := n.identity.LookupUserByEmail(ctx, strings.ToLower(identity)); err == nil && u != nil {
			userID = strings.TrimSpace(u.ID)
		}
	}
	if userID == "" {
		// Last resort: pass email-like string as machine target only if online (unlikely).
		if n.devices.IsMachineOnline(identity) {
			return []string{identity}
		}
		return nil
	}
	machines, err := n.devices.ListMachines(ctx, userID)
	if err != nil || len(machines) == 0 {
		return nil
	}
	// Broadcast to all online machines for the identity (multi-device users).
	// If none are online, fall back to every known machine so offline reconnect
	// still receives the last buffered delivery when the transport allows it.
	var online, offline []string
	for _, m := range machines {
		mid := strings.TrimSpace(m.MachineID)
		if mid == "" {
			continue
		}
		if m.Online {
			online = append(online, mid)
		} else {
			offline = append(offline, mid)
		}
	}
	if len(online) > 0 {
		return online
	}
	return offline
}
