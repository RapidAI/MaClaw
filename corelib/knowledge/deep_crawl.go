package knowledge

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// deepCrawlHTTPClient is a dedicated HTTP client for deep crawl fetches.
// It uses its own connection pool (isolated from the rest of the application)
// and has conservative timeouts to prevent resource leaks.
var deepCrawlHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   3,
		IdleConnTimeout:       60 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	},
	// No client-level Timeout — per-request timeout is controlled by context.
	// Max 10 redirects (Go default).
}

// DeepCrawlEngine 深度检索引擎
type DeepCrawlEngine struct {
	store          *SQLiteStore
	onProgress     func(DeepCrawlProgress)
	maxConcurrency int           // 默认 3
	requestDelay   time.Duration // 默认 500ms
	maxURLs        int           // 默认 200
	perURLTimeout  time.Duration // 默认 30s
	sessionTimeout time.Duration // 默认 10min

	// skipPublicCheck disables the public URL validation (for testing with httptest servers).
	skipPublicCheck bool

	// fetchFunc overrides the default fetchHTML behavior (for testing).
	// If nil, the default HTTP fetch is used.
	fetchFunc func(ctx context.Context, rawURL string) (string, error)
}

// NewDeepCrawlEngine 创建引擎实例
func NewDeepCrawlEngine(store *SQLiteStore, onProgress func(DeepCrawlProgress)) *DeepCrawlEngine {
	return &DeepCrawlEngine{
		store:          store,
		onProgress:     onProgress,
		maxConcurrency: 3,
		requestDelay:   500 * time.Millisecond,
		maxURLs:        200,
		perURLTimeout:  30 * time.Second,
		sessionTimeout: 10 * time.Minute,
	}
}

