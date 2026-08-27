package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	cloudWorkspaceEntitlementPath      = "/api/v1/cloud-workspaces/entitlement"
	cloudWorkspaceCollectionPath       = "/api/v1/cloud-workspaces"
	cloudWorkspaceResponseMaxSize      = 3 << 20
	cloudWorkspaceManifestMaxSize      = 16 << 20
	cloudWorkspaceObjectMaxBytes       = 64 << 20
	cloudWorkspaceChunkBytes           = 8 << 20
	cloudWorkspaceRequestTimeout       = 30 * time.Second
	cloudWorkspaceChunkTimeout         = 60 * time.Second
	cloudWorkspaceHubUnavailableBanner = "Hub 不可用，云端工作区暂不可用"
	cloudWorkspaceBytesPerTimeoutSec   = 262144
)

func cloudWorkspaceItemPath(id string) string {
	return cloudWorkspaceCollectionPath + "/" + url.PathEscape(strings.TrimSpace(id))
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
	timeout     time.Duration
	maxRead     int64
	accept      string
	contentType string
	jsonBody    any
	rawBody     []byte
}

func (a *App) cloudWorkspaceHubDo(ctx context.Context, method, path string, opt cloudWorkspaceHTTPOptions) ([]byte, int, error) {
	hubURL, token, machineID, err := a.virtualRepositorySyncClient()
	if err != nil {
		return nil, 0, err
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
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
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
