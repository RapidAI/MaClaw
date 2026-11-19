package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

func TestAccountStreamSameHubSSESkipsSend(t *testing.T) {
	h, token, grant := newAccountStreamFixture(t)
	sends := installAccountSendHook(t)
	srv := accountStreamServer(h)
	defer srv.Close()

	payload := accountPayload(grant.ID, `D:\work\demo`, "token", 3, "正文")
	resp, reader := openAccountSSE(t, srv.URL, token, grant.ID, `D:\work\demo`)
	defer resp.Body.Close()
	h.relayControlledStream(grant.TargetMachineID, payload)
	got := readSSEData(t, reader, 3*time.Second)
	if got != string(payload) {
		t.Fatalf("sse = %s\npayload = %s", got, payload)
	}
	if sends.Load() != 0 {
		t.Fatalf("machine Send calls = %d", sends.Load())
	}
}

func TestAccountStreamFederatedReturnsWithoutSend(t *testing.T) {
	prev := companionOriginTrusted
	companionOriginTrusted = func(context.Context, string) bool { return true }
	t.Cleanup(func() { companionOriginTrusted = prev })

	controller, token, enroll, controllerDB := newAccountStreamHandler(t)
	target, _, _, targetDB := newAccountStreamHandler(t)
	srv := accountStreamServer(controller)
	defer srv.Close()
	secret := "cd_0123456789abcdef0123456789abcdef"
	grant := remotecontrol.Grant{
		ID: "rc_account_fed", TenantID: enroll.TenantID, ControllerUserID: enroll.UserID,
		ControllerMachineName: "移动设备", TargetUserID: enroll.UserID, TargetAccount: enroll.UserID,
		TargetMachineID: "mac-target", TargetMachineName: "HOME",
		ControllerHub: srv.URL, TargetHub: "https://target.example",
		FederationSecret: secret, Permission: remotecontrol.PermView, Status: remotecontrol.StatusActive,
		SameAccount: true,
	}
	insertActiveGrant(t, controllerDB, grant)
	insertActiveGrant(t, targetDB, grant)
	sends := installAccountSendHook(t)

	resp, reader := openAccountSSE(t, srv.URL, token, grant.ID, `D:\work\demo`)
	defer resp.Body.Close()
	payload := accountPayload(grant.ID, `D:\work\demo`, "token", 4, "联邦")
	started := time.Now()
	postAccountFederation(t, srv.URL, grant.ID, secret, payload)
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("federated stream handler blocked")
	}
	if got := readSSEData(t, reader, 3*time.Second); got != string(payload) {
		t.Fatalf("federation sse = %s", got)
	}

	second := accountPayload(grant.ID, `D:\work\demo`, "progress", 5, "转发")
	target.relayControlledStream(grant.TargetMachineID, second)
	if got := readSSEData(t, reader, 3*time.Second); got != string(second) {
		t.Fatalf("relayed federation sse = %s", got)
	}
	if sends.Load() != 0 {
		t.Fatalf("Send calls = %d", sends.Load())
	}
}

func TestAccountStreamDesktopGrantRejected(t *testing.T) {
	h, token, enroll, db := newAccountStreamHandler(t)
	grant := remotecontrol.Grant{
		ID: "rc_desktop_grant", TenantID: enroll.TenantID, ControllerUserID: enroll.UserID,
		ControllerMachineID: enroll.MachineID, ControllerMachineName: "office",
		TargetUserID: enroll.UserID, TargetAccount: enroll.UserID,
		TargetMachineID: "mac-target", Permission: remotecontrol.PermView,
		Status: remotecontrol.StatusActive,
	}
	insertActiveGrant(t, db, grant)
	srv := accountStreamServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/remote-control/grants/"+grant.ID+"/stream?project_path="+url.QueryEscape(`D:\work\demo`), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-Id", enroll.MachineID)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", res.StatusCode)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(res.Body)
	if !strings.Contains(buf.String(), "桌面授权不使用这条流") {
		t.Fatalf("body = %s", buf.String())
	}
}

