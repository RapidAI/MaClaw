package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

type misAgentViewDraft struct {
	ActionID      string
	TransactionID string
	Data          map[string]interface{}
	CreatedAt     time.Time
}

var misAgentViewDraftStore = struct {
	sync.Mutex
	next  int64
	items map[string]misAgentViewDraft
}{items: map[string]misAgentViewDraft{}}

// misDataHTTPClient is a shared HTTP client for MIS data service calls.
// Reusing a single client enables TCP connection keep-alive and avoids
// per-request TLS handshake overhead. Per-request deadlines are controlled
// via context.WithTimeout on individual requests.
var misDataHTTPClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     90 * time.Second,
	},
}

func (h *IMMessageHandler) toolMISData(args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return "MIS data tool is not initialized"
	}
	return h.app.executeMISDataTool(args)
}

type AgentViewSubmitPayload struct {
	ViewID    string                 `json:"view_id"`
	Data      map[string]interface{} `json:"data"`
	RequestID string                 `json:"request_id,omitempty"`
}

type AgentViewDismissPayload struct {
	ViewID string                 `json:"view_id"`
	Data   map[string]interface{} `json:"data,omitempty"`
}

func (a *App) SubmitAgentView(payload AgentViewSubmitPayload) (*IMAgentResponse, error) {
	resp := a.handleAgentViewSubmitPayload(payload)
	normalizeArtifactResponseSource(resp)
	return resp, nil
}

func (a *App) DismissAgentView(payload AgentViewDismissPayload) (*IMAgentResponse, error) {
	// When a workflow form is dismissed, mark the form as skipped so the
	// engine doesn't re-show it on the next HandleInput call.
	if phaseID, ok := strings.CutPrefix(strings.TrimSpace(payload.ViewID), "workflow:form:"); ok {
		phaseID = strings.TrimSpace(phaseID)

		// If the user clicked "Cancel Workflow" button, cancel the entire workflow
		// instead of just skipping the form.
		if cancelWF, _ := payload.Data["__cancel_workflow"].(bool); cancelWF {
			workflowID := workflowFormStringField(payload.Data, workflowFormWorkflowIDField)
			userID := workflowFormStringField(payload.Data, workflowFormUserIDField)
			if userID == "" {
				userID = a.workflowOwnerIDForCurrentProject()
			}
			if workflowID != "" {
				matchesActive := true
				switch {
				case a.workflowEngine != nil:
					matchesActive = workflowFormMatchesActiveWorkflow(a.workflowEngine, userID, phaseID, payload.Data)
				case a.workflowV2 != nil && a.workflowV2.machine != nil:
					matchesActive = workflowFormMatchesActiveWorkflowV2(a.workflowV2.machine, userID, phaseID, payload.Data)
				}
				if !matchesActive {
					resp := &IMAgentResponse{
						Text:           avTr("This workflow is no longer active.", "当前工作流已失效或已切换，未执行取消。"),
						ResponseSource: imResponseSourceAgentViewDismiss.String(),
					}
					normalizeArtifactResponseSource(resp)
					return resp, nil
				}
			}
			a.clearAgentView(payload.ViewID)
			if h := a.ensureLocalIMHandler(); h != nil {
				h.cancelWorkflowForUser(userID)
				if _, err := h.CancelSessionForUser(userID); err != nil {
					// Workflow cancellation should still establish a fresh-task boundary
					// even when there is no active foreground loop to stop.
					h.markTaskCancelledByUser(userID)
				}
			} else {
				if a.workflowEngine != nil {
					_ = a.workflowEngine.CancelWorkflow(userID)
				}
				if a.workflowV2 != nil && a.workflowV2.machine != nil {
					a.workflowV2.machine.Cancel(userID)
				}
			}
			resp := &IMAgentResponse{
				Text:           avTr("Workflow cancelled. Describe your task again to start a new workflow.", "工作流已取消。如需重新开始，请直接描述您的任务。"),
				ResponseSource: imResponseSourceAgentViewDismiss.String(),
			}
			normalizeArtifactResponseSource(resp)
			return resp, nil
		}

		if userID := workflowFormStringField(payload.Data, workflowFormUserIDField); userID != "" && a.workflowEngine != nil {
			if workflowFormMatchesActiveWorkflow(a.workflowEngine, userID, phaseID, payload.Data) {
				if err := a.workflowEngine.SkipPhaseForm(userID); err != nil {
					resp := &IMAgentResponse{
						Text:           avTr("Failed to close the task panel.", "关闭任务面板失败。"),
						Error:          err.Error(),
						ResponseSource: imResponseSourceAgentViewDismiss.String(),
					}
					normalizeArtifactResponseSource(resp)
					return resp, err
				}
			}
		}

		workflowLifecyclePayload := workflowFormLifecyclePayloadFor("", phaseID, "", payload.Data)
		a.clearAgentViewWithPayload(payload.ViewID, workflowLifecyclePayload)
	} else {
		a.clearAgentView(payload.ViewID)
	}

	resp := &IMAgentResponse{Text: avTr("Task panel closed.", "任务面板已关闭。"), ResponseSource: imResponseSourceAgentViewDismiss.String()}
	normalizeArtifactResponseSource(resp)
	return resp, nil
}

func (a *App) handleAgentViewControlMessage(text string) (*IMAgentResponse, bool, error) {
	control := classifyAgentViewControlMessage(text)
	if control.Kind == agentViewControlMessageDismiss {
		var payload AgentViewDismissPayload
		if err := json.Unmarshal([]byte(control.Raw), &payload); err != nil {
			return &IMAgentResponse{Text: avTr("Invalid task panel dismissal.", "任务面板关闭请求无效。"), Error: err.Error(), ResponseSource: imResponseSourceAgentViewDismiss.String()}, true, nil
		}
		resp, err := a.DismissAgentView(payload)
		return resp, true, err
	}
	if control.Kind != agentViewControlMessageSubmit {
		return nil, false, nil
	}
	var payload AgentViewSubmitPayload
	if err := json.Unmarshal([]byte(control.Raw), &payload); err != nil {
		return &IMAgentResponse{Text: avTr("Invalid task panel submission.", "任务面板提交无效。"), Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}, true, nil
	}
	return a.handleAgentViewSubmitPayload(payload), true, nil
}

