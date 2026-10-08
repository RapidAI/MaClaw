package zhipu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// overrideEndpoints points the login origins and business host at test
// servers and returns a restore func.
func overrideEndpoints(t *testing.T, origins []string, business string) func() {
	t.Helper()
	prevOrigins, prevBusiness := endpointOrigins, bigModelOrigin
	endpointOrigins, bigModelOrigin = origins, business
	return func() {
		endpointOrigins, bigModelOrigin = prevOrigins, prevBusiness
	}
}

func TestIsAuthorizeURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://bigmodel.cn/login?appId=zcode&redirect=http%3A%2F%2F127.0.0.1%3A1%2Fx&state=ab", true},
		{"https://zcode.chatglm.site/login", true},
		{"https://zcode.z.ai/login", true},
		{"http://127.0.0.1:9999/login", true}, // loopback test servers
		{"http://bigmodel.cn/login", false},   // plain http on a real origin
		{"https://evil.example.com/login", false},
		{"ftp://bigmodel.cn/login", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsAuthorizeURL(tc.raw); got != tc.want {
			t.Errorf("IsAuthorizeURL(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// startOriginServer serves the init endpoint and records the request. Polls
// are answered by the returned per-flow state machine the test installs.
func startOriginServer(t *testing.T, authorizeURL string, polls *atomic.Int32, readyAfter int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/oauth/cli/init", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("init method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") || len(got)-len("Bearer ") != 64 {
			t.Errorf("init Authorization = %q, want 64-hex bearer token", got)
		}
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("init body decode: %v", err)
		}
		if body.Provider != OAuthProviderID {
			t.Errorf("init provider = %q, want %q", body.Provider, OAuthProviderID)
		}
		writeJSON(w, map[string]any{
			"code": 0,
			"data": map[string]any{
				"flow_id":           "flow-1",
				"authorize_url":     authorizeURL,
				"expires_at":        time.Now().Add(5 * time.Minute).Unix(),
				"poll_interval_sec": 2,
			},
		})
	})
	mux.HandleFunc("/api/v1/oauth/cli/poll/flow-1", func(w http.ResponseWriter, r *http.Request) {
		if polls == nil {
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"status": "pending"}})
			return
		}
		if polls.Add(1) >= readyAfter {
			writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"status": "ready",
				"token":  "jwt-token",
				"bigmodel": map[string]any{
					"access_token":  "bm-access",
					"refresh_token": "bm-refresh",
				},
				"user": map[string]any{"user_id": "u-1", "name": "Zhipu User", "email": "u@example.com"},
			}})
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"status": "pending"}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func TestStartLoginCreatesFlow(t *testing.T) {
	origin := startOriginServer(t, "https://bigmodel.cn/login?appId=zcode", nil, 0)
	restore := overrideEndpoints(t, []string{origin.URL}, origin.URL)
	defer restore()

	login, err := StartLogin(context.Background())
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if !strings.HasPrefix(login.AuthURL(), "https://bigmodel.cn/login") {
		t.Errorf("AuthURL = %q", login.AuthURL())
	}
	if login.flowID != "flow-1" {
		t.Errorf("flowID = %q", login.flowID)
	}
	if login.pollToken == "" || len(login.pollToken) != 64 {
		t.Errorf("pollToken missing or wrong length: %d", len(login.pollToken))
	}
	if login.pollInterval < minPollInterval {
		t.Errorf("pollInterval = %v, want >= %v", login.pollInterval, minPollInterval)
	}
}

func TestStartLoginFallsBackToSecondOrigin(t *testing.T) {
	fallback := startOriginServer(t, "https://bigmodel.cn/login?appId=zcode", nil, 0)
	restore := overrideEndpoints(t, []string{"http://127.0.0.1:1", fallback.URL}, "")
	defer restore()

	login, err := StartLogin(context.Background())
	if err != nil {
		t.Fatalf("StartLogin fallback: %v", err)
	}
	if !strings.Contains(login.origin, "127.0.0.1") {
		t.Errorf("origin = %q, want the reachable fallback", login.origin)
	}
}

