package guiapp

import (
	"encoding/json"
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
func (a *App) probeRevokedFileCompanionShares(hub, viewer string, previous []FileCompanionLibraryItem, remote []FileCompanionLibraryItem, dismissed map[string]bool) (map[string]bool, map[string]fileCompanionShareView) {
	revoked := map[string]bool{}
	foreignViews := map[string]fileCompanionShareView{}
	if a == nil || len(previous) == 0 {
		return revoked, foreignViews
	}
	seen := map[string]bool{}
	for _, item := range remote {
		if item.Token != "" {
			seen[item.Token] = true
		}
	}
	now := time.Now()
	grants := map[string]string{}
	if a != nil {
		fileCompanionLibraryMu.Lock()
		grants = a.readFileCompanionShareGrantsLocked()
		fileCompanionLibraryMu.Unlock()
	}
	type probeJob struct {
		token   string
		origin  string
		grant   string
		foreign bool
	}
	jobs := make([]probeJob, 0, fileCompanionShareProbeCap*2)
	localN := 0
	foreignN := 0
	for _, prev := range previous {
		token := strings.TrimSpace(prev.Token)
		if prev.Source != "cloud" || !fileCompanionShareTokenMatch.MatchString(token) || seen[token] || dismissed[token] {
			continue
		}
		if prev.Visibility == "group" {
			continue
		}
		origin := fileCompanionHubOrigin(prev.HubOrigin)
		foreign := origin != "" && origin != fileCompanionHubOrigin(hub)
		// A foreign share is refreshed on every list: that GET is how notes
		// and new revisions arrive. The 30-second cache is only for home-hub
		// links that are missing because they expired, not because they moved.
		if !foreign && fileCompanionShareProbeRecentlyChecked(token, now) {
			continue
		}
		if foreign {
			if foreignN >= fileCompanionShareProbeCap {
				continue
			}
			foreignN++
			jobs = append(jobs, probeJob{token: token, origin: origin, grant: grants[token], foreign: true})
			continue
		}
		if localN >= fileCompanionShareProbeCap {
			continue
		}
		localN++
		jobs = append(jobs, probeJob{token: token})
	}
	if len(jobs) == 0 {
		return revoked, foreignViews
	}
	client := fileCompanionDirectClient(fileCompanionShareProbeTimeout)
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, job := range jobs {
		wg.Add(1)
		go func(job probeJob) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			target := hub
			auth := viewer
			if job.foreign {
				target = job.origin
				auth = ""
			}
			deleted, view, err := probeFileCompanionShareDeleted(client, target, auth, job.grant, job.token)
			if !job.foreign {
				// Mark the attempt even on a network error. Otherwise a Hub whose
				// single-link route is briefly failing would cost a 5-second probe
				// on every list refresh; the retry waits out the cache TTL.
				fileCompanionShareProbeMark(job.token)
			}
			if err != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if deleted {
				revoked[job.token] = true
				return
			}
			if job.foreign && view != nil {
				foreignViews[job.token] = *view
			}
		}(job)
	}
	wg.Wait()
	return revoked, foreignViews
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
func probeFileCompanionShareDeleted(client *http.Client, hub, viewer, grant, token string) (bool, *fileCompanionShareView, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(hub, "/")+"/api/mobile/documents/shares/"+token, nil)
	if err != nil {
		return false, nil, err
	}
	fileCompanionShareAuthorize(req, viewer, grant)
	resp, err := client.Do(req)
	if err != nil {
		return false, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		var view fileCompanionShareView
		if json.Unmarshal(body, &view) != nil || !fileCompanionShareTokenMatch.MatchString(strings.TrimSpace(view.Token)) {
			return false, nil, nil
		}
		return false, &view, nil
	case http.StatusNotFound:
		// The current Hub answers with both the code and the Chinese message.
		// A route that does not exist has neither, and must not erase the copy.
		if strings.Contains(string(body), `"SHARE_NOT_FOUND"`) || strings.Contains(string(body), "分享链接不存在") {
			return true, nil, nil
		}
		return false, nil, errors.New("share probe is not supported by this Hub")
	default:
		return false, nil, &fileCompanionShareStatus{status: resp.StatusCode, message: fileCompanionShareMessage(body)}
	}
}
