package agentnet

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BundleManager wraps the anet pack/unpack commands for .nut bundle operations.
// Per skill.md: always create task bundles through `anet pack`.
type BundleManager struct {
	anetBin string // path to anet binary
}

// NewBundleManager creates a BundleManager using the given anet binary path.
func NewBundleManager(anetBin string) *BundleManager {
	return &BundleManager{anetBin: anetBin}
}

// BundleToolStatus holds the result of anet pack/unpack availability check.
type BundleToolStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// IsInstalled checks if the anet binary supports pack/unpack.
func (n *BundleManager) IsInstalled() BundleToolStatus {
	out, err := n.runAnet("--version")
	if err != nil {
		return BundleToolStatus{Installed: false, Error: err.Error()}
	}
	return BundleToolStatus{Installed: true, Version: strings.TrimSpace(out)}
}

// Install is a no-op since pack/unpack is built into anet.
func (n *BundleManager) Install() error {
	return nil // anet pack is built-in
}

// Pack creates a .nut bundle file using `anet pack`.
func (n *BundleManager) Pack(dir, outputFile, peerID string) (string, error) {
	args := []string{"pack", dir}
	if outputFile != "" {
		args = append(args, outputFile)
	}
	if peerID != "" {
		args = append(args, "--encrypt", "--peer", peerID)
	}
	return n.runAnet(args...)
}

// Unpack extracts a .nut bundle file using `anet unpack`.
func (n *BundleManager) Unpack(nutFile, outputDir string) (string, error) {
	args := []string{"unpack", nutFile}
	if outputDir != "" {
		args = append(args, outputDir)
	}
	return n.runAnet(args...)
}

// runAnet executes an anet subcommand.
func (n *BundleManager) runAnet(args ...string) (string, error) {
	bin := n.anetBin
	if bin == "" {
		bin = "anet"
	}
	cmd := exec.Command(bin, args...)
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// BundleLocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
func BundleLocalBinaryName() string {
	return LocalBinaryName()
}

// BundleInstallDir returns the directory where anet is installed.
func BundleInstallDir() (string, error) {
	return InstallDir()
}

// BundleBinaryPath returns the expected anet binary path.
func BundleBinaryPath() string {
	dir, err := BundleInstallDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, BundleLocalBinaryName())
}

// InstallWithProgress installs anet when it is not already available.
// If anet is not installed, it installs anet.
func (n *BundleManager) InstallWithProgress(emitProgress func(stage string, pct int, msg string)) (string, error) {
	// Check if anet is already available
	bin := n.anetBin
	if bin == "" {
		if p, ok := ManualBinaryPath(); ok {
			return p, nil
		}
		if p, err := exec.LookPath("anet"); err == nil {
			return p, nil
		}
	} else {
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
	}

	// Install anet, which includes pack/unpack functionality.
	return Download(emitProgress)
}

// BundleBinaryInstalled checks if the anet binary exists.
func BundleBinaryInstalled() bool {
	if _, ok := ManualBinaryPath(); ok {
		return true
	}
	_, err := exec.LookPath("anet")
	return err == nil
}

// BundleBinaryVersion returns the version of the anet binary.
func BundleBinaryVersion() string {
	bin := BundleBinaryPath()
	if bin == "" {
		return ""
	}
	cmd := exec.Command(bin, "--version")
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
