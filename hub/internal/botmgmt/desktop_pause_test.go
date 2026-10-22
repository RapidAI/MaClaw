package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pauseFixture(t *testing.T, h http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token" {
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
			return
		}
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	settings := &memSettings{}
	raw, _ := json.Marshal(record{
		BaseURL: srv.URL, AccessToken: "secret-token", AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "k", APISecret: "s"}},
		Bots:       []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants:     []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	return svc
}

func TestPauseResumeDesktopRunProxiesAndMarksActive(t *testing.T) {
	var paths []string
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	svc := pauseFixture(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/runs/run_a/pause"):
			_, _ = w.Write([]byte(`{"run_id":"run_a","paused":true,"expires_at":"` + exp + `","timeout_sec":1800}`))
		case strings.HasSuffix(r.URL.Path, "/runs/run_a/resume"):
			_, _ = w.Write([]byte(`{"run_id":"run_a","outcome":"resumed"}`))
		case strings.HasSuffix(r.URL.Path, "/runs/run_gone/pause"):
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"RUN_NOT_RUNNING","message":"run is not running"}}`))
		case strings.HasSuffix(r.URL.Path, "/runs"):
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	})
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_a"): {task: "t", status: "running", userID: "alice", botID: "bot_alice", runID: "run_a", started: time.Now()},
	}
	st, err := svc.PauseDesktopRun(context.Background(), "tenant-a", "alice", "bot_alice", "run_a", "why", 0)
	if err != nil || !st.Paused {
		t.Fatalf("pause %v %+v", err, st)
	}
	items, _ := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_alice")
	if len(items) != 1 || !items[0].Paused || items[0].PauseExpiresAt == nil {
		t.Fatalf("active not marked paused: %+v", items)
	}
	if _, err := svc.ResumeDesktopRun(context.Background(), "tenant-a", "alice", "bot_alice", "run_a"); err != nil {
		t.Fatal(err)
	}
	items, _ = svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_alice")
	if items[0].Paused {
		t.Fatal("still paused after resume")
	}
	if _, err := svc.PauseDesktopRun(context.Background(), "tenant-a", "alice", "bot_alice", "run_gone", "", 0); !errors.Is(err, ErrRunNotRunning) {
		t.Fatalf("ended run: %v", err)
	}
	if _, err := svc.PauseDesktopRun(context.Background(), "tenant-a", "bob", "bot_alice", "run_a", "", 0); err == nil {
		t.Fatal("another user paused alice's run")
	}
	for _, p := range paths {
		if strings.Contains(p, "pause") && !strings.Contains(p, "/instances/inst_alice/") {
			t.Fatalf("wrong instance %s", p)
		}
	}
}

func TestPauseOnOlderServerIsUnsupported(t *testing.T) {
	svc := pauseFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/runs/run_a") {
			_, _ = w.Write([]byte(`{"id":"run_a","status":"running"}`))
			return
		}
		http.NotFound(w, r)
	})
	if _, err := svc.PauseDesktopRun(context.Background(), "tenant-a", "alice", "bot_alice", "run_a", "", 0); !errors.Is(err, ErrPauseUnsupported) {
		t.Fatalf("older server: %v", err)
	}
	if _, err := svc.PauseDesktopRun(context.Background(), "tenant-a", "alice", "bot_alice", "run_missing", "", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing run: %v", err)
	}
}
