package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudagent"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
	_ "modernc.org/sqlite"
)

type streamMachines struct{}

func (streamMachines) BelongsTo(_ context.Context, _, machineID string) (remotecontrol.Machine, bool) {
	if machineID == "" {
		return remotecontrol.Machine{}, false
	}
	return remotecontrol.Machine{ID: machineID, Name: machineID}, true
}
func (streamMachines) Online(string) bool { return false }

type streamSend struct {
	msgs []any
}

func (s *streamSend) Send(_ string, msg any) error {
	s.msgs = append(s.msgs, msg)
	return nil
}

type streamNote struct{}

func (streamNote) Notify(context.Context, remotecontrol.Notice) (string, error) {
	return "", nil
}
func (streamNote) Revoke(context.Context, string) {}

type streamAccounts struct{}

func (streamAccounts) Lookup(context.Context, string, string) (string, bool) { return "", false }

type streamSettings map[string]string

func (s streamSettings) Get(_ context.Context, key string) (string, error) { return s[key], nil }

func TestCloudAgentStreamSkipsDesktopGrantPath(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	sender := &streamSend{}
	rc := remotecontrol.New(db, streamAccounts{}, streamMachines{}, sender, streamNote{})
	svc := cloudagent.New(db, streamSettings{
		cloudagent.SrvSettingsKey("tenant-1"): `{"base_url":"http://127.0.0.1:9","admin_secret":"sec-1"}`,
	})
	// Stream auth resolves the instance's recorded srv binding, so the
	// grant target must be a real instance row.
	if _, err := db.Exec(`INSERT INTO cloud_agents (id, tenant_id, owner_user_id, name, kind, llm_mode, status, created_at, updated_at) VALUES ('cag_stream', 'tenant-1', 'user-a', 's', 'session', 'official', 'idle', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	grant, err := rc.EnsureCloudGrant(context.Background(), "tenant-1", "user-a", "mac-1", "cag_stream", "user-a")
	if err != nil {
		t.Fatal(err)
	}
	desk, err := rc.GrantSameAccount(context.Background(), "tenant-1", "user-a", "mac-1", "mac-2", remotecontrol.PermOperate)
	if err != nil {
		t.Fatal(err)
	}
	handler := cloudAgentStream(rc, svc)
	payload := `{"grant_id":"` + grant.ID + `","session_key":"cloud-agent:cag_stream:task-1","event":"token","project_path":"","text":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/stream", strings.NewReader(payload))
	req.RemoteAddr = "127.0.0.1:9"
	req.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream: %d %s", rec.Code, rec.Body.String())
	}
	if len(sender.msgs) != 1 {
		t.Fatalf("sends = %d", len(sender.msgs))
	}
	env, ok := sender.msgs[0].(map[string]any)
	if !ok || env["type"] != "remote_control.stream" || env["machine_id"] != "" {
		t.Fatalf("envelope = %#v", sender.msgs[0])
	}
	raw, _ := env["payload"].(json.RawMessage)
	if !strings.Contains(string(raw), "cloud-agent:cag_stream:task-1") || !strings.Contains(string(raw), `"project_path":""`) {
		t.Fatalf("payload = %#v", env["payload"])
	}

	foreign := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/stream", strings.NewReader(payload))
	foreign.RemoteAddr = "10.1.1.1:9"
	foreign.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec = httptest.NewRecorder()
	handler(rec, foreign)
	if rec.Code != http.StatusForbidden || len(sender.msgs) != 1 {
		t.Fatalf("non-loopback: %d sends=%d", rec.Code, len(sender.msgs))
	}

	desktop := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/stream", strings.NewReader(`{"grant_id":"`+desk.ID+`","event":"token"}`))
	desktop.RemoteAddr = "127.0.0.1:9"
	desktop.Header.Set("X-MaClaw-Admin-Secret", "sec-1")
	rec = httptest.NewRecorder()
	handler(rec, desktop)
	if rec.Code == http.StatusOK || len(sender.msgs) != 1 {
		t.Fatalf("desktop grant streamed: %d sends=%d", rec.Code, len(sender.msgs))
	}
	bad := httptest.NewRequest(http.MethodPost, "/api/internal/cloud-agents/stream", strings.NewReader(payload))
	bad.RemoteAddr = "127.0.0.1:9"
	bad.Header.Set("X-MaClaw-Admin-Secret", "nope")
	rec = httptest.NewRecorder()
	handler(rec, bad)
	if rec.Code != http.StatusUnauthorized || len(sender.msgs) != 1 {
		t.Fatalf("bad secret: %d sends=%d", rec.Code, len(sender.msgs))
	}
}
