package tenant

import (
	"context"
	"encoding/json"
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
