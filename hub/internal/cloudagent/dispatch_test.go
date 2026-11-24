package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	mu.Lock()
	if len(hits) != before {
		t.Fatalf("refused targets called runtime: %v", hits)
	}
	mu.Unlock()

	// A bot owner_node pin does not reroute an existing public instance:
	// its recorded srv binding is empty, so it still dispatches to the
	// public runtime where it was provisioned.
	set(`{"base_url":"` + runtime.URL + `","admin_secret":"sec-1","owner_node":{"user-1":"node-9"},"access_token":"bot-token-should-not-leak"}`)
	if _, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudListTasks, json.RawMessage(`{"instance_id":"`+id+`"}`)); err != nil {
		t.Fatalf("public instance under a bot pin: %v", err)
	}
	mu.Lock()
	if len(hits) == before {
		t.Fatal("public-bound instance did not reach its runtime")
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

func TestSrvConnResolvesOwnerCloudNode(t *testing.T) {
	settings := memSettings{kv: map[string]string{}}
	svc := New(openTestDB(t), settings)
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"http://127.0.0.1:9","admin_secret":"pub-sec","owner_cloud_node":{"user-1":"node-7"},"node_links":[{"node_id":"node-7","base_url":"http://srv.node-7.node.invalid","admin_secret":"link-sec"}]}`

	// The pin resolves new instances to the node srv.
	conn, err := svc.srvConn(context.Background(), "tenant-a", "user-1")
	if err != nil || conn.NodeID != "node-7" || conn.AdminSecret != "link-sec" {
		t.Fatalf("node conn = %+v err = %v", conn, err)
	}
	pub, err := svc.srvConn(context.Background(), "tenant-a", "user-2")
	if err != nil || pub.NodeID != "" || pub.AdminSecret != "pub-sec" {
		t.Fatalf("public conn = %+v err = %v", pub, err)
	}

	// Instance-level routing follows the recorded srv, not the pin: an
	// instance bound to node-7 authenticates with the link secret even
	// after the pin moves, and a public instance stays public.
	if !svc.AdminSecretMatches(context.Background(), "tenant-a", "node-7", "link-sec") {
		t.Fatal("link secret should authenticate a node-bound instance")
	}
	if svc.AdminSecretMatches(context.Background(), "tenant-a", "node-7", "pub-sec") {
		t.Fatal("public secret must not authenticate a node-bound instance")
	}
	if !svc.SrvConnIsNode(context.Background(), "tenant-a", "node-7") {
		t.Fatal("a node-bound instance should report a node conn")
	}
	if svc.SrvConnIsNode(context.Background(), "tenant-a", "") {
		t.Fatal("a public instance must report the public conn")
	}

	// A pin without a matching link fails closed: no silent public fallback.
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"http://127.0.0.1:9","admin_secret":"pub-sec","owner_cloud_node":{"user-1":"node-missing"}}`
	if _, err := svc.srvConn(context.Background(), "tenant-a", "user-1"); err == nil {
		t.Fatal("pin without link must fail closed")
	}
	if _, err := svc.srvConnForNode(context.Background(), "tenant-a", "node-missing"); err == nil {
		t.Fatal("a recorded node without a link must fail closed")
	}
}

// rewriteHost sends any request to the test server regardless of its URL:
// the stand-in for an edge-tunnel client reaching srv.<id>.node.invalid.
type rewriteHost struct{ base *url.URL }

