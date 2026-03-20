package clawnet

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// NutshellManager wraps the nutshell CLI for .nut bundle operations.
type NutshellManager struct {
	clawnetBin string // path to clawnet binary
}

// NewNutshellManager creates a NutshellManager using the given clawnet binary path.
func NewNutshellManager(clawnetBin string) *NutshellManager {
	return &NutshellManager{clawnetBin: clawnetBin}
}

// NutshellStatus holds the result of nutshell availability check.
type NutshellStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

// IsInstalled checks if the nutshell CLI is available.
func (n *NutshellManager) IsInstalled() NutshellStatus {
	out, err := n.runNutshell("--version")
	if err != nil {
		return NutshellStatus{Installed: false, Error: err.Error()}
	}
	return NutshellStatus{Installed: true, Version: strings.TrimSpace(out)}
}

// Install installs the nutshell CLI via clawnet.
func (n *NutshellManager) Install() error {
	_, err := n.runClawnet("nutshell", "install")
	return err
}

// Init initializes a new nutshell task bundle in the given directory.
func (n *NutshellManager) Init(dir string) (string, error) {
	return n.runNutshell("init", "--dir", dir)
}

// Check validates a nutshell bundle directory.
func (n *NutshellManager) Check(dir string) (string, error) {
	return n.runNutshell("check", "--dir", dir)
}

// Publish publishes a nutshell bundle to the ClawNet network with a reward.
func (n *NutshellManager) Publish(dir string, reward float64) (string, error) {
	return n.runNutshell("publish", "--dir", dir, "--reward", fmt.Sprintf("%.0f", reward))
}

// Claim claims a task and creates a local workspace.
func (n *NutshellManager) Claim(taskID, outputDir string) (string, error) {
	return n.runNutshell("claim", taskID, "-o", outputDir)
}

// Deliver submits completed work from a workspace directory.
func (n *NutshellManager) Deliver(dir string) (string, error) {
	return n.runNutshell("deliver", "--dir", dir)
}

// Pack creates a .nut bundle file. If peerID is non-empty, encrypts for that peer.
func (n *NutshellManager) Pack(dir, outputFile, peerID string) (string, error) {
	args := []string{"pack", "--dir", dir, "-o", outputFile}
	if peerID != "" {
		args = append(args, "--encrypt", "--peer", peerID)
	}
	return n.runNutshell(args...)
}

// Unpack extracts a .nut bundle file.
func (n *NutshellManager) Unpack(nutFile, outputDir string) (string, error) {
	return n.runNutshell("unpack", nutFile, "-o", outputDir)
}

// ListBundles returns locally known nutshell bundles (if supported by CLI).
func (n *NutshellManager) ListBundles() ([]map[string]interface{}, error) {
	out, err := n.runNutshell("list", "--json")
	if err != nil {
		return nil, err
	}
	var bundles []map[string]interface{}
	if err := json.Unmarshal([]byte(out), &bundles); err != nil {
		// CLI may not support --json; return raw output as single entry
		return []map[string]interface{}{{"raw": strings.TrimSpace(out)}}, nil
	}
	return bundles, nil
}

// runClawnet executes a clawnet subcommand.
func (n *NutshellManager) runClawnet(args ...string) (string, error) {
	bin := n.clawnetBin
	if bin == "" {
		bin = "clawnet"
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// runNutshell executes a nutshell subcommand.
func (n *NutshellManager) runNutshell(args ...string) (string, error) {
	cmd := exec.Command("nutshell", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}
