package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetHubCenterJSONReturnsExplicitLimitErrorInsteadOfUnexpectedEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"payload": strings.Repeat("x", 64)})
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]string
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", 32, &dest)
	if err == nil {
		t.Fatal("expected response size error")
	}
	if !strings.Contains(err.Error(), "hubcenter response exceeds 32 bytes") {
		t.Fatalf("error = %v, want explicit size limit", err)
	}
}

func TestGetHubCenterJSONAcceptsResponseAtLimit(t *testing.T) {
	body := `{"ok":true}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	app := &App{}
	var dest map[string]bool
	_, _, err := app.getHubCenterJSONFromCandidates(context.Background(), server.Client(), []string{server.URL}, "/download", int64(len(body)), &dest)
	if err != nil {
		t.Fatalf("getHubCenterJSONFromCandidates: %v", err)
	}
	if !dest["ok"] {
		t.Fatalf("dest = %+v", dest)
	}
}

func TestGetHubCenterBytesReturnsExactLimitedBody(t *testing.T) {
	body := strings.Repeat("a", 32)
	data, err := readLimitedHubCenterBody(strings.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("readLimitedHubCenterBody: %v", err)
	}
	if string(data) != body {
		t.Fatalf("data = %q, want %q", string(data), body)
	}
}
