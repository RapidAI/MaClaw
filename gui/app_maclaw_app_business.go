package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ExecuteMaclawAppBusinessOperation runs the DataSrv/MIS binding for an
// enterprise_normal_app that has no dedicated appSkill runtime. It keeps
// credentials in the Go backend while the GUI remains a visual operation shell.
func (a *App) ExecuteMaclawAppBusinessOperation(input MaclawAppBusinessOperationInput) (map[string]any, error) {
	cfg, err := a.GetMISDataConfig()
	if err != nil {
		return nil, fmt.Errorf("load MIS data config failed: %w", err)
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("MIS data service is disabled. Enable it in Settings > MIS data")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("MIS data service token is empty. Configure it in Settings > MIS data")
	}
	limit := input.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	base := map[string]any{
		"_maclaw_app":         true,
		"app_id":              strings.TrimSpace(input.AppID),
		"app_name":            strings.TrimSpace(input.AppName),
		"dataset_id":          strings.TrimSpace(input.DatasetID),
		"object_role":         strings.TrimSpace(input.ObjectRole),
		"blueprint_id":        strings.TrimSpace(input.BlueprintID),
		"business_entity":     strings.TrimSpace(input.BusinessEntity),
		"business_action":     strings.TrimSpace(input.BusinessAction),
		"business_note":       strings.TrimSpace(input.BusinessNote),
		"preferred_action":    strings.TrimSpace(input.PreferredAction),
		"preferred_view":      strings.TrimSpace(input.PreferredView),
		"preferred_report":    strings.TrimSpace(input.PreferredReport),
		"preferred_dashboard": strings.TrimSpace(input.PreferredDashboard),
	}
	for key, value := range input.Data {
		base[key] = value
	}
	actionID := strings.TrimSpace(input.PreferredAction)
	var raw []byte
	mode := ""
	target := ""
	switch {
	case actionID != "":
		mode = "business_action"
		target = actionID
		body := map[string]interface{}{"data": mapAnyToInterfaceMap(base), "dry_run": input.DryRun}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/business-actions/"+pathEscape(actionID)+"/execute", compactPayload(body))
	case strings.TrimSpace(input.PreferredView) != "":
		mode = "business_view"
		target = strings.TrimSpace(input.PreferredView)
		body := map[string]interface{}{"q": strings.TrimSpace(input.BusinessNote), "filter": mapAnyToInterfaceMap(input.Filter), "limit": limit}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/views/"+pathEscape(target)+"/query", compactPayload(body))
	case strings.TrimSpace(input.PreferredReport) != "":
		mode = "business_report"
		target = strings.TrimSpace(input.PreferredReport)
		body := map[string]interface{}{"filter": mapAnyToInterfaceMap(input.Filter), "limit": limit}
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/reports/"+pathEscape(target)+"/run", compactPayload(body))
	case strings.TrimSpace(input.PreferredDashboard) != "":
		mode = "business_dashboard"
		target = strings.TrimSpace(input.PreferredDashboard)
		raw, err = a.callMISDataAPIBytes(cfg, http.MethodPost, "/api/v1/data/dashboards/"+pathEscape(target)+"/run", nil)
	default:
		return nil, fmt.Errorf("enterprise normal app has no DataSrv operation binding")
	}
	if err != nil {
		return nil, err
	}
	response := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &response); err != nil {
			response["raw"] = strings.TrimSpace(string(raw))
		}
	}
	resultStatus := firstNonEmptyMaclawAppString(stringMapValue(response, "result_status"), stringMapValue(response, "status"), "done")
	resultPayload, outputs, artifacts, primaryResult := maclawAppBusinessOperationResultPackage(input, mode, target, resultStatus, response)
	return map[string]any{
		"synced":          true,
		"mode":            mode,
		"target":          target,
		"app_id":          strings.TrimSpace(input.AppID),
		"dataset_id":      strings.TrimSpace(input.DatasetID),
		"object_role":     strings.TrimSpace(input.ObjectRole),
		"business_action": strings.TrimSpace(input.BusinessAction),
		"result_status":   resultStatus,
		"business_status": firstNonEmptyMaclawAppString(stringMapValue(response, "business_status"), resultStatus),
		"primary_result":  primaryResult,
		"result_payload":  resultPayload,
		"outputs":         outputs,
		"artifacts":       artifacts,
		"response":        response,
	}, nil
}

func maclawAppBusinessOperationResultPackage(input MaclawAppBusinessOperationInput, mode string, target string, resultStatus string, response map[string]any) (map[string]any, []map[string]any, []map[string]any, string) {
	resultPayload := cloneMapAny(anyMap(response["result_payload"]))
	if resultPayload == nil {
		resultPayload = cloneMapAny(response)
	}
	if resultPayload == nil {
		resultPayload = map[string]any{}
	}
	if strings.TrimSpace(input.AppID) != "" {
		resultPayload["app_id"] = strings.TrimSpace(input.AppID)
	}
	if strings.TrimSpace(input.DatasetID) != "" {
		resultPayload["dataset_id"] = strings.TrimSpace(input.DatasetID)
	}
	if strings.TrimSpace(input.ObjectRole) != "" {
		resultPayload["object_role"] = strings.TrimSpace(input.ObjectRole)
	}
	if strings.TrimSpace(input.BusinessAction) != "" {
		resultPayload["business_action"] = strings.TrimSpace(input.BusinessAction)
	}
	if resultStatus != "" {
		resultPayload["result_status"] = resultStatus
	}

	outputs := maclawAppBusinessOperationOutputs(response, mode, target, resultStatus)
	artifacts := maclawAppBusinessOperationArtifacts(response)
	primaryResult := firstNonEmptyMaclawAppString(stringMapValue(response, "primary_result"), stringMapValue(response, "primaryResult"))
	if primaryResult == "" {
		switch mode {
		case "business_action":
			if response["record"] != nil || stringMapValue(response, "record_id") != "" {
				primaryResult = "business_record"
			} else {
				primaryResult = "business_status"
			}
		case "business_view":
			primaryResult = "records"
		case "business_report":
			primaryResult = "report"
		case "business_dashboard":
			primaryResult = "dashboard"
		default:
			primaryResult = "content"
		}
	}
	return resultPayload, outputs, artifacts, primaryResult
}

func maclawAppBusinessOperationOutputs(response map[string]any, mode string, target string, resultStatus string) []map[string]any {
	if raw := anySlice(response["outputs"]); len(raw) > 0 {
		outputs := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if output := cloneMapAny(anyMap(item)); output != nil {
				outputs = append(outputs, output)
			}
		}
		if len(outputs) > 0 {
			return outputs
		}
	}
	kind := "content"
	switch mode {
	case "business_action":
		kind = "business_record"
	case "business_view":
		kind = "table"
	case "business_report":
		kind = "report"
	case "business_dashboard":
		kind = "dashboard"
	}
	output := map[string]any{
		"kind":   kind,
		"title":  target,
		"status": resultStatus,
		"data":   cloneMapAny(response),
	}
	if text := firstNonEmptyMaclawAppString(stringMapValue(response, "text"), stringMapValue(response, "summary"), stringMapValue(response, "message")); text != "" {
		output["text"] = text
	}
	return []map[string]any{output}
}

func maclawAppBusinessOperationArtifacts(response map[string]any) []map[string]any {
	raw := anySlice(response["artifacts"])
	if len(raw) == 0 {
		return []map[string]any{}
	}
	artifacts := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if artifact := cloneMapAny(anyMap(item)); artifact != nil {
			artifacts = append(artifacts, artifact)
		}
	}
	return artifacts
}
