package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	defaultBraveSearchURL  = "https://api.search.brave.com/res/v1/web/search"
	defaultSerperSearchURL = "https://google.serper.dev/search"
)

var defaultLegacySearchURL = "https://html.duckduckgo.com/html/"

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Search performs the legacy direct web search and returns results.
func Search(query string, maxResults int) ([]SearchResult, error) {
	query, maxResults, ctx, cancel, err := prepareSearch(query, maxResults)
	if err != nil {
		return nil, err
	}
	defer cancel()
	results, err := searchDirectLegacy(ctx, query, maxResults)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	return results, nil
}

// SearchWithProvider performs a provider-aware search and falls back to Search
// when the selected provider is unavailable due to missing credentials.
func SearchWithProvider(query string, maxResults int, provider corelib.WebSearchProvider) ([]SearchResult, error) {
	query, maxResults, ctx, cancel, err := prepareSearch(query, maxResults)
	if err != nil {
		return nil, err
	}
	defer cancel()

	provider = normalizeProvider(provider)
	switch provider.Type {
	case "brave":
		if provider.Key == "" {
			return searchDirectLegacy(ctx, query, maxResults)
		}
		return searchBrave(ctx, provider, query, maxResults)
	case "serper":
		if provider.Key == "" {
			return searchDirectLegacy(ctx, query, maxResults)
		}
		return searchSerper(ctx, provider, query, maxResults)
	case "duckduckgo":
		return searchDuckDuckGo(ctx, provider, query, maxResults)
	default:
		return searchDirectLegacy(ctx, query, maxResults)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

// extractAttr extracts the value of the given attribute from the current position.
func extractAttr(s, attr string) string {
	key := attr + `="`
	idx := strings.Index(s, key)
	if idx < 0 || idx > 200 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(s[start:], `"`)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
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
	end := strings.Index(s[start:], endTag)
	if end < 0 {
		return ""
	}
	return s[start : start+end]
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
