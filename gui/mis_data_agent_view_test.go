package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contract "github.com/RapidAI/CodeClaw/corelib/structureddata"
)

func resetMISTransactionStoreForAgentViewTest(t *testing.T) {
	t.Helper()
	previousLang, _ := agentViewCurrentLang.Load().(string)
	setAgentViewLang("en")
	t.Cleanup(func() { setAgentViewLang(previousLang) })
	misTransactionStore.Lock()
	misTransactionStore.items = map[string]*misBusinessTransaction{}
	misTransactionStore.next = 0
	misTransactionStore.loadedPath = ""
	misTransactionStore.Unlock()
}

func TestBuildMISIntentAgentViewFromResolveResult(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	payload := map[string]interface{}{
		"query": "trip meal receipt",
		"matches": []interface{}{
			map[string]interface{}{
				"domain":             "finance",
				"title":              "Finance",
				"confidence":         0.86,
				"decision":           "auto_open_task_panel",
				"business_action_id": "finance.expense_submit",
				"business_object_id": "finance.expenses",
				"use_case": map[string]interface{}{
					"id":    "finance.submit_expense",
					"title": "Submit expense",
				},
				"next_steps": []interface{}{
					map[string]interface{}{
						"action":  "execute_business_action",
						"purpose": "business_write",
						"input_fields": []interface{}{
							map[string]interface{}{"key": "expense_no", "title": "Expense No", "type": "string", "required": true},
							map[string]interface{}{"key": "amount", "title": "Amount", "type": "number", "required": true},
							map[string]interface{}{"key": "status", "title": "Status", "type": "string", "config": map[string]interface{}{"enum": []interface{}{"submitted", "approved"}}},
						},
						"data_template": map[string]interface{}{"expense_no": "", "amount": 0, "status": "submitted"},
					},
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil {
		t.Fatal("expected agent view")
	}
	if view["type"] != "form" || view["id"] != "mis:intent:finance.expense_submit" {
		t.Fatalf("unexpected view identity: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) != 4 {
		t.Fatalf("unexpected fields: %#v", view["fields"])
	}
	if fields[1]["type"] != "number" {
		t.Fatalf("amount type=%v want number", fields[1]["type"])
	}
	if fields[2]["type"] != "select" {
		t.Fatalf("status type=%v want select", fields[2]["type"])
	}
	if fields[3]["type"] != "hidden" || fields[3]["name"] != misAgentViewTransactionField || fields[3]["value"] == "" {
		t.Fatalf("expected hidden transaction field, got %#v", fields[3])
	}
	if txn, ok := getMISBusinessTransaction(fields[3]["value"].(string)); !ok || txn.ActionID != "finance.expense_submit" || txn.BusinessObject != "finance.expenses" {
		t.Fatalf("expected transaction for intent form, got ok=%v txn=%#v", ok, txn)
	} else if txn.ActionSnapshot == nil || len(txn.ActionSnapshot.InputFields) != 3 || txn.ActionSnapshot.InputFields[0].Key != "expense_no" {
		t.Fatalf("expected intent form snapshot on transaction, got %#v", txn.ActionSnapshot)
	}
}

func TestBuildMISIntentAgentViewUsesTableEditorForLineItems(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	data := []byte(`{"query":"submit expense items","matches":[{"decision":"auto_open_task_panel","confidence":0.9,"domain":"finance","business_object_id":"finance.expenses","business_action_id":"finance.expense_items","use_case":{"title":"Expense items"},"next_steps":[{"action":"execute_business_action","purpose":"business_write","tool_call_template":{"action":"execute_business_action"},"input_fields":[{"key":"items","title":"Items","type":"array","required":true,"config":{"min_items":1,"columns":[{"name":"description","label":"Description","type":"text","required":true},{"name":"amount","label":"Amount","type":"number","required":true}]}}],"data_template":{"items":[{"description":"Taxi","amount":86}]}}]}]} `)
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil || view["type"] != "table_editor" || view["id"] != "mis:intent:finance.expense_items" {
		t.Fatalf("expected intent table_editor view, got %#v", view)
	}
	if view["dataKey"] != "items" {
		t.Fatalf("expected items dataKey, got %#v", view["dataKey"])
	}
	rows, ok := view["rows"].([]map[string]interface{})
	if !ok || len(rows) != 1 || rows[0]["description"] != "Taxi" {
		t.Fatalf("expected default rows from intent data_template, got %#v", view["rows"])
	}
	hiddenData, ok := view["hiddenData"].(map[string]interface{})
	if !ok || strings.TrimSpace(fmt.Sprint(hiddenData[misAgentViewTransactionField])) == "" {
		t.Fatalf("expected transaction hiddenData, got %#v", view["hiddenData"])
	}
	meta, ok := view["meta"].(map[string]interface{})
	if !ok || meta["source"] != "mis.resolve_intent" || meta["tool_call_template"] == nil {
		t.Fatalf("expected resolve_intent metadata, got %#v", view["meta"])
	}
}

func TestBuildMISIntentAgentViewUsesWizardForLargeForms(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	inputFields := []interface{}{}
	dataTemplate := map[string]interface{}{}
	for i := 0; i < 7; i++ {
		key := fmt.Sprintf("field_%d", i+1)
		inputFields = append(inputFields, map[string]interface{}{"key": key, "title": fmt.Sprintf("Field %d", i+1), "type": "text", "required": i == 0})
		dataTemplate[key] = ""
	}
	payload := map[string]interface{}{
		"query": "create detailed request",
		"matches": []interface{}{map[string]interface{}{
			"decision":           "auto_open_task_panel",
			"confidence":         0.91,
			"domain":             "hr",
			"business_object_id": "hr.requests",
			"business_action_id": "hr.large_form",
			"use_case":           map[string]interface{}{"title": "Large request"},
			"next_steps": []interface{}{map[string]interface{}{
				"action":             "execute_business_action",
				"purpose":            "business_write",
				"tool_call_template": map[string]interface{}{"action": "execute_business_action"},
				"input_fields":       inputFields,
				"data_template":      dataTemplate,
				"required_fields":    []interface{}{"field_1"},
				"body_template":      map[string]interface{}{"data": dataTemplate},
			}},
		}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil || view["type"] != "wizard" || view["id"] != "mis:intent:hr.large_form" {
		t.Fatalf("expected intent wizard view, got %#v", view)
	}
	steps, ok := view["steps"].([]map[string]interface{})
	if !ok || len(steps) < 2 {
		t.Fatalf("expected multiple wizard steps, got %#v", view["steps"])
	}
	meta, ok := view["meta"].(map[string]interface{})
	if !ok || meta["source"] != "mis.resolve_intent" || meta["confidence"] != 0.91 {
		t.Fatalf("expected resolve metadata on wizard, got %#v", view["meta"])
	}
}

func TestBuildMISIntentAgentViewBuildsChoiceForm(t *testing.T) {
	data := []byte(`{"query":"x","matches":[{"decision":"ask_user_to_choose","confidence":0.62,"domain":"finance","business_object_id":"finance.expenses","business_action_id":"finance.expense_submit","use_case":{"title":"Submit expense"},"next_steps":[]}]} `)
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil || view["type"] != "form" || view["id"] != "mis:choose-intent" {
		t.Fatalf("unexpected choice view: %#v", view)
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) != 1 || fields[0]["type"] != "select" {
		t.Fatalf("unexpected choice fields: %#v", view["fields"])
	}
}

func TestBuildMISIntentChoiceAgentViewCarriesCandidateSnapshots(t *testing.T) {
	data := []byte(`{"query":"x","matches":[{"decision":"ask_user_to_choose","confidence":0.62,"domain":"finance","business_object_id":"finance.expenses","business_action_id":"finance.expense_submit","use_case":{"title":"Submit expense"},"next_steps":[{"action":"execute_business_action","purpose":"business_write","input_fields":[{"key":"amount","title":"Amount","type":"number","required":true}],"data_template":{"amount":0}}]}]} `)
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil || view["type"] != "form" || view["id"] != "mis:choose-intent" {
		t.Fatalf("unexpected choice view: %#v", view)
	}
	fields := view["fields"].([]map[string]interface{})
	if len(fields) != 2 || fields[1]["type"] != "hidden" || fields[1]["name"] != "business_action_snapshots" {
		t.Fatalf("expected hidden candidate snapshot field, got %#v", fields)
	}
	snapshots, ok := fields[1]["value"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected snapshot map, got %#v", fields[1]["value"])
	}
	snapshot := misActionSnapshotFromAny(snapshots["finance.expense_submit"])
	if snapshot == nil || len(snapshot.InputFields) != 1 || snapshot.InputFields[0].Key != "amount" {
		t.Fatalf("expected decodable form snapshot, got %#v", snapshots["finance.expense_submit"])
	}
}

func TestHandleAgentViewControlMessageAcceptsNonMISSubmit(t *testing.T) {
	app := &App{}
	resp, handled, err := app.handleAgentViewControlMessage(`__agent_view_submit__ {"view_id":"other","data":{"x":1}}`)
	if err != nil {
		t.Fatalf("handleAgentViewControlMessage: %v", err)
	}
	if !handled || resp == nil || resp.ResponseSource != "agent_view_submit" {
		t.Fatalf("unexpected response: handled=%v resp=%#v", handled, resp)
	}
}

func TestHandleAgentViewControlMessageRejectsInvalidDismissJSON(t *testing.T) {
	app := &App{}
	resp, handled, err := app.handleAgentViewControlMessage(`__agent_view_dismiss__ {`)
	if err != nil {
		t.Fatalf("handleAgentViewControlMessage: %v", err)
	}
	if !handled || resp == nil || resp.ResponseSource != "agent_view_dismiss" || resp.Error == "" {
		t.Fatalf("expected localized dismiss parse error, handled=%v resp=%#v", handled, resp)
	}
}

func TestBuildMISIntentAgentViewOffersResumeForMatchingTransaction(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)

	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi", map[string]interface{}{"amount": 86}, "test")
	data := []byte(`{"query":"taxi","matches":[{"decision":"auto_open_task_panel","confidence":0.91,"domain":"finance","business_object_id":"finance.expenses","business_action_id":"finance.expense_submit","use_case":{"title":"Submit expense"},"next_steps":[{"action":"execute_business_action","purpose":"business_write","input_fields":[{"key":"amount","title":"Amount","type":"number","required":true}],"data_template":{"amount":0}}]}]} `)
	view := buildMISIntentAgentViewFromResolveResult(data)
	if view == nil || view["type"] != "form" || view["id"] != "mis:resume-transaction" {
		t.Fatalf("expected resume choice view, got %#v", view)
	}
	fields := view["fields"].([]map[string]interface{})
	options := fields[0]["options"].([]map[string]string)
	if len(options) != 1 || options[0]["value"] != txnID {
		t.Fatalf("expected transaction option %s, got %#v", txnID, options)
	}
}

func TestBuildMISBusinessActionInputAgentViewRestoresTransactionFields(t *testing.T) {
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi", map[string]interface{}{"amount": 86}, "test")
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok {
		t.Fatal("expected transaction")
	}
	view := buildMISBusinessActionInputAgentViewForTransaction(contract.BusinessAction{
		ID:        "finance.expense_submit",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "amount",
			Title:    "Amount",
			Type:     "number",
			Required: true,
		}},
	}, txn)
	fields := view["fields"].([]map[string]interface{})
	if fields[0]["value"] != 86 {
		t.Fatalf("expected restored amount value, got %#v", fields[0])
	}
	if fields[1]["type"] != "hidden" || fields[1]["value"] != txnID {
		t.Fatalf("expected hidden transaction id, got %#v", fields[1])
	}
}

func TestBuildMISTransactionWorkspaceAgentView(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi", map[string]interface{}{"amount": 86}, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{ID: "finance.expense_submit", Title: "Submit expense", DatasetID: "finance.expenses"}))
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok {
		t.Fatal("expected transaction")
	}
	view := buildMISTransactionWorkspaceAgentView([]misBusinessTransaction{*txn})
	if view == nil || view["type"] != "result_browser" || view["id"] != "mis:transaction-workspace" {
		t.Fatalf("expected workspace result browser, got %#v", view)
	}
	results := view["results"].([]map[string]interface{})
	if len(results) != 1 || results[0]["id"] != txnID {
		t.Fatalf("expected workspace transaction card, got %#v", results)
	}
	if results[0]["title"] != "Submit expense" {
		t.Fatalf("expected form title in workspace card, got %#v", results[0])
	}
	actions := results[0]["actions"].([]map[string]interface{})
	if len(actions) != 1 || actions[0]["viewId"] != "mis:resume-transaction" {
		t.Fatalf("expected direct resume action, got %#v", actions)
	}
	actionData := actions[0]["data"].(map[string]interface{})
	if actionData["transaction_id"] != txnID {
		t.Fatalf("expected resume action transaction id, got %#v", actionData)
	}
	summaries := view["meta"].(map[string]interface{})["transactions"].([]map[string]interface{})
	if summaries[0]["has_form_snapshot"] != true || summaries[0]["form_title"] != "Submit expense" {
		t.Fatalf("expected snapshot metadata in workspace summary, got %#v", summaries[0])
	}
	if summaries[0]["next_action"] != "continue_editing" {
		t.Fatalf("expected next action in workspace summary, got %#v", summaries[0])
	}

	empty := buildMISTransactionWorkspaceAgentView(nil)
	if empty == nil || empty["type"] != "result_browser" {
		t.Fatalf("expected empty result browser, got %#v", empty)
	}
}

func TestToolMISDataListAgentTransactionsUsesLocalSnapshot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	misTransactionStore.Lock()
	misTransactionStore.items = map[string]*misBusinessTransaction{}
	misTransactionStore.next = 0
	misTransactionStore.loadedPath = ""
	misTransactionStore.Unlock()

	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi", map[string]interface{}{"amount": 86}, "test")
	if err := saveMISBusinessTransactions(misTransactionStorePath(app)); err != nil {
		t.Fatalf("saveMISBusinessTransactions: %v", err)
	}
	misTransactionStore.Lock()
	misTransactionStore.items = map[string]*misBusinessTransaction{}
	misTransactionStore.loadedPath = ""
	misTransactionStore.next = 0
	misTransactionStore.Unlock()

	out := app.executeMISDataTool(map[string]interface{}{"action": "list_agent_transactions"})
	if !strings.Contains(out, txnID) || !strings.Contains(out, "finance.expense_submit") {
		t.Fatalf("expected local transaction in tool output, got %s", out)
	}
}

func TestBuildMISBusinessActionDryRunAgentViewApproval(t *testing.T) {
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "", nil, "test")
	view := buildMISBusinessActionDryRunAgentViewWithTransaction("finance.expense_submit", map[string]interface{}{"amount": 12}, []byte(`{"valid":true}`), txnID)
	if view == nil || view["type"] != "approval" || view["id"] != "mis:commit:finance.expense_submit" {
		t.Fatalf("unexpected approval view: %#v", view)
	}
	action, ok := view["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing action: %#v", view)
	}
	params, ok := action["parameters"].(map[string]interface{})
	if !ok || params["draft_id"] == "" {
		t.Fatalf("expected draft-backed parameters: %#v", action["parameters"])
	}
	if params["transaction_id"] != txnID {
		t.Fatalf("expected transaction id in approval parameters: %#v", params)
	}
	if _, leaksData := params["data"]; leaksData {
		t.Fatalf("approval parameters should not carry submitted data: %#v", params)
	}
	if reviewData, ok := action["reviewData"].(map[string]interface{}); !ok || reviewData["amount"] == nil {
		t.Fatalf("expected visible review data: %#v", action["reviewData"])
	}
}

func TestBuildMISBusinessActionCommitReviewAgentViewRecreatesDraft(t *testing.T) {
	view := buildMISBusinessActionCommitReviewAgentView("finance.expense_submit", map[string]interface{}{"amount": 86}, "mistxn-1", "", "")
	if view == nil || view["type"] != "approval" || view["id"] != "mis:commit:finance.expense_submit" {
		t.Fatalf("unexpected commit review view: %#v", view)
	}
	action := view["action"].(map[string]interface{})
	params := action["parameters"].(map[string]interface{})
	if params["draft_id"] == "" || params["transaction_id"] != "mistxn-1" {
		t.Fatalf("expected recreated draft parameters, got %#v", params)
	}
	data, ok := getMISAgentViewDraft(params["draft_id"].(string), "finance.expense_submit")
	if !ok || data["amount"] != 86 {
		t.Fatalf("expected draft-backed commit data, ok=%v data=%#v", ok, data)
	}
}

func TestBuildMISBusinessActionDryRunAgentViewInvalid(t *testing.T) {
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "", nil, "test")
	view := buildMISBusinessActionDryRunAgentViewWithTransaction("finance.expense_submit", map[string]interface{}{"amount": 0}, []byte(`{"valid":false,"validation":{"errors":["amount is required"]},"action":{"id":"finance.expense_submit","input_fields":[{"key":"amount","title":"Amount","type":"number","required":true}]}}`), txnID)
	if view == nil || view["type"] != "form" || view["id"] != "mis:intent:finance.expense_submit" {
		t.Fatalf("unexpected validation view: %#v", view)
	}
	if errors, ok := view["formErrors"].([]string); !ok || len(errors) == 0 {
		t.Fatalf("expected form errors: %#v", view["formErrors"])
	}
	fields, ok := view["fields"].([]map[string]interface{})
	if !ok || len(fields) != 2 || fields[0]["value"] != 0.0 && fields[0]["value"] != 0 {
		t.Fatalf("expected editable submitted data field: %#v", view["fields"])
	}
	if fields[1]["type"] != "hidden" || fields[1]["value"] != txnID {
		t.Fatalf("expected hidden transaction field: %#v", fields)
	}
}

