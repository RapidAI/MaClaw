package cua

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The live tests need MACLAW_CUA_SOCKET pointing at a running
// `cua-driver serve`; without one they skip. A failure in them is a
// protocol break, not an environment quirk.

func liveDriver(t *testing.T) *Driver {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := OpenDefault(ctx)
	if err != nil {
		t.Skipf("no live driver: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func TestOpenDefaultAgainstLiveDriver(t *testing.T) {
	d := liveDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	health, err := d.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got, _ := health["overall"].(string); got != "ok" {
		t.Fatalf("overall = %q, want ok: %v", got, health)
	}
}

// TestListWindowsAgainstServe proves the wire dialect: the tool name and
// its arguments ride beside "method", and the answer's content[] is
// unwrapped for the caller.
func TestListWindowsAgainstServe(t *testing.T) {
	d := liveDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	wins, err := d.Call(ctx, "list_windows", map[string]any{})
	if err != nil {
		t.Fatalf("list_windows: %v", err)
	}
	if list, _ := wins["windows"].([]any); len(list) == 0 {
		t.Fatalf("list_windows returned no rows: %v", wins)
	}
}

// TestSnapshotAndVerifyLiveDriver reads a real window's tree and then
// asserts something about it, which is the loop the step backend runs.
func TestSnapshotAndVerifyLiveDriver(t *testing.T) {
	d := liveDriver(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	pid, wid := firstExplorerWindow(t, d, ctx)
	if wid == 0 {
		t.Skip("no explorer window to read")
	}
	snap, err := d.Snapshot(ctx, pid, wid, "", 40)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap.Elements) == 0 {
		t.Fatalf("snapshot of pid %d window %d returned no elements", pid, wid)
	}
	t.Logf("snapshot %s: %d of %d rows, complete=%v", snap.ID, snap.Returned, snap.Total, snap.Complete)
	if snap.Elements[0].Token == "" {
		t.Fatal("an element row must carry its element_token")
	}
	// The window's own title row is what a window-level check can assert.
	yes := true
	v, err := d.Verify(ctx, pid, wid, []Expect{{Window: &WindowExpect{Exists: &yes}}}, 1, 4000)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !v.Satisfied() {
		t.Fatalf("window exists should be satisfied: %s", VerifyText(v))
	}
	v, err = d.Verify(ctx, pid, wid, []Expect{{Window: &WindowExpect{Exists: ptr(false)}}}, 1, 1500)
	if err != nil {
		t.Fatalf("verify absent: %v", err)
	}
	if v.Status != "unsatisfied" {
		t.Fatalf("an open window is not absent: %s", VerifyText(v))
	}
}

func ptr(b bool) *bool { return &b }

// firstExplorerWindow returns the pid and window id of a plain Win32
// window the driver can walk: explorer's folder window is one.
func firstExplorerWindow(t *testing.T, d *Driver, ctx context.Context) (int, int) {
	t.Helper()
	out, err := d.Call(ctx, "list_windows", map[string]any{})
	if err != nil {
		t.Fatalf("list_windows: %v", err)
	}
	wins, _ := out["windows"].([]any)
	for _, raw := range wins {
		w, _ := raw.(map[string]any)
		if app, _ := w["app_name"].(string); app != "explorer.exe" {
			continue
		}
		if on, _ := w["is_on_screen"].(bool); !on {
			continue
		}
		title, _ := w["title"].(string)
		if title == "" || title == "Program Manager" {
			continue
		}
		pid := numberField(w, "pid")
		wid := numberField(w, "window_id")
		return int(pid), int(wid)
	}
	return 0, 0
}

// TestErrNoDriverWithoutSocket guards the unset-variable path.
func TestErrNoDriverWithoutSocket(t *testing.T) {
	t.Setenv(EnvSocket, "")
	if _, err := OpenDefault(context.Background()); err != ErrNoDriver {
		t.Fatalf("got %v, want ErrNoDriver", err)
	}
}

var _ = os.Getpid

// numberField reads an integer field the JSON decoder may have produced as
// float64 (encoding/json) or json.Number (UseNumber).
func numberField(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case json.Number:
		f, _ := v.Float64()
		return f
	}
	return 0
}
