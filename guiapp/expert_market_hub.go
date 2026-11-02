package guiapp

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// resolveExpertMarketBase returns the HubCenter that stores this account's
// Expert Market. Pet Store listings are replicated across official nodes, but
// expert listings are not: a desktop enrolled on an empty peer otherwise shows
// an empty market and an empty library while the same session still sees both
// on the node that accepted the submissions.
//
// A private HubCenter is never probed, and the session token is only sent to
// the official centers that already share this cluster's session secret.
func (a *App) resolveExpertMarketBase() string {
	if a == nil {
		return ""
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	enrolled := skillMarketSessionBase(cfg)
	token := strings.TrimSpace(cfg.SkillMarketSessionToken)
	if enrolled == "" || token == "" || !officialHubCenterBase(enrolled) {
		return enrolled
	}
	for {
		a.expertMarketBaseMu.Lock()
		if base, ok := a.cachedExpertMarketBase(token); ok {
			a.expertMarketBaseMu.Unlock()
			return base
		}
		if a.expertMarketProbe != nil {
			wait := a.expertMarketProbe
			a.expertMarketBaseMu.Unlock()
			<-wait
			// The probe may belong to an older session. A mismatch must
			// search again; returning the enrolled node here shows an empty
			// market after the session has just been refreshed.
			continue
		}
		wait := make(chan struct{})
		a.expertMarketProbe = wait
		a.expertMarketBaseMu.Unlock()
		return a.finishExpertMarketProbe(wait, token, enrolled)
	}
}

func (a *App) finishExpertMarketProbe(wait chan struct{}, token, enrolled string) (base string) {
	defer func() {
		a.expertMarketBaseMu.Lock()
		if a.expertMarketProbe == wait {
			a.expertMarketProbe = nil
			close(wait)
		}
		a.expertMarketBaseMu.Unlock()
	}()
	best, complete, unauthorized := chooseExpertMarketBase(token, officialExpertMarketBases(enrolled))
	a.expertMarketBaseMu.Lock()
	defer a.expertMarketBaseMu.Unlock()
	switch {
	case unauthorized:
		// Every reached node rejected the session. The request path refreshes
		// it; caching the enrolled node would keep the empty catalogue in place.
		a.expertMarketBase = ""
		a.expertMarketBaseToken = ""
		a.expertMarketBaseUntil = time.Time{}
		return enrolled
	case best.ok && complete:
		a.expertMarketBase = best.base
		a.expertMarketBaseToken = token
		a.expertMarketBaseUntil = time.Time{}
	case best.ok:
		// A peer timed out. Keep the best answer briefly, including when it
		// already has a library: the timed-out node may hold more of it.
		a.expertMarketBase = best.base
		a.expertMarketBaseToken = token
		a.expertMarketBaseUntil = time.Now().Add(2 * time.Minute)
	default:
		a.expertMarketBase = enrolled
		a.expertMarketBaseToken = token
		a.expertMarketBaseUntil = time.Now().Add(30 * time.Second)
	}
	if chosen, ok := a.cachedExpertMarketBase(token); ok {
		return chosen
	}
	return enrolled
}

func (a *App) cachedExpertMarketBase(token string) (string, bool) {
	if a.expertMarketBase == "" || a.expertMarketBaseToken != token {
		return "", false
	}
	if !a.expertMarketBaseUntil.IsZero() && !time.Now().Before(a.expertMarketBaseUntil) {
		return "", false
	}
	return a.expertMarketBase, true
}

func officialHubCenterBase(base string) bool {
	base = remote.NormalizeHubCenterURL(base)
	if base == "" {
		return false
	}
	for _, candidate := range remote.DefaultRemoteHubCenterURLs {
		if remote.NormalizeHubCenterURL(candidate) == base {
			return true
		}
	}
	return false
}

func officialExpertMarketBases(enrolled string) []string {
	return remote.NormalizeHubCenterURLs(append([]string{enrolled}, remote.DefaultRemoteHubCenterURLs...))
}

type expertMarketHubScore struct {
	base         string
	library      int
	public       int
	ok           bool
	unauthorized bool
}

// chooseExpertMarketBase prefers the node where this session already has
// submissions or purchases. When every library is empty, it prefers the node
// whose public catalogue is larger. Equal scores keep the enrolled node,
// which is the first candidate.
func chooseExpertMarketBase(token string, bases []string) (expertMarketHubScore, bool, bool) {
	if len(bases) == 0 {
		return expertMarketHubScore{}, false, false
	}
	scores := make([]expertMarketHubScore, len(bases))
	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	var wg sync.WaitGroup
	for i, base := range bases {
		wg.Add(1)
		go func(i int, base string) {
			defer wg.Done()
			scores[i] = probeExpertMarketHub(client, base, token)
		}(i, base)
	}
	wg.Wait()
	best := expertMarketHubScore{}
	bestLibrary, bestPublic := -1, -1
	complete := true
	unauthorized := false
	for _, score := range scores {
		if score.unauthorized {
			unauthorized = true
		}
		if !score.ok {
			complete = false
			continue
		}
		if score.library > bestLibrary || (score.library == bestLibrary && score.public > bestPublic) {
			best = score
			bestLibrary = score.library
			bestPublic = score.public
		}
	}
	return best, complete, unauthorized && !best.ok
}

func probeExpertMarketHub(client *http.Client, base, token string) expertMarketHubScore {
	score := expertMarketHubScore{base: base}
	type probe struct {
		count  int
		status int
	}
	results := make([]probe, 2)
	var wg sync.WaitGroup
	targets := []struct {
		url     string
		library bool
	}{
		{base + "/api/v1/expert-market/account", true},
		{base + "/api/v1/expert-market/experts?page=1&page_size=1", false},
	}
	wg.Add(len(targets))
	for i, target := range targets {
		go func(i int, targetURL string, library bool) {
			defer wg.Done()
			results[i].count, results[i].status = expertMarketProbeCount(client, targetURL, token, library)
		}(i, target.url, target.library)
	}
	wg.Wait()
	if results[0].status == http.StatusUnauthorized || results[1].status == http.StatusUnauthorized {
		score.unauthorized = true
	}
	if results[0].status != http.StatusOK || results[1].status != http.StatusOK {
		return score
	}
	score.library = results[0].count
	score.public = results[1].count
	score.ok = true
	return score
}

func expertMarketProbeCount(client *http.Client, target, token string, library bool) (int, int) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return 0, 0
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0, resp.StatusCode
	}
	if resp.StatusCode != http.StatusOK {
		return 0, resp.StatusCode
	}
	var body struct {
		Uploads   []json.RawMessage `json:"uploads"`
		Purchases []json.RawMessage `json:"purchases"`
		Experts   []json.RawMessage `json:"experts"`
		Total     *int              `json:"total"`
	}
	if json.Unmarshal(data, &body) != nil {
		return 0, resp.StatusCode
	}
	if library {
		return len(body.Uploads) + len(body.Purchases), http.StatusOK
	}
	if body.Total != nil {
		return *body.Total, http.StatusOK
	}
	return len(body.Experts), http.StatusOK
}
