package guiapp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileCompanionSameOriginIncludesSchemeAndPort(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		value, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, test := range []struct {
		name  string
		left  string
		right string
		want  bool
	}{
		{name: "same origin", left: "https://hub.example/a", right: "https://hub.example/b", want: true},
		{name: "default port", left: "https://hub.example/a", right: "https://hub.example:443/b", want: true},
		{name: "different service port", left: "https://hub.example/a", right: "https://hub.example:8443/b"},
		{name: "TLS downgrade", left: "https://hub.example/a", right: "http://hub.example/b"},
		{name: "different host", left: "https://hub.example/a", right: "https://other.example/b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := fileCompanionSameOrigin(parse(test.left), parse(test.right)); got != test.want {
				t.Fatalf("sameOrigin(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}

func TestFileCompanionDirectClientStopsCrossPortCredentialRedirect(t *testing.T) {
	var targetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled.Store(true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")

	resp, err := fileCompanionDirectClient(5 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want the unforwarded redirect", resp.StatusCode)
	}
	if targetCalled.Load() {
		t.Fatal("credential-bearing request followed a redirect to another service on the same host")
	}
}

func TestFileCompanionDirectClientAllowsSameOriginRedirect(t *testing.T) {
	var finalAuth atomic.Value
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		finalAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	req, err := http.NewRequest(http.MethodGet, server.URL+"/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")

	resp, err := fileCompanionDirectClient(5 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || finalAuth.Load() != "Bearer secret" {
		t.Fatalf("same-origin redirect status=%d auth=%q", resp.StatusCode, finalAuth.Load())
	}
}
