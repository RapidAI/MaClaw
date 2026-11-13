package remotecontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type memAccounts struct {
	users map[string]string
}

func (a memAccounts) Lookup(_ context.Context, _, contact string) (string, bool) {
	id, ok := a.users[contact]
	return id, ok
}

type memMachines struct {
	owned  map[string]Machine
	online map[string]bool
}

func (m memMachines) BelongsTo(_ context.Context, userID, machineID string) (Machine, bool) {
	machine, ok := m.owned[userID+"/"+machineID]
	return machine, ok
}

func (m memMachines) Online(machineID string) bool { return m.online[machineID] }

type memSender struct {
	mu   sync.Mutex
	sent []any
	fail error
}

func (s *memSender) Send(_ string, msg any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.sent = append(s.sent, msg)
	return nil
}

func (s *memSender) first() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sent) == 0 {
		return nil
	}
	return s.sent[0]
}

type memNotice struct {
	mu       sync.Mutex
	items    []Notice
	revoked  []string
	nextID   int
	onNotify func(Notice)
	fail     error
}

func (n *memNotice) Notify(ctx context.Context, notice Notice) (string, error) {
	n.mu.Lock()
	if n.fail != nil {
		err := n.fail
		n.mu.Unlock()
		return "", err
	}
	n.nextID++
	id := "n" + string(rune('0'+n.nextID))
	n.items = append(n.items, notice)
	n.mu.Unlock()
	if n.onNotify != nil {
		n.onNotify(notice)
	}
	return id, nil
}

func (n *memNotice) Revoke(_ context.Context, id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.revoked = append(n.revoked, id)
}

func (n *memNotice) sources() []string {
	out := make([]string, len(n.items))
	for i, item := range n.items {
		out[i] = item.Source + ":" + item.AudienceUserID
	}
	return out
}

func testService(t *testing.T) (*Service, memMachines, *memSender, *memNotice) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	machines := memMachines{
		owned: map[string]Machine{
			"user-a/mac-a": {ID: "mac-a", Name: "办公室"},
			"user-a/mac-b": {ID: "mac-b", Name: "家里"},
			"user-b/mac-c": {ID: "mac-c", Name: "对方电脑"},
		},
		online: map[string]bool{"mac-a": true, "mac-b": true, "mac-c": true},
	}
	sender := &memSender{}
	notes := &memNotice{}
	svc := New(db, memAccounts{users: map[string]string{"bob@example.com": "user-b"}}, machines, sender, notes)
	return svc, machines, sender, notes
}

func TestSameAccountGrantIsImmediateAndNotifiesTarget(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	grant, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if grant.Status != StatusActive || grant.TargetMachineName != "家里" || grant.Permission != PermView {
		t.Fatalf("grant = %+v", grant)
	}
	if _, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-a", PermView); err == nil {
		t.Fatal("expected self grant to fail")
	}
	state, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Grants) != 1 || state.Grants[0].MachineName != "家里" || !state.Grants[0].Online {
		t.Fatalf("tabs = %+v", state.Grants)
	}
	controlled, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(controlled.Controlled) != 1 || controlled.Controlled[0].Permission != PermView {
		t.Fatalf("controlled = %+v", controlled.Controlled)
	}
	if len(notes.items) != 1 || notes.items[0].Source != NoticeWatch || notes.items[0].AudienceUserID != "user-a" {
		t.Fatalf("notices = %+v", notes.items)
	}
}

func TestSnapshotUsesCurrentMachineNameAndLivePresence(t *testing.T) {
	svc, machines, _, _ := testService(t)
	ctx := context.Background()
	if _, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView); err != nil {
		t.Fatal(err)
	}
	machines.owned["user-a/mac-b"] = Machine{ID: "mac-b", Name: "DESKTOP-HOME"}
	machines.online["mac-b"] = false
	state, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Grants) != 1 || state.Grants[0].MachineName != "DESKTOP-HOME" || state.Grants[0].Online {
		t.Fatalf("tabs = %+v", state.Grants)
	}
}

