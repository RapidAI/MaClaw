package structureddata

func applyBusinessViewResultPackage(result *BusinessViewResult) {
	if result == nil {
		return
	}
	rows := make([]map[string]any, 0, len(result.Records))
	for _, record := range result.Records {
		rows = append(rows, map[string]any{
			"id":         record.ID,
			"dataset_id": record.DatasetID,
			"title":      record.Title,
			"data":       cloneJSONMap(record.Data),
		})
	}
	status := "done"
	result.PrimaryResult = "records"
	result.BusinessStatus = status
	result.ResultStatus = status
	result.ResultPayload = map[string]any{
		"business_status": status,
		"result_status":   status,
		"view_id":         result.View.ID,
		"dataset_id":      result.View.DatasetID,
		"domain":          result.View.Domain,
		"records":         rows,
		"record_count":    len(rows),
		"has_more":        result.HasMore,
	}
	if result.NextBeforeID != "" {
		result.ResultPayload["next_before_id"] = result.NextBeforeID
	}
	result.Outputs = []map[string]any{{
		"kind":   "table",
		"title":  result.View.Title,
		"status": status,
		"data": map[string]any{
			"columns": append([]string(nil), result.View.Fields...),
			"rows":    rows,
		},
	}}
	result.Artifacts = []map[string]any{}
}

func applyReportResultPackage(result *ReportResult) {
	if result == nil {
		return
	}
	status := "ready"
	result.PrimaryResult = "report"
	result.BusinessStatus = status
	result.ResultStatus = status
	result.ResultPayload = map[string]any{
		"business_status": status,
		"result_status":   status,
		"report_id":       result.Report.ID,
		"dataset_id":      result.Report.DatasetID,
		"domain":          result.Report.Domain,
		"rows":            cloneJSONValue(result.Result.Rows),
		"row_count":       len(result.Result.Rows),
		"scanned":         result.Result.Scanned,
		"truncated":       result.Result.Truncated,
	}
	result.Outputs = []map[string]any{{
		"kind":   "report",
		"title":  result.Report.Title,
		"status": status,
		"data":   cloneJSONValue(result.Result),
	}}
	result.Artifacts = []map[string]any{}
}

func applyDashboardResultPackage(result *DashboardResult) {
	if result == nil {
		return
	}
	status := "ready"
	cards := make([]map[string]any, 0, len(result.Reports))
	for _, report := range result.Reports {
		card := map[string]any{
			"report_id": report.ReportID,
			"title":     report.Title,
		}
		if report.Error != "" {
			card["status"] = "error"
			card["error"] = report.Error
		} else {
			card["status"] = "ready"
		}
		if report.Result != nil {
			card["row_count"] = len(report.Result.Result.Rows)
			card["rows"] = cloneJSONValue(report.Result.Result.Rows)
		}
		cards = append(cards, card)
	}
	result.PrimaryResult = "dashboard"
	result.BusinessStatus = status
	result.ResultStatus = status
	result.ResultPayload = map[string]any{
		"business_status": status,
		"result_status":   status,
		"dashboard_id":    result.Dashboard.ID,
		"domain":          result.Dashboard.Domain,
		"cards":           cards,
		"card_count":      len(cards),
		"generated_at":    result.GeneratedAt,
	}
	result.Outputs = []map[string]any{{
		"kind":   "dashboard",
		"title":  result.Dashboard.Title,
		"status": status,
		"data": map[string]any{
			"cards": cards,
		},
	}}
	result.Artifacts = []map[string]any{}
}
