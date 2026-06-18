package llmservice

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type comparableRoundTripper struct {
	fn roundTripFunc
}

func (rt *comparableRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.fn(req)
}

func TestMaClawProviderClientForwardStreamUsesStreamingClientWithoutTotalTimeout(t *testing.T) {
	transport := &comparableRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Fatalf("path = %q, want chat completions endpoint", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("Authorization = %q, want bearer secret", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Tenant-ID") != "tenant_acme" {
			t.Fatalf("X-Tenant-ID = %q, want tenant_acme", r.Header.Get("X-Tenant-ID"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	}}
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second, Transport: transport}

	streamClient := client.streamHTTPClient()
	if streamClient == client.HTTPClient {
		t.Fatal("streamHTTPClient() reused timeout client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	if streamClient.Transport != transport {
		t.Fatal("streamHTTPClient() did not preserve transport")
	}
	if client.HTTPClient.Timeout != 120*time.Second {
		t.Fatalf("base client timeout = %s, want 120s", client.HTTPClient.Timeout)
	}

	resp, err := client.ForwardStream(context.Background(), []byte(`{"stream":true}`), "tenant_acme")
	if err != nil {
		t.Fatalf("ForwardStream() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMaClawProviderClientForwardStreamBoundsResponseHeaderWait(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Timeout: 120 * time.Second}

	streamClient := client.streamHTTPClient()
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	transport, ok := streamClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream transport = %T, want *http.Transport", streamClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 120s", transport.ResponseHeaderTimeout)
	}
	if client.HTTPClient.Transport != nil {
		t.Fatal("base client transport was mutated")
	}
}

func TestMaClawProviderClientForwardStreamBoundsResponseHeaderWaitWithZeroTimeoutClient(t *testing.T) {
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{}

	streamClient := client.streamHTTPClient()
	if streamClient == client.HTTPClient {
		t.Fatal("streamHTTPClient() reused unbounded default client")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want 0", streamClient.Timeout)
	}
	transport, ok := streamClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream transport = %T, want *http.Transport", streamClient.Transport)
	}
	if transport.ResponseHeaderTimeout != 120*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 120s", transport.ResponseHeaderTimeout)
	}
	if client.HTTPClient.Transport != nil {
		t.Fatal("base client transport was mutated")
	}
}

func TestQueryAuthorizationRefreshesCredentialsWhenMissing(t *testing.T) {
	refreshCalls := 0
	var gotHubID string
	var gotAuth string
	var gotTenantID string

	client := NewMaClawProviderClient(MaClawProviderConfig{HubCenterURL: "https://hubcenter.example.com"})
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("path = %q, want authorization endpoint", r.URL.Path)
		}
		gotHubID = r.URL.Query().Get("hub_id")
		gotTenantID = r.URL.Query().Get("tenant_id")
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("X-Hub-ID") != "hub_refreshed" {
			t.Fatalf("X-Hub-ID = %q, want hub_refreshed", r.Header.Get("X-Hub-ID"))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub_refreshed","tenant_id":"tenant_acme","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}

	client.SetRefreshCredentials(func() (string, string) {
		refreshCalls++
		return "hub_refreshed", "secret_refreshed"
	})

	status, err := client.QueryAuthorization(context.Background(), "tenant_acme")
	if err != nil {
		t.Fatalf("QueryAuthorization() error = %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refreshCalls = %d, want 1", refreshCalls)
	}
	if gotHubID != "hub_refreshed" {
		t.Fatalf("hub_id query = %q, want hub_refreshed", gotHubID)
	}
	if gotTenantID != "tenant_acme" {
		t.Fatalf("tenant_id query = %q, want tenant_acme", gotTenantID)
	}
	if gotAuth != "Bearer secret_refreshed" {
		t.Fatalf("Authorization = %q, want bearer secret", gotAuth)
	}
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("status.AllowExternalProviders = %v, want true", status)
	}
}

func TestTenantLLMAccessControlCachesAuthorizationTenantAliases(t *testing.T) {
	ac := NewTenantLLMAccessControl(nil)
	ac.UpdateFromHeartbeat("tenant_acme", &TenantAuthorizationStatus{
		HubID:                  "hub1",
		TenantID:               "tenant_acme",
		AllowExternalProviders: true,
	})

	status := ac.GetAuthorizationStatus(context.Background(), "acme")
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("alias status = %#v, want allowed", status)
	}
}

func TestTenantLLMAccessControlFetchCachesAuthorizationTenantAliases(t *testing.T) {
	requests := 0
	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub1",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if got := r.URL.Query().Get("tenant_id"); got != "acme" {
			t.Fatalf("tenant_id query = %q, want acme", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub1","tenant_id":"tenant_acme","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}
	ac := NewTenantLLMAccessControl(client)

	status := ac.GetAuthorizationStatus(context.Background(), "acme")
	if status == nil || !status.AllowExternalProviders {
		t.Fatalf("status = %#v, want allowed", status)
	}
	alias := ac.GetAuthorizationStatus(context.Background(), "tenant_acme")
	if alias == nil || !alias.AllowExternalProviders {
		t.Fatalf("alias status = %#v, want allowed", alias)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}
