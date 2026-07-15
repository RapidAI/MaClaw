package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

// maclawAppApprovalEngine values for maclawAppApprovalInstance.ApprovalEngine.
const (
	maclawAppApprovalEngineHub   = "hub"
	maclawAppApprovalEngineLocal = "local"
)

// maclawAppHubTriggerResult is the parsed Hub trigger response subset we need.
type maclawAppHubTriggerResult struct {
	InstanceID    string
	WorkflowID    string
	CurrentNodeID string
	Status        string
	Raw           map[string]any
}

// maclawAppHubDecisionResult is the parsed Hub decision response subset.
type maclawAppHubDecisionResult struct {
	InstanceID string
	NodeID     string
	Status     string
	Raw        map[string]any
}

// resolveMaclawAppWorkflowHubAuth prefers the interactive viewer token (human
// panel decisions) and falls back to the machine token used by VE/hub sessions.
func (a *App) resolveMaclawAppWorkflowHubAuth() (hubURL, token string, err error) {
	if a == nil {
		return "", "", fmt.Errorf("app is nil")
	}
	cfg, loadErr := a.LoadConfig()
	if loadErr != nil {
		return "", "", fmt.Errorf("load config: %w", loadErr)
	}
	hubURL = strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if hubURL == "" {
		return "", "", fmt.Errorf("Hub URL not configured")
	}
	token = strings.TrimSpace(cfg.RemoteViewerToken)
	if token == "" {
		token = strings.TrimSpace(cfg.RemoteMachineToken)
	}
	if token == "" {
		return "", "", fmt.Errorf("Hub token not configured")
	}
	return hubURL, token, nil
}

func normalizeMaclawAppApprovalEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case maclawAppApprovalEngineHub, "remote", "enterprise_hub":
		return maclawAppApprovalEngineHub
	case maclawAppApprovalEngineLocal, "desktop", "offline":
		return maclawAppApprovalEngineLocal
	default:
		return ""
	}
}

func maclawAppApprovalUsesHubEngine(instance maclawAppApprovalInstance) bool {
	if normalizeMaclawAppApprovalEngine(instance.ApprovalEngine) == maclawAppApprovalEngineHub {
		return true
	}
	return strings.TrimSpace(instance.HubInstanceID) != "" && strings.TrimSpace(instance.HubNodeID) != ""
}

func maclawAppHubWorkflowIDFromStart(input MaclawAppApprovalWorkflowStartInput, instance maclawAppApprovalInstance) string {
	// Prefer explicit Hub workflow ids. Workflow skill ids are not treated as Hub graph ids
	// unless the caller forces TriggerHubWorkflow (enterprise packs may share one identifier).
	explicit := firstNonEmptyMaclawAppString(input.HubWorkflowID, instance.HubWorkflowID)
	if explicit != "" {
		return explicit
	}
	if input.TriggerHubWorkflow != nil && *input.TriggerHubWorkflow {
		return firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, input.WorkflowSkillID, instance.WorkflowSkillID)
	}
	return ""
}

func maclawAppShouldTriggerHubWorkflow(input MaclawAppApprovalWorkflowStartInput, instance maclawAppApprovalInstance) bool {
	if input.TriggerHubWorkflow != nil && !*input.TriggerHubWorkflow {
		return false
	}
	if strings.TrimSpace(input.HubInstanceID) != "" || strings.TrimSpace(instance.HubInstanceID) != "" {
		// Already bound; optional re-bind only, no re-trigger unless explicitly requested.
		if input.TriggerHubWorkflow == nil || !*input.TriggerHubWorkflow {
			return false
		}
	}
	// Auto path only when an explicit hub_workflow_id is provided.
	// Force path (TriggerHubWorkflow=true) may fall back to skill/approval workflow id.
	return maclawAppHubWorkflowIDFromStart(input, instance) != ""
}

// applyMaclawAppHubBinding copies Hub runtime ids onto the local projection and
// marks approval_engine=hub when a hub instance is bound.
func applyMaclawAppHubBinding(instance maclawAppApprovalInstance, hubWorkflowID, hubInstanceID, hubNodeID string) maclawAppApprovalInstance {
	hubWorkflowID = strings.TrimSpace(hubWorkflowID)
	hubInstanceID = strings.TrimSpace(hubInstanceID)
	hubNodeID = strings.TrimSpace(hubNodeID)
	if hubWorkflowID != "" {
		instance.HubWorkflowID = hubWorkflowID
		instance.ApprovalWorkflowID = firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, hubWorkflowID)
	}
	if hubInstanceID != "" {
		instance.HubInstanceID = hubInstanceID
		instance.ApprovalEngine = maclawAppApprovalEngineHub
		instance.HubSyncError = ""
	}
	if hubNodeID != "" {
		instance.HubNodeID = hubNodeID
		instance.CurrentNode = firstNonEmptyMaclawAppString(hubNodeID, instance.CurrentNode)
		instance.CurrentNodeIDs = appendMaclawAppUniqueStrings(instance.CurrentNodeIDs, hubNodeID)
		instance.WorkflowNodeIDs = appendMaclawAppUniqueStrings(instance.WorkflowNodeIDs, hubNodeID)
	}
	if instance.ApprovalEngine == "" && hubInstanceID == "" {
		instance.ApprovalEngine = maclawAppApprovalEngineLocal
	}
	return normalizeMaclawAppApprovalInstanceFields(instance)
}

