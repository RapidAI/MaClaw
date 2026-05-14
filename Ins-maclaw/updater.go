package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	githubLatestManifestURL = "https://github.com/RapidAI/MaClaw/releases/latest/download/latest.json"
	r2LatestManifestURL     = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest.json"
	r2PublicBaseURL         = "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev"
	cosLatestManifestURL    = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com/latest.json"
	cosPublicBaseURL        = "https://maclaw-1252723594.cos.ap-beijing.myqcloud.com"
	minInstallerBytes       = 5 * 1024 * 1024
)

type updateManifest struct {
	Version string                         `json:"version"`
	Tag     string                         `json:"tag"`
	Assets  map[string]updateManifestAsset `json:"assets"`
}

type updateManifestAsset struct {
	Name   string   `json:"name"`
	Size   int64    `json:"size"`
	URL    string   `json:"url"`
	URLs   []string `json:"urls"`
	SHA256 string   `json:"sha256,omitempty"`
}

type latestReleaseInfo struct {
	TagName     string
	DownloadURL string
	Source      string
	AssetName   string
	SHA256      string
}

type progressFunc func(downloaded, total int64)

func updateHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 6 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   6 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
}

func fetchLatestReleaseFast(ctx context.Context, productName, targetFileName string) (latestReleaseInfo, error) {
	var errors []string
	checks := []struct {
		source  string
		url     string
		timeout time.Duration
	}{
		{source: "github", url: githubLatestManifestURL, timeout: 4 * time.Second},
		{source: "r2", url: r2LatestManifestURL, timeout: 5 * time.Second},
		{source: "cos", url: cosLatestManifestURL, timeout: 5 * time.Second},
	}
	for _, check := range checks {
		release, err := fetchManifestLatestRelease(ctx, productName, targetFileName, check.source, check.url, check.timeout)
		if err == nil {
			return release, nil
		}
		errors = append(errors, fmt.Sprintf("%s: %v", check.source, err))
	}
	return latestReleaseInfo{}, fmt.Errorf("all latest manifest checks failed: %s", strings.Join(errors, "; "))
}

func fetchManifestLatestRelease(parent context.Context, productName, targetFileName, source, manifestURL string, timeout time.Duration) (latestReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return latestReleaseInfo{}, err
	}
	req.Header.Set("User-Agent", productName+"-Installer")
	resp, err := updateHTTPClient(timeout).Do(req)
	if err != nil {
		return latestReleaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyText, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return latestReleaseInfo{}, fmt.Errorf("%s latest manifest returned status %d: %s", source, resp.StatusCode, string(bodyText))
	}
	var manifest updateManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return latestReleaseInfo{}, err
	}
	tagName := strings.TrimSpace(manifest.Tag)
	if tagName == "" {
		tagName = strings.TrimSpace(manifest.Version)
	}
	if tagName == "" {
		return latestReleaseInfo{}, fmt.Errorf("latest manifest has no tag/version")
	}
	urls := manifestAssetDownloadURLs(manifest, targetFileName, tagName)
	githubURL := fmt.Sprintf("https://github.com/RapidAI/MaClaw/releases/download/%s/%s", tagName, targetFileName)
	if source == "github" {
		urls = append([]string{githubURL}, urls...)
	} else {
		urls = append(urls, githubURL)
	}
	downloadURL := combineDownloadURLList(urls...)
	if downloadURL == "" {
		return latestReleaseInfo{}, fmt.Errorf("latest manifest has no download URL for %s", targetFileName)
	}
	sha256 := ""
	if asset, ok := manifest.Assets[targetFileName]; ok {
		sha256 = strings.TrimSpace(asset.SHA256)
	}
	return latestReleaseInfo{TagName: tagName, DownloadURL: downloadURL, Source: source, AssetName: targetFileName, SHA256: sha256}, nil
}

func manifestAssetDownloadURLs(manifest updateManifest, fileName, tagName string) []string {
	urls := []string{}
	if asset, ok := manifest.Assets[fileName]; ok {
		urls = append(urls, asset.URLs...)
		urls = append(urls, asset.URL)
	}
	if tagName != "" {
		urls = append(urls, r2ReleaseAssetURL(fileName), cosReleaseAssetURL(fileName))
	}
	combined := combineDownloadURLList(urls...)
	if combined == "" {
		return nil
	}
	return strings.Split(combined, "\n")
}

func r2ReleaseAssetURL(fileName string) string {
	return fmt.Sprintf("%s/latest/%s", r2PublicBaseURL, fileName)
}

func cosReleaseAssetURL(fileName string) string {
	return fmt.Sprintf("%s/latest/%s", cosPublicBaseURL, fileName)
}

func combineDownloadURLList(urls ...string) string {
	seen := make(map[string]bool, len(urls))
	cleaned := make([]string, 0, len(urls))
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" || seen[url] {
			continue
		}
		seen[url] = true
		cleaned = append(cleaned, url)
	}
	return strings.Join(cleaned, "\n")
}

func splitDownloadURLs(urls string) []string {
	parts := strings.FieldsFunc(urls, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	})
	cleaned := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		cleaned = append(cleaned, part)
	}
	return cleaned
}

