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
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	defaultBraveSearchURL    = "https://api.search.brave.com/res/v1/web/search"
	defaultSerperSearchURL   = "https://google.serper.dev/search"
	defaultTinyFishSearchURL = "https://api.search.tinyfish.ai"
	defaultTinyFishFetchURL  = "https://api.fetch.tinyfish.ai"
)

var defaultLegacySearchURL = "https://html.duckduckgo.com/html/"
var defaultMojeekSearchURL = "https://www.mojeek.com/search"

var (
	searchTimeout               = 15 * time.Second
	providerSearchTimeout       = 6 * time.Second
	fallbackSearchTimeout       = 8 * time.Second
	directEndpointSearchTimeout = 3 * time.Second
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
	query, maxResults, ctx, cancel, err := prepareSearch(query, maxResults)
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
	query, maxResults, ctx, cancel, err := prepareSearch(query, maxResults)
	if err != nil {
		return nil, err
	}
	defer cancel()

	provider = normalizeProvider(provider)
	providerCtx, providerCancel := context.WithTimeout(ctx, providerSearchTimeout)
	defer providerCancel()

	var results []SearchResult
	switch provider.Type {
	case "brave":
		if provider.Key == "" {
			return searchDirectFallbackChain(ctx, query, maxResults, "")
		}
		results, err = searchBrave(providerCtx, provider, query, maxResults)
	case "serper":
		if provider.Key == "" {
			return searchDirectFallbackChain(ctx, query, maxResults, "")
		}
		results, err = searchSerper(providerCtx, provider, query, maxResults)
	case "tinyfish":
		if provider.Key == "" {
			return searchDirectFallbackChain(ctx, query, maxResults, "")
		}
		results, err = searchTinyFish(providerCtx, provider, query, maxResults)
	case "duckduckgo":
		results, err = searchDuckDuckGo(providerCtx, provider, query, maxResults)
	default:
		return searchDirectFallbackChain(ctx, query, maxResults, "")
	}
	if err == nil && len(results) > 0 {
		return results, nil
	}
	return fallbackDirectSearch(ctx, query, maxResults, provider, err, results)
}

func prepareSearch(query string, maxResults int) (string, int, context.Context, context.CancelFunc, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", 0, nil, nil, fmt.Errorf("query is empty")
	}
	if maxResults <= 0 {
		maxResults = 8
	}
	if maxResults > 20 {
		maxResults = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeout)
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
	form := url.Values{}
	form.Set("q", query)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	return parseDDGResults(string(body), maxResults), nil
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

