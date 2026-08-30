package websearch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	WebSearchMaclawHubTimeout         = 180 * time.Second
	WebSearchMaclawHubDownloadTimeout = WebSearchMaclawHubTimeout
	WebSearchEngineMaclawHub          = "maclaw_hub"
	strategyMaclawHubTimeout          = WebSearchMaclawHubTimeout
)

var (
	// WebSearchEngineMaclawHub is the opt-in MaClaw Hub/RapidSearch engine ID.
	// Keep the identifier in this package so strategy normalization, auth and
	// download routing all share one stable value.
	WebSearchEngineMaclawHub          = "maclaw_hub"
	WebSearchMaclawHubTimeout         = 180 * time.Second
	WebSearchMaclawHubDownloadTimeout = WebSearchMaclawHubTimeout
	// strategyMaclawHubTimeout gives the authenticated Hub proxy enough time to
	// complete a remote search while remaining bounded by the caller context.
	strategyMaclawHubTimeout = WebSearchMaclawHubTimeout

	// strategySearchTimeout is the HTTP/API portion of a runtime search. When a
	// browser engine or browser fallback is reachable, strategySearchBudget adds
	// strategyBrowserTimeout so the last-resort path is not starved by retries.
	strategySearchTimeout       = 30 * time.Second
	strategyBrowserTimeout      = 15 * time.Second
	strategyEngineTimeout       = 6 * time.Second
	strategyProbeEngineTimeout  = 12 * time.Second
	strategyProbeBrowserTimeout = 30 * time.Second
	strategyRetryDelay          = 200 * time.Millisecond
)

var searchHTTPStatusPattern = regexp.MustCompile(`(?i)\bHTTP\s+([1-5][0-9]{2})\b`)

var builtinWebSearchEngines = map[string]struct {
	Transport string
	Name      string
	BaseURL   string
	NeedsKey  bool
	OptIn     bool
}{
	"bing_cn":                {Transport: corelib.WebSearchTransportHTTPHTML, Name: "Bing", BaseURL: defaultBingSearchURL},
	"baidu":                  {Transport: corelib.WebSearchTransportHTTPHTML, Name: "百度", BaseURL: defaultBaiduSearchURL},
	"google":                 {Transport: corelib.WebSearchTransportBrowser, Name: "Google"},
	"duckduckgo":             {Transport: corelib.WebSearchTransportHTTPHTML, Name: "DuckDuckGo", BaseURL: defaultLegacySearchURL},
	"brave":                  {Transport: corelib.WebSearchTransportAPI, Name: "Brave Search API", BaseURL: defaultBraveSearchURL, NeedsKey: true},
	"serper":                 {Transport: corelib.WebSearchTransportAPI, Name: "Serper（Google API）", BaseURL: defaultSerperSearchURL, NeedsKey: true},
	"tinyfish":               {Transport: corelib.WebSearchTransportAPI, Name: "TinyFish", BaseURL: defaultTinyFishSearchURL, NeedsKey: true},
	"tavily":                 {Transport: corelib.WebSearchTransportAPI, Name: "Tavily", BaseURL: defaultTavilySearchURL, NeedsKey: true},
	WebSearchEngineMaclawHub: {Transport: corelib.WebSearchTransportAPI, Name: "MaClaw Hub / RapidSearch", BaseURL: defaultMaclawHubSearchURL, OptIn: true},
}

var searchQueryHashSalt = func() [32]byte {
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		// crypto/rand failures are exceptional. Hashing a timestamp still keeps the
		// query out of logs and avoids falling back to an unsalted fingerprint.
		return sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	return salt
}()

// SearchAttempt is a safe diagnostic for one engine attempt. It contains no
// query text, API key, cookies, or page content.
type SearchAttempt struct {
	EngineID    string `json:"engine_id"`
	Transport   string `json:"transport"`
	DurationMS  int64  `json:"duration_ms"`
	ResultCount int    `json:"result_count"`
	RetryCount  int    `json:"retry_count,omitempty"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail,omitempty"`
}

// SearchResponse contains user-facing results and internal degradation data.
type SearchResponse struct {
	Results     []SearchResult  `json:"results"`
	Diagnostics []SearchAttempt `json:"diagnostics,omitempty"`
	Degraded    bool            `json:"degraded"`
}

