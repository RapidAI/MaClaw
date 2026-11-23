package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
)

type ownMachines struct{}

func (ownMachines) BelongsTo(context.Context, string, string) (remotecontrol.Machine, bool) {
	return remotecontrol.Machine{ID: "mac-1", Name: "pc"}, true
}

func (ownMachines) Online(string) bool { return false }

type nopSend struct{}

func (nopSend) Send(string, any) error { return nil }

type nopNote struct{}

func (nopNote) Notify(context.Context, remotecontrol.Notice) (string, error) { return "n", nil }
func (nopNote) Revoke(context.Context, string)                               {}

type nopAccounts struct{}

func (nopAccounts) Lookup(context.Context, string, string) (string, bool) { return "", false }

func TestDispatchReportsRuntimeClientError(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/control") && strings.Contains(string(raw), "还没检出") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"还没有检出代码库"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/control") && strings.Contains(string(raw), "boom") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"accepted":true,"request_id":"req-1","task_id":"task-1"}`))
	}))
	defer runtime.Close()

	settings := memSettings{kv: map[string]string{}}
	db := openTestDB(t)
	svc := New(db, settings)
	rc := remotecontrol.New(db, nopAccounts{}, ownMachines{}, nopSend{}, nopNote{})
	rc.Cloud = svc.Dispatcher()
	mux := userMux(svc, rc)
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"` + runtime.URL + `","admin_secret":"sec-1"}`
	enable(t, svc, "tenant-a", 3, 1)
	created := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "会话", Kind: KindSession})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := decodeMap(t, created)["id"].(string)
	grantRec := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/control-grant", map[string]any{})
	if grantRec.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", grantRec.Code, grantRec.Body.String())
	}
	grantID := decodeMap(t, grantRec)["grant_id"].(string)

	_, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"task_id":"task-1","text":"还没检出"}`))
	var status *remotecontrol.StatusError
	if !errors.As(err, &status) || status.Code != "INVALID" || status.Message != "还没有检出代码库" {
		t.Fatalf("client error = %v", err)
	}
	_, err = rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"task_id":"task-1","text":"boom"}`))
	status = nil
	if !errors.As(err, &status) || status.Code != "OFFLINE" || status.Message != "云端运行时不可用" {
		t.Fatalf("server error = %v", err)
	}
}

