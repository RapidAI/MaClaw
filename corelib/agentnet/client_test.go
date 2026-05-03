package agentnet

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testClient(server *httptest.Server) *Client {
	return &Client{
		baseURL: server.URL,
		client:  &http.Client{Timeout: time.Second},
	}
}

func TestHTTPHelpersAttachBearerToken(t *testing.T) {
	t.Setenv("AGENTNETWORK_API_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var out map[string]bool
	if err := testClient(server).get("/api/status", &out); err != nil {
		t.Fatalf("get failed: %v", err)
	}
}

func TestHTTPHelpersReadBearerTokenFromHomeFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("AGENTNETWORK_API_TOKEN", "")
	if err := os.MkdirAll(filepath.Join(home, ".anet"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".anet", "api_token"), []byte("file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer file-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	var out map[string]bool
	if err := testClient(server).get("/api/status", &out); err != nil {
		t.Fatalf("get failed: %v", err)
	}
}

func TestCreateTaskNormalizesSmallRewardsAndRequiresDepositConfirmation(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"task-1","title":"demo","reward":0}`))
	}))
	defer server.Close()
	c := testClient(server)

	if _, err := c.CreateTaskWithOptions("demo", "", 99, nil, "", TaskCreateOptions{RequireDeposit: true}); err == nil {
		t.Fatal("expected unconfirmed deposit to fail")
	}
	if _, err := c.CreateTask("demo", 99); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if got := payload["reward"]; got != float64(0) {
		t.Fatalf("reward = %v, want 0", got)
	}
	if _, ok := payload["require_deposit"]; ok {
		t.Fatal("require_deposit should not be set by default")
	}
}

func TestAttachAndDownloadBundleUseAuthorizedBinaryRequests(t *testing.T) {
	t.Setenv("AGENTNETWORK_API_TOKEN", "bundle-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer bundle-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/api/tasks/task%2F1/bundle":
			body, _ := io.ReadAll(r.Body)
			if string(body) != "bundle" {
				t.Fatalf("bundle body = %q", string(body))
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/tasks/task%2F1/bundle":
			_, _ = w.Write([]byte("bundle"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	c := testClient(server)

	if err := c.AttachBundle("task/1", []byte("bundle")); err != nil {
		t.Fatalf("AttachBundle failed: %v", err)
	}
	data, err := c.DownloadBundle("task/1")
	if err != nil {
		t.Fatalf("DownloadBundle failed: %v", err)
	}
	if string(data) != "bundle" {
		t.Fatalf("bundle data = %q", string(data))
	}
}

func TestRegisterServiceRejectsNonLocalURLsAndNormalizesPayload(t *testing.T) {
	if err := validateServiceRegistration(&Service{Name: "bad", URL: "https://example.com"}); err == nil {
		t.Fatal("expected non-local service URL to fail")
	}
	var payload Service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/svc/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c := testClient(server)

	if err := c.RegisterService(&Service{Name: "SearchAPI", URL: "http://127.0.0.1:8080", Billing: "per_call", Price: 2}); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
	if payload.Name != "searchapi" {
		t.Fatalf("service name = %q, want searchapi", payload.Name)
	}
	if len(payload.Modes) != 1 || payload.Modes[0] != "rr" {
		t.Fatalf("modes = %#v, want rr default", payload.Modes)
	}
}

func TestANSValidationAndDMSupportsAgentURI(t *testing.T) {
	if _, err := validateANSName("Bad-Name"); err == nil {
		t.Fatal("expected invalid ANS name to fail")
	}
	var dmPayload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ans/resolve":
			if got := r.URL.Query().Get("name"); got != "alice" {
				t.Fatalf("resolved name = %q, want alice", got)
			}
			_, _ = w.Write([]byte(`{"name":"alice","did":"did:key:z6MkAlice"}`))
		case "/api/dm/send-plaintext":
			if err := json.NewDecoder(r.Body).Decode(&dmPayload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := testClient(server)

	if err := c.SendDM("agent://alice", "hello"); err != nil {
		t.Fatalf("SendDM failed: %v", err)
	}
	if got := dmPayload["to"]; got != "did:key:z6MkAlice" {
		t.Fatalf("dm target = %v, want resolved DID", got)
	}
}

func TestInitAgentRequiresNameAndSkills(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	c := testClient(server)
	if err := c.InitAgent("", []string{"coding"}); err == nil {
		t.Fatal("expected missing name to fail")
	}
	if err := c.InitAgent("bot", nil); err == nil {
		t.Fatal("expected missing skills to fail")
	}
}

func TestCreateSplitTaskNormalizesReward(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks/split" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"id":"split-1","title":"demo","reward":0}`))
	}))
	defer server.Close()

	if _, err := testClient(server).CreateSplitTask("demo", 50, 2); err != nil {
		t.Fatalf("CreateSplitTask failed: %v", err)
	}
	if got := payload["reward"]; got != float64(0) {
		t.Fatalf("reward = %v, want 0", got)
	}
	if got := payload["slots"]; got != float64(2) {
		t.Fatalf("slots = %v, want 2", got)
	}
}

