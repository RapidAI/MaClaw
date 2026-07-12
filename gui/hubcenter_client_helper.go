package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	return bases, nil
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
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
			if err != nil {
				return "", nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				lastErr = fmt.Errorf("request hubcenter JSON %s%s failed: %w", base, path, err)
				continue
			}
			ok := false
			truncated := false
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
					truncated = isUnexpectedEOFError(err)
					return
				}
				if err := json.Unmarshal(data, dest); err != nil {
					lastErr = fmt.Errorf("decode hubcenter JSON %s%s failed after %d bytes: %w", base, path, len(data), err)
					truncated = isUnexpectedEOFError(err)
					return
				}
				ok = true
			}()
			if ok {
				a.rememberHubCenterSelection(base, bases)
				return base, bases, nil
			}
			if !truncated || attempt == 1 {
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
	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return "", nil, nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, readErr := func() ([]byte, error) {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				return nil, fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
			}
			if limit > 0 {
				return readLimitedHubCenterBodyWithLength(resp.Body, resp.ContentLength, limit)
			}
			return io.ReadAll(resp.Body)
		}()
		if readErr != nil {
			lastErr = readErr
			continue
		}
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
