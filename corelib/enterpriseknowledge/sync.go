package enterpriseknowledge

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ErrSyncInProgress is returned when RunOnce is invoked while another cycle runs.
var ErrSyncInProgress = errors.New("enterprise sync already in progress")

// ErrSyncCredentialsMissing reports that the device has no usable Hub endpoint
// and viewer token, so a manually requested sync did not run.
var ErrSyncCredentialsMissing = errors.New("enterprise sync requires Hub URL and viewer token")

// maxPackageBytes caps a single digital-asset package download (DoS guard).
const maxPackageBytes = 512 << 20 // 512 MiB

// maxJSONResponseBytes caps Hub JSON responses (manifest / pull).
const maxJSONResponseBytes = 16 << 20 // 16 MiB

// maxPullPagesPerRun bounds catch-up work in one scheduled cycle. Hub returns
// ready changelog rows in pages of 50, so a single pull would otherwise leave
// a long-offline device behind until the next 30-minute cycle.
const maxPullPagesPerRun = 4

// SyncStatus is agent status for UI / health.
type SyncStatus struct {
	Running      bool   `json:"running"`
	Paused       bool   `json:"paused"`
	LastRunAt    string `json:"last_run_at"`
	LastError    string `json:"last_error"`
	LastOutcome  string `json:"last_outcome"`
	LibraryCount int    `json:"library_count"`
}

// HubAuthFunc resolves Hub base URL and viewer token for a sync cycle.
// Return empty hub/token or error to skip the cycle without hard failure.
type HubAuthFunc func() (hubURL, token string, err error)

// SyncAgent performs staggered Hub→local pulls for one Client.
type SyncAgent struct {
	client   *Client
	auth     HubAuthFunc
	deviceID string

	mu          sync.Mutex
	paused      bool
	running     bool
	lastRun     time.Time
	lastError   string
	lastOutcome string
	stopCh      chan struct{}
	started     bool
	tmpSeq      atomic.Uint64 // unique suffix for temp package files
	// runCancel cancels the in-flight RunOnce context (set while running).
	runCancel context.CancelFunc
}

// sharedSyncTransport is process-wide so Hub keep-alives are reused across cycles.
// Safe for concurrent use (*http.Transport is concurrent-safe).
var sharedSyncTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ResponseHeaderTimeout: 30 * time.Second,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 2 * time.Second,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   8,
	ForceAttemptHTTP2:     true,
}

// hubScopedHTTPClient rejects redirects that leave the Hub host (SSRF via Location).
func hubScopedHTTPClient(hubURL string) *http.Client {
	return &http.Client{
		Transport: sharedSyncTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			if req == nil || req.URL == nil {
				return fmt.Errorf("redirect missing url")
			}
			// Re-validate each hop against the original Hub origin.
			if err := validatePackageURL(hubURL, req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
	}
}

// NewSyncAgent builds a sync agent. deviceID should be stable per host (e.g. "srv-hostname").
// client must be non-nil with an open meta DB (Open / OpenMetaOnly).
func NewSyncAgent(client *Client, auth HubAuthFunc, deviceID string) *SyncAgent {
	if client == nil {
		return nil
	}
	if _, err := client.metaDB(); err != nil {
		return nil
	}
	if deviceID == "" {
		host, _ := os.Hostname()
		deviceID = "enterprise-" + host
	}
	if auth == nil {
		auth = func() (string, string, error) { return "", "", nil }
	}
	return &SyncAgent{
		client:   client,
		auth:     auth,
		deviceID: deviceID,
		stopCh:   make(chan struct{}),
	}
}

// StartBackground begins the staggered loop (idempotent while running).
// After Stop(), a subsequent StartBackground recreates stopCh and restarts the loop.
func (a *SyncAgent) StartBackground() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.started {
		// If stopCh is still open, loop is live.
		select {
		case <-a.stopCh:
			// stopped — fall through to restart
		default:
			a.mu.Unlock()
			return
		}
		a.stopCh = make(chan struct{})
	}
	a.started = true
	a.mu.Unlock()
	go a.loop()
}

// Stop ends the background loop and cancels any in-flight RunOnce HTTP work.
func (a *SyncAgent) Stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopCh != nil {
		select {
		case <-a.stopCh:
			// already closed
		default:
			close(a.stopCh)
		}
	}
	if a.runCancel != nil {
		a.runCancel()
	}
}

