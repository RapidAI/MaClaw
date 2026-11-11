package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
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

func TestFederatedAcceptConcurrentCallsNotifyOnce(t *testing.T) {
	controller, _, _, notes := testService(t)
	controller.db.SetMaxOpenConns(1)
	target, _, _, _ := testService(t)
	grant := importTestFederatedInvite(t, controller, target)
	ctx := context.Background()
	const calls = 12
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := controller.AcceptFederated(ctx, grant.ID, testFederationSecret, "mac-c", "对方电脑")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(notes.items) != 1 {
		t.Fatalf("concurrent accept notices = %d", len(notes.items))
	}
	stored, err := controller.FederationGrant(ctx, grant.ID, testFederationSecret)
	if err != nil || stored.Status != StatusActive {
		t.Fatalf("grant = %+v err=%v", stored, err)
	}
}

func TestFederatedAcceptCannotReviveConcurrentRevoke(t *testing.T) {
	controller, _, _, _ := testService(t)
	controller.db.SetMaxOpenConns(1)
	target, _, _, _ := testService(t)
	grant := importTestFederatedInvite(t, controller, target)
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _ = controller.AcceptFederated(ctx, grant.ID, testFederationSecret, "mac-c", "对方电脑")
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = controller.RevokeFederated(ctx, grant.ID, testFederationSecret, false)
	}()
	close(start)
	wg.Wait()
	stored, err := controller.FederationGrant(ctx, grant.ID, testFederationSecret)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != StatusRevoked {
		t.Fatalf("accepted an already revoked grant: %+v", stored)
	}
}

func TestFederatedImportRetriesFailedNotification(t *testing.T) {
	controller, _, _, _ := testService(t)
	target, _, _, notes := testService(t)
	notes.fail = errors.New("notification unavailable")
	grant := importTestFederatedInvite(t, controller, target)
	if len(notes.items) != 0 {
		t.Fatal("failed notification was recorded as sent")
	}
	notes.fail = nil
	if err := target.ImportFederatedInvite(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	if len(notes.items) != 1 || notes.items[0].Source != NoticeInvite {
		t.Fatalf("retry notices = %+v", notes.items)
	}
	stored, err := target.FederationGrant(context.Background(), grant.ID, testFederationSecret)
	if err != nil || stored.InviteNotificationID == "" {
		t.Fatalf("notification was not attached: %+v err=%v", stored, err)
	}
}

func TestFederatedAcceptDisplacesTheEarlierController(t *testing.T) {
	controller, _, _, controllerNotes := testService(t)
	other, otherMachines, _, _ := testService(t)
	target, _, _, _ := testService(t)
	ctx := context.Background()
	otherMachines.owned["user-c/mac-d"] = Machine{ID: "mac-d", Name: "远端二"}

	// The production relay posts to the displaced grant's ControllerHub; here
	// the hook calls that hub's revoke path directly.
	hubs := map[string]*Service{}
	target.RelayFederatedRevoke = func(ctx context.Context, grant Grant) {
		if hub, ok := hubs[grant.ID]; ok {
			_ = hub.RevokeFederated(ctx, grant.ID, grant.FederationSecret, false)
		}
	}

	first, err := controller.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermView, "https://hub-a.example.com", "https://hub-t.example.com", testFederationSecret)
	if err != nil {
		t.Fatal(err)
	}
	first.TargetUserID = "user-b"
	if err := target.ImportFederatedInvite(ctx, first); err != nil {
		t.Fatal(err)
	}
	hubs[first.ID] = controller
	if _, err := target.Accept(ctx, "tenant-b", "user-b", "mac-c", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.AcceptFederated(ctx, first.ID, testFederationSecret, "mac-c", "对方电脑"); err != nil {
		t.Fatal(err)
	}

	second, err := other.InviteFederated(ctx, "tenant-c", "user-c", "mac-d", "bob@example.com", PermOperate, "https://hub-c.example.com", "https://hub-t.example.com", testFederationSecret)
	if err != nil {
		t.Fatal(err)
	}
	second.TargetUserID = "user-b"
	if err := target.ImportFederatedInvite(ctx, second); err != nil {
		t.Fatal(err)
	}
	hubs[second.ID] = other
	if _, err := target.Accept(ctx, "tenant-b", "user-b", "mac-c", second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := other.AcceptFederated(ctx, second.ID, testFederationSecret, "mac-c", "对方电脑"); err != nil {
		t.Fatal(err)
	}

	stored, err := target.FederationGrant(ctx, first.ID, testFederationSecret)
	if err != nil || stored.Status != StatusRevoked {
		t.Fatalf("displaced target-side grant = %+v err=%v", stored, err)
	}
	remote, err := controller.FederationGrant(ctx, first.ID, testFederationSecret)
	if err != nil || remote.Status != StatusRevoked {
		t.Fatalf("displaced controller-side grant = %+v err=%v", remote, err)
	}
	state, err := target.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(state.Controlled) != 1 || state.Controlled[0].ID != second.ID {
		t.Fatalf("controlled = %+v err=%v", state.Controlled, err)
	}
	ended := false
	for _, item := range controllerNotes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已结束" {
			ended = true
		}
	}
	if !ended {
		t.Fatalf("displaced controller was not told: %+v", controllerNotes.items)
	}
}
