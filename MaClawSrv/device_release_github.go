package main

// This file is the Hub's trust boundary for firmware *metadata*.  It is
// deliberately independent of the device protocol: the Hub downloads and
// verifies a complete, signed .clawfw archive, then persists only the small
// metadata needed for version reminders.  No firmware URL or byte ever flows
// from this code to an ESP32.

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	srvOfficialFirmwareRepository = "RapidAI/MaClaw"
	srvGitHubAPIBase              = "https://api.github.com"
	srvReleaseCatalogMaxArchive   = int64(64 * 1024 * 1024)
	srvReleaseCatalogMaxEntries   = 64
	srvReleaseCatalogMaxManifest  = int64(1024 * 1024)
	srvReleaseCatalogMaxFile      = int64(32 * 1024 * 1024)
	srvReleaseCatalogMaxAge       = 24 * time.Hour
	srvReleaseCatalogRefresh      = 15 * time.Minute
)

type srvOfficialFirmwareProfile struct {
	assetName string
	boardID   string
	profileID string
}

var srvOfficialFirmwareProfiles = []srvOfficialFirmwareProfile{
	{assetName: "MaClaw-ESP32S3-EchoEar-2ST-firmware.clawfw", boardID: "echoear-2st-r8", profileID: "echoear-2st"},
	{assetName: "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw", boardID: "bread-compact-wifi-lcd-v1", profileID: "bread-compact"},
	{assetName: "MaClaw-ESP32S3-Fangtang-4G-firmware.clawfw", boardID: "fangtang-4g-v1", profileID: "fangtang-4g"},
}

type srvGitHubReleaseCatalog struct {
	catalog      *srvDeviceUpdateCatalog
	client       *http.Client
	apiBase      string
	keyID        string
	publicKey    ed25519.PublicKey
	minimumMaker string
	stop         context.CancelFunc
}

type srvGitHubReleaseResponse struct {
	ID          int64                   `json:"id"`
	TagName     string                  `json:"tag_name"`
	Draft       bool                    `json:"draft"`
	Prerelease  bool                    `json:"prerelease"`
	PublishedAt time.Time               `json:"published_at"`
	Assets      []srvGitHubReleaseAsset `json:"assets"`
}

type srvGitHubReleaseAsset struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Size               int64     `json:"size"`
	UpdatedAt          time.Time `json:"updated_at"`
	BrowserDownloadURL string    `json:"browser_download_url"`
}

// These are the signed fields consumed by the Hub.  Use DisallowUnknownFields
// when decoding them: a schema addition is a deliberate compatibility review,
// not a silently ignored security-relevant manifest field.
type srvReleaseManifest struct {
	SchemaVersion    int                      `json:"schemaVersion"`
	PackageID        string                   `json:"packageId"`
	ReleaseVersion   string                   `json:"releaseVersion"`
	Board            srvReleaseManifestBoard  `json:"board"`
	Chip             srvReleaseManifestChip   `json:"chip"`
	SecurityBaseline srvReleaseSecurityBase   `json:"securityBaseline"`
	Layout           srvReleaseManifestLayout `json:"layout"`
	Mode             string                   `json:"mode"`
	WriteOrder       []string                 `json:"writeOrder,omitempty"`
	AppIdentity      srvReleaseAppIdentity    `json:"appIdentity"`
	BootVerification srvReleaseBootPolicy     `json:"bootVerification"`
	Files            []srvReleaseManifestFile `json:"files"`
}

type srvReleaseManifestBoard struct {
	ID          string `json:"id"`
	ProfileHash string `json:"profileHash"`
}
type srvReleaseManifestChip struct {
	Family     string `json:"family"`
	FlashBytes int64  `json:"flashBytes"`
}
type srvReleaseSecurityBase struct {
	SecureBoot      bool `json:"secureBoot"`
	FlashEncryption bool `json:"flashEncryption"`
	SecureVersion   int  `json:"secureVersion"`
}
type srvReleaseManifestLayout struct {
	ID                 string `json:"id"`
	Fingerprint        string `json:"fingerprint"`
	PartitionTablePath string `json:"partitionTablePath"`
}
type srvReleaseAppIdentity struct {
	ProjectName     string `json:"projectName"`
	AppVersion      string `json:"appVersion"`
	ELFSHA256       string `json:"elfSha256"`
	ReleaseSequence int64  `json:"releaseSequence"`
	PSRAMBytes      int64  `json:"psramBytes"`
}
type srvReleaseBootPolicy struct {
	Baud              int      `json:"baud"`
	RequiredSelfTests []string `json:"requiredSelfTests"`
	TimeoutSeconds    int      `json:"timeoutSeconds"`
}
type srvReleaseManifestFile struct {
	Name   string  `json:"name,omitempty"`
	Path   string  `json:"path"`
	Size   int64   `json:"size"`
	SHA256 string  `json:"sha256"`
	Offset *uint64 `json:"offset,omitempty"`
	Region string  `json:"region,omitempty"`
}
type srvReleaseSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

