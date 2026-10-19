package desktoppool

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotSleepCases(t *testing.T) {
	t.Run("memory_turn_does_not_wake", func(t *testing.T) {
		for _, op := range []string{WakeMemory, WakeSendToUser, WakeProgress, WakeMessage, WakeSchedule} {
			if TurnWakes(op, true) {
				t.Fatalf("%s started a desktop", op)
			}
		}
		if TurnWakes(WakeProgress, true) {
			t.Fatal("progress copied on the run record started a desktop")
		}
		for _, op := range []string{WakeFile, WakeBash, WakeSession, WakeScreenshot, WakeScreen} {
			if !TurnWakes(op, true) {
				t.Fatalf("%s did not start a desktop", op)
			}
		}

		var paths []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			switch r.URL.Path {
			case "/v1/desktops/ensure":
				_, _ = w.Write([]byte(`{"status":"running"}`))
			case "/v1/desktops/file":
				_, _ = w.Write([]byte(`{"output":"ok"}`))
			case "/v1/desktops/bash":
				_, _ = w.Write([]byte(`{"output":"hi"}`))
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
		if _, err := pool.RunFile(context.Background(), "tenant-a", "alice", "read", "/home/desktop/a.txt", "", "", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.RunBash(context.Background(), "tenant-a", "alice", "echo hi"); err != nil {
			t.Fatal(err)
		}
		if len(paths) != 4 || paths[0] != "/v1/desktops/ensure" || paths[1] != "/v1/desktops/file" || paths[2] != "/v1/desktops/ensure" || paths[3] != "/v1/desktops/bash" {
			t.Fatalf("paths = %v", paths)
		}

		t0 := time.Date(2026, 10, 18, 1, 0, 0, 0, time.UTC)
		var n int
		pool.Now = func() time.Time {
			n++
			if n%2 == 1 {
				return t0
			}
			return t0.Add(WarmStartBudget + time.Nanosecond)
		}
		err = pool.Ensure(context.Background(), "tenant-a", "alice")
		wake, ok := err.(WakeError)
		if !ok || wake.UserBubble() || !wake.Retryable() || !IsDesktopWake(err) || !strings.Contains(err.Error(), "再试一次") {
			t.Fatalf("wake err = %v", err)
		}
		if strings.Contains(err.Error(), "send_to_user") {
			t.Fatalf("wake error is a user sentence: %v", err)
		}
		n = 0
		pool.Now = func() time.Time {
			n++
			if n%2 == 1 {
				return t0
			}
			return t0.Add(WarmStartBudget)
		}
		if err := pool.Ensure(context.Background(), "tenant-a", "alice"); err != nil {
			t.Fatal(err)
		}
	})
}
