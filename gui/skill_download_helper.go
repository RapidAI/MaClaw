package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// skillHubDownloadTrace records which HubCenter node actually served a package
// download (after cluster failover). Surfaced in MaClaw App dependency diagnostics.
type skillHubDownloadTrace struct {
	UsedBase            string   // base URL of the node that served bytes
	ResolvedDownloadURL string   // full URL used on success
	PreferredLocator    string   // original package_download_url / absolute hint
	Candidates          []string // candidate bases considered for this attempt
	Path                string   // request path (e.g. /api/v1/skills/X/download)
}

func (t skillHubDownloadTrace) withResolvedURL() skillHubDownloadTrace {
	if t.UsedBase == "" || t.Path == "" {
		return t
	}
	base := strings.TrimRight(strings.TrimSpace(t.UsedBase), "/")
	path := t.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	t.ResolvedDownloadURL = base + path
	return t
}

// downloadSkillJSONFromHubCenter fetches a skill definition through the
// current HubCenter discovery/failover pool.
func downloadSkillJSONFromHubCenter(ctx context.Context, app *App, path string) (*corelib.NLSkillEntry, error) {
	return downloadSkillJSONFromHubCenterToDir(ctx, app, path, "")
}

func downloadSkillJSONFromHubCenterToDir(ctx context.Context, app *App, path, targetDir string) (*corelib.NLSkillEntry, error) {
	return downloadSkillJSONFromHubCenterToDirWithIntegrity(ctx, app, path, targetDir, "", "")
}

func downloadSkillJSONFromHubCenterToDirWithIntegrity(ctx context.Context, app *App, path, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	entry, _, err := downloadSkillJSONFromHubCenterLocatorToDirWithIntegrityTrace(ctx, app, "", path, targetDir, expectedSHA256, expectedSignature)
	return entry, err
}

func downloadSkillJSONFromHubCenterLocatorToDirWithIntegrity(ctx context.Context, app *App, locator, fallbackPath, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, error) {
	entry, _, err := downloadSkillJSONFromHubCenterLocatorToDirWithIntegrityTrace(ctx, app, locator, fallbackPath, targetDir, expectedSHA256, expectedSignature)
	return entry, err
}

func downloadSkillJSONFromHubCenterLocatorToDirWithIntegrityTrace(ctx context.Context, app *App, locator, fallbackPath, targetDir, expectedSHA256, expectedSignature string) (*corelib.NLSkillEntry, skillHubDownloadTrace, error) {
	trace := skillHubDownloadTrace{PreferredLocator: strings.TrimSpace(locator)}
	if app == nil {
		return nil, trace, fmt.Errorf("app is nil")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	data, downloadTrace, err := app.getHubCenterDownloadLocatorBytes(ctx, client, locator, fallbackPath, maxDownloadSize)
	trace = mergeSkillHubDownloadTrace(trace, downloadTrace)
	if err != nil {
		return nil, trace.withResolvedURL(), err
	}
	entry, err := decodeVerifiedDownloadedSkillPackage(app, data, targetDir, expectedSHA256, expectedSignature)
	if err == nil {
		return entry, trace.withResolvedURL(), nil
	}
	if strings.TrimSpace(locator) == "" || strings.TrimSpace(fallbackPath) == "" {
		return nil, trace.withResolvedURL(), err
	}
	fallbackData, fallbackTrace, fallbackErr := app.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, maxDownloadSize, err)
	trace = mergeSkillHubDownloadTrace(trace, fallbackTrace)
	if fallbackErr != nil {
		return nil, trace.withResolvedURL(), fallbackErr
	}
	entry, fallbackErr = decodeVerifiedDownloadedSkillPackage(app, fallbackData, targetDir, expectedSHA256, expectedSignature)
	if fallbackErr != nil {
		return nil, trace.withResolvedURL(), fmt.Errorf("%v; fallback package verification failed: %w", err, fallbackErr)
	}
	return entry, trace.withResolvedURL(), nil
}