type srvVerifiedReleaseArchive struct {
	manifest       srvReleaseManifest
	manifestSHA256 string
	archiveSHA256  string
}

func newSrvGitHubReleaseCatalog(catalog *srvDeviceUpdateCatalog, client *http.Client, apiBase, keyID, publicKeyBase64, minimumMaker string) (*srvGitHubReleaseCatalog, error) {
	if catalog == nil || strings.TrimSpace(keyID) == "" {
		return nil, errors.New("trusted release catalog needs a catalog and signing key ID")
	}
	keyRaw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil || len(keyRaw) != ed25519.PublicKeySize {
		return nil, errors.New("trusted release catalog has an invalid Ed25519 public key")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(apiBase), "/"))
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("trusted release catalog has an invalid GitHub API base URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	if client.CheckRedirect == nil {
		copyClient := *client
		copyClient.CheckRedirect = validateSrvGitHubReleaseRedirect
		client = &copyClient
	}
	return &srvGitHubReleaseCatalog{catalog: catalog, client: client, apiBase: strings.TrimRight(base.String(), "/"), keyID: strings.TrimSpace(keyID), publicKey: ed25519.PublicKey(keyRaw), minimumMaker: strings.TrimSpace(minimumMaker)}, nil
}

// github.com uses redirects for Release assets.  Do not allow a malformed
// response or transport configuration to turn that necessary redirect into an
// arbitrary firmware source.
func validateSrvGitHubReleaseRedirect(next *http.Request, via []*http.Request) error {
	if next == nil || next.URL == nil || next.URL.Scheme != "https" || next.URL.User != nil || next.URL.Fragment != "" {
		return errors.New("unsafe GitHub Release redirect")
	}
	switch strings.ToLower(next.URL.Hostname()) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return errors.New("GitHub Release redirect target is not allow-listed")
	}
}

func newSrvGitHubReleaseCatalogFromEnv(catalog *srvDeviceUpdateCatalog) (*srvGitHubReleaseCatalog, error) {
	if !srvEnvBool("MACLAW_GITHUB_RELEASE_CATALOG_ENABLED") {
		return nil, nil
	}
	return newSrvGitHubReleaseCatalog(catalog, nil, srvGitHubAPIBase,
		os.Getenv("CLAWMATE_FIRMWARE_SIGNING_KEY_ID"), os.Getenv("CLAWMATE_FIRMWARE_PUBLIC_KEY"),
		os.Getenv("MACLAW_MINIMUM_MAKER_VERSION"))
}

func srvEnvBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (p *srvGitHubReleaseCatalog) start() {
	if p == nil || p.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.stop = cancel
	go func() {
		p.refreshWithTimeout(ctx)
		ticker := time.NewTicker(srvReleaseCatalogRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.refreshWithTimeout(ctx)
			}
		}
	}()
}

func (p *srvGitHubReleaseCatalog) close() {
	if p != nil && p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

func (p *srvGitHubReleaseCatalog) refreshWithTimeout(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Minute)
	defer cancel()
	if err := p.refresh(ctx); err != nil {
		// Keep the last verified document. latestFor enforces its max age, so an
		// outage cannot turn into an indefinite stale update notification.
		fmt.Printf("[release-catalog] GitHub refresh rejected: %v\n", err)
	}
}

