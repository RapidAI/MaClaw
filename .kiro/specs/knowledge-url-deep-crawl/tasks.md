# Implementation Plan: Knowledge URL Deep Crawl

## Overview

实现知识库 URL 深度检索功能，从种子 URL 出发按 BFS 策略逐层发现同站链接并保存到知识库。采用 Go channel + Worker Pool 并发模型，复用现有的 DiscoverURLsFromText 和 SaveURL 基础设施，通过 Wails 事件系统提供实时进度反馈。

## Tasks

- [x] 1. 定义核心数据类型和接口
  - [x] 1.1 Create DeepCrawl types in `corelib/knowledge/deep_crawl_types.go`
    - Define `DeepCrawlRequest`, `DeepCrawlProgress`, `DeepCrawlResult`, `DeepCrawlItem`, `DeepCrawlDepthSummary` structs
    - Define `bfsLevel` and `crawlState` internal types
    - _Requirements: 1.1, 1.2, 1.3, 2.1, 3.1, 5.4_

  - [x] 1.2 Create DeepCrawlEngine struct and constructor in `corelib/knowledge/deep_crawl.go`
    - Define `DeepCrawlEngine` struct with store, onProgress callback, concurrency/delay/timeout config fields
    - Implement `NewDeepCrawlEngine(store, onProgress)` constructor with default values (3 concurrent, 500ms delay, 200 max URLs, 30s per-URL timeout, 10min session timeout)
    - _Requirements: 6.1, 6.2, 6.3, 6.5, 6.6_

- [x] 2. Implement BFS crawl engine core logic
  - [x] 2.1 Implement URL validation and normalization
    - Validate seed URL starts with http:// or https://
    - Implement URL normalization for deduplication (lowercase scheme/host, remove fragment, sort query params)
    - Implement same-domain hostname comparison
    - _Requirements: 1.4, 2.2, 2.4_

  - [x] 2.2 Implement BFS discovery loop in `StartCrawl` method
    - Initialize visited set with seed URL, process levels depth 0 to maxDepth
    - For each level: launch workers (bounded by semaphore channel of size 3), fetch HTML, extract links via `DiscoverURLsFromText`
    - Filter discovered URLs: same-domain check, domain policy check, deduplication, max URL limit
    - Collect next level URLs, emit progress events after each URL completion
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 6.3, 6.4_

  - [x] 2.3 Implement content save logic
    - For non-preview mode: call `SaveURL` for each successfully fetched page
    - Pass SaveScope, TopicHint, DistillMode, Labels, AutoLabels from request to each SaveURL call
    - Skip URLs that already exist as sources in Knowledge_Store (duplicate detection by canonical URL)
    - Record save result (saved/duplicate/failed) in DeepCrawlItem
    - _Requirements: 5.1, 5.2, 5.3_

  - [x] 2.4 Implement concurrency control and rate limiting
    - Use semaphore channel (buffered channel of size 3) to limit concurrent HTTP requests
    - Implement per-host request delay (500ms minimum between requests to same host) using sync.Mutex + time tracking per host
    - Implement per-URL fetch timeout (30s) via context.WithTimeout
    - Implement session timeout (10min) via parent context.WithTimeout
    - _Requirements: 6.1, 6.2, 6.5, 6.6_

  - [x] 2.5 Implement cancellation and error handling
    - Check context cancellation at each URL processing step
    - Record failure reason for network errors, HTTP 4xx/5xx, timeouts
    - Continue processing remaining URLs after individual failures
    - Return partial results on cancellation or session timeout
    - _Requirements: 3.4, 3.5_

  - [ ]* 2.6 Write property tests for BFS engine (Properties 1-5)
    - **Property 1: Invalid URL rejection** - random non-http(s) strings must be rejected
    - **Property 2: Same-domain filtering** - cross-domain URLs must not be enqueued when restriction enabled
    - **Property 3: BFS ordering invariant** - all depth N URLs completed before depth N+1 starts
    - **Property 4: URL deduplication** - each normalized URL appears at most once in results
    - **Property 5: Depth limit enforcement** - no URL in results has depth > maxDepth
    - **Validates: Requirements 1.4, 2.2, 2.3, 2.4, 2.5**

  - [ ]* 2.7 Write property tests for policy and fault tolerance (Properties 6-8)
    - **Property 6: Domain policy enforcement** - blocked URLs skipped with rejection reason recorded
    - **Property 7: Fault tolerance** - failed URLs recorded and remaining URLs continue processing
    - **Property 8: Preview mode does not persist** - zero SaveURL calls in preview mode
    - **Validates: Requirements 2.6, 3.4, 4.1, 7.3**

- [x] 3. Implement Preview mode
  - [x] 3.1 Implement `Preview` method in DeepCrawlEngine
    - Perform lightweight discovery pass (fetch HTML + extract links) for first 2 levels only
    - Do not call SaveURL for any discovered page
    - Return DeepCrawlResult with ByDepth populated (URLs per depth level)
    - Return total count of discovered URLs
    - _Requirements: 4.1, 4.2, 4.3, 4.5_