func markMaclawAppApprovalHubAttention(instance maclawAppApprovalInstance, hubErr error) maclawAppApprovalInstance {
	msg := strings.TrimSpace(fmt.Sprint(hubErr))
	if msg == "" {
		msg = "hub workflow operation failed"
	}
	instance.HubSyncError = msg
	instance.Status = "attention"
	instance.Lane = "attention"
	instance.BusinessStatus = firstNonEmptyMaclawAppString(instance.BusinessStatus, "hub_sync_failed")
	instance.ResultStatus = "attention"
	instance.Result = msg
	if instance.ResultPayload == nil {
		instance.ResultPayload = map[string]any{}
	} else {
		instance.ResultPayload = cloneMapAny(instance.ResultPayload)
	}
	instance.ResultPayload["hub_sync_error"] = msg
	instance.ResultPayload["approval_result"] = "attention"
	return normalizeMaclawAppApprovalInstanceFields(instance)
}

// triggerMaclawAppHubWorkflow starts a published Hub workflow instance.
// POST /api/v1/workflows/{id}/trigger
func (a *App) triggerMaclawAppHubWorkflow(workflowID string, triggerData map[string]any) (maclawAppHubTriggerResult, error) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return maclawAppHubTriggerResult{}, fmt.Errorf("hub_workflow_id is required")
	}
	hubURL, token, err := a.resolveMaclawAppWorkflowHubAuth()
	if err != nil {
		return maclawAppHubTriggerResult{}, err
	}
	body := map[string]any{}
	if triggerData != nil {
		body["trigger_data"] = triggerData
	}
	path := "/api/v1/workflows/" + url.PathEscape(workflowID) + "/trigger"
	raw, err := a.postHubJSON(hubURL, token, path, body)
	if err != nil {
		return maclawAppHubTriggerResult{}, err
	}
	return parseMaclawAppHubTriggerResponse(raw)
}

// submitMaclawAppHubApprovalDecision posts an approver decision to Hub.
// POST /api/v1/instances/{id}/nodes/{nodeID}/decision
// decision must be approve|reject|escalate (Hub wire format).
func (a *App) submitMaclawAppHubApprovalDecision(hubInstanceID, hubNodeID, decision, rationale string) (maclawAppHubDecisionResult, error) {
	hubInstanceID = strings.TrimSpace(hubInstanceID)
	hubNodeID = strings.TrimSpace(hubNodeID)
	decision = normalizeMaclawAppHubDecisionWire(decision)
	if hubInstanceID == "" || hubNodeID == "" {
		return maclawAppHubDecisionResult{}, fmt.Errorf("hub_instance_id and hub_node_id are required")
	}
	if decision == "" {
		return maclawAppHubDecisionResult{}, fmt.Errorf("decision must be approve, reject, or escalate")
	}
	hubURL, token, err := a.resolveMaclawAppWorkflowHubAuth()
	if err != nil {
		return maclawAppHubDecisionResult{}, err
	}
	path := "/api/v1/instances/" + url.PathEscape(hubInstanceID) + "/nodes/" + url.PathEscape(hubNodeID) + "/decision"
	body := map[string]any{
		"decision":  decision,
		"rationale": strings.TrimSpace(rationale),
	}
	raw, err := a.postHubJSON(hubURL, token, path, body)
	if err != nil {
		return maclawAppHubDecisionResult{}, err
	}
	return parseMaclawAppHubDecisionResponse(raw)
}

func normalizeMaclawAppHubDecisionWire(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved", "yes", "true", "1":
		return "approve"
	case "reject", "rejected", "deny", "denied", "no", "false", "0":
		return "reject"
	case "escalate", "escalated":
		return "escalate"
	default:
		return ""
	}
}

func parseMaclawAppHubTriggerResponse(raw []byte) (maclawAppHubTriggerResult, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return maclawAppHubTriggerResult{}, fmt.Errorf("invalid hub trigger response: %w", err)
	}
	instMap := anyMap(envelope["instance"])
	if instMap == nil {
		instMap = envelope
	}
	out := maclawAppHubTriggerResult{
		InstanceID:    firstNonEmptyMaclawAppString(maclawAppStringValue(instMap, "id", "instance_id", "instanceId"), maclawAppStringValue(envelope, "instance_id", "instanceId")),
		WorkflowID:    firstNonEmptyMaclawAppString(maclawAppStringValue(instMap, "workflow_id", "workflowId"), maclawAppStringValue(envelope, "workflow_id", "workflowId")),
		CurrentNodeID: firstNonEmptyMaclawAppString(maclawAppStringValue(instMap, "current_node_id", "currentNodeId", "current_node", "currentNode"), maclawAppStringValue(envelope, "current_node_id", "currentNodeId")),
		Status:        firstNonEmptyMaclawAppString(maclawAppStringValue(instMap, "status"), maclawAppStringValue(envelope, "status")),
		Raw:           envelope,
	}
	if out.InstanceID == "" {
		return maclawAppHubTriggerResult{}, fmt.Errorf("hub trigger response missing instance id")
	}
	return out, nil
}

func parseMaclawAppHubDecisionResponse(raw []byte) (maclawAppHubDecisionResult, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return maclawAppHubDecisionResult{}, fmt.Errorf("invalid hub decision response: %w", err)
	}
	return maclawAppHubDecisionResult{
		InstanceID: firstNonEmptyMaclawAppString(maclawAppStringValue(envelope, "instance_id", "instanceId", "id")),
		NodeID:     firstNonEmptyMaclawAppString(maclawAppStringValue(envelope, "node_id", "nodeId")),
		Status:     firstNonEmptyMaclawAppString(maclawAppStringValue(envelope, "status")),
		Raw:        envelope,
	}, nil
}