func (a *App) handleAgentViewSubmitPayload(payload AgentViewSubmitPayload) (resp *IMAgentResponse) {
	a.emitAgentViewLifecycle(agentViewLifecycleSubmit, map[string]interface{}{
		"view_id": payload.ViewID,
		"data":    payload.Data,
	})
	startSeq := a.agentViewSeq()
	defer func() {
		if resp == nil {
			resp = &IMAgentResponse{Text: avTr("Task panel submission failed.", "任务面板提交失败。"), Error: "empty agent view response", ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		if a.agentViewSeq() != startSeq {
			return
		}
		lifecyclePayload := map[string]interface{}{
			"view_id": payload.ViewID,
		}
		for key, value := range workflowFormLifecyclePayload(payload.Data) {
			lifecyclePayload[key] = value
		}
		if strings.TrimSpace(resp.Error) != "" {
			lifecyclePayload["error"] = resp.Error
			a.emitAgentViewLifecycle(agentViewLifecycleError, lifecyclePayload)
			return
		}
		a.emitAgentViewLifecycle(agentViewLifecycleComplete, lifecyclePayload)
	}()
	a.ensureMISBusinessTransactionsLoaded()
	viewID := classifyMISAgentViewID(payload.ViewID)
	switch viewID.Kind {
	case misAgentViewIDSkillRun:
		return a.handleSkillRunAgentViewSubmit(viewID.Arg, payload.Data)
	case misAgentViewIDSkillStatus:
		return a.handleSkillStatusAgentViewSubmit(payload.Data)
	case misAgentViewIDToolApproval:
		hubClient := a.ensureHubClient()
		if hubClient == nil {
			return &IMAgentResponse{Text: avTr("AI assistant is not initialized.", "AI 助手尚未初始化。"), Error: "missing hub client", ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		return hubClient.ensureIMHandler().handleRegisteredToolApprovalAgentViewSubmit(payload.Data)
	case misAgentViewIDToolRun:
		hubClient := a.ensureHubClient()
		if hubClient == nil {
			return &IMAgentResponse{Text: avTr("AI assistant is not initialized.", "AI 助手尚未初始化。"), Error: "missing hub client", ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		return hubClient.ensureIMHandler().handleRegisteredToolAgentViewSubmit(viewID.Arg, payload.Data)
	case misAgentViewIDMCPCall:
		hubClient := a.ensureHubClient()
		if hubClient == nil {
			return &IMAgentResponse{Text: avTr("AI assistant is not initialized.", "AI 助手尚未初始化。"), Error: "missing hub client", ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		return hubClient.ensureIMHandler().handleMCPToolAgentViewSubmit(payload.Data)
	case misAgentViewIDChooseIntent:
		return a.handleMISIntentChoiceAgentViewSubmit(payload.Data)
	case misAgentViewIDResumeTransaction:
		return a.handleMISTransactionResumeAgentViewSubmit(payload.Data)
	case misAgentViewIDCommit:
		return a.handleMISCommitAgentViewSubmit(viewID.Arg, payload.Data)
	case misAgentViewIDIntent:
		return a.handleMISIntentAgentViewSubmit(viewID.Arg, payload.Data)
	case misAgentViewIDWorkflowForm:
		return a.handleWorkflowFormAgentViewSubmit(viewID.Arg, payload.Data, payload.RequestID)
	default:
		return &IMAgentResponse{Text: avTr("Task panel submission received.", "任务面板提交已收到。"), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
}

// handleMISIntentAgentViewSubmit handles business object form submissions:
// sanitize → create/reuse transaction → dry-run → emit result view.
func (a *App) handleMISIntentAgentViewSubmit(actionID string, data map[string]interface{}) *IMAgentResponse {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return &IMAgentResponse{Text: avTr("Task panel submission received.", "任务面板提交已收到。"), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	submittedData := sanitizeMISAgentViewSubmittedData(data)
	transactionID := ensureMISBusinessTransaction(extractMISAgentViewTransactionID(data), actionID, "", "", "", "", submittedData, imResponseSourceAgentViewSubmit.String())
	updateMISBusinessTransactionFields(transactionID, submittedData, "user_input", true, 1)
	result, err := a.executeMISBusinessActionBytes(actionID, submittedData, true)
	if err != nil {
		markMISBusinessTransaction(transactionID, misTransactionStateAwaitingValidation.String(), "business_action.validation_unavailable", err.Error(), map[string]interface{}{"reason": err.Error()})
		if view := buildMISBusinessActionPendingValidationAgentView(actionID, submittedData, transactionID, err); view != nil {
			a.emitAgentView(view)
		}
		a.saveMISBusinessTransactions()
		return &IMAgentResponse{Text: "Structured business data was saved locally and is waiting for MIS validation.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	markMISBusinessTransaction(transactionID, misTransactionStateValidating.String(), "business_action.dry_run_completed", "Structured business data dry-run completed.", nil)
	if view := buildMISBusinessActionDryRunAgentViewWithTransaction(actionID, submittedData, result, transactionID); view != nil {
		a.emitAgentView(view)
	}
	a.saveMISBusinessTransactions()
	return &IMAgentResponse{
		Text:           "Structured data dry-run completed. Review the result in the task panel.",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

func (a *App) handleMISIntentChoiceAgentViewSubmit(data map[string]interface{}) *IMAgentResponse {
	a.ensureMISBusinessTransactionsLoaded()
	actionID := strings.TrimSpace(fmt.Sprint(data["business_action_id"]))
	if actionID == "" {
		return &IMAgentResponse{Text: "Choose a business task before continuing.", Error: "missing business_action_id", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	var action contract.BusinessAction
	cfg, cfgErr := a.GetMISDataConfig()
	var actionLoadErr error
	if cfgErr == nil && cfg.Enabled && strings.TrimSpace(cfg.Token) != "" {
		result, err := a.callMISDataAPIBytes(cfg, http.MethodGet, "/api/v1/data/business-actions/"+pathEscape(actionID), nil)
		if err == nil {
			if err := json.Unmarshal(result, &action); err != nil {
				return &IMAgentResponse{Text: "Decode business action failed.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
			}
		} else {
			actionLoadErr = err
		}
	}
	if strings.TrimSpace(action.ID) == "" {
		if snapshots, ok := data["business_action_snapshots"].(map[string]interface{}); ok {
			if snapshot := misActionSnapshotFromAny(snapshots[actionID]); snapshot != nil {
				action = snapshot.toBusinessAction()
			}
		} else if snapshotMap := misActionSnapshotFromAny(data["business_action_snapshot"]); snapshotMap != nil && snapshotMap.ID == actionID {
			action = snapshotMap.toBusinessAction()
		}
	}
	if strings.TrimSpace(action.ID) == "" {
		if cfgErr != nil {
			return &IMAgentResponse{Text: "Load MIS data config failed.", Error: cfgErr.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		if actionLoadErr != nil {
			return &IMAgentResponse{Text: "Load business action failed.", Error: actionLoadErr.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		return &IMAgentResponse{Text: "Load business action failed.", Error: "business action unavailable and no intent form snapshot exists", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if view := buildMISBusinessActionInputAgentView(action); view != nil {
		a.emitAgentView(view)
	}
	a.saveMISBusinessTransactions()
	return &IMAgentResponse{Text: "Business task form opened in the task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (a *App) handleMISTransactionResumeAgentViewSubmit(data map[string]interface{}) *IMAgentResponse {
	a.ensureMISBusinessTransactionsLoaded()
	transactionID := strings.TrimSpace(fmt.Sprint(data["transaction_id"]))
	if transactionID == "" {
		return &IMAgentResponse{Text: "Choose a business transaction before continuing.", Error: "missing transaction_id", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	txn, ok := getMISBusinessTransaction(transactionID)
	if !ok {
		return &IMAgentResponse{Text: "Business transaction is no longer available.", Error: "transaction not found", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	var action contract.BusinessAction
	actionLoadedFromSnapshot := false
	cfg, cfgErr := a.GetMISDataConfig()
	var actionLoadErr error
	if cfgErr == nil {
		if !cfg.Enabled {
			actionLoadErr = fmt.Errorf("MIS data service is disabled")
		} else if strings.TrimSpace(cfg.Token) == "" {
			actionLoadErr = fmt.Errorf("MIS data service token is empty")
		} else {
			result, callErr := a.callMISDataAPIBytes(cfg, http.MethodGet, "/api/v1/data/business-actions/"+pathEscape(txn.ActionID), nil)
			if callErr == nil {
				if err := json.Unmarshal(result, &action); err != nil {
					return &IMAgentResponse{Text: "Decode business action failed.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
				}
				setMISBusinessTransactionActionSnapshot(transactionID, misActionSnapshotFromBusinessAction(action))
			} else {
				actionLoadErr = callErr
			}
		}
	}
	if strings.TrimSpace(action.ID) == "" && txn.ActionSnapshot != nil {
		action = txn.ActionSnapshot.toBusinessAction()
		actionLoadedFromSnapshot = true
	}
	if strings.TrimSpace(action.ID) == "" {
		if cfgErr != nil {
			return &IMAgentResponse{Text: "Load MIS data config failed.", Error: cfgErr.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		if actionLoadErr != nil {
			return &IMAgentResponse{Text: "Load business action failed.", Error: actionLoadErr.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
		}
		return &IMAgentResponse{Text: "Load business action failed.", Error: "business action unavailable and no local form snapshot exists", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	markMISBusinessTransaction(transactionID, "", "transaction.resumed", "Business transaction resumed from AgentView.", nil)
	if a != nil {
		var view map[string]interface{}
		values := misTransactionFieldValues(txn)
		switch normalizeMISTransactionStateKind(txn.State) {
		case misTransactionStateAwaitingCommit, misTransactionStateCommitFailed:
			view = buildMISBusinessActionCommitReviewAgentView(action.ID, values, transactionID, "Review business commit", "This business transaction has already passed validation. Review and commit when ready.")
		case misTransactionStateAwaitingValidation:
			view = buildMISBusinessActionPendingValidationAgentView(action.ID, values, transactionID, nil)
		default:
			view = buildMISBusinessActionInputAgentViewForTransaction(action, txn)
		}
		if view != nil {
			a.emitAgentView(view)
		}
	}
	a.saveMISBusinessTransactions()
	if actionLoadedFromSnapshot {
		return &IMAgentResponse{Text: "Business transaction form reopened from the local form snapshot.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	return &IMAgentResponse{Text: "Business transaction form reopened in the task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (a *App) handleMISCommitAgentViewSubmit(actionID string, data map[string]interface{}) *IMAgentResponse {
	a.ensureMISBusinessTransactionsLoaded()
	actionID = strings.TrimSpace(actionID)
	approved, _ := data["approved"].(bool)
	if !approved {
		parameters, _ := data["parameters"].(map[string]interface{})
		transactionID := strings.TrimSpace(fmt.Sprint(parameters["transaction_id"]))
		markMISBusinessTransaction(transactionID, misTransactionStateCollecting.String(), "business_action.commit_rejected", "User kept editing the structured business data.", nil)
		deleteMISAgentViewDraft(strings.TrimSpace(fmt.Sprint(parameters["draft_id"])))
		a.saveMISBusinessTransactions()
		return &IMAgentResponse{Text: "Business action was not committed.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	parameters, _ := data["parameters"].(map[string]interface{})
	draftID := strings.TrimSpace(fmt.Sprint(parameters["draft_id"]))
	transactionID := strings.TrimSpace(fmt.Sprint(parameters["transaction_id"]))
	submittedData, foundDraft := getMISAgentViewDraft(draftID, actionID)
	if !foundDraft {
		submittedData, _ = parameters["data"].(map[string]interface{})
	}
	if len(submittedData) == 0 {
		return &IMAgentResponse{Text: "Missing structured data for commit.", Error: "missing data", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	result, err := a.executeMISBusinessActionBytes(actionID, submittedData, false)
	if err != nil {
		markMISBusinessTransaction(transactionID, misTransactionStateCommitFailed.String(), "business_action.commit_failed", err.Error(), nil)
		a.emitAgentView(buildMISBusinessActionCommitFailedAgentView(actionID, submittedData, transactionID, draftID, err))
		a.saveMISBusinessTransactions()
		return &IMAgentResponse{Text: "Business action commit failed.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	markMISBusinessTransaction(transactionID, misTransactionStateCommitted.String(), "business_action.committed", "Structured business data committed.", misBusinessActionCommitSummary(actionID, decodeMISJSONMap(result)))
	a.emitAgentView(buildMISBusinessActionCommittedAgentViewWithTransaction(actionID, result, transactionID))
	deleteMISAgentViewDraft(draftID)
	a.saveMISBusinessTransactions()
	return &IMAgentResponse{Text: "Business action committed. Result is shown in the task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (a *App) executeMISDataTool(args map[string]interface{}) string {
	action := normalizeMISDataToolAction(stringArg(args, "action"))
	if action == misDataToolActionListAgentTransactions {
		a.ensureMISBusinessTransactionsLoaded()
		txns := activeMISBusinessTransactions(strings.TrimSpace(stringArg(args, "business_action_id")), strings.TrimSpace(stringArg(args, "dataset_id")), intArg(args, "limit", 5))
		if view := buildMISTransactionWorkspaceAgentView(txns); view != nil {
			a.emitAgentView(view)
		}
		return marshalToolResult(map[string]interface{}{"count": len(txns), "transactions": misTransactionWorkspaceSummaries(txns)})
	}
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return fmt.Sprintf("load MIS data config failed: %v", err)
	}
	// resolve_intent local fallback: when MIS service is disabled or token is empty,
	// generate a local AgentView form from the LLM-provided field descriptions in args.
	// This allows AG UI form generation to work without a remote MIS service.
	if !cfg.Enabled || strings.TrimSpace(cfg.Token) == "" {
		if action == misDataToolActionResolveIntent {
			return a.resolveIntentLocalFallback(args)
		}
		if !cfg.Enabled {
			return "MIS data service is disabled. Enable it in Settings > MIS data."
		}
		return "MIS data service token is empty. Configure it in Settings > MIS data."
	}
	if action == misDataToolActionUnknown {
		return "missing action. Supported: status, get_capabilities, list_domains, get_domain, list_business_objects, list_app_installations, get_app_installation, resolve_object_role, list_relationships, resolve_intent, get_inbox, get_inbox_summary, get_stats, export_governance_evidence_pack, export_governance_evidence_summary, run_maintenance, list_business_actions, get_business_action, list_business_rules, evaluate_business_rules, list_event_contracts, get_event_contract, list_event_dead_letters, get_event_dead_letter, retry_event_dead_letter, resolve_event_dead_letter, list_connectors, list_connector_health, get_connector, upsert_connector, delete_connector, test_connector, validate_connector_config, check_connector_readiness, get_connector_health, get_connector_sync_state, list_connector_sync_runs, update_connector_sync_state, plan_connector_sync, sync_connector_batch, suggest_connector_mapping, patch_connector_config, preview_connector_event, ingest_connector_event, execute_business_action, list_business_views, get_business_view, query_business_view, list_dashboards, get_dashboard, run_dashboard, list_reports, get_report, run_report, aggregate_records, list_quality_checks, run_quality_check, list_quality_runs, get_quality_run, list_import_jobs, get_import_job, list_export_jobs, get_export_job, download_export_job, list_operation_plans, create_operation_plan, get_operation_plan, review_operation_plan, apply_operation_plan, cancel_operation_plan, mis.approval.start, mis.approval.list, mis.approval.list_by_record, mis.approval.get, mis.approval.sync_result, mis.approval.my_inbox, mis.approval.my_pending, mis.approval.my_requests, mis.approval.pending_my_approval, mis.approval.handled, mis.approval.attention, list_record_approvals, create_record_approval, get_record_approval, review_record_approval, list_audit_logs, export_audit_logs_csv, list_data_events, list_record_revisions, get_record_timeline, get_related_records, list_schema_proposals, get_schema_proposal, list_templates, get_template, bootstrap_templates, create_dataset_from_template, list_datasets, get_dataset, create_dataset, delete_dataset, list_fields, upsert_fields, propose_schema, apply_schema_proposal, validate_record, batch_import_records, bulk_update_records, bulk_delete_records, restore_record, start_batch_import_job, get_import_template_csv, import_records_csv, start_csv_import_job, import_records_jsonl, start_jsonl_import_job, upsert_record, get_record, delete_record, query_records, export_records, export_records_jsonl, start_csv_export_job, start_jsonl_export_job, ingest_event, create_backup, list_backups, get_backup, download_backup, restore_backup"
	}
	switch action {
	case "status":
		status, _ := a.TestMISDataConnection(cfg)
		return marshalToolResult(status)
	case "get_capabilities":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/capabilities", nil)
	case "list_domains":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/domains", nil)
	case "get_domain":
		domain := strings.TrimSpace(stringArg(args, "domain"))
		if domain == "" {
			return "missing domain"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/domains/"+pathEscape(domain), nil)
	case "list_business_objects":
		values := url.Values{}
		if appID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId"))); appID != "" {
			values.Set("app_id", appID)
		}
		if blueprintID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId"))); blueprintID != "" {
			values.Set("blueprint_id", blueprintID)
		}
		if domain := strings.TrimSpace(stringArg(args, "domain")); domain != "" {
			values.Set("domain", domain)
		}
		if objectRole := misObjectRoleArg(args); objectRole != "" {
			values.Set("object_role", objectRole)
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		if beforeID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "before_id"), stringArg(args, "before"), stringArg(args, "cursor"))); beforeID != "" {
			values.Set("before_id", beforeID)
		}
		path := "/api/v1/data/business-objects"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case misDataToolActionListAppInstallations:
		values := url.Values{}
		for _, pair := range []struct {
			arg   string
			query string
		}{
			{"app_id", "app_id"},
			{"appId", "app_id"},
			{"blueprint_id", "blueprint_id"},
			{"blueprintId", "blueprint_id"},
			{"kind", "kind"},
			{"source", "source"},
			{"status", "status"},
			{"workflow_skill_id", "workflow_skill_id"},
			{"workflowSkillId", "workflow_skill_id"},
			{"workflow_node", "workflow_node"},
			{"workflowNode", "workflow_node"},
			{"current_node", "workflow_node"},
			{"currentNode", "workflow_node"},
			{"approval_status", "approval_status"},
			{"approvalStatus", "approval_status"},
			{"approval_result_status", "approval_status"},
			{"approvalResultStatus", "approval_status"},
			{"approval_decision", "approval_decision"},
			{"approvalDecision", "approval_decision"},
			{"decision", "approval_decision"},
			{"applicant_id", "applicant_id"},
			{"applicantId", "applicant_id"},
			{"submitted_by", "applicant_id"},
			{"submittedBy", "applicant_id"},
			{"created_by", "applicant_id"},
			{"createdBy", "applicant_id"},
			{"approver_id", "approver_id"},
			{"approverId", "approver_id"},
			{"assigned_to", "approver_id"},
			{"assignedTo", "approver_id"},
			{"current_assignee", "approver_id"},
			{"currentAssignee", "approver_id"},
			{"approval_id", "approval_id"},
			{"approvalId", "approval_id"},
			{"record_approval_id", "approval_id"},
			{"recordApprovalId", "approval_id"},
			{"recordApprovalID", "approval_id"},
			{"workflow_instance_id", "workflow_instance_id"},
			{"workflowInstanceId", "workflow_instance_id"},
			{"workflowInstanceID", "workflow_instance_id"},
			{"approval_instance_id", "workflow_instance_id"},
			{"approvalInstanceId", "workflow_instance_id"},
			{"approvalInstanceID", "workflow_instance_id"},
			{"instance_id", "workflow_instance_id"},
			{"instanceId", "workflow_instance_id"},
			{"dataset_id", "dataset_id"},
			{"datasetId", "dataset_id"},
			{"dataset", "dataset_id"},
			{"object_role", "object_role"},
			{"objectRole", "object_role"},
			{"object", "object_role"},
			{"record_id", "record_id"},
			{"recordId", "record_id"},
			{"business_record_id", "record_id"},
			{"businessRecordId", "record_id"},
			{"businessRecordID", "record_id"},
			{"result_type", "result_type"},
			{"resultType", "result_type"},
			{"output_type", "result_type"},
			{"outputType", "result_type"},
			{"definition_fingerprint", "definition_fingerprint"},
			{"definitionFingerprint", "definition_fingerprint"},
			{"definition_hash", "definition_fingerprint"},
			{"definitionHash", "definition_fingerprint"},
			{"app_definition_hash", "definition_fingerprint"},
			{"appDefinitionHash", "definition_fingerprint"},
			{"app_definition_fingerprint", "definition_fingerprint"},
			{"appDefinitionFingerprint", "definition_fingerprint"},
			{"before", "before"},
			{"before_id", "before_id"},
			{"beforeId", "before_id"},
		} {
			if value := strings.TrimSpace(stringArg(args, pair.arg)); value != "" && values.Get(pair.query) == "" {
				if pair.query == "app_id" {
					value = canonicalMISDataAppInstallationID(value)
				}
				values.Set(pair.query, value)
			}
		}
		if value, ok := misBoolQueryArg(args, "has_blocking_dependency", "hasBlockingDependency"); ok {
			values.Set("has_blocking_dependency", value)
		}
		if value, ok := misBoolQueryArg(args, "has_missing_required_dependency", "hasMissingRequiredDependency", "has_missing_required", "hasMissingRequired"); ok {
			values.Set("has_missing_required_dependency", value)
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/app-installations"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case misDataToolActionGetAppInstallation:
		appID := canonicalMISDataAppInstallationID(firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId"), stringArg(args, "id")))
		if appID == "" {
			return "missing app_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/app-installations/"+url.PathEscape(appID), nil)
	case "resolve_object_role":
		objectRole := misObjectRoleArg(args)
		if objectRole == "" {
			return "missing object_role"
		}
		body := map[string]interface{}{
			"app_id":       firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId")),
			"blueprint_id": firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId")),
			"object_role":  objectRole,
		}
		if misBoolArg(args, "require_initialized") {
			body["require_initialized"] = true
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/object-roles/resolve", compactPayload(body))
	case "list_relationships":
		values := url.Values{}
		if datasetID, datasetErr := a.resolveOptionalMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		path := "/api/v1/data/relationships"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "resolve_intent":
		body := map[string]interface{}{"query": stringArg(args, "query"), "domain": stringArg(args, "domain"), "limit": args["limit"]}
		data, err := a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/intent/resolve", compactPayload(body))
		if err != nil {
			return err.Error()
		}
		a.emitMISIntentAgentView(data)
		return prettyMISDataResponse(data)
	case misDataToolActionGetInbox, misDataToolActionGetInboxSummary:
		values := url.Values{}
		if datasetID, datasetErr := a.resolveInboxMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if appID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "app"))); appID != "" {
			values.Set("app_id", appID)
		}
		if blueprintID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprint"))); blueprintID != "" {
			values.Set("blueprint_id", blueprintID)
		}
		if objectRole := misObjectRoleArg(args); objectRole != "" {
			values.Set("object_role", objectRole)
		}
		if workflowSkillID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_skill_id"), stringArg(args, "workflowSkillId"))); workflowSkillID != "" {
			values.Set("workflow_skill_id", workflowSkillID)
		}
		if approvalWorkflowID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "approval_workflow_id"), stringArg(args, "approvalWorkflowId"), stringArg(args, "workflow_id"))); approvalWorkflowID != "" {
			values.Set("approval_workflow_id", approvalWorkflowID)
		}
		if triggerEvent := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "trigger_event"), stringArg(args, "event"), stringArg(args, "approval_event"))); triggerEvent != "" {
			values.Set("trigger_event", triggerEvent)
		}
		if submittedBy := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "submitted_by"), stringArg(args, "applicant"), stringArg(args, "owner"))); submittedBy != "" {
			values.Set("submitted_by", submittedBy)
		}
		if currentAssignee := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee"), stringArg(args, "assigned_to"), stringArg(args, "assignee"), stringArg(args, "approver"))); currentAssignee != "" {
			values.Set("current_assignee", currentAssignee)
		}
		if currentAssigneeType := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee_type"), stringArg(args, "assignee_type"))); currentAssigneeType != "" {
			values.Set("current_assignee_type", currentAssigneeType)
		}
		if fromStatus := strings.TrimSpace(stringArg(args, "from_status")); fromStatus != "" {
			values.Set("from_status", fromStatus)
		}
		if toStatus := strings.TrimSpace(stringArg(args, "to_status")); toStatus != "" {
			values.Set("to_status", toStatus)
		}
		if workflowInstanceID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_instance_id"), stringArg(args, "approval_instance_id"))); workflowInstanceID != "" {
			values.Set("workflow_instance_id", workflowInstanceID)
		}
		if workflowNodeID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_node_id"), stringArg(args, "current_node"))); workflowNodeID != "" {
			values.Set("workflow_node_id", workflowNodeID)
		}
		if businessStatus := strings.TrimSpace(stringArg(args, "business_status")); businessStatus != "" {
			values.Set("business_status", businessStatus)
		}
		if resultStatus := strings.TrimSpace(stringArg(args, "result_status")); resultStatus != "" {
			values.Set("result_status", resultStatus)
		}
		if lane := strings.TrimSpace(stringArg(args, "lane")); lane != "" {
			values.Set("lane", lane)
		}
		if userID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "user_id"), stringArg(args, "actor"))); userID != "" {
			values.Set("user_id", userID)
		}
		if itemType := strings.TrimSpace(stringArg(args, "type")); itemType != "" {
			values.Set("type", itemType)
		}
		if status := strings.TrimSpace(stringArg(args, "status")); status != "" {
			values.Set("status", status)
		}
		if misBoolArg(args, "include_ok") {
			values.Set("include_ok", "true")
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/inbox"
		if action == misDataToolActionGetInboxSummary {
			path = "/api/v1/data/inbox/summary"
		}
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_stats":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/stats", nil)
	case "export_governance_evidence_pack":
		values := url.Values{}
		if minSeverity := strings.TrimSpace(stringArg(args, "min_severity")); minSeverity != "" {
			values.Set("min_severity", minSeverity)
		}
		if lang := strings.TrimSpace(stringArg(args, "lang")); lang != "" {
			values.Set("lang", lang)
		}
		path := "/api/v1/data/governance/evidence-pack"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "export_governance_evidence_summary":
		values := url.Values{}
		if minSeverity := strings.TrimSpace(stringArg(args, "min_severity")); minSeverity != "" {
			values.Set("min_severity", minSeverity)
		}
		if lang := strings.TrimSpace(stringArg(args, "lang")); lang != "" {
			values.Set("lang", lang)
		}
		path := "/api/v1/data/governance/evidence-summary.txt"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "run_maintenance":
		body := map[string]interface{}{"tasks": args["tasks"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/maintenance/run", compactPayload(body))
	case "list_business_actions":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/business-actions", nil)
	case "get_business_action":
		actionID := strings.TrimSpace(stringArg(args, "business_action_id"))
		if actionID == "" {
			return "missing business_action_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/business-actions/"+pathEscape(actionID), nil)
	case "list_business_rules":
		values := url.Values{}
		for _, key := range []string{"domain", "dataset_id", "business_action_id", "severity"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		path := "/api/v1/data/business-rules"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "evaluate_business_rules":
		body := map[string]interface{}{"domain": stringArg(args, "domain"), "dataset_id": stringArg(args, "dataset_id"), "business_action_id": stringArg(args, "business_action_id"), "record_id": stringArg(args, "record_id"), "dry_run": misBoolArg(args, "dry_run"), "data": args["data"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/business-rules/evaluate", compactPayload(body))
	case "list_event_contracts":
		values := url.Values{}
		if domain := strings.TrimSpace(stringArg(args, "domain")); domain != "" {
			values.Set("domain", domain)
		}
		path := "/api/v1/data/event-contracts"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_event_contract":
		actionID := strings.TrimSpace(stringArg(args, "business_action_id"))
		if actionID == "" {
			return "missing business_action_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/event-contracts/"+pathEscape(actionID), nil)
	case "list_event_dead_letters":
		values := url.Values{}
		for _, key := range []string{"status", "source", "event_type", "business_action_id", "dataset_id", "record_id", "idempotency_key", "before"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		if limit := intArg(args, "limit", 0); limit > 0 {
			values.Set("limit", fmt.Sprint(limit))
		}
		path := "/api/v1/data/events/dead-letter"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_event_dead_letter":
		deadLetterID := strings.TrimSpace(stringArg(args, "dead_letter_id"))
		if deadLetterID == "" {
			return "missing dead_letter_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/events/dead-letter/"+pathEscape(deadLetterID), nil)
	case "retry_event_dead_letter":
		deadLetterID := strings.TrimSpace(stringArg(args, "dead_letter_id"))
		if deadLetterID == "" {
			return "missing dead_letter_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/events/dead-letter/"+pathEscape(deadLetterID)+"/retry", nil)
	case "resolve_event_dead_letter":
		deadLetterID := strings.TrimSpace(stringArg(args, "dead_letter_id"))
		if deadLetterID == "" {
			return "missing dead_letter_id"
		}
		body := map[string]interface{}{"resolution": stringArg(args, "resolution")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/events/dead-letter/"+pathEscape(deadLetterID)+"/resolve", compactPayload(body))
	case "list_connectors":
		values := url.Values{}
		for _, key := range []string{"domain", "kind", "enabled"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		if limit := intArg(args, "limit", 0); limit > 0 {
			values.Set("limit", fmt.Sprint(limit))
		}
		path := "/api/v1/data/connectors"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "list_connector_health":
		values := url.Values{}
		for _, key := range []string{"domain", "kind", "enabled"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		if limit := intArg(args, "limit", 0); limit > 0 {
			values.Set("limit", fmt.Sprint(limit))
		}
		path := "/api/v1/data/connectors/health"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_connector":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/connectors/"+pathEscape(connectorID), nil)
	case "upsert_connector":
		body := map[string]interface{}{"id": stringArg(args, "connector_id"), "domain": stringArg(args, "domain"), "name": stringArg(args, "name"), "kind": stringArg(args, "kind"), "base_url": stringArg(args, "base_url"), "auth_type": stringArg(args, "auth_type"), "token_ref": stringArg(args, "token_ref"), "enabled": args["enabled"], "subscribed_actions": args["subscribed_actions"], "config": args["config"]}
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID != "" {
			return a.callMISDataAPI(cfg, http.MethodPut, "/api/v1/data/connectors/"+pathEscape(connectorID), compactPayload(body))
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors", compactPayload(body))
	case "delete_connector":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodDelete, "/api/v1/data/connectors/"+pathEscape(connectorID), nil)
	case "test_connector":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/test", nil)
	case "validate_connector_config":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/config/validate", nil)
	case "check_connector_readiness":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"sample_event": args["sample_event"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/readiness", compactPayload(body))
	case "get_connector_health":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/health", nil)
	case "get_connector_sync_state":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/sync-state", nil)
	case "list_connector_sync_runs":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/sync-runs", nil)
	case "update_connector_sync_state":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"status": stringArg(args, "status"), "cursor": stringArg(args, "cursor"), "checkpoint": args["checkpoint"], "last_error": stringArg(args, "last_error"), "message": stringArg(args, "message"), "synced_records": args["synced_records"], "started_at": stringArg(args, "started_at"), "finished_at": stringArg(args, "finished_at")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/sync-state", compactPayload(body))
	case "plan_connector_sync":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"sample_event": args["sample_event"], "first_page_events": args["events"], "page_size": args["page_size"], "cursor": stringArg(args, "cursor")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/sync-plan", compactPayload(body))
	case "sync_connector_batch":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"events": args["events"], "dry_run": args["dry_run"], "stop_on_error": args["stop_on_error"], "sync_state": args["sync_state"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/sync-batch", compactPayload(body))
	case "suggest_connector_mapping":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"business_action_id": stringArg(args, "business_action_id"), "sample_data": args["sample_data"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/mappings/suggest", compactPayload(body))
	case "patch_connector_config":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"patch": args["patch"], "dry_run": args["dry_run"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/config/patch", compactPayload(body))
	case "preview_connector_event":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"source": stringArg(args, "source"), "business_action_id": stringArg(args, "business_action_id"), "event_type": stringArg(args, "event_type"), "operation": stringArg(args, "operation"), "dataset_id": stringArg(args, "dataset_id"), "record_id": stringArg(args, "record_id"), "idempotency_key": stringArg(args, "idempotency_key"), "title": stringArg(args, "title"), "tags": args["tags"], "data": args["data"], "occurred_at": stringArg(args, "occurred_at")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/events/preview", compactPayload(body))
	case "ingest_connector_event":
		connectorID := strings.TrimSpace(stringArg(args, "connector_id"))
		if connectorID == "" {
			return "missing connector_id"
		}
		body := map[string]interface{}{"source": stringArg(args, "source"), "business_action_id": stringArg(args, "business_action_id"), "event_type": stringArg(args, "event_type"), "operation": stringArg(args, "operation"), "dataset_id": stringArg(args, "dataset_id"), "record_id": stringArg(args, "record_id"), "idempotency_key": stringArg(args, "idempotency_key"), "title": stringArg(args, "title"), "tags": args["tags"], "data": args["data"], "occurred_at": stringArg(args, "occurred_at"), "dry_run": args["dry_run"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/connectors/"+pathEscape(connectorID)+"/events", compactPayload(body))
	case misDataToolActionExecuteBusinessAction:
		actionID := strings.TrimSpace(stringArg(args, "business_action_id"))
		if actionID == "" {
			return "missing business_action_id"
		}
		body := map[string]interface{}{"record_id": stringArg(args, "record_id"), "idempotency_key": stringArg(args, "idempotency_key"), "title": stringArg(args, "title"), "tags": args["tags"], "data": args["data"], "occurred_at": stringArg(args, "occurred_at"), "dry_run": args["dry_run"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/business-actions/"+pathEscape(actionID)+"/execute", compactPayload(body))
	case "list_business_views":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/views", nil)
	case "get_business_view":
		viewID := strings.TrimSpace(stringArg(args, "view_id"))
		if viewID == "" {
			return "missing view_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/views/"+pathEscape(viewID), nil)
	case "query_business_view":
		viewID := strings.TrimSpace(stringArg(args, "view_id"))
		if viewID == "" {
			return "missing view_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/views/"+pathEscape(viewID)+"/query", compactPayload(body))
	case "list_dashboards":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/dashboards", nil)
	case "get_dashboard":
		dashboardID := strings.TrimSpace(stringArg(args, "dashboard_id"))
		if dashboardID == "" {
			return "missing dashboard_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/dashboards/"+pathEscape(dashboardID), nil)
	case "run_dashboard":
		dashboardID := strings.TrimSpace(stringArg(args, "dashboard_id"))
		if dashboardID == "" {
			return "missing dashboard_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/dashboards/"+pathEscape(dashboardID)+"/run", nil)
	case "list_reports":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/reports", nil)
	case "get_report":
		reportID := strings.TrimSpace(stringArg(args, "report_id"))
		if reportID == "" {
			return "missing report_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/reports/"+pathEscape(reportID), nil)
	case "run_report":
		reportID := strings.TrimSpace(stringArg(args, "report_id"))
		if reportID == "" {
			return "missing report_id"
		}
		body := map[string]interface{}{"filter": args["filter"], "limit": args["limit"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/reports/"+pathEscape(reportID)+"/run", compactPayload(body))
	case "aggregate_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"filter": args["filter"], "group_by": args["group_by"], "metrics": args["metrics"], "limit": args["limit"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/aggregate", compactPayload(body))
	case "list_quality_checks":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/quality-checks", nil)
	case "run_quality_check":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"checks": args["checks"], "limit": args["limit"], "include_warnings": args["include_warnings"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/quality/run", compactPayload(body))
	case "list_quality_runs":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		values := url.Values{}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/quality/runs"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_quality_run":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		runID := strings.TrimSpace(stringArg(args, "quality_run_id"))
		if runID == "" {
			runID = strings.TrimSpace(stringArg(args, "id"))
		}
		if runID == "" {
			return "missing quality_run_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/quality/runs/"+pathEscape(runID), nil)
	case "list_import_jobs":
		values := url.Values{}
		for _, key := range []string{"dataset_id", "status", "before"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		if limit := intArg(args, "limit", 0); limit > 0 {
			values.Set("limit", fmt.Sprint(limit))
		}
		path := "/api/v1/data/import-jobs"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_import_job":
		jobID := strings.TrimSpace(stringArg(args, "import_job_id"))
		if jobID == "" {
			jobID = strings.TrimSpace(stringArg(args, "id"))
		}
		if jobID == "" {
			return "missing import_job_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/import-jobs/"+pathEscape(jobID), nil)
	case "list_export_jobs":
		values := url.Values{}
		for _, key := range []string{"dataset_id", "status", "before"} {
			if value := strings.TrimSpace(stringArg(args, key)); value != "" {
				values.Set(key, value)
			}
		}
		if limit := intArg(args, "limit", 0); limit > 0 {
			values.Set("limit", fmt.Sprint(limit))
		}
		path := "/api/v1/data/export-jobs"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_export_job":
		jobID := strings.TrimSpace(stringArg(args, "export_job_id"))
		if jobID == "" {
			jobID = strings.TrimSpace(stringArg(args, "id"))
		}
		if jobID == "" {
			return "missing export_job_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/export-jobs/"+pathEscape(jobID), nil)
	case "download_export_job":
		jobID := strings.TrimSpace(stringArg(args, "export_job_id"))
		if jobID == "" {
			jobID = strings.TrimSpace(stringArg(args, "id"))
		}
		if jobID == "" {
			return "missing export_job_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/export-jobs/"+pathEscape(jobID)+"/download", nil)
	case "list_operation_plans":
		values := url.Values{}
		if datasetID, datasetErr := a.resolveOptionalMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if operation := strings.TrimSpace(stringArg(args, "operation")); operation != "" {
			values.Set("operation", operation)
		}
		if status := strings.TrimSpace(stringArg(args, "status")); status != "" {
			values.Set("status", status)
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/operation-plans?"+values.Encode(), nil)
	case "create_operation_plan":
		body := map[string]interface{}{"dataset_id": stringArg(args, "dataset_id"), "operation": stringArg(args, "operation"), "summary": stringArg(args, "summary"), "risk_level": stringArg(args, "risk_level"), "request": args["request"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/operation-plans", compactPayload(body))
	case "get_operation_plan":
		planID := strings.TrimSpace(stringArg(args, "operation_plan_id"))
		if planID == "" {
			return "missing operation_plan_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/operation-plans/"+pathEscape(planID), nil)
	case "review_operation_plan":
		planID := strings.TrimSpace(stringArg(args, "operation_plan_id"))
		if planID == "" {
			return "missing operation_plan_id"
		}
		body := map[string]interface{}{"decision": stringArg(args, "decision"), "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/operation-plans/"+pathEscape(planID)+"/review", compactPayload(body))
	case "apply_operation_plan":
		planID := strings.TrimSpace(stringArg(args, "operation_plan_id"))
		if planID == "" {
			return "missing operation_plan_id"
		}
		body := map[string]interface{}{"confirm": misBoolArg(args, "confirm"), "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/operation-plans/"+pathEscape(planID)+"/apply", compactPayload(body))
	case "cancel_operation_plan":
		planID := strings.TrimSpace(stringArg(args, "operation_plan_id"))
		if planID == "" {
			return "missing operation_plan_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/operation-plans/"+pathEscape(planID)+"/cancel", map[string]interface{}{})
	case "list_record_approvals", "mis.approval.list", "mis.approval.list_by_record", "mis.approval.my_inbox", "mis.approval.my_pending", "mis.approval.my_requests", "mis.approval.pending_my_approval", "mis.approval.handled", "mis.approval.attention":
		values := url.Values{}
		if datasetID, datasetErr := a.resolveOptionalMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if appID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId"))); appID != "" {
			values.Set("app_id", appID)
		}
		if blueprintID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId"))); blueprintID != "" {
			values.Set("blueprint_id", blueprintID)
		}
		if objectRole := misObjectRoleArg(args); objectRole != "" {
			values.Set("object_role", objectRole)
		}
		if recordID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "id"), stringArg(args, "record_id"))); recordID != "" {
			values.Set("record_id", recordID)
		}
		if status := strings.TrimSpace(stringArg(args, "status")); status != "" {
			values.Set("status", status)
		} else if action == "mis.approval.my_pending" {
			values.Set("status", "pending")
		}
		if kind := strings.TrimSpace(stringArg(args, "kind")); kind != "" {
			values.Set("kind", kind)
		}
		if lane := strings.TrimSpace(stringArg(args, "lane")); lane != "" {
			values.Set("lane", lane)
		}
		switch action {
		case "mis.approval.my_requests":
			values.Set("lane", "my_requests")
		case "mis.approval.pending_my_approval":
			values.Set("lane", "pending_my_approval")
		case "mis.approval.handled":
			values.Set("lane", "handled")
		case "mis.approval.attention":
			values.Set("lane", "attention")
		}
		if assignedTo := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "assigned_to"), stringArg(args, "assignee"))); assignedTo != "" {
			values.Set("assigned_to", assignedTo)
		} else if (action == "mis.approval.my_inbox" || action == "mis.approval.my_pending") && strings.TrimSpace(cfg.UserID) != "" {
			values.Set("assigned_to", strings.TrimSpace(cfg.UserID))
		}
		if workflowSkillID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_skill_id"), stringArg(args, "workflowSkillId"))); workflowSkillID != "" {
			values.Set("workflow_skill_id", workflowSkillID)
		}
		if approvalWorkflowID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "approval_workflow_id"), stringArg(args, "approvalWorkflowId"), stringArg(args, "workflow_id"))); approvalWorkflowID != "" {
			values.Set("approval_workflow_id", approvalWorkflowID)
		}
		if triggerEvent := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "trigger_event"), stringArg(args, "event"), stringArg(args, "approval_event"))); triggerEvent != "" {
			values.Set("trigger_event", triggerEvent)
		}
		if submittedBy := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "submitted_by"), stringArg(args, "applicant"), stringArg(args, "owner"))); submittedBy != "" {
			values.Set("submitted_by", submittedBy)
		}
		if currentAssignee := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee"), stringArg(args, "assigned_to"), stringArg(args, "assignee"), stringArg(args, "approver"))); currentAssignee != "" {
			values.Set("current_assignee", currentAssignee)
		}
		if currentAssigneeType := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee_type"), stringArg(args, "assignee_type"))); currentAssigneeType != "" {
			values.Set("current_assignee_type", currentAssigneeType)
		}
		if fromStatus := strings.TrimSpace(stringArg(args, "from_status")); fromStatus != "" {
			values.Set("from_status", fromStatus)
		}
		if toStatus := strings.TrimSpace(stringArg(args, "to_status")); toStatus != "" {
			values.Set("to_status", toStatus)
		}
		if workflowInstanceID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_instance_id"), stringArg(args, "approval_instance_id"))); workflowInstanceID != "" {
			values.Set("workflow_instance_id", workflowInstanceID)
		}
		if workflowNodeID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_node_id"), stringArg(args, "current_node"), stringArg(args, "workflowNodeId"))); workflowNodeID != "" {
			values.Set("workflow_node_id", workflowNodeID)
		}
		if businessStatus := strings.TrimSpace(stringArg(args, "business_status")); businessStatus != "" {
			values.Set("business_status", businessStatus)
		}
		if resultStatus := strings.TrimSpace(stringArg(args, "result_status")); resultStatus != "" {
			values.Set("result_status", resultStatus)
		}
		if misBoolArg(args, "overdue") {
			values.Set("overdue", "true")
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/approvals"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "create_record_approval", "mis.approval.start":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "id"), stringArg(args, "record_id")))
		if recordID == "" {
			return "missing id"
		}
		body := map[string]interface{}{
			"app_id":                firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId")),
			"blueprint_id":          firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId")),
			"object_role":           misObjectRoleArg(args),
			"kind":                  stringArg(args, "kind"),
			"priority":              stringArg(args, "priority"),
			"summary":               stringArg(args, "summary"),
			"assigned_to":           firstNonEmptyMISAgentView(stringArg(args, "assigned_to"), stringArg(args, "current_assignee"), stringArg(args, "approver")),
			"due_at":                stringArg(args, "due_at"),
			"request":               args["request"],
			"approval_workflow_id":  firstNonEmptyMISAgentView(stringArg(args, "approval_workflow_id"), stringArg(args, "approvalWorkflowId"), stringArg(args, "workflow_id")),
			"trigger_event":         firstNonEmptyMISAgentView(stringArg(args, "trigger_event"), stringArg(args, "event"), stringArg(args, "approval_event")),
			"submitted_by":          firstNonEmptyMISAgentView(stringArg(args, "submitted_by"), stringArg(args, "applicant"), stringArg(args, "owner")),
			"current_assignee":      firstNonEmptyMISAgentView(stringArg(args, "current_assignee"), stringArg(args, "assigned_to"), stringArg(args, "approver")),
			"current_assignee_type": firstNonEmptyMISAgentView(stringArg(args, "current_assignee_type"), stringArg(args, "assignee_type")),
			"from_status":           stringArg(args, "from_status"),
			"to_status":             stringArg(args, "to_status"),
			"workflow_skill_id":     firstNonEmptyMISAgentView(stringArg(args, "workflow_skill_id"), stringArg(args, "workflowSkillId")),
			"workflow_version":      firstNonEmptyMISAgentView(stringArg(args, "workflow_version"), stringArg(args, "approval_workflow_version"), stringArg(args, "workflowVersion")),
			"workflow_instance_id":  firstNonEmptyMISAgentView(stringArg(args, "workflow_instance_id"), stringArg(args, "approval_instance_id")),
			"workflow_node_id":      stringArg(args, "workflow_node_id"),
			"workflow_node_ids":     args["workflow_node_ids"],
			"workflow_decision_id":  stringArg(args, "workflow_decision_id"),
			"detail_url":            firstNonEmptyMISAgentView(stringArg(args, "detail_url"), stringArg(args, "detailUrl")),
			"business_status":       stringArg(args, "business_status"),
			"result_status":         stringArg(args, "result_status"),
			"result_payload":        args["result_payload"],
			"outputs":               args["outputs"],
			"artifacts":             args["artifacts"],
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/"+pathEscape(recordID)+"/approvals", compactPayload(body))
	case "get_record_approval", "mis.approval.get":
		approvalID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "approval_id"), stringArg(args, "id")))
		if approvalID != "" {
			return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/approvals/"+pathEscape(approvalID), nil)
		}
		values := url.Values{}
		if datasetID, datasetErr := a.resolveInboxMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if appID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId"))); appID != "" {
			values.Set("app_id", appID)
		}
		if blueprintID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId"))); blueprintID != "" {
			values.Set("blueprint_id", blueprintID)
		}
		if objectRole := misObjectRoleArg(args); objectRole != "" {
			values.Set("object_role", objectRole)
		}
		if recordID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "record_id"), stringArg(args, "recordId"))); recordID != "" {
			values.Set("record_id", recordID)
		}
		if workflowSkillID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_skill_id"), stringArg(args, "workflowSkillId"))); workflowSkillID != "" {
			values.Set("workflow_skill_id", workflowSkillID)
		}
		if approvalWorkflowID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "approval_workflow_id"), stringArg(args, "approvalWorkflowId"), stringArg(args, "workflow_id"))); approvalWorkflowID != "" {
			values.Set("approval_workflow_id", approvalWorkflowID)
		}
		if triggerEvent := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "trigger_event"), stringArg(args, "event"), stringArg(args, "approval_event"))); triggerEvent != "" {
			values.Set("trigger_event", triggerEvent)
		}
		if submittedBy := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "submitted_by"), stringArg(args, "applicant"), stringArg(args, "owner"))); submittedBy != "" {
			values.Set("submitted_by", submittedBy)
		}
		if currentAssignee := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee"), stringArg(args, "assigned_to"), stringArg(args, "assignee"), stringArg(args, "approver"))); currentAssignee != "" {
			values.Set("current_assignee", currentAssignee)
		}
		if currentAssigneeType := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "current_assignee_type"), stringArg(args, "assignee_type"))); currentAssigneeType != "" {
			values.Set("current_assignee_type", currentAssigneeType)
		}
		if fromStatus := strings.TrimSpace(stringArg(args, "from_status")); fromStatus != "" {
			values.Set("from_status", fromStatus)
		}
		if toStatus := strings.TrimSpace(stringArg(args, "to_status")); toStatus != "" {
			values.Set("to_status", toStatus)
		}
		if workflowInstanceID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_instance_id"), stringArg(args, "approval_instance_id"))); workflowInstanceID != "" {
			values.Set("workflow_instance_id", workflowInstanceID)
		}
		if workflowNodeID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "workflow_node_id"), stringArg(args, "current_node"), stringArg(args, "workflowNodeId"))); workflowNodeID != "" {
			values.Set("workflow_node_id", workflowNodeID)
		}
		if businessStatus := strings.TrimSpace(stringArg(args, "business_status")); businessStatus != "" {
			values.Set("business_status", businessStatus)
		}
		if resultStatus := strings.TrimSpace(stringArg(args, "result_status")); resultStatus != "" {
			values.Set("result_status", resultStatus)
		}
		if assignedTo := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "assigned_to"), stringArg(args, "assignee"))); assignedTo != "" {
			values.Set("assigned_to", assignedTo)
		}
		if status := strings.TrimSpace(stringArg(args, "status")); status != "" {
			values.Set("status", status)
		}
		if values.Get("record_id") == "" && values.Get("workflow_instance_id") == "" {
			return "missing approval_id or record_id"
		}
		if values.Get("limit") == "" {
			values.Set("limit", "1")
		}
		path := "/api/v1/data/approvals"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataFirstItemAPI(cfg, http.MethodGet, path, nil, "approval")
	case "review_record_approval", "mis.approval.sync_result":
		approvalID := strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "approval_id"), stringArg(args, "id")))
		if approvalID == "" {
			return "missing approval_id"
		}
		body := map[string]interface{}{
			"decision":              firstNonEmptyMISAgentView(stringArg(args, "decision"), stringArg(args, "result"), stringArg(args, "result_status")),
			"reason":                firstNonEmptyMISAgentView(stringArg(args, "reason"), stringArg(args, "message")),
			"workflow_instance_id":  firstNonEmptyMISAgentView(stringArg(args, "workflow_instance_id"), stringArg(args, "approval_instance_id")),
			"current_assignee":      firstNonEmptyMISAgentView(stringArg(args, "current_assignee"), stringArg(args, "assigned_to"), stringArg(args, "approver")),
			"current_assignee_type": firstNonEmptyMISAgentView(stringArg(args, "current_assignee_type"), stringArg(args, "assignee_type")),
			"from_status":           stringArg(args, "from_status"),
			"to_status":             stringArg(args, "to_status"),
			"workflow_node_id":      stringArg(args, "workflow_node_id"),
			"workflow_node_ids":     args["workflow_node_ids"],
			"workflow_version":      firstNonEmptyMISAgentView(stringArg(args, "workflow_version"), stringArg(args, "approval_workflow_version"), stringArg(args, "workflowVersion")),
			"workflow_decision_id":  stringArg(args, "workflow_decision_id"),
			"detail_url":            firstNonEmptyMISAgentView(stringArg(args, "detail_url"), stringArg(args, "detailUrl")),
			"business_status":       stringArg(args, "business_status"),
			"result_status":         stringArg(args, "result_status"),
			"result_payload":        args["result_payload"],
			"outputs":               args["outputs"],
			"artifacts":             args["artifacts"],
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/approvals/"+pathEscape(approvalID)+"/review", compactPayload(body))
	case "list_audit_logs", "export_audit_logs_csv":
		values := url.Values{}
		if datasetID, datasetErr := a.resolveOptionalMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if actionFilter := strings.TrimSpace(stringArg(args, "audit_action")); actionFilter != "" {
			values.Set("action", actionFilter)
		}
		if userID := strings.TrimSpace(stringArg(args, "user_id")); userID != "" {
			values.Set("user_id", userID)
		}
		if targetType := strings.TrimSpace(stringArg(args, "target_type")); targetType != "" {
			values.Set("target_type", targetType)
		}
		if targetID := strings.TrimSpace(stringArg(args, "target_id")); targetID != "" {
			values.Set("target_id", targetID)
		}
		if q := strings.TrimSpace(stringArg(args, "q")); q != "" {
			values.Set("q", q)
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/audit"
		if action == misDataToolActionExportAuditLogsCSV {
			path = "/api/v1/data/audit/export.csv"
		}
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "list_data_events":
		values := url.Values{}
		if datasetID, datasetErr := a.resolveOptionalMISDatasetIDArg(cfg, args); datasetErr != nil {
			return datasetErr.Error()
		} else if datasetID != "" {
			values.Set("dataset_id", datasetID)
		}
		if source := strings.TrimSpace(stringArg(args, "source")); source != "" {
			values.Set("source", source)
		}
		if eventType := strings.TrimSpace(stringArg(args, "event_type")); eventType != "" {
			values.Set("event_type", eventType)
		}
		if businessAction := strings.TrimSpace(stringArg(args, "business_action_id")); businessAction != "" {
			values.Set("business_action_id", businessAction)
		}
		if key := strings.TrimSpace(stringArg(args, "idempotency_key")); key != "" {
			values.Set("idempotency_key", key)
		}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/events"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "list_record_revisions":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID == "" {
			recordID = strings.TrimSpace(stringArg(args, "record_id"))
		}
		if recordID == "" {
			return "missing id"
		}
		values := url.Values{}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/records/" + pathEscape(recordID) + "/revisions"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_record_timeline":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID == "" {
			recordID = strings.TrimSpace(stringArg(args, "record_id"))
		}
		if recordID == "" {
			return "missing id"
		}
		values := url.Values{}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/records/" + pathEscape(recordID) + "/timeline"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_related_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID == "" {
			recordID = strings.TrimSpace(stringArg(args, "record_id"))
		}
		if recordID == "" {
			return "missing id"
		}
		values := url.Values{}
		if limit := strings.TrimSpace(fmt.Sprint(args["limit"])); limit != "" && limit != "<nil>" {
			values.Set("limit", limit)
		}
		path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/records/" + pathEscape(recordID) + "/related"
		if encoded := values.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "list_schema_proposals":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		path := "/api/v1/data/datasets/" + pathEscape(datasetID) + "/schema-proposals"
		if status := strings.TrimSpace(stringArg(args, "status")); status != "" {
			path += "?status=" + url.QueryEscape(status)
		}
		return a.callMISDataAPI(cfg, http.MethodGet, path, nil)
	case "get_schema_proposal":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		proposalID := strings.TrimSpace(stringArg(args, "proposal_id"))
		if proposalID == "" {
			return "missing proposal_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/schema-proposals/"+pathEscape(proposalID), nil)
	case "list_templates":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/templates", nil)
	case "get_template":
		templateID := strings.TrimSpace(stringArg(args, "template_id"))
		if templateID == "" {
			return "missing template_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/templates/"+pathEscape(templateID), nil)
	case "bootstrap_templates":
		body := map[string]interface{}{"template_ids": args["template_ids"], "domains": args["domains"], "skip_existing": args["skip_existing"], "dry_run": args["dry_run"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/templates/bootstrap", compactPayload(body))
	case "create_dataset_from_template":
		templateID := strings.TrimSpace(stringArg(args, "template_id"))
		if templateID == "" {
			return "missing template_id"
		}
		body := map[string]interface{}{"id": stringArg(args, "id"), "domain": stringArg(args, "domain"), "name": stringArg(args, "name"), "title": stringArg(args, "title"), "description": stringArg(args, "description")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/templates/"+pathEscape(templateID)+"/create", compactPayload(body))
	case "list_datasets":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets", nil)
	case "get_dataset":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID), nil)
	case "create_dataset":
		body := map[string]interface{}{"id": stringArg(args, "id"), "domain": stringArg(args, "domain"), "name": stringArg(args, "name"), "title": stringArg(args, "title"), "description": stringArg(args, "description")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets", compactPayload(body))
	case "delete_dataset":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		return a.callMISDataAPI(cfg, http.MethodDelete, "/api/v1/data/datasets/"+pathEscape(datasetID), nil)
	case "list_fields":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/fields", nil)
	case "upsert_fields":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		return a.callMISDataAPI(cfg, http.MethodPut, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/fields", compactPayload(map[string]interface{}{"fields": args["fields"]}))
	case "propose_schema":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"sample_data": args["data"], "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/schema-proposals", compactPayload(body))
	case "apply_schema_proposal":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"proposal_id": stringArg(args, "proposal_id"), "fields": args["fields"], "confirm": misBoolArg(args, "confirm"), "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/schema-proposals/apply", compactPayload(body))
	case "validate_record":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"data": args["data"]}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/validate", compactPayload(body))
	case "batch_import_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"records": args["records"], "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/batch", compactPayload(body))
	case "bulk_update_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{
			"query":   args["query"],
			"set":     args["set"],
			"unset":   args["unset"],
			"title":   args["title"],
			"tags":    args["tags"],
			"limit":   args["limit"],
			"dry_run": misBoolArg(args, "dry_run"),
			"confirm": misBoolArg(args, "confirm"),
			"reason":  stringArg(args, "reason"),
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/bulk-update", compactPayload(body))
	case "bulk_delete_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{
			"query":   args["query"],
			"limit":   args["limit"],
			"dry_run": misBoolArg(args, "dry_run"),
			"confirm": misBoolArg(args, "confirm"),
			"reason":  stringArg(args, "reason"),
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/bulk-delete", compactPayload(body))
	case "restore_record":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if datasetID == "" || recordID == "" {
			return "missing dataset_id or id"
		}
		body := map[string]interface{}{"confirm": misBoolArg(args, "confirm"), "revision_id": stringArg(args, "revision_id"), "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/"+pathEscape(recordID)+"/restore", compactPayload(body))
	case "start_batch_import_job":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"records": args["records"], "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/batch/jobs", compactPayload(body))
	case "get_import_template_csv":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/import-template.csv", nil)
	case "import_records_csv":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"csv": stringArg(args, "csv"), "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/import.csv", compactPayload(body))
	case "start_csv_import_job":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"csv": stringArg(args, "csv"), "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/import.csv/jobs", compactPayload(body))
	case "import_records_jsonl":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"jsonl": stringArg(args, "jsonl"), "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/import.jsonl", compactPayload(body))
	case "start_jsonl_import_job":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"jsonl": stringArg(args, "jsonl"), "dry_run": misBoolArg(args, "dry_run")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/import.jsonl/jobs", compactPayload(body))
	case "upsert_record":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"id": stringArg(args, "id"), "title": stringArg(args, "title"), "tags": args["tags"], "data": args["data"], "source_id": stringArg(args, "source_id")}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID != "" {
			return a.callMISDataAPI(cfg, http.MethodPatch, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/"+pathEscape(recordID), compactPayload(body))
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records", compactPayload(body))
	case "get_record":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID == "" {
			return "missing id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/"+pathEscape(recordID), nil)
	case "delete_record":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		recordID := strings.TrimSpace(stringArg(args, "id"))
		if recordID == "" {
			return "missing id"
		}
		return a.callMISDataAPI(cfg, http.MethodDelete, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/"+pathEscape(recordID), nil)
	case "query_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/query", compactPayload(body))
	case "export_records":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/export.csv", compactPayload(body))
	case "export_records_jsonl":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/export.jsonl", compactPayload(body))
	case "start_csv_export_job":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/export.csv/jobs", compactPayload(body))
	case "start_jsonl_export_job":
		datasetID, datasetErr := a.resolveMISDatasetIDArg(cfg, args, true)
		if datasetErr != nil {
			return datasetErr.Error()
		}
		if datasetID == "" {
			return "missing dataset_id"
		}
		body := map[string]interface{}{"q": stringArg(args, "q"), "tag": stringArg(args, "tag"), "filter": args["filter"], "sort": args["sort"], "limit": args["limit"], "before": stringArg(args, "before")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/datasets/"+pathEscape(datasetID)+"/records/export.jsonl/jobs", compactPayload(body))
	case "ingest_event":
		body := map[string]interface{}{
			"source":             stringArg(args, "source"),
			"event_type":         stringArg(args, "event_type"),
			"operation":          stringArg(args, "operation"),
			"business_action_id": stringArg(args, "business_action_id"),
			"dataset_id":         stringArg(args, "dataset_id"),
			"record_id":          stringArg(args, "record_id"),
			"idempotency_key":    stringArg(args, "idempotency_key"),
			"title":              stringArg(args, "title"),
			"tags":               args["tags"],
			"data":               args["data"],
			"occurred_at":        stringArg(args, "occurred_at"),
			"dry_run":            args["dry_run"],
		}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/events", compactPayload(body))
	case "create_backup":
		body := map[string]interface{}{"name": stringArg(args, "name"), "note": stringArg(args, "note")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/backups", compactPayload(body))
	case "list_backups":
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/backups", nil)
	case "get_backup":
		backupID := strings.TrimSpace(stringArg(args, "backup_id"))
		if backupID == "" {
			return "missing backup_id"
		}
		return a.callMISDataAPI(cfg, http.MethodGet, "/api/v1/data/backups/"+pathEscape(backupID), nil)
	case "download_backup":
		backupID := strings.TrimSpace(stringArg(args, "backup_id"))
		if backupID == "" {
			return "missing backup_id"
		}
		return a.callMISDataDownloadSummary(cfg, "/api/v1/data/backups/"+pathEscape(backupID)+"/download")
	case "restore_backup":
		backupID := strings.TrimSpace(stringArg(args, "backup_id"))
		if backupID == "" {
			return "missing backup_id"
		}
		body := map[string]interface{}{"confirm": misBoolArg(args, "confirm"), "reason": stringArg(args, "reason")}
		return a.callMISDataAPI(cfg, http.MethodPost, "/api/v1/data/backups/"+pathEscape(backupID)+"/restore", compactPayload(body))
	default:
		return "unknown MIS data action: " + string(action)
	}
}

func (a *App) resolveOptionalMISDatasetIDArg(cfg corelib.MISDataConfig, args map[string]interface{}) (string, error) {
	return a.resolveMISDatasetIDArg(cfg, args, false)
}

func (a *App) resolveInboxMISDatasetIDArg(cfg corelib.MISDataConfig, args map[string]interface{}) (string, error) {
	if strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "dataset_id"), stringArg(args, "dataset"), stringArg(args, "data_set"))) != "" || strings.TrimSpace(stringArg(args, "domain")) != "" {
		return a.resolveMISDatasetIDArg(cfg, args, false)
	}
	return "", nil
}

