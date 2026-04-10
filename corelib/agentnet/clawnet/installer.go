package clawnet

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const gitHubReleasesBase = "https://github.com/ChatChatTech/ClawNet/releases/latest/download"

var supportedOS = map[string]bool{"windows": true, "darwin": true, "linux": true}
var supportedArch = map[string]bool{"amd64": true, "arm64": true}

func assetName() (string, error) {
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

func installDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "clawnet"), nil
}

// LocalBinaryName returns "clawnet.exe" on Windows, "clawnet" otherwise.
func LocalBinaryName() string {
	if runtime.GOOS == "windows" {
		return "clawnet.exe"
	}
	return "clawnet"
}

// ManualBinaryPath checks if the user has manually placed a clawnet binary.
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

// Download downloads the clawnet binary from GitHub Releases.
// Returns the path to the installed binary.
func Download(emitProgress func(stage string, pct int, msg string)) (string, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	asset, err := assetName()
	if err != nil {
		return "", err
	}

	dir, err := installDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", dir, err)
	}

	targetPath := filepath.Join(dir, LocalBinaryName())

	if p, ok := ManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("Using manually installed binary → %s", p))
		return p, nil
	}

	downloadURL := fmt.Sprintf("%s/%s", gitHubReleasesBase, asset)
	emit("downloading", 0, fmt.Sprintf("Downloading %s ...", asset))

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download clawnet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf(
			"[clawnet-not-available] 🦞 ClawNet %s/%s not yet available\n\n"+
				"The author hasn't published a prebuilt binary for your platform yet.\n"+
				"You can manually place the binary at:\n  %s\n"+
				"and it will be picked up automatically next time.",
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

	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to install binary: %w", err)
	}

	if runtime.GOOS != "windows" {
		os.Chmod(targetPath, 0755)
	}

	emit("done", 100, fmt.Sprintf("ClawNet installed → %s", targetPath))
	return targetPath, nil
}