func TestBuildMISBusinessActionPendingValidationAgentViewRestoresSnapshotFields(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "", map[string]interface{}{"amount": 86}, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{
		ID:        "finance.expense_submit",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "create",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "amount",
			Title:    "Amount",
			Type:     "number",
			Required: true,
		}},
	}))
	view := buildMISBusinessActionPendingValidationAgentView("finance.expense_submit", map[string]interface{}{"amount": 86}, txnID, nil)
	if view == nil || view["type"] != "form" || view["id"] != "mis:intent:finance.expense_submit" {
		t.Fatalf("unexpected pending validation view: %#v", view)
	}
	if view["submitLabel"] != "Retry validation" {
		t.Fatalf("expected retry submit label, got %#v", view["submitLabel"])
	}
	fields := view["fields"].([]map[string]interface{})
	if len(fields) != 2 || fields[0]["name"] != "amount" || fields[0]["value"] != 86 {
		t.Fatalf("expected restored amount and hidden transaction field, got %#v", fields)
	}
	if fields[1]["type"] != "hidden" || fields[1]["value"] != txnID {
		t.Fatalf("expected hidden transaction id, got %#v", fields[1])
	}
}

func TestBuildMISBusinessActionCommitFailedAgentViewKeepsDraftRetry(t *testing.T) {
	view := buildMISBusinessActionCommitFailedAgentView("finance.expense_submit", map[string]interface{}{"amount": 86}, "mistxn-1", "misdraft-1", nil)
	if view == nil || view["type"] != "approval" || view["id"] != "mis:commit:finance.expense_submit" {
		t.Fatalf("unexpected commit retry view: %#v", view)
	}
	action := view["action"].(map[string]interface{})
	params := action["parameters"].(map[string]interface{})
	if params["draft_id"] != "misdraft-1" || params["transaction_id"] != "mistxn-1" {
		t.Fatalf("expected draft retry parameters, got %#v", params)
	}
	if _, leaksData := params["data"]; leaksData {
		t.Fatalf("retry parameters should keep data in draft, not inline: %#v", params)
	}
	if reviewData := action["reviewData"].(map[string]interface{}); reviewData["amount"] != 86 {
		t.Fatalf("expected visible review data, got %#v", reviewData)
	}
}