func (a *App) resolveMISDatasetIDArg(cfg corelib.MISDataConfig, args map[string]interface{}, requireInitialized bool) (string, error) {
	datasetID := strings.TrimSpace(stringArg(args, "dataset_id"))
	if datasetID != "" {
		return datasetID, nil
	}
	objectRole := misObjectRoleArg(args)
	if objectRole == "" {
		return "", nil
	}
	body := map[string]interface{}{
		"app_id":       firstNonEmptyMISAgentView(stringArg(args, "app_id"), stringArg(args, "appId")),
		"blueprint_id": firstNonEmptyMISAgentView(stringArg(args, "blueprint_id"), stringArg(args, "blueprintId")),
		"object_role":  objectRole,
	}
	if requireInitialized || misBoolArg(args, "require_initialized") {
		body["require_initialized"] = true
	}
	data, err := a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/object-roles/resolve", compactPayload(body))
	if err != nil {
		return "", fmt.Errorf("resolve object_role %q failed: %v", objectRole, err)
	}
	var result struct {
		DatasetID      string `json:"dataset_id"`
		BusinessObject struct {
			DatasetID string `json:"dataset_id"`
		} `json:"business_object"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decode object_role %q resolution failed: %v", objectRole, err)
	}
	datasetID = strings.TrimSpace(result.DatasetID)
	if datasetID == "" {
		datasetID = strings.TrimSpace(result.BusinessObject.DatasetID)
	}
	if datasetID == "" {
		return "", fmt.Errorf("resolve object_role %q returned empty dataset_id", objectRole)
	}
	if args != nil {
		args["dataset_id"] = datasetID
	}
	return datasetID, nil
}

func misObjectRoleArg(args map[string]interface{}) string {
	return strings.TrimSpace(firstNonEmptyMISAgentView(stringArg(args, "object_role"), stringArg(args, "business_object_role"), stringArg(args, "role")))
}

func (a *App) callMISDataAPI(cfg corelib.MISDataConfig, method, path string, body map[string]interface{}) string {
	data, err := a.callMISDataAPIBytes(cfg, method, path, body)
	if err != nil {
		return err.Error()
	}
	return prettyMISDataResponse(data)
}

func (a *App) callMISDataFirstItemAPI(cfg corelib.MISDataConfig, method, path string, body map[string]interface{}, itemName string) string {
	data, err := a.callMISDataAPIBytes(cfg, method, path, body)
	if err != nil {
		return err.Error()
	}
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Items) == 0 {
		name := strings.TrimSpace(itemName)
		if name == "" {
			name = "item"
		}
		return fmt.Sprintf("no %s found", name)
	}
	return prettyMISDataResponse(envelope.Items[0])
}

func (a *App) callMISDataAPIBytes(cfg corelib.MISDataConfig, method, path string, body map[string]interface{}) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode MIS data request failed: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(cfg.Endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("create MIS data request failed: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	req.Header.Set("X-MaClaw-Tenant-ID", cfg.TenantID)
	req.Header.Set("X-MaClaw-User-ID", cfg.UserID)
	req.Header.Set("X-MaClaw-Role", cfg.Role)
	resp, err := misDataHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MIS data service request failed: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read MIS data response failed: %v", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MIS data service returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (a *App) executeMISBusinessActionBytes(actionID string, data map[string]interface{}, dryRun bool) ([]byte, error) {
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return nil, fmt.Errorf("load MIS data config failed: %v", err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("MIS data service is disabled. Enable it in Settings > MIS data")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("MIS data service token is empty. Configure it in Settings > MIS data")
	}
	body := map[string]interface{}{"data": data, "dry_run": dryRun}
	return a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/business-actions/"+pathEscape(actionID)+"/execute", compactPayload(body))
}

func prettyMISDataResponse(data []byte) string {
	if len(data) == 0 {
		return "{}"
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		out := pretty.String()
		if len(out) > 12000 {
			out = out[:12000] + "\n... (MIS data result truncated)"
		}
		return out
	}
	out := string(data)
	if len(out) > 12000 {
		out = out[:12000] + "\n... (MIS data result truncated)"
	}
	return out
}

func (a *App) emitMISIntentAgentView(data []byte) {
	if a == nil || a.ctx == nil {
		return
	}
	a.ensureMISBusinessTransactionsLoaded()
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil {
		return
	}
	a.emitAgentView(view)
	a.saveMISBusinessTransactions()
}

func buildMISIntentAgentViewFromResolveResult(data []byte) map[string]interface{} {
	var result contract.ResolveBusinessIntentResult
	if err := json.Unmarshal(data, &result); err != nil || len(result.Matches) == 0 {
		return nil
	}
	top := result.Matches[0]
	decision := normalizeMISIntentDecisionKind(top.Decision)
	if decision == misIntentDecisionAskUserToChoose {
		return buildMISIntentChoiceAgentView(result)
	}
	if decision != misIntentDecisionAutoOpenTaskPanel || strings.TrimSpace(top.BusinessActionID) == "" {
		return nil
	}
	step := preferredMISIntentWriteStep(top.NextSteps)
	if step == nil || len(step.InputFields) == 0 {
		return nil
	}
	if txns := activeMISBusinessTransactions(top.BusinessActionID, top.BusinessObjectID, 5); len(txns) > 0 {
		return buildMISTransactionResumeChoiceAgentView(top.BusinessActionID, top.BusinessObjectID, result.Query, txns)
	}
	transactionID := createMISBusinessTransaction(top.BusinessActionID, top.BusinessObjectID, top.Domain, "execute_business_action", result.Query, step.DataTemplate, "mis.resolve_intent")
	fields := buildMISAgentViewFields(step.InputFields, step.DataTemplate, nil, nil)
	if transactionID != "" {
		fields = append(fields, misTransactionHiddenField(transactionID))
	}
	if len(fields) == 0 {
		return nil
	}
	title := strings.TrimSpace(top.UseCase.Title)
	if title == "" {
		title = "Business task"
	}
	setMISBusinessTransactionActionSnapshot(transactionID, misActionSnapshotFromIntent(top.BusinessActionID, top.BusinessObjectID, top.Domain, "execute_business_action", title, step.Description, step.RequiredFields, step.InputFields))
	meta := map[string]interface{}{
		"source":             "mis.resolve_intent",
		"query":              result.Query,
		"domain":             top.Domain,
		"business_action_id": top.BusinessActionID,
		"business_object_id": top.BusinessObjectID,
		"confidence":         top.Confidence,
		"decision":           top.Decision,
		"tool_call_template": step.ToolCallTemplate,
		"body_template":      step.BodyTemplate,
	}
	action := contract.BusinessAction{
		ID:             top.BusinessActionID,
		Domain:         top.Domain,
		DatasetID:      top.BusinessObjectID,
		Operation:      "execute_business_action",
		Title:          title,
		Description:    step.Description,
		RequiredFields: append([]string(nil), step.RequiredFields...),
		InputFields:    step.InputFields,
	}
	if view := buildMISAdaptiveInputAgentViewWithMeta(action, title, fmt.Sprintf("Confidence %.0f%%. Business object: %s. Review and submit structured data.", top.Confidence*100, top.BusinessObjectID), fields, transactionID, meta); view != nil {
		return view
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:intent:" + top.BusinessActionID,
		"title":       title,
		"description": fmt.Sprintf("Confidence %.0f%%. Business object: %s. Review and submit structured data.", top.Confidence*100, top.BusinessObjectID),
		"fields":      fields,
		"submitLabel": avTr("Submit structured data", "提交结构化数据"),
		"meta":        meta,
	}
}

func buildMISIntentChoiceAgentView(result contract.ResolveBusinessIntentResult) map[string]interface{} {
	options := make([]map[string]string, 0, len(result.Matches))
	snapshots := map[string]interface{}{}
	for _, match := range result.Matches {
		actionID := strings.TrimSpace(match.BusinessActionID)
		if actionID == "" {
			continue
		}
		label := strings.TrimSpace(match.UseCase.Title)
		if label == "" {
			label = strings.TrimSpace(match.Title)
		}
		if label == "" {
			label = actionID
		}
		label = fmt.Sprintf("%s - %.0f%% - %s", label, match.Confidence*100, firstNonEmptyMISAgentView(match.BusinessObjectID, match.Domain))
		options = append(options, map[string]string{"label": label, "value": actionID})
		if step := preferredMISIntentWriteStep(match.NextSteps); step != nil && len(step.InputFields) > 0 {
			snapshots[actionID] = misActionSnapshotFromIntent(actionID, match.BusinessObjectID, match.Domain, "execute_business_action", label, step.Description, step.RequiredFields, step.InputFields)
		}
	}
	if len(options) == 0 {
		return nil
	}
	fields := []map[string]interface{}{
		{
			"name":        "business_action_id",
			"label":       "Business task",
			"type":        "select",
			"required":    true,
			"options":     options,
			"description": "Selection is based on semantic intent ranking, not keyword routing.",
		},
	}
	if len(snapshots) > 0 {
		fields = append(fields, map[string]interface{}{"name": "business_action_snapshots", "label": "business_action_snapshots", "type": "hidden", "value": snapshots})
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:choose-intent",
		"title":       avTr("Choose business task", "选择业务任务"),
		"description": avTr("The intent is plausible but not certain. Choose the target business operation to open its structured form.", "意图可能匹配但不确定。请选择目标业务操作以打开其结构化表单。"),
		"fields":      fields,
		"submitLabel": avTr("Open form", "打开表单"),
		"meta": map[string]interface{}{
			"source": "mis.resolve_intent",
			"query":  result.Query,
		},
	}
}

func buildMISTransactionResumeChoiceAgentView(actionID, businessObject, query string, txns []misBusinessTransaction) map[string]interface{} {
	options := make([]map[string]string, 0, len(txns))
	for _, txn := range txns {
		label := misTransactionChoiceLabel(txn)
		if label == "" {
			label = txn.ID
		}
		options = append(options, map[string]string{"label": label, "value": txn.ID})
	}
	if len(options) == 0 {
		return nil
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:resume-transaction",
		"title":       avTr("Continue business transaction", "继续业务事务"),
		"description": avTr("A matching unfinished business transaction already exists. Choose one to reopen its structured form, or start a new task from chat if this is unrelated.", "已存在匹配的未完成业务事务。选择一个重新打开其表单，或从聊天中开始新任务。"),
		"fields": []map[string]interface{}{
			{
				"name":        "transaction_id",
				"label":       "Business transaction",
				"type":        "select",
				"required":    true,
				"options":     options,
				"description": "Selection is based on active transaction state and business action binding.",
			},
		},
		"submitLabel": avTr("Continue", "继续"),
		"meta": map[string]interface{}{
			"source":             "mis.transaction_store",
			"query":              strings.TrimSpace(query),
			"business_action_id": strings.TrimSpace(actionID),
			"business_object_id": strings.TrimSpace(businessObject),
		},
	}
}

func buildMISTransactionWorkspaceAgentView(txns []misBusinessTransaction) map[string]interface{} {
	if len(txns) == 0 {
		return map[string]interface{}{
			"type":        "result_browser",
			"id":          "mis:transaction-workspace",
			"title":       avTr("Business transaction workspace", "业务事务工作区"),
			"description": avTr("No unfinished business transactions are available.", "暂无未完成的业务事务。"),
			"results": []map[string]interface{}{
				{
					"title":  avTr("No active transactions", "暂无活跃事务"),
					"status": "empty",
					"data":   map[string]interface{}{"count": 0},
				},
			},
		}
	}
	results := make([]map[string]interface{}, 0, len(txns))
	for _, txn := range txns {
		summary := misTransactionWorkspaceSummary(txn)
		title := strings.TrimSpace(fmt.Sprint(summary["form_title"]))
		if title == "" {
			title = strings.TrimSpace(txn.ActionID)
		}
		if title == "" {
			title = txn.ID
		}
		subtitle := strings.TrimSpace(fmt.Sprint(summary["label"]))
		results = append(results, map[string]interface{}{
			"id":       txn.ID,
			"title":    title,
			"subtitle": subtitle,
			"status":   strings.TrimSpace(txn.State),
			"data":     summary,
			"actions": []map[string]interface{}{
				{
					"label":   avTr("Continue", "继续"),
					"viewId":  "mis:resume-transaction",
					"primary": true,
					"data":    map[string]interface{}{"transaction_id": txn.ID},
				},
			},
		})
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          "mis:transaction-workspace",
		"title":       avTr("Business transaction workspace", "业务事务工作区"),
		"description": avTr("Choose an unfinished business transaction to reopen its structured form.", "选择一个未完成的业务事务以重新打开其表单。"),
		"results":     results,
		"meta":        map[string]interface{}{"source": "mis.transaction_workspace", "count": len(txns), "transactions": misTransactionWorkspaceSummaries(txns)},
	}
}

func misTransactionWorkspaceSummaries(txns []misBusinessTransaction) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(txns))
	for _, txn := range txns {
		out = append(out, misTransactionWorkspaceSummary(txn))
	}
	return out
}

func misTransactionWorkspaceSummary(txn misBusinessTransaction) map[string]interface{} {
	formTitle := ""
	hasSnapshot := false
	if txn.ActionSnapshot != nil {
		hasSnapshot = true
		formTitle = strings.TrimSpace(txn.ActionSnapshot.Title)
	}
	return map[string]interface{}{
		"transaction_id":     txn.ID,
		"business_action_id": txn.ActionID,
		"business_object_id": txn.BusinessObject,
		"domain":             txn.Domain,
		"state":              txn.State,
		"next_action":        misTransactionNextAction(txn),
		"updated_at":         txn.UpdatedAt,
		"field_count":        len(txn.Fields),
		"has_form_snapshot":  hasSnapshot,
		"form_title":         formTitle,
		"label":              misTransactionChoiceLabel(txn),
	}
}

func misTransactionNextAction(txn misBusinessTransaction) string {
	switch normalizeMISTransactionStateKind(txn.State) {
	case misTransactionStateAwaitingValidation:
		return "retry_validation"
	case misTransactionStateValidationFailed, misTransactionStateCollecting, misTransactionStateUnknown:
		return "continue_editing"
	case misTransactionStateAwaitingCommit, misTransactionStateCommitFailed:
		return "review_or_retry_commit"
	case misTransactionStateValidating:
		return "retry_or_wait_validation"
	default:
		return "continue"
	}
}

func misTransactionChoiceLabel(txn misBusinessTransaction) string {
	parts := []string{}
	if txn.ActionSnapshot != nil && strings.TrimSpace(txn.ActionSnapshot.Title) != "" {
		parts = append(parts, strings.TrimSpace(txn.ActionSnapshot.Title))
	}
	if txn.ActionID != "" {
		parts = append(parts, txn.ActionID)
	}
	if txn.State != "" {
		parts = append(parts, txn.State)
	}
	if summary := misTransactionFieldSummary(txn.Fields); summary != "" {
		parts = append(parts, summary)
	}
	if !txn.UpdatedAt.IsZero() {
		parts = append(parts, txn.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return strings.Join(parts, " - ")
}

func misTransactionFieldSummary(fields map[string]misTransactionField) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	shown := make([]string, 0, 3)
	for _, key := range keys {
		value := fields[key].Value
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			continue
		}
		if len([]rune(text)) > 24 {
			runes := []rune(text)
			text = string(runes[:24]) + "..."
		}
		shown = append(shown, key+"="+text)
		if len(shown) >= 3 {
			break
		}
	}
	return strings.Join(shown, ", ")
}

func buildMISBusinessActionInputAgentView(action contract.BusinessAction) map[string]interface{} {
	return buildMISBusinessActionInputAgentViewForTransaction(action, nil)
}

func buildMISBusinessActionInputAgentViewForTransaction(action contract.BusinessAction, txn *misBusinessTransaction) map[string]interface{} {
	values := map[string]interface{}(nil)
	if txn != nil {
		values = misTransactionFieldValues(txn)
	}
	fields := buildMISAgentViewFields(action.InputFields, nil, values, nil)
	if len(fields) == 0 {
		for _, key := range action.RequiredFields {
			key = strings.TrimSpace(key)
			if key != "" {
				field := map[string]interface{}{"name": key, "label": key, "type": "text", "required": true}
				if values != nil {
					if value, ok := values[key]; ok {
						field["value"] = value
					}
				}
				fields = append(fields, field)
			}
		}
	}
	if len(fields) == 0 {
		return nil
	}
	transactionID := ""
	if txn != nil {
		transactionID = txn.ID
	} else {
		transactionID = createMISBusinessTransaction(action.ID, action.DatasetID, action.Domain, action.Operation, "", nil, "mis.business_action")
	}
	setMISBusinessTransactionActionSnapshot(transactionID, misActionSnapshotFromBusinessAction(action))
	if transactionID != "" {
		fields = append(fields, misTransactionHiddenField(transactionID))
	}
	title := firstNonEmptyMISAgentView(action.Title, action.ID, "Business task")
	description := "Review and submit structured business data."
	if txn != nil {
		description = "Continue the saved business transaction. Existing fields are restored from the transaction snapshot."
	}
	if view := buildMISAdaptiveInputAgentView(action, title, description, fields, transactionID); view != nil {
		return view
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:intent:" + action.ID,
		"title":       title,
		"description": description,
		"fields":      fields,
		"submitLabel": avTr("Submit structured data", "提交结构化数据"),
		"meta": map[string]interface{}{
			"source":             "mis.business_action",
			"domain":             action.Domain,
			"business_action_id": action.ID,
			"business_object_id": action.DatasetID,
		},
	}
}

func buildMISAdaptiveInputAgentView(action contract.BusinessAction, title string, description string, fields []map[string]interface{}, transactionID string) map[string]interface{} {
	return buildMISAdaptiveInputAgentViewWithMeta(action, title, description, fields, transactionID, map[string]interface{}{
		"source":             "mis.business_action",
		"domain":             action.Domain,
		"business_action_id": action.ID,
		"business_object_id": action.DatasetID,
		"transaction_id":     strings.TrimSpace(transactionID),
	})
}

func buildMISAdaptiveInputAgentViewWithMeta(action contract.BusinessAction, title string, description string, fields []map[string]interface{}, transactionID string, meta map[string]interface{}) map[string]interface{} {
	visibleFields := make([]map[string]interface{}, 0, len(fields))
	hiddenFields := []map[string]interface{}{}
	hiddenData := map[string]interface{}{}
	if meta == nil {
		meta = map[string]interface{}{}
	}
	if strings.TrimSpace(transactionID) != "" {
		meta["transaction_id"] = strings.TrimSpace(transactionID)
	}
	for _, field := range fields {
		if normalizeAgentViewFieldType(fmt.Sprint(field["type"])) == agentViewFieldTypeHidden {
			hiddenFields = append(hiddenFields, field)
			name := strings.TrimSpace(fmt.Sprint(field["name"]))
			if name != "" {
				if value, ok := field["value"]; ok {
					hiddenData[name] = value
				}
			}
			continue
		}
		visibleFields = append(visibleFields, field)
	}
	if len(visibleFields) == 1 && normalizeAgentViewFieldType(fmt.Sprint(visibleFields[0]["type"])) == agentViewFieldTypeArrayTable {
		field := visibleFields[0]
		columns, _ := field["columns"].([]map[string]interface{})
		if len(columns) > 0 {
			view := map[string]interface{}{
				"type":        "table_editor",
				"id":          "mis:intent:" + action.ID,
				"title":       title,
				"description": description,
				"columns":     columns,
				"rows":        normalizeMISTableRows(firstNonNilMISAgentView(field["value"], field["defaultValue"])),
				"dataKey":     strings.TrimSpace(fmt.Sprint(field["name"])),
				"hiddenData":  hiddenData,
				"submitLabel": avTr("Submit structured data", "提交结构化数据"),
				"meta":        cloneMISInterfaceMap(meta),
			}
			copyOptionalMISFieldConstraint(view, field, "minItems")
			copyOptionalMISFieldConstraint(view, field, "maxItems")
			copyOptionalMISFieldConstraint(view, field, "uniqueItems")
			copyOptionalMISFieldConstraint(view, field, "dependentRequired")
			return view
		}
	}
	if len(visibleFields) == 1 && isMISResourceField(visibleFields[0]) {
		field := visibleFields[0]
		options, _ := field["options"].([]map[string]string)
		if len(options) > 0 {
			return map[string]interface{}{
				"type":         "resource_picker",
				"id":           "mis:intent:" + action.ID,
				"title":        title,
				"description":  description,
				"resourceType": strings.TrimSpace(fmt.Sprint(firstNonNilMISAgentView(field["resourceType"], field["type"]))),
				"options":      options,
				"multiple":     field["multiple"] == true,
				"value":        firstNonNilMISAgentView(field["value"], field["defaultValue"]),
				"dataKey":      strings.TrimSpace(fmt.Sprint(field["name"])),
				"hiddenData":   hiddenData,
				"submitLabel":  avTr("Submit structured data", "提交结构化数据"),
				"meta":         cloneMISInterfaceMap(meta),
			}
		}
	}
	if len(visibleFields) == 1 && normalizeAgentViewFieldType(fmt.Sprint(visibleFields[0]["type"])) == agentViewFieldTypeFieldMapper {
		field := visibleFields[0]
		sourceFields, _ := field["sourceFields"].([]string)
		targetFields, _ := field["targetFields"].([]map[string]interface{})
		if len(sourceFields) > 0 && len(targetFields) > 0 {
			return map[string]interface{}{
				"type":         "field_mapper",
				"id":           "mis:intent:" + action.ID,
				"title":        title,
				"description":  description,
				"sourceFields": sourceFields,
				"targetFields": targetFields,
				"value":        firstNonNilMISAgentView(field["value"], field["defaultValue"]),
				"dataKey":      strings.TrimSpace(fmt.Sprint(field["name"])),
				"hiddenData":   hiddenData,
				"submitLabel":  avTr("Submit structured data", "提交结构化数据"),
				"meta":         cloneMISInterfaceMap(meta),
			}
		}
	}
	if len(visibleFields) <= 6 {
		return nil
	}
	steps := []map[string]interface{}{}
	stepFields := append([]map[string]interface{}{}, hiddenFields...)
	for i, field := range visibleFields {
		stepFields = append(stepFields, field)
		if len(stepFields) >= 4 || i == len(visibleFields)-1 {
			stepNumber := len(steps) + 1
			steps = append(steps, map[string]interface{}{
				"id":     fmt.Sprintf("step-%d", stepNumber),
				"title":  fmt.Sprintf("Details %d", stepNumber),
				"fields": stepFields,
			})
			stepFields = []map[string]interface{}{}
		}
	}
	return map[string]interface{}{
		"type":        "wizard",
		"id":          "mis:intent:" + action.ID,
		"title":       title,
		"description": description,
		"steps":       steps,
		"submitLabel": avTr("Submit structured data", "提交结构化数据"),
		"meta":        cloneMISInterfaceMap(meta),
	}
}

func isMISResourceField(field map[string]interface{}) bool {
	return normalizeAgentViewFieldType(fmt.Sprint(field["type"])).IsResourceReference()
}

func firstNonNilMISAgentView(values ...interface{}) interface{} {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func normalizeMISTableRows(value interface{}) []map[string]interface{} {
	switch rows := value.(type) {
	case []map[string]interface{}:
		out := make([]map[string]interface{}, 0, len(rows))
		for _, row := range rows {
			out = append(out, cloneMISInterfaceMap(row))
		}
		return out
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(rows))
		for _, item := range rows {
			if row, ok := item.(map[string]interface{}); ok {
				out = append(out, cloneMISInterfaceMap(row))
			}
		}
		return out
	default:
		return []map[string]interface{}{}
	}
}

func copyOptionalMISFieldConstraint(target map[string]interface{}, source map[string]interface{}, key string) {
	if value, ok := source[key]; ok {
		target[key] = value
	}
}

func buildMISBusinessActionDryRunAgentView(actionID string, submittedData map[string]interface{}, resultData []byte) map[string]interface{} {
	return buildMISBusinessActionDryRunAgentViewWithTransaction(actionID, submittedData, resultData, "")
}

func buildMISBusinessActionPendingValidationAgentView(actionID string, submittedData map[string]interface{}, transactionID string, cause error) map[string]interface{} {
	fields := []map[string]interface{}{}
	if txn, ok := getMISBusinessTransaction(transactionID); ok && txn.ActionSnapshot != nil {
		action := txn.ActionSnapshot.toBusinessAction()
		fields = buildMISAgentViewFields(action.InputFields, nil, submittedData, nil)
		if len(fields) == 0 {
			for _, key := range action.RequiredFields {
				key = strings.TrimSpace(key)
				if key == "" {
					continue
				}
				field := map[string]interface{}{"name": key, "label": key, "type": "text", "required": true}
				if value, ok := submittedData[key]; ok {
					field["value"] = value
				}
				fields = append(fields, field)
			}
		}
	}
	if len(fields) == 0 {
		for key, value := range submittedData {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			fields = append(fields, map[string]interface{}{
				"name":  key,
				"label": key,
				"type":  agentViewFieldTypeFromValue(value),
				"value": value,
			})
		}
	}
	if strings.TrimSpace(transactionID) != "" {
		fields = append(fields, misTransactionHiddenField(transactionID))
	}
	if len(fields) == 0 {
		return nil
	}
	message := "MIS validation is temporarily unavailable. The structured data is saved locally; submit again when the service is available."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = message + " Last error: " + cause.Error()
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:intent:" + strings.TrimSpace(actionID),
		"title":       avTr("Waiting for MIS validation", "等待 MIS 验证"),
		"description": avTr("The business transaction is saved locally and can be edited or retried from this panel.", "业务事务已本地保存，可在此面板中编辑或重试。"),
		"fields":      fields,
		"formErrors":  []string{message},
		"submitLabel": avTr("Retry validation", "重试验证"),
		"meta": map[string]interface{}{
			"source":             "mis.transaction_store",
			"business_action_id": strings.TrimSpace(actionID),
			"transaction_id":     strings.TrimSpace(transactionID),
			"state":              misTransactionStateAwaitingValidation.String(),
		},
	}
}

func buildMISBusinessActionCommitReviewAgentView(actionID string, submittedData map[string]interface{}, transactionID string, title string, description string) map[string]interface{} {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" || len(submittedData) == 0 {
		return nil
	}
	draftID := createMISAgentViewDraft(actionID, submittedData, transactionID)
	if strings.TrimSpace(title) == "" {
		title = "Commit business action"
	}
	if strings.TrimSpace(description) == "" {
		description = "Validation passed. Review the data and commit when ready."
	}
	return map[string]interface{}{
		"type":         "approval",
		"id":           "mis:commit:" + actionID,
		"title":        title,
		"description":  description,
		"approveLabel": "Commit",
		"rejectLabel":  "Keep editing",
		"action": map[string]interface{}{
			"summary":    "Write structured business data through " + actionID,
			"risk":       "medium",
			"reviewData": submittedData,
			"effects": []string{
				"Creates or updates the target business object.",
				"Records audit and business event metadata in MaClawDataSrv.",
			},
			"parameters": map[string]interface{}{
				"business_action_id": actionID,
				"transaction_id":     strings.TrimSpace(transactionID),
				"draft_id":           draftID,
			},
		},
	}
}

func buildMISBusinessActionCommitFailedAgentView(actionID string, submittedData map[string]interface{}, transactionID string, draftID string, cause error) map[string]interface{} {
	message := "Commit failed. The validated business data is still staged and can be retried."
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = message + " Last error: " + cause.Error()
	}
	return map[string]interface{}{
		"type":         "approval",
		"id":           "mis:commit:" + strings.TrimSpace(actionID),
		"title":        avTr("Retry business commit", "重试业务提交"),
		"description":  message,
		"approveLabel": "Retry commit",
		"rejectLabel":  "Keep editing",
		"action": map[string]interface{}{
			"summary":    "Retry writing structured business data through " + strings.TrimSpace(actionID),
			"risk":       "medium",
			"reviewData": submittedData,
			"effects": []string{
				"Uses the already validated structured business data.",
				"Retries the original business action commit when the MIS service is available.",
			},
			"parameters": map[string]interface{}{
				"business_action_id": strings.TrimSpace(actionID),
				"transaction_id":     strings.TrimSpace(transactionID),
				"draft_id":           strings.TrimSpace(draftID),
			},
		},
	}
}

func buildMISBusinessActionDryRunAgentViewWithTransaction(actionID string, submittedData map[string]interface{}, resultData []byte, transactionID string) map[string]interface{} {
	var result map[string]interface{}
	_ = json.Unmarshal(resultData, &result)
	valid, _ := result["valid"].(bool)
	if valid {
		markMISBusinessTransaction(transactionID, misTransactionStateAwaitingCommit.String(), "business_action.validation_passed", "Business validation passed; awaiting user commit.", misBusinessActionValidationSummary(result))
		view := buildMISBusinessActionCommitReviewAgentView(actionID, submittedData, transactionID, "Commit business action", "Validation passed. Review the data and commit when ready.")
		if view != nil {
			action, _ := view["action"].(map[string]interface{})
			parameters, _ := action["parameters"].(map[string]interface{})
			if parameters != nil {
				parameters["validation"] = misBusinessActionValidationSummary(result)
			}
		}
		return view
	}
	markMISBusinessTransaction(transactionID, misTransactionStateValidationFailed.String(), "business_action.validation_failed", "Business validation failed.", map[string]interface{}{"errors": misDryRunValidationErrors(result)})
	fields := buildMISDryRunCorrectionFields(submittedData, result, transactionID)
	if strings.TrimSpace(transactionID) != "" {
		fields = append(fields, misTransactionHiddenField(transactionID))
	}
	return map[string]interface{}{
		"type":        "form",
		"id":          "mis:intent:" + actionID,
		"title":       avTr("Validation failed", "验证失败"),
		"description": avTr("Fix the highlighted business data and submit again.", "请修正标记的业务数据后重新提交。"),
		"fields":      fields,
		"formErrors":  misDryRunValidationErrors(result),
		"submitLabel": avTr("Validate again", "重新验证"),
	}
}

func createMISAgentViewDraft(actionID string, data map[string]interface{}, transactionID ...string) string {
	misAgentViewDraftStore.Lock()
	defer misAgentViewDraftStore.Unlock()
	now := time.Now()
	for id, draft := range misAgentViewDraftStore.items {
		if now.Sub(draft.CreatedAt) > 2*time.Hour {
			delete(misAgentViewDraftStore.items, id)
		}
	}
	misAgentViewDraftStore.next++
	id := fmt.Sprintf("misdraft-%d-%d", now.UnixNano(), misAgentViewDraftStore.next)
	txnID := ""
	if len(transactionID) > 0 {
		txnID = strings.TrimSpace(transactionID[0])
	}
	misAgentViewDraftStore.items[id] = misAgentViewDraft{ActionID: strings.TrimSpace(actionID), TransactionID: txnID, Data: cloneMISInterfaceMap(data), CreatedAt: now}
	return id
}

func getMISAgentViewDraft(draftID string, actionID string) (map[string]interface{}, bool) {
	draftID = strings.TrimSpace(draftID)
	actionID = strings.TrimSpace(actionID)
	if draftID == "" {
		return nil, false
	}
	misAgentViewDraftStore.Lock()
	defer misAgentViewDraftStore.Unlock()
	draft, ok := misAgentViewDraftStore.items[draftID]
	if !ok || draft.ActionID != actionID || time.Since(draft.CreatedAt) > 2*time.Hour {
		return nil, false
	}
	return cloneMISInterfaceMap(draft.Data), true
}

func deleteMISAgentViewDraft(draftID string) {
	draftID = strings.TrimSpace(draftID)
	if draftID == "" {
		return
	}
	misAgentViewDraftStore.Lock()
	defer misAgentViewDraftStore.Unlock()
	delete(misAgentViewDraftStore.items, draftID)
}

func cloneMISInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = cloneMISInterfaceValue(value)
	}
	return out
}

// cloneMISInterfaceValue deep-clones a single value. Handles the types that
// appear in JSON-deserialized tool arguments: map, slice, and scalars.
func cloneMISInterfaceValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return cloneMISInterfaceMap(v)
	case []interface{}:
		cloned := make([]interface{}, len(v))
		for i, item := range v {
			cloned[i] = cloneMISInterfaceValue(item)
		}
		return cloned
	case []map[string]interface{}:
		cloned := make([]map[string]interface{}, len(v))
		for i, item := range v {
			cloned[i] = cloneMISInterfaceMap(item)
		}
		return cloned
	default:
		// Scalars (string, float64, bool, nil, json.Number) are immutable — no clone needed.
		return value
	}
}

func buildMISDryRunCorrectionFields(submittedData map[string]interface{}, result map[string]interface{}, transactionID string) []map[string]interface{} {
	var action contract.BusinessAction
	if rawAction, ok := result["action"]; ok {
		if data, err := json.Marshal(rawAction); err == nil {
			_ = json.Unmarshal(data, &action)
		}
	}
	if len(action.InputFields) > 0 && strings.TrimSpace(transactionID) != "" {
		setMISBusinessTransactionActionSnapshot(transactionID, misActionSnapshotFromBusinessAction(action))
	}
	fieldErrors := misDryRunFieldErrors(result)
	if len(action.InputFields) > 0 {
		return buildMISAgentViewFields(action.InputFields, nil, submittedData, fieldErrors)
	}
	fields := make([]map[string]interface{}, 0, len(submittedData))
	for key, value := range submittedData {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		field := map[string]interface{}{
			"name":  key,
			"label": key,
			"type":  agentViewFieldTypeFromValue(value),
			"value": value,
		}
		if errText := fieldErrors[key]; errText != "" {
			field["error"] = errText
		}
		fields = append(fields, field)
	}
	return fields
}

func buildMISAgentViewFields(inputFields []contract.DatasetTemplateField, defaults map[string]any, values map[string]interface{}, fieldErrors map[string]string) []map[string]interface{} {
	fields := make([]map[string]interface{}, 0, len(inputFields))
	for _, field := range inputFields {
		name := strings.TrimSpace(field.Key)
		if name == "" {
			continue
		}
		uiField := map[string]interface{}{
			"name":        name,
			"label":       firstNonEmptyMISAgentView(field.Title, name),
			"type":        datasetAgentViewFieldType(field),
			"required":    field.Required,
			"description": field.Type,
		}
		if values != nil {
			if value, ok := values[name]; ok {
				uiField["value"] = value
			}
		}
		if _, hasValue := uiField["value"]; !hasValue && defaults != nil {
			if value, ok := defaults[name]; ok {
				uiField["defaultValue"] = value
			}
		}
		if options := enumOptionsFromFieldConfig(field.Config); len(options) > 0 {
			uiField["type"] = "select"
			uiField["options"] = options
		}
		if options := resourceOptionsFromFieldConfig(field.Config); len(options) > 0 {
			uiField["options"] = options
			uiField["resourceType"] = firstNonEmptyMISAgentView(nonNilString(field.Config["resource_type"]), nonNilString(field.Config["resourceType"]), fmt.Sprint(uiField["type"]))
			if boolFromAny(field.Config["multiple"]) {
				uiField["multiple"] = true
			}
		}
		if mapper := fieldMapperConfigFromField(field); mapper != nil {
			uiField["type"] = "field_mapper"
			for key, value := range mapper {
				uiField[key] = value
			}
		}
		applyMISFieldConstraints(uiField, field.Config)
		if uiField["type"] == "array_table" {
			if columns := tableColumnsFromFieldConfig(field.Config); len(columns) > 0 {
				uiField["columns"] = columns
			}
		}
		if uiField["type"] == "object_form" {
			if columns := tableColumnsFromFieldConfig(field.Config); len(columns) > 0 {
				uiField["columns"] = columns
			}
		}
		if fieldErrors != nil {
			if errText := fieldErrors[name]; errText != "" {
				uiField["error"] = errText
			}
		}
		fields = append(fields, uiField)
	}
	return fields
}

func misDryRunValidationErrors(result map[string]interface{}) []string {
	errors := []string{}
	validation, _ := result["validation"].(map[string]interface{})
	for _, item := range stringSliceFromAny(validation["errors"]) {
		errors = append(errors, item)
	}
	for _, item := range stringSliceFromAny(result["errors"]) {
		errors = append(errors, item)
	}
	if len(errors) == 0 {
		errors = append(errors, "Business validation did not pass.")
	}
	return errors
}

func misDryRunFieldErrors(result map[string]interface{}) map[string]string {
	out := map[string]string{}
	validation, _ := result["validation"].(map[string]interface{})
	for _, item := range stringSliceFromAny(validation["errors"]) {
		for _, token := range strings.FieldsFunc(item, func(r rune) bool {
			return r == ' ' || r == ':' || r == ',' || r == ';' || r == '.'
		}) {
			token = strings.Trim(token, "`'\"")
			if token != "" {
				out[token] = item
			}
		}
	}
	return out
}

