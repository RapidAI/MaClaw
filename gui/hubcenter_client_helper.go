package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

func (a *App) resolveHubCenterBaseURL(ctx context.Context, client *http.Client) (string, []string, error) {
	if a == nil {
		return "", nil, fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", nil, err
	}
	urls := cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs)
	urls = remote.NormalizeHubCenterURLs(urls)
	if len(urls) == 0 && strings.TrimSpace(a.testHomeDir) != "" {
		testURLs := append([]string{cfg.RemoteHubCenterURL}, cfg.RemoteHubCenterURLs...)
		urls = remote.NormalizeHubCenterURLs(testURLs)
	}
	if len(urls) == 0 {
		return "", nil, fmt.Errorf("hubcenter URL not configured")
	}
	preferred := strings.TrimSpace(cfg.RemoteHubCenterURL)
	ordered := remote.DiscoverHubCenterURLs(ctx, client, urls, preferred)
	if len(ordered) == 0 {
		ordered = remote.SelectBestCenter(ctx, client, urls, preferred)
	}
	if len(ordered) == 0 {
		ordered = urls
	}
	ordered = remote.NormalizeHubCenterURLs(ordered)
	for _, candidate := range ordered {
		base := remote.NormalizeHubCenterURL(candidate)
		if base == "" {
			continue
		}
		view, err := remote.FetchHubCenterDiscovery(ctx, client, base)
		if err != nil || view == nil {
			continue
		}
		merged := append([]string{base}, ordered...)
		merged = append(merged, view.URLs...)
		for _, node := range view.Nodes {
			merged = append(merged, node.BaseURL)
		}
		return base, remote.NormalizeHubCenterURLs(merged), nil
	}
	return "", ordered, fmt.Errorf("no reachable hubcenter")
}

func (a *App) resolveHubCenterCandidates(ctx context.Context, client *http.Client) ([]string, error) {
	base, discovered, err := a.resolveHubCenterBaseURLCached(ctx, client)
	if err != nil {
		return nil, err
	}
	bases := append([]string{base}, discovered...)
	bases = append(bases, remote.DefaultRemoteHubCenterURLs...)
	bases = remote.NormalizeHubCenterURLs(bases)
	if len(bases) == 0 {
		return nil, fmt.Errorf("hubcenter URL not configured")
	}
	// Cached preferred may still be a node that just failed connectivity. Keep it in
	// the pool for recovery, but try clean nodes first so skill download / search
	// do not burn a per-node timeout on a known-dead host every request.
	return deprioritizeRecentlyFailedHubCenters(bases), nil
}

// deprioritizeRecentlyFailedHubCenters stable-partitions bases so hosts with recent
// probe failures are tried after clean hosts, without dropping any candidate.
func deprioritizeRecentlyFailedHubCenters(bases []string) []string {
	bases = remote.NormalizeHubCenterURLs(bases)
	if len(bases) <= 1 {
		return bases
	}
	healthy := make([]string, 0, len(bases))
	failed := make([]string, 0, len(bases))
	for _, base := range bases {
		if remote.HasRecentFailures(base) {
			failed = append(failed, base)
			continue
		}
		healthy = append(healthy, base)
	}
	if len(failed) == 0 || len(healthy) == 0 {
		return bases
	}
	return append(healthy, failed...)
}

func (a *App) rememberHubCenterSelection(base string, discovered []string) {
	if a == nil {
		return
	}
	// Delegate filtering to RememberSelectionThrottled: it prefers public URLs
	// when present, keeps pure-loopback chains for local/dev, and never replaces
	// a public preferred URL with loopback.
	a.rememberHubCenterSelectionThrottled(base, discovered)
}

func (a *App) getHubCenterJSON(ctx context.Context, client *http.Client, path string, limit int64, dest interface{}) (string, []string, error) {
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return "", nil, err
	}
	return a.getHubCenterJSONFromCandidates(ctx, client, bases, path, limit, dest)
}

func (a *App) getHubCenterJSONFromCandidates(ctx context.Context, client *http.Client, bases []string, path string, limit int64, dest interface{}) (string, []string, error) {
	bases = remote.NormalizeHubCenterURLs(bases)
	if len(bases) == 0 {
		return "", nil, fmt.Errorf("hubcenter URL not configured")
	}
	var lastErr error
	for _, base := range bases {
		for attempt := 0; attempt < 2; attempt++ {
			reqCtx, cancel := withHubCenterCandidateContext(ctx, client, len(bases))
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+path, nil)
			if err != nil {
				cancel()
				return "", nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				cancel()
				lastErr = fmt.Errorf("request hubcenter JSON %s%s failed: %w", base, path, err)
				if shouldDemoteHubCenterCandidate(ctx, err, 0) {
					remote.RecordProbeResult(base, false)
				}
				// Connectivity errors are not "unexpected EOF" retries.
				break
			}
			ok := false
			retryable := false
			statusCode := resp.StatusCode
			func() {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
					lastErr = fmt.Errorf("request hubcenter JSON %s%s failed (%d): %s", base, path, resp.StatusCode, strings.TrimSpace(string(body)))
					return
				}
				data, err := readHubCenterJSONBody(resp.Body, limit)
				if err != nil {
					lastErr = fmt.Errorf("read hubcenter JSON %s%s failed: %w", base, path, err)
					retryable = isUnexpectedEOFError(err)
					return
				}
				if err := json.Unmarshal(data, dest); err != nil {
					lastErr = fmt.Errorf("decode hubcenter JSON %s%s failed after %d bytes: %w", base, path, len(data), err)
					retryable = isUnexpectedEOFError(err)
					return
				}
				ok = true
			}()
			cancel()
			if ok {
				remote.RecordProbeResult(base, true)
				a.rememberHubCenterSelection(base, bases)
				return base, bases, nil
			}
			if shouldDemoteHubCenterCandidate(ctx, lastErr, statusCode) {
				remote.RecordProbeResult(base, false)
			}
			if !retryable || attempt == 1 {
				break
			}
		}
	}
	// All candidates failed — invalidate cache so next call re-discovers live nodes.
	// But do NOT invalidate on context cancellation (user cancelled, not server dead).
	if a.hubCenterCache != nil && ctx.Err() == nil {
		a.hubCenterCache.Invalidate()
	}
	if lastErr != nil {
		return "", bases, lastErr
	}
	return "", bases, fmt.Errorf("no reachable hubcenter")
}

