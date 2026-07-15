package main

import (
	"testing"
	"time"
)

func TestApplyHubWorkflowStatusAttentionUpdatesLocal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.writeMaclawAppApprovalRegistry(maclawAppApprovalRegistry{
		Schema: "maclaw.app.approvals.v1",
		Instances: []maclawAppApprovalInstance{{
			AppID: "hub-workflow", InstanceID: "local-1", Title: "Leave",
			Status: "pending", Lane: "pending_my_approval",
			ApprovalEngine: maclawAppApprovalEngineHub,
			HubInstanceID:  "hub-inst-block", HubNodeID: "mgr",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.applyHubWorkflowStatusAttention("hub-inst-block", "mgr", "Leave", "timeout: no fallback", "overdue", nil); err != nil {
		t.Fatal(err)
	}
	reg, err := app.readMaclawAppApprovalRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Instances) != 1 {
		t.Fatalf("len=%d", len(reg.Instances))
	}
	got := reg.Instances[0]
	if normalizeMaclawAppApprovalStatus(got.Status) != "attention" || normalizeMaclawAppApprovalLane(got.Lane) != "attention" {
		t.Fatalf("status/lane = %s/%s", got.Status, got.Lane)
	}
	if got.HubSyncError == "" {
		t.Fatal("expected hub_sync_error")
	}
	if urgency, _ := got.ResultPayload["urgency"].(string); urgency != "overdue" {
		t.Fatalf("expected urgency=overdue, payload=%#v", got.ResultPayload)
	}
}

func TestApplyHubWorkflowStatusAttentionCreatesWhenMissing(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := app.applyHubWorkflowStatusAttention("hub-new", "n1", "Expense", "blocked", "", map[string]any{
		"escalation_pending":   true,
		"escalation_approvers": []string{"ve-a", "ve-c"},
	}); err != nil {
		t.Fatal(err)
	}
	reg, err := app.readMaclawAppApprovalRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Instances) != 1 || reg.Instances[0].HubInstanceID != "hub-new" {
		t.Fatalf("%#v", reg.Instances)
	}
	approvers := maclawAppStringSliceFromAny(reg.Instances[0].ResultPayload["escalation_approvers"])
	if len(approvers) != 2 {
		t.Fatalf("escalation_approvers=%v", approvers)
	}
}