func stringSliceFromAny(value interface{}) []string {
	out := []string{}
	switch items := value.(type) {
	case []interface{}:
		for _, item := range items {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
	case []string:
		for _, item := range items {
			text := strings.TrimSpace(item)
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func buildMISBusinessActionCommittedAgentView(actionID string, resultData []byte) map[string]interface{} {
	return buildMISBusinessActionCommittedAgentViewWithTransaction(actionID, resultData, "")
}

func buildMISBusinessActionCommittedAgentViewWithTransaction(actionID string, resultData []byte, transactionID string) map[string]interface{} {
	var result map[string]interface{}
	_ = json.Unmarshal(resultData, &result)
	summary := misBusinessActionCommitSummary(actionID, result)
	if txn, ok := getMISBusinessTransaction(transactionID); ok {
		summary["transaction_id"] = txn.ID
		summary["transaction_state"] = txn.State
		summary["field_provenance"] = txn.Fields
		summary["field_summary"] = misTransactionFieldSummaryData(txn.Fields)
		summary["event_count"] = len(txn.Events)
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          "mis:committed:" + actionID,
		"title":       avTr("Business action committed", "业务操作已提交"),
		"description": avTr("The structured business data was written through MaClawDataSrv.", "结构化业务数据已通过 MaClawDataSrv 写入。"),
		"results": []map[string]interface{}{
			{
				"title":  actionID,
				"status": "committed",
				"data":   summary,
			},
		},
	}
}

func decodeMISJSONMap(data []byte) map[string]interface{} {
	out := map[string]interface{}{}
	_ = json.Unmarshal(data, &out)
	return out
}

func misBusinessActionValidationSummary(result map[string]interface{}) map[string]interface{} {
	summary := map[string]interface{}{}
	if valid, ok := result["valid"].(bool); ok {
		summary["valid"] = valid
	}
	if action, ok := result["action"].(map[string]interface{}); ok {
		if datasetID := strings.TrimSpace(fmt.Sprint(action["dataset_id"])); datasetID != "" {
			summary["dataset_id"] = datasetID
		}
		if eventType := strings.TrimSpace(fmt.Sprint(action["event_type"])); eventType != "" {
			summary["event_type"] = eventType
		}
	}
	if preview, ok := result["preview"].(map[string]interface{}); ok && len(preview) > 0 {
		summary["preview_fields"] = len(preview)
	}
	if validation, ok := result["validation"].(map[string]interface{}); ok {
		if fieldCount, ok := validation["field_count"]; ok {
			summary["field_count"] = fieldCount
		}
		if unknown := stringSliceFromAny(validation["unknown_fields"]); len(unknown) > 0 {
			summary["unknown_fields"] = strings.Join(unknown, ", ")
		}
	}
	return summary
}

func misBusinessActionCommitSummary(actionID string, result map[string]interface{}) map[string]interface{} {
	summary := map[string]interface{}{"business_action_id": actionID}
	if action, ok := result["action"].(map[string]interface{}); ok {
		for _, key := range []string{"id", "domain", "dataset_id", "event_type", "operation"} {
			if value := strings.TrimSpace(fmt.Sprint(action[key])); value != "" && value != "<nil>" {
				summary["action_"+key] = value
			}
		}
	}
	if event, ok := result["event"].(map[string]interface{}); ok {
		for _, key := range []string{"status", "result_status", "dataset_id", "record_id", "event_type", "idempotency_key"} {
			if value := strings.TrimSpace(fmt.Sprint(event[key])); value != "" && value != "<nil>" {
				summary[key] = value
			}
		}
		if id := strings.TrimSpace(fmt.Sprint(event["id"])); id != "" && id != "<nil>" {
			summary["event_id"] = id
		}
	}
	if rules, ok := result["rules"].(map[string]interface{}); ok {
		if valid, ok := rules["valid"].(bool); ok {
			summary["rules_valid"] = valid
		}
		if count, ok := rules["violation_count"]; ok {
			summary["rule_violations"] = count
		}
	}
	return summary
}

func misTransactionFieldSummaryData(fields map[string]misTransactionField) map[string]interface{} {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		key = strings.TrimSpace(key)
		if key != "" && !strings.HasPrefix(key, "_mis_") {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := map[string]interface{}{}
	for _, key := range keys {
		field := fields[key]
		item := map[string]interface{}{
			"value":     misFieldValuePreview(field.Value),
			"source":    field.Source,
			"confirmed": field.Confirmed,
		}
		if field.Confidence > 0 {
			item["confidence"] = field.Confidence
		}
		if field.ConfirmedAt != nil {
			item["confirmed_at"] = field.ConfirmedAt.Format(time.RFC3339)
		}
		out[key] = item
	}
	return out
}

func misFieldValuePreview(value interface{}) interface{} {
	switch v := value.(type) {
	case []interface{}:
		return fmt.Sprintf("%d selected", len(v))
	case []string:
		return fmt.Sprintf("%d selected", len(v))
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for key := range v {
			if strings.TrimSpace(key) != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if len(keys) > 4 {
			return strings.Join(keys[:4], ", ") + fmt.Sprintf(" +%d more", len(keys)-4)
		}
		return strings.Join(keys, ", ")
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if len([]rune(text)) > 80 {
			runes := []rune(text)
			return string(runes[:80]) + "..."
		}
		return value
	}
}

func preferredMISIntentWriteStep(steps []contract.BusinessIntentNextStep) *contract.BusinessIntentNextStep {
	var dryRun *contract.BusinessIntentNextStep
	for i := range steps {
		step := &steps[i]
		if normalizeMISDataToolAction(step.Action) != misDataToolActionExecuteBusinessAction {
			continue
		}
		if step.DryRun {
			dryRun = step
			continue
		}
		return step
	}
	return dryRun
}

func datasetAgentViewFieldType(field contract.DatasetTemplateField) string {
	return normalizeMISDatasetAgentViewFieldType(field.Type).String()
}

func agentViewFieldTypeFromValue(value interface{}) string {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}, []map[string]interface{}:
		return "array_table"
	case map[string]interface{}:
		return "object_form"
	default:
		return "text"
	}
}

func tableColumnsFromFieldConfig(config map[string]interface{}) []map[string]interface{} {
	raw, ok := config["columns"]
	if !ok {
		raw = config["fields"]
	}
	if raw == nil {
		raw = config["item_fields"]
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	columns := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstNonEmptyMISAgentView(fmt.Sprint(itemMap["name"]), fmt.Sprint(itemMap["key"])))
		if name == "" || name == "<nil>" {
			continue
		}
		column := map[string]interface{}{
			"name":  name,
			"label": firstNonEmptyMISAgentView(nonNilString(itemMap["label"]), nonNilString(itemMap["title"]), name),
			"type":  agentViewTableColumnType(fmt.Sprint(itemMap["type"])),
		}
		if required, ok := itemMap["required"].(bool); ok {
			column["required"] = required
		}
		if options := enumOptionsFromFieldConfig(itemMap); len(options) > 0 {
			column["type"] = "select"
			column["options"] = options
		}
		applyMISFieldConstraints(column, itemMap)
		columns = append(columns, column)
	}
	return columns
}

func applyMISFieldConstraints(target map[string]interface{}, config map[string]interface{}) {
	if len(target) == 0 || len(config) == 0 {
		return
	}
	for _, key := range []string{"min", "minimum"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["min"] = value
			break
		}
	}
	for _, key := range []string{"max", "maximum"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["max"] = value
			break
		}
	}
	for _, key := range []string{"const", "constant", "const_value", "fixed", "fixed_value"} {
		if value, ok := config[key]; ok {
			target["constValue"] = value
			break
		}
	}
	for _, key := range []string{"exclusiveMin", "exclusive_min", "exclusiveMinimum", "exclusive_minimum"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["exclusiveMin"] = value
			break
		}
	}
	for _, key := range []string{"exclusiveMax", "exclusive_max", "exclusiveMaximum", "exclusive_maximum"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["exclusiveMax"] = value
			break
		}
	}
	for _, key := range []string{"step", "multipleOf", "multiple_of"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["step"] = value
			break
		}
	}
	for _, key := range []string{"minItems", "min_items", "min_count", "minimum_items"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["minItems"] = value
			break
		}
	}
	for _, key := range []string{"maxItems", "max_items", "max_count", "maximum_items"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["maxItems"] = value
			break
		}
	}
	for _, key := range []string{"uniqueItems", "unique_items", "unique"} {
		if boolFromAny(config[key]) {
			target["uniqueItems"] = true
			break
		}
	}
	for _, key := range []string{"readOnly", "read_only", "readonly", "disabled"} {
		if boolFromAny(config[key]) {
			target["readOnly"] = true
			break
		}
	}
	for _, key := range []string{"sensitive", "secret", "password", "masked", "writeOnly", "write_only"} {
		if boolFromAny(config[key]) {
			target["sensitive"] = true
			break
		}
	}
	for _, key := range []string{"minLength", "min_length", "minimum_length"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["minLength"] = value
			break
		}
	}
	for _, key := range []string{"maxLength", "max_length", "maximum_length"} {
		if value, ok := numberFromAny(config[key]); ok {
			target["maxLength"] = value
			break
		}
	}
	for _, key := range []string{"pattern", "regex", "regexp"} {
		if value := nonEmptyStringFromAny(config[key]); value != "" {
			target["pattern"] = value
			break
		}
	}
	if value := nonEmptyStringFromAny(config["format"]); value != "" {
		target["format"] = value
		if strings.EqualFold(value, "password") || strings.EqualFold(value, "secret") || strings.EqualFold(value, "token") {
			target["sensitive"] = true
		}
	}
}

