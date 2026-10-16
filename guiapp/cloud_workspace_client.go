package guiapp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

var (
	cloudWorkspaceInstanceOnce sync.Once
	cloudWorkspaceInstanceID   string
	cloudWorkspaceSessions     = struct {
		sync.Mutex
		items map[string]*cloudWorkspaceInstanceSessionEntry
	}{items: make(map[string]*cloudWorkspaceInstanceSessionEntry)}
)

func cloudWorkspaceClientInstanceID() string {
	cloudWorkspaceInstanceOnce.Do(func() {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err == nil {
			cloudWorkspaceInstanceID = "cwi_" + hex.EncodeToString(buf)
		} else {
			cloudWorkspaceInstanceID = "cwi_unknown"
		}
	})
	return cloudWorkspaceInstanceID
}

const (
	cloudWorkspaceEntitlementPath       = "/api/v1/cloud-workspaces/entitlement"
	cloudWorkspaceCollectionPath        = "/api/v1/cloud-workspaces"
	cloudWorkspaceTaskProvisionPath     = "/api/v1/cloud-workspace-tasks"
	cloudWorkspaceInstanceSessionsPath  = "/api/v1/cloud-workspace-sessions"
	cloudWorkspaceProtocolVersion       = "v1-sequential"
	cloudWorkspaceInstanceSessionHeader = "X-Cloud-Workspace-Instance-Session"
	cloudWorkspaceResponseMaxSize       = 3 << 20
	cloudWorkspaceManifestMaxSize       = 16 << 20
	cloudWorkspaceMaxManifestEntries    = 20000
	cloudWorkspaceObjectMaxBytes        = 64 << 20
	cloudWorkspaceChunkBytes            = 8 << 20
	cloudWorkspaceRequestTimeout        = 30 * time.Second
	cloudWorkspaceChunkTimeout          = 60 * time.Second
	cloudWorkspaceHubUnavailableBanner  = "Hub 不可用，云端工作区暂不可用"
	cloudWorkspaceBytesPerTimeoutSec    = 262144
)

type cloudWorkspaceInstanceSession struct {
	SessionID        string `json:"session_id"`
	SessionToken     string `json:"session_token"`
	ClientInstanceID string `json:"client_instance_id"`
	Protocol         string `json:"protocol"`
	ExpiresAt        string `json:"expires_at"`
}

type cloudWorkspaceInstanceSessionEntry struct {
	ready   chan struct{}
	session cloudWorkspaceInstanceSession
}

func cloudWorkspaceInstanceSessionCacheKey(hubURL, machineID, machineToken string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(hubURL) + "\x00" + strings.TrimSpace(machineID) + "\x00" + machineToken))
	return hex.EncodeToString(sum[:])
}

func resetCloudWorkspaceInstanceSessions() {
	cloudWorkspaceSessions.Lock()
	cloudWorkspaceSessions.items = make(map[string]*cloudWorkspaceInstanceSessionEntry)
	cloudWorkspaceSessions.Unlock()
}

func issueCloudWorkspaceInstanceSession(ctx context.Context, hubURL, machineToken, machineID string) (cloudWorkspaceInstanceSession, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+cloudWorkspaceInstanceSessionsPath, nil)
	if err != nil {
		return cloudWorkspaceInstanceSession{}, err
	}
	req.Header.Set("Authorization", "Bearer "+machineToken)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("X-Cloud-Workspace-Protocol", cloudWorkspaceProtocolVersion)
	req.Header.Set("Accept", "application/json")
	resp, err := cloudWorkspaceHTTPClient(cloudWorkspaceRequestTimeout).Do(req)
	if err != nil {
		return cloudWorkspaceInstanceSession{}, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, cloudWorkspaceResponseMaxSize+1))
	if readErr != nil {
		return cloudWorkspaceInstanceSession{}, fmt.Errorf("read Hub response: %w", readErr)
	}
	if len(data) > cloudWorkspaceResponseMaxSize {
		return cloudWorkspaceInstanceSession{}, fmt.Errorf("Hub response exceeds %d byte limit", cloudWorkspaceResponseMaxSize)
	}
	if resp.StatusCode != http.StatusCreated {
		return cloudWorkspaceInstanceSession{}, cloudWorkspaceAPIError(resp.StatusCode, data)
	}
	var out cloudWorkspaceInstanceSession
	if err := json.Unmarshal(data, &out); err != nil {
		return cloudWorkspaceInstanceSession{}, fmt.Errorf("decode cloud workspace instance session: %w", err)
	}
	if strings.TrimSpace(out.SessionID) == "" || strings.TrimSpace(out.SessionToken) == "" || strings.TrimSpace(out.ClientInstanceID) == "" || out.Protocol != cloudWorkspaceProtocolVersion {
		return cloudWorkspaceInstanceSession{}, fmt.Errorf("Hub returned an invalid cloud workspace instance session")
	}
	return out, nil
}

