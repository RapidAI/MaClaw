package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type maclawAppRegionBinding struct {
	ID     string
	Label  string
	Kind   string // action | view | report | dashboard
	Target string
	Input  MaclawAppBusinessOperationInput
}

type maclawAppRegionLoad struct {
	Binding maclawAppRegionBinding
	Result  map[string]any
	Err     error
}

// OpenMaclawAppBusinessWorkspace runs DataSrv-backed business operation(s) and
// opens a multi-region AppView workspace (view / report / dashboard / action).
//
// Phase 3: when the app declares multiple preferred* bindings (input + install
// record), each binding becomes a nav tab; secondary regions soft-fail.
func (a *App) OpenMaclawAppBusinessWorkspace(input MaclawAppBusinessOperationInput) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	input = enrichMaclawAppBusinessInputFromInstall(a, input)
	bindings := collectMaclawAppRegionBindings(input)
	if len(bindings) == 0 {
		return nil, fmt.Errorf("enterprise normal app has no DataSrv operation binding")
	}

	loads := make([]maclawAppRegionLoad, 0, len(bindings))
	var primary map[string]any
	var primaryErr error
	for i, b := range bindings {
		res, err := a.ExecuteMaclawAppBusinessOperation(b.Input)
		loads = append(loads, maclawAppRegionLoad{Binding: b, Result: res, Err: err})
		if err != nil {
			if i == 0 {
				primaryErr = err
			}
			continue
		}
		if primary == nil {
			primary = res
		}
	}
	if primary == nil {
		if primaryErr != nil {
			return nil, primaryErr
		}
		return nil, fmt.Errorf("all DataSrv region loads failed")
	}

	sessionID := maclawAppWorkspaceSessionID(a, input)
	view, buildErr := BuildMaclawAppBusinessMultiAppView(input, loads, sessionID)
	if buildErr != nil {
		view, buildErr = BuildMaclawAppBusinessAppView(input, primary, sessionID)
	}
	if buildErr != nil {
		primary["app_view_error"] = buildErr.Error()
		primary["app_view_opened"] = false
		primary["regions_loaded"] = len(loads)
		return primary, nil
	}
	attachMaclawAppWorkspaceBindingFields(view, input)
	opened := a.emitAgentView(view)
	primary["app_view_opened"] = opened
	primary["app_view_id"] = strings.TrimSpace(fmt.Sprint(view["id"]))
	primary["regions_loaded"] = len(loads)
	primary["region_count"] = countSuccessfulMaclawAppRegionLoads(loads)
	if rev := parseAgentViewInt64(view["viewRevision"]); rev > 0 {
		primary["view_revision"] = rev
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta != nil {
		if ver := strings.TrimSpace(fmt.Sprint(meta["schemaVersion"])); ver != "" && ver != "<nil>" {
			primary["schema_version"] = ver
		}
	}
	return primary, nil
}

// OpenMaclawAppApprovalWorkspace starts an approval workflow and opens the
// instance snapshot as a controlled AppView (progress / approval / result).
func (a *App) OpenMaclawAppApprovalWorkspace(input MaclawAppApprovalWorkflowStartInput) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	result, err := a.StartMaclawAppApprovalWorkflow(input)
	if err != nil {
		return nil, err
	}
	return a.emitMaclawAppApprovalAppViewResult(result, input.AppID)
}