func TestMISTransactionNextActionTracksRecoverableStates(t *testing.T) {
	cases := map[string]string{
		"":                    "continue_editing",
		"collecting":          "continue_editing",
		"validation_failed":   "continue_editing",
		"awaiting_validation": "retry_validation",
		"awaiting_commit":     "review_or_retry_commit",
		"commit_failed":       "review_or_retry_commit",
		"validating":          "retry_or_wait_validation",
	}
	for state, want := range cases {
		if got := misTransactionNextAction(misBusinessTransaction{State: state}); got != want {
			t.Fatalf("state %q next action=%q want %q", state, got, want)
		}
	}
}

func TestHandleMISAgentViewSubmitStoresAwaitingValidationWhenServiceUnavailable(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	app := &App{testHomeDir: t.TempDir()}
	app.ensureMISBusinessTransactionsLoaded()
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "", nil, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{
		ID:        "finance.expense_submit",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "create",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "amount",
			Title:    "Amount",
			Type:     "number",
			Required: true,
		}},
	}))

	resp := app.handleAgentViewSubmitPayload(AgentViewSubmitPayload{
		ViewID: "mis:intent:finance.expense_submit",
		Data: map[string]interface{}{
			"amount":                     float64(86),
			misAgentViewTransactionField: txnID,
		},
	})
	if resp == nil || !strings.Contains(resp.Text, "saved locally") {
		t.Fatalf("expected local save response, got %#v", resp)
	}
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok || txn.State != "awaiting_validation" {
		t.Fatalf("expected awaiting_validation transaction, ok=%v txn=%#v", ok, txn)
	}
	if value := txn.Fields["amount"].Value; value != float64(86) {
		t.Fatalf("expected submitted amount to be stored, got %#v", value)
	}
	if !isRecoverableMISTransactionState("awaiting_validation") {
		t.Fatal("awaiting_validation must remain recoverable")
	}
}