// bindOrTriggerMaclawAppHubWorkflow applies existing hub ids or triggers a new Hub instance.
// Soft-fails (attention) when Hub is configured enough to attempt but the call fails.
// When Hub credentials are missing and trigger was only auto, keeps engine=local without attention.
func (a *App) bindOrTriggerMaclawAppHubWorkflow(instance maclawAppApprovalInstance, input MaclawAppApprovalWorkflowStartInput) (maclawAppApprovalInstance, map[string]any, error) {
	hubWorkflowID := maclawAppHubWorkflowIDFromStart(input, instance)
	hubInstanceID := firstNonEmptyMaclawAppString(input.HubInstanceID, instance.HubInstanceID)
	hubNodeID := firstNonEmptyMaclawAppString(input.HubNodeID, instance.HubNodeID)

	// Explicit pre-bound instance: only apply fields.
	if hubInstanceID != "" && (input.TriggerHubWorkflow == nil || !*input.TriggerHubWorkflow) {
		bound := applyMaclawAppHubBinding(instance, hubWorkflowID, hubInstanceID, hubNodeID)
		return bound, map[string]any{
			"bound":           true,
			"triggered":       false,
			"hub_workflow_id": bound.HubWorkflowID,
			"hub_instance_id": bound.HubInstanceID,
			"hub_node_id":     bound.HubNodeID,
		}, nil
	}

	if !maclawAppShouldTriggerHubWorkflow(input, instance) {
		if normalizeMaclawAppApprovalEngine(instance.ApprovalEngine) == "" {
			instance.ApprovalEngine = maclawAppApprovalEngineLocal
		}
		return instance, map[string]any{"bound": false, "triggered": false, "skipped": true}, nil
	}

	// Credentials missing on auto path → local engine, no hard error.
	if _, _, authErr := a.resolveMaclawAppWorkflowHubAuth(); authErr != nil {
		if input.TriggerHubWorkflow != nil && *input.TriggerHubWorkflow {
			return markMaclawAppApprovalHubAttention(instance, authErr), map[string]any{
				"bound": false, "triggered": false, "error": authErr.Error(),
			}, authErr
		}
		instance.ApprovalEngine = maclawAppApprovalEngineLocal
		return instance, map[string]any{
			"bound": false, "triggered": false, "skipped": true, "reason": "hub_not_configured",
		}, nil
	}

	triggerData := map[string]any{
		"app_id":               instance.AppID,
		"app_name":             instance.AppName,
		"record_id":            instance.RecordID,
		"approval_id":          instance.ApprovalID,
		"approval_instance_id": instance.InstanceID,
		"applicant":            instance.Applicant,
		"owner":                instance.Owner,
		"approver":             instance.Approver,
		"dataset_id":           instance.DatasetID,
		"object_role":          instance.ObjectRole,
		"blueprint_id":         instance.BlueprintID,
		"business_entity":      instance.BusinessEntity,
		"business_action":      instance.BusinessAction,
		"business_note":        instance.BusinessNote,
		"form_data":            cloneMapAny(input.FormData),
		"business_payload":     cloneMapAny(input.BusinessPayload),
		"result_payload":       cloneMapAny(instance.ResultPayload),
	}
	// Drop empty nested maps to keep trigger payload compact.
	for _, key := range []string{"form_data", "business_payload", "result_payload"} {
		if m, ok := triggerData[key].(map[string]any); ok && len(m) == 0 {
			delete(triggerData, key)
		}
	}

	triggered, err := a.triggerMaclawAppHubWorkflow(hubWorkflowID, triggerData)
	if err != nil {
		failed := markMaclawAppApprovalHubAttention(instance, err)
		return failed, map[string]any{"bound": false, "triggered": false, "error": err.Error()}, err
	}
	bound := applyMaclawAppHubBinding(instance, firstNonEmptyMaclawAppString(triggered.WorkflowID, hubWorkflowID), triggered.InstanceID, triggered.CurrentNodeID)
	if bound.CurrentNodeStatus == "" {
		bound.CurrentNodeStatus = "pending"
	}
	return bound, map[string]any{
		"bound":           true,
		"triggered":       true,
		"hub_workflow_id": bound.HubWorkflowID,
		"hub_instance_id": bound.HubInstanceID,
		"hub_node_id":     bound.HubNodeID,
		"hub_status":      triggered.Status,
		"raw":             triggered.Raw,
	}, nil
}

// probeMaclawAppHubDirectoryReachable reports whether Hub directory APIs respond.
// Used by reconcile to avoid marking local projections missing during outages.
func (a *App) probeMaclawAppHubDirectoryReachable() bool {
	if a == nil {
		return false
	}
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		hubURL, token, err = a.resolveMaclawAppWorkflowHubAuth()
		if err != nil {
			return false
		}
	}
	if machineURL, machineToken, merr := a.getHubCredentials(); merr == nil && machineToken != "" {
		hubURL, token = machineURL, machineToken
	}
	_, err = a.getHubJSON(hubURL, token, "/api/v1/directory/pending-action?page=1&page_size=1")
	return err == nil
}

