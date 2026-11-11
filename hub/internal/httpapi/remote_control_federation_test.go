package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"

	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type federationTestSender struct {
	messages chan map[string]any
}

func (s federationTestSender) Send(_ string, msg any) error {
	item, _ := msg.(map[string]any)
	s.messages <- item
	return nil
}

type federationTestNotifier struct {
	notices []remotecontrol.Notice
}

func (n *federationTestNotifier) Notify(_ context.Context, notice remotecontrol.Notice) (string, error) {
	n.notices = append(n.notices, notice)
	return "notice-1", nil
}

func (n *federationTestNotifier) Revoke(context.Context, string) {}

func newRemoteControlFederationHandler(t *testing.T, userID, machineID, machineName string) (remoteControlHandler, federationTestSender) {
	t.Helper()
	runtime := device.NewRuntime()
	devices := device.NewService(nil, runtime)
	conn := &ws.ConnContext{UserID: userID, Conn: &websocket.Conn{}}
	devices.BindDesktop(machineID, conn)
	if err := devices.MarkOnline(context.Background(), machineID, ws.MachineHelloPayload{Name: machineName, Nickname: machineName}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	adapter := remoteControlMachines{devices: devices}
	sender := federationTestSender{messages: make(chan map[string]any, 4)}
	svc := remotecontrol.New(db, nil, adapter, sender, nil)
	return remoteControlHandler{svc: svc, devices: devices}, sender
}

func remoteControlFederationServer(h remoteControlHandler) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/remote-control/federation/proof", h.federationProof)
	mux.HandleFunc("/api/remote-control/federation/grants/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case len(r.URL.Path) >= 9 && r.URL.Path[len(r.URL.Path)-9:] == "/decision":
			h.federationDecision(w, r)
		case len(r.URL.Path) >= 3 && r.URL.Path[len(r.URL.Path)-3:] == "/op":
			h.federationOp(w, r)
		case len(r.URL.Path) >= 7 && r.URL.Path[len(r.URL.Path)-7:] == "/status":
			h.federationStatus(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	return httptest.NewServer(mux)
}

func TestRemoteControlFederationAcceptOperateAndEnd(t *testing.T) {
	oldTrust := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = oldTrust })
	controller, _ := newRemoteControlFederationHandler(t, "user-a", "mac-a", "OFFICE-A")
	target, targetSender := newRemoteControlFederationHandler(t, "user-b", "mac-c", "HOME-C")
	controllerServer := remoteControlFederationServer(controller)
	defer controllerServer.Close()
	targetServer := remoteControlFederationServer(target)
	defer targetServer.Close()
	ctx := context.Background()
	secret := "cd_0123456789abcdef0123456789abcdef"
	grant, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", remotecontrol.PermOperate, controllerServer.URL, targetServer.URL, secret)
	if err != nil {
		t.Fatal(err)
	}
	grant.TargetUserID = "user-b"
	if err := target.svc.ImportFederatedInvite(ctx, grant); err != nil {
		t.Fatal(err)
	}
	accepted, err := target.svc.Accept(ctx, "tenant-b", "user-b", "mac-c", grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.relayTargetDecision(ctx, accepted, "accept"); err != nil {
		t.Fatal(err)
	}
	state, err := controller.svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil {
		t.Fatal(err)
	}
	controller.refreshFederatedGrants(ctx, "tenant-a", "user-a", "mac-a", &state)
	if len(state.Grants) != 1 || !state.Grants[0].Online || state.Grants[0].MachineName != "HOME-C" {
		t.Fatalf("federated tabs = %+v", state.Grants)
	}
	grant, err = controller.svc.ControllerGrant(ctx, "tenant-a", "user-a", "mac-a", grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan json.RawMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := controller.execAcrossHub(ctx, grant, remotecontrol.OpListTasks, nil)
		if err != nil {
			errCh <- err
			return
		}
		resultCh <- result
	}()
	select {
	case msg := <-targetSender.messages:
		requestID, _ := msg["request_id"].(string)
		target.svc.Complete("mac-c", requestID, json.RawMessage(`{"body":{"tasks":[]}}`))
	case <-time.After(3 * time.Second):
		t.Fatal("federated operation did not reach the target")
	}
	select {
	case result := <-resultCh:
		if string(result) != `{"tasks":[]}` {
			t.Fatalf("result = %s", result)
		}
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("federated operation did not return")
	}
	if err := target.svc.Revoke(ctx, "tenant-b", "user-b", "mac-c", grant.ID); err != nil {
		t.Fatal(err)
	}
	if err := target.relayTargetDecision(ctx, accepted, "revoke"); err != nil {
		t.Fatal(err)
	}
	state, err = controller.svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(state.Grants) != 0 {
		t.Fatalf("ended tabs = %+v err=%v", state.Grants, err)
	}
	declined, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", remotecontrol.PermView, controllerServer.URL, targetServer.URL, secret)
	if err != nil {
		t.Fatal(err)
	}
	declined.TargetUserID = "user-b"
	if err := target.svc.ImportFederatedInvite(ctx, declined); err != nil {
		t.Fatal(err)
	}
	if err := target.svc.Reject(ctx, "tenant-b", "user-b", declined.ID); err != nil {
		t.Fatal(err)
	}
	if err := target.relayTargetDecision(ctx, declined, "decline"); err != nil {
		t.Fatal(err)
	}
	state, err = controller.svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(state.Pending) != 0 {
		t.Fatalf("declined pending = %+v err=%v", state.Pending, err)
	}
}

func TestRemoteControlFederationRejectsWrongSecret(t *testing.T) {
	oldTrust := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = oldTrust })
	h, _ := newRemoteControlFederationHandler(t, "user-a", "mac-a", "OFFICE-A")
	server := remoteControlFederationServer(h)
	defer server.Close()
	grant, err := h.svc.InviteFederated(context.Background(), "tenant-a", "user-a", "mac-a", "bob@example.com", remotecontrol.PermView, server.URL, "https://hub-b.example.com", "cd_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/remote-control/federation/proof", nil)
	body := remoteControlFederationRequest{GrantID: grant.ID, OriginHub: server.URL, Secret: "cd_ffffffffffffffffffffffffffffffff"}
	raw, _ := json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/remote-control/federation/proof", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	h.federationProof(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong secret status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRemoteControlAcceptRetriesAfterRelayFailure(t *testing.T) {
	oldTrust := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = oldTrust })
	controller, _ := newRemoteControlFederationHandler(t, "user-a", "mac-a", "OFFICE-A")
	attempts := 0
	controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			writeError(w, http.StatusServiceUnavailable, "REMOTE", "temporary failure")
			return
		}
		controller.federationDecision(w, r)
	}))
	defer controllerServer.Close()

	identity, targetDevices, _ := newHTTPAPITestServices(t)
	token, enrollment := issueViewerToken(t, identity, "target-retry@example.com")
	conn := &ws.ConnContext{UserID: enrollment.UserID, Conn: &websocket.Conn{}}
	targetDevices.BindDesktop(enrollment.MachineID, conn)
	if err := targetDevices.MarkOnline(context.Background(), enrollment.MachineID, ws.MachineHelloPayload{Name: "TARGET-HOME", Nickname: "Home"}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	adapter := remoteControlMachines{devices: targetDevices}
	sender := federationTestSender{messages: make(chan map[string]any, 1)}
	target := remoteControlHandler{
		svc: remotecontrol.New(db, nil, adapter, sender, nil), devices: targetDevices, identity: identity,
	}
	ctx := context.Background()
	secret := "cd_0123456789abcdef0123456789abcdef"
	grant, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "target-retry@example.com", remotecontrol.PermView, controllerServer.URL, "https://hub-b.example.com", secret)
	if err != nil {
		t.Fatal(err)
	}
	grant.TargetUserID = enrollment.UserID
	if err := target.svc.ImportFederatedInvite(ctx, grant); err != nil {
		t.Fatal(err)
	}
	callAccept := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/remote-control/grants/"+grant.ID+"/accept", nil)
		req.SetPathValue("id", grant.ID)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Machine-Id", enrollment.MachineID)
		rec := httptest.NewRecorder()
		target.accept(rec, req)
		return rec
	}
	if rec := callAccept(); rec.Code == http.StatusOK {
		t.Fatalf("first accept unexpectedly succeeded: %s", rec.Body.String())
	}
	pending, err := target.svc.FederationGrant(ctx, grant.ID, secret)
	if err != nil || pending.Status != remotecontrol.StatusPending {
		t.Fatalf("failed relay changed local status: %+v err=%v", pending, err)
	}
	if rec := callAccept(); rec.Code != http.StatusOK {
		controllerState, controllerErr := controller.svc.FederationGrant(ctx, grant.ID, secret)
		targetState, targetErr := target.svc.FederationGrant(ctx, grant.ID, secret)
		t.Fatalf("retry accept status = %d body=%s attempts=%d controller=%+v/%v target=%+v/%v", rec.Code, rec.Body.String(), attempts, controllerState, controllerErr, targetState, targetErr)
	}
	if rec := callAccept(); rec.Code != http.StatusOK {
		t.Fatalf("repeated accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	active, err := controller.svc.FederationGrant(ctx, grant.ID, secret)
	if err != nil || active.Status != remotecontrol.StatusActive || active.TargetMachineID != enrollment.MachineID {
		t.Fatalf("controller grant = %+v err=%v", active, err)
	}
	originalID := active.ID
	secondSecret := "cd_abcdef0123456789abcdef0123456789"
	second, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "target-retry@example.com", remotecontrol.PermOperate, controllerServer.URL, "https://hub-b.example.com", secondSecret)
	if err != nil {
		t.Fatal(err)
	}
	second.TargetUserID = enrollment.UserID
	if err := target.svc.ImportFederatedInvite(ctx, second); err != nil {
		t.Fatal(err)
	}
	grant = second
	if rec := callAccept(); rec.Code != http.StatusOK {
		t.Fatalf("replacement accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	updated, err := controller.svc.FederationGrant(ctx, originalID, secret)
	if err != nil || updated.Status != remotecontrol.StatusActive || updated.Permission != remotecontrol.PermOperate {
		t.Fatalf("updated grant = %+v err=%v", updated, err)
	}
	replaced, err := controller.svc.FederationGrant(ctx, second.ID, secondSecret)
	if err != nil || replaced.Status != remotecontrol.StatusRevoked {
		t.Fatalf("replaced grant = %+v err=%v", replaced, err)
	}
	controllerState, err := controller.svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(controllerState.Grants) != 1 || controllerState.Grants[0].ID != originalID {
		t.Fatalf("controller tabs = %+v err=%v", controllerState.Grants, err)
	}
	targetState, err := target.svc.Snapshot(ctx, "tenant-b", enrollment.UserID, enrollment.MachineID)
	if err != nil || len(targetState.Controlled) != 1 || targetState.Controlled[0].ID != originalID {
		t.Fatalf("target controlled = %+v err=%v", targetState.Controlled, err)
	}
}

func TestRemoteControlFederationInviteDeliversNotificationAndRejectsMismatch(t *testing.T) {
	oldTrust := companionOriginTrusted
	oldOrigin := companionLocalOriginFn
	companionOriginTrusted = func(context.Context, string) bool { return true }
	companionLocalOriginFn = func() string { return "https://hub-b.example.com" }
	t.Cleanup(func() {
		companionOriginTrusted = oldTrust
		companionLocalOriginFn = oldOrigin
	})
	controller, _ := newRemoteControlFederationHandler(t, "user-a", "mac-a", "OFFICE-A")
	controllerServer := remoteControlFederationServer(controller)
	defer controllerServer.Close()
	identity, targetDevices, _ := newHTTPAPITestServices(t)
	_, enrollment := issueViewerToken(t, identity, "invited@example.com")
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	adapter := remoteControlMachines{devices: targetDevices}
	sender := federationTestSender{messages: make(chan map[string]any, 1)}
	notifier := &federationTestNotifier{}
	target := remoteControlHandler{
		svc: remotecontrol.New(db, nil, adapter, sender, notifier), devices: targetDevices, identity: identity,
	}
	ctx := context.Background()
	secret := "cd_0123456789abcdef0123456789abcdef"
	grant, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "invited@example.com", remotecontrol.PermView, controllerServer.URL, "https://hub-b.example.com", secret)
	if err != nil {
		t.Fatal(err)
	}
	invite := remoteControlFederationRequest{
		GrantID: grant.ID, OriginHub: grant.ControllerHub, Secret: secret, Contact: grant.TargetAccount,
		Permission: grant.Permission, ControllerUserID: grant.ControllerUserID,
		ControllerMachineID: grant.ControllerMachineID, ControllerMachineName: grant.ControllerMachineName,
		TenantID: grant.TenantID, ExpiresAt: grant.ExpiresAt,
	}
	callInvite := func(body remoteControlFederationRequest) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/remote-control/federation/invites", bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		target.federationInvite(rec, req)
		return rec
	}
	if rec := callInvite(invite); rec.Code != http.StatusOK {
		t.Fatalf("invite status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(notifier.notices) != 1 || notifier.notices[0].Source != remotecontrol.NoticeInvite || notifier.notices[0].AudienceUserID != enrollment.UserID {
		t.Fatalf("notices = %+v", notifier.notices)
	}
	imported, err := target.svc.FederationGrant(ctx, grant.ID, secret)
	if err != nil || imported.Status != remotecontrol.StatusPending || imported.TargetUserID != enrollment.UserID {
		t.Fatalf("imported = %+v err=%v", imported, err)
	}
	if rec := callInvite(invite); rec.Code != http.StatusOK {
		t.Fatalf("repeated invite status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(notifier.notices) != 1 {
		t.Fatal("repeated invite sent another notification")
	}

	secondSecret := "cd_abcdef0123456789abcdef0123456789"
	second, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "other@example.com", remotecontrol.PermView, controllerServer.URL, "https://hub-b.example.com", secondSecret)
	if err != nil {
		t.Fatal(err)
	}
	tampered := invite
	tampered.GrantID = second.ID
	tampered.Secret = secondSecret
	tampered.Contact = second.TargetAccount
	tampered.ExpiresAt = second.ExpiresAt
	tampered.Permission = remotecontrol.PermOperate
	if rec := callInvite(tampered); rec.Code != http.StatusBadRequest {
		t.Fatalf("tampered invite status = %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := target.svc.FederationGrant(ctx, second.ID, secondSecret); !errors.Is(err, remotecontrol.ErrNotFound) {
		t.Fatalf("tampered invite was imported: %v", err)
	}
	if len(notifier.notices) != 1 {
		t.Fatal("tampered invite sent a notification")
	}
}

func TestRemoteControlStaleFederatedGrantHealsOnNotFound(t *testing.T) {
	oldTrust := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = oldTrust })
	controller, _ := newRemoteControlFederationHandler(t, "user-a", "mac-a", "OFFICE-A")
	target, _ := newRemoteControlFederationHandler(t, "user-b", "mac-c", "HOME-C")
	controllerServer := remoteControlFederationServer(controller)
	defer controllerServer.Close()
	targetServer := remoteControlFederationServer(target)
	defer targetServer.Close()
	ctx := context.Background()
	secret := "cd_0123456789abcdef0123456789abcdef"
	grant, err := controller.svc.InviteFederated(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", remotecontrol.PermOperate, controllerServer.URL, targetServer.URL, secret)
	if err != nil {
		t.Fatal(err)
	}
	grant.TargetUserID = "user-b"
	if err := target.svc.ImportFederatedInvite(ctx, grant); err != nil {
		t.Fatal(err)
	}
	accepted, err := target.svc.Accept(ctx, "tenant-b", "user-b", "mac-c", grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := target.relayTargetDecision(ctx, accepted, "accept"); err != nil {
		t.Fatal(err)
	}
	// The target ends the grant without telling the controller's Hub — the
	// state a lost displacement or revoke relay leaves behind.
	if err := target.svc.Revoke(ctx, "tenant-b", "user-b", "mac-c", grant.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := controller.svc.ControllerGrant(ctx, "tenant-a", "user-a", "mac-a", grant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.execAcrossHub(ctx, stored, remotecontrol.OpListTasks, nil); err == nil {
		t.Fatal("op on a stale grant must fail")
	}
	state, err := controller.svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(state.Grants) != 0 {
		t.Fatalf("stale grant still listed: %+v err=%v", state.Grants, err)
	}
}
