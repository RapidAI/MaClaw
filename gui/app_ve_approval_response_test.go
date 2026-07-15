package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestVEHandleApprovalRequest_AutoApprovePostsDecision(t *testing.T) {
	var decisionHits int
	var lastBody string
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore capability sync.
		if strings.Contains(r.URL.Path, "approval_capability") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/decision") {
			decisionHits++
			raw, _ := io.ReadAll(r.Body)
			lastBody = string(raw)
			if !strings.Contains(lastBody, `"decision":"approve"`) {
				t.Errorf("body=%s", lastBody)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"instance_id": "inst-1", "node_id": "n1", "status": "running"})
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-ve-1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultVEApprovalConfig()
	cfg.Enabled = true
	cfg.ACL = AccessControlList{Mode: ACLBlacklist} // empty blacklist = allow all
	cfg.Rules.AutoApprove = []ApprovalRule{{
		Name: "small-amount",
		Conditions: []RuleCondition{{
			Field:    "amount",
			Operator: OpLT,
			Value:    float64(1000),
		}},
	}}
	if err := app.SaveVEApprovalConfig(cfg); err != nil {
		t.Fatal(err)
	}

	details, _ := json.Marshal(map[string]any{"amount": 100})
	payload, _ := json.Marshal(veApprovalRequestPayload{
		ID:            "req-1",
		InstanceID:    "inst-1",
		NodeID:        "n1",
		RequesterID:   "u1",
		RequesterName: "Alice",
		WorkflowName:  "Expense",
		Title:         "Expense #1",
		Summary:       "100 yuan",
		Details:       details,
	})
	handler := NewVEMessageHandler(app)
	handler.HandleGroupEnvelope(a2a.GroupEnvelope{
		Type:      a2a.GroupMessageApprovalRequest,
		SessionID: "inst-1",
		Payload:   payload,
		FromID:    "hub-workflow-engine",
		CreatedAt: time.Now().UTC(),
	})

	if decisionHits != 1 {
		t.Fatalf("decisionHits=%d body=%s", decisionHits, lastBody)
	}
}

func TestVEHandleApprovalRequest_RequireHumanEnqueuesLocal(t *testing.T) {
	var decisionHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "approval_capability") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if strings.Contains(r.URL.Path, "/decision") {
			decisionHits++
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-ve-1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultVEApprovalConfig()
	cfg.Enabled = true
	cfg.ACL = AccessControlList{Mode: ACLBlacklist} // empty blacklist = allow all
	// Empty rules → default require_human
	if err := app.SaveVEApprovalConfig(cfg); err != nil {
		t.Fatal(err)
	}

	details, _ := json.Marshal(map[string]any{"amount": 9999})
	payload, _ := json.Marshal(veApprovalRequestPayload{
		ID:           "req-human-1",
		InstanceID:   "inst-h1",
		NodeID:       "mgr",
		RequesterID:  "u2",
		WorkflowName: "Expense",
		Title:        "Big expense",
		Details:      details,
	})
	handler := NewVEMessageHandler(app)
	handler.HandleGroupEnvelope(a2a.GroupEnvelope{
		Type:      a2a.GroupMessageApprovalRequest,
		SessionID: "inst-h1",
		Payload:   payload,
		FromID:    "hub-workflow-engine",
		CreatedAt: time.Now().UTC(),
	})

	if decisionHits != 0 {
		t.Fatalf("require_human must not call decision API, hits=%d", decisionHits)
	}
	list, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected local pending approval instance")
	}
	found := list[0]
	if found.HubInstanceID != "inst-h1" || found.HubNodeID != "mgr" {
		t.Fatalf("hub binding missing: %#v", found)
	}
	if found.ApprovalEngine != maclawAppApprovalEngineHub {
		t.Fatalf("engine=%q", found.ApprovalEngine)
	}
}