// SetPaused pauses/resumes automatic cycles (manual RunOnce still works).
func (a *SyncAgent) SetPaused(paused bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.paused = paused
	a.mu.Unlock()
}

// Status returns current agent status.
func (a *SyncAgent) Status() SyncStatus {
	st := SyncStatus{}
	if a == nil {
		return st
	}
	a.mu.Lock()
	st.Running = a.running
	st.Paused = a.paused
	if !a.lastRun.IsZero() {
		st.LastRunAt = a.lastRun.UTC().Format(time.RFC3339)
	}
	st.LastError = a.lastError
	st.LastOutcome = a.lastOutcome
	a.mu.Unlock()
	if a.client != nil {
		libs, _ := a.client.ListLibraries()
		st.LibraryCount = len(libs)
	}
	return st
}

// RunOnce executes one sync cycle. Returns ErrSyncInProgress if already running.
// Stop() cancels the active context so in-flight Hub HTTP calls abort promptly.
func (a *SyncAgent) RunOnce(ctx context.Context) (runErr error) {
	if a == nil || a.client == nil {
		return fmt.Errorf("sync agent not configured")
	}
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return ErrSyncInProgress
	}
	a.running = true
	a.lastOutcome = "running"
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.running = false
		a.lastRun = time.Now().UTC()
		if runErr != nil && a.lastOutcome == "running" {
			a.lastOutcome = "failed"
		} else if a.lastOutcome == "running" {
			a.lastOutcome = "completed"
		}
		a.runCancel = nil
		a.mu.Unlock()
	}()

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
	}
	// Child context cancelled by Stop() via runCancel.
	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	a.mu.Lock()
	a.runCancel = runCancel
	// If Stop already closed stopCh, abort immediately.
	select {
	case <-a.stopCh:
		a.mu.Unlock()
		return context.Canceled
	default:
	}
	a.mu.Unlock()
	ctx = runCtx

	hubURL, token, err := a.auth()
	if err != nil {
		a.setErr(err)
		return err
	}
	if strings.TrimSpace(hubURL) == "" || strings.TrimSpace(token) == "" {
		a.setOutcome("skipped_no_credentials")
		a.setErr(ErrSyncCredentialsMissing)
		return ErrSyncCredentialsMissing
	}
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if err := validateHubURL(hubURL); err != nil {
		a.setErr(err)
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubURL+"/api/digital-assets/sync/manifest", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// Always scope redirects to Hub host (ignore agent.http override for safety).
	httpClient := hubScopedHTTPClient(hubURL)
	resp, err := httpClient.Do(req)
	if err != nil {
		a.setErr(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		a.setOutcome("skipped_feature_disabled")
		return nil // feature disabled
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		err = fmt.Errorf("manifest status %d: %s", resp.StatusCode, string(b))
		a.setErr(err)
		return err
	}
	var man struct {
		TenantSyncEnabled bool `json:"tenant_sync_enabled"`
		Libraries         []struct {
			LibraryID      string `json:"library_id"`
			Name           string `json:"name"`
			ContentRev     int64  `json:"content_rev"`
			ContentHash    string `json:"content_hash"`
			ACLFingerprint string `json:"acl_fingerprint"`
			SyncEnabled    bool   `json:"sync_enabled"`
		} `json:"libraries"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&man); err != nil {
		a.setErr(err)
		return err
	}
	meta, err := a.client.metaDB()
	if err != nil {
		a.setErr(err)
		return err
	}
	if !man.TenantSyncEnabled {
		if _, err := meta.Exec(`UPDATE enterprise_library_state SET access_state = CASE WHEN access_state = 'revoked' THEN 'revoked' ELSE 'sync_disabled' END`); err != nil {
			a.setErr(err)
			return err
		}
		a.setOutcome("skipped_tenant_sync_disabled")
		return nil
	}

	seen := map[string]struct{}{}
	var firstLibErr error
	for i, lib := range man.Libraries {
		if err := ctx.Err(); err != nil {
			a.setErr(err)
			return err
		}
		seen[lib.LibraryID] = struct{}{}
		// Fixed 1s stagger between libraries (interruptible).
		if i > 0 {
			select {
			case <-ctx.Done():
				err := ctx.Err()
				a.setErr(err)
				return err
			case <-a.stopCh:
				return nil
			case <-time.After(time.Second):
			}
		}
		if strings.TrimSpace(lib.LibraryID) == "" {
			continue
		}
		if err := a.syncLibrary(ctx, hubURL, token, lib.LibraryID, lib.Name, lib.ContentRev, lib.ContentHash, lib.ACLFingerprint, lib.SyncEnabled); err != nil {
			log.Printf("[enterpriseknowledge] library %s: %v", lib.LibraryID, err)
			a.setErr(err)
			if firstLibErr == nil {
				firstLibErr = err
			}
		}
	}
	// Mark libraries missing from manifest as revoked.
	if meta, err := a.client.metaDB(); err == nil {
		rows, qerr := meta.Query(`SELECT library_id FROM enterprise_library_state`)
		if qerr == nil {
			var all []string
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					all = append(all, id)
				}
			}
			rows.Close()
			for _, id := range all {
				if _, ok := seen[id]; !ok {
					_, _ = meta.Exec(`UPDATE enterprise_library_state SET access_state = 'revoked', last_error = 'revoked_by_manifest' WHERE library_id = ?`, id)
				}
			}
		}
	}
	// Only clear lastError when the whole cycle succeeded.
	if firstLibErr == nil {
		a.mu.Lock()
		a.lastError = ""
		a.mu.Unlock()
	}
	return firstLibErr
}

func (a *SyncAgent) loop() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(a.deviceID + time.Now().UTC().Format("2006-01-02")))
	jitter := time.Duration(h.Sum32()%600) * time.Second
	timer := time.NewTimer(jitter)
	defer timer.Stop()
	for {
		select {
		case <-a.stopCh:
			return
		case <-timer.C:
			a.mu.Lock()
			paused := a.paused
			a.mu.Unlock()
			if !paused {
				// Bound background cycles; ignore overlap with manual SyncNow.
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				err := a.RunOnce(ctx)
				cancel()
				if err != nil && !errors.Is(err, ErrSyncInProgress) {
					log.Printf("[enterpriseknowledge] background sync: %v", err)
				}
			}
			timer.Reset(30 * time.Minute)
		}
	}
}

func (a *SyncAgent) syncLibrary(ctx context.Context, hubURL, token, libraryID, name string, tipRev int64, tipContentHash, aclFP string, syncEnabled bool) (syncErr error) {
	return a.syncLibraryPages(ctx, hubURL, token, libraryID, name, tipRev, tipContentHash, aclFP, syncEnabled, maxPullPagesPerRun)
}

func (a *SyncAgent) syncLibraryPages(ctx context.Context, hubURL, token, libraryID, name string, tipRev int64, tipContentHash, aclFP string, syncEnabled bool, remainingPages int) (syncErr error) {
	c := a.client
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library_id required")
	}
	meta, err := c.metaDB()
	if err != nil {
		return err
	}
	// Preserve the last successful library error until this pull actually
	// succeeds. The settings panel presents errors per digital asset, so
	// clearing it at the start of a request hid the useful failure context as
	// soon as a retry began.
	defer func() {
		if syncErr != nil {
			_, _ = meta.Exec(`UPDATE enterprise_library_state SET last_error=? WHERE library_id=?`, syncErr.Error(), libraryID)
		}
	}()
	var lastRev int64
	var userSyncEnabled int = 1
	_ = meta.QueryRow(`SELECT last_rev, COALESCE(user_sync_enabled, 1) FROM enterprise_library_state WHERE library_id = ?`, libraryID).Scan(&lastRev, &userSyncEnabled)
	access := "active"
	if !syncEnabled {
		access = "sync_disabled"
	}
	_, err = meta.Exec(`INSERT INTO enterprise_library_state (library_id, name, last_rev, acl_fingerprint, access_state, last_sync_at, last_error, user_sync_enabled)
		VALUES (?, ?, ?, ?, ?, '', '', 1)
		ON CONFLICT(library_id) DO UPDATE SET name=excluded.name, acl_fingerprint=excluded.acl_fingerprint,
		access_state=excluded.access_state`,
		libraryID, name, lastRev, aclFP, access)
	if err != nil {
		return err
	}
	if !syncEnabled || userSyncEnabled == 0 {
		return nil
	}

	// A local revision ahead of the manifest can occur after a Hub restore or
	// library recreation. Asking from that cursor would produce a permanent
	// empty pull, so bootstrap from revision zero without committing the reset
	// until a package has been applied successfully.
	pullSinceRev := lastRev
	if tipRev > 0 && lastRev > tipRev {
		pullSinceRev = 0
		lastRev = 0
	}
	body, _ := json.Marshal(map[string]any{
		"library_id": libraryID,
		"since_rev":  pullSinceRev,
		"device_id":  a.deviceID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubURL+"/api/digital-assets/sync/pull", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	httpClient := hubScopedHTTPClient(hubURL)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited")
	}
	if resp.StatusCode == http.StatusNotFound {
		if _, err := meta.Exec(`UPDATE enterprise_library_state SET access_state='revoked', last_error='' WHERE library_id=?`, libraryID); err != nil {
			return err
		}
		return nil
	}
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("pull %d: %s", resp.StatusCode, string(b))
	}
	var pull struct {
		Reason string `json:"reason"`
		Ops    []struct {
			Rev           int64  `json:"rev"`
			Op            string `json:"op"`
			PackageURL    string `json:"package_url"`
			PackageSHA256 string `json:"package_sha256"`
			PackageBytes  int64  `json:"package_bytes"`
			ContentHash   string `json:"content_hash"`
		} `json:"ops"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONResponseBytes)).Decode(&pull); err != nil {
		return err
	}
	if pull.Reason == "tenant_sync_disabled" || pull.Reason == "library_sync_disabled" {
		if _, err := meta.Exec(`UPDATE enterprise_library_state SET access_state='sync_disabled', last_error='' WHERE library_id=?`, libraryID); err != nil {
			return err
		}
		return nil
	}
	// A non-empty reason without operations is a protocol-level result, not an
	// assertion that this device has reached the manifest tip. In particular,
	// the Hub uses tenant_busy while its per-tenant pull slot is exhausted.
	// Treat it as a retryable sync failure, preserving the current active cache
	// and avoiding the misleading "no package operations" diagnosis below.
	if strings.TrimSpace(pull.Reason) != "" {
		return fmt.Errorf("hub deferred library sync: %s", pull.Reason)
	}
	if len(pull.Ops) == 0 {
		// The manifest is advisory. A no-op pull is the only authoritative
		// confirmation that this client has reached Hub's current revision.
		if tipRev > 0 && lastRev < tipRev {
			return fmt.Errorf("hub returned no package operations for revision %d (local revision %d)", tipRev, lastRev)
		}
		if _, err := meta.Exec(`UPDATE enterprise_library_state
			SET content_hash = CASE WHEN ? <> '' THEN ? ELSE content_hash END,
				access_state='active', last_sync_at=?, last_error=''
			WHERE library_id=?`, strings.TrimSpace(tipContentHash), strings.TrimSpace(tipContentHash), time.Now().UTC().Format(time.RFC3339), libraryID); err != nil {
			return err
		}
		return nil
	}
	// Hub publishes complete snapshots for every non-tombstone revision. When a
	// device has been offline long enough to receive a full page, only the
	// newest snapshot can affect the resulting local cache. Skipping older
	// snapshots avoids repeatedly deleting and rebuilding the same library,
	// while still advancing the cursor to the most recent returned revision.
	// Tombstones are terminal and must always take precedence.
	latestOpRev := int64(0)
	for _, op := range pull.Ops {
		if op.Rev > latestOpRev {
			latestOpRev = op.Rev
		}
	}
	for _, op := range pull.Ops {
		if op.Rev < latestOpRev {
			continue
		}
		if op.Rev <= lastRev {
			continue
		}
		if op.Rev <= 0 {
			return fmt.Errorf("invalid package revision %d", op.Rev)
		}
		if strings.TrimSpace(op.Op) == "tombstone_library" {
			if err := c.PurgeLibrary(libraryID); err != nil {
				return fmt.Errorf("apply library tombstone: %w", err)
			}
			return nil
		}
		if op.Op != "replace_snapshot" && op.Op != "upsert_sources" {
			return fmt.Errorf("unsupported package operation %q", op.Op)
		}
		store, err := c.EnsureStore()
		if err != nil {
			return err
		}
		pkgURL := op.PackageURL
		if strings.HasPrefix(pkgURL, "/") {
			pkgURL = hubURL + pkgURL
		}
		if pkgURL == "" {
			pkgURL = fmt.Sprintf("%s/api/digital-assets/libraries/%s/sync/packages/%d", hubURL, libraryID, op.Rev)
		}
		if err := validatePackageURL(hubURL, pkgURL); err != nil {
			return err
		}
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("dal_%s_%d_%d_%d.jsonl",
			sanitizeFilePart(libraryID), op.Rev, time.Now().UnixNano(), a.tmpSeq.Add(1)))
		if err := downloadPackage(ctx, httpClient, pkgURL, token, tmp, op.PackageBytes); err != nil {
			return err
		}
		if err := verifyPackageSHA256(tmp, op.PackageSHA256); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		if err := applyPackage(ctx, store, c, libraryID, op.Op, tmp); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		_ = os.Remove(tmp)
		lastRev = op.Rev
		contentHash := strings.TrimSpace(op.ContentHash)
		if contentHash == "" && lastRev == tipRev {
			contentHash = strings.TrimSpace(tipContentHash)
		}
		if _, err := meta.Exec(`UPDATE enterprise_library_state
			SET last_rev=?, content_hash = CASE WHEN ? <> '' THEN ? ELSE content_hash END,
				access_state='active', last_sync_at=?, last_error=''
			WHERE library_id=?`,
			lastRev, contentHash, contentHash, time.Now().UTC().Format(time.RFC3339), libraryID); err != nil {
			return err
		}
	}
	if lastRev < tipRev {
		if remainingPages <= 1 {
			return fmt.Errorf("sync remains behind hub revision %d after %d pull pages (local revision %d)", tipRev, maxPullPagesPerRun, lastRev)
		}
		return a.syncLibraryPages(ctx, hubURL, token, libraryID, name, tipRev, tipContentHash, aclFP, syncEnabled, remainingPages-1)
	}
	return nil
}