func TestMISTransactionStoresSpecializedUIPayloadByBusinessField(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	txnID := createMISBusinessTransaction("approval.choose_approver", "approval.requests", "approval", "create", "", nil, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{
		ID:        "approval.choose_approver",
		Domain:    "approval",
		DatasetID: "approval.requests",
		Operation: "create",
		InputFields: []contract.DatasetTemplateField{
			{Key: "approvers", Title: "Approvers", Type: "user_ref", Required: true},
			{Key: "import_mapping", Title: "Import Mapping", Type: "field_mapper"},
		},
	}))

	mapping := map[string]interface{}{"amount": "Amount", "receipt_no": "Receipt No"}
	payload := map[string]interface{}{
		"approvers":                  []interface{}{"u1", "u2"},
		"import_mapping":             mapping,
		misAgentViewTransactionField: txnID,
	}
	submittedData := sanitizeMISAgentViewSubmittedData(payload)
	updateMISBusinessTransactionFields(extractMISAgentViewTransactionID(payload), submittedData, "user_input", true, 1)
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok {
		t.Fatalf("expected transaction %s", txnID)
	}
	if _, leaked := txn.Fields[misAgentViewTransactionField]; leaked {
		t.Fatalf("transaction hidden field leaked into business fields: %#v", txn.Fields)
	}
	approvers, ok := txn.Fields["approvers"].Value.([]interface{})
	if !ok || len(approvers) != 2 || approvers[0] != "u1" {
		t.Fatalf("expected approvers field to be stored, got %#v", txn.Fields["approvers"].Value)
	}
	if got := txn.Fields["import_mapping"].Value; fmt.Sprint(got) != fmt.Sprint(mapping) {
		t.Fatalf("expected import mapping field to be stored, got %#v", got)
	}
}

