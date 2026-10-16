package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setLinkedVersionForTest overrides the version injected at build time for the
// duration of one test.
func setLinkedVersionForTest(t *testing.T, value string) {
	t.Helper()
	previous := version
	version = value
	t.Cleanup(func() { version = previous })
}

func newPendingUpdateTestApp(t *testing.T) *App {
	t.Helper()
	return &App{testHomeDir: t.TempDir()}
}

func TestRecordPendingUpdateRequiresTargetVersion(t *testing.T) {
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("", filepath.Join(t.TempDir(), "MaClaw-Setup.exe"))
	if _, err := os.Stat(app.pendingUpdatePath()); err == nil {
		t.Fatal("recordPendingUpdate with an empty target must not arm a notice that could never be cleared")
	}
	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil", notice)
	}
}

func TestPendingUpdateNoticeReportsWhenTargetNotReached(t *testing.T) {
	setLinkedVersionForTest(t, "1.2.3")
	app := newPendingUpdateTestApp(t)
	installer := filepath.Join(t.TempDir(), "MaClaw-Setup.exe")
	if err := os.WriteFile(installer, []byte("installer"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app.recordPendingUpdate("2.0.0", installer)

	notice := app.GetPendingUpdateNotice()
	if notice == nil {
		t.Fatal("GetPendingUpdateNotice() = nil, want a notice for the update that never landed")
	}
	if notice.TargetVersion != "2.0.0" {
		t.Errorf("TargetVersion = %q, want %q", notice.TargetVersion, "2.0.0")
	}
	if notice.InstallerPath != installer {
		t.Errorf("InstallerPath = %q, want %q", notice.InstallerPath, installer)
	}
	if !notice.InstallerStillPresent {
		t.Error("InstallerStillPresent = false, want true while the installer is still on disk")
	}
}

func TestPendingUpdateNoticeFlagsMissingInstaller(t *testing.T) {
	setLinkedVersionForTest(t, "1.2.3")
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("2.0.0", filepath.Join(t.TempDir(), "gone-Setup.exe"))

	notice := app.GetPendingUpdateNotice()
	if notice == nil {
		t.Fatal("GetPendingUpdateNotice() = nil, want a notice")
	}
	if notice.InstallerStillPresent {
		t.Error("InstallerStillPresent = true, want false when the installer was already removed")
	}
}

func TestPendingUpdateClearsOnceTargetReached(t *testing.T) {
	setLinkedVersionForTest(t, "2.0.0")
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("2.0.0", filepath.Join(t.TempDir(), "MaClaw-Setup.exe"))

	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil once the target version is running", notice)
	}
	if _, err := os.Stat(app.pendingUpdatePath()); err == nil {
		t.Error("pending record must be deleted when the update is confirmed")
	}
}

func TestPendingUpdateClearsForNewerRunningVersion(t *testing.T) {
	setLinkedVersionForTest(t, "2.1.0")
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("2.0.0", filepath.Join(t.TempDir(), "MaClaw-Setup.exe"))

	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil when a newer version is running", notice)
	}
}

func TestPendingUpdateExpires(t *testing.T) {
	setLinkedVersionForTest(t, "1.2.3")
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("2.0.0", filepath.Join(t.TempDir(), "MaClaw-Setup.exe"))
	path := app.pendingUpdatePath()
	// Rewind the launch time past the retention window.
	stale, err := json.Marshal(pendingUpdate{
		TargetVersion: "2.0.0",
		InstallerPath: filepath.Join(t.TempDir(), "MaClaw-Setup.exe"),
		LaunchedAt:    time.Now().Add(-2 * pendingUpdateMaxAge),
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil for an expired record", notice)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("expired pending record must be deleted")
	}
}

func TestPendingUpdateDropsUnreadableRecord(t *testing.T) {
	setLinkedVersionForTest(t, "1.2.3")
	app := newPendingUpdateTestApp(t)
	path := app.pendingUpdatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil for an unreadable record", notice)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("unreadable pending record must be deleted")
	}
}

func TestDismissPendingUpdate(t *testing.T) {
	setLinkedVersionForTest(t, "1.2.3")
	app := newPendingUpdateTestApp(t)
	app.recordPendingUpdate("2.0.0", filepath.Join(t.TempDir(), "MaClaw-Setup.exe"))
	if _, err := os.Stat(app.pendingUpdatePath()); err != nil {
		t.Fatalf("expected a pending record: %v", err)
	}
	app.DismissPendingUpdate()
	if notice := app.GetPendingUpdateNotice(); notice != nil {
		t.Fatalf("GetPendingUpdateNotice() = %+v, want nil after dismissal", notice)
	}
}