// listMaclawAppApprovalInstancesFromHub pulls Hub directory items (pending-action /
// initiated / completed) and maps them into the App approval instance projection.
// Soft-fails (empty list) when Hub is not configured or the call fails.
func (a *App) listMaclawAppApprovalInstancesFromHub(lane string, limit int) ([]maclawAppApprovalInstance, error) {
	if a == nil {
		return nil, nil
	}
	hubURL, token, err := a.resolveMaclawAppWorkflowHubAuth()
	if err != nil {
		// Prefer machine token for directory (approver identity = machine_id).
		hubURL, token, err = a.getHubCredentials()
		if err != nil {
			return nil, nil
		}
	}
	// Machine token is preferred so pending-action matches resolved approver machine IDs.
	if machineURL, machineToken, merr := a.getHubCredentials(); merr == nil && machineToken != "" {
		hubURL, token = machineURL, machineToken
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	paths := maclawAppHubDirectoryPathsForLane(lane)
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]maclawAppApprovalInstance, 0, limit)
	seen := map[string]struct{}{}
	for _, path := range paths {
		pageSize := limit
		if pageSize > 100 {
			pageSize = 100
		}
		fullPath := path + "?page=1&page_size=" + fmt.Sprintf("%d", pageSize)
		raw, err := a.getHubJSON(hubURL, token, fullPath)
		if err != nil {
			continue
		}
		items, err := parseMaclawAppHubDirectoryItems(raw)
		if err != nil {
			continue
		}
		for _, item := range items {
			inst := maclawAppApprovalInstanceFromHubDirectoryItem(item, lane)
			if inst.HubInstanceID == "" && inst.InstanceID == "" {
				continue
			}
			key := firstNonEmptyMaclawAppString(inst.HubInstanceID+"|"+inst.HubNodeID, inst.InstanceID)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, inst)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func maclawAppHubDirectoryPathsForLane(lane string) []string {
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	switch lane {
	case "pending_my_approval":
		return []string{"/api/v1/directory/pending-action"}
	case "my_requests":
		return []string{"/api/v1/directory/initiated"}
	case "handled":
		return []string{"/api/v1/directory/completed"}
	case "attention":
		// Hub has no dedicated attention lane; pending + initiated may include blocked later.
		return nil
	case "all", "":
		return []string{
			"/api/v1/directory/pending-action",
			"/api/v1/directory/initiated",
			"/api/v1/directory/completed",
		}
	default:
		return nil
	}
}

func parseMaclawAppHubDirectoryItems(raw []byte) ([]map[string]any, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	// { items: [...] } or bare array
	if items := anySlice(envelope["items"]); len(items) > 0 {
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m := anyMap(item); m != nil {
				out = append(out, m)
			}
		}
		return out, nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	return nil, nil
}

func maclawAppApprovalInstanceFromHubDirectoryItem(item map[string]any, requestedLane string) maclawAppApprovalInstance {
	hubInstanceID := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "instance_id", "instanceId", "id"),
	)
	hubNodeID := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "current_node", "currentNode", "node_id", "nodeId"),
	)
	workflowName := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "workflow_name", "workflowName", "title", "name"),
		"Hub Workflow",
	)
	initiator := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "initiator_name", "initiatorName", "requester_name", "requesterName"),
	)
	hubStatus := strings.ToLower(strings.TrimSpace(maclawAppStringValue(item, "status")))
	userRole := strings.ToLower(strings.TrimSpace(maclawAppStringValue(item, "user_role", "userRole")))
	urgency := maclawAppStringValue(item, "urgency")
	result := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "result"),
		urgency,
		hubStatus,
	)
	// Map Hub instance status → App projection status/lane.
	status := "pending"
	lane := "pending_my_approval"
	switch hubStatus {
	case "completed", "approved":
		status = "approved"
		lane = "handled"
	case "failed", "rejected":
		status = "rejected"
		if hubStatus == "failed" {
			status = "failed"
		}
		lane = "handled"
	case "blocked":
		status = "attention"
		lane = "attention"
		if result == "" || result == hubStatus {
			result = "blocked"
		}
	case "running", "pending", "":
		status = "pending"
		if userRole == "initiator" || requestedLane == "my_requests" {
			lane = "my_requests"
		} else {
			lane = "pending_my_approval"
		}
		// Timeout pressure from Hub directory urgency → ops attention lane.
		if strings.EqualFold(urgency, "overdue") {
			status = "attention"
			lane = "attention"
			if result == "" || result == hubStatus || result == urgency {
				result = "approval overdue"
			}
		}
	default:
		status = "pending"
		lane = firstNonEmptyMaclawAppString(normalizeMaclawAppApprovalLaneFilter(requestedLane), "pending_my_approval")
		if lane == "all" {
			lane = "pending_my_approval"
		}
	}
	if requestedLane == "my_requests" && status == "pending" {
		lane = "my_requests"
	}
	if requestedLane == "pending_my_approval" && status == "pending" {
		lane = "pending_my_approval"
	}
	createdAt := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "initiated_at", "initiatedAt", "created_at", "createdAt"),
	)
	updatedAt := firstNonEmptyMaclawAppString(
		maclawAppStringValue(item, "completed_at", "completedAt", "updated_at", "updatedAt"),
		createdAt,
	)
	localID := "hub-" + firstNonEmptyMaclawAppString(hubInstanceID, "unknown")
	if hubNodeID != "" {
		localID = localID + "-" + hubNodeID
	}
	payload := map[string]any{
		"source":          "hub_directory",
		"hub_status":      hubStatus,
		"user_role":       userRole,
		"urgency":         urgency,
		"hub_instance_id": hubInstanceID,
		"hub_node_id":     hubNodeID,
	}
	if tr := item["time_remaining_hours"]; tr != nil {
		payload["time_remaining_hours"] = tr
	}
	escPending := maclawAppBoolValue(item, "escalation_pending", "escalationPending")
	escApprovers := maclawAppStringSliceFromAny(item["escalation_approvers"])
	if len(escApprovers) == 0 {
		escApprovers = maclawAppStringSliceFromAny(item["escalationApprovers"])
	}
	if len(escApprovers) == 0 {
		if one := maclawAppStringValue(item, "escalation_approver", "escalationApprover"); one != "" {
			escApprovers = []string{one}
		}
	}
	if escPending || len(escApprovers) > 0 {
		payload["escalation_pending"] = true
		if len(escApprovers) > 0 {
			payload["escalation_approvers"] = escApprovers
		}
		// Surface as attention when still retrying delivery of offline peers.
		if status == "pending" {
			status = "attention"
			lane = "attention"
			if result == "" || result == hubStatus || result == urgency {
				result = "escalation pending"
			}
		}
	}
	return normalizeMaclawAppApprovalInstanceFields(maclawAppApprovalInstance{
		AppID:           "hub-workflow",
		AppName:         "Hub Workflow",
		Title:           workflowName,
		InstanceID:      localID,
		Status:          status,
		Lane:            lane,
		ApprovalEngine:  maclawAppApprovalEngineHub,
		HubInstanceID:   hubInstanceID,
		HubNodeID:       hubNodeID,
		CurrentNode:     hubNodeID,
		CurrentNodeIDs:  []string{hubNodeID},
		WorkflowNodeIDs: []string{hubNodeID},
		Owner:           initiator,
		Applicant:       initiator,
		Result:          result,
		BusinessStatus:  status,
		ResultStatus:    status,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
		ResultPayload:   payload,
	})
}

