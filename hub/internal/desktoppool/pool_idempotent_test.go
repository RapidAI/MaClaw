package desktoppool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

func newRetryPool(t *testing.T, rawURL string) *Pool {
	t.Helper()
	pool := New(&memSettings{}, memDir{})
	view, err := pool.CreateServer(context.Background(), "tenant-a", Server{
		Name: "blink", BaseURL: rawURL, AccessToken: "blink-token", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	return pool
}

// A service that answers 5xx, or does not answer at all, is asked again for
// a session. The session call is create-or-get for one (tenant, user), so a
// second try cannot double a desktop — and an overloaded service no longer
// fails a command that has not opened one yet.
func TestOpenDesktopRetriesAServiceThatNeverAnswered(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/desktops/ensure" {
			_, _ = w.Write([]byte(`{"status":"running"}`))
			return
		}
		if r.URL.Path != "/v1/desktops/session" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream gone"}`))
			return
		}
		_, _ = w.Write([]byte(`{"cdp_url":"http://desktop.example:19020","display":":20","novnc_url":"http://desktop.example:6080/vnc.html"}`))
	}))
	defer server.Close()
	pool := newRetryPool(t, server.URL)

	session, err := pool.OpenDesktop(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if session.CDP != "http://desktop.example:19020" {
		t.Fatalf("cdp = %q", session.CDP)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("session calls = %d, want 3", got)
	}
}

// A status the service chose is returned at once. Retrying a rejection is
// how a misconfigured service turns into a slow one.
func TestOpenDesktopDoesNotRetryARejection(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"image unknown"}`))
	}))
	defer server.Close()
	pool := newRetryPool(t, server.URL)

	if _, err := pool.OpenDesktop(context.Background(), "tenant-a", "alice"); err == nil {
		t.Fatal("a 400 must fail the open")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("session calls = %d, want 1", got)
	}
}

// A service that never answers at all is tried the bounded number of times,
// then the failure stands.
func TestOpenDesktopStopsAfterTheLastTry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()
	pool := newRetryPool(t, url)

	_, err := pool.OpenDesktop(context.Background(), "tenant-a", "alice")
	if err == nil || !strings.Contains(err.Error(), "docker service") && !strings.Contains(err.Error(), "desktop service") {
		t.Fatalf("err = %v", err)
	}
}

// The retry rule itself: nothing the service decided is retried, and a
// status it did choose is not.
func TestDesktopPoolCallWorthRetry(t *testing.T) {
	if desktopPoolCallWorthRetry(nil) {
		t.Fatal("no error is not worth a retry")
	}
	if !desktopPoolCallWorthRetry(errServiceProbe("connection refused")) {
		t.Fatal("a transport error never reached a server that could reject it")
	}
	if !desktopPoolCallWorthRetry(upstream.NewStatusError(ErrService, http.StatusBadGateway, []byte(`{"error":"upstream gone"}`))) {
		t.Fatal("a 5xx is the proxy failing, not the service deciding")
	}
	if desktopPoolCallWorthRetry(upstream.NewStatusError(ErrService, http.StatusBadRequest, []byte(`{"error":"image unknown"}`))) {
		t.Fatal("a 4xx is an answer the service meant")
	}
	if desktopPoolCallWorthRetry(upstream.NewStatusError(ErrService, http.StatusNotFound, []byte(`{}`))) {
		t.Fatal("a 404 is an answer the service meant")
	}
}

func errServiceProbe(message string) error {
	return &serviceErrProbe{message: message}
}

type serviceErrProbe struct{ message string }

func (e *serviceErrProbe) Error() string { return e.message }