func nonNilString(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func agentViewTableColumnType(value string) string {
	return normalizeMISTableColumnAgentViewFieldType(value).String()
}

func enumOptionsFromFieldConfig(config map[string]interface{}) []map[string]string {
	raw, ok := config["enum"]
	if !ok {
		raw = config["values"]
	}
	options := []map[string]string{}
	switch values := raw.(type) {
	case []interface{}:
		for _, value := range values {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" {
				options = append(options, map[string]string{"label": text, "value": text})
			}
		}
	case []string:
		for _, value := range values {
			text := strings.TrimSpace(value)
			if text != "" {
				options = append(options, map[string]string{"label": text, "value": text})
			}
		}
	}
	return options
}

func resourceOptionsFromFieldConfig(config map[string]interface{}) []map[string]string {
	raw, ok := config["options"]
	if !ok {
		raw = config["resources"]
	}
	if raw == nil {
		raw = config["candidates"]
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	options := []map[string]string{}
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		value := strings.TrimSpace(firstNonEmptyMISAgentView(nonNilString(itemMap["value"]), nonNilString(itemMap["id"]), nonNilString(itemMap["key"])))
		label := strings.TrimSpace(firstNonEmptyMISAgentView(nonNilString(itemMap["label"]), nonNilString(itemMap["name"]), nonNilString(itemMap["title"]), value))
		if value == "" || label == "" {
			continue
		}
		option := map[string]string{"label": label, "value": value}
		if description := strings.TrimSpace(firstNonEmptyMISAgentView(nonNilString(itemMap["description"]), nonNilString(itemMap["subtitle"]))); description != "" {
			option["description"] = description
		}
		if status := strings.TrimSpace(nonNilString(itemMap["status"])); status != "" {
			option["status"] = status
		}
		options = append(options, option)
	}
	return options
}

