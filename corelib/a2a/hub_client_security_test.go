package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHubClientRejectsOversizedSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat(" ", (32<<20)+1)))
	}))
	defer server.Close()
	client, err := NewHubClient(server.URL, WithHubBearerToken("secret"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListExperts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized response error = %v, want explicit size rejection", err)
	}
}

func TestNewHubClientRejectsUnsafeEndpointForms(t *testing.T) {
	t.Setenv("MACLAW_HUB_ALLOW_HTTP", "")
	for _, endpoint := range []string{
		"hub.example",
		"http://hub.example",
		"ftp://hub.example",
		"https://user:secret@hub.example",
	} {
		if _, err := NewHubClient(endpoint); err == nil {
			t.Errorf("endpoint %q was accepted", endpoint)
		}
	}
}
