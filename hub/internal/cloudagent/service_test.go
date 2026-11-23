package cloudagent

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", "\\", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type memSettings struct {
	kv map[string]string
}

func (m memSettings) Get(_ context.Context, key string) (string, error) {
	if m.kv == nil {
		return "", nil
	}
	return m.kv[key], nil
}

func viewer(tenant, user string) ViewerFunc {
	return func(*http.Request) (*auth.ViewerPrincipal, error) {
		return &auth.ViewerPrincipal{TenantID: tenant, UserID: user, Email: user + "@example.com"}, nil
	}
}

func userMux(svc *Service, rc *remotecontrol.Service) *http.ServeMux {
	h := UserHandler{Svc: svc, RC: rc, Viewer: viewer("tenant-a", "user-1")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/cloud-agents", h.List)
	mux.HandleFunc("POST /api/v1/cloud-agents", h.Create)
	mux.HandleFunc("GET /api/v1/cloud-agents/{id}", h.Get)
	mux.HandleFunc("PATCH /api/v1/cloud-agents/{id}", h.Patch)
	mux.HandleFunc("DELETE /api/v1/cloud-agents/{id}", h.Delete)
	mux.HandleFunc("POST /api/v1/cloud-agents/{id}/llm", h.SetLLM)
	mux.HandleFunc("POST /api/v1/cloud-agents/{id}/git", h.ConnectGit)
	mux.HandleFunc("DELETE /api/v1/cloud-agents/{id}/git", h.DisconnectGit)
	mux.HandleFunc("POST /api/v1/cloud-agents/{id}/stop", h.Stop)
	mux.HandleFunc("POST /api/v1/cloud-agents/{id}/resume", h.Resume)
	mux.HandleFunc("POST /api/v1/cloud-agents/{id}/control-grant", h.ControlGrant)
	return mux
}

func call(mux http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		buf = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("X-Machine-Id", "mac-1")
	req.Host = "hub.example"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func enable(t *testing.T, svc *Service, tenant string, instances, turns int) {
	t.Helper()
	on := true
	view, err := svc.SaveAdminSettings(context.Background(), tenant, AdminSettings{
		CloudAgentsEnabled: &on,
		MaxInstances:       &instances,
		MaxConcurrentTurns: &turns,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !view.CloudAgentsEnabled || view.MaxInstances != instances || view.MaxConcurrentTurns != turns {
		t.Fatalf("settings = %+v", view)
	}
}

func TestDefaultsStayOffWithoutDesktopBotLimit(t *testing.T) {
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"default_desktop_bot_limit":99,"base_url":"http://maclawsrv.example","admin_secret":"sek"}`,
	}})
	instances, turns, enabled, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if instances != 3 || turns != 1 || enabled {
		t.Fatalf("limits = %d %d enabled=%v", instances, turns, enabled)
	}
	_, err = svc.db.Exec(`INSERT INTO cloud_agent_limits (tenant_id, user_id, max_instances, max_concurrent_turns, enabled, updated_at) VALUES ('tenant-a', 'user-1', NULL, NULL, 1, 't')`)
	if err != nil {
		t.Fatal(err)
	}
	if on, err := svc.FeatureEnabled(context.Background(), "tenant-a"); err != nil || on {
		t.Fatalf("user row enabled the feature: %v %v", on, err)
	}
	rec := call(userMux(svc, nil), http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "甲", Kind: KindSession})
	if rec.Code != http.StatusForbidden || !strings.Contains(decodeMap(t, rec)["message"].(string), DisabledMessage) {
		t.Fatalf("create while off: %d %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM cloud_agents`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows = %d err=%v", n, err)
	}
}

func TestQuotaAndKindImmutability(t *testing.T) {
	db := openTestDB(t)
	svc := New(db, memSettings{})
	enable(t, svc, "tenant-a", 3, 1)
	one := 1
	if err := svc.SaveUserLimits(context.Background(), "tenant-a", "user-1", UserLimits{MaxInstances: &one}); err != nil {
		t.Fatal(err)
	}
	mux := userMux(svc, nil)
	first := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "会话", Kind: KindSession})
	if first.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", first.Code, first.Body.String())
	}
	session := decodeMap(t, first)
	if !strings.HasPrefix(session["id"].(string), "cag_") || session["kind"] != KindSession || session["cloud_workspace_id"] != "" {
		t.Fatalf("session = %+v", session)
	}
	if session["status"] != StatusProvisionFailed {
		t.Fatalf("missing maclawsrv host status = %v", session["status"])
	}
	second := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "编程", Kind: KindCoding})
	if second.Code != http.StatusConflict {
		t.Fatalf("override of 1 should reject the second instance: %d %s", second.Code, second.Body.String())
	}
	if err := svc.SaveUserLimits(context.Background(), "tenant-a", "user-1", UserLimits{}); err != nil {
		t.Fatal(err)
	}
	instances, turns, _, err := svc.EffectiveLimits(context.Background(), "tenant-a", "user-1")
	if err != nil || instances != 3 || turns != 1 {
		t.Fatalf("inherit = %d %d %v", instances, turns, err)
	}
	codingRec := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "编程", Kind: KindCoding})
	if codingRec.Code != http.StatusCreated {
		t.Fatalf("create coding: %d %s", codingRec.Code, codingRec.Body.String())
	}
	coding := decodeMap(t, codingRec)
	if coding["kind"] != KindCoding || coding["id"] == session["id"] {
		t.Fatalf("coding = %+v", coding)
	}
	renamed := call(mux, http.MethodPatch, "/api/v1/cloud-agents/"+session["id"].(string), map[string]any{"name": "不该改", "kind": KindCoding})
	if renamed.Code != http.StatusBadRequest || !strings.Contains(decodeMap(t, renamed)["message"].(string), "不能修改") {
		t.Fatalf("kind patch: %d %s", renamed.Code, renamed.Body.String())
	}
	got := call(mux, http.MethodGet, "/api/v1/cloud-agents/"+session["id"].(string), nil)
	body := decodeMap(t, got)
	if body["kind"] != KindSession || body["name"] != "会话" {
		t.Fatalf("kind changed: %+v", body)
	}
	third := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "三", Kind: KindSession})
	fourth := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "四", Kind: KindSession})
	if third.Code != http.StatusCreated || fourth.Code != http.StatusConflict {
		t.Fatalf("quota 3: third %d fourth %d %s", third.Code, fourth.Code, fourth.Body.String())
	}
	zero := 0
	if err := svc.SaveUserLimits(context.Background(), "tenant-a", "user-2", UserLimits{MaxInstances: &zero}); err != nil {
		t.Fatal(err)
	}
	other := UserHandler{Svc: svc, Viewer: viewer("tenant-a", "user-2")}
	otherMux := http.NewServeMux()
	otherMux.HandleFunc("POST /api/v1/cloud-agents", other.Create)
	blocked := call(otherMux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "零", Kind: KindSession})
	if blocked.Code != http.StatusConflict {
		t.Fatalf("zero cap: %d %s", blocked.Code, blocked.Body.String())
	}
}

