package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

// RecordMaclawAppApprovalInstance persists one approval instance snapshot for a
// MaClaw approval app. It is the GUI-facing approval instance cache; DataSrv or
// workflow sync can later write the same shape.
func (a *App) RecordMaclawAppApprovalInstance(instance maclawAppApprovalInstance) (maclawAppApprovalInstance, error) {
	instance = normalizeMaclawAppApprovalInstanceFields(instance)
	if instance.AppID == "" {
		return maclawAppApprovalInstance{}, fmt.Errorf("app_id is required")
	}
	if instance.InstanceID == "" {
		instance.InstanceID = "appr-" + firstMaclawAppID([]string{instance.AppID}) + "-" + shortRandomHex()
	}
	if instance.Title == "" {
		instance.Title = instance.AppID
	}
	instance.Lane = normalizeMaclawAppApprovalLane(instance.Lane)
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	if instance.CurrentNode == "" {
		instance.CurrentNode = "submit"
	}
	var err error
	instance, err = a.applyMaclawAppApprovalRuntimeContract(instance)
	if err != nil {
		return maclawAppApprovalInstance{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if instance.CreatedAt == "" {
		instance.CreatedAt = now
	}
	if instance.UpdatedAt == "" {
		instance.UpdatedAt = now
	}
	if len(instance.Events) == 0 {
		instance.Events = []maclawAppApprovalEvent{{At: instance.UpdatedAt, Node: instance.CurrentNode, Actor: firstNonEmptyMaclawAppString(instance.Owner, instance.Applicant), Decision: instance.Status, Message: instance.Result}}
	}
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return maclawAppApprovalInstance{}, err
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.approvals.v1"
	}
	stored := registry.upsert(instance)
	registry.UpdatedAt = now
	if err := a.writeMaclawAppApprovalRegistry(registry); err != nil {
		return maclawAppApprovalInstance{}, err
	}
	return cloneMaclawAppApprovalInstance(stored), nil
}

func (a *App) StartMaclawAppApprovalWorkflow(input MaclawAppApprovalWorkflowStartInput) (map[string]any, error) {
	input.AppID = strings.TrimSpace(input.AppID)
	input.RecordID = strings.TrimSpace(input.RecordID)
	if input.AppID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	if input.RecordID == "" {
		return nil, fmt.Errorf("record_id is required")
	}
	install, err := a.findMaclawAppInstallRecord(input.AppID)
	if err != nil {
		return nil, err
	}
	if install == nil {
		return nil, fmt.Errorf("installed MaClaw App %s was not found", input.AppID)
	}
	if strings.TrimSpace(install.Kind) != "" && !strings.EqualFold(strings.TrimSpace(install.Kind), "enterprise_approval_app") {
		return nil, fmt.Errorf("installed MaClaw App %s is not an enterprise approval app", input.AppID)
	}
	currentNode := firstNonEmptyMaclawAppString(input.CurrentNode, maclawAppWorkflowMappingNodeFromInstall(install, "approvalNode", "approval_node"), maclawAppWorkflowMappingNodeFromInstall(install, "submitNode", "submit_node"), "submit")
	resultPayload := cloneMapAny(input.ResultPayload)
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	if input.FormData != nil {
		resultPayload["form_data"] = cloneMapAny(input.FormData)
	}
	if input.BusinessPayload != nil {
		resultPayload["business_payload"] = cloneMapAny(input.BusinessPayload)
	}
	resumeInstanceID := strings.TrimSpace(firstNonEmptyMaclawAppString(input.ContinueFromID, input.InstanceID))
	resumeApprovalID := strings.TrimSpace(input.ApprovalID)
	var resumeBase *maclawAppApprovalInstance
	if resumeInstanceID != "" || resumeApprovalID != "" {
		found, err := a.findMaclawAppApprovalInstanceForContinue(input.AppID, resumeInstanceID, resumeApprovalID, input.RecordID)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, fmt.Errorf("approval instance to continue was not found")
		}
		resumeBase = found
		if resumeApprovalID == "" {
			resumeApprovalID = firstNonEmptyMaclawAppString(found.ApprovalID, found.RecordApprovalID)
		}
		if resumeInstanceID == "" {
			resumeInstanceID = found.InstanceID
		}
		resultPayload["supplemental_input"] = map[string]any{"form_data": cloneMapAny(input.FormData), "business_payload": cloneMapAny(input.BusinessPayload)}
	}
	if _, ok := resultPayload["business_record"]; !ok {
		resultPayload["business_record"] = map[string]any{"id": input.RecordID}
	}
	if _, ok := resultPayload["text"]; !ok {
		resultPayload["text"] = firstNonEmptyMaclawAppString(input.BusinessNote, "workflow submitted")
	}
	instance := maclawAppApprovalInstance{
		AppID:               input.AppID,
		AppName:             firstNonEmptyMaclawAppString(input.AppName, install.AppName),
		BlueprintID:         strings.TrimSpace(input.BlueprintID),
		DatasetID:           strings.TrimSpace(input.DatasetID),
		ObjectRole:          strings.TrimSpace(input.ObjectRole),
		ApprovalObjectRole:  strings.TrimSpace(input.ObjectRole),
		ApprovalEvent:       strings.TrimSpace(input.ApprovalEvent),
		Title:               firstNonEmptyMaclawAppString(input.Title, install.AppName, input.AppID),
		Lane:                "my_requests",
		Status:              "pending",
		CurrentNode:         currentNode,
		CurrentNodeIDs:      append([]string(nil), firstNonEmptyMaclawAppStringList(input.CurrentNodeIDs, input.WorkflowNodeIDs)...),
		WorkflowNodeIDs:     append([]string(nil), firstNonEmptyMaclawAppStringList(input.WorkflowNodeIDs, input.CurrentNodeIDs)...),
		Owner:               firstNonEmptyMaclawAppString(input.Owner, input.Applicant),
		Applicant:           firstNonEmptyMaclawAppString(input.Applicant, input.Owner),
		Approver:            strings.TrimSpace(input.Approver),
		CurrentAssignee:     firstNonEmptyMaclawAppString(input.CurrentAssignee, input.Approver),
		CurrentAssigneeType: firstNonEmptyMaclawAppString(input.CurrentAssigneeType, "user"),
		WorkflowSkillID:     strings.TrimSpace(input.WorkflowSkillID),
		WorkflowVersion:     strings.TrimSpace(input.WorkflowVersion),
		BusinessStatus:      firstNonEmptyMaclawAppString(input.BusinessStatus, "pending"),
		ResultStatus:        firstNonEmptyMaclawAppString(input.ResultStatus, "pending"),
		FromStatus:          strings.TrimSpace(input.FromStatus),
		ToStatus:            firstNonEmptyMaclawAppString(input.ToStatus, input.BusinessStatus, "pending"),
		RecordID:            input.RecordID,
		BusinessEntity:      strings.TrimSpace(input.BusinessEntity),
		BusinessAction:      strings.TrimSpace(input.BusinessAction),
		BusinessNote:        strings.TrimSpace(input.BusinessNote),
		Result:              firstNonEmptyMaclawAppString(input.BusinessNote, "workflow submitted"),
		ResultPayload:       resultPayload,
	}
	if resumeBase != nil {
		previous := cloneMaclawAppApprovalInstance(*resumeBase)
		instance.InstanceID = firstNonEmptyMaclawAppString(resumeInstanceID, previous.InstanceID)
		instance.ApprovalID = firstNonEmptyMaclawAppString(resumeApprovalID, previous.ApprovalID, previous.RecordApprovalID)
		instance.RecordApprovalID = instance.ApprovalID
		instance.AppName = firstNonEmptyMaclawAppString(input.AppName, previous.AppName, install.AppName)
		instance.BlueprintID = firstNonEmptyMaclawAppString(input.BlueprintID, previous.BlueprintID)
		instance.DatasetID = firstNonEmptyMaclawAppString(input.DatasetID, previous.DatasetID)
		instance.ObjectRole = firstNonEmptyMaclawAppString(input.ObjectRole, previous.ObjectRole, previous.ApprovalObjectRole)
		instance.ApprovalObjectRole = instance.ObjectRole
		instance.ApprovalEvent = firstNonEmptyMaclawAppString(input.ApprovalEvent, previous.ApprovalEvent)
		instance.Title = firstNonEmptyMaclawAppString(input.Title, previous.Title, install.AppName, input.AppID)
		instance.Owner = firstNonEmptyMaclawAppString(input.Owner, previous.Owner, input.Applicant, previous.Applicant)
		instance.Applicant = firstNonEmptyMaclawAppString(input.Applicant, previous.Applicant, instance.Owner)
		instance.Approver = firstNonEmptyMaclawAppString(input.Approver, previous.Approver)
		instance.CurrentAssignee = firstNonEmptyMaclawAppString(input.CurrentAssignee, input.Approver, previous.Approver, previous.CurrentAssignee)
		instance.WorkflowSkillID = firstNonEmptyMaclawAppString(input.WorkflowSkillID, previous.WorkflowSkillID)
		instance.WorkflowVersion = firstNonEmptyMaclawAppString(input.WorkflowVersion, previous.WorkflowVersion)
		instance.FromStatus = firstNonEmptyMaclawAppString(input.FromStatus, previous.Status, previous.BusinessStatus)
		instance.BusinessStatus = firstNonEmptyMaclawAppString(input.BusinessStatus, "supplemented")
		instance.ResultStatus = firstNonEmptyMaclawAppString(input.ResultStatus, "pending")
		instance.ToStatus = firstNonEmptyMaclawAppString(input.ToStatus, instance.BusinessStatus)
		instance.Result = firstNonEmptyMaclawAppString(input.BusinessNote, "supplemental input submitted")
		instance.Lane = "pending_my_approval"
	}
	if len(instance.CurrentNodeIDs) == 0 && currentNode != "" {
		instance.CurrentNodeIDs = []string{currentNode}
	}
	if len(instance.WorkflowNodeIDs) == 0 {
		instance.WorkflowNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	}
	stored, err := a.RecordMaclawAppApprovalInstance(instance)
	if err != nil {
		return nil, err
	}
	syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: stored.DatasetID, ObjectRole: stored.ObjectRole, AppID: stored.AppID, BlueprintID: stored.BlueprintID, RecordID: stored.RecordID, Instance: stored})
	if err != nil {
		return nil, err
	}
	if approvalID := firstNonEmptyMaclawAppString(stringMapValue(syncResult, "approval_id"), stringMapValue(syncResult, "record_approval_id")); approvalID != "" {
		stored.ApprovalID = approvalID
		stored.RecordApprovalID = approvalID
		stored, _ = a.RecordMaclawAppApprovalInstance(stored)
	}
	result := map[string]any{"started": true, "instance": stored, "sync": syncResult, "workflow_skill_id": stored.WorkflowSkillID, "workflow_version": stored.WorkflowVersion, "approval_id": stored.ApprovalID, "result_feedback": maclawAppApprovalResultFeedback(stored)}
	if input.RunWorkflowSkill {
		workflowRun, err := a.runMaclawAppApprovalWorkflowSkill(stored, input)
		if err != nil {
			return nil, err
		}
		result["workflow_run"] = workflowRun
		if instance, ok := workflowRun["instance"].(maclawAppApprovalInstance); ok {
			result["instance"] = instance
			result["approval_id"] = instance.ApprovalID
			result["result_feedback"] = maclawAppApprovalResultFeedback(instance)
		}
	}
	return result, nil
}

func (a *App) runMaclawAppApprovalWorkflowSkill(base maclawAppApprovalInstance, input MaclawAppApprovalWorkflowStartInput) (map[string]any, error) {
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("workflow skill runner is not initialized")
	}
	workflowSkillID := strings.TrimSpace(base.WorkflowSkillID)
	if workflowSkillID == "" {
		return nil, fmt.Errorf("workflow_skill_id is required to run approval workflow skill")
	}
	runArgs := cloneMapAny(input.WorkflowRunArgs)
	if runArgs == nil {
		runArgs = map[string]any{}
	}
	runArgs["app_id"] = base.AppID
	runArgs["app_name"] = base.AppName
	runArgs["dataset_id"] = base.DatasetID
	runArgs["object_role"] = base.ObjectRole
	runArgs["blueprint_id"] = base.BlueprintID
	runArgs["record_id"] = base.RecordID
	runArgs["approval_id"] = base.ApprovalID
	runArgs["approval_instance_id"] = base.InstanceID
	runArgs["workflow_skill_id"] = base.WorkflowSkillID
	runArgs["workflow_version"] = base.WorkflowVersion
	runArgs["current_node"] = base.CurrentNode
	runArgs["current_node_ids"] = append([]string(nil), base.CurrentNodeIDs...)
	runArgs["workflow_node_ids"] = append([]string(nil), firstNonEmptyMaclawAppStringList(base.WorkflowNodeIDs, base.CurrentNodeIDs)...)
	runArgs["applicant"] = base.Applicant
	runArgs["approver"] = base.Approver
	runArgs["business_status"] = base.BusinessStatus
	runArgs["result_status"] = base.ResultStatus
	runArgs["business_payload"] = cloneMapAny(input.BusinessPayload)
	runArgs["form_data"] = cloneMapAny(input.FormData)
	runArgs["result_payload"] = cloneMapAny(base.ResultPayload)
	runArgs["instance"] = cloneMaclawAppApprovalInstance(base)
	runArgs["_skill_owner_id"] = "maclaw_app:" + base.AppID
	execResult := a.skillExecutor.executeSkillByNameDetailed(workflowSkillID, runArgs)
	if execResult.Err != nil {
		return a.maclawAppFailedWorkflowRun(base, workflowSkillID, execResult.Output, execResult.Captured, execResult.Err)
	}
	payload, err := maclawAppWorkflowSkillPayloadFromOutput(execResult.Output, execResult.Captured)
	if err != nil {
		return a.maclawAppFailedWorkflowRun(base, workflowSkillID, execResult.Output, execResult.Captured, err)
	}
	progressInstances := maclawAppApprovalProgressInstancesFromWorkflowPayload(payload, base)
	progressSyncs := make([]map[string]any, 0, len(progressInstances))
	for _, progress := range progressInstances {
		syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: progress.DatasetID, ObjectRole: progress.ObjectRole, AppID: progress.AppID, BlueprintID: progress.BlueprintID, RecordID: progress.RecordID, ApprovalID: firstNonEmptyMaclawAppString(progress.ApprovalID, base.ApprovalID), Instance: progress})
		if err != nil {
			return nil, err
		}
		progressSyncs = append(progressSyncs, syncResult)
	}
	instance := maclawAppApprovalInstanceFromWorkflowPayload(payload, base)
	syncResult, err := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: instance.DatasetID, ObjectRole: instance.ObjectRole, AppID: instance.AppID, BlueprintID: instance.BlueprintID, RecordID: instance.RecordID, ApprovalID: firstNonEmptyMaclawAppString(instance.ApprovalID, base.ApprovalID), Instance: instance})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ran": true, "workflow_skill_id": workflowSkillID, "output": execResult.Output, "captured": execResult.Captured, "payload": payload, "progress_instances": progressInstances, "progress_sync": progressSyncs, "instance": instance, "sync": syncResult, "result_feedback": maclawAppApprovalResultFeedback(instance)}, nil
}

