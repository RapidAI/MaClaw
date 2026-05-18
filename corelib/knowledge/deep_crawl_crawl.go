package knowledge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// hostRateLimiter 管理每个 host 的请求间隔
type hostRateLimiter struct {
	mu       sync.Mutex
	lastReqs map[string]time.Time
	delay    time.Duration
}

func newHostRateLimiter(delay time.Duration) *hostRateLimiter {
	return &hostRateLimiter{
		lastReqs: make(map[string]time.Time),
		delay:    delay,
	}
}

// wait 等待直到可以向指定 host 发送请求（满足最小间隔要求）。
// 使用乐观更新模式：在锁内计算下一个允许时间并立即占位，然后在锁外 sleep。
// 这确保多个 goroutine 对同一 host 的请求被串行化，不会绕过延迟约束。
func (rl *hostRateLimiter) wait(ctx context.Context, host string) error {
	rl.mu.Lock()
	last, ok := rl.lastReqs[host]
	var waitTime time.Duration
	now := time.Now()
	if ok {
		earliest := last.Add(rl.delay)
		if now.Before(earliest) {
			waitTime = earliest.Sub(now)
			// 乐观占位：将 lastReqs 设为 earliest（本次请求的预期发出时间）
			// 后续 goroutine 会基于这个时间计算自己的等待
			rl.lastReqs[host] = earliest
		} else {
			rl.lastReqs[host] = now
		}
	} else {
		rl.lastReqs[host] = now
	}
	rl.mu.Unlock()

	if waitTime > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitTime):
		}
	}
	return nil
}

