# 任务列表：智能工具路由 v2

## Task 1: EnrichmentStore 核心实现
- [x] 创建 `corelib/tool/enrichment.go`
- [x] 实现 EnrichmentStore (Load/Save/GetSearchText/Set)
- [x] 硬编码 BuiltinEnrichments (所有 core tools 的 synthetic queries)
- [x] GenerateEnrichmentPrompt / ParseEnrichmentResponse (MCP/Skill 工具 LLM 生成)
- [x] 创建 `corelib/tool/enrichment_test.go` — 9 个测试全部通过

## Task 2: Router/Builder 集成 EnrichmentStore
- [x] Router 新增 SetEnrichmentStore + buildSearchText 方法
- [x] Router.Route() 中 BM25 索引文本改用 enrichStore.GetSearchText()
- [x] DynamicToolBuilder 新增 SetEnrichmentStore 方法
- [x] DynamicToolBuilder.Build() 中 BM25 索引文本改用 enrichStore.GetSearchText()
- [x] gui/tool_router.go 透传 enrichStore
- [x] gui/tool_builder.go 透传 enrichStore

## Task 3: UsageTracker 核心实现
- [x] 创建 `corelib/tool/usage_tracker.go`
- [x] 实现 UsageTracker (Record/ExperienceScore/Load/Save)
- [x] ring buffer 逻辑 (maxItems=2000)
- [x] Jaccard token overlap + recency decay + success weight 算法
- [x] 创建 `corelib/tool/usage_tracker_test.go` — 7 个测试全部通过

## Task 4: Router/Builder 集成 UsageTracker + 三信号融合
- [x] Router 新增 SetUsageTracker 方法
- [x] Router.Route() 打分改为三信号融合 (α=0.6 retrieval + β=0.3 experience + γ=0.1 priority)
- [x] DynamicToolBuilder 新增 SetUsageTracker 方法
- [x] DynamicToolBuilder.Build() 打分改为三信号融合
- [x] gui/tool_router.go 透传 tracker
- [x] gui/tool_builder.go 透传 tracker
- [x] 已有 BM25 路由测试零回归

## Task 5: Session Tools + discover_tool
- [x] Router 新增 sessionTools map + ActivateSessionTool/ResetSession
- [x] Router.Route() 中 sessionTools 视为 core tools
- [x] discover_tool 加入 CoreToolNames + BuiltinEnrichments
- [x] gui/tool_router.go 透传 ActivateSessionTool/ResetSession
- [x] gui/tool_registry_builtin.go 注册 discover_tool handler
- [x] gui/tool_discover.go 实现 toolDiscoverTool handler (BM25 检索 + session 激活)
- [x] GUI 编译通过，corelib/tool 全量测试通过