func (p *srvGitHubReleaseCatalog) refresh(ctx context.Context) error {
	if p == nil || p.catalog == nil {
		return errors.New("trusted release catalog is unavailable")
	}
	release, err := p.fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	if release.Draft || release.Prerelease || release.ID <= 0 || strings.TrimSpace(release.TagName) == "" || release.PublishedAt.IsZero() {
		return errors.New("GitHub latest release is draft, prerelease, or incomplete")
	}
	assets := make(map[string]srvGitHubReleaseAsset, len(release.Assets))
	for _, asset := range release.Assets {
		if asset.ID <= 0 || asset.Size <= 0 || asset.Size > srvReleaseCatalogMaxArchive || asset.Name == "" {
			return errors.New("GitHub release has an invalid asset")
		}
		if _, exists := assets[asset.Name]; exists {
			return fmt.Errorf("GitHub release contains duplicate asset %q", asset.Name)
		}
		assets[asset.Name] = asset
	}

	document := srvDeviceUpdateCatalogDocument{SchemaVersion: srvDeviceUpdateCatalogSchemaVersion, Source: "github-release", Repository: srvOfficialFirmwareRepository, ReleaseID: release.ID, ReleaseTag: release.TagName, VerifiedAt: time.Now().UTC().UnixMilli(), MaxAgeSeconds: int(srvReleaseCatalogMaxAge / time.Second)}
	previous, _ := p.catalog.document()
	for _, profile := range srvOfficialFirmwareProfiles {
		asset, ok := assets[profile.assetName]
		if !ok {
			return fmt.Errorf("GitHub release is missing official asset %q", profile.assetName)
		}
		if err := validateSrvGitHubAssetURL(asset.BrowserDownloadURL, release.TagName, asset.Name); err != nil {
			return err
		}
		archive, err := p.downloadAndVerify(ctx, asset)
		if err != nil {
			return fmt.Errorf("verify %s: %w", asset.Name, err)
		}
		releaseEntry, err := srvReleaseFromVerifiedArchive(profile, release, asset, archive, p.minimumMaker)
		if err != nil {
			return fmt.Errorf("validate %s: %w", asset.Name, err)
		}
		if err := rejectSrvPublishedAssetMutation(previous, releaseEntry); err != nil {
			return err
		}
		document.Releases = append(document.Releases, releaseEntry)
	}
	if err := validateSrvTrustedCatalogDocument(document, time.Now().UTC()); err != nil {
		return err
	}
	return p.catalog.replaceTrusted(document)
}

func (p *srvGitHubReleaseCatalog) fetchLatestRelease(ctx context.Context) (srvGitHubReleaseResponse, error) {
	var release srvGitHubReleaseResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBase+"/repos/"+srvOfficialFirmwareRepository+"/releases/latest", nil)
	if err != nil {
		return release, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := p.client.Do(req)
	if err != nil {
		return release, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return release, fmt.Errorf("GitHub latest-release status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&release); err != nil {
		return release, fmt.Errorf("decode GitHub latest release: %w", err)
	}
	return release, nil
}

func validateSrvGitHubAssetURL(raw, tag, assetName string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("GitHub asset URL is not a canonical HTTPS github.com URL")
	}
	expected := "/RapidAI/MaClaw/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(assetName)
	if u.EscapedPath() != expected {
		return fmt.Errorf("GitHub asset URL is not bound to release tag and name")
	}
	return nil
}

func (p *srvGitHubReleaseCatalog) downloadAndVerify(ctx context.Context, asset srvGitHubReleaseAsset) (srvVerifiedReleaseArchive, error) {
	var verified srvVerifiedReleaseArchive
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return verified, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return verified, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return verified, fmt.Errorf("GitHub asset status %d", resp.StatusCode)
	}
	if resp.ContentLength > srvReleaseCatalogMaxArchive {
		return verified, errors.New("GitHub asset exceeds maximum archive size")
	}
	if err := os.MkdirAll(filepath.Dir(p.catalog.path), 0700); err != nil {
		return verified, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.catalog.path), ".release-catalog-*.clawfw")
	if err != nil {
		return verified, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, srvReleaseCatalogMaxArchive+1))
	if err != nil {
		return verified, err
	}
	if n <= 0 || n > srvReleaseCatalogMaxArchive || (asset.Size > 0 && n != asset.Size) {
		return verified, errors.New("GitHub asset byte count does not match release metadata")
	}
	if err := tmp.Close(); err != nil {
		return verified, err
	}
	return verifySrvReleaseArchive(tmpPath, p.keyID, p.publicKey)
}