func ensureCloudWorkspaceInstanceSession(ctx context.Context, hubURL, machineToken, machineID string) (cloudWorkspaceInstanceSession, error) {
	key := cloudWorkspaceInstanceSessionCacheKey(hubURL, machineID, machineToken)
	for {
		cloudWorkspaceSessions.Lock()
		if existing := cloudWorkspaceSessions.items[key]; existing != nil {
			if existing.ready == nil {
				out := existing.session
				cloudWorkspaceSessions.Unlock()
				return out, nil
			}
			ready := existing.ready
			cloudWorkspaceSessions.Unlock()
			select {
			case <-ready:
				continue
			case <-ctx.Done():
				return cloudWorkspaceInstanceSession{}, ctx.Err()
			}
		}
		entry := &cloudWorkspaceInstanceSessionEntry{ready: make(chan struct{})}
		cloudWorkspaceSessions.items[key] = entry
		cloudWorkspaceSessions.Unlock()

		out, err := issueCloudWorkspaceInstanceSession(ctx, hubURL, machineToken, machineID)
		cloudWorkspaceSessions.Lock()
		ready := entry.ready
		if err != nil {
			delete(cloudWorkspaceSessions.items, key)
		} else {
			entry.session = out
			entry.ready = nil
		}
		close(ready)
		cloudWorkspaceSessions.Unlock()
		return out, err
	}
}

func dropCloudWorkspaceInstanceSession(hubURL, machineToken, machineID string) {
	key := cloudWorkspaceInstanceSessionCacheKey(hubURL, machineID, machineToken)
	cloudWorkspaceSessions.Lock()
	delete(cloudWorkspaceSessions.items, key)
	cloudWorkspaceSessions.Unlock()
}

func invalidateCloudWorkspaceInstanceSession(hubURL, machineToken, machineID, usedToken string) {
	key := cloudWorkspaceInstanceSessionCacheKey(hubURL, machineID, machineToken)
	cloudWorkspaceSessions.Lock()
	if entry := cloudWorkspaceSessions.items[key]; entry != nil && entry.ready == nil && entry.session.SessionToken == usedToken {
		delete(cloudWorkspaceSessions.items, key)
	}
	cloudWorkspaceSessions.Unlock()
}

func cloudWorkspaceSessionRejected(status int, data []byte) bool {
	if status != http.StatusUnauthorized {
		return false
	}
	var payload struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return false
	}
	return payload.Code == "CLOUD_WORKSPACE_SESSION_REQUIRED" || payload.Code == "CLOUD_WORKSPACE_SESSION_INVALID"
}

func cloudWorkspaceItemPath(id string) string {
	return cloudWorkspaceCollectionPath + "/" + url.PathEscape(strings.TrimSpace(id))
}

func cloudWorkspaceSharePath(id string) string {
	return cloudWorkspaceItemPath(id) + "/share"
}

func cloudWorkspaceShareRecipientPath(id, userID string) string {
	return cloudWorkspaceSharePath(id) + "/recipients/" + url.PathEscape(strings.TrimSpace(userID))
}

func cloudWorkspaceShareAcceptPath(token string) string {
	return "/api/v1/cloud-workspace-shares/" + url.PathEscape(strings.TrimSpace(token)) + "/accept"
}

func cloudWorkspaceRestorePath(id string) string {
	return cloudWorkspaceItemPath(id) + "/restore"
}

func cloudWorkspaceLeasesPath(id string) string {
	return cloudWorkspaceItemPath(id) + "/leases"
}

func cloudWorkspaceLeasePath(id, leaseID string) string {
	return cloudWorkspaceLeasesPath(id) + "/" + url.PathEscape(strings.TrimSpace(leaseID))
}

func cloudWorkspaceLeaseHeartbeatPath(id, leaseID string) string {
	return cloudWorkspaceLeasePath(id, leaseID) + "/heartbeat"
}