func TestAccountStreamForgedSourceDropped(t *testing.T) {
	h, token, grant := newAccountStreamFixture(t)
	srv := accountStreamServer(h)
	defer srv.Close()
	resp, reader := openAccountSSE(t, srv.URL, token, grant.ID, `D:\work\demo`)
	defer resp.Body.Close()
	payload := accountPayload(grant.ID, `D:\work\demo`, "token", 2, "伪造")
	h.relayControlledStream("not-the-target", payload)
	if line, ok := readSSEDataOptional(reader, 400*time.Millisecond); ok {
		t.Fatalf("forged source delivered %s", line)
	}
	if cur := h.streams.cursor(grant.ID, `D:\work\demo`); cur != 0 {
		t.Fatalf("forged source cursor = %d", cur)
	}
}

func TestAccountStreamCursorsStayPerSession(t *testing.T) {
	h, _, grant := newAccountStreamFixture(t)
	h.relayControlledStream(grant.TargetMachineID, accountPayload(grant.ID, `D:\work\demo`, "token", 2, "甲"))
	h.relayControlledStream(grant.TargetMachineID, accountPayload(grant.ID, `E:\other`, "token", 9, "乙"))
	if got := h.streams.cursor(grant.ID, `D:\work\demo`); got != 2 {
		t.Fatalf("demo cursor = %d", got)
	}
	if got := h.streams.cursor(grant.ID, `E:\other`); got != 9 {
		t.Fatalf("other cursor = %d", got)
	}
}

func TestAccountStreamPathSpellingsShareKey(t *testing.T) {
	h, token, grant := newAccountStreamFixture(t)
	srv := accountStreamServer(h)
	defer srv.Close()
	resp, reader := openAccountSSE(t, srv.URL, token, grant.ID, `d:/work/demo`)
	defer resp.Body.Close()
	payload := accountPayload(grant.ID, `D:\work\demo`, "token", 4, "同一路径")
	h.relayControlledStream(grant.TargetMachineID, payload)
	if got := readSSEData(t, reader, 3*time.Second); got != string(payload) {
		t.Fatalf("shared key sse = %s", got)
	}
	if a, b := h.streams.cursor(grant.ID, `D:\work\demo`), h.streams.cursor(grant.ID, `d:/work/demo`); a != 4 || b != 4 {
		t.Fatalf("cursors = %d %d", a, b)
	}
}

