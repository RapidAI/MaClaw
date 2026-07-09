//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const desktopFileName = "tigerproxy.desktop"

func autoStartSupported() bool {
	return true
}

func isAutoStartEnabled() (bool, error) {
	path := autostartDesktopPath()
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func setAutoStartEnabled(enabled bool) error {
	path := autostartDesktopPath()
	if !enabled {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	content := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=TigerProxy
Exec=%s --hidden
Icon=tigerproxy
Terminal=false
X-GNOME-Autostart-enabled=true
Comment=CodeGen protocol proxy
`, exe)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func autostartDesktopPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config")
	}
	return filepath.Join(configDir, "autostart", desktopFileName)
}