func mergeSkillHubDownloadTrace(base, next skillHubDownloadTrace) skillHubDownloadTrace {
	if next.UsedBase != "" {
		base.UsedBase = next.UsedBase
	}
	if next.ResolvedDownloadURL != "" {
		base.ResolvedDownloadURL = next.ResolvedDownloadURL
	}
	if next.Path != "" {
		base.Path = next.Path
	}
	if next.PreferredLocator != "" && base.PreferredLocator == "" {
		base.PreferredLocator = next.PreferredLocator
	}
	if len(next.Candidates) > 0 {
		// Union candidate lists so diagnostics show the full cluster pool tried.
		base.Candidates = remote.NormalizeHubCenterURLs(append(append([]string(nil), base.Candidates...), next.Candidates...))
	}
	return base
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

func (a *App) getHubCenterDownloadLocatorBytes(ctx context.Context, client *http.Client, locator, fallbackPath string, limit int64) ([]byte, skillHubDownloadTrace, error) {
	trace := skillHubDownloadTrace{PreferredLocator: strings.TrimSpace(locator)}
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return a.getHubCenterDownloadAcrossCandidates(ctx, client, fallbackPath, limit)
	}
	parsed, err := url.Parse(locator)
	if err != nil {
		return nil, trace, fmt.Errorf("invalid skill download locator %q: %w", locator, err)
	}
	if parsed.Scheme == "" {
		path := locator
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return a.getHubCenterDownloadAcrossCandidates(ctx, client, path, limit)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, fmt.Errorf("unsupported skill download locator scheme %q", parsed.Scheme))
	}

	bases, allowedBases, err := a.hubCenterDownloadAllowedBases(ctx, client)
	if err != nil {
		return nil, trace, err
	}
	if !hubCenterDownloadLocatorAllowed(locator, allowedBases) {
		return a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, fmt.Errorf("skill download locator host is not an active Hub or HubCenter: %s", parsed.Host))
	}

	// Absolute HubCenter URL is only a preferred host hint — never pin the download
	// to that single node. Rewrite the path onto the full cluster candidate pool so a
	// dead hubs2 (etc.) fails over to other live HubCenter nodes automatically.
	// Recently failed hosts are already deprioritized in bases / sticky ordering.
	path := hubCenterLocatorRequestPath(parsed)
	trace.Path = path
	ordered := orderHubCenterBasesPreferringHost(bases, parsed.Scheme, parsed.Host)
	trace.Candidates = append([]string(nil), ordered...)
	usedBase, _, data, downloadErr := a.getHubCenterBytesFromCandidates(ctx, client, ordered, path, limit)
	if downloadErr == nil {
		if usedBase != "" {
			trace.UsedBase = usedBase
		}
		return data, trace.withResolvedURL(), nil
	}

	// First pass exhausted the candidate pool. Invalidate discovery cache and try a
	// freshly resolved order (failure memory may have re-ranked live nodes).
	if a.hubCenterCache != nil && ctx.Err() == nil {
		a.hubCenterCache.Invalidate()
	}
	freshBases, resolveErr := a.resolveHubCenterCandidates(ctx, client)
	if resolveErr == nil && len(freshBases) > 0 {
		// Do not re-apply sticky host prefer after a failed pass.
		freshOrdered := remote.NormalizeHubCenterURLs(freshBases)
		if !remote.StringSliceEqual(freshOrdered, ordered) {
			trace.Candidates = remote.NormalizeHubCenterURLs(append(append([]string(nil), trace.Candidates...), freshOrdered...))
			usedBase, _, data, retryErr := a.getHubCenterBytesFromCandidates(ctx, client, freshOrdered, path, limit)
			if retryErr == nil {
				if usedBase != "" {
					trace.UsedBase = usedBase
				}
				return data, trace.withResolvedURL(), nil
			}
			downloadErr = retryErr
		}
	}

	// Path may differ from the installer's canonical skill-id download route.
	if fallbackPath = strings.TrimSpace(fallbackPath); fallbackPath != "" && !hubCenterDownloadPathsEqual(path, fallbackPath) {
		data, fallbackTrace, fallbackErr := a.getHubCenterDownloadFallbackBytes(ctx, client, fallbackPath, limit, downloadErr)
		return data, mergeSkillHubDownloadTrace(trace, fallbackTrace), fallbackErr
	}
	return nil, trace.withResolvedURL(), downloadErr
}

