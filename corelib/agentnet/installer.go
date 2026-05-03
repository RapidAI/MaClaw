package agentnet

import (
	"archive/tar"
	"compress/gzip"
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

// npm scoped package for AgentNet binary.
const npmPkg = "@agentnetwork/anet"

// gitHubRepo is the GitHub repository for direct binary download fallback.
const gitHubRepo = "ChatChatTech/skills"

// npmRegistries is the ordered fallback list of npm registries.
// npmmirror (China) is tried first for better connectivity in mainland China.
var npmRegistries = []struct {
	URL  string
	Name string
}{
	{"https://registry.npmmirror.com", "npmmirror"},
	{"https://registry.npmjs.org", "npmjs"},
}

var supportedOS = map[string]bool{"windows": true, "darwin": true, "linux": true}
var supportedArch = map[string]bool{"amd64": true, "arm64": true}

// npmOSMap maps Go GOOS to npm platform names.
var npmOSMap = map[string]string{
	"linux":   "linux",
	"darwin":  "darwin",
	"windows": "win32",
}

// npmArchMap maps Go GOARCH to npm architecture names.
var npmArchMap = map[string]string{
	"amd64": "x64",
	"arm64": "arm64",
}

// InstallDir returns the expected install directory for the anet binary.
// Windows: %LOCALAPPDATA%\anet   (or ~/.anet as fallback)
// Others:  ~/.anet
func InstallDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
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
	dir, err := InstallDir()
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

// Download installs the anet binary using a three-tier fallback chain:
//  1. npmmirror (China mirror) — fetch npm tgz package and extract binary
//  2. npm official registry   — same approach
//  3. GitHub Releases         — direct binary download
//
// All downloads are done via Go native HTTP, no external tools (curl/sh/powershell) required.
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
	if p, err := exec.LookPath("anet"); err == nil {
		emit("done", 100, fmt.Sprintf("Using anet from PATH → %s", p))
		return p, nil
	}

	emit("downloading", 5, "Installing anet binary...")

	dir, err := InstallDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create install directory %s: %w", dir, err)
	}

	targetPath := filepath.Join(dir, LocalBinaryName())

	// Strategy 1 & 2: try npm registries
	for i, reg := range npmRegistries {
		basePct := 10 + i*30 // 10 for first, 40 for second
		emit("downloading", basePct, fmt.Sprintf("Trying %s ...", reg.Name))
		if err := installViaNpm(reg.URL, reg.Name, targetPath, emit, basePct); err == nil {
			emit("done", 100, fmt.Sprintf("AgentNet installed via %s → %s", reg.Name, targetPath))
			return targetPath, nil
		}
	}

	// Strategy 3: GitHub Releases
	emit("downloading", 70, "Trying GitHub Releases ...")
	if err := installViaGitHub(targetPath, emit); err == nil {
		emit("done", 100, fmt.Sprintf("AgentNet installed via GitHub → %s", targetPath))
		return targetPath, nil
	}

	// All sources failed — check if installer put it somewhere else
	if p, ok := ManualBinaryPath(); ok {
		emit("done", 100, fmt.Sprintf("AgentNet installed → %s", p))
		return p, nil
	}
	if p, err := exec.LookPath("anet"); err == nil {
		emit("done", 100, fmt.Sprintf("AgentNet installed → %s", p))
		return p, nil
	}

	return "", fmt.Errorf(
		"[agentnet-not-available] 🌐 AgentNet installation failed for %s/%s\n\n"+
			"All download sources failed (npmmirror, npm, GitHub).\n"+
			"You can manually install by running:\n"+
			"  curl -fsSL https://agentnet.cc/install.sh | sh\n\n"+
			"Or place the anet binary at:\n  %s",
		runtime.GOOS, runtime.GOARCH, targetPath,
	)
}

