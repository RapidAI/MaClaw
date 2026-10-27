package guiapp

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// A link that still exists but is not in the owner's list (an expired
	// share) answers 200. Cache that answer briefly so a received share whose
	// 5-second poll runs all day does not probe the Hub every tick.
	fileCompanionShareProbeTTL     = 30 * time.Second
	fileCompanionShareProbeCap     = 12
	fileCompanionShareProbeTimeout = 5 * time.Second
)

var fileCompanionShareProbe = struct {
	mu      sync.Mutex
	checked map[string]time.Time
}{checked: map[string]time.Time{}}

// probeRevokedFileCompanionShares asks the Hub about previous cloud rows the
// list no longer returns. A 404 means the link was deleted; an expired link
// still answers 200 and stays on this machine. Only non-group rows are probed:
// a group share missing from the list is already withdrawn by membership
// revocation.
func (a *App) probeRevokedFileCompanionShares(hub, viewer string, previous []FileCompanionLibraryItem, remote []FileCompanionLibraryItem, dismissed map[string]bool) map[string]bool {
	revoked := map[string]bool{}
	if a == nil || len(previous) == 0 {
		return revoked
	}
	seen := map[string]bool{}
	for _, item := range remote {
		if item.Token != "" {
			seen[item.Token] = true
		}
	}
	now := time.Now()
	candidates := make([]string, 0, fileCompanionShareProbeCap)
	for _, prev := range previous {
		token := strings.TrimSpace(prev.Token)
		if prev.Source != "cloud" || !fileCompanionShareTokenMatch.MatchString(token) || seen[token] || dismissed[token] {
			continue
		}
		if prev.Visibility == "group" {
			continue
		}
		if fileCompanionShareProbeRecentlyChecked(token, now) {
			continue
		}
		candidates = append(candidates, token)
		if len(candidates) >= fileCompanionShareProbeCap {
			break
		}
	}
	if len(candidates) == 0 {
		return revoked
	}
	client := &http.Client{Timeout: fileCompanionShareProbeTimeout}
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, token := range candidates {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			deleted, err := probeFileCompanionShareDeleted(client, hub, viewer, token)
			// Mark the attempt even on a network error. Otherwise a Hub whose
			// single-link route is briefly failing would cost a 5-second probe
			// on every list refresh; the retry waits out the cache TTL.
			fileCompanionShareProbeMark(token)
			if err != nil {
				return
			}
			if deleted {
				mu.Lock()
				revoked[token] = true
				mu.Unlock()
			}
		}(token)
	}
	wg.Wait()
	return revoked
}

func fileCompanionShareProbeRecentlyChecked(token string, now time.Time) bool {
	fileCompanionShareProbe.mu.Lock()
	defer fileCompanionShareProbe.mu.Unlock()
	at, ok := fileCompanionShareProbe.checked[token]
	if !ok {
		return false
	}
	if now.Sub(at) < fileCompanionShareProbeTTL {
		return true
	}
	delete(fileCompanionShareProbe.checked, token)
	return false
}

func fileCompanionShareProbeMark(token string) {
	fileCompanionShareProbe.mu.Lock()
	defer fileCompanionShareProbe.mu.Unlock()
	if fileCompanionShareProbe.checked == nil {
		fileCompanionShareProbe.checked = map[string]time.Time{}
	}
	fileCompanionShareProbe.checked[token] = time.Now()
	if len(fileCompanionShareProbe.checked) > 4096 {
		cutoff := time.Now().Add(-10 * fileCompanionShareProbeTTL)
		for key, at := range fileCompanionShareProbe.checked {
			if at.Before(cutoff) {
				delete(fileCompanionShareProbe.checked, key)
			}
		}
	}
}

// probeFileCompanionShareDeleted returns true only for the Hub's explicit
// share-not-found. An old Hub without the single-link route also answers 404,
// but without the share JSON; treating that as a deletion would erase every
// received copy on an older deployment.
func probeFileCompanionShareDeleted(client *http.Client, hub, viewer, token string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(hub, "/")+"/api/mobile/documents/shares/"+token, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+viewer)
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		return false, nil
	case http.StatusNotFound:
		// The current Hub answers with both the code and the Chinese message.
		// A route that does not exist has neither, and must not erase the copy.
		if strings.Contains(string(body), `"SHARE_NOT_FOUND"`) || strings.Contains(string(body), "分享链接不存在") {
			return true, nil
		}
		return false, errors.New("share probe is not supported by this Hub")
	default:
		return false, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
}
