package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

const testFederationSecret = "cd_0123456789abcdef0123456789abcdef"

func importTestFederatedInvite(t *testing.T, controller, target *Service) Grant {
	t.Helper()
	ctx := context.Background()
	grant, err := controller.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermView, "https://hub-a.example.com", "https://hub-b.example.com", testFederationSecret)
	if err != nil {
		t.Fatal(err)
	}
	grant.TargetUserID = "user-b"
	if err := target.ImportFederatedInvite(ctx, grant); err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestFederatedInviteAcceptAndRevoke(t *testing.T) {
	controller, _, _, controllerNotes := testService(t)
	target, _, _, targetNotes := testService(t)
	ctx := context.Background()
	grant := importTestFederatedInvite(t, controller, target)
	if len(targetNotes.items) != 1 || targetNotes.items[0].Source != NoticeInvite {
		t.Fatalf("target notices = %+v", targetNotes.items)
	}
	invites, err := target.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(invites.Invites) != 1 || invites.Invites[0].ID != grant.ID {
		t.Fatalf("invites = %+v err=%v", invites.Invites, err)
	}
	accepted, err := target.Accept(ctx, "tenant-b", "user-b", "mac-c", grant.ID)
	if err != nil || accepted.Status != StatusActive || accepted.TargetMachineName != "对方电脑" {
		t.Fatalf("accepted = %+v err=%v", accepted, err)
	}
	if _, err := controller.AcceptFederated(ctx, grant.ID, testFederationSecret, accepted.TargetMachineID, accepted.TargetMachineName); err != nil {
		t.Fatal(err)
	}
	tabs, err := controller.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(tabs.Grants) != 1 || tabs.Grants[0].MachineName != "对方电脑" {
		t.Fatalf("controller tabs = %+v err=%v", tabs.Grants, err)
	}
	controlled, err := target.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(controlled.Controlled) != 1 {
		t.Fatalf("controlled = %+v err=%v", controlled.Controlled, err)
	}
	acceptedNotice := false
	for _, notice := range controllerNotes.items {
		if notice.Source == NoticeResult && notice.Title == "远控已接受" && notice.AudienceUserID == "user-a" {
			acceptedNotice = true
		}
	}
	if !acceptedNotice {
		t.Fatalf("controller notices = %+v", controllerNotes.items)
	}
	noticeCount := len(controllerNotes.items)
	if _, err := controller.AcceptFederated(ctx, grant.ID, testFederationSecret, accepted.TargetMachineID, accepted.TargetMachineName); err != nil {
		t.Fatal(err)
	}
	if len(controllerNotes.items) != noticeCount {
		t.Fatal("repeated accept sent another notification")
	}
	if _, err := controller.FederationGrant(ctx, grant.ID, "cd_ffffffffffffffffffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong secret err = %v", err)
	}
	if _, err := target.Exec(ctx, grant.TenantID, grant.ControllerUserID, grant.ControllerMachineID, grant.ID, OpSendMessage, json.RawMessage(`{}`)); err == nil {
		t.Fatal("view-only federated grant must not send messages")
	}
	if err := target.Revoke(ctx, "tenant-b", "user-b", "mac-c", grant.ID); err != nil {
		t.Fatal(err)
	}
	if err := controller.RevokeFederated(ctx, grant.ID, testFederationSecret, false); err != nil {
		t.Fatal(err)
	}
	tabs, err = controller.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(tabs.Grants) != 0 {
		t.Fatalf("ended tabs = %+v err=%v", tabs.Grants, err)
	}
	endedNotice := false
	for _, notice := range controllerNotes.items {
		if notice.Source == NoticeResult && notice.Title == "远控已结束" && notice.AudienceUserID == "user-a" {
			endedNotice = true
		}
	}
	if !endedNotice {
		t.Fatalf("controller end notices = %+v", controllerNotes.items)
	}
	if err := controller.RevokeFederated(ctx, grant.ID, testFederationSecret, false); err != nil {
		t.Fatal(err)
	}
	if len(controllerNotes.items) != noticeCount+1 {
		t.Fatal("repeated revoke sent another notification")
	}
}

func TestFederatedInviteDeclineAndIgnore(t *testing.T) {
	controller, _, _, controllerNotes := testService(t)
	target, _, _, _ := testService(t)
	ctx := context.Background()
	declined := importTestFederatedInvite(t, controller, target)
	if err := target.Reject(ctx, "tenant-b", "user-b", declined.ID); err != nil {
		t.Fatal(err)
	}
	if err := controller.RejectFederated(ctx, declined.ID, testFederationSecret); err != nil {
		t.Fatal(err)
	}
	state, err := controller.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(state.Pending) != 0 {
		t.Fatalf("declined pending = %+v err=%v", state.Pending, err)
	}
	declinedNotice := false
	for _, notice := range controllerNotes.items {
		if notice.Source == NoticeResult && notice.Title == "远控已拒绝" && notice.AudienceUserID == "user-a" {
			declinedNotice = true
		}
	}
	if !declinedNotice {
		t.Fatalf("controller decline notices = %+v", controllerNotes.items)
	}
	if err := controller.RejectFederated(ctx, declined.ID, testFederationSecret); err != nil {
		t.Fatal(err)
	}
	if len(controllerNotes.items) != 1 {
		t.Fatal("repeated decline sent another notification")
	}
	ignored := importTestFederatedInvite(t, controller, target)
	if err := target.Ignore(ctx, "tenant-b", "user-b", ignored.ID); err != nil {
		t.Fatal(err)
	}
	state, err = target.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(state.Invites) != 0 {
		t.Fatalf("ignored invites = %+v err=%v", state.Invites, err)
	}
	state, err = controller.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(state.Pending) != 1 || state.Pending[0].ID != ignored.ID {
		t.Fatalf("controller should keep waiting after ignore: %+v err=%v", state.Pending, err)
	}
}

func TestFederatedInviteExpiryCannotBeAccepted(t *testing.T) {
	controller, _, _, _ := testService(t)
	target, _, _, _ := testService(t)
	grant := importTestFederatedInvite(t, controller, target)
	ctx := context.Background()
	stored, err := controller.FederationGrant(ctx, grant.ID, testFederationSecret)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = StatusPending
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if _, err := controller.db.ExecContext(ctx, `UPDATE remote_control_grants SET expires_at = ? WHERE id = ?`, formatTime(stored.ExpiresAt), stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcceptFederated(ctx, grant.ID, testFederationSecret, "mac-c", "对方电脑"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired accept err = %v", err)
	}
}