func cloudWorkspaceLeaseHandoffRequestPath(id string) string {
	return cloudWorkspaceLeasesPath(id) + "/handoff-request"
}

func cloudWorkspaceManifestPath(id string) string {
	return cloudWorkspaceItemPath(id) + "/manifest"
}

func cloudWorkspaceObjectPath(id, sha256hex string) string {
	return cloudWorkspaceItemPath(id) + "/objects/" + url.PathEscape(strings.TrimSpace(sha256hex))
}

func cloudWorkspaceObjectChunkPath(id, sha256hex string, index int) string {
	return cloudWorkspaceObjectPath(id, sha256hex) + "/chunks/" + strconv.Itoa(index)
}

func cloudWorkspaceObjectCompletePath(id, sha256hex string) string {
	return cloudWorkspaceObjectPath(id, sha256hex) + "/complete"
}

func cloudWorkspaceSidecarPath(id, name string) string {
	return cloudWorkspaceItemPath(id) + "/sidecars/" + url.PathEscape(strings.TrimSpace(name))
}

func cloudWorkspaceAuditAPIPath(id string) string {
	return cloudWorkspaceItemPath(id) + "/audit"
}

func cloudWorkspaceTaskProvisionItemPath(operationID string) string {
	return cloudWorkspaceTaskProvisionPath + "/" + url.PathEscape(strings.TrimSpace(operationID))
}

// cloudWorkspaceTransferTimeout is max(60s, 30s + sizeBytes/262144).
// Object uploads must not reuse virtualRepositorySyncRequest's 30s cap.
func cloudWorkspaceTransferTimeout(sizeBytes int64) time.Duration {
	if sizeBytes < 0 {
		sizeBytes = 0
	}
	d := 30*time.Second + time.Duration(sizeBytes/cloudWorkspaceBytesPerTimeoutSec)*time.Second
	if d < 60*time.Second {
		return 60 * time.Second
	}
	return d
}

type cloudWorkspaceHTTPOptions struct {
	timeout         time.Duration
	maxRead         int64
	accept          string
	contentType     string
	jsonBody        any
	rawBody         []byte
	headers         map[string]string
	instanceSession bool
}

func (a *App) cloudWorkspaceHubDoRemote(ctx context.Context, hubURL, accessToken, method, path string, opt cloudWorkspaceHTTPOptions) ([]byte, int, error) {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if hubURL == "" {
		return nil, 0, fmt.Errorf("Hub URL not configured")
	}
	var reader io.Reader
	var contentLength int64
	switch {
	case opt.rawBody != nil:
		reader = bytes.NewReader(opt.rawBody)
		contentLength = int64(len(opt.rawBody))
	case opt.jsonBody != nil:
		raw, marshalErr := json.Marshal(opt.jsonBody)
		if marshalErr != nil {
			return nil, 0, marshalErr
		}
		reader = bytes.NewReader(raw)
		contentLength = int64(len(raw))
	}
	reqURL := hubURL + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(accessToken) != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	if strings.HasPrefix(path, cloudWorkspaceCollectionPath) || strings.HasPrefix(path, cloudWorkspaceTaskProvisionPath) || strings.HasPrefix(path, "/api/v1/cloud-workspace-shares/") {
		req.Header.Set("X-Cloud-Workspace-Protocol", cloudWorkspaceProtocolVersion)
	}
	for key, value := range opt.headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	if strings.TrimSpace(opt.accept) != "" {
		req.Header.Set("Accept", opt.accept)
	} else {
		req.Header.Set("Accept", "application/json")
	}
	switch {
	case opt.rawBody != nil:
		ct := strings.TrimSpace(opt.contentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		req.Header.Set("Content-Type", ct)
		req.ContentLength = contentLength
	case opt.jsonBody != nil:
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLength
	}
	timeout := opt.timeout
	if timeout <= 0 {
		timeout = cloudWorkspaceRequestTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining <= 0 {
			return nil, 0, ctx.Err()
		} else if remaining < timeout {
			timeout = remaining
		}
	}
	maxRead := opt.maxRead
	if maxRead <= 0 {
		maxRead = cloudWorkspaceResponseMaxSize
	}
	resp, err := cloudWorkspaceHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRead+1))
	if readErr != nil {
		return data, resp.StatusCode, fmt.Errorf("read Hub response: %w", readErr)
	}
	if int64(len(data)) > maxRead {
		return nil, resp.StatusCode, fmt.Errorf("Hub response exceeds %d byte limit", maxRead)
	}
	return data, resp.StatusCode, nil
}

func cloudWorkspaceHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("stopped after 5 redirects")
			}
			if len(via) == 0 || req == nil || req.URL == nil || via[0] == nil || via[0].URL == nil {
				return http.ErrUseLastResponse
			}
			orig := via[0].URL
			if !strings.EqualFold(req.URL.Scheme, orig.Scheme) || !strings.EqualFold(req.URL.Host, orig.Host) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

type cloudWorkspaceLeaseOverride struct {
	workspaceID  string
	leaseID      string
	fencingToken int64
}

type cloudWorkspaceLeaseOverrideKey struct{}

// cloudWorkspaceWriteLeaseFunc borrows a writer lease only for the manifest
// write that follows. DeletePaths calls it after the read, and only when the
// remote tree actually changes.
type cloudWorkspaceWriteLeaseFunc func(context.Context) (context.Context, func(), error)

type cloudWorkspaceWriteLeaseKey struct{}

func withCloudWorkspaceWriteLease(ctx context.Context, fn cloudWorkspaceWriteLeaseFunc) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, cloudWorkspaceWriteLeaseKey{}, fn)
}

func cloudWorkspaceWriteLeaseFrom(ctx context.Context) cloudWorkspaceWriteLeaseFunc {
	if ctx == nil {
		return nil
	}
	fn, _ := ctx.Value(cloudWorkspaceWriteLeaseKey{}).(cloudWorkspaceWriteLeaseFunc)
	return fn
}

func withCloudWorkspaceLeaseOverride(ctx context.Context, workspaceID, leaseID string, fencingToken int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, cloudWorkspaceLeaseOverrideKey{}, cloudWorkspaceLeaseOverride{
		workspaceID:  strings.TrimSpace(workspaceID),
		leaseID:      strings.TrimSpace(leaseID),
		fencingToken: fencingToken,
	})
}

func cloudWorkspaceLeaseOverrideFrom(ctx context.Context, workspaceID string) (string, int64, bool) {
	if ctx == nil {
		return "", 0, false
	}
	value, ok := ctx.Value(cloudWorkspaceLeaseOverrideKey{}).(cloudWorkspaceLeaseOverride)
	if !ok || value.workspaceID != strings.TrimSpace(workspaceID) || value.leaseID == "" || value.fencingToken <= 0 {
		return "", 0, false
	}
	return value.leaseID, value.fencingToken, true
}

// cloudWorkspaceLeaseHeaders prefers a one-shot delete lease over a browse
// mount that has no writer lease. Caller headers still replace the result.
func cloudWorkspaceLeaseHeaders(ctx context.Context, workspaceID string) (fencing, session string) {
	if mount := lookupHeldCloudWorkspace(workspaceID); mount != nil {
		mount.mu.Lock()
		token := mount.FencingToken
		leaseID := strings.TrimSpace(mount.LeaseID)
		mount.mu.Unlock()
		if token > 0 {
			fencing = strconv.FormatInt(token, 10)
		}
		session = leaseID
	}
	if leaseID, token, ok := cloudWorkspaceLeaseOverrideFrom(ctx, workspaceID); ok {
		fencing = strconv.FormatInt(token, 10)
		session = leaseID
	}
	return fencing, session
}

func (a *App) cloudWorkspaceHubDoWithShare(ctx context.Context, sess cloudWorkspaceShareAccessSession, method, path string, opt cloudWorkspaceHTTPOptions) ([]byte, int, error) {
	headers := map[string]string{}
	if workspaceID := cloudWorkspaceWorkspaceIDFromPath(path); workspaceID != "" {
		fencing, session := cloudWorkspaceLeaseHeaders(ctx, workspaceID)
		if fencing != "" {
			headers["X-Cloud-Workspace-Fencing"] = fencing
		}
		if session != "" {
			headers["X-Cloud-Workspace-Session"] = session
		}
	}
	for key, value := range opt.headers {
		if strings.TrimSpace(key) != "" {
			headers[key] = value
		}
	}
	opt.headers = headers
	return a.cloudWorkspaceHubDoRemote(ctx, sess.HubURL, sess.AccessToken, method, path, opt)
}

