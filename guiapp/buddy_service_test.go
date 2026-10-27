package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

func newBuddyTestApp(t *testing.T) *App {
	t.Helper()
	app := &App{testHomeDir: t.TempDir()}
	return app
}

func TestBuddyConfigDefaultsEnabledAndRoundTrips(t *testing.T) {
	app := newBuddyTestApp(t)

	cfg := app.loadBuddyConfig()
	if !cfg.Enabled {
		t.Fatal("fresh install must default to enabled (plug and play)")
	}

	want := buddyConfig{Enabled: false, Port: "COM7"}
	if err := app.saveBuddyConfig(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.GetDataDir(), "buddy.json")); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	got := app.loadBuddyConfig()
	if got != want {
		t.Fatalf("round trip mismatch: %+v != %+v", got, want)
	}
}

func TestBuddyPushSnapshotStoresPayload(t *testing.T) {
	app := newBuddyTestApp(t)
	snap := protocol.Snapshot{
		Counts:    protocol.Counts{Total: 2, InProgress: 1, Done: 1},
		Current:   []protocol.TaskItem{{ID: "a", Title: "整理会议纪要", Progress: -1}},
		UpdatedAt: time.Now().UnixMilli(),
	}
	raw, _ := json.Marshal(snap)
	if err := app.BuddyPushSnapshot(string(raw)); err != nil {
		t.Fatalf("push: %v", err)
	}
	if app.buddyLastSnapshot != string(raw) {
		t.Fatal("snapshot payload was not cached for reconnect")
	}

	status := app.BuddyGetStatus()
	if status.SnapshotAgeS < 0 || status.SnapshotAgeS > 5 {
		t.Fatalf("unexpected snapshot age: %d", status.SnapshotAgeS)
	}
}

func TestBuddyPushSnapshotRejectsInvalidJSON(t *testing.T) {
	app := newBuddyTestApp(t)
	if err := app.BuddyPushSnapshot("{not json"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestBuddyGetStatusWithoutRunner(t *testing.T) {
	app := newBuddyTestApp(t)
	app.buddyCfg = buddyConfig{Enabled: false, Port: "COM9"}
	status := app.BuddyGetStatus()
	if status.Enabled || status.Connected || status.Port != "COM9" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestBuddyStopPersistsDisabled(t *testing.T) {
	app := newBuddyTestApp(t)
	if err := app.BuddyStop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if app.loadBuddyConfig().Enabled {
		t.Fatal("BuddyStop must persist enabled=false")
	}
}