func TestServiceCallNormalizesPayloadAndRejectsBadNames(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/svc/call" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c := testClient(server)

	if _, err := c.CallService(" peer-1 ", "SearchAPI", "get", "query", nil, ""); err != nil {
		t.Fatalf("CallService failed: %v", err)
	}
	if payload["peer"] != "peer-1" || payload["service"] != "searchapi" || payload["method"] != "GET" || payload["path"] != "/query" {
		t.Fatalf("payload = %#v", payload)
	}
	if _, err := c.CallService("peer-1", "bad-name", "POST", "/", nil, ""); err == nil {
		t.Fatal("expected invalid service name to fail")
	}
	if err := c.UnregisterService("Bad-Name"); err == nil {
		t.Fatal("expected invalid unregister service name to fail")
	}
}

func TestQueryOntologyNormalizesDepth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ontology/subgraph" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "graph" {
			t.Fatalf("query = %q", got)
		}
		if got := r.URL.Query().Get("depth"); got != "10" {
			t.Fatalf("depth = %q, want 10", got)
		}
		_, _ = w.Write([]byte(`{"nodes":[]}`))
	}))
	defer server.Close()
	c := testClient(server)

	if _, err := c.QueryOntology(" graph ", 99); err != nil {
		t.Fatalf("QueryOntology failed: %v", err)
	}
	if _, err := c.QueryOntology(" ", 1); err == nil {
		t.Fatal("expected empty ontology query to fail")
	}
}

func TestServiceRegistrationNormalizesURLBillingAndModes(t *testing.T) {
	var payload Service
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/svc/register" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	c := testClient(server)

	if err := c.RegisterService(&Service{Name: "SearchAPI", URL: " http://127.0.0.1:8080 ", Billing: " PER_CALL ", Price: 2, Modes: []string{" RR "}}); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}
	if payload.URL != "http://127.0.0.1:8080" || payload.Billing != "per_call" || len(payload.Modes) != 1 || payload.Modes[0] != "rr" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestTransferCreditsValidatesAndNormalizesPayload(t *testing.T) {
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
	c := testClient(server)

	if err := c.TransferCredits(" did:key:z6MkBob ", 3, "thanks"); err != nil {
		t.Fatalf("TransferCredits failed: %v", err)
	}
	if payload["to"] != "did:key:z6MkBob" || payload["amount"] != float64(3) || payload["reason"] != "thanks" {
		t.Fatalf("payload = %#v", payload)
	}
	if err := c.TransferCredits("did:key:z6MkBob", 0, ""); err == nil {
		t.Fatal("expected non-positive transfer amount to fail")
	}
	if err := c.TransferCredits(" ", 1, ""); err == nil {
		t.Fatal("expected empty transfer recipient to fail")
	}
}

func TestPingTreatsUnauthorizedAsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	if !testClient(server).ping() {
		t.Fatal("expected unauthorized status to mean daemon is reachable")
	}
}

func TestBidAndAssignValidation(t *testing.T) {
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
	c := testClient(server)

	if err := c.BidOnTask("task-1", 0, " hello "); err != nil {
		t.Fatalf("BidOnTask failed: %v", err)
	}
	if payload["message"] != "hello" {
		t.Fatalf("bid payload = %#v", payload)
	}
	if err := c.BidOnTask("task-1", -1, ""); err == nil {
		t.Fatal("expected negative bid amount to fail")
	}
	if err := c.AssignTask("task-1", " peer-1 "); err != nil {
		t.Fatalf("AssignTask failed: %v", err)
	}
	if payload["bidder_id"] != "peer-1" {
		t.Fatalf("assign payload = %#v", payload)
	}
	if err := c.AssignTask("task-1", " "); err == nil {
		t.Fatal("expected empty assignee to fail")
	}
}

func TestServiceCallRejectsProtocolRelativePath(t *testing.T) {
	if _, _, _, _, err := normalizeServiceCall("peer-1", "search", "POST", "//evil"); err == nil {
		t.Fatal("expected protocol-relative service path to fail")
	}
}
