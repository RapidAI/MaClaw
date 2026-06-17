package llmservice

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
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