func DefaultWebSearchStrategy(preset string) corelib.WebSearchStrategy {
	preset = normalizePreset(preset)
	var order []string
	if preset == corelib.WebSearchPresetInternational {
		order = []string{"google", "duckduckgo", "bing_cn", "baidu", "brave", "serper", "tinyfish", "tavily", WebSearchEngineMaclawHub}
	} else {
		preset = corelib.WebSearchPresetMainland
		order = []string{"bing_cn", "baidu", "duckduckgo", "google", "brave", "serper", "tinyfish", "tavily", WebSearchEngineMaclawHub}
	}
	engines := make([]corelib.WebSearchEngineConfig, 0, len(order))
	for i, id := range order {
		meta := builtinWebSearchEngines[id]
		engines = append(engines, corelib.WebSearchEngineConfig{
			ID: id, Enabled: !meta.NeedsKey && !meta.OptIn, Priority: i + 1,
			Transport: meta.Transport, BaseURL: meta.BaseURL,
		})
	}
	return corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion, Preset: preset,
		Mode: corelib.WebSearchModePriority, Engines: engines,
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: defaultBrowserFallbackEngineID(preset),
		// Manual verification can foreground a browser tab and wait substantially
		// longer than an ordinary search, so it must be an explicit opt-in.
		BrowserHumanAssistEnabled: false,
		HedgingDelayMS:            500, MinResultsBeforeHedge: 3,
	}
}

func normalizePreset(preset string) string {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == corelib.WebSearchPresetInternational || preset == corelib.WebSearchPresetCustom {
		return preset
	}
	return corelib.WebSearchPresetMainland
}

// NormalizeWebSearchStrategy validates built-in IDs/transports, fills missing
// entries and returns a stable, consecutively-prioritized strategy.
func NormalizeWebSearchStrategy(strategy corelib.WebSearchStrategy) (corelib.WebSearchStrategy, error) {
	if strategy.Version <= 0 || len(strategy.Engines) == 0 {
		return DefaultWebSearchStrategy(corelib.WebSearchPresetMainland), nil
	}
	strategy.Version = corelib.WebSearchStrategyVersion
	strategy.Preset = normalizePreset(strategy.Preset)
	strategy.Mode = strings.ToLower(strings.TrimSpace(strategy.Mode))
	// Smart and aggregate are reserved schema values for a later release. Until
	// their schedulers exist, normalize them to the behavior we actually run so
	// persisted or externally supplied configuration never over-promises.
	if strategy.Mode != corelib.WebSearchModePriority {
		strategy.Mode = corelib.WebSearchModePriority
	}
	if strategy.HedgingDelayMS <= 0 || strategy.HedgingDelayMS > 5000 {
		strategy.HedgingDelayMS = 500
	}
	if strategy.MinResultsBeforeHedge <= 0 || strategy.MinResultsBeforeHedge > 20 {
		strategy.MinResultsBeforeHedge = 3
	}
	strategy.BrowserFallbackEngineID = strings.ToLower(strings.TrimSpace(strategy.BrowserFallbackEngineID))
	if strategy.BrowserFallbackEngineID == "" {
		strategy.BrowserFallbackEngineID = defaultBrowserFallbackEngineID(strategy.Preset)
	}
	if strategy.BrowserFallbackEngineID != "bing_cn" && strategy.BrowserFallbackEngineID != "google" {
		return corelib.WebSearchStrategy{}, fmt.Errorf("browser fallback engine must be bing_cn or google")
	}

	seen := make(map[string]bool, len(builtinWebSearchEngines))
	seenInputIDs := make(map[string]bool, len(strategy.Engines))
	engines := append([]corelib.WebSearchEngineConfig(nil), strategy.Engines...)
	sort.SliceStable(engines, func(i, j int) bool { return engines[i].Priority < engines[j].Priority })
	out := make([]corelib.WebSearchEngineConfig, 0, len(builtinWebSearchEngines))
	for _, engine := range engines {
		engine.ID = strings.ToLower(strings.TrimSpace(engine.ID))
		// Mojeek no longer permits this application's automated HTML queries.
		// Drop the retired catalog entry while migrating persisted strategies.
		if engine.ID == "mojeek" {
			continue
		}
		if seenInputIDs[engine.ID] {
			return corelib.WebSearchStrategy{}, fmt.Errorf("duplicate web search engine %q", engine.ID)
		}
		seenInputIDs[engine.ID] = true
		meta, ok := builtinWebSearchEngines[engine.ID]
		if !ok {
			return corelib.WebSearchStrategy{}, fmt.Errorf("unknown web search engine %q", engine.ID)
		}
		seen[engine.ID] = true
		engine.Transport = strings.ToLower(strings.TrimSpace(engine.Transport))
		if engine.Transport == "" {
			engine.Transport = meta.Transport
		}
		if engine.Transport != meta.Transport {
			return corelib.WebSearchStrategy{}, fmt.Errorf("engine %s requires transport %s", engine.ID, meta.Transport)
		}
		engine.APIKey = strings.TrimSpace(engine.APIKey)
		engine.BaseURL = strings.TrimRight(strings.TrimSpace(engine.BaseURL), "/")
		if engine.BaseURL == "" {
			engine.BaseURL = meta.BaseURL
		}
		if engine.BaseURL != "" {
			if err := validateWebSearchBaseURL(engine.BaseURL); err != nil {
				return corelib.WebSearchStrategy{}, fmt.Errorf("engine %s base URL: %w", engine.ID, err)
			}
		}
		if engine.Enabled && meta.NeedsKey && engine.APIKey == "" {
			return corelib.WebSearchStrategy{}, fmt.Errorf("engine %s requires an API key", engine.ID)
		}
		out = append(out, engine)
	}
	defaults := DefaultWebSearchStrategy(strategy.Preset)
	for _, engine := range defaults.Engines {
		if !seen[engine.ID] {
			// A partial explicit strategy is commonly used by provider tests and
			// API clients. Backfill catalog entries for UI stability, but never
			// enable an engine the caller did not request.
			engine.Enabled = false
			out = append(out, engine)
		}
	}
	for i := range out {
		out[i].Priority = i + 1
	}
	strategy.Engines = out
	return strategy, nil
}