// decideMaclawAppApprovalOnHub advances the Hub workflow when the local projection is hub-bound.
// Returns (hubResult, usedHub, error). usedHub=false means caller should continue local-only.
func (a *App) decideMaclawAppApprovalOnHub(instance maclawAppApprovalInstance, decision, note string) (maclawAppHubDecisionResult, bool, error) {
	if !maclawAppApprovalUsesHubEngine(instance) {
		return maclawAppHubDecisionResult{}, false, nil
	}
	hubInstanceID := strings.TrimSpace(instance.HubInstanceID)
	hubNodeID := firstNonEmptyMaclawAppString(instance.HubNodeID, instance.CurrentNode)
	if hubInstanceID == "" || hubNodeID == "" {
		return maclawAppHubDecisionResult{}, true, fmt.Errorf("hub-bound approval instance is missing hub_instance_id or hub_node_id")
	}
	maclawAppApprovalTrace("hub_decision_submit", map[string]any{
		"app_id": instance.AppID, "instance_id": instance.InstanceID,
		"hub_instance_id": hubInstanceID, "hub_node_id": hubNodeID, "decision": decision,
	})
	result, err := a.submitMaclawAppHubApprovalDecision(hubInstanceID, hubNodeID, decision, note)
	if err != nil {
		maclawAppApprovalTrace("hub_decision_error", map[string]any{
			"app_id": instance.AppID, "instance_id": instance.InstanceID,
			"hub_instance_id": hubInstanceID, "hub_node_id": hubNodeID, "error": err.Error(),
		})
		return maclawAppHubDecisionResult{}, true, err
	}
	maclawAppApprovalTrace("hub_decision_ok", map[string]any{
		"app_id": instance.AppID, "instance_id": instance.InstanceID,
		"hub_instance_id": hubInstanceID, "hub_node_id": hubNodeID,
		"hub_status": result.Status, "decision": decision,
	})
	return result, true, nil
}

// maclawAppApprovalReconcileResult summarizes a Hub ↔ local projection sync pass.
type maclawAppApprovalReconcileResult struct {
	Checked   int      `json:"checked"`
	Updated   int      `json:"updated"`
	Unchanged int      `json:"unchanged"`
	Missing   int      `json:"missing"`
	Upserted  int      `json:"upserted"`
	Errors    []string `json:"errors,omitempty"`
	HubItems  int      `json:"hub_items"`
}

// ReconcileMaclawAppApprovalProjections aligns local hub-bound approval snapshots
// with Hub directory SoT (pending-action / initiated / completed). Soft-fails when
// Hub is offline. Safe to call on every ops-panel refresh.
func (a *App) ReconcileMaclawAppApprovalProjections() (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	result := a.reconcileMaclawAppApprovalProjections(100)
	maclawAppApprovalTrace("reconcile_done", map[string]any{
		"checked": result.Checked, "updated": result.Updated, "missing": result.Missing,
		"upserted": result.Upserted, "hub_items": result.HubItems, "errors": len(result.Errors),
	})
	return map[string]any{
		"checked":   result.Checked,
		"updated":   result.Updated,
		"unchanged": result.Unchanged,
		"missing":   result.Missing,
		"upserted":  result.Upserted,
		"hub_items": result.HubItems,
		"errors":    result.Errors,
	}, nil
}