func TestHeartbeatAndStaleTurn(t *testing.T) {
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url":"http://127.0.0.1:9","admin_secret":"sec-1"}`,
	}})
	svc.now = func() time.Time { return base }
	enable(t, svc, "tenant-a", 3, 1)
	inst, err := svc.Create(context.Background(), "tenant-a", "user-1", CreateInput{Name: "会话", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	missed := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/heartbeat", strings.NewReader(`{"instance_id":"`+inst.ID+`","task_id":"task-1","request_id":"req-1"}`))
	missed.RemoteAddr = ""
	missed.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec := httptest.NewRecorder()
	svc.ServeHeartbeat(rec, missed)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("empty remote addr: %d %s", rec.Code, rec.Body.String())
	}
	svc.now = func() time.Time { return base.Add(30 * time.Second) }
	beat := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/heartbeat", strings.NewReader(`{"instance_id":"`+inst.ID+`","task_id":"task-1","request_id":"req-1"}`))
	beat.RemoteAddr = "127.0.0.1:9"
	beat.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec = httptest.NewRecorder()
	svc.ServeHeartbeat(rec, beat)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("heartbeat: %d %s", rec.Code, rec.Body.String())
	}
	if err := svc.InterruptStale(context.Background(), base.Add(30*time.Second+89*time.Second)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-1'`).Scan(&status); err != nil || status != TurnRunning {
		t.Fatalf("status after 89s = %s %v", status, err)
	}
	if err := svc.InterruptStale(context.Background(), base.Add(30*time.Second+91*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-1'`).Scan(&status); err != nil || status != TurnInterrupted {
		t.Fatalf("status after 91s = %s %v", status, err)
	}
}