// MaclawAppApprovalDecisionInput is the in-panel approve/reject payload.
type MaclawAppApprovalDecisionInput struct {
	AppID      string `json:"app_id"`
	InstanceID string `json:"instance_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
	// Decision: approve|approved|reject|rejected (normalized to approved/rejected).
	Decision string `json:"decision"`
	Actor    string `json:"actor,omitempty"`
	Note     string `json:"note,omitempty"`
	// OpenAppView defaults to true when omitted from JSON (see Decide…).
	OpenAppView *bool `json:"open_app_view,omitempty"`
}

// DecideMaclawAppApprovalInstance applies approve/reject on a cached instance,
// syncs to DataSrv when possible, and re-opens the approval AppView.
func (a *App) DecideMaclawAppApprovalInstance(input MaclawAppApprovalDecisionInput) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	decision := normalizeMaclawAppPanelDecision(input.Decision)
	if decision == "" {
		return nil, fmt.Errorf("decision must be approve or reject")
	}
	found, err := a.findMaclawAppApprovalInstanceForContinue(appID, input.InstanceID, input.ApprovalID, input.RecordID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("approval instance was not found")
	}
	status := normalizeMaclawAppApprovalStatus(found.Status)
	if status == "approved" || status == "rejected" || status == "failed" || status == "cancelled" || status == "timeout" {
		result := map[string]any{
			"decided":         false,
			"already_final":   true,
			"instance":        cloneMaclawAppApprovalInstance(*found),
			"decision":        status,
			"approval_id":     found.ApprovalID,
			"result_feedback": maclawAppApprovalResultFeedback(*found),
		}
		if input.OpenAppView == nil || *input.OpenAppView {
			return a.emitMaclawAppApprovalAppViewResult(result, appID)
		}
		return result, nil
	}
	note := strings.TrimSpace(input.Note)
	requireNoteOnApprove := false
	if rec, recErr := a.findMaclawAppInstallRecord(appID); recErr == nil && rec != nil {
		if policy := maclawAppInstallApprovalPanelPolicy(rec); policy != nil {
			if v, ok := policy["require_note_on_approve"].(bool); ok {
				requireNoteOnApprove = v
			}
			if v, ok := policy["requireNoteOnApprove"].(bool); ok {
				requireNoteOnApprove = requireNoteOnApprove || v
			}
		}
	}
	if decision == "rejected" && note == "" {
		return nil, fmt.Errorf("note is required when rejecting")
	}
	if decision == "approved" && requireNoteOnApprove && note == "" {
		return nil, fmt.Errorf("note is required when approving")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	actor := firstNonEmptyMaclawAppString(input.Actor, found.CurrentAssignee, found.Approver, found.Owner, "approver")
	if note == "" {
		// Approve may omit note; reject is validated above.
		note = avTr("Approved from AppView panel", "已在应用工作区批准")
	}
	updated := cloneMaclawAppApprovalInstance(*found)
	updated.FromStatus = status
	updated.Status = decision
	updated.ToStatus = decision
	updated.BusinessStatus = decision
	updated.ResultStatus = decision
	updated.Lane = "handled"
	updated.Result = note
	updated.BusinessNote = firstNonEmptyMaclawAppString(note, updated.BusinessNote)
	updated.UpdatedAt = now
	updated.CurrentNodeStatus = decision
	if updated.ResultPayload == nil {
		updated.ResultPayload = map[string]any{}
	}
	updated.ResultPayload["panel_decision"] = decision
	updated.ResultPayload["panel_decision_note"] = note
	updated.ResultPayload["panel_decision_actor"] = actor
	updated.Events = append(updated.Events, maclawAppApprovalEvent{
		At: now, Node: firstNonEmptyMaclawAppString(updated.CurrentNode, "decision"),
		Actor: actor, Decision: decision, Message: note,
	})

	stored, err := a.RecordMaclawAppApprovalInstance(updated)
	if err != nil {
		return nil, err
	}
	syncResult, syncErr := a.SyncMaclawAppApprovalInstanceToDataSrv(maclawAppApprovalDataSrvSyncInput{
		DatasetID:  stored.DatasetID,
		ObjectRole: stored.ObjectRole,
		AppID:      stored.AppID,
		BlueprintID: stored.BlueprintID,
		RecordID:   stored.RecordID,
		ApprovalID: firstNonEmptyMaclawAppString(stored.ApprovalID, stored.RecordApprovalID),
		Instance:   stored,
	})
	result := map[string]any{
		"decided":         true,
		"decision":        decision,
		"instance":        stored,
		"approval_id":     stored.ApprovalID,
		"result_feedback": maclawAppApprovalResultFeedback(stored),
	}
	if syncErr != nil {
		result["sync_error"] = syncErr.Error()
	} else {
		result["sync"] = syncResult
	}
	if input.OpenAppView == nil || *input.OpenAppView {
		return a.emitMaclawAppApprovalAppViewResult(result, appID)
	}
	return result, nil
}

// MaclawAppOpenWorkspaceInput opens a workspace from an installed app (one-click).
type MaclawAppOpenWorkspaceInput struct {
	AppID   string `json:"app_id"`
	AppName string `json:"app_name,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// OpenMaclawAppWorkspaceFromInstall opens the preferred DataSrv multi-region
// workspace or the latest pending approval instance for the installed app.
func (a *App) OpenMaclawAppWorkspaceFromInstall(input MaclawAppOpenWorkspaceInput) (map[string]any, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required")
	}
	install, err := a.findMaclawAppInstallRecord(appID)
	if err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	appName := strings.TrimSpace(input.AppName)
	if install != nil {
		if kind == "" {
			kind = strings.ToLower(strings.TrimSpace(install.Kind))
		}
		if appName == "" {
			appName = strings.TrimSpace(install.AppName)
		}
	}
	if kind == "enterprise_approval_app" || strings.Contains(kind, "approval") {
		// Prefer pending_my_approval, then my_requests, then any recent.
		for _, lane := range []string{"pending_my_approval", "my_requests", "attention", "all"} {
			list, listErr := a.ListMaclawAppApprovalInstances(appID, lane, 5)
			if listErr != nil || len(list) == 0 {
				continue
			}
			inst := list[0]
			result := map[string]any{
				"opened_from":  "install",
				"lane":         lane,
				"instance":     inst,
				"approval_id":  inst.ApprovalID,
				"result_feedback": maclawAppApprovalResultFeedback(inst),
			}
			return a.emitMaclawAppApprovalAppViewResult(result, appID)
		}
		return map[string]any{
			"opened_from":      "install",
			"app_view_opened":  false,
			"reason":           "no_approval_instances",
			"app_id":           appID,
		}, nil
	}
	// Default: enterprise_normal_app (and unknown kinds with datasrv bindings).
	return a.OpenMaclawAppBusinessWorkspace(MaclawAppBusinessOperationInput{
		AppID:   appID,
		AppName: appName,
		Limit:   50,
	})
}

func (a *App) emitMaclawAppApprovalAppViewResult(result map[string]any, appID string) (map[string]any, error) {
	if result == nil {
		result = map[string]any{}
	}
	// Optional per-app policy: require note on approve (and always on reject).
	if _, has := result["require_note_on_approve"]; !has {
		if rec, err := a.findMaclawAppInstallRecord(appID); err == nil && rec != nil {
			if policy := maclawAppInstallApprovalPanelPolicy(rec); policy != nil {
				if v, ok := policy["require_note_on_approve"].(bool); ok && v {
					result["require_note_on_approve"] = true
				}
				if v, ok := policy["requireNoteOnApprove"].(bool); ok && v {
					result["require_note_on_approve"] = true
				}
			}
		}
	}
	sessionID := maclawAppWorkspaceSessionID(a, MaclawAppBusinessOperationInput{AppID: appID})
	view, buildErr := BuildMaclawAppApprovalAppView(result, sessionID)
	if buildErr != nil {
		result["app_view_error"] = buildErr.Error()
		result["app_view_opened"] = false
		return result, nil
	}
	opened := a.emitAgentView(view)
	result["app_view_opened"] = opened
	result["app_view_id"] = strings.TrimSpace(fmt.Sprint(view["id"]))
	if rev := parseAgentViewInt64(view["viewRevision"]); rev > 0 {
		result["view_revision"] = rev
	}
	if meta, _ := view["meta"].(map[string]interface{}); meta != nil {
		if ver := strings.TrimSpace(fmt.Sprint(meta["schemaVersion"])); ver != "" && ver != "<nil>" {
			result["schema_version"] = ver
		}
	}
	return result, nil
}

func normalizeMaclawAppPanelDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "approve", "approved", "yes", "true", "1":
		return "approved"
	case "reject", "rejected", "deny", "denied", "no", "false", "0":
		return "rejected"
	default:
		return ""
	}
}

func maclawAppWorkspaceSessionID(a *App, input MaclawAppBusinessOperationInput) string {
	if a != nil {
		if cfg, err := a.GetMISDataConfig(); err == nil {
			if uid := strings.TrimSpace(cfg.UserID); uid != "" {
				return uid
			}
		}
	}
	return "desktop-user"
}

func enrichMaclawAppBusinessInputFromInstall(a *App, input MaclawAppBusinessOperationInput) MaclawAppBusinessOperationInput {
	if a == nil || strings.TrimSpace(input.AppID) == "" {
		return input
	}
	rec, err := a.findMaclawAppInstallRecord(input.AppID)
	if err != nil || rec == nil {
		return input
	}
	if input.AppName == "" {
		input.AppName = strings.TrimSpace(rec.AppName)
	}
	ds := extractMaclawAppInstallDataSrv(rec)
	if ds == nil {
		return input
	}
	if input.PreferredAction == "" {
		input.PreferredAction = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "preferredAction", "preferred_action"))
	}
	if input.PreferredView == "" {
		input.PreferredView = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "preferredView", "preferred_view"))
	}
	if input.PreferredReport == "" {
		input.PreferredReport = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "preferredReport", "preferred_report"))
	}
	if input.PreferredDashboard == "" {
		input.PreferredDashboard = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "preferredDashboard", "preferred_dashboard"))
	}
	if input.DatasetID == "" {
		input.DatasetID = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "datasetID", "dataset_id", "dataset"))
	}
	if input.ObjectRole == "" {
		input.ObjectRole = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "objectRole", "object_role"))
	}
	if input.BlueprintID == "" {
		input.BlueprintID = firstNonEmptyMaclawAppString(maclawAppStringValue(ds, "blueprintID", "blueprint_id"))
	}
	return input
}

func extractMaclawAppInstallDataSrv(rec *maclawAppInstallRecord) map[string]any {
	if rec == nil {
		return nil
	}
	if pkg := anyMap(rec.Package); pkg != nil {
		if ds := anyMap(pkg["datasrv"]); ds != nil {
			return ds
		}
		if manifest := anyMap(pkg["manifest"]); manifest != nil {
			if ds := anyMap(manifest["datasrv"]); ds != nil {
				return ds
			}
		}
	}
	if reg := anyMap(rec.DataSrvRegistration); reg != nil {
		if ds := anyMap(reg["datasrv"]); ds != nil {
			return ds
		}
	}
	return nil
}

// maclawAppInstallApprovalPanelPolicy reads optional AppView approval panel
// policy from the install package:
//
//	governance.approval_panel.require_note_on_approve
//	appview.approval.require_note_on_approve
func maclawAppInstallApprovalPanelPolicy(rec *maclawAppInstallRecord) map[string]any {
	if rec == nil {
		return nil
	}
	candidates := []map[string]any{}
	if pkg := anyMap(rec.Package); pkg != nil {
		if g := anyMap(pkg["governance"]); g != nil {
			candidates = append(candidates, anyMap(g["approval_panel"]), anyMap(g["approvalPanel"]), anyMap(g["appview"]), anyMap(g["app_view"]))
		}
		if av := anyMap(pkg["appview"]); av != nil {
			candidates = append(candidates, anyMap(av["approval"]), av)
		}
		if av := anyMap(pkg["app_view"]); av != nil {
			candidates = append(candidates, anyMap(av["approval"]), av)
		}
		if manifest := anyMap(pkg["manifest"]); manifest != nil {
			if g := anyMap(manifest["governance"]); g != nil {
				candidates = append(candidates, anyMap(g["approval_panel"]), anyMap(g["approvalPanel"]))
			}
		}
	}
	for _, c := range candidates {
		if c == nil {
			continue
		}
		// Nested approval block preferred when present.
		if nested := anyMap(c["approval"]); nested != nil {
			return nested
		}
		if nested := anyMap(c["approval_panel"]); nested != nil {
			return nested
		}
		return c
	}
	return nil
}

// collectMaclawAppRegionBindings lists all non-empty preferred* targets.
// Order matches ExecuteMaclawAppBusinessOperation priority for the first tab
// when only one is set; when multiple are set, order is view → report → dashboard → action
// (browse-first UX for multi-region workspaces).
func collectMaclawAppRegionBindings(input MaclawAppBusinessOperationInput) []maclawAppRegionBinding {
	base := input
	var out []maclawAppRegionBinding
	add := func(id, label, kind, target string, apply func(*MaclawAppBusinessOperationInput)) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		single := base
		// Clear all preferred* so ExecuteMaclawAppBusinessOperation switch is unambiguous.
		single.PreferredAction = ""
		single.PreferredView = ""
		single.PreferredReport = ""
		single.PreferredDashboard = ""
		apply(&single)
		out = append(out, maclawAppRegionBinding{
			ID:     id,
			Label:  label,
			Kind:   kind,
			Target: target,
			Input:  single,
		})
	}
	// Multi-region order: browse → analyze → act.
	add("view", avTr("Records", "记录"), "view", input.PreferredView, func(in *MaclawAppBusinessOperationInput) {
		in.PreferredView = strings.TrimSpace(input.PreferredView)
	})
	add("report", avTr("Report", "报表"), "report", input.PreferredReport, func(in *MaclawAppBusinessOperationInput) {
		in.PreferredReport = strings.TrimSpace(input.PreferredReport)
	})
	add("dashboard", avTr("Dashboard", "仪表盘"), "dashboard", input.PreferredDashboard, func(in *MaclawAppBusinessOperationInput) {
		in.PreferredDashboard = strings.TrimSpace(input.PreferredDashboard)
	})
	add("action", avTr("Action", "业务动作"), "action", input.PreferredAction, func(in *MaclawAppBusinessOperationInput) {
		in.PreferredAction = strings.TrimSpace(input.PreferredAction)
	})
	// If only PreferredAction was set (legacy single-path), order still works.
	return out
}