func (a *App) reconcileMaclawAppApprovalProjections(limit int) maclawAppApprovalReconcileResult {
	out := maclawAppApprovalReconcileResult{}
	if a == nil {
		return out
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	// Require Hub credentials; otherwise skip silently (local-only mode).
	if _, _, err := a.getHubCredentials(); err != nil {
		if _, _, err2 := a.resolveMaclawAppWorkflowHubAuth(); err2 != nil {
			return out
		}
	}
	// Detect total Hub outage before mutating local state.
	if !a.probeMaclawAppHubDirectoryReachable() {
		out.Errors = append(out.Errors, "hub directory unreachable; skip reconcile mutations")
		maclawAppApprovalTrace("reconcile_skipped", map[string]any{"error": "hub_unreachable"})
		return out
	}
	hubPending, _ := a.listMaclawAppApprovalInstancesFromHub("pending_my_approval", limit)
	hubMine, _ := a.listMaclawAppApprovalInstancesFromHub("my_requests", limit)
	hubHandled, _ := a.listMaclawAppApprovalInstancesFromHub("handled", limit)
	hubAll := append(append([]maclawAppApprovalInstance{}, hubPending...), hubMine...)
	hubAll = append(hubAll, hubHandled...)
	out.HubItems = len(hubAll)

	hubByKey := map[string]maclawAppApprovalInstance{}
	for _, item := range hubAll {
		for _, key := range maclawAppHubProjectionKeys(item) {
			hubByKey[key] = item
		}
	}

	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
		return out
	}
	now := time.Now().UTC().Format(time.RFC3339)
	changed := false

	// 1) Align local hub-bound open items with Hub state.
	for i := range registry.Instances {
		local := registry.Instances[i]
		if !maclawAppApprovalUsesHubEngine(local) && strings.TrimSpace(local.HubInstanceID) == "" {
			continue
		}
		status := normalizeMaclawAppApprovalStatus(local.Status)
		if status != "pending" && status != "attention" && status != "requires_input" && status != "draft" {
			continue
		}
		out.Checked++
		hub, ok := lookupMaclawAppHubProjection(hubByKey, local)
		if !ok {
			// Not visible in Hub directory while Hub is reachable.
			if status == "pending" {
				updated := cloneMaclawAppApprovalInstance(local)
				updated.Status = "attention"
				updated.Lane = "attention"
				updated.HubSyncError = "hub instance not found in directory (missing or not visible)"
				updated.UpdatedAt = now
				updated.Result = updated.HubSyncError
				if updated.ResultPayload == nil {
					updated.ResultPayload = map[string]any{}
				} else {
					updated.ResultPayload = cloneMapAny(updated.ResultPayload)
				}
				updated.ResultPayload["reconcile"] = "missing_on_hub"
				updated.Events = append(updated.Events, maclawAppApprovalEvent{
					At: now, Node: firstNonEmptyMaclawAppString(updated.HubNodeID, updated.CurrentNode),
					Actor: "reconcile", Decision: "attention", Message: updated.HubSyncError,
				})
				registry.Instances[i] = normalizeMaclawAppApprovalInstanceFields(updated)
				out.Missing++
				out.Updated++
				changed = true
				maclawAppApprovalTrace("reconcile_missing", map[string]any{
					"app_id": local.AppID, "instance_id": local.InstanceID,
					"hub_instance_id": local.HubInstanceID, "hub_node_id": local.HubNodeID,
				})
			} else {
				out.Unchanged++
			}
			continue
		}
		// Hub has a view of this instance — project status/lane/node forward when final or node moved.
		updated := mergeMaclawAppApprovalInstance(local, hub)
		// Prefer Hub final states.
		hubStatus := normalizeMaclawAppApprovalStatus(hub.Status)
		hubLane := normalizeMaclawAppApprovalLane(hub.Lane)
		needsWrite := false
		if hubStatus == "approved" || hubStatus == "rejected" || hubStatus == "failed" || hubStatus == "cancelled" || hubStatus == "timeout" {
			if status != hubStatus || normalizeMaclawAppApprovalLane(local.Lane) != "handled" {
				updated.Status = hubStatus
				updated.Lane = "handled"
				updated.BusinessStatus = hubStatus
				updated.ResultStatus = hubStatus
				updated.HubSyncError = ""
				needsWrite = true
			}
		} else if hubStatus == "attention" && status != "attention" {
			updated.Status = "attention"
			updated.Lane = "attention"
			if hub.Result != "" {
				updated.Result = hub.Result
			}
			needsWrite = true
		} else if urgency, _ := hub.ResultPayload["urgency"].(string); strings.EqualFold(urgency, "overdue") && status == "pending" {
			updated.Status = "attention"
			updated.Lane = "attention"
			updated.Result = firstNonEmptyMaclawAppString(hub.Result, "approval overdue")
			needsWrite = true
		} else if hub.HubNodeID != "" && hub.HubNodeID != local.HubNodeID {
			updated.HubNodeID = hub.HubNodeID
			updated.CurrentNode = hub.HubNodeID
			needsWrite = true
		} else if hubLane != "" && hubLane != normalizeMaclawAppApprovalLane(local.Lane) && status == "pending" {
			updated.Lane = hubLane
			needsWrite = true
		}
		// Refresh escalation markers from Hub so ops UI shows 升级重投 without waiting for push.
		if maclawAppApprovalResultPayloadEscalationChanged(local.ResultPayload, hub.ResultPayload) {
			needsWrite = true
		}
		if needsWrite {
			updated.UpdatedAt = now
			if updated.ResultPayload == nil {
				updated.ResultPayload = map[string]any{}
			} else {
				updated.ResultPayload = cloneMapAny(updated.ResultPayload)
			}
			updated.ResultPayload["reconcile"] = "aligned_with_hub"
			updated.ResultPayload["hub_status"] = hub.Status
			mergeMaclawAppApprovalEscalationPayload(updated.ResultPayload, hub.ResultPayload)
			if pending, _ := updated.ResultPayload["escalation_pending"].(bool); pending && status == "pending" {
				updated.Status = "attention"
				updated.Lane = "attention"
				updated.Result = firstNonEmptyMaclawAppString(hub.Result, updated.Result, "escalation pending")
			}
			registry.Instances[i] = normalizeMaclawAppApprovalInstanceFields(updated)
			out.Updated++
			changed = true
			maclawAppApprovalTrace("reconcile_updated", map[string]any{
				"app_id": local.AppID, "instance_id": local.InstanceID,
				"hub_instance_id": local.HubInstanceID, "from_status": status, "to_status": updated.Status,
			})
		} else {
			out.Unchanged++
		}
	}

	// 2) Upsert Hub-only open items so ops panel has durable offline cache.
	// Include initiator-side directory rows (my_requests) that carry attention /
	// escalation pressure — not only approver pending-action.
	upsertCandidates := append([]maclawAppApprovalInstance{}, hubPending...)
	for _, hub := range hubMine {
		st := normalizeMaclawAppApprovalStatus(hub.Status)
		if st == "pending" || st == "attention" || st == "requires_input" || st == "draft" {
			upsertCandidates = append(upsertCandidates, hub)
			continue
		}
		// Hub may still list running initiated items with escalation markers.
		if pending, _ := maclawAppApprovalEscalationFromPayload(hub.ResultPayload); pending {
			upsertCandidates = append(upsertCandidates, hub)
		}
	}
	seenUpsert := map[string]struct{}{}
	for _, hub := range upsertCandidates {
		if strings.TrimSpace(hub.HubInstanceID) == "" {
			continue
		}
		key := strings.TrimSpace(hub.HubInstanceID) + "|" + strings.TrimSpace(hub.HubNodeID)
		if _, dup := seenUpsert[key]; dup {
			continue
		}
		seenUpsert[key] = struct{}{}
		if _, ok := lookupLocalMaclawAppByHub(registry.Instances, hub); ok {
			continue
		}
		hub.UpdatedAt = now
		if hub.CreatedAt == "" {
			hub.CreatedAt = now
		}
		_ = registry.upsert(hub)
		out.Upserted++
		changed = true
		maclawAppApprovalTrace("reconcile_upsert", map[string]any{
			"app_id": hub.AppID, "instance_id": hub.InstanceID,
			"hub_instance_id": hub.HubInstanceID, "hub_node_id": hub.HubNodeID,
		})
	}

	if changed {
		registry.UpdatedAt = now
		if err := a.writeMaclawAppApprovalRegistry(registry); err != nil {
			out.Errors = append(out.Errors, err.Error())
		}
	}
	return out
}

