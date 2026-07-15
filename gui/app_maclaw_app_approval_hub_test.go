package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestNormalizeMaclawAppHubDecisionWire(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"approve":  "approve",
		"approved": "approve",
		"reject":   "reject",
		"rejected": "reject",
		"escalate": "escalate",
		"maybe":    "",
	}
	for in, want := range cases {
		if got := normalizeMaclawAppHubDecisionWire(in); got != want {
			t.Fatalf("normalizeMaclawAppHubDecisionWire(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseMaclawAppHubTriggerResponse(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"instance":{"id":"inst-1","workflow_id":"wf-expense","current_node_id":"manager","status":"running"}}`)
	got, err := parseMaclawAppHubTriggerResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.InstanceID != "inst-1" || got.WorkflowID != "wf-expense" || got.CurrentNodeID != "manager" {
		t.Fatalf("parsed trigger = %#v", got)
	}
}

func TestApplyMaclawAppHubBinding(t *testing.T) {
	t.Parallel()
	inst := applyMaclawAppHubBinding(maclawAppApprovalInstance{AppID: "expense", InstanceID: "local-1", Status: "pending"}, "wf-1", "hub-inst", "node-a")
	if inst.ApprovalEngine != maclawAppApprovalEngineHub {
		t.Fatalf("engine=%q", inst.ApprovalEngine)
	}
	if inst.HubInstanceID != "hub-inst" || inst.HubNodeID != "node-a" || inst.HubWorkflowID != "wf-1" {
		t.Fatalf("binding fields = %#v", inst)
	}
	if inst.CurrentNode != "node-a" {
		t.Fatalf("current_node=%q", inst.CurrentNode)
	}
}

func TestBindOrTriggerMaclawAppHubWorkflowTriggersHub(t *testing.T) {
	var triggerHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/api/v1/workflows/") && strings.HasSuffix(r.URL.Path, "/trigger") {
			triggerHits++
			if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
				t.Errorf("missing bearer auth: %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), "record_id") {
				t.Errorf("trigger body missing record_id: %s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instance": map[string]any{
					"id":              "hub-inst-99",
					"workflow_id":     "wf-expense",
					"current_node_id": "mgr-approval",
					"status":          "running",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatal(err)
	}

	force := true
	input := MaclawAppApprovalWorkflowStartInput{
		AppID:              "expense-hub",
		RecordID:           "rec-1",
		WorkflowSkillID:    "expense-workflow",
		HubWorkflowID:      "wf-expense",
		TriggerHubWorkflow: &force,
		Applicant:          "alice",
		DatasetID:          "ds-expense",
		ObjectRole:         "expense_report",
		FormData:           map[string]any{"amount": 100},
	}
	base := maclawAppApprovalInstance{
		AppID:           "expense-hub",
		InstanceID:      "local-appr-1",
		RecordID:        "rec-1",
		Status:          "pending",
		Lane:            "my_requests",
		Title:           "Expense",
		WorkflowSkillID: "expense-workflow",
		DatasetID:       "ds-expense",
		ObjectRole:      "expense_report",
		Applicant:       "alice",
		Owner:           "alice",
	}
	bound, meta, err := app.bindOrTriggerMaclawAppHubWorkflow(base, input)
	if err != nil {
		t.Fatalf("bindOrTrigger error: %v", err)
	}
	if triggerHits != 1 {
		t.Fatalf("triggerHits=%d", triggerHits)
	}
	if !meta["triggered"].(bool) {
		t.Fatalf("meta=%v", meta)
	}
	if bound.HubInstanceID != "hub-inst-99" || bound.HubNodeID != "mgr-approval" || bound.ApprovalEngine != maclawAppApprovalEngineHub {
		t.Fatalf("bound=%#v", bound)
	}
}

func TestBindOrTriggerMaclawAppHubWorkflowSkipsWithoutCreds(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{}); err != nil {
		t.Fatal(err)
	}
	input := MaclawAppApprovalWorkflowStartInput{
		AppID:         "expense-hub",
		RecordID:      "rec-1",
		HubWorkflowID: "wf-expense",
	}
	base := maclawAppApprovalInstance{AppID: "expense-hub", InstanceID: "local-1", Status: "pending"}
	bound, meta, err := app.bindOrTriggerMaclawAppHubWorkflow(base, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta["skipped"] != true {
		t.Fatalf("meta=%v", meta)
	}
	if bound.ApprovalEngine != maclawAppApprovalEngineLocal {
		t.Fatalf("engine=%q", bound.ApprovalEngine)
	}
}

func TestDecideMaclawAppApprovalInstanceCallsHub(t *testing.T) {
	var decisionHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/decision") {
			decisionHits++
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"decision":"approve"`) {
				t.Errorf("decision body=%s", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"instance_id": "hub-inst-1",
				"node_id":     "mgr",
				"status":      "running",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatal(err)
	}

	// Install + instance so runtime contract passes.
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema: "maclaw.app.installs.v1",
		Installs: []maclawAppInstallRecord{{
			AppID:   "expense-hub",
			AppName: "Expense",
			Kind:    "enterprise_approval_app",
			WorkflowContract: map[string]any{
				"workflowSkillId": "expense-workflow",
				"workflowVersion": "1.0.0",
			},
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{
					Event:           "expense.submitted",
					ObjectRole:      "expense_report",
					WorkflowSkillID: "expense-workflow",
					WorkflowVersion: "1.0.0",
				}},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	inst := maclawAppApprovalInstance{
		AppID:           "expense-hub",
		InstanceID:      "local-1",
		Title:           "Expense",
		Status:          "pending",
		Lane:            "pending_my_approval",
		CurrentNode:     "mgr",
		Owner:           "alice",
		Applicant:       "alice",
		Approver:        "boss",
		ApprovalEngine:  maclawAppApprovalEngineHub,
		HubWorkflowID:   "wf-expense",
		HubInstanceID:   "hub-inst-1",
		HubNodeID:       "mgr",
		WorkflowSkillID: "expense-workflow",
		WorkflowVersion: "1.0.0",
		ObjectRole:      "expense_report",
		RecordID:        "rec-1",
	}
	stored, err := app.RecordMaclawAppApprovalInstance(inst)
	if err != nil {
		t.Fatalf("RecordMaclawAppApprovalInstance: %v", err)
	}

	open := false
	result, err := app.DecideMaclawAppApprovalInstance(MaclawAppApprovalDecisionInput{
		AppID:       stored.AppID,
		InstanceID:  stored.InstanceID,
		Decision:    "approve",
		Note:        "lgtm",
		Actor:       "boss",
		OpenAppView: &open,
	})
	if err != nil {
		t.Fatalf("Decide error: %v", err)
	}
	if decisionHits != 1 {
		t.Fatalf("decisionHits=%d result=%v", decisionHits, result)
	}
	if result["decided"] != true {
		t.Fatalf("result=%v", result)
	}
	outInst, _ := result["instance"].(maclawAppApprovalInstance)
	if normalizeMaclawAppApprovalStatus(outInst.Status) != "approved" {
		t.Fatalf("status=%q instance=%#v", outInst.Status, outInst)
	}
}

func TestDecideMaclawAppApprovalInstanceHubFailureMarksAttention(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      hub.URL,
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeMaclawAppInstallRegistry(maclawAppInstallRegistry{
		Schema: "maclaw.app.installs.v1",
		Installs: []maclawAppInstallRecord{{
			AppID:   "expense-hub",
			AppName: "Expense",
			Kind:    "enterprise_approval_app",
			WorkflowContract: map[string]any{
				"workflowSkillId": "expense-workflow",
				"workflowVersion": "1.0.0",
			},
			VersionSnapshot: maclawAppInstallVersionSnapshot{
				ApprovalBindings: []maclawAppInstallApprovalBindingSnapshot{{
					Event:           "expense.submitted",
					ObjectRole:      "expense_report",
					WorkflowSkillID: "expense-workflow",
					WorkflowVersion: "1.0.0",
				}},
			},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	inst := maclawAppApprovalInstance{
		AppID:           "expense-hub",
		InstanceID:      "local-2",
		Title:           "Expense",
		Status:          "pending",
		Lane:            "pending_my_approval",
		CurrentNode:     "mgr",
		Owner:           "alice",
		ApprovalEngine:  maclawAppApprovalEngineHub,
		HubInstanceID:   "hub-inst-2",
		HubNodeID:       "mgr",
		WorkflowSkillID: "expense-workflow",
		WorkflowVersion: "1.0.0",
		ObjectRole:      "expense_report",
	}
	if _, err := app.RecordMaclawAppApprovalInstance(inst); err != nil {
		t.Fatal(err)
	}
	open := false
	result, err := app.DecideMaclawAppApprovalInstance(MaclawAppApprovalDecisionInput{
		AppID:       "expense-hub",
		InstanceID:  "local-2",
		Decision:    "approve",
		Note:        "try",
		OpenAppView: &open,
	})
	if err == nil {
		t.Fatalf("expected hub error, got result=%v", result)
	}
	if result["decided"] != false {
		t.Fatalf("expected decided=false, got %v", result)
	}
	outInst, _ := result["instance"].(maclawAppApprovalInstance)
	if normalizeMaclawAppApprovalStatus(outInst.Status) != "attention" {
		t.Fatalf("expected attention, got %#v", outInst)
	}
	if outInst.HubSyncError == "" {
		t.Fatalf("expected hub_sync_error")
	}
}

func TestNormalizeMaclawAppApprovalInstanceFieldsHubEngine(t *testing.T) {
	t.Parallel()
	inst := normalizeMaclawAppApprovalInstanceFields(maclawAppApprovalInstance{
		AppID:         "a",
		InstanceID:    "i",
		HubInstanceID: "h1",
		CurrentNode:   "n1",
		Status:        "pending",
		Lane:          "my_requests",
	})
	if inst.ApprovalEngine != maclawAppApprovalEngineHub {
		t.Fatalf("engine=%q", inst.ApprovalEngine)
	}
	if inst.HubNodeID != "n1" {
		t.Fatalf("hub_node_id=%q", inst.HubNodeID)
	}
}

func TestListMaclawAppApprovalInstancesAllMergesHubPending(t *testing.T) {
	var pendingHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/directory/pending-action" {
			pendingHits++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"instance_id":    "hub-inst-pend-1",
					"workflow_name":  "Leave Request",
					"status":         "running",
					"current_node":   "manager",
					"initiator_name": "Alice",
					"initiated_at":   "2026-07-01T10:00:00Z",
					"user_role":      "approver",
					"urgency":        "approaching_timeout",
				}},
				"total": 1, "page": 1, "page_size": 50,
			})
			return
		}
		// Other directory endpoints empty.
		if strings.HasPrefix(r.URL.Path, "/api/v1/directory/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-1",
	}); err != nil {
		t.Fatal(err)
	}
	// Seed a local non-hub instance so merge keeps both.
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1",
		Instances: []maclawAppApprovalInstance{{
			AppID: "expense-local", InstanceID: "local-1", Title: "Local expense",
			Status: "pending", Lane: "my_requests", Owner: "alice",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	all, err := app.ListMaclawAppApprovalInstancesAll("all", 50)
	if err != nil {
		t.Fatal(err)
	}
	if pendingHits < 1 {
		t.Fatalf("expected hub pending-action call, hits=%d", pendingHits)
	}
	var sawHub, sawLocal bool
	for _, inst := range all {
		if inst.HubInstanceID == "hub-inst-pend-1" && inst.Lane == "pending_my_approval" {
			sawHub = true
			if inst.ApprovalEngine != maclawAppApprovalEngineHub {
				t.Fatalf("hub item engine=%q", inst.ApprovalEngine)
			}
		}
		if inst.InstanceID == "local-1" {
			sawLocal = true
		}
	}
	if !sawHub || !sawLocal {
		t.Fatalf("merge incomplete hub=%v local=%v items=%#v", sawHub, sawLocal, all)
	}

	pendingOnly, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingOnly) == 0 {
		t.Fatal("expected pending_my_approval items from hub")
	}
	for _, inst := range pendingOnly {
		if normalizeMaclawAppApprovalLane(inst.Lane) != "pending_my_approval" && normalizeMaclawAppApprovalStatus(inst.Status) != "pending" {
			// lane filter should keep only pending_my_approval matches
		}
	}
}

func TestMaclawAppApprovalInstanceFromHubDirectoryItem(t *testing.T) {
	t.Parallel()
	inst := maclawAppApprovalInstanceFromHubDirectoryItem(map[string]any{
		"instance_id":    "i1",
		"workflow_name":  "Expense",
		"status":         "running",
		"current_node":   "n1",
		"initiator_name": "Bob",
		"user_role":      "approver",
	}, "pending_my_approval")
	if inst.HubInstanceID != "i1" || inst.HubNodeID != "n1" || inst.Lane != "pending_my_approval" {
		t.Fatalf("%#v", inst)
	}
	if inst.AppID != "hub-workflow" || inst.ApprovalEngine != maclawAppApprovalEngineHub {
		t.Fatalf("%#v", inst)
	}
}

func TestMaclawAppApprovalInstanceFromHubDirectoryItem_EscalationApprovers(t *testing.T) {
	t.Parallel()
	inst := maclawAppApprovalInstanceFromHubDirectoryItem(map[string]any{
		"instance_id":          "inst-esc-1",
		"current_node":         "approval-1",
		"workflow_name":        "Leave",
		"status":               "running",
		"urgency":              "normal",
		"escalation_pending":   true,
		"escalation_approvers": []any{"ve-a", "ve-c"},
	}, "my_requests")
	if normalizeMaclawAppApprovalStatus(inst.Status) != "attention" || normalizeMaclawAppApprovalLane(inst.Lane) != "attention" {
		t.Fatalf("status/lane=%s/%s", inst.Status, inst.Lane)
	}
	approvers := maclawAppStringSliceFromAny(inst.ResultPayload["escalation_approvers"])
	if len(approvers) != 2 {
		t.Fatalf("approvers=%v payload=%#v", approvers, inst.ResultPayload)
	}
	if pending, _ := inst.ResultPayload["escalation_pending"].(bool); !pending {
		t.Fatal("expected escalation_pending")
	}
}

func TestMaclawAppHubDirectoryPathsForLane(t *testing.T) {
	t.Parallel()
	if got := maclawAppHubDirectoryPathsForLane("pending_my_approval"); len(got) != 1 || got[0] != "/api/v1/directory/pending-action" {
		t.Fatalf("%#v", got)
	}
	if got := maclawAppHubDirectoryPathsForLane("all"); len(got) != 3 {
		t.Fatalf("%#v", got)
	}
}

func TestReconcileMaclawAppApprovalProjectionsAlignsHandled(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/directory/pending-action":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
		case "/api/v1/directory/initiated":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
		case "/api/v1/directory/completed":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{
					"instance_id":    "hub-done-1",
					"workflow_name":  "Leave",
					"status":         "completed",
					"current_node":   "end",
					"initiator_name": "Alice",
					"user_role":      "approver",
				}},
				"total": 1, "page": 1, "page_size": 50,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "m1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1",
		Instances: []maclawAppApprovalInstance{{
			AppID: "hub-workflow", InstanceID: "local-hub-1", Title: "Leave",
			Status: "pending", Lane: "pending_my_approval",
			ApprovalEngine: maclawAppApprovalEngineHub,
			HubInstanceID:  "hub-done-1", HubNodeID: "manager",
			Owner: "alice",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	summary, err := app.ReconcileMaclawAppApprovalProjections()
	if err != nil {
		t.Fatal(err)
	}
	if summary["updated"].(int) < 1 {
		t.Fatalf("expected updated>=1, got %v", summary)
	}
	list, err := app.listMaclawAppApprovalInstances("", "all", 50)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, inst := range list {
		if inst.HubInstanceID == "hub-done-1" {
			found = true
			if normalizeMaclawAppApprovalStatus(inst.Status) != "approved" && normalizeMaclawAppApprovalLane(inst.Lane) != "handled" {
				// completed maps to approved/handled
				if inst.Lane != "handled" && inst.Status != "approved" {
					t.Fatalf("expected handled/approved after reconcile, got status=%s lane=%s", inst.Status, inst.Lane)
				}
			}
		}
	}
	if !found {
		t.Fatalf("instance missing after reconcile: %#v", list)
	}
}

func TestReconcileSkipsWhenHubUnreachable(t *testing.T) {
	// Dead port — probe must fail and not mutate local pending.
	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       "http://127.0.0.1:1",
		RemoteMachineToken: "machine-token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1",
		Instances: []maclawAppApprovalInstance{{
			AppID: "hub-workflow", InstanceID: "local-1", Title: "X",
			Status: "pending", Lane: "pending_my_approval",
			ApprovalEngine: maclawAppApprovalEngineHub, HubInstanceID: "hub-x",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := app.ReconcileMaclawAppApprovalProjections()
	if err != nil {
		t.Fatal(err)
	}
	if summary["updated"].(int) != 0 {
		t.Fatalf("should not update when hub down: %v", summary)
	}
	reg, _ := app.readMaclawAppApprovalRegistry()
	if normalizeMaclawAppApprovalStatus(reg.Instances[0].Status) != "pending" {
		t.Fatalf("status mutated offline: %#v", reg.Instances[0])
	}
}

func TestReconcileMergesEscalationApproversFromHub(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/directory/pending-action":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{
					map[string]any{
						"instance_id":          "hub-esc-1",
						"workflow_name":        "Leave",
						"status":               "running",
						"current_node":         "n1",
						"escalation_pending":   true,
						"escalation_approvers": []any{"ve-a", "ve-b"},
					},
				},
				"total": 1, "page": 1, "page_size": 50,
			})
		case "/api/v1/directory/initiated", "/api/v1/directory/completed":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1",
		Instances: []maclawAppApprovalInstance{{
			AppID: "hub-workflow", InstanceID: "local-esc", Title: "Leave",
			Status: "pending", Lane: "my_requests",
			ApprovalEngine: maclawAppApprovalEngineHub,
			HubInstanceID:  "hub-esc-1", HubNodeID: "n1",
			ResultPayload: map[string]any{"source": "local"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := app.ReconcileMaclawAppApprovalProjections()
	if err != nil {
		t.Fatal(err)
	}
	if summary["updated"].(int) < 1 {
		t.Fatalf("expected escalation merge update: %v", summary)
	}
	reg, err := app.readMaclawAppApprovalRegistry()
	if err != nil {
		t.Fatal(err)
	}
	got := reg.Instances[0]
	if normalizeMaclawAppApprovalStatus(got.Status) != "attention" {
		t.Fatalf("status=%s want attention", got.Status)
	}
	approvers := maclawAppStringSliceFromAny(got.ResultPayload["escalation_approvers"])
	if len(approvers) != 2 {
		t.Fatalf("approvers=%v payload=%#v", approvers, got.ResultPayload)
	}
}

func TestReconcileUpsertsInitiatorEscalationFromDirectory(t *testing.T) {
	// Initiator-only view: item appears on /initiated with escalation markers, not pending-action.
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/directory/pending-action", "/api/v1/directory/completed":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 50})
		case "/api/v1/directory/initiated":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{
					map[string]any{
						"instance_id":          "hub-init-esc",
						"workflow_name":        "Expense",
						"status":               "running",
						"current_node":         "mgr",
						"user_role":            "initiator",
						"escalation_pending":   true,
						"escalation_approvers": []any{"ve-offline"},
					},
				},
				"total": 1, "page": 1, "page_size": 50,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1", Instances: nil,
	}); err != nil {
		t.Fatal(err)
	}
	summary, err := app.ReconcileMaclawAppApprovalProjections()
	if err != nil {
		t.Fatal(err)
	}
	if summary["upserted"].(int) < 1 {
		t.Fatalf("expected upsert of initiator escalation item: %v", summary)
	}
	reg, err := app.readMaclawAppApprovalRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Instances) != 1 {
		t.Fatalf("instances=%#v", reg.Instances)
	}
	got := reg.Instances[0]
	if got.HubInstanceID != "hub-init-esc" {
		t.Fatalf("%#v", got)
	}
	if normalizeMaclawAppApprovalStatus(got.Status) != "attention" {
		t.Fatalf("status=%s want attention for escalation pending", got.Status)
	}
	if len(maclawAppStringSliceFromAny(got.ResultPayload["escalation_approvers"])) != 1 {
		t.Fatalf("payload=%#v", got.ResultPayload)
	}
}

func TestMaclawAppApprovalResultPayloadEscalationChanged(t *testing.T) {
	if !maclawAppApprovalResultPayloadEscalationChanged(
		map[string]any{},
		map[string]any{"escalation_pending": true, "escalation_approvers": []string{"a"}},
	) {
		t.Fatal("expected change when hub introduces escalation")
	}
	if maclawAppApprovalResultPayloadEscalationChanged(
		map[string]any{"escalation_pending": true, "escalation_approvers": []string{"a"}},
		map[string]any{"escalation_pending": true, "escalation_approvers": []string{"a"}},
	) {
		t.Fatal("expected no change for identical markers")
	}
}
