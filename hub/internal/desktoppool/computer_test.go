package desktoppool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComputerOpsCallDesktopdWithAssignedSpec(t *testing.T) {
	var gotPath string
	var got map[string]any
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		switch status {
		case http.StatusConflict:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"desktop is busy"}`))
		case http.StatusNotFound:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"desktop does not exist"}`))
		default:
			_, _ = w.Write([]byte(`{"action":"reset","container":"c","status":"running","backup":"img:prev"}`))
		}
	}))
	defer srv.Close()
	pool := assignedPool(t, srv.URL)
	ctx := context.Background()
	out, err := pool.ResetDesktop(ctx, "tenant-a", "alice", true)
	if err != nil || gotPath != "/v1/desktops/reset" || out.Backup != "img:prev" {
		t.Fatalf("out=%+v err=%v path=%s", out, err, gotPath)
	}
	if got["user_id"] != "alice" || got["memory"] != "3g" || got["force"] != true {
		t.Fatalf("body=%v", got)
	}
	if _, err := pool.RestartDesktop(ctx, "tenant-a", "alice", false); err != nil || gotPath != "/v1/desktops/restart" {
		t.Fatalf("restart err=%v path=%s", err, gotPath)
	}
	status = http.StatusConflict
	if _, err := pool.RestartDesktop(ctx, "tenant-a", "alice", false); !errors.Is(err, ErrComputerBusy) {
		t.Fatalf("busy err=%v", err)
	}
	status = http.StatusNotFound
	if _, err := pool.RestartDesktop(ctx, "tenant-a", "alice", false); !errors.Is(err, ErrComputerMissing) {
		t.Fatalf("missing err=%v", err)
	}
	status = http.StatusOK
	if _, err := pool.SetTenantComputerPolicy(ctx, "tenant-a", ComputerPolicy{ResetDisabled: true}); err != nil {
		t.Fatal(err)
	}
	gotPath = ""
	if _, err := pool.ResetDesktop(ctx, "tenant-a", "alice", false); !errors.Is(err, ErrComputerResetDisabled) || gotPath != "" {
		t.Fatalf("disabled reset err=%v path=%s", err, gotPath)
	}
	if _, err := pool.RestartDesktop(ctx, "tenant-a", "alice", false); err != nil {
		t.Fatalf("restart must stay allowed: %v", err)
	}
}