func verifySrvReleaseArchive(pathname, trustedKeyID string, publicKey ed25519.PublicKey) (srvVerifiedReleaseArchive, error) {
	var result srvVerifiedReleaseArchive
	info, err := os.Stat(pathname)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > srvReleaseCatalogMaxArchive {
		return result, errors.New("invalid firmware archive")
	}
	archive, err := zip.OpenReader(pathname)
	if err != nil {
		return result, fmt.Errorf("open firmware archive: %w", err)
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > srvReleaseCatalogMaxEntries {
		return result, errors.New("invalid firmware archive entry count")
	}
	entries := make(map[string]*zip.File, len(archive.File))
	var total uint64
	for _, entry := range archive.File {
		name, err := validateSrvArchivePath(entry.Name)
		if err != nil {
			return result, err
		}
		key := strings.ToLower(name)
		if _, exists := entries[key]; exists || entry.FileInfo().IsDir() || entry.FileInfo().Mode()&os.ModeSymlink != 0 || entry.UncompressedSize64 > uint64(srvReleaseCatalogMaxArchive)-total {
			return result, errors.New("unsafe or duplicate firmware archive entry")
		}
		total += entry.UncompressedSize64
		entries[key] = entry
	}
	manifestEntry, exists := entries["manifest.json"]
	if !exists || manifestEntry.UncompressedSize64 > uint64(srvReleaseCatalogMaxManifest) {
		return result, errors.New("firmware manifest is missing or too large")
	}
	manifestRaw, err := readSrvZipEntry(manifestEntry, srvReleaseCatalogMaxManifest)
	if err != nil {
		return result, err
	}
	manifest, err := decodeSrvReleaseManifest(manifestRaw)
	if err != nil {
		return result, err
	}
	signatureEntry, exists := entries["manifest.sig"]
	if !exists {
		return result, errors.New("firmware manifest signature is missing")
	}
	signatureRaw, err := readSrvZipEntry(signatureEntry, 16*1024)
	if err != nil {
		return result, err
	}
	var envelope srvReleaseSignature
	if err := rejectSrvDuplicateJSONKeys(signatureRaw); err != nil {
		return result, fmt.Errorf("firmware signature JSON is ambiguous: %w", err)
	}
	if err := decodeSrvStrictJSON(signatureRaw, &envelope); err != nil || envelope.Algorithm != "ed25519" || envelope.KeyID != trustedKeyID || envelope.Signature == "" {
		return result, errors.New("firmware manifest signature envelope is invalid or untrusted")
	}
	signature, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, manifestRaw, signature) {
		return result, errors.New("firmware manifest signature verification failed")
	}
	if err := verifySrvArchiveFiles(entries, manifest); err != nil {
		return result, err
	}
	archiveHash, err := srvSHA256File(pathname)
	if err != nil {
		return result, err
	}
	manifestHash := sha256.Sum256(manifestRaw)
	return srvVerifiedReleaseArchive{manifest: manifest, manifestSHA256: "sha256:" + hex.EncodeToString(manifestHash[:]), archiveSHA256: "sha256:" + archiveHash}, nil
}

func validateSrvArchivePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") || strings.Contains(name, "//") {
		return "", fmt.Errorf("unsafe firmware archive path %q", name)
	}
	for _, b := range []byte(name) {
		if !(b == '/' || b == '.' || b == '-' || b == '_' || (b >= '0' && b <= '9') || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')) {
			return "", fmt.Errorf("firmware archive path is not portable ASCII: %q", name)
		}
	}
	return name, nil
}

func readSrvZipEntry(entry *zip.File, maximum int64) ([]byte, error) {
	if entry == nil || entry.UncompressedSize64 > uint64(maximum) {
		return nil, errors.New("firmware archive entry exceeds limit")
	}
	r, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	raw, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, errors.New("read firmware archive entry failed or exceeded limit")
	}
	return raw, nil
}