func searchDirectFallbackChain(ctx context.Context, query string, maxResults int, skipFailureDomain string) ([]SearchResult, error) {
	var failures []string
	for _, endpoint := range directSearchEndpoints() {
		if skipFailureDomain != "" && endpoint.FailureDomain == skipFailureDomain {
			continue
		}
		endpointCtx, cancel := directEndpointContext(ctx)
		results, err := endpoint.Search(endpointCtx, query, maxResults)
		cancel()
		if err == nil && len(results) > 0 {
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
		{Name: "duckduckgo-html", FailureDomain: "duckduckgo", Search: searchDirectLegacy},
		{Name: "mojeek-html", FailureDomain: "mojeek", Search: searchMojeekDirect},
	}
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
	searchURL := baseURL + "?q=" + url.QueryEscape(query) + "&count=" + fmt.Sprintf("%d", maxResults)
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
	searchURL := baseURL + "?query=" + url.QueryEscape(query)
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

// FetchWithProvider performs a provider-aware fetch. When the provider has
// enhanced fetch capabilities (e.g. TinyFish), it uses the provider's API
// for better content extraction, falling back to standard Fetch on failure.
// Pass a zero-value provider to use standard fetch directly.
func FetchWithProvider(rawURL string, opts *FetchOptions, provider corelib.WebSearchProvider) (*FetchResult, error) {
	provider = normalizeProvider(provider)

	// TinyFish has its own fetch API with better content extraction.
	if provider.Type == "tinyfish" && provider.Key != "" && opts != nil && opts.SavePath == "" {
		fetchURL := deriveTinyFishFetchURL(provider.BaseURL)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := FetchWithTinyFish(ctx, rawURL, provider.Key, fetchURL)
		if err == nil && result != nil && result.Content != "" {
			// Apply offset + maxChars windowing on full content
			applyFetchWindowing(result, opts.Offset, opts.MaxChars)
			return result, nil
		}
		// TinyFish failed — fall through to standard fetch
	}

	return Fetch(rawURL, opts)
}

// deriveTinyFishFetchURL derives the fetch endpoint from the search base URL.
func deriveTinyFishFetchURL(searchBaseURL string) string {
	if searchBaseURL == "" || searchBaseURL == defaultTinyFishSearchURL {
		return defaultTinyFishFetchURL
	}
	// Custom base URL (e.g. proxy): try replacing "search" with "fetch"
	fetched := strings.Replace(searchBaseURL, "search", "fetch", 1)
	if fetched == searchBaseURL {
		return defaultTinyFishFetchURL // no "search" in URL, use default
	}
	return fetched
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

// searchDirectLegacy scrapes DuckDuckGo HTML lite for search results.
func searchDirectLegacy(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	searchURL := defaultLegacySearchURL + "?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}

	return parseDDGResults(string(body), maxResults), nil
}

// searchMojeekDirect scrapes Mojeek HTML as a provider-diverse direct fallback.
func searchMojeekDirect(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	searchURL := defaultMojeekSearchURL + "?q=" + url.QueryEscape(query)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Mojeek returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}

	return parseMojeekResults(string(body), maxResults), nil
}

// parseDDGResults extracts search results from DuckDuckGo HTML lite response.
func parseDDGResults(html string, maxResults int) []SearchResult {
	var results []SearchResult

	// DuckDuckGo HTML lite uses <a class="result__a" href="...">title</a>
	// and <a class="result__snippet" ...>snippet</a>
	remaining := html
	for len(results) < maxResults {
		// Find result link
		idx := strings.Index(remaining, `class="result__a"`)
		if idx < 0 {
			break
		}
		remaining = remaining[idx:]

		// Extract href
		href := extractAttr(remaining, "href")
		if len(remaining) <= 17 {
			break
		}
		if href == "" {
			remaining = remaining[17:]
			continue
		}
		// DuckDuckGo wraps URLs in redirect: //duckduckgo.com/l/?uddg=...
		href = resolveDDGURL(href)
		href = normalizeSearchResultURL(href)

		// Extract title (text between > and </a>)
		title := extractTagText(remaining, "a")

		// Find snippet
		snippet := ""
		snippetIdx := strings.Index(remaining, `class="result__snippet"`)
		if snippetIdx > 0 && snippetIdx < 2000 {
			snippet = extractTagText(remaining[snippetIdx:], "a")
			if snippet == "" {
				// Try span-based snippet
				snippet = extractTagText(remaining[snippetIdx:], "span")
			}
		}

		if href != "" && title != "" {
			results = append(results, SearchResult{
				Title:   cleanHTML(title),
				URL:     href,
				Snippet: cleanHTML(snippet),
			})
		}

		if len(remaining) > 17 {
			remaining = remaining[17:]
		} else {
			break
		}
	}

	return results
}

// parseMojeekResults extracts search results from Mojeek's HTML response.
func parseMojeekResults(html string, maxResults int) []SearchResult {
	var results []SearchResult
	remaining := html
	lowerRemaining := strings.ToLower(html)
	for len(results) < maxResults {
		idx := strings.Index(lowerRemaining, `<a `)
		if idx < 0 {
			break
		}
		contextBefore := remaining[:idx]
		remaining = remaining[idx:]
		lowerRemaining = lowerRemaining[idx:]
		openEnd := strings.Index(remaining, ">")
		if openEnd < 0 {
			break
		}
		anchorOpen := remaining[:openEnd+1]

		href := normalizeSearchResultURL(extractAttr(anchorOpen, "href"))
		title := cleanHTML(extractTagText(remaining, "a"))
		if href != "" && title != "" && isSearchResultAnchor(contextBefore, anchorOpen) && !looksLikeSearchChromeURL(href) {
			results = append(results, SearchResult{
				Title: title,
				URL:   href,
			})
		}

		if len(remaining) > 6 {
			remaining = remaining[6:]
			lowerRemaining = lowerRemaining[6:]
		} else {
			break
		}
	}
	return results
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
	case host == "mojeek.com" || strings.HasSuffix(host, ".mojeek.com"):
		return true
	case host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com"):
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
