package a2a

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGroupEnvelopeCurrentHubValidation(t *testing.T) {
	now := time.Date(2026, 5, 3, 1, 2, 3, 0, time.UTC)
	env := NewGroupEnvelope("env-1", GroupMessageConsultationRequest, "maclaw-a", now)
	env.Request = &GroupConsultationRequest{ID: "req-1", FromID: "maclaw-a", Question: "How should we split this refactor?"}
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("ValidateCurrentHub returned error: %v", err)
	}
	if env.Scope != GroupScopeCurrentHub {
		t.Fatalf("scope = %q, want %q", env.Scope, GroupScopeCurrentHub)
	}
}

func TestGroupEnvelopeRejectsCrossHubScope(t *testing.T) {
	env := NewGroupEnvelope("env-1", GroupMessageProfile, "maclaw-a", time.Now())
	env.Scope = "cross-hub"
	env.Profile = &GroupProfile{AgentID: "maclaw-a", Discoverable: true, Available: true}
	err := env.ValidateCurrentHub()
	if err == nil || !strings.Contains(err.Error(), GroupScopeCurrentHub) {
		t.Fatalf("expected current-hub scope error, got %v", err)
	}
}

func TestGroupEnvelopeDiscussionMessageAllowsStreamEndAndAttachmentOnlyPayloads(t *testing.T) {
	env := NewGroupEnvelope("env-stream", GroupMessageDiscussionMessage, "maclaw-a", time.Now())
	env.Message = &GroupDiscussionMessage{Kind: MessageStreamEnd}
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("stream_end should validate without content: %v", err)
	}
	env.Message = &GroupDiscussionMessage{Kind: MessageStatement, FileAttachments: []FileAttachment{{FileURL: "https://hub.local/file", Filename: "report.pdf"}}}
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("attachment-only message should validate: %v", err)
	}
	env.Message = &GroupDiscussionMessage{Kind: MessageStatement}
	if err := env.ValidateCurrentHub(); err == nil {
		t.Fatalf("empty non-stream message should be rejected")
	}
}

func TestGroupProfileDiscoveryViewHidesModelWhenConfigured(t *testing.T) {
	profile := GroupProfile{
		AgentID:         " maclaw-a ",
		DisplayName:     " Builder ",
		Description:     " Code review and refactoring ",
		ModelClass:      " frontier ",
		SecurityGroupID: " team-a ",
		Discoverable:    true,
		Available:       true,
	}
	view := profile.DiscoveryView("hidden")
	if view.ModelClass != "" {
		t.Fatalf("ModelClass = %q, want hidden", view.ModelClass)
	}
	if view.AgentID != "maclaw-a" || view.DisplayName != "Builder" || view.SecurityGroupID != "team-a" {
		t.Fatalf("profile was not trimmed: %+v", view)
	}
}