func validateHubURL(hubURL string) error {
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return fmt.Errorf("empty hub url")
	}
	u, err := url.Parse(hubURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid hub url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("hub url scheme must be http(s)")
	}
	if u.User != nil {
		return fmt.Errorf("hub url must not include userinfo")
	}
	return nil
}

// validatePackageURL rejects absolute package URLs that do not share hub host (SSRF guard).
func validatePackageURL(hubURL, pkgURL string) error {
	pkgURL = strings.TrimSpace(pkgURL)
	if pkgURL == "" {
		return fmt.Errorf("empty package url")
	}
	hub, err := url.Parse(hubURL)
	if err != nil || hub.Scheme == "" || hub.Host == "" {
		return fmt.Errorf("invalid hub url")
	}
	if hub.Scheme != "http" && hub.Scheme != "https" {
		return fmt.Errorf("hub url scheme must be http(s)")
	}
	pkg, err := url.Parse(pkgURL)
	if err != nil {
		return fmt.Errorf("invalid package url: %w", err)
	}
	if pkg.Scheme == "" {
		// Relative — will not be used as-is after join; reject if we got here relative.
		return fmt.Errorf("package url must be absolute")
	}
	if pkg.Scheme != "http" && pkg.Scheme != "https" {
		return fmt.Errorf("package url scheme must be http(s)")
	}
	// Normalize default ports so https://h and https://h:443 compare equal.
	if !strings.EqualFold(normalizeHostPort(pkg), normalizeHostPort(hub)) {
		return fmt.Errorf("package url host %q does not match hub host %q", pkg.Host, hub.Host)
	}
	// Block credentials in URL userinfo (unexpected for package endpoints).
	if pkg.User != nil {
		return fmt.Errorf("package url must not include userinfo")
	}
	return nil
}

