package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	defaultBraveSearchURL       = "https://api.search.brave.com/res/v1/web/search"
	defaultSerperSearchURL      = "https://google.serper.dev/search"
	defaultTinyFishSearchURL    = "https://api.search.tinyfish.ai"
	defaultTinyFishFetchURL     = "https://api.fetch.tinyfish.ai"
	defaultTavilySearchURL      = "https://api.tavily.com/search"
	defaultMaclawHubSearchURL   = "https://hub.maclaw.top/searchproxy/search"
	defaultMaclawHubDownloadURL = "https://hub.maclaw.top/searchproxy/download"
)

var defaultLegacySearchURL = "https://html.duckduckgo.com/html/"
var defaultBingSearchURL = "https://cn.bing.com/search"
var defaultBaiduSearchURL = "https://www.baidu.com/s"

var (
	searchTimeout               = 25 * time.Second
	providerSearchTimeout       = 6 * time.Second
	fallbackSearchTimeout       = 20 * time.Second
	directEndpointSearchTimeout = 5 * time.Second
)

// lastGoodEndpoint caches the name of the most recently successful direct
// search endpoint so subsequent searches try it first (adaptive ordering).
var (
	lastGoodEndpointMu   sync.Mutex
	lastGoodEndpointName string
)

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type directSearchEndpoint struct {
	Name          string
	FailureDomain string
	Search        func(context.Context, string, int) ([]SearchResult, error)
}

// Search performs the legacy direct web search and returns results.
func Search(query string, maxResults int) ([]SearchResult, error) {
	return SearchCtx(context.Background(), query, maxResults)
}

func SearchCtx(parent context.Context, query string, maxResults int) ([]SearchResult, error) {
	query, maxResults, ctx, cancel, err := prepareSearch(parent, query, maxResults)
	if err != nil {
		return nil, err
	}
	defer cancel()
	results, err := searchDirectFallbackChain(ctx, query, maxResults, "")
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	return results, nil
}

// SearchWithProvider performs a provider-aware search and falls back to direct
// HTML search when the selected provider is unavailable or fails. Search API
// outages should degrade to ordinary web access instead of making live-data
// tasks report that search is unavailable.
func SearchWithProvider(query string, maxResults int, provider corelib.WebSearchProvider) ([]SearchResult, error) {
	return SearchWithProviderCtx(context.Background(), query, maxResults, provider)
}

func SearchWithProviderCtx(parent context.Context, query string, maxResults int, provider corelib.WebSearchProvider) ([]SearchResult, error) {
	query, maxResults, ctx, cancel, err := prepareSearch(parent, query, maxResults)
	if err != nil {
		return nil, err
	}
	defer cancel()

	provider = normalizeProvider(provider)
	results, err := runProviderSearch(ctx, query, maxResults, provider, false)
	if err == nil && len(results) > 0 {
		return results, nil
	}
	return fallbackDirectSearch(ctx, query, maxResults, provider, err, results)
}

// TestProvider performs a strict provider probe without falling back to direct
// search. It is intended for configuration validation where provider-specific
// failures must be surfaced instead of being hidden by the runtime fallback chain.
func TestProvider(parent context.Context, provider corelib.WebSearchProvider) error {
	query, maxResults, ctx, cancel, err := prepareSearch(parent, "MaClaw web search configuration test", 3)
	if err != nil {
		return err
	}
	defer cancel()

	provider = normalizeProvider(provider)
	results, err := runProviderSearch(ctx, query, maxResults, provider, true)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return fmt.Errorf("%s provider returned no results", providerNameForError(provider))
	}
	return nil
}

func runProviderSearch(parent context.Context, query string, maxResults int, provider corelib.WebSearchProvider, strict bool) ([]SearchResult, error) {
	return runProviderSearchWithTimeout(parent, query, maxResults, provider, strict, providerSearchTimeout)
}

func runProviderSearchWithTimeout(parent context.Context, query string, maxResults int, provider corelib.WebSearchProvider, strict bool, timeout time.Duration) ([]SearchResult, error) {
	providerCtx := parent
	providerCancel := func() {}
	if timeout > 0 {
		providerCtx, providerCancel = context.WithTimeout(parent, timeout)
	}
	defer providerCancel()

	switch provider.Type {
	case "brave":
		if provider.Key == "" {
			if strict {
				return nil, fmt.Errorf("Brave API key is not configured")
			}
			return searchDirectFallbackChain(parent, query, maxResults, "")
		}
		return searchBrave(providerCtx, provider, query, maxResults)
	case "serper":
		if provider.Key == "" {
			if strict {
				return nil, fmt.Errorf("Serper API key is not configured")
			}
			return searchDirectFallbackChain(parent, query, maxResults, "")
		}
		return searchSerper(providerCtx, provider, query, maxResults)
	case "tinyfish":
		if provider.Key == "" {
			if strict {
				return nil, fmt.Errorf("TinyFish API key is not configured")
			}
			return searchDirectFallbackChain(parent, query, maxResults, "")
		}
		return searchTinyFish(providerCtx, provider, query, maxResults)
	case "tavily":
		if provider.Key == "" {
			if strict {
				return nil, fmt.Errorf("Tavily API key is not configured")
			}
			return searchDirectFallbackChain(parent, query, maxResults, "")
		}
		return searchTavily(providerCtx, provider, query, maxResults)
	case WebSearchEngineMaclawHub:
		if provider.Key == "" {
			if strict {
				return nil, fmt.Errorf("MaClaw Hub search requires a signed-in hub account")
			}
			return searchDirectFallbackChain(parent, query, maxResults, "")
		}
		return searchMaclawHub(providerCtx, provider, query, maxResults)
	case "duckduckgo":
		return searchDuckDuckGo(providerCtx, provider, query, maxResults)
	default:
		if strict {
			return nil, fmt.Errorf("unsupported search provider %q", provider.Type)
		}
		return searchDirectFallbackChain(parent, query, maxResults, "")
	}
}

