package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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
}