func TestInviteIsBoundToTheControllerMachine(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	first, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Invite(ctx, "tenant", "user-a", "mac-b", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.ControllerMachineID != "mac-b" {
		t.Fatalf("second invite = %+v", second)
	}
	if len(notes.items) != 2 {
		t.Fatalf("each machine should notify once, notices=%d", len(notes.items))
	}
	home, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-b")
	if err != nil || len(home.Pending) != 1 || home.Pending[0].ID != second.ID {
		t.Fatalf("home pending = %+v err=%v", home.Pending, err)
	}
}

func TestAcceptDuringInviteNoticeKeepsTheGrantActive(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	notes.onNotify = func(notice Notice) {
		if notice.Source != NoticeInvite {
			return
		}
		if _, err := svc.Accept(ctx, "other-tenant", "user-b", "mac-c", notice.SourceID); err != nil {
			t.Errorf("accept during notify: %v", err)
		}
	}
	grant, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	state, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pending) != 0 || len(state.Grants) != 1 || state.Grants[0].ID != grant.ID || state.Grants[0].MachineName != "对方电脑" {
		t.Fatalf("tabs = %+v pending = %+v", state.Grants, state.Pending)
	}
	invitee, err := svc.Snapshot(ctx, "other-tenant", "user-b", "mac-c")
	if err != nil {
		t.Fatal(err)
	}
	if len(invitee.Invites) != 0 || len(invitee.Controlled) != 1 {
		t.Fatalf("invitee = invites:%+v controlled:%+v", invitee.Invites, invitee.Controlled)
	}
	if len(notes.revoked) == 0 {
		t.Fatal("the invite notice should be withdrawn once the grant is no longer pending")
	}
}

func TestCrossTenantInviteCanBeAcceptedAndEnded(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := svc.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(seen.Invites) != 1 || seen.Invites[0].ID != invited.ID {
		t.Fatalf("invitee snapshot = %+v err=%v", seen.Invites, err)
	}
	accepted, err := svc.Accept(ctx, "tenant-b", "user-b", "mac-c", invited.ID)
	if err != nil || accepted.Status != StatusActive || accepted.TargetMachineID != "mac-c" {
		t.Fatalf("accept = %+v err=%v", accepted, err)
	}
	controller, err := svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(controller.Grants) != 1 {
		t.Fatalf("controller tabs = %+v err=%v", controller.Grants, err)
	}
	if err := svc.Revoke(ctx, "tenant-b", "user-b", "mac-c", accepted.ID); err != nil {
		t.Fatal(err)
	}
	ended := false
	for _, item := range notes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已结束" {
			ended = true
		}
	}
	if !ended {
		t.Fatalf("controller was not told the other tenant ended control: %+v", notes.items)
	}
}