func validateWebSearchBaseURL(raw string) error {
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("must use HTTP or HTTPS")
	}
	if u.User != nil {
		return fmt.Errorf("must not contain embedded credentials")
	}
	return nil
}

// MigrateLegacyWebSearchStrategy creates a v1 strategy while preserving the
// legacy selected provider, keys and custom API base URLs.
func MigrateLegacyWebSearchStrategy(strategy corelib.WebSearchStrategy, providers []corelib.WebSearchProvider, current string) corelib.WebSearchStrategy {
	if strategy.Version > 0 && len(strategy.Engines) > 0 {
		normalized, err := NormalizeWebSearchStrategy(strategy)
		if err == nil {
			return normalized
		}
	}
	strategy = DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	byID := make(map[string]corelib.WebSearchProvider, len(providers))
	for _, provider := range providers {
		id := strings.ToLower(strings.TrimSpace(provider.Type))
		if _, ok := builtinWebSearchEngines[id]; ok {
			byID[id] = provider
		}
	}
	current = strings.ToLower(strings.TrimSpace(current))
	for i := range strategy.Engines {
		engine := &strategy.Engines[i]
		if provider, ok := byID[engine.ID]; ok {
			engine.APIKey = strings.TrimSpace(provider.Key)
			if strings.TrimSpace(provider.BaseURL) != "" {
				engine.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
			}
			if builtinWebSearchEngines[engine.ID].NeedsKey && engine.APIKey != "" {
				engine.Enabled = true
			}
		}
	}
	if _, ok := builtinWebSearchEngines[current]; ok {
		strategy.Preset = corelib.WebSearchPresetCustom
		for i := range strategy.Engines {
			if strategy.Engines[i].ID == current {
				strategy.Engines[i].Enabled = !builtinWebSearchEngines[current].NeedsKey || strategy.Engines[i].APIKey != ""
				selected := strategy.Engines[i]
				strategy.Engines = append([]corelib.WebSearchEngineConfig{selected}, append(strategy.Engines[:i], strategy.Engines[i+1:]...)...)
				break
			}
		}
	}
	for i := range strategy.Engines {
		strategy.Engines[i].Priority = i + 1
	}
	return strategy
}

func ResetWebSearchStrategy(current corelib.WebSearchStrategy, preset string) corelib.WebSearchStrategy {
	reset := DefaultWebSearchStrategy(preset)
	secrets := make(map[string]corelib.WebSearchEngineConfig, len(current.Engines))
	for _, engine := range current.Engines {
		secrets[engine.ID] = engine
	}
	for i := range reset.Engines {
		if old, ok := secrets[reset.Engines[i].ID]; ok {
			reset.Engines[i].APIKey = old.APIKey
			if old.BaseURL != "" {
				reset.Engines[i].BaseURL = old.BaseURL
			}
		}
	}
	reset.BrowserFallbackEnabled = current.BrowserFallbackEnabled
	reset.BrowserHumanAssistEnabled = current.BrowserHumanAssistEnabled
	if current.BrowserFallbackEngineID == "google" || current.BrowserFallbackEngineID == "bing_cn" {
		reset.BrowserFallbackEngineID = current.BrowserFallbackEngineID
	}
	return reset
}

