package agentnet

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

// installScriptURL is the one-line installer endpoint for anet binary.
const installScriptURL = "https://clawnet.cc/install.sh"
const installPowerShellURL = "https://clawnet.cc/install.ps1"

var supportedOS = map[string]bool{"windows": true, "darwin": true, "linux": true}
var supportedArch = map[string]bool{"amd64": true, "arm64": true}

func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		// Windows: %LOCALAPPDATA%\anet
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			return filepath.Join(localAppData, "anet"), nil
		}
	}
	return filepath.Join(home, ".anet"), nil
}

// LocalBinaryName returns "anet.exe" on Windows, "anet" otherwise.
func LocalBinaryName() string {
	if runtime.GOOS == "windows" {
		return "anet.exe"
	}
	return "anet"
}

// ManualBinaryPath checks if the user has manually placed an anet binary.
func ManualBinaryPath() (string, bool) {
	dir, err := installDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(dir, LocalBinaryName())
	info, err := os.Stat(p)
	if err == nil && !info.IsDir() && info.Size() > 0 {
		return p, true
	}
	return "", false
}

// Download installs the anet binary using the official installer script.
// On Linux/macOS it runs `curl -fsSL https://clawnet.cc/install.sh | sh`.
// On Windows it runs the PowerShell installer.
// Returns the path to the installed binary.
func Download(emitProgress func(stage string, pct int, msg string)) (string, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	if !supportedOS[runtime.GOOS] {
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	if !supportedArch[runtime.GOARCH] {
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}

	// Check if already installed
	if p, ok := ManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("Using existing binary → %s", p))
		return p, nil
	}

	// Also check PATH
	if p, err := exec.LookPath("anet"); err == nil {
		emit("done", 100, fmt.Sprintf("Using anet from PATH → %s", p))
		return p, nil
	}

	emit("downloading", 10, "Installing anet via official installer...")

	dir, err := installDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", dir, err)
	}

	targetPath := filepath.Join(dir, LocalBinaryName())

	if runtime.GOOS == "windows" {
		err = installWindows(emit)
	} else {
		err = installUnix(emit)
	}
	if err != nil {
		return "", err
	}

	// Verify installation
	if p, ok := ManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("AgentNet installed → %s", p))
		return p, nil
	}
	// Check PATH as fallback (installer may put it in /usr/local/bin)
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

func installUnix(emit func(string, int, string)) error {
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

func installWindows(emit func(string, int, string)) error {
	emit("downloading", 30, "Running PowerShell installer...")
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-Command", fmt.Sprintf("irm %s | iex", installPowerShellURL))
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: try direct HTTP download
		emit("downloading", 50, "PowerShell installer failed, trying direct download...")
		return downloadDirect(emit)
	}
	_ = out
	emit("downloading", 90, "Installation completed")
	return nil
}

// downloadDirect is a fallback that downloads the binary directly via HTTP.
func downloadDirect(emit func(string, int, string)) error {
	arch := runtime.GOARCH
	osName := runtime.GOOS
	asset := fmt.Sprintf("anet-%s-%s", osName, arch)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	downloadURL := fmt.Sprintf("https://clawnet.cc/download/%s", asset)
	emit("downloading", 55, fmt.Sprintf("Downloading %s ...", asset))

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

	dir, err := installDir()
	if err != nil {
		return err
	}
	targetPath := filepath.Join(dir, LocalBinaryName())
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