func countSuccessfulMaclawAppRegionLoads(loads []maclawAppRegionLoad) int {
	n := 0
	for _, l := range loads {
		if l.Err == nil && l.Result != nil {
			n++
		}
	}
	return n
}

// BuildMaclawAppBusinessMultiAppView builds nav + main[] for multiple region loads.
func BuildMaclawAppBusinessMultiAppView(input MaclawAppBusinessOperationInput, loads []maclawAppRegionLoad, sessionID string) (map[string]interface{}, error) {
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		return nil, fmt.Errorf("app_id is required for business app view")
	}
	if len(loads) == 0 {
		return nil, fmt.Errorf("no region loads")
	}
	mains := make([]map[string]interface{}, 0, len(loads))
	nav := make([]map[string]interface{}, 0, len(loads))
	targets := make([]string, 0, len(loads))
	for _, load := range loads {
		var main map[string]interface{}
		mode := load.Binding.Kind
		target := load.Binding.Target
		if load.Err != nil {
			main = buildMaclawAppRegionErrorView(input, load.Binding, load.Err)
		} else {
			// Normalize kind to Execute mode names for mapping helpers.
			execMode := mode
			switch mode {
			case "view":
				execMode = "business_view"
			case "report":
				execMode = "business_report"
			case "dashboard":
				execMode = "business_dashboard"
			case "action":
				execMode = "business_action"
			}
			if m := strings.TrimSpace(fmt.Sprint(load.Result["mode"])); m != "" {
				execMode = m
			}
			if t := strings.TrimSpace(fmt.Sprint(load.Result["target"])); t != "" {
				target = t
			}
			main = buildMaclawAppBusinessMainAgentView(load.Binding.Input, load.Result, execMode, target)
			if main == nil {
				main = buildMaclawAppRegionErrorView(input, load.Binding, fmt.Errorf("empty mapped view"))
			}
		}
		// Unique main id per region so nav targetViewId resolves.
		mainID := "maclaw-app:region:" + firstNonEmptyMaclawAppString(appID, "app") + ":" + load.Binding.ID
		main["id"] = mainID
		if title := strings.TrimSpace(fmt.Sprint(main["title"])); title == "" || title == target {
			main["title"] = load.Binding.Label
		}
		// Region-specific preferred binding for refresh.
		appendAgentViewHiddenField(main, "_region_kind", load.Binding.Kind)
		appendAgentViewHiddenField(main, "_preferred_action", load.Binding.Input.PreferredAction)
		appendAgentViewHiddenField(main, "_preferred_view", load.Binding.Input.PreferredView)
		appendAgentViewHiddenField(main, "_preferred_report", load.Binding.Input.PreferredReport)
		appendAgentViewHiddenField(main, "_preferred_dashboard", load.Binding.Input.PreferredDashboard)
		mains = append(mains, main)
		nav = append(nav, map[string]interface{}{
			"id":           load.Binding.ID,
			"label":        load.Binding.Label,
			"targetViewId": mainID,
		})
		targets = append(targets, load.Binding.Target)
	}
	title := firstNonEmptyMaclawAppString(input.AppName, appID)
	desc := avTr("Multi-region DataSrv workspace", "DataSrv 多区域工作区")
	if n := countSuccessfulMaclawAppRegionLoads(loads); n > 0 {
		desc = fmt.Sprintf("%s · %d/%d", desc, n, len(loads))
	}
	view, err := BuildAppView(AppViewBuildInput{
		AppID:       appID,
		SessionID:   sessionID,
		Title:       title,
		Description: desc,
		Layout:      "workspace",
		Main:        mains,
		Nav:         nav,
		SchemaContract: map[string]interface{}{
			"schema":  appViewSchemaV1,
			"appId":   appID,
			"regions": targets,
		},
	})
	if err != nil {
		return nil, err
	}
	return view, nil
}

func buildMaclawAppRegionErrorView(input MaclawAppBusinessOperationInput, b maclawAppRegionBinding, err error) map[string]interface{} {
	msg := ""
	if err != nil {
		msg = strings.TrimSpace(err.Error())
	}
	if msg == "" {
		msg = "region load failed"
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"title":       b.Label,
		"description": avTr("This region failed to load. Switch tabs or refresh.", "该区域加载失败。可切换页签或刷新。"),
		"results": []map[string]interface{}{{
			"id":     "error",
			"title":  b.Target,
			"status": "error",
			"data": map[string]interface{}{
				"error": msg,
				"kind":  b.Kind,
			},
		}},
	}
}

// BuildMaclawAppBusinessAppView maps a single ExecuteMaclawAppBusinessOperation
// result into maclaw.appview.v1 (Phase 2 single-region path).
func BuildMaclawAppBusinessAppView(input MaclawAppBusinessOperationInput, result map[string]any, sessionID string) (map[string]interface{}, error) {
	appID := strings.TrimSpace(input.AppID)
	if appID == "" {
		appID = strings.TrimSpace(fmt.Sprint(result["app_id"]))
	}
	if appID == "" {
		return nil, fmt.Errorf("app_id is required for business app view")
	}
	mode := strings.TrimSpace(fmt.Sprint(result["mode"]))
	target := strings.TrimSpace(fmt.Sprint(result["target"]))
	title := firstNonEmptyMaclawAppString(input.AppName, appID)
	main := buildMaclawAppBusinessMainAgentView(input, result, mode, target)
	if main == nil {
		return nil, fmt.Errorf("unable to map business result to agent view")
	}
	mainID := strings.TrimSpace(fmt.Sprint(main["id"]))
	layout := "workspace"
	switch mode {
	case "business_report", "business_dashboard":
		layout = "report"
	case "business_action":
		layout = "record"
	case "business_view":
		layout = "workspace"
	}
	desc := strings.TrimSpace(mode + " · " + target)
	if status := strings.TrimSpace(fmt.Sprint(result["result_status"])); status != "" {
		desc = desc + " · " + status
	}
	navLabel := avTr("Result", "结果")
	switch mode {
	case "business_view":
		navLabel = avTr("Records", "记录")
	case "business_report":
		navLabel = avTr("Report", "报表")
	case "business_dashboard":
		navLabel = avTr("Dashboard", "仪表盘")
	case "business_action":
		navLabel = avTr("Action", "业务动作")
	}
	view, err := BuildAppView(AppViewBuildInput{
		AppID:       appID,
		SessionID:   sessionID,
		Title:       title,
		Description: desc,
		Layout:      layout,
		Main:        main,
		Nav: []map[string]interface{}{{
			"id":           "main",
			"label":        navLabel,
			"targetViewId": mainID,
		}},
		SchemaContract: map[string]interface{}{
			"schema": appViewSchemaV1,
			"appId":  appID,
			"mode":   mode,
			"target": target,
		},
	})
	if err != nil {
		return nil, err
	}
	attachMaclawAppWorkspaceBindingFields(view, input)
	return view, nil
}

