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

	"clawmatemaker/internal/logging"
)

const latestReleaseURL = "https://api.github.com/repos/" + Repository + "/releases/latest"
const releasesURL = "https://api.github.com/repos/" + Repository + "/releases?per_page=30"
const maxAssetBytes int64 = 128 * 1024 * 1024

const cacheLockStaleAfter = 30 * time.Minute
const cacheLockRetryInterval = 100 * time.Millisecond

type releaseResponse struct {
	TagName     string         `json:"tag_name"`
	PublishedAt time.Time      `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}
type releaseAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"`
}

type DownloadedRelease struct {
	JobID string `json:"jobId,omitempty"`
	// PackageRef is an opaque, in-memory capability issued by the desktop
	// application after validation. It is the only firmware handle that the UI
	// may submit to the irreversible flash endpoint.
	PackageRef  string `json:"packageRef,omitempty"`
	BoardID     string `json:"boardId"`
	BoardName   string `json:"boardName"`
	ReleaseTag  string `json:"releaseTag"`
	PublishedAt string `json:"publishedAt,omitempty"`
	AssetName   string `json:"assetName"`
	// Path is intentionally not serialized across the Wails boundary. A
	// browser renderer must not learn or submit arbitrary host file paths.
	Path              string `json:"-"`
	Size              int64  `json:"size"`
	SHA256            string `json:"sha256"`
	GitHubDigest      string `json:"githubDigest,omitempty"`
	InstallStatus     string `json:"installStatus"`
	InstallPlan       string `json:"installPlan,omitempty"`
	PreservesUserData bool   `json:"preservesUserData"`
	RequiresRecovery  bool   `json:"requiresRecovery"`
	SafetyNote        string `json:"safetyNote"`
}
type Client struct {
	cacheDir string
	http     *http.Client
	apiURL   string
}

func NewClient(cacheDir string) *Client {
	return &Client{cacheDir: cacheDir, http: &http.Client{Timeout: 90 * time.Second}, apiURL: latestReleaseURL}
}

