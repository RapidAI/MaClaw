package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

func TestProductionAdapterDropsIdleStoppedRememberedURL(t *testing.T) {
	var opens int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.URL.Path == "/v1/desktops/open" {
			opens++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"novnc_url":"http://hub/stale/vnc.html","desktop_state":"display_stopped"}`))
	}))
	defer server.Close()
	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	view, err := pool.CreateServer(context.Background(), "tenant-a", desktoppool.Server{
		Name: "srv", BaseURL: server.URL, AccessToken: "tok", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", desktoppool.Assignment{Scope: desktoppool.ScopeUser, TargetID: "alice", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"bots": []any{map[string]any{
			"id": "bot_alice", "name": "duty", "instance_id": "inst_alice", "owner_user_id": "alice",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mem := &botSettingsMem{value: string(raw)}
	svc := botmgmt.NewService(mem)
	svc.Desktop = botDesktopControl{pool: pool}
	svc.NoteDesktopView("tenant-a", "alice", "http://hub/stale/vnc.html")
	url, _, state, err := svc.DesktopPicture(context.Background(), "tenant-a", "alice", "bot_alice", false)
	if err != nil || url != "" || state != botmgmt.DesktopStateDisplayStopped || opens != 0 {
		t.Fatalf("url=%q state=%q opens=%d err=%v", url, state, opens, err)
	}
}

func TestProductionAdapterDoesNotWakeOnUnavailable(t *testing.T) {
	var opens int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.URL.Path == "/v1/desktops/open" {
			opens++
			http.Error(w, "opened", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"novnc_url":"","desktop_state":"unavailable"}`))
	}))
	defer server.Close()
	pool := desktoppool.New(newDesktopSettingsMem(), nil)
	view, err := pool.CreateServer(context.Background(), "tenant-a", desktoppool.Server{
		Name: "srv", BaseURL: server.URL, AccessToken: "tok", Memory: "3g", CPUs: "1.5", ShmSize: "1g",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.CreateAssignment(context.Background(), "tenant-a", desktoppool.Assignment{Scope: desktoppool.ScopeUser, TargetID: "alice", ServerID: view.ID}); err != nil {
		t.Fatal(err)
	}
	pinKey := "tenant-a\x00alice"
	raw, err := json.Marshal(map[string]any{
		"bots": []any{map[string]any{
			"id": "bot_alice", "name": "duty", "instance_id": "inst_alice", "owner_user_id": "alice",
		}},
		"desktop": map[string]any{
			"pins": map[string]any{pinKey: map[string]any{"held_by_bot": "bot_alice", "awaiting": true}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := botmgmt.NewService(&botSettingsMem{value: string(raw)})
	ctrl := botDesktopControl{pool: pool}
	svc.Desktop = ctrl
	svc.NoteDesktopView("tenant-a", "alice", "http://hub/remembered/vnc.html")
	if _, err := ctrl.PeekDesktopView(context.Background(), "tenant-a", "alice"); !errors.Is(err, desktoppool.ErrDesktopViewUnavailable) {
		t.Fatalf("peek %v", err)
	}
	url, _, state, err := svc.DesktopPicture(context.Background(), "tenant-a", "alice", "bot_alice", false)
	if err != nil || url != "" || state != botmgmt.DesktopStateUnavailable || opens != 0 {
		t.Fatalf("url=%q state=%q opens=%d err=%v", url, state, opens, err)
	}
}