func normalizeHostPort(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return host + ":" + port
}

func sanitizeFilePart(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
	if s == "" {
		return "lib"
	}
	if len(s) > 64 {
		return s[:64]
	}
	return s
}

func downloadPackage(ctx context.Context, client *http.Client, url, token, dest string, expectedBytes int64) error {
	if client == nil {
		// No hub scope available here; callers should pass hubScopedHTTPClient.
		client = &http.Client{Transport: sharedSyncTransport}
	}
	if expectedBytes < 0 {
		return fmt.Errorf("invalid package size %d", expectedBytes)
	}
	if expectedBytes > maxPackageBytes {
		return fmt.Errorf("package too large: declared size %d exceeds %d", expectedBytes, maxPackageBytes)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("download package %d: %s", resp.StatusCode, string(b))
	}
	// Content-Length check when provided (still enforce LimitReader below).
	if resp.ContentLength > maxPackageBytes {
		return fmt.Errorf("package too large: Content-Length %d exceeds %d", resp.ContentLength, maxPackageBytes)
	}
	if expectedBytes > 0 && resp.ContentLength >= 0 && resp.ContentLength != expectedBytes {
		return fmt.Errorf("package size mismatch: Content-Length %d, Hub declared %d", resp.ContentLength, expectedBytes)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	// +1 byte so we can detect overflow vs exact max.
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxPackageBytes+1))
	if err != nil {
		_ = os.Remove(dest)
		return err
	}
	if n > maxPackageBytes {
		_ = os.Remove(dest)
		return fmt.Errorf("package too large: exceeds %d bytes", maxPackageBytes)
	}
	if expectedBytes > 0 && n != expectedBytes {
		_ = os.Remove(dest)
		return fmt.Errorf("package size mismatch: downloaded %d bytes, Hub declared %d", n, expectedBytes)
	}
	return nil
}