func (a *App) cloudWorkspaceHubDo(ctx context.Context, method, path string, opt cloudWorkspaceHTTPOptions) ([]byte, int, error) {
	return a.cloudWorkspaceHubDoRecover(ctx, method, path, opt, true)
}

func (a *App) cloudWorkspaceHubDoRecover(ctx context.Context, method, path string, opt cloudWorkspaceHTTPOptions, allowAuthRecovery bool) ([]byte, int, error) {
	workspaceID := cloudWorkspaceWorkspaceIDFromPath(path)
	if workspaceID != "" {
		if sess, ok := a.lookupForeignShareAccessSession(workspaceID); ok {
			return a.cloudWorkspaceHubDoWithShare(ctx, sess, method, path, opt)
		}
	}
	hubURL, token, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		if workspaceID != "" {
			if sess, ok := a.lookupShareAccessSession(workspaceID); ok && validReferralHandoffHubURL(sess.HubURL) {
				return a.cloudWorkspaceHubDoWithShare(ctx, sess, method, path, opt)
			}
		}
		return nil, 0, err
	}
	var instanceSession cloudWorkspaceInstanceSession
	requiresInstanceSession := opt.instanceSession || (strings.HasPrefix(path, cloudWorkspaceCollectionPath) && path != cloudWorkspaceEntitlementPath) || strings.HasPrefix(path, cloudWorkspaceTaskProvisionPath)
	if requiresInstanceSession {
		instanceSession, err = ensureCloudWorkspaceInstanceSession(ctx, hubURL, token, machineID)
		if err != nil {
			if allowAuthRecovery && isCloudWorkspaceMachineAuthError(err) && a.recoverCloudWorkspaceMachineAuthorization(token) {
				retryCtx, cancel := cloudWorkspaceAuthRetryContext(ctx)
				defer cancel()
				return a.cloudWorkspaceHubDoRecover(retryCtx, method, path, opt, false)
			}
			return nil, 0, err
		}
	}
	var reader io.Reader
	var contentLength int64
	switch {
	case opt.rawBody != nil:
		reader = bytes.NewReader(opt.rawBody)
		contentLength = int64(len(opt.rawBody))
	case opt.jsonBody != nil:
		raw, marshalErr := json.Marshal(opt.jsonBody)
		if marshalErr != nil {
			return nil, 0, marshalErr
		}
		reader = bytes.NewReader(raw)
		contentLength = int64(len(raw))
	}
	reqURL := strings.TrimRight(hubURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Machine-ID", machineID)
	if strings.TrimSpace(instanceSession.SessionToken) != "" {
		req.Header.Set(cloudWorkspaceInstanceSessionHeader, instanceSession.SessionToken)
	}
	// Negotiate the single writable protocol on every cloud-workspace API,
	// including workspace-task provisioning paths that do not contain a
	// workspace id yet. Lease/session fencing is attached only when an id can
	// be resolved from the URL.
	if strings.HasPrefix(path, cloudWorkspaceCollectionPath) || strings.HasPrefix(path, cloudWorkspaceTaskProvisionPath) || strings.TrimSpace(instanceSession.SessionToken) != "" {
		req.Header.Set("X-Cloud-Workspace-Protocol", cloudWorkspaceProtocolVersion)
	}
	if workspaceID := cloudWorkspaceWorkspaceIDFromPath(path); workspaceID != "" {
		fencing, session := cloudWorkspaceLeaseHeaders(ctx, workspaceID)
		if fencing != "" {
			req.Header.Set("X-Cloud-Workspace-Fencing", fencing)
		}
		if session != "" {
			req.Header.Set("X-Cloud-Workspace-Session", session)
		}
	}
	for key, value := range opt.headers {
		if strings.TrimSpace(key) != "" {
			req.Header.Set(key, value)
		}
	}
	if strings.TrimSpace(opt.accept) != "" {
		req.Header.Set("Accept", opt.accept)
	} else {
		req.Header.Set("Accept", "application/json")
	}
	switch {
	case opt.rawBody != nil:
		ct := strings.TrimSpace(opt.contentType)
		if ct == "" {
			ct = "application/octet-stream"
		}
		req.Header.Set("Content-Type", ct)
		req.ContentLength = contentLength
	case opt.jsonBody != nil:
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLength
	}
	timeout := opt.timeout
	if timeout <= 0 {
		timeout = cloudWorkspaceRequestTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining <= 0 {
			return nil, 0, ctx.Err()
		} else if remaining < timeout {
			timeout = remaining
		}
	}
	maxRead := opt.maxRead
	if maxRead <= 0 {
		maxRead = cloudWorkspaceResponseMaxSize
	}
	resp, err := cloudWorkspaceHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRead+1))
	if readErr != nil {
		return data, resp.StatusCode, fmt.Errorf("read Hub response: %w", readErr)
	}
	if int64(len(data)) > maxRead {
		return nil, resp.StatusCode, fmt.Errorf("Hub response exceeds %d byte limit", maxRead)
	}
	if requiresInstanceSession && cloudWorkspaceSessionRejected(resp.StatusCode, data) {
		invalidateCloudWorkspaceInstanceSession(hubURL, token, machineID, instanceSession.SessionToken)
	}
	if workspaceID != "" && cloudWorkspaceShareFallbackStatus(resp.StatusCode, path) {
		if sess, ok := a.lookupShareAccessSession(workspaceID); ok && validReferralHandoffHubURL(sess.HubURL) {
			return a.cloudWorkspaceHubDoWithShare(ctx, sess, method, path, opt)
		}
	}
	if allowAuthRecovery && resp.StatusCode == http.StatusUnauthorized && cloudWorkspaceUnauthorizedBody(data) && a.recoverCloudWorkspaceMachineAuthorization(token) {
		retryCtx, cancel := cloudWorkspaceAuthRetryContext(ctx)
		defer cancel()
		return a.cloudWorkspaceHubDoRecover(retryCtx, method, path, opt, false)
	}
	return data, resp.StatusCode, nil
}

