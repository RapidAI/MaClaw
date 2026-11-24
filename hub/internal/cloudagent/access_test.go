package cloudagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

type fakeDirectory struct {
	email  map[string]string
	group  map[string]string
	parent map[string]string
	calls  int
}

func (f *fakeDirectory) Email(_ context.Context, userID string) (string, error) {
	f.calls++
	return f.email[userID], nil
}

func (f *fakeDirectory) GroupID(_ context.Context, email string) (string, error) {
	return f.group[email], nil
}

func (f *fakeDirectory) ParentID(_ context.Context, groupID string) (string, error) {
	return f.parent[groupID], nil
}

func TestMigrateLegacyLimitsKeepUserInherit(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`CREATE TABLE cloud_agent_limits (
		tenant_id TEXT NOT NULL,
		user_id TEXT NOT NULL DEFAULT '',
		max_instances INTEGER,
		max_concurrent_turns INTEGER,
		enabled INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, user_id)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO cloud_agent_limits (tenant_id, user_id, max_instances, max_concurrent_turns, enabled, updated_at) VALUES
		('tenant-a', '', 4, 1, 0, 't'),
		('tenant-a', 'user-1', 2, NULL, 1, 't')`); err != nil {
		t.Fatal(err)
	}
	svc := New(db, memSettings{})
	if svc == nil {
		t.Fatal("schema migration failed")
	}
	instances, turns, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil || enabled || instances != 2 || turns != 1 {
		t.Fatalf("migrated user = %d %d enabled=%v err=%v", instances, turns, enabled, err)
	}
	if on, err := svc.FeatureEnabled(context.Background(), "tenant-a"); err != nil || on {
		t.Fatalf("migrated global = %v %v", on, err)
	}
	cols, err := svc.limitColumns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cols["user_id"]; ok {
		t.Fatal("legacy user_id column remains")
	}
	if _, ok := cols["scope"]; !ok {
		t.Fatal("scope column missing")
	}
	again, turnsAgain, enabledAgain, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil || enabledAgain || again != 2 || turnsAgain != 1 {
		t.Fatalf("second read = %d %d %v %v", again, turnsAgain, enabledAgain, err)
	}
}