func TestAccountStreamOtherTaskDoesNotGap(t *testing.T) {
	h, token, grant := newAccountStreamFixture(t)
	srv := accountStreamServer(h)
	defer srv.Close()
	resp, reader := openAccountSSE(t, srv.URL, token, grant.ID, `D:\work\demo`)
	defer resp.Body.Close()
	for i := 1; i <= 40; i++ {
		h.relayControlledStream(grant.TargetMachineID, accountPayload(grant.ID, `E:\other`, "token", uint64(i), "别的任务"))
	}
	payload := accountPayload(grant.ID, `D:\work\demo`, "token", 8, "本任务")
	h.relayControlledStream(grant.TargetMachineID, payload)
	got := readSSEData(t, reader, 3*time.Second)
	var head struct {
		Event string `json:"event"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(got), &head); err != nil {
		t.Fatal(err)
	}
	if head.Event == "snapshot" || head.Text != "本任务" {
		t.Fatalf("other task opened a gap: %s", got)
	}
	if got != string(payload) {
		t.Fatalf("sse = %s", got)
	}
}

func newAccountStreamFixture(t *testing.T) (remoteControlHandler, string, remotecontrol.Grant) {
	t.Helper()
	h, token, enroll, _ := newAccountStreamHandler(t)
	grant, err := h.svc.GrantSameAccount(context.Background(), enroll.TenantID, enroll.UserID, "", enroll.MachineID, remotecontrol.PermView)
	if err != nil {
		t.Fatal(err)
	}
	if grant.ControllerMachineID != "" {
		t.Fatalf("account grant machine = %q", grant.ControllerMachineID)
	}
	return h, token, grant
}

func newAccountStreamHandler(t *testing.T) (remoteControlHandler, string, enrollmentIdentity, *sql.DB) {
	t.Helper()
	identity, devices, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, fmt.Sprintf("acct-stream-%d@example.com", time.Now().UnixNano()))
	if enroll.TenantID == "" || enroll.UserID == "" || enroll.MachineID == "" {
		t.Fatalf("enrollment = %+v", enroll)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "rc.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	svc := remotecontrol.New(db, nil, remoteControlMachines{devices: devices}, nil, nil)
	if svc == nil {
		t.Fatal("nil remote control service")
	}
	h := remoteControlHandler{svc: svc, devices: devices, identity: identity, streams: newAccountStreamBroker()}
	return h, token, enrollmentIdentity{TenantID: enroll.TenantID, UserID: enroll.UserID, MachineID: enroll.MachineID}, db
}

type enrollmentIdentity struct {
	TenantID  string
	UserID    string
	MachineID string
}

func accountStreamServer(h remoteControlHandler) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/remote-control/grants/{id}/stream", h.accountStream)
	mux.HandleFunc("POST /api/remote-control/federation/grants/{id}/stream", h.federationStream)
	return httptest.NewServer(mux)
}

func installAccountSendHook(t *testing.T) *atomic.Int32 {
	t.Helper()
	var sends atomic.Int32
	prev := remoteControlSendHook
	remoteControlSendHook = func(machineID string, _ any) error {
		sends.Add(1)
		time.Sleep(2 * time.Second)
		return nil
	}
	t.Cleanup(func() { remoteControlSendHook = prev })
	return &sends
}

func accountPayload(grantID, projectPath, event string, cursor uint64, text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"grant_id": grantID, "event": event, "session_key": "desktop-user:" + projectPath,
		"project_path": projectPath, "cursor": cursor, "text": text,
	})
	return raw
}

func openAccountSSE(t *testing.T, base, token, grantID, projectPath string) (*http.Response, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q := url.Values{}
	q.Set("project_path", projectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/remote-control/grants/"+grantID+"/stream?"+q.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("sse status = %d body = %s", resp.StatusCode, buf.String())
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("X-Accel-Buffering = %q", resp.Header.Get("X-Accel-Buffering"))
	}
	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache-control = %q", resp.Header.Get("Cache-Control"))
	}
	return resp, bufio.NewReader(resp.Body)
}

func readSSEData(t *testing.T, reader *bufio.Reader, timeout time.Duration) string {
	t.Helper()
	line, ok := readSSEDataOptional(reader, timeout)
	if !ok {
		t.Fatal("timed out waiting for sse data")
	}
	return line
}

func readSSEDataOptional(reader *bufio.Reader, timeout time.Duration) (string, bool) {
	ch := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
				return
			}
			if strings.HasPrefix(line, "data: ") {
				ch <- strings.TrimRight(strings.TrimPrefix(line, "data: "), "\r\n")
				return
			}
		}
	}()
	select {
	case line := <-ch:
		return line, true
	case <-errCh:
		return "", false
	case <-time.After(timeout):
		return "", false
	}
}

func postAccountFederation(t *testing.T, base, grantID, secret string, payload json.RawMessage) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"grant_id":   grantID,
		"origin_hub": "https://target.example",
		"secret":     secret,
		"body":       payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, base+"/api/remote-control/federation/grants/"+grantID+"/stream", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("federation status = %d body = %s", resp.StatusCode, buf.String())
	}
}

func insertActiveGrant(t *testing.T, db *sql.DB, grant remotecontrol.Grant) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.Exec(`INSERT INTO remote_control_grants (
		id, tenant_id, controller_user_id, controller_machine_id, controller_machine_name,
		target_user_id, target_account, target_machine_id, target_machine_name,
		controller_hub, target_hub, federation_secret,
		permission, status, same_account, target_ignored, invite_notification_id, notice_notification_id,
		created_at, decided_at, expires_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, '', '', ?, ?, '', ?)`,
		grant.ID, grant.TenantID, grant.ControllerUserID, grant.ControllerMachineID, grant.ControllerMachineName,
		grant.TargetUserID, grant.TargetAccount, grant.TargetMachineID, grant.TargetMachineName,
		grant.ControllerHub, grant.TargetHub, grant.FederationSecret,
		grant.Permission, grant.Status, boolToInt(grant.SameAccount), now, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