- [x] 4. Checkpoint - Ensure all core engine tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 5. Implement Wails bindings and event system
  - [x] 5.1 Add Wails binding methods in `gui/app_knowledge.go`
    - Implement `KnowledgeDeepCrawl(req)` - starts full crawl, manages cancel context
    - Implement `KnowledgeDeepCrawlPreview(req)` - starts preview mode
    - Implement `KnowledgeDeepCrawlCancel()` - cancels active crawl via context
    - Store active crawl cancel func in App struct for cancellation support
    - _Requirements: 7.4, 7.5, 3.5_

  - [x] 5.2 Implement progress event emission
    - Create onProgress callback that calls `runtime.EventsEmit(ctx, "knowledge:deep-crawl-progress", progress)`
    - Emit progress after each URL is processed (completed/failed/skipped)
    - Emit final status event on completion/cancellation/timeout
    - _Requirements: 3.1, 3.2, 3.3, 7.5_

  - [ ]* 5.3 Write property tests for configuration propagation and result consistency (Properties 9-10)
    - **Property 9: Configuration propagation** - SaveScope/TopicHint/Labels passed to every SaveURL call
    - **Property 10: Result count consistency** - TotalSaved + Duplicates + Failed + Skipped equals total processed URLs
    - **Validates: Requirements 5.2, 5.4**

- [x] 6. Implement frontend DeepCrawlPanel component
  - [x] 6.1 Create DeepCrawlPanel React component in `gui/frontend/src/components/settings/DeepCrawlPanel.tsx`
    - Input field for seed URL with http/https validation
    - Depth selector (1-5, default 2)
    - Same-domain toggle (default enabled)
    - Save scope, topic hint, and labels fields (reuse existing KnowledgeSettingsPanel patterns)
    - Preview button and Start Crawl button
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5_

  - [x] 6.2 Implement progress display and cancel functionality
    - Listen to `knowledge:deep-crawl-progress` Wails event
    - Display progress bar with percentage (completed / total_discovered)
    - Show current status: total discovered, completed, pending, failed, current depth
    - Display current URL being processed
    - Cancel button that calls `KnowledgeDeepCrawlCancel`
    - _Requirements: 3.1, 3.2, 3.3, 3.5_

  - [x] 6.3 Implement preview results display
    - Show preview results grouped by depth level
    - Display total count of discovered URLs
    - Confirm button to proceed with full crawl using same configuration
    - Cancel button to discard preview data
    - _Requirements: 4.2, 4.3, 4.4, 4.5_

  - [x] 6.4 Integrate DeepCrawlPanel into KnowledgeSettingsPanel
    - Add DeepCrawlPanel as a section within existing KnowledgeSettingsPanel
    - Wire up Wails bindings (KnowledgeDeepCrawl, KnowledgeDeepCrawlPreview, KnowledgeDeepCrawlCancel)
    - _Requirements: 7.4_

- [x] 7. Checkpoint - Ensure frontend and backend integration works
  - Ensure all tests pass, ask the user if questions arise.

- [ ] 8. Implement concurrency and resource protection property tests
  - [ ]* 8.1 Write property test for concurrency limit (Property 11)
    - **Property 11: Concurrency limit** - in-flight HTTP requests never exceed 3 at any point
    - Use atomic counter in mock fetcher to track concurrent requests
    - **Validates: Requirements 6.1**

  - [ ]* 8.2 Write property test for request delay enforcement (Property 12)
    - **Property 12: Request delay enforcement** - consecutive requests to same host have >= 500ms gap
    - Use timestamp recording in mock fetcher
    - **Validates: Requirements 6.2**

  - [ ]* 8.3 Write property test for maximum URL limit (Property 13)
    - **Property 13: Maximum URL limit** - total URLs processed never exceeds 200
    - Use large random graphs (>200 nodes) as input
    - **Validates: Requirements 6.3**

- [x] 9. Integration testing and wiring
  - [x] 9.1 Write integration tests with httptest server
    - Set up httptest server with multi-page site structure (links between pages)
    - Test full crawl flow: discovery → save → progress events → result summary
    - Test preview flow: discovery only, no saves
    - Test cancellation mid-crawl
    - Test domain policy enforcement during crawl
    - _Requirements: 7.1, 7.2, 7.3_

  - [ ]* 9.2 Write unit tests for edge cases
    - Empty HTML page (no links to discover)
    - Single page with no outbound links
    - All links are cross-domain (same_domain=true results in empty discovery)
    - All links blocked by domain policy
    - Seed URL fetch failure (immediate error return)
    - Depth parameter boundaries (1, 5)
    - _Requirements: 1.4, 1.5, 2.2, 2.5, 2.6_

- [x] 10. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests use `pgregory.net/rapid` library (project already has this dependency)
- Property tests validate universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The implementation reuses existing `DiscoverURLsFromText`, `SaveURL`, and `enforceURLDomainPolicy` infrastructure
- Frontend component follows existing patterns in `KnowledgeSettingsPanel.tsx`
- Progress events use the existing Wails `runtime.EventsEmit` pattern

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["2.1", "3.1"] },
    { "id": 2, "tasks": ["2.2", "2.3", "2.4", "2.5"] },
    { "id": 3, "tasks": ["2.6", "2.7"] },
    { "id": 4, "tasks": ["5.1", "5.2", "6.1"] },
    { "id": 5, "tasks": ["5.3", "6.2", "6.3"] },
    { "id": 6, "tasks": ["6.4"] },
    { "id": 7, "tasks": ["8.1", "8.2", "8.3"] },
    { "id": 8, "tasks": ["9.1", "9.2"] }
  ]
}
```
