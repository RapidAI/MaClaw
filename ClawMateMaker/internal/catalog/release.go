package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const latestReleaseURL = "https://api.github.com/repos/" + Repository + "/releases/latest"
const maxAssetBytes int64 = 128 * 1024 * 1024

type releaseResponse struct {
	TagName     string         `json:"tag_name"`
	PublishedAt time.Time      `json:"published_at"`
	Assets      []releaseAsset `json:"assets"`
}
type releaseAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"`
}

type DownloadedRelease struct {
	BoardID       string `json:"boardId"`
	BoardName     string `json:"boardName"`
	ReleaseTag    string `json:"releaseTag"`
	PublishedAt   string `json:"publishedAt,omitempty"`
	AssetName     string `json:"assetName"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	GitHubDigest  string `json:"githubDigest,omitempty"`
	InstallStatus string `json:"installStatus"`
	SafetyNote    string `json:"safetyNote"`
}
type Client struct {
	cacheDir string
	http     *http.Client
	apiURL   string
}

func NewClient(cacheDir string) *Client {
	return &Client{cacheDir: cacheDir, http: &http.Client{Timeout: 90 * time.Second}, apiURL: latestReleaseURL}
}

func (c *Client) DownloadLatest(ctx context.Context, boardID string) (DownloadedRelease, error) {
	profile, err := Profile(boardID)
	if err != nil {
		return DownloadedRelease{}, err
	}
	if c == nil || c.http == nil {
		return DownloadedRelease{}, errors.New("release client is unavailable")
	}
	apiURL := c.apiURL
	if apiURL == "" {
		apiURL = latestReleaseURL
	}
	if apiURL != latestReleaseURL {
		return DownloadedRelease{}, errors.New("custom release endpoints are not permitted")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return DownloadedRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ClawMateMaker/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return DownloadedRelease{}, fmt.Errorf("discover GitHub release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DownloadedRelease{}, fmt.Errorf("discover GitHub release: HTTP %d", resp.StatusCode)
	}
	var release releaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release); err != nil {
		return DownloadedRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return DownloadedRelease{}, errors.New("GitHub release has no tag")
	}
	var asset *releaseAsset
	for i := range release.Assets {
		if release.Assets[i].Name == profile.AssetName {
			asset = &release.Assets[i]
			break
		}
	}
	if asset == nil {
		return DownloadedRelease{}, fmt.Errorf("release %s does not include official asset %s", release.TagName, profile.AssetName)
	}
	if asset.Size <= 0 || asset.Size > maxAssetBytes {
		return DownloadedRelease{}, fmt.Errorf("release asset size %d is outside permitted bounds", asset.Size)
	}
	if !isGitHubReleaseURL(asset.DownloadURL) {
		return DownloadedRelease{}, errors.New("release asset URL is not an approved GitHub HTTPS host")
	}
	dir := filepath.Join(c.cacheDir, profile.ID, safeSegment(release.TagName))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return DownloadedRelease{}, fmt.Errorf("create firmware cache: %w", err)
	}
	destination := filepath.Join(dir, profile.AssetName)
	sum, size, err := c.download(ctx, asset.DownloadURL, destination, asset.Size)
	if err != nil {
		return DownloadedRelease{}, err
	}
	if asset.Digest != "" && !strings.EqualFold(asset.Digest, "sha256:"+sum) {
		return DownloadedRelease{}, errors.New("GitHub release digest does not match downloaded asset")
	}
	return DownloadedRelease{BoardID: profile.ID, BoardName: profile.Name, ReleaseTag: release.TagName, PublishedAt: release.PublishedAt.UTC().Format(time.RFC3339), AssetName: profile.AssetName, Path: destination, Size: size, SHA256: "sha256:" + sum, GitHubDigest: asset.Digest, InstallStatus: "downloaded_not_installable", SafetyNote: "This Workflow ZIP is not a signed .clawfw package. It was downloaded for inspection only and cannot be installed by the secure flasher."}, nil
}
func (c *Client) download(ctx context.Context, rawURL, destination string, expectedSize int64) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "ClawMateMaker/0.1")
	client := *c.http
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if !isGitHubReleaseURL(req.URL.String()) {
			return errors.New("redirected outside approved GitHub release hosts")
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("download release asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("download release asset: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxAssetBytes || (resp.ContentLength >= 0 && resp.ContentLength != expectedSize) {
		return "", 0, errors.New("release asset content length is invalid")
	}
	temporary := destination + ".part"
	defer os.Remove(temporary)
	out, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, maxAssetBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	if n != expectedSize || n > maxAssetBytes {
		return "", 0, fmt.Errorf("downloaded asset has unexpected size: %d", n)
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
func isGitHubReleaseURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || host == "objects.githubusercontent.com" || host == "release-assets.githubusercontent.com"
}
func safeSegment(value string) string {
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, value)
	if value == "" {
		return "unknown"
	}
	return value
}
