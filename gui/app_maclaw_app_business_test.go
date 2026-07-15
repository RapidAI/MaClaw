package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestExecuteMaclawAppBusinessOperationRunsPreferredAction(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		if r.Header.Get("X-MaClaw-User-ID") != "operator" {
			t.Fatalf("expected user header, got %q", r.Header.Get("X-MaClaw-User-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"committed","result_status":"success","record_id":"cust-1","record":{"id":"cust-1"}}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{
		AppID:           "customer-profile",
		AppName:         "Customer Profile",
		DatasetID:       "sales.customers",
		ObjectRole:      "customer",
		BlueprintID:     "sales.customer.v1",
		BusinessEntity:  "sales",
		BusinessAction:  "upsert",
		BusinessNote:    "new customer from card",
		PreferredAction: "sales.customer_upsert",
		Data:            map[string]any{"customer_name": "Acme"},
	})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation() error = %v", err)
	}
	if result["synced"] != true || result["mode"] != "business_action" || result["target"] != "sales.customer_upsert" || result["result_status"] != "success" {
		t.Fatalf("unexpected business operation result: %#v", result)
	}
	if result["primary_result"] != "business_record" || result["business_status"] != "success" {
		t.Fatalf("business action should expose standard result identity: %#v", result)
	}
	resultPayload, ok := result["result_payload"].(map[string]any)
	if !ok || resultPayload["app_id"] != "customer-profile" || resultPayload["dataset_id"] != "sales.customers" || resultPayload["object_role"] != "customer" || resultPayload["business_action"] != "upsert" || resultPayload["record_id"] != "cust-1" || resultPayload["result_status"] != "success" {
		t.Fatalf("business action should expose standard result payload: %#v", result["result_payload"])
	}
	outputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(outputs) != 1 || outputs[0]["kind"] != "business_record" || outputs[0]["title"] != "sales.customer_upsert" || outputs[0]["status"] != "success" {
		t.Fatalf("business action should expose default output package: %#v", result["outputs"])
	}
	artifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 0 {
		t.Fatalf("business action should expose empty artifact package: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/business-actions/sales.customer_upsert/execute" {
		t.Fatalf("unexpected request: %#v", captured)
	}
	data, ok := captured[0].Body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("request body missing data: %#v", captured[0].Body)
	}
	if captured[0].Body["dry_run"] != false || data["app_id"] != "customer-profile" || data["object_role"] != "customer" || data["preferred_action"] != "sales.customer_upsert" || data["customer_name"] != "Acme" {
		t.Fatalf("request body missing app business semantics: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationQueriesPreferredView(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","primary_result":"records","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","view_id":"sales.customer_directory","records":[{"id":"cust-1","dataset_id":"sales.customers"}],"record_count":1},"outputs":[{"kind":"table","title":"Customer directory","status":"ready","data":{"rows":[{"id":"cust-1"}]}}],"artifacts":[{"id":"cust-export","name":"customers.csv","uri":"artifact://customers.csv","status":"ready"}],"records":[{"id":"cust-1"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "customer-profile", PreferredView: "sales.customer_directory", BusinessNote: "Acme", Limit: 25})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation view error = %v", err)
	}
	if result["mode"] != "business_view" || result["target"] != "sales.customer_directory" || result["result_status"] != "ready" {
		t.Fatalf("unexpected view operation result: %#v", result)
	}
	if result["primary_result"] != "records" || result["business_status"] != "ready" {
		t.Fatalf("view operation should preserve upstream result identity: %#v", result)
	}
	resultPayload, ok := result["result_payload"].(map[string]any)
	if !ok || resultPayload["view_id"] != "sales.customer_directory" || resultPayload["record_count"] != float64(1) {
		t.Fatalf("view operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	outputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(outputs) != 1 || outputs[0]["kind"] != "table" || outputs[0]["title"] != "Customer directory" {
		t.Fatalf("view operation should preserve upstream outputs: %#v", result["outputs"])
	}
	artifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(artifacts) != 1 || artifacts[0]["id"] != "cust-export" {
		t.Fatalf("view operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/views/sales.customer_directory/query" {
		t.Fatalf("unexpected view request: %#v", captured)
	}
	if captured[0].Body["q"] != "Acme" || captured[0].Body["limit"] != float64(25) {
		t.Fatalf("view query body missing q/limit: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationRunsPreferredReport(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready","primary_result":"report","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","report_id":"procurement.purchase_by_status","rows":[{"id":"report-1"}],"row_count":1},"outputs":[{"kind":"report","title":"Purchase by status","status":"ready","data":{"rows":[{"id":"report-1"}]}}],"artifacts":[{"id":"report-pdf","name":"purchase-by-status.pdf","uri":"artifact://reports/purchase-by-status.pdf","status":"ready"}],"rows":[{"id":"report-1"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "purchase-report", PreferredReport: "procurement.purchase_by_status", Filter: map[string]any{"status": "open"}, Limit: 15})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation report error = %v", err)
	}
	if result["mode"] != "business_report" || result["target"] != "procurement.purchase_by_status" || result["result_status"] != "ready" {
		t.Fatalf("unexpected report operation result: %#v", result)
	}
	if result["primary_result"] != "report" || result["business_status"] != "ready" {
		t.Fatalf("report operation should preserve upstream result identity: %#v", result)
	}
	reportPayload, ok := result["result_payload"].(map[string]any)
	if !ok || reportPayload["report_id"] != "procurement.purchase_by_status" || reportPayload["row_count"] != float64(1) {
		t.Fatalf("report operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	reportOutputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(reportOutputs) != 1 || reportOutputs[0]["kind"] != "report" || reportOutputs[0]["title"] != "Purchase by status" {
		t.Fatalf("report operation should preserve upstream outputs: %#v", result["outputs"])
	}
	reportArtifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(reportArtifacts) != 1 || reportArtifacts[0]["id"] != "report-pdf" {
		t.Fatalf("report operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/reports/procurement.purchase_by_status/run" {
		t.Fatalf("unexpected report request: %#v", captured)
	}
	filter, ok := captured[0].Body["filter"].(map[string]interface{})
	if !ok || filter["status"] != "open" || captured[0].Body["limit"] != float64(15) {
		t.Fatalf("report body missing filter/limit: %#v", captured[0].Body)
	}
}

func TestExecuteMaclawAppBusinessOperationRunsPreferredDashboard(t *testing.T) {
	type capturedRequest struct {
		Method string
		Path   string
		Body   map[string]interface{}
	}
	captured := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := capturedRequest{Method: r.Method, Path: r.URL.Path}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		captured = append(captured, item)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","primary_result":"dashboard","business_status":"ready","result_status":"ready","result_payload":{"business_status":"ready","dashboard_id":"inventory.overview","cards":[{"id":"inventory"}],"card_count":1},"outputs":[{"kind":"dashboard","title":"Inventory overview","status":"ready","data":{"cards":[{"id":"inventory"}]}}],"artifacts":[{"id":"dashboard-shot","name":"inventory.png","uri":"artifact://dashboards/inventory.png","status":"ready"}],"cards":[{"id":"inventory"}]}`))
	}))
	defer server.Close()
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveMISDataConfig(corelib.MISDataConfig{Enabled: true, Endpoint: server.URL, Token: "token", TenantID: "tenant", UserID: "operator", Role: "ops"}); err != nil {
		t.Fatalf("SaveMISDataConfig() error = %v", err)
	}

	result, err := app.ExecuteMaclawAppBusinessOperation(maclawAppBusinessOperationInput{AppID: "inventory-dashboard", PreferredDashboard: "inventory.overview"})
	if err != nil {
		t.Fatalf("ExecuteMaclawAppBusinessOperation dashboard error = %v", err)
	}
	if result["mode"] != "business_dashboard" || result["target"] != "inventory.overview" || result["result_status"] != "ready" {
		t.Fatalf("unexpected dashboard operation result: %#v", result)
	}
	if result["primary_result"] != "dashboard" || result["business_status"] != "ready" {
		t.Fatalf("dashboard operation should preserve upstream result identity: %#v", result)
	}
	dashboardPayload, ok := result["result_payload"].(map[string]any)
	if !ok || dashboardPayload["dashboard_id"] != "inventory.overview" || dashboardPayload["card_count"] != float64(1) {
		t.Fatalf("dashboard operation should preserve upstream result payload: %#v", result["result_payload"])
	}
	dashboardOutputs, ok := result["outputs"].([]map[string]any)
	if !ok || len(dashboardOutputs) != 1 || dashboardOutputs[0]["kind"] != "dashboard" || dashboardOutputs[0]["title"] != "Inventory overview" {
		t.Fatalf("dashboard operation should preserve upstream outputs: %#v", result["outputs"])
	}
	dashboardArtifacts, ok := result["artifacts"].([]map[string]any)
	if !ok || len(dashboardArtifacts) != 1 || dashboardArtifacts[0]["id"] != "dashboard-shot" {
		t.Fatalf("dashboard operation should preserve upstream artifacts: %#v", result["artifacts"])
	}
	if len(captured) != 1 || captured[0].Method != http.MethodPost || captured[0].Path != "/api/v1/data/dashboards/inventory.overview/run" {
		t.Fatalf("unexpected dashboard request: %#v", captured)
	}
	if captured[0].Body != nil {
		t.Fatalf("dashboard request should not send a body: %#v", captured[0].Body)
	}
}