// Preview 预览模式（仅发现链接，不保存内容）
// 执行轻量级 BFS 发现（最多 2 层），不调用 SaveURL。
func (e *DeepCrawlEngine) Preview(ctx context.Context, req DeepCrawlRequest) (DeepCrawlResult, error) {
	// 1. Validate seed URL
	seedURL := strings.TrimSpace(req.SeedURL)
	if seedURL == "" {
		return DeepCrawlResult{}, fmt.Errorf("seed URL is required")
	}
	parsedSeed, err := e.validateSeedForEngine(seedURL)
	if err != nil {
		return DeepCrawlResult{}, fmt.Errorf("invalid seed URL: %w", err)
	}
	// 使用 normalizeURL 确保与去重逻辑一致
	normalizedSeed := normalizeURL(parsedSeed.String())
	if normalizedSeed == "" {
		normalizedSeed = parsedSeed.String()
	}
	seedHost := strings.ToLower(parsedSeed.Hostname())

	// 2. Create context with session timeout
	ctx, cancel := context.WithTimeout(ctx, e.sessionTimeout)
	defer cancel()

	// 3. Initialize visited set and BFS state
	visited := make(map[string]struct{})
	visited[normalizedSeed] = struct{}{}

	// Preview uses at most 2 levels (depth 0 = seed, depth 1 = links from seed)
	maxPreviewDepth := 2
	if req.MaxDepth > 0 && req.MaxDepth < maxPreviewDepth {
		maxPreviewDepth = req.MaxDepth
	}

	// BFS levels: currentLevel starts with seed URL at depth 0
	currentLevel := []string{normalizedSeed}
	var byDepth []DeepCrawlDepthSummary
	var items []DeepCrawlItem
	totalDiscovered := 0
	limitReached := false

	// 4. BFS loop: process at most maxPreviewDepth levels
	for depth := 0; depth < maxPreviewDepth && len(currentLevel) > 0; depth++ {
		// Check context cancellation
		if ctx.Err() != nil {
			return DeepCrawlResult{
				Status:          "cancelled",
				TotalDiscovered: totalDiscovered,
				Items:           items,
				ByDepth:         byDepth,
			}, nil
		}

		depthSummary := DeepCrawlDepthSummary{
			Depth: depth,
			Total: len(currentLevel),
			URLs:  make([]string, len(currentLevel)),
		}
		copy(depthSummary.URLs, currentLevel)
		totalDiscovered += len(currentLevel)

		var nextLevel []string

		// Process each URL at current depth
		for _, pageURL := range currentLevel {
			// Check context cancellation
			if ctx.Err() != nil {
				byDepth = append(byDepth, depthSummary)
				return DeepCrawlResult{
					Status:          "cancelled",
					TotalDiscovered: totalDiscovered,
					Items:           items,
					ByDepth:         byDepth,
				}, nil
			}

			// Add item as discovered
			items = append(items, DeepCrawlItem{
				URL:    pageURL,
				Depth:  depth,
				Status: "discovered",
			})

			// Emit progress
			if e.onProgress != nil {
				e.onProgress(DeepCrawlProgress{
					Mode:            "preview",
					ClientRunID:     req.ClientRunID,
					Status:          "discovering",
					CurrentDepth:    depth,
					MaxDepth:        maxPreviewDepth,
					TotalDiscovered: totalDiscovered + len(nextLevel),
					CurrentURL:      pageURL,
				})
			}

			// Fetch HTML content with per-URL timeout
			htmlContent, fetchErr := e.fetchHTML(ctx, pageURL)
			if fetchErr != nil {
				// Record failure but continue
				continue
			}

			// Ask for one candidate beyond the remaining capacity so limit_reached
			// means a real, normalized URL was truncated instead of merely reaching
			// the exact natural crawl size.
			discoveryLimit := e.maxURLs - totalDiscovered - len(nextLevel) + 1
			if discoveryLimit < 1 {
				discoveryLimit = 1
			}
			discoveryResult := DiscoverURLsFromText(URLDiscoveryRequest{
				Text:           htmlContent,
				BaseURL:        pageURL,
				SameDomainOnly: req.SameDomainOnly,
				Limit:          discoveryLimit,
			})

			// Filter and deduplicate discovered URLs
			for _, item := range discoveryResult.Items {
				if item.Status != URLDiscoveryStatusCandidate {
					continue
				}
				discoveredURL := item.URL

				// Same-domain check
				if req.SameDomainOnly {
					discoveredHost := strings.ToLower(item.Host)
					if discoveredHost != "" && !sameOrSubdomain(discoveredHost, seedHost) {
						continue
					}
				}

				// Normalize for deduplication (consistent with StartCrawl's nextLevel dedup)
				normalizedDiscovered := normalizeURL(discoveredURL)
				if normalizedDiscovered == "" {
					continue
				}

				// Deduplication using normalized URL
				if _, exists := visited[normalizedDiscovered]; exists {
					continue
				}

				// Max URL limit
				if totalDiscovered+len(nextLevel) >= e.maxURLs {
					limitReached = true
					break
				}

				visited[normalizedDiscovered] = struct{}{}
				nextLevel = append(nextLevel, normalizedDiscovered)
			}
		}

		byDepth = append(byDepth, depthSummary)
		currentLevel = nextLevel
	}

	// If there are remaining URLs in currentLevel (depth == maxPreviewDepth), add them as a final depth summary
	if len(currentLevel) > 0 {
		depthSummary := DeepCrawlDepthSummary{
			Depth: maxPreviewDepth,
			Total: len(currentLevel),
			URLs:  make([]string, len(currentLevel)),
		}
		copy(depthSummary.URLs, currentLevel)
		totalDiscovered += len(currentLevel)

		for _, u := range currentLevel {
			items = append(items, DeepCrawlItem{
				URL:    u,
				Depth:  maxPreviewDepth,
				Status: "discovered",
			})
		}
		byDepth = append(byDepth, depthSummary)
	}

	finalStatus := "completed"
	if limitReached {
		finalStatus = "limit_reached"
	}

	// Emit final progress
	if e.onProgress != nil {
		e.onProgress(DeepCrawlProgress{
			Mode:            "preview",
			ClientRunID:     req.ClientRunID,
			Status:          finalStatus,
			CurrentDepth:    maxPreviewDepth,
			MaxDepth:        maxPreviewDepth,
			TotalDiscovered: totalDiscovered,
		})
	}

	return DeepCrawlResult{
		Status:          finalStatus,
		TotalDiscovered: totalDiscovered,
		Items:           items,
		ByDepth:         byDepth,
	}, nil
}

// validateSeedForEngine validates the seed URL, optionally skipping public IP checks (for testing).
func (e *DeepCrawlEngine) validateSeedForEngine(seedURL string) (*url.URL, error) {
	if e.skipPublicCheck {
		u, err := url.Parse(seedURL)
		if err != nil {
			return nil, err
		}
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
		}
		if u.Hostname() == "" {
			return nil, fmt.Errorf("URL host is required")
		}
		u.Fragment = ""
		return u, nil
	}
	return ValidatePublicHTTPURL(seedURL)
}

// fetchHTML fetches the HTML content of a URL with per-URL timeout.
// Known limitation: HTTP redirects are followed transparently (Go default behavior),
// but the final URL after redirect is not fed back to the visited set. This means
// a redirect target may be fetched again if discovered independently. SaveURL's
// internal deduplication handles this at the persistence layer.
func (e *DeepCrawlEngine) fetchHTML(ctx context.Context, rawURL string) (string, error) {
	if e.fetchFunc != nil {
		return e.fetchFunc(ctx, rawURL)
	}

	ctx, cancel := context.WithTimeout(ctx, e.perURLTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MaClaw/1.0; +https://maclaw.ai)")

	resp, err := deepCrawlHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Only process HTML/text content — skip binary resources (PDF, images, archives)
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !isHTMLContentType(ct) {
		return "", fmt.Errorf("non-HTML content type: %s", ct)
	}

	// Read up to 5MB of content
	const maxBytes = 5 * 1024 * 1024
	limited := io.LimitReader(resp.Body, maxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}