func attachMaclawAppWorkspaceBindingFields(view map[string]interface{}, input MaclawAppBusinessOperationInput) {
	if view == nil {
		return
	}
	appendAgentViewHiddenField(view, "_preferred_action", strings.TrimSpace(input.PreferredAction))
	appendAgentViewHiddenField(view, "_preferred_view", strings.TrimSpace(input.PreferredView))
	appendAgentViewHiddenField(view, "_preferred_report", strings.TrimSpace(input.PreferredReport))
	appendAgentViewHiddenField(view, "_preferred_dashboard", strings.TrimSpace(input.PreferredDashboard))
	appendAgentViewHiddenField(view, "_dataset_id", strings.TrimSpace(input.DatasetID))
	appendAgentViewHiddenField(view, "_object_role", strings.TrimSpace(input.ObjectRole))
	appendAgentViewHiddenField(view, "_business_entity", strings.TrimSpace(input.BusinessEntity))
	appendAgentViewHiddenField(view, "_business_action", strings.TrimSpace(input.BusinessAction))
	appendAgentViewHiddenField(view, "_app_name", strings.TrimSpace(input.AppName))
}

// BuildMaclawAppApprovalAppView maps approval start/decision result to AppView.
// Pending instances use type=approval (in-panel approve/reject); terminal states
// show result_browser / progress.
func BuildMaclawAppApprovalAppView(started map[string]any, sessionID string) (map[string]interface{}, error) {
	if started == nil {
		return nil, fmt.Errorf("empty approval start result")
	}
	instance := extractMaclawAppApprovalInstanceMap(started)
	appID := firstNonEmptyMaclawAppString(stringMapValue(instance, "app_id"), stringMapValue(instance, "AppID"))
	if appID == "" {
		if inst, ok := started["instance"].(maclawAppApprovalInstance); ok {
			appID = inst.AppID
			instance = maclawAppApprovalInstanceToMap(inst)
		}
	}
	if appID == "" {
		return nil, fmt.Errorf("approval instance missing app_id")
	}
	title := firstNonEmptyMaclawAppString(stringMapValue(instance, "title"), stringMapValue(instance, "app_name"), appID)
	status := normalizeMaclawAppApprovalStatus(firstNonEmptyMaclawAppString(stringMapValue(instance, "status"), stringMapValue(instance, "business_status"), "pending"))
	mainID := "maclaw-app:approval:" + appID + ":main"
	sideID := "maclaw-app:approval:" + appID + ":side"
	instanceID := firstNonEmptyMaclawAppString(stringMapValue(instance, "instance_id"), stringMapValue(instance, "InstanceID"))
	approvalID := firstNonEmptyMaclawAppString(stringMapValue(instance, "approval_id"), stringMapValue(instance, "ApprovalID"), fmt.Sprint(started["approval_id"]))
	recordID := firstNonEmptyMaclawAppString(stringMapValue(instance, "record_id"), stringMapValue(instance, "RecordID"))

	pendingDecision := status == "pending" || status == "requires_input" || status == "attention"
	var main map[string]interface{}
	if pendingDecision {
		summary := title
		if node := firstNonEmptyMaclawAppString(stringMapValue(instance, "current_node")); node != "" {
			summary = title + " · " + node
		}
		requireNoteOnApprove := started["require_note_on_approve"] == true || started["requireNoteOnApprove"] == true
		notePlaceholder := avTr("Required when rejecting; optional when approving", "驳回必填；批准可选")
		if requireNoteOnApprove {
			notePlaceholder = avTr("Required for approve and reject", "批准与驳回均必填")
		}
		main = map[string]interface{}{
			"type":                "approval",
			"id":                  mainID,
			"title":               title,
			"description":         avTr("Review and decide in the task panel.", "在任务面板中审批并决策。"),
			"approveLabel":        avTr("Approve", "批准"),
			"rejectLabel":         avTr("Reject", "驳回"),
			"noteLabel":           avTr("Decision note", "审批意见"),
			"notePlaceholder":     notePlaceholder,
			"requireNote":         requireNoteOnApprove,
			"requireNoteOnReject": true,
			"action": map[string]interface{}{
				"summary": summary,
				"risk":    "medium",
				"effects": []string{
					avTr("Update approval instance status", "更新审批实例状态"),
					avTr("Sync decision to DataSrv when configured", "在已配置时同步决策到 DataSrv"),
				},
				"reviewData": instance,
				"parameters": map[string]interface{}{
					"app_id":      appID,
					"instance_id": instanceID,
					"approval_id": approvalID,
					"record_id":   recordID,
				},
			},
		}
	} else {
		main = map[string]interface{}{
			"type":        "result_browser",
			"id":          mainID,
			"title":       title,
			"description": avTr("Approval workflow instance", "审批工作流实例"),
			"results": []map[string]interface{}{{
				"id":       firstNonEmptyMaclawAppString(instanceID, approvalID, "instance"),
				"title":    title,
				"subtitle": firstNonEmptyMaclawAppString(stringMapValue(instance, "current_node"), stringMapValue(instance, "lane")),
				"status":   status,
				"data":     instance,
			}},
		}
		if progress := extractMaclawAppApprovalProgressResults(started); len(progress) > 0 {
			main["type"] = "progress"
			main["steps"] = progress
			delete(main, "results")
		}
	}

	side := map[string]interface{}{
		"type":        "result_browser",
		"id":          sideID,
		"title":       avTr("Feedback", "结果反馈"),
		"description": avTr("Sync and workflow feedback", "同步与工作流反馈"),
		"results": []map[string]interface{}{{
			"id":     "feedback",
			"title":  avTr("Result package", "结果包"),
			"status": status,
			"data": map[string]interface{}{
				"result_feedback": started["result_feedback"],
				"approval_id":     firstNonEmptyMaclawAppString(fmt.Sprint(started["approval_id"]), approvalID),
				"sync":            started["sync"],
				"workflow_run":    started["workflow_run"],
				"decision":        started["decision"],
				"decided":         started["decided"],
			},
		}},
	}

	view, err := BuildAppView(AppViewBuildInput{
		AppID:       appID,
		SessionID:   sessionID,
		Title:       title,
		Description: avTr("Approval workspace", "审批工作区") + " · " + status,
		Layout:      "record",
		Main:        main,
		Side:        side,
		Nav: []map[string]interface{}{{
			"id":           "instance",
			"label":        avTr("Instance", "实例"),
			"targetViewId": mainID,
		}},
		SchemaContract: map[string]interface{}{
			"schema":     appViewSchemaV1,
			"appId":      appID,
			"kind":       "enterprise_approval_app",
			"approvalId": approvalID,
			"status":     status,
		},
	})
	if err != nil {
		return nil, err
	}
	appendAgentViewHiddenField(view, "_app_name", stringMapValue(instance, "app_name"))
	appendAgentViewHiddenField(view, "_approval_instance_id", instanceID)
	appendAgentViewHiddenField(view, "_approval_id", approvalID)
	appendAgentViewHiddenField(view, "_record_id", recordID)
	appendAgentViewHiddenField(view, "_approval_workspace", "1")
	return view, nil
}

