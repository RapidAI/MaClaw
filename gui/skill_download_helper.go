package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// downloadSkillJSONFromHubCenter fetches a skill definition through the
// current HubCenter discovery/failover pool.
func downloadSkillJSONFromHubCenter(ctx context.Context, app *App, path string) (*corelib.NLSkillEntry, error) {
	return downloadSkillJSONFromHubCenterToDir(ctx, app, path, "")
}

func downloadSkillJSONFromHubCenterToDir(ctx context.Context, app *App, path, targetDir string) (*corelib.NLSkillEntry, error) {
	return downloadSkillJSONFromHubCenterToDirWithIntegrity(ctx, app, path, targetDir, "", "")
}

func downloadSkillJSONFromHubCenterToDirWithIntegrity(ctx context.Context, app *App, path, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	return downloadSkillJSONFromHubCenterLocatorToDirWithIntegrity(ctx, app, "", path, targetDir, expectedSHA256, expectedSignature)
}

func downloadSkillJSONFromHubCenterLocatorToDirWithIntegrity(ctx context.Context, app *App, locator, fallbackPath, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	if app == nil {
		return nil, fmt.Errorf("app is nil")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	data, err := app.getHubCenterDownloadLocatorBytes(ctx, client, locator, fallbackPath, maxDownloadSize)
	if err != nil {
		return nil, err
	}
	entry, err := decodeVerifiedDownloadedSkillPackage(app, data, targetDir, expectedSHA256, expectedSignature)
	if err == nil {
		return entry, nil
	}
	if strings.TrimSpace(locator) == "" || strings.TrimSpace(fallbackPath) == "" {
		return nil, err
	}
	fallbackData, fallbackErr := app.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, maxDownloadSize, err)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	entry, fallbackErr = decodeVerifiedDownloadedSkillPackage(app, fallbackData, targetDir, expectedSHA256, expectedSignature)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%v; fallback package verification failed: %w", err, fallbackErr)
	}
	return entry, nil
}

func decodeVerifiedDownloadedSkillPackage(app *App, data []byte, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	if err := verifyDownloadedSkillPackageSHA256(data, expectedSHA256); err != nil {
		return nil, err
	}
	if err := verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, expectedSignature, app.trustedSkillPackageKeyFingerprints()); err != nil {
		return nil, err
	}
	return decodeDownloadedSkillJSONToDir(data, targetDir)
}

func (a *App) getHubCenterDownloadLocatorBytes(ctx context.Context, client *http.Client, locator, fallbackPath string, limit int64) ([]byte, error) {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		_, _, data, err := a.getHubCenterBytes(ctx, client, fallbackPath, limit)
		return data, err
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return nil, fmt.Errorf("invalid skill download locator %q: %w", locator, err)
	}
	if parsed.Scheme == "" {
		path := locator
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		_, _, data, err := a.getHubCenterBytes(ctx, client, path, limit)
		return data, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, fmt.Errorf("unsupported skill download locator scheme %q", parsed.Scheme))
	}
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return nil, err
	}
	allowedBases := append([]string{}, bases...)
	if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
		allowedBases = append(allowedBases, cfg.RemoteHubURL, cfg.RemoteHubCenterURL)
		allowedBases = append(allowedBases, cfg.RemoteHubCenterURLs...)
	}
	if !hubCenterDownloadLocatorAllowed(locator, allowedBases) {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, fmt.Errorf("skill download locator host is not an active Hub or HubCenter: %s", parsed.Host))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}
	if limit > 0 {
		data, readErr := readLimitedHubCenterBody(resp.Body, limit)
		if readErr != nil {
			return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, readErr)
		}
		return data, nil
	}
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, readErr)
	}
	return data, nil
}

func (a *App) getHubCenterDownloadFallbackBytes(ctx context.Context, client *http.Client, fallbackPath string, limit int64, locatorErr error) ([]byte, error) {
	fallbackPath = strings.TrimSpace(fallbackPath)
	if fallbackPath == "" {
		return nil, locatorErr
	}
	_, _, data, fallbackErr := a.getHubCenterBytes(ctx, client, fallbackPath, limit)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%v; fallback download failed: %w", locatorErr, fallbackErr)
	}
	return data, nil
}