func (c *Client) DownloadLatest(ctx context.Context, boardID string, emit func(logging.Event)) (DownloadedRelease, error) {
	profile, err := Profile(boardID)
	if err != nil {
		return DownloadedRelease{}, err
	}
	if c == nil || c.http == nil {
		return DownloadedRelease{}, errors.New("release client is unavailable")
	}
	emitEvent(emit, logging.Info, "RELEASE_DISCOVERY_STARTED", "Discovering latest official firmware release.", map[string]any{"boardId": boardID})
	apiURL := c.apiURL
	if apiURL == "" {
		apiURL = latestReleaseURL
	}
	if apiURL != latestReleaseURL {
		return DownloadedRelease{}, errors.New("custom release endpoints are not permitted")
	}
	release, err := c.fetchRelease(ctx, apiURL)
	if err != nil {
		return DownloadedRelease{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return DownloadedRelease{}, errors.New("GitHub release has no tag")
	}
	asset := exactReleaseAsset(release, profile.AssetName)
	if asset == nil {
		emitEvent(emit, logging.Info, "RELEASE_LATEST_MISSING_ASSET", "Latest GitHub release does not yet contain this board's exact firmware asset; checking recent stable releases.", map[string]any{"boardId": boardID, "asset": profile.AssetName, "release": release.TagName})
		recent, listErr := c.fetchRecentReleases(ctx)
		if listErr != nil {
			return DownloadedRelease{}, fmt.Errorf("release %s does not include official asset %s; list recent releases: %w", release.TagName, profile.AssetName, listErr)
		}
		var found bool
		release, asset, found = newestStableReleaseWithAsset(recent, profile.AssetName)
		if !found {
			return DownloadedRelease{}, fmt.Errorf("no recent stable GitHub release includes official asset %s", profile.AssetName)
		}
		emitEvent(emit, logging.Info, "RELEASE_FALLBACK_SELECTED", "Selected the newest stable GitHub release containing the exact firmware asset.", map[string]any{"boardId": boardID, "asset": asset.Name, "release": release.TagName})
	}
	emitEvent(emit, logging.Info, "RELEASE_ASSET_SELECTED", "Selected exact allow-listed firmware asset.", map[string]any{"boardId": boardID, "asset": asset.Name, "release": release.TagName, "bytes": asset.Size})
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
	releaseLock, waited, err := acquireAssetLock(ctx, destination)
	if err != nil {
		return DownloadedRelease{}, err
	}
	defer releaseLock()
	if waited {
		emitEvent(emit, logging.Info, "RELEASE_CACHE_WAIT_COMPLETED", "Another download task completed access to this firmware cache entry; validating its result.", map[string]any{"boardId": boardID, "asset": asset.Name, "release": release.TagName})
	}
	if sum, size, ok := cachedAsset(destination, asset.Size, asset.Digest); ok {
		emitEvent(emit, logging.Info, "RELEASE_CACHE_HIT", "Using cached firmware after SHA-256 verification.", map[string]any{"boardId": boardID, "asset": asset.Name, "release": release.TagName, "bytes": size, "sha256": "sha256:" + sum, "cached": true})
		return DownloadedRelease{BoardID: profile.ID, BoardName: profile.Name, ReleaseTag: release.TagName, PublishedAt: release.PublishedAt.UTC().Format(time.RFC3339), AssetName: profile.AssetName, Path: destination, Size: size, SHA256: "sha256:" + sum, GitHubDigest: asset.Digest, InstallStatus: "downloaded_unverified", SafetyNote: "Using cached firmware. Signature and hardware compatibility must be verified before installation."}, nil
	}
	sum, size, err := c.download(ctx, asset.DownloadURL, destination, asset.Size)
	if err != nil {
		return DownloadedRelease{}, err
	}
	if asset.Digest != "" && !strings.EqualFold(asset.Digest, "sha256:"+sum) {
		_ = os.Remove(destination)
		return DownloadedRelease{}, errors.New("GitHub release digest does not match downloaded asset")
	}
	emitEvent(emit, logging.Info, "RELEASE_DOWNLOAD_COMPLETED", "Firmware downloaded and SHA-256 computed.", map[string]any{"boardId": boardID, "asset": asset.Name, "release": release.TagName, "bytes": size, "sha256": "sha256:" + sum})
	return DownloadedRelease{BoardID: profile.ID, BoardName: profile.Name, ReleaseTag: release.TagName, PublishedAt: release.PublishedAt.UTC().Format(time.RFC3339), AssetName: profile.AssetName, Path: destination, Size: size, SHA256: "sha256:" + sum, GitHubDigest: asset.Digest, InstallStatus: "downloaded_unverified", SafetyNote: "Download complete. Firmware signature and hardware compatibility must be verified before installation."}, nil
}

// acquireAssetLock uses an adjacent lock directory, which is atomically
// created by all supported desktop filesystems. Unlike an in-memory mutex it
// also prevents two running instances of the application from appending to a
// shared .part file. A lock is reclaimed only after a conservative timeout;
// package hash/signature validation remains mandatory after acquisition.
func acquireAssetLock(ctx context.Context, destination string) (release func(), waited bool, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := filepath.Clean(destination) + ".lock"
	for {
		err := os.Mkdir(lockPath, 0700)
		if err == nil {
			return func() { _ = os.Remove(lockPath) }, waited, nil
		}
		if !os.IsExist(err) {
			return nil, waited, fmt.Errorf("create firmware cache lock: %w", err)
		}
		waited = true
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > cacheLockStaleAfter {
			// The directory is intentionally empty. Remove never traverses
			// content, so it cannot affect the adjacent firmware package.
			_ = os.Remove(lockPath)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, waited, fmt.Errorf("wait for firmware cache lock: %w", ctx.Err())
		case <-time.After(cacheLockRetryInterval):
		}
	}
}

func (c *Client) fetchRelease(ctx context.Context, apiURL string) (releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return releaseResponse{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ClawMateMaker/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return releaseResponse{}, fmt.Errorf("discover GitHub release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseResponse{}, fmt.Errorf("discover GitHub release: HTTP %d", resp.StatusCode)
	}
	var release releaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release); err != nil {
		return releaseResponse{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	return release, nil
}

func (c *Client) fetchRecentReleases(ctx context.Context) ([]releaseResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ClawMateMaker/0.1")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list GitHub releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list GitHub releases: HTTP %d", resp.StatusCode)
	}
	var releases []releaseResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8*1024*1024)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode GitHub release list: %w", err)
	}
	return releases, nil
}