func strategySearchBudget(strategy corelib.WebSearchStrategy) time.Duration {
	if strategy.BrowserHumanAssistEnabled && browserWorkConfigured(strategy) {
		return 2 * time.Minute
	}
	if browserWorkReachable(strategy) {
		budget := strategySearchTimeout + strategyBrowserTimeout
		for _, engine := range strategy.Engines {
			if engine.Enabled && engine.ID == WebSearchEngineMaclawHub && budget < strategyMaclawHubTimeout {
				return strategyMaclawHubTimeout
			}
		}
		return budget
	}
	for _, engine := range strategy.Engines {
		if engine.Enabled && engine.ID == WebSearchEngineMaclawHub && strategySearchTimeout < strategyMaclawHubTimeout {
			return strategyMaclawHubTimeout
		}
	}
	return strategySearchTimeout
}

func hasEnabledBrowserEngine(strategy corelib.WebSearchStrategy) bool {
	for _, engine := range strategy.Engines {
		if engine.Enabled && engine.Transport == corelib.WebSearchTransportBrowser {
			return true
		}
	}
	return false
}

func browserWorkConfigured(strategy corelib.WebSearchStrategy) bool {
	return strategy.BrowserFallbackEnabled || hasEnabledBrowserEngine(strategy)
}

func browserWorkReachable(strategy corelib.WebSearchStrategy) bool {
	return getBrowserSearchProvider() != nil && browserWorkConfigured(strategy)
}

func browserAttemptTimeout(strategy corelib.WebSearchStrategy) time.Duration {
	if strategy.BrowserHumanAssistEnabled {
		return 100 * time.Second
	}
	return strategyBrowserTimeout
}

