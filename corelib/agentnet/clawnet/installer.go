package clawnet

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	agentnet "github.com/RapidAI/CodeClaw/corelib/agentnet"
)

// LocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
func LocalBinaryName() string {
	return agentnet.LocalBinaryName()
}

// installDir returns the expected install directory for the anet binary.
func installDir() (string, error) {
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "anet"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".anet"), nil
}

// ManualBinaryPath checks if the user has an anet binary in the install dir.
func ManualBinaryPath() (string, bool) {
	return agentnet.ManualBinaryPath()
}

// Download installs the anet binary using the official installer.
// Delegates to the parent agentnet package which uses the official install scripts.
func Download(emitProgress func(stage string, pct int, msg string)) (string, error) {
	return agentnet.Download(emitProgress)
}

// FindBinary locates the anet executable.
// Search order: install dir → PATH.
func FindBinary() string {
	binName := LocalBinaryName()
	if dir, err := installDir(); err == nil {
		p := filepath.Join(dir, binName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("anet"); err == nil {
		return p
	}
	return ""
}