// StartCrawl 启动深度检索（阻塞直到完成或取消）
func (e *DeepCrawlEngine) StartCrawl(ctx context.Context, req DeepCrawlRequest) (DeepCrawlResult, error) {
	// 1. Validate seed URL
	seedURL := strings.TrimSpace(req.SeedURL)
	if seedURL == "" {
		return DeepCrawlResult{}, fmt.Errorf("seed URL is required")
	}
	parsedSeed, err := e.validateSeedForEngine(seedURL)
	if err != nil {
		return DeepCrawlResult{}, fmt.Errorf("invalid seed URL: %w", err)
	}
	// 使用 normalizeURL 作为 visited set 的 key，确保与 nextLevel 去重使用相同的规范化逻辑
	normalizedSeed := normalizeURL(parsedSeed.String())
	if normalizedSeed == "" {
		normalizedSeed = parsedSeed.String()
	}
	seedHost := strings.ToLower(parsedSeed.Hostname())

	// 2. Check domain policy for seed URL
	if e.store != nil {
		if err := enforceURLDomainPolicy(ctx, e.store, normalizedSeed); err != nil {
			return DeepCrawlResult{}, fmt.Errorf("seed URL blocked by domain policy: %w", err)
		}
	}

	// 3. Validate and normalize maxDepth
	maxDepth := req.MaxDepth
	if maxDepth < 1 {
		maxDepth = 1
	}
	if maxDepth > 5 {
		maxDepth = 5
	}

	// 4. Create session context with timeout
	sessionCtx, sessionCancel := context.WithTimeout(ctx, e.sessionTimeout)
	defer sessionCancel()

	// 5. Generate job ID
	jobID := uuid.New().String()[:8]

	// 6. Initialize BFS state
	state := &crawlState{
		visited: make(map[string]struct{}),
	}
	state.visited[normalizedSeed] = struct{}{}
	state.totalQueued = 1

	// 7. Initialize rate limiter and semaphore
	rateLimiter := newHostRateLimiter(e.requestDelay)
	semaphore := make(chan struct{}, e.maxConcurrency)

	// 8. BFS loop
	currentLevel := []string{normalizedSeed}
	var byDepth []DeepCrawlDepthSummary

	for depth := 0; depth <= maxDepth && len(currentLevel) > 0; depth++ {
		// Check session context cancellation between levels
		if sessionCtx.Err() != nil {
			return e.buildPartialResult(jobID, "cancelled", state, byDepth), nil
		}

		depthSummary := DeepCrawlDepthSummary{
			Depth: depth,
			Total: len(currentLevel),
		}

		// Process current level URLs concurrently
		type workerResult struct {
			item    DeepCrawlItem
			newURLs []string
		}

		var wg sync.WaitGroup
		resultsCh := make(chan workerResult, len(currentLevel))

		for _, pageURL := range currentLevel {
			// Check context before launching worker
			if sessionCtx.Err() != nil {
				break
			}

			wg.Add(1)
			go func(url string, d int) {
				defer wg.Done()

				// Acquire semaphore
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-sessionCtx.Done():
					resultsCh <- workerResult{
						item: DeepCrawlItem{URL: url, Depth: d, Status: "skipped", Error: "cancelled"},
					}
					return
				}

				// Check context after acquiring semaphore
				if sessionCtx.Err() != nil {
					resultsCh <- workerResult{
						item: DeepCrawlItem{URL: url, Depth: d, Status: "skipped", Error: "cancelled"},
					}
					return
				}

				// Rate limiting: wait for per-host delay
				host := extractHostname(url)
				if err := rateLimiter.wait(sessionCtx, host); err != nil {
					resultsCh <- workerResult{
						item: DeepCrawlItem{URL: url, Depth: d, Status: "skipped", Error: "cancelled"},
					}
					return
				}

				// Fetch HTML with per-URL timeout
				htmlContent, fetchErr := e.fetchHTML(sessionCtx, url)
				if fetchErr != nil {
					resultsCh <- workerResult{
						item: DeepCrawlItem{URL: url, Depth: d, Status: "failed", Error: fetchErr.Error()},
					}
					return
				}

				// Save content (non-preview mode)
				item := DeepCrawlItem{URL: url, Depth: d}
				if !req.PreviewOnly {
					item = e.saveContent(sessionCtx, url, d, htmlContent, req)
				} else {
					item.Status = "discovered"
				}

				// Discover links for next level (only if not at max depth)
				var newURLs []string
				if d < maxDepth {
					newURLs = e.discoverLinks(htmlContent, url, seedHost, req.SameDomainOnly)
				}

				resultsCh <- workerResult{item: item, newURLs: newURLs}
			}(pageURL, depth)
		}

		// Wait for all workers at this level to complete
		go func() {
			wg.Wait()
			close(resultsCh)
		}()

		// Collect results
		var nextLevelCandidates []string
		for wr := range resultsCh {
			// Record item result
			state.mu.Lock()
			state.results = append(state.results, wr.item)
			switch wr.item.Status {
			case "saved":
				state.completed++
				depthSummary.Saved++
			case "duplicate":
				state.skipped++
			case "failed":
				state.failed++
				depthSummary.Failed++
			case "skipped":
				state.skipped++
			default:
				state.completed++
			}
			state.mu.Unlock()

			// Collect next level URLs
			nextLevelCandidates = append(nextLevelCandidates, wr.newURLs...)

			// Emit progress
			if e.onProgress != nil {
				state.mu.Lock()
				e.onProgress(DeepCrawlProgress{
					JobID:           jobID,
					Status:          "crawling",
					CurrentDepth:    depth,
					MaxDepth:        maxDepth,
					TotalDiscovered: state.totalQueued,
					Completed:       state.completed,
					Pending:         state.totalQueued - state.completed - state.failed - state.skipped,
					Failed:          state.failed,
					Skipped:         state.skipped,
					CurrentURL:      wr.item.URL,
				})
				state.mu.Unlock()
			}
		}

		byDepth = append(byDepth, depthSummary)

		// Deduplicate and filter next level URLs, respecting max URL limit
		var nextLevel []string
		state.mu.Lock()
		for _, candidate := range nextLevelCandidates {
			normalized := normalizeURL(candidate)
			if normalized == "" {
				continue
			}
			if _, exists := state.visited[normalized]; exists {
				continue
			}
			if state.totalQueued >= e.maxURLs {
				break
			}
			// Domain policy check (single-threaded here to avoid DB lock contention in workers)
			if e.store != nil {
				if err := enforceURLDomainPolicy(sessionCtx, e.store, normalized); err != nil {
					continue
				}
			}
			state.visited[normalized] = struct{}{}
			state.totalQueued++
			nextLevel = append(nextLevel, normalized)
		}
		state.mu.Unlock()

		currentLevel = nextLevel
	}

	// 9. Determine final status
	finalStatus := "completed"
	if sessionCtx.Err() != nil {
		finalStatus = "timeout"
	}

	state.mu.Lock()
	if state.totalQueued >= e.maxURLs {
		finalStatus = "limit_reached"
	}
	state.mu.Unlock()

	// 10. Emit final progress
	if e.onProgress != nil {
		state.mu.Lock()
		e.onProgress(DeepCrawlProgress{
			JobID:           jobID,
			Status:          finalStatus,
			CurrentDepth:    maxDepth,
			MaxDepth:        maxDepth,
			TotalDiscovered: state.totalQueued,
			Completed:       state.completed,
			Pending:         0,
			Failed:          state.failed,
			Skipped:         state.skipped,
		})
		state.mu.Unlock()
	}

	return e.buildPartialResult(jobID, finalStatus, state, byDepth), nil
}