func maclawAppHubProjectionKeys(instance maclawAppApprovalInstance) []string {
	keys := []string{}
	if id := strings.TrimSpace(instance.HubInstanceID); id != "" {
		keys = append(keys, "hub:"+id)
		if node := strings.TrimSpace(instance.HubNodeID); node != "" {
			keys = append(keys, "hub_node:"+id+"|"+node)
		}
	}
	return keys
}

func lookupMaclawAppHubProjection(hubByKey map[string]maclawAppApprovalInstance, local maclawAppApprovalInstance) (maclawAppApprovalInstance, bool) {
	for _, key := range maclawAppHubProjectionKeys(local) {
		if item, ok := hubByKey[key]; ok {
			return item, true
		}
	}
	// Fallback: match by hub instance only when node differs after advance.
	if id := strings.TrimSpace(local.HubInstanceID); id != "" {
		if item, ok := hubByKey["hub:"+id]; ok {
			return item, true
		}
	}
	return maclawAppApprovalInstance{}, false
}

func lookupLocalMaclawAppByHub(locals []maclawAppApprovalInstance, hub maclawAppApprovalInstance) (maclawAppApprovalInstance, bool) {
	hubID := strings.TrimSpace(hub.HubInstanceID)
	if hubID == "" {
		return maclawAppApprovalInstance{}, false
	}
	for _, local := range locals {
		if strings.TrimSpace(local.HubInstanceID) == hubID {
			return local, true
		}
	}
	return maclawAppApprovalInstance{}, false
}

// applyHubWorkflowStatusAttention updates or creates a local projection when Hub
// pushes a blocked/escalation status for a workflow instance.
// urgency is optional (overdue|critical|attention). extras may carry
// escalation_approvers / escalation_pending for ops UI.
func (a *App) applyHubWorkflowStatusAttention(hubInstanceID, hubNodeID, workflowName, message string, urgency string, extras map[string]any) error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	hubInstanceID = strings.TrimSpace(hubInstanceID)
	if hubInstanceID == "" {
		return fmt.Errorf("hub_instance_id is required")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "workflow blocked"
	}
	urgencyVal := strings.ToLower(strings.TrimSpace(urgency))
	if urgencyVal == "" {
		// Derive from message when Hub payload omits urgency.
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "timeout"), strings.Contains(lower, "overdue"), strings.Contains(lower, "escalat"):
			urgencyVal = "overdue"
		case strings.Contains(lower, "unavailable"), strings.Contains(lower, "no fallback"):
			urgencyVal = "critical"
		default:
			urgencyVal = "attention"
		}
	}
	var escApprovers []string
	escPending := false
	if extras != nil {
		escApprovers = maclawAppStringSliceFromAny(extras["escalation_approvers"])
		if len(escApprovers) == 0 {
			if one := strings.TrimSpace(fmt.Sprint(extras["escalation_approver"])); one != "" && one != "<nil>" {
				escApprovers = []string{one}
			}
		}
		if v, ok := extras["escalation_pending"].(bool); ok {
			escPending = v
		}
		if len(escApprovers) > 0 {
			escPending = true
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return err
	}
	applyPayload := func(payload map[string]any) map[string]any {
		if payload == nil {
			payload = map[string]any{}
		} else {
			payload = cloneMapAny(payload)
		}
		payload["hub_push"] = "blocked"
		payload["blocked_message"] = message
		payload["urgency"] = urgencyVal
		if escPending {
			payload["escalation_pending"] = true
		}
		if len(escApprovers) > 0 {
			payload["escalation_approvers"] = escApprovers
		}
		return payload
	}
	updated := false
	for i := range registry.Instances {
		local := registry.Instances[i]
		if strings.TrimSpace(local.HubInstanceID) != hubInstanceID {
			continue
		}
		local.Status = "attention"
		local.Lane = "attention"
		local.HubSyncError = message
		local.Result = message
		local.UpdatedAt = now
		if hubNodeID != "" {
			local.HubNodeID = hubNodeID
			local.CurrentNode = hubNodeID
		}
		local.ResultPayload = applyPayload(local.ResultPayload)
		local.Events = append(local.Events, maclawAppApprovalEvent{
			At: now, Node: firstNonEmptyMaclawAppString(hubNodeID, local.CurrentNode),
			Actor: "hub", Decision: "attention", Message: message,
		})
		registry.Instances[i] = normalizeMaclawAppApprovalInstanceFields(local)
		updated = true
	}
	if !updated {
		payload := applyPayload(map[string]any{"source": "ve:workflow_status"})
		inst := maclawAppApprovalInstance{
			AppID: "hub-workflow", AppName: "Hub Workflow", Title: firstNonEmptyMaclawAppString(workflowName, "Hub Workflow"),
			InstanceID: "hub-" + hubInstanceID + "-blocked", Status: "attention", Lane: "attention",
			ApprovalEngine: maclawAppApprovalEngineHub, HubInstanceID: hubInstanceID, HubNodeID: hubNodeID,
			CurrentNode: hubNodeID, Result: message, HubSyncError: message,
			CreatedAt: now, UpdatedAt: now,
			ResultPayload: payload,
			Events:       []maclawAppApprovalEvent{{At: now, Node: hubNodeID, Actor: "hub", Decision: "attention", Message: message}},
		}
		registry.upsert(normalizeMaclawAppApprovalInstanceFields(inst))
	}
	registry.UpdatedAt = now
	if err := a.writeMaclawAppApprovalRegistry(registry); err != nil {
		return err
	}
	if a.ctx != nil {
		ev := map[string]any{
			"hub_instance_id": hubInstanceID, "hub_node_id": hubNodeID, "message": message, "urgency": urgencyVal,
		}
		if len(escApprovers) > 0 {
			ev["escalation_approvers"] = escApprovers
		}
		a.emitEvent("ve:approval-attention", ev)
	}
	return nil
}