// The open that discovered the stale token often has only a few seconds of
// deadline left. Re-enroll must not spend that budget, and the retry needs
// its own window.
func cloudWorkspaceAuthRetryContext(parent context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	return context.WithTimeout(base, cloudWorkspaceRequestTimeout)
}

var (
	cloudWorkspaceAuthRecoverMu    sync.Mutex
	cloudWorkspaceAuthRecoverLast  time.Time
	cloudWorkspaceAuthRecoverToken string
)

func resetCloudWorkspaceAuthRecovery() {
	cloudWorkspaceAuthRecoverMu.Lock()
	cloudWorkspaceAuthRecoverLast = time.Time{}
	cloudWorkspaceAuthRecoverToken = ""
	cloudWorkspaceAuthRecoverMu.Unlock()
}

// recoverCloudWorkspaceMachineAuthorization re-enrolls this PC against the
// configured Hub and stores the new machine token. A stale token makes every
// cloud-workspace open fail with "machine authorization required".
// rejectedToken is the bearer that Hub just refused. The token issued by the
// last successful re-enroll is not rotated again while that refusal stands.
func (a *App) recoverCloudWorkspaceMachineAuthorization(rejectedToken string) bool {
	if a == nil {
		return false
	}
	rejectedToken = strings.TrimSpace(rejectedToken)
	cloudWorkspaceAuthRecoverMu.Lock()
	defer cloudWorkspaceAuthRecoverMu.Unlock()
	if !cloudWorkspaceAuthRecoverLast.IsZero() && time.Since(cloudWorkspaceAuthRecoverLast) < 30*time.Second {
		if cloudWorkspaceAuthRecoverToken != "" && rejectedToken != cloudWorkspaceAuthRecoverToken {
			return true
		}
		return false
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		cloudWorkspaceAuthRecoverLast = time.Now()
		return false
	}
	email := strings.TrimSpace(cfg.RemoteEmail)
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	if email == "" || hubURL == "" {
		log.Printf("[cloud_workspace] machine re-enroll skipped: email or hub missing")
		cloudWorkspaceAuthRecoverLast = time.Now()
		return false
	}
	profile := a.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	log.Printf("[cloud_workspace] machine authorization rejected, re-enrolling hub=%s", hubURL)
	enrollClient := &remote.EnrollmentClient{HTTPClient: hubHTTPClient, EnrollTimeout: remoteEnrollTimeout}
	result, err := enrollClient.Enroll(context.Background(), remote.EnrollConfig{
		Email:        email,
		ClientID:     strings.TrimSpace(cfg.RemoteClientID),
		HubURL:       hubURL,
		MachineName:  profile.Name,
		Platform:     profile.Platform,
		Hostname:     profile.Hostname,
		Arch:         profile.Arch,
		AppVersion:   profile.AppVersion,
		HeartbeatSec: profile.HeartbeatSec,
		TenantID:     strings.TrimSpace(cfg.RemoteTenantID),
	})
	if err != nil {
		log.Printf("[cloud_workspace] machine re-enroll failed: %v", err)
		cloudWorkspaceAuthRecoverLast = time.Now()
		return false
	}
	if err := a.PatchConfig(func(next *corelib.AppConfig) {
		if strings.TrimSpace(result.Email) != "" {
			next.RemoteEmail = result.Email
		}
		if strings.TrimSpace(result.UserID) != "" {
			next.RemoteUserID = result.UserID
		}
		if strings.TrimSpace(result.TenantID) != "" {
			next.RemoteTenantID = result.TenantID
		}
		if strings.TrimSpace(result.TenantName) != "" {
			next.RemoteTenantName = result.TenantName
		}
		if strings.TrimSpace(result.SN) != "" {
			next.RemoteSN = result.SN
		}
		next.RemoteMachineID = result.MachineID
		next.RemoteMachineToken = result.MachineToken
		if strings.TrimSpace(result.ViewerToken) != "" {
			next.RemoteViewerToken = result.ViewerToken
		}
		if strings.TrimSpace(result.ClientID) != "" {
			next.RemoteClientID = result.ClientID
		}
		next.RemoteEnabled = true
	}); err != nil {
		log.Printf("[cloud_workspace] machine re-enroll persist failed: %v", err)
		cloudWorkspaceAuthRecoverLast = time.Now()
		return false
	}
	resetCloudWorkspaceInstanceSessions()
	cloudWorkspaceAuthRecoverLast = time.Now()
	cloudWorkspaceAuthRecoverToken = result.MachineToken
	log.Printf("[cloud_workspace] machine authorization recovered machine_id=%s", result.MachineID)
	go a.reconnectHubAfterCloudWorkspaceAuthRecovery()
	return true
}

