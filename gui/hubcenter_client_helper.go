package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/remote"
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
	a.rememberHubCenterSelectionThrottled(base, discovered)
}

func (a *App) getHubCenterJSON(ctx context.Context, client *http.Client, path string, limit int64, dest interface{}) (string, []string, error) {
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return "", nil, err
	}
	var lastErr error
	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return "", nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		ok := false
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
				lastErr = fmt.Errorf("request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
				return
			}
			reader := io.Reader(resp.Body)
			if limit > 0 {
				reader = io.LimitReader(resp.Body, limit)
			}
			if err := json.NewDecoder(reader).Decode(dest); err != nil {
				lastErr = err
				return
			}
			ok = true
		}()
		if ok {
			a.rememberHubCenterSelection(base, bases)
			return base, bases, nil
		}
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
			reader := io.Reader(resp.Body)
			if limit > 0 {
				reader = io.LimitReader(resp.Body, limit)
			}
			return io.ReadAll(reader)
		}()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		a.rememberHubCenterSelection(base, bases)
		return base, bases, data, nil
	}
	if lastErr != nil {
		return "", bases, nil, lastErr
	}
	return "", bases, nil, fmt.Errorf("no reachable hubcenter")
}