func (a *App) getHubCenterBytes(ctx context.Context, client *http.Client, path string, limit int64) (string, []string, []byte, error) {
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return "", nil, nil, err
	}
	return a.getHubCenterBytesFromCandidates(ctx, client, bases, path, limit)
}

func (a *App) getHubCenterBytesFromCandidates(ctx context.Context, client *http.Client, bases []string, path string, limit int64) (string, []string, []byte, error) {
	bases = remote.NormalizeHubCenterURLs(bases)
	if len(bases) == 0 {
		return "", nil, nil, fmt.Errorf("hubcenter URL not configured")
	}
	var lastErr error
	// Bound per-node wait so a hung preferred node (e.g. dead hubs2) does not burn the
	// full client timeout before cluster failover can try the next live base.
	for _, base := range bases {
		reqCtx, cancel := withHubCenterCandidateContext(ctx, client, len(bases))
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+path, nil)
		if err != nil {
			cancel()
			return "", nil, nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			if shouldDemoteHubCenterCandidate(ctx, err, 0) {
				remote.RecordProbeResult(base, false)
			}
			continue
		}
		data, statusCode, readErr := func() ([]byte, int, error) {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				return nil, resp.StatusCode, fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			if limit > 0 {
				payload, err := readLimitedHubCenterBodyWithLength(resp.Body, resp.ContentLength, limit)
				return payload, resp.StatusCode, err
			}
			payload, err := io.ReadAll(resp.Body)
			return payload, resp.StatusCode, err
		}()
		cancel()
		if readErr != nil {
			lastErr = readErr
			if shouldDemoteHubCenterCandidate(ctx, readErr, statusCode) {
				remote.RecordProbeResult(base, false)
			}
			continue
		}
		remote.RecordProbeResult(base, true)
		a.rememberHubCenterSelection(base, bases)
		return base, bases, data, nil
	}
	// All candidates failed — invalidate cache so next call re-discovers live nodes.
	// But do NOT invalidate on context cancellation (user cancelled, not server dead).
	if a.hubCenterCache != nil && ctx.Err() == nil {
		a.hubCenterCache.Invalidate()
	}
	if lastErr != nil {
		return "", bases, nil, lastErr
	}
	return "", bases, nil, fmt.Errorf("no reachable hubcenter")
}

// hubCenterCandidateRequestTimeout returns a per-node deadline for multi-candidate
// downloads. Single-candidate requests keep the caller's full client timeout.
func hubCenterCandidateRequestTimeout(client *http.Client, candidateCount int) time.Duration {
	if candidateCount <= 1 {
		return 0
	}
	const perNode = 8 * time.Second
	if client == nil || client.Timeout <= 0 {
		return perNode
	}
	// Leave headroom for later candidates; never exceed the shared client budget.
	budget := client.Timeout / time.Duration(candidateCount)
	if budget < 3*time.Second {
		budget = 3 * time.Second
	}
	if budget > perNode {
		return perNode
	}
	return budget
}

// shouldDemoteHubCenterCandidate reports whether a failed request should lower the
// node's SelectBestCenter ranking. Parent-context cancellation and client 4xx
// (especially 404 skill-missing) must not poison an otherwise healthy node.
func shouldDemoteHubCenterCandidate(parentCtx context.Context, err error, statusCode int) bool {
	if parentCtx != nil && parentCtx.Err() != nil {
		return false
	}
	if statusCode > 0 {
		// 5xx / 408 / 429: node or gateway is unhealthy / overloaded.
		if statusCode >= 500 || statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests {
			return true
		}
		// Truncated 2xx bodies are transport-class failures, not content-missing.
		if statusCode >= 200 && statusCode < 300 && err != nil && isUnexpectedEOFError(err) {
			return true
		}
		// Other 2xx/3xx/4xx outcomes are request or content specific (e.g. 404 skill missing).
		return false
	}
	if err == nil {
		return false
	}
	// Per-node deadline exceeded with healthy parent → treat as node hang/dead.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		// Canceled here is from the per-node child context (parent already checked).
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "tls handshake timeout") ||
		isUnexpectedEOFError(err) {
		return true
	}
	return false
}

// withHubCenterCandidateContext applies the shared per-node timeout used by multi-candidate
// HubCenter requests. Caller must invoke the returned cancel func.
func withHubCenterCandidateContext(parent context.Context, client *http.Client, candidateCount int) (context.Context, context.CancelFunc) {
	timeout := hubCenterCandidateRequestTimeout(client, candidateCount)
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func readLimitedHubCenterBody(body io.Reader, limit int64) ([]byte, error) {
	return skill.ReadLimitedHTTPBody(body, -1, limit)
}

func readLimitedHubCenterBodyWithLength(body io.Reader, contentLength, limit int64) ([]byte, error) {
	return skill.ReadLimitedHTTPBody(body, contentLength, limit)
}

func readHubCenterJSONBody(body io.Reader, limit int64) ([]byte, error) {
	return skill.ReadLimitedHTTPBody(body, -1, limit)
}

func isUnexpectedEOFError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(msg, "unexpected eof") || strings.Contains(msg, "unexpected end of json")
}