func decodeSrvReleaseManifest(raw []byte) (srvReleaseManifest, error) {
	var manifest srvReleaseManifest
	if !utf8.Valid(raw) || !json.Valid(raw) || (len(raw) >= 3 && raw[0] == 0xef && raw[1] == 0xbb && raw[2] == 0xbf) {
		return manifest, errors.New("firmware manifest is not canonical JSON")
	}
	if err := rejectSrvDuplicateJSONKeys(raw); err != nil {
		return manifest, fmt.Errorf("firmware manifest JSON is ambiguous: %w", err)
	}
	if err := decodeSrvStrictJSON(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("decode firmware manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 || !validSrvCatalogString(manifest.PackageID, 256) || !validSrvCatalogString(manifest.ReleaseVersion, 128) || !validSrvCatalogString(manifest.Board.ID, 128) || !validSrvCatalogString(manifest.Layout.ID, 128) || !validSrvCatalogString(manifest.Layout.Fingerprint, 256) || !validSrvCatalogString(manifest.Layout.PartitionTablePath, 256) || manifest.AppIdentity.ReleaseSequence <= 0 || !validSrvCatalogString(manifest.AppIdentity.AppVersion, 128) || !validSrvSHA256(manifest.AppIdentity.ELFSHA256) || manifest.Chip.FlashBytes != 16*1024*1024 || !strings.EqualFold(manifest.Chip.Family, "esp32s3") || manifest.SecurityBaseline.SecureBoot || manifest.SecurityBaseline.FlashEncryption || manifest.SecurityBaseline.SecureVersion != 0 || (manifest.Mode != "full" && manifest.Mode != "app-only") || len(manifest.Files) == 0 {
		return manifest, errors.New("firmware manifest has an unsupported identity or security baseline")
	}
	return manifest, nil
}

func decodeSrvStrictJSON(raw []byte, out any) error {
	if !utf8.Valid(raw) {
		return errors.New("JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

func rejectSrvDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if err := walkSrvJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}

func walkSrvJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := walkSrvJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkSrvJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func verifySrvArchiveFiles(entries map[string]*zip.File, manifest srvReleaseManifest) error {
	listed := make(map[string]bool, len(manifest.Files))
	for _, spec := range manifest.Files {
		name, err := validateSrvArchivePath(spec.Path)
		if err != nil || spec.Size <= 0 || spec.Size > srvReleaseCatalogMaxFile || !validSrvSHA256(spec.SHA256) || strings.TrimSpace(spec.Region) == "" {
			return errors.New("firmware manifest has an invalid file specification")
		}
		if spec.Region == "metadata" {
			if spec.Offset != nil {
				return errors.New("firmware metadata unexpectedly has a flash offset")
			}
		} else if spec.Offset == nil || *spec.Offset%0x1000 != 0 {
			return errors.New("firmware image has an invalid flash offset")
		}
		key := strings.ToLower(name)
		entry, exists := entries[key]
		if listed[key] || !exists || int64(entry.UncompressedSize64) != spec.Size {
			return errors.New("firmware manifest does not exactly describe archive entries")
		}
		listed[key] = true
		hash, err := srvSHA256ZipEntry(entry, spec.Size)
		if err != nil || !strings.EqualFold(hash, strings.TrimPrefix(strings.ToLower(strings.TrimSpace(spec.SHA256)), "sha256:")) {
			return errors.New("firmware archive entry hash does not match signed manifest")
		}
	}
	for key := range entries {
		if key != "manifest.json" && key != "manifest.sig" && !listed[key] {
			return errors.New("firmware archive has an unlisted entry")
		}
	}
	if err := validateSrvReleaseInstallPlan(manifest); err != nil {
		return err
	}
	return nil
}

func validateSrvReleaseInstallPlan(manifest srvReleaseManifest) error {
	metadataFound := false
	images := make(map[string]srvReleaseManifestFile)
	for _, file := range manifest.Files {
		if file.Path == manifest.Layout.PartitionTablePath {
			if file.Region != "metadata" || file.Offset != nil {
				return errors.New("release partition table metadata is invalid")
			}
			metadataFound = true
		}
		if file.Region != "metadata" {
			if file.Name == "" || images[file.Name].Path != "" {
				return errors.New("release image names must be unique and non-empty")
			}
			images[file.Name] = file
		}
	}
	if !metadataFound || manifest.BootVerification.Baud <= 0 || manifest.BootVerification.TimeoutSeconds <= 0 || len(manifest.BootVerification.RequiredSelfTests) == 0 || manifest.AppIdentity.PSRAMBytes <= 0 || !validSrvCatalogString(manifest.AppIdentity.ProjectName, 128) {
		return errors.New("release manifest lacks mandatory installation policy")
	}
	switch manifest.Mode {
	case "app-only":
		if len(manifest.WriteOrder) != 0 || len(images) == 0 {
			return errors.New("app-only release has an invalid signed write plan")
		}
		for _, image := range images {
			if image.Region != "app" {
				return errors.New("app-only release contains a non-app image")
			}
		}
	case "full":
		if len(manifest.WriteOrder) == 0 {
			// Legacy full images are still signature-valid for import tooling, but
			// never allowed into the Hub's current update catalog.
			return errors.New("trusted release must use a split signed write plan")
		}
		if len(manifest.WriteOrder) != len(images) || len(images) < 3 {
			return errors.New("full release has an incomplete signed write plan")
		}
		seen := map[string]bool{}
		for index, name := range manifest.WriteOrder {
			image, exists := images[name]
			if !exists || seen[name] {
				return errors.New("full release writeOrder is not an exact image list")
			}
			seen[name] = true
			if (name == "bootloader" || name == "partition-table") && index < len(manifest.WriteOrder)-2 {
				return errors.New("full release writes boot-critical images too early")
			}
			if name == "partition-table" && (image.Offset == nil || *image.Offset != 0x8000 || image.Region != "partition-table") {
				return errors.New("full release has an invalid partition-table image")
			}
		}
		if len(manifest.WriteOrder) < 2 || manifest.WriteOrder[len(manifest.WriteOrder)-2] != "partition-table" || manifest.WriteOrder[len(manifest.WriteOrder)-1] != "bootloader" {
			return errors.New("full release boot-critical write order is invalid")
		}
	default:
		return errors.New("unsupported release install mode")
	}
	return nil
}

func srvSHA256ZipEntry(entry *zip.File, expected int64) (string, error) {
	r, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, expected+1))
	if err != nil || n != expected {
		return "", errors.New("firmware archive entry has unexpected length")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func srvSHA256File(pathname string) (string, error) {
	f, err := os.Open(pathname)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func srvReleaseFromVerifiedArchive(profile srvOfficialFirmwareProfile, github srvGitHubReleaseResponse, asset srvGitHubReleaseAsset, verified srvVerifiedReleaseArchive, minimumMaker string) (srvDeviceUpdateRelease, error) {
	m := verified.manifest
	if m.Board.ID != profile.boardID || m.Board.ProfileHash != "catalog:"+profile.profileID {
		return srvDeviceUpdateRelease{}, errors.New("signed package board/profile binding is not official")
	}
	if m.ReleaseVersion != github.TagName || m.AppIdentity.ReleaseSequence <= 0 {
		return srvDeviceUpdateRelease{}, errors.New("signed package version does not match GitHub release")
	}
	compat := "maclaw-clawmate:" + m.Board.ID + ":" + m.Layout.ID
	return srvDeviceUpdateRelease{ProductID: "maclaw-clawmate", BoardID: m.Board.ID, HardwareRev: "1", LayoutID: m.Layout.ID, CompatibilityID: compat, Channel: srvDeviceUpdateChannel, ReleaseSequence: m.AppIdentity.ReleaseSequence, DisplayVersion: m.AppIdentity.AppVersion, ReleaseTag: github.TagName, PublishedAt: github.PublishedAt.UTC().UnixMilli(), Severity: "normal", MinimumMakerVersion: minimumMaker, PackageID: m.PackageID, ManifestSHA256: verified.manifestSHA256, ArchiveSHA256: verified.archiveSHA256, SourceAssetID: asset.ID, SourceAssetName: asset.Name, SourceAssetSize: asset.Size, NotesSummary: "官方发布 " + github.TagName, NotesSHA256: "sha256:" + srvSHA256Text(github.TagName), CheckAfterSeconds: 6 * 60 * 60}, nil
}

func srvSHA256Text(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func rejectSrvPublishedAssetMutation(previous srvDeviceUpdateCatalogDocument, candidate srvDeviceUpdateRelease) error {
	if previous.SchemaVersion != srvDeviceUpdateCatalogSchemaVersion || previous.Repository != srvOfficialFirmwareRepository || previous.ReleaseTag != candidate.ReleaseTag {
		return nil
	}
	for _, prior := range previous.Releases {
		if prior.SourceAssetName != candidate.SourceAssetName {
			continue
		}
		if prior.SourceAssetID != candidate.SourceAssetID || prior.SourceAssetSize != candidate.SourceAssetSize || !strings.EqualFold(prior.ArchiveSHA256, candidate.ArchiveSHA256) || !strings.EqualFold(prior.ManifestSHA256, candidate.ManifestSHA256) {
			return fmt.Errorf("published GitHub asset mutation detected for %s", candidate.SourceAssetName)
		}
	}
	return nil
}