func TestPollUntilReady(t *testing.T) {
	var polls atomic.Int32
	restore := overrideEndpoints(t, []string{startOriginServer(t, "https://bigmodel.cn/login", &polls, 3).URL}, "")
	defer restore()

	login, err := StartLogin(context.Background())
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	login.pollInterval = time.Millisecond // keep the test fast

	token, err := PollUntilReady(context.Background(), login)
	if err != nil {
		t.Fatalf("PollUntilReady: %v", err)
	}
	if token.AccessToken != "bm-access" || token.RefreshToken != "bm-refresh" {
		t.Errorf("token = %+v", token)
	}
	if token.JWT != "jwt-token" || token.UserID != "u-1" || token.Email != "u@example.com" {
		t.Errorf("token = %+v", token)
	}
}

func TestPollUntilReadyFailedStatus(t *testing.T) {
	login := newPollTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{"status": "failed"}})
	})
	if _, err := PollUntilReady(context.Background(), login); err == nil || !strings.Contains(err.Error(), "智谱授权失败") {
		t.Fatalf("PollUntilReady err = %v, want 智谱授权失败", err)
	}
}

func TestPollUntilReadyNilLogin(t *testing.T) {
	if _, err := PollUntilReady(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil login")
	}
}

// newPollTestLogin starts an origin server whose poll responses are produced
// by the caller-supplied handler and returns a login pointed at it.
func newPollTestLogin(t *testing.T, pollHandler func(w http.ResponseWriter, r *http.Request)) *Login {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/oauth/cli/init", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
			"flow_id": "flow-1", "authorize_url": "https://bigmodel.cn/login",
			"expires_at": time.Now().Add(time.Minute).Unix(), "poll_interval_sec": 1,
		}})
	})
	mux.HandleFunc("/api/v1/oauth/cli/poll/flow-1", pollHandler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	restore := overrideEndpoints(t, []string{server.URL}, "")
	t.Cleanup(restore)

	login, err := StartLogin(context.Background())
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	login.pollInterval = time.Millisecond // keep the test fast
	return login
}

// The CLI tolerates 5xx (and 408/429) poll rounds: one gateway blip must not
// kill an approval the user is mid-way through.
func TestPollUntilReadyRetriesServerErrors(t *testing.T) {
	var polls atomic.Int32
	login := newPollTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		if polls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"code": 0, "data": map[string]any{
			"status": "ready", "token": "jwt",
			"bigmodel": map[string]any{"access_token": "bm-access"},
			"user":     map[string]any{"user_id": "u-1"},
		}})
	})
	token, err := PollUntilReady(context.Background(), login)
	if err != nil {
		t.Fatalf("PollUntilReady: %v", err)
	}
	if token.AccessToken != "bm-access" {
		t.Errorf("AccessToken = %q", token.AccessToken)
	}
}