func extractMaclawAppApprovalInstanceMap(started map[string]any) map[string]any {
	if started == nil {
		return map[string]any{}
	}
	if m := anyMap(started["instance"]); m != nil {
		return m
	}
	if inst, ok := started["instance"].(maclawAppApprovalInstance); ok {
		return maclawAppApprovalInstanceToMap(inst)
	}
	if wr := anyMap(started["workflow_run"]); wr != nil {
		if m := anyMap(wr["instance"]); m != nil {
			return m
		}
		if inst, ok := wr["instance"].(maclawAppApprovalInstance); ok {
			return maclawAppApprovalInstanceToMap(inst)
		}
	}
	return map[string]any{}
}

func maclawAppApprovalInstanceToMap(inst maclawAppApprovalInstance) map[string]any {
	// JSON round-trip keeps field naming consistent for the UI.
	raw, err := jsonMarshalMaclawApp(inst)
	if err != nil {
		return map[string]any{
			"app_id":      inst.AppID,
			"app_name":    inst.AppName,
			"instance_id": inst.InstanceID,
			"approval_id": inst.ApprovalID,
			"title":       inst.Title,
			"status":      inst.Status,
		}
	}
	out := map[string]any{}
	_ = jsonUnmarshalMaclawApp(raw, &out)
	return out
}

func extractMaclawAppApprovalProgressResults(started map[string]any) []map[string]interface{} {
	var list []any
	if wr := anyMap(started["workflow_run"]); wr != nil {
		list = anySlice(wr["progress_instances"])
		if len(list) == 0 {
			list = anySlice(wr["progressInstances"])
		}
	}
	if len(list) == 0 {
		list = anySlice(started["progress_instances"])
	}
	if len(list) == 0 {
		return nil
	}
	steps := make([]map[string]interface{}, 0, len(list))
	for i, item := range list {
		m := anyMap(item)
		if m == nil {
			if inst, ok := item.(maclawAppApprovalInstance); ok {
				m = maclawAppApprovalInstanceToMap(inst)
			}
		}
		if m == nil {
			continue
		}
		status := firstNonEmptyMaclawAppString(stringMapValue(m, "status"), stringMapValue(m, "business_status"), "pending")
		stepStatus := "pending"
		switch strings.ToLower(status) {
		case "done", "approved", "completed", "success":
			stepStatus = "done"
		case "running", "pending", "in_progress":
			stepStatus = "running"
		case "failed", "rejected", "error":
			stepStatus = "error"
		}
		steps = append(steps, map[string]interface{}{
			"id":          firstNonEmptyMaclawAppString(stringMapValue(m, "instance_id"), fmt.Sprintf("step-%d", i+1)),
			"title":       firstNonEmptyMaclawAppString(stringMapValue(m, "current_node"), stringMapValue(m, "title"), fmt.Sprintf("Step %d", i+1)),
			"status":      stepStatus,
			"description": firstNonEmptyMaclawAppString(stringMapValue(m, "business_note"), stringMapValue(m, "result")),
		})
	}
	return steps
}

func jsonMarshalMaclawApp(v any) ([]byte, error) {
	return json.Marshal(v)
}