func (a *App) reconnectHubAfterCloudWorkspaceAuthRecovery() {
	if a == nil || a.remoteSessions == nil || a.remoteSessions.hubClient == nil {
		return
	}
	client := a.remoteSessions.hubClient
	// A live socket was authenticated with the previous token. Dropping it
	// here also stops hardware and in-flight Hub calls. Leave it up; the next
	// reconnect reloads the token saved above. Connect only when already down.
	if client.IsConnected() {
		return
	}
	if err := client.Connect(); err != nil {
		log.Printf("[cloud_workspace] hub reconnect after re-enroll failed: %v", err)
	}
}

func (a *App) revokeCloudWorkspaceInstanceSession(ctx context.Context) error {
	hubURL, machineToken, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		return err
	}
	key := cloudWorkspaceInstanceSessionCacheKey(hubURL, machineID, machineToken)
	cloudWorkspaceSessions.Lock()
	entry := cloudWorkspaceSessions.items[key]
	if entry == nil || entry.ready != nil {
		cloudWorkspaceSessions.Unlock()
		return nil
	}
	session := entry.session
	cloudWorkspaceSessions.Unlock()

	path := cloudWorkspaceInstanceSessionsPath + "/" + url.PathEscape(session.SessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(hubURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+machineToken)
	req.Header.Set("X-Machine-ID", machineID)
	req.Header.Set("X-Cloud-Workspace-Protocol", cloudWorkspaceProtocolVersion)
	req.Header.Set(cloudWorkspaceInstanceSessionHeader, session.SessionToken)
	req.Header.Set("Accept", "application/json")
	resp, err := cloudWorkspaceHTTPClient(cloudWorkspaceRequestTimeout).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, cloudWorkspaceResponseMaxSize+1))
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cloudWorkspaceAPIError(resp.StatusCode, data)
	}
	invalidateCloudWorkspaceInstanceSession(hubURL, machineToken, machineID, session.SessionToken)
	return nil
}

// cloudWorkspaceWorkspaceIDFromPath extracts the escaped workspace segment
// from any cloud-workspace API path. It is deliberately conservative so a
// non-workspace request cannot accidentally inherit a fencing token.
func cloudWorkspaceShareFallbackStatus(status int, path string) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if status != http.StatusNotFound {
		return false
	}
	// Missing blobs are 404 for owners and recipients alike; retrying those
	// with a share token only doubles the round trip.
	return !strings.Contains(path, "/objects/")
}