func contextRemaining(ctx context.Context) time.Duration {
	if ctx == nil {
		return 0
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Hour
	}
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func stillNeedsBrowserAttempt(strategy corelib.WebSearchStrategy, attemptedBrowser map[string]bool, hint string) bool {
	if getBrowserSearchProvider() == nil {
		return false
	}
	for _, engine := range strategy.Engines {
		if engine.Enabled && engine.Transport == corelib.WebSearchTransportBrowser && !attemptedBrowser[engine.ID] {
			return true
		}
	}
	return len(remainingBrowserFallbackIDs(strategy, attemptedBrowser, hint)) > 0
}

func shouldSkipToPreserveBrowserBudget(ctx context.Context, engine corelib.WebSearchEngineConfig, strategy corelib.WebSearchStrategy, attemptedBrowser map[string]bool, hint string) bool {
	if engine.Transport == corelib.WebSearchTransportBrowser {
		return false
	}
	if !stillNeedsBrowserAttempt(strategy, attemptedBrowser, hint) {
		return false
	}
	return contextRemaining(ctx) <= browserAttemptTimeout(strategy)
}

func shouldSkipHTMLAfterConsecutiveMisses(engine corelib.WebSearchEngineConfig, htmlUnavailable int, strategy corelib.WebSearchStrategy, attemptedBrowser map[string]bool, hint string) bool {
	if engine.Transport != corelib.WebSearchTransportHTTPHTML || htmlUnavailable < htmlMissSkipThreshold(hint) {
		return false
	}
	return stillNeedsBrowserAttempt(strategy, attemptedBrowser, hint)
}

func browserFallbackParent(parent, ctx context.Context, strategy corelib.WebSearchStrategy) context.Context {
	if ctx != nil && ctx.Err() == nil && contextRemaining(ctx) >= browserAttemptTimeout(strategy) {
		return ctx
	}
	if parent != nil && parent.Err() == nil {
		return parent
	}
	if ctx != nil {
		return ctx
	}
	return parent
}

// SearchWithStrategyCtx runs enabled engines in user order. Small result sets
// are accumulated so a degraded engine cannot prematurely end the search.
func SearchWithStrategyCtx(parent context.Context, query string, maxResults int, strategy corelib.WebSearchStrategy) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResponse{}, fmt.Errorf("query is empty")
	}
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}
	var err error
	strategy, err = NormalizeWebSearchStrategy(strategy)
	if err != nil {
		return SearchResponse{}, err
	}
	publicNetworkOnly := isPublicNetworkOnly(parent)
	if publicNetworkOnly {
		strategy.BrowserFallbackEnabled = false
		strategy.BrowserHumanAssistEnabled = false
		for i := range strategy.Engines {
			// API engines use user-configured credentials and Baidu obtains a
			// verification cookie before searching. Neither belongs to the
			// public, unauthenticated group-search contract.
			if strategy.Engines[i].Transport != corelib.WebSearchTransportHTTPHTML || strategy.Engines[i].ID == "baidu" {
				strategy.Engines[i].Enabled = false
			}
		}
	}
	if parent == nil {
		parent = context.Background()
	}
	totalTimeout := strategySearchBudget(strategy)
	ctx, cancel := context.WithTimeout(parent, totalTimeout)
	defer cancel()

	response := SearchResponse{}
	var candidates []SearchResult
	var failures []string
	attemptedBrowserEngines := make(map[string]bool)
	hint := searchQuerySiteHint(query)
	htmlUnavailable := 0
	for _, engine := range strategy.Engines {
		if !engine.Enabled {
			continue
		}
		if builtinWebSearchEngines[engine.ID].NeedsKey && engine.APIKey == "" {
			response.Diagnostics = append(response.Diagnostics, SearchAttempt{EngineID: engine.ID, Transport: engine.Transport, Outcome: "skipped", Detail: "missing API key"})
			continue
		}
		if shouldSkipToPreserveBrowserBudget(ctx, engine, strategy, attemptedBrowserEngines, hint) {
			attempt := SearchAttempt{EngineID: engine.ID, Transport: engine.Transport, Outcome: "skipped", Detail: "reserved remaining time for browser fallback"}
			response.Diagnostics = append(response.Diagnostics, attempt)
			logSearchAttempt(query, attempt)
			continue
		}
		if shouldSkipHTMLAfterConsecutiveMisses(engine, htmlUnavailable, strategy, attemptedBrowserEngines, hint) {
			attempt := SearchAttempt{EngineID: engine.ID, Transport: engine.Transport, Outcome: "skipped", Detail: "prior HTML engines missed the query; using browser fallback"}
			response.Diagnostics = append(response.Diagnostics, attempt)
			logSearchAttempt(query, attempt)
			continue
		}
		if engine.Transport == corelib.WebSearchTransportBrowser {
			attemptedBrowserEngines[engine.ID] = true
		}
		preserve := time.Duration(0)
		if stillNeedsBrowserAttempt(strategy, attemptedBrowserEngines, hint) && engine.Transport != corelib.WebSearchTransportBrowser {
			preserve = browserAttemptTimeout(strategy)
		}
		results, attemptErr, elapsed, retryCount := searchStrategyEnginePreserving(ctx, query, maxResults, engine, strategy.BrowserHumanAssistEnabled, preserve)
		validResults := mergeSearchResultsPrefer(nil, results, maxResults, hint)
		attempt := SearchAttempt{EngineID: engine.ID, Transport: engine.Transport, DurationMS: elapsed.Milliseconds(), ResultCount: len(validResults), RetryCount: retryCount}
		if attemptErr != nil {
			attempt.Outcome = classifySearchError(attemptErr)
			attempt.Detail = safeSearchErrorDetail(attemptErr)
			failures = append(failures, fmt.Sprintf("%s: %s", engine.ID, attempt.Detail))
		} else if len(validResults) == 0 {
			attempt.Outcome = "no_results"
			if len(results) > 0 {
				failures = append(failures, engine.ID+": no valid results")
			} else {
				failures = append(failures, engine.ID+": no results")
			}
		} else {
			attempt.Outcome = "success"
			candidates = mergeSearchResultsPrefer(candidates, validResults, maxResults, hint)
		}
		if engine.Transport == corelib.WebSearchTransportHTTPHTML {
			uncovered := hint != "" && !resultsCoverSiteHint(validResults, hint)
			if attemptErr != nil || len(validResults) == 0 || uncovered {
				htmlUnavailable++
			} else {
				htmlUnavailable = 0
			}
		} else if engine.Transport == corelib.WebSearchTransportBrowser && hint != "" {
			// A real browser that missed the named site will not be beaten by
			// another HTML scrape; skip remaining HTTP engines and keep the
			// other browser fallback.
			if attemptErr != nil || len(validResults) == 0 || !resultsCoverSiteHint(validResults, hint) {
				htmlUnavailable = htmlMissSkipThreshold(hint)
			}
		}
		response.Diagnostics = append(response.Diagnostics, attempt)
		logSearchAttempt(query, attempt)
		if len(candidates) >= minInt(maxResults, strategy.MinResultsBeforeHedge) && (resultsCoverSiteHint(candidates, hint) || !stillNeedsBrowserAttempt(strategy, attemptedBrowserEngines, hint)) {
			response.Results = candidates
			response.Degraded = len(response.Diagnostics) > 1 || retryCount > 0
			return response, nil
		}
		if ctx.Err() != nil {
			break
		}
	}

	if getBrowserSearchProvider() != nil {
		for _, fallbackID := range remainingBrowserFallbackIDs(strategy, attemptedBrowserEngines, hint) {
			fallbackParent := browserFallbackParent(parent, ctx, strategy)
			if fallbackParent.Err() != nil {
				break
			}
			attemptedBrowserEngines[fallbackID] = true
			fallback := corelib.WebSearchEngineConfig{ID: fallbackID, Enabled: true, Transport: corelib.WebSearchTransportBrowser}
			results, attemptErr, elapsed, retryCount := searchStrategyEngine(fallbackParent, query, maxResults, fallback, strategy.BrowserHumanAssistEnabled)
			validResults := mergeSearchResultsPrefer(nil, results, maxResults, hint)
			attempt := SearchAttempt{EngineID: fallback.ID, Transport: fallback.Transport, DurationMS: elapsed.Milliseconds(), ResultCount: len(validResults), RetryCount: retryCount}
			if attemptErr == nil && len(validResults) > 0 {
				candidates = mergeSearchResultsPrefer(candidates, validResults, maxResults, hint)
				attempt.Outcome = "success"
				response.Diagnostics = append(response.Diagnostics, attempt)
				logSearchAttempt(query, attempt)
				if hint == "" || resultsCoverSiteHint(candidates, hint) {
					response.Results = candidates
					response.Degraded = true
					return response, nil
				}
				continue
			}
			if attemptErr != nil {
				attempt.Outcome = classifySearchError(attemptErr)
				attempt.Detail = safeSearchErrorDetail(attemptErr)
				failures = append(failures, fmt.Sprintf("browser fallback %s: %s", fallback.ID, attempt.Detail))
			} else {
				attempt.Outcome = "no_results"
				if len(results) > 0 {
					failures = append(failures, "browser fallback "+fallback.ID+": no valid results")
				} else {
					failures = append(failures, "browser fallback "+fallback.ID+": no results")
				}
			}
			response.Diagnostics = append(response.Diagnostics, attempt)
			logSearchAttempt(query, attempt)
		}
	}
	if len(candidates) > 0 {
		response.Results = candidates
		response.Degraded = true
		return response, nil
	}
	if err := parent.Err(); err != nil {
		return response, fmt.Errorf("web search interrupted: %w", err)
	}
	if len(failures) > 0 {
		return response, errors.New(strings.Join(failures, "; "))
	}
	if ctx.Err() != nil {
		return response, fmt.Errorf("web search interrupted: %w", ctx.Err())
	}
	return response, fmt.Errorf("no enabled web search engines are available")
}