func TestApplyVETwoPhaseExecutionPolicy(t *testing.T) {
	t.Parallel()
	// digital_suggest: both auto paths become require_human
	d := applyVETwoPhaseExecutionPolicy("digital_suggest", &ApprovalDecision{
		Decision: DecisionAutoApprove, Rationale: "amount ok",
	})
	if d.Decision != DecisionRequireHuman || !strings.Contains(d.Rationale, "digital_suggest") {
		t.Fatalf("suggest approve: %#v", d)
	}
	d = applyVETwoPhaseExecutionPolicy("digital_suggest", &ApprovalDecision{
		Decision: DecisionAutoReject, Rationale: "too high",
	})
	if d.Decision != DecisionRequireHuman {
		t.Fatalf("suggest reject should not finalize: %#v", d)
	}
	// digital_review: auto_reject finalizes, auto_approve needs human
	d = applyVETwoPhaseExecutionPolicy("digital_review", &ApprovalDecision{
		Decision: DecisionAutoReject, Rationale: "policy deny",
	})
	if d.Decision != DecisionAutoReject {
		t.Fatalf("review reject should finalize: %#v", d)
	}
	d = applyVETwoPhaseExecutionPolicy("digital_review", &ApprovalDecision{
		Decision: DecisionAutoApprove, Rationale: "precheck ok",
	})
	if d.Decision != DecisionRequireHuman || !strings.Contains(d.Rationale, "digital_review") {
		t.Fatalf("review approve needs human: %#v", d)
	}
	// auto: unchanged
	d = applyVETwoPhaseExecutionPolicy("auto", &ApprovalDecision{Decision: DecisionAutoApprove, Rationale: "ok"})
	if d.Decision != DecisionAutoApprove {
		t.Fatalf("auto should pass through: %#v", d)
	}
}

func TestVEHandleApprovalRequest_DigitalSuggestDoesNotAutoApprove(t *testing.T) {
	var decisionHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "approval_capability") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if strings.Contains(r.URL.Path, "/decision") {
			decisionHits++
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL: hub.URL, RemoteMachineToken: "machine-token", RemoteMachineID: "m1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultVEApprovalConfig()
	cfg.Enabled = true
	cfg.ACL = AccessControlList{Mode: ACLBlacklist}
	cfg.Rules.AutoApprove = []ApprovalRule{{
		Name: "small", Conditions: []RuleCondition{{Field: "amount", Operator: OpLT, Value: float64(1000)}},
	}}
	if err := app.SaveVEApprovalConfig(cfg); err != nil {
		t.Fatal(err)
	}
	details, _ := json.Marshal(map[string]any{"amount": 100, "execution_mode": "digital_suggest"})
	payload, _ := json.Marshal(veApprovalRequestPayload{
		ID: "req-suggest", InstanceID: "inst-s", NodeID: "n1", Details: details,
	})
	NewVEMessageHandler(app).HandleGroupEnvelope(a2a.GroupEnvelope{
		Type: a2a.GroupMessageApprovalRequest, SessionID: "inst-s", Payload: payload,
	})
	if decisionHits != 0 {
		t.Fatalf("digital_suggest must not call decision API, hits=%d", decisionHits)
	}
	list, err := app.ListMaclawAppApprovalInstancesAll("pending_my_approval", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected require_human local pending for digital_suggest")
	}
}

func TestHandleVEApprovalRequestRoutesToHandler(t *testing.T) {
	var decisionHits int
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "approval_capability") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/decision") {
			decisionHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "running"})
			return
		}
		http.NotFound(w, r)
	}))
	defer hub.Close()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteHubURL:       hub.URL,
		RemoteMachineToken: "machine-token",
		RemoteMachineID:    "machine-ve-1",
	}); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultVEApprovalConfig()
	cfg.Enabled = true
	cfg.ACL = AccessControlList{Mode: ACLBlacklist}
	cfg.Rules.AutoReject = []ApprovalRule{{
		Name:       "block",
		Conditions: []RuleCondition{{Field: "flag", Operator: OpEquals, Value: "bad"}},
	}}
	if err := app.SaveVEApprovalConfig(cfg); err != nil {
		t.Fatal(err)
	}

	details, _ := json.Marshal(map[string]any{"flag": "bad"})
	reqPayload, _ := json.Marshal(veApprovalRequestPayload{
		ID: "req-route", InstanceID: "inst-r", NodeID: "n", Details: details,
	})
	envelope := a2a.GroupEnvelope{
		Type: a2a.GroupMessageApprovalRequest, SessionID: "inst-r",
		Payload: reqPayload, FromID: "hub-workflow-engine", CreatedAt: time.Now().UTC(),
	}
	wire, _ := json.Marshal(map[string]any{"envelope": envelope})

	client := &RemoteHubClient{app: app}
	client.handleVEApprovalRequest(inboundHubEnvelope{
		Type:    veEventApprovalRequest,
		Payload: wire,
	})

	if decisionHits != 1 {
		t.Fatalf("expected decision API hit via routed handler, got %d", decisionHits)
	}
}
