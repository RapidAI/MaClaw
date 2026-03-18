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

// clawnetAssetName returns the expected release asset filename for the current platform.
// Pattern: clawnet-{os}-{arch}[.exe]
func clawnetAssetName() (string, error) {
	var osName, archName string
	switch runtime.GOOS {
	case "windows":
		osName = "windows"
	case "darwin":
		osName = "darwin"
	case "linux":
		osName = "linux"
	default:
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		archName = "amd64"
	case "arm64":
		archName = "arm64"
	default:
		return "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
	name := fmt.Sprintf("clawnet-%s-%s", osName, archName)
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

	downloadURL := fmt.Sprintf("%s/%s", clawnetGitHubReleasesBase, asset)
	targetPath := filepath.Join(installDir, clawnetLocalBinaryName())

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
		return "", fmt.Errorf("download failed: HTTP %s (url: %s)", resp.Status, downloadURL)
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
	outFile.Close()

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