func (a *App) maclawAppFailedWorkflowRun(base maclawAppApprovalInstance, workflowSkillID, output string, captured map[string]string, runErr error) (map[string]any, error) {
	message := strings.TrimSpace(fmt.Sprint(runErr))
	if message == "" {
		message = "workflow skill failed"
	}
	failed := cloneMaclawAppApprovalInstance(base)
	failed.Status = "failed"
	failed.Lane = "handled"
	failed.BusinessStatus = "workflow_failed"
	failed.ResultStatus = "failed"
	failed.Result = message
	resultNode := "workflow.failed"
	failed.CurrentNode = firstNonEmptyMaclawAppString(resultNode, failed.CurrentNode)
	failed.CurrentNodeIDs = appendMaclawAppUniqueStrings(firstNonEmptyMaclawAppStringList(failed.WorkflowNodeIDs, failed.CurrentNodeIDs), failed.CurrentNode)
	failed.WorkflowNodeIDs = append([]string(nil), failed.CurrentNodeIDs...)
	failed.WorkflowSkillID = firstNonEmptyMaclawAppString(failed.WorkflowSkillID, workflowSkillID)
	failed.ResultPayload = cloneMapAny(failed.ResultPayload)
	if failed.ResultPayload == nil {
		failed.ResultPayload = map[string]any{}
	}
	failed.ResultPayload["approval_result"] = "failed"
	failed.ResultPayload["business_status"] = failed.BusinessStatus
	failed.ResultPayload["result_status"] = failed.ResultStatus
	failed.ResultPayload["error"] = message
	failed.ResultPayload["text"] = message
	if _, ok := failed.ResultPayload["business_record"]; !ok && failed.RecordID != "" {
		failed.ResultPayload["business_record"] = map[string]any{"id": failed.RecordID, "status": failed.BusinessStatus}
	}
	failed.Outputs = []maclawAppApprovalOutput{{Kind: "approval_result", Type: "approval_result", Title: "Workflow failed", Text: message, Status: "failed"}}
	stored, _ := a.RecordMaclawAppApprovalInstance(failed)
	if stored.AppID != "" {
		failed = stored
	}
	syncResult, syncErr := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{DatasetID: failed.DatasetID, ObjectRole: failed.ObjectRole, AppID: failed.AppID, BlueprintID: failed.BlueprintID, RecordID: failed.RecordID, ApprovalID: firstNonEmptyMaclawAppString(failed.ApprovalID, base.ApprovalID), Instance: failed})
	workflowRun := map[string]any{"ran": false, "workflow_skill_id": workflowSkillID, "output": output, "captured": captured, "error": message, "payload": map[string]any{"approval_instance": failed, "result_payload": cloneMapAny(failed.ResultPayload), "outputs": cloneMaclawAppApprovalOutputs(failed.Outputs)}, "instance": failed, "sync": syncResult, "result_feedback": maclawAppApprovalResultFeedback(failed)}
	if syncErr != nil {
		workflowRun["sync_error"] = syncErr.Error()
	}
	return workflowRun, nil
}

func maclawAppWorkflowSkillPayloadFromOutput(output string, captured map[string]string) (map[string]any, error) {
	if captured != nil {
		for _, key := range []string{"maclaw_app_workflow_result", "workflow_result", "approval_result", "result"} {
			if value := strings.TrimSpace(captured[key]); value != "" {
				if payload, err := decodeMaclawAppWorkflowSkillJSONPayload(value); err == nil {
					return payload, nil
				}
			}
		}
	}
	return decodeMaclawAppWorkflowSkillJSONPayload(output)
}

func decodeMaclawAppWorkflowSkillJSONPayload(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("workflow skill returned empty output")
	}
	candidates := []string{trimmed}
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			candidates = append(candidates, trimmed[start:end+1])
		}
	}
	var lastErr error
	for _, candidate := range candidates {
		var payload map[string]any
		if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
			lastErr = err
			continue
		}
		if len(payload) == 0 {
			lastErr = fmt.Errorf("workflow skill returned empty JSON object")
			continue
		}
		return payload, nil
	}
	return nil, fmt.Errorf("workflow skill output is not a JSON object: %v", lastErr)
}

func maclawAppApprovalInstanceFromWorkflowPayload(payload map[string]any, base maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance := cloneMaclawAppApprovalInstance(base)
	source := payload
	if nested := anyMap(firstNonEmptyMaclawAppAny(payload["approval_instance"], payload["approvalInstance"], payload["instance"])); nested != nil {
		source = nested
	}
	instance.AppID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "app_id", "appID"), instance.AppID)
	instance.AppName = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "app_name", "appName"), instance.AppName)
	instance.BlueprintID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "blueprint_id", "blueprintID"), instance.BlueprintID)
	instance.DatasetID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "dataset_id", "datasetID"), instance.DatasetID)
	instance.ObjectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "object_role", "objectRole"), instance.ObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole)
	instance.ApprovalEvent = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "approval_event", "approvalEvent", "trigger_event", "triggerEvent"), instance.ApprovalEvent)
	instance.RecordID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "record_id", "recordID"), instance.RecordID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "approval_id", "approvalID", "record_approval_id", "recordApprovalID"), instance.ApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID)
	instance.InstanceID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "instance_id", "instanceID", "workflow_instance_id", "workflowInstanceID", "approval_instance_id", "approvalInstanceID"), instance.InstanceID)
	instance.Status = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "status", "decision"), instance.Status)
	instance.Lane = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "lane"), instance.Lane)
	instance.CurrentNode = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_node", "currentNode", "workflow_node_id", "workflowNodeId", "node"), instance.CurrentNode)
	instance.CurrentNodeStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_node_status", "currentNodeStatus", "node_status", "nodeStatus", "workflow_node_status", "workflowNodeStatus"), instance.CurrentNodeStatus)
	if nodes := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(source["current_node_ids"], source["currentNodeIDs"], source["workflow_node_ids"], source["workflowNodeIds"])); len(nodes) > 0 {
		instance.CurrentNodeIDs = nodes
	}
	if nodes := maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(source["workflow_node_ids"], source["workflowNodeIds"], source["current_node_ids"], source["currentNodeIDs"])); len(nodes) > 0 {
		instance.WorkflowNodeIDs = nodes
	}
	if instance.CurrentNode != "" && len(instance.CurrentNodeIDs) == 0 {
		instance.CurrentNodeIDs = []string{instance.CurrentNode}
	}
	if len(instance.WorkflowNodeIDs) == 0 {
		instance.WorkflowNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	}
	instance.CurrentAssignee = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_assignee", "currentAssignee", "assigned_to", "assignedTo"), instance.CurrentAssignee)
	instance.CurrentAssigneeType = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "current_assignee_type", "currentAssigneeType"), instance.CurrentAssigneeType)
	instance.WorkflowSkillID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_skill_id", "workflowSkillId"), instance.WorkflowSkillID)
	instance.WorkflowVersion = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_version", "workflowVersion"), instance.WorkflowVersion)
	instance.WorkflowDecisionID = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "workflow_decision_id", "workflowDecisionId", "decision_id", "decisionID"), instance.WorkflowDecisionID)
	instance.DetailURL = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "detail_url", "detailURL"), instance.DetailURL)
	instance.BusinessStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "business_status", "businessStatus"), instance.BusinessStatus)
	instance.ResultStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "result_status", "resultStatus"), instance.ResultStatus)
	instance.FromStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "from_status", "fromStatus"), instance.FromStatus)
	instance.ToStatus = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "to_status", "toStatus"), instance.ToStatus)
	instance.Result = firstNonEmptyMaclawAppString(maclawAppStringValue(source, "result", "reason", "summary", "text", "progress"), instance.Result)
	if resultPayload := anyMap(firstNonEmptyMaclawAppAny(source["result_payload"], source["resultPayload"], payload["result_payload"], payload["resultPayload"])); resultPayload != nil {
		instance.ResultPayload = cloneMapAny(resultPayload)
		if supplemental, ok := base.ResultPayload["supplemental_input"]; ok {
			if _, exists := instance.ResultPayload["supplemental_input"]; !exists {
				if supplementalMap := anyMap(supplemental); supplementalMap != nil {
					instance.ResultPayload["supplemental_input"] = cloneMapAny(supplementalMap)
				} else {
					instance.ResultPayload["supplemental_input"] = supplemental
				}
			}
		}
	}
	if outputs := decodeMaclawAppApprovalOutputsFromAny(firstNonEmptyMaclawAppAny(source["outputs"], payload["outputs"])); len(outputs) > 0 {
		instance.Outputs = outputs
	}
	if artifacts := decodeMaclawAppApprovalArtifactsFromAny(firstNonEmptyMaclawAppAny(source["artifacts"], payload["artifacts"])); len(artifacts) > 0 {
		instance.Artifacts = artifacts
	}
	if tasks := decodeMaclawAppApprovalNodeTasksFromAny(firstNonEmptyMaclawAppAny(source["node_tasks"], source["nodeTasks"], source["current_node_tasks"], source["currentNodeTasks"], source["approval_tasks"], source["approvalTasks"], source["tasks"], payload["node_tasks"], payload["nodeTasks"], payload["approval_tasks"], payload["approvalTasks"], payload["tasks"])); len(tasks) > 0 {
		instance.NodeTasks = tasks
	}
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	if instance.Status == "approved" || instance.Status == "rejected" || instance.Status == "failed" || instance.Status == "cancelled" || instance.Status == "timeout" {
		instance.Lane = "handled"
	}
	if instance.Status == "attention" {
		instance.Lane = "attention"
	}
	if instance.Status == "requires_input" {
		instance.Lane = "my_requests"
	}
	if instance.Lane == "" {
		instance.Lane = "my_requests"
	}
	return instance
}

func maclawAppApprovalProgressInstancesFromWorkflowPayload(payload map[string]any, base maclawAppApprovalInstance) []maclawAppApprovalInstance {
	raw := firstNonEmptyMaclawAppAny(payload["progress_instances"], payload["progressInstances"], payload["workflow_progress"], payload["workflowProgress"], payload["approval_progress"], payload["approvalProgress"])
	items := anySlice(raw)
	if len(items) == 0 {
		if item := anyMap(raw); item != nil {
			items = []any{item}
		}
	}
	if len(items) == 0 {
		return nil
	}
	instances := make([]maclawAppApprovalInstance, 0, len(items))
	current := cloneMaclawAppApprovalInstance(base)
	for _, item := range items {
		progressPayload := anyMap(item)
		if progressPayload == nil {
			continue
		}
		progress := maclawAppApprovalInstanceFromWorkflowPayload(progressPayload, current)
		progress.Status = normalizeMaclawAppApprovalStatus(firstNonEmptyMaclawAppString(progress.Status, "pending"))
		if progress.ApprovalID == "" {
			progress.ApprovalID = current.ApprovalID
			progress.RecordApprovalID = firstNonEmptyMaclawAppString(progress.RecordApprovalID, progress.ApprovalID)
		}
		instances = append(instances, progress)
		current = progress
	}
	return instances
}

func maclawAppApprovalResultFeedback(instance maclawAppApprovalInstance) map[string]any {
	payload := cloneMapAny(instance.ResultPayload)
	if payload == nil {
		payload = map[string]any{}
	}
	outputs := cloneMaclawAppApprovalOutputs(instance.Outputs)
	artifacts := append([]maclawAppApprovalArtifact(nil), instance.Artifacts...)
	approvalResult := firstNonEmptyMaclawAppString(
		maclawAppStringValue(payload, "approval_result", "approvalResult", "approval_status", "approvalStatus", "decision"),
		instance.Status,
	)
	businessStatus := firstNonEmptyMaclawAppString(
		maclawAppStringValue(payload, "business_status", "businessStatus", "status"),
		instance.BusinessStatus,
	)
	resultStatus := firstNonEmptyMaclawAppString(
		maclawAppStringValue(payload, "result_status", "resultStatus"),
		instance.ResultStatus,
		instance.Status,
	)
	content := firstNonEmptyMaclawAppString(
		maclawAppStringValue(payload, "text", "content", "message", "summary", "result"),
		instance.Result,
	)
	if content == "" {
		for _, output := range outputs {
			if output.Text != "" {
				content = output.Text
				break
			}
		}
	}
	var primaryArtifact map[string]any
	if len(artifacts) > 0 {
		primaryArtifact = compactPayload(map[string]any{
			"id":     artifacts[0].ID,
			"name":   artifacts[0].Name,
			"uri":    artifacts[0].URI,
			"status": artifacts[0].Status,
		})
	}
	return compactPayload(map[string]any{
		"status":           instance.Status,
		"approval_result":  approvalResult,
		"business_status":  businessStatus,
		"result_status":    resultStatus,
		"content":          content,
		"result_payload":   payload,
		"outputs":          outputs,
		"artifacts":        artifacts,
		"output_count":     len(outputs),
		"artifact_count":   len(artifacts),
		"primary_artifact": primaryArtifact,
	})
}