func hubCenterDownloadLocatorAllowed(locator string, bases []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(locator))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	for _, base := range remote.NormalizeHubCenterURLs(bases) {
		candidate, err := url.Parse(base)
		if err != nil || candidate.Scheme == "" || candidate.Host == "" {
			continue
		}
		if strings.EqualFold(parsed.Scheme, candidate.Scheme) && strings.EqualFold(parsed.Host, candidate.Host) {
			return true
		}
	}
	return false
}

func verifyDownloadedSkillPackageSHA256(data []byte, expectedSHA256 string) error {
	expected := normalizeExpectedPackageSHA256(expectedSHA256)
	if expected == "" {
		return nil
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("package integrity checksum mismatch: expected sha256 %s, got %s", expected, actual)
	}
	return nil
}

func normalizeExpectedPackageSHA256(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return ""
	}
	return value
}

type downloadedSkillPackageSignature struct {
	Algorithm            string `json:"algorithm,omitempty"`
	PublicKey            string `json:"public_key,omitempty"`
	PublicKeyB64         string `json:"public_key_base64,omitempty"`
	Signature            string `json:"signature,omitempty"`
	SignatureB64         string `json:"signature_base64,omitempty"`
	PackageSHA256        string `json:"package_sha256,omitempty"`
	PublicKeyFingerprint string `json:"public_key_fingerprint,omitempty"`
}

func verifyDownloadedSkillPackageSignature(data []byte, signatureValue string) error {
	return verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data, signatureValue, nil)
}

func (a *App) trustedSkillPackageKeyFingerprints() []string {
	if a == nil {
		return nil
	}
	cfg, err := a.LoadConfig()
	if err != nil || len(cfg.TrustedSkillPackageKeyFingerprints) == 0 {
		return nil
	}
	trusted := make([]string, 0, len(cfg.TrustedSkillPackageKeyFingerprints))
	for _, value := range cfg.TrustedSkillPackageKeyFingerprints {
		if normalized := normalizeDownloadedSkillPublicKeyFingerprint(value); normalized != "" {
			trusted = append(trusted, normalized)
		}
	}
	return trusted
}