// verifyPackageSHA256 verifies the digest supplied by Hub before a downloaded
// package is parsed or allowed to replace any local cache.
func verifyPackageSHA256(path, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if expected == "" {
		return fmt.Errorf("package sha256 is required")
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("invalid package sha256")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return fmt.Errorf("invalid package sha256: %w", err)
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
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("package sha256 mismatch")
	}
	return nil
}

func applyPackage(ctx context.Context, store *knowledge.SQLiteStore, c *Client, libraryID, op, path string) error {
	return applyPackageWithImporter(ctx, store, c, libraryID, op, path, store.ImportSnapshot)
}

// snapshotImporter permits a deterministic failure injection in the recovery
// test while production retains the SQLite store's transactional importer.
type snapshotImporter func(context.Context, knowledge.SnapshotImportOptions) (knowledge.SnapshotImportResult, error)

type sourceMapRow struct {
	remoteID string
	localID  string
}

func applyPackageWithImporter(ctx context.Context, store *knowledge.SQLiteStore, c *Client, libraryID, op, path string, importPackage snapshotImporter) error {
	if importPackage == nil {
		return fmt.Errorf("snapshot importer is required")
	}
	meta, err := c.metaDB()
	if err != nil {
		return err
	}
	prefix := "dal_" + libraryID + "_"
	remoteSourceIDs, err := snapshotSourceIDs(path)
	if err != nil {
		return err
	}
	rewritten, err := rewriteSnapshotIDs(path, libraryID)
	if err != nil {
		return err
	}
	defer os.Remove(rewritten)
	// Hub packages are complete library snapshots for both replace_snapshot and
	// upsert_sources. Validate in an empty store before either operation removes
	// the current cache.
	// This ensures dangling package references cannot turn a failed update into data loss.
	if err := validateSnapshotPackage(ctx, rewritten); err != nil {
		return fmt.Errorf("validate sync package: %w", err)
	}
	// The enterprise store is dedicated to Hub-provided libraries. Keep a
	// recoverable snapshot before removing a library's previous revision: the
	// snapshot importer is transactional, but source deletion is a separate
	// operation and can otherwise leave a library empty after a disk/SQLite
	// failure during the replacement import.
	var previousMappings []sourceMapRow
	rollbackPath := ""
	if op == "replace_snapshot" || op == "upsert_sources" {
		rollbackPath = filepath.Join(os.TempDir(), fmt.Sprintf("dal_rollback_%s_%d.jsonl", sanitizeFilePart(libraryID), time.Now().UnixNano()))
		if _, err := store.ExportSnapshot(ctx, knowledge.ExportOptions{OutputPath: rollbackPath}); err != nil {
			return fmt.Errorf("snapshot local enterprise cache before update: %w", err)
		}
		defer os.Remove(rollbackPath)
	}
	restorePrevious := func(cause error) error {
		if rollbackPath == "" {
			return cause
		}
		prefix := "dal_" + libraryID + "_"
		if _, err := store.DeleteSourcesByIDPrefix(ctx, prefix); err != nil {
			return fmt.Errorf("%w; rollback cleanup failed: %v", cause, err)
		}
		if _, err := store.ImportSnapshot(ctx, knowledge.SnapshotImportOptions{
			InputPath: rollbackPath, Overwrite: true, ReplaceAll: false,
			SkipSafetyBackup: true, AbortOnError: true,
		}); err != nil {
			return fmt.Errorf("%w; rollback restore failed: %v", cause, err)
		}
		if err := replaceLibrarySourceMappings(meta, libraryID, previousMappings); err != nil {
			return fmt.Errorf("%w; rollback map restore failed: %v", cause, err)
		}
		return cause
	}
	if op == "replace_snapshot" || op == "upsert_sources" {
		rows, qerr := meta.Query(`SELECT remote_source_id, local_source_id FROM enterprise_source_map WHERE library_id = ?`, libraryID)
		if qerr != nil {
			return qerr
		}
		for rows.Next() {
			var row sourceMapRow
			if err := rows.Scan(&row.remoteID, &row.localID); err != nil {
				_ = rows.Close()
				return err
			}
			previousMappings = append(previousMappings, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, mapping := range previousMappings {
			localID := mapping.localID
			// Namespaced sources are removed in the bulk delete below. Preserve
			// cleanup of pre-namespace map entries for existing installations.
			if localID == "" || strings.HasPrefix(localID, prefix) {
				continue
			}
			if err := store.DeleteSource(ctx, localID); err != nil {
				return restorePrevious(fmt.Errorf("remove previous mapped source %q: %w", localID, err))
			}
		}
		if _, err := store.DeleteSourcesByIDPrefix(ctx, prefix); err != nil {
			return restorePrevious(fmt.Errorf("remove previous snapshot sources: %w", err))
		}
		if _, err := meta.Exec(`DELETE FROM enterprise_source_map WHERE library_id = ?`, libraryID); err != nil {
			return restorePrevious(err)
		}
	}
	_, err = importPackage(ctx, knowledge.SnapshotImportOptions{
		InputPath:        rewritten,
		Overwrite:        true,
		ReplaceAll:       false,
		SkipSafetyBackup: true,
		AbortOnError:     true,
	})
	if err != nil {
		return restorePrevious(err)
	}
	newMappings := make([]sourceMapRow, 0, len(remoteSourceIDs))
	for _, remoteID := range remoteSourceIDs {
		newMappings = append(newMappings, sourceMapRow{remoteID: remoteID, localID: namespaceSnapshotID(prefix, remoteID)})
	}
	if err := replaceLibrarySourceMappings(meta, libraryID, newMappings); err != nil {
		return restorePrevious(err)
	}
	return nil
}

// replaceLibrarySourceMappings updates every mapping for one library in a
// single meta-db transaction. It prevents partial source-map replacement and
// lets rollback restore the exact mapping set that existed before the import.
func replaceLibrarySourceMappings(meta *sql.DB, libraryID string, mappings []sourceMapRow) error {
	tx, err := meta.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM enterprise_source_map WHERE library_id = ?`, libraryID); err != nil {
		return err
	}
	for _, mapping := range mappings {
		if _, err := tx.Exec(`INSERT INTO enterprise_source_map (library_id, remote_source_id, local_source_id) VALUES (?, ?, ?)`, libraryID, mapping.remoteID, mapping.localID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// validateSnapshotPackage checks package references in an isolated store, where
// stale local sources cannot accidentally satisfy an invalid package reference.
func validateSnapshotPackage(ctx context.Context, path string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	dir, err := os.MkdirTemp("", "dal_validate_")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	probe, err := knowledge.NewSQLiteStore(filepath.Join(dir, "knowledge.db"))
	if err != nil {
		return err
	}
	defer probe.Close()
	_, err = probe.ImportSnapshot(ctx, knowledge.SnapshotImportOptions{
		InputPath:        path,
		DryRun:           true,
		Overwrite:        true,
		SkipSafetyBackup: true,
		AbortOnError:     true,
	})
	return err
}

func rewriteSnapshotIDs(path, libraryID string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	prefix := "dal_" + libraryID + "_"
	out := filepath.Join(os.TempDir(), fmt.Sprintf("dal_ns_%s_%d.jsonl", sanitizeFilePart(libraryID), time.Now().UnixNano()))
	// Always run JSONL namespace (idempotent for already-prefixed ids). Do not
	// short-circuit on "contains prefix" — mixed packages may still have bare ids.
	rewritten, err := namespaceJSONLSourceIDs(data, prefix)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(out, rewritten, 0o644); err != nil {
		return "", err
	}
	return out, nil
}

// snapshotSourceIDs extracts remote source IDs before the package is rewritten,
// so the local source map remains useful for migration and diagnostics instead
// of relying solely on prefix cleanup.
func snapshotSourceIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("parse snapshot source map: %w", err)
		}
		if record.Type != "source" || len(record.Data) == 0 {
			continue
		}
		var source struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(record.Data, &source); err != nil {
			return nil, fmt.Errorf("parse snapshot source: %w", err)
		}
		id := strings.TrimSpace(source.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// namespaceJSONLSourceIDs prefixes source/node/card/fact id fields so multi-library
// content coexists in one SQLite store. Skips values already using the prefix.
func namespaceJSONLSourceIDs(data []byte, prefix string) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	// Keep every identifier reference in a snapshot in the same library namespace.
	// Source-link records reference their target through related_source_id; omitting
	// that key leaves a bare ID behind and makes ImportSnapshot reject the package
	// as an invalid dangling link. ImportSnapshot also requires sources, nodes,
	// cards, and their dependent records in dependency order. Older Hub snapshots
	// did not guarantee that order, so normalize it here before isolated validation.
	idKeys := []string{"id", "source_id", "sourceId", "related_source_id", "relatedSourceId", "node_id", "card_id", "fact_id", "parent_id", "local_source_id"}
	type snapshotLine struct {
		line     string
		priority int
		order    int
	}
	rewritten := make([]snapshotLine, 0, len(lines))
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			rewritten = append(rewritten, snapshotLine{line: line, priority: 2, order: i})
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(trim), &obj); err != nil {
			// Non-JSON line: keep raw.
			rewritten = append(rewritten, snapshotLine{line: line, priority: 1, order: i})
			continue
		}
		namespaceMapIDs(obj, prefix, idKeys)
		out, err := json.Marshal(obj)
		if err != nil {
			rewritten = append(rewritten, snapshotLine{line: line, priority: 1, order: i})
			continue
		}
		priority := snapshotRecordPriority(snapshotRecordType(obj))
		rewritten = append(rewritten, snapshotLine{line: string(out), priority: priority, order: i})
	}
	sort.SliceStable(rewritten, func(i, j int) bool {
		if rewritten[i].priority != rewritten[j].priority {
			return rewritten[i].priority < rewritten[j].priority
		}
		return rewritten[i].order < rewritten[j].order
	})
	var b strings.Builder
	b.Grow(len(data) + len(data)/8)
	for i, line := range rewritten {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line.line)
	}
	return []byte(b.String()), nil
}

func snapshotRecordType(obj map[string]any) string {
	typ, _ := obj["type"].(string)
	return strings.TrimSpace(typ)
}

// snapshotRecordPriority gives each record type a stable ImportSnapshot-safe
// position. Records of the same type retain their original order.
func snapshotRecordPriority(recordType string) int {
	switch recordType {
	case "manifest":
		return -1
	case "source":
		return 0
	case "source_label", "source_version":
		return 1
	case "source_link", "source_link_event":
		return 2
	case "node":
		return 3
	case "card":
		return 4
	case "fact":
		return 5
	case "summary":
		return 7
	default:
		return 6
	}
}

func namespaceMapIDs(obj map[string]any, prefix string, idKeys []string) {
	for k, v := range obj {
		switch tv := v.(type) {
		case string:
			if isIDKey(k, idKeys) && tv != "" {
				obj[k] = namespaceSnapshotID(prefix, tv)
			}
		case map[string]any:
			namespaceMapIDs(tv, prefix, idKeys)
		case []any:
			for _, item := range tv {
				if m, ok := item.(map[string]any); ok {
					namespaceMapIDs(m, prefix, idKeys)
				}
			}
		}
	}
}

func namespaceSnapshotID(prefix, id string) string {
	if strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + id
}

func isIDKey(k string, idKeys []string) bool {
	for _, key := range idKeys {
		if k == key {
			return true
		}
	}
	return false
}

func (a *SyncAgent) setErr(err error) {
	if err == nil || a == nil {
		return
	}
	a.mu.Lock()
	a.lastError = err.Error()
	a.mu.Unlock()
}

func (a *SyncAgent) setOutcome(outcome string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.lastOutcome = outcome
	a.mu.Unlock()
}