func prepareSearch(parent context.Context, query string, maxResults int) (string, int, context.Context, context.CancelFunc, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, nil, nil, fmt.Errorf("query is empty")
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return "", 0, nil, nil, err
	}
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}
	ctx, cancel := context.WithTimeout(parent, searchTimeout)
	return query, maxResults, ctx, cancel, nil
}

func normalizeProvider(provider corelib.WebSearchProvider) corelib.WebSearchProvider {
	provider.Name = strings.TrimSpace(provider.Name)
	provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
	provider.Key = strings.TrimSpace(provider.Key)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	return provider
}

func searchDuckDuckGo(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = "https://lite.duckduckgo.com/lite/"
	}
	// GET through the anti-bot chain: the DDG HTML endpoints accept GET, and
	// anomaly challenges (HTTP 202 + challenge page) then escalate instead of
	// failing flat.
	searchURL, err := appendSearchURLQuery(baseURL, url.Values{"q": {query}})
	if err != nil {
		return nil, err
	}
	html, err := fetchRawHTMLWithChain(ctx, searchURL, nil)
	if err != nil {
		return nil, err
	}
	results := parseDDGResults(html, maxResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("DuckDuckGo returned no parseable results")
	}
	return results, nil
}

func fallbackDirectSearch(ctx context.Context, query string, maxResults int, provider corelib.WebSearchProvider, providerErr error, providerResults []SearchResult) ([]SearchResult, error) {
	fallbackCtx, cancel := context.WithTimeout(ctx, fallbackSearchTimeout)
	defer cancel()

	results, fallbackErr := searchDirectFallbackChain(fallbackCtx, query, maxResults, providerFailureDomain(provider))
	if fallbackErr == nil && len(results) > 0 {
		return results, nil
	}
	if providerErr != nil {
		if fallbackErr != nil {
			return nil, fmt.Errorf("%s provider failed: %w; direct fallback failed: %v", providerNameForError(provider), providerErr, fallbackErr)
		}
		return nil, fmt.Errorf("%s provider failed: %w; direct fallback returned no results", providerNameForError(provider), providerErr)
	}
	if len(providerResults) == 0 {
		if fallbackErr != nil {
			return nil, fmt.Errorf("%s provider returned no results; direct fallback failed: %w", providerNameForError(provider), fallbackErr)
		}
		return nil, fmt.Errorf("%s provider and direct fallback returned no results", providerNameForError(provider))
	}
	return providerResults, nil
}

// BrowserSearchHit mirrors browser.SearchHit without an import dependency
// (websearch must not import corelib/browser; the GUI wires the hook).
type BrowserSearchHit struct {
	Title   string
	URL     string
	Snippet string
}

// BrowserSearchProvider searches the web with the managed real browser
// (last-resort path when every HTTP-level endpoint fails).
type BrowserSearchProvider func(ctx context.Context, engineID, query string, maxResults int, humanAssist bool) ([]BrowserSearchHit, error)

var (
	browserSearchProviderMu sync.RWMutex
	browserSearchProvider   BrowserSearchProvider
)

// SetBrowserSearchProvider installs the browser-based search fallback. Safe
// to call multiple times; nil disables the fallback.
func SetBrowserSearchProvider(p BrowserSearchProvider) {
	browserSearchProviderMu.Lock()
	browserSearchProvider = p
	browserSearchProviderMu.Unlock()
}

func getBrowserSearchProvider() BrowserSearchProvider {
	browserSearchProviderMu.RLock()
	defer browserSearchProviderMu.RUnlock()
	return browserSearchProvider
}

func searchDirectFallbackChain(ctx context.Context, query string, maxResults int, skipFailureDomain string) ([]SearchResult, error) {
	endpoints := orderedDirectSearchEndpoints()
	var failures []string
	for _, endpoint := range endpoints {
		if skipFailureDomain != "" && endpoint.FailureDomain == skipFailureDomain {
			continue
		}
		endpointCtx, cancel := directEndpointContext(ctx)
		results, err := endpoint.Search(endpointCtx, query, maxResults)
		cancel()
		if err == nil && len(results) > 0 {
			recordGoodEndpoint(endpoint.Name)
			return results, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s failed: %v", endpoint.Name, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s returned no results", endpoint.Name))
		}
		if ctx.Err() != nil {
			break
		}
	}
	// Ultimate fallback: the managed real browser (cookies, TLS fingerprint,
	// JS execution) searches Bing/Google directly. Registered by the GUI.
	if hook := getBrowserSearchProvider(); hook != nil && ctx.Err() == nil {
		dlogf("[search] all direct endpoints failed; trying browser search fallback")
		hits, herr := hook(ctx, "bing_cn", query, maxResults, false)
		if herr == nil && len(hits) > 0 {
			results := make([]SearchResult, 0, len(hits))
			for _, h := range hits {
				results = append(results, SearchResult{Title: h.Title, URL: h.URL, Snippet: h.Snippet})
			}
			return results, nil
		}
		if herr != nil {
			failures = append(failures, fmt.Sprintf("browser-search failed: %v", herr))
		} else {
			failures = append(failures, "browser-search returned no results")
		}
	}
	if len(failures) == 0 {
		return nil, fmt.Errorf("no direct fallback endpoints available")
	}
	return nil, errors.New(strings.Join(failures, "; "))
}

func directEndpointContext(parent context.Context) (context.Context, context.CancelFunc) {
	if directEndpointSearchTimeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, directEndpointSearchTimeout)
}