func TestMISAgentViewSubmittedDataSanitizesTransactionField(t *testing.T) {
	data := map[string]interface{}{
		"amount":                     float64(86),
		"approvers":                  []interface{}{"u1", "u2"},
		"import_mapping":             map[string]interface{}{"amount": "Amount"},
		misAgentViewTransactionField: "mistxn-test",
		"_mis_internal":              "drop",
	}
	clean := sanitizeMISAgentViewSubmittedData(data)
	if clean["amount"] != float64(86) {
		t.Fatalf("amount not preserved: %#v", clean)
	}
	if _, ok := clean[misAgentViewTransactionField]; ok {
		t.Fatalf("transaction field leaked into submitted data: %#v", clean)
	}
	if _, ok := clean["_mis_internal"]; ok {
		t.Fatalf("internal field leaked into submitted data: %#v", clean)
	}
	if approvers, ok := clean["approvers"].([]interface{}); !ok || len(approvers) != 2 {
		t.Fatalf("resource picker payload not preserved: %#v", clean)
	}
	if mapping, ok := clean["import_mapping"].(map[string]interface{}); !ok || mapping["amount"] != "Amount" {
		t.Fatalf("field mapper payload not preserved: %#v", clean)
	}
}

func TestBuildMISBusinessActionCommittedAgentViewIncludesTransactionAudit(t *testing.T) {
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "", map[string]interface{}{"amount": 86}, "test")
	updateMISBusinessTransactionFields(txnID, map[string]interface{}{"amount": 86}, "user_input", true, 1)
	markMISBusinessTransaction(txnID, "committed", "business_action.committed", "done", nil)
	view := buildMISBusinessActionCommittedAgentViewWithTransaction("finance.expense_submit", []byte(`{"event":{"record_id":"rec-1"},"valid":true}`), txnID)
	results := view["results"].([]map[string]interface{})
	data := results[0]["data"].(map[string]interface{})
	if data["transaction_id"] != txnID || data["transaction_state"] != "committed" {
		t.Fatalf("missing transaction summary: %#v", data)
	}
	provenance, ok := data["field_provenance"].(map[string]misTransactionField)
	if !ok || !provenance["amount"].Confirmed || provenance["amount"].Source != "user_input" {
		t.Fatalf("missing field provenance: %#v", data["field_provenance"])
	}
	fieldSummary, ok := data["field_summary"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing field summary: %#v", data)
	}
	amount, ok := fieldSummary["amount"].(map[string]interface{})
	if !ok || amount["value"] != 86 || amount["source"] != "user_input" || amount["confirmed"] != true {
		t.Fatalf("unexpected amount field summary: %#v", fieldSummary["amount"])
	}
}

