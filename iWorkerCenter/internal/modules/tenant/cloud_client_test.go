package tenant

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchCenterLicenseUsesCenterSecretHeader(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/license" {
			t.Fatalf("path = %q, want /api/centers/center-1/license", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
			t.Fatalf("X-Center-Secret = %q, want secret-abc", got)
		}
		if got := r.URL.Query().Get("secret"); got != "" {
			t.Fatalf("secret query should not be set, got %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CloudLicense{
			ID:          "lic-1",
			CenterID:    "center-1",
			Modules:     `["compute","skill_market"]`,
			Type:        "manual",
			ExpiresAt:   now.Add(24 * time.Hour),
			IsLongTerm:  false,
			Certificate: "signed-json",
			CreatedAt:   now,
		})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL + "/"})
	lic, err := client.FetchCenterLicense(context.Background(), "center-1", "secret-abc")
	if err != nil {
		t.Fatalf("FetchCenterLicense() error: %v", err)
	}
	if lic.ID != "lic-1" || lic.CenterID != "center-1" || lic.Certificate != "signed-json" {
		t.Fatalf("license = %+v", lic)
	}
}

func TestFetchCenterLicenseRequiresConfiguredCloud(t *testing.T) {
	client := NewCloudClient(CloudConfig{})
	if _, err := client.FetchCenterLicense(context.Background(), "center-1", "secret-abc"); err == nil {
		t.Fatal("FetchCenterLicense() should fail when cloud is not configured")
	}
}

func TestFetchCenterLicenseRequiresCenterCredentials(t *testing.T) {
	client := NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com"})
	if _, err := client.FetchCenterLicense(context.Background(), "", "secret-abc"); err == nil || !strings.Contains(err.Error(), "center_id") {
		t.Fatalf("empty center id error = %v, want center_id error", err)
	}
	if _, err := client.FetchCenterLicense(context.Background(), "center-1", ""); err == nil || !strings.Contains(err.Error(), "center_secret") {
		t.Fatalf("empty secret error = %v, want center_secret error", err)
	}
}

func TestFetchCenterLicenseReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	_, err := client.FetchCenterLicense(context.Background(), "center-1", "bad-secret")
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %v, want status 401", err)
	}
}

func TestSendCenterHeartbeatUsesServiceIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/heartbeat" {
			t.Fatalf("path = %q, want /api/centers/center-1/heartbeat", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		var req CenterHeartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Secret != "secret-abc" || req.RuntimeType != "service" || req.ProductKind != "iworkercenter" || req.AdminConsole != "web_console" {
			t.Fatalf("heartbeat request = %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL + "/"})
	if err := client.SendCenterHeartbeat(context.Background(), "center-1", "secret-abc", nil); err != nil {
		t.Fatalf("SendCenterHeartbeat() error: %v", err)
	}
}

func TestSendCenterHeartbeatIncludesOnlyCloudSafeIWorkerReadiness(t *testing.T) {
	seen := make(chan CenterHeartbeatRequest, 1)
	rawBody := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		rawBody <- string(body)
		var req CenterHeartbeatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		seen <- req
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	readiness := &CloudIWorkerReadiness{Ready: true, Status: "ready", AgentInstanceCount: 4, AgentRuntimeReady: true, GoalWatchReady: true, WorkloadSummary: &CloudWorkloadSummary{AgentInstanceCount: 4, ActiveCount: 2, CompletedCount: 8, ReviewCount: 1, BlockedCount: 0}}
	if err := client.SendCenterHeartbeat(context.Background(), "center-1", "secret-abc", readiness); err != nil {
		t.Fatalf("SendCenterHeartbeat() error: %v", err)
	}
	req := <-seen
	if req.IWorkerReadiness == nil || !req.IWorkerReadiness.Ready || req.IWorkerReadiness.AgentInstanceCount != 4 || !req.IWorkerReadiness.AgentRuntimeReady || !req.IWorkerReadiness.GoalWatchReady {
		t.Fatalf("IWorkerReadiness = %+v", req.IWorkerReadiness)
	}
	body := <-rawBody
	for _, forbidden := range []string{"tenant_count", "role_count", "colleague_count", "local_account_count", "required_client_paths", "checks", "auth_methods", "current_task", "current_detail"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("heartbeat leaked %q: %s", forbidden, body)
		}
	}
}

func TestSendCenterHeartbeatRequiresCenterCredentials(t *testing.T) {
	client := NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com"})
	if err := client.SendCenterHeartbeat(context.Background(), "", "secret-abc", nil); err == nil || !strings.Contains(err.Error(), "center_id") {
		t.Fatalf("empty center id error = %v, want center_id error", err)
	}
	if err := client.SendCenterHeartbeat(context.Background(), "center-1", "", nil); err == nil || !strings.Contains(err.Error(), "center_secret") {
		t.Fatalf("empty secret error = %v, want center_secret error", err)
	}
}

func TestSendCenterHeartbeatReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad identity", http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	err := client.SendCenterHeartbeat(context.Background(), "center-1", "secret-abc", nil)
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("error = %v, want status 401", err)
	}
}

func TestFetchCenterComputeProvidersUsesCenterSecretHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/centers/center-1/compute-providers" {
			t.Fatalf("path = %q, want /api/centers/center-1/compute-providers", r.URL.Path)
		}
		if got := r.Header.Get("X-Center-Secret"); got != "secret-abc" {
			t.Fatalf("X-Center-Secret = %q, want secret-abc", got)
		}
		if got := r.URL.Query().Get("secret"); got != "" {
			t.Fatalf("secret query should not be set, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"compute_permission":true,"force_sync":true,"providers":[{"id":"p1","name":"Cloud GPT","base_url":"https://llm.example/v1","api_key":"sk-cloud","protocol":"openai","model":"gpt-4.1","enabled":true,"priority":80,"description":"cloud assigned"}]}`))
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL + "/"})
	result, err := client.FetchCenterComputeProviders(context.Background(), "center-1", "secret-abc")
	if err != nil {
		t.Fatalf("FetchCenterComputeProviders() error: %v", err)
	}
	if !result.ComputePermission || !result.ForceSync {
		t.Fatalf("response flags = %+v", result)
	}
	if len(result.Providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(result.Providers))
	}
	provider := result.Providers[0]
	if provider.ID != "p1" || provider.APIKey != "sk-cloud" || provider.Protocol != "openai" || provider.Model != "gpt-4.1" {
		t.Fatalf("provider = %+v", provider)
	}
}

func TestFetchCenterComputeProvidersRequiresCenterCredentials(t *testing.T) {
	client := NewCloudClient(CloudConfig{BaseURL: "https://cloud.example.com"})
	if _, err := client.FetchCenterComputeProviders(context.Background(), "", "secret-abc"); err == nil || !strings.Contains(err.Error(), "center_id") {
		t.Fatalf("empty center id error = %v, want center_id error", err)
	}
	if _, err := client.FetchCenterComputeProviders(context.Background(), "center-1", ""); err == nil || !strings.Contains(err.Error(), "center_secret") {
		t.Fatalf("empty secret error = %v, want center_secret error", err)
	}
}

func TestFetchCenterComputeProvidersReturnsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "disabled", http.StatusForbidden)
	}))
	defer srv.Close()

	client := NewCloudClient(CloudConfig{BaseURL: srv.URL})
	_, err := client.FetchCenterComputeProviders(context.Background(), "center-1", "secret-abc")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("error = %v, want status 403", err)
	}
}