// ProbeWebSearchEngineCtx validates one engine with a cold-start-friendly
// timeout. Runtime strategy searches intentionally keep their short per-engine
// budget so an unavailable provider does not stall the fallback chain; a
// settings probe is different because its first request may need DNS, proxy and
// TLS setup before the shared HTTP connection can be reused.
func ProbeWebSearchEngineCtx(parent context.Context, query string, maxResults int, engine corelib.WebSearchEngineConfig, humanAssist bool) (SearchResponse, error) {
	engine.ID = strings.ToLower(strings.TrimSpace(engine.ID))
	strategy := corelib.WebSearchStrategy{
		Version:                 corelib.WebSearchStrategyVersion,
		Preset:                  corelib.WebSearchPresetCustom,
		Mode:                    corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{engine},
		BrowserFallbackEngineID: "bing_cn",
		MinResultsBeforeHedge:   1,
	}
	normalized, err := NormalizeWebSearchStrategy(strategy)
	if err != nil {
		return SearchResponse{}, err
	}
	for _, candidate := range normalized.Engines {
		if candidate.ID == engine.ID {
			engine = candidate
			break
		}
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return SearchResponse{}, err
	}

	timeout := strategyProbeEngineTimeout
	if engine.Transport == corelib.WebSearchTransportBrowser {
		timeout = strategyProbeBrowserTimeout
		if humanAssist {
			timeout = 100 * time.Second
		}
	}
	results, attemptErr, elapsed, retryCount := searchStrategyEngineWithRetry(parent, query, maxResults, engine, humanAssist, timeout, 0)
	validResults := mergeSearchResults(nil, results, maxResults)
	attempt := SearchAttempt{
		EngineID: engine.ID, Transport: engine.Transport,
		DurationMS: elapsed.Milliseconds(), ResultCount: len(validResults), RetryCount: retryCount,
	}
	response := SearchResponse{Results: validResults, Diagnostics: []SearchAttempt{attempt}}
	if attemptErr != nil {
		response.Diagnostics[0].Outcome = classifySearchError(attemptErr)
		response.Diagnostics[0].Detail = safeSearchErrorDetail(attemptErr)
		logSearchAttempt(query, response.Diagnostics[0])
		return response, attemptErr
	}
	if len(validResults) == 0 {
		response.Diagnostics[0].Outcome = "no_results"
		logSearchAttempt(query, response.Diagnostics[0])
		if len(results) > 0 {
			return response, fmt.Errorf("%s returned no valid results", engine.ID)
		}
		return response, fmt.Errorf("%s returned no results", engine.ID)
	}
	response.Diagnostics[0].Outcome = "success"
	logSearchAttempt(query, response.Diagnostics[0])
	return response, nil
}

