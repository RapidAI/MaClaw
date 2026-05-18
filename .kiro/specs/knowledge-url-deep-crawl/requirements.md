# Requirements Document

## Introduction

知识库 URL 深度检索功能增强。用户在设置→知识库中保存 URL 时，支持从种子 URL 出发，按广度优先策略发现同站链接，然后逐层深入抓取（可配置深度 1-5 层），将所有抓取到的页面内容保存为知识库来源。系统在抓取过程中遵守已有的 URL 域名策略（allow/block），自动去重，并提供实时进度反馈和抓取前预览。

## Glossary

- **Crawl_Engine**: 后端深度检索引擎，负责从种子 URL 出发执行 BFS 抓取、链接发现、去重和内容保存
- **Seed_URL**: 用户提供的起始 URL，作为深度检索的入口点
- **Crawl_Depth**: 从种子 URL 出发的链接跳转层数，第 0 层为种子页面本身，第 1 层为种子页面中发现的链接，以此类推
- **Same_Domain**: 与种子 URL 具有相同主机名（hostname）的链接
- **Domain_Policy**: 已有的 URL 域名策略管理系统（allow/block 规则），由 KnowledgeListURLDomainPolicies 和 KnowledgeUpdateURLDomainPolicies 管理
- **Discovery_Queue**: BFS 队列，按层级存储待抓取的 URL
- **Knowledge_Store**: 已有的知识库存储系统，通过 KnowledgeSaveURL/KnowledgeSaveURLs 接口保存 URL 内容
- **Deep_Crawl_Panel**: 前端知识库设置面板中的深度检索 UI 区域

## Requirements

### Requirement 1: 深度检索配置输入

**User Story:** As a 知识库用户, I want to 配置深度检索参数（种子 URL、抓取深度、域名限制）, so that 系统按我的意图范围抓取网页内容。

#### Acceptance Criteria

