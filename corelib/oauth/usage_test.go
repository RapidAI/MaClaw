package oauth

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryUsageFrom_Success(t *testing.T) {
	resp := UsageInfo{
		TotalGranted:   50.0,
		TotalUsed:      12.34,
		TotalAvailable: 37.66,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	info, err := QueryUsageFrom(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(info.TotalGranted-50.0) > 0.001 {
		t.Errorf("TotalGranted = %f, want 50.0", info.TotalGranted)
	}
	if math.Abs(info.TotalUsed-12.34) > 0.001 {
		t.Errorf("TotalUsed = %f, want 12.34", info.TotalUsed)
	}
	if math.Abs(info.TotalAvailable-37.66) > 0.001 {
		t.Errorf("TotalAvailable = %f, want 37.66", info.TotalAvailable)
	}
}

func TestQueryUsageFrom_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer srv.Close()

	_, err := QueryUsageFrom(srv.URL, "bad-token")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestQueryUsageFrom_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := QueryUsageFrom(srv.URL, "token")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