func logSearchAttempt(query string, attempt SearchAttempt) {
	// Log only a short irreversible query fingerprint. Never log query text,
	// credentials, cookies, result URLs, snippets, or browser page content.
	log.Printf("[web-search] query_hash=%s engine_id=%s transport=%s elapsed_ms=%d result_count=%d retry_count=%d outcome=%s",
		queryFingerprint(query), attempt.EngineID, attempt.Transport, attempt.DurationMS, attempt.ResultCount, attempt.RetryCount, attempt.Outcome)
}

func queryFingerprint(query string) string {
	h := sha256.New()
	_, _ = h.Write(searchQueryHashSalt[:])
	_, _ = h.Write([]byte(query))
	return fmt.Sprintf("%x", h.Sum(nil)[:8])
}

func searchStrategyEngine(parent context.Context, query string, maxResults int, engine corelib.WebSearchEngineConfig, humanAssist bool) ([]SearchResult, error, time.Duration, int) {
	return searchStrategyEnginePreserving(parent, query, maxResults, engine, humanAssist, 0)
}

func searchStrategyEnginePreserving(parent context.Context, query string, maxResults int, engine corelib.WebSearchEngineConfig, humanAssist bool, preserve time.Duration) ([]SearchResult, error, time.Duration, int) {
	timeout := strategyEngineTimeout
	if engine.Transport == corelib.WebSearchTransportBrowser {
		timeout = strategyBrowserTimeout
		if humanAssist {
			timeout = 100 * time.Second
		}
	}
	return searchStrategyEngineWithRetry(parent, query, maxResults, engine, humanAssist, timeout, preserve)
}

func searchStrategyEngineWithRetry(parent context.Context, query string, maxResults int, engine corelib.WebSearchEngineConfig, humanAssist bool, timeout, preserve time.Duration) ([]SearchResult, error, time.Duration, int) {
	started := time.Now()
	results, err, _ := searchStrategyEngineWithTimeout(parent, query, maxResults, engine, humanAssist, timeout)
	if err == nil || !shouldRetrySearchEngine(engine, err) || parent.Err() != nil {
		return results, err, time.Since(started), 0
	}
	if preserve > 0 && contextRemaining(parent) <= preserve {
		return results, err, time.Since(started), 0
	}
	// One retry absorbs transient DNS, proxy-connect, TLS and upstream 5xx
	// failures. Browser and human-verification paths are intentionally excluded:
	// repeating them can open duplicate tabs or verification prompts. Rate-limit
	// responses are also excluded: without honoring Retry-After, an immediate
	// retry is likely to consume more quota and fail again.
	if strategyRetryDelay > 0 {
		timer := time.NewTimer(strategyRetryDelay)
		select {
		case <-parent.Done():
			timer.Stop()
			return results, parent.Err(), time.Since(started), 0
		case <-timer.C:
		}
	}
	results, err, _ = searchStrategyEngineWithTimeout(parent, query, maxResults, engine, humanAssist, timeout)
	return results, err, time.Since(started), 1
}

func shouldRetrySearchEngine(engine corelib.WebSearchEngineConfig, err error) bool {
	if err == nil || engine.Transport == corelib.WebSearchTransportBrowser {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	text := strings.ToLower(err.Error())
	status := searchHTTPStatus(err)
	// A concrete 4xx response is a completed request, not a transport failure.
	// Provider response bodies are untrusted text and may contain words such as
	// "temporary" or "timeout"; do not let those words turn a stable client
	// error into an automatic retry. Explicitly recognized 5xx statuses are
	// handled below.
	if status >= 400 && status < 500 {
		return false
	}
	// Configuration/authentication and human-verification failures are stable;
	// an immediate repeat only burns quota or triggers more anti-bot pressure.
	for _, marker := range []string{"api key", "captcha", "verification", "challenge", "blocked"} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	return errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(text, "timeout") || strings.Contains(text, "timed out") ||
		strings.Contains(text, "connection reset") || strings.Contains(text, "connection refused") ||
		strings.Contains(text, "temporary") || strings.Contains(text, "tls handshake") ||
		strings.Contains(text, "no such host") || strings.Contains(text, "server misbehaving") ||
		status == 500 || status == 502 || status == 503 || status == 504
}

func searchHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	match := searchHTTPStatusPattern.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return 0
	}
	status, parseErr := strconv.Atoi(match[1])
	if parseErr != nil {
		return 0
	}
	return status
}