func jsonUnmarshalMaclawApp(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func buildMaclawAppBusinessMainAgentView(input MaclawAppBusinessOperationInput, result map[string]any, mode, target string) map[string]interface{} {
	appID := firstNonEmptyMaclawAppString(input.AppID, stringMapValue(result, "app_id"))
	status := firstNonEmptyMaclawAppString(stringMapValue(result, "result_status"), stringMapValue(result, "business_status"), "done")
	title := firstNonEmptyMaclawAppString(target, input.AppName, appID, avTr("Business result", "业务结果"))
	viewID := "maclaw-app:result:" + firstNonEmptyMaclawAppString(appID, "app") + ":" + firstNonEmptyMaclawAppString(mode, "result")

	if rows := extractMaclawAppResultRows(result); len(rows) > 0 && (mode == "business_view" || mode == "business_report") {
		columns := inferMaclawAppTableColumns(rows)
		return map[string]interface{}{
			"type":        "table_editor",
			"id":          viewID,
			"title":       title,
			"description": avTr("DataSrv query result. Edit is local-only unless you re-run.", "DataSrv 查询结果。编辑仅在本地面板，除非重新运行。"),
			"columns":     columns,
			"rows":        rows,
			"submitLabel": avTr("Refresh", "刷新"),
			"formErrors":  []string{},
		}
	}

	results := []map[string]interface{}{}
	if outputs := anySlice(result["outputs"]); len(outputs) > 0 {
		for i, item := range outputs {
			out := anyMap(item)
			if out == nil {
				continue
			}
			results = append(results, map[string]interface{}{
				"id":       firstNonEmptyMaclawAppString(stringMapValue(out, "id"), fmt.Sprintf("output-%d", i+1)),
				"title":    firstNonEmptyMaclawAppString(stringMapValue(out, "title"), target, avTr("Output", "输出")),
				"subtitle": firstNonEmptyMaclawAppString(stringMapValue(out, "kind"), mode),
				"status":   firstNonEmptyMaclawAppString(stringMapValue(out, "status"), status),
				"data":     cloneMapAny(out),
			})
		}
	}
	if len(results) == 0 {
		payload := anyMap(result["result_payload"])
		if payload == nil {
			payload = cloneMapAny(result)
		}
		results = append(results, map[string]interface{}{
			"id":     "primary",
			"title":  title,
			"status": status,
			"data":   payload,
		})
	}
	if artifacts := anySlice(result["artifacts"]); len(artifacts) > 0 {
		for i, item := range artifacts {
			art := anyMap(item)
			if art == nil {
				continue
			}
			results = append(results, map[string]interface{}{
				"id":       firstNonEmptyMaclawAppString(stringMapValue(art, "id"), fmt.Sprintf("artifact-%d", i+1)),
				"title":    firstNonEmptyMaclawAppString(stringMapValue(art, "name"), stringMapValue(art, "title"), avTr("Artifact", "产物")),
				"subtitle": firstNonEmptyMaclawAppString(stringMapValue(art, "uri"), stringMapValue(art, "kind")),
				"status":   firstNonEmptyMaclawAppString(stringMapValue(art, "status"), status),
				"data":     cloneMapAny(art),
			})
		}
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          viewID,
		"title":       title,
		"description": avTr("Business operation completed.", "业务操作已完成。"),
		"results":     results,
	}
}

func extractMaclawAppResultRows(result map[string]any) []map[string]interface{} {
	if result == nil {
		return nil
	}
	candidates := []any{
		result["records"],
		result["rows"],
		result["items"],
		result["results"],
	}
	if payload := anyMap(result["result_payload"]); payload != nil {
		candidates = append(candidates, payload["records"], payload["rows"], payload["items"], payload["results"])
	}
	if outputs := anySlice(result["outputs"]); len(outputs) > 0 {
		if out := anyMap(outputs[0]); out != nil {
			if data := anyMap(out["data"]); data != nil {
				candidates = append(candidates, data["rows"], data["records"], data["items"])
			}
			candidates = append(candidates, out["rows"], out["records"])
		}
	}
	for _, raw := range candidates {
		if rows := normalizeMaclawAppRowMaps(raw); len(rows) > 0 {
			return rows
		}
	}
	return nil
}

func normalizeMaclawAppRowMaps(raw any) []map[string]interface{} {
	list := anySlice(raw)
	if len(list) == 0 {
		// Also accept []map[string]interface{}
		if typed, ok := raw.([]map[string]interface{}); ok {
			return typed
		}
		return nil
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m := anyMap(item); m != nil {
			cp := map[string]interface{}{}
			for k, v := range m {
				cp[k] = v
			}
			out = append(out, cp)
			continue
		}
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
			continue
		}
		out = append(out, map[string]interface{}{"value": item})
	}
	return out
}