func (rt rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.base.Scheme
	clone.URL.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func TestProvisionOnNodeSrvRecordsBindingAndPublicLoopback(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	var got map[string]any
	var gotSecret string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		gotSecret = r.Header.Get("X-MaClaw-Admin-Secret")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer runtime.Close()
	runtimeURL, _ := url.Parse(runtime.URL)

	settings := memSettings{kv: map[string]string{}}
	db := openTestDB(t)
	svc := New(db, settings)
	svc.NodeHTTP = func(string) *http.Client {
		return &http.Client{Transport: rewriteHost{base: runtimeURL}}
	}
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"http://127.0.0.1:9","admin_secret":"pub-sec","owner_cloud_node":{"user-1":"node-7"},"node_links":[{"node_id":"node-7","base_url":"http://srv.node-7.node.invalid","admin_secret":"link-sec"}]}`

	if _, err := svc.db.ExecContext(context.Background(), `INSERT INTO cloud_agents (id, tenant_id, owner_user_id, name, kind, llm_mode, status, created_at, updated_at) VALUES ('cag_p', 'tenant-a', 'user-1', 's', 'session', 'official', 'provisioning', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	inst := Instance{ID: "cag_p", TenantID: "tenant-a", OwnerUserID: "user-1"}
	if err := svc.disp.provision(context.Background(), &inst); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if inst.SrvNodeID != "node-7" {
		t.Fatalf("srv node = %q", inst.SrvNodeID)
	}
	var recorded string
	if err := db.QueryRow(`SELECT srv_node_id FROM cloud_agents WHERE id = 'cag_p'`).Scan(&recorded); err != nil || recorded != "node-7" {
		t.Fatalf("recorded = %q %v", recorded, err)
	}
	if got["hub_loopback"] != "https://hub.example" {
		t.Fatalf("hub_loopback = %v", got["hub_loopback"])
	}
	if got["hub_origin"] != "https://hub.example" {
		t.Fatalf("hub_origin = %v", got["hub_origin"])
	}
	if gotSecret != "link-sec" {
		t.Fatalf("secret = %q", gotSecret)
	}
}

func TestDispatchReprovisionsFailedInstance(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/control") {
			_, _ = w.Write([]byte(`{"accepted":true,"request_id":"req-9","task_id":"task-9"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer runtime.Close()

	settings := memSettings{kv: map[string]string{}}
	db := openTestDB(t)
	svc := New(db, settings)
	rc := remotecontrol.New(db, nopAccounts{}, ownMachines{}, nopSend{}, nopNote{})
	rc.Cloud = svc.Dispatcher()
	mux := userMux(svc, rc)

	// Create against a non-loopback base_url: the instance goes straight to
	// provision_failed without a single runtime call.
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"https://maclawsrv.example","admin_secret":"sec-1"}`
	enable(t, svc, "tenant-a", 3, 1)
	created := call(mux, http.MethodPost, "/api/v1/cloud-agents", CreateInput{Name: "会话", Kind: KindSession})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	id := decodeMap(t, created)["id"].(string)
	var status string
	if err := db.QueryRow(`SELECT status FROM cloud_agents WHERE id = ?`, id).Scan(&status); err != nil || status != StatusProvisionFailed {
		t.Fatalf("status = %s %v", status, err)
	}
	grantRec := call(mux, http.MethodPost, "/api/v1/cloud-agents/"+id+"/control-grant", map[string]any{})
	if grantRec.Code != http.StatusOK {
		t.Fatalf("grant: %d %s", grantRec.Code, grantRec.Body.String())
	}
	grantID := decodeMap(t, grantRec)["grant_id"].(string)
	mu.Lock()
	before := len(hits)
	mu.Unlock()

	// Once the record is fixed the next op re-provisions and dispatches.
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"` + runtime.URL + `","admin_secret":"sec-1"}`
	raw, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudSendMessage, json.RawMessage(`{"instance_id":"`+id+`","task_id":"task-9","text":"hi"}`))
	if err != nil {
		t.Fatalf("send after repair: %v", err)
	}
	if !strings.Contains(string(raw), `"request_id":"req-9"`) {
		t.Fatalf("result = %s", raw)
	}
	if err := db.QueryRow(`SELECT status FROM cloud_agents WHERE id = ?`, id).Scan(&status); err != nil || status != StatusRunning {
		t.Fatalf("status after re-provision = %s %v", status, err)
	}
	mu.Lock()
	joined := strings.Join(hits[before:], "\n")
	mu.Unlock()
	put := strings.Index(joined, "PUT /api/v1/cloud-agents/"+id)
	ctl := strings.Index(joined, "POST /api/v1/cloud-agents/"+id+"/control")
	if put < 0 || ctl < 0 || put > ctl {
		t.Fatalf("expected PUT then control, hits = %s", joined)
	}
}

func TestDispatchReprovisionsAfterRuntimeNotFound(t *testing.T) {
	var mu sync.Mutex
	var hits []string
	provisioned := false
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPut {
			provisioned = true
		}
		known := provisioned
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/control") {
			if !known {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"没有找到这个云端实例"}`))
				return
			}
			_, _ = w.Write([]byte(`{"tasks":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
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
	grantID := decodeMap(t, grantRec)["grant_id"].(string)

	// Simulate the runtime losing the instance (wiped dir, replaced srv).
	mu.Lock()
	provisioned = false
	before := len(hits)
	mu.Unlock()
	if _, err := rc.Exec(context.Background(), "tenant-a", "user-1", "mac-1", grantID, remotecontrol.OpCloudListTasks, json.RawMessage(`{"instance_id":"`+id+`"}`)); err != nil {
		t.Fatalf("list after runtime loss: %v", err)
	}
	mu.Lock()
	joined := strings.Join(hits[before:], "\n")
	mu.Unlock()
	want := "POST /api/v1/cloud-agents/" + id + "/control\nPUT /api/v1/cloud-agents/" + id + "\nPOST /api/v1/cloud-agents/" + id + "/control"
	if joined != want {
		t.Fatalf("hits =\n%s\nwant =\n%s", joined, want)
	}
}

func TestProvisionDefaultsHubOriginToLoopback(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "")
	t.Setenv("MACLAW_HUB_LOOPBACK", "")
	var got map[string]any
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer runtime.Close()

	settings := memSettings{kv: map[string]string{}}
	db := openTestDB(t)
	svc := New(db, settings)
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"` + runtime.URL + `","admin_secret":"sec-1"}`
	if _, err := svc.db.ExecContext(context.Background(), `INSERT INTO cloud_agents (id, tenant_id, owner_user_id, name, kind, llm_mode, status, created_at, updated_at) VALUES ('cag_l', 'tenant-a', 'user-1', 's', 'session', 'official', 'provisioning', 'now', 'now')`); err != nil {
		t.Fatal(err)
	}
	inst := Instance{ID: "cag_l", TenantID: "tenant-a", OwnerUserID: "user-1"}
	if err := svc.disp.provision(context.Background(), &inst); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if got["hub_origin"] != "http://127.0.0.1:9399" || got["hub_loopback"] != "http://127.0.0.1:9399" {
		t.Fatalf("origin = %v loopback = %v", got["hub_origin"], got["hub_loopback"])
	}
}

func TestProvisionOnNodeSrvWithoutPublicOriginFails(t *testing.T) {
	t.Setenv("MACLAW_HUB_URL", "")
	settings := memSettings{kv: map[string]string{}}
	svc := New(openTestDB(t), settings)
	settings.kv[SrvSettingsKey("tenant-a")] = `{"base_url":"http://127.0.0.1:9","admin_secret":"pub-sec","owner_cloud_node":{"user-1":"node-7"},"node_links":[{"node_id":"node-7","base_url":"http://srv.node-7.node.invalid","admin_secret":"link-sec"}]}`

	inst := Instance{ID: "cag_dead", TenantID: "tenant-a", OwnerUserID: "user-1"}
	err := svc.disp.provision(context.Background(), &inst)
	var failure *Failure
	if !errors.As(err, &failure) || !strings.Contains(failure.Message, "MACLAW_HUB_URL") {
		t.Fatalf("err = %v", err)
	}
	// The pin resolves and the binding is recorded before the origin check,
	// so the failure is about hub config, not node selection.
	if inst.SrvNodeID != "node-7" {
		t.Fatalf("srv node = %q", inst.SrvNodeID)
	}
}
