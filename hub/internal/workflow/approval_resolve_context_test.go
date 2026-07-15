package workflow

import "testing"

func TestApprovalResolveContextFromInstanceData_NestedPayload(t *testing.T) {
	rc := ApprovalResolveContextFromInstanceData(map[string]interface{}{
		"business_payload": map[string]interface{}{
			"applicant_email": "bob@example.com",
			"department_id":   "dept-ops",
		},
		"form_data": map[string]interface{}{
			"applicant_name": "Bob",
		},
	})
	if rc.ApplicantID != "bob@example.com" {
		t.Fatalf("ApplicantID=%q", rc.ApplicantID)
	}
	if rc.DepartmentID != "dept-ops" {
		t.Fatalf("DepartmentID=%q", rc.DepartmentID)
	}
	if rc.ApplicantName != "Bob" {
		t.Fatalf("ApplicantName=%q", rc.ApplicantName)
	}
}

func TestWithApprovalResolveContextRoundTrip(t *testing.T) {
	ctx := WithApprovalResolveContext(nil, &ApprovalResolveContext{ApplicantID: "a@b.c"})
	got := ApprovalResolveContextFrom(ctx)
	if got == nil || got.ApplicantID != "a@b.c" {
		t.Fatalf("got %#v", got)
	}
}

func TestApplyDigitalTwoPhaseDispatchShape(t *testing.T) {
	cfg := ApprovalNodeConfig{
		ApproverIDs:   []string{"machine-bot", "machine-boss"},
		Mode:          ModeSingle,
		ExecutionMode: "digital_suggest",
	}
	got := applyDigitalTwoPhaseDispatchShape(cfg)
	if got.Mode != ModeSequential {
		t.Fatalf("mode=%q want sequential", got.Mode)
	}
	if len(got.ApproverOrder) != 2 || got.ApproverOrder[0] != "machine-bot" {
		t.Fatalf("order=%#v", got.ApproverOrder)
	}
	// Explicit countersign stays.
	cfg.Mode = ModeCountersign
	got = applyDigitalTwoPhaseDispatchShape(cfg)
	if got.Mode != ModeCountersign {
		t.Fatalf("countersign must be preserved, got %q", got.Mode)
	}
}