func fieldMapperConfigFromField(field contract.DatasetTemplateField) map[string]interface{} {
	if !strings.EqualFold(strings.TrimSpace(field.Type), "field_mapper") && !strings.EqualFold(strings.TrimSpace(field.Type), "mapping") {
		return nil
	}
	sourceFields := stringListFromAny(firstNonNilMISAgentView(field.Config["source_fields"], field.Config["sourceFields"], field.Config["columns"]))
	targetFields := targetFieldsFromAny(firstNonNilMISAgentView(field.Config["target_fields"], field.Config["targetFields"], field.Config["fields"]))
	if len(sourceFields) == 0 || len(targetFields) == 0 {
		return nil
	}
	return map[string]interface{}{
		"sourceFields": sourceFields,
		"targetFields": targetFields,
	}
}

func targetFieldsFromAny(value interface{}) []map[string]interface{} {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	fields := []map[string]interface{}{}
	for _, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(firstNonEmptyMISAgentView(nonNilString(itemMap["name"]), nonNilString(itemMap["key"])))
		if name == "" {
			continue
		}
		target := map[string]interface{}{
			"name":  name,
			"label": firstNonEmptyMISAgentView(nonNilString(itemMap["label"]), nonNilString(itemMap["title"]), name),
			"type":  agentViewTableColumnType(nonNilString(itemMap["type"])),
		}
		if boolFromAny(itemMap["required"]) {
			target["required"] = true
		}
		fields = append(fields, target)
	}
	return fields
}