func exactReleaseAsset(release releaseResponse, name string) *releaseAsset {
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i]
		}
	}
	return nil
}

// newestStableReleaseWithAsset trusts GitHub's release-list ordering (newest
// first) only after filtering out drafts/prereleases and selecting the exact
// catalog asset name. The signed package verification remains mandatory.
func newestStableReleaseWithAsset(releases []releaseResponse, assetName string) (releaseResponse, *releaseAsset, bool) {
	for _, release := range releases {
		if release.Draft || release.Prerelease || strings.TrimSpace(release.TagName) == "" {
			continue
		}
		if asset := exactReleaseAsset(release, assetName); asset != nil {
			return release, asset, true
		}
	}
	return releaseResponse{}, nil, false
}

func cachedAsset(path string, expectedSize int64, expectedDigest string) (string, int64, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() != expectedSize || expectedSize <= 0 {
		return "", 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, maxAssetBytes+1)); err != nil {
		return "", 0, false
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if expectedDigest != "" && !strings.EqualFold(expectedDigest, "sha256:"+sum) {
		return "", 0, false
	}
	return sum, info.Size(), true
}

func emitEvent(emit func(logging.Event), severity logging.Severity, code, detail string, fields map[string]any) {
	if emit == nil {
		return
	}
	emit(logging.Event{Timestamp: time.Now().UTC(), Severity: severity, Stage: "download", Component: "catalog", Code: code, MessageKey: "catalog." + strings.ToLower(code), Detail: detail, Fields: logging.SafeFields(fields)})
}
func (c *Client) download(ctx context.Context, rawURL, destination string, expectedSize int64) (string, int64, error) {
	temporary := destination + ".part"
	resumeAt, err := resumeOffset(temporary, expectedSize)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "ClawMateMaker/0.1")
	if resumeAt > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeAt))
	}
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
	if resumeAt > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if sum, size, ok := cachedAsset(temporary, expectedSize, ""); ok {
			if err := os.Rename(temporary, destination); err != nil {
				return "", 0, err
			}
			return sum, size, nil
		}
		_ = os.Remove(temporary)
		return c.download(ctx, rawURL, destination, expectedSize)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", 0, fmt.Errorf("download release asset: HTTP %d", resp.StatusCode)
	}
	if resumeAt > 0 && resp.StatusCode != http.StatusPartialContent {
		// A server that ignores Range is safe to use only by restarting from
		// zero. Never append a whole asset to a partial cache file.
		_ = os.Remove(temporary)
		return c.download(ctx, rawURL, destination, expectedSize)
	}
	remaining := expectedSize - resumeAt
	if remaining <= 0 || remaining > maxAssetBytes || resp.ContentLength > remaining || resp.ContentLength > maxAssetBytes {
		return "", 0, errors.New("release asset content length is invalid")
	}
	if resumeAt > 0 && !validContentRange(resp.Header.Get("Content-Range"), resumeAt, expectedSize) {
		return "", 0, errors.New("release asset content range is invalid")
	}
	flags := os.O_CREATE | os.O_WRONLY
	if resumeAt > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(temporary, flags, 0600)
	if err != nil {
		return "", 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(resp.Body, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", 0, copyErr
	}
	if closeErr != nil {
		return "", 0, closeErr
	}
	if n != remaining || n+resumeAt > maxAssetBytes {
		return "", 0, fmt.Errorf("downloaded asset has unexpected size: %d", n+resumeAt)
	}
	sum, size, ok := cachedAsset(temporary, expectedSize, "")
	if !ok || size != expectedSize {
		return "", 0, errors.New("downloaded asset failed local size/hash validation")
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", 0, err
	}
	return sum, size, nil
}

func resumeOffset(path string, expectedSize int64) (int64, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.IsDir() || info.Size() < 0 || info.Size() >= expectedSize || info.Size() > maxAssetBytes {
		if err := os.Remove(path); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return info.Size(), nil
}

func validContentRange(value string, start, total int64) bool {
	var gotStart, gotEnd, gotTotal int64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "bytes %d-%d/%d", &gotStart, &gotEnd, &gotTotal); err != nil {
		return false
	}
	return gotStart == start && gotEnd >= gotStart && gotTotal == total
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