func TestFinishTurnReleasesTheSlot(t *testing.T) {
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url":"http://127.0.0.1:9","admin_secret":"sec-1"}`,
	}})
	svc.now = func() time.Time { return base }
	enable(t, svc, "tenant-a", 3, 1)
	inst, err := svc.Create(context.Background(), "tenant-a", "user-1", CreateInput{Name: "会话", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-2", "req-2"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-1'`).Scan(&status); err != nil || status != TurnFinished {
		t.Fatalf("previous request status=%s err=%v", status, err)
	}
	var running int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, inst.ID, TurnRunning).Scan(&running); err != nil || running != 1 {
		t.Fatalf("running=%d err=%v", running, err)
	}
	done := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/heartbeat", strings.NewReader(`{"instance_id":"`+inst.ID+`","task_id":"task-2","request_id":"req-2","finished":true}`))
	done.RemoteAddr = "127.0.0.1:9"
	done.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec := httptest.NewRecorder()
	svc.ServeHeartbeat(rec, done)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("finish beat: %d %s", rec.Code, rec.Body.String())
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, inst.ID, TurnRunning).Scan(&running); err != nil || running != 0 {
		t.Fatalf("running after finish=%d err=%v", running, err)
	}
	var instStatus string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agents WHERE id = ?`, inst.ID).Scan(&instStatus); err != nil || instStatus != StatusIdle {
		t.Fatalf("instance status=%s err=%v", instStatus, err)
	}
	early := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/heartbeat", strings.NewReader(`{"instance_id":"`+inst.ID+`","task_id":"task-3","request_id":"req-3","finished":true}`))
	early.RemoteAddr = "127.0.0.1:9"
	early.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec = httptest.NewRecorder()
	svc.ServeHeartbeat(rec, early)
	if rec.Code != http.StatusOK {
		t.Fatalf("early finish: %d %s", rec.Code, rec.Body.String())
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-3", "req-3"); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-3'`).Scan(&status); err != nil || status != TurnFinished {
		t.Fatalf("accept after finish status=%s err=%v", status, err)
	}
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, inst.ID, TurnRunning).Scan(&running); err != nil || running != 0 {
		t.Fatalf("running after early finish=%d err=%v", running, err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-4", "req-4"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Stop(context.Background(), "tenant-a", "user-1", inst.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.FinishTurn(context.Background(), inst.ID, "task-4", "req-4", base); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-4'`).Scan(&status); err != nil || status != TurnCancelled {
		t.Fatalf("cancelled turn status=%s err=%v", status, err)
	}
}

func TestFinishTurnRetiresPreviousWhenNotedLate(t *testing.T) {
	base := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	svc := New(openTestDB(t), memSettings{kv: map[string]string{
		SrvSettingsKey("tenant-a"): `{"base_url":"http://127.0.0.1:9","admin_secret":"sec-1"}`,
	}})
	svc.now = func() time.Time { return base }
	enable(t, svc, "tenant-a", 3, 1)
	inst, err := svc.Create(context.Background(), "tenant-a", "user-1", CreateInput{Name: "会话", Kind: KindSession})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-1", "req-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.FinishTurn(context.Background(), inst.ID, "task-2", "req-2", base); err != nil {
		t.Fatal(err)
	}
	if err := svc.NoteAcceptedTurn(context.Background(), "tenant-a", "user-1", inst.ID, "task-2", "req-2"); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-1'`).Scan(&status); err != nil || status != TurnFinished {
		t.Fatalf("previous status=%s err=%v", status, err)
	}
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agent_turns WHERE request_id = 'req-2'`).Scan(&status); err != nil || status != TurnFinished {
		t.Fatalf("early finish status=%s err=%v", status, err)
	}
	var running int
	if err := svc.db.QueryRow(`SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, inst.ID, TurnRunning).Scan(&running); err != nil || running != 0 {
		t.Fatalf("running=%d err=%v", running, err)
	}
	var instStatus string
	if err := svc.db.QueryRow(`SELECT status FROM cloud_agents WHERE id = ?`, inst.ID).Scan(&instStatus); err != nil || instStatus != StatusIdle {
		t.Fatalf("instance status=%s err=%v", instStatus, err)
	}
}

func TestDesktopExecuteRemoteControlHasNoCloudOps(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "guiapp", "remote_control.go")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(text)
	start := strings.Index(src, "func (a *App) executeRemoteControl")
	if start < 0 {
		t.Fatal("executeRemoteControl missing")
	}
	rest := src[start+1:]
	next := strings.Index(rest, "\nfunc ")
	body := src[start:]
	if next >= 0 {
		body = src[start : start+1+next]
	}
	for _, op := range []string{
		"cloud_list_tasks", "cloud_get_task", "cloud_create_task", "cloud_send_message",
		"cloud_stop_task", "cloud_rename_task", "cloud_watch_task", "cloud_unwatch_task", "cloud_fetch_file",
	} {
		if strings.Contains(body, op) {
			t.Fatalf("desktop executeRemoteControl implements %s", op)
		}
	}
	if !strings.Contains(body, "不支持这个远控操作") {
		t.Fatal("desktop default rejection missing")
	}
}
