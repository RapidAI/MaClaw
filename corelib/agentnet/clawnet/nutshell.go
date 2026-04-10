package clawnet

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
// It first checks the install directory for the binary, then falls back to PATH.
func (n *NutshellManager) runNutshell(args ...string) (string, error) {
	bin := "nutshell"
	if p := NutshellBinaryPath(); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			bin = p
		}
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// ---------------------------------------------------------------------------
// Direct HTTP download installer (no black console window)
// ---------------------------------------------------------------------------

const nutshellReleasesBase = "https://github.com/ChatChatTech/ClawNet/releases/latest/download"

func nutshellAssetName() (string, error) {
	if !supportedOS[runtime.GOOS] {
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	if !supportedArch[runtime.GOARCH] {
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
	name := fmt.Sprintf("nutshell-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

// NutshellLocalBinaryName returns "nutshell.exe" on Windows, "nutshell" otherwise.
func NutshellLocalBinaryName() string {
	if runtime.GOOS == "windows" {
		return "nutshell.exe"
	}
	return "nutshell"
}

// NutshellInstallDir returns the directory where nutshell is installed.
func NutshellInstallDir() (string, error) {
	return installDir() // same as clawnet: ~/.openclaw/clawnet/
}

// NutshellBinaryPath returns the expected nutshell binary path (may not exist).
func NutshellBinaryPath() string {
	dir, err := NutshellInstallDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, NutshellLocalBinaryName())
}

// InstallWithProgress downloads the nutshell binary directly via HTTP,
// emitting progress through the callback. No console window is spawned.
func (n *NutshellManager) InstallWithProgress(emitProgress func(stage string, pct int, msg string)) (string, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	asset, err := nutshellAssetName()
	if err != nil {
		return "", err
	}

	dir, err := NutshellInstallDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", dir, err)
	}

	targetPath := filepath.Join(dir, NutshellLocalBinaryName())

	// Check if already exists
	if info, err := os.Stat(targetPath); err == nil && !info.IsDir() && info.Size() > 0 {
		emit("done", 100, fmt.Sprintf("Nutshell already installed → %s", targetPath))
		return targetPath, nil
	}

	downloadURL := fmt.Sprintf("%s/%s", nutshellReleasesBase, asset)
	emit("downloading", 0, fmt.Sprintf("Downloading %s ...", asset))

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download nutshell: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf(
			"[nutshell-not-available] 📦 Nutshell binary for %s/%s is not available yet.\n\n"+
				"You can manually download or build the nutshell binary and place it at:\n  %s\n\n"+
				"GitHub Releases: https://github.com/ChatChatTech/ClawNet/releases",
			runtime.GOOS, runtime.GOARCH, targetPath,
		)
	}

	// Guard against GitHub returning an HTML error page with 200 status.
	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/html") {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf(
			"[nutshell-not-available] 📦 Nutshell binary for %s/%s is not available yet.\n\n"+
				"You can manually download or build the nutshell binary and place it at:\n  %s\n\n"+
				"GitHub Releases: https://github.com/ChatChatTech/ClawNet/releases",
			runtime.GOOS, runtime.GOARCH, targetPath,
		)
	}

	totalSize := resp.ContentLength
	tmpPath := targetPath + ".download"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	var written int64
	buf := make([]byte, 64*1024)
	lastPct := -1
	for {
		nr, readErr := resp.Body.Read(buf)
		if nr > 0 {
			if _, wErr := outFile.Write(buf[:nr]); wErr != nil {
				outFile.Close()
				os.Remove(tmpPath)
				return "", fmt.Errorf("write error: %w", wErr)
			}
			written += int64(nr)
			if totalSize > 0 {
				pct := int(written * 100 / totalSize)
				if pct != lastPct {
					lastPct = pct
					mb := float64(written) / (1024 * 1024)
					totalMB := float64(totalSize) / (1024 * 1024)
					emit("downloading", pct, fmt.Sprintf("%.1f / %.1f MB (%d%%)", mb, totalMB, pct))
				}
			} else {
				// Unknown total size — report bytes downloaded.
				mb := float64(written) / (1024 * 1024)
				pct := int(mb * 10) % 100 // fake progress to keep bar moving
				emit("downloading", pct, fmt.Sprintf("%.1f MB downloaded", mb))
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			outFile.Close()
			os.Remove(tmpPath)
			return "", fmt.Errorf("download interrupted: %w", readErr)
		}
	}
	if err := outFile.Sync(); err != nil {
		outFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("sync error: %w", err)
	}
	if err := outFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("close error: %w", err)
	}

	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to install binary: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0755)
	}

	emit("done", 100, fmt.Sprintf("Nutshell installed → %s", targetPath))
	return targetPath, nil
}