func TestMISTransactionFieldSummaryDataPreviewsSpecializedValues(t *testing.T) {
	now := time.Now()
	summary := misTransactionFieldSummaryData(map[string]misTransactionField{
		"approvers": {
			Value:       []interface{}{"u1", "u2"},
			Source:      "user_input",
			Confirmed:   true,
			Confidence:  1,
			ConfirmedAt: &now,
		},
		"import_mapping": {
			Value:     map[string]interface{}{"amount": "Amount", "receipt_no": "Receipt No"},
			Source:    "user_input",
			Confirmed: true,
		},
		"_mis_transaction_id": {
			Value: "hidden",
		},
	})
	if _, leaked := summary["_mis_transaction_id"]; leaked {
		t.Fatalf("internal field leaked into summary: %#v", summary)
	}
	approvers := summary["approvers"].(map[string]interface{})
	if approvers["value"] != "2 selected" || approvers["confirmed"] != true || approvers["confidence"] != float64(1) {
		t.Fatalf("unexpected approvers summary: %#v", approvers)
	}
	mapping := summary["import_mapping"].(map[string]interface{})
	if mapping["value"] != "amount, receipt_no" {
		t.Fatalf("unexpected mapping summary: %#v", mapping)
	}
}

func TestMISBusinessTransactionStorePersistsSnapshot(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	path := filepath.Join(t.TempDir(), "mis_transactions.json")
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi expense", map[string]interface{}{"amount": 86}, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{
		ID:        "finance.expense_submit",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "create",
		Title:     "Submit expense",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "amount",
			Title:    "Amount",
			Type:     "number",
			Required: true,
		}},
	}))
	updateMISBusinessTransactionFields(txnID, map[string]interface{}{"amount": 86}, "user_input", true, 1)
	markMISBusinessTransaction(txnID, "awaiting_commit", "business_action.validation_passed", "ok", nil)
	if err := saveMISBusinessTransactions(path); err != nil {
		t.Fatalf("saveMISBusinessTransactions: %v", err)
	}

	misTransactionStore.Lock()
	misTransactionStore.items = map[string]*misBusinessTransaction{}
	misTransactionStore.loadedPath = ""
	misTransactionStore.next = 0
	misTransactionStore.Unlock()

	if err := ensureMISBusinessTransactionsLoaded(path); err != nil {
		t.Fatalf("ensureMISBusinessTransactionsLoaded: %v", err)
	}
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok {
		t.Fatalf("expected transaction %s after reload", txnID)
	}
	if txn.State != "awaiting_commit" || txn.Fields["amount"].Source != "user_input" || !txn.Fields["amount"].Confirmed {
		t.Fatalf("unexpected reloaded transaction: %#v", txn)
	}
	if txn.ActionSnapshot == nil || txn.ActionSnapshot.Title != "Submit expense" || len(txn.ActionSnapshot.InputFields) != 1 {
		t.Fatalf("expected persisted action snapshot, got %#v", txn.ActionSnapshot)
	}
}

func TestHandleMISTransactionResumeUsesLocalActionSnapshotWhenServiceUnavailable(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	app := &App{testHomeDir: t.TempDir()}
	app.ensureMISBusinessTransactionsLoaded()
	txnID := createMISBusinessTransaction("finance.expense_submit", "finance.expenses", "finance", "create", "taxi expense", map[string]interface{}{"amount": 86}, "test")
	setMISBusinessTransactionActionSnapshot(txnID, misActionSnapshotFromBusinessAction(contract.BusinessAction{
		ID:        "finance.expense_submit",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "create",
		Title:     "Submit expense",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "amount",
			Title:    "Amount",
			Type:     "number",
			Required: true,
		}},
	}))

	resp := app.handleMISTransactionResumeAgentViewSubmit(map[string]interface{}{"transaction_id": txnID})
	if resp == nil || resp.Error != "" || !strings.Contains(resp.Text, "local form snapshot") {
		t.Fatalf("expected local snapshot resume response, got %#v", resp)
	}
	txn, ok := getMISBusinessTransaction(txnID)
	if !ok || txn.State != "collecting" {
		t.Fatalf("expected resumed transaction, ok=%v txn=%#v", ok, txn)
	}
}