func TestInviteAcceptRejectIgnoreAndExpiry(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != invited.ID {
		t.Fatalf("duplicate invite id = %s, want %s", again.ID, invited.ID)
	}
	if len(notes.items) != 1 || notes.items[0].Source != NoticeInvite || notes.items[0].AudienceUserID != "user-b" {
		t.Fatalf("invite notices = %+v", notes.items)
	}
	waiting, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil || len(waiting.Pending) != 1 || len(waiting.Grants) != 0 {
		t.Fatalf("waiting = %+v err=%v", waiting, err)
	}
	if err := svc.Ignore(ctx, "tenant", "user-b", invited.ID); err != nil {
		t.Fatal(err)
	}
	if len(notes.items) != 1 {
		t.Fatalf("ignore must not notify the inviter, notices=%d", len(notes.items))
	}
	afterIgnore, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil || len(afterIgnore.Pending) != 1 || afterIgnore.Pending[0].Status != StatusPending {
		t.Fatalf("inviter should still be waiting: %+v", afterIgnore.Pending)
	}
	hidden, err := svc.Snapshot(ctx, "tenant", "user-b", "mac-c")
	if err != nil || len(hidden.Invites) != 0 {
		t.Fatalf("ignored invite still visible: %+v", hidden.Invites)
	}
	renewed, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil || renewed.ID == invited.ID {
		t.Fatalf("re-invite = %+v err=%v", renewed, err)
	}
	if len(notes.items) != 2 || notes.items[1].Source != NoticeInvite {
		t.Fatalf("re-invite notices = %+v", notes.items)
	}
	accepted, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", renewed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != StatusActive || accepted.TargetMachineID != "mac-c" || accepted.Permission != PermView {
		t.Fatalf("accepted = %+v", accepted)
	}
	tabs, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if err != nil || len(tabs.Grants) != 1 || tabs.Grants[0].MachineName != "对方电脑" {
		t.Fatalf("tabs = %+v err=%v", tabs.Grants, err)
	}
	foundResult := false
	for _, item := range notes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已接受" {
			foundResult = true
		}
	}
	if !foundResult {
		t.Fatalf("missing accept notice: %+v", notes.items)
	}

	rejected, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Reject(ctx, "tenant", "user-b", rejected.ID); err != nil {
		t.Fatal(err)
	}
	afterReject, _ := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if len(afterReject.Pending) != 1 {
		t.Fatalf("rejected invite should leave only the earlier ignored wait, pending=%d", len(afterReject.Pending))
	}

	stale := renewed
	stale.ID = "rc_stale"
	stale.Status = StatusPending
	stale.TargetIgnored = false
	stale.TargetMachineID = ""
	stale.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	stale.CreatedAt = time.Now().UTC().Add(-8 * 24 * time.Hour)
	if err := svc.insert(ctx, stale); err != nil {
		t.Fatal(err)
	}
	before := len(notes.items)
	if _, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-a"); err != nil {
		t.Fatal(err)
	}
	if len(notes.items) != before+1 || notes.items[len(notes.items)-1].Title != "远控邀请已过期" {
		t.Fatalf("expiry notices = %+v", notes.items[before:])
	}
}

func TestCrossTenantAcceptUpdatesTheExistingGrant(t *testing.T) {
	svc, _, _, _ := testService(t)
	ctx := context.Background()
	first, err := svc.Invite(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant-b", "user-b", "mac-c", first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Invite(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("a new invite should be created after the first one is active")
	}
	updated, err := svc.Accept(ctx, "tenant-b", "user-b", "mac-c", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.Permission != PermOperate || updated.Status != StatusActive {
		t.Fatalf("second accept = %+v", updated)
	}
	tabs, err := svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(tabs.Grants) != 1 || len(tabs.Pending) != 0 {
		t.Fatalf("tabs = %+v pending = %+v err=%v", tabs.Grants, tabs.Pending, err)
	}
	controlled, err := svc.Snapshot(ctx, "tenant-b", "user-b", "mac-c")
	if err != nil || len(controlled.Controlled) != 1 {
		t.Fatalf("controlled = %+v err=%v", controlled.Controlled, err)
	}
}

func TestTargetCanEndFromAnotherDesktop(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant-a", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.Accept(ctx, "tenant-b", "user-b", "mac-c", invited.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, "tenant-x", "user-x", "mac-d", accepted.ID); err == nil {
		t.Fatal("another account must not end the grant")
	}
	if err := svc.Revoke(ctx, "tenant-b", "user-b", "mac-d", accepted.ID); err != nil {
		t.Fatal(err)
	}
	tabs, err := svc.Snapshot(ctx, "tenant-a", "user-a", "mac-a")
	if err != nil || len(tabs.Grants) != 0 {
		t.Fatalf("tab should disappear: %+v err=%v", tabs.Grants, err)
	}
	told := false
	for _, item := range notes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已结束" {
			told = true
		}
	}
	if !told {
		t.Fatalf("controller was not told: %+v", notes.items)
	}
}