func decodeMaclawAppApprovalOutputsFromAny(value any) []maclawAppApprovalOutput {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func decodeMaclawAppApprovalArtifactsFromAny(value any) []maclawAppApprovalArtifact {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalArtifact
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func decodeMaclawAppApprovalNodeTasksFromAny(value any) []map[string]any {
	if value == nil {
		return nil
	}
	items := anySlice(value)
	if len(items) == 0 {
		if item := anyMap(value); item != nil {
			items = []any{item}
		}
	}
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		task := cloneMapAny(anyMap(item))
		if task == nil {
			continue
		}
		out = append(out, task)
	}
	return out
}

// SyncMaclawAppApprovalInstanceToDataSrv writes the app approval instance state
// into DataSrv's RecordApproval link. It is intentionally app-level so the GUI
// never handles DataSrv credentials directly.
func (a *App) SyncMaclawAppApprovalInstanceToDataSrv(input maclawAppApprovalDataSrvSyncInput) (map[string]any, error) {
	input.DatasetID = strings.TrimSpace(input.DatasetID)
	input.ObjectRole = strings.TrimSpace(input.ObjectRole)
	input.AppID = strings.TrimSpace(input.AppID)
	input.BlueprintID = strings.TrimSpace(input.BlueprintID)
	input.RecordID = strings.TrimSpace(input.RecordID)
	input.ApprovalID = strings.TrimSpace(input.ApprovalID)
	instance := normalizeMaclawAppApprovalInstanceFields(cloneMaclawAppApprovalInstance(input.Instance))
	if instance.AppID == "" || instance.InstanceID == "" {
		return nil, fmt.Errorf("instance app_id and instance_id are required")
	}
	instance.Status = normalizeMaclawAppApprovalStatus(instance.Status)
	input.AppID = firstNonEmptyMaclawAppString(input.AppID, instance.AppID)
	input.BlueprintID = firstNonEmptyMaclawAppString(input.BlueprintID, instance.BlueprintID)
	input.DatasetID = firstNonEmptyMaclawAppString(input.DatasetID, instance.DatasetID)
	input.ObjectRole = firstNonEmptyMaclawAppString(input.ObjectRole, instance.ObjectRole, instance.ApprovalObjectRole)
	input.RecordID = firstNonEmptyMaclawAppString(input.RecordID, instance.RecordID)
	input.ApprovalID = firstNonEmptyMaclawAppString(input.ApprovalID, instance.ApprovalID, instance.RecordApprovalID)
	instance.AppID = firstNonEmptyMaclawAppString(instance.AppID, input.AppID)
	instance.BlueprintID = firstNonEmptyMaclawAppString(instance.BlueprintID, input.BlueprintID)
	instance.DatasetID = firstNonEmptyMaclawAppString(instance.DatasetID, input.DatasetID)
	instance.ObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, input.ObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	instance.RecordID = firstNonEmptyMaclawAppString(instance.RecordID, input.RecordID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, input.ApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)
	var runtimeErr error
	instance, runtimeErr = a.applyMaclawAppApprovalRuntimeContract(instance)
	if runtimeErr != nil {
		return nil, runtimeErr
	}
	input.AppID = firstNonEmptyMaclawAppString(input.AppID, instance.AppID)
	input.BlueprintID = firstNonEmptyMaclawAppString(input.BlueprintID, instance.BlueprintID)
	input.DatasetID = firstNonEmptyMaclawAppString(input.DatasetID, instance.DatasetID)
	input.ObjectRole = firstNonEmptyMaclawAppString(input.ObjectRole, instance.ObjectRole, instance.ApprovalObjectRole)
	input.RecordID = firstNonEmptyMaclawAppString(input.RecordID, instance.RecordID)
	input.ApprovalID = firstNonEmptyMaclawAppString(input.ApprovalID, instance.ApprovalID, instance.RecordApprovalID)
	if input.DatasetID == "" && input.ObjectRole != "" {
		cfg, err := a.GetMISDataConfig()
		if err != nil {
			return map[string]any{"synced": false, "reason": err.Error(), "object_role": input.ObjectRole}, nil
		}
		if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
			return map[string]any{"synced": false, "reason": "mis data service unavailable", "object_role": input.ObjectRole}, nil
		}
		resolvedDatasetID, err := a.resolveMISDatasetIDArg(cfg, map[string]interface{}{
			"app_id":       input.AppID,
			"blueprint_id": input.BlueprintID,
			"object_role":  input.ObjectRole,
		}, true)
		if err != nil {
			return map[string]any{"synced": false, "reason": err.Error(), "object_role": input.ObjectRole}, nil
		}
		input.DatasetID = resolvedDatasetID
	}
	if input.DatasetID == "" || input.RecordID == "" {
		return map[string]any{"synced": false, "reason": "missing dataset_id/object_role or record_id"}, nil
	}
	if instance.WorkflowSkillID == "" {
		return map[string]any{"synced": false, "reason": "missing workflow_skill_id"}, nil
	}
	if instance.Status == "attention" && input.ApprovalID != "" {
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		return map[string]any{"synced": true, "action": "attention_view_only", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "reason": "attention is view-only and does not review the DataSrv approval", "business_record_sync": businessRecordSync}, nil
	}
	if input.ApprovalID != "" && (instance.Status == "pending" || instance.Status == "requires_input") {
		out := a.executeMISDataTool(map[string]interface{}{
			"action":                "update_record_approval_progress",
			"approval_id":           input.ApprovalID,
			"workflow_instance_id":  instance.InstanceID,
			"workflow_node_id":      instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"current_node_status":   instance.CurrentNodeStatus,
			"node_tasks":            cloneMaclawAppMapSlice(instance.NodeTasks),
			"current_assignee":      instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"from_status":           instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"detail_url":            instance.DetailURL,
			"workflow_version":      instance.WorkflowVersion,
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
			"progress":              instance.Result,
		})
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		return map[string]any{"synced": true, "action": "update_record_approval_progress", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "response": out, "business_record_sync": businessRecordSync}, nil
	}
	if input.ApprovalID == "" && maclawAppApprovalStatusCanReview(instance.Status) {
		input.ApprovalID = a.findMaclawAppRecordApprovalID(input, instance)
	}
	if input.ApprovalID != "" && maclawAppApprovalStatusCanReview(instance.Status) {
		out := a.executeMISDataTool(map[string]interface{}{
			"action":                "review_record_approval",
			"approval_id":           input.ApprovalID,
			"workflow_instance_id":  instance.InstanceID,
			"decision":              instance.Status,
			"reason":                instance.Result,
			"workflow_node_id":      instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"current_node_status":   instance.CurrentNodeStatus,
			"node_tasks":            cloneMaclawAppMapSlice(instance.NodeTasks),
			"current_assignee":      instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"from_status":           instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"detail_url":            instance.DetailURL,
			"workflow_version":      instance.WorkflowVersion,
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
		})
		instance.ApprovalID = input.ApprovalID
		instance.RecordApprovalID = input.ApprovalID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
		businessRecordSync := a.syncMaclawAppApprovalBusinessRecord(input.DatasetID, input.RecordID, instance)
		return map[string]any{"synced": true, "action": "review_record_approval", "dataset_id": input.DatasetID, "approval_id": input.ApprovalID, "response": out, "business_record_sync": businessRecordSync}, nil
	}
	businessRecordSync := a.syncMaclawAppApprovalBusinessRecordForApproval(input.DatasetID, input.RecordID, instance, true)
	approvalKind := "approval"
	if instance.Status == "attention" {
		approvalKind = "attention"
	}
	out := a.executeMISDataTool(map[string]interface{}{
		"action":                "create_record_approval",
		"dataset_id":            input.DatasetID,
		"object_role":           input.ObjectRole,
		"app_id":                input.AppID,
		"blueprint_id":          input.BlueprintID,
		"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, input.AppID),
		"submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
		"record_id":             input.RecordID,
		"kind":                  approvalKind,
		"summary":               instance.Title,
		"assigned_to":           instance.Approver,
		"current_assignee":      instance.CurrentAssignee,
		"current_assignee_type": instance.CurrentAssigneeType,
		"from_status":           instance.FromStatus,
		"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
		"request": compactPayload(map[string]interface{}{
			"app_id":                input.AppID,
			"blueprint_id":          input.BlueprintID,
			"dataset_id":            input.DatasetID,
			"object_role":           input.ObjectRole,
			"approval_instance_id":  instance.InstanceID,
			"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
			"trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, input.AppID),
			"owner":                 instance.Owner,
			"applicant":             instance.Applicant,
			"submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
			"assigned_to":           instance.Approver,
			"current_assignee":      instance.CurrentAssignee,
			"currentAssignee":       instance.CurrentAssignee,
			"current_assignee_type": instance.CurrentAssigneeType,
			"currentAssigneeType":   instance.CurrentAssigneeType,
			"business_entity":       instance.BusinessEntity,
			"business_action":       instance.BusinessAction,
			"business_note":         instance.BusinessNote,
			"workflow_skill_id":     instance.WorkflowSkillID,
			"workflowSkillId":       instance.WorkflowSkillID,
			"workflow_version":      instance.WorkflowVersion,
			"workflowVersion":       instance.WorkflowVersion,
			"workflow_node_id":      instance.CurrentNode,
			"workflowNodeId":        instance.CurrentNode,
			"workflow_node_ids":     append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"workflowNodeIds":       append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
			"current_node_status":   instance.CurrentNodeStatus,
			"currentNodeStatus":     instance.CurrentNodeStatus,
			"node_tasks":            cloneMaclawAppMapSlice(instance.NodeTasks),
			"nodeTasks":             cloneMaclawAppMapSlice(instance.NodeTasks),
			"workflow_decision_id":  instance.WorkflowDecisionID,
			"workflowDecisionId":    instance.WorkflowDecisionID,
			"from_status":           instance.FromStatus,
			"fromStatus":            instance.FromStatus,
			"to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"toStatus":              firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
			"business_status":       firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"businessStatus":        firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
			"result_status":         firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"resultStatus":          firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
			"result_payload":        cloneMapAny(instance.ResultPayload),
			"resultPayload":         cloneMapAny(instance.ResultPayload),
			"outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
			"artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
			"result":                instance.Result,
			"detail_url":            instance.DetailURL,
			"detailURL":             instance.DetailURL,
		}),
		"workflow_skill_id":    instance.WorkflowSkillID,
		"workflow_version":     instance.WorkflowVersion,
		"workflow_instance_id": instance.InstanceID,
		"workflow_node_id":     instance.CurrentNode,
		"workflow_node_ids":    append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
		"current_node_status":  instance.CurrentNodeStatus,
		"node_tasks":           cloneMaclawAppMapSlice(instance.NodeTasks),
		"workflow_decision_id": instance.WorkflowDecisionID,
		"detail_url":           instance.DetailURL,
		"business_status":      firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"result_status":        firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
		"result_payload":       cloneMapAny(instance.ResultPayload),
		"outputs":              cloneMaclawAppApprovalOutputs(instance.Outputs),
		"artifacts":            append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
	})
	approvalID := maclawAppApprovalIDFromToolResult(out)
	if approvalID != "" {
		instance.ApprovalID = approvalID
		instance.RecordApprovalID = approvalID
		instance.DatasetID = input.DatasetID
		instance.ObjectRole = input.ObjectRole
		instance.ApprovalObjectRole = input.ObjectRole
		instance.BlueprintID = input.BlueprintID
		instance.RecordID = input.RecordID
		_, _ = a.RecordMaclawAppApprovalInstance(instance)
	}
	return map[string]any{"synced": true, "action": "create_record_approval", "dataset_id": input.DatasetID, "approval_id": approvalID, "response": out, "business_record_sync": businessRecordSync}, nil
}

func (a *App) findMaclawAppApprovalInstanceForContinue(appID, instanceID, approvalID, recordID string) (*maclawAppApprovalInstance, error) {
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return nil, err
	}
	appID = strings.TrimSpace(appID)
	instanceID = strings.TrimSpace(instanceID)
	approvalID = strings.TrimSpace(approvalID)
	recordID = strings.TrimSpace(recordID)
	for _, existing := range registry.Instances {
		if appID != "" && !strings.EqualFold(strings.TrimSpace(existing.AppID), appID) {
			continue
		}
		if instanceID != "" && strings.TrimSpace(existing.InstanceID) == instanceID {
			found := cloneMaclawAppApprovalInstance(existing)
			return &found, nil
		}
		if approvalID != "" && strings.EqualFold(strings.TrimSpace(firstNonEmptyMaclawAppString(existing.ApprovalID, existing.RecordApprovalID)), approvalID) {
			found := cloneMaclawAppApprovalInstance(existing)
			return &found, nil
		}
		if instanceID == "" && approvalID == "" && recordID != "" && strings.EqualFold(strings.TrimSpace(existing.RecordID), recordID) && normalizeMaclawAppApprovalStatus(existing.Status) == "requires_input" {
			found := cloneMaclawAppApprovalInstance(existing)
			return &found, nil
		}
	}
	return nil, nil
}

func maclawAppWorkflowMappingNodeFromInstall(install *maclawAppInstallRecord, keys ...string) string {
	if install == nil {
		return ""
	}
	for _, source := range []map[string]any{install.TestEvidence, install.WorkflowContract} {
		if node := maclawAppStringValue(source, keys...); node != "" {
			return node
		}
		if mapping := anyMap(source["workflow_mapping"]); mapping != nil {
			if node := maclawAppStringValue(mapping, keys...); node != "" {
				return node
			}
		}
	}
	if mapping := anyMap(install.Package["workflow_mapping"]); mapping != nil {
		if node := maclawAppStringValue(mapping, keys...); node != "" {
			return node
		}
	}
	for _, entry := range anySlice(install.Package["apps"]) {
		entryMap := anyMap(entry)
		app := anyMap(entryMap["app"])
		for _, source := range []map[string]any{anyMap(app["binding"]), anyMap(app["governance"]), app} {
			if mapping := anyMap(source["workflow"]); mapping != nil {
				if node := maclawAppStringValue(mapping, keys...); node != "" {
					return node
				}
			}
			if mapping := anyMap(source["workflow_mapping"]); mapping != nil {
				if node := maclawAppStringValue(mapping, keys...); node != "" {
					return node
				}
			}
		}
	}
	return ""
}

