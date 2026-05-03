package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testCleanLastUpdate removes the timestamp file before/after a test.
func testCleanLastUpdate(t *testing.T) string {
	t.Helper()
	p := agentnetLastUpdatePath()
	if p == "" {
		t.Skip("cannot determine home dir")
	}
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	os.Remove(p)
	t.Cleanup(func() { os.Remove(p) })
	return p
}

func TestNeedsUpdate_NoFile(t *testing.T) {
	testCleanLastUpdate(t)
	if !needsUpdate() {
		t.Error("expected needsUpdate=true when no timestamp file exists")
	}
}

func TestWriteAndReadLastUpdateTime(t *testing.T) {
	testCleanLastUpdate(t)

	writeLastUpdateTime()
	last := readLastUpdateTime()
	if last.IsZero() {
		t.Fatal("readLastUpdateTime returned zero after write")
	}
	if time.Since(last) > 5*time.Second {
		t.Errorf("timestamp too old: %v", last)
	}
}

func TestNeedsUpdate_RecentWrite(t *testing.T) {
	testCleanLastUpdate(t)

	writeLastUpdateTime()
	if needsUpdate() {
		t.Error("expected needsUpdate=false right after writing timestamp")
	}
}

func TestNeedsUpdate_StaleTimestamp(t *testing.T) {
	p := testCleanLastUpdate(t)

	stale := time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339)
	_ = os.WriteFile(p, []byte(stale), 0644)

	if !needsUpdate() {
		t.Error("expected needsUpdate=true for 25h-old timestamp")
	}
}

func TestStartAutoUpdate_Idempotent(t *testing.T) {
	c := NewAgentNetClient()
	defer c.StopAutoUpdate()

	c.StartAutoUpdate(func(string) {})
	c.StartAutoUpdate(func(string) {})
	c.StartAutoUpdate(func(string) {})
	// No panic = pass.
}

func TestStopAutoUpdate_BeforeStart(t *testing.T) {
	c := NewAgentNetClient()
	// StopAutoUpdate before StartAutoUpdate should not panic.
	c.StopAutoUpdate()
	c.StopAutoUpdate()
}

func TestAutoUpdate_RestartAfterStop(t *testing.T) {
	c := NewAgentNetClient()

	c.StartAutoUpdate(func(string) {})
	c.StopAutoUpdate()

	// After stop, StartAutoUpdate should be able to launch again.
	c.StartAutoUpdate(func(string) {})
	c.StopAutoUpdate()
}

func TestAgentNetClientCreateTaskWithOptionsNormalizesReward(t *testing.T) {
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"task-1","title":"small","reward":0,"state":"created"}`))
	}))
	defer server.Close()

	c := NewAgentNetClient()
	c.baseURL = server.URL
	if _, err := c.CreateTaskWithOptions("small", "", 50, nil, "", AgentNetTaskCreateOptions{}); err != nil {
		t.Fatalf("CreateTaskWithOptions: %v", err)
	}
	if got["reward"] != float64(0) {
		t.Fatalf("reward = %#v, want 0", got["reward"])
	}
	if _, ok := got["require_deposit"]; ok {
		t.Fatal("require_deposit should be omitted without explicit opt-in")
	}
}

func TestAgentNetClientCreateTaskWithOptionsRequiresDepositConfirmation(t *testing.T) {
	c := NewAgentNetClient()
	_, err := c.CreateTaskWithOptions("paid", "", 100, nil, "", AgentNetTaskCreateOptions{RequireDeposit: true})
	if err == nil || !strings.Contains(err.Error(), "explicit user confirmation") {
		t.Fatalf("expected deposit confirmation error, got %v", err)
	}

	_, err = c.CreateTaskWithOptions("bad", "", -1, nil, "", AgentNetTaskCreateOptions{})
	if err == nil || !strings.Contains(err.Error(), "cannot be negative") {
		t.Fatalf("expected negative reward error, got %v", err)
	}
}

func TestAgentNetClientSubmitTaskResultSendsEvidenceAndResult(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/tasks/task%2F1/submit" {
			t.Fatalf("unexpected path: %s", r.URL.EscapedPath())
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewAgentNetClient()
	c.baseURL = server.URL
	if err := c.SubmitTaskResult("task/1", "done"); err != nil {
		t.Fatalf("SubmitTaskResult: %v", err)
	}
	if got["result"] != "done" || got["evidence"] != "done" {
		t.Fatalf("payload = %#v, want both result and evidence", got)
	}
}

func TestAgentNetClientLoadAPITokenPrefersEnvironment(t *testing.T) {
	t.Setenv("AGENTNETWORK_API_TOKEN", "env-token")
	c := NewAgentNetClient()
	if got := c.loadAPIToken(); got != "env-token" {
		t.Fatalf("loadAPIToken() = %q, want env-token", got)
	}
}

func TestAgentNetServiceCallNormalizesPayloadAndOntologyDepth(t *testing.T) {
	var servicePayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/svc/call":
			if err := json.NewDecoder(r.Body).Decode(&servicePayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/ontology/subgraph":
			if got := r.URL.Query().Get("depth"); got != "2" {
				t.Fatalf("depth = %q, want 2", got)
			}
			_, _ = w.Write([]byte(`{"nodes":[]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewAgentNetClient()
	c.baseURL = server.URL

	if _, err := c.CallService(" peer-1 ", "SearchAPI", "", "query", nil, ""); err != nil {
		t.Fatalf("CallService: %v", err)
	}
	if servicePayload["peer"] != "peer-1" || servicePayload["service"] != "searchapi" || servicePayload["method"] != "POST" || servicePayload["path"] != "/query" {
		t.Fatalf("payload = %#v", servicePayload)
	}
	if _, err := c.QueryOntology(" graph ", -3); err != nil {
		t.Fatalf("QueryOntology: %v", err)
	}
}

func TestAgentNetTransferCreditsValidatesAndNormalizesPayload(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/credits/transfer" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c := NewAgentNetClient()
	c.baseURL = server.URL

	if err := c.TransferCredits(" did:key:z6MkBob ", 2, "bonus"); err != nil {
		t.Fatalf("TransferCredits: %v", err)
	}
	if payload["to"] != "did:key:z6MkBob" || payload["amount"] != float64(2) || payload["reason"] != "bonus" {
		t.Fatalf("payload = %#v", payload)
	}
	if err := c.TransferCredits("did:key:z6MkBob", -1, ""); err == nil {
		t.Fatal("expected negative transfer amount to fail")
	}
}

func TestAgentNetBidAndAssignValidation(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks/task-1/bid", "/api/tasks/task-1/assign":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewAgentNetClient()
	c.baseURL = server.URL

	if err := c.BidOnTask("task-1", 0, " hello "); err != nil {
		t.Fatalf("BidOnTask: %v", err)
	}
	if payload["message"] != "hello" {
		t.Fatalf("bid payload = %#v", payload)
	}
	if err := c.BidOnTask("task-1", -1, ""); err == nil {
		t.Fatal("expected negative bid amount to fail")
	}
	if err := c.AssignTask("task-1", " peer-1 "); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}
	if payload["bidder_id"] != "peer-1" {
		t.Fatalf("assign payload = %#v", payload)
	}
	if err := c.AssignTask("task-1", " "); err == nil {
		t.Fatal("expected empty assignee to fail")
	}
}