func TestExecEnforcesPermissionAndControllerMachine(t *testing.T) {
	svc, machines, sender, _ := testService(t)
	ctx := context.Background()
	grant, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{OpSendMessage, OpCreateTask, OpStopTask, OpRenameTask, OpPinTask} {
		if _, err := svc.Exec(ctx, "tenant", "user-a", "mac-a", grant.ID, op, json.RawMessage(`{}`)); err == nil {
			t.Fatalf("view permission must reject %s", op)
		}
	}
	machines.online["mac-b"] = false
	if _, err := svc.Exec(ctx, "tenant", "user-a", "mac-a", grant.ID, OpListTasks, nil); err == nil {
		t.Fatal("offline target must fail")
	}
	machines.online["mac-b"] = true
	done := make(chan struct{})
	go func() {
		defer close(done)
		body, err := svc.Exec(ctx, "tenant", "user-a", "mac-a", grant.ID, OpListTasks, nil)
		if err != nil {
			t.Errorf("list: %v", err)
			return
		}
		if string(body) != `{"tasks":[]}` {
			t.Errorf("body = %s", body)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	var msg map[string]any
	for msg == nil {
		if time.Now().After(deadline) {
			t.Fatal("exec was not sent")
		}
		time.Sleep(10 * time.Millisecond)
		if first := sender.first(); first != nil {
			msg = first.(map[string]any)
		}
	}
	svc.Complete("mac-b", msg["request_id"].(string), json.RawMessage(`{"body":{"tasks":[]}}`))
	<-done
	if _, err := svc.Exec(ctx, "tenant", "user-a", "mac-b", grant.ID, OpListTasks, nil); err == nil {
		t.Fatal("a different machine must not reuse the grant")
	}
}

func TestFetchFileOpPermissions(t *testing.T) {
	// fetch_file is read-only: a view grant may pull a produced attachment,
	// while an unknown op is still rejected outright.
	if !allowedOp(OpFetchFile) {
		t.Fatal("fetch_file must be a known op")
	}
	if !permissionAllows(PermView, OpFetchFile) || !permissionAllows(PermOperate, OpFetchFile) {
		t.Fatal("fetch_file must be allowed for view and operate grants")
	}
	if allowedOp("delete_file") {
		t.Fatal("unknown op must stay rejected")
	}
}

func TestRevokeNotifiesTheOtherSide(t *testing.T) {
	svc, _, _, notes := testService(t)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, "tenant", "user-b", "mac-c", invited.ID); err != nil {
		t.Fatal(err)
	}
	tabs, _ := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	if len(tabs.Grants) != 0 {
		t.Fatalf("tab should disappear: %+v", tabs.Grants)
	}
	var toldController bool
	for _, item := range notes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已结束" {
			toldController = true
		}
	}
	if !toldController {
		t.Fatalf("controller was not told: %+v", notes.items)
	}
	ended, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", ended.ID); err != nil {
		t.Fatal(err)
	}
	beforeEnd := len(notes.items)
	if err := svc.Revoke(ctx, "tenant", "user-a", "mac-a", ended.ID); err != nil {
		t.Fatal(err)
	}
	toldTarget := false
	for _, item := range notes.items[beforeEnd:] {
		if item.Source == NoticeResult && item.AudienceUserID == "user-b" && item.Title == "远控已结束" {
			toldTarget = true
		}
	}
	if !toldTarget {
		t.Fatalf("target was not told the controller ended control: %+v", notes.items[beforeEnd:])
	}
	if _, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermOperate); err != nil {
		t.Fatal(err)
	}
	active, _ := svc.Snapshot(ctx, "tenant", "user-a", "mac-a")
	before := len(notes.items)
	if err := svc.Revoke(ctx, "tenant", "user-a", "mac-a", active.Grants[0].ID); err != nil {
		t.Fatal(err)
	}
	for _, item := range notes.items[before:] {
		if item.Source == NoticeResult {
			t.Fatalf("same-account close should not add a result notice: %+v", item)
		}
	}
}

