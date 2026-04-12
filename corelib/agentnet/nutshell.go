package agentnet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// NutshellManager wraps the anet pack/unpack commands for .nut bundle operations.
// Per skill.md: always use `anet pack`, never standalone nutshell CLI.
type NutshellManager struct {
	anetBin string // path to anet binary
}

// NewNutshellManager creates a NutshellManager using the given anet binary path.
func NewNutshellManager(anetBin string) *NutshellManager {
	return &NutshellManager{anetBin: anetBin}
}

// NutshellStatus holds the result of nutshell availability check.
type NutshellStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// IsInstalled checks if the anet binary supports pack/unpack.
func (n *NutshellManager) IsInstalled() NutshellStatus {
	out, err := n.runAnet("--version")
	if err != nil {
		return NutshellStatus{Installed: false, Error: err.Error()}
	}
	return NutshellStatus{Installed: true, Version: strings.TrimSpace(out)}
}

// Install is a no-op since nutshell is built into anet.
func (n *NutshellManager) Install() error {
	return nil // anet pack is built-in
}

// Init initializes a new nutshell task bundle in the given directory.
func (n *NutshellManager) Init(dir string) (string, error) {
	return n.runAnet("nutshell", "init", "--dir", dir)
}

// Check validates a nutshell bundle directory.
func (n *NutshellManager) Check(dir string) (string, error) {
	return n.runAnet("nutshell", "check", "--dir", dir)
}

// Publish publishes a nutshell bundle to the AgentNet network with a reward.
func (n *NutshellManager) Publish(dir string, reward float64) (string, error) {
	return n.runAnet("nutshell", "publish", "--dir", dir, "--reward", fmt.Sprintf("%.0f", reward))
}

// Claim claims a task and creates a local workspace.
func (n *NutshellManager) Claim(taskID, outputDir string) (string, error) {
	return n.runAnet("nutshell", "claim", taskID, "-o", outputDir)
}

// Deliver submits completed work from a workspace directory.
func (n *NutshellManager) Deliver(dir string) (string, error) {
	return n.runAnet("nutshell", "deliver", "--dir", dir)
}

// Pack creates a .nut bundle file using `anet pack`.
func (n *NutshellManager) Pack(dir, outputFile, peerID string) (string, error) {
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
func (n *NutshellManager) Unpack(nutFile, outputDir string) (string, error) {
	args := []string{"unpack", nutFile}
	if outputDir != "" {
		args = append(args, outputDir)
	}
	return n.runAnet(args...)
}

// ListBundles returns locally known nutshell bundles.
func (n *NutshellManager) ListBundles() ([]map[string]interface{}, error) {
	out, err := n.runAnet("nutshell", "list", "--json")
	if err != nil {
		return nil, err
	}
	var bundles []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &bundles); err != nil {
		return []map[string]interface{}{{"raw": strings.TrimSpace(out)}}, nil
	}
	return bundles, nil
}

// runAnet executes an anet subcommand.
func (n *NutshellManager) runAnet(args ...string) (string, error) {
	bin := n.anetBin
	if bin == "" {
		bin = "anet"
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// NutshellLocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
// (nutshell is now built into anet)
func NutshellLocalBinaryName() string {
	return LocalBinaryName()
}

// NutshellInstallDir returns the directory where anet is installed.
func NutshellInstallDir() (string, error) {
	return InstallDir()
}

// NutshellBinaryPath returns the expected anet binary path.
func NutshellBinaryPath() string {
	dir, err := NutshellInstallDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, NutshellLocalBinaryName())
}

// InstallWithProgress is a no-op since nutshell is built into anet.
// If anet is not installed, it installs anet.
func (n *NutshellManager) InstallWithProgress(emitProgress func(stage string, pct int, msg string)) (string, error) {
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

	// Install anet (which includes nutshell/pack functionality)
	return Download(emitProgress)
}

// NutshellBinaryInstalled checks if the anet binary exists.
func NutshellBinaryInstalled() bool {
	if _, ok := ManualBinaryPath(); ok {
		return true
	}
	_, err := exec.LookPath("anet")
	return err == nil
}

// NutshellBinaryVersion returns the version of the anet binary.
func NutshellBinaryVersion() string {
	bin := NutshellBinaryPath()
	if bin == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
	}
	cmd := exec.Command(bin, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