func TestBuildMISAgentViewFieldsArrayTable(t *testing.T) {
	fields := buildMISAgentViewFields([]contract.DatasetTemplateField{{
		Key:      "items",
		Title:    "Expense items",
		Type:     "array",
		Required: true,
		Config: map[string]any{
			"min_items":    1,
			"max_items":    8,
			"unique_items": true,
			"columns": []interface{}{
				map[string]interface{}{"name": "category", "label": "Category", "type": "select", "enum": []interface{}{"transport", "meal"}, "format": "expense-category", "fixed_value": "meal", "read_only": true},
				map[string]interface{}{"name": "amount", "label": "Amount", "type": "number", "required": true, "min": 1, "max": 5000, "multiple_of": 0.01},
				map[string]interface{}{"name": "token", "label": "Token", "type": "text", "write_only": true},
			},
		},
	}}, nil, map[string]interface{}{
		"items": []interface{}{map[string]interface{}{"category": "meal", "amount": float64(86)}},
	}, nil)
	if len(fields) != 1 {
		t.Fatalf("expected one field, got %#v", fields)
	}
	if fields[0]["type"] != "array_table" {
		t.Fatalf("expected array_table field, got %#v", fields[0])
	}
	if fields[0]["minItems"] != float64(1) && fields[0]["minItems"] != 1 {
		t.Fatalf("expected minItems constraint, got %#v", fields[0])
	}
	if fields[0]["maxItems"] != float64(8) && fields[0]["maxItems"] != 8 {
		t.Fatalf("expected maxItems constraint, got %#v", fields[0])
	}
	if fields[0]["uniqueItems"] != true {
		t.Fatalf("expected uniqueItems constraint, got %#v", fields[0])
	}
	columns, ok := fields[0]["columns"].([]map[string]interface{})
	if !ok || len(columns) != 3 {
		t.Fatalf("expected table columns, got %#v", fields[0]["columns"])
	}
	if columns[0]["type"] != "select" || columns[1]["type"] != "number" {
		t.Fatalf("unexpected column types: %#v", columns)
	}
	if columns[0]["format"] != "expense-category" {
		t.Fatalf("expected category format hint, got %#v", columns)
	}
	if columns[0]["constValue"] != "meal" {
		t.Fatalf("expected category const constraint, got %#v", columns)
	}
	if columns[0]["readOnly"] != true {
		t.Fatalf("expected category readOnly annotation, got %#v", columns)
	}
	if columns[1]["required"] != true {
		t.Fatalf("expected required amount column, got %#v", columns)
	}
	if columns[1]["min"] != float64(1) && columns[1]["min"] != 1 {
		t.Fatalf("expected amount min constraint, got %#v", columns)
	}
	if columns[1]["max"] != float64(5000) && columns[1]["max"] != 5000 {
		t.Fatalf("expected amount max constraint, got %#v", columns)
	}
	if columns[1]["step"] != float64(0.01) {
		t.Fatalf("expected amount step constraint, got %#v", columns)
	}
	if columns[2]["sensitive"] != true {
		t.Fatalf("expected token sensitive annotation, got %#v", columns)
	}
}

func TestBuildMISAgentViewFieldsObjectForm(t *testing.T) {
	fields := buildMISAgentViewFields([]contract.DatasetTemplateField{{
		Key:      "metadata",
		Title:    "Metadata",
		Type:     "object",
		Required: true,
		Config: map[string]any{
			"fields": []interface{}{
				map[string]interface{}{"name": "source", "label": "Source", "type": "text", "min_length": 2, "max_length": 64, "regex": "^[a-z]+$"},
				map[string]interface{}{"name": "reviewed", "label": "Reviewed", "type": "boolean", "required": true},
			},
		},
	}}, nil, map[string]interface{}{
		"metadata": map[string]interface{}{"source": "email"},
	}, nil)
	if len(fields) != 1 {
		t.Fatalf("expected one field, got %#v", fields)
	}
	if fields[0]["type"] != "object_form" {
		t.Fatalf("expected object_form field, got %#v", fields[0])
	}
	columns, ok := fields[0]["columns"].([]map[string]interface{})
	if !ok || len(columns) != 2 {
		t.Fatalf("expected object form columns, got %#v", fields[0]["columns"])
	}
	if columns[1]["type"] != "boolean" {
		t.Fatalf("expected boolean reviewed column, got %#v", columns)
	}
	if columns[0]["minLength"] != float64(2) && columns[0]["minLength"] != 2 {
		t.Fatalf("expected source minLength constraint, got %#v", columns)
	}
	if columns[0]["maxLength"] != float64(64) && columns[0]["maxLength"] != 64 {
		t.Fatalf("expected source maxLength constraint, got %#v", columns)
	}
	if columns[0]["pattern"] != "^[a-z]+$" {
		t.Fatalf("expected source pattern constraint, got %#v", columns)
	}
	if columns[1]["required"] != true {
		t.Fatalf("expected required reviewed column, got %#v", columns)
	}
}

func TestBuildMISBusinessActionInputAgentViewUsesTableEditorForLineItems(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	view := buildMISBusinessActionInputAgentView(contract.BusinessAction{
		ID:        "finance.expense_items",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "create",
		Title:     "Expense items",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "items",
			Title:    "Items",
			Type:     "array",
			Required: true,
			Config: map[string]any{
				"min_items": 1,
				"columns": []interface{}{
					map[string]interface{}{"name": "description", "label": "Description", "type": "text", "required": true},
					map[string]interface{}{"name": "amount", "label": "Amount", "type": "number", "required": true},
				},
			},
		}},
	})
	if view == nil || view["type"] != "table_editor" {
		t.Fatalf("expected table_editor view, got %#v", view)
	}
	if view["dataKey"] != "items" {
		t.Fatalf("expected items dataKey, got %#v", view["dataKey"])
	}
	if view["minItems"] != float64(1) && view["minItems"] != 1 {
		t.Fatalf("expected minItems constraint, got %#v", view)
	}
	hiddenData, ok := view["hiddenData"].(map[string]interface{})
	if !ok || strings.TrimSpace(fmt.Sprint(hiddenData[misAgentViewTransactionField])) == "" {
		t.Fatalf("expected transaction id hiddenData, got %#v", view["hiddenData"])
	}
	columns, ok := view["columns"].([]map[string]interface{})
	if !ok || len(columns) != 2 || columns[1]["type"] != "number" {
		t.Fatalf("expected table editor columns, got %#v", view["columns"])
	}
}