func TestExistingGrantStoreAddsFederationColumns(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE remote_control_grants (
		id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, controller_user_id TEXT NOT NULL, target_user_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_ = New(db, nil, nil, nil, nil)
	rows, err := db.Query(`PRAGMA table_info(remote_control_grants)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"controller_hub", "target_hub", "federation_secret"} {
		if !columns[name] {
			t.Fatalf("missing column %s: %+v", name, columns)
		}
	}
}

func TestConcurrentLocalAcceptNotifiesOnce(t *testing.T) {
	svc, _, _, notes := testService(t)
	svc.db.SetMaxOpenConns(1)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	const calls = 12
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	if len(notes.items) != 3 {
		t.Fatalf("invite, accept, and watch should each notify once: %+v", notes.items)
	}
	stored, err := svc.load(ctx, invited.ID)
	if err != nil || stored.Status != StatusActive {
		t.Fatalf("grant = %+v err=%v", stored, err)
	}
}

func TestLocalAcceptCannotReviveConcurrentRevoke(t *testing.T) {
	svc, _, _, _ := testService(t)
	svc.db.SetMaxOpenConns(1)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _ = svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID)
	}()
	go func() {
		defer wg.Done()
		<-start
		_ = svc.Revoke(ctx, "tenant", "user-a", "mac-a", invited.ID)
	}()
	close(start)
	wg.Wait()
	stored, err := svc.load(ctx, invited.ID)
	if err != nil || stored.Status != StatusRevoked {
		t.Fatalf("grant = %+v err=%v", stored, err)
	}
}

func TestLateExpiryDoesNotExpireAcceptedGrant(t *testing.T) {
	svc, _, _, _ := testService(t)
	ctx := context.Background()
	invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID); err != nil {
		t.Fatal(err)
	}
	svc.expireOne(ctx, invited)
	stored, err := svc.load(ctx, invited.ID)
	if err != nil || stored.Status != StatusActive {
		t.Fatalf("grant = %+v err=%v", stored, err)
	}
}

func TestLocalNotificationActionsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	t.Run("accept", func(t *testing.T) {
		svc, _, _, notes := testService(t)
		invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID); err != nil {
				t.Fatal(err)
			}
		}
		if len(notes.items) != 3 {
			t.Fatalf("accept notices = %+v", notes.items)
		}
	})
	t.Run("decline", func(t *testing.T) {
		svc, _, _, notes := testService(t)
		invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := svc.Reject(ctx, "tenant", "user-b", invited.ID); err != nil {
				t.Fatal(err)
			}
		}
		if len(notes.items) != 2 {
			t.Fatalf("decline notices = %+v", notes.items)
		}
	})
	t.Run("ignore", func(t *testing.T) {
		svc, _, _, notes := testService(t)
		invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := svc.Ignore(ctx, "tenant", "user-b", invited.ID); err != nil {
				t.Fatal(err)
			}
		}
		if len(notes.items) != 1 {
			t.Fatalf("ignore notices = %+v", notes.items)
		}
	})
	t.Run("end", func(t *testing.T) {
		svc, _, _, notes := testService(t)
		invited, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", invited.ID); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := svc.Revoke(ctx, "tenant-b", "user-b", "mac-c", invited.ID); err != nil {
				t.Fatal(err)
			}
		}
		if len(notes.items) != 4 {
			t.Fatalf("end notices = %+v", notes.items)
		}
	})
}

func TestDuplicateInviteRetriesFailedNotification(t *testing.T) {
	svc, _, _, notes := testService(t)
	notes.fail = errors.New("notification unavailable")
	ctx := context.Background()
	first, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.items) != 0 {
		t.Fatal("failed notification was recorded as sent")
	}
	notes.fail = nil
	second, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || len(notes.items) != 1 || notes.items[0].Source != NoticeInvite {
		t.Fatalf("retry grant = %+v notices = %+v", second, notes.items)
	}
	stored, err := svc.load(ctx, first.ID)
	if err != nil || stored.InviteNotificationID == "" {
		t.Fatalf("notification was not attached: %+v err=%v", stored, err)
	}
}

func TestStatusErrorIsDistinct(t *testing.T) {
	err := invalid("请填写对方帐号")
	var status *StatusError
	if !errors.As(err, &status) || status.Code != "INVALID" {
		t.Fatal(err)
	}
}

func TestNewControllerDisplacesThePreviousOne(t *testing.T) {
	svc, machines, _, notes := testService(t)
	ctx := context.Background()
	machines.owned["user-c/mac-d"] = Machine{ID: "mac-d", Name: "第三台"}
	machines.online["mac-d"] = true

	first, err := svc.Invite(ctx, "tenant", "user-a", "mac-a", "bob@example.com", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Invite(ctx, "tenant", "user-c", "mac-d", "bob@example.com", PermView)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Accept(ctx, "tenant", "user-b", "mac-c", second.ID); err != nil {
		t.Fatal(err)
	}

	loaded, err := svc.load(ctx, first.ID)
	if err != nil || loaded.Status != StatusRevoked {
		t.Fatalf("displaced grant = %+v err=%v", loaded, err)
	}
	state, err := svc.Snapshot(ctx, "tenant", "user-b", "mac-c")
	if err != nil || len(state.Controlled) != 1 || state.Controlled[0].ID != second.ID {
		t.Fatalf("controlled = %+v err=%v", state.Controlled, err)
	}
	if _, err := svc.Exec(ctx, "tenant", "user-a", "mac-a", first.ID, OpListTasks, nil); err == nil {
		t.Fatal("displaced controller must not keep operating the machine")
	}
	ended := false
	for _, item := range notes.items {
		if item.Source == NoticeResult && item.AudienceUserID == "user-a" && item.Title == "远控已结束" {
			ended = true
		}
	}
	if !ended {
		t.Fatalf("displaced controller was not told: %+v", notes.items)
	}
}

func TestSameAccountNewMachineDisplacesTheEarlierController(t *testing.T) {
	svc, machines, _, _ := testService(t)
	ctx := context.Background()
	machines.owned["user-a/mac-d"] = Machine{ID: "mac-d", Name: "第三台"}
	machines.online["mac-d"] = true

	first, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermView)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-d", "mac-b", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := svc.load(ctx, first.ID)
	if err != nil || loaded.Status != StatusRevoked {
		t.Fatalf("displaced grant = %+v err=%v", loaded, err)
	}
	state, err := svc.Snapshot(ctx, "tenant", "user-a", "mac-b")
	if err != nil || len(state.Controlled) != 1 || state.Controlled[0].ID != second.ID {
		t.Fatalf("controlled = %+v err=%v", state.Controlled, err)
	}
	again, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-d", "mac-b", PermView)
	if err != nil || again.ID != second.ID {
		t.Fatalf("re-add = %+v err=%v", again, err)
	}
}

func TestExecRetiresAStaleDuplicateController(t *testing.T) {
	svc, _, sender, _ := testService(t)
	ctx := context.Background()
	first, err := svc.GrantSameAccount(ctx, "tenant", "user-a", "mac-a", "mac-b", PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	// A leftover second active row — the state a pre-sweep database or a
	// missed activation path could leave on the target machine.
	later := time.Now().UTC().Add(time.Hour)
	stale := Grant{
		ID: "rc_stale_peer", TenantID: "tenant", ControllerUserID: "user-a",
		ControllerMachineID: "mac-d", ControllerMachineName: "第三台",
		TargetUserID: "user-a", TargetAccount: "user-a",
		TargetMachineID: "mac-b", TargetMachineName: "家里",
		Permission: PermOperate, Status: StatusActive, SameAccount: true,
		CreatedAt: later, DecidedAt: later, UpdatedAt: later,
	}
	if err := svc.insert(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Exec(ctx, "tenant", "user-a", "mac-a", first.ID, OpListTasks, nil); err == nil {
		t.Fatal("stale controller must lose at exec time")
	}
	loaded, err := svc.load(ctx, first.ID)
	if err != nil || loaded.Status != StatusRevoked {
		t.Fatalf("stale grant = %+v err=%v", loaded, err)
	}
	done := make(chan error, 1)
	go func() {
		body, err := svc.Exec(ctx, "tenant", "user-a", "mac-d", stale.ID, OpListTasks, nil)
		if err != nil {
			done <- err
			return
		}
		if string(body) != `{"tasks":[]}` {
			done <- errors.New("unexpected body " + string(body))
			return
		}
		done <- nil
	}()
	deadline := time.Now().Add(2 * time.Second)
	var msg map[string]any
	for msg == nil {
		if time.Now().After(deadline) {
			t.Fatal("exec was not sent")
		}
		time.Sleep(10 * time.Millisecond)
		if first := sender.first(); first != nil {
			msg = first.(map[string]any)
		}
	}
	svc.Complete("mac-b", msg["request_id"].(string), json.RawMessage(`{"body":{"tasks":[]}}`))
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