func verifyDownloadedSkillPackageSignatureWithTrustedFingerprints(data []byte, signatureValue string, trustedFingerprints []string) error {
	sig, supported, err := parseDownloadedSkillPackageSignature(signatureValue)
	if err != nil || !supported {
		return err
	}
	if expected := normalizeExpectedPackageSHA256(sig.PackageSHA256); expected != "" {
		if err := verifyDownloadedSkillPackageSHA256(data, expected); err != nil {
			return err
		}
	}
	publicKey, err := decodeDownloadedSkillSignatureBytes(firstNonEmpty(sig.PublicKeyB64, sig.PublicKey))
	if err != nil {
		return fmt.Errorf("package integrity signature invalid public key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("package integrity signature invalid public key length: got %d", len(publicKey))
	}
	fingerprint := downloadedSkillPublicKeyFingerprint(publicKey)
	if declared := normalizeDownloadedSkillPublicKeyFingerprint(sig.PublicKeyFingerprint); declared != "" && declared != fingerprint {
		return fmt.Errorf("package integrity signature public key fingerprint mismatch: expected %s, got %s", declared, fingerprint)
	}
	if len(trustedFingerprints) > 0 && !downloadedSkillPublicKeyFingerprintTrusted(fingerprint, trustedFingerprints) {
		return fmt.Errorf("package integrity signature public key is not trusted: %s", fingerprint)
	}
	signature, err := decodeDownloadedSkillSignatureBytes(firstNonEmpty(sig.SignatureB64, sig.Signature))
	if err != nil {
		return fmt.Errorf("package integrity signature invalid signature bytes: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("package integrity signature invalid signature length: got %d", len(signature))
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), data, signature) {
		return fmt.Errorf("package integrity signature verification failed")
	}
	return nil
}

func parseDownloadedSkillPackageSignature(value string) (downloadedSkillPackageSignature, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return downloadedSkillPackageSignature{}, false, nil
	}
	if strings.HasPrefix(strings.ToLower(value), "ed25519:") {
		parts := strings.Split(value, ":")
		if len(parts) != 3 {
			return downloadedSkillPackageSignature{}, true, fmt.Errorf("package integrity signature has invalid ed25519 format")
		}
		return downloadedSkillPackageSignature{Algorithm: "ed25519", PublicKeyB64: parts[1], SignatureB64: parts[2]}, true, nil
	}
	if !strings.HasPrefix(value, "{") {
		return downloadedSkillPackageSignature{}, false, nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(value), &raw); err != nil {
		return downloadedSkillPackageSignature{}, true, fmt.Errorf("package integrity signature JSON is invalid: %w", err)
	}
	algorithm := strings.ToLower(strings.TrimSpace(stringFromAny(raw["algorithm"])))
	if algorithm != "ed25519" {
		return downloadedSkillPackageSignature{}, false, nil
	}
	return downloadedSkillPackageSignature{
		Algorithm:            algorithm,
		PublicKey:            stringFromAny(raw["public_key"]),
		PublicKeyB64:         stringFromAny(raw["public_key_base64"]),
		Signature:            stringFromAny(raw["signature"]),
		SignatureB64:         stringFromAny(raw["signature_base64"]),
		PackageSHA256:        stringFromAny(raw["package_sha256"]),
		PublicKeyFingerprint: firstNonEmpty(stringFromAny(raw["public_key_fingerprint"]), stringFromAny(raw["key_fingerprint"]), stringFromAny(raw["fingerprint"])),
	}, true, nil
}

func decodeDownloadedSkillSignatureBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "base64:")
	value = strings.TrimPrefix(value, "ed25519:")
	if value == "" {
		return nil, fmt.Errorf("empty value")
	}
	encodings := []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(value)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func downloadedSkillPublicKeyFingerprint(publicKey []byte) string {
	sum := sha256.Sum256(publicKey)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeDownloadedSkillPublicKeyFingerprint(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "sha256:")
	value = strings.TrimSpace(value)
	if len(value) != 64 {
		return ""
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return ""
	}
	return "sha256:" + value
}

func downloadedSkillPublicKeyFingerprintTrusted(fingerprint string, trusted []string) bool {
	fingerprint = normalizeDownloadedSkillPublicKeyFingerprint(fingerprint)
	if fingerprint == "" {
		return false
	}
	for _, candidate := range trusted {
		if normalizeDownloadedSkillPublicKeyFingerprint(candidate) == fingerprint {
			return true
		}
	}
	return false
}

func decodeDownloadedSkillJSON(data []byte) (*corelib.NLSkillEntry, error) {
	return decodeDownloadedSkillJSONToDir(data, "")
}

func normalizeDownloadedSkillVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || len(version) > 32 {
		return ""
	}
	first := version[0]
	if first != 'v' && first != 'V' && (first < '0' || first > '9') {
		return ""
	}
	for _, r := range version {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '.', '-', '_', '+':
			continue
		default:
			return ""
		}
	}
	return version
}

func decodeDownloadedSkillJSONToDir(data []byte, targetDir string) (*corelib.NLSkillEntry, error) {
	// Peek id for SkillID fallback when name is missing.
	var peek struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(data, &peek)
	skillID := firstNonEmpty(peek.ID, peek.Name)

	entry, err := cskill.ParseSkillHubDownloadJSON(data, cskill.HubDownloadOptions{
		SkillID:   skillID,
		Source:    "hub",
		TargetDir: targetDir,
	})
	if err != nil {
		return nil, err
	}
	// Preserve legacy hub version normalization used by capability install paths.
	entry.HubVersion = normalizeDownloadedSkillVersion(entry.HubVersion)
	return entry, nil
}