1. THE Deep_Crawl_Panel SHALL provide an input field for the user to enter a Seed_URL
2. THE Deep_Crawl_Panel SHALL provide a depth selector allowing the user to choose Crawl_Depth from 1 to 5
3. THE Deep_Crawl_Panel SHALL provide a toggle for restricting crawl to Same_Domain only, defaulting to enabled
4. WHEN the user submits an invalid URL (not starting with http:// or https://), THE Deep_Crawl_Panel SHALL display a validation error message
5. WHEN the user submits a URL blocked by Domain_Policy, THE Deep_Crawl_Panel SHALL display a policy rejection message with the blocking reason

### Requirement 2: 广度优先链接发现

**User Story:** As a 知识库用户, I want the system to 从种子页面出发按广度优先策略发现同站链接, so that 同一层级的页面被优先发现后再深入下一层。

#### Acceptance Criteria

1. WHEN a crawl is initiated, THE Crawl_Engine SHALL fetch the Seed_URL page content and extract all hyperlinks from the HTML
2. WHILE the Same_Domain restriction is enabled, THE Crawl_Engine SHALL only enqueue links whose hostname matches the Seed_URL hostname
3. THE Crawl_Engine SHALL process discovered URLs in breadth-first order, completing all URLs at depth N before processing depth N+1
4. THE Crawl_Engine SHALL skip URLs that have already been visited or are already in the Discovery_Queue (deduplication by normalized URL)
5. THE Crawl_Engine SHALL stop discovering new links when the current depth reaches the configured Crawl_Depth limit
6. WHEN a discovered URL is blocked by Domain_Policy, THE Crawl_Engine SHALL skip that URL and record the rejection reason

### Requirement 3: 抓取进度反馈

**User Story:** As a 知识库用户, I want to 实时看到深度检索的进度, so that 我知道系统正在工作以及当前进展。

#### Acceptance Criteria

1. WHILE a crawl is in progress, THE Deep_Crawl_Panel SHALL display the current crawl status including: total URLs discovered, URLs completed, URLs pending, current depth level
2. WHILE a crawl is in progress, THE Deep_Crawl_Panel SHALL display a progress bar or percentage indicator
3. WHEN a URL is successfully fetched and saved, THE Deep_Crawl_Panel SHALL update the completed count in real time
4. IF a URL fetch fails (network error, HTTP 4xx/5xx, timeout), THEN THE Crawl_Engine SHALL record the failure reason and continue with the next URL
5. THE Deep_Crawl_Panel SHALL provide a cancel button to stop the crawl at any time

### Requirement 4: 抓取前 URL 预览

**User Story:** As a 知识库用户, I want to 在正式抓取前预览将要抓取的 URL 列表, so that 我可以确认范围是否合理再开始。

#### Acceptance Criteria

1. WHEN the user clicks a "Preview" button, THE Crawl_Engine SHALL perform a lightweight discovery pass (fetch HTML and extract links without saving content) for the first 2 levels
2. THE Deep_Crawl_Panel SHALL display the preview results as a grouped list showing URLs per depth level
3. THE Deep_Crawl_Panel SHALL display the total count of discovered URLs in the preview
4. WHEN the user confirms the preview, THE Crawl_Engine SHALL proceed with the full crawl using the same configuration
5. WHEN the user cancels after preview, THE Crawl_Engine SHALL discard all preview data without saving anything

### Requirement 5: 内容保存到知识库

**User Story:** As a 知识库用户, I want 所有成功抓取的页面内容被保存为知识库来源, so that 我可以在后续对话中检索和引用这些内容。

#### Acceptance Criteria

1. WHEN a page is successfully fetched, THE Crawl_Engine SHALL save the page content to Knowledge_Store using the existing KnowledgeSaveURL interface
2. THE Crawl_Engine SHALL pass the user-configured save_scope, topic_hint, and labels to each saved URL source
3. THE Crawl_Engine SHALL skip saving URLs that already exist as sources in Knowledge_Store (duplicate detection by canonical URL)
4. WHEN all URLs in the crawl have been processed, THE Crawl_Engine SHALL return a summary report including: total saved, duplicates skipped, failures with reasons

### Requirement 6: 并发控制与资源保护

**User Story:** As a 系统管理员, I want the crawl engine to 限制并发请求数和总抓取量, so that 系统不会因过度抓取而耗尽资源或被目标站点封禁。

#### Acceptance Criteria

1. THE Crawl_Engine SHALL limit concurrent HTTP requests to a maximum of 3 simultaneous connections
2. THE Crawl_Engine SHALL enforce a minimum delay of 500 milliseconds between requests to the same host
3. THE Crawl_Engine SHALL enforce a maximum total URL limit of 200 URLs per single crawl session
4. IF the total discovered URLs exceed 200, THEN THE Crawl_Engine SHALL stop discovery and notify the user that the limit has been reached
5. THE Crawl_Engine SHALL enforce a per-URL fetch timeout of 30 seconds
6. THE Crawl_Engine SHALL enforce a total crawl session timeout of 10 minutes

### Requirement 7: 与现有基础设施集成

**User Story:** As a 开发者, I want the deep crawl feature to 复用已有的 KnowledgeDiscoverURLs 和 KnowledgeSaveURLs 接口, so that 不重复实现 URL 发现和保存逻辑。

#### Acceptance Criteria

1. THE Crawl_Engine SHALL use the existing KnowledgeDiscoverURLs API for extracting links from fetched HTML content
2. THE Crawl_Engine SHALL use the existing KnowledgeSaveURLs API for batch-saving crawled page content
3. THE Crawl_Engine SHALL respect all existing Domain_Policy rules during both discovery and save phases
4. THE Crawl_Engine SHALL be exposed as a new Wails binding (KnowledgeDeepCrawl) callable from the frontend
5. THE Crawl_Engine SHALL emit progress events via the existing Wails event system for real-time frontend updates
