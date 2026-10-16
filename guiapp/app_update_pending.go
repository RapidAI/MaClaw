package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pendingUpdateMaxAge bounds how long a recorded launch stays relevant. Past
// this the record is dropped on the next read: the user has long since moved
// on, and the installer left in Downloads has probably been superseded.
const pendingUpdateMaxAge = 7 * 24 * time.Hour

// pendingUpdate is written immediately before the app quits to hand control to
// the installer.
//
// A silent install has no channel to report failure: the app has already quit
// and the installer shows nothing. Without this record a failed update is
// indistinguishable from a successful one — the app simply comes back on the
// old version and nothing says why. The record lets the next start notice that
// the target version never arrived.
type pendingUpdate struct {
	TargetVersion string    `json:"target_version"`
	InstallerPath string    `json:"installer_path"`
	LaunchedAt    time.Time `json:"launched_at"`
}

// PendingUpdateNotice describes a silent update that did not take effect.
type PendingUpdateNotice struct {
	TargetVersion string `json:"target_version"`
	InstallerPath string `json:"installer_path"`
	// InstallerStillPresent lets the UI offer "reveal in folder" only while the
	// installer is actually still on disk.
	InstallerStillPresent bool `json:"installer_still_present"`
}

// pendingUpdatePath is the app-owned state file holding the last launch record.
// It lives next to the rest of the app data (not the OS temp dir): it must
// survive a reboot to be of any use.
func (a *App) pendingUpdatePath() string {
	if a == nil {
		return ""
	}
	base := strings.TrimSpace(a.getMaclawBaseDir())
	if base == "" {
		return ""
	}
	return filepath.Join(base, "update", "pending-update.json")
}

// recordPendingUpdate arms the "update did not apply" notice.
//
// Best effort by design: failing to record only costs the notice, never the
// update itself, so every failure here is logged and swallowed.
func (a *App) recordPendingUpdate(targetVersion, installerPath string) {
	if a == nil {
		return
	}
	targetVersion = strings.TrimSpace(targetVersion)
	if targetVersion == "" {
		// Without a target version the next start cannot tell whether the
		// update landed, so the record could never clear itself and would
		// eventually be reported as a failure that did not happen.
		a.log("[update-install] no target version; not arming the pending-update notice")
		return
	}
	path := a.pendingUpdatePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.log(fmt.Sprintf("[update-install] cannot create update state directory: %v", err))
		return
	}
	data, err := json.Marshal(pendingUpdate{
		TargetVersion: targetVersion,
		InstallerPath: installerPath,
		LaunchedAt:    time.Now(),
	})
	if err != nil {
		a.log(fmt.Sprintf("[update-install] cannot encode pending-update record: %v", err))
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		a.log(fmt.Sprintf("[update-install] cannot write pending-update record: %v", err))
		return
	}
	a.log(fmt.Sprintf("[update-install] armed pending-update notice for %s", targetVersion))
}

// GetPendingUpdateNotice reports a silent update that never took effect, or nil
// when there is nothing to report.
//
// Reading is also how a successful update clears itself: once the running
// version reaches the recorded target (or newer), the record is deleted. That
// happens on the first start after the installer relaunches the app.
func (a *App) GetPendingUpdateNotice() *PendingUpdateNotice {
	record, ok := a.readPendingUpdate()
	if !ok {
		return nil
	}
	notice := &PendingUpdateNotice{
		TargetVersion: record.TargetVersion,
		InstallerPath: record.InstallerPath,
	}
	if record.InstallerPath != "" {
		if info, err := os.Stat(record.InstallerPath); err == nil && !info.IsDir() {
			notice.InstallerStillPresent = true
		}
	}
	return notice
}

// readPendingUpdate loads the record and applies the retention rules.
func (a *App) readPendingUpdate() (pendingUpdate, bool) {
	var empty pendingUpdate
	if a == nil {
		return empty, false
	}
	path := a.pendingUpdatePath()
	if path == "" {
		return empty, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			a.log(fmt.Sprintf("[update-install] cannot read pending-update record: %v", err))
		}
		return empty, false
	}
	var record pendingUpdate
	if err := json.Unmarshal(data, &record); err != nil || strings.TrimSpace(record.TargetVersion) == "" {
		// Unreadable or incomplete: drop it rather than report an update that
		// may never have been attempted.
		a.log(fmt.Sprintf("[update-install] discarding unusable pending-update record: %v", err))
		_ = os.Remove(path)
		return empty, false
	}
	if !record.LaunchedAt.IsZero() && time.Since(record.LaunchedAt) > pendingUpdateMaxAge {
		a.log(fmt.Sprintf("[update-install] pending-update record for %s expired", record.TargetVersion))
		_ = os.Remove(path)
		return empty, false
	}
	if current, ok := linkedReleaseVersion(); ok && compareVersions(current, record.TargetVersion) >= 0 {
		a.log(fmt.Sprintf("[update-install] update to %s confirmed (running %s); cleared pending record", record.TargetVersion, current))
		_ = os.Remove(path)
		return empty, false
	}
	return record, true
}

// DismissPendingUpdate drops the notice once the user has seen it, so the same
// failed update is not reported again on every start.
func (a *App) DismissPendingUpdate() {
	if a == nil {
		return
	}
	path := a.pendingUpdatePath()
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		a.log(fmt.Sprintf("[update-install] cannot clear pending-update record: %v", err))
	}
}