func TestShouldAutoAcceptGroupInvitation(t *testing.T) {
	inv := GroupInvitation{ToID: "maclaw-b", Role: GroupRoleSpeak, Trusted: false, SecurityGroupID: "team-a"}
	if !ShouldAutoAcceptGroupInvitation("ask_always", true, "team-a", inv) {
		t.Fatal("same security group should auto-accept when explicitly allowed")
	}
	if ShouldAutoAcceptGroupInvitation("ask_always", false, "team-a", inv) {
		t.Fatal("ask_always should not auto-accept without same-security-group allowance")
	}
	if !ShouldAutoAcceptGroupInvitation("trusted_auto", false, "team-b", GroupInvitation{Trusted: true, Role: GroupRoleSpeak}) {
		t.Fatal("trusted_auto should auto-accept trusted invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("auto_trusted", false, "team-b", GroupInvitation{Trusted: true, Role: GroupRoleSpeak}) {
		t.Fatal("auto_trusted should auto-accept trusted invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("observe_auto", false, "team-b", GroupInvitation{Role: GroupRoleObserve}) {
		t.Fatal("observe_auto should auto-accept observe-only invitations")
	}
	if !ShouldAutoAcceptGroupInvitation("observe_only_auto", false, "team-b", GroupInvitation{Role: GroupRoleObserve}) {
		t.Fatal("observe_only_auto should auto-accept observe-only invitations")
	}
	if ShouldAutoAcceptGroupInvitation("reject_all", true, "team-a", inv) {
		t.Fatal("reject_all should override same-security-group auto-accept")
	}
}

func TestGroupProfileDiscoveryViewClampsContributionScore(t *testing.T) {
	profile := GroupProfile{AgentID: "maclaw-a", ContributionScore: 1.5, ContributionEvidence: -2}
	view := profile.DiscoveryView("")
	if view.ContributionScore != 1 || view.ContributionEvidence != 0 {
		t.Fatalf("contribution fields = score %.2f evidence %d, want clamped", view.ContributionScore, view.ContributionEvidence)
	}
}

func TestGroupEnvelopeApprovalRequestValidation(t *testing.T) {
	payload := json.RawMessage(`{"id":"req-1","instance_id":"inst-1","node_id":"node-1","workflow_name":"采购审批","title":"采购申请","summary":"购买办公设备"}`)

	env := NewGroupEnvelope("env-approval-1", GroupMessageApprovalRequest, "hub-engine", time.Now())
	env.Payload = payload
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("approval_request with payload should validate: %v", err)
	}

	// Without payload should fail.
	env2 := NewGroupEnvelope("env-approval-2", GroupMessageApprovalRequest, "hub-engine", time.Now())
	if err := env2.ValidateCurrentHub(); err == nil {
		t.Fatal("approval_request without payload should be rejected")
	} else if !strings.Contains(err.Error(), "approval request payload is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGroupEnvelopeApprovalResponseValidation(t *testing.T) {
	payload := json.RawMessage(`{"request_id":"req-1","decision":"approve","approver_id":"ve-001","decided_at":"2026-05-01T10:00:00Z"}`)

	env := NewGroupEnvelope("env-resp-1", GroupMessageApprovalResponse, "ve-001", time.Now())
	env.Payload = payload
	if err := env.ValidateCurrentHub(); err != nil {
		t.Fatalf("approval_response with payload should validate: %v", err)
	}

	// Without payload should fail.
	env2 := NewGroupEnvelope("env-resp-2", GroupMessageApprovalResponse, "ve-001", time.Now())
	if err := env2.ValidateCurrentHub(); err == nil {
		t.Fatal("approval_response without payload should be rejected")
	} else if !strings.Contains(err.Error(), "approval response payload is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnvelopeTypeApprovalConstants(t *testing.T) {
	// Verify the string alias constants match the GroupMessageType constants.
	if EnvelopeTypeApprovalRequest != string(GroupMessageApprovalRequest) {
		t.Fatalf("EnvelopeTypeApprovalRequest = %q, want %q", EnvelopeTypeApprovalRequest, GroupMessageApprovalRequest)
	}
	if EnvelopeTypeApprovalResponse != string(GroupMessageApprovalResponse) {
		t.Fatalf("EnvelopeTypeApprovalResponse = %q, want %q", EnvelopeTypeApprovalResponse, GroupMessageApprovalResponse)
	}
}

func TestGroupEnvelopeApprovalRequestJSONRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"id":"req-1","instance_id":"inst-1","title":"采购申请"}`)
	env := NewGroupEnvelope("env-rt-1", GroupMessageApprovalRequest, "hub-engine", time.Now())
	env.ToIDs = []string{"ve-001"}
	env.Payload = payload

	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GroupEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != GroupMessageApprovalRequest {
		t.Fatalf("type = %q, want %q", decoded.Type, GroupMessageApprovalRequest)
	}
	if len(decoded.Payload) == 0 {
		t.Fatal("payload should be preserved after round-trip")
	}
	if decoded.ToIDs[0] != "ve-001" {
		t.Fatalf("to_ids = %v, want [ve-001]", decoded.ToIDs)
	}
}
