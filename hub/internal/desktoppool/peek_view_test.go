package desktoppool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPeekDesktopViewReturnsPerBotURLWithoutStarting(t *testing.T) {
	var paths []string
	var users []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		var body struct {
			UserID string `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		users = append(users, body.UserID)
		switch r.URL.Path {
		case "/v1/desktops/view":
			_, _ = w.Write([]byte(`{"display":":38","novnc_url":"http://dockerd.example/vnc.html?u=` + body.UserID + `"}`))
		case "/v1/desktops/session", "/v1/desktops/ensure":
			t.Errorf("peek must not call %s", r.URL.Path)
			http.Error(w, "started", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	pool := New(&memSettings{}, nil)
	ctx := context.Background()
	server, err := pool.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: srv.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: server.ID}); err != nil {
		t.Fatal(err)
	}

	u1 := "alice::desk::bot_1"
	u2 := "alice::desk::bot_2"
	one, err := pool.PeekDesktopView(ctx, "tenant-a", u1)
	if err != nil || one != "http://dockerd.example/vnc.html?u="+u1 {
		t.Fatalf("bot_1 view %q err=%v", one, err)
	}
	two, err := pool.PeekDesktopView(ctx, "tenant-a", u2)
	if err != nil || two != "http://dockerd.example/vnc.html?u="+u2 || two == one {
		t.Fatalf("bot_2 view %q err=%v", two, err)
	}
	for _, path := range paths {
		if strings.Contains(path, "/session") || strings.Contains(path, "/ensure") {
			t.Fatalf("peek started the desktop: %v", paths)
		}
	}
	if strings.Join(users, ",") != u1+","+u2 {
		t.Fatalf("users=%v", users)
	}
}

func TestPeekDesktopViewMissingOrAsleepIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/desktops/view" {
			_, _ = w.Write([]byte(`{"display":"","novnc_url":""}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	pool := New(&memSettings{}, nil)
	ctx := context.Background()
	server, err := pool.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: srv.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: server.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := pool.PeekDesktopView(ctx, "tenant-a", "alice")
	if err != nil || got != "" {
		t.Fatalf("asleep view %q err=%v", got, err)
	}

	// An older desktopd without /view must not surface as a hard error, and
	// must not be retried as a session start.
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(missing.Close)
	pool2 := New(&memSettings{}, nil)
	server2, err := pool2.CreateServer(ctx, "tenant-a", Server{Name: "srv", BaseURL: missing.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool2.CreateAssignment(ctx, "tenant-a", Assignment{Scope: ScopeGlobal, ServerID: server2.ID}); err != nil {
		t.Fatal(err)
	}
	got, err = pool2.PeekDesktopView(ctx, "tenant-a", "alice")
	if err != nil || got != "" {
		t.Fatalf("missing route %q err=%v", got, err)
	}
}
