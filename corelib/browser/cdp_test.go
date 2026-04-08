package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoverTargetsIncludesHTTPStatusAndBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not cdp"))
	}))
	defer ts.Close()

	_, err := DiscoverTargets(ts.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "404") || !strings.Contains(msg, "not cdp") {
		t.Fatalf("DiscoverTargets error = %q", msg)
	}
}

func TestDiscoverTargetsIncludesBodyOnParseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>bad json</html>"))
	}))
	defer ts.Close()

	_, err := DiscoverTargets(ts.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "parse targets") || !strings.Contains(msg, "bad json") {
		t.Fatalf("DiscoverTargets error = %q", msg)
	}
}
