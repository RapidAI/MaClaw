package main

// clawnet_installer.go — Auto-download ClawNet binary from GitHub Releases.
// When the clawnet binary is not found locally, downloads the correct
// precompiled binary for the current OS/arch from:
//   https://github.com/ChatChatTech/ClawNet/releases/latest/download/clawnet-{os}-{arch}[.exe]
// Saves to ~/.openclaw/clawnet/clawnet[.exe] and makes it executable.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	clawnetGitHubReleasesBase = "https://github.com/ChatChatTech/ClawNet/releases/latest/download"
)

// supportedOS lists the operating systems for which prebuilt binaries exist.
var supportedOS = map[string]bool{"windows": true, "darwin": true, "linux": true}

// supportedArch lists the architectures for which prebuilt binaries exist.
var supportedArch = map[string]bool{"amd64": true, "arm64": true}

// clawnetAssetName returns the expected release asset filename for the current platform.
// Pattern: clawnet-{os}-{arch}[.exe]
func clawnetAssetName() (string, error) {
	if !supportedOS[runtime.GOOS] {
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	if !supportedArch[runtime.GOARCH] {
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
	name := fmt.Sprintf("clawnet-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name, nil
}

// clawnetInstallDir returns ~/.openclaw/clawnet/
func clawnetInstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "clawnet"), nil
}

// clawnetLocalBinaryName returns "clawnet.exe" on Windows, "clawnet" otherwise.
func clawnetLocalBinaryName() string {
	if runtime.GOOS == "windows" {
		return "clawnet.exe"
	}
	return "clawnet"
}

// clawnetManualBinaryPath checks if the user has manually placed a clawnet binary
// at ~/.openclaw/clawnet/clawnet (or .exe on Windows). Returns the path if found.
func clawnetManualBinaryPath() (string, bool) {
	installDir, err := clawnetInstallDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(installDir, clawnetLocalBinaryName())
	info, err := os.Stat(p)
	if err == nil && !info.IsDir() && info.Size() > 0 {
		return p, true
	}
	return "", false
}

// DownloadClawNet downloads the clawnet binary from GitHub Releases.
// It accepts an optional emitProgress callback for reporting status to the frontend.
// Returns the path to the installed binary.
func DownloadClawNet(emitProgress func(stage string, pct int, msg string)) (string, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	asset, err := clawnetAssetName()
	if err != nil {
		return "", err
	}

	installDir, err := clawnetInstallDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(installDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", installDir, err)
	}

	targetPath := filepath.Join(installDir, clawnetLocalBinaryName())

	// Check for a manually placed binary before attempting download.
	if p, ok := clawnetManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("Using manually installed binary → %s", p))
		return p, nil
	}

	downloadURL := fmt.Sprintf("%s/%s", clawnetGitHubReleasesBase, asset)

	emit("downloading", 0, fmt.Sprintf("Downloading %s ...", asset))

	client := &http.Client{
		Timeout: 10 * time.Minute, // generous timeout for large binaries
	}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download clawnet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain body so the connection can be reused.
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf(
			"[clawnet-not-available] 🦞 ClawNet %s/%s not yet available\n\n"+
				"The author hasn't published a prebuilt binary for your platform yet.\n"+
				"This is not your fault — just waiting on an upstream release.\n\n"+
				"You can manually place the binary at:\n  %s\n"+
				"and it will be picked up automatically next time.",
			runtime.GOOS, runtime.GOARCH,
			targetPath,
		)
	}

	totalSize := resp.ContentLength // may be -1 if unknown

	// Write to a temp file first, then rename for atomicity.
	tmpPath := targetPath + ".download"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	var written int64
	buf := make([]byte, 64*1024)
	lastPct := -1
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := outFile.Write(buf[:n]); wErr != nil {
				outFile.Close()
				os.Remove(tmpPath)
				return "", fmt.Errorf("write error: %w", wErr)
			}
			written += int64(n)
			if totalSize > 0 {
				pct := int(written * 100 / totalSize)
				if pct != lastPct {
					lastPct = pct
					mb := float64(written) / (1024 * 1024)
					totalMB := float64(totalSize) / (1024 * 1024)
					emit("downloading", pct, fmt.Sprintf("%.1f / %.1f MB (%d%%)", mb, totalMB, pct))
				}
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

	// Remove old binary if present, then rename temp → final.
	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to install binary: %w", err)
	}

	// Make executable on unix
	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0755)
	}

	emit("done", 100, fmt.Sprintf("ClawNet installed → %s", targetPath))
	return targetPath, nil
}