func directSearchEndpoints() []directSearchEndpoint {
	return []directSearchEndpoint{
		{Name: "bing-cn", FailureDomain: "bing", Search: searchBingDirect},
		{Name: "baidu", FailureDomain: "baidu", Search: searchBaiduDirect},
		{Name: "duckduckgo-html", FailureDomain: "duckduckgo", Search: searchDirectLegacy},
	}
}

// orderedDirectSearchEndpoints returns the endpoint list with the last known
// good endpoint moved to the front for faster success on repeat queries.
func orderedDirectSearchEndpoints() []directSearchEndpoint {
	all := directSearchEndpoints()
	lastGoodEndpointMu.Lock()
	good := lastGoodEndpointName
	lastGoodEndpointMu.Unlock()
	if good == "" {
		return all
	}
	// Move the last-good endpoint to front.
	ordered := make([]directSearchEndpoint, 0, len(all))
	var rest []directSearchEndpoint
	for _, ep := range all {
		if ep.Name == good {
			ordered = append(ordered, ep)
		} else {
			rest = append(rest, ep)
		}
	}
	ordered = append(ordered, rest...)
	return ordered
}

func recordGoodEndpoint(name string) {
	lastGoodEndpointMu.Lock()
	lastGoodEndpointName = name
	lastGoodEndpointMu.Unlock()
}

func providerFailureDomain(provider corelib.WebSearchProvider) string {
	switch provider.Type {
	case "duckduckgo":
		return "duckduckgo"
	case "brave":
		return "brave"
	case "serper":
		return "serper"
	case "tinyfish":
		return "tinyfish"
	case "tavily":
		return "tavily"
	case WebSearchEngineMaclawHub:
		return WebSearchEngineMaclawHub
	default:
		return ""
	}
}

func providerNameForError(provider corelib.WebSearchProvider) string {
	if provider.Type != "" {
		return provider.Type
	}
	if provider.Name != "" {
		return provider.Name
	}
	return "search"
}

