package desktoppool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestRunHTTPDoesNotWakeAndKeepsASlowWakeTyped(t *testing.T) {
	var paths []string
	waking := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/v1/desktops/ensure" {
			t.Errorf("fetch called ensure")
		}
		if waking {
			waking = false
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"ok":false,"code":"DESKTOP_WAKING","message":"桌面正在唤醒，再试一次。"}`))
			return
		}
		_, _ = w.Write([]byte(`{"body":"daily paper","status":200,"content_type":"text/html"}`))
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
	_, _, _, err = pool.RunHTTP(context.Background(), "tenant-a", "alice", "https://example.com/papers")
	var wake WakeError
	if !errors.As(err, &wake) || !IsDesktopWake(err) {
		t.Fatalf("slow wake err=%v", err)
	}
	body, status, kind, err := pool.RunHTTP(context.Background(), "tenant-a", "alice", "https://example.com/papers")
	if err != nil || body != "daily paper" || status != 200 || kind != "text/html" {
		t.Fatalf("body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	if strings.Join(paths, " ") != "/v1/desktops/http /v1/desktops/http" {
		t.Fatalf("paths=%v", paths)
	}
	asleep := classifyDesktopOp(desktopbot.AsleepError{})
	if !desktopbot.IsDesktopAsleep(asleep) {
		t.Fatal("asleep was flattened")
	}
}