// installViaNpm downloads the anet binary from an npm registry.
// It fetches the package metadata to get the latest version, then downloads
// the platform-specific tgz package and extracts the binary from package/bin/.
func installViaNpm(registryURL, registryName, targetPath string, emit func(string, int, string), basePct int) error {
	npmOS := npmOSMap[runtime.GOOS]
	npmArch := npmArchMap[runtime.GOARCH]
	if npmOS == "" || npmArch == "" {
		return fmt.Errorf("no npm platform mapping for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Platform package: @agentnetwork/anet-linux-x64
	platPkg := fmt.Sprintf("@agentnetwork/anet-%s-%s", npmOS, npmArch)
	platShort := fmt.Sprintf("anet-%s-%s", npmOS, npmArch)

	client := &http.Client{Timeout: 30 * time.Second}

	// Fetch latest version from the root package metadata
	emit("downloading", basePct+5, fmt.Sprintf("[%s] fetching version...", registryName))
	metaURL := fmt.Sprintf("%s/%s", registryURL, npmPkg)
	ver, metaErr := fetchNpmLatest(client, metaURL)
	if metaErr != nil {
		return fmt.Errorf("[%s] %w", registryName, metaErr)
	}
	emit("downloading", basePct+10, fmt.Sprintf("[%s] latest version: %s", registryName, ver))

	// Download platform tgz: /@agentnetwork/anet-linux-x64/-/anet-linux-x64-1.1.5.tgz
	tarballURL := fmt.Sprintf("%s/%s/-/%s-%s.tgz", registryURL, platPkg, platShort, ver)
	emit("downloading", basePct+12, fmt.Sprintf("[%s] downloading %s ...", registryName, platShort))

	dlClient := &http.Client{Timeout: 5 * time.Minute}
	dlResp, err := dlClient.Get(tarballURL)
	if err != nil {
		return fmt.Errorf("[%s] download failed: %w", registryName, err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, dlResp.Body)
		return fmt.Errorf("[%s] download returned %d", registryName, dlResp.StatusCode)
	}

	// Extract binary from tgz: package/bin/anet (or anet.exe)
	binName := LocalBinaryName()
	wantPath := "package/bin/" + binName

	emit("downloading", basePct+20, fmt.Sprintf("[%s] extracting %s ...", registryName, binName))

	binData, err := extractFileFromTgz(dlResp.Body, wantPath)
	if err != nil {
		return fmt.Errorf("[%s] extraction failed: %w", registryName, err)
	}

	// Write binary to target
	tmpPath := targetPath + ".npm-download"
	if err := os.WriteFile(tmpPath, binData, 0755); err != nil {
		return fmt.Errorf("[%s] failed to write binary: %w", registryName, err)
	}
	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("[%s] failed to install binary: %w", registryName, err)
	}

	return nil
}

// fetchNpmLatest fetches the "latest" dist-tag from an npm registry metadata URL.
// The response body is always closed before returning.
func fetchNpmLatest(client *http.Client, metaURL string) (string, error) {
	resp, err := client.Get(metaURL)
	if err != nil {
		return "", fmt.Errorf("metadata request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("metadata returned %d", resp.StatusCode)
	}
	var meta struct {
		DistTags struct {
			Latest string `json:"latest"`
		} `json:"dist-tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", fmt.Errorf("failed to parse metadata: %w", err)
	}
	if meta.DistTags.Latest == "" {
		return "", fmt.Errorf("could not determine latest version")
	}
	return meta.DistTags.Latest, nil
}

// extractFileFromTgz reads a .tgz stream and extracts the content of the
// specified file path. Returns the file bytes or an error if not found.
func extractFileFromTgz(r io.Reader, wantPath string) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		// Normalize path separators for comparison
		name := filepath.ToSlash(hdr.Name)
		if name == wantPath {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", wantPath, err)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("file %s not found in tarball", wantPath)
}

// installViaGitHub downloads the anet binary directly from GitHub Releases.
func installViaGitHub(targetPath string, emit func(string, int, string)) error {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	asset := fmt.Sprintf("anet-%s-%s", osName, arch)
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", gitHubRepo, asset)
	emit("downloading", 75, fmt.Sprintf("[GitHub] downloading %s ...", asset))

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("[GitHub] download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("[GitHub] download returned status %d", resp.StatusCode)
	}

	tmpPath := targetPath + ".gh-download"
	outFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("[GitHub] failed to create temp file: %w", err)
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
				return fmt.Errorf("[GitHub] write error: %w", wErr)
			}
			written += int64(n)
			if totalSize > 0 {
				pct := 75 + int(written*20/totalSize)
				if pct != lastPct {
					lastPct = pct
					mb := float64(written) / (1024 * 1024)
					totalMB := float64(totalSize) / (1024 * 1024)
					emit("downloading", pct, fmt.Sprintf("[GitHub] %.1f / %.1f MB", mb, totalMB))
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			outFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("[GitHub] download interrupted: %w", readErr)
		}
	}
	outFile.Sync()
	outFile.Close()

	if runtime.GOOS != "windows" {
		os.Chmod(tmpPath, 0755)
	}

	os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("[GitHub] failed to install binary: %w", err)
	}

	emit("downloading", 95, "[GitHub] download completed")
	return nil
}

// UpdateInfo holds the result of a version check against npm registries.
type UpdateInfo struct {
	LocalVersion  string `json:"local_version"`
	RemoteVersion string `json:"remote_version"`
	NeedsUpdate   bool   `json:"needs_update"`
	Registry      string `json:"registry,omitempty"` // which registry responded
}

// FetchLatestVersion queries npm registries (npmmirror first, then npmjs)
// and returns the latest published version string (e.g. "1.1.5").
func FetchLatestVersion() (version string, registry string, err error) {
	client := &http.Client{Timeout: 15 * time.Second}
	for _, reg := range npmRegistries {
		metaURL := fmt.Sprintf("%s/%s", reg.URL, npmPkg)
		ver, fetchErr := fetchNpmLatest(client, metaURL)
		if fetchErr != nil {
			continue
		}
		return ver, reg.Name, nil
	}
	return "", "", fmt.Errorf("could not fetch latest version from any npm registry")
}

// GetLocalVersion runs `anet --version` and returns the version string.
// Returns "" if the binary is not found or the command fails.
func GetLocalVersion() string {
	bin := ""
	if p, ok := ManualBinaryPath(); ok {
		bin = p
	} else if p, err := exec.LookPath("anet"); err == nil {
		bin = p
	}
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

// CheckUpdate compares the local anet version against the latest on npm.
// Returns UpdateInfo with version details and whether an update is needed.
func CheckUpdate() (*UpdateInfo, error) {
	local := GetLocalVersion()
	remote, reg, err := FetchLatestVersion()
	if err != nil {
		return nil, err
	}
	info := &UpdateInfo{
		LocalVersion:  local,
		RemoteVersion: remote,
		Registry:      reg,
		NeedsUpdate:   local == "" || compareVersions(local, remote) < 0,
	}
	return info, nil
}

// SmartUpdate checks the remote version and only updates if needed.
// Strategy: check version → anet update → npm download fallback.
//  1. Compare local vs remote version via npm registry
//  2. If update needed, try `anet update` first (built-in updater)
//  3. If `anet update` fails, fall back to npm tgz download
//
// Returns the UpdateInfo and any error.
func SmartUpdate(emitProgress func(stage string, pct int, msg string)) (*UpdateInfo, error) {
	emit := func(stage string, pct int, msg string) {
		if emitProgress != nil {
			emitProgress(stage, pct, msg)
		}
	}

	emit("checking", 5, "Checking for updates...")
	info, err := CheckUpdate()
	if err != nil {
		return nil, fmt.Errorf("version check failed: %w", err)
	}

	if !info.NeedsUpdate {
		emit("done", 100, fmt.Sprintf("Already up to date: %s", info.LocalVersion))
		return info, nil
	}

	emit("updating", 10, fmt.Sprintf("Update available: %s → %s (via %s)",
		info.LocalVersion, info.RemoteVersion, info.Registry))

	// Strategy 1: try `anet update` (the binary's built-in updater).
	if anetUpdateErr := tryAnetUpdate(emit); anetUpdateErr == nil {
		// Verify the update actually worked by re-checking version.
		newVer := GetLocalVersion()
		if newVer != "" && compareVersions(newVer, info.RemoteVersion) >= 0 {
			info.LocalVersion = newVer
			info.NeedsUpdate = false
			emit("done", 100, fmt.Sprintf("Updated via anet update: %s", newVer))
			return info, nil
		}
		emit("updating", 40, fmt.Sprintf("anet update ran but version still %s, trying npm download...", newVer))
	} else {
		emit("updating", 40, fmt.Sprintf("anet update failed: %v, trying npm download...", anetUpdateErr))
	}

	// Strategy 2: download from npm registries, then GitHub.
	dir, err := InstallDir()
	if err != nil {
		return info, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return info, fmt.Errorf("failed to create install directory: %w", err)
	}
	targetPath := filepath.Join(dir, LocalBinaryName())

	// On Windows, stop the daemon first so we can replace the binary.
	if runtime.GOOS == "windows" {
		stopDaemonForUpdate(targetPath)
	}

	updated := false
	for i, reg := range npmRegistries {
		basePct := 45 + i*20
		emit("updating", basePct, fmt.Sprintf("Downloading from %s ...", reg.Name))
		if err := installViaNpm(reg.URL, reg.Name, targetPath, emit, basePct); err == nil {
			updated = true
			break
		}
	}
	if !updated {
		emit("updating", 85, "Trying GitHub Releases ...")
		if err := installViaGitHub(targetPath, emit); err != nil {
			return info, fmt.Errorf("all update methods failed (anet update + npm + GitHub): %w", err)
		}
	}

	emit("done", 100, fmt.Sprintf("Updated: %s → %s", info.LocalVersion, info.RemoteVersion))
	return info, nil
}

// tryAnetUpdate runs `anet update` using the installed binary.
// Returns nil on success, error on failure.
func tryAnetUpdate(emit func(string, int, string)) error {
	bin := ""
	if p, ok := ManualBinaryPath(); ok {
		bin = p
	} else if p, err := exec.LookPath("anet"); err == nil {
		bin = p
	}
	if bin == "" {
		return fmt.Errorf("anet binary not found")
	}
	emit("updating", 15, "Running anet update ...")
	cmd := exec.Command(bin, "update")
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("anet update: %w — %s", err, strings.TrimSpace(string(out)))
	}
	emit("updating", 35, "anet update completed, verifying...")
	return nil
}

// stopDaemonForUpdate attempts to stop the anet daemon so the binary can be
// replaced on Windows (where running executables are locked).
func stopDaemonForUpdate(binPath string) {
	if _, err := os.Stat(binPath); err != nil {
		return
	}
	cmd := exec.Command(binPath, "stop")
	hideCommandWindow(cmd)
	_ = cmd.Run()
	// Give the daemon a moment to release the file
	time.Sleep(500 * time.Millisecond)
}

// compareVersions compares two semver-like version strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
// Handles versions like "1.1.5", "v1.1.5", "anet 1.1.5".
func compareVersions(a, b string) int {
	pa := parseVersion(a)
	pb := parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parseVersion extracts [major, minor, patch] from a version string.
// Handles "1.1.5", "v1.1.5", "anet 1.1.5", "anet/1.1.5" etc.
func parseVersion(s string) [3]int {
	s = strings.TrimSpace(s)
	// Strip common prefixes
	for _, prefix := range []string{"anet/", "anet ", "v"} {
		if strings.HasPrefix(strings.ToLower(s), prefix) {
			s = s[len(prefix):]
			break
		}
	}
	var v [3]int
	fmt.Sscanf(s, "%d.%d.%d", &v[0], &v[1], &v[2])
	return v
}