func TestBuildMISBusinessActionInputAgentViewUsesWizardForLargeForms(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	inputFields := []contract.DatasetTemplateField{}
	for i := 0; i < 7; i++ {
		inputFields = append(inputFields, contract.DatasetTemplateField{
			Key:      fmt.Sprintf("field_%d", i+1),
			Title:    fmt.Sprintf("Field %d", i+1),
			Type:     "text",
			Required: i == 0,
		})
	}
	view := buildMISBusinessActionInputAgentView(contract.BusinessAction{
		ID:          "hr.large_form",
		Domain:      "hr",
		DatasetID:   "hr.requests",
		Operation:   "create",
		Title:       "Large request",
		InputFields: inputFields,
	})
	if view == nil || view["type"] != "wizard" {
		t.Fatalf("expected wizard view, got %#v", view)
	}
	steps, ok := view["steps"].([]map[string]interface{})
	if !ok || len(steps) < 2 {
		t.Fatalf("expected multiple wizard steps, got %#v", view["steps"])
	}
	firstFields, ok := steps[0]["fields"].([]map[string]interface{})
	if !ok || len(firstFields) == 0 {
		t.Fatalf("expected fields in first wizard step, got %#v", steps[0])
	}
	foundTransaction := false
	for _, field := range firstFields {
		if field["type"] == "hidden" && field["name"] == misAgentViewTransactionField {
			foundTransaction = true
		}
	}
	if !foundTransaction {
		t.Fatalf("expected transaction hidden field in wizard payload, got %#v", firstFields)
	}
}

func TestBuildMISBusinessActionInputAgentViewUsesResourcePicker(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	view := buildMISBusinessActionInputAgentView(contract.BusinessAction{
		ID:        "approval.choose_approver",
		Domain:    "approval",
		DatasetID: "approval.requests",
		Operation: "create",
		Title:     "Choose approvers",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "approvers",
			Title:    "Approvers",
			Type:     "user_ref",
			Required: true,
			Config: map[string]any{
				"resource_type": "employee",
				"multiple":      true,
				"options": []interface{}{
					map[string]interface{}{"id": "u1", "label": "Alice", "status": "Finance", "description": "Finance manager"},
					map[string]interface{}{"id": "u2", "label": "Bob", "status": "HR"},
				},
			},
		}},
	})
	if view == nil || view["type"] != "resource_picker" {
		t.Fatalf("expected resource_picker view, got %#v", view)
	}
	if view["resourceType"] != "employee" || view["multiple"] != true {
		t.Fatalf("expected employee multi picker, got %#v", view)
	}
	if view["dataKey"] != "approvers" {
		t.Fatalf("expected approvers dataKey, got %#v", view["dataKey"])
	}
	if hiddenData, ok := view["hiddenData"].(map[string]interface{}); !ok || strings.TrimSpace(fmt.Sprint(hiddenData[misAgentViewTransactionField])) == "" {
		t.Fatalf("expected transaction hiddenData, got %#v", view["hiddenData"])
	}
	options, ok := view["options"].([]map[string]string)
	if !ok || len(options) != 2 || options[0]["value"] != "u1" || options[0]["status"] != "Finance" {
		t.Fatalf("expected resource options, got %#v", view["options"])
	}
}

func TestBuildMISBusinessActionInputAgentViewUsesFieldMapper(t *testing.T) {
	resetMISTransactionStoreForAgentViewTest(t)
	view := buildMISBusinessActionInputAgentView(contract.BusinessAction{
		ID:        "import.map_fields",
		Domain:    "finance",
		DatasetID: "finance.expenses",
		Operation: "import",
		Title:     "Map import fields",
		InputFields: []contract.DatasetTemplateField{{
			Key:      "mapping",
			Title:    "Mapping",
			Type:     "field_mapper",
			Required: true,
			Config: map[string]any{
				"source_fields": []interface{}{"Amount", "Receipt No", "Ignored"},
				"target_fields": []interface{}{
					map[string]interface{}{"name": "amount", "label": "Amount", "type": "number", "required": true},
					map[string]interface{}{"name": "receipt_no", "label": "Receipt No", "type": "text"},
				},
			},
		}},
	})
	if view == nil || view["type"] != "field_mapper" {
		t.Fatalf("expected field_mapper view, got %#v", view)
	}
	if view["dataKey"] != "mapping" {
		t.Fatalf("expected mapping dataKey, got %#v", view["dataKey"])
	}
	if hiddenData, ok := view["hiddenData"].(map[string]interface{}); !ok || strings.TrimSpace(fmt.Sprint(hiddenData[misAgentViewTransactionField])) == "" {
		t.Fatalf("expected transaction hiddenData, got %#v", view["hiddenData"])
	}
	sourceFields, ok := view["sourceFields"].([]string)
	if !ok || len(sourceFields) != 3 || sourceFields[0] != "Amount" {
		t.Fatalf("expected source fields, got %#v", view["sourceFields"])
	}
	targetFields, ok := view["targetFields"].([]map[string]interface{})
	if !ok || len(targetFields) != 2 || targetFields[0]["required"] != true {
		t.Fatalf("expected target fields, got %#v", view["targetFields"])
	}
}