// getHubCenterDownloadAcrossCandidates downloads a HubCenter-relative path with
// multi-node failover and optional cache re-discovery after a total failure.
func (a *App) getHubCenterDownloadAcrossCandidates(ctx context.Context, client *http.Client, path string, limit int64) ([]byte, skillHubDownloadTrace, error) {
	trace := skillHubDownloadTrace{}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, trace, fmt.Errorf("hubcenter download path is empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	trace.Path = path
	usedBase, candidates, data, err := a.getHubCenterBytes(ctx, client, path, limit)
	trace.Candidates = append([]string(nil), candidates...)
	if err == nil {
		if usedBase != "" {
			trace.UsedBase = usedBase
		}
		return data, trace.withResolvedURL(), nil
	}
	if a.hubCenterCache == nil || ctx.Err() != nil {
		return nil, trace, err
	}
	// Total failure — re-discover in case the cache still preferred a dead node.
	a.hubCenterCache.Invalidate()
	freshBases, resolveErr := a.resolveHubCenterCandidates(ctx, client)
	if resolveErr != nil || len(freshBases) == 0 {
		return nil, trace, err
	}
	trace.Candidates = append([]string(nil), freshBases...)
	usedBase, _, data, retryErr := a.getHubCenterBytesFromCandidates(ctx, client, freshBases, path, limit)
	if retryErr != nil {
		// Prefer the rediscovery error when it is more specific than the first pass.
		return nil, trace, retryErr
	}
	if usedBase != "" {
		trace.UsedBase = usedBase
	}
	return data, trace.withResolvedURL(), nil
}

func (a *App) getHubCenterDownloadFallbackBytes(ctx context.Context, client *http.Client, fallbackPath string, limit int64, locatorErr error) ([]byte, skillHubDownloadTrace, error) {
	fallbackPath = strings.TrimSpace(fallbackPath)
	if fallbackPath == "" {
		return nil, skillHubDownloadTrace{}, locatorErr
	}
	data, trace, fallbackErr := a.getHubCenterDownloadAcrossCandidates(ctx, client, fallbackPath, limit)
	if fallbackErr != nil {
		if locatorErr != nil {
			return nil, trace, fmt.Errorf("%v; fallback download failed: %w", locatorErr, fallbackErr)
		}
		return nil, trace, fallbackErr
	}
	return data, trace, nil
}

func (a *App) hubCenterDownloadAllowedBases(ctx context.Context, client *http.Client) (candidates []string, allowed []string, err error) {
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return nil, nil, err
	}
	allowed = append([]string{}, bases...)
	// Always allow built-in public HubCenter hosts even if discovery temporarily
	// returned a short list (e.g. sticky preferred node that is currently down).
	allowed = append(allowed, remote.DefaultRemoteHubCenterURLs...)
	if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
		allowed = append(allowed, cfg.RemoteHubURL, cfg.RemoteHubCenterURL)
		allowed = append(allowed, cfg.RemoteHubCenterURLs...)
	}
	return bases, remote.NormalizeHubCenterURLs(allowed), nil
}

func hubCenterLocatorRequestPath(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		return path + "?" + u.RawQuery
	}
	return path
}

func hubCenterDownloadPathsEqual(a, b string) bool {
	normalize := func(p string) string {
		p = strings.TrimSpace(p)
		if p == "" {
			return ""
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		return p
	}
	return normalize(a) == normalize(b)
}

// orderHubCenterBasesPreferringHost puts the locator's host first (if present),
// then keeps the remaining discovery order for cluster failover.
//
// If that sticky host has recent probe failures (e.g. hubs2 just timed out), do
// not force it first again — discovery order already demoted it, and re-pinning
// would make every install burn a per-node timeout on a known-dead node.
// Always re-apply failure deprioritization so a sticky prefer cannot leave known-dead
// hosts ahead of clean ones.
func orderHubCenterBasesPreferringHost(bases []string, scheme, host string) []string {
	bases = remote.NormalizeHubCenterURLs(bases)
	if len(bases) == 0 {
		return nil
	}
	preferred := hubCenterBaseForHost(bases, scheme, host)
	if preferred == "" || remote.HasRecentFailures(preferred) {
		return deprioritizeRecentlyFailedHubCenters(bases)
	}
	out := make([]string, 0, len(bases))
	out = append(out, preferred)
	for _, base := range bases {
		if strings.EqualFold(base, preferred) {
			continue
		}
		out = append(out, base)
	}
	return deprioritizeRecentlyFailedHubCenters(out)
}

func hubCenterBaseForHost(bases []string, scheme, host string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	for _, base := range remote.NormalizeHubCenterURLs(bases) {
		candidate, err := url.Parse(base)
		if err != nil || candidate.Host == "" {
			continue
		}
		if scheme != "" && !strings.EqualFold(candidate.Scheme, scheme) {
			continue
		}
		if strings.EqualFold(candidate.Host, host) {
			return base
		}
	}
	return ""
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