func downloadInstaller(ctx context.Context, productName, targetFileName, downloadURLs, expectedSHA256 string, onProgress progressFunc) (string, error) {
	urls := splitDownloadURLs(downloadURLs)
	if len(urls) == 0 {
		return "", fmt.Errorf("download url is empty")
	}
	destPath, err := installerDownloadPath(targetFileName)
	if err != nil {
		return "", err
	}
	var lastErr error
	for _, candidateURL := range urls {
		_ = os.Remove(destPath)
		_ = os.Remove(destPath + ".download")
		if err := downloadInstallerFromURL(ctx, productName, candidateURL, destPath, targetFileName, expectedSHA256, onProgress); err == nil {
			return destPath, nil
		} else {
			lastErr = err
		}
	}
	return "", fmt.Errorf("all download sources failed: %w", lastErr)
}

func installerDownloadPath(targetFileName string) (string, error) {
	base := os.Getenv("TEMP")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Ins-maclaw")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create download directory: %w", err)
	}
	return filepath.Join(dir, targetFileName), nil
}

func normalizeDownloadURL(rawURL string) (string, error) {
	downloadURL := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(downloadURL)
	if err != nil {
		return "", fmt.Errorf("invalid download URL: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("download URL must be https with a host: %s", rawURL)
	}
	return downloadURL, nil
}

func validateDownloadURL(rawURL string) error {
	_, err := normalizeDownloadURL(rawURL)
	return err
}

func downloadInstallerFromURL(ctx context.Context, productName, downloadURL, destPath, targetFileName, expectedSHA256 string, onProgress progressFunc) error {
	var err error
	downloadURL, err = normalizeDownloadURL(downloadURL)
	if err != nil {
		return err
	}
	sourceTimeout := 30 * time.Minute
	if strings.Contains(downloadURL, "github.com/") || strings.Contains(downloadURL, "githubusercontent.com/") {
		sourceTimeout = 90 * time.Second
	}
	sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(sourceCtx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", productName+"-Installer")
	resp, err := updateHTTPClient(12 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("installer not found (HTTP 404)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	if resp.ContentLength >= 0 && resp.ContentLength < minInstallerBytes {
		return fmt.Errorf("file too small (%d bytes), possibly an error page", resp.ContentLength)
	}
	if filepath.Ext(destPath) != filepath.Ext(targetFileName) {
		return fmt.Errorf("invalid download target extension: %s", destPath)
	}
	tmpPath := destPath + ".download"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer out.Close()
	var downloaded int64
	buf := make([]byte, 256*1024)
	lastReport := time.Now()
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				_ = os.Remove(tmpPath)
				return writeErr
			}
			downloaded += int64(n)
			if onProgress != nil && time.Since(lastReport) > 200*time.Millisecond {
				onProgress(downloaded, resp.ContentLength)
				lastReport = time.Now()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = os.Remove(tmpPath)
			if sourceCtx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("download source timed out: %s", downloadURL)
			}
			return readErr
		}
	}
	if onProgress != nil {
		onProgress(downloaded, resp.ContentLength)
	}
	if downloaded < minInstallerBytes {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("file too small (%d bytes), possibly an error page", downloaded)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := verifySHA256(tmpPath, expectedSHA256); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func verifySHA256(path, expected string) error {
	expected = strings.TrimSpace(strings.ToLower(expected))
	if expected == "" {
		return nil
	}
	if !isSHA256Hex(expected) {
		return fmt.Errorf("invalid sha256 digest %q", expected)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := fmt.Sprintf("%x", h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actual, expected)
	}
	return nil
}

func targetAssetName(productName string) (string, error) {
	return targetAssetNameFor(productName, runtime.GOOS, runtime.GOARCH, linuxUbuntuLabel())
}

func targetAssetNameFor(productName, goos, goarch, ubuntuLabel string) (string, error) {
	switch goos {
	case "windows":
		return productName + "-Setup.exe", nil
	case "darwin":
		return productName + "-Universal.pkg", nil
	case "linux":
		switch goarch {
		case "amd64":
			return fmt.Sprintf("%s-x86_64-%s.AppImage", productName, ubuntuLabel), nil
		case "arm64":
			return fmt.Sprintf("%s-aarch64-%s.AppImage", productName, ubuntuLabel), nil
		default:
			return "", fmt.Errorf("unsupported linux arch: %s", goarch)
		}
	default:
		return "", fmt.Errorf("unsupported OS: %s", goos)
	}
}

func linuxUbuntuLabel() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "u2404"
	}
	text := string(data)
	versionID := ""
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key != "VERSION_ID" {
			continue
		}
		versionID = strings.Trim(value, `"'`)
		break
	}
	if strings.HasPrefix(versionID, "22.") {
		return "u2204"
	}
	return "u2404"
}

func displayVersion(tagName string) string {
	version := strings.TrimSpace(tagName)
	if version == "" {
		return "unknown"
	}
	if !strings.HasPrefix(strings.ToUpper(version), "V") {
		version = "V" + version
	}
	return version
}

func cleanVersion(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(strings.ToLower(version)), "v")
	return strings.Split(version, " ")[0]
}

func compareVersions(v1, v2 string) int {
	parts1 := strings.Split(cleanVersion(v1), ".")
	parts2 := strings.Split(cleanVersion(v2), ".")
	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for i := 0; i < maxLen; i++ {
		val1 := 0
		if i < len(parts1) {
			fmt.Sscanf(parts1[i], "%d", &val1)
		}
		val2 := 0
		if i < len(parts2) {
			fmt.Sscanf(parts2[i], "%d", &val2)
		}
		if val1 > val2 {
			return 1
		}
		if val1 < val2 {
			return -1
		}
	}
	return 0
}