func TestScopeCascade(t *testing.T) {
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url":"http://maclawsrv.example","admin_secret":"sek"}`,
	}})
	dir := &fakeDirectory{
		email:  map[string]string{"user-1": "a@example.com", "user-2": "b@example.com"},
		group:  map[string]string{"a@example.com": "leaf", "b@example.com": "leaf"},
		parent: map[string]string{"leaf": "root", "root": "leaf"},
	}
	svc.Directory = dir
	off := false
	three, one := 3, 1
	if _, err := svc.SaveAdminSettings(context.Background(), "tenant-a", AdminSettings{
		CloudAgentsEnabled: &off, MaxInstances: &three, MaxConcurrentTurns: &one,
	}); err != nil {
		t.Fatal(err)
	}
	dir.calls = 0
	if on, err := svc.FeatureEnabled(context.Background(), "tenant-a"); err != nil || on || dir.calls != 0 {
		t.Fatalf("global read walked the directory: on=%v calls=%d err=%v", on, dir.calls, err)
	}
	two, four := 2, 4
	if _, deleted, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeDepartment, TargetID: "leaf", Enabled: AccessOn, MaxInstances: &two, MaxConcurrentTurns: &four,
	}); err != nil || deleted {
		t.Fatalf("department save deleted=%v err=%v", deleted, err)
	}
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeDepartment, TargetID: "root", Enabled: AccessOff,
	}); err != nil {
		t.Fatal(err)
	}
	instances, turns, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil || !enabled || instances != 2 || turns != 4 {
		t.Fatalf("nearest department = %d %d enabled=%v err=%v", instances, turns, enabled, err)
	}
	if _, _, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-3"); err != nil || enabled {
		t.Fatalf("user outside the tree = enabled=%v err=%v", enabled, err)
	}
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-2", Enabled: AccessOff,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-2"); err != nil || enabled {
		t.Fatalf("user off should beat the department: enabled=%v err=%v", enabled, err)
	}
	zero := 0
	if err := svc.SaveUserLimits(context.Background(), "tenant-a", "user-1", UserLimits{MaxInstances: &zero}); err != nil {
		t.Fatal(err)
	}
	instances, _, enabled, err = svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil || !enabled || instances != 0 {
		t.Fatalf("numeric override kept the switch: %d enabled=%v err=%v", instances, enabled, err)
	}
	if _, deleted, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-2", Enabled: AccessInherit,
	}); err != nil || !deleted {
		t.Fatalf("blank inherit row deleted=%v err=%v", deleted, err)
	}
	if _, _, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-2"); err != nil || !enabled {
		t.Fatalf("deleted user row should inherit the department: enabled=%v err=%v", enabled, err)
	}
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeGlobal, Enabled: AccessInherit, MaxInstances: &three, MaxConcurrentTurns: &one,
	}); err == nil || !strings.Contains(err.Error(), "不能继承") {
		t.Fatalf("global inherit: %v", err)
	}
	items, err := svc.ListAccess(context.Background(), "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 || items[0].Scope != ScopeGlobal || items[0].Enabled != AccessOff || items[1].TargetID != "leaf" || items[1].Enabled != AccessOn {
		t.Fatalf("access list = %+v", items)
	}

	if err := svc.SaveUserLimits(context.Background(), "tenant-a", "user-1", UserLimits{MaxInstances: &two}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.Create(context.Background(), "tenant-a", "user-1", CreateInput{Name: "会话", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Dispatcher().DispatchCloud(context.Background(), remotecontrol.Grant{
		ID: "grant-1", TenantID: "tenant-a", TargetUserID: "user-1", TargetAgentID: inst.ID,
	}, remotecontrol.OpCloudListTasks, nil)
	var status *remotecontrol.StatusError
	if err == nil || strings.Contains(err.Error(), DisabledMessage) || !errors.As(err, &status) || status.Code != "OFFLINE" {
		t.Fatalf("opened user dispatch = %v", err)
	}
	grantRec := call(userMux(svc, nil), http.MethodPost, "/api/v1/cloud-agents/"+inst.ID+"/control-grant", map[string]any{})
	if grantRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("opened user grant: %d %s", grantRec.Code, grantRec.Body.String())
	}
	closed := UserHandler{Svc: svc, Viewer: viewer("tenant-a", "user-3")}
	closedMux := http.NewServeMux()
	closedMux.HandleFunc("POST /api/v1/cloud-agents/{id}/control-grant", closed.ControlGrant)
	denied := call(closedMux, http.MethodPost, "/api/v1/cloud-agents/"+inst.ID+"/control-grant", map[string]any{})
	if denied.Code != http.StatusForbidden || !strings.Contains(denied.Body.String(), DisabledMessage) {
		t.Fatalf("closed user grant: %d %s", denied.Code, denied.Body.String())
	}

	other, err := svc.Create(context.Background(), "tenant-a", "user-2", CreateInput{Name: "乙", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-2", other.ID, "task-2", "req-2"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-2", Enabled: AccessOff,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.InterruptStale(context.Background(), svc.now()); err != nil {
		t.Fatal(err)
	}
	var user1, user2 string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE owner_user_id = 'user-1'`).Scan(&user1); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE owner_user_id = 'user-2'`).Scan(&user2); err != nil {
		t.Fatal(err)
	}
	if user1 != TurnRunning || user2 != TurnInterrupted {
		t.Fatalf("stale interrupt user-1=%s user-2=%s", user1, user2)
	}
}

func TestHeartbeatReportsOwnerScope(t *testing.T) {
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url":"http://127.0.0.1:9","admin_secret":"sec-1"}`,
	}})
	enable(t, svc, "tenant-a", 3, 1)
	inst, err := svc.Create(context.Background(), "tenant-a", "user-1", CreateInput{Name: "会话", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveAccess(context.Background(), "tenant-a", AccessView{
		Scope: ScopeUser, TargetID: "user-1", Enabled: AccessOff,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/heartbeat", strings.NewReader(`{"instance_id":"`+inst.ID+`","task_id":"task-1","request_id":"req-1"}`))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec := httptest.NewRecorder()
	svc.ServeHeartbeat(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("owner off heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	var status string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE owner_user_id = 'user-1'`).Scan(&status); err != nil || status != TurnInterrupted {
		t.Fatalf("turn after owner off = %s %v", status, err)
	}
}