func inferMaclawAppTableColumns(rows []map[string]interface{}) []map[string]interface{} {
	if len(rows) == 0 {
		return []map[string]interface{}{{"name": "value", "label": "value", "type": "text"}}
	}
	seen := map[string]bool{}
	var names []string
	for _, row := range rows {
		if len(names) >= 12 {
			break
		}
		for k := range row {
			if seen[k] {
				continue
			}
			seen[k] = true
			names = append(names, k)
			if len(names) >= 12 {
				break
			}
		}
		if len(names) >= 8 {
			break
		}
	}
	if len(names) == 0 {
		return []map[string]interface{}{{"name": "value", "label": "value", "type": "text"}}
	}
	cols := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		typ := "text"
		for _, row := range rows[:minInt(len(rows), 5)] {
			switch row[name].(type) {
			case float64, float32, int, int32, int64:
				typ = "number"
			case bool:
				typ = "boolean"
			}
			if typ != "text" {
				break
			}
		}
		cols = append(cols, map[string]interface{}{
			"name":  name,
			"label": name,
			"type":  typ,
		})
	}
	return cols
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (a *App) handleMaclawAppWorkspaceSubmit(payload AgentViewSubmitPayload) *IMAgentResponse {
	data := payload.Data
	if data == nil {
		data = map[string]interface{}{}
	}
	appID := firstNonEmptyMaclawAppString(payload.AppID, agentViewSubmitAppID(payload), stringMapValue(mapAnyFromInterface(data), "app_id"), stringMapValue(mapAnyFromInterface(data), appViewIDField))
	if appID == "" {
		if id := strings.TrimSpace(payload.ViewID); strings.HasPrefix(id, "app:") {
			parts := strings.Split(id, ":")
			if len(parts) >= 2 {
				appID = parts[1]
			}
		}
	}
	if appID == "" {
		return &IMAgentResponse{
			Text:           avTr("Missing app id for workspace refresh.", "刷新应用工作区时缺少 appId。"),
			Error:          agentViewMissingAppIDErr,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	// In-panel approve/reject from type=approval buttons.
	if isMaclawAppApprovalPanelSubmit(payload, data) {
		return a.handleMaclawAppApprovalPanelSubmit(payload, data, appID)
	}
	// Non-decision refresh on approval workspace — keep panel.
	if agentViewStringFromAny(data["_approval_workspace"]) == "1" || agentViewStringFromAny(data["_approval_instance_id"]) != "" {
		return &IMAgentResponse{
			Text:           avTr("Approval workspace is open in the task panel.", "审批工作区已在任务面板中打开。"),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
			KeepPanel:      true,
		}
	}
	input := MaclawAppBusinessOperationInput{
		AppID:              appID,
		AppName:            agentViewStringFromAny(data["_app_name"]),
		DatasetID:          agentViewStringFromAny(data["_dataset_id"]),
		ObjectRole:         agentViewStringFromAny(data["_object_role"]),
		BusinessEntity:     agentViewStringFromAny(data["_business_entity"]),
		BusinessAction:     agentViewStringFromAny(data["_business_action"]),
		BusinessNote:       firstNonEmptyMaclawAppString(agentViewStringFromAny(data["q"]), agentViewStringFromAny(data["note"]), agentViewStringFromAny(data["business_note"])),
		PreferredAction:    agentViewStringFromAny(data["_preferred_action"]),
		PreferredView:      agentViewStringFromAny(data["_preferred_view"]),
		PreferredReport:    agentViewStringFromAny(data["_preferred_report"]),
		PreferredDashboard: agentViewStringFromAny(data["_preferred_dashboard"]),
		Limit:              50,
	}
	if input.PreferredAction == "" && input.PreferredView == "" && input.PreferredReport == "" && input.PreferredDashboard == "" {
		input = enrichMaclawAppBusinessInputFromInstall(a, input)
	}
	result, err := a.OpenMaclawAppBusinessWorkspace(input)
	if err != nil {
		return &IMAgentResponse{
			Text:           avTr("Failed to refresh app workspace.", "刷新应用工作区失败。"),
			Error:          err.Error(),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	_ = result
	return &IMAgentResponse{
		Text:           avTr("App workspace refreshed in the task panel.", "应用工作区已在任务面板中刷新。"),
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
		KeepPanel:      true,
	}
}

func isMaclawAppApprovalPanelSubmit(payload AgentViewSubmitPayload, data map[string]interface{}) bool {
	if data == nil {
		return false
	}
	if _, ok := data["approved"].(bool); ok {
		return true
	}
	if agentViewStringFromAny(data["decision"]) != "" && (agentViewStringFromAny(data["_approval_workspace"]) == "1" || agentViewStringFromAny(data["_approval_instance_id"]) != "") {
		return true
	}
	_ = payload
	return false
}

func (a *App) handleMaclawAppApprovalPanelSubmit(payload AgentViewSubmitPayload, data map[string]interface{}, appID string) *IMAgentResponse {
	decision := ""
	if approved, ok := data["approved"].(bool); ok {
		if approved {
			decision = "approved"
		} else {
			decision = "rejected"
		}
	} else {
		decision = normalizeMaclawAppPanelDecision(agentViewStringFromAny(data["decision"]))
	}
	params := map[string]interface{}{}
	if raw, ok := data["parameters"].(map[string]interface{}); ok {
		params = raw
	}
	instanceID := firstNonEmptyMaclawAppString(
		agentViewStringFromAny(data["_approval_instance_id"]),
		agentViewStringFromAny(params["instance_id"]),
	)
	approvalID := firstNonEmptyMaclawAppString(
		agentViewStringFromAny(data["_approval_id"]),
		agentViewStringFromAny(params["approval_id"]),
		fmt.Sprint(params["approval_id"]),
	)
	recordID := firstNonEmptyMaclawAppString(
		agentViewStringFromAny(data["_record_id"]),
		agentViewStringFromAny(params["record_id"]),
	)
	if appID == "" {
		appID = firstNonEmptyMaclawAppString(agentViewStringFromAny(params["app_id"]), agentViewSubmitAppID(payload))
	}
	note := firstNonEmptyMaclawAppString(agentViewStringFromAny(data["note"]), agentViewStringFromAny(data["business_note"]))
	if decision == "rejected" && strings.TrimSpace(note) == "" {
		return &IMAgentResponse{
			Text:           avTr("A note is required when rejecting.", "驳回时必须填写意见。"),
			Error:          "note_required_on_reject",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
			KeepPanel:      true,
		}
	}
	// Policy-enforced note on approve is validated inside Decide…; surface a stable error code when possible.
	open := true
	result, err := a.DecideMaclawAppApprovalInstance(MaclawAppApprovalDecisionInput{
		AppID:       appID,
		InstanceID:  instanceID,
		ApprovalID:  approvalID,
		RecordID:    recordID,
		Decision:    decision,
		Note:        note,
		OpenAppView: &open,
	})
	if err != nil {
		msg := avTr("Failed to apply approval decision.", "应用审批决策失败。")
		code := err.Error()
		switch {
		case strings.Contains(err.Error(), "when rejecting"):
			msg = avTr("A note is required when rejecting.", "驳回时必须填写意见。")
			code = "note_required_on_reject"
		case strings.Contains(err.Error(), "when approving"):
			msg = avTr("A note is required when approving.", "批准时必须填写意见。")
			code = "note_required_on_approve"
		}
		return &IMAgentResponse{
			Text:           msg,
			Error:          code,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
			KeepPanel:      true,
		}
	}
	msg := avTr("Approval decision applied.", "审批决策已生效。")
	if result != nil {
		if d := strings.TrimSpace(fmt.Sprint(result["decision"])); d != "" {
			msg = msg + " (" + d + ")"
		}
		if result["already_final"] == true {
			msg = avTr("Approval was already final.", "审批已是终态。")
		}
	}
	_ = payload
	return &IMAgentResponse{
		Text:           msg,
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
		KeepPanel:      true,
	}
}

func agentViewStringFromAny(v interface{}) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return s
}

func mapAnyFromInterface(m map[string]interface{}) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