func (a *App) applyMaclawAppApprovalRuntimeContract(instance maclawAppApprovalInstance) (maclawAppApprovalInstance, error) {
	registry, err := a.readMaclawAppInstallRegistry()
	if err != nil {
		return instance, err
	}
	var install *maclawAppInstallRecord
	for i := range registry.Installs {
		if strings.EqualFold(strings.TrimSpace(registry.Installs[i].AppID), strings.TrimSpace(instance.AppID)) {
			install = &registry.Installs[i]
			break
		}
	}
	if install == nil {
		return instance, nil
	}
	contract := install.WorkflowContract
	snapshot := install.VersionSnapshot
	if instance.WorkflowSkillID == "" {
		instance.WorkflowSkillID = maclawAppRuntimeDefaultWorkflowSkillID(contract, snapshot)
	}
	if instance.WorkflowSkillID == "" {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: missing workflow_skill_id for installed app %s", instance.AppID)
	}
	contractWorkflowID := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id")
	if contractWorkflowID != "" && !strings.EqualFold(contractWorkflowID, instance.WorkflowSkillID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_skill_id %s does not match installed contract %s", instance.WorkflowSkillID, contractWorkflowID)
	}
	binding := maclawAppRuntimeApprovalBindingSnapshot(snapshot, instance)
	if binding.WorkflowSkillID != "" && !strings.EqualFold(binding.WorkflowSkillID, instance.WorkflowSkillID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_skill_id %s does not match installed binding %s", instance.WorkflowSkillID, binding.WorkflowSkillID)
	}
	if instance.ApprovalEvent == "" {
		instance.ApprovalEvent = binding.Event
	}
	expectedDatasetID := binding.DatasetID
	if instance.DatasetID == "" {
		instance.DatasetID = expectedDatasetID
	}
	if expectedDatasetID != "" && instance.DatasetID != "" && !strings.EqualFold(instance.DatasetID, expectedDatasetID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: dataset_id %s does not match installed contract %s", instance.DatasetID, expectedDatasetID)
	}
	expectedBlueprintID := binding.BlueprintID
	if instance.BlueprintID == "" {
		instance.BlueprintID = expectedBlueprintID
	}
	if expectedBlueprintID != "" && instance.BlueprintID != "" && !strings.EqualFold(instance.BlueprintID, expectedBlueprintID) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: blueprint_id %s does not match installed contract %s", instance.BlueprintID, expectedBlueprintID)
	}
	expectedObjectRole := firstNonEmptyMaclawAppString(binding.ObjectRole, maclawAppStringValue(contract, "objectRole", "object_role", "businessObjectRole", "business_object_role"))
	if instance.ObjectRole == "" {
		instance.ObjectRole = expectedObjectRole
		instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	}
	if instance.ApprovalObjectRole == "" {
		instance.ApprovalObjectRole = instance.ObjectRole
	}
	if expectedObjectRole != "" && !strings.EqualFold(instance.ObjectRole, expectedObjectRole) && !strings.EqualFold(instance.ApprovalObjectRole, expectedObjectRole) {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: object_role %s does not match installed contract %s", firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole), expectedObjectRole)
	}
	expectedVersion := firstNonEmptyMaclawAppString(binding.WorkflowVersion, maclawAppStringValue(contract, "workflowVersion", "workflow_version"), maclawAppRuntimeWorkflowVersion(snapshot, instance.WorkflowSkillID))
	if instance.WorkflowVersion == "" {
		instance.WorkflowVersion = expectedVersion
	}
	if expectedVersion != "" && instance.WorkflowVersion != "" && instance.WorkflowVersion != expectedVersion {
		return instance, fmt.Errorf("approval workflow contract runtime check failed: workflow_version %s does not match installed version %s", instance.WorkflowVersion, expectedVersion)
	}
	return normalizeMaclawAppApprovalInstanceFields(instance), nil
}

func maclawAppRuntimeDefaultWorkflowSkillID(contract map[string]any, snapshot maclawAppInstallVersionSnapshot) string {
	if id := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"); id != "" {
		return id
	}
	if len(snapshot.ApprovalBindings) == 1 {
		return snapshot.ApprovalBindings[0].WorkflowSkillID
	}
	if len(snapshot.WorkflowSkills) == 1 {
		return snapshot.WorkflowSkills[0].ID
	}
	return ""
}

func maclawAppRuntimeApprovalBindingSnapshot(snapshot maclawAppInstallVersionSnapshot, instance maclawAppApprovalInstance) maclawAppInstallApprovalBindingSnapshot {
	for _, binding := range snapshot.ApprovalBindings {
		if instance.WorkflowSkillID != "" && strings.EqualFold(binding.WorkflowSkillID, instance.WorkflowSkillID) {
			return binding
		}
	}
	for _, binding := range snapshot.ApprovalBindings {
		if instance.ApprovalEvent != "" && strings.EqualFold(binding.Event, instance.ApprovalEvent) {
			return binding
		}
	}
	if len(snapshot.ApprovalBindings) == 1 {
		return snapshot.ApprovalBindings[0]
	}
	return maclawAppInstallApprovalBindingSnapshot{}
}

func maclawAppRuntimeWorkflowVersion(snapshot maclawAppInstallVersionSnapshot, workflowSkillID string) string {
	for _, skill := range snapshot.WorkflowSkills {
		if strings.EqualFold(skill.ID, workflowSkillID) {
			return skill.Version
		}
	}
	return ""
}

func (a *App) findMaclawAppRecordApprovalID(input maclawAppApprovalDataSrvSyncInput, instance maclawAppApprovalInstance) string {
	out := a.executeMISDataTool(map[string]interface{}{
		"action":                "list_record_approvals",
		"dataset_id":            input.DatasetID,
		"record_id":             input.RecordID,
		"app_id":                input.AppID,
		"blueprint_id":          input.BlueprintID,
		"object_role":           input.ObjectRole,
		"workflow_instance_id":  instance.InstanceID,
		"approval_workflow_id":  firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"trigger_event":         instance.ApprovalEvent,
		"current_assignee":      instance.CurrentAssignee,
		"current_assignee_type": instance.CurrentAssigneeType,
		"from_status":           instance.FromStatus,
		"to_status":             instance.ToStatus,
		"status":                "pending",
		"limit":                 1,
	})
	return maclawAppApprovalIDFromToolResult(out)
}

func maclawAppApprovalIDFromToolResult(out string) string {
	var body map[string]any
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		return ""
	}
	if id := firstNonEmptyMaclawAppString(stringMapValue(body, "id"), stringMapValue(body, "approval_id"), stringMapValue(body, "record_approval_id")); id != "" {
		return id
	}
	if approval := anyMap(body["approval"]); approval != nil {
		if id := firstNonEmptyMaclawAppString(stringMapValue(approval, "id"), stringMapValue(approval, "approval_id"), stringMapValue(approval, "record_approval_id")); id != "" {
			return id
		}
	}
	for _, item := range anySlice(body["items"]) {
		approval := anyMap(item)
		if approval == nil {
			continue
		}
		if id := firstNonEmptyMaclawAppString(stringMapValue(approval, "id"), stringMapValue(approval, "approval_id"), stringMapValue(approval, "record_approval_id")); id != "" {
			return id
		}
	}
	return ""
}

func maclawAppApprovalStatusCanReview(status string) bool {
	switch strings.TrimSpace(status) {
	case "approved", "rejected", "failed", "cancelled", "timeout":
		return true
	default:
		return false
	}
}

func (a *App) syncMaclawAppApprovalBusinessRecord(datasetID, recordID string, instance maclawAppApprovalInstance) map[string]any {
	return a.syncMaclawAppApprovalBusinessRecordForApproval(datasetID, recordID, instance, false)
}

func (a *App) syncMaclawAppApprovalBusinessRecordForApproval(datasetID, recordID string, instance maclawAppApprovalInstance, createIfMissing bool) map[string]any {
	patch := maclawAppApprovalBusinessRecordPatch(instance)
	if len(patch) == 0 && createIfMissing {
		patch = maclawAppApprovalFallbackBusinessRecordPatch(instance)
	}
	if len(patch) == 0 {
		return map[string]any{"synced": false, "reason": "missing business record patch"}
	}
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return map[string]any{"synced": false, "reason": err.Error()}
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		return map[string]any{"synced": false, "reason": "mis data service unavailable"}
	}
	path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/records/" + pathEscape(recordID)
	data, err := a.callMISDataAPIBytes(cfg, http.MethodGet, path, nil)
	if err != nil {
		if createIfMissing && strings.Contains(err.Error(), "HTTP 404") {
			createBody := compactPayload(map[string]interface{}{
				"id":        recordID,
				"title":     instance.Title,
				"source_id": instance.InstanceID,
				"data":      cloneMapAny(patch),
			})
			response, createErr := a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records", createBody)
			if createErr != nil {
				return map[string]any{"synced": false, "reason": createErr.Error(), "patch": cloneMapAny(patch), "action": "create_business_record"}
			}
			result := map[string]any{}
			_ = json.Unmarshal(response, &result)
			return map[string]any{"synced": true, "action": "create_business_record", "record_id": recordID, "patch": cloneMapAny(patch), "response": result}
		}
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	merged := cloneMapAny(anyMap(record["data"]))
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range patch {
		merged[key] = value
	}
	response, err := a.callMISDataAPIBytes(cfg, http.MethodPatch, path, compactPayload(map[string]interface{}{"data": merged}))
	if err != nil {
		return map[string]any{"synced": false, "reason": err.Error(), "patch": cloneMapAny(patch)}
	}
	result := map[string]any{}
	_ = json.Unmarshal(response, &result)
	return map[string]any{"synced": true, "action": "update_business_record", "record_id": recordID, "patch": cloneMapAny(patch), "response": result}
}

func maclawAppApprovalBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	for _, key := range []string{"business_record_patch", "record_patch", "business_record"} {
		if patch := maclawAppApprovalPatchMap(instance.ResultPayload[key]); len(patch) > 0 {
			return mergeMaclawAppApprovalBusinessRecordPatch(patch, maclawAppApprovalSemanticBusinessRecordPatch(instance))
		}
	}
	for _, output := range instance.Outputs {
		outputKind := strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(output.Kind, output.Type)))
		if outputKind != "business_record" && outputKind != "record_patch" {
			continue
		}
		if patch := maclawAppApprovalPatchMap(output.Data); len(patch) > 0 {
			return mergeMaclawAppApprovalBusinessRecordPatch(patch, maclawAppApprovalSemanticBusinessRecordPatch(instance))
		}
	}
	return nil
}

func maclawAppApprovalFallbackBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	patch := mergeMaclawAppApprovalBusinessRecordPatch(nil, maclawAppApprovalSemanticBusinessRecordPatch(instance))
	if len(patch) == 0 {
		return nil
	}
	return patch
}

func maclawAppApprovalBusinessRecordLane(instance maclawAppApprovalInstance) string {
	lane := normalizeMaclawAppApprovalLane(instance.Lane)
	switch strings.TrimSpace(instance.Status) {
	case "approved", "rejected":
		return "handled"
	case "attention":
		return "attention"
	}
	return lane
}

func maclawAppApprovalSemanticBusinessRecordPatch(instance maclawAppApprovalInstance) map[string]any {
	return compactPayload(map[string]interface{}{
		"app_id":                         instance.AppID,
		"app_name":                       instance.AppName,
		"blueprint_id":                   instance.BlueprintID,
		"object_role":                    firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole),
		"approval_event":                 instance.ApprovalEvent,
		"approval_workflow_id":           firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID),
		"approval_trigger_event":         firstNonEmptyMaclawAppString(instance.ApprovalEvent, instance.AppID),
		"approval_submitted_by":          firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner),
		"approval_instance_id":           instance.InstanceID,
		"approval_id":                    firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID),
		"record_approval_id":             firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID),
		"approval_status":                instance.Status,
		"approval_lane":                  maclawAppApprovalBusinessRecordLane(instance),
		"approval_current_node":          instance.CurrentNode,
		"approval_current_node_status":   instance.CurrentNodeStatus,
		"approval_current_nodes":         append([]string(nil), instance.CurrentNodeIDs...),
		"approval_node_tasks":            cloneMaclawAppMapSlice(instance.NodeTasks),
		"approval_current_assignee":      instance.CurrentAssignee,
		"approval_current_assignee_type": instance.CurrentAssigneeType,
		"approval_from_status":           instance.FromStatus,
		"approval_to_status":             firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status),
		"workflow_skill_id":              instance.WorkflowSkillID,
		"workflow_version":               instance.WorkflowVersion,
		"workflow_instance_id":           instance.InstanceID,
		"workflow_node_id":               instance.CurrentNode,
		"workflow_node_ids":              append([]string(nil), firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs)...),
		"workflow_decision_id":           instance.WorkflowDecisionID,
		"approval_detail_url":            instance.DetailURL,
		"approval_result_summary":        maclawAppApprovalResultSummary(instance),
		"approval_primary_artifact":      maclawAppApprovalPrimaryArtifactName(instance),
		"approval_output_count":          maclawAppApprovalCountValue(len(instance.Outputs)),
		"approval_artifact_count":        maclawAppApprovalCountValue(len(instance.Artifacts)),
		"approval_result_payload":        cloneMapAny(instance.ResultPayload),
		"approval_outputs":               cloneMaclawAppApprovalOutputs(instance.Outputs),
		"approval_artifacts":             append([]maclawAppApprovalArtifact(nil), instance.Artifacts...),
		"business_entity":                instance.BusinessEntity,
		"business_action":                instance.BusinessAction,
		"business_note":                  instance.BusinessNote,
		"status":                         firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"business_status":                firstNonEmptyMaclawAppString(instance.BusinessStatus, instance.Status),
		"result_status":                  firstNonEmptyMaclawAppString(instance.ResultStatus, instance.Status),
		"owner":                          instance.Owner,
		"applicant":                      instance.Applicant,
		"approver":                       instance.Approver,
	})
}

func maclawAppApprovalCountValue(count int) any {
	if count <= 0 {
		return nil
	}
	return count
}

func maclawAppApprovalResultSummary(instance maclawAppApprovalInstance) string {
	for _, key := range []string{"summary", "text", "content", "message", "result"} {
		if value, ok := instance.ResultPayload[key].(string); ok && strings.TrimSpace(value) != "" {
			return truncateMaclawAppApprovalSummary(value)
		}
	}
	businessRecordSummary := ""
	if businessRecord := anyMap(instance.ResultPayload["business_record"]); len(businessRecord) > 0 {
		for _, key := range []string{"summary", "title", "name", "status"} {
			if value, ok := businessRecord[key].(string); ok && strings.TrimSpace(value) != "" {
				businessRecordSummary = value
				break
			}
		}
	}
	if (instance.Status == "approved" || instance.Status == "rejected") && businessRecordSummary != "" {
		return truncateMaclawAppApprovalSummary(businessRecordSummary)
	}
	for _, output := range instance.Outputs {
		for _, value := range []string{output.Text, output.Title, output.Status, output.Kind, output.Type} {
			if strings.TrimSpace(value) != "" {
				return truncateMaclawAppApprovalSummary(value)
			}
		}
	}
	if businessRecordSummary != "" {
		return truncateMaclawAppApprovalSummary(businessRecordSummary)
	}
	for _, value := range []string{instance.Result, instance.ResultStatus, instance.BusinessStatus, instance.Status, instance.Title} {
		if strings.TrimSpace(value) != "" {
			return truncateMaclawAppApprovalSummary(value)
		}
	}
	return ""
}

