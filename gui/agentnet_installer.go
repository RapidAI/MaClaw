package main

// agentnet_installer.go — Auto-install anet binary via official installer.
// When the anet binary is not found locally, installs using:
//   Linux/macOS: curl -fsSL https://clawnet.cc/install.sh | sh
//   Windows:     irm https://clawnet.cc/install.ps1 | iex
// Fallback: direct download from GitHub Releases (ChatChatTech/skills)

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

const (
	anetInstallScriptURL     = "https://clawnet.cc/install.sh"
	anetInstallPowerShellURL = "https://clawnet.cc/install.ps1"
	// GitHub Releases: uses /latest/download/ which 302-redirects to the newest tag.
	anetGitHubRepo = "ChatChatTech/skills"
)

// anetLocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
func anetLocalBinaryName() string {
	if runtime.GOOS == "windows" {
		return "anet.exe"
	}
	return "anet"
}

// anetInstallDir returns the expected install directory for the anet binary.
// Windows: %LOCALAPPDATA%\anet   (or ~/.anet as fallback)
// Others:  ~/.anet
func anetInstallDir() (string, error) {
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

// anetManualBinaryPath checks if the anet binary exists in the install dir.
func anetManualBinaryPath() (string, bool) {
	dir, err := anetInstallDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(dir, anetLocalBinaryName())
	info, err := os.Stat(p)
	if err == nil && !info.IsDir() && info.Size() > 0 {
		return p, true
	}
	return "", false
}

// DownloadAnet installs the anet binary using the official installer script.
// On Linux/macOS: curl -fsSL https://clawnet.cc/install.sh | sh
// On Windows: irm https://clawnet.cc/install.ps1 | iex (fallback: direct download)
// Returns the path to the installed binary.
func DownloadAnet(emitProgress func(stage string, pct int, msg string)) (string, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	// Check if already installed
	if p, ok := anetManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("Using existing binary → %s", p))
		return p, nil
	}
	if p, err := exec.LookPath("anet"); err == nil {
		emit("done", 100, fmt.Sprintf("Using anet from PATH → %s", p))
		return p, nil
	}

	emit("downloading", 10, "Installing anet via official installer...")

	dir, err := anetInstallDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", dir, err)
	}

	targetPath := filepath.Join(dir, anetLocalBinaryName())

	if runtime.GOOS == "windows" {
		err = anetInstallWindows(emit)
	} else {
		err = anetInstallUnix(emit)
	}
	if err != nil {
		return "", err
	}

	// Verify installation
	if p, ok := anetManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("AgentNet installed → %s", p))
		return p, nil
	}
	if p, err := exec.LookPath("anet"); err == nil {
		emit("done", 100, fmt.Sprintf("AgentNet installed → %s", p))
		return p, nil
	}

	return "", fmt.Errorf(
		"[agentnet-not-available] 🌐 AgentNet installation failed for %s/%s\n\n"+
			"You can manually install by running:\n"+
			"  curl -fsSL https://clawnet.cc/install.sh | sh\n\n"+
			"Or place the anet binary at:\n  %s",
		runtime.GOOS, runtime.GOARCH, targetPath,
	)
}

func anetInstallUnix(emit func(string, int, string)) error {
	emit("downloading", 30, "Running: curl -fsSL https://clawnet.cc/install.sh | sh")
	cmd := exec.Command("sh", "-c", "curl -fsSL https://clawnet.cc/install.sh | sh")
	cmd.Env = append(os.Environ(), "ANET_NO_INTERACTIVE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installer failed: %w\n%s", err, string(out))
	}
	emit("downloading", 90, "Installation script completed")
	return nil
}

func anetInstallWindows(emit func(string, int, string)) error {
	emit("downloading", 30, "Running PowerShell installer...")
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", fmt.Sprintf("irm %s | iex", anetInstallPowerShellURL))
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		emit("downloading", 50, "PowerShell installer failed, trying direct download...")
		return anetDownloadDirect(emit)
	}
	_ = out
	emit("downloading", 90, "Installation completed")
	return nil
}

// anetDownloadDirect is a fallback that downloads the binary from GitHub Releases.
// Uses the /latest/download/ URL which 302-redirects to the newest release asset.
func anetDownloadDirect(emit func(string, int, string)) error {
	// Map Go GOOS/GOARCH to GitHub release asset names.
	// Asset naming: anet-{os}-{arch}[.exe]
	// os: windows, darwin, linux   arch: amd64, arm64
	osName := runtime.GOOS
	arch := runtime.GOARCH
	asset := fmt.Sprintf("anet-%s-%s", osName, arch)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", anetGitHubRepo, asset)
	emit("downloading", 55, fmt.Sprintf("Downloading from GitHub Releases: %s ...", asset))

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download anet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	dir, err := anetInstallDir()
	if err != nil {
		return err
	}
	targetPath := filepath.Join(dir, anetLocalBinaryName())
	tmpPath := targetPath + ".download"

	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	totalSize := resp.ContentLength
	var written int64
	buf := make([]byte, 64*1024)
	lastPct := -1
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := outFile.Write(buf[:n]); wErr != nil {
				outFile.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("write error: %w", wErr)
			}
			written += int64(n)
			if totalSize > 0 {
				pct := 55 + int(written*40/totalSize)
				if pct != lastPct {
					lastPct = pct
					mb := float64(written) / (1024 * 1024)
					totalMB := float64(totalSize) / (1024 * 1024)
					emit("downloading", pct, fmt.Sprintf("%.1f / %.1f MB (%d%%)", mb, totalMB, pct-55))
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			outFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("download interrupted: %w", readErr)
		}
	}
	outFile.Sync()
	outFile.Close()

	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to install binary: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0755)
	}

	emit("downloading", 95, "Direct download completed")
	return nil
}