// mergeMaclawAppApprovalEscalationPayload copies escalation_* markers from hub
// into local ResultPayload (or clears them when Hub no longer reports pending).
func mergeMaclawAppApprovalEscalationPayload(dst, hub map[string]any) {
	if dst == nil {
		return
	}
	if hub == nil {
		delete(dst, "escalation_pending")
		delete(dst, "escalation_approvers")
		delete(dst, "escalation_approver")
		return
	}
	approvers := maclawAppStringSliceFromAny(hub["escalation_approvers"])
	if len(approvers) == 0 {
		if one := strings.TrimSpace(fmt.Sprint(hub["escalation_approver"])); one != "" && one != "<nil>" {
			approvers = []string{one}
		}
	}
	pending, _ := hub["escalation_pending"].(bool)
	if !pending && len(approvers) == 0 {
		delete(dst, "escalation_pending")
		delete(dst, "escalation_approvers")
		delete(dst, "escalation_approver")
		return
	}
	dst["escalation_pending"] = true
	if len(approvers) > 0 {
		dst["escalation_approvers"] = approvers
		dst["escalation_approver"] = approvers[len(approvers)-1]
	}
}

// maclawAppApprovalResultPayloadEscalationChanged reports whether Hub escalation
// markers differ from the local projection (pending flag or approver set).
func maclawAppApprovalResultPayloadEscalationChanged(local, hub map[string]any) bool {
	localPending, localApprovers := maclawAppApprovalEscalationFromPayload(local)
	hubPending, hubApprovers := maclawAppApprovalEscalationFromPayload(hub)
	if localPending != hubPending {
		return true
	}
	if len(localApprovers) != len(hubApprovers) {
		return true
	}
	// Order-independent set compare (Hub/list order is not stable).
	seen := map[string]struct{}{}
	for _, a := range localApprovers {
		seen[strings.TrimSpace(a)] = struct{}{}
	}
	for _, a := range hubApprovers {
		if _, ok := seen[strings.TrimSpace(a)]; !ok {
			return true
		}
	}
	return false
}

func maclawAppApprovalEscalationFromPayload(payload map[string]any) (pending bool, approvers []string) {
	if payload == nil {
		return false, nil
	}
	pending, _ = payload["escalation_pending"].(bool)
	approvers = maclawAppStringSliceFromAny(payload["escalation_approvers"])
	if len(approvers) == 0 {
		if one := strings.TrimSpace(fmt.Sprint(payload["escalation_approver"])); one != "" && one != "<nil>" {
			approvers = []string{one}
		}
	}
	if len(approvers) > 0 {
		pending = true
	}
	return pending, approvers
}

// maclawAppApprovalTrace emits a structured one-line log for approval E2E correlation.
// Fields are intentionally stable: app_id, instance_id, hub_instance_id, hub_node_id, request_id, decision.
func maclawAppApprovalTrace(event string, fields map[string]any) {
	event = strings.TrimSpace(event)
	if event == "" {
		event = "event"
	}
	parts := make([]string, 0, len(fields)+1)
	parts = append(parts, "event="+event)
	// Stable order for grepping.
	for _, key := range []string{
		"app_id", "instance_id", "hub_instance_id", "hub_node_id", "hub_workflow_id",
		"request_id", "decision", "from_status", "to_status", "hub_status", "error",
		"checked", "updated", "missing", "upserted", "hub_items", "errors",
	} {
		if v, ok := fields[key]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				parts = append(parts, key+"="+s)
			}
		}
	}
	log.Printf("[maclaw-approval] %s", strings.Join(parts, " "))
}