func maclawAppApprovalPrimaryArtifactName(instance maclawAppApprovalInstance) string {
	for _, artifact := range instance.Artifacts {
		for _, value := range []string{artifact.Name, artifact.URI, artifact.ID, artifact.Path, artifact.RemoteURL} {
			if strings.TrimSpace(value) != "" {
				return truncateMaclawAppApprovalSummary(value)
			}
		}
	}
	for _, output := range instance.Outputs {
		if output.Artifact != nil {
			for _, value := range []string{output.Artifact.Name, output.Artifact.URI, output.Artifact.ID, output.Artifact.Path, output.Artifact.RemoteURL} {
				if strings.TrimSpace(value) != "" {
					return truncateMaclawAppApprovalSummary(value)
				}
			}
		}
	}
	return ""
}

func truncateMaclawAppApprovalSummary(value string) string {
	value = strings.TrimSpace(value)
	const maxRunes = 240
	if len([]rune(value)) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func mergeMaclawAppApprovalBusinessRecordPatch(primary, semantic map[string]any) map[string]any {
	merged := cloneMapAny(primary)
	if merged == nil {
		merged = map[string]any{}
	}
	for key, value := range semantic {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		if _, exists := merged[key]; exists {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func maclawAppApprovalPatchMap(value any) map[string]any {
	data := anyMap(value)
	for _, key := range []string{"data", "fields", "set"} {
		if nested := anyMap(data[key]); len(nested) > 0 {
			data = nested
			break
		}
	}
	patch := map[string]any{}
	for key, item := range data {
		field := strings.TrimSpace(key)
		switch strings.ToLower(field) {
		case "", "id", "record_id", "recordid", "dataset_id", "datasetid":
			continue
		}
		patch[field] = item
	}
	if len(patch) == 0 {
		return nil
	}
	return patch
}

// ListMaclawAppApprovalInstances returns newest-first approval instances for a
// MaClaw approval app. lane can be my_requests, pending_my_approval, handled,
// attention, or all/empty.
func (a *App) ListMaclawAppApprovalInstances(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	return a.listMaclawAppApprovalInstances(appID, lane, limit)
}

// ListMaclawAppApprovalInstancesAll returns newest-first approval instances
// across all MaClaw approval apps. lane can be my_requests,
// pending_my_approval, handled, attention, or all/empty.
func (a *App) ListMaclawAppApprovalInstancesAll(lane string, limit int) ([]maclawAppApprovalInstance, error) {
	return a.listMaclawAppApprovalInstances("", lane, limit)
}

func (a *App) listMaclawAppApprovalInstances(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	registry, err := a.readMaclawAppApprovalRegistry()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	localContext := make([]maclawAppApprovalInstance, 0, len(registry.Instances))
	localVisible := make([]maclawAppApprovalInstance, 0, len(registry.Instances))
	for _, instance := range registry.Instances {
		if appID != "" && instance.AppID != appID {
			continue
		}
		cloned := cloneMaclawAppApprovalInstance(instance)
		localContext = append(localContext, cloned)
		if maclawAppApprovalInstanceMatchesLane(instance, lane) {
			localVisible = append(localVisible, cloned)
		}
	}
	remote, _ := a.listMaclawAppApprovalInstancesFromDataSrv(appID, lane, limit)
	out := localVisible
	if len(remote) > 0 {
		out = filterMaclawAppApprovalInstancesByLane(mergeMaclawAppApprovalInstanceLists(localContext, remote), lane)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (a *App) listMaclawAppApprovalInstancesFromDataSrv(appID string, lane string, limit int) ([]maclawAppApprovalInstance, error) {
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		return nil, nil
	}
	values := url.Values{}
	if appID = strings.TrimSpace(appID); appID != "" {
		values.Set("app_id", appID)
	}
	if lane = normalizeMaclawAppApprovalLaneFilter(lane); lane != "" && lane != "all" {
		values.Set("lane", lane)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	values.Set("limit", fmt.Sprintf("%d", limit))
	path := "/api/v1/data/approvals"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	data, err := a.callMISDataAPIBytes(cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Items []contract.RecordApproval `json:"items"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	out := make([]maclawAppApprovalInstance, 0, len(body.Items))
	for _, item := range body.Items {
		instance := maclawAppApprovalInstanceFromRecordApproval(item, lane, cfg.UserID)
		if instance.AppID == "" && appID != "" {
			instance.AppID = appID
		}
		if instance.AppID == "" && appID != "" {
			continue
		}
		out = append(out, instance)
	}
	return out, nil
}

func maclawAppApprovalInstanceFromRecordApproval(item contract.RecordApproval, requestedLane string, currentUserID string) maclawAppApprovalInstance {
	request := cloneMapAny(item.Request)
	resultPayload := cloneMapAny(item.ResultPayload)
	if resultPayload == nil {
		resultPayload = cloneMapAny(anyMap(firstNonEmptyMaclawAppAny(request["result_payload"], request["resultPayload"])))
	}
	instanceID := firstNonEmptyMaclawAppString(item.WorkflowInstanceID, stringMapValue(request, "approval_instance_id"), item.ID)
	status := normalizeMaclawAppApprovalStatusForRecordApproval(item)
	lane := normalizeMaclawAppApprovalLaneForRecordApproval(item, requestedLane, status, currentUserID)
	result := firstNonEmptyMaclawAppString(item.Reason, stringMapValue(resultPayload, "text"), stringMapValue(resultPayload, "summary"), item.ResultStatus, item.BusinessStatus, status)
	currentNodeIDs := append([]string(nil), item.WorkflowNodeIDs...)
	for _, key := range []string{"current_node_ids", "currentNodeIDs", "workflow_node_ids", "workflowNodeIDs", "workflowNodeIds", "current_node", "currentNode", "workflow_node", "workflowNode", "workflow_node_id", "workflowNodeId"} {
		currentNodeIDs = append(currentNodeIDs, maclawAppStringListFromAny(request[key])...)
	}
	outputs := maclawAppApprovalOutputsFromRecordApprovals(item.Outputs)
	if len(outputs) == 0 {
		outputs = maclawAppApprovalOutputsFromAny(firstNonEmptyMaclawAppAny(request["outputs"], request["approval_outputs"], request["approvalOutputs"]))
	}
	artifacts := maclawAppApprovalArtifactsFromRecordApprovals(item.Artifacts)
	if len(artifacts) == 0 {
		artifacts = maclawAppApprovalArtifactsFromAny(firstNonEmptyMaclawAppAny(request["artifacts"], request["approval_artifacts"], request["approvalArtifacts"]))
	}
	instance := maclawAppApprovalInstance{
		AppID:               firstNonEmptyMaclawAppString(item.AppID, stringMapValue(request, "app_id"), stringMapValue(request, "appID"), stringMapValue(request, "maclaw_app_id"), stringMapValue(request, "maclawAppID")),
		BlueprintID:         firstNonEmptyMaclawAppString(item.BlueprintID, stringMapValue(request, "blueprint_id"), stringMapValue(request, "blueprintID")),
		DatasetID:           item.DatasetID,
		ObjectRole:          firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role"), stringMapValue(request, "objectRole"), stringMapValue(request, "business_object_role"), stringMapValue(request, "businessObjectRole")),
		ApprovalObjectRole:  firstNonEmptyMaclawAppString(item.ObjectRole, stringMapValue(request, "object_role"), stringMapValue(request, "objectRole"), stringMapValue(request, "business_object_role"), stringMapValue(request, "businessObjectRole")),
		ApprovalEvent:       firstNonEmptyMaclawAppString(item.TriggerEvent, stringMapValue(request, "approval_event"), stringMapValue(request, "approvalEvent"), stringMapValue(request, "trigger_event"), stringMapValue(request, "triggerEvent")),
		InstanceID:          instanceID,
		Title:               firstNonEmptyMaclawAppString(item.Summary, stringMapValue(request, "title"), instanceID, item.ID),
		Lane:                lane,
		Status:              status,
		CurrentNode:         firstNonEmptyMaclawAppString(item.WorkflowNodeID, stringMapValue(request, "current_node"), stringMapValue(request, "currentNode"), stringMapValue(request, "workflow_node"), stringMapValue(request, "workflowNode"), stringMapValue(request, "workflow_node_id"), stringMapValue(request, "workflowNodeId")),
		CurrentNodeStatus:   firstNonEmptyMaclawAppString(stringMapValue(request, "current_node_status"), stringMapValue(request, "currentNodeStatus"), stringMapValue(request, "node_status"), stringMapValue(request, "nodeStatus"), stringMapValue(resultPayload, "current_node_status"), stringMapValue(resultPayload, "currentNodeStatus"), stringMapValue(resultPayload, "node_status"), stringMapValue(resultPayload, "nodeStatus")),
		CurrentNodeIDs:      currentNodeIDs,
		WorkflowNodeIDs:     append([]string(nil), currentNodeIDs...),
		NodeTasks:           decodeMaclawAppApprovalNodeTasksFromAny(firstNonEmptyMaclawAppAny(request["node_tasks"], request["nodeTasks"], request["current_node_tasks"], request["currentNodeTasks"], request["approval_tasks"], request["approvalTasks"], request["tasks"], resultPayload["node_tasks"], resultPayload["nodeTasks"], resultPayload["approval_tasks"], resultPayload["approvalTasks"], resultPayload["tasks"])),
		Owner:               firstNonEmptyMaclawAppString(item.SubmittedBy, item.CreatedBy, stringMapValue(request, "submitted_by"), stringMapValue(request, "submittedBy"), stringMapValue(request, "owner"), stringMapValue(request, "applicant")),
		Applicant:           firstNonEmptyMaclawAppString(stringMapValue(request, "applicant"), item.SubmittedBy, item.CreatedBy),
		Approver:            firstNonEmptyMaclawAppString(item.AssignedTo, item.CurrentAssignee, item.ReviewedBy, stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo"), stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "reviewed_by"), stringMapValue(request, "reviewedBy")),
		CurrentAssignee:     firstNonEmptyMaclawAppString(item.CurrentAssignee, item.AssignedTo, stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo")),
		CurrentAssigneeType: firstNonEmptyMaclawAppString(item.CurrentAssigneeType, stringMapValue(request, "current_assignee_type"), stringMapValue(request, "currentAssigneeType"), stringMapValue(request, "assigned_to_type"), stringMapValue(request, "assignedToType")),
		CreatedAt:           maclawAppApprovalTimeString(item.CreatedAt),
		UpdatedAt:           maclawAppApprovalTimeString(item.UpdatedAt),
		Result:              result,
		ApprovalWorkflowID:  firstNonEmptyMaclawAppString(item.ApprovalWorkflowID, stringMapValue(request, "approval_workflow_id"), stringMapValue(request, "approvalWorkflowID"), stringMapValue(request, "workflow_id"), stringMapValue(request, "workflowID")),
		WorkflowSkillID:     firstNonEmptyMaclawAppString(item.WorkflowSkillID, stringMapValue(request, "workflow_skill_id"), stringMapValue(request, "workflowSkillID"), stringMapValue(request, "workflowSkillId")),
		WorkflowVersion:     firstNonEmptyMaclawAppString(item.WorkflowVersion, stringMapValue(request, "workflow_version"), stringMapValue(request, "workflowVersion")),
		BusinessStatus:      firstNonEmptyMaclawAppString(item.BusinessStatus, stringMapValue(request, "business_status"), stringMapValue(request, "businessStatus")),
		ResultStatus:        firstNonEmptyMaclawAppString(item.ResultStatus, stringMapValue(request, "result_status"), stringMapValue(request, "resultStatus")),
		FromStatus:          firstNonEmptyMaclawAppString(item.FromStatus, stringMapValue(request, "from_status"), stringMapValue(request, "fromStatus")),
		ToStatus:            firstNonEmptyMaclawAppString(item.ToStatus, stringMapValue(request, "to_status"), stringMapValue(request, "toStatus")),
		WorkflowDecisionID:  firstNonEmptyMaclawAppString(item.WorkflowDecisionID, stringMapValue(request, "workflow_decision_id"), stringMapValue(request, "workflowDecisionId")),
		RecordID:            item.RecordID,
		ApprovalID:          item.ID,
		RecordApprovalID:    item.ID,
		DetailURL:           firstNonEmptyMaclawAppString(item.DetailURL, stringMapValue(request, "detail_url"), stringMapValue(request, "detailURL"), stringMapValue(request, "detailUrl")),
		BusinessEntity:      firstNonEmptyMaclawAppString(stringMapValue(request, "business_entity"), stringMapValue(request, "businessEntity")),
		BusinessAction:      firstNonEmptyMaclawAppString(stringMapValue(request, "business_action"), stringMapValue(request, "businessAction")),
		BusinessNote:        firstNonEmptyMaclawAppString(stringMapValue(request, "business_note"), stringMapValue(request, "businessNote")),
		ResultPayload:       resultPayload,
		Outputs:             outputs,
		Artifacts:           artifacts,
	}
	if instance.UpdatedAt == "" {
		instance.UpdatedAt = instance.CreatedAt
	}
	return normalizeMaclawAppApprovalInstanceFields(instance)
}

func normalizeMaclawAppApprovalLaneForRecordApproval(item contract.RecordApproval, requestedLane string, status string, currentUserID string) string {
	request := cloneMapAny(item.Request)
	if lane := normalizeMaclawAppApprovalLaneFilter(firstNonEmptyMaclawAppString(stringMapValue(request, "lane"), stringMapValue(request, "approval_lane"), stringMapValue(request, "approvalLane"))); lane != "" && lane != "all" {
		return lane
	}
	if strings.EqualFold(strings.TrimSpace(item.Kind), "attention") || strings.EqualFold(strings.TrimSpace(item.BusinessStatus), "attention") || strings.EqualFold(strings.TrimSpace(item.ResultStatus), "attention") || status == "attention" {
		return "attention"
	}
	currentUserID = strings.TrimSpace(currentUserID)
	if currentUserID != "" {
		submitter := firstNonEmptyMaclawAppString(item.SubmittedBy, item.CreatedBy, stringMapValue(request, "submitted_by"), stringMapValue(request, "submittedBy"), stringMapValue(request, "owner"), stringMapValue(request, "applicant"))
		assignee := firstNonEmptyMaclawAppString(item.CurrentAssignee, item.AssignedTo, stringMapValue(request, "current_assignee"), stringMapValue(request, "currentAssignee"), stringMapValue(request, "assigned_to"), stringMapValue(request, "assignedTo"))
		reviewer := firstNonEmptyMaclawAppString(item.ReviewedBy, stringMapValue(request, "reviewed_by"), stringMapValue(request, "reviewedBy"))
		switch normalizeMaclawAppApprovalLaneFilter(requestedLane) {
		case "pending_my_approval":
			if status == "pending" && maclawAppApprovalActorMatches(currentUserID, assignee) {
				return "pending_my_approval"
			}
		case "handled":
			if status == "approved" || status == "rejected" || status == "failed" || status == "cancelled" || status == "timeout" || maclawAppApprovalActorMatches(currentUserID, reviewer) {
				return "handled"
			}
		case "my_requests":
			if maclawAppApprovalActorMatches(currentUserID, submitter) {
				return "my_requests"
			}
		}
		if maclawAppApprovalActorMatches(currentUserID, submitter) {
			return "my_requests"
		}
		if status == "pending" && maclawAppApprovalActorMatches(currentUserID, assignee) {
			return "pending_my_approval"
		}
		if maclawAppApprovalActorMatches(currentUserID, reviewer) {
			return "handled"
		}
	}
	switch status {
	case "approved", "rejected", "failed", "cancelled", "timeout":
		return "handled"
	default:
		return "pending"
	}
}

func maclawAppApprovalActorMatches(currentUserID string, actor string) bool {
	currentUserID = strings.ToLower(strings.TrimSpace(currentUserID))
	if currentUserID == "" {
		return false
	}
	for _, candidate := range strings.FieldsFunc(strings.ToLower(actor), func(r rune) bool {
		switch r {
		case ',', ';', '|', ' ', '\t', '\n', '\r':
			return true
		default:
			return false
		}
	}) {
		if strings.TrimSpace(candidate) == currentUserID {
			return true
		}
	}
	return false
}

func maclawAppApprovalOutputsFromRecordApprovals(outputs []contract.RecordApprovalOutput) []maclawAppApprovalOutput {
	if len(outputs) == 0 {
		return nil
	}
	out := make([]maclawAppApprovalOutput, 0, len(outputs))
	for _, output := range outputs {
		item := maclawAppApprovalOutput{
			Type:       output.Type,
			Kind:       output.Kind,
			Title:      output.Title,
			Text:       output.Text,
			Status:     output.Status,
			ArtifactID: output.ArtifactID,
			Data:       cloneMapAny(output.Data),
		}
		if output.Artifact != nil {
			artifact := maclawAppApprovalArtifactFromRecordApproval(*output.Artifact)
			item.Artifact = &artifact
		}
		out = append(out, item)
	}
	return out
}

func maclawAppApprovalOutputsFromAny(value any) []maclawAppApprovalOutput {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalOutput
	if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func maclawAppApprovalArtifactsFromRecordApprovals(artifacts []contract.RecordApprovalArtifact) []maclawAppApprovalArtifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]maclawAppApprovalArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		out = append(out, maclawAppApprovalArtifactFromRecordApproval(artifact))
	}
	return out
}

func maclawAppApprovalArtifactsFromAny(value any) []maclawAppApprovalArtifact {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []maclawAppApprovalArtifact
	if err := json.Unmarshal(data, &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func maclawAppApprovalArtifactFromRecordApproval(artifact contract.RecordApprovalArtifact) maclawAppApprovalArtifact {
	return maclawAppApprovalArtifact{
		ID:            artifact.ID,
		URI:           artifact.URI,
		Name:          artifact.Name,
		Path:          artifact.Path,
		MimeType:      artifact.MimeType,
		SizeBytes:     artifact.SizeBytes,
		RemoteURL:     artifact.RemoteURL,
		Checksum:      artifact.Checksum,
		DownloadState: artifact.DownloadState,
		Status:        artifact.Status,
		Presentation:  artifact.Presentation,
	}
}

func mergeMaclawAppApprovalInstanceLists(local, remote []maclawAppApprovalInstance) []maclawAppApprovalInstance {
	merged := make([]maclawAppApprovalInstance, 0, len(local)+len(remote))
	index := map[string]int{}
	add := func(instance maclawAppApprovalInstance, preferIncoming bool) {
		instance = normalizeMaclawAppApprovalInstanceFields(instance)
		keys := maclawAppApprovalInstanceMergeKeys(instance)
		for _, key := range keys {
			if pos, ok := index[key]; ok {
				if preferIncoming {
					merged[pos] = mergeMaclawAppApprovalInstance(merged[pos], instance)
				} else {
					merged[pos] = mergeMaclawAppApprovalInstance(instance, merged[pos])
				}
				for _, nextKey := range maclawAppApprovalInstanceMergeKeys(merged[pos]) {
					index[nextKey] = pos
				}
				return
			}
		}
		pos := len(merged)
		merged = append(merged, instance)
		for _, key := range keys {
			index[key] = pos
		}
	}
	for _, instance := range local {
		add(instance, false)
	}
	for _, instance := range remote {
		add(instance, true)
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return maclawAppApprovalSortTime(merged[i]).After(maclawAppApprovalSortTime(merged[j]))
	})
	out := make([]maclawAppApprovalInstance, len(merged))
	for i, instance := range merged {
		out[i] = cloneMaclawAppApprovalInstance(instance)
	}
	return out
}

func filterMaclawAppApprovalInstancesByLane(instances []maclawAppApprovalInstance, lane string) []maclawAppApprovalInstance {
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	if lane == "" || lane == "all" {
		return instances
	}
	out := make([]maclawAppApprovalInstance, 0, len(instances))
	for _, instance := range instances {
		if maclawAppApprovalInstanceMatchesLane(instance, lane) {
			out = append(out, instance)
		}
	}
	return out
}

func maclawAppApprovalInstanceMatchesLane(instance maclawAppApprovalInstance, lane string) bool {
	lane = normalizeMaclawAppApprovalLaneFilter(lane)
	if lane == "" || lane == "all" {
		return true
	}
	status := normalizeMaclawAppApprovalStatus(instance.Status)
	switch lane {
	case "handled":
		return status == "approved" || status == "rejected" || status == "failed" || status == "cancelled" || status == "timeout" || normalizeMaclawAppApprovalLane(instance.Lane) == "handled"
	case "attention":
		return status == "attention" || normalizeMaclawAppApprovalLane(instance.Lane) == "attention"
	case "pending_my_approval":
		isLocalUnlinked := strings.TrimSpace(firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)) == ""
		return status == "pending" && (normalizeMaclawAppApprovalLane(instance.Lane) == "pending_my_approval" || (isLocalUnlinked && strings.TrimSpace(firstNonEmptyMaclawAppString(instance.CurrentAssignee, instance.Approver)) != ""))
	case "my_requests":
		if status == "requires_input" {
			return true
		}
		return (status == "draft" || status == "pending") && normalizeMaclawAppApprovalLane(instance.Lane) == "my_requests"
	default:
		return normalizeMaclawAppApprovalLane(instance.Lane) == lane
	}
}

func maclawAppApprovalInstanceMergeKeys(instance maclawAppApprovalInstance) []string {
	keys := []string{}
	add := func(prefix string, parts ...string) {
		cleaned := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return
			}
			cleaned = append(cleaned, part)
		}
		keys = append(keys, prefix+":"+strings.Join(cleaned, "|"))
	}
	add("approval", firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID))
	add("workflow", instance.WorkflowSkillID, instance.InstanceID)
	add("instance", instance.AppID, instance.InstanceID)
	add("record", instance.AppID, instance.DatasetID, instance.RecordID)
	return keys
}

func maclawAppApprovalTimeString(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func maclawAppApprovalSortTime(instance maclawAppApprovalInstance) time.Time {
	for _, value := range []string{instance.UpdatedAt, instance.CreatedAt} {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value)); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func maclawAppWorkflowContractForEntry(entry parsedMaclawAppEntry) map[string]any {
	if governance := anyMap(entry.App["governance"]); governance != nil {
		if contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"])); contract != nil {
			return contract
		}
	}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		if contract := anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"])); contract != nil {
			return contract
		}
	}
	return nil
}

func maclawAppApprovalBindingMapsForEntry(entry parsedMaclawAppEntry) []map[string]any {
	out := []map[string]any{}
	for _, holder := range maclawAppBindingHolders(entry) {
		misBlock := anyMap(holder["mis"])
		if misBlock == nil {
			continue
		}
		bindings := anySlice(misBlock["approvalBindings"])
		if len(bindings) == 0 {
			bindings = anySlice(misBlock["approval_bindings"])
		}
		for _, item := range bindings {
			if binding := anyMap(item); binding != nil {
				out = append(out, binding)
			}
		}
	}
	return out
}

func maclawAppWorkflowMappingForEntry(entry parsedMaclawAppEntry) map[string]any {
	for _, holder := range maclawAppBindingHolders(entry) {
		if workflow := anyMap(holder["workflow"]); workflow != nil {
			return workflow
		}
	}
	if governance := anyMap(entry.App["governance"]); governance != nil {
		if workflow := anyMap(governance["workflow"]); workflow != nil {
			return workflow
		}
	}
	return nil
}

func normalizeMaclawAppWorkflowMapping(app map[string]any, kind, path string) error {
	kind = normalizeMaclawAppKind(kind)
	workflow, owner := maclawAppWorkflowMappingBlock(app)
	if kind != "enterprise_approval_app" {
		if workflow != nil {
			return fmt.Errorf("%s.binding.workflow is only valid for enterprise_approval_app", path)
		}
		return nil
	}
	objectRole := "record"
	if binding := anyMap(app["binding"]); binding != nil {
		if datasrv := anyMap(binding["datasrv"]); datasrv != nil {
			objectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "objectRole", "object_role", "domain"), objectRole)
		}
	}
	for _, binding := range maclawAppApprovalBindingMapsForApp(app) {
		objectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "objectRole", "object_role", "businessObjectRole", "business_object_role", "role"), objectRole)
	}
	if workflow == nil {
		binding := anyMap(app["binding"])
		if binding == nil {
			binding = map[string]any{}
			app["binding"] = binding
		}
		binding["workflow"] = defaultMaclawAppWorkflowMapping(objectRole)
		return nil
	}
	if err := normalizeMaclawAppWorkflowMappingDetails(workflow, path+".binding.workflow", objectRole); err != nil {
		return err
	}
	if owner != nil {
		owner["workflow"] = workflow
	}
	return nil
}

func maclawAppWorkflowMappingBlock(app map[string]any) (map[string]any, map[string]any) {
	if binding := anyMap(app["binding"]); binding != nil {
		if workflow := anyMap(binding["workflow"]); workflow != nil {
			return workflow, binding
		}
	}
	if workflow := anyMap(app["workflow"]); workflow != nil {
		return workflow, app
	}
	return nil, nil
}

func maclawAppApprovalBindingMapsForApp(app map[string]any) []map[string]any {
	return maclawAppApprovalBindingMapsForEntry(parsedMaclawAppEntry{App: app})
}

func defaultMaclawAppWorkflowMapping(objectRole string) map[string]any {
	role := strings.TrimSpace(objectRole)
	if role == "" {
		role = "record"
	}
	return map[string]any{
		"schema":        "maclaw.app.workflow.v1",
		"submitNode":    role + ".submit",
		"approvalNode":  role + ".manager_approval",
		"resultNode":    role + ".result_feedback",
		"attentionNode": role + ".attention_review",
		"statusMapping": map[string]any{
			"pending":       "approval_pending",
			"approved":      "approved",
			"rejected":      "rejected",
			"attention":     "attention",
			"requiresInput": "requires_input",
		},
	}
}

func normalizeMaclawAppWorkflowMappingDetails(workflow map[string]any, path string, objectRole string) error {
	if workflow == nil {
		return fmt.Errorf("%s must be an object", path)
	}
	if schema := firstNonEmptyMaclawAppString(maclawAppStringValue(workflow, "schema"), "maclaw.app.workflow.v1"); schema != "maclaw.app.workflow.v1" {
		return fmt.Errorf("%s.schema must be maclaw.app.workflow.v1", path)
	}
	workflow["schema"] = "maclaw.app.workflow.v1"
	defaults := defaultMaclawAppWorkflowMapping(objectRole)
	for _, pair := range []struct{ camel, snake string }{{"submitNode", "submit_node"}, {"approvalNode", "approval_node"}, {"resultNode", "result_node"}, {"attentionNode", "attention_node"}} {
		value := firstNonEmptyMaclawAppString(maclawAppStringValue(workflow, pair.camel), maclawAppStringValue(workflow, pair.snake), maclawAppStringValue(defaults, pair.camel))
		if value == "" {
			return fmt.Errorf("%s.%s is required", path, pair.camel)
		}
		workflow[pair.camel] = value
		delete(workflow, pair.snake)
	}
	statusMapping := anyMap(workflow["statusMapping"])
	if statusMapping == nil {
		statusMapping = anyMap(workflow["status_mapping"])
	}
	if statusMapping == nil {
		statusMapping = map[string]any{}
	}
	defaultStatus := anyMap(defaults["statusMapping"])
	for _, pair := range []struct{ camel, snake string }{{"pending", "pending"}, {"approved", "approved"}, {"rejected", "rejected"}, {"attention", "attention"}, {"requiresInput", "requires_input"}} {
		value := firstNonEmptyMaclawAppString(maclawAppStringValue(statusMapping, pair.camel), maclawAppStringValue(statusMapping, pair.snake), maclawAppStringValue(defaultStatus, pair.camel))
		if value == "" {
			return fmt.Errorf("%s.statusMapping.%s is required", path, pair.camel)
		}
		statusMapping[pair.camel] = value
		if pair.snake != pair.camel {
			delete(statusMapping, pair.snake)
		}
	}
	workflow["statusMapping"] = statusMapping
	delete(workflow, "status_mapping")
	return nil
}

func maclawAppHasWorkflowSkillForEntry(entry parsedMaclawAppEntry) bool {
	if len(maclawAppWorkflowSkillIDsForEntry(entry)) > 0 {
		return true
	}
	for _, holder := range maclawAppBindingHolders(entry) {
		depsBlock := anyMap(holder["dependencies"])
		if depsBlock == nil {
			continue
		}
		for _, item := range anySlice(depsBlock["skills"]) {
			depMap := anyMap(item)
			if depMap == nil {
				continue
			}
			if required, ok := depMap["required"].(bool); ok && !required {
				continue
			}
			if strings.TrimSpace(maclawAppStringValue(depMap, "id")) != "" && strings.TrimSpace(maclawAppStringValue(depMap, "kind")) == "workflow_skill" {
				return true
			}
		}
	}
	return false
}

func maclawAppWorkflowSkillIDsForEntry(entry parsedMaclawAppEntry) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
		workflowSkillID := firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "workflowSkillId", "workflow_skill_id"), maclawAppStringValue(binding, "workflowId", "workflow_id"))
		if workflowSkillID == "" {
			continue
		}
		key := strings.ToLower(workflowSkillID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, workflowSkillID)
	}
	return out
}

func maclawAppWorkflowContractIssuesForEntries(entries []parsedMaclawAppEntry, installed map[string]NLSkillDefinition) []maclawAppReviewIssue {
	issues := []maclawAppReviewIssue{}
	for idx, entry := range entries {
		governance := anyMap(entry.App["governance"])
		appPath := fmt.Sprintf("apps[%d].app", idx)
		if issue := maclawAppWorkflowContractReviewIssue(entry, governance, appPath); issue != nil {
			issues = append(issues, *issue)
		}
		issues = append(issues, maclawAppWorkflowRuntimeContractReviewIssues(entry, installed, appPath)...)
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppWorkflowContractIssuesShouldPrecedeDependencyBlock(issues []maclawAppReviewIssue, hasDependencyBlock bool) bool {
	if len(issues) == 0 {
		return false
	}
	if !hasDependencyBlock {
		return true
	}
	first := strings.ToLower(strings.TrimSpace(firstMaclawAppReviewIssueMessage(issues, "")))
	return first != "" && !strings.Contains(first, "missing approval workflow contract")
}

func maclawAppWorkflowRuntimeContractReviewIssues(entry parsedMaclawAppEntry, installed map[string]NLSkillDefinition, appPath string) []maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" {
		return nil
	}
	snapshot := maclawAppInstallVersionSnapshotForEntry(entry)
	if len(snapshot.ApprovalBindings) == 0 {
		return []maclawAppReviewIssue{{Path: appPath + ".binding.mis.approvalBindings", Severity: "error", Message: "approval app has no approval workflow binding", Suggestion: "declare binding.mis.approvalBindings with event, objectRole, and workflowSkillId"}}
	}
	contract := maclawAppWorkflowContractMapForEntry(entry)
	contractWorkflowID := maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id")
	contractVersion := maclawAppWorkflowContractVersion(contract)
	issues := []maclawAppReviewIssue{}
	for idx, binding := range snapshot.ApprovalBindings {
		path := fmt.Sprintf("%s.binding.mis.approvalBindings[%d]", appPath, idx)
		workflowID := firstNonEmptyMaclawAppString(binding.WorkflowSkillID, contractWorkflowID)
		if workflowID == "" {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".workflowSkillId", Severity: "error", Message: "approval workflow binding is missing workflowSkillId", Suggestion: "bind this approval event to an installed workflow Skill"})
			continue
		}
		match, ok := installed[strings.ToLower(workflowID)]
		if !ok {
			continue
		}
		installedStatus, health := maclawAppInstalledSkillStatus(match)
		if health != "ready" {
			issue := maclawAppReviewIssue{Path: path + ".workflowSkillId", Severity: "error", Message: fmt.Sprintf("approval workflow Skill %s is installed but not active", workflowID), Suggestion: "enable or finish setup for the workflow Skill before running approval instances", Metadata: maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, "", "", binding.Event, binding.ObjectRole, installedStatus, health)}
			if installedStatus != "" {
				issue.Message += fmt.Sprintf(" (status: %s)", installedStatus)
			}
			issues = append(issues, issue)
			continue
		}
		expectedVersion := firstNonEmptyMaclawAppString(binding.WorkflowVersion, contractVersion)
		installedVersion := firstNonEmptyMaclawAppString(match.HubVersion)
		if !maclawAppWorkflowVersionMatches(expectedVersion, installedVersion) {
			issues = append(issues, maclawAppReviewIssue{Path: path + ".workflowVersion", Severity: "error", Message: fmt.Sprintf("approval workflow Skill %s version %s does not match required %s", workflowID, installedVersion, expectedVersion), Suggestion: "install the workflow Skill version declared by the app approval binding or workflow contract", Metadata: maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, expectedVersion, installedVersion, binding.Event, binding.ObjectRole, installedStatus, health)})
		}
	}
	return normalizeMaclawAppReviewIssues(issues)
}

func maclawAppWorkflowRuntimeContractIssueMetadata(workflowID, requiredVersion, installedVersion, event, objectRole, installedStatus, health string) map[string]any {
	metadata := map[string]any{}
	for key, value := range map[string]string{
		"workflow_skill_id": workflowID,
		"required_version":  requiredVersion,
		"installed_version": installedVersion,
		"binding_event":     event,
		"object_role":       objectRole,
		"installed_status":  installedStatus,
		"health":            health,
	} {
		if value := strings.TrimSpace(value); value != "" {
			metadata[key] = value
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func maclawAppWorkflowContractMapForEntry(entry parsedMaclawAppEntry) map[string]any {
	governance := anyMap(entry.App["governance"])
	contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"]))
	if contract != nil {
		return contract
	}
	if binding := anyMap(entry.App["binding"]); binding != nil {
		return anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"]))
	}
	return nil
}

func maclawAppWorkflowContractVersion(contract map[string]any) string {
	if contract == nil {
		return ""
	}
	return maclawAppVersionString(firstNonEmptyMaclawAppAny(contract["workflowVersion"], contract["workflow_version"], contract["version"], contract["versionConstraint"], contract["version_constraint"]))
}

func maclawAppWorkflowVersionMatches(required, installed string) bool {
	required = strings.TrimSpace(required)
	installed = strings.TrimSpace(installed)
	if required == "" || installed == "" {
		return true
	}
	if strings.EqualFold(required, installed) {
		return true
	}
	if strings.ContainsAny(required, "<>=^~*") {
		return true
	}
	return false
}

func maclawAppWorkflowContractReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" {
		return nil
	}
	contract := anyMap(firstNonEmptyMaclawAppAny(governance["workflowContract"], governance["workflow_contract"]))
	if contract == nil {
		if binding := anyMap(entry.App["binding"]); binding != nil {
			contract = anyMap(firstNonEmptyMaclawAppAny(binding["workflowContract"], binding["workflow_contract"]))
		}
	}
	if contract == nil {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract", Severity: "error", Message: "missing approval workflow contract", Suggestion: "declare the workflow skill input, decision output, and status mapping contract before submitting"}
	}
	if strings.TrimSpace(maclawAppStringValue(contract, "schema")) != "maclaw.app.workflow_contract.v1" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract", Severity: "error", Message: "invalid approval workflow contract schema", Suggestion: "set workflowContract.schema to maclaw.app.workflow_contract.v1"}
	}
	workflowSkillID := strings.TrimSpace(maclawAppStringValue(contract, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"))
	if workflowSkillID == "" || !maclawAppWorkflowContractMatchesWorkflowSkill(entry, workflowSkillID) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.workflowSkillId", Severity: "error", Message: "approval workflow contract does not match approval binding", Suggestion: "use a workflowSkillId declared by approvalBindings or workflow_skill dependencies"}
	}
	objectRole := strings.TrimSpace(maclawAppStringValue(contract, "objectRole", "object_role", "businessObjectRole", "business_object_role"))
	if objectRole == "" || !maclawAppWorkflowContractMatchesObjectRole(entry, objectRole) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.objectRole", Severity: "error", Message: "approval workflow contract object role does not match app binding", Suggestion: "align workflowContract.objectRole with approvalBindings.objectRole or binding.datasrv.objectRole"}
	}
	for _, required := range []string{"record_ref", "applicant", "business_payload"} {
		if !maclawAppStringListContains(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(contract["requiredInputs"], contract["required_inputs"])), required) {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.requiredInputs", Severity: "error", Message: "approval workflow contract is missing required input: " + required, Suggestion: "include record_ref, applicant, and business_payload in workflowContract.requiredInputs"}
		}
	}
	for _, required := range []string{"approved", "rejected", "attention"} {
		if !maclawAppStringListContains(maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(contract["decisionOutputs"], contract["decision_outputs"])), required) {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.decisionOutputs", Severity: "error", Message: "approval workflow contract is missing decision output: " + required, Suggestion: "include approved, rejected, and attention in workflowContract.decisionOutputs"}
		}
	}
	statusMapping := anyMap(firstNonEmptyMaclawAppAny(contract["statusMapping"], contract["status_mapping"]))
	for _, required := range []string{"pending", "approved", "rejected", "attention"} {
		if strings.TrimSpace(maclawAppStringValue(statusMapping, required)) == "" {
			return &maclawAppReviewIssue{Path: appPath + ".governance.workflowContract.statusMapping", Severity: "error", Message: "approval workflow contract is missing status mapping: " + required, Suggestion: "map pending, approved, rejected, and attention workflow states to app statuses"}
		}
	}
	return nil
}

func maclawAppWorkflowContractMatchesWorkflowSkill(entry parsedMaclawAppEntry, workflowSkillID string) bool {
	needle := strings.ToLower(strings.TrimSpace(workflowSkillID))
	if needle == "" {
		return false
	}
	for _, id := range maclawAppWorkflowSkillIDsForEntry(entry) {
		if strings.ToLower(strings.TrimSpace(id)) == needle {
			return true
		}
	}
	for _, dep := range maclawAppDependenciesForEntry(entry) {
		if strings.TrimSpace(dep.Kind) == "workflow_skill" && strings.ToLower(strings.TrimSpace(dep.ID)) == needle {
			return true
		}
	}
	return false
}

func maclawAppWorkflowContractMatchesObjectRole(entry parsedMaclawAppEntry, objectRole string) bool {
	needle := strings.ToLower(strings.TrimSpace(objectRole))
	if needle == "" {
		return false
	}
	for _, binding := range maclawAppApprovalBindingMapsForEntry(entry) {
		if strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(maclawAppStringValue(binding, "objectRole", "object_role"), maclawAppStringValue(binding, "businessObjectRole", "business_object_role"), maclawAppStringValue(binding, "role")))) == needle {
			return true
		}
	}
	if datasrv := maclawAppDataSrvBlockForEntry(entry); datasrv != nil {
		if strings.ToLower(strings.TrimSpace(firstNonEmptyMaclawAppString(maclawAppStringValue(datasrv, "objectRole", "object_role"), maclawAppStringValue(datasrv, "businessObjectRole", "business_object_role"), maclawAppStringValue(datasrv, "domain")))) == needle {
			return true
		}
	}
	return false
}

func maclawAppApprovalInstanceTestEvidenceReviewIssue(entry parsedMaclawAppEntry, governance map[string]any, appPath string) *maclawAppReviewIssue {
	if normalizeMaclawAppKind(entry.Kind) != "enterprise_approval_app" || governance == nil || !maclawAppHasPublishableTestEvidence(governance) {
		return nil
	}
	testEvidence := maclawAppTestEvidenceMap(governance)
	if testEvidence == nil {
		return nil
	}
	instance := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalInstance"], testEvidence["approval_instance"], testEvidence["approval"]))
	instanceID := firstNonEmptyMaclawAppString(
		maclawAppStringValue(instance, "instanceId", "instance_id", "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
		maclawAppStringValue(testEvidence, "approvalInstanceId", "approval_instance_id", "workflowInstanceId", "workflow_instance_id"),
	)
	if instanceID == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance", Severity: "error", Message: "approval app test evidence is missing a created approval instance", Suggestion: "run the approval app in App Studio, create a test approval instance, and save its approval_instance_id in test evidence"}
	}
	status := firstNonEmptyMaclawAppString(
		maclawAppStringValue(instance, "status", "approvalStatus", "approval_status", "resultStatus", "result_status"),
		maclawAppStringValue(testEvidence, "approvalStatus", "approval_status", "resultStatus", "result_status"),
	)
	if status == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.status", Severity: "error", Message: "approval app test evidence is missing approval instance status", Suggestion: "save the status observed from the approval instance view after the App Studio test run"}
	}
	if currentNode := maclawAppStringValue(instance, "currentNode", "current_node"); currentNode == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.currentNode", Severity: "error", Message: "approval app test evidence is missing the current workflow node", Suggestion: "save the final current_node observed from the approval workflow instance after the App Studio test run"}
	}
	if workflowSkillID := maclawAppStringValue(instance, "workflowSkillId", "workflow_skill_id", "workflowId", "workflow_id"); workflowSkillID == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.workflowSkillId", Severity: "error", Message: "approval app test evidence is missing the workflow Skill id", Suggestion: "save workflowSkillId/workflow_skill_id in approvalInstance evidence so the market can verify the approval workflow dependency"}
	}
	if resultStatus := firstNonEmptyMaclawAppString(maclawAppStringValue(instance, "resultStatus", "result_status"), maclawAppStringValue(instance, "businessStatus", "business_status")); resultStatus == "" {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.resultStatus", Severity: "error", Message: "approval app test evidence is missing business/result status", Suggestion: "save businessStatus/resultStatus from the completed approval workflow instance"}
	}
	if !maclawAppApprovalInstanceHasResultPackage(instance) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalInstance.resultPayload", Severity: "error", Message: "approval app test evidence is missing the approval result package", Suggestion: "save resultPayload or outputs on approvalInstance after the approval workflow completes and DataSrv sync has been attempted"}
	}
	if !maclawAppApprovalInstanceViewVerified(testEvidence, instance) {
		return &maclawAppReviewIssue{Path: appPath + ".governance.testEvidence.approvalViews", Severity: "error", Message: "approval app test evidence has not verified the approval instance view", Suggestion: "open the generated approval app instance list and save view verification for my_requests, pending_my_approval, handled, or attention"}
	}
	return nil
}

func maclawAppApprovalInstanceHasResultPackage(instance map[string]any) bool {
	if instance == nil {
		return false
	}
	if payload := anyMap(firstNonEmptyMaclawAppAny(instance["resultPayload"], instance["result_payload"])); len(payload) > 0 {
		return true
	}
	if outputs := anySlice(instance["outputs"]); len(outputs) > 0 {
		return true
	}
	if artifacts := anySlice(instance["artifacts"]); len(artifacts) > 0 {
		return true
	}
	return false
}

func maclawAppApprovalInstanceViewVerified(testEvidence map[string]any, instance map[string]any) bool {
	if maclawAppBoolValue(testEvidence, "approvalInstanceViewVerified", "approval_instance_view_verified", "approvalViewVerified", "approval_view_verified") || maclawAppBoolValue(instance, "viewVerified", "view_verified") {
		return true
	}
	views := anyMap(firstNonEmptyMaclawAppAny(testEvidence["approvalViews"], testEvidence["approval_views"], testEvidence["instanceViews"], testEvidence["instance_views"], instance["views"]))
	if views == nil {
		return false
	}
	if maclawAppBoolValue(views, "verified", "ok") {
		return true
	}
	for _, lane := range []string{"my_requests", "pending_my_approval", "handled", "attention"} {
		if value, ok := views[lane]; ok && value != nil {
			return true
		}
	}
	return false
}

func normalizeMaclawAppApprovalCurrentNodes(currentNode string, currentNodeIDs []string) (string, []string) {
	primary := strings.TrimSpace(currentNode)
	seen := map[string]struct{}{}
	nodes := make([]string, 0, len(currentNodeIDs)+1)
	for _, node := range currentNodeIDs {
		node = strings.TrimSpace(node)
		if node == "" {
			continue
		}
		key := strings.ToLower(node)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		nodes = append(nodes, node)
	}
	if primary != "" {
		key := strings.ToLower(primary)
		if _, ok := seen[key]; !ok {
			nodes = append([]string{primary}, nodes...)
		}
	}
	if primary == "" && len(nodes) > 0 {
		primary = nodes[0]
	}
	if len(nodes) == 0 && primary != "" {
		nodes = []string{primary}
	}
	return primary, nodes
}

func (a *App) maclawAppApprovalRegistryPath() string {
	return filepath.Join(a.GetDataDir(), "app_approval_instances.json")
}

func (a *App) readMaclawAppApprovalRegistry() (maclawAppApprovalRegistry, error) {
	path := a.maclawAppApprovalRegistryPath()
	registry := maclawAppApprovalRegistry{Schema: "maclaw.app.approvals.v1"}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return registry, nil
	}
	if err != nil {
		return registry, err
	}
	if len(data) == 0 {
		return registry, nil
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return registry, fmt.Errorf("decode maclaw app approval registry: %w", err)
	}
	if registry.Schema == "" {
		registry.Schema = "maclaw.app.approvals.v1"
	}
	return registry, nil
}

func (a *App) writeMaclawAppApprovalRegistry(registry maclawAppApprovalRegistry) error {
	path := a.maclawAppApprovalRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeMaclawAppApprovalInstanceFields(instance maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance.AppID = strings.TrimSpace(instance.AppID)
	instance.AppName = strings.TrimSpace(instance.AppName)
	instance.BlueprintID = strings.TrimSpace(instance.BlueprintID)
	instance.DatasetID = strings.TrimSpace(instance.DatasetID)
	instance.ObjectRole = strings.TrimSpace(instance.ObjectRole)
	instance.ApprovalObjectRole = strings.TrimSpace(instance.ApprovalObjectRole)
	instance.ObjectRole = firstNonEmptyMaclawAppString(instance.ObjectRole, instance.ApprovalObjectRole)
	instance.ApprovalObjectRole = firstNonEmptyMaclawAppString(instance.ApprovalObjectRole, instance.ObjectRole)
	instance.ApprovalEvent = strings.TrimSpace(instance.ApprovalEvent)
	instance.ApprovalWorkflowID = strings.TrimSpace(instance.ApprovalWorkflowID)
	instance.ApprovalWorkflowID = firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID)
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	instance.Title = strings.TrimSpace(instance.Title)
	instance.Lane = strings.TrimSpace(instance.Lane)
	instance.Status = strings.TrimSpace(instance.Status)
	instance.CurrentNode, instance.CurrentNodeIDs = normalizeMaclawAppApprovalCurrentNodes(instance.CurrentNode, firstNonEmptyMaclawAppStringList(instance.CurrentNodeIDs, instance.WorkflowNodeIDs))
	instance.CurrentNodeStatus = strings.TrimSpace(instance.CurrentNodeStatus)
	_, instance.WorkflowNodeIDs = normalizeMaclawAppApprovalCurrentNodes(instance.CurrentNode, firstNonEmptyMaclawAppStringList(instance.WorkflowNodeIDs, instance.CurrentNodeIDs))
	instance.Owner = strings.TrimSpace(instance.Owner)
	instance.Applicant = strings.TrimSpace(instance.Applicant)
	instance.Owner = firstNonEmptyMaclawAppString(instance.Owner, instance.Applicant)
	instance.Applicant = firstNonEmptyMaclawAppString(instance.Applicant, instance.Owner)
	instance.Approver = strings.TrimSpace(instance.Approver)
	instance.CurrentAssignee = strings.TrimSpace(instance.CurrentAssignee)
	instance.CurrentAssignee = firstNonEmptyMaclawAppString(instance.CurrentAssignee, instance.Approver)
	instance.CurrentAssigneeType = strings.TrimSpace(instance.CurrentAssigneeType)
	if instance.CurrentAssigneeType == "" && instance.CurrentAssignee != "" {
		instance.CurrentAssigneeType = "user"
	}
	instance.CreatedAt = strings.TrimSpace(instance.CreatedAt)
	instance.UpdatedAt = strings.TrimSpace(instance.UpdatedAt)
	instance.Result = strings.TrimSpace(instance.Result)
	instance.WorkflowSkillID = strings.TrimSpace(instance.WorkflowSkillID)
	instance.ApprovalWorkflowID = firstNonEmptyMaclawAppString(instance.ApprovalWorkflowID, instance.WorkflowSkillID)
	instance.WorkflowVersion = strings.TrimSpace(instance.WorkflowVersion)
	instance.BusinessStatus = strings.TrimSpace(instance.BusinessStatus)
	instance.ResultStatus = strings.TrimSpace(instance.ResultStatus)
	instance.FromStatus = strings.TrimSpace(instance.FromStatus)
	instance.ToStatus = strings.TrimSpace(instance.ToStatus)
	instance.ToStatus = firstNonEmptyMaclawAppString(instance.ToStatus, instance.BusinessStatus, instance.Status)
	instance.WorkflowDecisionID = strings.TrimSpace(instance.WorkflowDecisionID)
	instance.RecordID = strings.TrimSpace(instance.RecordID)
	instance.ApprovalID = strings.TrimSpace(instance.ApprovalID)
	instance.RecordApprovalID = strings.TrimSpace(instance.RecordApprovalID)
	instance.ApprovalID = firstNonEmptyMaclawAppString(instance.ApprovalID, instance.RecordApprovalID)
	instance.RecordApprovalID = firstNonEmptyMaclawAppString(instance.RecordApprovalID, instance.ApprovalID)
	instance.DetailURL = strings.TrimSpace(instance.DetailURL)
	instance.BusinessEntity = strings.TrimSpace(instance.BusinessEntity)
	instance.BusinessAction = strings.TrimSpace(instance.BusinessAction)
	instance.BusinessNote = strings.TrimSpace(instance.BusinessNote)
	return instance
}

func mergeMaclawAppApprovalInstance(existing, incoming maclawAppApprovalInstance) maclawAppApprovalInstance {
	if normalizeMaclawAppApprovalStatus(incoming.Status) == "pending" && normalizeMaclawAppApprovalLane(existing.Lane) == "pending_my_approval" && normalizeMaclawAppApprovalLane(incoming.Lane) == "my_requests" {
		incoming.Lane = existing.Lane
	}
	incoming.AppName = firstNonEmptyMaclawAppString(incoming.AppName, existing.AppName)
	incoming.BlueprintID = firstNonEmptyMaclawAppString(incoming.BlueprintID, existing.BlueprintID)
	incoming.DatasetID = firstNonEmptyMaclawAppString(incoming.DatasetID, existing.DatasetID)
	incoming.ObjectRole = firstNonEmptyMaclawAppString(incoming.ObjectRole, existing.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalObjectRole = firstNonEmptyMaclawAppString(incoming.ApprovalObjectRole, incoming.ObjectRole, existing.ApprovalObjectRole)
	incoming.ApprovalEvent = firstNonEmptyMaclawAppString(incoming.ApprovalEvent, existing.ApprovalEvent)
	incoming.ApprovalWorkflowID = firstNonEmptyMaclawAppString(incoming.ApprovalWorkflowID, existing.ApprovalWorkflowID, incoming.WorkflowSkillID, existing.WorkflowSkillID)
	incoming.Owner = firstNonEmptyMaclawAppString(incoming.Owner, existing.Owner, existing.Applicant)
	incoming.Applicant = firstNonEmptyMaclawAppString(incoming.Applicant, incoming.Owner, existing.Applicant)
	incoming.Approver = firstNonEmptyMaclawAppString(incoming.Approver, existing.Approver)
	incoming.CurrentAssignee = firstNonEmptyMaclawAppString(incoming.CurrentAssignee, existing.CurrentAssignee, incoming.Approver, existing.Approver)
	incoming.CurrentAssigneeType = firstNonEmptyMaclawAppString(incoming.CurrentAssigneeType, existing.CurrentAssigneeType)
	incoming.CurrentNodeStatus = firstNonEmptyMaclawAppString(incoming.CurrentNodeStatus, existing.CurrentNodeStatus)
	incoming.CreatedAt = firstNonEmptyMaclawAppString(incoming.CreatedAt, existing.CreatedAt)
	incoming.WorkflowSkillID = firstNonEmptyMaclawAppString(incoming.WorkflowSkillID, existing.WorkflowSkillID)
	incoming.WorkflowVersion = firstNonEmptyMaclawAppString(incoming.WorkflowVersion, existing.WorkflowVersion)
	incoming.FromStatus = firstNonEmptyMaclawAppString(incoming.FromStatus, existing.FromStatus)
	incoming.ToStatus = firstNonEmptyMaclawAppString(incoming.ToStatus, existing.ToStatus, incoming.BusinessStatus, incoming.Status)
	incoming.WorkflowDecisionID = firstNonEmptyMaclawAppString(incoming.WorkflowDecisionID, existing.WorkflowDecisionID)
	incoming.RecordID = firstNonEmptyMaclawAppString(incoming.RecordID, existing.RecordID)
	incoming.ApprovalID = firstNonEmptyMaclawAppString(incoming.ApprovalID, existing.ApprovalID, existing.RecordApprovalID)
	incoming.RecordApprovalID = firstNonEmptyMaclawAppString(incoming.RecordApprovalID, incoming.ApprovalID, existing.RecordApprovalID)
	incoming.DetailURL = firstNonEmptyMaclawAppString(incoming.DetailURL, existing.DetailURL)
	incoming.BusinessEntity = firstNonEmptyMaclawAppString(incoming.BusinessEntity, existing.BusinessEntity)
	incoming.BusinessAction = firstNonEmptyMaclawAppString(incoming.BusinessAction, existing.BusinessAction)
	incoming.BusinessNote = firstNonEmptyMaclawAppString(incoming.BusinessNote, existing.BusinessNote)
	if incoming.ResultPayload == nil {
		incoming.ResultPayload = cloneMapAny(existing.ResultPayload)
	}
	if len(incoming.Outputs) == 0 {
		incoming.Outputs = cloneMaclawAppApprovalOutputs(existing.Outputs)
	}
	if len(incoming.Artifacts) == 0 {
		incoming.Artifacts = append([]maclawAppApprovalArtifact(nil), existing.Artifacts...)
	}
	if len(incoming.NodeTasks) == 0 {
		incoming.NodeTasks = cloneMaclawAppMapSlice(existing.NodeTasks)
	}
	if len(incoming.Events) == 0 {
		incoming.Events = append([]maclawAppApprovalEvent(nil), existing.Events...)
	}
	return normalizeMaclawAppApprovalInstanceFields(incoming)
}

func normalizeMaclawAppApprovalLane(lane string) string {
	lane = strings.ToLower(strings.TrimSpace(lane))
	switch lane {
	case "pending", "pending_my_approval", "handled", "attention":
		return lane
	default:
		return "my_requests"
	}
}

func normalizeMaclawAppApprovalLaneFilter(lane string) string {
	lane = strings.ToLower(strings.TrimSpace(lane))
	switch lane {
	case "my_requests", "pending_my_approval", "handled", "attention", "all":
		return lane
	default:
		return "all"
	}
}

func normalizeMaclawAppApprovalStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "draft", "pending", "approved", "rejected", "attention", "failed", "cancelled", "timeout", "requires_input":
		return status
	default:
		return "pending"
	}
}

func normalizeMaclawAppApprovalStatusForRecordApproval(item contract.RecordApproval) string {
	if strings.TrimSpace(item.Kind) == "attention" || strings.TrimSpace(item.BusinessStatus) == "attention" || strings.TrimSpace(item.ResultStatus) == "attention" {
		return "attention"
	}
	return normalizeMaclawAppApprovalStatus(firstNonEmptyMaclawAppString(item.Decision, item.Status, item.ResultStatus, item.BusinessStatus))
}

func cloneMaclawAppApprovalInstance(instance maclawAppApprovalInstance) maclawAppApprovalInstance {
	instance.CurrentNodeIDs = append([]string(nil), instance.CurrentNodeIDs...)
	instance.WorkflowNodeIDs = append([]string(nil), instance.WorkflowNodeIDs...)
	instance.NodeTasks = cloneMaclawAppMapSlice(instance.NodeTasks)
	instance.Events = append([]maclawAppApprovalEvent(nil), instance.Events...)
	instance.ResultPayload = cloneMapAny(instance.ResultPayload)
	instance.Outputs = cloneMaclawAppApprovalOutputs(instance.Outputs)
	instance.Artifacts = append([]maclawAppApprovalArtifact(nil), instance.Artifacts...)
	return instance
}

func cloneMaclawAppApprovalOutputs(outputs []maclawAppApprovalOutput) []maclawAppApprovalOutput {
	if len(outputs) == 0 {
		return nil
	}
	cloned := make([]maclawAppApprovalOutput, len(outputs))
	for i, output := range outputs {
		cloned[i] = output
		cloned[i].Data = cloneMapAny(output.Data)
		if output.Artifact != nil {
			artifact := *output.Artifact
			cloned[i].Artifact = &artifact
		}
	}
	return cloned
}
