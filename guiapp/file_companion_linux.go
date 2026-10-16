//go:build linux

package guiapp

import (
	"os"
	"path/filepath"
)

func registerFileCompanionLinuxDesktop() error {
	dir := fileCompanionApplicationsDir()
	if dir == "" {
		logFileCompanion("linux desktop dir unavailable")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		logFileCompanion("linux desktop exe: %v", err)
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != "" {
		exe = resolved
	}
	id, displayName := fileCompanionDesktopBrand()
	content := fileCompanionDesktopEntry(exe, displayName)
	changed, err := writeFileCompanionDesktopFile(dir, id, content)
	if err != nil {
		logFileCompanion("linux desktop write: %v", err)
		return nil
	}
	if changed {
		logFileCompanion("linux desktop registered id=%s", id)
	}
	return nil
}

// UnregisterFileCompanionLinuxDesktop removes only this brand's desktop file.
func (a *App) UnregisterFileCompanionLinuxDesktop() error {
	dir := fileCompanionApplicationsDir()
	if dir == "" {
		return nil
	}
	id, _ := fileCompanionDesktopBrand()
	if err := removeFileCompanionDesktopFile(dir, id); err != nil {
		return err
	}
	logFileCompanion("linux desktop removed id=%s", id)
	return nil
}