func cloudWorkspaceWorkspaceIDFromPath(raw string) string {
	const prefix = "/api/v1/cloud-workspaces/"
	if !strings.HasPrefix(raw, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(raw, prefix)
	if rest == "" || rest == "entitlement" {
		return ""
	}
	seg := rest
	if idx := strings.IndexByte(seg, '/'); idx >= 0 {
		seg = seg[:idx]
	}
	id, err := url.PathUnescape(seg)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(id)
}

// cloudWorkspaceHubRequest calls a Hub cloud-workspace JSON API with the same
// Bearer + X-Machine-ID headers as virtualRepositorySyncRequest.
func (a *App) cloudWorkspaceHubRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	opt := cloudWorkspaceHTTPOptions{
		timeout: cloudWorkspaceRequestTimeout,
		maxRead: cloudWorkspaceResponseMaxSize,
		accept:  "application/json",
	}
	if body != nil {
		opt.jsonBody = body
	}
	return a.cloudWorkspaceHubDo(ctx, method, path, opt)
}

func (a *App) cloudWorkspaceRequestContext() (context.Context, context.CancelFunc) {
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	return context.WithTimeout(parent, cloudWorkspaceRequestTimeout)
}

func cloudWorkspaceMaxObjectChunkCount() int64 {
	n := cloudWorkspaceObjectMaxBytes / cloudWorkspaceChunkBytes
	if cloudWorkspaceObjectMaxBytes%cloudWorkspaceChunkBytes != 0 {
		n++
	}
	if n < 1 {
		return 1
	}
	return int64(n)
}

// cloudWorkspaceSyncTimeout covers one max-size object at the spec formula
// (direct PUT or 60s-per-chunk) plus a manifest round-trip.
func cloudWorkspaceSyncTimeout() time.Duration {
	direct := cloudWorkspaceTransferTimeout(cloudWorkspaceObjectMaxBytes)
	chunked := time.Duration(cloudWorkspaceMaxObjectChunkCount()) * cloudWorkspaceChunkTimeout
	d := direct
	if chunked > d {
		d = chunked
	}
	d += 60 * time.Second
	if d < 60*time.Second {
		return 60 * time.Second
	}
	return d
}

func cloudWorkspaceEntriesTimeout(entries []cloudWorkspaceManifestEntry) time.Duration {
	var total int64
	var chunks int64
	for _, e := range entries {
		if e.Size <= 0 {
			continue
		}
		total += e.Size
		if e.Size > cloudWorkspaceChunkBytes {
			n := e.Size / cloudWorkspaceChunkBytes
			if e.Size%cloudWorkspaceChunkBytes != 0 {
				n++
			}
			chunks += n
		}
	}
	d := cloudWorkspaceTransferTimeout(total)
	if extra := time.Duration(chunks) * cloudWorkspaceChunkTimeout; extra > d {
		d = extra
	}
	d += 60 * time.Second
	if min := cloudWorkspaceSyncTimeout(); d < min {
		return min
	}
	return d
}

// bindCloudWorkspaceTimeout applies the size-based budget without shrinking a
// longer parent, and without extending the short OnShutdown deadline.
func bindCloudWorkspaceTimeout(ctx context.Context, want time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if want <= 0 {
		want = cloudWorkspaceSyncTimeout()
	}
	if deadline, ok := ctx.Deadline(); ok {
		remain := time.Until(deadline)
		if remain <= cloudWorkspaceShutdownReleaseTimeout {
			return context.WithCancel(ctx)
		}
		if remain >= want {
			return context.WithCancel(ctx)
		}
	}
	child, cancel := context.WithTimeout(context.WithoutCancel(ctx), want)
	stop := context.AfterFunc(ctx, cancel)
	return child, func() {
		stop()
		cancel()
	}
}

func (a *App) cloudWorkspaceLongContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	if timeout <= 0 {
		timeout = cloudWorkspaceSyncTimeout()
	}
	return context.WithTimeout(parent, timeout)
}

func (a *App) cloudWorkspaceSyncContext() (context.Context, context.CancelFunc) {
	return a.cloudWorkspaceLongContext(cloudWorkspaceSyncTimeout())
}
