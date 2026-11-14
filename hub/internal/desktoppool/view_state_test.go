package desktoppool

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDesktopViewStateReadsTheTypedPicture(t *testing.T) {
	answers := []string{
		`{"novnc_url":"http://hub/stale/vnc.html","desktop_state":"display_stopped"}`,
		`{"novnc_url":"http://hub/stale/vnc.html","desktop_state":"asleep"}`,
		`{"novnc_url":"http://hub/live/vnc.html","desktop_state":"running"}`,
		`{"novnc_url":""}`,
		`{"ok":false,"code":"DESKTOP_ASLEEP","message":"桌面在休眠","error":"桌面在休眠"}`,
	}
	var n int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.URL.Path != "/v1/desktops/view" {
			t.Errorf("path %s", r.URL.Path)
		}
		body := answers[n]
		n++
		if n == len(answers) {
			w.WriteHeader(http.StatusConflict)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()
	pool := New(&memSettings{}, memDir{})
	view, err := pool.CreateServer(context.Background(), "tenant-a", Server{
		Name: "srv", BaseURL: server.URL, AccessToken: "tok", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", Assignment{Scope: ScopeUser, TargetID: "alice", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	state, url, err := pool.DesktopViewState(context.Background(), "tenant-a", "alice")
	if err != nil || state != "display_stopped" || url != "http://hub/stale/vnc.html" {
		t.Fatalf("display stopped state=%q url=%q err=%v", state, url, err)
	}
	state, url, err = pool.DesktopViewState(context.Background(), "tenant-a", "alice")
	if err != nil || state != "asleep" || url != "http://hub/stale/vnc.html" {
		t.Fatalf("asleep state=%q url=%q err=%v", state, url, err)
	}
	state, url, err = pool.DesktopViewState(context.Background(), "tenant-a", "alice")
	if err != nil || state != "running" || url != "http://hub/live/vnc.html" {
		t.Fatalf("running state=%q url=%q err=%v", state, url, err)
	}
	state, url, err = pool.DesktopViewState(context.Background(), "tenant-a", "alice")
	if err != nil || state != "asleep" || url != "" {
		t.Fatalf("older empty state=%q url=%q err=%v", state, url, err)
	}
	state, url, err = pool.DesktopViewState(context.Background(), "tenant-a", "alice")
	if err != nil || state != "asleep" || url != "" {
		t.Fatalf("409 state=%q url=%q err=%v", state, url, err)
	}
}