func searchBrave(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = defaultBraveSearchURL
	}
	searchURL, err := appendSearchURLQuery(baseURL, url.Values{
		"q":     {query},
		"count": {fmt.Sprintf("%d", maxResults)},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("X-Subscription-Token", provider.Key)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("Brave returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(payload.Web.Results))
	for _, item := range payload.Web.Results {
		if item.URL == "" || item.Title == "" {
			continue
		}
		results = append(results, SearchResult{Title: item.Title, URL: item.URL, Snippet: item.Description})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func searchSerper(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = defaultSerperSearchURL
	}
	body, err := json.Marshal(map[string]interface{}{"q": query, "num": maxResults})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("X-API-KEY", provider.Key)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("Serper returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var payload struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(payload.Organic))
	for _, item := range payload.Organic {
		if item.Link == "" || item.Title == "" {
			continue
		}
		results = append(results, SearchResult{Title: item.Title, URL: item.Link, Snippet: item.Snippet})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func searchTinyFish(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = defaultTinyFishSearchURL
	}
	searchURL, err := appendSearchURLQuery(baseURL, url.Values{"query": {query}})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("X-API-Key", provider.Key)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("TinyFish returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// TinyFish search API may return either:
	//   {"results": [{title, url, content, snippet}]}
	// or a bare array:
	//   [{title, url, content, snippet}]
	type tfSearchItem struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
		Snippet string `json:"snippet"`
	}

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	rawBody = bytes.TrimSpace(rawBody)

	var items []tfSearchItem
	if len(rawBody) > 0 && rawBody[0] == '[' {
		if err := json.Unmarshal(rawBody, &items); err != nil {
			return nil, fmt.Errorf("TinyFish: failed to parse array response: %w", err)
		}
	} else {
		var wrapped struct {
			Results []tfSearchItem `json:"results"`
		}
		if err := json.Unmarshal(rawBody, &wrapped); err != nil {
			return nil, fmt.Errorf("TinyFish: failed to parse response: %w", err)
		}
		items = wrapped.Results
	}

	results := make([]SearchResult, 0, len(items))
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		title := item.Title
		if title == "" {
			title = item.URL
		}
		snippet := item.Snippet
		if snippet == "" {
			snippet = item.Content
		}
		if len([]rune(snippet)) > 300 {
			snippet = string([]rune(snippet)[:300]) + "…"
		}
		results = append(results, SearchResult{Title: title, URL: item.URL, Snippet: snippet})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func searchTavily(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	baseURL := provider.BaseURL
	if baseURL == "" {
		baseURL = defaultTavilySearchURL
	}
	payload, err := json.Marshal(map[string]interface{}{
		"query":       query,
		"max_results": maxResults,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+provider.Key)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("Tavily returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tavilyResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&tavilyResp); err != nil {
		return nil, fmt.Errorf("Tavily: failed to parse response: %w", err)
	}

	results := make([]SearchResult, 0, len(tavilyResp.Results))
	for _, item := range tavilyResp.Results {
		if item.URL == "" {
			continue
		}
		title := item.Title
		if title == "" {
			title = item.URL
		}
		snippet := item.Content
		if len([]rune(snippet)) > 300 {
			snippet = string([]rune(snippet)[:300]) + "…"
		}
		results = append(results, SearchResult{Title: title, URL: item.URL, Snippet: snippet})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func searchMaclawHub(ctx context.Context, provider corelib.WebSearchProvider, query string, maxResults int) ([]SearchResult, error) {
	token := strings.TrimSpace(provider.Key)
	if token == "" {
		return nil, fmt.Errorf("MaClaw Hub search requires a signed-in hub account")
	}
	searchURL, err := maclawHubSearchURL(provider.BaseURL)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]interface{}{
		"query":    query,
		"engine":   "auto",
		"limit":    maxResults,
		"content":  true,
		"region":   "cn",
		"fallback": true,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", pickUserAgent())

	resp, err := maclawHubHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, maclawHubSearchStatusError(resp.StatusCode, raw)
	}

	var payloadResp struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2*1024*1024)).Decode(&payloadResp); err != nil {
		return nil, fmt.Errorf("MaClaw Hub search: failed to parse response")
	}

	results := make([]SearchResult, 0, len(payloadResp.Results))
	for _, item := range payloadResp.Results {
		href := normalizeSearchResultURL(item.URL)
		if href == "" {
			continue
		}
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = href
		}
		snippet := strings.TrimSpace(item.Snippet)
		if snippet == "" {
			snippet = strings.TrimSpace(item.Content)
		}
		if len([]rune(snippet)) > 300 {
			snippet = string([]rune(snippet)[:300]) + "…"
		}
		results = append(results, SearchResult{Title: title, URL: href, Snippet: snippet})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func maclawHubSearchStatusError(status int, body []byte) error {
	code, detail := parseMaclawHubSearchError(body)
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "captcha":
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: search was blocked or challenged", status)
	case "timeout":
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: search timed out", status)
	case "parse":
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: search failed to parse", status)
	case "unauthorized":
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: sign in to MaClaw Hub", status)
	case "offline":
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: search backend offline", status)
	}
	if status == http.StatusUnauthorized {
		return fmt.Errorf("MaClaw Hub search returned HTTP 401 (sign in to MaClaw Hub)")
	}
	if detail != "" {
		return fmt.Errorf("MaClaw Hub search returned HTTP %d: %s", status, detail)
	}
	return fmt.Errorf("MaClaw Hub search returned HTTP %d", status)
}

func parseMaclawHubSearchError(body []byte) (code, detail string) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "", ""
	}
	var payload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	code = strings.TrimSpace(payload.Code)
	detail = strings.TrimSpace(payload.Error)
	if hubProxyDetailLooksSecret(detail) {
		detail = ""
	}
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "api key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "token") {
		detail = ""
	}
	if len([]rune(detail)) > 200 {
		detail = string([]rune(detail)[:200]) + "…"
	}
	return code, detail
}

func maclawHubSearchURL(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return defaultMaclawHubSearchURL, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid search base URL")
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "/searchproxy" || strings.HasSuffix(path, "/searchproxy") {
		u.Path = path + "/search"
	}
	return u.String(), nil
}

func maclawHubHTTPClient() *http.Client {
	return &http.Client{
		Timeout:       strategyMaclawHubTimeout,
		Transport:     httpClient().Transport,
		CheckRedirect: sharedCheckRedirect,
	}
}

// FetchWithProvider performs a provider-aware fetch. When the provider has
// enhanced fetch capabilities (e.g. TinyFish), it uses the provider's API
// for better content extraction, falling back to standard Fetch on failure.
// Pass a zero-value provider to use standard fetch directly.
func FetchWithProvider(rawURL string, opts *FetchOptions, provider corelib.WebSearchProvider) (*FetchResult, error) {
	return FetchWithProviderCtx(context.Background(), rawURL, opts, provider)
}

func FetchWithProviderCtx(parent context.Context, rawURL string, opts *FetchOptions, provider corelib.WebSearchProvider) (*FetchResult, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if opts != nil && opts.PublicNetworkOnly {
		// Enhanced providers are configured with desktop credentials and may
		// fetch the requested URL from their own infrastructure. Public group
		// fetches must use only the local guarded HTTP transport.
		provider = corelib.WebSearchProvider{}
	}
	provider = normalizeProvider(provider)
	if opts == nil {
		opts = &FetchOptions{}
	}

	if provider.Type == WebSearchEngineMaclawHub {
		if err := applyProviderHubDownload(opts, provider); err != nil {
			return nil, err
		}
	}

	// TinyFish has its own fetch API with better content extraction.
	// When the Hub RapidSearch download channel is active, keep egress on
	// that proxy instead of a third-party fetch API.
	if opts.HubDownload == nil && provider.Type == "tinyfish" && provider.Key != "" && opts.SavePath == "" {
		fetchURL := deriveTinyFishFetchURL(provider.BaseURL)
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		result, err := FetchWithTinyFish(ctx, rawURL, provider.Key, fetchURL)
		if err == nil && result != nil && result.Content != "" {
			// Apply offset + maxChars windowing on full content
			applyFetchWindowing(result, opts.Offset, opts.MaxChars)
			return result, nil
		}
		// TinyFish failed — fall through to standard fetch
	}

	return FetchCtx(parent, rawURL, opts)
}

// deriveTinyFishFetchURL derives the fetch endpoint from the search base URL.
func deriveTinyFishFetchURL(searchBaseURL string) string {
	if searchBaseURL == "" || searchBaseURL == defaultTinyFishSearchURL {
		return defaultTinyFishFetchURL
	}
	u, err := url.Parse(searchBaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return defaultTinyFishFetchURL
	}
	// Custom endpoints commonly identify the operation either in the path
	// (/search) or hostname (api.search.example). Never replace query values:
	// they may contain proxy credentials or routing data that happen to include
	// the word "search".
	if fetchedPath := strings.Replace(u.Path, "search", "fetch", 1); fetchedPath != u.Path {
		u.Path = fetchedPath
		return u.String()
	}
	if fetchedHost := strings.Replace(u.Host, "search", "fetch", 1); fetchedHost != u.Host {
		u.Host = fetchedHost
		return u.String()
	}
	return defaultTinyFishFetchURL
}

// applyFetchWindowing applies offset and maxChars windowing to a FetchResult.
func applyFetchWindowing(result *FetchResult, offset int, maxChars int) {
	totalChars := len([]rune(result.Content))
	result.TotalChars = totalChars

	// Fast path: no windowing needed
	if offset <= 0 && maxChars <= 0 {
		result.Truncated = false
		result.HasMore = false
		result.NextOffset = totalChars
		return
	}

	runes := []rune(result.Content)

	// Apply offset
	if offset > 0 {
		if offset >= totalChars {
			result.Content = ""
			result.Truncated = false
			result.HasMore = false
			result.NextOffset = totalChars
			return
		}
		runes = runes[offset:]
	}

	// Apply maxChars
	if maxChars > 0 && len(runes) > maxChars {
		runes = runes[:maxChars]
		result.Content = string(runes)
		result.Truncated = true
		result.HasMore = true
		result.NextOffset = offset + maxChars
	} else if offset > 0 {
		// Only convert back if we actually sliced
		result.Content = string(runes)
		result.Truncated = false
		result.HasMore = false
		result.NextOffset = totalChars
	} else {
		result.Truncated = false
		result.HasMore = false
		result.NextOffset = totalChars
	}
}

// FetchWithTinyFish uses TinyFish's fetch API to extract content from URLs.
// fetchURL allows the caller to override the endpoint (e.g. via proxy).
// Pass "" to use the default endpoint.
// Returns the extracted content or an error. The caller should fall back to
// the standard Fetch() if this fails.
func FetchWithTinyFish(ctx context.Context, rawURL string, apiKey string, fetchURL string) (*FetchResult, error) {
	if fetchURL == "" {
		fetchURL = defaultTinyFishFetchURL
	}
	body, err := json.Marshal(map[string]interface{}{
		"urls": []string{rawURL},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fetchURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", apiKey)

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("TinyFish fetch returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// TinyFish fetch API may return either:
	//   {"results": [{url, title, content, error}]}
	// or a bare array:
	//   [{url, title, content, error}]
	type tfItem struct {
		URL     string `json:"url"`
		Title   string `json:"title"`
		Content string `json:"content"`
		Error   string `json:"error"`
	}

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, err
	}
	rawBody = bytes.TrimSpace(rawBody)

	var items []tfItem
	if len(rawBody) > 0 && rawBody[0] == '[' {
		// Bare array format
		if err := json.Unmarshal(rawBody, &items); err != nil {
			return nil, fmt.Errorf("TinyFish fetch: failed to parse array response: %w", err)
		}
	} else {
		// Wrapped format {"results": [...]}
		var wrapped struct {
			Results []tfItem `json:"results"`
		}
		if err := json.Unmarshal(rawBody, &wrapped); err != nil {
			return nil, fmt.Errorf("TinyFish fetch: failed to parse response: %w", err)
		}
		items = wrapped.Results
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("TinyFish fetch returned no results")
	}
	item := items[0]
	if item.Error != "" {
		return nil, fmt.Errorf("TinyFish fetch error: %s", item.Error)
	}
	if item.Content == "" {
		return nil, fmt.Errorf("TinyFish fetch returned empty content")
	}

	content := item.Content
	totalChars := len([]rune(content))
	return &FetchResult{
		URL:         rawURL,
		Title:       item.Title,
		ContentType: "text/html",
		Content:     content,
		BytesRead:   len(content),
		TotalChars:  totalChars,
		Truncated:   false,
		HasMore:     false,
		NextOffset:  totalChars,
	}, nil
}

// fetchRawHTMLWithChain fetches a URL through the anti-bot escalation chain
// and returns the raw HTML (no readable-text extraction). Used by the direct
// HTML search endpoints so a blocked search page (Cloudflare/rate limit)
// escalates the same way downloads do.
//
// maxAttemptsPerLevel=1: the search fallback chain walks several endpoints
// under a tight shared time budget, so a failing endpoint must fail fast and
// yield to the next one instead of burning the budget on same-URL retries.
func fetchRawHTMLWithChain(ctx context.Context, rawURL string, headers map[string]string) (string, error) {
	return fetchRawHTMLWithChainOptions(ctx, rawURL, headers, isPublicNetworkOnly(ctx))
}

func fetchRawHTMLWithChainOptions(ctx context.Context, rawURL string, headers map[string]string, publicNetworkOnly bool) (string, error) {
	opts := &FetchOptions{TimeoutS: 30, MaxBytes: 2 * 1024 * 1024, Headers: headers, PublicNetworkOnly: publicNetworkOnly}
	client := httpClient()
	if publicNetworkOnly {
		publicURL, err := validatePublicHTTPURL(rawURL)
		if err != nil {
			return "", err
		}
		rawURL = publicURL.String()
		client = PublicHTTPClient(30 * time.Second)
	}
	result, err := runFetchChain(ctx, rawURL, "[search]", 1, opts, client, func(c *http.Client, extra map[string]string) *fetchAttempt {
		return performTextFetch(ctx, rawURL, opts, c, extra, true)
	})
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

// searchDirectLegacy scrapes DuckDuckGo HTML lite for search results.
func searchDirectLegacy(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	searchURL := defaultLegacySearchURL + "?q=" + url.QueryEscape(query)
	html, err := fetchRawHTMLWithChain(ctx, searchURL, nil)
	if err != nil {
		return nil, err
	}
	return parseDDGResults(html, maxResults), nil
}

// parseDDGResults extracts search results from DuckDuckGo's HTML and Lite
// responses. The two endpoints use different result-link class names.
func parseDDGResults(html string, maxResults int) []SearchResult {
	var results []SearchResult

	// The HTML endpoint uses result__a / result__snippet, while Lite uses
	// result-link / result-snippet. Match the anchor opening tag rather than a
	// specific attribute order so a harmless upstream markup change does not
	// turn a successful HTTP response into an empty search result.
	remaining := html
	for len(results) < maxResults {
		idx, _ := findDDGElement(remaining, "a", "result__a", "result-link")
		if idx < 0 {
			break
		}
		remaining = remaining[idx:]

		// Extract href from the matched opening tag only. Looking through the
		// remainder can accidentally associate a child link with this result.
		openEnd := strings.IndexByte(remaining, '>')
		if openEnd < 0 {
			break
		}
		href := extractAttr(remaining[:openEnd+1], "href")
		if href == "" {
			remaining = advanceDDGElement(remaining)
			continue
		}
		// DuckDuckGo wraps URLs in redirect: //duckduckgo.com/l/?uddg=...
		href = resolveDDGURL(href)
		href = normalizeSearchResultURL(href)
		if isDDGAdURL(href) {
			// Ad entries share the regular result-link classes but redirect through DDG's
			// ad tracker; they are not organic results.
			remaining = advanceDDGElement(remaining)
			continue
		}

		// Extract title (text between > and </a>)
		title := extractTagText(remaining, "a")

		// Find snippet
		snippet := ""
		snippetSearch := remaining
		if nextResult, _ := findDDGElement(remaining[1:], "a", "result__a", "result-link"); nextResult >= 0 {
			snippetSearch = remaining[:nextResult+1]
		}
		if snippetIdx, tag := findDDGElement(snippetSearch, "", "result__snippet", "result-snippet"); snippetIdx >= 0 {
			snippet = extractTagText(snippetSearch[snippetIdx:], tag)
		}

		if href != "" && title != "" {
			results = append(results, SearchResult{
				Title:   cleanHTML(title),
				URL:     href,
				Snippet: cleanHTML(snippet),
			})
		}

		if next := advanceDDGElement(remaining); next != "" {
			remaining = next
		} else {
			break
		}
	}

	return results
}

func advanceDDGElement(html string) string {
	if openEnd := strings.IndexByte(html, '>'); openEnd >= 0 && openEnd+1 < len(html) {
		return html[openEnd+1:]
	}
	return ""
}

// findDDGElement returns the next opening element matching one of classes.
// Passing an empty tag matches any element type, which is useful because DDG
// has used both anchors and table cells for snippets.
func findDDGElement(html, tag string, classes ...string) (int, string) {
	lower := strings.ToLower(html)
	for offset := 0; offset < len(lower); {
		idx := strings.IndexByte(lower[offset:], '<')
		if idx < 0 {
			break
		}
		idx += offset
		openEnd := strings.IndexByte(lower[idx:], '>')
		if openEnd < 0 {
			break
		}
		openEnd += idx
		opening := strings.TrimSpace(lower[idx+1 : openEnd])
		nameEnd := strings.IndexAny(opening, " \t\r\n/")
		if nameEnd < 0 {
			nameEnd = len(opening)
		}
		name := opening[:nameEnd]
		if name != "" && (tag == "" || name == tag) && hasDDGClass(extractAttr(html[idx:openEnd+1], "class"), classes...) {
			return idx, name
		}
		offset = openEnd + 1
	}
	return -1, ""
}

func hasDDGClass(class string, candidates ...string) bool {
	for _, className := range strings.Fields(class) {
		for _, candidate := range candidates {
			if strings.EqualFold(className, candidate) {
				return true
			}
		}
	}
	return false
}

func isSearchResultAnchor(contextBefore, anchorOpen string) bool {
	class := strings.ToLower(extractAttr(anchorOpen, "class"))
	if strings.Contains(class, "result") || strings.Contains(class, "title") || class == "ob" {
		return true
	}
	if len(contextBefore) > 240 {
		contextBefore = contextBefore[len(contextBefore)-240:]
	}
	contextBefore = strings.ToLower(contextBefore)
	resultMarkers := []string{
		`class="result`,
		`class='result`,
		`class="r1`,
		`class='r1`,
		`class="ob`,
		`class='ob`,
		`id="results`,
		`id='results`,
	}
	for _, marker := range resultMarkers {
		if strings.Contains(contextBefore, marker) {
			return true
		}
	}
	return false
}

// resolveDDGURL extracts the actual URL from DuckDuckGo's redirect wrapper.
// isDDGAdURL reports whether a DDG result URL is an ad-tracker redirect
// rather than an organic result.
func isDDGAdURL(href string) bool {
	return strings.Contains(href, "://duckduckgo.com/y.js") ||
		strings.Contains(href, "://duckduckgo.com/aclick")
}

func resolveDDGURL(href string) string {
	if strings.Contains(href, "uddg=") {
		if u, err := url.Parse(href); err == nil {
			if actual := u.Query().Get("uddg"); actual != "" {
				return actual
			}
		}
	}
	// Strip leading //
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	return href
}

func normalizeSearchResultURL(href string) string {
	href = strings.TrimSpace(cleanHTML(href))
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil || !u.IsAbs() {
		return ""
	}
	switch u.Scheme {
	case "http", "https":
		return u.String()
	default:
		return ""
	}
}

func looksLikeSearchChromeURL(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return true
	}
	if u.Host == "" {
		return true
	}
	host := strings.ToLower(u.Hostname())
	if !isSearchProviderHost(host) {
		return false
	}
	path := strings.ToLower(u.Path)
	switch {
	case strings.Contains(path, "/search"):
		return true
	case strings.Contains(path, "/preferences"):
		return true
	default:
		return false
	}
}

func isSearchProviderHost(host string) bool {
	switch {
	case host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com"):
		return true
	case host == "bing.com" || strings.HasSuffix(host, ".bing.com"):
		return true
	case host == "baidu.com" || strings.HasSuffix(host, ".baidu.com"):
		return true
	default:
		return false
	}
}

// extractAttr extracts the value of the given attribute from the current position.
func extractAttr(s, attr string) string {
	key := attr + `=`
	lower := strings.ToLower(s)
	idx := indexHTMLAttr(lower, strings.ToLower(key))
	if idx < 0 || idx > 200 {
		return ""
	}
	start := idx + len(key)
	if start >= len(s) {
		return ""
	}
	quote := s[start]
	if quote == '"' || quote == '\'' {
		start++
		end := strings.IndexByte(s[start:], quote)
		if end < 0 {
			return ""
		}
		return s[start : start+end]
	}
	end := strings.IndexAny(s[start:], " >\t\r\n")
	if end < 0 {
		return s[start:]
	}
	return s[start : start+end]
}

func indexHTMLAttr(s, key string) int {
	searchFrom := 0
	for {
		idx := strings.Index(s[searchFrom:], key)
		if idx < 0 {
			return -1
		}
		idx += searchFrom
		if idx == 0 || isHTMLAttrBoundary(s[idx-1]) {
			return idx
		}
		searchFrom = idx + len(key)
	}
}

func isHTMLAttrBoundary(ch byte) bool {
	switch ch {
	case ' ', '\t', '\r', '\n', '<', '/', '\'':
		return true
	default:
		return ch == '"'
	}
}

// extractTagText extracts text content from the first occurrence of <tag...>text</tag>.
func extractTagText(s, tag string) string {
	// Find the closing > of the opening tag
	gt := strings.Index(s, ">")
	if gt < 0 {
		return ""
	}
	start := gt + 1
	endTag := "</" + tag + ">"
	tail := s[start:]
	end := strings.Index(strings.ToLower(tail), strings.ToLower(endTag))
	if end < 0 {
		return ""
	}
	return tail[:end]
}

// cleanHTML strips HTML tags and decodes common entities.
func cleanHTML(s string) string {
	// Strip tags
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	result := out.String()
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", `"`)
	result = strings.ReplaceAll(result, "&#x27;", "'")
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.TrimSpace(result)
	return result
}

// ---------------------------------------------------------------------------
// Bing China direct HTML search (no API key required)
// ---------------------------------------------------------------------------

func searchBingDirect(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return searchBingDirectWithBaseURL(ctx, query, maxResults, defaultBingSearchURL)
}

func searchBingDirectWithBaseURL(ctx context.Context, query string, maxResults int, baseURL string) ([]SearchResult, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBingSearchURL
	}
	searchURL, err := appendSearchURLQuery(baseURL, url.Values{"q": {query}, "count": {fmt.Sprintf("%d", maxResults)}})
	if err != nil {
		return nil, err
	}
	html, err := fetchRawHTMLWithChain(ctx, searchURL, nil)
	if err != nil {
		return nil, err
	}
	return parseBingResults(html, maxResults), nil
}

// parseBingResults extracts search results from Bing's HTML response.
// Bing uses <li class="b_algo"> for each result, with <h2><a href="...">title</a></h2>
// and <p class="b_lineclamp..."> or <div class="b_caption"><p> for snippets.
func parseBingResults(html string, maxResults int) []SearchResult {
	var results []SearchResult
	remaining := html

	for len(results) < maxResults {
		// Find the next result block: <li class="b_algo">
		idx := strings.Index(remaining, `class="b_algo"`)
		if idx < 0 {
			break
		}
		remaining = remaining[idx:]

		// Find the next result block boundary for scoping
		nextBlock := strings.Index(remaining[14:], `class="b_algo"`)
		var block string
		if nextBlock > 0 {
			block = remaining[:nextBlock+14]
		} else {
			block = remaining
		}

		// Extract URL and title from <h2><a href="...">title</a></h2>
		href := ""
		title := ""
		h2Idx := strings.Index(block, "<h2")
		if h2Idx >= 0 {
			aIdx := strings.Index(block[h2Idx:], "<a ")
			if aIdx >= 0 {
				anchorStart := block[h2Idx+aIdx:]
				href = normalizeSearchResultURL(extractAttr(anchorStart, "href"))
				title = cleanHTML(extractTagText(anchorStart, "a"))
			}
		}

		// Extract snippet: look for <p> after the <h2> block
		snippet := ""
		h2End := strings.Index(block, "</h2>")
		if h2End < 0 {
			h2End = 0
		}
		afterH2 := block[h2End:]
		captionIdx := strings.Index(afterH2, `class="b_caption"`)
		if captionIdx >= 0 {
			pIdx := strings.Index(afterH2[captionIdx:], "<p")
			if pIdx >= 0 {
				snippet = cleanHTML(extractTagText(afterH2[captionIdx+pIdx:], "p"))
			}
		}
		if snippet == "" {
			// Fallback: first <p> after </h2>
			pIdx := strings.Index(afterH2, "<p")
			if pIdx >= 0 {
				snippet = cleanHTML(extractTagText(afterH2[pIdx:], "p"))
			}
		}

		if href != "" && title != "" {
			results = append(results, SearchResult{
				Title:   title,
				URL:     href,
				Snippet: snippet,
			})
		}

		// Advance past this block
		if len(remaining) > 14 {
			remaining = remaining[14:]
		} else {
			break
		}
	}

	return results
}

// ---------------------------------------------------------------------------
// Baidu direct HTML search (no API key required)
// ---------------------------------------------------------------------------

func searchBaiduDirect(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	return searchBaiduDirectWithBaseURL(ctx, query, maxResults, defaultBaiduSearchURL)
}

func searchBaiduDirectWithBaseURL(ctx context.Context, query string, maxResults int, baseURL string) ([]SearchResult, error) {
	// Baidu requires a valid BAIDUID cookie to avoid redirect to verification page.
	// Fetch one dynamically by hitting the homepage first.
	cookie := acquireBaiduCookie(ctx)

	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaiduSearchURL
	}
	searchURL, err := appendSearchURLQuery(baseURL, url.Values{"wd": {query}, "rn": {fmt.Sprintf("%d", maxResults)}})
	if err != nil {
		return nil, err
	}
	html, err := fetchRawHTMLWithChain(ctx, searchURL, map[string]string{"Cookie": cookie})
	if err != nil {
		return nil, err
	}

	results := parseBaiduResults(html, maxResults)
	if len(results) == 0 {
		return nil, fmt.Errorf("Baidu returned no parseable results (possibly captcha page)")
	}
	return results, nil
}

func appendSearchURLQuery(baseURL string, values url.Values) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid search base URL")
	}
	query := u.Query()
	for key, incoming := range values {
		query.Del(key)
		for _, value := range incoming {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// baiduCookie caches a dynamically acquired BAIDUID cookie to avoid
// hitting baidu.com homepage on every search request.
var (
	baiduCookieMu    sync.Mutex
	baiduCookieValue string
	baiduCookieTime  time.Time
)

const baiduCookieTTL = 30 * time.Minute

// acquireBaiduCookie returns a cached or freshly obtained BAIDUID cookie string.
// If the homepage request fails, returns a static fallback cookie.
func acquireBaiduCookie(ctx context.Context) string {
	const fallbackCookie = "BAIDUID=0000000000000000:FG=1"

	baiduCookieMu.Lock()
	if baiduCookieValue != "" && time.Since(baiduCookieTime) < baiduCookieTTL {
		cached := baiduCookieValue
		baiduCookieMu.Unlock()
		return cached
	}
	// Hold lock during fetch to prevent concurrent duplicate requests.
	// The fetch has its own 3s timeout so it won't block long.
	defer baiduCookieMu.Unlock()

	// If the parent context has very little time left, use fallback immediately.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 1*time.Second {
		return fallbackCookie
	}

	// Fetch homepage to get Set-Cookie headers
	cookieCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(cookieCtx, http.MethodGet, "https://www.baidu.com/", nil)
	if err != nil {
		return fallbackCookie
	}
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := httpClient().Do(req)
	if err != nil {
		return fallbackCookie
	}
	defer resp.Body.Close()
	// Drain body to allow connection reuse
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	// Extract all Set-Cookie values and build a cookie header
	var cookies []string
	for _, setCookie := range resp.Header["Set-Cookie"] {
		// Take just the name=value part (before the first ';')
		parts := strings.SplitN(setCookie, ";", 2)
		if len(parts) > 0 && strings.Contains(parts[0], "=") {
			cookies = append(cookies, strings.TrimSpace(parts[0]))
		}
	}

	cookie := strings.Join(cookies, "; ")
	if cookie == "" {
		cookie = fallbackCookie
	}

	baiduCookieValue = cookie
	baiduCookieTime = time.Now()
	return cookie
}

// parseBaiduResults extracts search results from Baidu's HTML response.
// Baidu uses <div class="result ..."> or <div class="c-container"> for each result,
// with <h3 class="t"><a href="...">title</a></h3> and various snippet containers.
// parseBaiduResults extracts organic results from Baidu's current SERP markup.
// Organic blocks are <div class="result c-container ..." mu="REAL_URL">; the
// mu attribute carries the direct target URL (better than the /link redirect),
// the title is the first <h3> inside the block, and the snippet text lives in
// the <!--s-data:{"summaryData":...}--> JSON comment as "text":"...".
func parseBaiduResults(html string, maxResults int) []SearchResult {
	var results []SearchResult
	remaining := html

	for len(results) < maxResults {
		idx := strings.Index(remaining, `class="result c-container`)
		if op := strings.Index(remaining, `class="result-op c-container`); op >= 0 && (idx < 0 || op < idx) {
			idx = op
		}
		if idx < 0 {
			break
		}
		remaining = remaining[idx:]

		// Scope the block to the next result container (or a 30KB window).
		block := remaining
		next := strings.Index(remaining[10:], `class="result`)
		if next > 0 {
			block = remaining[:next+10]
		} else if len(block) > 30*1024 {
			block = block[:30*1024]
		}

		href := extractBaiduMuURL(block)
		if href == "" {
			// Fallback: the title anchor's /link redirect (still functional).
			if aIdx := strings.Index(block, "<a "); aIdx >= 0 {
				href = normalizeSearchResultURL(extractAttr(block[aIdx:], "href"))
			}
		}

		title := ""
		if h3Idx := strings.Index(block, "<h3"); h3Idx >= 0 {
			title = cleanHTML(extractTagText(block[h3Idx:], "h3"))
		}

		snippet := extractBaiduSnippet(block)

		if href != "" && title != "" {
			results = append(results, SearchResult{
				Title:   title,
				URL:     href,
				Snippet: snippet,
			})
		}

		if len(remaining) > 10 {
			remaining = remaining[10:]
		} else {
			break
		}
	}

	return results
}

// extractBaiduMuURL pulls the direct target URL from the container's mu
// attribute. extractAttr's 200-char window is too small (the attribute sits
// behind srcid/id/tpl), so this uses a wider window.
func extractBaiduMuURL(block string) string {
	const marker = `mu="http`
	idx := strings.Index(block, marker)
	if idx < 0 || idx > 2000 {
		return ""
	}
	start := idx + len(`mu="`)
	end := strings.IndexByte(block[start:], '"')
	if end < 0 {
		return ""
	}
	return block[start : start+end]
}

// extractBaiduSnippet pulls the first "text":"..." payload from the
// summaryData JSON comment, unescaping the minimal JSON escapes.
func extractBaiduSnippet(block string) string {
	const marker = `"text":"`
	idx := strings.Index(block, marker)
	if idx < 0 {
		return ""
	}
	rest := block[idx+len(marker):]
	var sb strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\\' && i+1 < len(rest) {
			switch rest[i+1] {
			case '"', '\\', '/':
				sb.WriteByte(rest[i+1])
				i++
				continue
			case 'n', 't', 'r':
				sb.WriteByte(' ')
				i++
				continue
			}
		}
		if c == '"' {
			break
		}
		sb.WriteByte(c)
	}
	return strings.TrimSpace(sb.String())
}