func TestDispatchUsesLoopbackAdminSecretOnly(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	var lastBody, lastSecret, lastAuth string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		lastBody = string(raw)
		lastSecret = r.Header.Get("X-MaClaw-Admin-Secret")
		lastAuth = r.Header.Get("Authorization")
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/control"):
			_, _ = w.Write([]byte(`{"accepted":true,"request_id":"req-1","task_id":"task-1"}`))
		case strings.HasSuffix(r.URL.Path, "/git"):
			_, _ = w.Write([]byte(`{"connected":true,"service":"github","origin":"https://github.com","repo":"acme/demo","author_name":"Ada","author_email":"ada@example.com","token":"git-secret-token"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer runtime.Close()

	settings := memSettings{kv: map[string]string{}}
	db := openTestDB(t)
	svc := New(db, settings)
	rc := remotecontrol.New(db, nopAccounts{}, ownMachines{}, nopSend{}, nopNote{})
	rc.Cloud = svc.Dispatcher()
	mux := userMux(svc, rc)
	key := SrvSettingsKey("tenant-a")
	set := func(raw string) { settings.kv[key] = raw }
	set(`{"base_url":"` + runtime.URL + `","admin_secret":"sec-1","access_token":"bot-token-should-not-leak"}`)
	enable(t, svc, "tenant-a", 3, 1)

	created := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "会话", Kind: KindSession})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := decodeMap(t, created)["id"].(string)
	grantRec := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/control-grant", map[string]any{})
	if grantRec.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", grantRec.Code, grantRec.Body.String())
	}
	grantID := decodeMap(t, grantRec)["grant_id"].(string)
	again := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/control-grant", map[string]any{})
	if decodeMap(t, again)["grant_id"] != grantID {
		t.Fatalf("grant not idempotent: %s", again.Body.String())
	}

	off := false
	three, one := 3, 1
	if _, err := svc.SaveAdminSettings(context.Background(), "tenant-a", AdminSettings{CloudAgentsEnabled: &off, MaxInstances: &three, MaxConcurrentTurns: &one}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	before := len(hits)
	mu.Unlock()
	_, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"instance_id":"`+id+`","task_id":"task-1","text":"hi"}`))
	if err == nil || !strings.Contains(err.Error(), DisabledMessage) {
		t.Fatalf("flag off: %v", err)
	}
	mu.Lock()
	if len(hits) != before {
		t.Fatalf("flag off still called runtime: %v", hits)
	}
	mu.Unlock()

	on := true
	if _, err := svc.SaveAdminSettings(context.Background(), "tenant-a", AdminSettings{CloudAgentsEnabled: &on, MaxInstances: &three, MaxConcurrentTurns: &one}); err != nil {
		t.Fatal(err)
	}
	set(`{"base_url":"http://maclawsrv.example","admin_secret":"sec-1","access_token":"bot-token-should-not-leak"}`)
	_, err = rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudListTasks, json.RawMessage(`{"instance_id":"`+id+`"}`))
	var status *remotecontrol.StatusError
	if !errors.As(err, &status) || status.Code != "OFFLINE" || status.Message != "云端运行时不可用" {
		t.Fatalf("public runtime: %v", err)
	}
	mu.Lock()
	if len(hits) != before {
		t.Fatalf("public url called runtime: %v", hits)
	}
	mu.Unlock()
	var instStatus string
	if err := db.QueryRow(`SELECT status FROM cloud_agents WHERE id = ?`, id).Scan(&instStatus); err != nil || instStatus != StatusProvisionFailed {
		t.Fatalf("status = %s %v", instStatus, err)
	}

	set(`{"base_url":"` + runtime.URL + `","admin_secret":"sec-1","dial_node":"node-1","access_token":"bot-token-should-not-leak"}`)
	if _, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudListTasks, json.RawMessage(`{"instance_id":"`+id+`"}`)); err == nil {
		t.Fatal("dial_node was dispatched")
	}
	set(`{"base_url":"` + runtime.URL + `","admin_secret":"sec-1","owner_node":{"user-1":"node-9"},"access_token":"bot-token-should-not-leak"}`)
	if _, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudListTasks, json.RawMessage(`{"instance_id":"`+id+`"}`)); err == nil {
		t.Fatal("owner node was dispatched")
	}
	mu.Lock()
	if len(hits) != before {
		t.Fatalf("refused targets called runtime: %v", hits)
	}
	mu.Unlock()

	set(`{"base_url":"` + runtime.URL + `","admin_secret":"sec-1","access_token":"bot-token-should-not-leak"}`)
	raw, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"instance_id":"`+id+`","task_id":"task-1","text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"request_id":"req-1"`) {
		t.Fatalf("result = %s", raw)
	}
	mu.Lock()
	gotBody, gotSecret, gotAuth := lastBody, lastSecret, lastAuth
	gotHits := append([]string(nil), hits...)
	mu.Unlock()
	if gotSecret != "sec-1" || gotAuth != "" {
		t.Fatalf("secret=%q auth=%q", gotSecret, gotAuth)
	}
	if !strings.Contains(gotBody, `"owner_user_id":"user-1"`) || strings.Contains(gotBody, "bot-token-should-not-leak") {
		t.Fatalf("control body = %s", gotBody)
	}
	if !strings.Contains(strings.Join(gotHits, "\n"), "POST /api/v1/cloud-agents/"+id+"/control") {
		t.Fatalf("hits = %v", gotHits)
	}
	raw, err = rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"instance_id":"`+id+`","task_id":"task-1","text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	var turns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND request_id = 'req-1'`, id).Scan(&turns); err != nil || turns != 1 {
		t.Fatalf("turns = %d %v", turns, err)
	}

	sessionGit := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/git", GitConnect{
		Service: "github", Origin: "https://github.com", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com",
	})
	if sessionGit.Code != http.StatusForbidden {
		t.Fatalf("session git: %d %s", sessionGit.Code, sessionGit.Body.String())
	}

	coding := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "编程", Kind: KindCoding})
	if coding.Code != http.StatusCreated {
		t.Fatalf("coding: %d %s", coding.Code, coding.Body.String())
	}
	codingID := decodeMap(t, coding)["id"].(string)
	for _, origin := range []string{"http://github.com/acme/demo", "git@github.com:acme/demo.git"} {
		rec := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+codingID+"/git", GitConnect{
			Service: "github", Origin: origin, Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com",
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("origin %s: %d %s", origin, rec.Code, rec.Body.String())
		}
	}
	mu.Lock()
	beforeGit := len(hits)
	mu.Unlock()
	connected := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+codingID+"/git", GitConnect{
		Service: "github", Origin: "https://github.com", Repo: "acme/demo", Token: "git-secret-token", AuthorName: "Ada", AuthorEmail: "ada@example.com",
	})
	if connected.Code != http.StatusOK || strings.Contains(connected.Body.String(), "git-secret-token") {
		t.Fatalf("git response: %d %s", connected.Code, connected.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT COALESCE(group_concat(name||kind||llm_mode||llm_label||llm_model||status), '') FROM cloud_agents`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "git-secret-token") || strings.Contains(stored, "bot-token-should-not-leak") {
		t.Fatalf("secret stored in hub row: %s", stored)
	}
	mu.Lock()
	if len(hits) != beforeGit+1 {
		t.Fatalf("git hits = %v", hits)
	}
	mu.Unlock()

	llm := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/llm", CustomLLM{URL: "https://hub.example/api/llm/v1", Key: "sk-hub", Model: "auto"})
	if llm.Code != http.StatusBadRequest {
		t.Fatalf("hub llm url: %d %s", llm.Code, llm.Body.String())
	}
	custom := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/llm", CustomLLM{URL: "https://llm.example/v1", Key: "sk-custom", Model: "demo"})
	if custom.Code != http.StatusOK || strings.Contains(custom.Body.String(), "sk-custom") {
		t.Fatalf("custom llm: %d %s", custom.Code, custom.Body.String())
	}
	var mode string
	if err := db.QueryRow(`SELECT llm_mode FROM cloud_agents WHERE id = ?`, id).Scan(&mode); err != nil || mode != LLMCustom {
		t.Fatalf("mode = %s %v", mode, err)
	}
}