// saveContent 保存单个 URL 的内容到知识库
func (e *DeepCrawlEngine) saveContent(ctx context.Context, pageURL string, depth int, htmlContent string, req DeepCrawlRequest) DeepCrawlItem {
	item := DeepCrawlItem{URL: pageURL, Depth: depth}

	if e.store == nil {
		item.Status = "failed"
		item.Error = "store not available"
		return item
	}

	// Domain policy already enforced:
	// - Seed URL checked at StartCrawl entry
	// - Discovered URLs checked in discoverLinks()
	// No need to re-check here.

	// Call SaveURL with pre-fetched HTML to avoid re-fetching
	saveReq := URLSaveRequest{
		URL:            pageURL,
		OwnerID:        req.OwnerID,
		ProjectPath:    req.ProjectPath,
		TopicHint:      req.TopicHint,
		SaveScope:      req.SaveScope,
		DistillMode:    req.DistillMode,
		Labels:         req.Labels,
		AutoLabels:     req.AutoLabels,
		PrefetchedHTML: htmlContent,
	}

	source, err := e.store.SaveURL(ctx, saveReq)
	if err != nil {
		// Check if it's a duplicate (SaveURL handles duplicates internally by updating)
		// If SaveURL returns an error, it's a real failure
		item.Status = "failed"
		item.Error = err.Error()
		return item
	}

	// SaveURL succeeded
	if source.ID != "" {
		item.Status = "saved"
		item.SourceID = source.ID
		item.Title = source.Title
	} else {
		item.Status = "saved"
	}

	return item
}

// discoverLinks 从 HTML 内容中发现链接并过滤（纯内存操作，无 DB 调用）
func (e *DeepCrawlEngine) discoverLinks(htmlContent, baseURL, seedHost string, sameDomainOnly bool) []string {
	discoveryResult := DiscoverURLsFromText(URLDiscoveryRequest{
		Text:           htmlContent,
		BaseURL:        baseURL,
		SameDomainOnly: sameDomainOnly,
		Limit:          e.maxURLs,
	})

	var urls []string
	for _, item := range discoveryResult.Items {
		if item.Status != URLDiscoveryStatusCandidate {
			continue
		}

		// Same-domain check (additional check beyond DiscoverURLsFromText)
		if sameDomainOnly {
			discoveredHost := strings.ToLower(item.Host)
			if discoveredHost != "" && !sameOrSubdomain(discoveredHost, seedHost) {
				continue
			}
		}

		urls = append(urls, item.URL)
	}

	return urls
}

// buildPartialResult 构建（部分）结果
func (e *DeepCrawlEngine) buildPartialResult(jobID, status string, state *crawlState, byDepth []DeepCrawlDepthSummary) DeepCrawlResult {
	state.mu.Lock()
	defer state.mu.Unlock()

	return DeepCrawlResult{
		JobID:      jobID,
		Status:     status,
		TotalSaved: state.completed,
		Duplicates: 0, // SaveURL handles duplicates by updating, so we don't track separately
		Failed:     state.failed,
		Skipped:    state.skipped,
		Items:      state.results,
		ByDepth:    byDepth,
	}
}