func stringListFromAny(value interface{}) []string {
	out := []string{}
	switch items := value.(type) {
	case []interface{}:
		for _, item := range items {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
	case []string:
		for _, item := range items {
			text := strings.TrimSpace(item)
			if text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func firstNonEmptyMISAgentView(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func canonicalMISDataAppInstallationID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "datasrv-installed-") {
		return strings.TrimSpace(strings.TrimPrefix(id, "datasrv-installed-"))
	}
	return id
}

func (a *App) callMISDataDownloadSummary(cfg corelib.MISDataConfig, path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	endpoint := strings.TrimRight(cfg.Endpoint, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Sprintf("create MIS data download request failed: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	req.Header.Set("X-MaClaw-Tenant-ID", cfg.TenantID)
	req.Header.Set("X-MaClaw-User-ID", cfg.UserID)
	req.Header.Set("X-MaClaw-Role", cfg.Role)
	resp, err := misDataHTTPClient.Do(req)
	if err != nil {
		return fmt.Sprintf("MIS data download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Sprintf("MIS data service returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	size, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Sprintf("read MIS data download failed: %v", err)
	}
	return marshalToolResult(map[string]interface{}{
		"status":              "downloaded",
		"content_type":        resp.Header.Get("Content-Type"),
		"content_disposition": resp.Header.Get("Content-Disposition"),
		"sha256":              resp.Header.Get("X-MaClaw-Backup-SHA256"),
		"size_bytes":          size,
	})
}

func compactPayload(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range in {
		switch v := value.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				out[key] = strings.TrimSpace(v)
			}
		case nil:
			continue
		default:
			out[key] = value
		}
	}
	return out
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func misBoolArg(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

func misBoolQueryArg(args map[string]interface{}, keys ...string) (string, bool) {
	if args == nil {
		return "", false
	}
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			if typed {
				return "true", true
			}
			return "false", true
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			if normalized == "true" || normalized == "1" {
				return "true", true
			}
			if normalized == "false" || normalized == "0" {
				return "false", true
			}
		}
	}
	return "", false
}

func pathEscape(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

func marshalToolResult(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