// A missing flow never comes back: like the CLI, a 404 must fail the login
// instead of silently polling until the approval window closes.
func TestPollUntilReadyStopsOnNotFound(t *testing.T) {
	login := newPollTestLogin(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	start := time.Now()
	_, err := PollUntilReady(context.Background(), login)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("PollUntilReady err = %v, want HTTP 404 failure", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("404 poll took %s, want a prompt failure", elapsed)
	}
}

// startBusinessServer serves the BigModel business chain: customer info,
// api key list/create, and the secret copy.
func startBusinessServer(t *testing.T, listHasKey bool, recorded *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/biz/customer/getCustomerInfo", func(w http.ResponseWriter, r *http.Request) {
		*recorded = append(*recorded, r.URL.Path)
		writeJSON(w, map[string]any{"code": 200, "data": map[string]any{
			"organizations": []map[string]any{
				{"organizationId": "org-other", "organizationName": "Other Org", "projects": []map[string]any{
					{"projectId": "p-other", "projectName": "Other Project"},
				}},
				{"organizationId": "org-main", "organizationName": "默认机构", "projects": []map[string]any{
					{"projectId": "p-main", "projectName": "Some Project"},
					{"projectId": "proj-default", "projectName": "默认项目"},
				}},
			},
		}})
	})
	listHandler := func(w http.ResponseWriter, r *http.Request) {
		*recorded = append(*recorded, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create body: %v", err)
			}
			if body.Name != codingPlanKeyName {
				t.Errorf("create name = %q, want %q", body.Name, codingPlanKeyName)
			}
			writeJSON(w, map[string]any{"code": 200, "data": map[string]any{"apiKey": "key-new"}})
			return
		}
		if listHasKey {
			writeJSON(w, map[string]any{"code": 200, "data": []map[string]any{
				{"apiKey": "key-existing", "name": codingPlanKeyName},
				{"apiKey": "key-other", "name": "other"},
			}})
			return
		}
		writeJSON(w, map[string]any{"code": 200, "data": []map[string]any{}})
	}
	mux.HandleFunc("/api/biz/v1/organization/org-main/projects/proj-default/api_keys", listHandler)
	mux.HandleFunc("/api/biz/v1/organization/org-main/projects/proj-default/api_keys/copy/", func(w http.ResponseWriter, r *http.Request) {
		*recorded = append(*recorded, r.URL.Path)
		writeJSON(w, map[string]any{"code": 200, "data": map[string]any{"secretKey": "secret-1"}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestResolveCodingPlanAPIKeyCreatesMissingKey(t *testing.T) {
	var recorded []string
	restore := overrideEndpoints(t, endpointOrigins, startBusinessServer(t, false, &recorded).URL)
	defer restore()

	apiKey, err := ResolveCodingPlanAPIKey(context.Background(), "bm-access")
	if err != nil {
		t.Fatalf("ResolveCodingPlanAPIKey: %v", err)
	}
	if apiKey != "key-new.secret-1" {
		t.Errorf("apiKey = %q, want key-new.secret-1", apiKey)
	}
	// The 默认机构/默认项目 entries win over the first listing.
	joined := strings.Join(recorded, "\n")
	if !strings.Contains(joined, "/organization/org-main/projects/proj-default/api_keys") {
		t.Errorf("requests = %s, want org-main/proj-default path", joined)
	}
	if !strings.Contains(joined, "POST /api/biz/v1/organization/org-main/projects/proj-default/api_keys") {
		t.Errorf("requests = %s, want create POST", joined)
	}
}

func TestResolveCodingPlanAPIKeyReusesExistingKey(t *testing.T) {
	var recorded []string
	restore := overrideEndpoints(t, endpointOrigins, startBusinessServer(t, true, &recorded).URL)
	defer restore()

	apiKey, err := ResolveCodingPlanAPIKey(context.Background(), "bm-access")
	if err != nil {
		t.Fatalf("ResolveCodingPlanAPIKey: %v", err)
	}
	if apiKey != "key-existing.secret-1" {
		t.Errorf("apiKey = %q, want key-existing.secret-1", apiKey)
	}
	if strings.Contains(strings.Join(recorded, "\n"), "POST ") {
		t.Errorf("requests = %v, want no create POST when the key exists", recorded)
	}
}

func TestResolveCodingPlanAPIKeyRequiresToken(t *testing.T) {
	if _, err := ResolveCodingPlanAPIKey(context.Background(), "  "); err == nil {
		t.Fatal("expected error for empty access token")
	}
}

func TestStartLoginBusinessErrorDoesNotFallThrough(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/oauth/cli/init", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"code": 3004, "msg": "invalid_flow"})
	})
	server := httptest.NewServer(mux)
	restore := overrideEndpoints(t, []string{server.URL, "http://127.0.0.1:1"}, "")
	defer restore()

	_, err := StartLogin(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid_flow") {
		t.Fatalf("StartLogin err = %v, want invalid_flow business error", err)
	}
}
