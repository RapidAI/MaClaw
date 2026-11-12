package desktoppool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunHTTPWakesWithBotDesktopBeforeCurl(t *testing.T) {
	var paths []string
	var reasons []string
	httpCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/desktops/ensure":
			var in struct {
				WakeReason string `json:"wake_reason"`
			}
			_ = json.Unmarshal(raw, &in)
			reasons = append(reasons, in.WakeReason)
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case "/v1/desktops/http":
			httpCalls++
			if httpCalls == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"ok":false,"code":"DESKTOP_ASLEEP","error":"desktop_asleep"}`))
				return
			}
			_, _ = w.Write([]byte(`{"body":"daily paper","status":200,"content_type":"text/html"}`))
		default:
			http.NotFound(w, r)
		}
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
	body, status, kind, err := pool.RunHTTP(context.Background(), "tenant-a", "alice", "https://example.com/papers")
	if err != nil || body != "daily paper" || status != 200 || kind != "text/html" {
		t.Fatalf("body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	if len(paths) != 4 || paths[0] != "/v1/desktops/ensure" || paths[1] != "/v1/desktops/http" || paths[2] != "/v1/desktops/ensure" || paths[3] != "/v1/desktops/http" {
		t.Fatalf("paths=%v", paths)
	}
	for _, reason := range reasons {
		if reason != WakeReasonBot {
			t.Fatalf("wake reason %q, want %s", reason, WakeReasonBot)
		}
	}
	if strings.Contains(strings.Join(paths, " "), "bash") {
		t.Fatal("http woke by execing a shell")
	}
}