func searchStrategyEngineWithTimeout(parent context.Context, query string, maxResults int, engine corelib.WebSearchEngineConfig, humanAssist bool, timeout time.Duration) ([]SearchResult, error, time.Duration) {
	ctx := parent
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	}
	defer cancel()
	started := time.Now()
	var results []SearchResult
	var err error
	switch engine.Transport {
	case corelib.WebSearchTransportBrowser:
		results, err = searchBrowserEngine(ctx, engine.ID, query, maxResults, humanAssist)
	case corelib.WebSearchTransportHTTPHTML:
		switch engine.ID {
		case "bing_cn":
			results, err = searchBingDirectWithBaseURL(ctx, query, maxResults, engine.BaseURL)
		case "baidu":
			results, err = searchBaiduDirectWithBaseURL(ctx, query, maxResults, engine.BaseURL)
		case "duckduckgo":
			results, err = searchDuckDuckGo(ctx, corelib.WebSearchProvider{Type: engine.ID, BaseURL: engine.BaseURL}, query, maxResults)
		default:
			err = fmt.Errorf("unsupported HTTP search engine %s", engine.ID)
		}
	case corelib.WebSearchTransportAPI:
		// ctx already owns this attempt's deadline. Avoid layering another timer
		// with the same duration inside the provider dispatcher; one deadline is
		// easier to reason about and preserves the caller's cancellation cause.
		results, err = runProviderSearchWithTimeout(ctx, query, maxResults, corelib.WebSearchProvider{Type: engine.ID, Key: engine.APIKey, BaseURL: engine.BaseURL}, true, 0)
	default:
		err = fmt.Errorf("unsupported search transport %s", engine.Transport)
	}
	return results, err, time.Since(started)
}

func searchBrowserEngine(ctx context.Context, engineID, query string, maxResults int, humanAssist bool) ([]SearchResult, error) {
	hook := getBrowserSearchProvider()
	if hook == nil {
		return nil, fmt.Errorf("browser search is unavailable")
	}
	hits, err := hook(ctx, engineID, query, maxResults, humanAssist)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		results = append(results, SearchResult{Title: hit.Title, URL: hit.URL, Snippet: hit.Snippet})
	}
	return results, nil
}

func classifySearchError(err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "timed out") {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	status := searchHTTPStatus(err)
	for _, marker := range []string{"captcha", "verification", "challenge", "blocked", "反爬", "人机验证", "自動化查詢", "自动查询"} {
		if strings.Contains(text, marker) {
			return "blocked"
		}
	}
	if status == 401 || status == 403 || strings.Contains(text, "api key") || strings.Contains(text, "signed-in hub") || strings.Contains(text, "hub account") {
		return "invalid_key"
	}
	if status == 429 || strings.Contains(text, "rate limit") {
		return "rate_limited"
	}
	return "error"
}

// safeSearchErrorDetail deliberately avoids forwarding arbitrary provider
// response bodies. Those bodies can echo requests, credentials, or private
// gateway diagnostics and are unsafe for logs, tool output, and settings UI.
func SafeSearchErrorDetail(err error) string {
	if err != nil {
		text := strings.ToLower(err.Error())
		if strings.Contains(text, "signed-in hub") || strings.Contains(text, "hub account") || strings.Contains(text, "sign in to maclaw hub") {
			return "sign in to MaClaw Hub"
		}
		if strings.Contains(text, "maclaw hub search returned http 401") {
			return "MaClaw Hub search is unavailable"
		}
		if strings.Contains(text, "maclaw hub search returned") && (strings.Contains(text, "unavailable") || strings.Contains(text, "offline") || strings.Contains(text, "failed to parse")) {
			return "MaClaw Hub search is unavailable"
		}
	}
	switch classifySearchError(err) {
	case "timeout":
		return "request timed out"
	case "blocked":
		return "request was blocked or challenged"
	case "invalid_key":
		return "credentials were rejected"
	case "rate_limited":
		return "request was rate limited"
	default:
		return "request failed"
	}
}

func safeSearchErrorDetail(err error) string {
	return SafeSearchErrorDetail(err)
}

func mergeSearchResults(existing, incoming []SearchResult, limit int) []SearchResult {
	return mergeSearchResultsPrefer(existing, incoming, limit, "")
}

func mergeSearchResultsPrefer(existing, incoming []SearchResult, limit int, hint string) []SearchResult {
	var covering, rest []SearchResult
	seen := make(map[string]bool, len(existing)+len(incoming))
	add := func(result SearchResult) {
		key := canonicalSearchResultURL(result.URL)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		if hint != "" && resultCoversSiteHint(result, hint) {
			covering = append(covering, result)
			return
		}
		rest = append(rest, result)
	}
	for _, result := range existing {
		add(result)
	}
	for _, result := range incoming {
		add(result)
	}
	out := append(covering, rest...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func canonicalSearchResultURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	u.Host = hostname
	if port != "" {
		u.Host += ":" + port
	}
	u.Fragment = ""
	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "gclid" || lower == "fbclid" || lower == "msclkid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path != "/" {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}
	return u.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
